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

// expectedDevices checks that exactly count devices of the platform run. Maestro picks the running devices on its own,
// so with more devices the flows could run on another one than the app was installed on, and with fewer Maestro cannot
// start every shard.
func expectedDevices(platform Platform, devices []runningDevice, count int) ([]runningDevice, error) {
	count = max(count, 1)
	if len(devices) == count {
		return devices, nil
	}

	var names []string
	for _, device := range devices {
		names = append(names, device.Name)
	}
	running := fmt.Sprintf("%d Android devices are running", len(devices))
	if platform == PlatformIOS {
		running = fmt.Sprintf("%d iOS simulators are booted", len(devices))
	}

	switch {
	case len(devices) == 0 && count == 1 && platform == PlatformAndroid:
		return nil, errors.New("no Android emulator or device is running, start one before this Step, for example with AVD Manager")
	case len(devices) == 0 && count == 1:
		return nil, errors.New("no iOS simulator is booted, boot one before this Step, for example with Xcode Start Simulator")
	case len(devices) == 0 && platform == PlatformAndroid:
		return nil, fmt.Errorf("shard_split is %d, but no Android emulator or device is running: start %d before this Step, for example with AVD Manager", count, count)
	case len(devices) == 0:
		return nil, fmt.Errorf("shard_split is %d, but no iOS simulator is booted: boot %d before this Step, for example with Xcode Start Simulator", count, count)
	case count == 1:
		return nil, fmt.Errorf("%s (%s), the Step runs the flows on one device unless shard_split is set: set shard_split to %d to split the flows across them, or run one device per machine with parallel Pipeline Workflows", running, strings.Join(names, ", "), len(devices))
	default:
		return nil, fmt.Errorf("shard_split is %d, but %s (%s): run exactly %d, or set shard_split to %d", count, running, strings.Join(names, ", "), count, len(devices))
	}
}

func deviceIDs(devices []runningDevice) []string {
	var ids []string
	for _, device := range devices {
		ids = append(ids, device.ID)
	}
	return ids
}
