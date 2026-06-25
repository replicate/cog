package doctor

import (
	"errors"
	"fmt"

	"github.com/replicate/cog/pkg/config"
)

// ConfigSchemaCheck validates cog.yaml against the configuration schema.
// Parse errors are handled by ConfigParseCheck; this check catches schema
// and validation errors (wrong types, invalid values, etc.).
type ConfigSchemaCheck struct{}

func (c *ConfigSchemaCheck) Name() string        { return "config-schema" }
func (c *ConfigSchemaCheck) Group() Group        { return GroupConfig }
func (c *ConfigSchemaCheck) Description() string { return "Config schema" }

func (c *ConfigSchemaCheck) Check(ctx *CheckContext) ([]Finding, error) {
	// No config file on disk — ConfigParseCheck handles this
	if ctx.ConfigFile == nil {
		return nil, nil
	}

	// No load error means valid config
	if ctx.LoadErr == nil {
		return nil, nil
	}

	// If this is a parse error, skip — ConfigParseCheck handles it
	var parseErr *config.ParseError
	if errors.As(ctx.LoadErr, &parseErr) {
		return nil, nil
	}

	// Any other error is a schema/validation error
	return []Finding{{
		Severity:    SeverityError,
		Message:     fmt.Sprintf("%s validation failed: %v", ctx.ConfigFilename, ctx.LoadErr),
		Remediation: fmt.Sprintf("Fix the configuration errors in %s", ctx.ConfigFilename),
		File:        ctx.ConfigFilename,
	}}, nil
}

func (c *ConfigSchemaCheck) Fix(_ *CheckContext, _ []Finding) error {
	return ErrNoAutoFix
}
