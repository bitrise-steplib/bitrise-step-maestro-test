package step

import (
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-android/v2/sdk"
	"github.com/bitrise-io/go-utils/v2/pathutil"
)

// adbPath is the SDK's adb, the one AVD Manager runs too, and adb on PATH only without an SDK: adb clients of
// different versions restart each other's server, which drops the emulator connection.
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
