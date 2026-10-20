package step

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

func runningDevicesCommand(platform Platform, sdkDir string) (string, []string) {
	if platform == PlatformAndroid {
		return adbPath(sdkDir), []string{"devices"}
	}
	return "xcrun", []string{"simctl", "list", "devices", "--json"}
}

func parseRunningDevices(platform Platform, output string) ([]string, error) {
	if platform == PlatformAndroid {
		return parseADBDevices(output), nil
	}
	return parseBootedSimulators(output)
}

// parseADBDevices returns the serials `adb devices` lists as ready, skipping offline and unauthorized ones.
func parseADBDevices(output string) []string {
	var serials []string
	for _, line := range strings.Split(output, "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[1] == "device" {
			serials = append(serials, fields[0])
		}
	}
	return serials
}

func parseBootedSimulators(output string) ([]string, error) {
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

	var simulators []string
	for _, runtime := range slices.Sorted(maps.Keys(list.Devices)) {
		for _, device := range list.Devices[runtime] {
			if device.State == "Booted" {
				simulators = append(simulators, fmt.Sprintf("%s (%s)", device.Name, device.UDID))
			}
		}
	}
	return simulators, nil
}

// The app install and Maestro each pick a running device on their own, so with several devices the flows could
// run on one the app was not installed on.
func singleDeviceError(platform Platform, devices []string) error {
	switch {
	case len(devices) == 1:
		return nil
	case len(devices) == 0 && platform == PlatformAndroid:
		return errors.New("no Android emulator or device is running, start one before this Step, for example with AVD Manager")
	case len(devices) == 0:
		return errors.New("no iOS simulator is booted, boot one before this Step, for example with Xcode Start Simulator")
	case platform == PlatformAndroid:
		return fmt.Errorf("%d Android devices are running (%s), the Step supports one device per machine: to run the flows on several devices, use parallel Pipeline Workflows", len(devices), strings.Join(devices, ", "))
	default:
		return fmt.Errorf("%d iOS simulators are booted (%s), the Step supports one device per machine: to run the flows on several devices, use parallel Pipeline Workflows", len(devices), strings.Join(devices, ", "))
	}
}
