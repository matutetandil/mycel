package aspect

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/ratelimit"
)

// An aspect is applied by pattern, not by being called from anywhere, so a
// mistake here is invisible in the flow that suffers it: the aspect simply does
// not run, or runs when it should not, and nothing in the configuration says so.

func executorWith(t *testing.T, aspects ...*Config) *Executor {
	t.Helper()
	registry := NewRegistry()
	if err := registry.RegisterAll(aspects); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	e, err := NewExecutor(registry, connector.NewRegistry())
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	return e
}

// invokerFunc adapts a function to the FlowInvoker an aspect action uses to
// call another flow.
type invokerFunc func(ctx context.Context, name string, input map[string]interface{}) error

func (f invokerFunc) InvokeFlow(ctx context.Context, name string, input map[string]interface{}) (interface{}, error) {
	return nil, f(ctx, name, input)
}

func succeeds() (FlowFunc, *int) {
	calls := 0
	return func(context.Context, map[string]interface{}) (*connector.Result, error) {
		calls++
		return &connector.Result{Rows: []map[string]interface{}{{"ok": true}}}, nil
	}, &calls
}

func TestTheFlowSeesNoneOfTheAspectMetadata(t *testing.T) {
	// The metadata is bound so an aspect can match on the flow it is wrapping.
	// Leaving it in the payload would send those fields to the destination — an
	// extra column on an insert, an unexpected field on an API.
	e := executorWith(t)

	var seen map[string]interface{}
	_, err := e.Execute(context.Background(), "create_order", "POST", "/orders",
		map[string]interface{}{"id": "1"},
		func(_ context.Context, input map[string]interface{}) (*connector.Result, error) {
			seen = input
			return &connector.Result{}, nil
		})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	for _, key := range []string{"_flow", "_operation", "_target", "_timestamp"} {
		if _, ok := seen[key]; ok {
			t.Errorf("the flow was handed %s, which belongs to the aspects", key)
		}
	}
	if seen["id"] != "1" {
		t.Errorf("the payload was changed: %v", seen)
	}
}

func TestAnAspectSeesWhichFlowItIsWrapping(t *testing.T) {
	// This is what makes one aspect able to serve many flows.
	var seen map[string]interface{}
	e := executorWith(t, &Config{
		Name: "watcher", When: "before", On: []string{"create_*"},
		Action: &ActionConfig{Flow: "audit", Transform: map[string]string{
			"flow":      "input._flow",
			"operation": "input._operation",
			"target":    "input._target",
		}},
	})
	e.SetFlowInvoker(invokerFunc(func(_ context.Context, _ string, data map[string]interface{}) error {
		seen = data
		return nil
	}))

	flowFn, _ := succeeds()
	if _, err := e.Execute(context.Background(), "create_order", "POST", "/orders", nil, flowFn); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if seen == nil {
		t.Fatal("the aspect did not run")
	}
	if seen["flow"] != "create_order" || seen["operation"] != "POST" || seen["target"] != "/orders" {
		t.Errorf("the aspect was told %v", seen)
	}
}

func TestABeforeAspectThatFailsStopsTheFlow(t *testing.T) {
	// A before aspect is the place a gate goes — an authorisation check, a
	// quota. If the flow ran anyway the gate would be decoration.
	e := executorWith(t, &Config{
		Name: "gate", When: "before", On: []string{"*"},
		Action: &ActionConfig{Flow: "check"},
	})
	e.SetFlowInvoker(invokerFunc(func(context.Context, string, map[string]interface{}) error {
		return errors.New("refused")
	}))

	flowFn, calls := succeeds()
	_, err := e.Execute(context.Background(), "create_order", "POST", "", nil, flowFn)
	if err == nil {
		t.Fatal("the flow succeeded although its before aspect failed")
	}
	if !strings.Contains(err.Error(), "gate") {
		t.Errorf("error = %q, want it to name the aspect", err)
	}
	if *calls != 0 {
		t.Error("the flow ran after its before aspect refused")
	}
}

