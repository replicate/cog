package requirements

import (
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPythonRequirements(t *testing.T) {
	srcDir := t.TempDir()
	reqFile := path.Join(srcDir, "requirements.txt")
	err := os.WriteFile(reqFile, []byte("torch==2.5.1"), 0o644)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	requirementsFile, err := GenerateRequirements(tmpDir, reqFile, RequirementsFile)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(tmpDir, "requirements.txt"), requirementsFile)
}

func TestReadRequirements(t *testing.T) {
	srcDir := t.TempDir()
	reqFile := path.Join(srcDir, "requirements.txt")
	err := os.WriteFile(reqFile, []byte("torch==2.5.1"), 0o644)
	require.NoError(t, err)

	requirements, err := ReadRequirements(reqFile)
	require.NoError(t, err)
	require.Equal(t, []string{"torch==2.5.1"}, requirements)
}

func TestReadRequirementsLineContinuations(t *testing.T) {
	srcDir := t.TempDir()
	reqFile := path.Join(srcDir, "requirements.txt")
	err := os.WriteFile(reqFile, []byte("torch==\\\n2.5.1\ntorchvision==\\\r\n2.5.1"), 0o644)
	require.NoError(t, err)

	requirements, err := ReadRequirements(reqFile)
	require.NoError(t, err)
	require.Equal(t, []string{"torch==2.5.1", "torchvision==2.5.1"}, requirements)
}

func TestReadRequirementsStripComments(t *testing.T) {
	srcDir := t.TempDir()
	reqFile := path.Join(srcDir, "requirements.txt")
	err := os.WriteFile(reqFile, []byte("torch==\\\n2.5.1# Heres my comment\ntorchvision==2.5.1\n# Heres a beginning of line comment"), 0o644)
	require.NoError(t, err)

	requirements, err := ReadRequirements(reqFile)
	require.NoError(t, err)
	require.Equal(t, []string{"torch==2.5.1", "torchvision==2.5.1"}, requirements)
}

func TestReadRequirementsComplex(t *testing.T) {
	srcDir := t.TempDir()
	reqFile := path.Join(srcDir, "requirements.txt")
	err := os.WriteFile(reqFile, []byte(`foo==1.0.0
# complex requirements
fastapi>=0.6,<1
flask>0.4
# comments!
# blank lines!

# arguments
-f http://example.com`), 0o644)
	require.NoError(t, err)

	requirements, err := ReadRequirements(reqFile)
	require.NoError(t, err)
	require.Equal(t, []string{"foo==1.0.0", "fastapi>=0.6,<1", "flask>0.4", "-f http://example.com"}, requirements)
}
