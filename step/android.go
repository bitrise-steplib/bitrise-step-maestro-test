package step

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/bitrise-io/go-android/v2/adbmanager"
	"github.com/bitrise-io/go-android/v2/sdk"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/hashicorp/go-version"
)

const (
	avdName                = "bitrise_maestro"
	avdDeviceProfile       = "pixel"
	systemImagePrefix      = "system-images"
	ps16kSystemImageSuffix = "_ps16k"
)

// Overridden in tests.
var (
	androidBootTimeout    = 10 * time.Minute
	emulatorCheckInterval = 5 * time.Second
	emulatorStopTimeout   = time.Minute
)

// The first tag with an image for the host ABI wins, then the highest API level within it.
var preferredSystemImageTags = []string{"google_apis", "google_apis_ps16k", "aosp_atd"}

var animationSettings = []string{"window_animation_scale", "transition_animation_scale", "animator_duration_scale"}

type androidDevices struct {
	logger         log.Logger
	commandFactory command.Factory
	sdkDir         string
	adb            string
	serialHint     string
	deployDir      string
}

func newAndroidDevices(logger log.Logger, commandFactory command.Factory, sdkDir, serialHint, deployDir string) androidDevices {
	return androidDevices{
		logger:         logger,
		commandFactory: commandFactory,
		sdkDir:         sdkDir,
		adb:            adbPath(sdkDir),
		serialHint:     serialHint,
		deployDir:      deployDir,
	}
}

// adbPath is the SDK's adb, the one adbmanager runs too, and adb on PATH only without an SDK: adb clients of different
// versions restart each other's server, which drops the emulator connection.
func adbPath(sdkDir string) string {
	if sdkDir != "" {
		pth := filepath.Join(sdkDir, "platform-tools", "adb")
		if _, err := os.Stat(pth); err == nil {
			return pth
		}
	}
	return "adb"
}

func findAndroidSDKDir(androidHome, androidSDKRoot string) string {
	sdkModel, err := sdk.NewDefaultModel(sdk.Environment{AndroidHome: androidHome, AndroidSDKRoot: androidSDKRoot}, pathutil.NewPathChecker())
	if err != nil {
		return ""
	}
	return sdkModel.GetAndroidHome()
}

func (a androidDevices) acquire() (Device, error) {
	sdkModel, adbManager, err := a.sdk()
	if err != nil {
		return Device{}, err
	}

	running, err := a.runningDevices()
	if err != nil {
		return Device{}, err
	}
	serial, err := pickEmulator(running, a.serialHint)
	if err != nil {
		return Device{}, err
	}
	if a.serialHint != "" && serial != a.serialHint {
		a.logger.Warnf("%s is %s, but adb does not list that device, so it is ignored", emulatorSerialEnv, a.serialHint)
	}
	if serial != "" {
		if serial == a.serialHint {
			a.logger.Printf("Using the emulator from %s: %s", emulatorSerialEnv, serial)
		} else {
			a.logger.Printf("Using the running device: %s", serial)
		}
		if err := adbManager.WaitForDevice(serial, androidBootTimeout); err != nil {
			return Device{}, err
		}
		return Device{ID: serial}, nil
	}

	a.logger.Printf("No running device, booting an emulator")
	return a.boot(sdkModel, adbManager)
}

func (a androidDevices) sdk() (*sdk.Model, *adbmanager.Model, error) {
	if a.sdkDir == "" {
		return nil, nil, errors.New("the Step needs the Android SDK, but neither ANDROID_HOME nor ANDROID_SDK_ROOT points to one")
	}
	sdkModel, err := sdk.New(a.sdkDir, pathutil.NewPathChecker())
	if err != nil {
		return nil, nil, fmt.Errorf("init Android SDK (%s): %w", a.sdkDir, err)
	}
	adbManager, err := adbmanager.New(sdkModel, a.commandFactory, a.logger)
	if err != nil {
		return nil, nil, err
	}
	return sdkModel, adbManager, nil
}

