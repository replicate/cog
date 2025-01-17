package dockerfile

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func TestGenerateRequirements(t *testing.T) {
	tmpDir := t.TempDir()
	build := config.Build{
		PythonPackages: []string{"torch==2.5.1"},
	}
	config := config.Config{
		Build: &build,
	}
	requirementsFile, err := GenerateRequirements(tmpDir, &config)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(tmpDir, "requirements.txt"), requirementsFile)
}
