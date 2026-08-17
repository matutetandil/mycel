package webhook

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// An allow-list on an inbound webhook is the control that says only the
// provider may deliver here — Stripe, GitHub, a partner's VPN. It decided on
// the address in X-Forwarded-For, which is written by whoever sends the
// request, so a single header let anyone on the internet past a list of the
// provider's addresses. The refusal that never happened is invisible: the
// delivery simply succeeds.

func inbound(allowed, trusted []string) *InboundConnector {
	return &InboundConnector{config: &InboundConfig{
		AllowedIPs:     allowed,
		TrustedProxies: trusted,
	}}
}

func from(peer string, headers map[string]string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/hooks/orders", nil)
	request.RemoteAddr = peer + ":54321"
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	return request
}

func TestAForwardingHeaderFromAStrangerIsNotBelieved(t *testing.T) {
	// The attack, in one line: claim to be the provider by saying so.
	c := inbound([]string{"203.0.113.9"}, nil)

	request := from("198.51.100.7", map[string]string{"X-Forwarded-For": "203.0.113.9"})
	if got := c.getClientIP(request); got != "198.51.100.7" {
		t.Errorf("the caller was taken to be %q, want the peer we can see", got)
	}
	if c.isIPAllowed(c.getClientIP(request)) {
		t.Error("a stranger claiming the provider's address was allowed through")
	}

	// X-Real-IP is the same claim under another name.
	request = from("198.51.100.7", map[string]string{"X-Real-IP": "203.0.113.9"})
	if c.isIPAllowed(c.getClientIP(request)) {
		t.Error("a stranger was allowed through by X-Real-IP")
	}
}

func TestTheProviderReachingUsDirectlyIsAllowed(t *testing.T) {
	c := inbound([]string{"203.0.113.9"}, nil)
	if !c.isIPAllowed(c.getClientIP(from("203.0.113.9", nil))) {
		t.Error("the provider was refused")
	}
}

func TestBehindANamedProxyTheForwardedAddressIsUsed(t *testing.T) {
	// The deployment this has to keep working: an ingress in front, so the
	// peer is always the ingress and the provider's address is only in the
	// header. Naming the ingress is what makes that header worth reading.
	c := inbound([]string{"203.0.113.9"}, []string{"10.0.0.0/8"})

	request := from("10.0.0.5", map[string]string{"X-Forwarded-For": "203.0.113.9"})
	if got := c.getClientIP(request); got != "203.0.113.9" {
		t.Errorf("the caller was taken to be %q, want the address the proxy forwarded", got)
	}
	if !c.isIPAllowed(c.getClientIP(request)) {
		t.Error("the provider was refused from behind a named proxy")
	}
}

func TestAProxyDoesNotLetAStrangerClaimAnAddress(t *testing.T) {
	// Trusting the hop does not mean trusting what the caller prepended to the
	// chain. Taking the leftmost entry would let a caller write its own
	// address in and have the proxy append the real one behind it.
	c := inbound([]string{"203.0.113.9"}, []string{"10.0.0.0/8"})

	// The caller sent "203.0.113.9"; the ingress appended what it saw.
	request := from("10.0.0.5", map[string]string{
		"X-Forwarded-For": "203.0.113.9, 198.51.100.7",
	})
	if got := c.getClientIP(request); got != "198.51.100.7" {
		t.Errorf("the caller was taken to be %q, want the address the proxy observed", got)
	}
	if c.isIPAllowed(c.getClientIP(request)) {
		t.Error("a forged first hop was allowed through a named proxy")
	}
}

func TestSeveralOfOurOwnHopsAreSkipped(t *testing.T) {
	// A load balancer in front of an ingress: both are ours, and the caller is
	// the first address that is not.
	c := inbound([]string{"203.0.113.9"}, []string{"10.0.0.0/8", "172.16.0.0/12"})

	request := from("10.0.0.5", map[string]string{
		"X-Forwarded-For": "203.0.113.9, 172.16.4.4",
	})
	if got := c.getClientIP(request); got != "203.0.113.9" {
		t.Errorf("the caller was taken to be %q, want the one beyond our own hops", got)
	}
}

func TestARangeIsMatchedAsWellAsAnAddress(t *testing.T) {
	// Providers publish ranges, not addresses.
	c := inbound([]string{"203.0.113.0/24", "192.0.2.10"}, nil)

	for address, want := range map[string]bool{
		"203.0.113.1":   true,
		"203.0.113.254": true,
		"192.0.2.10":    true,
		"203.0.114.1":   false,
		"192.0.2.11":    false,
	} {
		if got := c.isIPAllowed(address); got != want {
			t.Errorf("%s allowed = %v, want %v", address, got, want)
		}
	}
}

func TestAnEmptyListAllowsNobody(t *testing.T) {
	// The handler only consults the list when one was written, so this is the
	// answer for the list itself: nothing matches nothing. Answering true here
	// would make a misconfigured list open the door to everyone.
	c := inbound(nil, nil)
	if c.isIPAllowed("203.0.113.9") {
		t.Error("an empty allow-list allowed an address")
	}
	if c.isIPAllowed("") {
		t.Error("an empty address matched")
	}
}

func TestAListEntryThatIsNotAnAddressMatchesNothing(t *testing.T) {
	// A typo in a range must not widen the list.
	c := inbound([]string{"203.0.113.0/notacidr", "  ", "not-an-address"}, nil)
	for _, address := range []string{"203.0.113.9", "198.51.100.7", "0.0.0.0"} {
		if c.isIPAllowed(address) {
			t.Errorf("%s matched a list with nothing valid in it", address)
		}
	}
	// And the exact text still matches itself, which is the one case where a
	// non-address entry is meaningful.
	if !c.isIPAllowed("not-an-address") {
		t.Error("an exact entry stopped matching itself")
	}
}

func TestARequestWithNoUsableAddressIsNotAllowed(t *testing.T) {
	c := inbound([]string{"203.0.113.9"}, nil)
	request := httptest.NewRequest(http.MethodPost, "/hooks/orders", nil)
	request.RemoteAddr = "" // what a unix socket or a test double can leave

	if c.isIPAllowed(c.getClientIP(request)) {
		t.Error("a request with no address was allowed")
	}
}

func TestIPv6IsHandled(t *testing.T) {
	c := inbound([]string{"2001:db8::/32"}, nil)

	request := from("[2001:db8::1]", nil)
	request.RemoteAddr = "[2001:db8::1]:54321"
	if got := c.getClientIP(request); got != "2001:db8::1" {
		t.Fatalf("the caller was taken to be %q", got)
	}
	if !c.isIPAllowed("2001:db8::1") {
		t.Error("an address inside the range was refused")
	}
	if c.isIPAllowed("2001:db9::1") {
		t.Error("an address outside the range was allowed")
	}
}
