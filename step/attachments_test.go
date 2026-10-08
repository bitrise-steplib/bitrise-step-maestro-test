package step

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitrise-io/go-android/v2/testresult/junitxml"
	"github.com/bitrise-io/go-steputils/v2/testreport"
	"github.com/bitrise-io/go-utils/v2/fileutil"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const maestroJUnitReport = `<?xml version='1.0' encoding='UTF-8'?>
<testsuites>
  <testsuite name="Test Suite" device="emulator-5554" tests="3" failures="1" time="552.743" timestamp="2026-10-02T10:11:46">
    <testcase id="Flow A" name="Flow A" classname="Flow A" file=".maestro/flow_a.yaml" time="421.573" timestamp="2026-10-02T10:11:47" status="SUCCESS"/>
    <testcase id="Flow B" name="Flow B" classname="Flow B" file=".maestro/flow_b.yaml" time="131.846" timestamp="2026-10-02T10:18:49" status="ERROR">
      <failure>Assertion is false: "Welcome" is visible</failure>
    </testcase>
    <testcase id="Login/Happy path" name="Login/Happy path" classname="Login/Happy path" file=".maestro/login.yaml" time="12.5" timestamp="2026-10-02T10:21:01" status="SUCCESS">
      <properties>
        <property name="tags" value="smoke"/>
      </properties>
    </testcase>
  </testsuite>
</testsuites>
`

func TestExportOutputs_LinksFlowArtifactsToTestCases(t *testing.T) {
	workDir := t.TempDir()
	junitPath := filepath.Join(workDir, junitReportFileName)
	writeFile(t, junitPath, maestroJUnitReport)

	runDir := filepath.Join(workDir, testOutputDirectoryName, "2026-10-02_101146")
	writeFlowBundle(t, filepath.Join(runDir, "Flow A"), `{"entries":[
		{"kind":"COMMAND_METADATA","format":"JSON","relativePath":"commands.json"},
		{"kind":"MAESTRO_LOG","format":"TXT","relativePath":"logs/maestro.log"}
	]}`, "commands.json", "logs/maestro.log")
	writeFlowBundle(t, filepath.Join(runDir, "Flow B"), `{"$schema":"https://storage.googleapis.com/maestro-schemas/artifact-manifest/v1.schema.json","entries":[
		{"kind":"COMMAND_METADATA","format":"JSON","relativePath":"commands.json","sizeBytes":4096},
		{"kind":"MAESTRO_LOG","format":"TXT","relativePath":"logs/maestro.log"},
		{"kind":"DEVICE_LOG","format":"TXT","relativePath":"logs/device-logcat.txt","metadata":{"source":"emulator"}},
		{"kind":"SCREENSHOT","format":"PNG","relativePath":"screenshots/","count":1},
		{"kind":"SCREEN_HIERARCHY","format":"JSON","relativePath":"screen-hierarchy","count":1},
		{"kind":"TAKE_SCREENSHOT","format":"PNG","relativePath":"takeScreenshot","count":1},
		{"kind":"FUTURE_KIND","relativePath":"future.png"}
	],"futureField":"ignored"}`,
		"commands.json", "logs/maestro.log", "logs/device-logcat.txt", "screenshots/step-003-assertVisible.png",
		"screen-hierarchy/step-3.json", "takeScreenshot/login/home.png", "future.png")
	writeFlowBundle(t, filepath.Join(runDir, "Login_Happy path"), `{"entries":[
		{"kind":"MAESTRO_LOG","format":"TXT","relativePath":"logs/maestro.log"}
	]}`, "logs/maestro.log")
	writeFlowBundle(t, filepath.Join(runDir, "Flow B-2"), `{"entries":[
		{"kind":"MAESTRO_LOG","format":"TXT","relativePath":"logs/maestro.log"}
	]}`, "logs/maestro.log")

	testResultDir := t.TempDir()
	s := testStep()
	err := s.ExportOutputs(
		Config{TestName: "Maestro", TestResultDir: testResultDir},
		Result{JUnitPath: junitPath, TestOutputDir: filepath.Join(workDir, testOutputDirectoryName)},
	)
	require.NoError(t, err)

	testRunDir := filepath.Join(testResultDir, "Maestro")
	assert.FileExists(t, filepath.Join(testRunDir, "test-info.json"))

	report := convertReport(t, filepath.Join(testRunDir, junitReportFileName))
	attachments := attachmentsByTestCase(report)
	assert.Equal(t, map[string][]string{
		"Flow A": {"Flow A/logs/maestro.log"},
		"Flow B": {
			"Flow B/logs/device-logcat.txt",
			"Flow B/logs/maestro.log",
			"Flow B/screenshots/step-003-assertVisible.png",
			"Flow B/takeScreenshot/login/home.png",
		},
		"Login/Happy path": {"Login_Happy path/logs/maestro.log"},
	}, attachments)

	for _, values := range attachments {
		for _, value := range values {
			assert.FileExists(t, filepath.Join(testRunDir, value), "attachment values are relative to the report's folder")
		}
	}
	assert.NoFileExists(t, filepath.Join(testRunDir, "Flow B", "commands.json"))
	assert.NoFileExists(t, filepath.Join(testRunDir, "Flow B", "screen-hierarchy", "step-3.json"))
	assert.NoDirExists(t, filepath.Join(testRunDir, "Flow B-2"))

	flowB := findTestCase(t, report, "Flow B")
	require.NotNil(t, flowB.Failure)
	assert.Contains(t, flowB.Failure.Value, `"Welcome" is visible`)
	assert.Equal(t, "smoke", propertyValue(findTestCase(t, report, "Login/Happy path"), "tags"))

	original := convertReport(t, junitPath)
	assert.Empty(t, attachmentsByTestCase(original), "the Maestro report itself is left untouched")
}

