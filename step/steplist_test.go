package step

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatStepList_OneLinePerStep(t *testing.T) {
	steps := []flowStep{
		{Name: "Launch app", Status: "COMPLETED", DurationMs: 4142},
		{Name: `Tap on (Optional) "Missing"`, Status: "WARNED", DurationMs: 17000},
		{Name: "Take screenshot", Status: "SKIPPED", DurationMs: 0},
	}

	got := formatStepList(steps)

	assert.Equal(t, "✓ Launch app (4.1s)\n⚠ Tap on (Optional) \"Missing\" (17.0s)\n- Take screenshot (0ms)", got)
}

func TestFormatStepList_ShortDurationsAreInMilliseconds(t *testing.T) {
	got := formatStepList([]flowStep{{Name: "Define variables", Status: "COMPLETED", DurationMs: 999}})

	assert.Equal(t, "✓ Define variables (999ms)", got)
}

func TestFormatStepList_IndentsStepsByDepth(t *testing.T) {
	steps := []flowStep{
		{Name: "Run helpers/launch.yaml", Status: "COMPLETED", DurationMs: 8000, Depth: 0},
		{Name: "Launch app", Status: "COMPLETED", DurationMs: 4000, Depth: 1},
		{Name: "Nested", Status: "COMPLETED", DurationMs: 1, Depth: 2},
		{Name: "Back at the top", Status: "COMPLETED", DurationMs: 1, Depth: 0},
	}

	got := formatStepList(steps)

	assert.Equal(t, "✓ Run helpers/launch.yaml (8.0s)\n  ✓ Launch app (4.0s)\n    ✓ Nested (1ms)\n✓ Back at the top (1ms)", got)
}

func TestFormatStepList_ExplainsAFailureUnderTheFailedStep(t *testing.T) {
	steps := []flowStep{
		{Name: "Launch app", Status: "COMPLETED", DurationMs: 1000},
		{Name: `Assert that "Welcome" is visible`, Status: "FAILED", DurationMs: 17000,
			Message: `Assertion is false: "Welcome" is visible`,
			Details: "Check the UI hierarchy.\n\nPossible causes:\n- Wrong selector"},
	}

	got := formatStepList(steps)

	assert.Equal(t, `✓ Launch app (1.0s)
✗ Assert that "Welcome" is visible (17.0s)
    Assertion is false: "Welcome" is visible

    Check the UI hierarchy.

    Possible causes:
    - Wrong selector`, got)
}

func TestFormatStepList_ExplainsAFailureOnceWhenASubflowFails(t *testing.T) {
	steps := []flowStep{
		{Name: "Run helpers/login.yaml", Status: "FAILED", Depth: 0, Message: "Element not found", Details: "details"},
		{Name: "Apply configuration", Status: "COMPLETED", Depth: 1},
		{Name: "Tap on id: login", Status: "FAILED", Depth: 1, Message: "Element not found", Details: "details"},
	}

	got := formatStepList(steps)

	assert.Equal(t, 1, strings.Count(got, "Element not found"), got)
	assert.Contains(t, got, "      Element not found", "the explanation is under the step that failed, indented below it")
}

func TestFormatStepList_ShowsTheDetailsOnceWhenTheyRepeatTheMessage(t *testing.T) {
	got := formatStepList([]flowStep{{Name: "Tap", Status: "FAILED", Message: "Not found", Details: "Not found"}})

	assert.Equal(t, 1, strings.Count(got, "Not found"), got)
}

func TestFormatStepList_ShortensAMultiLineName(t *testing.T) {
	got := formatStepList([]flowStep{{Name: "Run ${\n  output.a = 1;\n}", Status: "COMPLETED", DurationMs: 3}})

	assert.Equal(t, "✓ Run ${ ... (3ms)", got)
}

func TestFormatStepList_NoStepsGivesNoText(t *testing.T) {
	assert.Empty(t, formatStepList(nil))
}
