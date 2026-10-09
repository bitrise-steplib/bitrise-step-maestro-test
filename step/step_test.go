package step

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_DisablesMaestroAnalyticsAndUpdateCheck(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "env.txt")
	fakeMaestro := filepath.Join(t.TempDir(), "maestro")
	writeFile(t, fakeMaestro, "#!/bin/sh\nenv > \""+envFile+"\"\n")
	require.NoError(t, os.Chmod(fakeMaestro, 0755))

	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())

	_, err := s.Run(Config{FlowPaths: []string{".maestro"}}, Installation{BinaryPath: fakeMaestro})
	require.NoError(t, err)

	maestroEnv, err := os.ReadFile(envFile)
	require.NoError(t, err)
	assert.Contains(t, string(maestroEnv), "MAESTRO_CLI_NO_ANALYTICS=true\n")
	assert.Contains(t, string(maestroEnv), "MAESTRO_DISABLE_UPDATE_CHECK=true\n")
	assert.Contains(t, string(maestroEnv), "PATH=", "the rest of the environment is passed through")
}

func TestConfigFromInput_FlowPaths(t *testing.T) {
	config, err := configFromInput(Input{FlowPath: "maestro/entry\nmaestro/unlock\n", TestName: "Maestro"})
	require.NoError(t, err)
	assert.Equal(t, []string{"maestro/entry", "maestro/unlock"}, config.FlowPaths)

	_, err = configFromInput(Input{FlowPath: " \n ", TestName: "Maestro"})
	require.ErrorContains(t, err, "flow_path")
}

func TestConfigFromInput_ManageDevice(t *testing.T) {
	config, err := configFromInput(Input{FlowPath: ".maestro", TestName: "Maestro", AdditionalArgs: "-e A=b"})
	require.NoError(t, err)
	assert.True(t, config.ManageDevice)

	config, err = configFromInput(Input{FlowPath: ".maestro", TestName: "Maestro", AdditionalArgs: "--device emulator-5554"})
	require.NoError(t, err)
	assert.False(t, config.ManageDevice, "a device picked in additional_args is left to Maestro")

}

func TestConfigFromInput_AndroidHome(t *testing.T) {
	sdkRoot, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	config, err := configFromInput(Input{FlowPath: ".maestro", TestName: "Maestro", AndroidSDKRoot: sdkRoot})
	require.NoError(t, err)
	assert.Equal(t, sdkRoot, config.AndroidHome, "ANDROID_SDK_ROOT is used when ANDROID_HOME is unset")

	config, err = configFromInput(Input{FlowPath: ".maestro", TestName: "Maestro", AndroidHome: filepath.Join(sdkRoot, "missing"), AndroidSDKRoot: sdkRoot})
	require.NoError(t, err)
	assert.Equal(t, sdkRoot, config.AndroidHome, "ANDROID_SDK_ROOT is used when ANDROID_HOME is not an SDK")

	config, err = configFromInput(Input{FlowPath: ".maestro", TestName: "Maestro"})
	require.NoError(t, err)
	assert.Empty(t, config.AndroidHome, "a missing SDK only fails the steps that need it")
}

func TestRun_RunsOnTheAcquiredDeviceAndReleasesIt(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	fakeMaestro := filepath.Join(t.TempDir(), "maestro")
	writeFile(t, fakeMaestro, "#!/bin/sh\necho \"$@\" > \""+argsFile+"\"\nexit 1\n")
	require.NoError(t, os.Chmod(fakeMaestro, 0755))

	devices := &fakeDevices{device: bootedDevice()}
	exporter := &recordingExporter{}
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())
	s.devices = devices
	s.exporter = exporter

	_, err := s.Run(Config{FlowPaths: []string{".maestro"}, ManageDevice: true, ShutdownDevice: true}, Installation{BinaryPath: fakeMaestro})
	require.ErrorContains(t, err, "maestro test failed")

	maestroArgs, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	assert.Contains(t, string(maestroArgs), "--device emulator-5554")
	assert.True(t, devices.released, "the device is released even when the flows fail")
	assert.Empty(t, exporter.outputs)
}

func TestRun_LeavesTheBootedDeviceRunning(t *testing.T) {
	devices := &fakeDevices{device: bootedDevice()}
	exporter := &recordingExporter{}
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())
	s.devices = devices
	s.exporter = exporter

	_, err := s.Run(Config{FlowPaths: []string{".maestro"}, ManageDevice: true}, Installation{BinaryPath: fakeMaestroBinary(t)})
	require.NoError(t, err)
	assert.False(t, devices.released)
	assert.Equal(t, map[string]string{emulatorSerialEnv: "emulator-5554"}, exporter.outputs, "a later step finds the device by the exported serial")
}

func TestRun_LeavesARunningDeviceAlone(t *testing.T) {
	for _, shutdown := range []bool{true, false} {
		devices := &fakeDevices{device: Device{ID: "emulator-5554"}}
		exporter := &recordingExporter{}
		s := testStep()
		s.commandFactory = command.NewFactory(env.NewRepository())
		s.devices = devices
		s.exporter = exporter

		_, err := s.Run(Config{FlowPaths: []string{".maestro"}, ManageDevice: true, ShutdownDevice: shutdown}, Installation{BinaryPath: fakeMaestroBinary(t)})
		require.NoError(t, err)
		assert.False(t, devices.released)
		assert.Empty(t, exporter.outputs)
	}
}

func TestRun_DeviceNotManaged(t *testing.T) {
	devices := &fakeDevices{}
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())
	s.devices = devices

	_, err := s.Run(Config{FlowPaths: []string{".maestro"}}, Installation{BinaryPath: fakeMaestroBinary(t)})
	require.NoError(t, err)
	assert.False(t, devices.acquired)
}

func fakeMaestroBinary(t *testing.T) string {
	pth := filepath.Join(t.TempDir(), "maestro")
	writeFile(t, pth, "#!/bin/sh\n")
	require.NoError(t, os.Chmod(pth, 0755))
	return pth
}

func bootedDevice() Device {
	return Device{ID: "emulator-5554", HintEnv: emulatorSerialEnv, HintValue: "emulator-5554"}
}

type fakeDevices struct {
	device   Device
	acquired bool
	released bool
}

// Acquire hands out a Release only for a device with a hint, as the real managers do for the devices they boot.
func (f *fakeDevices) Acquire(Config) (Device, error) {
	f.acquired = true
	device := f.device
	if device.HintEnv != "" {
		device.Release = func() { f.released = true }
	}
	return device, nil
}

type recordingExporter struct {
	fakeExporter
	outputs map[string]string
}

func (r *recordingExporter) ExportOutput(key, value string) error {
	if r.outputs == nil {
		r.outputs = map[string]string{}
	}
	r.outputs[key] = value
	return nil
}
