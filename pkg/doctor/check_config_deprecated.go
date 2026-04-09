package doctor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/replicate/cog/pkg/config"
)

// ConfigDeprecatedFieldsCheck detects deprecated fields in cog.yaml.
type ConfigDeprecatedFieldsCheck struct{}

func (c *ConfigDeprecatedFieldsCheck) Name() string        { return "config-deprecated-fields" }
func (c *ConfigDeprecatedFieldsCheck) Group() Group        { return GroupConfig }
func (c *ConfigDeprecatedFieldsCheck) Description() string { return "Deprecated fields" }

func (c *ConfigDeprecatedFieldsCheck) Check(ctx *CheckContext) ([]Finding, error) {
	configPath := filepath.Join(ctx.ProjectDir, "cog.yaml")
	f, err := os.Open(configPath)
	if err != nil {
		return nil, nil // Config parse check handles missing file
	}
	defer f.Close()

	// We need to run validation to get deprecation warnings.
	// Load does parse + validate + complete; we want just parse + validate.
	loadResult, err := config.Load(f, ctx.ProjectDir)
	if err != nil {
		return nil, nil // Other config checks handle parse/validation errors
	}

	var findings []Finding
	for _, w := range loadResult.Warnings {
		findings = append(findings, Finding{
			Severity:    SeverityWarning,
			Message:     fmt.Sprintf("%q is deprecated: %s", w.Field, w.Message),
			Remediation: fmt.Sprintf("Use %q instead", w.Replacement),
			File:        "cog.yaml",
		})
	}

	return findings, nil
}

func (c *ConfigDeprecatedFieldsCheck) Fix(_ *CheckContext, _ []Finding) error {
	return ErrNoAutoFix
}
