package step

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitrise-io/go-steputils/v2/testreport"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/errorutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	flowBFailsReport = `<?xml version='1.0' encoding='UTF-8'?>
<testsuites>
  <testsuite name="Test Suite" device="emulator-5554" tests="2" failures="1">
    <testcase id="Flow A" name="Flow A" classname="Flow A" file=".maestro/flow_a.yaml" time="12.1" status="SUCCESS"/>
    <testcase id="Flow B" name="Flow B" classname="Flow B" file=".maestro/flow_b.yaml" time="29.2" status="ERROR">
      <failure>Assertion is false: "Welcome" is visible</failure>
    </testcase>
  </testsuite>
</testsuites>
`
	flowBPassesReport = `<?xml version='1.0' encoding='UTF-8'?>
<testsuites>
  <testsuite name="Test Suite" device="emulator-5554" tests="1" failures="0">
    <testcase id="Flow B" name="Flow B" classname="Flow B" file=".maestro/flow_b.yaml" time="28.4" status="SUCCESS"/>
  </testsuite>
</testsuites>
`
	flowBFailsAgainReport = `<?xml version='1.0' encoding='UTF-8'?>
<testsuites>
  <testsuite name="Test Suite" device="emulator-5554" tests="1" failures="1">
    <testcase id="Flow B" name="Flow B" classname="Flow B" file=".maestro/flow_b.yaml" time="30.0" status="ERROR">
      <failure>Assertion is false: "Welcome" is visible</failure>
    </testcase>
  </testsuite>
</testsuites>
`
	sameNameFlowsFailReport = `<?xml version='1.0' encoding='UTF-8'?>
<testsuites>
  <testsuite name="Test Suite" device="emulator-5554" tests="2" failures="2">
    <testcase id="login" name="login" classname="login" file="flows/smoke/login.yaml" time="10.0" status="ERROR">
      <failure>Assertion is false: "Welcome" is visible</failure>
    </testcase>
    <testcase id="login" name="login" classname="login" file="flows/regression/login.yaml" time="10.0" status="ERROR">
      <failure>Assertion is false: "Welcome" is visible</failure>
    </testcase>
  </testsuite>
</testsuites>
`
	smokeLoginFailsReport = `<?xml version='1.0' encoding='UTF-8'?>
<testsuites>
  <testsuite name="Test Suite" device="emulator-5554" tests="2" failures="1">
    <testcase id="login" name="login" classname="login" file="flows/smoke/login.yaml" time="10.0" status="ERROR">
      <failure>Assertion is false: "Welcome" is visible</failure>
    </testcase>
    <testcase id="login" name="login" classname="login" file="flows/regression/login.yaml" time="10.0" status="SUCCESS"/>
  </testsuite>
