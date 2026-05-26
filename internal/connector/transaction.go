package connector

import (
	"context"
	"database/sql"
	"fmt"
)

// TxRunner is implemented by connectors that can execute a unit of work inside
// a single pinned connection wrapped in a database transaction. The runtime's
// transaction executor depends only on this interface, never on a concrete
// driver: the connector owns named-parameter binding and last-insert-id
// semantics so the executor stays driver-agnostic.
type TxRunner interface {
	// RunInTx opens a transaction on a pinned connection, invokes fn with a
	// TxOps bound to that transaction, then commits if fn returns nil or rolls
	// back if fn returns an error (or panics). The pinned connection is what
	// makes LAST_INSERT_ID() and captured SELECTs coherent across statements.
	RunInTx(ctx context.Context, fn func(ops TxOps) error) error
}

// TxOps is the driver-agnostic handle for running statements inside a pinned
// transaction. Implementations resolve :named placeholders per their dialect.
type TxOps interface {
	// Exec runs a non-SELECT statement (INSERT/UPDATE/DELETE). It returns the
	// last inserted id (driver-defined; 0 when the driver does not report one)
	// and the number of rows affected.
	Exec(ctx context.Context, query string, params map[string]interface{}) (lastInsertID int64, rowsAffected int64, err error)

	// QueryScalar runs a SELECT and returns the first column of the first row,
	// or nil when the result set is empty.
	QueryScalar(ctx context.Context, query string, params map[string]interface{}) (interface{}, error)
}

// NamedParamParser rewrites a query with :name placeholders into the driver's
// positional form and returns the ordered argument values. Database connectors
// already implement this internally (parseNamedParams); they pass it to
// RunInSQLTx so the shared transaction machinery stays driver-agnostic.
type NamedParamParser func(query string, params map[string]interface{}) (string, []interface{})

// RunInSQLTx is the shared database/sql implementation of TxRunner.RunInTx.
// SQL database connectors implement RunInTx as a one-line delegation to this
// helper, passing their *sql.DB and their parseNamedParams so there is a single
// copy of the begin/commit/rollback + parameter-binding machinery.
//
// On a panic inside fn the transaction is rolled back and the panic is
// re-raised, so a buggy executor can never leave a connection mid-transaction.
func RunInSQLTx(ctx context.Context, db *sql.DB, parse NamedParamParser, fn func(ops TxOps) error) (err error) {
	if db == nil {
		return fmt.Errorf("database not connected")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	committed := false
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := fn(&sqlTxOps{tx: tx, parse: parse}); err != nil {
		return err // deferred rollback fires
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	return nil
}

// sqlTxOps is the database/sql-backed TxOps used by RunInSQLTx.
type sqlTxOps struct {
	tx    *sql.Tx
	parse NamedParamParser
}

func (o *sqlTxOps) Exec(ctx context.Context, query string, params map[string]interface{}) (int64, int64, error) {
	sqlText, args := o.bind(query, params)
	res, err := o.tx.ExecContext(ctx, sqlText, args...)
	if err != nil {
		return 0, 0, err
	}
	// LastInsertId / RowsAffected are best-effort: some drivers do not report
	// them. A capture of an unsupported last-insert-id yields 0, not an error.
	lastID, _ := res.LastInsertId()
	affected, _ := res.RowsAffected()
	return lastID, affected, nil
}

func (o *sqlTxOps) QueryScalar(ctx context.Context, query string, params map[string]interface{}) (interface{}, error) {
	sqlText, args := o.bind(query, params)
	rows, err := o.tx.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		// Zero rows -> null. Later `when` gates can test for it.
		return nil, rows.Err()
	}
	var value interface{}
	if err := rows.Scan(&value); err != nil {
		return nil, err
	}
	// database/sql hands TEXT/VARCHAR back as []byte; normalize to string so
	// CEL comparisons and JSON output behave like the rest of Mycel.
	if b, ok := value.([]byte); ok {
		value = string(b)
	}
	return value, rows.Err()
}

func (o *sqlTxOps) bind(query string, params map[string]interface{}) (string, []interface{}) {
	if o.parse == nil {
		return query, nil
	}
	return o.parse(query, params)
}
