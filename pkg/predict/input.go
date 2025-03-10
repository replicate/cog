package predict

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/mitchellh/go-homedir"
	"github.com/vincent-petithory/dataurl"

	"github.com/replicate/cog/pkg/util/mime"
)

type Input struct {
	String      *string
	File        *string
	Array       *[]any
	ChatMessage *json.RawMessage
}

type Inputs map[string]Input

var jsonSerializableSchemas = map[string]bool{
	"#/components/schemas/CommonChatSchemaDeveloperMessage": true,
	"#/components/schemas/CommonChatSchemaSystemMessage":    true,
	"#/components/schemas/CommonChatSchemaUserMessage":      true,
	"#/components/schemas/CommonChatSchemaAssistantMessage": true,
	"#/components/schemas/CommonChatSchemaToolMessage":      true,
	"#/components/schemas/CommonChatSchemaFunctionMessage":  true,
}

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

			// Check if we should explicitly parse the JSON based on a known schema
			if inputComponent != nil {
				properties, err := inputComponent.JSONLookup("properties")
				if err != nil {
					return input, err
				}
				propertiesSchemas := properties.(openapi3.Schemas)
				messages, err := propertiesSchemas.JSONLookup("messages")
				if err != nil {
					return input, err
				}
				messagesSchemas := messages.(*openapi3.Schema)
				found := false
				for _, schemaRef := range messagesSchemas.Items.Value.AnyOf {
					if _, ok := jsonSerializableSchemas[schemaRef.Ref]; ok {
						found = true
						message := json.RawMessage(val)
						input[key] = Input{ChatMessage: &message}
						break
					}
				}
				if found {
					continue
				}
			}

			switch {
			case strings.HasPrefix(val, "@"):
				val = val[1:]
				input[key] = Input{File: &val}

			default:
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
		case input.ChatMessage != nil:
			keyVals[key] = *input.ChatMessage
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
