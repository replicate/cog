package python

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/schema"
)

// helper that parses in predict mode and fails on error.
func parse(t *testing.T, source, predictRef string) *schema.PredictorInfo {
	t.Helper()
	info, err := ParsePredictor([]byte(source), predictRef, schema.ModePredict)
	require.NoError(t, err)
	return info
}

// helper to parse and expect an error.
func parseErr(t *testing.T, source, predictRef string, mode schema.Mode) *schema.SchemaError {
	t.Helper()
	_, err := ParsePredictor([]byte(source), predictRef, mode)
	require.Error(t, err)
	var se *schema.SchemaError
	require.True(t, errors.As(err, &se), "expected *schema.SchemaError, got %T: %v", err, err)
	return se
}

// ---------------------------------------------------------------------------
// Basic predictor tests
// ---------------------------------------------------------------------------

func TestSimpleStringPredictor(t *testing.T) {
	source := `
from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self, s: str) -> str:
        return "hello " + s
`
	info := parse(t, source, "Predictor")
	require.Equal(t, 1, info.Inputs.Len())

	s, ok := info.Inputs.Get("s")
	require.True(t, ok)
	require.Equal(t, schema.TypeString, s.FieldType.Primitive)
	require.Equal(t, schema.Required, s.FieldType.Repetition)
	require.Nil(t, s.Default)
	require.True(t, s.IsRequired())

	require.Equal(t, schema.OutputSingle, info.Output.Kind)
	require.NotNil(t, info.Output.Primitive)
	require.Equal(t, schema.TypeString, *info.Output.Primitive)
}

func TestMultipleInputsWithDefaults(t *testing.T) {
	source := `
from cog import BasePredictor, Input, Path

class Predictor(BasePredictor):
    def predict(
        self,
        image: Path = Input(description="Grayscale input image"),
        scale: float = Input(description="Factor to scale image by", ge=0, le=10, default=1.5),
    ) -> Path:
        pass
`
	info := parse(t, source, "Predictor")
	require.Equal(t, 2, info.Inputs.Len())

	image, ok := info.Inputs.Get("image")
	require.True(t, ok)
	require.Equal(t, schema.TypePath, image.FieldType.Primitive)
	require.Nil(t, image.Default)
	require.NotNil(t, image.Description)
	require.Equal(t, "Grayscale input image", *image.Description)
	require.True(t, image.IsRequired())

	scale, ok := info.Inputs.Get("scale")
	require.True(t, ok)
	require.Equal(t, schema.TypeFloat, scale.FieldType.Primitive)
	require.NotNil(t, scale.Default)
	require.Equal(t, schema.DefaultFloat, scale.Default.Kind)
	require.Equal(t, 1.5, scale.Default.Float)
	require.NotNil(t, scale.GE)
	require.Equal(t, 0.0, *scale.GE)
	require.NotNil(t, scale.LE)
	require.Equal(t, 10.0, *scale.LE)
	require.False(t, scale.IsRequired())
}

// ---------------------------------------------------------------------------
// Optional / union inputs
// ---------------------------------------------------------------------------

func TestOptionalInputPipeNone(t *testing.T) {
	source := `
from cog import BasePredictor, Input, Path

class Predictor(BasePredictor):
    def predict(
        self,
        test_image: Path | None = Input(description="Test image", default=None),
    ) -> Path:
        pass
`
	info := parse(t, source, "Predictor")
	img, ok := info.Inputs.Get("test_image")
	require.True(t, ok)
	require.Equal(t, schema.Optional, img.FieldType.Repetition)
	require.Equal(t, schema.TypePath, img.FieldType.Primitive)
	require.NotNil(t, img.Default)
	require.Equal(t, schema.DefaultNone, img.Default.Kind)
}

