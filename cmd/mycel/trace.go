package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/matutetandil/mycel/internal/runtime"
	"github.com/matutetandil/mycel/internal/trace"
)

var traceCmd = &cobra.Command{
	Use:   "trace <flow-name>",
	Short: "Execute a flow and show step-by-step data trace",
	Long: `Execute a single flow and display a detailed trace of the data pipeline.

Shows what happens at each stage: input → sanitize → validate → transform → read/write.
Useful for debugging flows without adding log statements to your HCL configuration.

With --dry-run, write operations (INSERT, UPDATE, DELETE, publish) are simulated
and shown without actually executing — safe for production data sources.

Examples:
  # Trace a read flow
  mycel trace get_users --config ./my-service

  # Trace a write flow with input data
  mycel trace create_user --input '{"email":"test@example.com","name":"Test"}'

  # Trace with path parameters
  mycel trace get_user --params id=123

  # Dry-run: show what would be written without executing
  mycel trace create_user --input '{"email":"test@x.com"}' --dry-run

  # List all available flows
  mycel trace --list`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTrace,
}

var (
	traceInput  string
	traceParams string
	traceDryRun bool
	traceList   bool
)

func init() {
	traceCmd.Flags().StringVar(&traceInput, "input", "", "JSON input data for the flow")
	traceCmd.Flags().StringVar(&traceParams, "params", "", "Key=value parameters (comma-separated, e.g., id=123,status=active)")
	traceCmd.Flags().BoolVar(&traceDryRun, "dry-run", false, "Simulate write operations without executing them")
	traceCmd.Flags().BoolVar(&traceList, "list", false, "List all available flows")

	rootCmd.AddCommand(traceCmd)
}

func runTrace(cmd *cobra.Command, args []string) error {
	// Load .env
	loadDotEnv()

	// Setup logger (quiet for trace output)
	logger := createLogger()

	// Resolve environment
	env := resolveEnvironment()

	// Create runtime (partial init — no server start)
	rt, err := runtime.New(runtime.Options{
		ConfigDir:   configDir,
		Environment: env,
		Logger:      logger,
	})
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}

	ctx := context.Background()

	// Initialize connectors and flows (no servers)
	if err := rt.InitForTrace(ctx); err != nil {
		return fmt.Errorf("failed to initialize for trace: %w", err)
	}
	defer rt.Shutdown()

	// List mode
	if traceList {
		flows := rt.ListFlows()
		if len(flows) == 0 {
			fmt.Println("No flows registered.")
			return nil
		}
		fmt.Printf("Available flows (%d):\n", len(flows))
		for _, name := range flows {
			fmt.Printf("  - %s\n", name)
		}
		return nil
	}

	// Need a flow name
	if len(args) == 0 {
		return fmt.Errorf("flow name required. Use --list to see available flows")
	}
	flowName := args[0]

	// Look up the flow
	handler, ok := rt.GetFlow(flowName)
	if !ok {
		flows := rt.ListFlows()
		return fmt.Errorf("flow %q not found. Available flows: %s", flowName, strings.Join(flows, ", "))
	}

	// Build input
	input := make(map[string]interface{})

	// Parse JSON input
	if traceInput != "" {
		if err := json.Unmarshal([]byte(traceInput), &input); err != nil {
			return fmt.Errorf("invalid --input JSON: %w", err)
		}
	}

	// Parse key=value params
	if traceParams != "" {
		for _, pair := range strings.Split(traceParams, ",") {
			parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
			if len(parts) == 2 {
				input[parts[0]] = parts[1]
			}
		}
	}

	// Create trace context
	collector := trace.NewMemoryCollector()
	tc := &trace.Context{
		FlowName:  flowName,
		Collector: collector,
		DryRun:    traceDryRun,
	}
	ctx = trace.WithTrace(ctx, tc)

	// Execute the flow
	start := time.Now()
	result, flowErr := handler.HandleRequest(ctx, input)
	totalDuration := time.Since(start)

	// If the flow produced a result and no error, record it
	if flowErr != nil {
		collector.Record(trace.Event{
			Stage: trace.StageWrite,
			Error: flowErr,
		})
	} else if result != nil {
		// Check if the last event already captured the result
		events := collector.Events()
		hasWriteOrRead := false
		for _, e := range events {
			if e.Stage == trace.StageWrite || e.Stage == trace.StageRead {
				hasWriteOrRead = true
				break
			}
		}
		if !hasWriteOrRead {
			collector.Record(trace.Event{
				Stage:  trace.StageRead,
				Output: result,
			})
		}
	}

	// Render the trace
	renderer := trace.NewRenderer(os.Stdout)
	renderer.Render(flowName, collector.Events(), totalDuration)

	return nil
}
