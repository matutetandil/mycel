package auth

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// MySQLUserStore implements UserStore using MySQL
type MySQLUserStore struct {
	db     *sql.DB
	config *UsersConfig
}

// NewMySQLUserStore creates a new MySQL user store
func NewMySQLUserStore(db *sql.DB, config *UsersConfig) *MySQLUserStore {
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
	return &MySQLUserStore{
		db:     db,
		config: config,
	}
}

// getTableName returns the configured table name
func (s *MySQLUserStore) getTableName() string {
	if s.config != nil && s.config.Table != "" {
		return s.config.Table
	}
	return "users"
}

// getFields returns the configured field mappings
func (s *MySQLUserStore) getFields() *UserFieldsConfig {
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
func (s *MySQLUserStore) Create(ctx context.Context, user *User) error {
	table := s.getTableName()
	fields := s.getFields()

	query := fmt.Sprintf(`
		INSERT INTO %s (%s, %s, %s, %s, %s)
		VALUES (?, ?, ?, ?, ?)
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
		if isMySQLUniqueViolation(err) {
			return ErrUserAlreadyExists
		}
		return fmt.Errorf("failed to create user: %w", err)
	}

	return nil
}

// GetByID retrieves a user by ID
func (s *MySQLUserStore) GetByID(ctx context.Context, id string) (*User, error) {
	table := s.getTableName()
	fields := s.getFields()

	query := fmt.Sprintf(`
		SELECT %s, %s, %s, %s, %s
		FROM %s
		WHERE %s = ?
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
func (s *MySQLUserStore) GetByEmail(ctx context.Context, email string) (*User, error) {
	table := s.getTableName()
	fields := s.getFields()

	query := fmt.Sprintf(`
		SELECT %s, %s, %s, %s, %s
		FROM %s
		WHERE %s = ?
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
func (s *MySQLUserStore) Update(ctx context.Context, user *User) error {
	table := s.getTableName()
	fields := s.getFields()

	user.UpdatedAt = time.Now()

	query := fmt.Sprintf(`
		UPDATE %s
		SET %s = ?, %s = ?, %s = ?
		WHERE %s = ?
	`, table, fields.Email, fields.PasswordHash, fields.UpdatedAt, fields.ID)

	result, err := s.db.ExecContext(ctx, query,
		user.Email,
		user.PasswordHash,
		user.UpdatedAt,
		user.ID,
	)
	if err != nil {
		if isMySQLUniqueViolation(err) {
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
func (s *MySQLUserStore) Delete(ctx context.Context, id string) error {
	table := s.getTableName()
	fields := s.getFields()

	query := fmt.Sprintf(`DELETE FROM %s WHERE %s = ?`, table, fields.ID)

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

// MySQLPasswordHistoryStore stores password history for users
type MySQLPasswordHistoryStore struct {
	db    *sql.DB
	table string
}

// NewMySQLPasswordHistoryStore creates a new password history store
func NewMySQLPasswordHistoryStore(db *sql.DB, table string) *MySQLPasswordHistoryStore {
	if table == "" {
		table = "password_history"
	}
	return &MySQLPasswordHistoryStore{
		db:    db,
		table: table,
	}
}

// AddPasswordHash adds a password hash to history
func (s *MySQLPasswordHistoryStore) AddPasswordHash(ctx context.Context, userID, hash string) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (user_id, password_hash, created_at)
		VALUES (?, ?, ?)
	`, s.table)

	_, err := s.db.ExecContext(ctx, query, userID, hash, time.Now())
	if err != nil {
		return fmt.Errorf("failed to add password to history: %w", err)
	}

	return nil
}

// GetRecentHashes returns the N most recent password hashes for a user
func (s *MySQLPasswordHistoryStore) GetRecentHashes(ctx context.Context, userID string, count int) ([]string, error) {
	query := fmt.Sprintf(`
		SELECT password_hash
		FROM %s
		WHERE user_id = ?
		ORDER BY created_at DESC
		LIMIT ?
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
func (s *MySQLPasswordHistoryStore) CleanOldHashes(ctx context.Context, userID string, keepCount int) error {
	// MySQL doesn't allow subquery on same table in DELETE, so we use a workaround
	// First get the IDs to keep
	selectQuery := fmt.Sprintf(`
		SELECT id FROM %s
		WHERE user_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, s.table)

	rows, err := s.db.QueryContext(ctx, selectQuery, userID, keepCount)
	if err != nil {
		return fmt.Errorf("failed to get recent hashes: %w", err)
	}

	var keepIDs []interface{}
	keepIDs = append(keepIDs, userID) // First parameter for the DELETE query
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan ID: %w", err)
		}
		keepIDs = append(keepIDs, id)
	}
	rows.Close()

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating IDs: %w", err)
	}

	// If no IDs to keep, delete all for this user
	if len(keepIDs) == 1 {
		deleteQuery := fmt.Sprintf(`DELETE FROM %s WHERE user_id = ?`, s.table)
		_, err := s.db.ExecContext(ctx, deleteQuery, userID)
		if err != nil {
			return fmt.Errorf("failed to clean old password hashes: %w", err)
		}
		return nil
	}

	// Build the NOT IN clause with placeholders
	placeholders := "?"
	for i := 1; i < len(keepIDs); i++ {
		placeholders += ", ?"
	}

	deleteQuery := fmt.Sprintf(`
		DELETE FROM %s
		WHERE user_id = ?
		AND id NOT IN (%s)
	`, s.table, placeholders)

	_, err = s.db.ExecContext(ctx, deleteQuery, keepIDs...)
	if err != nil {
		return fmt.Errorf("failed to clean old password hashes: %w", err)
	}

	return nil
}

