package step

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func TestExpectedDevices(t *testing.T) {
	emulator := runningDevice{ID: "emulator-5554", Name: "emulator-5554"}
	emulators := []runningDevice{emulator, {ID: "emulator-5556", Name: "emulator-5556"}, {ID: "emulator-5558", Name: "emulator-5558"}}
	simulators := []runningDevice{{ID: "1", Name: "A (1)"}, {ID: "2", Name: "B (2)"}}

	devices, err := expectedDevices(PlatformAndroid, []runningDevice{emulator}, 1)
	require.NoError(t, err)
	assert.Equal(t, []runningDevice{emulator}, devices)
	devices, err = expectedDevices(PlatformAndroid, emulators, 3)
	require.NoError(t, err)
	assert.Equal(t, emulators, devices)
	devices, err = expectedDevices(PlatformIOS, simulators, 2)
	require.NoError(t, err)
	assert.Equal(t, simulators, devices)

	for _, tc := range []struct {
		platform Platform
		devices  []runningDevice
		count    int
		want     string
	}{
		{PlatformAndroid, nil, 1, "AVD Manager"},
		{PlatformIOS, nil, 1, "Xcode Start Simulator"},
		{PlatformAndroid, emulators[:2], 1, "2 Android devices are running (emulator-5554, emulator-5556), the Step runs the flows on one device unless shard_split is set: set shard_split to 2"},
		{PlatformIOS, simulators, 1, "2 iOS simulators are booted (A (1), B (2))"},
		{PlatformAndroid, nil, 3, "shard_split is 3, but no Android emulator or device is running: start 3"},
		{PlatformIOS, nil, 2, "shard_split is 2, but no iOS simulator is booted: boot 2"},
		{PlatformAndroid, emulators[:2], 3, "shard_split is 3, but 2 Android devices are running (emulator-5554, emulator-5556): run exactly 3, or set shard_split to 2"},
		{PlatformAndroid, append(slices.Clone(emulators), runningDevice{ID: "emulator-5560", Name: "emulator-5560"}), 3, "shard_split is 3, but 4 Android devices are running"},
		{PlatformIOS, simulators[:1], 2, "shard_split is 2, but 1 iOS simulators are booted (A (1))"},
	} {
		_, err := expectedDevices(tc.platform, tc.devices, tc.count)
		assert.ErrorContains(t, err, tc.want)
	}
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

const twoDevicesADB = `#!/bin/sh
if [ "$1" = devices ]; then printf 'List of devices attached\nemulator-5554\tdevice\nemulator-5556\tdevice\n'; exit 0; fi
echo "$@" >> "$INSTALL_LOG"
`

func TestRun_FailsBeforeInstallWhenShardSplitDoesNotMatch(t *testing.T) {
	installLog := filepath.Join(t.TempDir(), "installs.txt")
	t.Setenv("INSTALL_LOG", installLog)

	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())
	_, err := s.Run(Config{App: App{Path: "app.apk", Platform: PlatformAndroid}, AndroidSDKDir: fakeAndroidSDK(t, twoDevicesADB), ShardSplit: 3}, Installation{})

	assert.Equal(t, errInstallApp+":", firstLine(err))
	assert.ErrorContains(t, err, "shard_split is 3, but 2 Android devices are running")
	assert.NoFileExists(t, installLog)
}

func TestRun_ShardSplitInstallsOnEveryDevice(t *testing.T) {
	installLog := filepath.Join(t.TempDir(), "installs.txt")
	t.Setenv("INSTALL_LOG", installLog)
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	fakeMaestro := filepath.Join(t.TempDir(), "maestro")
	writeFile(t, fakeMaestro, "#!/bin/sh\necho \"$@\" > \""+argsFile+"\"\n")
	require.NoError(t, os.Chmod(fakeMaestro, 0755))

	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())
	config := Config{FlowPaths: []string{".maestro"}, App: App{Path: "app.apk", Platform: PlatformAndroid}, AndroidSDKDir: fakeAndroidSDK(t, twoDevicesADB), ShardSplit: 2}
	_, err := s.Run(config, Installation{BinaryPath: fakeMaestro})
	require.NoError(t, err)

	installs, err := os.ReadFile(installLog)
	require.NoError(t, err)
	assert.Equal(t, "-s emulator-5554 install -r app.apk\n-s emulator-5556 install -r app.apk\n", string(installs))

	maestroArgs, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(maestroArgs), "--shard-split 2 --device emulator-5554,emulator-5556 "), string(maestroArgs))
}
