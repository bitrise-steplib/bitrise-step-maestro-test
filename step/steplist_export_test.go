package step

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-steputils/v2/testreport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const stepListJUnitReport = `<?xml version='1.0' encoding='UTF-8'?>
<testsuites>
  <testsuite name="Test Suite" tests="2" failures="1" time="10">
    <testcase id="Flow A" name="Flow A" classname="Flow A" time="4" status="SUCCESS"/>
    <testcase id="Flow B" name="Flow B" classname="Flow B" time="6" status="ERROR">
      <failure>Assertion is false: "Welcome" is visible</failure>
    </testcase>
  </testsuite>
</testsuites>
`

func TestExportOutputs_WritesTheStepListIntoEachFlowsSystemOut(t *testing.T) {
	result := writeStepListRun(t)

	testResultDir := t.TempDir()
	err := testStep().ExportOutputs(Config{TestName: "Maestro", TestResultDir: testResultDir}, result)
	require.NoError(t, err)

	report := convertReport(t, filepath.Join(testResultDir, "Maestro", junitReportFileName))

	flowA := findTestCase(t, report, "Flow A")
	require.NotNil(t, flowA.SystemOut)
	assert.Equal(t, "✓ Launch app (1.0s)", flowA.SystemOut.Value)
	assert.Nil(t, flowA.Failure)

	flowB := findTestCase(t, report, "Flow B")
	require.NotNil(t, flowB.SystemOut)
	assert.Equal(t, "✓ Launch app (1.0s)\n✗ Assert that \"Welcome\" is visible (5.0s)\n    Assertion is false: \"Welcome\" is visible", flowB.SystemOut.Value)
	require.NotNil(t, flowB.Failure)
	assert.Equal(t, `Assertion is false: "Welcome" is visible`, flowB.Failure.Value, "the failure element is left as Maestro wrote it")
}

func TestExportOutputs_StepListWorksForAFlowWithoutAttachableFiles(t *testing.T) {
	result := writeStepListRun(t)
	runDir := filepath.Join(result.Attempts[0].TestOutputDir, "2026-10-02_101146")
	writeFile(t, filepath.Join(runDir, "Flow A", manifestFileName), `{"entries":[]}`)

	testResultDir := t.TempDir()
	err := testStep().ExportOutputs(Config{TestName: "Maestro", TestResultDir: testResultDir}, result)
	require.NoError(t, err)

	report := convertReport(t, filepath.Join(testResultDir, "Maestro", junitReportFileName))
	flowA := findTestCase(t, report, "Flow A")
	require.NotNil(t, flowA.SystemOut)
	assert.Equal(t, "✓ Launch app (1.0s)", flowA.SystemOut.Value)
}

func TestExportOutputs_FlowWithUnreadableStepsKeepsItsTestCase(t *testing.T) {
	result := writeStepListRun(t)
	runDir := filepath.Join(result.Attempts[0].TestOutputDir, "2026-10-02_101146")
	require.NoError(t, os.Remove(filepath.Join(runDir, "Flow B", "logs", "maestro.log")))

	testResultDir := t.TempDir()
	err := testStep().ExportOutputs(Config{TestName: "Maestro", TestResultDir: testResultDir}, result)
	require.NoError(t, err)

	report := convertReport(t, filepath.Join(testResultDir, "Maestro", junitReportFileName))
	assert.Nil(t, findTestCase(t, report, "Flow B").SystemOut)
	assert.NotNil(t, findTestCase(t, report, "Flow A").SystemOut, "the other flows are not affected")
}

