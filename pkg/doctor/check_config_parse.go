package doctor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/replicate/cog/pkg/config"
)

// ConfigParseCheck verifies that cog.yaml exists and can be parsed as valid YAML.
type ConfigParseCheck struct{}

func (c *ConfigParseCheck) Name() string        { return "config-parse" }
func (c *ConfigParseCheck) Group() Group        { return GroupConfig }
func (c *ConfigParseCheck) Description() string { return "Config parsing" }

func (c *ConfigParseCheck) Check(ctx *CheckContext) ([]Finding, error) {
	configPath := filepath.Join(ctx.ProjectDir, "cog.yaml")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return []Finding{{
			Severity:    SeverityError,
			Message:     "cog.yaml not found",
			Remediation: `Run "cog init" to create a cog.yaml`,
			File:        "cog.yaml",
		}}, nil
	}

	f, err := os.Open(configPath)
	if err != nil {
		return []Finding{{
			Severity: SeverityError,
			Message:  fmt.Sprintf("cannot read cog.yaml: %v", err),
			File:     "cog.yaml",
		}}, nil
	}
	defer f.Close()

	_, loadErr := config.Load(f, ctx.ProjectDir)
	if loadErr != nil {
		var parseErr *config.ParseError
		if isParseError(loadErr, &parseErr) {
			return []Finding{{
				Severity:    SeverityError,
				Message:     fmt.Sprintf("cog.yaml has invalid YAML: %v", loadErr),
				Remediation: "Fix the YAML syntax in cog.yaml",
				File:        "cog.yaml",
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
