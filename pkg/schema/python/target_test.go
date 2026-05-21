package python

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/schema"
)

func TestFindPredictMethodInClassPrefersRunOverLegacyPredict(t *testing.T) {
	file := newPythonFileContextForTest(t, `
class Predictor:
    def predict(self, value: int) -> int:
        return value

    def run(self, value: str) -> str:
        return value
`)
	classNode := findClassByName(file.root, file.source, "Predictor")
	require.NotNil(t, classNode)

	target, err := findPredictMethodInClass(file, classNode, "Predictor")
	require.NoError(t, err)
	require.Equal(t, "run", Content(target.node.ChildByFieldName("name"), target.file.source))
}

func TestFindPredictMethodInClassCanDisableLegacyPredictFallback(t *testing.T) {
	file := newPythonFileContextForTest(t, `
class Predictor:
    def predict(self, value: int) -> int:
        return value
`)
	file.allowLegacy = false
	classNode := findClassByName(file.root, file.source, "Predictor")
	require.NotNil(t, classNode)

	_, err := findPredictMethodInClass(file, classNode, "Predictor")
	require.Error(t, err)
	var se *schema.SchemaError
	require.True(t, errors.As(err, &se))
	require.Equal(t, schema.ErrMethodNotFound, se.Kind)
	require.Contains(t, se.Error(), "run")
}

func TestCollectPredictMethodsFindsInheritedRunInSameFile(t *testing.T) {
	file := newPythonFileContextForTest(t, `
class Base:
    def run(self, value: str) -> str:
        return value

class Predictor(Base):
    pass
`)
	classNode := findClassByName(file.root, file.source, "Predictor")
	require.NotNil(t, classNode)

	runNode, predictNode := collectPredictMethods(file, classNode, "Predictor", map[string]bool{})
	require.NotNil(t, runNode)
	require.Nil(t, predictNode)
	require.Equal(t, "run", Content(runNode.node.ChildByFieldName("name"), runNode.file.source))
}

func TestFindTargetCallableNodeFallsBackToStandaloneRun(t *testing.T) {
	file := newPythonFileContextForTest(t, `
def predict(value: int) -> int:
    return value

def run(value: str) -> str:
    return value
`)

	target, err := findTargetCallableNode(file, "Predictor", "run")
	require.NoError(t, err)
	require.Equal(t, "run", Content(target.node.ChildByFieldName("name"), target.file.source))
}
