package step

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/errorutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_DisablesMaestroAnalyticsAndUpdateCheck(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "env.txt")
	fakeMaestro := filepath.Join(t.TempDir(), "maestro")
	writeFile(t, fakeMaestro, "#!/bin/sh\nenv > \""+envFile+"\"\n")
	require.NoError(t, os.Chmod(fakeMaestro, 0755))

	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())

	config := Config{FlowPaths: []string{".maestro"}, App: App{Path: "app.apk", Platform: PlatformAndroid}, AndroidSDKDir: fakeAndroidSDK(t, "#!/bin/sh\n")}
	_, err := s.Run(config, Installation{BinaryPath: fakeMaestro})
	require.NoError(t, err)

	maestroEnv, err := os.ReadFile(envFile)
	require.NoError(t, err)
	assert.Contains(t, string(maestroEnv), "MAESTRO_CLI_NO_ANALYTICS=true\n")
	assert.Contains(t, string(maestroEnv), "MAESTRO_DISABLE_UPDATE_CHECK=true\n")
	assert.Contains(t, string(maestroEnv), "PATH=", "the rest of the environment is passed through")
}

func TestConfigFromInput_FlowPaths(t *testing.T) {
	input := testInput()
	input.FlowPath = "maestro/entry\nmaestro/unlock\n"
	config, err := configFromInput(input)
	require.NoError(t, err)
	assert.Equal(t, []string{"maestro/entry", "maestro/unlock"}, config.FlowPaths)

	input.FlowPath = " \n "
	_, err = configFromInput(input)
	require.ErrorContains(t, err, "flow_path")
}

func TestConfigFromInput_AndroidSDKDir(t *testing.T) {
	sdkRoot, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	input := testInput()
	input.AndroidSDKRoot = sdkRoot
	config, err := configFromInput(input)
	require.NoError(t, err)
	assert.Equal(t, sdkRoot, config.AndroidSDKDir, "ANDROID_SDK_ROOT is used when ANDROID_HOME is unset")

	input.AndroidHome = filepath.Join(sdkRoot, "missing")
	config, err = configFromInput(input)
	require.NoError(t, err)
	assert.Equal(t, sdkRoot, config.AndroidSDKDir, "ANDROID_SDK_ROOT is used when ANDROID_HOME is not an SDK")

	config, err = configFromInput(testInput())
	require.NoError(t, err)
	assert.Empty(t, config.AndroidSDKDir, "a missing SDK only fails the steps that need it")
}

func testInput() Input {
	return Input{FlowPath: ".maestro", AppPath: "app.apk", TestName: "Maestro", MaestroVersion: "2.11.0"}
}

func TestPhaseErrors_FirstLineIsStable(t *testing.T) {
	cause := errors.New("cause with /tmp/run-specific/path")
	failingMaestro := filepath.Join(t.TempDir(), "maestro")
	writeFile(t, failingMaestro, "#!/bin/sh\nexit 1\n")
	require.NoError(t, os.Chmod(failingMaestro, 0755))
	app := App{Path: "/tmp/app.apk", Platform: PlatformAndroid}

	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())
	s.inputParser = fakeInputParser{err: cause}
	s.installer = fakeInstaller{err: cause}
	s.exporter = failingExporter{err: cause}

	_, configErr := s.ProcessConfig()
	_, installErr := s.InstallDependencies(Config{})
	_, appErr := s.Run(Config{App: app, AndroidSDKDir: fakeAndroidSDK(t, "#!/bin/sh\nexit 1\n")}, Installation{})
	_, flowsErr := s.Run(Config{FlowPaths: []string{".maestro"}, App: app, AndroidSDKDir: fakeAndroidSDK(t, "#!/bin/sh\n")}, Installation{BinaryPath: failingMaestro})
	exportErr := s.ExportOutputs(Config{}, Result{JUnitPath: writeReport(t, 1, 0)})

	assert.Equal(t, errProcessInputs+":", firstLine(configErr))
	assert.Equal(t, errInstallMaestro+":", firstLine(installErr))
	assert.Equal(t, errInstallApp+":", firstLine(appErr))
	assert.Equal(t, errMaestroFailed+":", firstLine(flowsErr))
	assert.Equal(t, errExportOutputs+":", firstLine(exportErr))
}

func TestFlowsError(t *testing.T) {
	runErr := errors.New("exit status 1")

	failing := flowsError(writeReport(t, 3, 2), runErr)
	assert.Equal(t, errFlowsFailed+":\n  2 of 3 flows failed", errorutil.FormattedError(failing))

	assert.Equal(t, errMaestroFailed+":", firstLine(flowsError(writeReport(t, 3, 0), runErr)), "no failing flow in the report")
	assert.Equal(t, errMaestroFailed+":", firstLine(flowsError(filepath.Join(t.TempDir(), "missing.xml"), runErr)), "no report")
}

func writeReport(t *testing.T, total, failed int) string {
	var cases strings.Builder
	for i := range total {
		if i < failed {
			fmt.Fprintf(&cases, `<testcase name="flow%d"><failure message="Element not found"/></testcase>`, i)
		} else {
			fmt.Fprintf(&cases, `<testcase name="flow%d"/>`, i)
		}
	}
	pth := filepath.Join(t.TempDir(), junitReportFileName)
	writeFile(t, pth, fmt.Sprintf(`<testsuites><testsuite name="Test Suite" tests="%d" failures="%d">%s</testsuite></testsuites>`, total, failed, cases.String()))
	return pth
}

func fakeAndroidSDK(t *testing.T, adbScript string) string {
	sdkDir := t.TempDir()
	adb := filepath.Join(sdkDir, "platform-tools", "adb")
	writeFile(t, adb, adbScript)
	require.NoError(t, os.Chmod(adb, 0755))
	return sdkDir
}

func firstLine(err error) string {
	if err == nil {
		return ""
	}
	return strings.SplitN(errorutil.FormattedError(err), "\n", 2)[0]
}

type fakeInputParser struct{ err error }

func (f fakeInputParser) Parse(any) error { return f.err }

type fakeInstaller struct{ err error }

func (f fakeInstaller) Install(string) (Installation, error) { return Installation{}, f.err }

type failingExporter struct {
	fakeExporter
	err error
}

func (f failingExporter) ExportOutput(string, string) error { return f.err }
