package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateRequirements(t *testing.T) {
	tmpDir := t.TempDir()
	build := Build{
		PythonPackages: []string{"torch==2.5.1"},
	}
	config := Config{
		Build: &build,
	}
	requirementsFile, err := GenerateRequirements(tmpDir, &config)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(tmpDir, "requirements.txt"), requirementsFile)
}
