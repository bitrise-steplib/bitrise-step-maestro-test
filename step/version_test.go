package step

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExcludesFlowsByPattern(t *testing.T) {
	assert.False(t, excludesFlowsByPattern("2.8.0"))
	assert.True(t, excludesFlowsByPattern("2.9.0"))
	assert.True(t, excludesFlowsByPattern("2.11.0"))
}

func TestResolveMaestroVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "newer version", input: "2.12.0", want: "2.12.0"},
		{name: "minimum version", input: "2.1.0", want: "2.1.0"},
		{name: "surrounding whitespace is ignored", input: " 2.10.0 ", want: "2.10.0"},
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
