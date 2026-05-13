package doctor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func TestConfigParseCheck_Valid(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)

	ctx := buildTestCheckContext(t, dir)
	check := &ConfigParseCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestConfigParseCheck_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build: [invalid yaml`)

	ctx := buildTestCheckContext(t, dir)
	check := &ConfigParseCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, SeverityError, findings[0].Severity)
	require.Contains(t, findings[0].Message, "cog.yaml")
}

func TestConfigParseCheck_MissingFile(t *testing.T) {
	dir := t.TempDir()

	ctx := buildTestCheckContext(t, dir)
	check := &ConfigParseCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Contains(t, findings[0].Message, "cog.yaml not found")
}

func TestConfigDeprecatedFieldsCheck_Clean(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
  python_requirements: "requirements.txt"
run: "run.py:Runner"
`)
	writeFile(t, dir, "requirements.txt", "torch==2.0.0\n")

	ctx := buildTestCheckContext(t, dir)
	check := &ConfigDeprecatedFieldsCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestConfigDeprecatedFieldsCheck_PythonPackages(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
  python_packages:
    - torch==2.0.0
run: "run.py:Runner"
`)

	ctx := buildTestCheckContext(t, dir)
	check := &ConfigDeprecatedFieldsCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, SeverityWarning, findings[0].Severity)
	require.Contains(t, findings[0].Message, "python_packages")
}

func TestConfigDeprecatedFieldsCheck_PreInstall(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
  pre_install:
    - pip install something
run: "run.py:Runner"
`)

	ctx := buildTestCheckContext(t, dir)
	check := &ConfigDeprecatedFieldsCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, SeverityWarning, findings[0].Severity)
	require.Contains(t, findings[0].Message, "pre_install")
}

func TestConfigPredictRefCheck_Valid(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "predict.py", `from cog import BasePredictor
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := buildTestCheckContext(t, dir)
	check := &ConfigPredictRefCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestConfigPredictRefCheck_MissingFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)

	ctx := buildTestCheckContext(t, dir)
	check := &ConfigPredictRefCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, SeverityError, findings[0].Severity)
	require.Contains(t, findings[0].Message, "predict.py")
	require.Contains(t, findings[0].Message, "not found")
}

func TestConfigPredictRefCheck_MissingClass(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:DoesNotExist"
`)
	writeFile(t, dir, "predict.py", `from cog import BasePredictor
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := buildTestCheckContext(t, dir)
	check := &ConfigPredictRefCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, SeverityError, findings[0].Severity)
	require.Contains(t, findings[0].Message, "DoesNotExist")
}

func TestConfigPredictRefCheck_NoPredictField(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
`)

	ctx := buildTestCheckContext(t, dir)
	check := &ConfigPredictRefCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings) // No predict field is valid (some projects are train-only)
}

// TestConfigPredictRefCheck_HonorsContextCancellation verifies that
// ConfigPredictRefCheck threads CheckContext.ctx into parser.ParseCtx
// so that a canceled context interrupts parsing.
func TestConfigPredictRefCheck_HonorsContextCancellation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	// Write a predict.py so we reach the parsing path (not the PythonFiles cache
	// path, which short-circuits before ParseCtx).
	writeFile(t, dir, "predict.py", `class Predictor:
    def predict(self) -> str:
        return ""
`)

	// Build context, then replace ctx.ctx with a pre-canceled one and wipe
	// the cached ParsedFile so the check goes through the ParseCtx path.
	ctx := buildTestCheckContext(t, dir)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	ctx.ctx = canceled
	ctx.PythonFiles = make(map[string]*ParsedFile)

	check := &ConfigPredictRefCheck{}
	// The check should not panic or hang. It may return findings (e.g., a
	// parse error) or an empty result; both are acceptable.
	_, err := check.Check(ctx)
	require.NoError(t, err)
}

