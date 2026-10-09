package main

import (
	"errors"
	"testing"

	"github.com/bitrise-io/go-utils/v2/exitcode"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"

	"github.com/bitrise-steplib/bitrise-step-maestro-test/step"
)

type fakeRunner struct {
	runErr   error
	calls    []string
	exported step.Result
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
	return step.Result{JUnitPath: "report.xml"}, f.runErr
}

func (f *fakeRunner) ExportOutputs(_ step.Config, result step.Result) error {
	f.calls = append(f.calls, "ExportOutputs")
	f.exported = result
	return nil
}

func TestRunPhases_ExportsOutputsWhenFlowsFail(t *testing.T) {
	r := &fakeRunner{runErr: errors.New("2 flows failed")}

	code := runPhases(r, log.NewLogger())

	assert.Equal(t, exitcode.Failure, code)
	assert.Equal(t, []string{"ProcessConfig", "InstallDependencies", "Run", "ExportOutputs"}, r.calls)
	assert.Equal(t, "report.xml", r.exported.JUnitPath)
}

func TestRunPhases_Success(t *testing.T) {
	r := &fakeRunner{}

	assert.Equal(t, exitcode.Success, runPhases(r, log.NewLogger()))
}
