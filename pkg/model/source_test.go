package model

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func TestNewSourceFromConfig(t *testing.T) {
	cfg := &config.Config{
		Build: &config.Build{
			GPU:           true,
			PythonVersion: "3.11",
		},
	}
	projectDir := "/path/to/project"

	src := NewSourceFromConfig(cfg, projectDir)

	require.NotNil(t, src)
	require.Equal(t, cfg, src.Config)
	require.Equal(t, projectDir, src.ProjectDir)
}

func TestNewSourceFromConfig_NilConfig(t *testing.T) {
	src := NewSourceFromConfig(nil, "/path/to/project")

	require.NotNil(t, src)
	require.Nil(t, src.Config)
	require.Equal(t, "/path/to/project", src.ProjectDir)
}