func TestAnAfterAspectThatFailsDoesNotFailTheFlow(t *testing.T) {
	// The work is already done and, for a queue consumer, already acknowledged.
	// Failing the flow because a notification could not be sent would redeliver
	// a message that was processed.
	e := executorWith(t, &Config{
		Name: "notify", When: "after", On: []string{"*"},
		Action: &ActionConfig{Flow: "slack"},
	})
	e.SetFlowInvoker(invokerFunc(func(context.Context, string, map[string]interface{}) error {
		return errors.New("slack is down")
	}))

	flowFn, _ := succeeds()
	result, err := e.Execute(context.Background(), "create_order", "POST", "", nil, flowFn)
	if err != nil {
		t.Errorf("a failing after aspect failed the flow: %v", err)
	}
	if result == nil {
		t.Error("the flow's result was lost")
	}
}

func TestAfterDoesNotRunWhenTheFlowFailed(t *testing.T) {
	// after is for success — a notification, a cache write. on_error is the
	// other branch, and running both would announce a success that did not
	// happen.
	var ran []string
	e := executorWith(t,
		&Config{Name: "after", When: "after", On: []string{"*"}, Action: &ActionConfig{Flow: "a"}},
		&Config{Name: "onerror", When: "on_error", On: []string{"*"}, Action: &ActionConfig{Flow: "b"}},
	)
	e.SetFlowInvoker(invokerFunc(func(_ context.Context, name string, _ map[string]interface{}) error {
		ran = append(ran, name)
		return nil
	}))

	_, err := e.Execute(context.Background(), "create_order", "POST", "", nil,
		func(context.Context, map[string]interface{}) (*connector.Result, error) {
			return nil, errors.New("the database is down")
		})
	if err == nil {
		t.Fatal("a failing flow reported success")
	}
	if len(ran) != 1 || ran[0] != "b" {
		t.Errorf("aspects that ran: %v, want only the on_error one", ran)
	}
}

func TestAnErrorAspectIsToldWhatWentWrong(t *testing.T) {
	var seen map[string]interface{}
	e := executorWith(t, &Config{
		Name: "alert", When: "on_error", On: []string{"*"},
		Action: &ActionConfig{Flow: "page", Transform: map[string]string{
			"message": "input.error.message",
		}},
	})
	e.SetFlowInvoker(invokerFunc(func(_ context.Context, _ string, data map[string]interface{}) error {
		seen = data
		return nil
	}))

	_, _ = e.Execute(context.Background(), "create_order", "POST", "", nil,
		func(context.Context, map[string]interface{}) (*connector.Result, error) {
			return nil, errors.New("connection refused")
		})

	message, ok := seen["message"].(string)
	if !ok {
		t.Fatalf("the aspect was not told about the error: %v", seen)
	}
	if !strings.Contains(message, "connection refused") {
		t.Errorf("error message = %v", message)
	}
}

// A deflected message took neither the success path nor the failure path: a
// filter rejected it, an accept gate refused it, a sequence guard found it
// older than what was already stored. It is neither an error to alert on nor a
// success to announce, and before on_drop existed there was no way to notice
// one at all.

func dropped(reason, policy string) FlowFunc {
	return func(context.Context, map[string]interface{}) (*connector.Result, error) {
		return nil, &flow.FilteredDropError{Result: &flow.FilteredResultWithPolicy{
			Filtered: true, Reason: reason, Policy: policy, MessageID: "msg-1",
		}}
	}
}

func TestADeflectedMessageReachesTheDropAspectAndNothingElse(t *testing.T) {
	var ran []string
	var seen map[string]interface{}
	e := executorWith(t,
		&Config{Name: "after", When: "after", On: []string{"*"}, Action: &ActionConfig{Flow: "a"}},
		&Config{Name: "onerror", When: "on_error", On: []string{"*"}, Action: &ActionConfig{Flow: "b"}},
		&Config{Name: "ondrop", When: "on_drop", On: []string{"*"}, Action: &ActionConfig{Flow: "c", Transform: map[string]string{
			"reason":     "input.drop.reason",
			"policy":     "input.drop.policy",
			"message_id": "input.drop.message_id",
		}}},
	)
	e.SetFlowInvoker(invokerFunc(func(_ context.Context, name string, data map[string]interface{}) error {
		ran = append(ran, name)
		seen = data
		return nil
	}))

	_, err := e.Execute(context.Background(), "consume_order", "", "", nil,
		dropped("sequence_older", "ack"))

	// The disposition still reaches the caller, since that is what tells a
	// consumer to acknowledge rather than requeue.
	var dropErr *flow.FilteredDropError
	if !errors.As(err, &dropErr) {
		t.Fatalf("the drop was not reported to the caller: %v", err)
	}

	if len(ran) != 1 || ran[0] != "c" {
		t.Fatalf("aspects that ran: %v, want only the on_drop one", ran)
	}

	if seen["reason"] != "sequence_older" || seen["policy"] != "ack" {
		t.Errorf("the aspect was told %v, want the gate and the disposition", seen)
	}
	if seen["message_id"] != "msg-1" {
		t.Errorf("the message was not identified: %v", seen)
	}
}

