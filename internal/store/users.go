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

// How a user's default DMARC report address is chosen (users.dmarc_default_mode).
const (
	DMARCDefaultHosted  = "hosted"  // SelfPost's own mailbox for the domain
	DMARCDefaultAccount = "account" // the user's account e-mail
	DMARCDefaultCustom  = "custom"  // DMARCDefault.Address
	DMARCDefaultNone    = "none"    // no aggregate reports
)

// DMARCDefault is a user's default report address: the choice a domain takes
// over when it follows that user (Domain.DMARCRuaUserID).
type DMARCDefault struct {
	UserID   int64
	Username string
	Mode     string
	Address  string // for DMARCDefaultCustom
	Email    string // the account e-mail, for DMARCDefaultAccount
}

// Resolve returns the address the default stands for on one domain. hosted is
// the SelfPost-hosted mailbox for that domain, "" when report ingest is off —
// a hosted default then resolves to no reports.
func (d DMARCDefault) Resolve(hosted string) string {
	switch d.Mode {
	case DMARCDefaultHosted:
		return hosted
	case DMARCDefaultAccount:
		return d.Email
	case DMARCDefaultCustom:
		return d.Address
	}
	return ""
}

// ValidDMARCDefaultMode reports whether mode is one of the stored choices.
func ValidDMARCDefaultMode(mode string) bool {
	switch mode {
	case DMARCDefaultHosted, DMARCDefaultAccount, DMARCDefaultCustom, DMARCDefaultNone:
		return true
	}
	return false
}

// User is a panel login (not an application SASL account).
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         Role
	// Email is the user's own address — what the panel writes to. DMARC may
	// use it (DMARCDefaultAccount) but it is not a DMARC field.
	Email               string
	DMARCDefaultMode    string
	DMARCDefaultAddress string
	// AllDomains / AllInboundDomains widen a domain user's list to every
	// domain of that direction, including those added later; the id lists
	// are then ignored.
	AllDomains        bool
	AllInboundDomains bool
	CreatedAt         time.Time
	DomainIDs         []int64
	InboundDomainIDs  []int64
}

// DMARCDefault returns the user's default report address choice.
func (u User) DMARCDefault() DMARCDefault {
	return DMARCDefault{UserID: u.ID, Username: u.Username, Mode: u.DMARCDefaultMode, Address: u.DMARCDefaultAddress, Email: u.Email}
}

const userColumns = "id, username, password_hash, role, email, dmarc_default_mode, dmarc_default_address, all_domains, all_inbound_domains, created_at"

func scanUser(r scanRow) (User, error) {
	var (
		u         User
		createdAt string
	)
	err := r.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Email, &u.DMARCDefaultMode, &u.DMARCDefaultAddress,
		&u.AllDomains, &u.AllInboundDomains, &createdAt)
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

// CreateGlobalUser inserts the first global user during setup.
func (s *Store) CreateGlobalUser(username, passwordHash string) error {
	exists, err := s.UserExists()
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("create global user: users already exist")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.Exec(
		"INSERT INTO users (username, password_hash, role, created_at) VALUES (?, ?, ?, ?)",
		username, passwordHash, RoleGlobal, now,
	)
	if err != nil {
		return fmt.Errorf("create global user: %w", err)
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

// CreateUser inserts a panel user. A domain user is created with its reach in
// the same call and must be given one (ErrEmptyReach); for a global user the
// reach is ignored.
func (s *Store) CreateUser(username, passwordHash string, role Role, reach Reach) (int64, error) {
	if role == RoleDomain && reach.Empty() {
		return 0, ErrEmptyReach
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		"INSERT INTO users (username, password_hash, role, created_at) VALUES (?, ?, ?, ?)",
		username, passwordHash, role, now,
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
	if role == RoleDomain {
		if err := s.setUserReach(id, reach); err != nil {
			return 0, err
		}
	}
	return id, nil
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

// SetDMARCDefault stores the user's default report address choice. address is
// kept only for DMARCDefaultCustom. Every domain that follows this user
// resolves to the new choice from the next read on.
func (s *Store) SetDMARCDefault(userID int64, mode, address string) error {
	if !ValidDMARCDefaultMode(mode) {
		return fmt.Errorf("set dmarc default: unknown mode %q", mode)
	}
	if mode != DMARCDefaultCustom {
		address = ""
	}
	res, err := s.db.Exec(
		"UPDATE users SET dmarc_default_mode = ?, dmarc_default_address = ? WHERE id = ?", mode, address, userID)
	if err != nil {
		return fmt.Errorf("set dmarc default: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set dmarc default: %w", err)
	}
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// GetDMARCDefault returns a user's default report address choice, or
// ErrUserNotFound.
func (s *Store) GetDMARCDefault(userID int64) (DMARCDefault, error) {
	d := DMARCDefault{UserID: userID}
	err := s.db.QueryRow(
		"SELECT username, dmarc_default_mode, dmarc_default_address, email FROM users WHERE id = ?", userID,
	).Scan(&d.Username, &d.Mode, &d.Address, &d.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return DMARCDefault{}, ErrUserNotFound
	}
	if err != nil {
		return DMARCDefault{}, fmt.Errorf("get dmarc default: %w", err)
	}
	return d, nil
}

// SetUserRole updates a user's role.
func (s *Store) SetUserRole(userID int64, role Role) error {
	res, err := s.db.Exec("UPDATE users SET role = ? WHERE id = ?", role, userID)
	if err != nil {
		return fmt.Errorf("set user role: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set user role: %w", err)
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

// DeleteUser removes a panel user. ErrLastGlobal when deleting the only global user.
func (s *Store) DeleteUser(id int64) error {
	u, err := s.GetUser(id)
	if err != nil {
		return err
	}
	if u.Role == RoleGlobal {
		n, err := s.CountGlobalUsers()
		if err != nil {
			return err
		}
		if n <= 1 {
			return ErrLastGlobal
		}
	}
	res, err := s.db.Exec("DELETE FROM users WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if n == 0 {
		return ErrUserNotFound
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
// With an All flag set the rows of that direction are dropped rather than
// stored: the flag already covers them, and stale rows would come back to life
// the day the flag is cleared.
func (s *Store) setUserReach(userID int64, reach Reach) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("set user reach begin: %w", err)
	}
	defer tx.Rollback()
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
				return fmt.Errorf("set user reach insert: %w", err)
			}
		}
	}
	if !reach.AllInbound {
		for _, id := range reach.InboundDomainIDs {
			if _, err := tx.Exec("INSERT INTO user_inbound_domains (user_id, inbound_domain_id) VALUES (?, ?)", userID, id); err != nil {
				return fmt.Errorf("set user reach insert inbound: %w", err)
			}
		}
	}
	return tx.Commit()

}
