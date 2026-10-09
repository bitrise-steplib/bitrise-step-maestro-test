package main

import (
	"os"

	"github.com/bitrise-io/go-steputils/v2/export"
	"github.com/bitrise-io/go-steputils/v2/stepconf"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/errorutil"
	"github.com/bitrise-io/go-utils/v2/exitcode"
	"github.com/bitrise-io/go-utils/v2/filedownloader"
	"github.com/bitrise-io/go-utils/v2/fileutil"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/bitrise-io/go-utils/v2/ziputil"

	"github.com/bitrise-steplib/bitrise-step-maestro-test/step"
)

type runner interface {
	ProcessConfig() (step.Config, error)
	InstallDependencies(config step.Config) (step.Installation, error)
	Run(config step.Config, installation step.Installation) (step.Result, error)
	ExportOutputs(config step.Config, result step.Result) error
}

func main() {
	os.Exit(int(run()))
}

func run() exitcode.ExitCode {
	logger := log.NewLogger()
	return runPhases(createStep(logger), logger)
}

func runPhases(r runner, logger log.Logger) exitcode.ExitCode {
	config, err := r.ProcessConfig()
	if err != nil {
		logger.Errorf("%s", errorutil.FormattedError(err))
		return exitcode.Failure
	}

	installation, err := r.InstallDependencies(config)
	if err != nil {
		logger.Errorf("%s", errorutil.FormattedError(err))
		return exitcode.Failure
	}

	result, runErr := r.Run(config, installation)

	if err := r.ExportOutputs(config, result); err != nil {
		logger.Errorf("%s", errorutil.FormattedError(err))
		return exitcode.Failure
	}

	if runErr != nil {
		logger.Errorf("%s", errorutil.FormattedError(runErr))
		return exitcode.Failure
	}

	return exitcode.Success
}

func createStep(logger log.Logger) step.Step {
	envRepo := env.NewRepository()
	commandFactory := command.NewFactory(envRepo)
	fileManager := fileutil.NewFileManager()
	pathChecker := pathutil.NewPathChecker()
	outputExporter := export.NewDefaultExporter(commandFactory)

	return step.New(
		logger,
		stepconf.NewInputParser(envRepo),
		commandFactory,
		step.NewInstaller(logger, filedownloader.NewDownloader(logger), ziputil.NewZipManager(pathChecker), pathChecker),
		&outputExporter,
		fileManager,
		step.NewDeviceManager(logger, commandFactory, envRepo),
	)
}
