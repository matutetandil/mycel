package runtime

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/cel-go/cel"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/flow"
	"github.com/matutetandil/mycel/internal/trace"
	"github.com/matutetandil/mycel/internal/transform"
)

// handleTransaction executes a `to { transaction { } }` block as the write of
// the flow. It pins one database connection, opens a transaction, runs the
// ordered statements (exec / each) with value capture and CEL-scoped params,
// and commits on success or rolls back on any error. It is invoked from the
// same point as a classic to-write, so dedupe / aspects / error_handling wrap
// it identically.
func (h *FlowHandler) handleTransaction(ctx context.Context, input map[string]interface{}) (interface{}, error) {
	txCfg := h.Config.To.Transaction

	runner, ok := h.Dest.(connector.TxRunner)
	if !ok {
		return nil, fmt.Errorf("flow %q: connector %q does not support transactions (the to connector must be a database connector)", h.Config.Name, h.Config.To.Connector)
	}

	// The transform output is the `output` binding of the transaction scope;
	// step results are exposed as `step`.
	payload, steps, err := h.applyTransformsWithSteps(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("transform error: %w", err)
	}
	if steps == nil {
		steps = map[string]interface{}{}
	}

	eval, err := h.transactionEvaluator()
	if err != nil {
		return nil, err
	}

	// Dry-run: report intent without touching the database.
	if tc := trace.FromContext(ctx); tc != nil && tc.DryRun {
		tc.Record(trace.Event{
			Stage:  trace.StageWrite,
			Name:   h.Config.To.Connector,
			Input:  trace.Snapshot(payload),
			DryRun: true,
			Detail: fmt.Sprintf("transaction: %d statement(s)", len(txCfg.Statements)),
		})
		return map[string]interface{}{
			"dry_run":     true,
			"transaction": true,
			"connector":   h.Config.To.Connector,
		}, nil
	}

	// captured is shared between the executor and the CEL scope: as each exec
	// captures a value it becomes visible to later statements' expressions.
	captured := map[string]interface{}{}
	var affected int64

	writeResult, writeErr := h.dedupeAwareWrite(ctx, input, payload, func() (interface{}, error) {
		return trace.RecordStage(ctx, trace.StageWrite, h.Config.To.Connector, trace.Snapshot(payload), func() (interface{}, error) {
			runErr := runner.RunInTx(ctx, func(ops connector.TxOps) error {
				ex := &txExecutor{
					eval:     eval,
					ops:      ops,
					captured: captured,
					scope: map[string]interface{}{
						"input":    input,
						"output":   payload,
						"step":     steps,
						"captured": captured,
					},
				}
				a, runErr := ex.run(ctx, txCfg.Statements)
				affected = a
				return runErr
			})
			if runErr != nil {
				return nil, runErr
			}
			return &connector.Result{
				Affected: affected,
				Metadata: map[string]interface{}{"captured": captured},
			}, nil
		})
	})
	if writeErr != nil {
		return nil, writeErr
	}

	// Dedupe match: propagate the filtered result so the MQ consumer can
	// ack/reject/requeue per policy.
	if filtered, ok := writeResult.(*flow.FilteredResultWithPolicy); ok {
		return filtered, nil
	}

	result := writeResult.(*connector.Result)
	return map[string]interface{}{
		"affected": result.Affected,
		"captured": captured,
	}, nil
}

// transactionEvaluator lazily builds (and caches) the CEL transformer used to
// evaluate transaction expressions. It extends the standard scope with the
// `captured` map and one variable per each loop name (plus its <name>_index
// companion). Built once per handler because constructing a CEL environment
// rebuilds the full function set.
func (h *FlowHandler) transactionEvaluator() (*transform.CELTransformer, error) {
	h.txEvalOnce.Do(func() {
		opts := transform.CreateWASMFunctionOptions(h.FunctionsRegistry)
		opts = append(opts, cel.Variable("captured", cel.MapType(cel.StringType, cel.DynType)))

		seen := map[string]bool{}
		for _, name := range h.Config.To.Transaction.EachVarNames() {
			if seen[name] {
				continue
			}
			seen[name] = true
			opts = append(opts,
				cel.Variable(name, cel.DynType),
				cel.Variable(name+"_index", cel.IntType),
			)
		}

		h.txEval, h.txEvalErr = transform.NewCELTransformerWithOptions(opts...)
	})
	return h.txEval, h.txEvalErr
}

