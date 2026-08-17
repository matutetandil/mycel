package parser

import (
	"fmt"
	"sort"

	"github.com/hashicorp/hcl/v2"
)

// TLS is configured with one vocabulary on every connector.
//
// It did not start that way. Each connector grew its own names — http took
// client_cert/client_key, grpc took cert_file/key_file/ca_file/skip_verify, and
// tcp, mq and mqtt took cert/key — while the parser accepted only http's set.
// The result was that cert and key were rejected everywhere, so mutual TLS
// could not be configured on tcp, mq, mqtt or grpc at all, and grpc's own
// schema advertised six attributes the parser refused.
//
// The canonical names below are the ones the majority of connectors already
// read. Every historical spelling is still accepted and folded onto its
// canonical name here, so no working configuration breaks; connectors read one
// set of names and stop caring which spelling was written.
const (
	tlsEnabled            = "enabled"
	tlsCACert             = "ca_cert"
	tlsCert               = "cert"
	tlsKey                = "key"
	tlsServerName         = "server_name"
	tlsInsecureSkipVerify = "insecure_skip_verify"
)

// tlsAliases maps each accepted historical spelling onto its canonical name.
var tlsAliases = map[string]string{
	"ca_file":     tlsCACert,             // grpc
	"cert_file":   tlsCert,               // grpc
	"key_file":    tlsKey,                // grpc
	"client_cert": tlsCert,               // http
	"client_key":  tlsKey,                // http
	"skip_verify": tlsInsecureSkipVerify, // grpc
}

// parseTLSBlock reads a connector's tls block into canonical properties.
//
// Writing the block means asking for TLS, so enabled defaults to true — the
// same rule the mfa block follows, where presence is the opt-in and
// `enabled = false` is how you turn it off without deleting the configuration.
func parseTLSBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	attrs := []hcl.AttributeSchema{
		{Name: tlsEnabled},
		{Name: tlsCACert},
		{Name: tlsCert},
		{Name: tlsKey},
		{Name: tlsServerName},
		{Name: tlsInsecureSkipVerify},
	}
	for alias := range tlsAliases {
		attrs = append(attrs, hcl.AttributeSchema{Name: alias})
	}

	content, diags := block.Body.Content(&hcl.BodySchema{Attributes: attrs})
	if diags.HasErrors() {
		return nil, fmt.Errorf("tls block content error: %s", diags.Error())
	}

	// Names are resolved in a fixed order so that a file setting two spellings
	// of one setting always reports the same pair, rather than whichever the
	// map happened to yield first.
	names := make([]string, 0, len(content.Attributes))
	for name := range content.Attributes {
		names = append(names, name)
	}
	sort.Strings(names)

	tls := map[string]interface{}{
		tlsEnabled: true,
	}
	writtenAs := map[string]string{}

	for _, name := range names {
		canonical := name
		if target, isAlias := tlsAliases[name]; isAlias {
			canonical = target
		}

		// An author who writes both spellings of the same thing gets told,
		// rather than having one of them silently discarded.
		if previous, clash := writtenAs[canonical]; clash {
			return nil, fmt.Errorf("tls block sets both %q and %q, which are the same setting", previous, name)
		}
		writtenAs[canonical] = name

		val, diags := content.Attributes[name].Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("tls %s error: %s", name, diags.Error())
		}
		tls[canonical] = ctyValueToGo(val)
	}

	return tls, nil
}

// CanonicalTLSAttributes returns the attribute names a tls block accepts.
//
// Exported so that the schema registries can be checked against the parser:
// the gRPC connector once advertised six TLS attributes the parser refused, and
// nothing noticed because connector schemas were never compared with it.
func CanonicalTLSAttributes() []string {
	return []string{tlsEnabled, tlsCACert, tlsCert, tlsKey, tlsServerName, tlsInsecureSkipVerify}
}

// SupersededTLSAttributes maps each historical spelling still accepted by the
// parser onto the canonical name it is folded into. They remain valid to write;
// they should not be offered as completions.
func SupersededTLSAttributes() map[string]string {
	out := make(map[string]string, len(tlsAliases))
	for alias, canonical := range tlsAliases {
		out[alias] = canonical
	}
	return out
}
