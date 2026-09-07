package ide

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/matutetandil/mycel/v3/internal/parser"
	"github.com/matutetandil/mycel/v3/internal/runtime"
)

// The checks `mycel validate` runs, in the editor.
//
// The editor kept its own list of checks, and the two drifted: a cache key
// written as CEL, `headers` aimed at a database, `key` and `key_from`
// together — each refused by validate, each a squiggle-free file in the
// editor, each failing the moment the service started. And every check added
// to the runtime had to be written a second time here or it was simply
// absent. So the project is fed to the real parser and to runtime.Checks,
// the single list behind validate and start, and each finding lands on the
// block it names.
//
// The editor's own positional checks stay where they are more precise than a
// message — an undefined reference underlines the reference, not the flow —
// and the runtime kinds they duplicate are skipped so nothing is reported
// twice. TestEveryRuntimeCheckReachesTheEditor ties the two lists together.

// coveredPositionally names the runtime check kinds the editor already
// reports with a range of its own (validateBlockRefs), so the project layer
// does not repeat them.
var coveredPositionally = map[string]bool{
	"type reference":        true,
	"aspect flow reference": true,
	"validator reference":   true,
	"connector reference":   true,
}

// namedBlock finds the `kind "name"` a runtime message or parse error opens
// with, which is how the finding is placed on the block at fault.
var namedBlock = regexp.MustCompile(`\b(connector|flow|type|transform|cache|aspect|validator|saga|state_machine) "([^"]+)"`)

// failedFile finds the path a project parse error names.
var failedFile = regexp.MustCompile(`failed to parse (\S+): `)

// projectDiagnostics parses the whole project as the runtime would and runs
// the runtime's checks over it.
//
// The result is kept until the files change: an editor asks for diagnostics
// far more often than it edits, and this pass parses everything.
func (e *Engine) projectDiagnostics() []*Diagnostic {
	e.index.mu.RLock()
	rev := e.index.rev
	e.index.mu.RUnlock()

	e.mu.RLock()
	cached, fresh := e.projectDiags, e.projectDiagsRev == rev && e.projectDiagsValid
	e.mu.RUnlock()
	if fresh {
		return cached
	}

	diags := e.runProjectChecks()

	e.mu.Lock()
	e.projectDiags, e.projectDiagsRev, e.projectDiagsValid = diags, rev, true
	e.mu.Unlock()
	return diags
}

// runProjectChecks does the work projectDiagnostics memoizes.
func (e *Engine) runProjectChecks() []*Diagnostic {
	e.index.mu.RLock()
	files := make(map[string][]byte, len(e.index.Files))
	for path, fi := range e.index.Files {
		// A file still being typed is a syntax error the editor already
		// reports; a whole-project finding about a configuration that does
		// not exist yet would only add noise beside it.
		if len(fi.ParseDiags) > 0 {
			e.index.mu.RUnlock()
			return nil
		}
		files[path] = fi.Source
	}
	e.index.mu.RUnlock()
	if len(files) == 0 {
		return nil
	}

	reg := e.registry
	if reg == nil {
		reg = runtime.NewSchemaRegistry()
	}

	cfg, err := parser.NewHCLParserWithRegistry(reg).ParseSources(context.Background(), files)
	if err != nil {
		return []*Diagnostic{e.locate(err.Error(), files)}
	}

	var diags []*Diagnostic
	for _, check := range runtime.Checks(cfg, reg) {
		if coveredPositionally[check.Kind] {
			continue
		}
		for _, err := range check.Errors {
			diags = append(diags, e.locate(err.Error(), files))
		}
	}
	return diags
}

// locate turns a message into a diagnostic on the block it names, falling
// back to the file it names, and then to the first file of the project.
func (e *Engine) locate(message string, files map[string][]byte) *Diagnostic {
	diag := &Diagnostic{Severity: SeverityError, Message: message}

	if m := namedBlock.FindStringSubmatch(message); m != nil {
		if entity := e.index.lookupEntity(m[1], m[2]); entity != nil {
			diag.File = entity.File
			diag.Range = entity.Range
			return diag
		}
	}

	if m := failedFile.FindStringSubmatch(message); m != nil {
		if _, known := files[m[1]]; known {
			diag.File = m[1]
			diag.Range = topOfFile()
			return diag
		}
	}

	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	diag.File = paths[0]
	diag.Range = topOfFile()
	return diag
}

func topOfFile() Range {
	return Range{Start: Position{Line: 1, Col: 1}, End: Position{Line: 1, Col: 1}}
}

// forFile keeps the diagnostics that belong to one file.
func forFile(diags []*Diagnostic, path string) []*Diagnostic {
	var out []*Diagnostic
	for _, d := range diags {
		if d.File == path || strings.EqualFold(d.File, path) {
			out = append(out, d)
		}
	}
	return out
}
