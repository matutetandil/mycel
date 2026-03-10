package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Dialect identifies the SQL database type for query adaptation.
type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
	DialectMySQL    Dialect = "mysql"
)

// SQLStore implements Store using a SQL database.
type SQLStore struct {
	db      *sql.DB
	dialect Dialect
	table   string
}

// NewSQLStore creates a new SQL-backed workflow store.
func NewSQLStore(db *sql.DB, dialect Dialect, table string) *SQLStore {
	if table == "" {
		table = "mycel_workflows"
	}
	return &SQLStore{db: db, dialect: dialect, table: table}
}

// EnsureSchema creates the workflow table if it doesn't exist.
func (s *SQLStore) EnsureSchema(ctx context.Context) error {
	var jsonType, timestampType string
	switch s.dialect {
	case DialectPostgres:
		jsonType = "JSONB"
		timestampType = "TIMESTAMP"
	case DialectMySQL:
		jsonType = "JSON"
		timestampType = "DATETIME"
	default: // sqlite
		jsonType = "TEXT"
		timestampType = "TEXT"
	}

	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id              TEXT PRIMARY KEY,
		saga_name       TEXT NOT NULL,
		status          TEXT NOT NULL DEFAULT 'running',
		current_step    INTEGER NOT NULL DEFAULT 0,
		input           %s NOT NULL DEFAULT '{}',
		step_results    %s NOT NULL DEFAULT '{}',
		signal_data     %s,
		resume_at       %s,
		await_event     TEXT,
		expires_at      %s,
		step_expires_at %s,
		error           TEXT,
		created_at      %s NOT NULL,
		updated_at      %s NOT NULL
	)`, s.table,
		jsonType, jsonType, jsonType,
		timestampType, timestampType, timestampType,
		timestampType, timestampType,
	)

	if _, err := s.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("failed to create workflow table: %w", err)
	}

	// Create indexes
	indexes := []string{
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_status ON %s(status)", s.table, s.table),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_resume ON %s(status, resume_at)", s.table, s.table),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_event ON %s(await_event)", s.table, s.table),
	}
	for _, idx := range indexes {
		if _, err := s.db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// Save creates or updates a workflow instance.
func (s *SQLStore) Save(ctx context.Context, instance *Instance) error {
	instance.UpdatedAt = time.Now()

	inputJSON, err := json.Marshal(instance.Input)
	if err != nil {
		return fmt.Errorf("failed to marshal input: %w", err)
	}
	stepsJSON, err := json.Marshal(instance.StepResults)
	if err != nil {
		return fmt.Errorf("failed to marshal step_results: %w", err)
	}
	var signalJSON []byte
	if instance.SignalData != nil {
		signalJSON, err = json.Marshal(instance.SignalData)
		if err != nil {
			return fmt.Errorf("failed to marshal signal_data: %w", err)
		}
	}

	var query string
	switch s.dialect {
	case DialectPostgres:
		query = fmt.Sprintf(`INSERT INTO %s
			(id, saga_name, status, current_step, input, step_results, signal_data,
			 resume_at, await_event, expires_at, step_expires_at, error, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			ON CONFLICT (id) DO UPDATE SET
				status = EXCLUDED.status, current_step = EXCLUDED.current_step,
				step_results = EXCLUDED.step_results, signal_data = EXCLUDED.signal_data,
				resume_at = EXCLUDED.resume_at, await_event = EXCLUDED.await_event,
				expires_at = EXCLUDED.expires_at, step_expires_at = EXCLUDED.step_expires_at,
				error = EXCLUDED.error, updated_at = EXCLUDED.updated_at`, s.table)
	case DialectMySQL:
		query = fmt.Sprintf(`INSERT INTO %s
			(id, saga_name, status, current_step, input, step_results, signal_data,
			 resume_at, await_event, expires_at, step_expires_at, error, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				status = VALUES(status), current_step = VALUES(current_step),
				step_results = VALUES(step_results), signal_data = VALUES(signal_data),
				resume_at = VALUES(resume_at), await_event = VALUES(await_event),
				expires_at = VALUES(expires_at), step_expires_at = VALUES(step_expires_at),
				error = VALUES(error), updated_at = VALUES(updated_at)`, s.table)
	default: // sqlite
		query = fmt.Sprintf(`INSERT OR REPLACE INTO %s
			(id, saga_name, status, current_step, input, step_results, signal_data,
			 resume_at, await_event, expires_at, step_expires_at, error, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.table)
	}

	_, err = s.db.ExecContext(ctx, query,
		instance.ID, instance.SagaName, string(instance.Status), instance.CurrentStep,
		string(inputJSON), string(stepsJSON), nullableJSON(signalJSON),
		s.formatNullableTime(instance.ResumeAt), nullableString(instance.AwaitEvent),
		s.formatNullableTime(instance.ExpiresAt), s.formatNullableTime(instance.StepExpiresAt),
		nullableString(instance.Error),
		s.formatTime(instance.CreatedAt), s.formatTime(instance.UpdatedAt),
	)
	return err
}

