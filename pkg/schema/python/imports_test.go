package python

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollectImportsRecordsAliasesGroupedImportsAndBuiltins(t *testing.T) {
	parsed := parsePythonTestModule(t, `
import os
import numpy as np, pandas
from cog import Input as CogInput, BasePredictor
from typing import Optional, List as TList
`)

	imports := CollectImports(parsed.root, parsed.source)

	osEntry, ok := imports.Names.Get("os")
	require.True(t, ok)
	require.Equal(t, "os", osEntry.Module)
	require.Equal(t, "os", osEntry.Original)

	numpyEntry, ok := imports.Names.Get("np")
	require.True(t, ok)
	require.Equal(t, "numpy", numpyEntry.Module)
	require.Equal(t, "numpy", numpyEntry.Original)

	pandasEntry, ok := imports.Names.Get("pandas")
	require.True(t, ok)
	require.Equal(t, "pandas", pandasEntry.Module)
	require.Equal(t, "pandas", pandasEntry.Original)

	inputEntry, ok := imports.Names.Get("CogInput")
	require.True(t, ok)
	require.Equal(t, "cog", inputEntry.Module)
	require.Equal(t, "Input", inputEntry.Original)

	listEntry, ok := imports.Names.Get("TList")
	require.True(t, ok)
	require.Equal(t, "typing", listEntry.Module)
	require.Equal(t, "List", listEntry.Original)

	strEntry, ok := imports.Names.Get("str")
	require.True(t, ok)
	require.Equal(t, "builtins", strEntry.Module)
	require.Equal(t, "str", strEntry.Original)

	noneEntry, ok := imports.Names.Get("None")
	require.True(t, ok)
	require.Equal(t, "builtins", noneEntry.Module)
	require.Equal(t, "None", noneEntry.Original)
}

func TestCollectImportsRecordsRelativeImportWithoutModuleNameField(t *testing.T) {
	parsed := parsePythonTestModule(t, `
from .models import Output as ModelOutput
from ..shared.types import InputModel
`)

	imports := CollectImports(parsed.root, parsed.source)

	outputEntry, ok := imports.Names.Get("ModelOutput")
	require.True(t, ok)
	require.Equal(t, ".models", outputEntry.Module)
	require.Equal(t, "Output", outputEntry.Original)

	inputEntry, ok := imports.Names.Get("InputModel")
	require.True(t, ok)
	require.Equal(t, "..shared.types", inputEntry.Module)
	require.Equal(t, "InputModel", inputEntry.Original)
}
