package patcher

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Patch reads a cog.yaml, applies patches, and writes it back
func Patch(cogYAMLPath string, sdkVersion string, overrides map[string]any) error {
	data, err := os.ReadFile(cogYAMLPath)
	if err != nil {
		return fmt.Errorf("reading cog.yaml: %w", err)
	}

	var config map[string]any
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parsing cog.yaml: %w", err)
	}

	if config == nil {
		config = make(map[string]any)
	}

	// Apply SDK version
	if sdkVersion != "" {
		build, ok := config["build"].(map[string]any)
		if !ok {
			build = make(map[string]any)
			config["build"] = build
		}
		build["sdk_version"] = sdkVersion
	}

	// Apply overrides
	if overrides != nil {
		config = deepMerge(config, overrides)
	}

	// Write back
	output, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshaling cog.yaml: %w", err)
	}

	if err := os.WriteFile(cogYAMLPath, output, 0o644); err != nil {
		return fmt.Errorf("writing cog.yaml: %w", err)
	}

	return nil
}

// deepMerge recursively merges override into base
func deepMerge(base, override map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range base {
		result[k] = v
	}

	for k, v := range override {
		if baseVal, ok := result[k]; ok {
			// Both are maps - merge recursively
			baseMap, baseIsMap := baseVal.(map[string]any)
			overrideMap, overrideIsMap := v.(map[string]any)
			if baseIsMap && overrideIsMap {
				result[k] = deepMerge(baseMap, overrideMap)
				continue
			}
		}
		result[k] = v
	}

	return result
}
