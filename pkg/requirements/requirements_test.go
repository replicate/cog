package requirements

import (
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func TestPythonPackages(t *testing.T) {
	tmpDir := t.TempDir()
	build := config.Build{
		PythonPackages: []string{"torch==2.5.1"},
	}
	config := config.Config{
		Build: &build,
	}
	_, err := GenerateRequirements(tmpDir, &config)
	require.ErrorContains(t, err, "python_packages is no longer supported, use python_requirements instead")
}

func TestPythonRequirements(t *testing.T) {
	srcDir := t.TempDir()
	reqFile := path.Join(srcDir, "requirements.txt")
	err := os.WriteFile(reqFile, []byte("torch==2.5.1"), 0o644)
	require.NoError(t, err)

	build := config.Build{
		PythonRequirements: reqFile,
	}
	config := config.Config{
		Build: &build,
	}
	tmpDir := t.TempDir()
	requirementsFile, err := GenerateRequirements(tmpDir, &config)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(tmpDir, "requirements.txt"), requirementsFile)
}
