package step

import (
	"testing"

	"github.com/hashicorp/go-version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveMaestroVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    maestroVersion
		wantErr string
	}{
		{name: "empty uses the bundled version", input: "", want: maestroVersion{Version: bundledMaestroVersion}},
		{name: "bundled version is not an override", input: bundledMaestroVersion, want: maestroVersion{Version: bundledMaestroVersion}},
		{name: "newer version", input: "2.12.0", want: maestroVersion{Version: "2.12.0", Overridden: true}},
		{name: "minimum version", input: "2.1.0", want: maestroVersion{Version: "2.1.0", Overridden: true}},
		{name: "surrounding whitespace is ignored", input: " 2.10.0 ", want: maestroVersion{Version: "2.10.0", Overridden: true}},
		{name: "below minimum", input: "2.0.10", wantErr: "not supported"},
		{name: "1.x", input: "1.41.0", wantErr: "not supported"},
		{name: "invalid", input: "latest", wantErr: "invalid version"},
		{name: "release tag form is invalid", input: "cli-2.10.0", wantErr: "invalid version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveMaestroVersion(tt.input)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBundledVersionIsSupported(t *testing.T) {
	bundled := version.Must(version.NewVersion(bundledMaestroVersion))
	minimum := version.Must(version.NewVersion(minimumMaestroVersion))
	assert.False(t, bundled.LessThan(minimum))
}