// MySQLAuditStore implements audit logging to MySQL
type MySQLAuditStore struct {
	db     *sql.DB
	table  string
	events []string
}

// NewMySQLAuditStore creates a new MySQL audit store
func NewMySQLAuditStore(db *sql.DB, table string, events []string) *MySQLAuditStore {
	if table == "" {
		table = "auth_audit_log"
	}
	return &MySQLAuditStore{
		db:     db,
		table:  table,
		events: events,
	}
}

// Log logs an audit event
func (s *MySQLAuditStore) Log(ctx context.Context, event *AuditEvent) error {
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
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
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

// MySQLSessionStore implements SessionStore using MySQL
type MySQLSessionStore struct {
	db    *sql.DB
	table string
}

// NewMySQLSessionStore creates a new MySQL session store
func NewMySQLSessionStore(db *sql.DB, table string) *MySQLSessionStore {
	if table == "" {
		table = "auth_sessions"
	}
	return &MySQLSessionStore{
		db:    db,
		table: table,
	}
}

// Create creates a new session
func (s *MySQLSessionStore) Create(ctx context.Context, session *Session) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (id, user_id, ip, user_agent, created_at, last_active_at, expires_at, device_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, s.table)

	_, err := s.db.ExecContext(ctx, query,
		session.ID,
		session.UserID,
		session.IP,
		session.UserAgent,
		session.CreatedAt,
		session.LastActiveAt,
		session.ExpiresAt,
		session.DeviceID,
	)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	return nil
}

// FindByID retrieves a session by ID
func (s *MySQLSessionStore) FindByID(ctx context.Context, id string) (*Session, error) {
	query := fmt.Sprintf(`
		SELECT id, user_id, ip, user_agent, created_at, last_active_at, expires_at, device_id
		FROM %s
		WHERE id = ?
	`, s.table)

	session := &Session{}
	var deviceID sql.NullString

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&session.ID,
		&session.UserID,
		&session.IP,
		&session.UserAgent,
		&session.CreatedAt,
		&session.LastActiveAt,
		&session.ExpiresAt,
		&deviceID,
	)
	if err == sql.ErrNoRows {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find session: %w", err)
	}

	if deviceID.Valid {
		session.DeviceID = deviceID.String
	}

	return session, nil
}

// FindByUserID returns all sessions for a user
func (s *MySQLSessionStore) FindByUserID(ctx context.Context, userID string) ([]*Session, error) {
	query := fmt.Sprintf(`
		SELECT id, user_id, ip, user_agent, created_at, last_active_at, expires_at, device_id
		FROM %s
		WHERE user_id = ?
		ORDER BY last_active_at DESC
	`, s.table)

	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to find sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		session := &Session{}
		var deviceID sql.NullString

		if err := rows.Scan(
			&session.ID,
			&session.UserID,
			&session.IP,
			&session.UserAgent,
			&session.CreatedAt,
			&session.LastActiveAt,
			&session.ExpiresAt,
			&deviceID,
		); err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}

		if deviceID.Valid {
			session.DeviceID = deviceID.String
		}
		sessions = append(sessions, session)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating sessions: %w", err)
	}

	return sessions, nil
}

