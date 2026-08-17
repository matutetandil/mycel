package main

import (
	"strings"
	"testing"
)

// --mock and --no-mock are how somebody runs a service against files instead of
// the real thing: a payment provider from a laptop, a queue nobody wants to
// touch. The runtime has always read them and the parser has always understood
// them; the flags themselves were documented in four places and never
// registered, so following the documentation produced "unknown flag: --mock".

func TestTheMockFlagsExist(t *testing.T) {
	for _, name := range []string{"mock", "no-mock"} {
		if startCmd.Flags().Lookup(name) == nil {
			t.Errorf("--%s is documented and not registered", name)
		}
	}
}

func TestTheMockFlagsReachTheRuntime(t *testing.T) {
	// The value the runtime reads is a comma-separated list, and the
	// documentation shows the flag both repeated and comma-separated.
	for name, args := range map[string][]string{
		"repeated":         {"--mock", "db", "--mock", "external_api"},
		"comma separated":  {"--mock", "db,external_api"},
		"the two together": {"--mock", "db", "--mock", "external_api"},
	} {
		t.Run(name, func(t *testing.T) {
			mockOnly, noMock = nil, nil
			t.Cleanup(func() { mockOnly, noMock = nil, nil })

			if err := startCmd.Flags().Parse(args); err != nil {
				t.Fatalf("parsing %v: %v", args, err)
			}
			if got := strings.Join(mockOnly, ","); got != "db,external_api" {
				t.Errorf("the runtime would be given %q", got)
			}
		})
	}
}

func TestConnectorsCanBeLeftOutOfMocking(t *testing.T) {
	mockOnly, noMock = nil, nil
	t.Cleanup(func() { mockOnly, noMock = nil, nil })

	if err := startCmd.Flags().Parse([]string{"--mock", "all", "--no-mock", "payments"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if strings.Join(mockOnly, ",") != "all" || strings.Join(noMock, ",") != "payments" {
		t.Errorf("mock = %v, no-mock = %v", mockOnly, noMock)
	}
}
