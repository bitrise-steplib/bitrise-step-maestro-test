package step

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/bitrise-io/go-android/v2/testresult/junitxml"
	"github.com/bitrise-io/go-steputils/v2/stepconf"
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
	TestName       string
	MaestroVersion string
	TestResultDir  string
	DeployDir      string
	AndroidSDKDir  string
}

type Result struct {
	JUnitPath     string
	TestOutputDir string
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

	return Config{
		FlowPaths:      flowPaths,
		App:            app,
		IncludeTags:    splitList(input.IncludeTags),
		ExcludeTags:    splitList(input.ExcludeTags),
		AdditionalArgs: additionalArgs,
		TestName:       input.TestName,
		MaestroVersion: maestroVersion,
		TestResultDir:  input.TestResultDir,
		DeployDir:      input.DeployDir,
		AndroidSDKDir:  findAndroidSDKDir(input.AndroidHome, input.AndroidSDKRoot),
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
	result := Result{
		JUnitPath:     filepath.Join(workDir, junitReportFileName),
		TestOutputDir: filepath.Join(workDir, testOutputDirectoryName),
	}

	args := testArgs(config, result)
	cmd := s.commandFactory.Create(installation.BinaryPath, args, &command.Opts{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Env:    []string{disableAnalyticsEnv, disableUpdateCheckEnv},
	})

	s.logger.Println()
	s.logger.Infof("Running Maestro flows")
	s.logger.TDonef("$ %s", cmd.PrintableCommandArgs())

	if err := cmd.Run(); err != nil {
		return result, flowsError(result.JUnitPath, err)
	}
	return result, nil
}

// flowsError tells failing flows, which are the customer's tests, apart from Maestro failing for any other reason.
// Maestro writes no report when, for example, a flow file does not parse.
func flowsError(junitPath string, runErr error) error {
	total, failed, err := countFlows(junitPath)
	if err != nil || failed == 0 {
		return fmt.Errorf("%s: %w", errMaestroFailed, runErr)
	}
	return fmt.Errorf("%s: %w", errFlowsFailed, fmt.Errorf("%d of %d flows failed", failed, total))
}

func countFlows(junitPath string) (total, failed int, err error) {
	if !exists(junitPath) {
		return 0, 0, fmt.Errorf("no JUnit report at %s", junitPath)
	}
	converter := junitxml.Converter{}
	converter.Detect([]string{junitPath})
	report, err := converter.Convert()
	if err != nil {
		return 0, 0, err
	}
	for _, suite := range report.TestSuites {
		for _, testCase := range suite.TestCases {
			total++
			if testCase.Failure != nil || testCase.Error != nil {
				failed++
			}
		}
	}
	return total, failed, nil
}

func (s Step) ExportOutputs(config Config, result Result) error {
	s.logger.Println()
	s.logger.Infof("Exporting outputs")

	if exists(result.JUnitPath) {
		if err := s.exporter.ExportOutput(junitPathOutputKey, result.JUnitPath); err != nil {
			return fmt.Errorf("%s: %w", errExportOutputs, err)
		}
		s.logger.Donef("%s: %s", junitPathOutputKey, result.JUnitPath)

		s.exportTestResults(config, result)
	} else if result.JUnitPath != "" {
		s.logger.Warnf("Maestro did not produce a JUnit report at %s", result.JUnitPath)
	}

	if exists(result.TestOutputDir) && config.DeployDir != "" {
		zipPath := filepath.Join(config.DeployDir, testOutputZipFileName)
		if err := s.exporter.ExportOutputFilesZip(testOutputZipOutputKey, []string{result.TestOutputDir}, zipPath); err != nil {
			s.logger.Warnf("Failed to export Maestro test output: %s", err)
		} else {
			s.logger.Donef("%s: %s", testOutputZipOutputKey, zipPath)
		}
	}

	return nil
}

func (s Step) installApp(config Config) error {
	if err := s.checkSingleDevice(config); err != nil {
		return fmt.Errorf("%s: %w", errInstallApp, err)
	}

	name, args := installAppCommand(config.App, config.AndroidSDKDir)
	cmd := s.commandFactory.Create(name, args, &command.Opts{Stdout: os.Stdout, Stderr: os.Stderr})
	s.logger.Println()
	s.logger.Infof("Installing app")
	s.logger.TDonef("$ %s", cmd.PrintableCommandArgs())
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", errInstallApp, fmt.Errorf("%s: %w", config.App.Path, err))
	}
	return nil
}

func (s Step) checkSingleDevice(config Config) error {
	name, args := runningDevicesCommand(config.App.Platform, config.AndroidSDKDir)
	output, err := s.commandFactory.Create(name, args, nil).RunAndReturnTrimmedOutput()
	if err != nil {
		return fmt.Errorf("list running devices: %w", err)
	}
	devices, err := parseRunningDevices(config.App.Platform, output)
	if err != nil {
		return err
	}
	return singleDeviceError(config.App.Platform, devices)
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
func (s Step) exportTestResults(config Config, result Result) {
	testRunDir := filepath.Join(config.TestResultDir, config.TestName)
	if err := s.writeTestInfo(testRunDir, config.TestName); err != nil {
		s.logger.Warnf("Failed to export test results: %s", err)
		return
	}

	reportPath := filepath.Join(testRunDir, filepath.Base(result.JUnitPath))
	linked, err := s.writeLinkedReport(result, reportPath)
	if err != nil {
		s.logger.Warnf("Test attachments are not exported: %s", err)
	}
	if len(linked) == 0 {
		if err := s.fileManager.CopyFile(result.JUnitPath, reportPath, &fileutil.CopyOptions{Overwrite: true}); err != nil {
			s.logger.Warnf("Failed to export test results: %s", err)
			return
		}
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

	s.logger.Donef("Test results exported as %q with %d attachments", config.TestName, attached)
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

// writeLinkedReport writes the JUnit report to reportPath with the flows' Maestro output linked to their test
// cases, and returns the linked flow folders. It writes nothing when there is nothing to link.
func (s Step) writeLinkedReport(result Result, reportPath string) (map[string]flowBundle, error) {
	converter := junitxml.Converter{}
	converter.Detect([]string{result.JUnitPath})
	report, err := converter.Convert()
	if err != nil {
		return nil, fmt.Errorf("parse JUnit report: %w", err)
	}

	bundles, err := findFlowBundles(result.TestOutputDir)
	if err != nil {
		return nil, err
	}

	link := linkAttachments(&report, bundles)
	for _, warning := range link.Warnings {
		s.logger.Warnf("%s", warning)
	}
	if len(link.Linked) == 0 {
		return nil, nil
	}

	data, err := xml.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append([]byte(xml.Header), data...)
	if err := os.WriteFile(reportPath, data, 0644); err != nil {
		return nil, err
	}
	return link.Linked, nil
}
