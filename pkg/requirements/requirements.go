package requirements

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/replicate/cog/pkg/config"
)

const REQUIREMENTS_FILE = "requirements.txt"

func GenerateRequirements(tmpDir string, cfg *config.Config) (string, error) {
	// Deduplicate packages between the requirements.txt and the python packages directive.
	packageNames := make(map[string]string)

	// Read the python packages configuration.
	for _, requirement := range cfg.Build.PythonPackages {
		packageName, err := config.PackageName(requirement)
		if err != nil {
			return "", err
		}
		packageNames[packageName] = requirement
	}

	// Read the python requirements.
	if cfg.Build.PythonRequirements != "" {
		fh, err := os.Open(cfg.Build.PythonRequirements)
		if err != nil {
			return "", err
		}
		scanner := bufio.NewScanner(fh)
		for scanner.Scan() {
			requirement := scanner.Text()
			packageName, err := config.PackageName(requirement)
			if err != nil {
				return "", err
			}
			packageNames[packageName] = requirement
		}
	}

	// If we don't have any packages skip further processing
	if len(packageNames) == 0 {
		return "", nil
	}

	// Sort the package names by alphabetical order.
	keys := make([]string, 0, len(packageNames))
	for k := range packageNames {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Render the expected contents
	requirementsContent := ""
	for _, k := range keys {
		requirementsContent += packageNames[k] + "\n"
	}

	// Check against the old requirements contents
	requirementsFile := filepath.Join(tmpDir, REQUIREMENTS_FILE)
	_, err := os.Stat(requirementsFile)
	if !errors.Is(err, os.ErrNotExist) {
		bytes, err := os.ReadFile(requirementsFile)
		if err != nil {
			return "", err
		}
		oldRequirementsContents := string(bytes)
		if oldRequirementsContents == requirementsFile {
			return requirementsFile, nil
		}
	}

	// Write out a new requirements file
	err = os.WriteFile(requirementsFile, []byte(requirementsContent), 0o644)
	if err != nil {
		return "", err
	}
	return requirementsFile, nil
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
