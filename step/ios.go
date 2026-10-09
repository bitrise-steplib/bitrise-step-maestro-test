package step

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-xcode/v2/destination"
	"github.com/bitrise-io/go-xcode/v2/simulator"
	"github.com/bitrise-io/go-xcode/v2/xcodeversion"
)

const (
	iosBootTimeout = 5 * time.Minute
	// The simulator Bitrise macOS stacks create for every iOS runtime, also the default of Xcode Test.
	defaultSimulatorDestination = "platform=iOS Simulator,name=Bitrise iOS default,OS=latest"
	iosRuntimePrefix            = "com.apple.CoreSimulator.SimRuntime.iOS-"
	simulatorShutdownState      = "Shutdown"
	simulatorBootedState        = "Booted"
	simulatorBootingState       = "Booting"
)

type iosDevices struct {
	logger           log.Logger
	commandFactory   command.Factory
	deviceFinder     destination.DeviceFinder
	simulatorManager simulator.Manager
	destinationHint  string
}

func newIOSDevices(logger log.Logger, commandFactory command.Factory, destinationHint string) iosDevices {
	xcodeVersion, err := xcodeversion.NewXcodeVersionProvider(commandFactory).GetVersion()
	if err != nil {
		logger.Warnf("Failed to read the Xcode version: %s", err)
	}
	return iosDevices{
		logger:           logger,
		commandFactory:   commandFactory,
		deviceFinder:     destination.NewDeviceFinder(logger, commandFactory, xcodeVersion),
		simulatorManager: simulator.NewManager(logger, commandFactory),
		destinationHint:  destinationHint,
	}
}

func (i iosDevices) acquire() (Device, error) {
	if i.destinationHint != "" {
		i.logger.Printf("Using the simulator from %s: %s", xcodeDestinationEnv, i.destinationHint)
		return i.useDestination(i.destinationHint)
	}

	list, err := i.deviceFinder.ListDevices()
	if err != nil {
		return Device{}, err
	}
	udid, err := pickRunningDevice(runningSimulators(list))
	if err != nil {
		return Device{}, err
	}
	if udid != "" {
		i.logger.Printf("Using the running simulator: %s", udid)
		if err := i.waitUntilBooted(udid, iosBootTimeout); err != nil {
			return Device{}, err
		}
		return Device{ID: udid}, nil
	}

	i.logger.Printf("No running simulator, booting %s", defaultSimulatorDestination)
	return i.useDestination(defaultSimulatorDestination)
}

func (i iosDevices) useDestination(dest string) (Device, error) {
	sim, err := destination.NewSimulator(dest)
	if err != nil {
		return Device{}, fmt.Errorf("invalid destination (%s): %w", dest, err)
	}
	device, err := i.deviceFinder.FindDevice(*sim)
	if err != nil {
		return Device{}, fmt.Errorf("find simulator (%s): %w", dest, err)
	}
	i.logger.Printf("Simulator: %s, %s, %s (%s)", device.Name, device.OS, device.UDID, device.State)

	if isRunningSimulator(device.State) {
		if err := i.waitUntilBooted(device.UDID, iosBootTimeout); err != nil {
			return Device{}, err
		}
		return Device{ID: device.UDID}, nil
	}
	if device.State != simulatorShutdownState {
		// A simulator that is shutting down cannot be booted until it is shut down.
		if err := i.simulatorManager.Shutdown(device.UDID); err != nil {
			return Device{}, err
		}
	}

	start := time.Now()
	release := func() {
		i.logger.Println()
		i.logger.Infof("Shutting down the simulator the step started")
		if err := i.simulatorManager.Shutdown(device.UDID); err != nil {
			i.logger.Warnf("%s", err)
		}
	}
	if err := i.simulatorManager.Boot(device); err != nil {
		return Device{}, err
	}
	if err := i.simulatorManager.WaitForBootFinished(device.UDID, iosBootTimeout); err != nil {
		release()
		return Device{}, err
	}
	i.logger.Donef("Simulator booted in %s", time.Since(start).Round(time.Second))
	return Device{
		ID:        device.UDID,
		Release:   release,
		HintEnv:   xcodeDestinationEnv,
		HintValue: fmt.Sprintf("platform=%s,name=%s,OS=%s", device.Platform, device.Name, device.OS),
	}, nil
}

// waitUntilBooted waits for a simulator the step found running. It must not change the simulator, so it does not use
// WaitForBootFinished, which launches the Settings app to tell that the boot finished.
func (i iosDevices) waitUntilBooted(udid string, timeout time.Duration) error {
	cmd := i.commandFactory.Create("xcrun", []string{"simctl", "bootstatus", udid}, &command.Opts{Stdout: os.Stdout, Stderr: os.Stderr})
	i.logger.TDonef("$ %s", cmd.PrintableCommandArgs())
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("wait for simulator %s: %w", udid, err)
	}

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		if err != nil {
			return fmt.Errorf("wait for simulator %s: %w", udid, err)
		}
		return nil
	case <-time.After(timeout):
		if err := cmd.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			i.logger.Warnf("Kill simctl bootstatus: %s", err)
		}
		return fmt.Errorf("simulator %s did not finish booting in %s", udid, timeout)
	}
}

// runningSimulators counts Booting simulators as running too, so the step never boots a second one next to them. A
// simulator that is shutting down is not running: waiting for it to boot would never end.
func runningSimulators(list *destination.DeviceList) []string {
	var udids []string
	for _, runtimeID := range slices.Sorted(maps.Keys(list.Devices)) {
		if !strings.HasPrefix(runtimeID, iosRuntimePrefix) {
			continue
		}
		for _, device := range list.Devices[runtimeID] {
			if isRunningSimulator(device.State) {
				udids = append(udids, device.UDID)
			}
		}
	}
	return udids
}

func isRunningSimulator(state string) bool {
	return state == simulatorBootedState || state == simulatorBootingState
}
