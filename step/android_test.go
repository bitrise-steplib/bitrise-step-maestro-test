package step

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunningDevices_UsesTheSDKAdb(t *testing.T) {
	androidHome := t.TempDir()
	writeExecutable(t, filepath.Join(androidHome, "platform-tools", "adb"), "printf 'List of devices attached\\nemulator-5554\\tdevice\\n'\n")
	fakeOnPath(t, "adb", "printf 'List of devices attached\\n'\n")

	running, err := testAndroidDevices(androidHome).runningDevices()
	require.NoError(t, err)
	assert.Equal(t, []string{"emulator-5554"}, running)
}

func TestInstallAppCommand_UsesTheSDKAdb(t *testing.T) {
	androidHome := t.TempDir()
	sdkAdb := filepath.Join(androidHome, "platform-tools", "adb")
	writeExecutable(t, sdkAdb, "")

	name, _ := installAppCommand(App{Path: "app.apk", Platform: PlatformAndroid}, "emulator-5554", androidHome)
	assert.Equal(t, sdkAdb, name)

	name, _ = installAppCommand(App{Path: "app.apk", Platform: PlatformAndroid}, "emulator-5554", t.TempDir())
	assert.Equal(t, "adb", name, "adb on PATH when the SDK has none")
}

func TestAcquire_WithoutSDK(t *testing.T) {
	_, err := testAndroidDevices("").acquire()
	require.ErrorContains(t, err, "ANDROID_HOME")
}

func TestStartEmulator_ExitsEarly(t *testing.T) {
	shortenEmulatorTimings(t)
	deployDir := t.TempDir()
	devices := emulatorDevices(t, "echo 'PANIC: broken AVD'\nexit 1\n", "printf 'List of devices attached\\n'\n")
	devices.deployDir = deployDir

	_, err := devices.startEmulator()
	require.ErrorContains(t, err, "exited before it came online")
	require.ErrorContains(t, err, deployDir, "the log is in the deploy directory, so it can be read after the build")

	logs, err := filepath.Glob(filepath.Join(deployDir, "emulator-*.log"))
	require.NoError(t, err)
	require.Len(t, logs, 1)
	content, err := os.ReadFile(logs[0])
	require.NoError(t, err)
	assert.Contains(t, string(content), "PANIC: broken AVD")
}

func TestStartEmulator_ComesOnline(t *testing.T) {
	shortenEmulatorTimings(t)
	devices := emulatorDevices(t, "sleep 30\n", "printf 'List of devices attached\\nemulator-5554\\toffline\\n'\n")

	emulator, err := devices.startEmulator()
	require.NoError(t, err)
	t.Cleanup(func() { _ = emulator.cmd.Kill() })
	assert.Equal(t, "emulator-5554", emulator.serial)
}

func TestStartEmulator_NeverComesOnline(t *testing.T) {
	shortenEmulatorTimings(t)
	devices := emulatorDevices(t, "sleep 30\n", "printf 'List of devices attached\\n'\n")

	_, err := devices.startEmulator()
	require.ErrorContains(t, err, "did not come online")
}

func TestStopEmulator_EmuKill(t *testing.T) {
	shortenEmulatorTimings(t)
	emulatorStopTimeout = time.Minute
	killed := filepath.Join(t.TempDir(), "killed")
	devices := emulatorDevices(t,
		"while [ ! -f '"+killed+"' ]; do sleep 0.05; done\n",
		"if [ \"$3\" = emu ]; then touch '"+killed+"'; exit 0; fi\nprintf 'List of devices attached\\nemulator-5554\\tdevice\\n'\n",
	)
	emulator, err := devices.startEmulator()
	require.NoError(t, err)

	start := time.Now()
	emulator.stop(devices.logger, devices.commandFactory, devices.adb)
	assert.Less(t, time.Since(start), 10*time.Second, "adb emu kill stops the emulator without waiting for the kill fallback")
	assertExited(t, emulator)
}

func TestStopEmulator_KillsAnEmulatorThatIgnoresEmuKill(t *testing.T) {
	shortenEmulatorTimings(t)
	devices := emulatorDevices(t, "sleep 30\n", "printf 'List of devices attached\\nemulator-5554\\tdevice\\n'\n")
	emulator, err := devices.startEmulator()
	require.NoError(t, err)

	emulator.stop(devices.logger, devices.commandFactory, devices.adb)
	assertExited(t, emulator)
}

// emulatorDevices fakes the emulator binary in a fresh SDK and adb on PATH, as the SDK has no platform-tools.
func emulatorDevices(t *testing.T, emulatorScript, adbScript string) androidDevices {
	androidHome := t.TempDir()
	writeExecutable(t, filepath.Join(androidHome, "emulator", "emulator"), emulatorScript)
	fakeOnPath(t, "adb", adbScript)
	return testAndroidDevices(androidHome)
}

func shortenEmulatorTimings(t *testing.T) {
	bootTimeout, checkInterval, stopTimeout := androidBootTimeout, emulatorCheckInterval, emulatorStopTimeout
	androidBootTimeout, emulatorCheckInterval, emulatorStopTimeout = 500*time.Millisecond, 10*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() {
		androidBootTimeout, emulatorCheckInterval, emulatorStopTimeout = bootTimeout, checkInterval, stopTimeout
	})
}

func assertExited(t *testing.T, emulator runningEmulator) {
	select {
	case <-emulator.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the emulator process is still running")
	}
}

func testAndroidDevices(androidHome string) androidDevices {
	return newAndroidDevices(log.NewLogger(), command.NewFactory(env.NewRepository()), androidHome, "", "")
}

func writeExecutable(t *testing.T, pth, script string) {
	writeFile(t, pth, "#!/bin/sh\n"+script)
	require.NoError(t, os.Chmod(pth, 0755))
}

// fakeOnPath puts a fake binary first on PATH for the rest of the test.
func fakeOnPath(t *testing.T, name, script string) {
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, name), script)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