func TestExportOutputs_EveryAttemptGetsItsOwnStepList(t *testing.T) {
	workDir := t.TempDir()
	first, err := newAttempt(workDir, 1)
	require.NoError(t, err)
	second, err := newAttempt(workDir, 2)
	require.NoError(t, err)
	writeFile(t, first.JUnitPath, stepListJUnitReport)
	writeFile(t, second.JUnitPath, `<?xml version='1.0' encoding='UTF-8'?>
<testsuites>
  <testsuite name="Test Suite" tests="1" failures="0" time="6">
    <testcase id="Flow B" name="Flow B" classname="Flow B" time="6" status="SUCCESS"/>
  </testsuite>
</testsuites>
`)
	manifest := `{"entries":[{"kind":"MAESTRO_LOG","relativePath":"logs/maestro.log"}]}`
	for _, attempt := range []Attempt{first, second} {
		runDir := filepath.Join(attempt.TestOutputDir, "2026-10-02_101146")
		writeFlowBundle(t, filepath.Join(runDir, "Flow B"), manifest, "logs/maestro.log")
	}
	writeFlowBundle(t, filepath.Join(first.TestOutputDir, "2026-10-02_101146", "Flow A"), manifest, "logs/maestro.log")
	writeFlowFiles(t, filepath.Join(first.TestOutputDir, "2026-10-02_101146", "Flow A"), testCommand{name: "Launch app", status: "COMPLETED", duration: 1000})
	writeFlowFiles(t, filepath.Join(first.TestOutputDir, "2026-10-02_101146", "Flow B"),
		testCommand{name: "Launch app", status: "COMPLETED", duration: 1000},
		testCommand{name: "Assert that \"Welcome\" is visible", status: "FAILED", duration: 5000, errorMessage: "Assertion is false: \"Welcome\" is visible"},
	)
	writeFlowFiles(t, filepath.Join(second.TestOutputDir, "2026-10-02_101146", "Flow B"),
		testCommand{name: "Launch app", status: "COMPLETED", duration: 1000},
		testCommand{name: "Assert that \"Welcome\" is visible", status: "COMPLETED", duration: 800},
	)

	testResultDir := t.TempDir()
	err = testStep().ExportOutputs(Config{TestName: "Maestro", TestResultDir: testResultDir}, Result{WorkDir: workDir, Attempts: []Attempt{first, second}})
	require.NoError(t, err)

	report := convertReport(t, filepath.Join(testResultDir, "Maestro", junitReportFileName))
	var flowB []testreport.TestCase
	forEachTestCase(&report, func(testCase *testreport.TestCase) {
		if testCase.Name == "Flow B" {
			flowB = append(flowB, *testCase)
		}
	})
	require.Len(t, flowB, 2, "both runs of the flow are in the report")
	require.NotNil(t, flowB[0].SystemOut)
	assert.Equal(t, "✓ Launch app (1.0s)\n✗ Assert that \"Welcome\" is visible (5.0s)\n    Assertion is false: \"Welcome\" is visible", flowB[0].SystemOut.Value)
	require.NotNil(t, flowB[1].SystemOut)
	assert.Equal(t, "✓ Launch app (1.0s)\n✓ Assert that \"Welcome\" is visible (800ms)", flowB[1].SystemOut.Value)
}

// writeStepListRun writes a run of two flows, each with a commands.json and a maestro.log, and the JUnit report.
func writeStepListRun(t *testing.T) Result {
	t.Helper()

	result := singleAttemptResult(t, stepListJUnitReport)
	runDir := filepath.Join(result.Attempts[0].TestOutputDir, "2026-10-02_101146")
	manifest := `{"entries":[{"kind":"MAESTRO_LOG","relativePath":"logs/maestro.log"}]}`
	writeFlowBundle(t, filepath.Join(runDir, "Flow A"), manifest, "logs/maestro.log")
	writeFlowBundle(t, filepath.Join(runDir, "Flow B"), manifest, "logs/maestro.log")

	writeFlowFiles(t, filepath.Join(runDir, "Flow A"), testCommand{name: "Launch app", status: "COMPLETED", duration: 1000})
	writeFlowFiles(t, filepath.Join(runDir, "Flow B"),
		testCommand{name: "Launch app", status: "COMPLETED", duration: 1000},
		testCommand{
			name: `Assert that "Welcome" is visible`, status: "FAILED", duration: 5000,
			errorMessage: `Assertion is false: "Welcome" is visible`,
		},
	)
	return result
}