</testsuites>
`
)

func TestRun_RetriesFailedFlows(t *testing.T) {
	maestro := newFakeMaestro(t, flowBFailsReport, flowBPassesReport)
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())

	result, err := s.Run(retryConfig(t, 2), Installation{BinaryPath: maestro.binary})
	require.NoError(t, err, "a flow that passes on a retry does not fail the Step")

	invocations := maestro.invocations(t)
	require.Len(t, invocations, 2)
	assert.True(t, strings.HasSuffix(invocations[0], " .maestro"))
	assert.True(t, strings.HasSuffix(invocations[1], " .maestro/flow_b.yaml"), "only the failed flow runs again")
	require.Len(t, result.Attempts, 2)
	assert.Contains(t, invocations[1], "--test-output-dir "+result.Attempts[1].TestOutputDir)
	assert.Equal(t, []string{"Flow B"}, flakyFlows(readReports(result.Attempts)))
}

func TestRun_FailsWhenAFlowFailsEveryAttempt(t *testing.T) {
	maestro := newFakeMaestro(t, flowBFailsReport, flowBFailsAgainReport, flowBFailsAgainReport)
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())

	result, err := s.Run(retryConfig(t, 2), Installation{BinaryPath: maestro.binary})
	require.Error(t, err)
	assert.Equal(t, errFlowsFailed+":\n  1 of 2 flows failed", errorutil.FormattedError(err), "flows are counted once, by their last run")
	assert.Len(t, maestro.invocations(t), 3)
	assert.Len(t, result.Attempts, 3)
	assert.Empty(t, flakyFlows(readReports(result.Attempts)))
}

func TestRun_DoesNotRetryWhenMaestroWritesNoReport(t *testing.T) {
	maestro := newFakeMaestro(t, "")
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())

	_, err := s.Run(retryConfig(t, 2), Installation{BinaryPath: maestro.binary})
	assert.Equal(t, errMaestroFailed+":", firstLine(err))
	assert.Len(t, maestro.invocations(t), 1)
}

func TestRun_DoesNotRetryByDefault(t *testing.T) {
	maestro := newFakeMaestro(t, flowBFailsReport)
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())

	result, err := s.Run(retryConfig(t, 0), Installation{BinaryPath: maestro.binary})
	assert.Equal(t, errFlowsFailed+":", firstLine(err))
	assert.Len(t, maestro.invocations(t), 1)
	assert.Len(t, result.Attempts, 1)
}

func TestRun_RetriesEveryFailedFileOfFlowsWithTheSameName(t *testing.T) {
	maestro := newFakeMaestro(t, sameNameFlowsFailReport, smokeLoginFailsReport)
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())

	_, err := s.Run(retryConfig(t, 1), Installation{BinaryPath: maestro.binary})
	assert.Equal(t, errFlowsFailed+":\n  1 of 2 flows failed", errorutil.FormattedError(err))

	invocations := maestro.invocations(t)
	require.Len(t, invocations, 2)
	assert.True(t, strings.HasSuffix(invocations[1], " flows/smoke/login.yaml flows/regression/login.yaml"))
}

func TestRun_CountsFlowsWithTheSameNameByFile(t *testing.T) {
	maestro := newFakeMaestro(t, smokeLoginFailsReport)
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())

	result, err := s.Run(retryConfig(t, 0), Installation{BinaryPath: maestro.binary})
	assert.Equal(t, errFlowsFailed+":\n  1 of 2 flows failed", errorutil.FormattedError(err))
	assert.Empty(t, flakyFlows(readReports(result.Attempts)))
}

func TestRun_DoesNotRetryFlowsOrderedByExecutionOrder(t *testing.T) {
	flowDir := t.TempDir()
	writeFile(t, filepath.Join(flowDir, "config.yaml"), "executionOrder:\n  continueOnFailure: false\n  flowsOrder:\n    - Flow A\n    - Flow B\n")
	maestro := newFakeMaestro(t, flowBFailsReport, flowBPassesReport)
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())

	config := retryConfig(t, 1)
	config.FlowPaths = []string{flowDir}
	config.WorkspaceConfig = workspaceConfigPath(config.FlowPaths, nil)
	_, err := s.Run(config, Installation{BinaryPath: maestro.binary})
	assert.Equal(t, errFlowsFailed+":", firstLine(err))
	assert.Len(t, maestro.invocations(t), 1)
}

func TestRun_RetriesKeepTheWorkspaceConfig(t *testing.T) {
	flowDir := t.TempDir()
	writeFile(t, filepath.Join(flowDir, "config.yaml"), "platform:\n  android:\n    disableAnimations: true\n")
	maestro := newFakeMaestro(t, flowBFailsReport, flowBPassesReport)
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())

	config := retryConfig(t, 1)
	config.FlowPaths = []string{flowDir}
	config.WorkspaceConfig = workspaceConfigPath(config.FlowPaths, nil)
	_, err := s.Run(config, Installation{BinaryPath: maestro.binary})
	require.NoError(t, err)

	invocations := maestro.invocations(t)
	require.Len(t, invocations, 2)
	for _, invocation := range invocations {
		assert.Contains(t, invocation, "--config "+filepath.Join(flowDir, "config.yaml"))
	}
}

func TestExportOutputs_AttachesEveryAttempt(t *testing.T) {
	maestro := newFakeMaestro(t, flowBFailsReport, flowBPassesReport)
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())
	result, err := s.Run(retryConfig(t, 1), Installation{BinaryPath: maestro.binary})
	require.NoError(t, err)

	testResultDir := t.TempDir()
	require.NoError(t, s.ExportOutputs(Config{TestName: "Maestro", TestResultDir: testResultDir}, result))

	testRunDir := filepath.Join(testResultDir, "Maestro")
	report := readTestReport(t, filepath.Join(testRunDir, junitReportFileName))
	assert.Equal(t, []string{"Flow A", "Flow B", "Flow B"}, testCaseNames(report))

	var flowBRuns [][]string
	forEachTestCase(&report, func(testCase *testreport.TestCase) {
		if testCase.Name == "Flow B" {
			flowBRuns = append(flowBRuns, attachmentValues(*testCase))
		}
	})
	assert.Equal(t, [][]string{
		{"attempt-1/Flow B/logs/maestro.log"},
		{"attempt-2/Flow B/logs/maestro.log"},
	}, flowBRuns, "each run of a flow links its own attempt's output")
	for _, run := range flowBRuns {
		assert.FileExists(t, filepath.Join(testRunDir, run[0]))
	}
}

func TestFailedFlowFiles(t *testing.T) {
	report := testreport.TestReport{TestSuites: []testreport.TestSuite{{TestCases: []testreport.TestCase{
		{Name: "Flow A", File: "flows/a.yaml"},
		{Name: "Flow B", File: "flows/b.yaml", Failure: &testreport.Failure{}},
		{Name: "Flow C", File: "flows/c.yaml", Error: &testreport.Error{}},
		{Name: "Flow D", Failure: &testreport.Failure{}},
	}}}}

	files, withoutFile := failedFlowFiles(report)
	assert.Equal(t, []string{"flows/b.yaml", "flows/c.yaml"}, files)
	assert.Equal(t, []string{"Flow D"}, withoutFile, "Maestro before 2.6.0 does not write the file attribute")
}

func TestMergeReports(t *testing.T) {
	first := testreport.TestReport{TestSuites: []testreport.TestSuite{{Name: "Test Suite", Tests: 2, Failures: 1, TestCases: []testreport.TestCase{
		{Name: "Flow A", ClassName: "Flow A"},
		{Name: "Flow B", ClassName: "Flow B", Failure: &testreport.Failure{}},
	}}}}
	retry := testreport.TestReport{TestSuites: []testreport.TestSuite{{Name: "Test Suite", Tests: 1, TestCases: []testreport.TestCase{
		{Name: "Flow B", ClassName: "Flow B"},
	}}}}

	merged := mergeReports([]testreport.TestReport{first, retry})
	require.Len(t, merged.TestSuites, 1)
	assert.Equal(t, []string{"Flow A", "Flow B", "Flow B"}, testCaseNames(merged))
	assert.Equal(t, 3, merged.TestSuites[0].Tests)
	assert.Equal(t, 1, merged.TestSuites[0].Failures)
	assert.Len(t, first.TestSuites[0].TestCases, 2, "the attempts' reports are left as they are")

	assert.Equal(t, first, mergeReports([]testreport.TestReport{first}), "a single attempt's report is kept as it is")
}

func TestFlakyFlows(t *testing.T) {
	report := testreport.TestReport{TestSuites: []testreport.TestSuite{{TestCases: []testreport.TestCase{
		{Name: "Passes", ClassName: "Passes"},
		{Name: "Flaky", ClassName: "Flaky", Failure: &testreport.Failure{}},
		{Name: "Broken", ClassName: "Broken", Failure: &testreport.Failure{}},
		{Name: "Flaky", ClassName: "Flaky"},
		{Name: "Broken", ClassName: "Broken", Error: &testreport.Error{}},
	}}}}
	assert.Equal(t, []string{"Flaky"}, flakyFlows([]testreport.TestReport{report}))
}

func retryConfig(t *testing.T, flakyAttempts int) Config {
	return Config{
		FlowPaths:     []string{".maestro"},
		App:           App{Path: "app.apk", Platform: PlatformAndroid},
		FlakyAttempts: flakyAttempts,
		AndroidSDKDir: fakeAndroidSDK(t, oneDeviceADB),
	}
}

type fakeMaestro struct {
	binary string
	dir    string
}

// newFakeMaestro returns a maestro that writes the given reports one per run, along with a Flow B output folder,
// and fails when the report has a failure. An empty report means the run writes none and fails. Like Maestro, it
// does not create the report's folder.
func newFakeMaestro(t *testing.T, reports ...string) fakeMaestro {
	dir := t.TempDir()
	for i, report := range reports {
		if report != "" {
			writeFile(t, filepath.Join(dir, fmt.Sprintf("report-%d.xml", i+1)), report)
		}
	}
	binary := filepath.Join(dir, "maestro")
	writeFile(t, binary, `#!/bin/sh
