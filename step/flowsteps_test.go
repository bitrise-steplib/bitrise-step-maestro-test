package step

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files of the flow steps")

type mobileFixture struct {
	platform   string
	outputDir  string
	appID      string
	screenshot string
}

// mobileFixtures is the output of Maestro 2.11.0 (maestro test --test-output-dir) from Bitrise builds: the Wikipedia
// sample on an iOS simulator and KeePassVault on an Android emulator. Each has a passing flow, and a failing flow with
// a subflow, a failing optional step and a failing assertion.
var mobileFixtures = []mobileFixture{
	{platform: "ios", outputDir: "testdata/ios/test-output", appID: "org.wikimedia.wikipedia", screenshot: "wikipedia-launch"},
	{platform: "android", outputDir: "testdata/android/test-output", appID: "com.ivanovsky.passnotes.automation", screenshot: "keepassvault-launch"},
}

func TestReadFlowSteps_MatchesGolden(t *testing.T) {
	var flowDirs []string
	for _, fixture := range mobileFixtures {
		dirs, err := filepath.Glob(filepath.Join(fixture.outputDir, "*", "*"))
		require.NoError(t, err)
		require.NotEmpty(t, dirs)
		flowDirs = append(flowDirs, dirs...)
	}

	for _, flowDir := range flowDirs {
		platform := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(flowDir))))
		t.Run(platform+"/"+filepath.Base(flowDir), func(t *testing.T) {
			steps, err := readFlowSteps(flowDir)
			require.NoError(t, err)
			got, err := json.MarshalIndent(steps, "", "  ")
			require.NoError(t, err)

			goldenPath := filepath.Join(flowDir, "steps.golden.json")
			if *updateGolden {
				require.NoError(t, os.WriteFile(goldenPath, append(got, '\n'), 0o644))
			}
			want, err := os.ReadFile(goldenPath)
			require.NoError(t, err)
			assert.JSONEq(t, string(want), string(got))
		})
	}
}

func TestReadFlowSteps_ACommandTextCanSpanSeveralLines(t *testing.T) {
	// The shape Maestro 2.11.0 logs for an evalScript with a multi-line script: only the first line starts with a time,
	// and the status follows the script's last newline.
	script := "Run ${\n  output.first = 1;\n  output.second = 2;\n}\n"
	flowDir := t.TempDir()
	writeFlowFiles(t, flowDir,
		testCommand{name: "Define variables", status: "COMPLETED"},
		testCommand{name: script, status: "COMPLETED"},
		testCommand{name: `Assert that "Fixture page" is visible`, status: "COMPLETED"},
	)

	steps, err := readFlowSteps(flowDir)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"Define variables",
		"Run ${\n  output.first = 1;\n  output.second = 2;\n}",
		`Assert that "Fixture page" is visible`,
	}, stepNames(steps))
	assert.Equal(t, []string{"COMPLETED", "COMPLETED", "COMPLETED"}, stepStatuses(steps))
}

func TestReadFlowSteps_LinesOfOtherLogEntriesDoNotChangeTheNames(t *testing.T) {
	flowDir := t.TempDir()
	writeFile(t, filepath.Join(flowDir, "commands.json"), `[
  {"metadata": {"status": "COMPLETED", "duration": 0, "depth": 0}},
  {"metadata": {"status": "FAILED", "duration": 900, "depth": 0}}
]`)
	writeFile(t, filepath.Join(flowDir, "logs", "maestro.log"), strings.Join([]string{
		logLine("12:00:00.000", "Start", "Define variables", "RUNNING"),
		logLine("12:00:00.000", "Finished", "Define variables", "COMPLETED"),
		logLine("12:00:00.100", "Start", "Tap on id: button", "RUNNING"),
		"12:00:00.900 [ERROR] maestro.orchestra.Orchestra.executeCommands: CommandFailed: Element not found\n" +
			"maestro.MaestroException$ElementNotFound: Element not found\n" +
			"\tat maestro.Maestro.tap(Maestro.kt:1)\n",
		logLine("12:00:01.000", "Finished", "Tap on id: button", "FAILED"),
	}, ""))

	steps, err := readFlowSteps(flowDir)
	require.NoError(t, err)

	assert.Equal(t, []string{"Define variables", "Tap on id: button"}, stepNames(steps))
}

func TestReadFlowSteps_NeverReadsTheFlowEnv(t *testing.T) {
	flowDir := t.TempDir()
	writeFlowFiles(t, flowDir, testCommand{
		name:    "Define variables",
		status:  "COMPLETED",
		command: `{"defineVariablesCommand": {"env": {"PASSWORD": "s3cret-value"}}}`,
	})

	steps, err := readFlowSteps(flowDir)
	require.NoError(t, err)

	got, err := json.Marshal(steps)
	require.NoError(t, err)
	assert.NotContains(t, string(got), "s3cret-value")
	assert.Equal(t, "Define variables", steps[0].Name)
}

func TestReadFlowSteps_DoesNotResolvePlaceholders(t *testing.T) {
	flowDir := t.TempDir()
	writeFlowFiles(t, flowDir, testCommand{
		name:    "Input text ${PASSWORD}",
		status:  "COMPLETED",
		command: `{"inputTextCommand": {"text": "${PASSWORD}"}}`,
	})

	steps, err := readFlowSteps(flowDir)
	require.NoError(t, err)

	require.Len(t, steps, 1)
	assert.Equal(t, "Input text ${PASSWORD}", steps[0].Name)
}