func TestOptionalInputTyping(t *testing.T) {
	source := `
from typing import Optional
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(
        self,
        name: Optional[str] = Input(default=None),
    ) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	name, ok := info.Inputs.Get("name")
	require.True(t, ok)
	require.Equal(t, schema.Optional, name.FieldType.Repetition)
	require.Equal(t, schema.TypeString, name.FieldType.Primitive)
}

// ---------------------------------------------------------------------------
// List inputs
// ---------------------------------------------------------------------------

func TestListInput(t *testing.T) {
	source := `
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self, paths: list[str] = Input(description="Paths")) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	paths, ok := info.Inputs.Get("paths")
	require.True(t, ok)
	require.Equal(t, schema.Repeated, paths.FieldType.Repetition)
	require.Equal(t, schema.TypeString, paths.FieldType.Primitive)
}

// ---------------------------------------------------------------------------
// Choices
// ---------------------------------------------------------------------------

func TestChoicesLiteralList(t *testing.T) {
	source := `
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self, color: str = Input(choices=["red", "green", "blue"])) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	color, ok := info.Inputs.Get("color")
	require.True(t, ok)
	require.Len(t, color.Choices, 3)
	require.Equal(t, "red", color.Choices[0].Str)
	require.Equal(t, "green", color.Choices[1].Str)
	require.Equal(t, "blue", color.Choices[2].Str)
}

func TestChoicesModuleLevelListVar(t *testing.T) {
	source := `
from cog import BasePredictor, Input

MY_CHOICES = ["x", "y", "z"]

class Predictor(BasePredictor):
    def predict(self, v: str = Input(choices=MY_CHOICES)) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	v, ok := info.Inputs.Get("v")
	require.True(t, ok)
	require.Len(t, v.Choices, 3)
	require.Equal(t, "x", v.Choices[0].Str)
	require.Equal(t, "y", v.Choices[1].Str)
	require.Equal(t, "z", v.Choices[2].Str)
}

func TestChoicesListDictKeys(t *testing.T) {
	source := `
from cog import BasePredictor, Input

ASPECT_RATIOS = {
    "1:1": (1024, 1024),
    "16:9": (1344, 768),
    "2:3": (832, 1216),
}

class Predictor(BasePredictor):
    def predict(self, ar: str = Input(choices=list(ASPECT_RATIOS.keys()), default="1:1")) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	ar, ok := info.Inputs.Get("ar")
	require.True(t, ok)
	require.Len(t, ar.Choices, 3)
	require.Equal(t, "1:1", ar.Choices[0].Str)
	require.Equal(t, "16:9", ar.Choices[1].Str)
	require.Equal(t, "2:3", ar.Choices[2].Str)
}

func TestChoicesListDictValues(t *testing.T) {
	source := `
from cog import BasePredictor, Input

LABELS = {"fast": "Fast Mode", "slow": "Slow Mode"}

class Predictor(BasePredictor):
    def predict(self, m: str = Input(choices=list(LABELS.values()))) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	m, ok := info.Inputs.Get("m")
	require.True(t, ok)
	require.Len(t, m.Choices, 2)
	require.Equal(t, "Fast Mode", m.Choices[0].Str)
	require.Equal(t, "Slow Mode", m.Choices[1].Str)
}

func TestChoicesDictKeysPlusLiteral(t *testing.T) {
	source := `
from cog import BasePredictor, Input

SIZES = {"small": 256, "large": 1024}

class Predictor(BasePredictor):
    def predict(self, s: str = Input(choices=list(SIZES.keys()) + ["custom"])) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	s, ok := info.Inputs.Get("s")
	require.True(t, ok)
	require.Len(t, s.Choices, 3)
	require.Equal(t, "small", s.Choices[0].Str)
	require.Equal(t, "large", s.Choices[1].Str)
	require.Equal(t, "custom", s.Choices[2].Str)
}

func TestChoicesIntegerDictKeys(t *testing.T) {
	source := `
from cog import BasePredictor, Input

STEP_LABELS = {1: "one", 2: "two", 4: "four"}

class Predictor(BasePredictor):
    def predict(self, steps: int = Input(choices=list(STEP_LABELS.keys()), default=1)) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	steps, ok := info.Inputs.Get("steps")
	require.True(t, ok)
	require.Len(t, steps.Choices, 3)
	require.Equal(t, schema.DefaultInt, steps.Choices[0].Kind)
	require.Equal(t, int64(1), steps.Choices[0].Int)
	require.Equal(t, int64(2), steps.Choices[1].Int)
	require.Equal(t, int64(4), steps.Choices[2].Int)
}