// Get retrieves a workflow instance by ID.
func (s *SQLStore) Get(ctx context.Context, id string) (*Instance, error) {
	query := fmt.Sprintf(`SELECT id, saga_name, status, current_step, input, step_results,
		signal_data, resume_at, await_event, expires_at, step_expires_at, error, created_at, updated_at
		FROM %s WHERE id = %s`, s.table, s.placeholder(1))

	row := s.db.QueryRowContext(ctx, query, id)
	return s.scanInstance(row)
}

// FindActive returns all running or paused instances.
func (s *SQLStore) FindActive(ctx context.Context) ([]*Instance, error) {
	query := fmt.Sprintf(`SELECT id, saga_name, status, current_step, input, step_results,
		signal_data, resume_at, await_event, expires_at, step_expires_at, error, created_at, updated_at
		FROM %s WHERE status IN ('running', 'paused')`, s.table)

	return s.queryInstances(ctx, query)
}

// FindReady returns paused instances whose delay has expired.
func (s *SQLStore) FindReady(ctx context.Context) ([]*Instance, error) {
	now := s.timeParam(time.Now())
	query := fmt.Sprintf(`SELECT id, saga_name, status, current_step, input, step_results,
		signal_data, resume_at, await_event, expires_at, step_expires_at, error, created_at, updated_at
		FROM %s WHERE status = 'paused' AND resume_at IS NOT NULL AND resume_at <= %s`,
		s.table, s.placeholder(1))

	return s.queryInstances(ctx, query, now)
}

// FindExpired returns instances that have timed out.
func (s *SQLStore) FindExpired(ctx context.Context) ([]*Instance, error) {
	now := s.timeParam(time.Now())
	query := fmt.Sprintf(`SELECT id, saga_name, status, current_step, input, step_results,
		signal_data, resume_at, await_event, expires_at, step_expires_at, error, created_at, updated_at
		FROM %s WHERE status IN ('running', 'paused')
		AND (expires_at IS NOT NULL AND expires_at <= %s
		  OR step_expires_at IS NOT NULL AND step_expires_at <= %s)`,
		s.table, s.placeholder(1), s.placeholder(2))

	return s.queryInstances(ctx, query, now, now)
}

// FindByEvent returns paused instances awaiting a specific event.
func (s *SQLStore) FindByEvent(ctx context.Context, event string) ([]*Instance, error) {
	query := fmt.Sprintf(`SELECT id, saga_name, status, current_step, input, step_results,
		signal_data, resume_at, await_event, expires_at, step_expires_at, error, created_at, updated_at
		FROM %s WHERE status = 'paused' AND await_event = %s`,
		s.table, s.placeholder(1))

	return s.queryInstances(ctx, query, event)
}

// Delete removes a workflow instance.
func (s *SQLStore) Delete(ctx context.Context, id string) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE id = %s", s.table, s.placeholder(1))
	_, err := s.db.ExecContext(ctx, query, id)
	return err
}

