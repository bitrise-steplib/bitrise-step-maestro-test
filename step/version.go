package step

import (
	"fmt"
	"strings"

	"github.com/hashicorp/go-version"
)

// The step passes --test-output-dir to `maestro test`, which this release already has.
const minimumMaestroVersion = "2.1.0"

func resolveMaestroVersion(input string) (string, error) {
	input = strings.TrimSpace(input)
	requested, err := version.NewVersion(input)
	if err != nil {
		return "", fmt.Errorf("maestro_version: invalid version %q: %w", input, err)
	}
	if requested.LessThan(version.Must(version.NewVersion(minimumMaestroVersion))) {
		return "", fmt.Errorf("maestro_version: %s is not supported, the minimum is %s", input, minimumMaestroVersion)
	}
	return input, nil
}
