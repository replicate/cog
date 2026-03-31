package python

import (
	"testing"

	schema "github.com/replicate/cog/pkg/schema"
)

// FuzzParsePredictor feeds arbitrary bytes as Python source to the parser.
// The parser should never panic regardless of input — it may return errors.
func FuzzParsePredictor(f *testing.F) {
	// Seed corpus: valid and invalid Python snippets.
	f.Add([]byte(`
from cog import BasePredictor
class Predictor(BasePredictor):
    def predict(self, x: str) -> str:
        return x
`), "Predictor", uint8(0))

	f.Add([]byte(`
from cog import BasePredictor
from pydantic import BaseModel
class Output(BaseModel):
    text: str
    score: float = 0.0
class Predictor(BasePredictor):
    def predict(self, x: str) -> Output:
        pass
`), "Predictor", uint8(0))

	f.Add([]byte(`
from cog import BasePredictor
class Predictor(BasePredictor):
    def predict(self, x: str) -> dict[str, list[int]]:
        return {}
`), "Predictor", uint8(0))

	f.Add([]byte(`
from cog import BasePredictor, ConcatenateIterator
class Predictor(BasePredictor):
    def predict(self, x: str) -> ConcatenateIterator[str]:
        yield x
`), "Predictor", uint8(0))

	// Training mode.
	f.Add([]byte(`
from cog import BasePredictor
class Predictor(BasePredictor):
    def train(self, x: str) -> str:
        return x
`), "Predictor", uint8(1))

	// No predictor class at all.
	f.Add([]byte(`print("hello")`), "Predictor", uint8(0))

	// Empty source.
	f.Add([]byte{}, "Predictor", uint8(0))

	// Garbage bytes.
	f.Add([]byte{0xff, 0xfe, 0x00, 0x01, 0x80, 0x90}, "Predictor", uint8(0))

	f.Fuzz(func(t *testing.T, source []byte, predictRef string, modeRaw uint8) {
		mode := schema.ModePredict
		if modeRaw%2 == 1 {
			mode = schema.ModeTrain
		}

		// Must not panic regardless of input.
		_, _ = ParsePredictor(source, predictRef, mode, "")
	})
}

// FuzzParseTypeAnnotation exercises the type annotation parser with
// arbitrary annotation strings embedded in a predict signature.
func FuzzParseTypeAnnotation(f *testing.F) {
	types := []string{
		"str", "int", "float", "bool", "Path",
		"dict", "dict[str, int]", "dict[str, list[str]]",
		"list[str]", "list[dict[str, float]]",
		"Optional[str]", "Optional[dict[str, int]]",
		"Iterator[str]", "ConcatenateIterator[str]",
		"dict[str, dict[str, dict[str, int]]]",
		"Any", "None", "list",
	}
	for _, typ := range types {
		f.Add(typ)
	}

	f.Fuzz(func(t *testing.T, typeName string) {
		// Build a minimal predict.py with the fuzzed return type.
		source := []byte("from cog import BasePredictor\nfrom typing import *\nclass Predictor(BasePredictor):\n    def predict(self, x: str) -> " + typeName + ":\n        pass\n")
		// Must not panic.
		_, _ = ParsePredictor(source, "Predictor", schema.ModePredict, "")
	})
}
