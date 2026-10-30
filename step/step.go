package step

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bitrise-io/go-steputils/v2/stepconf"
	"github.com/bitrise-io/go-steputils/v2/testquarantine"
	"github.com/bitrise-io/go-steputils/v2/testreport"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/fileutil"
	"github.com/bitrise-io/go-utils/v2/log"
)

const (
	junitPathOutputKey      = "MAESTRO_JUNIT_PATH"
	testOutputZipOutputKey  = "MAESTRO_TEST_OUTPUT_ZIP_PATH"
	testOutputZipFileName   = "maestro-test-output.zip"
	junitReportFileName     = "maestro-report.xml"
	testOutputDirectoryName = "test-output"
	disableAnalyticsEnv     = "MAESTRO_CLI_NO_ANALYTICS=true"
	disableUpdateCheckEnv   = "MAESTRO_DISABLE_UPDATE_CHECK=true"
)

// The first line of a failed run's error message, which the build_steps error_message column is grouped by. Keep
// them free of paths, counts and other run-specific values, and keep them unchanged across releases.
const (
	errProcessInputs  = "Failed to process Step inputs"
	errInstallMaestro = "Failed to install the Maestro CLI"
	errInstallApp     = "Failed to install the app"
	errFlowsFailed    = "Maestro flows failed"
	errMaestroFailed  = "Maestro failed without a failing flow"
	errExportOutputs  = "Failed to export Step outputs"
)

type Input struct {
	FlowPath       string `env:"flow_path,required"`
	AppPath        string `env:"app_path,required"`
	IncludeTags    string `env:"include_tags"`
	ExcludeTags    string `env:"exclude_tags"`
	AdditionalArgs string `env:"additional_args"`
	TestName       string `env:"test_name,required"`
	FlakyAttempts  int    `env:"num_flaky_test_attempts,range[0..10]"`
	Quarantined    string `env:"quarantined_tests"`
	MaestroVersion string `env:"maestro_version,required"`
	TestResultDir  string `env:"bitrise_test_result_dir,dir"`
	DeployDir      string `env:"BITRISE_DEPLOY_DIR"`
	AndroidHome    string `env:"ANDROID_HOME"`
	AndroidSDKRoot string `env:"ANDROID_SDK_ROOT"`
}

type Config struct {
	FlowPaths      []string
	App            App
	IncludeTags    []string
	ExcludeTags    []string
	AdditionalArgs []string
	// WorkspaceConfig is passed with --config, as the retries pass flow files and Maestro looks for config.yaml only in
	// folders. It is the flow folder's config.yaml, or the copy that skips the quarantined flows. Empty when
	// additional_args has --config.
	WorkspaceConfig string
	TestName        string
	FlakyAttempts   int
	Quarantined     []testquarantine.QuarantinedTest
	MaestroVersion  string
	TestResultDir   string
	DeployDir       string
	AndroidSDKDir   string
}

type Result struct {
	WorkDir  string
	Attempts []Attempt
}

type Exporter interface {
	ExportOutput(key, value string) error
	ExportOutputFilesZip(key string, sourcePaths []string, zipPath string) error
}

type Step struct {
	logger         log.Logger
	inputParser    stepconf.InputParser
	commandFactory command.Factory
	installer      Installer
	exporter       Exporter
	fileManager    fileutil.FileManager
}

func New(
	logger log.Logger,
	inputParser stepconf.InputParser,
	commandFactory command.Factory,
	installer Installer,
	exporter Exporter,
	fileManager fileutil.FileManager,
) Step {
	return Step{
		logger:         logger,
		inputParser:    inputParser,
		commandFactory: commandFactory,
		installer:      installer,
		exporter:       exporter,
		fileManager:    fileManager,
	}
}

func (s Step) ProcessConfig() (Config, error) {
	var input Input
	if err := s.inputParser.Parse(&input); err != nil {
		return Config{}, fmt.Errorf("%s: %w", errProcessInputs, err)
	}
	stepconf.Print(input)
	s.logger.Println()

	config, err := configFromInput(input)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", errProcessInputs, err)
	}
	return config, nil
}

