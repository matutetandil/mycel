package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
)

// A constants block declares values a configuration uses in more than one place.
//
//	constants {
//	  skus_to_skip = ["SKU-1", "SKU-2"]
//	  batch_size   = 500
//	  region       = env("REGION", "us")
//	}
//
// They are literals, resolved once when the configuration is read — a list, a
// map, a number, a string, or an env() call, which is a literal by the time
// anything else runs. Nothing about them is per-message: a value that has to be
// worked out from a request is what a transform is for.
//
// The point is one name that works everywhere. `constants.skus_to_skip` reads the
// same in a query, which HCL evaluates, and in a filter, which CEL evaluates —
// two different machines, and a reader should not have to know which one they
// are writing for. A constant that resolved in `query` and not in `filter`
// would be worse than no constants at all.

// Constants are the values a const block declared, keyed by name.
type Constants struct {
	// Values as HCL sees them, for the evaluation context.
	Values map[string]cty.Value

	// The same values as Go sees them, for the CEL activation.
	Go map[string]interface{}

	// Where each was declared, so a duplicate can name both files.
	DeclaredIn map[string]string
}

func newConstants() *Constants {
	return &Constants{
		Values:     map[string]cty.Value{},
		Go:         map[string]interface{}{},
		DeclaredIn: map[string]string{},
	}
}

// asObject is what goes into the HCL evaluation context under `constants`.
func (c *Constants) asObject() cty.Value {
	if len(c.Values) == 0 {
		return cty.EmptyObjectVal
	}
	return cty.ObjectVal(c.Values)
}

// collectConstants reads every constants block in a file and folds it into what is
// already known.
//
// Declaring the same name twice is refused rather than resolved: two files
// disagreeing about what a constant holds is a mistake somebody wants to hear
// about, and picking one silently is how a configuration comes to depend on
// the order its files are walked in.
func collectConstants(into *Constants, body hcl.Body, ctx *hcl.EvalContext, path string) error {
	content, _, diags := body.PartialContent(&hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{{Type: "constants"}},
	})
	if diags.HasErrors() {
		return fmt.Errorf("constants block error: %s", diags.Error())
	}

	for _, block := range content.Blocks {
		attrs, diags := block.Body.JustAttributes()
		if diags.HasErrors() {
			return fmt.Errorf("constants block error: %s", diags.Error())
		}

		names := make([]string, 0, len(attrs))
		for name := range attrs {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			if where, already := into.DeclaredIn[name]; already {
				return fmt.Errorf(
					"constant %q is declared twice: in %s and in %s — a constant holds one value, so which file wins cannot be left to the order they are read in",
					name, where, path)
			}

			value, diags := attrs[name].Expr.Value(ctx)
			if diags.HasErrors() {
				return fmt.Errorf("constant %q: %s", name, diags.Error())
			}
			if !value.IsWhollyKnown() {
				return fmt.Errorf("constant %q cannot be worked out when the configuration is read; a constant is a literal, an env() call, or something built out of those", name)
			}

			into.Values[name] = value
			into.Go[name] = ctyValueToInterface(value)
			into.DeclaredIn[name] = path
		}
	}

	return nil
}

// constantsIn reads the constants blocks out of every .mycel file under a
// directory, before anything else is parsed.
//
// It is a pass of its own because a constant has to exist before the
// configuration that uses it is evaluated, and a file may use one that a file
// later in the walk declares. Nothing else about the order of files matters in
// Mycel, and this keeps it that way.
func constantsIn(paths []string, ctx *hcl.EvalContext) (*Constants, error) {
	constants := newConstants()

	for _, path := range paths {
		parser := hclparse.NewParser()
		file, diags := parser.ParseHCLFile(path)
		if diags.HasErrors() {
			// Left to the main parse, which reports it against the file.
			continue
		}
		if err := collectConstants(constants, file.Body, ctx, path); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}

	return constants, nil
}

// readConstants runs the constants pass over a configuration directory.
func (p *HCLParser) readConstants(configDir string) error {
	var paths []string
	err := filepath.Walk(configDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "mycel_plugins" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isMycelFile(info.Name()) || isPluginManifest(path) {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(paths)

	constants, err := constantsIn(paths, p.evalCtx)
	if err != nil {
		return err
	}
	p.constants = constants
	p.applyConstants()
	return nil
}

// applyConstants puts them where HCL will look: `constants.<name>`.
func (p *HCLParser) applyConstants() {
	if p.constants == nil {
		return
	}
	if p.evalCtx.Variables == nil {
		p.evalCtx.Variables = map[string]cty.Value{}
	}
	p.evalCtx.Variables["constants"] = p.constants.asObject()
}
