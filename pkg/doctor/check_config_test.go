package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigParseCheck_Valid(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)

	ctx := &CheckContext{ProjectDir: dir}
	ctx.ConfigFile, _ = os.ReadFile(filepath.Join(dir, "cog.yaml"))

	check := &ConfigParseCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestConfigParseCheck_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build: [invalid yaml`)

	ctx := &CheckContext{ProjectDir: dir}
	ctx.ConfigFile, _ = os.ReadFile(filepath.Join(dir, "cog.yaml"))

	check := &ConfigParseCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, SeverityError, findings[0].Severity)
	require.Contains(t, findings[0].Message, "cog.yaml")
}

func TestConfigParseCheck_MissingFile(t *testing.T) {
	dir := t.TempDir()

	ctx := &CheckContext{ProjectDir: dir}

	check := &ConfigParseCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Contains(t, findings[0].Message, "cog.yaml not found")
}

// writeFile is a test helper to create fixture files.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
}
