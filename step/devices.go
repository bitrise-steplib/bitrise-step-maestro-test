package step

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// iosRuntimePrefix marks the simulators an iOS app can run on; a paired Apple Watch boots in a watchOS runtime.
const iosRuntimePrefix = "com.apple.CoreSimulator.SimRuntime.iOS-"

type runningDevice struct {
	// ID is the adb serial or the simulator UDID.
	ID   string
	Name string
}

func runningDevicesCommand(platform Platform, sdkDir string) (string, []string) {
	if platform == PlatformAndroid {
		return adbPath(sdkDir), []string{"devices"}
	}
	return "xcrun", []string{"simctl", "list", "devices", "--json"}
}

func parseRunningDevices(platform Platform, output string) ([]runningDevice, error) {
	if platform == PlatformAndroid {
		return parseADBDevices(output), nil
	}
	return parseBootedSimulators(output)
}

// parseADBDevices returns the devices `adb devices` lists as ready, skipping offline and unauthorized ones.
func parseADBDevices(output string) []runningDevice {
	var devices []runningDevice
	for _, line := range strings.Split(output, "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[1] == "device" {
			devices = append(devices, runningDevice{ID: fields[0], Name: fields[0]})
		}
	}
	return devices
}

func parseBootedSimulators(output string) ([]runningDevice, error) {
	var list struct {
		Devices map[string][]struct {
			UDID  string `json:"udid"`
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"devices"`
	}
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		return nil, fmt.Errorf("parse simctl device list: %w", err)
	}

	var simulators []runningDevice
	for _, runtime := range slices.Sorted(maps.Keys(list.Devices)) {
		if !strings.HasPrefix(runtime, iosRuntimePrefix) {
			continue
		}
		for _, device := range list.Devices[runtime] {
			if device.State == "Booted" {
				simulators = append(simulators, runningDevice{ID: device.UDID, Name: fmt.Sprintf("%s (%s)", device.Name, device.UDID)})
			}
		}
	}
	return simulators, nil
}

// Maestro picks a running device on its own, so with several devices the flows could run on another one than the app
// was installed on.
func singleDevice(platform Platform, devices []runningDevice) (runningDevice, error) {
	var names []string
	for _, device := range devices {
		names = append(names, device.Name)
	}

	switch {
	case len(devices) == 1:
		return devices[0], nil
	case len(devices) == 0 && platform == PlatformAndroid:
		return runningDevice{}, errors.New("no Android emulator or device is running, start one before this Step, for example with AVD Manager")
	case len(devices) == 0:
		return runningDevice{}, errors.New("no iOS simulator is booted, boot one before this Step, for example with Xcode Start Simulator")
	case platform == PlatformAndroid:
		return runningDevice{}, fmt.Errorf("%d Android devices are running (%s), the Step supports one device per machine: to run the flows on several devices, use parallel Pipeline Workflows", len(devices), strings.Join(names, ", "))
	default:
		return runningDevice{}, fmt.Errorf("%d iOS simulators are booted (%s), the Step supports one device per machine: to run the flows on several devices, use parallel Pipeline Workflows", len(devices), strings.Join(names, ", "))
	}
}
