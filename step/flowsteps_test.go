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

func TestReadFlowSteps_ACommandThatContainsOthersFinishesAfterThem(t *testing.T) {
	flowDir := t.TempDir()
	writeFile(t, filepath.Join(flowDir, "commands.json"), `[
  {"metadata": {"status": "COMPLETED", "timestamp": 1000, "duration": 0, "depth": 0}},
  {"metadata": {"status": "FAILED", "timestamp": 1100, "duration": 8910, "depth": 0}},
  {"metadata": {"status": "COMPLETED", "timestamp": 1100, "duration": 0, "depth": 1}},
  {"metadata": {"status": "FAILED", "timestamp": 1500, "duration": 8500, "depth": 1, "error": {"message": "Assertion is false", "debugMessage": "Details"}}}
]`)
	writeFile(t, filepath.Join(flowDir, "logs", "maestro.log"), strings.Join([]string{
		logLine("12:00:00.000", "Start", "Define variables", "RUNNING"),
		logLine("12:00:00.000", "Finished", "Define variables", "COMPLETED"),
		logLine("12:00:00.100", "Start", "Run helpers/fail.yaml", "RUNNING"),
		logLine("12:00:00.100", "Start", "Apply configuration", "RUNNING"),
		logLine("12:00:00.100", "Finished", "Apply configuration", "COMPLETED"),
		logLine("12:00:00.500", "Start", `Assert that "Missing" is visible`, "RUNNING"),
		logLine("12:00:09.000", "Finished", `Assert that "Missing" is visible`, "FAILED"),
		logLine("12:00:09.010", "Finished", "Run helpers/fail.yaml", "FAILED"),
	}, ""))

	steps, err := readFlowSteps(flowDir)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"Define variables", "Run helpers/fail.yaml", "Apply configuration", `Assert that "Missing" is visible`,
	}, stepNames(steps))
	assert.Equal(t, []string{"COMPLETED", "FAILED", "COMPLETED", "FAILED"}, stepStatuses(steps))
	assert.Equal(t, []int{0, 0, 1, 1}, stepDepths(steps))
	assert.Equal(t, "Assertion is false", steps[3].Message)
}

