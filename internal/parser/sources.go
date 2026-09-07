package parser

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// ParseSources parses a configuration out of file contents held in memory,
// keyed by path, with the same passes Parse runs over a directory: constants
// first, out of every source, then each source, then the whole-configuration
// checks (unique names, resolved references, transaction targets).
//
// It exists for the editor, whose buffers are not what is on disk: the
// checks `mycel validate` runs have to see what the user is typing, not what
// they last saved. The paths are used as the names of the files in every
// diagnostic and as the recorded source of what each declares.
func (p *HCLParser) ParseSources(ctx context.Context, files map[string][]byte) (*Configuration, error) {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	// The constants first, out of every source, before anything that might
	// use one is evaluated — see readConstants.
	constants := newConstants()
	for _, path := range paths {
		file, diags := hclsyntax.ParseConfig(files[path], path, hcl.Pos{Line: 1, Column: 1})
		if diags.HasErrors() {
			// Left to the main parse, which reports it against the file.
			continue
		}
		if err := collectConstants(constants, file.Body, p.evalCtx, path); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	p.constants = constants
	p.applyConstants()

	config := NewConfiguration()
	config.Constants = p.constants.Go
	for _, path := range paths {
		fileConfig, err := p.parseSource(ctx, path, files[path])
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", path, err)
		}
		config.Merge(fileConfig)
	}

	if err := config.ValidateUniqueNames(); err != nil {
		return nil, err
	}
	if err := config.ResolveReferences(); err != nil {
		return nil, err
	}
	if err := config.ValidateTransactionTargets(); err != nil {
		return nil, err
	}
	return config, nil
}
