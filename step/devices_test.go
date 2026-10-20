package step

import (
	"testing"

	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const oneDeviceADB = `#!/bin/sh
if [ "$1" = devices ]; then printf 'List of devices attached\nemulator-5554\tdevice\n\n'; fi
`

func TestParseADBDevices(t *testing.T) {
	output := "* daemon not running; starting now at tcp:5037\n* daemon started successfully\nList of devices attached\n" +
		"emulator-5554\tdevice\nemulator-5556\toffline\n0123456789ABCDEF\tunauthorized\nemulator-5558\tdevice\n"
	assert.Equal(t, []string{"emulator-5554", "emulator-5558"}, parseADBDevices(output))
	assert.Empty(t, parseADBDevices("List of devices attached\n"))
}

func TestParseBootedSimulators(t *testing.T) {
	output := `{"devices": {
		"com.apple.CoreSimulator.SimRuntime.iOS-26-0": [
			{"udid": "A4199825", "name": "Bitrise iOS default", "state": "Booted", "isAvailable": true},
			{"udid": "B1", "name": "iPhone 17", "state": "Shutdown", "isAvailable": true}
		],
		"com.apple.CoreSimulator.SimRuntime.watchOS-26-0": []
	}}`
	simulators, err := parseBootedSimulators(output)
	require.NoError(t, err)
	assert.Equal(t, []string{"Bitrise iOS default (A4199825)"}, simulators)

	_, err = parseBootedSimulators("not json")
	require.Error(t, err)
}

func TestSingleDeviceError(t *testing.T) {
	assert.NoError(t, singleDeviceError(PlatformAndroid, []string{"emulator-5554"}))
	assert.ErrorContains(t, singleDeviceError(PlatformAndroid, nil), "AVD Manager")
	assert.ErrorContains(t, singleDeviceError(PlatformIOS, nil), "Xcode Start Simulator")
	assert.ErrorContains(t, singleDeviceError(PlatformAndroid, []string{"emulator-5554", "emulator-5556"}),
		"2 Android devices are running (emulator-5554, emulator-5556)")
	assert.ErrorContains(t, singleDeviceError(PlatformIOS, []string{"A (1)", "B (2)"}), "2 iOS simulators are booted (A (1), B (2))")
}

func TestRun_FailsBeforeInstallWithSeveralDevices(t *testing.T) {
	installed := t.TempDir() + "/installed"
	sdkDir := fakeAndroidSDK(t, `#!/bin/sh
if [ "$1" = devices ]; then printf 'List of devices attached\nemulator-5554\tdevice\nemulator-5556\tdevice\n'; exit 0; fi
touch "`+installed+`"
`)

	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())
	_, err := s.Run(Config{App: App{Path: "app.apk", Platform: PlatformAndroid}, AndroidSDKDir: sdkDir}, Installation{})

	assert.Equal(t, errInstallApp+":", firstLine(err))
	assert.ErrorContains(t, err, "2 Android devices are running")
	assert.NoFileExists(t, installed)
}
