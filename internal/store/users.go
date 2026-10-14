package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNoUser is returned when primary setup has not happened yet.
var ErrNoUser = errors.New("no panel user")

// ErrUserNotFound is returned when a panel user id or username does not exist.
var ErrUserNotFound = errors.New("user not found")

// ErrUserExists is returned when a username is already taken.
var ErrUserExists = errors.New("username already taken")

// ErrNoSuchDomain is returned when a user is assigned a domain that does not
// exist - one deleted while the users form was open.
var ErrNoSuchDomain = errors.New("an assigned domain does not exist")

// ErrLastGlobal is returned when deleting or demoting the last global user.
var ErrLastGlobal = errors.New("cannot remove last global administrator")

// Role identifies a panel user's access level.
type Role string

// The role is a reach, not a rank: everyone in the panel is an administrator,
// of the whole instance or of the domains assigned to them.
const (
	RoleGlobal Role = "global"
	RoleDomain Role = "domain"
)

// User is a panel login (not an application SASL account).
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         Role
	// Email is the user's own address — what the panel writes to, and what the
	// button beside a domain's typed report address fills it with.
	Email string
	// AllDomains / AllInboundDomains widen a domain user's list to every
	// domain of that direction, including those added later; the id lists
	// are then ignored.
	AllDomains        bool
	AllInboundDomains bool
	// Notify is the user's own switch for notification e-mails (Account). It
	// is on for a new user and does nothing without an e-mail.
	Notify           bool
	CreatedAt        time.Time
	DomainIDs        []int64
	InboundDomainIDs []int64
}

const userColumns = "id, username, password_hash, role, email, all_domains, all_inbound_domains, notify, created_at"

func scanUser(r scanRow) (User, error) {
	var (
		u         User
		createdAt string
	)
	err := r.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Email,
		&u.AllDomains, &u.AllInboundDomains, &u.Notify, &createdAt)
	if err != nil {
		return User{}, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return u, nil
}

// UserExists reports whether any panel user exists (setup complete).
func (s *Store) UserExists() (bool, error) {
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&n); err != nil {
		return false, fmt.Errorf("count users: %w", err)
	}
	return n > 0, nil
}

