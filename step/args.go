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
	if pth == "" {
		return App{}, nil
	}

	switch strings.ToLower(filepath.Ext(strings.TrimSuffix(pth, "/"))) {
	case ".apk":
		return App{Path: pth, Platform: PlatformAndroid}, nil
	case ".app":
		return App{Path: pth, Platform: PlatformIOS}, nil
	default:
		return App{}, fmt.Errorf("app_path must point to an .apk (Android) or a simulator .app (iOS), got: %s", pth)
	}
}

// When app_path yields both an .apk and an .app, the host decides: Bitrise runs Android
// emulators on Linux and iOS simulators on macOS.
func resolveApp(input string, goos string) (App, error) {
	var apps []App
	for _, pth := range splitLines(input) {
		app, err := parseApp(pth)
		if err != nil {
			return App{}, err
		}
		apps = append(apps, app)
	}
	if len(apps) == 0 {
		return App{}, nil
	}

	preferred := hostPlatform(goos)
	for _, app := range apps {
		if app.Platform == preferred {
			return app, nil
		}
	}
	return apps[0], nil
}

func hostPlatform(goos string) Platform {
	if goos == "darwin" {
		return PlatformIOS
	}
	return PlatformAndroid
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
	args := []string{"test", "--format", "junit", "--output", result.JUnitPath, "--test-output-dir", result.TestOutputDir}
	if len(config.IncludeTags) > 0 {
		args = append(args, "--include-tags", strings.Join(config.IncludeTags, ","))
	}
	if len(config.ExcludeTags) > 0 {
		args = append(args, "--exclude-tags", strings.Join(config.ExcludeTags, ","))
	}
	args = append(args, config.AdditionalArgs...)
	return append(args, config.FlowPaths...)
}

func installAppCommand(app App, sdkDir string) (string, []string) {
	if app.Platform == PlatformAndroid {
		return adbPath(sdkDir), []string{"install", "-r", app.Path}
	}
	return "xcrun", []string{"simctl", "install", "booted", app.Path}
}
