package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var (
	topLevelPredictKeyPattern = regexp.MustCompile(`(?m)^predict\s*:`)
	topLevelRunRefPattern     = regexp.MustCompile(`(?m)^run:\s*["'][^"']+["']\s*$`)
	predictRefPattern         = regexp.MustCompile(`(?m)^predict:\s*["']predict\.py:Predictor["']\s*$`)
	basePredictorPattern      = regexp.MustCompile(`\bBasePredictor\b`)
	predictorClassPattern     = regexp.MustCompile(`\bclass\s+Predictor(\s*[(:])`)
	predictMethodPattern      = regexp.MustCompile(`\bdef\s+predict\s*\(`)
)

// PredictToRunMigrationCheck detects deprecated predict interface names and
// migrates the common starter-project shape to run interface names.
type PredictToRunMigrationCheck struct{}

func (c *PredictToRunMigrationCheck) Name() string { return "predict-to-run-migration" }
func (c *PredictToRunMigrationCheck) Group() Group { return GroupConfig }
func (c *PredictToRunMigrationCheck) Description() string {
	return "Deprecated predict interface names"
}

func (c *PredictToRunMigrationCheck) Check(ctx *CheckContext) ([]Finding, error) {
	var findings []Finding
	if topLevelPredictKeyPattern.Match(ctx.ConfigFile) || predictRefPattern.Match(ctx.ConfigFile) {
		findings = append(findings, Finding{
			Severity:    SeverityWarning,
			Message:     "predict in cog.yaml is deprecated; use run with run.py:Runner",
			Remediation: "Run cog doctor --fix to migrate predict: to run:",
			File:        ctx.ConfigFilename,
		})
	}

	if predictRefPattern.Match(ctx.ConfigFile) {
		if source, ok := predictMigrationSource(ctx); ok && hasLegacyPredictPythonNames(source) {
			findings = append(findings, Finding{
				Severity:    SeverityWarning,
				Message:     "predict.py uses deprecated Predictor/BasePredictor/predict() names",
				Remediation: "Run cog doctor --fix to migrate to Runner/BaseRunner/run()",
				File:        "predict.py",
			})
		}
	}

	return findings, nil
}

func (c *PredictToRunMigrationCheck) Fix(ctx *CheckContext, findings []Finding) error {
	if len(findings) == 0 {
		return nil
	}

	if err := preflightPredictToRunCollisions(ctx); err != nil {
		return err
	}
	source, ok := predictMigrationSource(ctx)
	if !ok {
		return fmt.Errorf("cannot migrate predict.py to run.py because predict.py was not found")
	}
	if !hasLegacyPredictPythonNames(source) {
		return fmt.Errorf("cannot migrate predict.py because no legacy Predictor/BasePredictor/predict() names were found")
	}
	if countRegexMatches(predictorClassPattern, source) != 1 || countRegexMatches(predictMethodPattern, source) != 1 {
		return fmt.Errorf("Manual migration required: automatic migration only supports a single Predictor class with a single predict method")
	}

	source = basePredictorPattern.ReplaceAll(source, []byte("BaseRunner"))
	source = predictorClassPattern.ReplaceAll(source, []byte("class Runner${1}"))
	source = predictMethodPattern.ReplaceAll(source, []byte("def run("))

	configPath := filepath.Join(ctx.ProjectDir, ctx.ConfigFilename)
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	configBytes = predictRefPattern.ReplaceAll(configBytes, []byte(`run: "run.py:Runner"`))
	if err := os.WriteFile(configPath, configBytes, 0o644); err != nil {
		return err
	}

	oldPath := filepath.Join(ctx.ProjectDir, "predict.py")
	newPath := filepath.Join(ctx.ProjectDir, "run.py")
	if err := os.WriteFile(newPath, source, 0o644); err != nil {
		return err
	}
	if err := os.Remove(oldPath); err != nil {
		return err
	}

	ctx.ConfigFile = configBytes
	if ctx.Config != nil {
		ctx.Config.Predict = "run.py:Runner"
	}
	if ctx.LoadResult != nil && ctx.LoadResult.Config != nil {
		ctx.LoadResult.Config.Predict = "run.py:Runner"
		warnings := ctx.LoadResult.Warnings[:0]
		for _, warning := range ctx.LoadResult.Warnings {
			if warning.Field != "predict" {
				warnings = append(warnings, warning)
			}
		}
		ctx.LoadResult.Warnings = warnings
	}
	delete(ctx.PythonFiles, "predict.py")
	parsePythonRef(ctx, "run.py:Runner")

	return nil
}

func hasLegacyPredictPythonNames(source []byte) bool {
	return basePredictorPattern.Match(source) ||
		predictorClassPattern.Match(source) ||
		predictMethodPattern.Match(source)
}

func countRegexMatches(pattern *regexp.Regexp, source []byte) int {
	return len(pattern.FindAll(source, -1))
}

func preflightPredictToRunCollisions(ctx *CheckContext) error {
	if topLevelRunRefPattern.Match(ctx.ConfigFile) {
		return fmt.Errorf("automatic migration cannot run when run is already set")
	}
	if !predictRefPattern.Match(ctx.ConfigFile) {
		return fmt.Errorf("Manual migration required: automatic migration only supports predict.py:Predictor")
	}
	candidate := filepath.Join(ctx.ProjectDir, "run.py")
	if _, err := os.Stat(candidate); err == nil {
		return fmt.Errorf("cannot migrate predict.py to run.py because run.py already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func predictMigrationSource(ctx *CheckContext) ([]byte, bool) {
	if pf, ok := ctx.PythonFiles["predict.py"]; ok && pf != nil {
		return pf.Source, true
	}
	source, err := os.ReadFile(filepath.Join(ctx.ProjectDir, "predict.py"))
	if err != nil {
		return nil, false
	}
	return source, true
}
