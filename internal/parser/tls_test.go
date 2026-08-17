package parser

import (
	"fmt"
	"strings"
	"testing"
)

// TLS has one vocabulary, and the parser is where that is enforced. These tests
// cover the two ways it used to fail: names the parser rejected outright, and
// names it accepted for one connector while others read something else.

func tlsProps(t *testing.T, body string) map[string]interface{} {
	t.Helper()

	cfg, err := tryParse(t, fmt.Sprintf(`
connector "c" {
  type = "http"
  base_url = "https://api.example.com"

  tls {
%s
  }
}
`, body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cfg.Connectors) != 1 {
		t.Fatalf("got %d connectors", len(cfg.Connectors))
	}
	tls, ok := cfg.Connectors[0].Properties["tls"].(map[string]interface{})
	if !ok {
		t.Fatalf("no tls properties: %#v", cfg.Connectors[0].Properties)
	}
	return tls
}

func TestTLSCanonicalNames(t *testing.T) {
	tls := tlsProps(t, `
    ca_cert              = "/certs/ca.pem"
    cert                 = "/certs/me.pem"
    key                  = "/certs/me.key"
    server_name          = "api.internal"
    insecure_skip_verify = true`)

	for k, want := range map[string]interface{}{
		"ca_cert":              "/certs/ca.pem",
		"cert":                 "/certs/me.pem",
		"key":                  "/certs/me.key",
		"server_name":          "api.internal",
		"insecure_skip_verify": true,
		"enabled":              true,
	} {
		if tls[k] != want {
			t.Errorf("%s = %#v, want %#v", k, tls[k], want)
		}
	}
}

func TestTLSHistoricalSpellingsAreFoldedOntoTheCanonicalNames(t *testing.T) {
	// Every one of these was written somewhere: client_cert and client_key in
	// the http connector's own schema, the rest in the gRPC one. None of them
	// may break, and all of them have to arrive under one name so a connector
	// reads a single set.
	for _, tc := range []struct{ wrote, canonical string }{
		{`ca_file = "/certs/ca.pem"`, "ca_cert"},
		{`cert_file = "/certs/me.pem"`, "cert"},
		{`key_file = "/certs/me.key"`, "key"},
		{`client_cert = "/certs/me.pem"`, "cert"},
		{`client_key = "/certs/me.key"`, "key"},
	} {
		t.Run(strings.SplitN(tc.wrote, " ", 2)[0], func(t *testing.T) {
			tls := tlsProps(t, "    "+tc.wrote)
			if _, canonical := tls[tc.canonical]; !canonical {
				t.Errorf("%s did not land on %s: %#v", tc.wrote, tc.canonical, tls)
			}
			old := strings.SplitN(tc.wrote, " ", 2)[0]
			if _, kept := tls[old]; kept {
				t.Errorf("the old name %s survived alongside the canonical one", old)
			}
		})
	}

	if tls := tlsProps(t, `    skip_verify = true`); tls["insecure_skip_verify"] != true {
		t.Errorf("skip_verify did not land on insecure_skip_verify: %#v", tls)
	}
}

func TestTLSEnabledDefaultsToTrueWhenTheBlockIsPresent(t *testing.T) {
	// Writing the block is the opt-in. Requiring enabled = true on top of it is
	// ceremony, and it was worse than ceremony here: enabled was not accepted
	// at all, while mq and mqtt refused to build TLS without it, so those
	// connectors could not be given TLS by any spelling.
	if tls := tlsProps(t, `    ca_cert = "/certs/ca.pem"`); tls["enabled"] != true {
		t.Errorf("enabled = %#v, want true", tls["enabled"])
	}

	// And it can still be turned off without deleting the certificates, which
	// is what makes it settable from the environment.
	if tls := tlsProps(t, `    enabled = false
    ca_cert = "/certs/ca.pem"`); tls["enabled"] != false {
		t.Errorf("enabled = %#v, want false", tls["enabled"])
	}
}

func TestTLSRejectsTwoSpellingsOfOneSetting(t *testing.T) {
	_, err := tryParse(t, `
connector "c" {
  type = "http"
  base_url = "https://api.example.com"

  tls {
    cert      = "/certs/a.pem"
    cert_file = "/certs/b.pem"
  }
}
`)
	if err == nil {
		t.Fatal("both spellings were accepted, so one was silently discarded")
	}
	if !strings.Contains(err.Error(), "same setting") {
		t.Errorf("error = %q, want it to explain that the two are one setting", err)
	}
}

func TestTLSRejectsAnUnknownAttribute(t *testing.T) {
	_, err := tryParse(t, `
connector "c" {
  type = "http"
  base_url = "https://api.example.com"

  tls {
    certificate = "/certs/me.pem"
  }
}
`)
	if err == nil {
		t.Fatal("an unknown tls attribute was accepted")
	}
}
