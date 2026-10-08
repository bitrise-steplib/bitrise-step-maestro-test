package step

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseApp(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		want    App
		wantErr bool
	}{
		{name: "empty", path: "", want: App{}},
		{name: "apk", path: "build/app-debug.apk", want: App{Path: "build/app-debug.apk", Platform: PlatformAndroid}},
		{name: "app with trailing slash", path: "Build/Wikipedia.app/", want: App{Path: "Build/Wikipedia.app/", Platform: PlatformIOS}},
		{name: "ipa is rejected", path: "Wikipedia.ipa", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseApp(tt.path)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveApp(t *testing.T) {
	apk := App{Path: "app-debug.apk", Platform: PlatformAndroid}
	app := App{Path: "Runner.app", Platform: PlatformIOS}

	tests := []struct {
		name    string
		input   string
		goos    string
		want    App
		wantErr bool
	}{
		{name: "build outputs unset", input: "\n", goos: "linux", want: App{}},
		{name: "only the apk is set", input: "app-debug.apk\n", goos: "darwin", want: apk},
		{name: "only the app is set", input: "\nRunner.app", goos: "linux", want: app},
		{name: "both set on Linux", input: "app-debug.apk\nRunner.app", goos: "linux", want: apk},
		{name: "both set on macOS", input: "app-debug.apk\nRunner.app", goos: "darwin", want: app},
		{name: "user override", input: "custom/My App.apk", goos: "linux", want: App{Path: "custom/My App.apk", Platform: PlatformAndroid}},
		{name: "unsupported file", input: "app-debug.apk\nRunner.ipa", goos: "darwin", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveApp(tt.input, tt.goos)
			if tt.wantErr {
				require.Error(t, err)
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
		IncludeTags:    []string{"smoke", "android"},
		ExcludeTags:    []string{"flaky"},
		AdditionalArgs: []string{"-e", "USERNAME=bitrise"},
	}
	result := Result{JUnitPath: "/tmp/report.xml", TestOutputDir: "/tmp/out"}

	assert.Equal(t, []string{
		"test", "--format", "junit", "--output", "/tmp/report.xml", "--test-output-dir", "/tmp/out",
		"--include-tags", "smoke,android",
		"--exclude-tags", "flaky",
		"-e", "USERNAME=bitrise",
		".maestro",
	}, testArgs(config, result, ""))
}

func TestTestArgs_Minimal(t *testing.T) {
	result := Result{JUnitPath: "/tmp/report.xml", TestOutputDir: "/tmp/out"}
	assert.Equal(t, []string{
		"test", "--format", "junit", "--output", "/tmp/report.xml", "--test-output-dir", "/tmp/out", "flows/login.yaml",
	}, testArgs(Config{FlowPaths: []string{"flows/login.yaml"}}, result, ""))
}

func TestTestArgs_MultipleFlowPaths(t *testing.T) {
	result := Result{JUnitPath: "/tmp/report.xml", TestOutputDir: "/tmp/out"}
	assert.Equal(t, []string{
		"test", "--format", "junit", "--output", "/tmp/report.xml", "--test-output-dir", "/tmp/out", "maestro/entry", "maestro/unlock",
	}, testArgs(Config{FlowPaths: []string{"maestro/entry", "maestro/unlock"}}, result, ""))
}

func TestSplitLines(t *testing.T) {
	assert.Equal(t, []string{"maestro/entry", "maestro/my flows,v2"}, splitLines(" maestro/entry\n\n maestro/my flows,v2 \n"))
	assert.Nil(t, splitLines(" \n"))
}

func TestInstallAppCommand(t *testing.T) {
	name, args := installAppCommand(App{Path: "app.apk", Platform: PlatformAndroid}, "", "")
	assert.Equal(t, "adb", name)
	assert.Equal(t, []string{"install", "-r", "app.apk"}, args)

	name, args = installAppCommand(App{Path: "My.app", Platform: PlatformIOS}, "", "")
	assert.Equal(t, "xcrun", name)
	assert.Equal(t, []string{"simctl", "install", "booted", "My.app"}, args)
}

func TestInstallAppCommand_Device(t *testing.T) {
	_, args := installAppCommand(App{Path: "app.apk", Platform: PlatformAndroid}, "emulator-5554", "")
	assert.Equal(t, []string{"-s", "emulator-5554", "install", "-r", "app.apk"}, args)

	_, args = installAppCommand(App{Path: "My.app", Platform: PlatformIOS}, "UDID-1", "")
	assert.Equal(t, []string{"simctl", "install", "UDID-1", "My.app"}, args)
}

func TestTestArgs_Device(t *testing.T) {
	result := Result{JUnitPath: "/tmp/report.xml", TestOutputDir: "/tmp/out"}
	assert.Equal(t, []string{
		"test", "--format", "junit", "--output", "/tmp/report.xml", "--test-output-dir", "/tmp/out", "--device", "emulator-5554", ".maestro",
	}, testArgs(Config{FlowPaths: []string{".maestro"}}, result, "emulator-5554"))
}

func TestResolvePlatform(t *testing.T) {
	assert.Equal(t, PlatformAndroid, resolvePlatform(App{Platform: PlatformAndroid}, "darwin"))
	assert.Equal(t, PlatformIOS, resolvePlatform(App{}, "darwin"))
	assert.Equal(t, PlatformAndroid, resolvePlatform(App{}, "linux"))
}
