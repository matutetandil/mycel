package connector

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// What a consumer does with a message it could not process is decided here, and
// getting it wrong is expensive in both directions: a message replayed for ever
// because nothing said the failure was final — the redelivery loop a live
// consumer hit — or one dropped that would have gone through on the next
// attempt.
//
// None of this had tests.

// rejected is the shape a connector's own error takes when the destination
// refused the payload rather than being unavailable — an HTTP 4xx.
type rejected struct{ message string }

func (e *rejected) Error() string     { return e.message }
func (e *rejected) IsPermanent() bool { return true }

// unavailable is the other kind: the same request may well work later.
type unavailable struct{ message string }

func (e *unavailable) Error() string     { return e.message }
func (e *unavailable) IsPermanent() bool { return false }

func TestAFailureThatReplayingCannotFixIsFinal(t *testing.T) {
	if !IsPermanent(&rejected{"400 Bad Request"}) {
		t.Error("a payload the destination refused was reported as worth retrying")
	}
	if IsPermanent(&unavailable{"503 Service Unavailable"}) {
		t.Error("a destination that was briefly down was reported as final")
	}
	if IsPermanent(errors.New("connection reset")) {
		t.Error("an error that says nothing about itself was treated as final")
	}
	if IsPermanent(nil) {
		t.Error("nothing at all was treated as a failure")
	}
}

func TestAFailureIsStillFinalOnceItHasBeenWrappedUp(t *testing.T) {
	// By the time a consumer sees it, the error has been through the flow,
	// the destination and the retry budget, each adding context. Losing the
	// classification on the way is what turns a 4xx into a redelivery loop.
	err := fmt.Errorf("flow %q failed: %w", "sync_orders",
		fmt.Errorf("writing to api: %w", &rejected{"422 Unprocessable Entity"}))

	if !IsPermanent(err) {
		t.Error("a final failure stopped being final once it was wrapped")
	}
}

func TestATimeoutIsRecognisedHoweverTheClientSpellsIt(t *testing.T) {
	// Every client says it differently and only one of them is a typed error,
	// so this is matched on what they write. A timeout is worth telling apart:
	// the local request was abandoned but the far side may still be working,
	// so replaying it can start the same operation twice.
	for name, err := range map[string]error{
		"the typed one":      context.DeadlineExceeded,
		"wrapped":            fmt.Errorf("calling the api: %w", context.DeadlineExceeded),
		"as net/http says":   errors.New(`Get "https://api": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`),
		"as a database says": errors.New("canceling statement due to statement timeout"),
		"as a driver says":   errors.New("read tcp 10.0.0.1:5432: i/o timeout"),
	} {
		t.Run(name, func(t *testing.T) {
			if !IsTimeoutError(err) {
				t.Errorf("%v was not recognised as a timeout", err)
			}
		})
	}
}

func TestSomethingThatIsNotATimeoutIsNotOne(t *testing.T) {
	for name, err := range map[string]error{
		"nothing":            nil,
		"a refused payload":  errors.New("422 Unprocessable Entity"),
		"a closed socket":    errors.New("connection reset by peer"),
		"a cancelled parent": context.Canceled,
	} {
		t.Run(name, func(t *testing.T) {
			if IsTimeoutError(err) {
				t.Errorf("%v was mistaken for a timeout", err)
			}
		})
	}
}

// A disposition is the flow saying outright what the broker should do, rather
// than the consumer guessing from whether the failure looked final.

func TestADispositionSurvivesBeingWrappedUpToo(t *testing.T) {
	err := fmt.Errorf("flow failed: %w",
		NewDispositionError(errors.New("the api timed out"), DispositionAck))

	disposition, found := GetDisposition(err)
	if !found {
		t.Fatal("the disposition the flow chose was lost on the way to the consumer")
	}
	if disposition != DispositionAck {
		t.Errorf("disposition = %q", disposition)
	}
}

func TestAFailureWithNoDispositionSaysSo(t *testing.T) {
	// The consumer then falls back to deciding for itself, which is what every
	// flow that does not configure error_handling relies on.
	if _, found := GetDisposition(errors.New("something went wrong")); found {
		t.Error("a disposition was found where none was chosen")
	}
	if _, found := GetDisposition(nil); found {
		t.Error("a disposition was found on no error at all")
	}
}

func TestADispositionAlsoAnswersTheOlderQuestion(t *testing.T) {
	// Anything that only understands final-versus-retry — the flow's retry
	// budget, a consumer that has not learnt to read dispositions — has to
	// reach the same conclusion, or the two disagree about the same message.
	for _, tc := range []struct {
		disposition Disposition
		final       bool
	}{
		{DispositionAck, true},      // dropped on purpose: do not replay
		{DispositionReject, true},   // dead-lettered: do not replay
		{DispositionRequeue, false}, // asked for again: that is a retry
	} {
		err := NewDispositionError(errors.New("failed"), tc.disposition)
		if got := IsPermanent(err); got != tc.final {
			t.Errorf("%q is reported as final=%v, want %v", tc.disposition, got, tc.final)
		}
	}
}

func TestTheUnderlyingFailureIsStillReadable(t *testing.T) {
	// Whatever the consumer decides, the log line has to say what actually
	// happened.
	underlying := errors.New("the database refused the row")
	err := NewDispositionError(underlying, DispositionReject)

	if err.Error() != underlying.Error() {
		t.Errorf("error = %q, want what went wrong underneath", err)
	}
	if !errors.Is(err, underlying) {
		t.Error("the original failure cannot be reached through the disposition")
	}
}

func TestTheTimeoutCheckIsTextualAndSaysSoHere(t *testing.T) {
	// Only one client reports a timeout as a typed error; the rest write it in
	// prose, so this is a search for the word. That has a cost worth writing
	// down: a destination whose refusal happens to mention the word is read as
	// a timeout, and a flow with on_timeout = "ack" would drop it.
	//
	// It stays textual because the alternative — recognising only the phrases
	// known today — misses a real timeout from a client nobody has used yet,
	// and treating a timeout as an ordinary failure replays a request the far
	// side may still be working on. Mycel's own messages of this shape come
	// from the parser and the factories, which run at startup and never reach
	// this check.
	if !IsTimeoutError(errors.New(`400: {"error":"unknown field: timeout"}`)) {
		t.Skip("the check has been tightened; the note above needs revisiting")
	}
}
