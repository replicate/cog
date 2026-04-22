package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	schemaPython "github.com/replicate/cog/pkg/schema/python"
)

func TestPydanticBaseModelCheck_Clean(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `from cog import BasePredictor, BaseModel, Path

class Output(BaseModel):
    audio: Path

class Predictor(BasePredictor):
    def predict(self, text: str) -> Output:
        return Output(audio="a.wav")
`)

	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
	}

	check := &PydanticBaseModelCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestPydanticBaseModelCheck_Detects(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `from cog import BasePredictor, Path
from pydantic import BaseModel, ConfigDict

class VoiceCloningOutputs(BaseModel):
    model_config = ConfigDict(arbitrary_types_allowed=True)
    audio: Path
    spectrogram: Path

class Predictor(BasePredictor):
    def predict(self, text: str) -> VoiceCloningOutputs:
        return VoiceCloningOutputs(audio="a.wav", spectrogram="s.png")
`)

	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
	}

	check := &PydanticBaseModelCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, SeverityError, findings[0].Severity)
	require.Contains(t, findings[0].Message, "VoiceCloningOutputs")
	require.Contains(t, findings[0].Message, "pydantic.BaseModel")
}

func TestPydanticBaseModelCheck_DetectsAliased(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `from cog import BasePredictor, Path
from pydantic import BaseModel as PBM, ConfigDict

class VoiceCloningOutputs(PBM):
    model_config = ConfigDict(arbitrary_types_allowed=True)
    audio: Path

class Predictor(BasePredictor):
    def predict(self, text: str) -> VoiceCloningOutputs:
        return VoiceCloningOutputs(audio="a.wav")
`)

	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
	}

	check := &PydanticBaseModelCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1,
		"aliased `from pydantic import BaseModel as PBM` should still trigger detection")
	require.Contains(t, findings[0].Message, "VoiceCloningOutputs")
}

func TestPydanticBaseModelCheck_DetectsDottedAttribute(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `from cog import BasePredictor, Path
import pydantic

class VoiceCloningOutputs(pydantic.BaseModel):
    model_config = pydantic.ConfigDict(arbitrary_types_allowed=True)
    audio: Path

class Predictor(BasePredictor):
    def predict(self, text: str) -> VoiceCloningOutputs:
        return VoiceCloningOutputs(audio="a.wav")
`)

	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
	}

	check := &PydanticBaseModelCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Contains(t, findings[0].Message, "VoiceCloningOutputs")
}

func TestPydanticBaseModelCheck_Fix(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `from cog import BasePredictor, Path
from pydantic import BaseModel, ConfigDict

class VoiceCloningOutputs(BaseModel):
    model_config = ConfigDict(arbitrary_types_allowed=True)
    audio: Path
    spectrogram: Path

class Predictor(BasePredictor):
    def predict(self, text: str) -> VoiceCloningOutputs:
        return VoiceCloningOutputs(audio="a.wav", spectrogram="s.png")
`)

	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
	}

	check := &PydanticBaseModelCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)

	err = check.Fix(ctx, findings)
	require.NoError(t, err)

	// Re-read and verify
	content, err := os.ReadFile(filepath.Join(dir, "predict.py"))
	require.NoError(t, err)
	require.Contains(t, string(content), "from cog import BasePredictor, Path, BaseModel")
	require.NotContains(t, string(content), "from pydantic import BaseModel")
	require.NotContains(t, string(content), "arbitrary_types_allowed")
	require.NotContains(t, string(content), "model_config")

	// Re-parse and verify doctor passes
	ctx.PythonFiles = parsePythonFiles(t, dir, "predict.py")
	findings, err = check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestPydanticBaseModelCheck_NoFalsePositive(t *testing.T) {
	// arbitrary_types_allowed=False should NOT trigger the check
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `from cog import BasePredictor, Path
from pydantic import BaseModel, ConfigDict

class Output(BaseModel):
    model_config = ConfigDict(arbitrary_types_allowed=False, validate_default=True)
    audio: Path

class Predictor(BasePredictor):
    def predict(self, text: str) -> Output:
        return Output(audio="a.wav")
`)
	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
	}
	check := &PydanticBaseModelCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestPydanticBaseModelCheck_Fix_NoCogImport(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `from pydantic import BaseModel, ConfigDict

class Output(BaseModel):
    model_config = ConfigDict(arbitrary_types_allowed=True)
    name: str
`)
	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
	}
	check := &PydanticBaseModelCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)

	err = check.Fix(ctx, findings)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "predict.py"))
	require.NoError(t, err)
	require.Contains(t, string(content), "from cog import BaseModel")
	require.NotContains(t, string(content), "from pydantic import BaseModel")
}