func configFromInput(input Input) (Config, error) {
	flowPaths := splitLines(input.FlowPath)
	if len(flowPaths) == 0 {
		return Config{}, fmt.Errorf("flow_path: no flow file or folder given")
	}

	additionalArgs, err := splitArgs(input.AdditionalArgs)
	if err != nil {
		return Config{}, fmt.Errorf("additional_args: %w", err)
	}

	app, err := parseApp(input.AppPath)
	if err != nil {
		return Config{}, err
	}

	maestroVersion, err := resolveMaestroVersion(input.MaestroVersion)
	if err != nil {
		return Config{}, err
	}

	quarantined, err := testquarantine.ParseQuarantinedTests(input.Quarantined)
	if err != nil {
		return Config{}, fmt.Errorf("quarantined_tests: %w", err)
	}

	return Config{
		FlowPaths:       flowPaths,
		App:             app,
		IncludeTags:     splitList(input.IncludeTags),
		ExcludeTags:     splitList(input.ExcludeTags),
		AdditionalArgs:  additionalArgs,
		WorkspaceConfig: workspaceConfigPath(flowPaths, additionalArgs),
		TestName:        input.TestName,
		FlakyAttempts:   input.FlakyAttempts,
		Quarantined:     quarantined,
		MaestroVersion:  maestroVersion,
		TestResultDir:   input.TestResultDir,
		DeployDir:       input.DeployDir,
		AndroidSDKDir:   findAndroidSDKDir(input.AndroidHome, input.AndroidSDKRoot),
	}, nil
}

func (s Step) InstallDependencies(config Config) (Installation, error) {
	s.logger.Infof("Installing Maestro CLI %s", config.MaestroVersion)
	installation, err := s.installer.Install(config.MaestroVersion)
	if err != nil {
		return Installation{}, fmt.Errorf("%s: %w", errInstallMaestro, err)
	}
	return installation, nil
}

func (s Step) Run(config Config, installation Installation) (Result, error) {
	if err := s.installApp(config); err != nil {
		return Result{}, err
	}

	workDir, err := os.MkdirTemp("", "maestro-test")
	if err != nil {
		return Result{}, err
	}
	result := Result{WorkDir: workDir}

	if config, err = s.skipQuarantinedFlows(config, workDir); err != nil {
		s.logger.Warnf("Quarantined flows are not skipped: %s", err)
	}
	if len(config.FlowPaths) == 0 {
		s.logger.Println()
		s.logger.Donef("Every flow is quarantined, Maestro does not run")
		return result, nil
	}
	if config.FlakyAttempts > 0 && ordersFlows(workspaceConfig(config)) {
		s.logger.Println()
		s.logger.Warnf("The workspace config orders the flows with executionOrder, so failed flows do not run again.")
		config.FlakyAttempts = 0
	}

	flowPaths := config.FlowPaths
	var runErr error
	for number := 1; ; number++ {
		attempt, err := newAttempt(workDir, number)
		if err != nil {
			return result, err
		}
		result.Attempts = append(result.Attempts, attempt)

		s.logger.Println()
		if number == 1 {
			s.logger.Infof("Running Maestro flows")
		} else {
			s.logger.Infof("Attempt %d of %d: running the failed flows again", number, config.FlakyAttempts+1)
		}
		runErr = s.runMaestro(installation, testArgs(config, attempt, flowPaths))

		if runErr == nil || number > config.FlakyAttempts {
			break
		}
		if flowPaths = s.flowFilesToRerun(attempt); len(flowPaths) == 0 {
			break
		}
	}

	if flaky := flakyFlows(readReports(result.Attempts)); len(flaky) > 0 {
		s.logger.Println()
		s.logger.Donef("Passed on a retry, reported as flaky: %s", strings.Join(flaky, ", "))
	}
	return result, flowsError(result.Attempts, runErr)
}

func (s Step) runMaestro(installation Installation, args []string) error {
	cmd := s.commandFactory.Create(installation.BinaryPath, args, &command.Opts{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Env:    []string{disableAnalyticsEnv, disableUpdateCheckEnv},
	})
	s.logger.TDonef("$ %s", cmd.PrintableCommandArgs())
	return cmd.Run()
}

