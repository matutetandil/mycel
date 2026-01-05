package auth

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// PostgresUserStore implements UserStore using PostgreSQL
type PostgresUserStore struct {
	db     *sql.DB
	config *UsersConfig
}

// NewPostgresUserStore creates a new PostgreSQL user store
func NewPostgresUserStore(db *sql.DB, config *UsersConfig) *PostgresUserStore {
	// Set defaults for field mappings
	if config.Fields == nil {
		config.Fields = &UserFieldsConfig{
			ID:           "id",
			Email:        "email",
			PasswordHash: "password_hash",
			CreatedAt:    "created_at",
			UpdatedAt:    "updated_at",
		}
	}
	return &PostgresUserStore{
		db:     db,
		config: config,
	}
}

// getTableName returns the configured table name
func (s *PostgresUserStore) getTableName() string {
	if s.config != nil && s.config.Table != "" {
		return s.config.Table
	}
	return "users"
}

// getFields returns the configured field mappings
func (s *PostgresUserStore) getFields() *UserFieldsConfig {
	if s.config != nil && s.config.Fields != nil {
		return s.config.Fields
	}
	return &UserFieldsConfig{
		ID:           "id",
		Email:        "email",
		PasswordHash: "password_hash",
		CreatedAt:    "created_at",
		UpdatedAt:    "updated_at",
	}
}

// Create creates a new user
func (s *PostgresUserStore) Create(ctx context.Context, user *User) error {
	table := s.getTableName()
	fields := s.getFields()

	query := fmt.Sprintf(`
		INSERT INTO %s (%s, %s, %s, %s, %s)
		VALUES ($1, $2, $3, $4, $5)
	`, table, fields.ID, fields.Email, fields.PasswordHash, fields.CreatedAt, fields.UpdatedAt)

	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, query,
		user.ID,
		user.Email,
		user.PasswordHash,
		user.CreatedAt,
		user.UpdatedAt,
	)
	if err != nil {
		// Check for unique constraint violation
		if isUniqueViolation(err) {
			return ErrUserAlreadyExists
		}
		return fmt.Errorf("failed to create user: %w", err)
	}

	return nil
}

// GetByID retrieves a user by ID
func (s *PostgresUserStore) GetByID(ctx context.Context, id string) (*User, error) {
	table := s.getTableName()
	fields := s.getFields()

	query := fmt.Sprintf(`
		SELECT %s, %s, %s, %s, %s
		FROM %s
		WHERE %s = $1
	`, fields.ID, fields.Email, fields.PasswordHash, fields.CreatedAt, fields.UpdatedAt,
		table, fields.ID)

	user := &User{}
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user by ID: %w", err)
	}

	return user, nil
}

// GetByEmail retrieves a user by email
func (s *PostgresUserStore) GetByEmail(ctx context.Context, email string) (*User, error) {
	table := s.getTableName()
	fields := s.getFields()

	query := fmt.Sprintf(`
		SELECT %s, %s, %s, %s, %s
		FROM %s
		WHERE %s = $1
	`, fields.ID, fields.Email, fields.PasswordHash, fields.CreatedAt, fields.UpdatedAt,
		table, fields.Email)

	user := &User{}
	err := s.db.QueryRowContext(ctx, query, email).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}

	return user, nil
}

// Update updates an existing user
func (s *PostgresUserStore) Update(ctx context.Context, user *User) error {
	table := s.getTableName()
	fields := s.getFields()

	user.UpdatedAt = time.Now()

	query := fmt.Sprintf(`
		UPDATE %s
		SET %s = $1, %s = $2, %s = $3
		WHERE %s = $4
	`, table, fields.Email, fields.PasswordHash, fields.UpdatedAt, fields.ID)

	result, err := s.db.ExecContext(ctx, query,
		user.Email,
		user.PasswordHash,
		user.UpdatedAt,
		user.ID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrUserAlreadyExists
		}
		return fmt.Errorf("failed to update user: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check affected rows: %w", err)
	}
	if rowsAffected == 0 {
		return ErrUserNotFound
	}

	return nil
}

