package config

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvironmentConfig(t *testing.T) {
	t.Run("ParsingValidInput", func(t *testing.T) {
		cases := []struct {
			Name     string
			Input    []string
			Expected map[string]string
		}{
			{
				Name:     "ValidInput",
				Input:    []string{"NAME=VALUE"},
				Expected: map[string]string{"NAME": "VALUE"},
			},
			{
				Name:     "ValidInputWithSpaces",
				Input:    []string{"NAME=VALUE WITH SPACES"},
				Expected: map[string]string{"NAME": "VALUE WITH SPACES"},
			},
			{
				Name:     "ValidInputWithQuotes",
				Input:    []string{"NAME=\"VALUE WITH QUOTES\""},
				Expected: map[string]string{"NAME": `"VALUE WITH QUOTES"`},
			},
			{
				Name:     "DelimitedValue",
				Input:    []string{"NAME=VALUE1,VALUE2"},
				Expected: map[string]string{"NAME": "VALUE1,VALUE2"},
			},
			{
				Name:     "EmptyValue",
				Input:    []string{"NAME="},
				Expected: map[string]string{"NAME": ""},
			},
			{
				Name:     "EmptyValueWithSpaces",
				Input:    []string{"NAME= "},
				Expected: map[string]string{"NAME": " "},
			},
			{
				Name:     "LowerCaseName",
				Input:    []string{"name=VALUE"},
				Expected: map[string]string{"name": "VALUE"},
			},
			{
				Name:     "MixedCaseName",
				Input:    []string{"MiXeD_Case=VALUE"},
				Expected: map[string]string{"MiXeD_Case": "VALUE"},
			},
			{
				Name:     "EqualSignInValue",
				Input:    []string{"NAME=VALUE=EQUAL"},
				Expected: map[string]string{"NAME": "VALUE=EQUAL"},
			},
			{
				Name:     "EqualSignInValueWithSpaces",
				Input:    []string{"NAME=VALUE=EQUAL WITH SPACES"},
				Expected: map[string]string{"NAME": "VALUE=EQUAL WITH SPACES"},
			},
			{
				Name:     "MultiLineValue",
				Input:    []string{"NAME=VALUE1\nVALUE2"},
				Expected: map[string]string{"NAME": "VALUE1\nVALUE2"},
			},
			{
				Name:     "MultiplePairs",
				Input:    []string{"NAME1=VALUE1", "NAME2=VALUE2"},
				Expected: map[string]string{"NAME1": "VALUE1", "NAME2": "VALUE2"},
			},
		}

		for _, c := range cases {
			t.Run(c.Name, func(t *testing.T) {
				parsed, err := parseAndValidateEnvironment(c.Input)
				require.NoError(t, err)
				require.Equal(t, c.Expected, parsed)
			})
		}
	})

	t.Run("ParsingInvalidInput", func(t *testing.T) {
		cases := []struct {
			Name                 string
			Input                []string
			ExpectedErrorMessage string
		}{
			{
				Name:                 "NameWithoutValue",
				Input:                []string{"NAME"},
				ExpectedErrorMessage: `environment variable "NAME" is not in the KEY=VALUE format`,
			},
			{
				Name:                 "EmptyName",
				Input:                []string{"=VALUE"},
				ExpectedErrorMessage: `environment variable "=VALUE" is not in the KEY=VALUE format`,
			},
		}

		for _, c := range cases {
			t.Run(c.Name, func(t *testing.T) {
				_, err := parseAndValidateEnvironment(c.Input)
				require.Error(t, err)
				require.ErrorContains(t, err, c.ExpectedErrorMessage)
			})
		}
	})

	t.Run("EnforceDenyList", func(t *testing.T) {
		for _, pattern := range EnvironmentVariableDenyList {
			// test that exact matches are rejected
			t.Run(fmt.Sprintf("Rejects %q", pattern), func(t *testing.T) {
				input := fmt.Sprintf("%s=VALUE", pattern)
				_, err := parseAndValidateEnvironment([]string{input})
				require.Error(t, err)
				require.ErrorContains(t, err, fmt.Sprintf("environment variable %q is not allowed", pattern))
			})

			// test that prefix matches are rejected
			if strings.HasSuffix(pattern, "*") {
				t.Run(fmt.Sprintf("Rejects %q prefix", pattern), func(t *testing.T) {
					name := strings.TrimSuffix(pattern, "*") + "SUFFIX"
					input := fmt.Sprintf("%s=VALUE", name)
					_, err := parseAndValidateEnvironment([]string{input})
					require.Error(t, err)
					require.ErrorContains(t, err, fmt.Sprintf("environment variable %q is not allowed", name))
				})
			}
		}
	})

	t.Run("DuplicateNamesAreRejected", func(t *testing.T) {
		input := []string{"NAME=VALUE", "NAME=VALUE2"}
		_, err := parseAndValidateEnvironment(input)
		require.Error(t, err)
		require.ErrorContains(t, err, "environment variable \"NAME\" is already defined")
	})
}
