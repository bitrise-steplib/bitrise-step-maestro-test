package step

import (
	"encoding/json"
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

// deviceWords is how the error messages name a platform's devices and how to start one.
type deviceWords struct {
	one, many, state, start, starter string
}

func wordsFor(platform Platform) deviceWords {
	if platform == PlatformIOS {
		return deviceWords{one: "iOS simulator", many: "iOS simulators", state: "booted", start: "boot", starter: "Xcode Start Simulator"}
	}
	return deviceWords{one: "Android emulator or device", many: "Android devices", state: "running", start: "start", starter: "AVD Manager"}
}

// expectedDevices checks that exactly count devices of the platform run. Maestro picks the running devices on its own,
// so with more devices the flows could run on another one than the app was installed on, and with fewer Maestro cannot
// start every shard. A Config built without configFromInput has no count, and runs on one device.
func expectedDevices(platform Platform, devices []runningDevice, count int) ([]runningDevice, error) {
	count = max(count, 1)
	if len(devices) == count {
		return devices, nil
	}

	var names []string
	for _, device := range devices {
		names = append(names, device.Name)
	}
	words := wordsFor(platform)
	running := fmt.Sprintf("%d %s are %s", len(devices), words.many, words.state)

	switch {
	case len(devices) == 0 && count == 1:
		return nil, fmt.Errorf("no %s is %s, %s one before this Step, for example with %s", words.one, words.state, words.start, words.starter)
	case len(devices) == 0:
		return nil, fmt.Errorf("shard_split is %d, but no %s is %s: %s %d before this Step, for example with %s", count, words.one, words.state, words.start, count, words.starter)
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

// shardDeviceIDs returns the devices an attempt runs its shards on. A retry runs flow files, so it needs no more devices
// than it has flows. Maestro 2.11.0 coerces the shard count to the flow count on its own, but the Step does not rely
// on it.
func shardDeviceIDs(ids, flowPaths []string, retry bool) []string {
	if retry && len(flowPaths) < len(ids) {
		return ids[:len(flowPaths)]
	}
	return ids
}
