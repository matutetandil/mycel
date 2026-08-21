package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matutetandil/mycel/v2/pkg/schema"
)

var addConstantsValues []string

var addConstantsCmd = &cobra.Command{
	Use:   "constants",
	Short: "Add a constants block in constants.mycel",
	Long: `Generate a constants block.

Every attribute is a value the configuration reads as constants.<name>, from an
HCL attribute and from a CEL expression alike. They hold literals and env()
calls — a value worked out from a message is what a transform is for.

Examples:
  mycel add constants --value page_size=500 --value region=us
  mycel add constants --value 'skus_to_skip=["SKU-1","SKU-2"]'
  mycel add constants`,
	Args: cobra.NoArgs,
	RunE: runAddConstants,
}

func runAddConstants(cmd *cobra.Command, args []string) error {
	return writeDeclaration("constants.mycel",
		renderConstants(addConstantsValues, schema.ConstantsSchema()))
}

func renderConstants(values []string, blk schema.Block) string {
	var b strings.Builder

	fmt.Fprintf(&b, "// %s\n", blk.Doc)
	b.WriteString("//\n")
	b.WriteString("// Read once, when the configuration is. A value computed from a message\n")
	b.WriteString("// belongs in a transform.\n")
	b.WriteString("constants {\n")

	if len(values) == 0 {
		b.WriteString("  // TODO — e.g. page_size = 500\n")
		b.WriteString("  //        skus_to_skip = [\"SKU-1\", \"SKU-2\"]\n")
		b.WriteString("  //        region = env(\"REGION\", \"us\")\n")
	}
	for _, value := range values {
		name, literal, found := strings.Cut(value, "=")
		name = strings.TrimSpace(name)
		literal = strings.TrimSpace(literal)
		if !found || name == "" {
			continue
		}
		fmt.Fprintf(&b, "  %s = %s\n", name, asHCLLiteral(literal))
	}

	b.WriteString("}\n")
	return b.String()
}

// asHCLLiteral leaves a value that already looks like HCL alone and quotes
// anything else. `page_size=500` is a number, `region=us` is a string, and
// `skus=["a","b"]` is a list somebody wrote out in full.
func asHCLLiteral(value string) string {
	if value == "" {
		return `""`
	}
	switch value[0] {
	case '[', '{', '"':
		return value
	}
	if value == "true" || value == "false" {
		return value
	}
	if strings.HasPrefix(value, "env(") {
		return value
	}
	if isNumeric(value) {
		return value
	}
	return fmt.Sprintf("%q", value)
}

func isNumeric(value string) bool {
	dots := 0
	for i, c := range value {
		switch {
		case c >= '0' && c <= '9':
		case c == '.':
			dots++
			if dots > 1 {
				return false
			}
		case c == '-' && i == 0:
		default:
			return false
		}
	}
	return value != "" && value != "-"
}

func init() {
	addConstantsCmd.Flags().StringArrayVar(&addConstantsValues, "value", nil,
		"A constant, as name=value (repeatable)")
	addCmd.AddCommand(addConstantsCmd)
}
