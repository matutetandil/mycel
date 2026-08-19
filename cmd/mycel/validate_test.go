package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `mycel validate` is the command someone runs before deploying, and it is the
// only thing standing between a configuration and a service that starts and
// then does not work. What it has to catch is not bad syntax — the parser
// catches that — but a file that parses and cannot run.

func project(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}
	return dir
}

const validService = `
service {
  name    = "orders"
  version = "1.0.0"
}

connector "api" {
  type = "rest"
  port = 18391
}

connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}

type "order" {
  id    = string()
  total = number()
}

flow "list_orders" {
  from {
    connector = "api"
    operation = "GET /orders"
  }
  to {
    connector = "db"
    target    = "orders"
  }
}
`

func TestAWorkingConfigurationValidates(t *testing.T) {
	withConfigDir(t, project(t, map[string]string{"config.mycel": validService}))
	if err := runValidate(nil, nil); err != nil {
		t.Fatalf("a working configuration was refused: %v", err)
	}
}

func TestValidateRefusesWhatWouldNotStart(t *testing.T) {
	// Each of these parses. Every one of them used to be something you found
	// out about from a running service rather than from this command.
	for name, files := range map[string]map[string]string{
		"a source missing what its connector requires": {"config.mycel": `
connector "api" {
  type = "rest"
  port = 18391
}
flow "list_orders" {
  from {
    connector = "api"
  }
  to {
    connector = "api"
  }
}
`},
		"an aspect with no action at all": {"config.mycel": validService + `
aspect "audit" {
  when = "after"
  on   = ["list_*"]
}
`},
	} {
		t.Run(name, func(t *testing.T) {
			withConfigDir(t, project(t, files))
			err := runValidate(nil, nil)
			if err == nil {
				t.Fatal("a configuration that cannot run was reported as valid")
			}
			if !strings.Contains(err.Error(), "validation failed") {
				t.Errorf("error = %q", err)
			}
		})
	}
}

func TestValidateRefusesAFileThatDoesNotParse(t *testing.T) {
	withConfigDir(t, project(t, map[string]string{"config.mycel": `
connector "api" {
  type = "rest"
`}))
	if err := runValidate(nil, nil); err == nil {
		t.Fatal("a file that does not parse was reported as valid")
	}
}

func TestValidateRefusesAnAttributeNothingAccepts(t *testing.T) {
	// The check that keeps documentation and configuration honest: a name that
	// looks plausible and is not one.
	withConfigDir(t, project(t, map[string]string{"config.mycel": `
connector "api" {
  type       = "rest"
  port       = 18391
  timeoutish = "30s"
}
`}))
	if err := runValidate(nil, nil); err == nil {
		t.Fatal("an attribute nothing accepts was reported as valid")
	}
}

func TestValidateReadsEveryFileInTheDirectory(t *testing.T) {
	// The layout the scaffold produces is one declaration per file, so a
	// command that read only the top level would validate a fraction of it.
	withConfigDir(t, project(t, map[string]string{
		"config.mycel":            `service { name = "orders" }`,
		"connectors/api.mycel":    `connector "api" { type = "rest" port = 18391 }`,
		"flows/list.mycel":        "flow \"list_orders\" {\n  from {\n    connector = \"api\"\n    operation = \"GET /orders\"\n  }\n  to {\n    connector = \"api\"\n  }\n}",
		"connectors/broken.mycel": `connector "db" { type = "database" driver = "sqlite" }`,
	}))

	// The nested file is missing a required attribute, so this must fail —
	// which proves it was read.
	if err := runValidate(nil, nil); err == nil {
		t.Fatal("a file in a subdirectory was not validated")
	}
}

func TestValidateOnADirectoryThatIsNotThere(t *testing.T) {
	withConfigDir(t, filepath.Join(t.TempDir(), "absent"))
	if err := runValidate(nil, nil); err == nil {
		t.Error("a configuration directory that does not exist was reported as valid")
	}
}

// The export commands turn a configuration into the document another team
// consumes. A specification that is subtly wrong is worse than none, because
// it is generated from something that is right.