func TestReadFlowSteps_FailsWhenTheFilesListDifferentNumbersOfCommands(t *testing.T) {
	flowDir := t.TempDir()
	writeFlowFiles(t, flowDir,
		testCommand{name: "Define variables", status: "COMPLETED"},
		testCommand{name: "Apply configuration", status: "COMPLETED"},
		testCommand{name: "Tap on id: button", status: "COMPLETED", skipInLog: true},
	)

	steps, err := readFlowSteps(flowDir)

	assert.Nil(t, steps)
	assert.ErrorContains(t, err, "has 3 commands but logs/maestro.log names 2")
}

func TestReadFlowSteps_FailsWithoutLog(t *testing.T) {
	flowDir := t.TempDir()
	writeFile(t, filepath.Join(flowDir, "commands.json"), `[{"metadata": {"status": "COMPLETED", "duration": 1, "depth": 0}}]`)

	_, err := readFlowSteps(flowDir)

	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestReadFlowSteps_PassingFlow(t *testing.T) {
	for _, fixture := range mobileFixtures {
		t.Run(fixture.platform, func(t *testing.T) {
			steps, err := readFlowSteps(fixtureFlowDir(t, fixture.outputDir, "fixture-passing"))
			require.NoError(t, err)

			assert.Equal(t, []string{
				"Define variables",
				"Apply configuration",
				fmt.Sprintf("Launch app %q", fixture.appID),
				"Wait for animation to end",
				"Take screenshot " + fixture.screenshot,
			}, stepNames(steps), "implicit steps included")
			assert.Equal(t, []string{"COMPLETED", "COMPLETED", "COMPLETED", "COMPLETED", "COMPLETED"}, stepStatuses(steps))
			assert.Equal(t, []int{0, 0, 0, 0, 0}, stepDepths(steps))
			for _, step := range steps {
				assert.Empty(t, step.Message)
			}
		})
	}
}

func TestReadFlowSteps_FailingFlow(t *testing.T) {
	for _, fixture := range mobileFixtures {
		t.Run(fixture.platform, func(t *testing.T) {
			steps, err := readFlowSteps(fixtureFlowDir(t, fixture.outputDir, "fixture-failing"))
			require.NoError(t, err)

			assert.Equal(t, []string{
				"Define variables",
				"Apply configuration",
				"Run helpers/launch.yaml",
				"Apply configuration",
				fmt.Sprintf("Launch app %q", fixture.appID),
				"Wait for animation to end",
				`Tap on (Optional) "Button that does not exist"`,
				`Assert that "Text that is not on the screen" is visible`,
			}, stepNames(steps), "the subflow has its own Apply configuration, and nothing follows the failed step")
			assert.Equal(t, []string{
				"COMPLETED", "COMPLETED", "COMPLETED", "COMPLETED", "COMPLETED", "COMPLETED", "WARNED", "FAILED",
			}, stepStatuses(steps))
			assert.Equal(t, []int{0, 0, 0, 1, 1, 1, 0, 0}, stepDepths(steps), "the subflow's steps are one level deeper")

			warned := steps[6]
			assert.Empty(t, warned.Message, "an optional step that did not match is not a failure")

			failed := steps[7]
			assert.Equal(t, `Assertion is false: "Text that is not on the screen" is visible`, failed.Message)
			assert.NotEmpty(t, failed.Details)
			assert.Positive(t, failed.DurationMs)
		})
	}
}

// testCommand is a command written to both commands.json and maestro.log.
type testCommand struct {
	name    string
	status  string
	depth   int
	command string

	skipInLog bool
}

// writeFlowFiles writes a commands.json and a maestro.log that describe the same commands.
func writeFlowFiles(t *testing.T, flowDir string, commands ...testCommand) {
	t.Helper()

	var records, logLines []string
	for _, command := range commands {
		raw := command.command
		if raw == "" {
			raw = "{}"
		}
		records = append(records, fmt.Sprintf(`{"command": %s, "metadata": {"status": %q, "duration": 1, "depth": %d}}`,
			raw, command.status, command.depth))

		if !command.skipInLog {
			logLines = append(logLines,
				logLine("12:00:00.000", "Start", command.name, "RUNNING"),
				logLine("12:00:00.000", "Finished", command.name, command.status))
		}
	}

	writeFile(t, filepath.Join(flowDir, "commands.json"), "["+strings.Join(records, ",")+"]")
	writeFile(t, filepath.Join(flowDir, "logs", "maestro.log"), strings.Join(logLines, ""))
}

func logLine(clock, event, name, status string) string {
	return fmt.Sprintf("%s [ INFO] maestro.cli.runner.CliConsoleListener.onCommand%s: %s %s\n", clock, event, name, status)
}

func fixtureFlowDir(t *testing.T, outputDir, flow string) string {
	t.Helper()
	flowDirs, err := filepath.Glob(filepath.Join(outputDir, "*", flow))
	require.NoError(t, err)
	require.Len(t, flowDirs, 1)
	return flowDirs[0]
}

func stepNames(steps []flowStep) []string {
	var names []string
	for _, step := range steps {
		names = append(names, step.Name)
	}
	return names
}

func stepStatuses(steps []flowStep) []string {
	var statuses []string
	for _, step := range steps {
		statuses = append(statuses, step.Status)
	}
	return statuses
}

func stepDepths(steps []flowStep) []int {
	var depths []int
	for _, step := range steps {
		depths = append(depths, step.Depth)
	}
	return depths
}
