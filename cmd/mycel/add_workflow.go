package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// The generators for the blocks that describe work rather than wiring: sagas,
// state machines, validators and named transforms.
//
// Same rule as the connector and flow generators — the shape comes from
// pkg/schema, never from a literal here. A template drifts the day the parser
// changes; a schema cannot, because the parity test parses what it describes.

var (
	addSagaFrom        string
	addSagaSteps       string
	addStates          string
	addInitialState    string
	addValidatorType   string
	addPattern         string
	addExpr            string
	addWASM            string
	addTransformFields string
)

var addSagaCmd = &cobra.Command{
	Use:   "saga <name>",
	Short: "Add a saga in sagas/<name>.mycel",
	Long: `Generate a saga: a sequence of steps, each with the call that undoes it.

A saga is for work that spans services and cannot be one database transaction.
When a step fails, the steps already done are compensated in reverse order.

Examples:
  mycel add saga place_order --steps reserve_stock,charge_card,ship
  mycel add saga place_order --from orders_queue --steps reserve,charge`,
	Args: cobra.ExactArgs(1),
	RunE: runAddSaga,
}

func runAddSaga(cmd *cobra.Command, args []string) error {
	name := args[0]

	if err := ensureNameIsFree(name, "saga"); err != nil {
		return err
	}

	// A saga with no from block is skipped at registration and never runs.
	// There is no other way to trigger one — nothing invokes a saga from a
	// flow — so generating one without a source produces a file that loads,
	// validates, and does nothing.
	if addSagaFrom == "" {
		return fmt.Errorf("a saga needs something to trigger it: pass --from <connector>")
	}
	if err := ensureConnectorExists(addSagaFrom); err != nil {
		return err
	}

	return writeDeclaration(declarationPath("saga", name),
		renderSaga(name, addSagaFrom, splitList(addSagaSteps), schema.SagaSchema()))
}

func renderSaga(name, from string, steps []string, blk schema.Block) string {
	var b strings.Builder

	fmt.Fprintf(&b, "// %s\n", blk.Doc)
	fmt.Fprintf(&b, "saga %q {\n", name)

	// The from block is what triggers the saga, and a saga without one never
	// registers, so it is always written.
	b.WriteString("  from {\n")
	fmt.Fprintf(&b, "    connector = %q\n", from)
	fmt.Fprintf(&b, "    operation = \"\" // TODO — %s\n", docFor(childOf(blk, "from"), "operation"))
	b.WriteString("  }\n\n")

	// The parser rejects a saga with no steps, and a step with no action. The
	// default lives here rather than in the caller so that no way of reaching
	// this function can produce a saga that fails to parse. Two named steps
	// also make the compensation order visible, which is the reason to reach
	// for a saga at all.
	if len(steps) == 0 {
		steps = []string{"first_step", "second_step"}
	}

	step := childOf(blk, "step")
	for i, s := range steps {
		fmt.Fprintf(&b, "  step %q {\n", s)
		b.WriteString("    action {\n")
		b.WriteString("      connector = \"\" // TODO — a connector declared in connectors/\n")
		b.WriteString("      operation = \"\"\n")
		b.WriteString("    }\n")

		// The compensate block is the reason a saga exists, so it is generated
		// rather than mentioned. The last step has nothing after it to fail,
		// but a later step added below it would have — leaving it out invites
		// exactly that mistake.
		b.WriteString("\n    // Undoes this step when a later one fails.\n")
		b.WriteString("    compensate {\n")
		b.WriteString("      connector = \"\" // TODO\n")
		b.WriteString("      operation = \"\"\n")
		b.WriteString("    }\n")

		if i == 0 {
			fmt.Fprintf(&b, "\n    // Optional: %s\n", attrHints(step, "timeout", "on_error", "delay", "await"))
		}
		b.WriteString("  }\n\n")
	}

	b.WriteString("  // Runs once every step has succeeded.\n")
	b.WriteString("  // on_complete { connector = \"notifications\" }\n")
	b.WriteString("\n  // Runs after compensation has unwound the saga.\n")
	b.WriteString("  // on_failure { connector = \"notifications\" }\n")
	b.WriteString("}\n")

	return b.String()
}

