package doctor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockCheck is a test double for Check.
type mockCheck struct {
	name        string
	group       Group
	description string
	findings    []Finding
	checkErr    error
	fixErr      error
	fixCalled   bool
}

func (m *mockCheck) Name() string        { return m.name }
func (m *mockCheck) Group() Group        { return m.group }
func (m *mockCheck) Description() string { return m.description }

func (m *mockCheck) Check(_ *CheckContext) ([]Finding, error) {
	return m.findings, m.checkErr
}

func (m *mockCheck) Fix(_ *CheckContext, _ []Finding) error {
	m.fixCalled = true
	return m.fixErr
}

func TestRunCollectsFindings(t *testing.T) {
	checks := []Check{
		&mockCheck{
			name:     "passing-check",
			group:    GroupConfig,
			findings: nil,
		},
		&mockCheck{
			name:  "failing-check",
			group: GroupPython,
			findings: []Finding{
				{Severity: SeverityError, Message: "something is wrong"},
			},
		},
	}

	result, err := Run(context.Background(), RunOptions{ProjectDir: t.TempDir()}, checks)
	require.NoError(t, err)
	require.Len(t, result.Results, 2)
	require.Empty(t, result.Results[0].Findings)
	require.Len(t, result.Results[1].Findings, 1)
	require.Equal(t, "something is wrong", result.Results[1].Findings[0].Message)
}

func TestRunCallsFixWhenEnabled(t *testing.T) {
	check := &mockCheck{
		name:  "fixable-check",
		group: GroupPython,
		findings: []Finding{
			{Severity: SeverityError, Message: "fixable issue"},
		},
	}

	_, err := Run(context.Background(), RunOptions{Fix: true, ProjectDir: t.TempDir()}, []Check{check})
	require.NoError(t, err)
	require.True(t, check.fixCalled)
}

func TestRunDoesNotCallFixWhenDisabled(t *testing.T) {
	check := &mockCheck{
		name:  "fixable-check",
		group: GroupPython,
		findings: []Finding{
			{Severity: SeverityError, Message: "fixable issue"},
		},
	}

	_, err := Run(context.Background(), RunOptions{Fix: false, ProjectDir: t.TempDir()}, []Check{check})
	require.NoError(t, err)
	require.False(t, check.fixCalled)
}

func TestRunDoesNotCallFixWhenNoFindings(t *testing.T) {
	check := &mockCheck{
		name:     "clean-check",
		group:    GroupConfig,
		findings: nil,
	}

	_, err := Run(context.Background(), RunOptions{Fix: true, ProjectDir: t.TempDir()}, []Check{check})
	require.NoError(t, err)
	require.False(t, check.fixCalled)
}

func TestRunHandlesCheckError(t *testing.T) {
	checks := []Check{
		&mockCheck{
			name:     "error-check",
			group:    GroupEnvironment,
			checkErr: context.DeadlineExceeded,
		},
		&mockCheck{
			name:     "ok-check",
			group:    GroupConfig,
			findings: nil,
		},
	}

	result, err := Run(context.Background(), RunOptions{ProjectDir: t.TempDir()}, checks)
	require.NoError(t, err) // Run itself doesn't fail; individual check errors are captured
	require.Len(t, result.Results, 2)
	require.Error(t, result.Results[0].Err)
	require.NoError(t, result.Results[1].Err)
}

func TestRunMarksFixedOnSuccess(t *testing.T) {
	check := &mockCheck{
		name:  "fixable",
		group: GroupPython,
		findings: []Finding{
			{Severity: SeverityError, Message: "broken"},
		},
		fixErr: nil,
	}

	result, err := Run(context.Background(), RunOptions{Fix: true, ProjectDir: t.TempDir()}, []Check{check})
	require.NoError(t, err)
	require.True(t, result.Results[0].Fixed)
}

func TestRunMarksNotFixedOnErrNoAutoFix(t *testing.T) {
	check := &mockCheck{
		name:  "unfixable",
		group: GroupConfig,
		findings: []Finding{
			{Severity: SeverityWarning, Message: "deprecated"},
		},
		fixErr: ErrNoAutoFix,
	}

	result, err := Run(context.Background(), RunOptions{Fix: true, ProjectDir: t.TempDir()}, []Check{check})
	require.NoError(t, err)
	require.False(t, result.Results[0].Fixed)
}

func TestHasErrorsWithCheckError(t *testing.T) {
	result := &Result{
		Results: []CheckResult{
			{Err: errors.New("check failed")},
		},
	}
	require.True(t, result.HasErrors())
}
