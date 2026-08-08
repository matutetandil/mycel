package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// scaffold is the layout `mycel init` writes.
//
// Mycel does not care how config is organised — it reads every .mycel file
// under the directory and merges them, so a single file behaves identically.
// What you start with is what you keep, though, so the scaffold hands people
// the layout that stays readable: one declaration per file, grouped by kind.
// This is the same shape as examples/basic and as the production services
// Mycel was built against.
//
// Keep every generated file runnable. A scaffold that needs a schema loaded or
// a broker running before `mycel start` says anything useful teaches the wrong
// first lesson.
var scaffold = []scaffoldFile{
	{
		path: "config.mycel",
		body: `// Service identity. One per project.
service {
  name    = "%s"
  version = "0.1.0"
}
`,
		formatWithName: true,
	},
	{
		path: "connectors/api.mycel",
		body: `// A connector is a system Mycel talks to — a server it exposes, or a
// database, queue or API it reaches. One file per connector.
connector "api" {
  type = "rest"
  port = 3000
}
`,
	},
	{
		path: "flows/status.mycel",
		body: `// A flow is one path data takes: where it comes from, what happens to
// it, where it goes. One file per flow.
//
// This one has no destination, so it answers directly with a response
// block. Add a to { } to write somewhere instead.
flow "status" {
  from {
    connector = "api"
    operation = "GET /status"
  }

  response {
    service = "'%s'"
    status  = "'ok'"
  }
}
`,
		formatWithName: true,
	},
	{
		path: ".gitignore",
		body: `# Local secrets — never commit
.env

# SQLite databases
*.db
`,
	},
	{
		path: ".env.example",
		body: `# Copy to .env and fill in. Mycel loads .env automatically and never
# overrides variables already set in the environment.
#
# MYCEL_ENV=development
# MYCEL_LOG_LEVEL=debug
`,
	},
}

type scaffoldFile struct {
	path string
	body string
	// formatWithName substitutes the service name into the body.
	formatWithName bool
}

var initCmd = &cobra.Command{
	Use:   "init [name]",
	Short: "Scaffold a new Mycel project",
	Long: `Create a new Mycel project with the recommended layout.

Mycel reads every .mycel file under the config directory and merges them, so
file and directory names carry no meaning for the runtime — a single file works
identically. The layout this writes is the one that stays maintainable as a
service grows: one declaration per file, grouped by kind.

The generated service runs as-is: start it and GET /status responds.

Examples:
  # Create ./my-service and scaffold into it
  mycel init my-service

  # Scaffold into the current directory
  mycel init`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	dir := "."
	name := ""
	if len(args) == 1 {
		dir = args[0]
		name = filepath.Base(dir)
	}
	if name == "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("resolving %s: %w", dir, err)
		}
		name = filepath.Base(abs)
	}
	name = serviceName(name)

	// Refuse to write over anything. Scaffolding is a starting move; silently
	// replacing a file someone already edited is never what they meant.
	var clashes []string
	for _, f := range scaffold {
		if _, err := os.Stat(filepath.Join(dir, f.path)); err == nil {
			clashes = append(clashes, f.path)
		}
	}
	if len(clashes) > 0 {
		sort.Strings(clashes)
		return fmt.Errorf("refusing to overwrite existing file(s) in %s: %s",
			dir, strings.Join(clashes, ", "))
	}

	for _, f := range scaffold {
		full := filepath.Join(dir, f.path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", filepath.Dir(full), err)
		}
		body := f.body
		if f.formatWithName {
			body = fmt.Sprintf(body, name)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", full, err)
		}
		fmt.Printf("  created %s\n", filepath.Join(dir, f.path))
	}

	fmt.Printf("\n✓ Scaffolded %q\n\n", name)
	fmt.Printf("Next:\n")
	if dir != "." {
		fmt.Printf("  cd %s\n", dir)
	}
	fmt.Printf("  mycel validate           # check the config parses\n")
	fmt.Printf("  mycel start              # run it\n")
	fmt.Printf("  curl localhost:3000/status\n\n")
	fmt.Printf("Add pieces as you go — one file each keeps it readable:\n")
	fmt.Printf("  connectors/<name>.mycel  a system to talk to\n")
	fmt.Printf("  flows/<name>.mycel       a path data takes\n")

	return nil
}

// serviceName turns a directory name into something usable as a service name,
// since that value ends up in logs, metrics labels and health output.
func serviceName(dir string) string {
	name := strings.TrimSpace(dir)
	name = strings.ReplaceAll(name, " ", "-")
	if name == "" || name == "." || name == "/" {
		return "my-service"
	}
	return name
}
