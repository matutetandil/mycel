package flow

// TransactionConfig is the `to { transaction { } }` write primitive: an
// ordered list of statements executed inside a single pinned database
// connection wrapped in one BEGIN/COMMIT. It runs as "the write" of the flow,
// so it is wrapped by the same dedupe / aspects / error_handling envelope as a
// classic single-statement `to` write.
//
// When a `to` block has a Transaction, its query/target/operation/envelope
// attributes are unused (the parser rejects that combination), and the `to`
// connector must be of type "database".
type TransactionConfig struct {
	// Statements is the ordered list of exec/each entries. Order is textual
	// (the order the blocks appear in the HCL file) and is significant:
	// LAST_INSERT_ID / captured SELECT values flow forward to later statements.
	Statements []TxStatement
}

// TxStatement is one ordered entry inside a transaction. Exactly one of Exec
// or Each is non-nil — it is a tagged union kept as a struct (rather than an
// interface) so the parser can preserve textual order in a single slice
// without losing the discriminator.
type TxStatement struct {
	// Exec is set when this statement is a single SQL statement.
	Exec *TxExec

	// Each is set when this statement iterates a list and runs nested
	// statements per element.
	Each *TxEach
}

// TxExec is a single SQL statement within a transaction.
type TxExec struct {
	// Query is the SQL with :named placeholders, resolved from Params.
	Query string

	// Params maps placeholder name -> CEL expression, evaluated in the
	// transaction scope (input/output/step/captured + active each bindings).
	// Same shape as a classic to.params block.
	Params map[string]string

	// When is an optional CEL gate. If it evaluates to false the statement is
	// skipped (not an error). Empty means always run.
	When string

	// Capture, when non-empty, stores a value under captured.<Capture> for use
	// by later statements:
	//   - INSERT/UPDATE/DELETE -> last insert id (driver-defined)
	//   - SELECT               -> first column of the first row (nil if 0 rows)
	Capture string
}

// TxEach iterates a CEL list expression and runs Body once per element. It is
// nestable (Body may contain further each blocks).
type TxEach struct {
	// Var is the loop variable name. The current element is bound to <Var> and
	// its 0-based index to <Var>_index in the CEL scope of the nested body.
	Var string

	// In is a CEL expression that evaluates to a list. A non-list or empty
	// result runs nothing (not an error).
	In string

	// Body is the ordered list of statements run per element.
	Body []TxStatement
}

// EachVarNames returns every each loop variable name declared anywhere in the
// transaction (recursively), so the runtime can declare them — plus their
// <name>_index companions — as variables in the scoped CEL environment.
func (t *TransactionConfig) EachVarNames() []string {
	if t == nil {
		return nil
	}
	var names []string
	var walk func(stmts []TxStatement)
	walk = func(stmts []TxStatement) {
		for _, s := range stmts {
			if s.Each != nil {
				names = append(names, s.Each.Var)
				walk(s.Each.Body)
			}
		}
	}
	walk(t.Statements)
	return names
}
