package doctor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func TestDockerCheck_RunsWithoutError(t *testing.T) {
	ctx := &CheckContext{ctx: context.Background(), ProjectDir: t.TempDir()}
	check := &DockerCheck{}
	// We don't assert findings — Docker may or may not be available in the test environment.
	// We just verify the check doesn't panic or return an error.
	_, err := check.Check(ctx)
	require.NoError(t, err)
}

func TestDockerCheck_FixReturnsNoAutoFix(t *testing.T) {
	check := &DockerCheck{}
	err := check.Fix(nil, nil)
	require.ErrorIs(t, err, ErrNoAutoFix)
}

func TestPythonVersionCheck_RunsWithoutError(t *testing.T) {
	ctx := &CheckContext{ctx: context.Background(), ProjectDir: t.TempDir()}
	check := &PythonVersionCheck{}
	// Python may or may not be available; just ensure no panic or error.
	_, err := check.Check(ctx)
	require.NoError(t, err)
}

func TestPythonVersionCheck_NoPython(t *testing.T) {
	ctx := &CheckContext{
		ctx:        context.Background(),
		ProjectDir: t.TempDir(),
		PythonPath: "", // explicitly no Python
	}

	check := &PythonVersionCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, SeverityWarning, findings[0].Severity)
	require.Contains(t, findings[0].Message, "Python not found")
}

func TestPythonVersionCheck_FixReturnsNoAutoFix(t *testing.T) {
	check := &PythonVersionCheck{}
	err := check.Fix(nil, nil)
	require.ErrorIs(t, err, ErrNoAutoFix)
}

func TestMajorMinor(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"3.12.1", "3.12"},
		{"3.12", "3.12"},
		{"3", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			require.Equal(t, tt.want, majorMinor(tt.input))
		})
	}
}

func TestPythonVersionCheck_VersionMismatch(t *testing.T) {
	// This test verifies that when PythonPath is set but not a real binary,
	// we get a warning. We can't easily fake the version output without a real binary.
	ctx := &CheckContext{
		ctx:        context.Background(),
		ProjectDir: t.TempDir(),
		PythonPath: "/nonexistent/python3",
		Config: &config.Config{
			Build: &config.Build{
				PythonVersion: "3.12",
			},
		},
	}

	check := &PythonVersionCheck{}
	findings, err := check.Check(ctx)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, SeverityWarning, findings[0].Severity)
	require.Contains(t, findings[0].Message, "could not determine Python version")
}