func TestReadFlowSteps_NeverReadsTheFlowEnv(t *testing.T) {
	flowDir := t.TempDir()
	writeFlowFiles(t, flowDir, 12*hourMs, testCommand{
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
	writeFlowFiles(t, flowDir, 12*hourMs, testCommand{
		name:    "Input text ${PASSWORD}",
		status:  "COMPLETED",
		command: `{"inputTextCommand": {"text": "${PASSWORD}"}}`,
	})

	steps, err := readFlowSteps(flowDir)
	require.NoError(t, err)

	require.Len(t, steps, 1)
	assert.Equal(t, "Input text ${PASSWORD}", steps[0].Name)
}

func TestReadFlowSteps_ToleratesTheLogClockPassingMidnight(t *testing.T) {
	flowDir := t.TempDir()
	writeFlowFiles(t, flowDir, dayMs-500,
		testCommand{name: "Define variables", status: "COMPLETED"},
		testCommand{name: "Apply configuration", status: "COMPLETED", atMs: 100},
		testCommand{name: "Launch app", status: "COMPLETED", atMs: 900},
	)

	steps, err := readFlowSteps(flowDir)
	require.NoError(t, err)

	assert.Equal(t, []string{"Define variables", "Apply configuration", "Launch app"}, stepNames(steps))
}

func TestReadFlowSteps_FailsWhenTheFilesDisagree(t *testing.T) {
	commands := func() []testCommand {
		return []testCommand{
			{name: "Define variables", status: "COMPLETED"},
			{name: "Apply configuration", status: "COMPLETED", atMs: 5},
			{name: "Tap on id: button", status: "COMPLETED", atMs: 1000},
		}
	}

	tests := []struct {
		name    string
		change  func(commands []testCommand) []testCommand
		wantErr string
	}{
		{
			name: "the log has fewer commands",
			change: func(commands []testCommand) []testCommand {
				commands[2].skipInLog = true
				return commands
			},
			wantErr: "has 3 commands but logs/maestro.log names 2",
		},
		{
			name: "a command has another status in the log",
			change: func(commands []testCommand) []testCommand {
				commands[2].logStatus = "FAILED"
				return commands
			},
			wantErr: `command 3 ("Tap on id: button") is FAILED in logs/maestro.log but COMPLETED in commands.json`,
		},
		{
			name: "a command started at another time in the log",
			change: func(commands []testCommand) []testCommand {
				commands[2].logDriftMs = 2000
				return commands
			},
			wantErr: `command 3 ("Tap on id: button") started 3000 ms after the first one in logs/maestro.log but 1000 ms in commands.json`,
		},
		{
			name: "a command has no timestamp",
			change: func(commands []testCommand) []testCommand {
				commands[1].noTimestamp = true
				return commands
			},
			wantErr: "command 2 in commands.json has no timestamp",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flowDir := t.TempDir()
			writeFlowFiles(t, flowDir, 12*hourMs, tt.change(commands())...)

			steps, err := readFlowSteps(flowDir)

			assert.Nil(t, steps)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestReadFlowSteps_FailsWhenAFinishedLineDoesNotMatchTheRunningCommand(t *testing.T) {
	flowDir := t.TempDir()
	writeFile(t, filepath.Join(flowDir, "commands.json"), `[
  {"metadata": {"status": "COMPLETED", "timestamp": 1000, "duration": 1, "depth": 0}},
  {"metadata": {"status": "COMPLETED", "timestamp": 1000, "duration": 1, "depth": 0}}
]`)
	writeFile(t, filepath.Join(flowDir, "logs", "maestro.log"), strings.Join([]string{
		logLine("12:00:00.000", "Start", "Define variables", "RUNNING"),
		logLine("12:00:00.000", "Start", "Apply configuration", "RUNNING"),
		logLine("12:00:00.000", "Finished", "Define variables", "COMPLETED"),
	}, ""))

	steps, err := readFlowSteps(flowDir)

	assert.Nil(t, steps)
	assert.ErrorContains(t, err, `"Define variables" finished while "Apply configuration" is the innermost running command`)
}

func TestReadFlowSteps_FailsWithoutLog(t *testing.T) {
	flowDir := t.TempDir()
	writeFile(t, filepath.Join(flowDir, "commands.json"), `[{"metadata": {"status": "COMPLETED", "timestamp": 1000, "duration": 1, "depth": 0}}]`)

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

const (
	hourMs = 60 * 60 * 1000
	// epochBaseMs is unrelated to the log's clock, as in a real run, where the log has only the time of day.
	epochBaseMs = 1_700_000_000_000
)

// testCommand is a command written to both commands.json and maestro.log. The fields below atMs make the log differ.
type testCommand struct {
	name    string
	status  string
	atMs    int64
	depth   int
	command string

	skipInLog   bool
	logStatus   string
	logDriftMs  int64
	noTimestamp bool
}

// writeFlowFiles writes a commands.json and a maestro.log that describe the same commands. The log's clock starts at
// startOfDayMs, a time of day in milliseconds.
func writeFlowFiles(t *testing.T, flowDir string, startOfDayMs int64, commands ...testCommand) {
	t.Helper()

	var records, logLines []string
	for _, command := range commands {
		raw := command.command
		if raw == "" {
			raw = "{}"
		}
		timestamp := fmt.Sprintf(`"timestamp": %d, `, epochBaseMs+command.atMs)
		if command.noTimestamp {
			timestamp = ""
		}
		records = append(records, fmt.Sprintf(`{"command": %s, "metadata": {"status": %q, %s"duration": 1, "depth": %d}}`,
			raw, command.status, timestamp, command.depth))

		if command.skipInLog {
			continue
		}
		clock := formatClock((startOfDayMs + command.atMs + command.logDriftMs) % dayMs)
		status := command.status
		if command.logStatus != "" {
			status = command.logStatus
		}
		logLines = append(logLines, logLine(clock, "Start", command.name, "RUNNING"), logLine(clock, "Finished", command.name, status))
	}

	writeFile(t, filepath.Join(flowDir, "commands.json"), "["+strings.Join(records, ",")+"]")
	writeFile(t, filepath.Join(flowDir, "logs", "maestro.log"), strings.Join(logLines, ""))
}

func logLine(clock, event, name, status string) string {
	return fmt.Sprintf("%s [ INFO] maestro.cli.runner.CliConsoleListener.onCommand%s: %s %s\n", clock, event, name, status)
}

func formatClock(ms int64) string {
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/hourMs, ms/60000%60, ms/1000%60, ms%1000)
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
