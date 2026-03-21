package trace

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// Renderer formats trace events for human-readable CLI output.
type Renderer struct {
	writer io.Writer
}

// NewRenderer creates a renderer that writes to the given writer.
func NewRenderer(w io.Writer) *Renderer {
	return &Renderer{writer: w}
}

// Render formats and writes all trace events.
func (r *Renderer) Render(flowName string, events []Event, totalDuration time.Duration) {
	fmt.Fprintf(r.writer, "\n")
	fmt.Fprintf(r.writer, "  Flow: %s\n", flowName)
	fmt.Fprintf(r.writer, "  Duration: %s\n", totalDuration.Round(time.Microsecond))
	fmt.Fprintf(r.writer, "  %s\n\n", strings.Repeat("─", 50))

	for i, event := range events {
		r.renderEvent(i+1, event)
	}

	// Summary
	hasErrors := false
	for _, e := range events {
		if e.Error != nil {
			hasErrors = true
			break
		}
	}

	if hasErrors {
		fmt.Fprintf(r.writer, "  ✗ completed with errors\n\n")
	} else {
		fmt.Fprintf(r.writer, "  ✓ completed successfully\n\n")
	}
}

// renderEvent formats a single trace event.
func (r *Renderer) renderEvent(index int, event Event) {
	// Stage header
	label := r.stageLabel(event)
	duration := ""
	if event.Duration > 0 {
		duration = fmt.Sprintf("  %s", event.Duration.Round(time.Microsecond))
	}

	if event.Skipped {
		fmt.Fprintf(r.writer, "  %d. %s%s  (skipped", index, label, duration)
		if event.Detail != "" {
			fmt.Fprintf(r.writer, ": %s", event.Detail)
		}
		fmt.Fprintf(r.writer, ")\n\n")
		return
	}

	if event.DryRun {
		fmt.Fprintf(r.writer, "  %d. %s%s  [dry-run]\n", index, label, duration)
	} else {
		fmt.Fprintf(r.writer, "  %d. %s%s\n", index, label, duration)
	}

	// Detail line
	if event.Detail != "" {
		fmt.Fprintf(r.writer, "     %s\n", event.Detail)
	}

	// Error
	if event.Error != nil {
		fmt.Fprintf(r.writer, "     ✗ error: %s\n", event.Error.Error())
		fmt.Fprintf(r.writer, "\n")
		return
	}

	// Output data
	if event.Output != nil {
		r.renderData(event.Output)
	}

	fmt.Fprintf(r.writer, "\n")
}

// stageLabel returns a human-readable label for a stage.
func (r *Renderer) stageLabel(event Event) string {
	switch event.Stage {
	case StageInput:
		return "INPUT"
	case StageSanitize:
		return "SANITIZE"
	case StageFilter:
		return "FILTER"
	case StageAccept:
		return "ACCEPT"
	case StageDedupe:
		return "DEDUPE"
	case StageValidateIn:
		return "VALIDATE INPUT"
	case StageEnrich:
		if event.Name != "" {
			return fmt.Sprintf("ENRICH (%s)", event.Name)
		}
		return "ENRICH"
	case StageTransform:
		return "TRANSFORM"
	case StageStep:
		if event.Name != "" {
			return fmt.Sprintf("STEP (%s)", event.Name)
		}
		return "STEP"
	case StageValidateOut:
		return "VALIDATE OUTPUT"
	case StageRead:
		if event.Name != "" {
			return fmt.Sprintf("READ → %s", event.Name)
		}
		return "READ"
	case StageWrite:
		if event.Name != "" {
			return fmt.Sprintf("WRITE → %s", event.Name)
		}
		return "WRITE"
	case StageCacheHit:
		return "CACHE HIT"
	case StageCacheMiss:
		return "CACHE MISS"
	default:
		return string(event.Stage)
	}
}

// renderData formats data output with indentation.
func (r *Renderer) renderData(data interface{}) {
	b, err := json.MarshalIndent(data, "     ", "  ")
	if err != nil {
		fmt.Fprintf(r.writer, "     %v\n", data)
		return
	}

	output := string(b)

	// Truncate very large outputs
	if len(output) > 2000 {
		output = output[:2000] + "\n     ... (truncated)"
	}

	fmt.Fprintf(r.writer, "     %s\n", output)
}
