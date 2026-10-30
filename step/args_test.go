package step

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseApp(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		want    App
		wantErr string
	}{
		{name: "apk", path: "build/app-debug.apk", want: App{Path: "build/app-debug.apk", Platform: PlatformAndroid}},
		{name: "path with spaces", path: " custom/My App.apk\n", want: App{Path: "custom/My App.apk", Platform: PlatformAndroid}},
		{name: "app with trailing slash", path: "Build/Wikipedia.app/", want: App{Path: "Build/Wikipedia.app/", Platform: PlatformIOS}},
		{name: "ipa is rejected", path: "Wikipedia.ipa", wantErr: "simulator .app"},
		{name: "aab points to the converter Step", path: "app-release.aab", wantErr: "Export Universal APK"},
		{name: "empty is rejected", path: " ", wantErr: "simulator .app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseApp(tt.path)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSplitList(t *testing.T) {
	assert.Equal(t, []string{"smoke", "android", "login"}, splitList(" smoke, android\nlogin,,"))
	assert.Nil(t, splitList(""))
}

func TestTestArgs(t *testing.T) {
	config := Config{
		FlowPaths:      []string{".maestro"},
		App:            App{Path: "app.apk", Platform: PlatformAndroid},
		IncludeTags:    []string{"smoke", "android"},
		ExcludeTags:    []string{"flaky"},
		AdditionalArgs: []string{"--no-reinstall-driver"},
	}
	attempt := Attempt{JUnitPath: "/tmp/report.xml", TestOutputDir: "/tmp/out"}

	assert.Equal(t, []string{
		"test", "--platform", "android", "--format", "junit", "--output", "/tmp/report.xml", "--test-output-dir", "/tmp/out",
		"--include-tags", "smoke,android",
		"--exclude-tags", "flaky",
		"--no-reinstall-driver",
		".maestro",
	}, testArgs(config, attempt, config.FlowPaths))
}

func TestTestArgs_Minimal(t *testing.T) {
	attempt := Attempt{JUnitPath: "/tmp/report.xml", TestOutputDir: "/tmp/out"}
	config := Config{FlowPaths: []string{"flows/login.yaml"}, App: App{Path: "My.app", Platform: PlatformIOS}}
	assert.Equal(t, []string{
		"test", "--platform", "ios", "--format", "junit", "--output", "/tmp/report.xml", "--test-output-dir", "/tmp/out", "flows/login.yaml",
	}, testArgs(config, attempt, config.FlowPaths), "Maestro runs on a device of the app's platform when an emulator and a simulator both run")
}

func TestTestArgs_MultipleFlowPaths(t *testing.T) {
	attempt := Attempt{JUnitPath: "/tmp/report.xml", TestOutputDir: "/tmp/out"}
	config := Config{FlowPaths: []string{"maestro/entry", "maestro/unlock"}, App: App{Path: "app.apk", Platform: PlatformAndroid}}
	assert.Equal(t, []string{
		"test", "--platform", "android", "--format", "junit", "--output", "/tmp/report.xml", "--test-output-dir", "/tmp/out", "maestro/entry", "maestro/unlock",
	}, testArgs(config, attempt, config.FlowPaths))
}

func TestTestArgs_WorkspaceConfig(t *testing.T) {
	attempt := Attempt{JUnitPath: "/tmp/report.xml", TestOutputDir: "/tmp/out"}
	config := Config{FlowPaths: []string{".maestro"}, App: App{Path: "app.apk", Platform: PlatformAndroid}, WorkspaceConfig: ".maestro/config.yaml"}
	assert.Equal(t, []string{
		"test", "--platform", "android", "--format", "junit", "--output", "/tmp/report.xml", "--test-output-dir", "/tmp/out", "--config", ".maestro/config.yaml", ".maestro/login.yaml",
	}, testArgs(config, attempt, []string{".maestro/login.yaml"}), "a retry of flow files keeps the folder's config")
}

func TestConfigArg(t *testing.T) {
	value, ok := configArgValue([]string{"--device", "emulator-5554", "--config", "user.yaml"})
	assert.True(t, ok)
	assert.Equal(t, "user.yaml", value)
	value, ok = configArgValue([]string{"--config=user.yaml"})
	assert.True(t, ok)
	assert.Equal(t, "user.yaml", value)
	_, ok = configArgValue([]string{"--device", "emulator-5554"})
	assert.False(t, ok)

	assert.Equal(t, []string{"--device", "emulator-5554"}, withoutConfigArg([]string{"--config", "user.yaml", "--device", "emulator-5554"}))
	assert.Equal(t, []string{"--device", "emulator-5554"}, withoutConfigArg([]string{"--device", "emulator-5554", "--config=user.yaml"}))
}

func TestWorkspaceConfigPath(t *testing.T) {
	withoutConfig := t.TempDir()
	withYml := t.TempDir()
	writeFile(t, filepath.Join(withYml, "config.yml"), "flows: ['*']\n")
	withYaml := t.TempDir()
	writeFile(t, filepath.Join(withYaml, "config.yaml"), "flows: ['*']\n")
	flowFile := filepath.Join(withYaml, "login.yaml")
	writeFile(t, flowFile, "appId: org.wikipedia\n---\n- launchApp\n")

	assert.Equal(t, filepath.Join(withYml, "config.yml"), workspaceConfigPath([]string{withoutConfig, withYml, withYaml}, nil), "the first folder with a config wins")
	assert.Empty(t, workspaceConfigPath([]string{withYaml}, []string{"--config", "custom.yaml"}), "the user's --config is in the arguments already")
	assert.Empty(t, workspaceConfigPath([]string{withYaml}, []string{"--config=custom.yaml"}))
	assert.Empty(t, workspaceConfigPath([]string{flowFile}, nil), "Maestro does not look next to a flow file")
}

func TestSplitLines(t *testing.T) {
	assert.Equal(t, []string{"maestro/entry", "maestro/my flows,v2"}, splitLines(" maestro/entry\n\n maestro/my flows,v2 \n"))
	assert.Nil(t, splitLines(" \n"))
}

func TestInstallAppCommand(t *testing.T) {
	name, args := installAppCommand(App{Path: "app.apk", Platform: PlatformAndroid}, "emulator-5554", "")
	assert.Equal(t, "adb", name)
	assert.Equal(t, []string{"-s", "emulator-5554", "install", "-r", "app.apk"}, args)

	name, args = installAppCommand(App{Path: "My.app", Platform: PlatformIOS}, "A4199825", "")
	assert.Equal(t, "xcrun", name)
	assert.Equal(t, []string{"simctl", "install", "A4199825", "My.app"}, args)
}
