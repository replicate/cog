package predict

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mitchellh/go-homedir"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/mime"
	"github.com/vincent-petithory/dataurl"
)

type Input struct {
	String     *string
	File       *string
	StringList *[]string
}

type Inputs map[string]Input

func NewInputs(keyVals map[string][]string) Inputs {
	input := Inputs{}
	for key, arr := range keyVals {
		arr := arr
		// Handle singleton slices by converting them to strings or expanding filenames
		if len(arr) == 1 {
			val := arr[0]
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
		} else {
			input[key] = Input{StringList: &arr}
		}
	}
	return input
}

func (inputs *Inputs) toMap() (map[string]interface{}, error) {
	keyVals := map[string]interface{}{}
	for key, input := range *inputs {
		if input.String != nil {
			keyVals[key] = *input.String
		} else if input.File != nil {
			content, err := os.ReadFile(*input.File)
			if err != nil {
				return keyVals, err
			}
			mimeType := mime.TypeByExtension(filepath.Ext(*input.File))
			keyVals[key] = dataurl.New(content, mimeType).String()
		} else if input.StringList != nil {
			keyVals[key] = *input.StringList
		}
	}
	return keyVals, nil
}