// Update updates an existing session
func (s *MySQLSessionStore) Update(ctx context.Context, session *Session) error {
	query := fmt.Sprintf(`
		UPDATE %s
		SET last_active_at = ?, ip = ?, user_agent = ?
		WHERE id = ?
	`, s.table)

	result, err := s.db.ExecContext(ctx, query,
		session.LastActiveAt,
		session.IP,
		session.UserAgent,
		session.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check affected rows: %w", err)
	}
	if rowsAffected == 0 {
		return ErrSessionNotFound
	}

	return nil
}

// Delete removes a session
func (s *MySQLSessionStore) Delete(ctx context.Context, id string) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, s.table)

	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check affected rows: %w", err)
	}
	if rowsAffected == 0 {
		return ErrSessionNotFound
	}

	return nil
}

// DeleteByUserID removes all sessions for a user
func (s *MySQLSessionStore) DeleteByUserID(ctx context.Context, userID string) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE user_id = ?`, s.table)

	_, err := s.db.ExecContext(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to delete sessions: %w", err)
	}

	return nil
}

// DeleteExpired removes all expired sessions
func (s *MySQLSessionStore) DeleteExpired(ctx context.Context) (int, error) {
	query := fmt.Sprintf(`DELETE FROM %s WHERE expires_at < ?`, s.table)

	result, err := s.db.ExecContext(ctx, query, time.Now())
	if err != nil {
		return 0, fmt.Errorf("failed to delete expired sessions: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to check affected rows: %w", err)
	}

	return int(rowsAffected), nil
}

// MySQLTokenStore implements TokenStore using MySQL
type MySQLTokenStore struct {
	db    *sql.DB
	table string
}

// NewMySQLTokenStore creates a new MySQL token store
func NewMySQLTokenStore(db *sql.DB, table string) *MySQLTokenStore {
	if table == "" {
		table = "auth_tokens"
	}
	return &MySQLTokenStore{
		db:    db,
		table: table,
	}
}

// Add adds a token to the store (for blacklist or replay protection)
func (s *MySQLTokenStore) Add(ctx context.Context, tokenID string, expiry time.Time) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (token_id, expires_at)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE expires_at = VALUES(expires_at)
	`, s.table)

	_, err := s.db.ExecContext(ctx, query, tokenID, expiry)
	if err != nil {
		return fmt.Errorf("failed to add token: %w", err)
	}

	return nil
}

// Exists checks if a token exists in the store
func (s *MySQLTokenStore) Exists(ctx context.Context, tokenID string) (bool, error) {
	query := fmt.Sprintf(`
		SELECT expires_at FROM %s WHERE token_id = ?
	`, s.table)

	var expiry time.Time
	err := s.db.QueryRowContext(ctx, query, tokenID).Scan(&expiry)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check token: %w", err)
	}

	// Check if expired
	if time.Now().After(expiry) {
		return false, nil
	}

	return true, nil
}

// Delete removes a token from the store
func (s *MySQLTokenStore) Delete(ctx context.Context, tokenID string) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE token_id = ?`, s.table)

	_, err := s.db.ExecContext(ctx, query, tokenID)
	if err != nil {
		return fmt.Errorf("failed to delete token: %w", err)
	}

	return nil
}

// Cleanup removes expired tokens
func (s *MySQLTokenStore) Cleanup(ctx context.Context) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE expires_at < ?`, s.table)

	_, err := s.db.ExecContext(ctx, query, time.Now())
	if err != nil {
		return fmt.Errorf("failed to cleanup expired tokens: %w", err)
	}

	return nil
}

// isMySQLUniqueViolation checks if an error is a MySQL unique constraint violation
func isMySQLUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// MySQL unique violation error code is 1062
	errStr := err.Error()
	return containsAny(errStr, "Duplicate entry", "1062", "unique", "UNIQUE constraint")
}
