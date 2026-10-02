package redis

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

// Whether there is a Redis at an address, asked properly.
//
// The tests in this package skip themselves when nothing is there, and they
// decided that by opening a socket — which answers a different question. A CI
// runner had something HTTP on the port this package defaults to: the dial
// succeeded, the suite ran, the client sent RESP and got back
// `HTTP/1.1 400 Bad Request`, and three tests failed in a job whose only
// correct behaviour was to skip them.

func TestTheProbeAcceptsARealRedis(t *testing.T) {
	server := miniredis.RunT(t)

	if err := redisAnswers(server.Addr()); err != nil {
		t.Errorf("a real Redis was not recognised: %v", err)
	}
}

func TestTheProbeRejectsSomethingThatIsNotRedis(t *testing.T) {
	// The exact shape of the CI failure: an HTTP server on the port.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	address := strings.TrimPrefix(server.URL, "http://")
	err := redisAnswers(address)
	if err == nil {
		t.Fatal("an HTTP server passed as Redis, which is how the suite ran against one")
	}
	if !strings.Contains(err.Error(), "not Redis") {
		t.Errorf("the error does not say what is wrong: %v", err)
	}
	if !strings.Contains(err.Error(), "HTTP") {
		t.Errorf("the error does not quote what answered, which is what identifies the squatter: %v", err)
	}
}

func TestTheProbeReportsNothingListening(t *testing.T) {
	// A port nobody holds: this is the ordinary "no stack running" case, and
	// it has to stay distinguishable from the one above.
	if err := redisAnswers("127.0.0.1:1"); err == nil {
		t.Error("a port with nothing on it was reported as Redis")
	}
}
