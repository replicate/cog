//go:build ignore

package model

import (
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReference(t *testing.T) {
	testCases := []struct {
		input        string
		expectedName string
		expectedRef  string
	}{
		{input: "test/model:latest", expectedName: "test/model", expectedRef: "r8.im/test/model:latest"},
		{input: "nousername/model:latest", expectedName: "nousername/model", expectedRef: "r8.im/nousername/model:latest"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.input, func(t *testing.T) {

			parsedRef, err := name.ParseReference(testCase.input, name.WithDefaultRegistry("r8.im"))
			require.NoError(t, err)

			model := Model{
				Ref: parsedRef,
			}

			assert.Equal(t, testCase.expectedName, model.Name())
			assert.Equal(t, testCase.expectedRef, model.ImageRef())
		})
	}
}