var addStateMachineCmd = &cobra.Command{
	Use:     "state-machine <name>",
	Aliases: []string{"state_machine"},
	Short:   "Add a state machine in state_machines/<name>.mycel",
	Long: `Generate a state machine: the states an entity may be in, and the
events that move it between them.

A transition that is not declared cannot happen, which is the point — it is how
an order gets refunded only after it was paid.

Examples:
  mycel add state-machine order --states pending,paid,shipped,delivered
  mycel add state-machine order --states draft,live --initial draft`,
	Args: cobra.ExactArgs(1),
	RunE: runAddStateMachine,
}

func runAddStateMachine(cmd *cobra.Command, args []string) error {
	name := args[0]

	if err := ensureNameIsFree(name, "state_machine"); err != nil {
		return err
	}

	states := splitList(addStates)
	if len(states) == 0 {
		states = []string{"pending", "active", "done"}
	}

	initial := addInitialState
	if initial == "" {
		initial = states[0]
	}
	if !contains(states, initial) {
		return fmt.Errorf("initial state %q is not in --states (%s)", initial, strings.Join(states, ", "))
	}

	return writeDeclaration(declarationPath("state_machine", name),
		renderStateMachine(name, initial, states, schema.StateMachineSchema()))
}

func renderStateMachine(name, initial string, states []string, blk schema.Block) string {
	var b strings.Builder

	fmt.Fprintf(&b, "// %s\n", blk.Doc)
	fmt.Fprintf(&b, "state_machine %q {\n", name)
	fmt.Fprintf(&b, "  initial = %q\n", initial)

	for i, s := range states {
		b.WriteString("\n")
		fmt.Fprintf(&b, "  state %q {\n", s)

		// Chain each state to the next, so the generated machine is a working
		// one. A machine whose states are all terminal parses and then refuses
		// every transition, which reads as a runtime bug rather than a
		// half-written file.
		if i+1 < len(states) {
			next := states[i+1]
			fmt.Fprintf(&b, "    on %q {\n", "to_"+next)
			fmt.Fprintf(&b, "      transition_to = %q\n", next)
			if i == 0 {
				b.WriteString("      // guard = \"input.amount > 0\" // CEL; refuses the transition when false\n")
			}
			b.WriteString("\n      // action {\n")
			b.WriteString("      //   connector = \"notifications\"\n")
			b.WriteString("      // }\n")
			b.WriteString("    }\n")
		} else {
			b.WriteString("    final = true\n")
		}
		b.WriteString("  }\n")
	}

	b.WriteString("}\n")
	return b.String()
}

var addValidatorCmd = &cobra.Command{
	Use:   "validator <name>",
	Short: "Add a validator in validators/<name>.mycel",
	Long: `Generate a custom validation rule.

A validator is referenced from a type field or a flow's validate block. It is a
regular expression, a CEL expression, or a WASM module for anything those two
cannot express.

Examples:
  mycel add validator nz_phone --type regex --pattern '^\+64[0-9]{8,9}$'
  mycel add validator adult --type cel --expr "input.age >= 18"
  mycel add validator tax_id --type wasm --wasm ./validators/tax_id.wasm`,
	Args: cobra.ExactArgs(1),
	RunE: runAddValidator,
}

func runAddValidator(cmd *cobra.Command, args []string) error {
	name := args[0]

	if err := ensureNameIsFree(name, "validator"); err != nil {
		return err
	}

	blk := schema.ValidatorSchema()
	vType := addValidatorType
	if vType == "" {
		vType = "regex"
	}
	if err := validateAgainstSchema(blk, "type", vType); err != nil {
		return err
	}

	// Each type has one attribute it cannot work without, and the parser
	// rejects an empty one by name. Generating a TODO there would produce a
	// file that fails to parse — the one thing a skeleton must not do — so the
	// flag is required, and the message says which.
	carrier := map[string]struct {
		flag  string
		value string
	}{
		"regex": {"--pattern", addPattern},
		"cel":   {"--expr", addExpr},
		"wasm":  {"--wasm", addWASM},
	}[vType]
	if carrier.value == "" {
		return fmt.Errorf("a %s validator needs its rule: pass %s", vType, carrier.flag)
	}

	return writeDeclaration(declarationPath("validator", name),
		renderValidator(name, vType, carrier.value, blk))
}

func renderValidator(name, vType, carrier string, blk schema.Block) string {
	var b strings.Builder

	fmt.Fprintf(&b, "// %s\n", blk.Doc)
	fmt.Fprintf(&b, "validator %q {\n", name)
	fmt.Fprintf(&b, "  type = %q\n", vType)

	attr := map[string]string{"regex": "pattern", "cel": "expr", "wasm": "wasm"}[vType]
	fmt.Fprintf(&b, "  %s = %q // %s\n", attr, carrier, docFor(blk, attr))

	if vType == "wasm" {
		fmt.Fprintf(&b, "  // entrypoint = \"validate\" // %s\n", docFor(blk, "entrypoint"))
	}

	fmt.Fprintf(&b, "\n  message = \"\" // %s\n", docFor(blk, "message"))
	b.WriteString("}\n")

	return b.String()
}

