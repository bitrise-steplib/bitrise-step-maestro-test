package step

import (
	"encoding/xml"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/bitrise-io/go-android/v2/testresult/junitxml"
	"github.com/bitrise-io/go-steputils/v2/stepconf"
	"github.com/bitrise-io/go-steputils/v2/testresultexport" //nolint:staticcheck // no non-deprecated exporter writes the test result dir layout yet
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

type Input struct {
	FlowPath       string `env:"flow_path,required"`
	AppPath        string `env:"app_path"`
	IncludeTags    string `env:"include_tags"`
	ExcludeTags    string `env:"exclude_tags"`
	AdditionalArgs string `env:"additional_args"`
	TestName       string `env:"test_name,required"`
	MaestroVersion string `env:"maestro_version"`
	ShutdownDevice bool   `env:"shutdown_device,opt[yes,no]"`
	TestResultDir  string `env:"bitrise_test_result_dir,dir"`
	DeployDir      string `env:"BITRISE_DEPLOY_DIR"`
	AndroidHome    string `env:"ANDROID_HOME"`
}

type Config struct {
	FlowPaths      []string
	App            App
	Platform       Platform
	ManageDevice   bool
	ShutdownDevice bool
	IncludeTags    []string
	ExcludeTags    []string
	AdditionalArgs []string
	TestName       string
	MaestroVersion maestroVersion
	TestResultDir  string
	DeployDir      string
	AndroidHome    string
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
	devices        DeviceManager
}

func New(
	logger log.Logger,
	inputParser stepconf.InputParser,
	commandFactory command.Factory,
	installer Installer,
	exporter Exporter,
	fileManager fileutil.FileManager,
	devices DeviceManager,
) Step {
	return Step{
		logger:         logger,
		inputParser:    inputParser,
		commandFactory: commandFactory,
		installer:      installer,
		exporter:       exporter,
		fileManager:    fileManager,
		devices:        devices,
	}
}

func (s Step) ProcessConfig() (Config, error) {
	var input Input
	if err := s.inputParser.Parse(&input); err != nil {
		return Config{}, err
	}
	stepconf.Print(input)
	s.logger.Println()

	return configFromInput(input)
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

	app, err := resolveApp(input.AppPath, runtime.GOOS)
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
		Platform:       resolvePlatform(app, runtime.GOOS),
		ManageDevice:   !hasDeviceArg(additionalArgs),
		ShutdownDevice: input.ShutdownDevice,
		IncludeTags:    splitList(input.IncludeTags),
		ExcludeTags:    splitList(input.ExcludeTags),
		AdditionalArgs: additionalArgs,
		TestName:       input.TestName,
		MaestroVersion: maestroVersion,
		TestResultDir:  input.TestResultDir,
		DeployDir:      input.DeployDir,
		AndroidHome:    input.AndroidHome,
	}, nil
}

func (s Step) InstallDependencies(config Config) (Installation, error) {
	s.logger.Infof("Installing Maestro CLI %s", config.MaestroVersion.Version)
	if config.MaestroVersion.Overridden {
		s.logger.Warnf("Maestro CLI %s is not the version this Step release was tested with (%s).", config.MaestroVersion.Version, bundledMaestroVersion)
		s.logger.Warnf("Running flows should work, but test output handling may differ. Clear maestro_version to use the tested version.")
	}
	return s.installer.Install(config.MaestroVersion.Version)
}

func (s Step) Run(config Config, installation Installation) (Result, error) {
	var device Device
	if config.ManageDevice {
		s.logger.Println()
		s.logger.Infof("Preparing the %s device", config.Platform)
		var err error
		if device, err = s.devices.Acquire(config); err != nil {
			return Result{}, fmt.Errorf("prepare device: %w", err)
		}
	}
	if device.Release != nil {
		if config.ShutdownDevice {
			defer device.Release()
		} else if err := s.exporter.ExportOutput(device.HintEnv, device.HintValue); err != nil {
			s.logger.Warnf("Failed to export %s, shutting the device down at the end: %s", device.HintEnv, err)
			defer device.Release()
		} else {
			s.logger.Printf("Leaving the device running, %s: %s", device.HintEnv, device.HintValue)
		}
	}

	if err := s.installApp(config, device.ID); err != nil {
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

	args := testArgs(config, result, device.ID)
	cmd := s.commandFactory.Create(installation.BinaryPath, args, &command.Opts{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Env:    []string{disableAnalyticsEnv, disableUpdateCheckEnv},
	})

	s.logger.Println()
	s.logger.Infof("Running Maestro flows")
	s.logger.TDonef("$ %s", cmd.PrintableCommandArgs())

	if err := cmd.Run(); err != nil {
		return result, fmt.Errorf("maestro test failed: %w", err)
	}
	return result, nil
}

func (s Step) ExportOutputs(config Config, result Result) error {
	s.logger.Println()
	s.logger.Infof("Exporting outputs")

	if exists(result.JUnitPath) {
		if err := s.exporter.ExportOutput(junitPathOutputKey, result.JUnitPath); err != nil {
			return err
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

func (s Step) installApp(config Config, deviceID string) error {
	if config.App.Path == "" {
		return nil
	}

	name, args := installAppCommand(config.App, deviceID, config.AndroidHome)
	cmd := s.commandFactory.Create(name, args, &command.Opts{Stdout: os.Stdout, Stderr: os.Stderr})
	s.logger.Println()
	s.logger.Infof("Installing app")
	s.logger.TDonef("$ %s", cmd.PrintableCommandArgs())
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install app (%s): %w", config.App.Path, err)
	}
	return nil
}

func exists(pth string) bool {
	if pth == "" {
		return false
	}
	_, err := os.Stat(pth)
	return err == nil
}

func (s Step) exportTestResults(config Config, result Result) {
	reportPath := result.JUnitPath
	linked, linkedReportPath, err := s.reportWithAttachments(result)
	if err != nil {
		s.logger.Warnf("Test attachments are not exported: %s", err)
	} else if linkedReportPath != "" {
		reportPath = linkedReportPath
	}

	exporter := testresultexport.NewExporter(config.TestResultDir, s.fileManager)
	if err := exporter.ExportTest(config.TestName, reportPath); err != nil {
		s.logger.Warnf("Failed to export test results: %s", err)
		return
	}

	testRunDir := filepath.Join(config.TestResultDir, config.TestName)
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

// reportWithAttachments writes a copy of the JUnit report with the flows' Maestro output linked to their
// test cases. It returns an empty path when there is nothing to link.
func (s Step) reportWithAttachments(result Result) (map[string]flowBundle, string, error) {
	converter := junitxml.Converter{}
	converter.Detect([]string{result.JUnitPath})
	report, err := converter.Convert()
	if err != nil {
		return nil, "", fmt.Errorf("parse JUnit report: %w", err)
	}

	bundles, err := findFlowBundles(result.TestOutputDir)
	if err != nil {
		return nil, "", err
	}

	link := linkAttachments(&report, bundles)
	for _, warning := range link.Warnings {
		s.logger.Warnf("%s", warning)
	}
	if len(link.Linked) == 0 {
		return nil, "", nil
	}

	data, err := xml.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, "", err
	}
	data = append([]byte(xml.Header), data...)

	dir, err := os.MkdirTemp("", "maestro-report")
	if err != nil {
		return nil, "", err
	}
	reportPath := filepath.Join(dir, filepath.Base(result.JUnitPath))
	if err := os.WriteFile(reportPath, data, 0644); err != nil {
		return nil, "", err
	}
	return link.Linked, reportPath, nil
}
