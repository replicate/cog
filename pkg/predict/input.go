package predict

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/mitchellh/go-homedir"
	"github.com/vincent-petithory/dataurl"

	"github.com/replicate/cog/pkg/util/mime"
)

type Input struct {
	String *string
	File   *string
	Array  *[]any
	Json   *json.RawMessage
	Float  *float32
	Int    *int32
}

type Inputs map[string]Input

func NewInputs(keyVals map[string][]string, schema *openapi3.T) (Inputs, error) {
	var inputComponent *openapi3.SchemaRef
	for name, component := range schema.Components.Schemas {
		if name == "Input" {
			inputComponent = component
			break
		}
	}

	input := Inputs{}
	for key, vals := range keyVals {
		if len(vals) == 1 {
			val := vals[0]
			if strings.HasPrefix(val, "@") {
				val = val[1:]
				input[key] = Input{File: &val}
			} else {
				// Check if we should explicitly parse the JSON based on a known schema
				if inputComponent != nil {
					properties, err := inputComponent.JSONLookup("properties")
					if err != nil {
						return input, err
					}
					propertiesSchemas := properties.(openapi3.Schemas)
					property, err := propertiesSchemas.JSONLookup(key)
					if err == nil {
						propertySchema := property.(*openapi3.Schema)
						switch {
						case propertySchema.Type.Is("object"):
							encodedVal := json.RawMessage(val)
							input[key] = Input{Json: &encodedVal}
							continue
						case propertySchema.Type.Is("array"):
							var parsed any
							err := json.Unmarshal([]byte(val), &parsed)
							if err == nil {
								t := reflect.TypeOf(parsed)
								if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
									encodedVal := json.RawMessage(val)
									input[key] = Input{Json: &encodedVal}
									continue
								}
							}
							var arr = []any{val}
							input[key] = Input{Array: &arr}
							continue
						case propertySchema.Type.Is("number"):
							value, err := strconv.ParseInt(val, 10, 32)
							if err == nil {
								valueInt := int32(value)
								input[key] = Input{Int: &valueInt}
								continue
							} else {
								value, err := strconv.ParseFloat(val, 32)
								if err != nil {
									return input, err
								}
								float := float32(value)
								input[key] = Input{Float: &float}
								continue
							}
						case propertySchema.Type.Is("integer"):
							value, err := strconv.ParseInt(val, 10, 32)
							if err != nil {
								return input, err
							}
							valueInt := int32(value)
							input[key] = Input{Int: &valueInt}
							continue
						}
					}
				}
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
	return input, nil
}

func NewInputsWithBaseDir(keyVals map[string]string, baseDir string) Inputs {
	input := Inputs{}
	for key, val := range keyVals {
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
		case input.Json != nil:
			keyVals[key] = *input.Json
		case input.Float != nil:
			keyVals[key] = *input.Float
		case input.Int != nil:
			keyVals[key] = *input.Int
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
