package predict

import (
	"path/filepath"
	"strings"

	"github.com/mitchellh/go-homedir"
	"github.com/replicate/cog/pkg/util/console"
)

type InputValue struct {
	String *string
	File   *string
}

type Input map[string]InputValue

func NewInput(keyVals map[string]string) Input {
	input := Input{}
	for key, val := range keyVals {
		val := val
		if strings.HasPrefix(val, "@") {
			val = val[1:]
			expandedVal, err := homedir.Expand(val)
			if err != nil {
				// FIXME: handle this better?
				console.Warnf("Error expanding homedir: %s", err)
			} else {
				val = expandedVal
			}

			input[key] = InputValue{File: &val}
		} else {
			input[key] = InputValue{String: &val}
		}
	}
	return input
}

func NewExampleWithBaseDir(keyVals map[string]string, baseDir string) Input {
	input := Input{}
	for key, val := range keyVals {
		val := val
		if strings.HasPrefix(val, "@") {
			val = filepath.Join(baseDir, val[1:])
			input[key] = InputValue{File: &val}
		} else {
			input[key] = InputValue{String: &val}
		}
	}
	return input
}
