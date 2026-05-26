package sqlite

import (
	"context"

	"github.com/matutetandil/mycel/internal/connector"
)

// RunInTx implements connector.TxRunner: it runs fn inside a single pinned
// connection wrapped in a transaction, committing on success and rolling back
// on error. last_insert_rowid() is reported via sql.Result.LastInsertId().
func (c *Connector) RunInTx(ctx context.Context, fn func(ops connector.TxOps) error) error {
	return connector.RunInSQLTx(ctx, c.db, c.parseNamedParams, fn)
}
