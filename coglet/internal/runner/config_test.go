package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadCogYaml(t *testing.T) {
	t.Parallel()

	t.Run("valid cog.yaml", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		// Using JSON format for test convenience (JSON is valid YAML)
		cogYaml := `{"build": {"python_version": "3.8"}, "predict": "predict.py:Predictor", "concurrency": {"max": 4}}`
		err := os.WriteFile(filepath.Join(tempDir, "cog.yaml"), []byte(cogYaml), 0o644)
		require.NoError(t, err)

		config, err := ReadCogYaml(tempDir)
		require.NoError(t, err)
		assert.Equal(t, "predict.py:Predictor", config.Predict)
		assert.Equal(t, 4, config.Concurrency.Max)
	})

	t.Run("missing cog.yaml", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		config, err := ReadCogYaml(tempDir)
		require.Error(t, err)
		assert.Nil(t, config)
	})

	t.Run("invalid yaml", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		invalidYaml := `{"build": {"python_version": "3.8", "predict": "predict.py:Predictor"`
		err := os.WriteFile(filepath.Join(tempDir, "cog.yaml"), []byte(invalidYaml), 0o644)
		require.NoError(t, err)

		config, err := ReadCogYaml(tempDir)
		require.Error(t, err)
		assert.Nil(t, config)
	})
}

func TestCogYamlPredictModuleAndPredictor(t *testing.T) {
	t.Parallel()

	t.Run("valid predict format", func(t *testing.T) {
		t.Parallel()

		cogYaml := &CogYaml{
			Predict: "predict.py:Predictor",
		}

		module, predictor, err := cogYaml.PredictModuleAndPredictor()
		require.NoError(t, err)
		assert.Equal(t, "predict", module)
		assert.Equal(t, "Predictor", predictor)
	})

	t.Run("predict without .py extension", func(t *testing.T) {
		t.Parallel()

		cogYaml := &CogYaml{
			Predict: "predictor:Model",
		}

		module, predictor, err := cogYaml.PredictModuleAndPredictor()
		require.NoError(t, err)
		assert.Equal(t, "predictor", module)
		assert.Equal(t, "Model", predictor)
	})

	t.Run("invalid predict format", func(t *testing.T) {
		t.Parallel()

		cogYaml := &CogYaml{
			Predict: "invalid_format",
		}

		module, predictor, err := cogYaml.PredictModuleAndPredictor()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid predict")
		assert.Empty(t, module)
		assert.Empty(t, predictor)
	})

	t.Run("multiple colons in predict", func(t *testing.T) {
		t.Parallel()

		cogYaml := &CogYaml{
			Predict: "predict.py:Model:Extra",
		}

		module, predictor, err := cogYaml.PredictModuleAndPredictor()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid predict")
		assert.Empty(t, module)
		assert.Empty(t, predictor)
	})
}
