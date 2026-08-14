package grpc

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// How a client reaches the service it calls: over plaintext or TLS, against one
// address or several behind a load balancer. Every one of these decisions is
// made once at startup and then shows up, if it is wrong, as a connection that
// never establishes.

func clientWith(config *ClientConfig) *ClientConnector {
	return &ClientConnector{config: config}
}

func TestAPlaintextConnectionIsTheDefault(t *testing.T) {
	// Most gRPC in a cluster is plaintext behind a mesh, and a client that
	// insisted on TLS by default would reach none of it.
	for name, config := range map[string]*ClientConfig{
		"asked for plaintext": {Target: "svc:50051", Insecure: true},
		"nothing said at all": {Target: "svc:50051"},
		"TLS turned off":      {Target: "svc:50051", TLS: &TLSConfig{Enabled: false}},
	} {
		t.Run(name, func(t *testing.T) {
			opts, err := clientWith(config).buildDialOptions()
			if err != nil {
				t.Fatalf("buildDialOptions: %v", err)
			}
			if len(opts) == 0 {
				t.Error("the connection carries no transport credentials at all, which gRPC refuses")
			}
		})
	}
}

func TestATLSConnectionUsesTheConfiguredAuthority(t *testing.T) {
	dir := t.TempDir()
	caFile, _, _ := certificate(t, dir, "ca", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	certFile, keyFile, _ := certificate(t, dir, "client", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))

	c := clientWith(&ClientConfig{
		Target: "svc:50051",
		TLS: &TLSConfig{
			Enabled: true, CAFile: caFile, CertFile: certFile, KeyFile: keyFile,
			ServerName: "orders.internal",
		},
	})

	if _, err := c.buildDialOptions(); err != nil {
		t.Fatalf("buildDialOptions: %v", err)
	}
	if _, err := c.buildTLSCredentials(); err != nil {
		t.Fatalf("buildTLSCredentials: %v", err)
	}
}

func TestTLSMaterialThatCannotBeLoadedStopsTheConnection(t *testing.T) {
	dir := t.TempDir()
	_, keyFile, _ := certificate(t, dir, "client", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))

	for name, tlsConfig := range map[string]*TLSConfig{
		"an authority that is not there": {Enabled: true, CAFile: filepath.Join(dir, "absent.pem")},
		"a certificate that is not there": {
			Enabled: true, CertFile: filepath.Join(dir, "absent.pem"), KeyFile: keyFile,
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := clientWith(&ClientConfig{Target: "svc:50051", TLS: tlsConfig})
			if _, err := c.buildDialOptions(); err == nil {
				t.Error("the client was built anyway, so the failure surfaces on the first call")
			}
		})
	}
}

func TestTheOptionsAServiceIsConfiguredWithReachTheConnection(t *testing.T) {
	// Each of these is one dial option, and a setting that does not become one
	// is a setting somebody wrote and nothing honoured.
	plain, err := clientWith(&ClientConfig{Target: "svc:50051", Insecure: true}).buildDialOptions()
	if err != nil {
		t.Fatalf("buildDialOptions: %v", err)
	}

	full, err := clientWith(&ClientConfig{
		Target:        "svc:50051",
		Insecure:      true,
		WaitForReady:  true,
		MaxRecv:       16,
		MaxSend:       16,
		KeepAlive:     &KeepAliveConfig{Time: 30 * time.Second, Timeout: 10 * time.Second},
		LoadBalancing: &LoadBalancingConfig{Policy: "round_robin"},
		Auth:          &ClientAuthConfig{Type: "bearer", Token: "t"},
	}).buildDialOptions()
	if err != nil {
		t.Fatalf("buildDialOptions: %v", err)
	}

	if len(full) <= len(plain) {
		t.Errorf("%d options with everything configured against %d with nothing: the settings are being dropped",
			len(full), len(plain))
	}
}

func TestTheLoadBalancingPolicyIsSomethingGRPCUnderstands(t *testing.T) {
	// gRPC parses this as JSON and ignores a service config it cannot read, so
	// a malformed one is a load balancer that silently does not balance.
	for name, lb := range map[string]*LoadBalancingConfig{
		"round robin":              {Policy: "round_robin"},
		"pick first":               {Policy: "pick_first"},
		"nothing said":             {},
		"with health checking":     {Policy: "round_robin", HealthCheck: true},
		"a policy of its own name": {Policy: "grpclb"},
	} {
		t.Run(name, func(t *testing.T) {
			config := clientWith(&ClientConfig{LoadBalancing: lb}).buildServiceConfig()

			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(config), &parsed); err != nil {
				t.Fatalf("the service config is not JSON, so gRPC discards it: %v (%s)", err, config)
			}
			if _, ok := parsed["loadBalancingConfig"]; !ok {
				t.Errorf("no load balancing policy in %s", config)
			}
			if lb.HealthCheck {
				if _, ok := parsed["healthCheckConfig"]; !ok {
					t.Errorf("health checking was asked for and is not in %s", config)
				}
			}
		})
	}
}

