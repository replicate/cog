package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Build struct {
	GPU        bool `yaml:"gpu"`
	Fast       bool `yaml:"fast"`
	CogRuntime bool `yaml:"cog_runtime"`
}

type CogConcurrency struct {
	Max int `yaml:"max"`
}

type CogYaml struct {
	Build       Build          `yaml:"build"`
	Concurrency CogConcurrency `yaml:"concurrency"`
	Predict     string         `yaml:"predict"`
}

func ReadCogYaml(dir string) (*CogYaml, error) {
	var cogYaml CogYaml
	bs, err := os.ReadFile(filepath.Join(dir, "cog.yaml")) //nolint:gosec // expected dynamic path
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(bs, &cogYaml); err != nil {
		return nil, err
	}
	return &cogYaml, nil
}

func (y *CogYaml) PredictModuleAndPredictor() (string, string, error) {
	parts := strings.Split(y.Predict, ":")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid predict: %s", y.Predict)
	}
	moduleName := strings.TrimSuffix(parts[0], ".py")
	predictorName := parts[1]
	return moduleName, predictorName, nil
}