func TestADropAspectCanSelectWhichGateItCaresAbout(t *testing.T) {
	// Otherwise an alerter for genuine rejections would also fire on every
	// duplicate a sequence guard quietly drops.
	var ran []string
	e := executorWith(t,
		&Config{
			Name: "alert_rejections", When: "on_drop", On: []string{"*"},
			If: `drop.reason == "accept"`, Action: &ActionConfig{Flow: "alert"},
		},
		&Config{
			Name: "count_duplicates", When: "on_drop", On: []string{"*"},
			If: `drop.reason == "sequence_older"`, Action: &ActionConfig{Flow: "count"},
		},
	)
	e.SetFlowInvoker(invokerFunc(func(_ context.Context, name string, _ map[string]interface{}) error {
		ran = append(ran, name)
		return nil
	}))

	_, _ = e.Execute(context.Background(), "consume", "", "", nil, dropped("sequence_older", "ack"))
	if len(ran) != 1 || ran[0] != "count" {
		t.Errorf("aspects that ran: %v, want only the one matching the gate", ran)
	}
}

func TestAFailingDropAspectDoesNotChangeTheDisposition(t *testing.T) {
	// A broken alerter must not turn an acknowledged drop into a requeue loop.
	e := executorWith(t, &Config{
		Name: "alert", When: "on_drop", On: []string{"*"}, Action: &ActionConfig{Flow: "alert"},
	})
	e.SetFlowInvoker(invokerFunc(func(context.Context, string, map[string]interface{}) error {
		return errors.New("the alerter is down")
	}))

	_, err := e.Execute(context.Background(), "consume", "", "", nil, dropped("filter", "ack"))
	var dropErr *flow.FilteredDropError
	if !errors.As(err, &dropErr) {
		t.Fatalf("the disposition was replaced by the aspect's failure: %v", err)
	}
	if dropErr.Result.Policy != "ack" {
		t.Errorf("policy = %q, want the one the flow chose", dropErr.Result.Policy)
	}
}

// An around aspect is the only one that can decide not to run the flow, which
// is what makes a rate limit or a circuit breaker possible.

func TestSeveralAroundAspectsNestAndTheFlowStillRunsOnce(t *testing.T) {
	// Each around aspect wraps the next, so a mistake in the chain shows up as
	// the flow running twice or not at all rather than as an error.
	e := executorWith(t,
		&Config{
			Name: "outer", When: "around", On: []string{"*"}, Priority: 1,
			CircuitBreaker: &CircuitBreakerConfig{FailureThreshold: 5, SuccessThreshold: 1, Timeout: "1m"},
		},
		&Config{
			Name: "inner", When: "around", On: []string{"*"}, Priority: 2,
			CircuitBreaker: &CircuitBreakerConfig{FailureThreshold: 5, SuccessThreshold: 1, Timeout: "1m"},
		},
	)

	flowFn, calls := succeeds()
	result, err := e.Execute(context.Background(), "f", "", "", nil, flowFn)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if *calls != 1 {
		t.Errorf("the flow ran %d times through two wrappers, want once", *calls)
	}
	if result == nil || len(result.Rows) != 1 {
		t.Errorf("the flow's result did not come back out through the wrappers: %v", result)
	}
}

func TestAnAspectWhoseConditionIsFalseStandsAside(t *testing.T) {
	// An around aspect decides whether the flow runs, so one that does not
	// apply has to pass it through rather than swallow it.
	e := executorWith(t, &Config{
		Name: "protect", When: "around", On: []string{"*"},
		If:             `input.mode == "guarded"`,
		CircuitBreaker: &CircuitBreakerConfig{FailureThreshold: 1, SuccessThreshold: 1, Timeout: "1m"},
	})

	flowFn, calls := succeeds()
	if _, err := e.Execute(context.Background(), "f", "", "", map[string]interface{}{"mode": "plain"}, flowFn); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if *calls != 1 {
		t.Errorf("the flow ran %d times, want once — the aspect did not apply", *calls)
	}
}

