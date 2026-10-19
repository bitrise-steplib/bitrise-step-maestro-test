package step

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallAppCommand_UsesTheSDKAdb(t *testing.T) {
	sdkDir := t.TempDir()
	sdkAdb := filepath.Join(sdkDir, "platform-tools", "adb")
	writeFile(t, sdkAdb, "#!/bin/sh\n")
	require.NoError(t, os.Chmod(sdkAdb, 0755))

	name, _ := installAppCommand(App{Path: "app.apk", Platform: PlatformAndroid}, sdkDir)
	assert.Equal(t, sdkAdb, name)

	name, _ = installAppCommand(App{Path: "app.apk", Platform: PlatformAndroid}, t.TempDir())
	assert.Equal(t, "adb", name, "adb on PATH when the SDK has none")
}
