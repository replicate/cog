package predict

import (
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/mitchellh/go-homedir"
	"github.com/vincent-petithory/dataurl"
)

type Input struct {
	String *string
	File   *string
	Array  *[]any
}

type Inputs map[string]Input

func NewInputs(keyVals map[string][]string) Inputs {
	input := Inputs{}
	for key, vals := range keyVals {
		if len(vals) == 1 {
			val := vals[0]
			if strings.HasPrefix(val, "@") {
				val = val[1:]
				input[key] = Input{File: &val}
			} else {
				input[key] = Input{String: &val}
			}
		} else if len(vals) > 1 {
			var anyVals = make([]any, len(vals))
			for i, v := range vals {
				anyVals[i] = v
			}
			input[key] = Input{Array: &anyVals}
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

func (inputs *Inputs) toMap() (map[string]any, error) {
	keyVals := map[string]any{}
	for key, input := range *inputs {
		switch {
		case input.String != nil:
			// Directly assign the string value
			keyVals[key] = *input.String
		case input.File != nil:
			// Single file handling: read content and convert to a data URL
			dataURL, err := fileToDataURL(*input.File)
			if err != nil {
				return keyVals, err
			}
			keyVals[key] = dataURL
		case input.Array != nil:
			// Handle array, potentially containing file paths
			dataURLs := make([]string, len(*input.Array))
			for i, elem := range *input.Array {
				if str, ok := elem.(string); ok && strings.HasPrefix(str, "@") {
					filePath := str[1:] // Remove '@' prefix
					dataURL, err := fileToDataURL(filePath)
					if err != nil {
						return keyVals, err
					}
					dataURLs[i] = dataURL
				} else if ok {
					// Directly use the string if it's not a file path
					dataURLs[i] = str
				}
			}
			keyVals[key] = dataURLs
		}
	}
	return keyVals, nil
}

// Helper function to read file content and convert to a data URL
func fileToDataURL(filePath string) (string, error) {
	// Expand home directory if necessary
	expandedVal, err := homedir.Expand(filePath)
	if err != nil {
		return "", fmt.Errorf("error expanding homedir for '%s': %w", filePath, err)
	}

	content, err := os.ReadFile(expandedVal)
	if err != nil {
		return "", err
	}
	mimeType := mime.TypeByExtension(filepath.Ext(expandedVal))
	dataURL := dataurl.New(content, mimeType).String()
	return dataURL, nil
}