func TestAnAspectOnlyRunsForTheFlowsItNames(t *testing.T) {
	var ran []string
	e := executorWith(t, &Config{
		Name: "audit_writes", When: "before", On: []string{"create_*", "update_*"},
		Action: &ActionConfig{Flow: "audit", Transform: map[string]string{"flow": "input._flow"}},
	})
	e.SetFlowInvoker(invokerFunc(func(_ context.Context, _ string, data map[string]interface{}) error {
		ran = append(ran, data["flow"].(string))
		return nil
	}))

	flowFn, _ := succeeds()
	for _, name := range []string{"create_order", "update_order", "read_order", "delete_order"} {
		if _, err := e.Execute(context.Background(), name, "", "", nil, flowFn); err != nil {
			t.Fatalf("Execute %s: %v", name, err)
		}
	}

	if len(ran) != 2 || ran[0] != "create_order" || ran[1] != "update_order" {
		t.Errorf("the aspect ran for %v, want only the flows its patterns name", ran)
	}
}

func TestARateLimitRefusesOnceTheAllowanceIsSpent(t *testing.T) {
	e := executorWith(t, &Config{
		Name: "per_caller", When: "before", On: []string{"*"},
		RateLimit: &RateLimitConfig{Key: "${input.caller}", RequestsPerSecond: 1, Burst: 2},
	})

	flowFn, calls := succeeds()
	input := map[string]interface{}{"caller": "someone"}

	var refused error
	for i := 0; i < 5; i++ {
		if _, err := e.Execute(context.Background(), "f", "", "", input, flowFn); err != nil {
			refused = err
			break
		}
	}
	if refused == nil {
		t.Fatal("five requests were allowed against an allowance of two")
	}
	if !errors.Is(refused, ratelimit.ErrRateLimited) {
		t.Errorf("error = %v, want it recognisable as a rate limit", refused)
	}
	if *calls > 2 {
		t.Errorf("the flow ran %d times, want no more than the burst", *calls)
	}
}

func TestOneCallerSpendingItsAllowanceDoesNotAffectAnother(t *testing.T) {
	// The key is the whole point: a limit counted per caller must not become a
	// limit on the service.
	e := executorWith(t, &Config{
		Name: "per_caller", When: "before", On: []string{"*"},
		RateLimit: &RateLimitConfig{Key: "${input.caller}", RequestsPerSecond: 1, Burst: 1},
	})
	flowFn, _ := succeeds()

	for i := 0; i < 4; i++ {
		_, _ = e.Execute(context.Background(), "f", "", "", map[string]interface{}{"caller": "noisy"}, flowFn)
	}
	if _, err := e.Execute(context.Background(), "f", "", "", map[string]interface{}{"caller": "quiet"}, flowFn); err != nil {
		t.Errorf("a caller that had made no requests was refused: %v", err)
	}
}

func TestTwoAspectsWithTheSameRateKeepSeparateAllowances(t *testing.T) {
	// Each aspect declares its own limit. Sharing a budget between two of them
	// because they name the same number means a caller allowed ten a second by
	// each gets ten between them, decided by an unrelated aspect elsewhere in
	// the configuration.
	e := executorWith(t,
		&Config{
			Name: "limit_reads", When: "before", On: []string{"read_*"},
			RateLimit: &RateLimitConfig{Key: "${input.caller}", RequestsPerSecond: 1, Burst: 1},
		},
		&Config{
			Name: "limit_exports", When: "before", On: []string{"export_*"},
			RateLimit: &RateLimitConfig{Key: "${input.caller}", RequestsPerSecond: 1, Burst: 1},
		},
	)
	flowFn, _ := succeeds()
	input := map[string]interface{}{"caller": "someone"}

	if _, err := e.Execute(context.Background(), "read_orders", "", "", input, flowFn); err != nil {
		t.Fatalf("the first request was refused: %v", err)
	}
	if _, err := e.Execute(context.Background(), "export_orders", "", "", input, flowFn); err != nil {
		t.Errorf("a separate aspect's allowance was spent by another aspect: %v", err)
	}
}