func TestChoicesConcatTwoVars(t *testing.T) {
	source := `
from cog import BasePredictor, Input

BASE = ["a", "b"]
EXTRA = ["c"]

class Predictor(BasePredictor):
    def predict(self, x: str = Input(choices=BASE + EXTRA)) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	x, ok := info.Inputs.Get("x")
	require.True(t, ok)
	require.Len(t, x.Choices, 3)
	require.Equal(t, "a", x.Choices[0].Str)
	require.Equal(t, "b", x.Choices[1].Str)
	require.Equal(t, "c", x.Choices[2].Str)
}

// ---------------------------------------------------------------------------
// Choices error cases
// ---------------------------------------------------------------------------

func TestChoicesVarNotAListErrors(t *testing.T) {
	source := `
from cog import BasePredictor, Input

NOT_A_LIST = "oops"

class Predictor(BasePredictor):
    def predict(self, x: str = Input(choices=NOT_A_LIST)) -> str:
        pass
`
	se := parseErr(t, source, "Predictor", schema.ModePredict)
	require.Equal(t, schema.ErrChoicesNotResolvable, se.Kind)
}

func TestChoicesUndefinedVarErrors(t *testing.T) {
	source := `
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self, x: str = Input(choices=DOES_NOT_EXIST)) -> str:
        pass
`
	se := parseErr(t, source, "Predictor", schema.ModePredict)
	require.Equal(t, schema.ErrChoicesNotResolvable, se.Kind)
}

func TestChoicesArbitraryCallErrors(t *testing.T) {
	source := `
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self, x: str = Input(choices=get_choices())) -> str:
        pass
`
	se := parseErr(t, source, "Predictor", schema.ModePredict)
	require.Equal(t, schema.ErrChoicesNotResolvable, se.Kind)
}

func TestChoicesListComprehensionErrors(t *testing.T) {
	source := `
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self, x: str = Input(choices=[f"{i}x" for i in range(5)])) -> str:
        pass
`
	se := parseErr(t, source, "Predictor", schema.ModePredict)
	require.Equal(t, schema.ErrChoicesNotResolvable, se.Kind)
}

func TestChoicesErrorIncludesParamName(t *testing.T) {
	source := `
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self, my_param: str = Input(choices=some_func())) -> str:
        pass
`
	se := parseErr(t, source, "Predictor", schema.ModePredict)
	require.Contains(t, se.Message, "my_param")
}

func TestChoicesNestedVarNotInScope(t *testing.T) {
	source := `
from cog import BasePredictor, Input

def helper():
    NESTED = ["a", "b"]

class Predictor(BasePredictor):
    def predict(self, x: str = Input(choices=NESTED)) -> str:
        pass
`
	se := parseErr(t, source, "Predictor", schema.ModePredict)
	require.Equal(t, schema.ErrChoicesNotResolvable, se.Kind)
}

// ---------------------------------------------------------------------------
// Standalone function
// ---------------------------------------------------------------------------

func TestStandaloneFunction(t *testing.T) {
	source := `
from cog import Input

def predict(text: str = Input(default="world")) -> str:
    return f"hello {text}"
`
	info := parse(t, source, "predict")
	require.Equal(t, 1, info.Inputs.Len())

	text, ok := info.Inputs.Get("text")
	require.True(t, ok)
	require.NotNil(t, text.Default)
	require.Equal(t, schema.DefaultString, text.Default.Kind)
	require.Equal(t, "world", text.Default.Str)
}

// ---------------------------------------------------------------------------
// Output types
// ---------------------------------------------------------------------------

func TestIteratorOutput(t *testing.T) {
	source := `
from typing import Iterator
from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self, count: int) -> Iterator[str]:
        for i in range(count):
            yield f"chunk {i}"
`
	info := parse(t, source, "Predictor")
	require.Equal(t, schema.OutputIterator, info.Output.Kind)
	require.NotNil(t, info.Output.Primitive)
	require.Equal(t, schema.TypeString, *info.Output.Primitive)
}

func TestConcatenateIteratorOutput(t *testing.T) {
	source := `