var addTransformCmd = &cobra.Command{
	Use:   "transform <name>",
	Short: "Add a named transform in transforms/<name>.mycel",
	Long: `Generate a reusable transform.

Every attribute is an output field and a CEL expression producing it. Naming it
here lets several flows share one shaping rule via use = "transform.<name>".

Examples:
  mycel add transform normalize_user --fields id,email,created_at
  mycel add transform order_summary`,
	Args: cobra.ExactArgs(1),
	RunE: runAddTransform,
}

func runAddTransform(cmd *cobra.Command, args []string) error {
	name := args[0]

	if err := ensureNameIsFree(name, "transform"); err != nil {
		return err
	}

	return writeDeclaration(declarationPath("transform", name),
		renderTransform(name, splitList(addTransformFields), schema.TransformSchema()))
}

func renderTransform(name string, fields []string, blk schema.Block) string {
	var b strings.Builder

	fmt.Fprintf(&b, "// %s\n", blk.Doc)
	fmt.Fprintf(&b, "// Each attribute is an output field; the value is CEL over `input`.\n")
	fmt.Fprintf(&b, "transform %q {\n", name)

	if len(fields) == 0 {
		b.WriteString("  // TODO — e.g. email = \"lower(input.email)\"\n")
	}
	for _, f := range fields {
		fmt.Fprintf(&b, "  %s = %q\n", f, "input."+f)
	}

	// The enrich block is worth naming here: it is the reason to make a
	// transform reusable rather than writing the mappings inline.
	b.WriteString("\n  // Fetch data from another connector before mapping, readable as enriched.<name>:\n")
	b.WriteString("  // enrich \"pricing\" {\n")
	b.WriteString("  //   connector = \"pricing_api\"\n")
	b.WriteString("  //   params { product_id = \"input.id\" }\n")
	b.WriteString("  // }\n")
	b.WriteString("}\n")

	return b.String()
}

// --- shared helpers ---

// childOf returns a named child block, or the zero Block when absent.
func childOf(blk schema.Block, typ string) schema.Block {
	for _, c := range blk.Children {
		if c.Type == typ {
			return c
		}
	}
	return schema.Block{}
}

// docFor returns an attribute's documentation from the schema, so generated
// comments say what the schema says and cannot contradict it.
func docFor(blk schema.Block, attr string) string {
	for _, a := range blk.Attrs {
		if a.Name == attr {
			return a.Doc
		}
	}
	return ""
}

// attrHints renders named attributes as a one-line hint, skipping any the
// schema does not declare.
func attrHints(blk schema.Block, names ...string) string {
	var out []string
	for _, n := range names {
		if doc := docFor(blk, n); doc != "" {
			out = append(out, n)
		}
	}
	return strings.Join(out, ", ")
}

func splitList(spec string) []string {
	var out []string
	for _, s := range strings.Split(spec, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func init() {
	addSagaCmd.Flags().StringVar(&addSagaFrom, "from", "", "Connector that starts the saga")
	addSagaCmd.Flags().StringVar(&addSagaSteps, "steps", "", "Comma-separated step names, in order")

	addStateMachineCmd.Flags().StringVar(&addStates, "states", "", "Comma-separated states, in lifecycle order")
	addStateMachineCmd.Flags().StringVar(&addInitialState, "initial", "", "Starting state (default: the first)")

	addValidatorCmd.Flags().StringVar(&addValidatorType, "type", "", "regex, cel or wasm (default regex)")
	addValidatorCmd.Flags().StringVar(&addPattern, "pattern", "", "Regular expression (type = regex)")
	addValidatorCmd.Flags().StringVar(&addExpr, "expr", "", "CEL expression (type = cel)")
	addValidatorCmd.Flags().StringVar(&addWASM, "wasm", "", "Path to the .wasm module (type = wasm)")

	addTransformCmd.Flags().StringVar(&addTransformFields, "fields", "", "Comma-separated output field names")

	addCmd.AddCommand(addSagaCmd, addStateMachineCmd, addValidatorCmd, addTransformCmd)
}