func TestExportOutputs_NoBundlesExportsOriginalReport(t *testing.T) {
	workDir := t.TempDir()
	junitPath := filepath.Join(workDir, junitReportFileName)
	writeFile(t, junitPath, maestroJUnitReport)

	testResultDir := t.TempDir()
	err := testStep().ExportOutputs(
		Config{TestName: "Maestro", TestResultDir: testResultDir},
		Result{JUnitPath: junitPath, TestOutputDir: filepath.Join(workDir, testOutputDirectoryName)},
	)
	require.NoError(t, err)

	exported, err := os.ReadFile(filepath.Join(testResultDir, "Maestro", junitReportFileName))
	require.NoError(t, err)
	assert.Equal(t, maestroJUnitReport, string(exported))
}

func TestLinkAttachments_KeepsExistingAttachmentNames(t *testing.T) {
	report := testreport.TestReport{TestSuites: []testreport.TestSuite{{TestCases: []testreport.TestCase{{
		Name: "Flow A",
		Properties: &testreport.Properties{Property: []testreport.Property{
			{Name: "attachment_kiscica", Value: "cat.png"},
			{Name: "attachment_1", Value: "custom.png"},
		}},
	}}}}}

	result := linkAttachments(&report, map[string]flowBundle{"Flow A": {Files: []string{"logs/maestro.log", "screenshots/final.png", "logs/device-logcat.txt"}}})

	assert.Empty(t, result.Warnings)
	assert.Equal(t, []testreport.Property{
		{Name: "attachment_kiscica", Value: "cat.png"},
		{Name: "attachment_1", Value: "custom.png"},
		{Name: "attachment_0", Value: "Flow A/logs/maestro.log"},
		{Name: "attachment_2", Value: "Flow A/screenshots/final.png"},
		{Name: "attachment_3", Value: "Flow A/logs/device-logcat.txt"},
	}, report.TestSuites[0].TestCases[0].Properties.Property)
}

func TestLinkAttachments_SkipsAmbiguousFlowNames(t *testing.T) {
	report := testreport.TestReport{TestSuites: []testreport.TestSuite{
		{TestCases: []testreport.TestCase{{Name: "Login"}}},
		{TestCases: []testreport.TestCase{{Name: "Login"}}},
	}}

	result := linkAttachments(&report, map[string]flowBundle{"Login": {Files: []string{"logs/maestro.log"}}})

	assert.Empty(t, result.Linked)
	assert.Len(t, result.Warnings, 1)
	assert.Nil(t, report.TestSuites[0].TestCases[0].Properties)
}

func TestAttachableFiles_IgnoresPathsOutsideTheFlowFolder(t *testing.T) {
	flowDir := filepath.Join(t.TempDir(), "flows", "Flow A")
	writeFlowBundle(t, flowDir, `{"entries":[
		{"kind":"MAESTRO_LOG","relativePath":"../../outside.log"},
		{"kind":"SCREENSHOT","relativePath":"/etc"},
		{"kind":"MAESTRO_LOG","relativePath":"logs/maestro.log"}
	]}`, "logs/maestro.log")
	writeFile(t, filepath.Join(flowDir, "..", "..", "outside.log"), "x")

	files, err := attachableFiles(flowDir)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join("logs", "maestro.log")}, files)
}

type fakeExporter struct{}

func (fakeExporter) ExportOutput(string, string) error                   { return nil }
func (fakeExporter) ExportOutputFilesZip(string, []string, string) error { return nil }

func testStep() Step {
	return Step{logger: log.NewLogger(), exporter: fakeExporter{}, fileManager: fileutil.NewFileManager()}
}

func writeFlowBundle(t *testing.T, flowDir, manifest string, files ...string) {
	writeFile(t, filepath.Join(flowDir, manifestFileName), manifest)
	for _, file := range files {
		writeFile(t, filepath.Join(flowDir, file), "content of "+file)
	}
}

func writeFile(t *testing.T, pth, content string) {
	require.NoError(t, os.MkdirAll(filepath.Dir(pth), 0755))
	require.NoError(t, os.WriteFile(pth, []byte(content), 0644))
}

func convertReport(t *testing.T, pth string) testreport.TestReport {
	converter := junitxml.Converter{}
	require.True(t, converter.Detect([]string{pth}))
	report, err := converter.Convert()
	require.NoError(t, err)
	return report
}

func attachmentsByTestCase(report testreport.TestReport) map[string][]string {
	attachments := map[string][]string{}
	forEachTestCase(&report, func(testCase *testreport.TestCase) {
		if testCase.Properties == nil {
			return
		}
		for _, property := range testCase.Properties.Property {
			if strings.HasPrefix(property.Name, attachmentPropertyPrefix) {
				attachments[testCase.Name] = append(attachments[testCase.Name], property.Value)
			}
		}
	})
	return attachments
}

func findTestCase(t *testing.T, report testreport.TestReport, name string) testreport.TestCase {
	var found *testreport.TestCase
	forEachTestCase(&report, func(testCase *testreport.TestCase) {
		if testCase.Name == name {
			found = testCase
		}
	})
	require.NotNil(t, found, name)
	return *found
}

func propertyValue(testCase testreport.TestCase, name string) string {
	if testCase.Properties == nil {
		return ""
	}
	for _, property := range testCase.Properties.Property {
		if property.Name == name {
			return property.Value
		}
	}
	return ""
}
