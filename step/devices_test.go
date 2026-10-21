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
	assert.Equal(t, []runningDevice{{ID: "emulator-5554", Name: "emulator-5554"}, {ID: "emulator-5558", Name: "emulator-5558"}}, parseADBDevices(output))
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
	assert.Equal(t, []runningDevice{{ID: "A4199825", Name: "Bitrise iOS default (A4199825)"}}, simulators)

	_, err = parseBootedSimulators("not json")
	require.Error(t, err)
}

func TestParseBootedSimulators_OnlyIOS(t *testing.T) {
	watch := `"com.apple.CoreSimulator.SimRuntime.watchOS-26-4": [{"udid": "WATCH", "name": "Apple Watch Series 11", "state": "Booted"}]`

	simulators, err := parseBootedSimulators(`{"devices": {
		"com.apple.CoreSimulator.SimRuntime.iOS-26-4": [{"udid": "PHONE", "name": "iPhone 17", "state": "Booted"}],
		` + watch + `
	}}`)
	require.NoError(t, err)
	assert.Equal(t, []runningDevice{{ID: "PHONE", Name: "iPhone 17 (PHONE)"}}, simulators, "an iPhone plus a booted Watch")

	simulators, err = parseBootedSimulators(`{"devices": {` + watch + `}}`)
	require.NoError(t, err)
	assert.Empty(t, simulators, "a Watch alone")
}

func TestSingleDevice(t *testing.T) {
	emulator := runningDevice{ID: "emulator-5554", Name: "emulator-5554"}
	device, err := singleDevice(PlatformAndroid, []runningDevice{emulator})
	require.NoError(t, err)
	assert.Equal(t, emulator, device)

	_, err = singleDevice(PlatformAndroid, nil)
	assert.ErrorContains(t, err, "AVD Manager")
	_, err = singleDevice(PlatformIOS, nil)
	assert.ErrorContains(t, err, "Xcode Start Simulator")
	_, err = singleDevice(PlatformAndroid, []runningDevice{emulator, {ID: "emulator-5556", Name: "emulator-5556"}})
	assert.ErrorContains(t, err, "2 Android devices are running (emulator-5554, emulator-5556)")
	_, err = singleDevice(PlatformIOS, []runningDevice{{ID: "1", Name: "A (1)"}, {ID: "2", Name: "B (2)"}})
	assert.ErrorContains(t, err, "2 iOS simulators are booted (A (1), B (2))")
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