func TestPredictToRunMigrationCheck_CleanRunProject(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
run: "run.py:Runner"
`)
	writeFile(t, dir, "run.py", `from cog import BaseRunner
class Runner(BaseRunner):
    def run(self, text: str) -> str:
        return text
`)

	ctx := buildTestCheckContext(t, dir)
	check := &PredictToRunMigrationCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestPredictToRunMigrationCheck_DetectsLegacyPredictUsage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "predict.py", `from cog import BasePredictor
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := buildTestCheckContext(t, dir)
	check := &PredictToRunMigrationCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	require.Equal(t, SeverityWarning, findings[0].Severity)
	require.Contains(t, findings[0].Message, "deprecated")
	require.Contains(t, findings[0].Remediation, "cog doctor --fix")
}

func TestPredictToRunMigrationCheck_FixMigratesConfigAndPython(t *testing.T) {
	tests := []struct {
		name      string
		classLine string
		wantLine  string
	}{
		{name: "class with base", classLine: "class Predictor(BasePredictor):", wantLine: "class Runner(BaseRunner):"},
		{name: "class without base", classLine: "class Predictor:", wantLine: "class Runner:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
			writeFile(t, dir, "predict.py", `from cog import BasePredictor

`+tt.classLine+`
    def predict(self, text: str) -> str:
        return text
`)

			ctx := buildTestCheckContext(t, dir)
			check := &PredictToRunMigrationCheck{}
			findings, err := check.Check(ctx)
			require.NoError(t, err)
			require.NotEmpty(t, findings)
			require.NoError(t, check.Fix(ctx, findings))

			cogYAML, err := os.ReadFile(filepath.Join(dir, "cog.yaml"))
			require.NoError(t, err)
			require.Contains(t, string(cogYAML), `run: "run.py:Runner"`)
			require.NotContains(t, string(cogYAML), `predict: "predict.py:Predictor"`)

			_, err = os.Stat(filepath.Join(dir, "predict.py"))
			require.ErrorIs(t, err, os.ErrNotExist)

			runPy, err := os.ReadFile(filepath.Join(dir, "run.py"))
			require.NoError(t, err)
			require.Contains(t, string(runPy), "from cog import BaseRunner")
			require.Contains(t, string(runPy), tt.wantLine)
			require.Contains(t, string(runPy), "def run(")
			require.NotContains(t, string(runPy), "BasePredictor")
			require.NotContains(t, string(runPy), "class Predictor")
			require.NotContains(t, string(runPy), "def predict(")
		})
	}
}

func TestPredictToRunMigrationCheck_FixRefusesFileCollision(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "predict.py", `from cog import BasePredictor
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)
	writeFile(t, dir, "run.py", `class Runner:
    def run(self) -> str:
        return "existing"
`)

	ctx := buildTestCheckContext(t, dir)
	check := &PredictToRunMigrationCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Error(t, check.Fix(ctx, findings))

	cogYAML, err := os.ReadFile(filepath.Join(dir, "cog.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(cogYAML), `predict: "predict.py:Predictor"`)
	require.NotContains(t, string(cogYAML), `run: "run.py:Runner"`)
}

func TestPredictToRunMigrationCheck_FixRefusesConfigOnlyFileCollision(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "run.py", `class Runner:
    def run(self) -> str:
        return "existing"
`)

	ctx := buildTestCheckContext(t, dir)
	check := &PredictToRunMigrationCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Error(t, check.Fix(ctx, findings))

	cogYAML, err := os.ReadFile(filepath.Join(dir, "cog.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(cogYAML), `predict: "predict.py:Predictor"`)
	require.NotContains(t, string(cogYAML), `run: "run.py:Runner"`)
}

func TestPredictToRunMigrationCheck_FixRefusesMissingPredictFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)

	ctx := buildTestCheckContext(t, dir)
	check := &PredictToRunMigrationCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Error(t, check.Fix(ctx, findings))

	cogYAML, err := os.ReadFile(filepath.Join(dir, "cog.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(cogYAML), `predict: "predict.py:Predictor"`)
	require.NotContains(t, string(cogYAML), `run: "run.py:Runner"`)
}

func TestPredictToRunMigrationCheck_DoesNotRewriteTrainFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
run: "run.py:Runner"
train: "train.py:Trainer"
`)
	writeFile(t, dir, "run.py", `from cog import BaseRunner
class Runner(BaseRunner):
    def run(self, text: str) -> str:
        return text
`)
	writeFile(t, dir, "train.py", `class Trainer:
    def predict(self) -> str:
        return "helper"
`)

	ctx := buildTestCheckContext(t, dir)
	parsePythonRef(ctx, "train.py:Trainer")
	check := &PredictToRunMigrationCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestPredictToRunMigrationCheck_IgnoresStrayPredictFileWithoutLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