from cog import BasePredictor, ConcatenateIterator

class Predictor(BasePredictor):
    def predict(self, prompt: str) -> ConcatenateIterator[str]:
        yield "hello "
        yield "world"
`
	info := parse(t, source, "Predictor")
	require.Equal(t, schema.OutputConcatenateIterator, info.Output.Kind)
	require.NotNil(t, info.Output.Primitive)
	require.Equal(t, schema.TypeString, *info.Output.Primitive)
}

func TestConcatenateIteratorNotStrErrors(t *testing.T) {
	source := `
from cog import BasePredictor, ConcatenateIterator

class Predictor(BasePredictor):
    def predict(self, n: int) -> ConcatenateIterator[int]:
        pass
`
	se := parseErr(t, source, "Predictor", schema.ModePredict)
	require.Equal(t, schema.ErrConcatIteratorNotStr, se.Kind)
}

func TestListOutput(t *testing.T) {
	source := `
from cog import BasePredictor, Path

class Predictor(BasePredictor):
    def predict(self, n: int) -> list[Path]:
        pass
`
	info := parse(t, source, "Predictor")
	require.Equal(t, schema.OutputList, info.Output.Kind)
	require.NotNil(t, info.Output.Primitive)
	require.Equal(t, schema.TypePath, *info.Output.Primitive)
}

func TestBaseModelOutput(t *testing.T) {
	source := `
from cog import BasePredictor, BaseModel

class ModelOutput(BaseModel):
    text: str
    score: float

class Predictor(BasePredictor):
    def predict(self, prompt: str) -> ModelOutput:
        pass
`
	info := parse(t, source, "Predictor")
	require.Equal(t, schema.OutputObject, info.Output.Kind)
	require.NotNil(t, info.Output.Fields)
	require.Equal(t, 2, info.Output.Fields.Len())

	text, ok := info.Output.Fields.Get("text")
	require.True(t, ok)
	require.Equal(t, schema.TypeString, text.FieldType.Primitive)

	score, ok := info.Output.Fields.Get("score")
	require.True(t, ok)
	require.Equal(t, schema.TypeFloat, score.FieldType.Primitive)
}

func TestOptionalOutputErrors(t *testing.T) {
	source := `
from typing import Optional
from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self, s: str) -> Optional[str]:
        pass
`
	se := parseErr(t, source, "Predictor", schema.ModePredict)
	require.Equal(t, schema.ErrOptionalOutput, se.Kind)
}

func TestOptionalOutputPipeNoneErrors(t *testing.T) {
	source := `
from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self, s: str) -> str | None:
        pass
`
	se := parseErr(t, source, "Predictor", schema.ModePredict)
	require.Equal(t, schema.ErrOptionalOutput, se.Kind)
}

// ---------------------------------------------------------------------------
// Train mode
// ---------------------------------------------------------------------------

func TestTrainMode(t *testing.T) {
	source := `
from cog import Input, Path

def train(n: int) -> Path:
    pass
`
	info, err := ParsePredictor([]byte(source), "train", schema.ModeTrain)
	require.NoError(t, err)
	require.Equal(t, schema.ModeTrain, info.Mode)
	require.Equal(t, 1, info.Inputs.Len())
}

// ---------------------------------------------------------------------------
// Non-BasePredictor class (just has predict method)
// ---------------------------------------------------------------------------

func TestNonBasePredictor(t *testing.T) {
	source := `
from cog import Input

class Predictor:
    def predict(self, text: str = Input(default="hello")) -> str:
        return f"hello {text}"
`
	info := parse(t, source, "Predictor")
	require.Equal(t, 1, info.Inputs.Len())
	text, ok := info.Inputs.Get("text")
	require.True(t, ok)
	require.NotNil(t, text.Default)
	require.Equal(t, "hello", text.Default.Str)
}

// ---------------------------------------------------------------------------
// default_factory hard error
// ---------------------------------------------------------------------------

func TestDefaultFactoryError(t *testing.T) {
	source := `
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self, items: list[str] = Input(default_factory=list)) -> str:
        pass
