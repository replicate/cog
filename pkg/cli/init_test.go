package cli

import (
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInit(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.Chdir(dir))

	// Reset flags
	skipExisting = false
	overwriteExisting = false

	err := initCommand(nil, []string{})
	require.NoError(t, err)

	require.FileExists(t, path.Join(dir, ".dockerignore"))
	require.FileExists(t, path.Join(dir, "cog.yaml"))
	require.FileExists(t, path.Join(dir, "predict.py"))
}

func TestInitSkipExisting(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.Chdir(dir))

	// Reset flags
	skipExisting = false
	overwriteExisting = false

	// First run to create files
	err := initCommand(nil, []string{})
	require.NoError(t, err)

	require.FileExists(t, path.Join(dir, ".dockerignore"))
	require.FileExists(t, path.Join(dir, "cog.yaml"))
	require.FileExists(t, path.Join(dir, "predict.py"))

	// update the file to show that its the same file after the second run
	require.NoError(t, os.WriteFile(path.Join(dir, "cog.yaml"), []byte("test123"), 0o644))
	require.NoError(t, os.WriteFile(path.Join(dir, "predict.py"), []byte("test456"), 0o644))
	require.NoError(t, os.WriteFile(path.Join(dir, ".dockerignore"), []byte("test789"), 0o644))

	// Second run with skip-existing flag set
	// mimics the behavior of `cog init --skip-existing`
	skipExisting = true
	err = initCommand(nil, []string{})
	require.NoError(t, err)

	require.FileExists(t, path.Join(dir, ".dockerignore"))
	require.FileExists(t, path.Join(dir, "cog.yaml"))
	require.FileExists(t, path.Join(dir, "predict.py"))

	// check that the files are the same as the first run
	content, err := os.ReadFile(path.Join(dir, "cog.yaml"))
	require.NoError(t, err)
	require.Equal(t, []byte("test123"), content)

	content, err = os.ReadFile(path.Join(dir, "predict.py"))
	require.NoError(t, err)
	require.Equal(t, []byte("test456"), content)

	content, err = os.ReadFile(path.Join(dir, ".dockerignore"))
	require.NoError(t, err)
	require.Equal(t, []byte("test789"), content)
}

func TestInitOverwriteExisting(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.Chdir(dir))

	// Reset flags
	skipExisting = false
	overwriteExisting = false

	// First run to create files
	err := initCommand(nil, []string{})
	require.NoError(t, err)

	require.FileExists(t, path.Join(dir, ".dockerignore"))
	require.FileExists(t, path.Join(dir, "cog.yaml"))
	require.FileExists(t, path.Join(dir, "predict.py"))

	// update the file to show that its the same file after the second run
	require.NoError(t, os.WriteFile(path.Join(dir, "cog.yaml"), []byte("test123"), 0o644))
	require.NoError(t, os.WriteFile(path.Join(dir, "predict.py"), []byte("test456"), 0o644))
	require.NoError(t, os.WriteFile(path.Join(dir, ".dockerignore"), []byte("test789"), 0o644))

	// Second run with overwrite-existing flag set
	// mimics the behavior of `cog init --overwrite-existing`
	overwriteExisting = true
	err = initCommand(nil, []string{})
	require.NoError(t, err)

	require.FileExists(t, path.Join(dir, ".dockerignore"))
	require.FileExists(t, path.Join(dir, "cog.yaml"))
	require.FileExists(t, path.Join(dir, "predict.py"))

	// check that the files are NOT the same as the first run
	content, err := os.ReadFile(path.Join(dir, "cog.yaml"))
	require.NoError(t, err)
	require.NotEqual(t, []byte("test123"), content)

	content, err = os.ReadFile(path.Join(dir, "predict.py"))
	require.NoError(t, err)
	require.NotEqual(t, []byte("test456"), content)

	content, err = os.ReadFile(path.Join(dir, ".dockerignore"))
	require.NoError(t, err)
	require.NotEqual(t, []byte("test789"), content)
}
