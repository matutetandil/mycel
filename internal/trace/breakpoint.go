package trace

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// BreakpointMode controls when the debugger pauses execution.
type BreakpointMode int

const (
	// BreakNone disables breakpoints (default).
	BreakNone BreakpointMode = iota
	// BreakAll pauses at every pipeline stage.
	BreakAll
	// BreakStages pauses only at specified stages.
	BreakStages
)

// Breakpoint controls interactive debugging during trace execution.
// When active, it pauses execution at pipeline stages and waits for
// user commands via stdin.
type Breakpoint struct {
	mode   BreakpointMode
	stages map[Stage]bool
	reader *bufio.Reader
	writer io.Writer

	// skip causes the debugger to run without pausing until the next
	// explicit breakpoint (used by the "continue" command).
	skip bool
}

// NewBreakpoint creates a breakpoint controller that pauses at every stage.
func NewBreakpoint(reader io.Reader, writer io.Writer) *Breakpoint {
	return &Breakpoint{
		mode:   BreakAll,
		reader: bufio.NewReader(reader),
		writer: writer,
	}
}

// NewBreakpointForStages creates a breakpoint controller that pauses at specific stages.
func NewBreakpointForStages(stages []Stage, reader io.Reader, writer io.Writer) *Breakpoint {
	m := make(map[Stage]bool, len(stages))
	for _, s := range stages {
		m[s] = true
	}
	return &Breakpoint{
		mode:   BreakStages,
		stages: m,
		reader: bufio.NewReader(reader),
		writer: writer,
	}
}

// ShouldBreak returns true if execution should pause at the given stage.
func (b *Breakpoint) ShouldBreak(stage Stage) bool {
	if b == nil || b.mode == BreakNone {
		return false
	}
	if b.skip {
		// "continue" was used — only break at explicit stage breakpoints
		if b.mode == BreakStages && b.stages[stage] {
			b.skip = false
			return true
		}
		return false
	}
	if b.mode == BreakAll {
		return true
	}
	return b.stages[stage]
}

// Pause blocks execution and waits for user input.
// Returns false if the user wants to abort.
func (b *Breakpoint) Pause(stage Stage, name string, data interface{}) bool {
	if b == nil {
		return true
	}

	// Print breakpoint header
	label := string(stage)
	if name != "" {
		label += " (" + name + ")"
	}
	fmt.Fprintf(b.writer, "\n  ⏸  BREAKPOINT at %s\n", label)

	if data != nil {
		jsonBytes, err := json.MarshalIndent(data, "     ", "  ")
		if err == nil {
			fmt.Fprintf(b.writer, "     %s\n", string(jsonBytes))
		}
	}

	for {
		fmt.Fprintf(b.writer, "\n  debug> ")
		line, err := b.reader.ReadString('\n')
		if err != nil {
			return false
		}

		cmd := strings.TrimSpace(strings.ToLower(line))
		switch cmd {
		case "", "n", "next":
			// Step to next stage
			return true
		case "c", "continue":
			// Run until next breakpoint (or end)
			b.skip = true
			return true
		case "p", "print":
			// Re-print the data
			if data != nil {
				jsonBytes, _ := json.MarshalIndent(data, "     ", "  ")
				fmt.Fprintf(b.writer, "     %s\n", string(jsonBytes))
			} else {
				fmt.Fprintf(b.writer, "     (no data)\n")
			}
		case "q", "quit", "abort":
			return false
		case "h", "help", "?":
			fmt.Fprintf(b.writer, "  Commands:\n")
			fmt.Fprintf(b.writer, "    n, next      Step to next stage (default)\n")
			fmt.Fprintf(b.writer, "    c, continue   Run until next breakpoint\n")
			fmt.Fprintf(b.writer, "    p, print      Print current data\n")
			fmt.Fprintf(b.writer, "    q, quit       Abort execution\n")
			fmt.Fprintf(b.writer, "    h, help       Show this help\n")
		default:
			fmt.Fprintf(b.writer, "  Unknown command: %s (type 'h' for help)\n", cmd)
		}
	}
}

// ParseBreakStages parses a comma-separated stage list into Stage values.
// Valid stages: input, sanitize, filter, validate, transform, step, read, write
func ParseBreakStages(stages string) []Stage {
	if stages == "" {
		return nil
	}

	var result []Stage
	for _, s := range strings.Split(stages, ",") {
		s = strings.TrimSpace(strings.ToLower(s))
		switch s {
		case "input":
			result = append(result, StageInput)
		case "sanitize":
			result = append(result, StageSanitize)
		case "filter":
			result = append(result, StageFilter)
		case "accept":
			result = append(result, StageAccept)
		case "dedupe":
			result = append(result, StageDedupe)
		case "validate", "validate_input":
			result = append(result, StageValidateIn)
		case "enrich":
			result = append(result, StageEnrich)
		case "transform":
			result = append(result, StageTransform)
		case "step":
			result = append(result, StageStep)
		case "validate_output":
			result = append(result, StageValidateOut)
		case "read":
			result = append(result, StageRead)
		case "write":
			result = append(result, StageWrite)
		}
	}
	return result
}