`
	se := parseErr(t, source, "Predictor", schema.ModePredict)
	require.Equal(t, schema.ErrDefaultFactoryNotSupported, se.Kind)
}

// ---------------------------------------------------------------------------
// Module-scope default resolution
// ---------------------------------------------------------------------------

func TestDefaultModuleLevelStringInInput(t *testing.T) {
	source := `
from cog import BasePredictor, Input

DEFAULT_RATIO = "1:1"

class Predictor(BasePredictor):
    def predict(self, ar: str = Input(default=DEFAULT_RATIO)) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	ar, ok := info.Inputs.Get("ar")
	require.True(t, ok)
	require.NotNil(t, ar.Default)
	require.Equal(t, schema.DefaultString, ar.Default.Kind)
	require.Equal(t, "1:1", ar.Default.Str)
}

func TestDefaultModuleLevelIntInInput(t *testing.T) {
	source := `
from cog import BasePredictor, Input

DEFAULT_STEPS = 50

class Predictor(BasePredictor):
    def predict(self, steps: int = Input(default=DEFAULT_STEPS)) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	steps, ok := info.Inputs.Get("steps")
	require.True(t, ok)
	require.NotNil(t, steps.Default)
	require.Equal(t, schema.DefaultInt, steps.Default.Kind)
	require.Equal(t, int64(50), steps.Default.Int)
}

func TestDefaultModuleLevelListInInput(t *testing.T) {
	source := `
from cog import BasePredictor, Input

DEFAULT_TAGS = ["a", "b"]

class Predictor(BasePredictor):
    def predict(self, tags: list[str] = Input(default=DEFAULT_TAGS)) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	tags, ok := info.Inputs.Get("tags")
	require.True(t, ok)
	require.NotNil(t, tags.Default)
	require.Equal(t, schema.DefaultList, tags.Default.Kind)
	require.Len(t, tags.Default.List, 2)
	require.Equal(t, "a", tags.Default.List[0].Str)
	require.Equal(t, "b", tags.Default.List[1].Str)
}

func TestDefaultModuleLevelVarPlain(t *testing.T) {
	source := `
from cog import BasePredictor

MY_DEFAULT = "hello"

class Predictor(BasePredictor):
    def predict(self, text: str = MY_DEFAULT) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	text, ok := info.Inputs.Get("text")
	require.True(t, ok)
	require.NotNil(t, text.Default)
	require.Equal(t, schema.DefaultString, text.Default.Kind)
	require.Equal(t, "hello", text.Default.Str)
}

func TestDefaultUndefinedVarPlainErrors(t *testing.T) {
	source := `
from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self, text: str = UNDEFINED_VAR) -> str:
        pass
`
	se := parseErr(t, source, "Predictor", schema.ModePredict)
	require.Contains(t, se.Message, "cannot be statically resolved")
}

// ---------------------------------------------------------------------------
// InputRegistry — class attribute reference
// ---------------------------------------------------------------------------

func TestInputRegistryAttribute(t *testing.T) {
	source := `
from dataclasses import dataclass
from cog import BasePredictor, Input

RATIOS = {"1:1": (1024, 1024), "16:9": (1344, 768)}

@dataclass(frozen=True)
class Inputs:
    ar = Input(description="Aspect ratio", choices=list(RATIOS.keys()), default="1:1")

class Predictor(BasePredictor):
    def predict(self, ar: str = Inputs.ar) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	ar, ok := info.Inputs.Get("ar")
	require.True(t, ok)
	require.NotNil(t, ar.Description)
	require.Equal(t, "Aspect ratio", *ar.Description)
	require.Len(t, ar.Choices, 2)
	require.Equal(t, "1:1", ar.Choices[0].Str)
	require.Equal(t, "16:9", ar.Choices[1].Str)
	require.NotNil(t, ar.Default)
	require.Equal(t, "1:1", ar.Default.Str)
}

// ---------------------------------------------------------------------------
// InputRegistry — static method reference
// ---------------------------------------------------------------------------