// txExecutor runs an ordered list of transaction statements against a pinned
// TxOps. It is single-threaded within one transaction, so the shared scope and
// captured maps need no synchronization.
type txExecutor struct {
	eval     *transform.CELTransformer
	ops      connector.TxOps
	captured map[string]interface{}
	scope    map[string]interface{}
}

// run executes statements in order, returning the total rows affected.
func (e *txExecutor) run(ctx context.Context, stmts []flow.TxStatement) (int64, error) {
	var affected int64
	for _, s := range stmts {
		switch {
		case s.Exec != nil:
			a, err := e.runExec(ctx, s.Exec)
			if err != nil {
				return affected, err
			}
			affected += a
		case s.Each != nil:
			a, err := e.runEach(ctx, s.Each)
			if err != nil {
				return affected, err
			}
			affected += a
		}
	}
	return affected, nil
}

// runExec evaluates the optional when gate and params, runs the statement, and
// captures its result when requested.
func (e *txExecutor) runExec(ctx context.Context, ex *flow.TxExec) (int64, error) {
	if ex.When != "" {
		ok, err := e.evalBool(ctx, ex.When)
		if err != nil {
			return 0, fmt.Errorf("transaction when %q: %w", ex.When, err)
		}
		if !ok {
			return 0, nil
		}
	}

	params := make(map[string]interface{}, len(ex.Params))
	for name, expr := range ex.Params {
		v, err := e.eval.EvaluateWith(ctx, expr, e.scope)
		if err != nil {
			return 0, fmt.Errorf("transaction param %q (%q): %w", name, expr, err)
		}
		params[name] = v
	}

	if isSelectStatement(ex.Query) {
		val, err := e.ops.QueryScalar(ctx, ex.Query, params)
		if err != nil {
			return 0, err
		}
		if ex.Capture != "" {
			e.captured[ex.Capture] = val
		}
		return 0, nil
	}

	lastID, affected, err := e.ops.Exec(ctx, ex.Query, params)
	if err != nil {
		return 0, err
	}
	if ex.Capture != "" {
		e.captured[ex.Capture] = lastID
	}
	return affected, nil
}

// runEach evaluates the list expression and runs the body once per element,
// binding the element to each.Var and its 0-based index to <Var>_index. A
// non-list or empty result runs nothing. Bindings are restored afterwards so
// sibling/nested each blocks don't leak into one another's scope.
func (e *txExecutor) runEach(ctx context.Context, each *flow.TxEach) (int64, error) {
	listVal, err := e.eval.EvaluateWith(ctx, each.In, e.scope)
	if err != nil {
		return 0, fmt.Errorf("transaction each %q in %q: %w", each.Var, each.In, err)
	}
	list, ok := toList(listVal)
	if !ok {
		return 0, nil
	}

	idxKey := each.Var + "_index"
	prevVar, hadVar := e.scope[each.Var]
	prevIdx, hadIdx := e.scope[idxKey]
	defer func() {
		if hadVar {
			e.scope[each.Var] = prevVar
		} else {
			delete(e.scope, each.Var)
		}
		if hadIdx {
			e.scope[idxKey] = prevIdx
		} else {
			delete(e.scope, idxKey)
		}
	}()

	var affected int64
	for i, item := range list {
		e.scope[each.Var] = item
		e.scope[idxKey] = int64(i)
		a, err := e.run(ctx, each.Body)
		if err != nil {
			return affected, err
		}
		affected += a
	}
	return affected, nil
}

// evalBool evaluates expr and coerces the result to a bool.
func (e *txExecutor) evalBool(ctx context.Context, expr string) (bool, error) {
	v, err := e.eval.EvaluateWith(ctx, expr, e.scope)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("expression %q did not evaluate to a boolean (got %T)", expr, v)
	}
	return b, nil
}

// isSelectStatement reports whether a query reads rows (SELECT/WITH), in which
// case a capture takes the first column of the first row rather than a last
// insert id.
func isSelectStatement(query string) bool {
	upper := strings.ToUpper(strings.TrimSpace(query))
	return strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH")
}

// toList converts a CEL-evaluated value into a slice. It returns ok=false for
// nil or non-list values so an each over a missing/empty field is a no-op.
func toList(v interface{}) ([]interface{}, bool) {
	if v == nil {
		return nil, false
	}
	if items, ok := v.([]interface{}); ok {
		return items, true
	}
	// Fall back to reflection for typed slices (e.g. []map[string]interface{}).
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	items := make([]interface{}, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		items[i] = rv.Index(i).Interface()
	}
	return items, true
}
