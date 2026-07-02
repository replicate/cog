package doctor

import (
	"fmt"
)

// ConfigDeprecatedFieldsCheck detects deprecated fields in cog.yaml.
type ConfigDeprecatedFieldsCheck struct{}

func (c *ConfigDeprecatedFieldsCheck) Name() string        { return "config-deprecated-fields" }
func (c *ConfigDeprecatedFieldsCheck) Group() Group        { return GroupConfig }
func (c *ConfigDeprecatedFieldsCheck) Description() string { return "Deprecated fields" }

func (c *ConfigDeprecatedFieldsCheck) Check(ctx *CheckContext) ([]Finding, error) {
	// No config loaded successfully — other checks handle parse/validation errors.
	// Note: warnings are only available when Load succeeds because they come from
	// ValidateConfigFile which uses an unexported type. If the config has validation
	// errors, deprecation warnings cannot be surfaced through Load.
	if ctx.LoadResult == nil {
		return nil, nil
	}

	var findings []Finding
	for _, w := range ctx.LoadResult.Warnings {
		findings = append(findings, Finding{
			Severity:    SeverityWarning,
			Message:     fmt.Sprintf("%q is deprecated: %s", w.Field, w.Message),
			Remediation: fmt.Sprintf("Use %q instead", w.Replacement),
			File:        ctx.ConfigFilename,
		})
	}

	return findings, nil
}

func (c *ConfigDeprecatedFieldsCheck) Fix(_ *CheckContext, _ []Finding) error {
	return ErrNoAutoFix
}
