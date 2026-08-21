package main

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// What `mycel add constants` writes has to parse.
//
// The rule this repository keeps relearning: something the tool generates that
// the parser then refuses is worse than no generator at all, because the
// person following it has no reason to doubt the output.
func TestGeneratedConstantsParse(t *testing.T) {
	for _, c := range []struct {
		name   string
		values []string
	}{
		{"nothing but the placeholder", nil},
		{"a number and a string", []string{"page_size=500", "region=us"}},
		{"a list written out", []string{`skus_to_skip=["SKU-1", "SKU-2"]`}},
		{"a map", []string{`thresholds={ warn = 100, alert = 500 }`}},
		{"a boolean", []string{"debug=false"}},
		{"a value from the environment", []string{`database_url=env("DATABASE_URL", "")`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			body := renderConstants(c.values, schema.ConstantsSchema())
			if err := parseGenerated(t, body); err != nil {
				t.Errorf("what was generated does not parse: %v\n%s", err, body)
			}
		})
	}
}

// A value is written as the literal it is: a number stays a number, a string
// is quoted, and something already written as HCL is left alone.
func TestGeneratedConstantsKeepTheirTypes(t *testing.T) {
	body := renderConstants([]string{
		"page_size=500",
		"ratio=1.5",
		"region=us",
		"debug=true",
		`skus=["a"]`,
	}, schema.ConstantsSchema())

	for _, want := range []string{
		"page_size = 500",
		"ratio = 1.5",
		`region = "us"`,
		"debug = true",
		`skus = ["a"]`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("generated block does not contain %q:\n%s", want, body)
		}
	}
}

// The comment comes from the schema's own wording, so it cannot contradict it.
func TestGeneratedConstantsCommentComesFromTheSchema(t *testing.T) {
	blk := schema.ConstantsSchema()
	if blk.Doc == "" {
		t.Fatal("the schema documents nothing, so this test is checking nothing")
	}
	if !strings.Contains(renderConstants(nil, blk), blk.Doc) {
		t.Error("generated comment does not use the schema's wording")
	}
}