// skipQuarantinedFlows leaves the quarantined flows out of the run. A flow file given in flow_path is left out of the
// command, a flow found in a folder is excluded by a copy of the workspace config. When every flow is quarantined it
// returns no flow paths, as Maestro fails when its patterns match no flow. On an error it returns the config unchanged.
func (s Step) skipQuarantinedFlows(config Config, workDir string) (Config, error) {
	if len(config.Quarantined) == 0 {
		return config, nil
	}
	baseConfig := workspaceConfig(config)
	flows, err := findFlowFiles(config.FlowPaths, baseConfig)
	if err != nil {
		return config, err
	}
	quarantined := quarantinedFlows(config.Quarantined, flows)
	if len(quarantined) == 0 {
		return config, nil
	}

	s.logger.Println()
	s.logger.Infof("Skipping quarantined flows")
	skipped := config
	skipped.FlowPaths = slices.DeleteFunc(slices.Clone(config.FlowPaths), func(flowPath string) bool {
		return slices.ContainsFunc(quarantined, func(flow flowFile) bool { return flow.Folder == "" && flow.Path == flowPath })
	})
	inFolders := false
	for _, flow := range quarantined {
		s.logger.Printf("- %s (%s)", flow.Name, flow.Path)
		inFolders = inFolders || flow.Folder != ""
	}
	var alsoSkipped []flowFile
	if excludesFlowsByPattern(config.MaestroVersion) {
		alsoSkipped = patternMatchedFlows(quarantined, flows)
	}
	for _, flow := range alsoSkipped {
		s.logger.Warnf("%s (%s) is not quarantined, but it is skipped too: the pattern that skips a quarantined flow matches it in another flow_path folder.", flow.Name, flow.Path)
	}
	if len(quarantined)+len(alsoSkipped) == len(flows) {
		skipped.FlowPaths = nil
		return skipped, nil
	}
	if inFolders {
		if !excludesFlowsByPattern(config.MaestroVersion) {
			s.logger.Warnf("Maestro %s cannot exclude flows found in folders, so these quarantined flows still run. Use Maestro %s or newer to skip them.", config.MaestroVersion, flowExclusionMaestroVersion)
		}
		dst := filepath.Join(workDir, "quarantine-config.yaml")
		if err := writeQuarantineConfig(baseConfig, dst, quarantined); err != nil {
			return config, err
		}
		skipped.WorkspaceConfig = dst
		skipped.AdditionalArgs = withoutConfigArg(config.AdditionalArgs)
	}
	return skipped, nil
}

func (s Step) flowFilesToRerun(attempt Attempt) []string {
	report, err := readReport(attempt.JUnitPath)
	if err != nil {
		return nil
	}
	files, withoutFile := failedFlowFiles(report)
	for _, name := range withoutFile {
		s.logger.Warnf("Flow %q failed, but the report does not name its file, so it is not run again. Maestro writes the file from 2.6.0.", name)
	}
	return files
}

// flowsError tells failing flows, which are the customer's tests, apart from Maestro failing for any other reason.
// Maestro writes no report when, for example, a flow file does not parse, and a run without a report is reported as
// Maestro failing, also when an earlier attempt has failed flows. Each flow counts by its last run.
func flowsError(attempts []Attempt, runErr error) error {
	if runErr != nil && len(attempts) > 0 {
		if _, err := readReport(attempts[len(attempts)-1].JUnitPath); err != nil {
			return maestroError(len(attempts), runErr)
		}
	}

	total, failed := 0, 0
	for _, testCase := range lastRuns(readReports(attempts)) {
		total++
		if flowFailed(testCase) {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%s: %w", errFlowsFailed, fmt.Errorf("%d of %d flows failed", failed, total))
	}
	if runErr != nil {
		return maestroError(len(attempts), runErr)
	}
	return nil
}

// maestroError names the attempt when the Step ran Maestro more than once.
func maestroError(attempts int, runErr error) error {
	if attempts > 1 {
		runErr = fmt.Errorf("attempt %d: %w", attempts, runErr)
	}
	return fmt.Errorf("%s: %w", errMaestroFailed, runErr)
}

func (s Step) ExportOutputs(config Config, result Result) error {
	s.logger.Println()
	s.logger.Infof("Exporting outputs")

	if len(result.Attempts) > 0 {
		if err := s.exportReport(config, result); err != nil {
			return fmt.Errorf("%s: %w", errExportOutputs, err)
		}
	}

	var outputDirs []string
	for _, attempt := range result.Attempts {
		if exists(attempt.TestOutputDir) {
			outputDirs = append(outputDirs, filepath.Dir(attempt.TestOutputDir))
		}
	}
	if len(outputDirs) > 0 && config.DeployDir != "" {
		zipPath := filepath.Join(config.DeployDir, testOutputZipFileName)
		if err := s.exporter.ExportOutputFilesZip(testOutputZipOutputKey, outputDirs, zipPath); err != nil {
			s.logger.Warnf("Failed to export Maestro test output: %s", err)
		} else {
			s.logger.Donef("%s: %s", testOutputZipOutputKey, zipPath)
		}
	}

	return nil
}

func (s Step) exportReport(config Config, result Result) error {
	report, linked, ok := s.mergedReport(result.Attempts)
	if !ok {
		s.logger.Warnf("No JUnit report to export")
		return nil
	}

	junitPath := filepath.Join(result.WorkDir, junitReportFileName)
	if err := writeJUnitReport(report, junitPath); err != nil {
		return err
	}
	if err := s.exporter.ExportOutput(junitPathOutputKey, junitPath); err != nil {
		return err
	}
	s.logger.Donef("%s: %s", junitPathOutputKey, junitPath)

	s.exportTestResults(config, junitPath, linked)
	return nil
}

func (s Step) mergedReport(attempts []Attempt) (testreport.TestReport, map[string]flowBundle, bool) {
	var reports []testreport.TestReport
	linked := map[string]flowBundle{}
	for _, attempt := range attempts {
		report, err := readReport(attempt.JUnitPath)
		if err != nil {
			if exists(attempt.JUnitPath) {
				s.logger.Warnf("The JUnit report of %s is not exported: %s", attempt.Name, err)
			}
			continue
		}

		if bundles, err := findFlowBundles(attempt.TestOutputDir, attempt.Name); err != nil {
			s.logger.Warnf("Test attachments of %s are not exported: %s", attempt.Name, err)
		} else {
			link := linkAttachments(&report, bundles)
			for _, warning := range link.Warnings {
				s.logger.Warnf("%s", warning)
			}
			maps.Copy(linked, link.Linked)
		}
		reports = append(reports, report)
	}
	if len(reports) == 0 {
		return testreport.TestReport{}, nil, false
	}
	return mergeReports(reports), linked, true
}

func (s Step) installApp(config Config) error {
	device, err := s.findSingleDevice(config)
	if err != nil {
		return fmt.Errorf("%s: %w", errInstallApp, err)
	}

	name, args := installAppCommand(config.App, device.ID, config.AndroidSDKDir)
	cmd := s.commandFactory.Create(name, args, &command.Opts{Stdout: os.Stdout, Stderr: os.Stderr})
	s.logger.Println()
	s.logger.Infof("Installing app")
	s.logger.TDonef("$ %s", cmd.PrintableCommandArgs())
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", errInstallApp, fmt.Errorf("%s: %w", config.App.Path, err))
	}
	return nil
}

