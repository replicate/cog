package predict

import (
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/vincent-petithory/dataurl"

	"github.com/mitchellh/go-homedir"
	"github.com/replicate/cog/pkg/util/console"
)

type Input struct {
	String *string
	File   *string
	Array  *[]interface{}
}

type Inputs map[string]Input

func NewInputs(keyVals map[string]string) Inputs {
	input := Inputs{}
	for key, val := range keyVals {
		val := val
		if strings.HasPrefix(val, "@") {
			val = val[1:]
			input[key] = Input{File: &val}
		} else if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
			// Handle array of strings
			var arr []interface{}
			if err := json.Unmarshal([]byte(val), &arr); err != nil {
				// Handle JSON unmarshalling error
				console.Warnf("Error parsing array for key '%s': %s", key, err)
			} else {
				input[key] = Input{Array: &arr}
			}
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

func (inputs *Inputs) toMap() (map[string]interface{}, error) {
	keyVals := map[string]interface{}{}
	for key, input := range *inputs {
		if input.String != nil {
			// Directly assign the string value
			keyVals[key] = *input.String
		} else if input.File != nil {
			// Single file handling: read content and convert to a data URL
			dataURL, err := fileToDataURL(*input.File)
			if err != nil {
				return keyVals, err
			}
			keyVals[key] = dataURL
		} else if input.Array != nil {
			// Handle array, potentially containing file paths
			var dataURLs []string
			for _, elem := range *input.Array {
				if str, ok := elem.(string); ok && strings.HasPrefix(str, "@") {
					filePath := str[1:] // Remove '@' prefix
					dataURL, err := fileToDataURL(filePath)
					if err != nil {
						return keyVals, err
					}
					dataURLs = append(dataURLs, dataURL)
				} else if ok {
					// Directly use the string if it's not a file path
					dataURLs = append(dataURLs, str)
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
