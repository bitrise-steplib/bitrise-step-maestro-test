package step

import (
	"fmt"
	"strings"

	"github.com/bitrise-io/go-steputils/v2/testreport"
)

const (
	stepIndent        = "  "
	explanationIndent = "    "
)

var statusSymbols = map[string]string{
	"COMPLETED": "✓",
	"WARNED":    "⚠",
	"FAILED":    "✗",
	"SKIPPED":   "-",
}

// formatStepList renders the steps of a flow as text for the test case's system-out: one line per step with its
// result and duration, indented by depth, and the explanation of a failure under the step that failed. The first line
// has to start at depth 0, because Test Reports trims the leading whitespace of the output.
func formatStepList(steps []flowStep) string {
	var lines []string
	for i, step := range steps {
		indent := strings.Repeat(stepIndent, step.Depth)
		lines = append(lines, fmt.Sprintf("%s%s %s (%s)", indent, statusSymbol(step.Status), displayName(step.Name), formatDuration(step.DurationMs)))

		if step.Message != "" && !childRepeatsFailure(steps, i) {
			lines = append(lines, explanationLines(indent+explanationIndent, step)...)
		}
	}
	return strings.Join(lines, "\n")
}

func statusSymbol(status string) string {
	if symbol, ok := statusSymbols[status]; ok {
		return symbol
	}
	return "•"
}

// displayName keeps the first line of a command text that has several lines, such as an evalScript block.
func displayName(name string) string {
	first, _, multiLine := strings.Cut(name, "\n")
	if multiLine {
		return first + " ..."
	}
	return name
}

func formatDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// childRepeatsFailure reports whether a step nested under steps[i] failed with the same message. When a subflow fails,
// Maestro gives the same error to the runFlow step and to the step inside it, and the one inside is where it belongs.
func childRepeatsFailure(steps []flowStep, i int) bool {
	for _, child := range steps[i+1:] {
		if child.Depth <= steps[i].Depth {
			return false
		}
		if child.Message == steps[i].Message {
			return true
		}
	}
	return false
}

func explanationLines(indent string, step flowStep) []string {
	text := step.Message
	if step.Details != "" && step.Details != step.Message {
		text += "\n\n" + step.Details
	}

	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, indent+line)
	}
	return lines
}

// addStepList puts the step list in front of any output the test case already has.
func addStepList(testCase *testreport.TestCase, steps []flowStep) {
	list := formatStepList(steps)
	if list == "" {
		return
	}
	if testCase.SystemOut != nil && testCase.SystemOut.Value != "" {
		list += "\n\n" + testCase.SystemOut.Value
	}
	testCase.SystemOut = &testreport.SystemOut{Value: list}
}
