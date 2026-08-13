package main

import (
	"strings"
	"testing"
)

// `mycel add type --fields "email:string:email,age:number"` is the one place the
// CLI parses a language of its own, so it is the one place it can write a file
// that does not parse — or quietly drop a field somebody asked for.

func TestParseFieldSpecs(t *testing.T) {
	fields, err := parseFieldSpecs("email:string:email, age:number, active:boolean")
	if err != nil {
		t.Fatalf("parseFieldSpecs: %v", err)
	}
	if len(fields) != 3 {
		t.Fatalf("got %d fields, want 3", len(fields))
	}
	if fields[0].name != "email" || fields[0].kind != "string" || fields[0].format != "email" {
		t.Errorf("first field = %+v", fields[0])
	}
	if fields[1].kind != "number" || fields[1].format != "" {
		t.Errorf("second field = %+v", fields[1])
	}
}

func TestAFormatWrittenWhereATypeGoesIsUnderstood(t *testing.T) {
	// id:uuid is the shorthand people reach for. A uuid is a format of a
	// string rather than a type of its own, and refusing it over that would be
	// pedantry.
	fields, err := parseFieldSpecs("id:uuid")
	if err != nil {
		t.Fatalf("parseFieldSpecs: %v", err)
	}
	if len(fields) != 1 || fields[0].kind != "string" || fields[0].format != "uuid" {
		t.Errorf("field = %+v", fields[0])
	}
}

func TestParseFieldSpecsRejectsWhatItCannotRender(t *testing.T) {
	// Each of these would otherwise become a type block that parses as HCL and
	// fails later, when the name is no longer in hand.
	for _, spec := range []string{
		"email:widget",        // not a type and not a format
		"email",               // no type at all
		":string",             // no name
		"email:string:postal", // not a format anyone recognises
	} {
		if _, err := parseFieldSpecs(spec); err == nil {
			t.Errorf("parseFieldSpecs(%q) was accepted", spec)
		}
	}
}

func TestARefusalNamesTheAlternatives(t *testing.T) {
	// The list is what turns a guess into a choice.
	_, err := parseFieldSpecs("email:widget")
	if err == nil {
		t.Fatal("an unknown type was accepted")
	}
	if !strings.Contains(err.Error(), "string") || !strings.Contains(err.Error(), "widget") {
		t.Errorf("error = %q, want it to name the field and the types that exist", err)
	}
}

func TestParseFieldSpecsOnNothing(t *testing.T) {
	// A type with no fields is a legitimate starting point, not an error.
	for _, spec := range []string{"", "   "} {
		fields, err := parseFieldSpecs(spec)
		if err != nil {
			t.Errorf("parseFieldSpecs(%q): %v", spec, err)
		}
		if len(fields) != 0 {
			t.Errorf("parseFieldSpecs(%q) = %v", spec, fields)
		}
	}
}

func TestRenderTypeUsesTheConstraintFormTheParserTakes(t *testing.T) {
	fields, err := parseFieldSpecs("id:uuid, age:number, active:boolean")
	if err != nil {
		t.Fatalf("parseFieldSpecs: %v", err)
	}

	rendered := renderType("customer", fields)
	if !strings.Contains(rendered, `type "customer"`) {
		t.Errorf("rendered = %s", rendered)
	}
	// string({ format = "uuid" }), not string { format = "uuid" } — the second
	// is the spelling the documentation carried for a long time and which the
	// parser never accepted.
	if strings.Contains(rendered, "string {") {
		t.Errorf("rendered the constraint form the parser rejects:\n%s", rendered)
	}
	for _, field := range []string{"id", "age", "active"} {
		if !strings.Contains(rendered, field) {
			t.Errorf("the rendered type is missing %q:\n%s", field, rendered)
		}
	}
}