func (a androidDevices) boot(sdkModel *sdk.Model, adbManager *adbmanager.Model) (Device, error) {
	cmdlineToolsPath, err := sdkModel.CmdlineToolsPath()
	if err != nil {
		return Device{}, err
	}

	installed, err := installedSystemImages(a.sdkDir)
	if err != nil {
		return Device{}, err
	}
	image, err := selectSystemImage(installed, hostABI(runtime.GOARCH))
	if err != nil {
		return Device{}, err
	}
	a.logger.Printf("System image: %s (newest preinstalled)", image)
	if err := a.createAVD(filepath.Join(cmdlineToolsPath, "avdmanager"), image); err != nil {
		return Device{}, err
	}

	start := time.Now()
	emulator, err := a.startEmulator()
	if err != nil {
		return Device{}, err
	}
	release := func() { emulator.stop(a.logger, a.commandFactory, a.adb) }

	if err := adbManager.WaitForDevice(emulator.serial, androidBootTimeout-time.Since(start)); err != nil {
		release()
		return Device{}, fmt.Errorf("%w, emulator log: %s", err, emulator.logPath)
	}
	a.logger.Donef("Emulator %s booted in %s", emulator.serial, time.Since(start).Round(time.Second))

	if err := a.disableAnimations(emulator.serial); err != nil {
		release()
		return Device{}, err
	}
	// The log only helps when the boot fails; once it succeeded it would just pile up among the deployed artifacts.
	if err := os.Remove(emulator.logPath); err != nil {
		a.logger.Warnf("Remove emulator log: %s", err)
	}
	return Device{ID: emulator.serial, Release: release, HintEnv: emulatorSerialEnv, HintValue: emulator.serial}, nil
}

func (a androidDevices) createAVD(avdManagerPath, image string) error {
	parts := strings.Split(image, ";")
	tag, abi := parts[2], parts[3]
	args := []string{"--verbose", "create", "avd", "--force", "--name", avdName, "--device", avdDeviceProfile, "--package", image, "--abi", abi}
	// The valid avdmanager tag of a 16 KB page size image varies by API level, so avdmanager picks it.
	if !strings.HasSuffix(tag, ps16kSystemImageSuffix) {
		args = append(args, "--tag", tag)
	}

	cmd := a.commandFactory.Create(avdManagerPath, args, &command.Opts{
		// Answers "no" to creating a custom hardware profile.
		Stdin: strings.NewReader(strings.Repeat("no\n", 20)),
	})
	a.logger.TDonef("$ %s", cmd.PrintableCommandArgs())
	if out, err := cmd.RunAndReturnTrimmedCombinedOutput(); err != nil {
		return fmt.Errorf("create emulator: %w\n%s", err, out)
	}
	return nil
}

type runningEmulator struct {
	serial  string
	cmd     command.Command
	exited  chan struct{}
	logPath string
}

// startEmulator writes the emulator output to the deploy directory, so that it is still there to read after a failed
// build.
func (a androidDevices) startEmulator() (runningEmulator, error) {
	logFile, err := os.CreateTemp(a.deployDir, "emulator-*.log")
	if err != nil {
		return runningEmulator{}, err
	}

	args := []string{
		"@" + avdName,
		"-no-window", "-no-boot-anim", "-no-audio",
		"-no-snapshot", "-wipe-data",
		"-netdelay", "none",
		"-gpu", "auto",
		"-camera-back", "none", "-camera-front", "none",
	}
	cmd := a.commandFactory.Create(filepath.Join(a.sdkDir, "emulator", "emulator"), args, &command.Opts{Stdout: logFile, Stderr: logFile})
	a.logger.TDonef("$ %s", cmd.PrintableCommandArgs())
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return runningEmulator{}, fmt.Errorf("start emulator: %w", err)
	}

	emulator := runningEmulator{cmd: cmd, exited: make(chan struct{}), logPath: logFile.Name()}
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
		close(emulator.exited)
	}()

	// The step only boots when no device runs, so the first device adb reports is this emulator.
	deadline := time.Now().Add(androidBootTimeout)
	for emulator.serial == "" {
		select {
		case <-emulator.exited:
			return runningEmulator{}, fmt.Errorf("emulator exited before it came online, see %s", emulator.logPath)
		case <-time.After(emulatorCheckInterval):
		}

		running, err := a.runningDevices()
		if err != nil {
			emulator.stop(a.logger, a.commandFactory, a.adb)
			return runningEmulator{}, err
		}
		if len(running) > 0 {
			emulator.serial = running[0]
		} else if time.Now().After(deadline) {
			emulator.stop(a.logger, a.commandFactory, a.adb)
			return runningEmulator{}, fmt.Errorf("emulator did not come online in %s, see %s", androidBootTimeout, emulator.logPath)
		}
	}
	return emulator, nil
}

