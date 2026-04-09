package doctor

import (
	"errors"
	"fmt"

	"github.com/replicate/cog/pkg/config"
)

// ConfigParseCheck verifies that cog.yaml exists and can be parsed as valid YAML.
type ConfigParseCheck struct{}

func (c *ConfigParseCheck) Name() string        { return "config-parse" }
func (c *ConfigParseCheck) Group() Group        { return GroupConfig }
func (c *ConfigParseCheck) Description() string { return "Config parsing" }

func (c *ConfigParseCheck) Check(ctx *CheckContext) ([]Finding, error) {
	// Config file not found on disk
	if ctx.ConfigFile == nil {
		return []Finding{{
			Severity:    SeverityError,
			Message:     fmt.Sprintf("%s not found", ctx.ConfigFilename),
			Remediation: `Run "cog init" to create a cog.yaml`,
			File:        ctx.ConfigFilename,
		}}, nil
	}

	// Check for parse errors from the single Load call in buildCheckContext
	if ctx.LoadErr != nil {
		var parseErr *config.ParseError
		if isParseError(ctx.LoadErr, &parseErr) {
			return []Finding{{
				Severity:    SeverityError,
				Message:     fmt.Sprintf("%s has invalid YAML: %v", ctx.ConfigFilename, ctx.LoadErr),
				Remediation: fmt.Sprintf("Fix the YAML syntax in %s", ctx.ConfigFilename),
				File:        ctx.ConfigFilename,
			}}, nil
		}
		// Other errors (validation, schema) are handled by other checks
	}

	return nil, nil
}

func (c *ConfigParseCheck) Fix(_ *CheckContext, _ []Finding) error {
	return ErrNoAutoFix
}

// isParseError checks if the error chain contains a ParseError.
func isParseError(err error, target **config.ParseError) bool {
	return errors.As(err, target)
}
