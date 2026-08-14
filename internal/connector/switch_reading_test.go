package connector

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every connector setting can come from the environment, and env() hands back a
// string — so require_https = env("WEBHOOK_HTTPS", "true") arrives spelt out.
// Seven connectors read their switches as native booleans only, which meant a
// spelt-out value fell through to the default. Where the default was the safe
// one that was merely puzzling; where it was not, a webhook kept accepting
// plaintext, a gRPC server kept publishing its schema, and a client the
// operator had secured stayed on plaintext.

func TestASwitchIsReadHoweverItWasWritten(t *testing.T) {
	for name, tc := range map[string]struct {
		value interface{}
		want  bool
	}{
		"a boolean":              {true, true},
		"a boolean that is off":  {false, false},
		"the word true":          {"true", true},
		"the word false":         {"false", false},
		"as written in a shell":  {"TRUE", true},
		"as a digit":             {"1", true},
		"as a digit that is off": {"0", false},
		"with the whitespace an environment variable carries": {" true ", true},
	} {
		t.Run(name, func(t *testing.T) {
			// The default is deliberately the opposite of what is expected, so a
			// value that is not read shows up as the default rather than passing.
			if got := BoolFromProps(map[string]interface{}{"flag": tc.value}, "flag", !tc.want); got != tc.want {
				t.Errorf("BoolFromProps(%#v) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestAValueThatIsNoAnswerLeavesTheDefaultStanding(t *testing.T) {
	// "maybe" is not a switch, and neither is a number of seconds. Guessing
	// would be worse than leaving the connector's own default in place.
	for _, value := range []interface{}{"maybe", "", 3.5, []string{"true"}, nil} {
		if !BoolFromProps(map[string]interface{}{"flag": value}, "flag", true) {
			t.Errorf("a value of %#v overrode the default", value)
		}
		if BoolFromProps(map[string]interface{}{"flag": value}, "flag", false) {
			t.Errorf("a value of %#v overrode the default", value)
		}
	}
	if _, ok := BoolFromPropsStrict(map[string]interface{}{}, "absent"); ok {
		t.Error("a setting nobody wrote was reported as set")
	}
	// Set to false and not set are different answers, which is what lets a
	// connector tell "leave it alone" from "turn it off".
	if v, ok := BoolFromPropsStrict(map[string]interface{}{"flag": "false"}, "flag"); !ok || v {
		t.Errorf("BoolFromPropsStrict = %v, %v", v, ok)
	}
}

func TestNoConnectorReadsItsConfigurationSwitchesTheNarrowWay(t *testing.T) {
	// The defect was copied into seven connectors before anybody noticed, so
	// this is the check that stops the eighth. A connector reading a switch
	// straight out of its properties map with a boolean type assertion cannot
	// see a value that came from the environment.
	//
	// Only configuration is covered: a value taken from a message payload is
	// produced by a transform, where a boolean really is a boolean.
	root := ".."
	var offenders []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		if !strings.Contains(path, "connector/") || !strings.HasSuffix(path, "factory.go") {
			return nil
		}

		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			assert, ok := n.(*ast.TypeAssertExpr)
			if !ok {
				return true
			}
			ident, ok := assert.Type.(*ast.Ident)
			if !ok || ident.Name != "bool" {
				return true
			}
			// A boolean assertion on a map lookup is the shape that goes wrong.
			if _, ok := assert.X.(*ast.IndexExpr); ok {
				offenders = append(offenders, path)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking the connectors: %v", err)
	}

	if len(offenders) > 0 {
		t.Errorf("these factories read a switch as a native boolean only, so a value "+
			"from env() falls through to the default — use BoolFromProps:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}