func TestPydanticBaseModelCheck_Fix_ImportPydanticStyle(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `from cog import BasePredictor, Path
import pydantic

class Output(pydantic.BaseModel):
    model_config = pydantic.ConfigDict(arbitrary_types_allowed=True)
    audio: Path

class Predictor(BasePredictor):
    def predict(self, text: str) -> Output:
        return Output(audio="a.wav")
`)
	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
	}
	check := &PydanticBaseModelCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)

	err = check.Fix(ctx, findings)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "predict.py"))
	require.NoError(t, err)
	require.Contains(t, string(content), "from cog import BasePredictor, Path, BaseModel")
	require.NotContains(t, string(content), "import pydantic")
	require.NotContains(t, string(content), "pydantic.BaseModel")
	require.NotContains(t, string(content), "pydantic.ConfigDict")
}

func TestPydanticBaseModelCheck_Fix_PreservesOtherConfigDict(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `from cog import BasePredictor, Path
from pydantic import BaseModel, ConfigDict

class Output(BaseModel):
    model_config = ConfigDict(arbitrary_types_allowed=True, validate_default=True)
    audio: Path

class Predictor(BasePredictor):
    def predict(self, text: str) -> Output:
        return Output(audio="a.wav")
`)
	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
	}
	check := &PydanticBaseModelCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)

	err = check.Fix(ctx, findings)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "predict.py"))
	require.NoError(t, err)
	require.Contains(t, string(content), "validate_default=True")
	require.NotContains(t, string(content), "arbitrary_types_allowed")
	require.Contains(t, string(content), "model_config")
}

func TestPydanticBaseModelCheck_Fix_MultilineImport(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `from cog import BasePredictor, Path
from pydantic import (
    BaseModel,
    ConfigDict,
)

class Output(BaseModel):
    model_config = ConfigDict(arbitrary_types_allowed=True)
    audio: Path

class Predictor(BasePredictor):
    def predict(self, text: str) -> Output:
        return Output(audio="a.wav")
`)
	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
	}
	check := &PydanticBaseModelCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)

	err = check.Fix(ctx, findings)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "predict.py"))
	require.NoError(t, err)
	require.Contains(t, string(content), "from cog import BasePredictor, Path, BaseModel")
	require.NotContains(t, string(content), "from pydantic import")
	require.NotContains(t, string(content), "arbitrary_types_allowed")
}

