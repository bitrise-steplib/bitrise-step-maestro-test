package step

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/bitrise-io/go-android/v2/testresult/junitxml"
	"github.com/bitrise-io/go-steputils/v2/testreport"
	"go.yaml.in/yaml/v3"
)

// Attempt is one run of maestro test. The ones after the first run the flows that failed in the one before.
type Attempt struct {
	// Name is the attempt's folder, in the work dir and next to the exported report.
	Name          string
	JUnitPath     string
	TestOutputDir string
}

func newAttempt(workDir string, number int) (Attempt, error) {
	name := fmt.Sprintf("attempt-%d", number)
	dir := filepath.Join(workDir, name)
	// Maestro does not create the folder of the report.
	if err := os.MkdirAll(dir, 0755); err != nil {
		return Attempt{}, err
	}
	return Attempt{
		Name:          name,
		JUnitPath:     filepath.Join(dir, junitReportFileName),
		TestOutputDir: filepath.Join(dir, testOutputDirectoryName),
	}, nil
}

func readReports(attempts []Attempt) []testreport.TestReport {
	var reports []testreport.TestReport
	for _, attempt := range attempts {
		if report, err := readReport(attempt.JUnitPath); err == nil {
			reports = append(reports, report)
		}
	}
	return reports
}

func readReport(junitPath string) (testreport.TestReport, error) {
	if !exists(junitPath) {
		return testreport.TestReport{}, fmt.Errorf("no JUnit report at %s", junitPath)
	}
	converter := junitxml.Converter{}
	converter.Detect([]string{junitPath})
	return converter.Convert()
}

func writeJUnitReport(report testreport.TestReport, pth string) error {
	data, err := xml.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pth, append([]byte(xml.Header), data...), 0644)
}

// ordersFlows tells whether the workspace config runs flows in a sequence. A retry of the failed flows alone breaks
// the sequence, and the report has no run of the flows the sequence skipped after a failure.
func ordersFlows(workspaceConfig string) bool {
	data, err := os.ReadFile(workspaceConfig)
	if err != nil {
		return false
	}
	var config struct {
		ExecutionOrder *yaml.Node `yaml:"executionOrder"`
	}
	return yaml.Unmarshal(data, &config) == nil && config.ExecutionOrder != nil
}

// flowKey tells the runs of a flow apart from other flows. The file keeps flows with the same name apart, a retry
// writes the same file as the first run.
type flowKey struct {
	name, className, file string
}

func keyOf(testCase testreport.TestCase) flowKey {
	return flowKey{testCase.Name, testCase.ClassName, testCase.File}
}

func flowFailed(testCase testreport.TestCase) bool {
	return testCase.Failure != nil || testCase.Error != nil
}

// lastRuns returns the last run of each flow, in the order the flows first ran. Test Reports judges a flow by its
// runs the same way.
func lastRuns(reports []testreport.TestReport) []testreport.TestCase {
	index := map[flowKey]int{}
	var runs []testreport.TestCase
	for _, report := range reports {
		forEachTestCase(&report, func(testCase *testreport.TestCase) {
			if i, ok := index[keyOf(*testCase)]; ok {
				runs[i] = *testCase
				return
			}
			index[keyOf(*testCase)] = len(runs)
			runs = append(runs, *testCase)
		})
	}
	return runs
}

// failedFlowFiles returns the flow files to run again, and the failed flows the report gives no file for: Maestro
// writes the file attribute from 2.6.0.
func failedFlowFiles(report testreport.TestReport) (files, withoutFile []string) {
	for _, testCase := range lastRuns([]testreport.TestReport{report}) {
		switch {
		case !flowFailed(testCase):
		case testCase.File == "":
			withoutFile = append(withoutFile, testCase.Name)
		case !slices.Contains(files, testCase.File):
			files = append(files, testCase.File)
		}
	}
	return files, withoutFile
}

// flakyFlows returns the flows that failed and then passed.
func flakyFlows(reports []testreport.TestReport) []string {
	failedBefore := map[flowKey]bool{}
	for _, report := range reports {
		forEachTestCase(&report, func(testCase *testreport.TestCase) {
			if flowFailed(*testCase) {
				failedBefore[keyOf(*testCase)] = true
			}
		})
	}
	var names []string
	for _, testCase := range lastRuns(reports) {
		if !flowFailed(testCase) && failedBefore[keyOf(testCase)] {
			names = append(names, testCase.Name)
		}
	}
	return names
}

// mergeReports appends the test cases of the later attempts to the first suite of the first attempt. Test Reports
// shows the runs of a flow as one test case, flaky when the runs differ, but only within one suite.
func mergeReports(reports []testreport.TestReport) testreport.TestReport {
	merged := reports[0]
	if len(reports) == 1 || len(merged.TestSuites) == 0 {
		return merged
	}
	merged.TestSuites = slices.Clone(merged.TestSuites)
	suite := &merged.TestSuites[0]
	suite.TestCases = slices.Clone(suite.TestCases)
	for _, report := range reports[1:] {
		forEachTestCase(&report, func(testCase *testreport.TestCase) {
			suite.TestCases = append(suite.TestCases, *testCase)
		})
	}

	suite.Tests, suite.Failures, suite.Errors = len(suite.TestCases), 0, 0
	for _, testCase := range suite.TestCases {
		if testCase.Failure != nil {
			suite.Failures++
		}
		if testCase.Error != nil {
			suite.Errors++
		}
	}
	return merged
}
