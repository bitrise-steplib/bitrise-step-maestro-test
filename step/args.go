package step

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kballard/go-shellquote"
)

type Platform string

const (
	PlatformAndroid Platform = "android"
	PlatformIOS     Platform = "ios"
)

type App struct {
	Path     string
	Platform Platform
}

func parseApp(pth string) (App, error) {
	pth = strings.TrimSpace(pth)
	switch strings.ToLower(filepath.Ext(strings.TrimSuffix(pth, "/"))) {
	case ".apk":
		return App{Path: pth, Platform: PlatformAndroid}, nil
	case ".app":
		return App{Path: pth, Platform: PlatformIOS}, nil
	case ".aab":
		return App{}, fmt.Errorf("app_path must point to an .apk, an Android App Bundle cannot be installed on an emulator; convert it with the Export Universal APK Step first, got: %s", pth)
	default:
		return App{}, fmt.Errorf("app_path must point to an .apk (Android) or a simulator .app (iOS), got: %s", pth)
	}
}

func splitList(input string) []string {
	var items []string
	for _, item := range strings.FieldsFunc(input, func(r rune) bool { return r == ',' || r == '\n' }) {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func splitLines(input string) []string {
	var lines []string
	for _, line := range strings.Split(input, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func splitArgs(input string) ([]string, error) {
	if strings.TrimSpace(input) == "" {
		return nil, nil
	}
	return shellquote.Split(input)
}

func testArgs(config Config, result Result) []string {
	args := []string{"test", "--platform", string(config.App.Platform), "--format", "junit", "--output", result.JUnitPath, "--test-output-dir", result.TestOutputDir}
	if len(config.IncludeTags) > 0 {
		args = append(args, "--include-tags", strings.Join(config.IncludeTags, ","))
	}
	if len(config.ExcludeTags) > 0 {
		args = append(args, "--exclude-tags", strings.Join(config.ExcludeTags, ","))
	}
	args = append(args, config.AdditionalArgs...)
	return append(args, config.FlowPaths...)
}

func installAppCommand(app App, deviceID, sdkDir string) (string, []string) {
	if app.Platform == PlatformAndroid {
		return adbPath(sdkDir), []string{"-s", deviceID, "install", "-r", app.Path}
	}
	return "xcrun", []string{"simctl", "install", deviceID, app.Path}
}
