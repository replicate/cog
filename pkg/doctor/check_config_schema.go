package doctor

import (
	"fmt"
	"os"
	"path/filepath"

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
	configPath := filepath.Join(ctx.ProjectDir, "cog.yaml")

	f, err := os.Open(configPath)
	if err != nil {
		return nil, nil // ConfigParseCheck handles missing files
	}
	defer f.Close()

	_, loadErr := config.Load(f, ctx.ProjectDir)
	if loadErr == nil {
		return nil, nil // Valid config
	}

	// If this is a parse error, skip — ConfigParseCheck handles it
	var parseErr *config.ParseError
	if isParseError(loadErr, &parseErr) {
		return nil, nil
	}

	// Any other error is a schema/validation error
	return []Finding{{
		Severity:    SeverityError,
		Message:     fmt.Sprintf("cog.yaml validation failed: %v", loadErr),
		Remediation: "Fix the configuration errors in cog.yaml",
		File:        "cog.yaml",
	}}, nil
}

func (c *ConfigSchemaCheck) Fix(_ *CheckContext, _ []Finding) error {
	return ErrNoAutoFix
}
