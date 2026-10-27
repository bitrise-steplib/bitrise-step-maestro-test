package step

import (
	"fmt"
	"strings"

	"github.com/hashicorp/go-version"
)

// The step passes --test-output-dir to `maestro test`, which this release already has.
const minimumMaestroVersion = "2.1.0"

// Maestro reads the "!pattern" exclusions of the workspace config's flows list from this release. Older releases take
// them as patterns that match no flow, so a quarantined flow found in a folder still runs.
const flowExclusionMaestroVersion = "2.9.0"

func excludesFlowsByPattern(maestroVersion string) bool {
	requested, err := version.NewVersion(maestroVersion)
	return err != nil || !requested.LessThan(version.Must(version.NewVersion(flowExclusionMaestroVersion)))
}

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