func TestACircuitOpensAfterRepeatedFailuresAndSparesTheDependency(t *testing.T) {
	e := executorWith(t, &Config{
		Name: "protect_api", When: "around", On: []string{"*"},
		CircuitBreaker: &CircuitBreakerConfig{
			Name: "payments", FailureThreshold: 2, SuccessThreshold: 1, Timeout: "1m",
		},
	})

	attempts := 0
	failing := func(context.Context, map[string]interface{}) (*connector.Result, error) {
		attempts++
		return nil, errors.New("connection refused")
	}

	for i := 0; i < 5; i++ {
		_, _ = e.Execute(context.Background(), "charge", "", "", nil, failing)
	}

	if attempts > 2 {
		t.Errorf("the dependency was called %d times, want it spared after the circuit opened", attempts)
	}

	_, err := e.Execute(context.Background(), "charge", "", "", nil, failing)
	if err == nil {
		t.Fatal("a call through an open circuit reported success")
	}
	if !strings.Contains(err.Error(), "payments") {
		t.Errorf("error = %q, want it to name the circuit", err)
	}
}

func TestTwoUnnamedCircuitsDoNotTripEachOther(t *testing.T) {
	// A name is how several flows calling one dependency trip together. Without
	// one, the breaker belongs to the aspect that declared it — otherwise a
	// failing dependency opens the circuit on unrelated flows that never called
	// it.
	e := executorWith(t,
		&Config{
			Name: "protect_payments", When: "around", On: []string{"charge_*"},
			CircuitBreaker: &CircuitBreakerConfig{FailureThreshold: 1, SuccessThreshold: 1, Timeout: "1m"},
		},
		&Config{
			Name: "protect_shipping", When: "around", On: []string{"ship_*"},
			CircuitBreaker: &CircuitBreakerConfig{FailureThreshold: 1, SuccessThreshold: 1, Timeout: "1m"},
		},
	)

	failing := func(context.Context, map[string]interface{}) (*connector.Result, error) {
		return nil, errors.New("connection refused")
	}
	for i := 0; i < 3; i++ {
		_, _ = e.Execute(context.Background(), "charge_card", "", "", nil, failing)
	}

	working, calls := succeeds()
	if _, err := e.Execute(context.Background(), "ship_order", "", "", nil, working); err != nil {
		t.Errorf("an unrelated flow was refused by another aspect's circuit: %v", err)
	}
	if *calls != 1 {
		t.Error("the unrelated flow did not run")
	}
}

func TestASharedCircuitTripsForEveryAspectNamingIt(t *testing.T) {
	// The other half of the same rule: naming a dependency is how flows that
	// call it agree to stop together.
	e := executorWith(t,
		&Config{
			Name: "reads", When: "around", On: []string{"read_*"},
			CircuitBreaker: &CircuitBreakerConfig{Name: "payments", FailureThreshold: 1, SuccessThreshold: 1, Timeout: "1m"},
		},
		&Config{
			Name: "writes", When: "around", On: []string{"write_*"},
			CircuitBreaker: &CircuitBreakerConfig{Name: "payments", FailureThreshold: 1, SuccessThreshold: 1, Timeout: "1m"},
		},
	)

	failing := func(context.Context, map[string]interface{}) (*connector.Result, error) {
		return nil, errors.New("connection refused")
	}
	for i := 0; i < 2; i++ {
		_, _ = e.Execute(context.Background(), "read_balance", "", "", nil, failing)
	}

	working, calls := succeeds()
	if _, err := e.Execute(context.Background(), "write_charge", "", "", nil, working); err == nil {
		t.Error("a flow naming the same failing dependency was still let through")
	}
	if *calls != 0 {
		t.Error("the dependency was called through an open circuit")
	}
}

func TestAspectsAreSafeUnderConcurrentFlows(t *testing.T) {
	// Limiters and breakers are created on first use and shared afterwards,
	// which is a race if the creation is not guarded.
	e := executorWith(t,
		&Config{
			Name: "limit", When: "before", On: []string{"*"},
			RateLimit: &RateLimitConfig{Key: "${input.caller}", RequestsPerSecond: 1000, Burst: 1000},
		},
		&Config{
			Name: "protect", When: "around", On: []string{"*"},
			CircuitBreaker: &CircuitBreakerConfig{FailureThreshold: 100, SuccessThreshold: 1, Timeout: "1m"},
		},
	)
	var ran atomic.Int64
	flowFn := func(context.Context, map[string]interface{}) (*connector.Result, error) {
		ran.Add(1)
		return &connector.Result{}, nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = e.Execute(context.Background(), "f", "", "", map[string]interface{}{"caller": "someone"}, flowFn)
		}()
	}
	wg.Wait()

	if ran.Load() != 50 {
		t.Errorf("%d of 50 concurrent flows ran", ran.Load())
	}
}