func TestNoLoadBalancingMeansNoServiceConfig(t *testing.T) {
	if config := clientWith(&ClientConfig{}).buildServiceConfig(); config != "" {
		t.Errorf("service config = %q, want none at all", config)
	}
}

func TestBalancingAcrossSeveralAddressesNeedsAResolver(t *testing.T) {
	// A bare host:port names one connection however many targets are listed:
	// gRPC balances across what a resolver returns, so without a scheme the
	// policy has nothing to balance over.
	c := clientWith(&ClientConfig{
		Target:        "orders:50051",
		LoadBalancing: &LoadBalancingConfig{Policy: "round_robin", Targets: []string{"a:50051", "b:50051"}},
	})
	if got := c.GetTarget(); !strings.HasPrefix(got, "dns:///") {
		t.Errorf("target = %q, want a resolver in front of it", got)
	}

	// One that already names its own resolver is left alone.
	c = clientWith(&ClientConfig{
		Target:        "xds:///orders",
		LoadBalancing: &LoadBalancingConfig{Targets: []string{"a:50051"}},
	})
	if got := c.GetTarget(); got != "xds:///orders" {
		t.Errorf("target = %q, want the address as written", got)
	}

	// And without load balancing the address is used exactly as given.
	c = clientWith(&ClientConfig{Target: "orders:50051"})
	if got := c.GetTarget(); got != "orders:50051" {
		t.Errorf("target = %q, want the address as written", got)
	}
}

// --- What the configuration file turns into ---------------------------------

func TestClientCredentialsAreReadFromTheConfiguration(t *testing.T) {
	auth := parseClientAuthConfig(map[string]interface{}{
		"type":          "oauth2",
		"token_url":     "https://login.example.com/token",
		"client_id":     "mycel",
		"client_secret": "s3cret",
		"scopes":        []interface{}{"orders:read"},
	})

	if auth.OAuth2 == nil {
		t.Fatal("no OAuth2 configuration was built, so the client sends nothing")
	}
	if auth.OAuth2.TokenURL != "https://login.example.com/token" || auth.OAuth2.ClientID != "mycel" {
		t.Errorf("OAuth2 = %+v", auth.OAuth2)
	}
	if len(auth.OAuth2.Scopes) != 1 || auth.OAuth2.Scopes[0] != "orders:read" {
		t.Errorf("scopes = %v", auth.OAuth2.Scopes)
	}

	key := parseClientAuthConfig(map[string]interface{}{
		"type":     "api_key",
		"api_key":  "k-123",
		"metadata": "x-tenant-key",
	})
	if key.APIKey == nil || key.APIKey.Key != "k-123" || key.APIKey.Metadata != "x-tenant-key" {
		t.Errorf("API key = %+v", key.APIKey)
	}

	bearer := parseClientAuthConfig(map[string]interface{}{"type": "bearer", "token": "t"})
	if bearer.Token != "t" {
		t.Errorf("token = %q", bearer.Token)
	}
}

func TestTheKeysAServerAcceptsAreReadFromTheConfiguration(t *testing.T) {
	// Written as a list, which is what rotation looks like: the new key is
	// added, callers move over, the old one is removed.
	many := parseAPIKeyConfig(map[string]interface{}{
		"keys":     []interface{}{"k-1", "k-2"},
		"header":   "x-tenant-key",
		"metadata": "tenant-key",
	})
	if len(many.Keys) != 2 {
		t.Errorf("keys = %v, want both of them", many.Keys)
	}
	if many.Header != "x-tenant-key" || many.Metadata != "tenant-key" {
		t.Errorf("header = %q, metadata = %q", many.Header, many.Metadata)
	}

	// And as a single one, which is what most configurations write.
	one := parseAPIKeyConfig(map[string]interface{}{"keys": "k-1"})
	if len(one.Keys) != 1 || one.Keys[0] != "k-1" {
		t.Errorf("keys = %v", one.Keys)
	}
	// The usual names, so a configuration that says nothing still matches what
	// callers send.
	if one.Header != "x-api-key" || one.Metadata != "api-key" {
		t.Errorf("header = %q, metadata = %q", one.Header, one.Metadata)
	}
}

func TestTheAudienceAServerAcceptsCanBeOneOrSeveral(t *testing.T) {
	// A service fronting two audiences during a migration accepts either.
	several := parseJWTAuthConfig(map[string]interface{}{
		"audience":   []interface{}{"orders-api", "orders-api-v2"},
		"algorithms": []interface{}{"RS256", "ES256"},
	})
	if len(several.Audience) != 2 {
		t.Errorf("audience = %v, want both", several.Audience)
	}
	if len(several.Algorithms) != 2 {
		t.Errorf("algorithms = %v, want both", several.Algorithms)
	}

	one := parseJWTAuthConfig(map[string]interface{}{"audience": "orders-api", "secret": "s"})
	if len(one.Audience) != 1 || one.Audience[0] != "orders-api" {
		t.Errorf("audience = %v", one.Audience)
	}
}
