package step

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-xcode/v2/destination"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Preinstalled images from the stack reports of the Linux stacks.
var (
	nobleImages = []string{
		"system-images;android-34;google_apis;x86_64",
		"system-images;android-35;aosp_atd;x86_64",
		"system-images;android-35;google_apis;x86_64",
		"system-images;android-36;google_apis;x86_64",
		"system-images;android-37.0;google_apis_ps16k;x86_64",
	}
	resoluteImages = []string{
		"system-images;android-35;aosp_atd;x86_64",
		"system-images;android-35;google_apis_ps16k;x86_64",
		"system-images;android-36;google_apis_ps16k;x86_64",
		"system-images;android-37.0;google_apis_ps16k;x86_64",
	}
	jammyImages = []string{
		"system-images;android-33;google_apis;x86_64",
		"system-images;android-34;aosp_atd;x86_64",
		"system-images;android-34;google_apis;x86_64",
		"system-images;android-35;google_apis;x86_64",
	}
)

func TestSelectSystemImage(t *testing.T) {
	tests := []struct {
		name      string
		installed []string
		abi       string
		want      string
	}{
		{name: "Ubuntu Noble", installed: nobleImages, abi: "x86_64", want: "system-images;android-36;google_apis;x86_64"},
		{name: "Ubuntu Resolute", installed: resoluteImages, abi: "x86_64", want: "system-images;android-37.0;google_apis_ps16k;x86_64"},
		{name: "Ubuntu 22.04", installed: jammyImages, abi: "x86_64", want: "system-images;android-35;google_apis;x86_64"},
		{name: "minor API level beats major", installed: []string{
			"system-images;android-36;aosp_atd;x86_64",
			"system-images;android-36.1;aosp_atd;x86_64",
		}, abi: "x86_64", want: "system-images;android-36.1;aosp_atd;x86_64"},
		{name: "other ABI is skipped", installed: []string{
			"system-images;android-36;google_apis;arm64-v8a",
			"system-images;android-34;google_apis;x86_64",
		}, abi: "x86_64", want: "system-images;android-34;google_apis;x86_64"},
		{name: "unparsable API level is skipped", installed: []string{
			"system-images;android-Baklava;google_apis;x86_64",
			"system-images;android-34;google_apis;x86_64",
		}, abi: "x86_64", want: "system-images;android-34;google_apis;x86_64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectSystemImage(tt.installed, tt.abi)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSelectSystemImage_NoneForABI(t *testing.T) {
	_, err := selectSystemImage(nobleImages, "arm64-v8a")
	require.ErrorContains(t, err, "AVD Manager")

	_, err = selectSystemImage([]string{"system-images;android-36;android-tv;x86_64"}, "x86_64")
	require.Error(t, err)
}

func TestInstalledSystemImages(t *testing.T) {
	androidHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(androidHome, "system-images", "android-36", "google_apis", "x86_64"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(androidHome, "system-images", "android-37.0", "google_apis_ps16k", "x86_64"), 0755))
	writeFile(t, filepath.Join(androidHome, "system-images", "android-35", "google_apis", "x86_64"), "not a folder")

	images, err := installedSystemImages(androidHome)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"system-images;android-36;google_apis;x86_64",
		"system-images;android-37.0;google_apis_ps16k;x86_64",
	}, images)

	images, err = installedSystemImages(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, images)
}

func TestHostABI(t *testing.T) {
	assert.Equal(t, "x86_64", hostABI("amd64"))
	assert.Equal(t, "arm64-v8a", hostABI("arm64"))
}

func TestParseADBDevices(t *testing.T) {
	out := "* daemon not running; starting now at tcp:5037\n* daemon started successfully\nList of devices attached\nemulator-5554\tdevice\nemulator-5556\toffline\n"
	assert.Equal(t, []string{"emulator-5554", "emulator-5556"}, parseADBDevices(out))
	assert.Empty(t, parseADBDevices("List of devices attached\n"))
}

func TestPickRunningDevice(t *testing.T) {
	serial, err := pickRunningDevice(nil)
	require.NoError(t, err)
	assert.Empty(t, serial)

	serial, err = pickRunningDevice([]string{"emulator-5554"})
	require.NoError(t, err)
	assert.Equal(t, "emulator-5554", serial)

	_, err = pickRunningDevice([]string{"emulator-5554", "emulator-5556"})
	require.ErrorContains(t, err, "--device")
}

func TestPickEmulator(t *testing.T) {
	serial, err := pickEmulator([]string{"emulator-5554", "emulator-5556"}, "emulator-5556")
	require.NoError(t, err)
	assert.Equal(t, "emulator-5556", serial, "the exported serial picks among several devices")

	serial, err = pickEmulator(nil, "emulator-5554")
	require.NoError(t, err)
	assert.Empty(t, serial, "a serial adb does not list is ignored, so the step boots an emulator")

	serial, err = pickEmulator([]string{"emulator-5556"}, "emulator-5554")
	require.NoError(t, err)
	assert.Equal(t, "emulator-5556", serial)

	_, err = pickEmulator([]string{"emulator-5556", "emulator-5558"}, "emulator-5554")
	require.ErrorContains(t, err, "--device")
}

func TestHasDeviceArg(t *testing.T) {
	assert.True(t, hasDeviceArg([]string{"-e", "A=b", "--device", "emulator-5554"}))
	assert.True(t, hasDeviceArg([]string{"--udid=UDID-1"}))
	assert.False(t, hasDeviceArg([]string{"-e", "DEVICE=--device"}))
	assert.False(t, hasDeviceArg(nil))
}

func TestRunningSimulators(t *testing.T) {
	list := &destination.DeviceList{Devices: map[string][]destination.Device{
		"com.apple.CoreSimulator.SimRuntime.iOS-26-5": {
			{UDID: "booted", State: "Booted"},
			{UDID: "shutdown", State: "Shutdown"},
		},
		"com.apple.CoreSimulator.SimRuntime.iOS-18-6": {
			{UDID: "booting", State: "Booting"},
			{UDID: "shutting-down", State: "Shutting Down"},
		},
		"com.apple.CoreSimulator.SimRuntime.watchOS-26-5": {
			{UDID: "watch", State: "Booted"},
		},
	}}
	assert.Equal(t, []string{"booting", "booted"}, runningSimulators(list))
}