// placeholder returns a placeholder for the given position.
func (s *SQLStore) placeholder(n int) string {
	if s.dialect == DialectPostgres {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

// queryInstances executes a query and returns a slice of instances.
func (s *SQLStore) queryInstances(ctx context.Context, query string, args ...interface{}) ([]*Instance, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []*Instance
	for rows.Next() {
		inst, err := s.scanInstanceFromRows(rows)
		if err != nil {
			return nil, err
		}
		instances = append(instances, inst)
	}
	return instances, rows.Err()
}

// scanInstance scans a single row into an Instance.
func (s *SQLStore) scanInstance(row *sql.Row) (*Instance, error) {
	var inst Instance
	var statusStr string
	var inputJSON, stepsJSON string
	var signalJSON sql.NullString
	var resumeAtStr, expiresAtStr, stepExpiresAtStr sql.NullString
	var awaitEvent, errStr sql.NullString
	var createdAtStr, updatedAtStr string

	err := row.Scan(&inst.ID, &inst.SagaName, &statusStr, &inst.CurrentStep,
		&inputJSON, &stepsJSON, &signalJSON,
		&resumeAtStr, &awaitEvent, &expiresAtStr, &stepExpiresAtStr, &errStr,
		&createdAtStr, &updatedAtStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("workflow instance not found")
		}
		return nil, err
	}

	s.populateInstance(&inst, statusStr, inputJSON, stepsJSON, signalJSON,
		resumeAtStr, awaitEvent, expiresAtStr, stepExpiresAtStr, errStr,
		createdAtStr, updatedAtStr)

	return &inst, nil
}

// scanInstanceFromRows scans a rows cursor into an Instance.
func (s *SQLStore) scanInstanceFromRows(rows *sql.Rows) (*Instance, error) {
	var inst Instance
	var statusStr string
	var inputJSON, stepsJSON string
	var signalJSON sql.NullString
	var resumeAtStr, expiresAtStr, stepExpiresAtStr sql.NullString
	var awaitEvent, errStr sql.NullString
	var createdAtStr, updatedAtStr string

	err := rows.Scan(&inst.ID, &inst.SagaName, &statusStr, &inst.CurrentStep,
		&inputJSON, &stepsJSON, &signalJSON,
		&resumeAtStr, &awaitEvent, &expiresAtStr, &stepExpiresAtStr, &errStr,
		&createdAtStr, &updatedAtStr)
	if err != nil {
		return nil, err
	}

	s.populateInstance(&inst, statusStr, inputJSON, stepsJSON, signalJSON,
		resumeAtStr, awaitEvent, expiresAtStr, stepExpiresAtStr, errStr,
		createdAtStr, updatedAtStr)

	return &inst, nil
}

// populateInstance fills an Instance from scanned string values.
func (s *SQLStore) populateInstance(inst *Instance, statusStr, inputJSON, stepsJSON string,
	signalJSON, resumeAtStr sql.NullString, awaitEvent sql.NullString,
	expiresAtStr, stepExpiresAtStr, errStr sql.NullString,
	createdAtStr, updatedAtStr string) {

	inst.Status = Status(statusStr)
	json.Unmarshal([]byte(inputJSON), &inst.Input)
	json.Unmarshal([]byte(stepsJSON), &inst.StepResults)
	if signalJSON.Valid {
		json.Unmarshal([]byte(signalJSON.String), &inst.SignalData)
	}
	if resumeAtStr.Valid {
		if t, err := parseTime(resumeAtStr.String); err == nil {
			inst.ResumeAt = &t
		}
	}
	if expiresAtStr.Valid {
		if t, err := parseTime(expiresAtStr.String); err == nil {
			inst.ExpiresAt = &t
		}
	}
	if stepExpiresAtStr.Valid {
		if t, err := parseTime(stepExpiresAtStr.String); err == nil {
			inst.StepExpiresAt = &t
		}
	}
	if awaitEvent.Valid {
		inst.AwaitEvent = awaitEvent.String
	}
	if errStr.Valid {
		inst.Error = errStr.String
	}
	inst.CreatedAt, _ = parseTime(createdAtStr)
	inst.UpdatedAt, _ = parseTime(updatedAtStr)
}

// timeParam converts a time to the appropriate query parameter type.
func (s *SQLStore) timeParam(t time.Time) interface{} {
	if s.dialect == DialectSQLite {
		return t.Format(time.RFC3339Nano)
	}
	return t
}

// formatTime formats a time value for SQL storage.
func (s *SQLStore) formatTime(t time.Time) interface{} {
	if s.dialect == DialectSQLite {
		return t.Format(time.RFC3339Nano)
	}
	return t
}

// formatNullableTime formats a nullable time for SQL storage.
func (s *SQLStore) formatNullableTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	if s.dialect == DialectSQLite {
		return t.Format(time.RFC3339Nano)
	}
	return *t
}

// parseTime tries multiple time formats for cross-dialect compatibility.
func parseTime(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time: %s", s)
}

// Helper functions for nullable SQL types.

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullableJSON(data []byte) interface{} {
	if data == nil {
		return nil
	}
	return string(data)
}
