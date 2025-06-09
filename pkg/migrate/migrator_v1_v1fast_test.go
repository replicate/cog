package migrate

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/coglog"
	"github.com/replicate/cog/pkg/requirements"
)

func TestMigrate(t *testing.T) {
	// Set our new working directory to a temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		err := os.Chdir(originalDir)
		require.NoError(t, err)
	}()
	dir := t.TempDir()
	err = os.Chdir(dir)
	require.NoError(t, err)

	// Write our test configs/code
	configFilepath := filepath.Join(dir, "cog.yaml")
	file, err := os.Create(configFilepath)
	require.NoError(t, err)
	_, err = file.WriteString(`build:
  python_version: "3.11"
  fast: true
  python_packages:
    - "pillow"
  run:
    - command: curl -o /usr/local/bin/pget -L \"https://github.com/replicate/pget/releases/latest/download/pget_$(uname -s)_$(uname -m)\" && chmod +x /usr/local/bin/pget
predict: "predict.py:Predictor"
`)
	require.NoError(t, err)
	err = file.Close()
	require.NoError(t, err)
	pythonFilepath := filepath.Join(dir, "predict.py")
	file, err = os.Create(pythonFilepath)
	require.NoError(t, err)
	_, err = file.WriteString(`from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(self, s: str = Input(description="My Input Description", default=None)) -> str:
        return "hello " + s
`)
	require.NoError(t, err)
	err = file.Close()
	require.NoError(t, err)

	// Perform the migration
	logCtx := coglog.NewMigrateLogContext(true)
	migrator := NewMigratorV1ToV1Fast(false, logCtx)
	err = migrator.Migrate(t.Context(), "cog.yaml")
	require.NoError(t, err)

	// Check config output
	file, err = os.Open(configFilepath)
	require.NoError(t, err)
	content, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, `build:
    python_version: "3.11"
    fast: true
    python_requirements: requirements.txt
predict: "predict.py:Predictor"
`, string(content))
	err = file.Close()
	require.NoError(t, err)

	// Check python code output
	file, err = os.Open(pythonFilepath)
	require.NoError(t, err)
	content, err = io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, `from typing import Optional
from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(self, s: Optional[str] = Input(description="My Input Description", default=None)) -> str:
        return "hello " + s
`, string(content))
	err = file.Close()
	require.NoError(t, err)

	// Check requirements.txt
	file, err = os.Open(filepath.Join(dir, requirements.RequirementsFile))
	require.NoError(t, err)
	content, err = io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, `pillow`, string(content))
	err = file.Close()
	require.NoError(t, err)
}

func TestMigrateGPU(t *testing.T) {
	// Set our new working directory to a temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		err := os.Chdir(originalDir)
		require.NoError(t, err)
	}()
	dir := t.TempDir()
	err = os.Chdir(dir)
	require.NoError(t, err)

	// Write our test configs/code
	configFilepath := filepath.Join(dir, "cog.yaml")
	file, err := os.Create(configFilepath)
	require.NoError(t, err)
	_, err = file.WriteString(`build:
  python_version: "3.11"
  fast: true
  python_packages:
    - "pillow"
  run:
    - command: curl -o /usr/local/bin/pget -L \"https://github.com/replicate/pget/releases/latest/download/pget_$(uname -s)_$(uname -m)\" && chmod +x /usr/local/bin/pget
  gpu: true
predict: "predict.py:Predictor"
`)
	require.NoError(t, err)
	err = file.Close()
	require.NoError(t, err)
	pythonFilepath := filepath.Join(dir, "predict.py")
	file, err = os.Create(pythonFilepath)
	require.NoError(t, err)
	_, err = file.WriteString(`from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(self, s: str = Input(description="My Input Description", default=None)) -> str:
        return "hello " + s
`)
	require.NoError(t, err)
	err = file.Close()
	require.NoError(t, err)

	// Perform the migration
	logCtx := coglog.NewMigrateLogContext(true)
	migrator := NewMigratorV1ToV1Fast(false, logCtx)
	err = migrator.Migrate(t.Context(), "cog.yaml")
	require.NoError(t, err)

	// Check config output
	file, err = os.Open(configFilepath)
	require.NoError(t, err)
	content, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, `build:
    python_version: "3.11"
    fast: true
    gpu: true
    python_requirements: requirements.txt
predict: "predict.py:Predictor"
`, string(content))
	err = file.Close()
	require.NoError(t, err)
}

func TestMigrateYAMLComments(t *testing.T) {
	// Set our new working directory to a temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		err := os.Chdir(originalDir)
		require.NoError(t, err)
	}()
	dir := t.TempDir()
	err = os.Chdir(dir)
	require.NoError(t, err)

	// Write our test configs/code
	configFilepath := filepath.Join(dir, "cog.yaml")
	file, err := os.Create(configFilepath)
	require.NoError(t, err)
	_, err = file.WriteString(`# Here we have a YAML comment
build:
  python_version: "3.11"
  fast: true
  python_packages:
    - "pillow"
  run:
    - command: curl -o /usr/local/bin/pget -L \"https://github.com/replicate/pget/releases/latest/download/pget_$(uname -s)_$(uname -m)\" && chmod +x /usr/local/bin/pget
  gpu: true
predict: "predict.py:Predictor"
`)
	require.NoError(t, err)
	err = file.Close()
	require.NoError(t, err)
	pythonFilepath := filepath.Join(dir, "predict.py")
	file, err = os.Create(pythonFilepath)
	require.NoError(t, err)
	_, err = file.WriteString(`from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def predict(self, s: str = Input(description="My Input Description", default=None)) -> str:
        return "hello " + s
`)
	require.NoError(t, err)
	err = file.Close()
	require.NoError(t, err)

	// Perform the migration
	logCtx := coglog.NewMigrateLogContext(true)
	migrator := NewMigratorV1ToV1Fast(false, logCtx)
	err = migrator.Migrate(t.Context(), "cog.yaml")
	require.NoError(t, err)

	// Check config output
	file, err = os.Open(configFilepath)
	require.NoError(t, err)
	content, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, `# Here we have a YAML comment
build:
    python_version: "3.11"
    fast: true
    gpu: true
    python_requirements: requirements.txt
predict: "predict.py:Predictor"
`, string(content))
	err = file.Close()
	require.NoError(t, err)
}
