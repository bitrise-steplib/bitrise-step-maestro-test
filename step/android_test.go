package step

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInstallAppCommand_UsesTheSDKAdb(t *testing.T) {
	sdkDir := fakeAndroidSDK(t, "#!/bin/sh\n")

	name, _ := installAppCommand(App{Path: "app.apk", Platform: PlatformAndroid}, "emulator-5554", sdkDir)
	assert.Equal(t, filepath.Join(sdkDir, "platform-tools", "adb"), name)

	name, _ = installAppCommand(App{Path: "app.apk", Platform: PlatformAndroid}, "emulator-5554", t.TempDir())
	assert.Equal(t, "adb", name, "adb on PATH when the SDK has none")
}
