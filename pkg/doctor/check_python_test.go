package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/stretchr/testify/require"

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

// parsePythonFiles is a test helper that parses Python files into ParsedFile structs.
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
	return files
}
