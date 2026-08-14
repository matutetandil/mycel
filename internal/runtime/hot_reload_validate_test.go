package runtime

import (
	"context"
	"strings"
	"testing"
)

// Before a reload touches a running service the watcher validates the new
// configuration and logs "configuration validation passed". That check only
// parsed the files: a configuration that parses and is then refused — a flow
// missing a parameter its connector requires, a setting the connector does not
// accept — passed the dry run and was turned away seconds later by the switch.
// The line was reassuring and meant nothing.

func TestTheDryRunRefusesWhatTheSwitchWouldRefuse(t *testing.T) {
	for name, broken := range map[string]string{
		"a flow missing what its connector requires": strings.Replace(workingConfig,
			`    operation = "GET /items"`, "", 1),
		"a setting the connector does not accept": strings.Replace(workingConfig,
			`connector "api" {
  type = "rest"`,
			`connector "api" {
  type = "rest"
  format = "yml"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			r, path := reloadableRuntime(t, workingConfig)
			rewrite(t, path, broken)

			if err := r.hotReloadValidate(context.Background()); err == nil {
				t.Fatal("the dry run passed a configuration the switch would refuse")
			}

			// And having said no, it left the running service alone.
			if len(r.flows.List()) != 1 {
				t.Errorf("the running service has %d flows after a failed dry run",
					len(r.flows.List()))
			}
		})
	}
}

func TestTheDryRunPassesAConfigurationThatWouldBeInstalled(t *testing.T) {
	r, path := reloadableRuntime(t, workingConfig)
	rewrite(t, path, strings.Replace(workingConfig, `flow "list_items"`, `flow "list_products"`, 1))

	if err := r.hotReloadValidate(context.Background()); err != nil {
		t.Fatalf("a configuration that installs cleanly was refused by the dry run: %v", err)
	}

	// The dry run is a dry run: nothing changed until the switch.
	if _, running := r.flows.Get("list_items"); !running {
		t.Error("the dry run replaced the running configuration")
	}
}

func TestFilesThatDoNotParseAreReportedByTheDryRun(t *testing.T) {
	r, path := reloadableRuntime(t, workingConfig)
	rewrite(t, path, "flow \"broken\" {\n  from {\n")

	if err := r.hotReloadValidate(context.Background()); err == nil {
		t.Error("a file that does not parse passed the dry run")
	}
}
