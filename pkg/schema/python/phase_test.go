package python

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/schema"
)

func TestRunPhasesAdvancesState(t *testing.T) {
	state := newParseState(defaultParserOptions([]byte(""), "Runner", schema.ModePredict, ""))
	phases := []parserPhase{
		{Name: "one", From: phaseInitial, To: phaseModuleParsed, Run: func(*ParseState) error { return nil }},
		{Name: "two", From: phaseModuleParsed, To: phaseImportsCollected, Run: func(*ParseState) error { return nil }},
	}

	require.NoError(t, runPhases(state, phases))
	require.Equal(t, phaseImportsCollected, state.Phase)
}

func TestRunPhasesRejectsOutOfOrderState(t *testing.T) {
	state := newParseState(defaultParserOptions([]byte(""), "Runner", schema.ModePredict, ""))
	state.Phase = phaseImportsCollected
	phases := []parserPhase{
		{Name: "one", From: phaseInitial, To: phaseModuleParsed, Run: func(*ParseState) error { return nil }},
	}

	err := runPhases(state, phases)
	require.Error(t, err)
	var se *schema.SchemaError
	require.True(t, errors.As(err, &se))
	require.Equal(t, schema.ErrParse, se.Kind)
	require.Contains(t, se.Error(), "expected parser state")
}
