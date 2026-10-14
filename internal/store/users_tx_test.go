package store

import (
	"errors"
	"sync"
	"testing"
)

// Two global administrators who demote, or delete, each other at the same
// moment: one of them must stay global. The count is taken inside the
// transaction that writes, so the second sees the first's change (audit
// 2026-10-12, M7).
func TestTheLastGlobalSurvivesTwoAtOnce(t *testing.T) {
	for _, how := range []string{"demote", "delete"} {
		t.Run(how, func(t *testing.T) {
			st := openTestStore(t)
			d := addTestDomain(t, st, "example.com")
			var ids []int64
			for _, name := range []string{"ann", "bob"} {
				id, err := st.CreateUser(name, "hash", RoleGlobal, Reach{})
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, id)
			}
			var wg sync.WaitGroup
			errs := make([]error, len(ids))
			for i, id := range ids {
				wg.Add(1)
				go func(i int, id int64) {
					defer wg.Done()
					if how == "delete" {
						errs[i] = st.DeleteUser(id)
						return
					}
					errs[i] = st.SaveUser(UserChange{ID: id, Username: []string{"ann", "bob"}[i], PasswordHash: "hash",
						Role: RoleDomain, Reach: Reach{DomainIDs: []int64{d.ID}}})
				}(i, id)
			}
			wg.Wait()
			refused := 0
			for _, err := range errs {
				switch {
				case errors.Is(err, ErrLastGlobal):
					refused++
				case err != nil:
					t.Fatalf("unexpected error: %v", err)
				}
			}
			n, err := st.CountGlobalUsers()
			if err != nil {
				t.Fatal(err)
			}
			if refused != 1 || n != 1 {
				t.Fatalf("%d refused, %d global users left; want 1 and 1", refused, n)
			}
		})
	}
}

// SaveUser writes all of a change or none of it.
func TestSaveUserIsOneChange(t *testing.T) {
	st := openTestStore(t)
	d := addTestDomain(t, st, "example.com")
	if _, err := st.CreateUser("root", "hash", RoleGlobal, Reach{}); err != nil {
		t.Fatal(err)
	}
	id, err := st.CreateUser("shop", "hash", RoleDomain, Reach{DomainIDs: []int64{d.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser("taken", "hash", RoleGlobal, Reach{}); err != nil {
		t.Fatal(err)
	}

	// A taken name: nothing else of the change is written either.
	err = st.SaveUser(UserChange{ID: id, Username: "taken", PasswordHash: "new", Email: "x@example.com", Role: RoleGlobal})
	if !errors.Is(err, ErrUserExists) {
		t.Fatalf("SaveUser with a taken name = %v", err)
	}
	// A reach naming a domain that does not exist fails at the last write.
	err = st.SaveUser(UserChange{ID: id, Username: "renamed", PasswordHash: "new", Role: RoleDomain, Reach: Reach{DomainIDs: []int64{9999}}})
	if err == nil {
		t.Fatal("SaveUser with a reach to no domain succeeded")
	}
	if err := st.SaveUser(UserChange{ID: id, Username: "renamed", PasswordHash: "new", Role: RoleDomain}); !errors.Is(err, ErrEmptyReach) {
		t.Fatalf("SaveUser of a domain user with nothing assigned = %v", err)
	}
	u, err := st.GetUser(id)
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "shop" || u.PasswordHash != "hash" || u.Email != "" || u.Role != RoleDomain || len(u.DomainIDs) != 1 {
		t.Fatalf("a refused change left %+v", u)
	}

	// Made global, the user loses the reach.
	if err := st.SaveUser(UserChange{ID: id, Username: "shop", PasswordHash: "hash", Email: "s@example.com", Role: RoleGlobal, Reach: Reach{DomainIDs: []int64{d.ID}}}); err != nil {
		t.Fatal(err)
	}
	u, _ = st.GetUser(id)
	if u.Role != RoleGlobal || u.Email != "s@example.com" || !u.Reach().Empty() {
		t.Fatalf("promoted: %+v", u)
	}
}

// AddUser writes the user, the e-mail and the reach at once; a reach that
// cannot be written leaves no user behind.
func TestAddUserIsOneChange(t *testing.T) {
	st := openTestStore(t)
	d := addTestDomain(t, st, "example.com")
	id, err := st.AddUser(UserChange{Username: "shop", PasswordHash: "hash", Email: "s@example.com", Role: RoleDomain, Reach: Reach{DomainIDs: []int64{d.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := st.GetUser(id)
	if u.Email != "s@example.com" || len(u.DomainIDs) != 1 {
		t.Fatalf("created %+v", u)
	}
	if _, err := st.AddUser(UserChange{Username: "ghost", PasswordHash: "hash", Role: RoleDomain, Reach: Reach{DomainIDs: []int64{9999}}}); err == nil {
		t.Fatal("AddUser with a reach to no domain succeeded")
	}
	if _, err := st.GetUserByUsername("ghost"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("the failed create left a user: %v", err)
	}
}

// Setup creates the first user once, however many requests race for it.
func TestCreateGlobalUserOnlyOnce(t *testing.T) {
	st := openTestStore(t)
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = st.CreateGlobalUser([]string{"a", "b", "c", "d"}[i], "hash")
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		}
	}
	n, _ := st.CountGlobalUsers()
	if ok != 1 || n != 1 {
		t.Fatalf("%d setups succeeded, %d global users; want 1 and 1", ok, n)
	}
}
