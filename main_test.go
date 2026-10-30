package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/bitrise-io/go-utils/v2/exitcode"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"

	"github.com/bitrise-steplib/bitrise-step-maestro-test/step"
)

type fakeRunner struct {
	runErr    error
	exportErr error
	calls     []string
	exported  step.Result
}

func (f *fakeRunner) ProcessConfig() (step.Config, error) {
	f.calls = append(f.calls, "ProcessConfig")
	return step.Config{}, nil
}

func (f *fakeRunner) InstallDependencies(step.Config) (step.Installation, error) {
	f.calls = append(f.calls, "InstallDependencies")
	return step.Installation{}, nil
}

func (f *fakeRunner) Run(step.Config, step.Installation) (step.Result, error) {
	f.calls = append(f.calls, "Run")
	return step.Result{WorkDir: "work"}, f.runErr
}

func (f *fakeRunner) ExportOutputs(_ step.Config, result step.Result) error {
	f.calls = append(f.calls, "ExportOutputs")
	f.exported = result
	return f.exportErr
}

type recordingLogger struct {
	log.Logger
	errors []string
}

func (r *recordingLogger) Errorf(format string, v ...interface{}) {
	r.errors = append(r.errors, fmt.Sprintf(format, v...))
}

func TestRunPhases_ExportsOutputsWhenFlowsFail(t *testing.T) {
	r := &fakeRunner{runErr: errors.New("2 flows failed")}

	code := runPhases(r, log.NewLogger())

	assert.Equal(t, exitcode.Failure, code)
	assert.Equal(t, []string{"ProcessConfig", "InstallDependencies", "Run", "ExportOutputs"}, r.calls)
	assert.Equal(t, "work", r.exported.WorkDir)
}

func TestRunPhases_LogsTheRunErrorLastWhenTheExportAlsoFails(t *testing.T) {
	r := &fakeRunner{runErr: errors.New("2 flows failed"), exportErr: errors.New("export failed")}
	logger := &recordingLogger{Logger: log.NewLogger()}

	code := runPhases(r, logger)

	assert.Equal(t, exitcode.Failure, code)
	assert.Equal(t, []string{"export failed", "2 flows failed"}, logger.errors, "the CLI reports the last error logged")
}

func TestRunPhases_ExportFailure(t *testing.T) {
	r := &fakeRunner{exportErr: errors.New("export failed")}

	assert.Equal(t, exitcode.Failure, runPhases(r, log.NewLogger()))
}

func TestRunPhases_Success(t *testing.T) {
	r := &fakeRunner{}

	assert.Equal(t, exitcode.Success, runPhases(r, log.NewLogger()))
}
