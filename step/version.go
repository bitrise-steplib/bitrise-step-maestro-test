package step

import (
	"fmt"
	"strings"

	"github.com/hashicorp/go-version"
)

const (
	bundledMaestroVersion = "2.11.0"
	// The step passes --test-output-dir and --device to `maestro test`, both available from this release.
	minimumMaestroVersion = "2.1.0"
)

type maestroVersion struct {
	Version    string
	Overridden bool
}

func resolveMaestroVersion(input string) (maestroVersion, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return maestroVersion{Version: bundledMaestroVersion}, nil
	}

	requested, err := version.NewVersion(input)
	if err != nil {
		return maestroVersion{}, fmt.Errorf("maestro_version: invalid version %q: %w", input, err)
	}
	if requested.LessThan(version.Must(version.NewVersion(minimumMaestroVersion))) {
		return maestroVersion{}, fmt.Errorf("maestro_version: %s is not supported, the minimum is %s", input, minimumMaestroVersion)
	}

	return maestroVersion{Version: input, Overridden: input != bundledMaestroVersion}, nil
}