// Delete removes a user
func (s *PostgresUserStore) Delete(ctx context.Context, id string) error {
	table := s.getTableName()
	fields := s.getFields()

	query := fmt.Sprintf(`DELETE FROM %s WHERE %s = $1`, table, fields.ID)

	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check affected rows: %w", err)
	}
	if rowsAffected == 0 {
		return ErrUserNotFound
	}

	return nil
}

// PostgresPasswordHistoryStore stores password history for users
type PostgresPasswordHistoryStore struct {
	db    *sql.DB
	table string
}

// NewPostgresPasswordHistoryStore creates a new password history store
func NewPostgresPasswordHistoryStore(db *sql.DB, table string) *PostgresPasswordHistoryStore {
	if table == "" {
		table = "password_history"
	}
	return &PostgresPasswordHistoryStore{
		db:    db,
		table: table,
	}
}

// AddPasswordHash adds a password hash to history
func (s *PostgresPasswordHistoryStore) AddPasswordHash(ctx context.Context, userID, hash string) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (user_id, password_hash, created_at)
		VALUES ($1, $2, $3)
	`, s.table)

	_, err := s.db.ExecContext(ctx, query, userID, hash, time.Now())
	if err != nil {
		return fmt.Errorf("failed to add password to history: %w", err)
	}

	return nil
}

// GetRecentHashes returns the N most recent password hashes for a user
func (s *PostgresPasswordHistoryStore) GetRecentHashes(ctx context.Context, userID string, count int) ([]string, error) {
	query := fmt.Sprintf(`
		SELECT password_hash
		FROM %s
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, s.table)

	rows, err := s.db.QueryContext(ctx, query, userID, count)
	if err != nil {
		return nil, fmt.Errorf("failed to get password history: %w", err)
	}
	defer rows.Close()

	var hashes []string
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, fmt.Errorf("failed to scan password hash: %w", err)
		}
		hashes = append(hashes, hash)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating password history: %w", err)
	}

	return hashes, nil
}

// CleanOldHashes removes password hashes older than the specified count
func (s *PostgresPasswordHistoryStore) CleanOldHashes(ctx context.Context, userID string, keepCount int) error {
	// Delete all except the most recent N hashes
	query := fmt.Sprintf(`
		DELETE FROM %s
		WHERE user_id = $1
		AND id NOT IN (
			SELECT id FROM %s
			WHERE user_id = $1
			ORDER BY created_at DESC
			LIMIT $2
		)
	`, s.table, s.table)

	_, err := s.db.ExecContext(ctx, query, userID, keepCount)
	if err != nil {
		return fmt.Errorf("failed to clean old password hashes: %w", err)
	}

	return nil
}

// PostgresAuditStore implements audit logging to PostgreSQL
type PostgresAuditStore struct {
	db     *sql.DB
	table  string
	events []string
}

// NewPostgresAuditStore creates a new PostgreSQL audit store
func NewPostgresAuditStore(db *sql.DB, table string, events []string) *PostgresAuditStore {
	if table == "" {
		table = "auth_audit_log"
	}
	return &PostgresAuditStore{
		db:     db,
		table:  table,
		events: events,
	}
}

// Log logs an audit event
func (s *PostgresAuditStore) Log(ctx context.Context, event *AuditEvent) error {
	// Check if event type should be logged
	if len(s.events) > 0 {
		found := false
		for _, e := range s.events {
			if e == event.Event {
				found = true
				break
			}
		}
		if !found {
			return nil // Event not in whitelist, skip
		}
	}

	query := fmt.Sprintf(`
		INSERT INTO %s (event, user_id, email, ip, user_agent, success, error_reason, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, s.table)

	_, err := s.db.ExecContext(ctx, query,
		event.Event,
		event.UserID,
		event.Email,
		event.IP,
		event.UserAgent,
		event.Success,
		event.ErrorReason,
		event.Metadata,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("failed to log audit event: %w", err)
	}

	return nil
}

// AuditEvent represents an audit log event
type AuditEvent struct {
	Event       string
	UserID      string
	Email       string
	IP          string
	UserAgent   string
	Success     bool
	ErrorReason string
	Metadata    string
}

// isUniqueViolation checks if an error is a unique constraint violation
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// PostgreSQL unique violation error code is 23505
	errStr := err.Error()
	return containsAny(errStr, "unique", "duplicate", "23505", "UNIQUE constraint")
}

// containsAny checks if a string contains any of the given substrings
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
