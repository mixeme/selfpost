package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

const (
	// SendLogRetentionDaysKey is the settings table key for delivery-journal
	// retention (architecture.md § Persistence).
	SendLogRetentionDaysKey = "send_log_retention_days"
	SendLogRetentionDaysMin = 7
	SendLogRetentionDaysMax = 365
	// SendLogRetentionDaysDefault matches SEND_LOG_RETENTION_DAYS when unset.
	SendLogRetentionDaysDefault = 90
)

// ErrSendLogRetentionDaysOutOfRange is returned when retention is outside the
// allowed panel range.
var ErrSendLogRetentionDaysOutOfRange = errors.New("send log retention days out of range")

// ValidateSendLogRetentionDays checks the panel-allowed retention window.
func ValidateSendLogRetentionDays(days int) error {
	if days < SendLogRetentionDaysMin || days > SendLogRetentionDaysMax {
		return fmt.Errorf("%w: must be between %d and %d days", ErrSendLogRetentionDaysOutOfRange, SendLogRetentionDaysMin, SendLogRetentionDaysMax)
	}
	return nil
}

func sendLogRetentionFallback(envDefault int) int {
	if envDefault > 0 {
		if err := ValidateSendLogRetentionDays(envDefault); err == nil {
			return envDefault
		}
	}
	return SendLogRetentionDaysDefault
}

// GetSendLogRetentionDays returns the effective retention window. When the
// setting is missing or invalid, envDefault is used (guide § Environment
// variables: SEND_LOG_RETENTION_DAYS).
func (s *Store) GetSendLogRetentionDays(envDefault int) (int, error) {
	raw, err := s.GetSetting(SendLogRetentionDaysKey)
	if err != nil {
		return 0, err
	}
	if raw == "" {
		return sendLogRetentionFallback(envDefault), nil
	}
	days, err := strconv.Atoi(raw)
	if err != nil || ValidateSendLogRetentionDays(days) != nil {
		return sendLogRetentionFallback(envDefault), nil
	}
	return days, nil
}

// SetSendLogRetentionDays persists the panel-configured retention window.
func (s *Store) SetSendLogRetentionDays(days int) error {
	if err := ValidateSendLogRetentionDays(days); err != nil {
		return err
	}
	return s.SetSetting(SendLogRetentionDaysKey, strconv.Itoa(days))
}

// EnsureSendLogRetentionDays seeds the setting from envDefault when it has
// never been written (first panel start after upgrade).
func (s *Store) EnsureSendLogRetentionDays(envDefault int) error {
	raw, err := s.GetSetting(SendLogRetentionDaysKey)
	if err != nil {
		return err
	}
	if raw != "" {
		return nil
	}
	return s.SetSendLogRetentionDays(sendLogRetentionFallback(envDefault))
}

// GetSetting returns a settings value or empty string when missing.
func (s *Store) GetSetting(key string) (string, error) {
	var value string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get setting %s: %w", key, err)
	}
	return value, nil
}

// SetSetting upserts a settings key.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value,
	)
	if err != nil {
		return fmt.Errorf("set setting %s: %w", key, err)
	}
	return nil
}
