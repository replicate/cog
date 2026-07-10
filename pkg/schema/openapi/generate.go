package openapi

import (
	"fmt"
	"os"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/schema"
	"github.com/replicate/cog/pkg/schema/python"
	cogversion "github.com/replicate/cog/pkg/util/version"
	"github.com/replicate/cog/pkg/wheels"
)

const minimumStaticSchemaSDKVersion = "0.17.0"

// GenerateSchema generates OpenAPI schema JSON from cog.yaml config.
func GenerateSchema(cfg *config.Config, dir string) ([]byte, error) {
	if err := ValidateSDKVersion(cfg); err != nil {
		return nil, err
	}
	if cfg.Predict == "" && cfg.Train == "" {
		return nil, fmt.Errorf("no predict or train reference found in cog.yaml")
	}
	return schema.GenerateCombined(dir, cfg.Predict, cfg.Train, schema.PathAwareParser(python.ParsePredictorWithSourcePath))
}

// ValidateSDKVersion rejects SDK versions too old for static schema generation.
func ValidateSDKVersion(cfg *config.Config) error {
	sdkVersion := explicitSDKVersion(cfg)
	if sdkVersion == "" {
		return nil
	}

	base := sdkVersion
	if m := wheels.BaseVersionRe.FindString(base); m != "" {
		base = m
	}
	ver, err := cogversion.NewVersion(base)
	if err != nil {
		return nil
	}
	minVer := cogversion.MustVersion(minimumStaticSchemaSDKVersion)
	if ver.GreaterOrEqual(minVer) {
		return nil
	}
	return fmt.Errorf("SDK version %s is not supported by static schema generation; use %s or newer", sdkVersion, minimumStaticSchemaSDKVersion)
}

func explicitSDKVersion(cfg *config.Config) string {
	if envVal := os.Getenv(wheels.CogSDKWheelEnvVar); envVal != "" {
		wc := wheels.ParseWheelValue(envVal)
		if wc != nil && wc.Source == wheels.WheelSourcePyPI && wc.Version != "" {
			return wc.Version
		}
		return ""
	}
	if cfg.Build != nil && cfg.Build.SDKVersion != "" && cfg.Build.SDKVersion != wheels.PreReleaseSentinel {
		return cfg.Build.SDKVersion
	}
	return ""
}
