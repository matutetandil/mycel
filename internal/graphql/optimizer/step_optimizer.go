package optimizer

import (
	"regexp"
	"strings"

	"github.com/matutetandil/mycel/internal/flow"
)

// StepOptimizer analyzes flow steps and determines which can be skipped
// based on the requested fields from a GraphQL query.
type StepOptimizer struct {
	steps           []*flow.StepConfig
	transformExprs  map[string]string // output.field -> expression
	requestedFields []string
}

// NewStepOptimizer creates a new step optimizer.
func NewStepOptimizer(steps []*flow.StepConfig, transformExprs map[string]string, requestedFields []string) *StepOptimizer {
	return &StepOptimizer{
		steps:           steps,
		transformExprs:  transformExprs,
		requestedFields: requestedFields,
	}
}

// StepDependency represents the relationship between output fields and steps.
type StepDependency struct {
	StepName     string   // Name of the step
	UsedByFields []string // Output fields that use this step
}

// AnalyzeDependencies determines which steps are needed based on requested fields.
// Returns a map of step names to whether they should be executed.
func (o *StepOptimizer) AnalyzeDependencies() map[string]bool {
	if len(o.requestedFields) == 0 || len(o.steps) == 0 {
		// No optimization info - execute all steps
		result := make(map[string]bool)
		for _, step := range o.steps {
			result[step.Name] = true
		}
		return result
	}

	// Build a map of which output fields depend on which steps
	fieldToSteps := o.buildFieldToStepMap()

	// Determine which steps are needed
	neededSteps := make(map[string]bool)

	for _, field := range o.requestedFields {
		// Get only the top-level field name
		topField := field
		if idx := strings.Index(field, "."); idx > 0 {
			topField = field[:idx]
		}

		// Check if this field depends on any steps
		if steps, ok := fieldToSteps[topField]; ok {
			for _, stepName := range steps {
				neededSteps[stepName] = true
			}
		}
	}

	// Also check for step dependencies (step B uses step A's result)
	o.resolveStepDependencies(neededSteps)

	return neededSteps
}

// ShouldExecuteStep returns true if a step should be executed.
func (o *StepOptimizer) ShouldExecuteStep(stepName string) bool {
	needed := o.AnalyzeDependencies()
	return needed[stepName]
}

// GetSkippableSteps returns the list of steps that can be skipped.
func (o *StepOptimizer) GetSkippableSteps() []string {
	needed := o.AnalyzeDependencies()
	var skippable []string

	for _, step := range o.steps {
		if !needed[step.Name] {
			skippable = append(skippable, step.Name)
		}
	}

	return skippable
}

// buildFieldToStepMap builds a map from output fields to the steps they depend on.
func (o *StepOptimizer) buildFieldToStepMap() map[string][]string {
	result := make(map[string][]string)

	// Pattern to match step.<name>. references
	stepPattern := regexp.MustCompile(`step\.(\w+)`)

	for field, expr := range o.transformExprs {
		// Find all step references in the expression
		matches := stepPattern.FindAllStringSubmatch(expr, -1)
		for _, match := range matches {
			if len(match) >= 2 {
				stepName := match[1]
				result[field] = appendUnique(result[field], stepName)
			}
		}
	}

	return result
}

// resolveStepDependencies adds steps that are dependencies of other needed steps.
func (o *StepOptimizer) resolveStepDependencies(neededSteps map[string]bool) {
	// Pattern to match step.<name>. references
	stepPattern := regexp.MustCompile(`step\.(\w+)`)

	// Keep iterating until no new dependencies are found
	changed := true
	for changed {
		changed = false
		for _, step := range o.steps {
			if !neededSteps[step.Name] {
				continue
			}

			// Check if this step's params or when condition references other steps
			for _, val := range step.Params {
				if strVal, ok := val.(string); ok {
					matches := stepPattern.FindAllStringSubmatch(strVal, -1)
					for _, match := range matches {
						if len(match) >= 2 {
							depStep := match[1]
							if !neededSteps[depStep] {
								neededSteps[depStep] = true
								changed = true
							}
						}
					}
				}
			}

			// Check when condition
			if step.When != "" {
				matches := stepPattern.FindAllStringSubmatch(step.When, -1)
				for _, match := range matches {
					if len(match) >= 2 {
						depStep := match[1]
						if !neededSteps[depStep] {
							neededSteps[depStep] = true
							changed = true
						}
					}
				}
			}
		}
	}
}

// appendUnique appends a value to a slice only if it doesn't already exist.
func appendUnique(slice []string, val string) []string {
	for _, v := range slice {
		if v == val {
			return slice
		}
	}
	return append(slice, val)
}

// GenerateStepConditions generates CEL conditions for steps based on field dependencies.
// This can be used to automatically add "when" conditions to steps.
func (o *StepOptimizer) GenerateStepConditions() map[string]string {
	fieldToSteps := o.buildFieldToStepMap()

	// Invert the map: step -> fields that use it
	stepToFields := make(map[string][]string)
	for field, steps := range fieldToSteps {
		for _, step := range steps {
			stepToFields[step] = appendUnique(stepToFields[step], field)
		}
	}

	// Generate conditions
	conditions := make(map[string]string)
	for stepName, fields := range stepToFields {
		if len(fields) == 1 {
			// Simple case: step is only used by one field
			conditions[stepName] = `has_field(input, "` + fields[0] + `")`
		} else if len(fields) > 1 {
			// Step is used by multiple fields - need OR condition
			parts := make([]string, len(fields))
			for i, f := range fields {
				parts[i] = `has_field(input, "` + f + `")`
			}
			conditions[stepName] = strings.Join(parts, " || ")
		}
	}

	return conditions
}

// OptimizeFlowSteps returns a modified list of steps with automatic "when" conditions
// added based on field dependencies. This is used to integrate step optimization
// transparently without modifying user HCL.
func OptimizeFlowSteps(steps []*flow.StepConfig, transformExprs map[string]string) []*flow.StepConfig {
	if len(steps) == 0 {
		return steps
	}

	optimizer := NewStepOptimizer(steps, transformExprs, nil)
	conditions := optimizer.GenerateStepConditions()

	// Create optimized steps (don't modify originals)
	optimized := make([]*flow.StepConfig, len(steps))
	for i, step := range steps {
		newStep := *step // Copy
		if condition, ok := conditions[step.Name]; ok {
			if newStep.When == "" {
				// Add automatic field-based condition
				newStep.When = condition
			} else {
				// Combine with existing condition
				newStep.When = "(" + newStep.When + ") && (" + condition + ")"
			}
		}
		optimized[i] = &newStep
	}

	return optimized
}

// ExtractTransformExpressions extracts output.field -> expression mappings from transform config.
func ExtractTransformExpressions(expressions map[string]string) map[string]string {
	result := make(map[string]string)

	for key, expr := range expressions {
		// Keys are like "output.id", "output.name", etc.
		if strings.HasPrefix(key, "output.") {
			field := strings.TrimPrefix(key, "output.")
			result[field] = expr
		} else {
			// Might be just the field name
			result[key] = expr
		}
	}

	return result
}