func TestInputRegistryMethod(t *testing.T) {
	source := `
from dataclasses import dataclass
from cog import BasePredictor, Input

@dataclass(frozen=True)
class Inputs:
    @staticmethod
    def guidance(default: float) -> Input:
        return Input(description="Guidance scale", ge=0.0, le=20.0, default=default)

class Predictor(BasePredictor):
    def predict(self, guidance_scale: float = Inputs.guidance(7.5)) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	gs, ok := info.Inputs.Get("guidance_scale")
	require.True(t, ok)
	require.NotNil(t, gs.Description)
	require.Equal(t, "Guidance scale", *gs.Description)
	require.NotNil(t, gs.GE)
	require.Equal(t, 0.0, *gs.GE)
	require.NotNil(t, gs.LE)
	require.Equal(t, 20.0, *gs.LE)
	require.NotNil(t, gs.Default)
	require.Equal(t, schema.DefaultFloat, gs.Default.Kind)
	require.Equal(t, 7.5, gs.Default.Float)
}

// ---------------------------------------------------------------------------
// Error cases: missing annotations, predictor not found, etc.
// ---------------------------------------------------------------------------

func TestPredictorNotFound(t *testing.T) {
	source := `
from cog import BasePredictor

class Other(BasePredictor):
    def predict(self, s: str) -> str:
        pass
`
	se := parseErr(t, source, "Predictor", schema.ModePredict)
	require.Equal(t, schema.ErrPredictorNotFound, se.Kind)
}

func TestMethodNotFound(t *testing.T) {
	source := `
from cog import BasePredictor

class Predictor(BasePredictor):
    def setup(self):
        pass
`
	se := parseErr(t, source, "Predictor", schema.ModePredict)
	require.Equal(t, schema.ErrMethodNotFound, se.Kind)
}

func TestMissingReturnType(t *testing.T) {
	source := `
from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self, s: str):
        pass
`
	se := parseErr(t, source, "Predictor", schema.ModePredict)
	require.Equal(t, schema.ErrMissingReturnType, se.Kind)
}

func TestMissingTypeAnnotation(t *testing.T) {
	source := `
from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self, s="hello") -> str:
        pass
`
	se := parseErr(t, source, "Predictor", schema.ModePredict)
	require.Equal(t, schema.ErrMissingTypeAnnotation, se.Kind)
}

// ---------------------------------------------------------------------------
// All input types
// ---------------------------------------------------------------------------

func TestAllPrimitiveInputTypes(t *testing.T) {
	tests := []struct {
		name     string
		pyType   string
		expected schema.PrimitiveType
	}{
		{"str", "str", schema.TypeString},
		{"int", "int", schema.TypeInteger},
		{"float", "float", schema.TypeFloat},
		{"bool", "bool", schema.TypeBool},
		{"Path", "Path", schema.TypePath},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := `
from cog import BasePredictor, Path

class Predictor(BasePredictor):
    def predict(self, x: ` + tt.pyType + `) -> str:
        pass
`
			info := parse(t, source, "Predictor")
			x, ok := info.Inputs.Get("x")
			require.True(t, ok)
			require.Equal(t, tt.expected, x.FieldType.Primitive)
			require.Equal(t, schema.Required, x.FieldType.Repetition)
		})
	}
}

// ---------------------------------------------------------------------------
// Input() with constraints
// ---------------------------------------------------------------------------

func TestInputConstraints(t *testing.T) {
	source := `
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(
        self,
        text: str = Input(
            description="Input text",
            min_length=1,
            max_length=100,
            regex="^[a-z]+$",
        ),
    ) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	text, ok := info.Inputs.Get("text")
	require.True(t, ok)
	require.NotNil(t, text.Description)
	require.Equal(t, "Input text", *text.Description)
	require.NotNil(t, text.MinLength)
	require.Equal(t, uint64(1), *text.MinLength)
	require.NotNil(t, text.MaxLength)
	require.Equal(t, uint64(100), *text.MaxLength)
	require.NotNil(t, text.Regex)
	require.Equal(t, "^[a-z]+$", *text.Regex)
}

// ---------------------------------------------------------------------------
// Negative numbers and booleans as defaults
// ---------------------------------------------------------------------------

