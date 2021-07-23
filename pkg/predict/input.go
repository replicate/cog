package predict

import (
	"path/filepath"
	"strings"

	"github.com/mitchellh/go-homedir"
	"github.com/replicate/cog/pkg/util/console"
)

type Input struct {
	String *string
	File   *string
}

type Inputs map[string]Input

func NewInputs(keyVals map[string]string) Inputs {
	input := Inputs{}
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

			input[key] = Input{File: &val}
		} else {
			input[key] = Input{String: &val}
		}
	}
	return input
}

func NewInputsWithBaseDir(keyVals map[string]string, baseDir string) Inputs {
	input := Inputs{}
	for key, val := range keyVals {
		val := val
		if strings.HasPrefix(val, "@") {
			val = filepath.Join(baseDir, val[1:])
			input[key] = Input{File: &val}
		} else {
			input[key] = Input{String: &val}
		}
	}
	return input
}