func (e runningEmulator) stop(logger log.Logger, commandFactory command.Factory, adb string) {
	logger.Println()
	logger.Infof("Shutting down the emulator the step started")
	if e.serial != "" {
		cmd := commandFactory.Create(adb, []string{"-s", e.serial, "emu", "kill"}, nil)
		logger.TDonef("$ %s", cmd.PrintableCommandArgs())
		if out, err := cmd.RunAndReturnTrimmedCombinedOutput(); err != nil {
			logger.Warnf("adb emu kill: %s\n%s", err, out)
		}
		select {
		case <-e.exited:
			return
		case <-time.After(emulatorStopTimeout):
			logger.Warnf("The emulator did not exit in %s, killing it", emulatorStopTimeout)
		}
	}
	if err := e.cmd.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		logger.Warnf("Kill emulator: %s", err)
	}
}

func (a androidDevices) disableAnimations(serial string) error {
	a.logger.Printf("Disabling animations")
	for _, setting := range animationSettings {
		cmd := a.commandFactory.Create(a.adb, []string{"-s", serial, "shell", "settings", "put", "global", setting, "0"}, nil)
		if out, err := cmd.RunAndReturnTrimmedCombinedOutput(); err != nil {
			return fmt.Errorf("disable animations (%s): %w\n%s", setting, err, out)
		}
	}
	return nil
}

func (a androidDevices) runningDevices() ([]string, error) {
	cmd := a.commandFactory.Create(a.adb, []string{"devices"}, nil)
	out, err := cmd.RunAndReturnTrimmedCombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("adb devices: %w\n%s", err, out)
	}
	return parseADBDevices(out), nil
}

// pickEmulator prefers the serial an earlier Step exported, but only while adb lists it: a serial of a device that is
// gone would otherwise be waited on until the boot timeout. AVD Manager exports the serial once adb lists the device.
func pickEmulator(running []string, serialHint string) (string, error) {
	if serialHint != "" && slices.Contains(running, serialHint) {
		return serialHint, nil
	}
	return pickRunningDevice(running)
}

// parseADBDevices returns every serial adb lists, offline ones included: a device that is still booting
// shows up as offline, and the step must not boot a second one next to it.
func parseADBDevices(out string) []string {
	var serials []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.HasPrefix(line, "*") {
			continue
		}
		serials = append(serials, fields[0])
	}
	return serials
}

func installedSystemImages(sdkDir string) ([]string, error) {
	dirs, err := filepath.Glob(filepath.Join(sdkDir, systemImagePrefix, "*", "*", "*"))
	if err != nil {
		return nil, err
	}

	var images []string
	for _, dir := range dirs {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		rel, err := filepath.Rel(sdkDir, dir)
		if err != nil {
			return nil, err
		}
		images = append(images, strings.Join(strings.Split(rel, string(filepath.Separator)), ";"))
	}
	return images, nil
}

func selectSystemImage(installed []string, abi string) (string, error) {
	for _, tag := range preferredSystemImageTags {
		var best string
		var bestAPILevel *version.Version
		for _, image := range installed {
			parts := strings.Split(image, ";")
			if len(parts) != 4 || parts[2] != tag || parts[3] != abi {
				continue
			}
			apiLevel, err := version.NewVersion(strings.TrimPrefix(parts[1], "android-"))
			if err != nil {
				continue
			}
			if bestAPILevel == nil || apiLevel.GreaterThan(bestAPILevel) {
				best, bestAPILevel = image, apiLevel
			}
		}
		if best != "" {
			return best, nil
		}
	}
	return "", fmt.Errorf("no preinstalled %s system image (%s) in the Android SDK: start an emulator before this Step, for example with AVD Manager", abi, strings.Join(preferredSystemImageTags, ", "))
}

func hostABI(goarch string) string {
	if goarch == "arm64" {
		return "arm64-v8a"
	}
	return "x86_64"
}