func TestNegativeNumberDefault(t *testing.T) {
	source := `
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self, temp: float = Input(default=-1.5)) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	temp, ok := info.Inputs.Get("temp")
	require.True(t, ok)
	require.NotNil(t, temp.Default)
	require.Equal(t, schema.DefaultFloat, temp.Default.Kind)
	require.Equal(t, -1.5, temp.Default.Float)
}

func TestBoolDefault(t *testing.T) {
	source := `
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self, flag: bool = Input(default=True)) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	flag, ok := info.Inputs.Get("flag")
	require.True(t, ok)
	require.NotNil(t, flag.Default)
	require.Equal(t, schema.DefaultBool, flag.Default.Kind)
	require.True(t, flag.Default.Bool)
}

// ---------------------------------------------------------------------------
// Parameter ordering
// ---------------------------------------------------------------------------

func TestParameterOrdering(t *testing.T) {
	source := `
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self, alpha: str, beta: int, gamma: float = Input(default=1.0)) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	require.Equal(t, 3, info.Inputs.Len())

	// Check insertion order
	keys := info.Inputs.Keys()
	require.Equal(t, "alpha", keys[0])
	require.Equal(t, "beta", keys[1])
	require.Equal(t, "gamma", keys[2])

	alpha, _ := info.Inputs.Get("alpha")
	require.Equal(t, 0, alpha.Order)
	beta, _ := info.Inputs.Get("beta")
	require.Equal(t, 1, beta.Order)
	gamma, _ := info.Inputs.Get("gamma")
	require.Equal(t, 2, gamma.Order)
}

// ---------------------------------------------------------------------------
// Deprecated flag
// ---------------------------------------------------------------------------

func TestDeprecatedInput(t *testing.T) {
	source := `
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self, old_param: str = Input(deprecated=True, default="x")) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	old, ok := info.Inputs.Get("old_param")
	require.True(t, ok)
	require.NotNil(t, old.Deprecated)
	require.True(t, *old.Deprecated)
}

// ---------------------------------------------------------------------------
// File type (deprecated alias for Path)
// ---------------------------------------------------------------------------

func TestFileType(t *testing.T) {
	source := `
from cog import BasePredictor, File

class Predictor(BasePredictor):
    def predict(self, f: File) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	f, ok := info.Inputs.Get("f")
	require.True(t, ok)
	require.Equal(t, schema.TypeFile, f.FieldType.Primitive)
}

// ---------------------------------------------------------------------------
// Secret type
// ---------------------------------------------------------------------------

func TestSecretType(t *testing.T) {
	source := `
from cog import BasePredictor, Secret

class Predictor(BasePredictor):
    def predict(self, token: Secret) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	token, ok := info.Inputs.Get("token")
	require.True(t, ok)
	require.Equal(t, schema.TypeSecret, token.FieldType.Primitive)
}

// ---------------------------------------------------------------------------
// Multiple classes — finds the right one
// ---------------------------------------------------------------------------

func TestMultipleClassesFindsTarget(t *testing.T) {
	source := `
from cog import BasePredictor, BaseModel

class Output(BaseModel):
    text: str

class Helper:
    pass

class Predictor(BasePredictor):
    def predict(self, s: str) -> str:
        pass
`
	info := parse(t, source, "Predictor")
	require.Equal(t, 1, info.Inputs.Len())
	require.Equal(t, schema.OutputSingle, info.Output.Kind)
}

// ---------------------------------------------------------------------------
// BaseModel with defaults
// ---------------------------------------------------------------------------

func TestBaseModelOutputWithDefaults(t *testing.T) {
	source := `
from cog import BasePredictor, BaseModel

class Result(BaseModel):
    text: str
    confidence: float = 0.0

class Predictor(BasePredictor):
    def predict(self, s: str) -> Result:
        pass
`
	info := parse(t, source, "Predictor")
	require.Equal(t, schema.OutputObject, info.Output.Kind)

	conf, ok := info.Output.Fields.Get("confidence")
	require.True(t, ok)
	require.NotNil(t, conf.Default)
	require.Equal(t, schema.DefaultFloat, conf.Default.Kind)
	require.Equal(t, 0.0, conf.Default.Float)
}