// TestPydanticBaseModelCheck_Fix_PreservesUnrelatedPydanticClass verifies that
// the narrow-scope AST fixer only rewrites classes that actually trigger the
// check, leaving unrelated pydantic-derived classes intact.
func TestPydanticBaseModelCheck_Fix_PreservesUnrelatedPydanticClass(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `from cog import BasePredictor, Path
from pydantic import BaseModel, ConfigDict

# This class does NOT have arbitrary_types_allowed=True and should remain a plain
# pydantic.BaseModel-derived class -- the fixer must not rewrite it.
class UnrelatedConfig(BaseModel):
    name: str

class VoiceCloningOutputs(BaseModel):
    model_config = ConfigDict(arbitrary_types_allowed=True)
    audio: Path

class Predictor(BasePredictor):
    def predict(self, text: str) -> VoiceCloningOutputs:
        return VoiceCloningOutputs(audio="a.wav")
`)
	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
	}

	check := &PydanticBaseModelCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1,
		"only VoiceCloningOutputs should be flagged; UnrelatedConfig has no arbitrary_types_allowed=True")

	err = check.Fix(ctx, findings)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "predict.py"))
	require.NoError(t, err)
	src := string(content)

	// BaseModel must still be imported from pydantic so UnrelatedConfig still works.
	require.Contains(t, src, "from pydantic import BaseModel",
		"BaseModel must still be imported from pydantic because UnrelatedConfig needs it")
	// UnrelatedConfig must be untouched.
	require.Contains(t, src, "class UnrelatedConfig(BaseModel):")
	// VoiceCloningOutputs should no longer use pydantic.BaseModel — it should
	// inherit from cog.BaseModel (aliased to avoid collision with pydantic's).
	require.NotContains(t, src, "class VoiceCloningOutputs(BaseModel):")
	require.NotContains(t, src, "model_config = ConfigDict",
		"the arbitrary_types_allowed workaround should have been removed")
}

// TestPydanticBaseModelCheck_Fix_PreservesPydanticImportForField verifies that
// `import pydantic` is preserved when other `pydantic.<attr>` references remain.
func TestPydanticBaseModelCheck_Fix_PreservesPydanticImportForField(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `from cog import BasePredictor, Path
import pydantic

class Output(pydantic.BaseModel):
    model_config = pydantic.ConfigDict(arbitrary_types_allowed=True)
    name: str = pydantic.Field(default="x")

class Predictor(BasePredictor):
    def predict(self, text: str) -> Output:
        return Output(name=text)
`)
	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
	}
	check := &PydanticBaseModelCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)

	err = check.Fix(ctx, findings)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "predict.py"))
	require.NoError(t, err)
	src := string(content)

	// `import pydantic` must be preserved because pydantic.Field is still used.
	require.Contains(t, src, "import pydantic",
		"import pydantic must be preserved because pydantic.Field is still referenced")
	require.Contains(t, src, "pydantic.Field")
	// But the class should now inherit from bare BaseModel.
	require.Contains(t, src, "class Output(BaseModel):")
	require.NotContains(t, src, "arbitrary_types_allowed")
}

// TestPydanticBaseModelCheck_Fix_TrailingComma handles the edge case of
// ConfigDict(arbitrary_types_allowed=True,) with a trailing comma.
func TestPydanticBaseModelCheck_Fix_TrailingComma(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `from cog import BasePredictor, Path
from pydantic import BaseModel, ConfigDict

class Output(BaseModel):
    model_config = ConfigDict(arbitrary_types_allowed=True,)
    audio: Path

class Predictor(BasePredictor):
    def predict(self, text: str) -> Output:
        return Output(audio="a.wav")
`)
	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
	}
	check := &PydanticBaseModelCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.NoError(t, check.Fix(ctx, findings))

	content, err := os.ReadFile(filepath.Join(dir, "predict.py"))
	require.NoError(t, err)
	src := string(content)
	require.NotContains(t, src, "arbitrary_types_allowed")
	require.NotContains(t, src, "model_config", "model_config should be dropped entirely (only arg was the removed one)")
}

