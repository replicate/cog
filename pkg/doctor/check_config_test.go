package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
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

func TestConfigDeprecatedFieldsCheck_Clean(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
  python_requirements: "requirements.txt"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "requirements.txt", "torch==2.0.0\n")

	ctx := &CheckContext{ProjectDir: dir}
	check := &ConfigDeprecatedFieldsCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestConfigDeprecatedFieldsCheck_PythonPackages(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
  python_packages:
    - torch==2.0.0
predict: "predict.py:Predictor"
`)

	ctx := &CheckContext{ProjectDir: dir}
	check := &ConfigDeprecatedFieldsCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, SeverityWarning, findings[0].Severity)
	require.Contains(t, findings[0].Message, "python_packages")
}

func TestConfigDeprecatedFieldsCheck_PreInstall(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
  pre_install:
    - pip install something
predict: "predict.py:Predictor"
`)

	ctx := &CheckContext{ProjectDir: dir}
	check := &ConfigDeprecatedFieldsCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, SeverityWarning, findings[0].Severity)
	require.Contains(t, findings[0].Message, "pre_install")
}

func TestConfigPredictRefCheck_Valid(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "predict.py", `from cog import BasePredictor
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := buildTestCheckContext(t, dir)
	check := &ConfigPredictRefCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestConfigPredictRefCheck_MissingFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)

	ctx := buildTestCheckContext(t, dir)
	check := &ConfigPredictRefCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, SeverityError, findings[0].Severity)
	require.Contains(t, findings[0].Message, "predict.py")
	require.Contains(t, findings[0].Message, "not found")
}

func TestConfigPredictRefCheck_MissingClass(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:DoesNotExist"
`)
	writeFile(t, dir, "predict.py", `from cog import BasePredictor
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := buildTestCheckContext(t, dir)
	check := &ConfigPredictRefCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, SeverityError, findings[0].Severity)
	require.Contains(t, findings[0].Message, "DoesNotExist")
}

func TestConfigPredictRefCheck_NoPredictField(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
`)

	ctx := buildTestCheckContext(t, dir)
	check := &ConfigPredictRefCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings) // No predict field is valid (some projects are train-only)
}

// buildTestCheckContext creates a CheckContext by loading the cog.yaml in the given dir.
func buildTestCheckContext(t *testing.T, dir string) *CheckContext {
	t.Helper()
	ctx := &CheckContext{
		ProjectDir:  dir,
		PythonFiles: make(map[string]*ParsedFile),
	}

	configPath := filepath.Join(dir, "cog.yaml")
	configBytes, err := os.ReadFile(configPath)
	if err == nil {
		ctx.ConfigFile = configBytes
		f, err := os.Open(configPath)
		if err == nil {
			defer f.Close()
			loadResult, err := config.Load(f, dir)
			if err == nil {
				ctx.Config = loadResult.Config
			}
		}
	}

	return ctx
}

func TestConfigSchemaCheck_Valid(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "predict.py", `from cog import BasePredictor
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := &CheckContext{ProjectDir: dir}
	check := &ConfigSchemaCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestConfigSchemaCheck_InvalidSchema(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "2.7"
predict: "predict.py:Predictor"
`)

	ctx := &CheckContext{ProjectDir: dir}
	check := &ConfigSchemaCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, SeverityError, findings[0].Severity)
	require.Contains(t, findings[0].Message, "validation failed")
}

func TestConfigSchemaCheck_ParseError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build: [invalid yaml`)

	ctx := &CheckContext{ProjectDir: dir}
	check := &ConfigSchemaCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings) // Parse errors are handled by ConfigParseCheck
}

func TestConfigSchemaCheck_MissingFile(t *testing.T) {
	dir := t.TempDir()

	ctx := &CheckContext{ProjectDir: dir}
	check := &ConfigSchemaCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings) // Missing file handled by ConfigParseCheck
}

// writeFile is a test helper to create fixture files.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
}