func (s Step) findSingleDevice(config Config) (runningDevice, error) {
	name, args := runningDevicesCommand(config.App.Platform, config.AndroidSDKDir)
	output, err := s.commandFactory.Create(name, args, nil).RunAndReturnTrimmedOutput()
	if err != nil {
		return runningDevice{}, fmt.Errorf("list running devices: %w", err)
	}
	devices, err := parseRunningDevices(config.App.Platform, output)
	if err != nil {
		return runningDevice{}, err
	}
	return singleDevice(config.App.Platform, devices)
}

func exists(pth string) bool {
	if pth == "" {
		return false
	}
	_, err := os.Stat(pth)
	return err == nil
}

// exportTestResults writes the report in the layout the Deploy to Bitrise.io Step picks up from
// $BITRISE_TEST_RESULT_DIR: the report and a test-info.json naming the run, plus the attachments.
func (s Step) exportTestResults(config Config, junitPath string, linked map[string]flowBundle) {
	testRun := testRunName(config.TestName)
	testRunDir := filepath.Join(config.TestResultDir, testRun)
	if err := s.writeTestInfo(testRunDir, testRun); err != nil {
		s.logger.Warnf("Failed to export test results: %s", err)
		return
	}
	if err := s.fileManager.CopyFile(junitPath, filepath.Join(testRunDir, junitReportFileName), &fileutil.CopyOptions{Overwrite: true}); err != nil {
		s.logger.Warnf("Failed to export test results: %s", err)
		return
	}

	attached := 0
	for _, folder := range slices.Sorted(maps.Keys(linked)) {
		bundle := linked[folder]
		for _, file := range bundle.Files {
			dst := filepath.Join(testRunDir, folder, file)
			if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
				s.logger.Warnf("Failed to export test attachment %s: %s", file, err)
				continue
			}
			if err := s.fileManager.CopyFile(filepath.Join(bundle.Dir, file), dst, &fileutil.CopyOptions{Overwrite: true}); err != nil {
				s.logger.Warnf("Failed to export test attachment %s: %s", file, err)
				continue
			}
			attached++
		}
	}

	s.logger.Donef("Test results exported as %q with %d attachments", testRun, attached)
}

// testRunName names both the test run folder and the run in test-info.json. The deploy step reads only one folder
// level below the test result dir, and finds the attachments' names by looking for the run's name in their paths, so
// the two have to match.
func testRunName(testName string) string {
	return strings.NewReplacer("/", "-", ":", "-").Replace(testName)
}

func (s Step) writeTestInfo(testRunDir, testName string) error {
	if err := os.MkdirAll(testRunDir, 0755); err != nil {
		return err
	}
	testInfo, err := json.Marshal(struct {
		TestName string `json:"test-name"`
	}{testName})
	if err != nil {
		return err
	}
	return s.fileManager.WriteBytes(filepath.Join(testRunDir, "test-info.json"), testInfo)
}