func TestDeprecatedImportsCheck_Clean(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
	}

	check := &DeprecatedImportsCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestDeprecatedImportsCheck_Detects(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `from cog import BasePredictor
from cog.types import ExperimentalFeatureWarning

class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
	}

	check := &DeprecatedImportsCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, SeverityError, findings[0].Severity)
	require.Contains(t, findings[0].Message, "ExperimentalFeatureWarning")
}

func TestDeprecatedImportsCheck_Fix(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `from cog import BasePredictor
from cog.types import ExperimentalFeatureWarning

class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
	}

	check := &DeprecatedImportsCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)

	err = check.Fix(ctx, findings)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "predict.py"))
	require.NoError(t, err)
	require.NotContains(t, string(content), "ExperimentalFeatureWarning")

	// Re-parse and verify clean
	ctx.PythonFiles = parsePythonFiles(t, dir, "predict.py")
	findings, err = check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestDeprecatedImportsCheck_Fix_WithWarningsFilterwarnings(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `import warnings
from cog import BasePredictor
from cog.types import ExperimentalFeatureWarning

warnings.filterwarnings("ignore", category=ExperimentalFeatureWarning)


class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return "hello " + text
`)

	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
	}

	check := &DeprecatedImportsCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)

	err = check.Fix(ctx, findings)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "predict.py"))
	require.NoError(t, err)
	require.NotContains(t, string(content), "ExperimentalFeatureWarning")
	require.NotContains(t, string(content), "cog.types")
	require.NotContains(t, string(content), "import warnings")

	// Re-parse and verify clean
	ctx.PythonFiles = parsePythonFiles(t, dir, "predict.py")
	findings, err = check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestDeprecatedImportsCheck_Fix_WarningsImportPreserved(t *testing.T) {
	// When warnings module is still used elsewhere, import should be preserved
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `import warnings
from cog import BasePredictor
from cog.types import ExperimentalFeatureWarning

warnings.filterwarnings("ignore", category=ExperimentalFeatureWarning)
warnings.warn("something else")


class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return "hello " + text
`)

	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
	}

	check := &DeprecatedImportsCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)

	err = check.Fix(ctx, findings)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "predict.py"))
	require.NoError(t, err)
	require.NotContains(t, string(content), "ExperimentalFeatureWarning")
	require.Contains(t, string(content), "import warnings")
	require.Contains(t, string(content), "warnings.warn")
}

func TestMissingTypeAnnotationsCheck_Clean(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text
`)

	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
		Config:      &config.Config{Predict: "predict.py:Predictor"},
	}

	check := &MissingTypeAnnotationsCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestMissingTypeAnnotationsCheck_MissingReturnType(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "predict.py", `from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self, text: str):
        return text
`)

	ctx := &CheckContext{
		ctx:         context.Background(),
		ProjectDir:  dir,
		PythonFiles: parsePythonFiles(t, dir, "predict.py"),
		Config:      &config.Config{Predict: "predict.py:Predictor"},
	}

	check := &MissingTypeAnnotationsCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, SeverityWarning, findings[0].Severity)
	require.Contains(t, findings[0].Message, "predict()")
	require.Contains(t, findings[0].Message, "return type annotation")
}

func TestMissingTypeAnnotationsCheck_NoConfig(t *testing.T) {
	ctx := &CheckContext{
		ctx:        context.Background(),
		ProjectDir: t.TempDir(),
		Config:     nil,
	}

	check := &MissingTypeAnnotationsCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestMissingTypeAnnotationsCheck_FixReturnsNoAutoFix(t *testing.T) {
	check := &MissingTypeAnnotationsCheck{}
	err := check.Fix(nil, nil)
	require.ErrorIs(t, err, ErrNoAutoFix)
}

// parsePythonFiles is a test helper that parses Python files into ParsedFile structs.
// It registers a t.Cleanup to close tree-sitter trees, preventing C memory leaks.
func parsePythonFiles(t *testing.T, dir string, filenames ...string) map[string]*ParsedFile {
	t.Helper()
	files := make(map[string]*ParsedFile)
	for _, name := range filenames {
		source, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)

		sitterParser := sitter.NewParser()
		sitterParser.SetLanguage(python.GetLanguage())
		tree, err := sitterParser.ParseCtx(context.Background(), nil, source)
		require.NoError(t, err)

		imports := schemaPython.CollectImports(tree.RootNode(), source)
		files[name] = &ParsedFile{
			Path:    name,
			Source:  source,
			Tree:    tree,
			Imports: imports,
		}
	}
	t.Cleanup(func() {
		for _, pf := range files {
			if pf != nil && pf.Tree != nil {
				pf.Tree.Close()
			}
		}
	})
	return files
}