// CreateGlobalUser inserts the first global user during setup. The check that
// no user exists yet and the insert are one statement, so two setup requests
// at once cannot both create one (audit 2026-10-12, M7).
// SQLite 3.53.4, https://sqlite.org/lang_transaction.html § 2.1: an INSERT
// starts a write transaction, one at a time, so its SELECT reads what is
// committed; an insert of no row leaves sqlite3_changes() at 0
// (https://sqlite.org/c3ref/changes.html).
func (s *Store) CreateGlobalUser(username, passwordHash string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT INTO users (username, password_hash, role, created_at)
		 SELECT ?, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM users)`,
		username, passwordHash, RoleGlobal, now,
	)
	if err != nil {
		return fmt.Errorf("create global user: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("create global user: users already exist")
	}
	return nil
}

// GetUserByUsername returns a user with domain assignments loaded.
func (s *Store) GetUserByUsername(username string) (User, error) {
	u, err := scanUser(s.db.QueryRow("SELECT "+userColumns+" FROM users WHERE username = ?", username))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user by username: %w", err)
	}
	if u.DomainIDs, err = s.listUserDomainIDs(u.ID); err != nil {
		return User{}, err
	}
	if u.InboundDomainIDs, err = s.listUserInboundDomainIDs(u.ID); err != nil {
		return User{}, err
	}
	return u, nil
}

// GetUser returns a user by id with domain assignments.
func (s *Store) GetUser(id int64) (User, error) {
	u, err := scanUser(s.db.QueryRow("SELECT "+userColumns+" FROM users WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}
	if u.DomainIDs, err = s.listUserDomainIDs(u.ID); err != nil {
		return User{}, err
	}
	if u.InboundDomainIDs, err = s.listUserInboundDomainIDs(u.ID); err != nil {
		return User{}, err
	}
	return u, nil
}

// ListUsers returns every panel user without the assigned domain ids.
func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query("SELECT " + userColumns + " FROM users ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("list users scan: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// Reach is what a domain user is assigned: outbound and inbound domains are
// granted separately, and the same name on both lists is two assignments. All
// means every domain of that direction, including the ones added later; the
// id list beside it is then ignored. Reach widens a list, never the role:
// adding and deleting domains, the queues and Server stay with the global role.
type Reach struct {
	AllDomains       bool
	DomainIDs        []int64
	AllInbound       bool
	InboundDomainIDs []int64
}

// Empty reports whether the reach assigns nothing at all. A domain user must
// have something on at least one of the two lists.
func (r Reach) Empty() bool {
	return !r.AllDomains && len(r.DomainIDs) == 0 && !r.AllInbound && len(r.InboundDomainIDs) == 0
}

// Reach returns what the user is assigned.
func (u User) Reach() Reach {
	return Reach{AllDomains: u.AllDomains, DomainIDs: u.DomainIDs, AllInbound: u.AllInboundDomains, InboundDomainIDs: u.InboundDomainIDs}
}

// ErrEmptyReach is returned when a domain user would be left with nothing
// assigned on either list.
var ErrEmptyReach = errors.New("a domain user needs at least one outbound or inbound domain")

// UserRow is a user plus the names of the assigned domains for the management
// list. The names are empty where the user's All flag covers the direction.
type UserRow struct {
	User               User
	DomainNames        []string
	InboundDomainNames []string
}

// ListUserRows returns users with assigned domain names for the management UI.
func (s *Store) ListUserRows() ([]UserRow, error) {
	users, err := s.ListUsers()
	if err != nil {
		return nil, err
	}
	rows := make([]UserRow, len(users))
	for i, u := range users {
		rows[i].User = u
		if u.Role == RoleGlobal {
			continue
		}
		if !u.AllDomains {
			if rows[i].DomainNames, err = s.listUserDomainNames(u.ID); err != nil {
				return nil, err
			}
		}
		if !u.AllInboundDomains {
			if rows[i].InboundDomainNames, err = s.listUserInboundDomainNames(u.ID); err != nil {
				return nil, err
			}
		}
	}
	return rows, nil
}

// CountGlobalUsers returns how many global-role users exist.
func (s *Store) CountGlobalUsers() (int, error) {
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM users WHERE role = ?", RoleGlobal).Scan(&n); err != nil {
		return 0, fmt.Errorf("count global users: %w", err)
	}
	return n, nil
}

// CreateUser inserts a panel user without an account e-mail; see AddUser.
func (s *Store) CreateUser(username, passwordHash string, role Role, reach Reach) (int64, error) {
	return s.AddUser(UserChange{Username: username, PasswordHash: passwordHash, Role: role, Reach: reach})
}

// UserChange is everything the users form sets on a user, written at once by
// AddUser and SaveUser. ID is ignored by AddUser; SaveUser keeps the stored
// password hash when PasswordHash is empty.
type UserChange struct {
	ID                            int64
	Username, PasswordHash, Email string
	Role                          Role
	Reach                         Reach // a domain user's; ignored for a global one
}

// AddUser inserts a panel user with its e-mail and, for a domain user, its
// reach, in one transaction: a domain user must be given one (ErrEmptyReach),
// and a failure part way leaves no user rather than one without it (audit
// 2026-10-12, M9).
func (s *Store) AddUser(c UserChange) (int64, error) {
	if err := knownRole(c.Role); err != nil {
		return 0, err
	}
	if c.Role == RoleDomain && c.Reach.Empty() {
		return 0, ErrEmptyReach
	}
	// The first statement is a write (see UpdateStatus).
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("create user: %w", err)
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := tx.Exec(
		"INSERT INTO users (username, password_hash, role, email, created_at) VALUES (?, ?, ?, ?, ?)",
		c.Username, c.PasswordHash, c.Role, c.Email, now,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrUserExists
		}
		return 0, fmt.Errorf("create user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("create user id: %w", err)
	}
	if c.Role == RoleDomain {
		if err := writeUserReach(tx, id, c.Reach); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("create user: %w", err)
	}
	return id, nil
}

// SaveUser writes what the users form changed - username, password hash,
// e-mail, role and reach - in one transaction (audit 2026-10-12, M9: five
// writes in a row left, on a failure part way, a user renamed with the old
// role, or a domain user with nothing assigned). A domain user must keep a
// reach (ErrEmptyReach); a user made global loses theirs, so a later demotion
// does not bring an old assignment back. The last global administrator cannot
// be demoted (ErrLastGlobal): the count is taken inside the transaction,
// after the write, so two administrators demoting each other at once cannot
// both succeed (M7).
func (s *Store) SaveUser(c UserChange) error {
	if err := knownRole(c.Role); err != nil {
		return err
	}
	if c.Role == RoleDomain && c.Reach.Empty() {
		return ErrEmptyReach
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("save user: %w", err)
	}
	defer tx.Rollback()
	// A write first: it takes the write lock, so what is counted below is
	// what is committed. SQLite 3.53.4, https://sqlite.org/lang_transaction.html
	// § 2.2: after a deferred BEGIN, a first statement that writes starts a
	// write transaction, and § 2.1: there is one at a time. RowsAffected counts
	// the row its WHERE found even when no value changes: update.c
	// sqlite3Update() writes every such row through sqlite3CompleteInsertion(),
	// which marks it counted (OPFLAG_NCHANGE).
	// An empty hash keeps the stored one: the form read it when it was
	// opened, and the user may have changed it on Account since.
	res, err := tx.Exec(
		"UPDATE users SET username = ?, password_hash = COALESCE(NULLIF(?, ''), password_hash), email = ?, role = ? WHERE id = ?",
		c.Username, c.PasswordHash, c.Email, c.Role, c.ID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrUserExists
		}
		return fmt.Errorf("save user: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUserNotFound
	}
	if err := requireAGlobal(tx); err != nil {
		return err
	}
	reach := c.Reach
	if c.Role == RoleGlobal {
		reach = Reach{}
	}
	if err := writeUserReach(tx, c.ID, reach); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("save user: %w", err)
	}
	return nil
}

// knownRole refuses a role that is neither global nor domain. The handlers
// check the form first; this is the second line.
func knownRole(r Role) error {
	if r != RoleGlobal && r != RoleDomain {
		return fmt.Errorf("unknown role %q", r)
	}
	return nil
}

// requireAGlobal is ErrLastGlobal when the transaction has left no global
// user.
func requireAGlobal(tx *sql.Tx) error {
	var n int
	if err := tx.QueryRow("SELECT COUNT(*) FROM users WHERE role = ?", RoleGlobal).Scan(&n); err != nil {
		return fmt.Errorf("count global users: %w", err)
	}
	if n == 0 {
		return ErrLastGlobal
	}
	return nil
}

// UpdateUser replaces a user's username, password hash and account e-mail.
func (s *Store) UpdateUser(id int64, username, passwordHash, email string) error {
	res, err := s.db.Exec(
		"UPDATE users SET username = ?, password_hash = ?, email = ? WHERE id = ?",
		username, passwordHash, email, id,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrUserExists
		}
		return fmt.Errorf("update user: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ClearUserReach removes everything a user is assigned — both lists and both
// All flags. A user promoted to the global role keeps none of it, so a later
// demotion does not quietly bring an old assignment back.
func (s *Store) ClearUserReach(userID int64) error {
	return s.setUserReach(userID, Reach{})
}

// SetUserReach replaces what a domain user is assigned, both directions at
// once. ErrEmptyReach if nothing would be left.
func (s *Store) SetUserReach(userID int64, reach Reach) error {
	u, err := s.GetUser(userID)
	if err != nil {
		return err
	}
	if u.Role != RoleDomain {
		return fmt.Errorf("set user reach: user does not have the domain role")
	}
	if reach.Empty() {
		return ErrEmptyReach
	}
	return s.setUserReach(userID, reach)
}

// DeleteUser removes a panel user. ErrLastGlobal when deleting the only global
// user: the delete and the count are one transaction, so two administrators
// deleting each other at once cannot both succeed (audit 2026-10-12, M7). The
// DELETE comes first and so takes the write lock (see SaveUser); the rows its
// cascade removes are not in RowsAffected (https://sqlite.org/c3ref/changes.html).
func (s *Store) DeleteUser(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec("DELETE FROM users WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUserNotFound
	}
	if err := requireAGlobal(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

func (s *Store) listUserInboundDomainIDs(userID int64) ([]int64, error) {
	rows, err := s.db.Query(
		"SELECT inbound_domain_id FROM user_inbound_domains WHERE user_id = ? ORDER BY inbound_domain_id", userID)
	if err != nil {
		return nil, fmt.Errorf("list user inbound domains: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("list user inbound domains scan: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) listUserDomainIDs(userID int64) ([]int64, error) {
	rows, err := s.db.Query("SELECT domain_id FROM user_domains WHERE user_id = ? ORDER BY domain_id", userID)
	if err != nil {
		return nil, fmt.Errorf("list user domains: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("list user domains scan: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) listUserInboundDomainNames(userID int64) ([]string, error) {
	rows, err := s.db.Query(
		"SELECT d.name FROM user_inbound_domains ud JOIN inbound_domains d ON d.id = ud.inbound_domain_id WHERE ud.user_id = ? ORDER BY d.name",
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list user inbound domain names: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("list user inbound domain names scan: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func (s *Store) listUserDomainNames(userID int64) ([]string, error) {
	rows, err := s.db.Query(
		"SELECT d.name FROM user_domains ud JOIN domains d ON d.id = ud.domain_id WHERE ud.user_id = ? ORDER BY d.name",
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list user domain names: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("list user domain names scan: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// setUserReach writes the flags and both assignment tables in one transaction.
func (s *Store) setUserReach(userID int64, reach Reach) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("set user reach begin: %w", err)
	}
	defer tx.Rollback()
	if err := writeUserReach(tx, userID, reach); err != nil {
		return err
	}
	return tx.Commit()
}

// writeUserReach writes a user's reach inside tx. With an All flag set the
// rows of that direction are dropped rather than stored: the flag already
// covers them, and stale rows would come back to life the day the flag is
// cleared. Its first statement is a write (see UpdateStatus). A domain deleted
// meanwhile fails its INSERT at once: SQLite 3.53.4's foreign keys are
// immediate unless declared deferred (https://sqlite.org/foreignkeys.html § 4.2),
// and the code is SQLITE_CONSTRAINT_FOREIGNKEY (isForeignKeyViolation).
func writeUserReach(tx *sql.Tx, userID int64, reach Reach) error {
	if _, err := tx.Exec("UPDATE users SET all_domains = ?, all_inbound_domains = ? WHERE id = ?",
		reach.AllDomains, reach.AllInbound, userID); err != nil {
		return fmt.Errorf("set user reach flags: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM user_domains WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("set user reach clear: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM user_inbound_domains WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("set user reach clear inbound: %w", err)
	}
	if !reach.AllDomains {
		for _, id := range reach.DomainIDs {
			if _, err := tx.Exec("INSERT INTO user_domains (user_id, domain_id) VALUES (?, ?)", userID, id); err != nil {
				if isForeignKeyViolation(err) {
					return ErrNoSuchDomain
				}
				return fmt.Errorf("set user reach insert: %w", err)
			}
		}
	}
	if !reach.AllInbound {
		for _, id := range reach.InboundDomainIDs {
			if _, err := tx.Exec("INSERT INTO user_inbound_domains (user_id, inbound_domain_id) VALUES (?, ?)", userID, id); err != nil {
				if isForeignKeyViolation(err) {
					return ErrNoSuchDomain
				}
				return fmt.Errorf("set user reach insert inbound: %w", err)
			}
		}
	}
	return nil
}
