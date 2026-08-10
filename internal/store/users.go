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

const (
	RoleGlobal      Role = "global"
	RoleDomainAdmin Role = "domain_admin"
)

// User is a panel login (not an application SASL account).
type User struct {
	ID               int64
	Username         string
	PasswordHash     string
	Role             Role
	DMARCReportEmail string
	CreatedAt        time.Time
	DomainIDs        []int64
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
		"INSERT INTO users (username, password_hash, role, dmarc_report_email, created_at) VALUES (?, ?, ?, '', ?)",
		username, passwordHash, RoleGlobal, now,
	)
	if err != nil {
		return fmt.Errorf("create global user: %w", err)
	}
	return nil
}

// GetUserByUsername returns a user with domain assignments loaded.
func (s *Store) GetUserByUsername(username string) (User, error) {
	var (
		u         User
		createdAt string
	)
	err := s.db.QueryRow(
		"SELECT id, username, password_hash, role, dmarc_report_email, created_at FROM users WHERE username = ?",
		username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.DMARCReportEmail, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user by username: %w", err)
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	u.DomainIDs, err = s.listUserDomainIDs(u.ID)
	if err != nil {
		return User{}, err
	}
	return u, nil
}

// GetUser returns a user by id with domain assignments.
func (s *Store) GetUser(id int64) (User, error) {
	var (
		u         User
		createdAt string
	)
	err := s.db.QueryRow(
		"SELECT id, username, password_hash, role, dmarc_report_email, created_at FROM users WHERE id = ?",
		id,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.DMARCReportEmail, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	u.DomainIDs, err = s.listUserDomainIDs(u.ID)
	if err != nil {
		return User{}, err
	}
	return u, nil
}

// ListUsers returns every panel user without domain ids.
func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(
		"SELECT id, username, password_hash, role, dmarc_report_email, created_at FROM users ORDER BY id",
	)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var (
			u         User
			createdAt string
		)
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.DMARCReportEmail, &createdAt); err != nil {
			return nil, fmt.Errorf("list users scan: %w", err)
		}
		u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		users = append(users, u)
	}
	return users, rows.Err()
}

// UserRow is a user plus assigned domain names for the management list.
type UserRow struct {
	User        User
	DomainNames []string
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
		names, err := s.listUserDomainNames(u.ID)
		if err != nil {
			return nil, err
		}
		rows[i].DomainNames = names
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

// CreateUser inserts a panel user and optional domain assignments.
func (s *Store) CreateUser(username, passwordHash string, role Role, domainIDs []int64) (int64, error) {
	if role == RoleDomainAdmin && len(domainIDs) == 0 {
		return 0, fmt.Errorf("create user: domain_admin requires domains")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		"INSERT INTO users (username, password_hash, role, dmarc_report_email, created_at) VALUES (?, ?, ?, '', ?)",
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
	if role == RoleDomainAdmin {
		if err := s.setUserDomains(id, domainIDs); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// UpdateUser replaces username, password hash, and DMARC email for a user.
func (s *Store) UpdateUser(id int64, username, passwordHash, dmarcReportEmail string) error {
	u, err := s.GetUser(id)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(
		"UPDATE users SET username = ?, password_hash = ?, dmarc_report_email = ? WHERE id = ?",
		username, passwordHash, dmarcReportEmail, id,
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
	if u.Role == RoleGlobal {
		if err := s.SetSetting("dmarc_report_email", dmarcReportEmail); err != nil {
			return err
		}
	}
	return nil
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

// ClearUserDomains removes all domain assignments for a user.
func (s *Store) ClearUserDomains(userID int64) error {
	_, err := s.db.Exec("DELETE FROM user_domains WHERE user_id = ?", userID)
	if err != nil {
		return fmt.Errorf("clear user domains: %w", err)
	}
	return nil
}

// SetUserDomains replaces domain assignments for a domain_admin user.
func (s *Store) SetUserDomains(userID int64, domainIDs []int64) error {
	u, err := s.GetUser(userID)
	if err != nil {
		return err
	}
	if u.Role != RoleDomainAdmin {
		return fmt.Errorf("set user domains: user is not domain_admin")
	}
	if len(domainIDs) == 0 {
		return fmt.Errorf("set user domains: at least one domain required")
	}
	return s.setUserDomains(userID, domainIDs)
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

// GlobalDMARCReportEmail returns the shared default rua= for domain inherit mode.
func (s *Store) GlobalDMARCReportEmail() (string, error) {
	return s.GetSetting("dmarc_report_email")
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

func (s *Store) setUserDomains(userID int64, domainIDs []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("set user domains begin: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM user_domains WHERE user_id = ?", userID); err != nil {
		tx.Rollback()
		return fmt.Errorf("set user domains clear: %w", err)
	}
	for _, did := range domainIDs {
		if _, err := tx.Exec("INSERT INTO user_domains (user_id, domain_id) VALUES (?, ?)", userID, did); err != nil {
			tx.Rollback()
			return fmt.Errorf("set user domains insert: %w", err)
		}
	}
	return tx.Commit()
}