run: "run.py:Runner"
`)
	writeFile(t, dir, "predict.py", `from cog import BasePredictor
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := buildTestCheckContext(t, dir)
	check := &PredictToRunMigrationCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestPredictToRunMigrationCheck_FixRefusesCustomPredictRef(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "foo.py:Predictor"
`)
	writeFile(t, dir, "foo.py", `from cog import BasePredictor
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := buildTestCheckContext(t, dir)
	check := &PredictToRunMigrationCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	err = check.Fix(ctx, findings)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Manual migration required")

	cogYAML, err := os.ReadFile(filepath.Join(dir, "cog.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(cogYAML), `predict: "foo.py:Predictor"`)
}

func TestPredictToRunMigrationCheck_FixMigratesTargetPredictMethodOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "predict.py", `from cog import BasePredictor
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
class Helper(BasePredictor):
    def predict(self) -> str:
        return "helper"
`)

	ctx := buildTestCheckContext(t, dir)
	check := &PredictToRunMigrationCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	require.NoError(t, check.Fix(ctx, findings))

	cogYAML, err := os.ReadFile(filepath.Join(dir, "cog.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(cogYAML), `run: "run.py:Runner"`)
	require.NotContains(t, string(cogYAML), `predict: "predict.py:Predictor"`)

	runPy, err := os.ReadFile(filepath.Join(dir, "run.py"))
	require.NoError(t, err)
	require.Contains(t, string(runPy), "from cog import BasePredictor, BaseRunner")
	require.Contains(t, string(runPy), "class Runner(BaseRunner):")
	require.Contains(t, string(runPy), "def run(self, text: str) -> str:")
	require.Contains(t, string(runPy), "class Helper(BasePredictor):")
	require.Contains(t, string(runPy), "def predict(self) -> str:")
}

func TestPredictToRunMigrationCheck_CheckIgnoresHelperOnlyPredictMethods(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "predict.py", `from cog import BaseRunner
class Runner(BaseRunner):
    def run(self, text: str) -> str:
        return text
class Helper:
    def predict(self) -> str:
        return "helper"
`)

	ctx := buildTestCheckContext(t, dir)
	check := &PredictToRunMigrationCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	for _, finding := range findings {
		require.NotEqual(t, "predict.py", finding.File)
	}
}

func TestPredictToRunMigrationCheck_FixMigratesCogPredictorImport(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "predict.py", `from cog.predictor import BasePredictor
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := buildTestCheckContext(t, dir)
	check := &PredictToRunMigrationCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	require.NoError(t, check.Fix(ctx, findings))

	runPy, err := os.ReadFile(filepath.Join(dir, "run.py"))
	require.NoError(t, err)
	require.Contains(t, string(runPy), "from cog.predictor import BaseRunner")
	require.Contains(t, string(runPy), "class Runner(BaseRunner):")
}

func TestPredictToRunMigrationCheck_FixRefusesAliasedBasePredictor(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "predict.py", `from cog import BasePredictor as CogBasePredictor
class Predictor(CogBasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := buildTestCheckContext(t, dir)
	check := &PredictToRunMigrationCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	err = check.Fix(ctx, findings)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Manual migration required")

	cogYAML, err := os.ReadFile(filepath.Join(dir, "cog.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(cogYAML), `predict: "predict.py:Predictor"`)
}

func TestPredictToRunMigrationCheck_FixMigratesNestedBasePredictorImport(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "predict.py", `try:
    from cog import BasePredictor
except ImportError:
    from cog.predictor import BasePredictor
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := buildTestCheckContext(t, dir)
	check := &PredictToRunMigrationCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	require.NoError(t, check.Fix(ctx, findings))

	runPy, err := os.ReadFile(filepath.Join(dir, "run.py"))
	require.NoError(t, err)
	require.Contains(t, string(runPy), "from cog import BaseRunner")
	require.Contains(t, string(runPy), "from cog.predictor import BaseRunner")
	require.Contains(t, string(runPy), "class Runner(BaseRunner):")
}

func TestPredictToRunMigrationCheck_FixMigratesImportListBasePredictor(t *testing.T) {
	tests := []struct {
		name       string
		importCode string
		wantImport string
	}{
		{
			name:       "multi-name",
			importCode: `from cog import BasePredictor, Input`,
			wantImport: `from cog import BaseRunner, Input`,
		},
		{
			name: "parenthesized",
			importCode: `from cog import (
    BasePredictor,
    Input,
)`,
			wantImport: "BaseRunner,",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
			writeFile(t, dir, "predict.py", tt.importCode+`
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

			ctx := buildTestCheckContext(t, dir)
			check := &PredictToRunMigrationCheck{}
			findings, err := check.Check(ctx)
			require.NoError(t, err)
			require.NotEmpty(t, findings)
			require.NoError(t, check.Fix(ctx, findings))

			runPy, err := os.ReadFile(filepath.Join(dir, "run.py"))
			require.NoError(t, err)
			require.Contains(t, string(runPy), tt.wantImport)
			require.Contains(t, string(runPy), "class Runner(BaseRunner):")
		})
	}
}

func TestPredictToRunMigrationCheck_FixRefusesNestedAliasedBasePredictor(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "predict.py", `try:
    from cog import BasePredictor as CogBasePredictor
except ImportError:
    from cog.predictor import BasePredictor as CogBasePredictor
class Predictor(CogBasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := buildTestCheckContext(t, dir)
	check := &PredictToRunMigrationCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	err = check.Fix(ctx, findings)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Manual migration required")
}

func TestPredictToRunMigrationCheck_FixMigratesBasePredictorWhenBaseRunnerImportedElsewhere(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "predict.py", `from cog import BasePredictor
if TYPE_CHECKING:
    from cog import BaseRunner
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := buildTestCheckContext(t, dir)
	check := &PredictToRunMigrationCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	require.NoError(t, check.Fix(ctx, findings))

	runPy, err := os.ReadFile(filepath.Join(dir, "run.py"))
	require.NoError(t, err)
	require.Contains(t, string(runPy), "from cog import BaseRunner")
	require.NotContains(t, string(runPy), "from cog import BasePredictor")
	require.Contains(t, string(runPy), "class Runner(BaseRunner):")
}

func TestPredictToRunMigrationCheck_FixRefusesNonCogBasePredictor(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "predict.py", `from other_pkg import BasePredictor
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := buildTestCheckContext(t, dir)
	check := &PredictToRunMigrationCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	err = check.Fix(ctx, findings)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Manual migration required")
}

func TestPredictToRunMigrationCheck_FixRefusesShadowedBasePredictorImport(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "predict.py", `from cog import BasePredictor
from other_pkg import BasePredictor
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := buildTestCheckContext(t, dir)
	check := &PredictToRunMigrationCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	err = check.Fix(ctx, findings)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Manual migration required")
}

func TestPredictToRunMigrationCheck_FixAddsBaseRunnerToAllFallbackImports(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "predict.py", `try:
    from cog import BasePredictor
except ImportError:
    from cog.predictor import BasePredictor
class Helper(BasePredictor):
    def predict(self) -> str:
        return "helper"
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := buildTestCheckContext(t, dir)
	check := &PredictToRunMigrationCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	require.NoError(t, check.Fix(ctx, findings))

	runPy, err := os.ReadFile(filepath.Join(dir, "run.py"))
	require.NoError(t, err)
	require.Contains(t, string(runPy), "from cog import BasePredictor, BaseRunner")
	require.Contains(t, string(runPy), "from cog.predictor import BasePredictor, BaseRunner")
	require.Contains(t, string(runPy), "class Helper(BasePredictor):")
	require.Contains(t, string(runPy), "class Runner(BaseRunner):")
}

func TestPredictToRunMigrationCheck_FixRefusesExistingRunField(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
run: "run.py:Runner"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "predict.py", `from cog import BasePredictor
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := buildTestCheckContext(t, dir)
	check := &PredictToRunMigrationCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	require.Error(t, check.Fix(ctx, findings))
}

func TestPredictToRunMigrationCheck_RunFixesContextForFollowingChecks(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "predict.py", `from cog import BasePredictor
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	result, err := Run(context.Background(), RunOptions{Fix: true, ProjectDir: dir}, []Check{
		&PredictToRunMigrationCheck{},
		&ConfigPredictRefCheck{},
	})
	require.NoError(t, err)
	require.Len(t, result.Results, 2)
	require.True(t, result.Results[0].Fixed)
	require.NoError(t, result.Results[1].Err)
	require.Empty(t, result.Results[1].Findings)
}

func TestPredictToRunMigrationCheck_RunFixSuppressesStalePredictDeprecation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "predict.py", `from cog import BasePredictor
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	result, err := Run(context.Background(), RunOptions{Fix: true, ProjectDir: dir}, []Check{
		&PredictToRunMigrationCheck{},
		&ConfigDeprecatedFieldsCheck{},
	})
	require.NoError(t, err)
	require.Len(t, result.Results, 2)
	require.True(t, result.Results[0].Fixed)
	require.Empty(t, result.Results[1].Findings)
}

// buildTestCheckContext creates a CheckContext by loading the cog.yaml in the given dir.
func buildTestCheckContext(t *testing.T, dir string) *CheckContext {
	t.Helper()
	ctx := &CheckContext{
		ctx:            context.Background(),
		ProjectDir:     dir,
		ConfigFilename: "cog.yaml",
		PythonFiles:    make(map[string]*ParsedFile),
	}

	configPath := filepath.Join(dir, "cog.yaml")
	configBytes, err := os.ReadFile(configPath)
	if err == nil {
		ctx.ConfigFile = configBytes
		loadResult, loadErr := config.Load(bytes.NewReader(configBytes), dir)
		ctx.LoadErr = loadErr
		if loadResult != nil {
			ctx.LoadResult = loadResult
			ctx.Config = loadResult.Config
		}
	}

	return ctx
}

func TestConfigSchemaCheck_Valid(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "3.12"
predict: "predict.py:Predictor"
`)
	writeFile(t, dir, "predict.py", `from cog import BasePredictor
class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := buildTestCheckContext(t, dir)
	check := &ConfigSchemaCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestConfigSchemaCheck_InvalidSchema(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build:
  python_version: "2.7"
predict: "predict.py:Predictor"
`)

	ctx := buildTestCheckContext(t, dir)
	check := &ConfigSchemaCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, SeverityError, findings[0].Severity)
	require.Contains(t, findings[0].Message, "validation failed")
}

func TestConfigSchemaCheck_ParseError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cog.yaml", `build: [invalid yaml`)

	ctx := buildTestCheckContext(t, dir)
	check := &ConfigSchemaCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings) // Parse errors are handled by ConfigParseCheck
}

func TestConfigSchemaCheck_MissingFile(t *testing.T) {
	dir := t.TempDir()

	ctx := buildTestCheckContext(t, dir)
	check := &ConfigSchemaCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings) // Missing file handled by ConfigParseCheck
}

// writeFile is a test helper to create fixture files.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
}
