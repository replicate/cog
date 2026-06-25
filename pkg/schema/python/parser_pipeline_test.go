package python

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/schema"
)

func TestParsePipelineResolvesImportedInheritedRunWithImportedModelOutput(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "base.py"), []byte(`
from cog import Input
from pydantic import BaseModel

DESCRIPTION = "base input"

class Output(BaseModel):
    text: str

class Base:
    def run(self, value: str = Input(description=DESCRIPTION)) -> Output:
        pass
`), 0o600))

	source := []byte(`
from base import Base

class Predictor(Base):
    pass
`)

	info, err := ParsePredictorWithSourcePath(source, "Predictor", schema.ModePredict, dir, "predict.py")
	require.NoError(t, err)

	value, ok := info.Inputs.Get("value")
	require.True(t, ok)
	require.NotNil(t, value.Description)
	require.Equal(t, "base input", *value.Description)
	require.Equal(t, schema.SchemaObject, info.Output.Kind)
	textField, ok := info.Output.Fields.Get("text")
	require.True(t, ok)
	require.Equal(t, schema.TypeString, textField.Type.Primitive)
}

func TestParsePipelineResolvesInputRegistryAndForwardRefOutput(t *testing.T) {
	source := []byte(`
from cog import BasePredictor, Input
from pydantic import BaseModel

class Node(BaseModel):
    child: "Node | None" = None

class Predictor(BasePredictor):
    name_input = Input(description="name from attr")

    def make_input(default="x"):
        return Input(default=default, description="from method")

    def run(
        self,
        name: str = Predictor.name_input,
        label: str = Predictor.make_input("y"),
    ) -> "Node":
        pass
`)

	info, err := ParsePredictor(source, "Predictor", schema.ModePredict, "")
	require.NoError(t, err)

	name, ok := info.Inputs.Get("name")
	require.True(t, ok)
	require.NotNil(t, name.Description)
	require.Equal(t, "name from attr", *name.Description)
	require.Nil(t, name.Default)

	label, ok := info.Inputs.Get("label")
	require.True(t, ok)
	require.NotNil(t, label.Description)
	require.Equal(t, "from method", *label.Description)
	require.NotNil(t, label.Default)
	require.Equal(t, schema.DefaultString, label.Default.Kind)
	require.Equal(t, "y", label.Default.Str)

	require.Equal(t, schema.SchemaObject, info.Output.Kind)
	childField, ok := info.Output.Fields.Get("child")
	require.True(t, ok)
	require.False(t, childField.Required)
	require.True(t, childField.Type.Nullable)
}
