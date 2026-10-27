package step

import (
	"fmt"
	"os"
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

func testArgs(config Config, attempt Attempt, flowPaths []string) []string {
	args := []string{"test", "--platform", string(config.App.Platform), "--format", "junit", "--output", attempt.JUnitPath, "--test-output-dir", attempt.TestOutputDir}
	if config.WorkspaceConfig != "" {
		args = append(args, "--config", config.WorkspaceConfig)
	}
	if len(config.IncludeTags) > 0 {
		args = append(args, "--include-tags", strings.Join(config.IncludeTags, ","))
	}
	if len(config.ExcludeTags) > 0 {
		args = append(args, "--exclude-tags", strings.Join(config.ExcludeTags, ","))
	}
	args = append(args, config.AdditionalArgs...)
	return append(args, flowPaths...)
}

// workspaceConfigPath returns the config.yaml (config.yml) of the first flow folder that has one, the way Maestro
// finds it. It returns none when additional_args passes --config.
func workspaceConfigPath(flowPaths, additionalArgs []string) string {
	if _, ok := configArgValue(additionalArgs); ok {
		return ""
	}
	for _, flowPath := range flowPaths {
		if info, err := os.Stat(flowPath); err != nil || !info.IsDir() {
			continue
		}
		for _, name := range []string{"config.yaml", "config.yml"} {
			if pth := filepath.Join(flowPath, name); exists(pth) {
				return pth
			}
		}
	}
	return ""
}

func configArgValue(args []string) (string, bool) {
	for i, arg := range args {
		if arg == "--config" {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", true
		}
		if value, ok := strings.CutPrefix(arg, "--config="); ok {
			return value, true
		}
	}
	return "", false
}

func installAppCommand(app App, deviceID, sdkDir string) (string, []string) {
	if app.Platform == PlatformAndroid {
		return adbPath(sdkDir), []string{"-s", deviceID, "install", "-r", app.Path}
	}
	return "xcrun", []string{"simctl", "install", deviceID, app.Path}
}