dir=$(dirname "$0")
n=$(($(cat "$dir/count" 2>/dev/null || echo 0) + 1))
echo "$n" > "$dir/count"
echo "$@" >> "$dir/invocations"
while [ $# -gt 0 ]; do
  case "$1" in
    --output) out="$2"; shift ;;
    --test-output-dir) testOutputDir="$2"; shift ;;
  esac
  shift
done
[ -f "$dir/report-$n.xml" ] || exit 1
cp "$dir/report-$n.xml" "$out" || exit 1
mkdir -p "$testOutputDir/2026-10-26_100000/Flow B/logs"
echo '{"entries":[{"kind":"MAESTRO_LOG","relativePath":"logs/maestro.log"}]}' > "$testOutputDir/2026-10-26_100000/Flow B/manifest.json"
echo "attempt $n" > "$testOutputDir/2026-10-26_100000/Flow B/logs/maestro.log"
if grep -q '<failure' "$out"; then exit 1; fi
`)
	require.NoError(t, os.Chmod(binary, 0755))
	return fakeMaestro{binary: binary, dir: dir}
}

func (f fakeMaestro) invocations(t *testing.T) []string {
	data, err := os.ReadFile(filepath.Join(f.dir, "invocations"))
	require.NoError(t, err)
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func readTestReport(t *testing.T, pth string) testreport.TestReport {
	report, err := readReport(pth)
	require.NoError(t, err)
	return report
}

func testCaseNames(report testreport.TestReport) []string {
	var names []string
	forEachTestCase(&report, func(testCase *testreport.TestCase) {
		names = append(names, testCase.Name)
	})
	return names
}

func attachmentValues(testCase testreport.TestCase) []string {
	var values []string
	if testCase.Properties == nil {
		return nil
	}
	for _, property := range testCase.Properties.Property {
		if strings.HasPrefix(property.Name, attachmentPropertyPrefix) {
			values = append(values, property.Value)
		}
	}
	return values
}