func TestOpenAPIIsExportedFromTheFlows(t *testing.T) {
	withConfigDir(t, project(t, map[string]string{"config.mycel": validService}))

	output := captureStdout(t, func() {
		if err := runExportOpenAPI(nil, nil); err != nil {
			t.Fatalf("runExportOpenAPI: %v", err)
		}
	})

	for _, want := range []string{"openapi:", "/orders", "get:", "orders"} {
		if !strings.Contains(output, want) {
			t.Errorf("the specification is missing %q:\n%s", want, output)
		}
	}
}

func TestAsyncAPIIsExportedForEventFlows(t *testing.T) {
	withConfigDir(t, project(t, map[string]string{"config.mycel": `
connector "queue" {
  type   = "mq"
  driver = "rabbitmq"
  url    = "amqp://localhost:5672"
}
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}
flow "consume_orders" {
  from {
    connector = "queue"
    queue     = "orders"
  }
  to {
    connector = "db"
    target    = "orders"
  }
}
`}))

	output := captureStdout(t, func() {
		if err := runExportAsyncAPI(nil, nil); err != nil {
			t.Fatalf("runExportAsyncAPI: %v", err)
		}
	})

	if !strings.Contains(output, "asyncapi:") {
		t.Errorf("the document does not declare itself:\n%s", output)
	}
	if !strings.Contains(output, "orders") {
		t.Errorf("the queue the flow consumes is not in the document:\n%s", output)
	}
}

func TestExportingFromADirectoryThatIsNotThere(t *testing.T) {
	withConfigDir(t, filepath.Join(t.TempDir(), "absent"))
	for name, run := range map[string]func() error{
		"openapi":        func() error { return runExportOpenAPI(nil, nil) },
		"asyncapi":       func() error { return runExportAsyncAPI(nil, nil) },
		"graphql schema": func() error { return runExportGraphQLSchema(nil, nil) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := run(); err == nil {
				t.Error("a configuration that does not exist produced a document")
			}
		})
	}
}

// captureStdout collects what a command prints, since these write the document
// to standard output for redirecting into a file.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	previous := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	os.Stdout = w

	done := make(chan string)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				sb.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		done <- sb.String()
	}()

	fn()
	w.Close()
	os.Stdout = previous
	return <-done
}

// Everything that is wrong, in one run.
//
// The checks grew one at a time, each returning as soon as it found something,
// so a configuration wrong in five ways reported the first kind, was fixed,
// and reported the next on the following run. That is the experience each
// check avoids inside itself — every duration at once, every duplicate at once
// — and they recreated it between them.
func TestEveryKindOfProblemIsReportedInOneRun(t *testing.T) {
	withConfigDir(t, project(t, map[string]string{"config.mycel": `
service {
  name = "orders"
}

connector "api" {
  type = "rest"
  port = 18392
}

flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }

  cache {
    storage = "a_cache_nobody_declared"
    ttl     = "5 minutes"
  }

  step "one" {
    connector = "api"
    on_error  = "ignore"
  }

  step "one" {
    connector = "api"
  }

  validate {
    input = "no_such_type"
  }
}
`}))

	err := runValidate(nil, nil)
	if err == nil {
		t.Fatal("a configuration wrong in five ways was accepted")
	}

	// The summary names each kind, so somebody reading only the last line
	// knows how much is ahead of them.
	for _, kind := range []string{
		"duration", "step", "type reference", "duplicate name", "connector reference",
	} {
		if !strings.Contains(err.Error(), kind) {
			t.Errorf("the summary does not mention %s errors: %v", kind, err)
		}
	}
}

func TestOneKindOfProblemStillReadsAsOne(t *testing.T) {
	// The ordinary case: nothing about reporting everything should make a
	// single mistake harder to read.
	withConfigDir(t, project(t, map[string]string{"config.mycel": `
service {
  name = "orders"
}

connector "api" {
  type = "rest"
  port = 18393
}

connector "memcache" {
  type   = "cache"
  driver = "memory"
}

flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }
  cache {
    storage = "memcache"
    ttl     = "5 minutes"
  }
}
`}))

	err := runValidate(nil, nil)
	if err == nil {
		t.Fatal("accepted")
	}
	if !strings.Contains(err.Error(), "1 duration error(s)") {
		t.Errorf("the summary does not read as one problem: %v", err)
	}
	// And nothing else is claimed alongside it.
	if strings.Contains(err.Error(), ",") {
		t.Errorf("a single problem was summarised as several: %v", err)
	}
}
