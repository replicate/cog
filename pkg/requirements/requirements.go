package requirements

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/replicate/cog/pkg/config"
)

const REQUIREMENTS_FILE = "requirements.txt"

func GenerateRequirements(tmpDir string, cfg *config.Config) (string, error) {
	if len(cfg.Build.PythonPackages) > 0 {
		return "", fmt.Errorf("python_packages is no longer supported, use python_requirements instead")
	}

	// No Python requirements
	if cfg.Build.PythonRequirements == "" {
		return "", nil
	}

	bs, err := os.ReadFile(cfg.Build.PythonRequirements)
	if err != nil {
		return "", err
	}
	requirements := string(bs)

	// Check against the old requirements
	requirementsFile := filepath.Join(tmpDir, REQUIREMENTS_FILE)
	if _, err := os.Stat(requirementsFile); err == nil {
		bs, err = os.ReadFile(requirementsFile)
		if err != nil {
			return "", err
		}
		if string(bs) == requirements {
			return requirementsFile, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	// Write out a new requirements file
	err = os.WriteFile(requirementsFile, []byte(requirements), 0o644)
	if err != nil {
		return "", err
	}
	return requirementsFile, err
}

func CurrentRequirements(tmpDir string) (string, error) {
	requirementsFile := filepath.Join(tmpDir, REQUIREMENTS_FILE)
	_, err := os.Stat(requirementsFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return requirementsFile, nil
}
