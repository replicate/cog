package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func TestValidateSDKVersion(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		sdkWheel string
		wantErr  string
	}{
		{
			name:    "allows unpinned SDK",
			cfg:     &config.Config{},
			wantErr: "",
		},
		{
			name: "allows minimum SDK from config",
			cfg: &config.Config{
				Build: &config.Build{SDKVersion: "0.17.0"},
			},
			wantErr: "",
		},
		{
			name: "rejects old SDK from config",
			cfg: &config.Config{
				Build: &config.Build{SDKVersion: "0.16.12"},
			},
			wantErr: "SDK version 0.16.12 is not supported by static schema generation",
		},
		{
			name:     "rejects old SDK from env wheel",
			cfg:      &config.Config{},
			sdkWheel: "pypi:0.16.12",
			wantErr:  "SDK version 0.16.12 is not supported by static schema generation",
		},
		{
			name: "env wheel takes precedence over config",
			cfg: &config.Config{
				Build: &config.Build{SDKVersion: "0.16.12"},
			},
			sdkWheel: "pypi:0.17.0",
			wantErr:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("COG_SDK_WHEEL", tt.sdkWheel)

			err := ValidateSDKVersion(tt.cfg)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
