package step

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-xcode/v2/destination"
	"github.com/bitrise-io/go-xcode/v2/simulator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaitUntilBooted(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	devices := iosDevicesWithFakeXcrun(t, "echo \"$@\" > \""+argsFile+"\"\n")

	require.NoError(t, devices.waitUntilBooted("UDID-1", time.Minute))

	args, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	assert.Equal(t, "simctl bootstatus UDID-1\n", string(args), "nothing is launched on a simulator the step found running")
}

func TestWaitUntilBooted_Fails(t *testing.T) {
	devices := iosDevicesWithFakeXcrun(t, "exit 1\n")

	require.ErrorContains(t, devices.waitUntilBooted("UDID-1", time.Minute), "wait for simulator UDID-1")
}

func TestWaitUntilBooted_TimesOut(t *testing.T) {
	devices := iosDevicesWithFakeXcrun(t, "sleep 10\n")

	start := time.Now()
	require.ErrorContains(t, devices.waitUntilBooted("UDID-1", 100*time.Millisecond), "did not finish booting")
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestUseDestination_ShuttingDownSimulator(t *testing.T) {
	manager := &fakeSimulatorManager{}
	devices := iosDevices{
		logger:           log.NewLogger(),
		deviceFinder:     fakeDeviceFinder{device: destination.Device{UDID: "UDID-1", Name: "iPhone 17", State: "Shutting Down"}},
		simulatorManager: manager,
	}

	device, err := devices.useDestination("platform=iOS Simulator,name=iPhone 17")
	require.NoError(t, err)
	assert.Equal(t, []string{"shutdown", "boot", "wait"}, manager.calls, "it is shut down before the step boots it")
	assert.NotNil(t, device.Release, "the step booted it, so it shuts it down too")
}

func TestUseDestination_BootedSimulator(t *testing.T) {
	manager := &fakeSimulatorManager{}
	devices := iosDevicesWithFakeXcrun(t, "")
	devices.deviceFinder = fakeDeviceFinder{device: destination.Device{UDID: "UDID-1", Name: "iPhone 17", State: "Booted"}}
	devices.simulatorManager = manager

	device, err := devices.useDestination("platform=iOS Simulator,name=iPhone 17")
	require.NoError(t, err)
	assert.Empty(t, manager.calls)
	assert.Nil(t, device.Release)
}

type fakeDeviceFinder struct {
	device destination.Device
}

func (f fakeDeviceFinder) FindDevice(destination.Simulator) (destination.Device, error) {
	return f.device, nil
}
func (f fakeDeviceFinder) ListDevices() (*destination.DeviceList, error) {
	return &destination.DeviceList{}, nil
}

type fakeSimulatorManager struct {
	simulator.Manager
	calls []string
}

func (f *fakeSimulatorManager) Boot(destination.Device) error {
	f.calls = append(f.calls, "boot")
	return nil
}

func (f *fakeSimulatorManager) WaitForBootFinished(string, time.Duration) error {
	f.calls = append(f.calls, "wait")
	return nil
}

func (f *fakeSimulatorManager) Shutdown(string) error {
	f.calls = append(f.calls, "shutdown")
	return nil
}

func iosDevicesWithFakeXcrun(t *testing.T, script string) iosDevices {
	fakeOnPath(t, "xcrun", script)
	return iosDevices{logger: log.NewLogger(), commandFactory: command.NewFactory(env.NewRepository())}
}
