package examples

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// What the TLS example is for: that naming a certificate authority is what
// makes the call work, and that not naming one is what makes it fail.
//
// Its README shows both, and the harness runs one of them — the failing call
// answers 500, which it treats as a broken example rather than a
// demonstration. So the pair is asserted here, together, since either alone
// proves nothing: a call that succeeds with a CA named tells you nothing if
// the same call succeeds without one.
func TestTheTLSExampleTrustsOnlyWhatItWasTold(t *testing.T) {
	if testing.Short() {
		t.Skip("starting services")
	}

	endpoint := os.Getenv("MYCEL_TEST_TLS_URL")
	if endpoint == "" {
		t.Skip("set MYCEL_TEST_TLS_URL to run this against a real TLS endpoint")
	}

	svc := start(t, "tls",
		"TLS_URL="+endpoint,
		"TLS_CA_CERT="+fetchTestCA(t))

	port := svc.ports[3000]
	if port == 0 {
		t.Fatal("the example's REST port was not moved; nothing to talk to")
	}
	base := fmt.Sprintf("http://localhost:%d", port)

	// Named CA: the certificate is signed by it, so the call goes through.
	status, answer := svc.run(t, fmt.Sprintf(`curl %s/internal`, base))
	if status != 200 {
		t.Errorf("the call verified against the named CA answered %d: %s", status, answer)
	}

	// No CA named: the machine's trust store has never heard of a certificate
	// this endpoint signed itself, and the call is refused. This is the half
	// that proves the first one meant something.
	status, answer = svc.run(t, fmt.Sprintf(`curl %s/untrusted`, base))
	if status == 200 {
		t.Errorf("a certificate nobody vouches for was accepted: %s", answer)
	}
	if !strings.Contains(answer, "certificate") {
		t.Errorf("the refusal does not say it was about the certificate: %s", answer)
	}

	// Verification off: anything is accepted, which is the point and the
	// danger. Mycel says so at startup.
	status, answer = svc.run(t, fmt.Sprintf(`curl %s/unverified`, base))
	if status != 200 {
		t.Errorf("insecure_skip_verify did not skip verification: %d %s", status, answer)
	}
	if log := svc.tail(); !strings.Contains(log, "TLS verification disabled") {
		t.Errorf("nothing warned that a connector is not verifying certificates:\n%s", log)
	}
}
