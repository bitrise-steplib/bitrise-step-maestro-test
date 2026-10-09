package step

import (
	"fmt"
	"strings"

	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
)

const (
	// Exported by AVD Manager and Xcode Start Simulator.
	emulatorSerialEnv   = "BITRISE_EMULATOR_SERIAL"
	xcodeDestinationEnv = "BITRISE_XCODE_DESTINATION"
)

// Device is the device the flows run on. Release is set only when the step booted the device: it shuts the device
// down. HintEnv and HintValue let a later step find a booted device the step leaves running.
type Device struct {
	ID        string
	Release   func()
	HintEnv   string
	HintValue string
}

type DeviceManager interface {
	Acquire(config Config) (Device, error)
}

type deviceManager struct {
	logger         log.Logger
	commandFactory command.Factory
	envRepo        env.Repository
}

func NewDeviceManager(logger log.Logger, commandFactory command.Factory, envRepo env.Repository) DeviceManager {
	return deviceManager{logger: logger, commandFactory: commandFactory, envRepo: envRepo}
}

func (m deviceManager) Acquire(config Config) (Device, error) {
	if config.Platform == PlatformIOS {
		return newIOSDevices(m.logger, m.commandFactory, m.envRepo.Get(xcodeDestinationEnv)).acquire()
	}
	return newAndroidDevices(m.logger, m.commandFactory, config.AndroidSDKDir, m.envRepo.Get(emulatorSerialEnv), config.DeployDir).acquire()
}

// pickRunningDevice returns the device the flows should run on, or "" when none runs and the step has to boot one.
func pickRunningDevice(running []string) (string, error) {
	switch len(running) {
	case 0:
		return "", nil
	case 1:
		return running[0], nil
	default:
		return "", fmt.Errorf("%d devices are running (%s), pass the one to use with --device in additional_args", len(running), strings.Join(running, ", "))
	}
}

func hasDeviceArg(args []string) bool {
	for _, arg := range args {
		name, _, _ := strings.Cut(arg, "=")
		if name == "--device" || name == "--udid" {
			return true
		}
	}
	return false
}
