package predict

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	Bool   *bool
}

type Inputs map[string]Input

func NewInputsForMode(keyVals map[string][]string, schema *openapi3.T, isTrain bool) (Inputs, error) {
	schemaKey := "Input"
	if isTrain {
		schemaKey = "TrainingInput"
	}
	var inputComponent *openapi3.SchemaRef
	if schema != nil && schema.Components != nil {
		inputComponent = schema.Components.Schemas[schemaKey]
		// Fallback: if TrainingInput not found, try Input (legacy schemas)
		if inputComponent == nil && isTrain {
			inputComponent = schema.Components.Schemas["Input"]
		}
	}

	input := Inputs{}
	for key, vals := range keyVals {
		// Resolve allOf/$ref to find the actual type. cog-schema-gen emits
		// allOf:[{$ref: ...}] for choices/enums, where the referenced schema
		// has the concrete type.
		originalSchema := lookupPropertySchema(inputComponent, key)

		if len(vals) == 1 {
			input[key] = newSingleInput(vals[0], originalSchema)
			continue
		}

		if len(vals) > 1 {
			// Repeated `-i name=v` flags form an array; coerce each element to
			// the array's item type so numeric/boolean arrays pass validation
			// and reach the runtime with the intended types.
			itemSchema := arrayItemSchema(originalSchema)
			anyVals := make([]any, len(vals))
			for i, v := range vals {
				anyVals[i] = coerceScalarValue(v, itemSchema)
			}
			input[key] = Input{Array: &anyVals}
		}
	}
	return input, nil
}

func newSingleInput(value string, schema *openapi3.Schema) Input {
	if strings.HasPrefix(value, "@") {
		file := value[1:]
		return Input{File: &file}
	}
	if schema == nil {
		return Input{String: &value}
	}

	resolved := resolveSchemaType(schema)
	switch {
	case resolved.Type != nil && resolved.Type.Is("object"):
		raw := json.RawMessage(value)
		return Input{Json: &raw}
	case resolved.Type != nil && resolved.Type.Is("array"):
		return newSingleArrayInput(value, schema)
	default:
		return newScalarInput(value, schema)
	}
}

func newSingleArrayInput(value string, schema *openapi3.Schema) Input {
	var array []any
	if err := json.Unmarshal([]byte(value), &array); err == nil && array != nil {
		raw := json.RawMessage(value)
		return Input{Json: &raw}
	}
	if schemaAcceptsString(schema) && scalarCandidateIsValid(value, schema) {
		return Input{String: &value}
	}

	array = []any{coerceScalarValue(value, arrayItemSchema(schema))}
	if schemaAcceptsString(schema) && !scalarCandidateIsValid(array, schema) {
		return Input{String: &value}
	}
	return Input{Array: &array}
}

func newScalarInput(value string, schema *openapi3.Schema) Input {
	switch coerced := coerceScalarValue(value, schema).(type) {
	case bool:
		return Input{Bool: &coerced}
	case int32:
		return Input{Int: &coerced}
	case float32:
		return Input{Float: &coerced}
	default:
		return Input{String: &value}
	}
}

func (inputs Inputs) toMap() (map[string]any, error) {
	keyVals, _, err := inputs.materialize()
	return keyVals, err
}

func (inputs Inputs) materialize() (map[string]any, Inputs, error) {
	keyVals := map[string]any{}
	materializedFiles := Inputs{}
	for key, input := range inputs {
		switch {
		case input.String != nil:
			// Directly assign the string value
			keyVals[key] = *input.String
		case input.File != nil:
			// Single file handling: read content and convert to a data URL
			dataURL, err := fileToDataURL(*input.File)
			if err != nil {
				return nil, nil, fmt.Errorf("input %q: %w", key, err)
			}
			keyVals[key] = dataURL
			materializedFiles[key] = Input{String: &dataURL}
		case input.Array != nil:
			// Handle array elements, which may be file paths (strings prefixed
			// with '@') or values already coerced to their schema type.
			values := make([]any, len(*input.Array))
			hasFile := false
			for i, elem := range *input.Array {
				if str, ok := elem.(string); ok && strings.HasPrefix(str, "@") {
					dataURL, err := fileToDataURL(str[1:]) // strip '@' prefix
					if err != nil {
						return nil, nil, fmt.Errorf("input %q: %w", key, err)
					}
					values[i] = dataURL
					hasFile = true
					continue
				}
				values[i] = elem
			}
			keyVals[key] = values
			if hasFile {
				materializedFiles[key] = Input{Array: &values}
			}
		case input.Json != nil:
			keyVals[key] = *input.Json
		case input.Float != nil:
			keyVals[key] = *input.Float
		case input.Int != nil:
			keyVals[key] = *input.Int
		case input.Bool != nil:
			keyVals[key] = *input.Bool
		}
	}
	return keyVals, materializedFiles, nil
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

// schemaAcceptsString reports whether the schema accepts a string value,
// including union (anyOf) members. This lets CLI `-i` parsing fall back to a
// string member when a numeric parse fails for unions such as `float | str`,
// where resolveSchemaType resolves to the numeric member.
func schemaAcceptsString(s *openapi3.Schema) bool {
	return schemaAcceptsTypes(s, "string")
}

// schemaAcceptsFloat reports whether the schema accepts a floating-point
// value, including union (anyOf) members. Unlike schemaAcceptsNumber, it does
// not match integer-only members, so CLI `-i` parsing can decide whether a
// fractional value like `1.5` is valid for unions such as `int | float`
// (accepts float) versus `str | int` (does not).
func schemaAcceptsFloat(s *openapi3.Schema) bool {
	return schemaAcceptsTypes(s, "number")
}

// schemaAcceptsNumber reports whether the schema accepts a numeric value,
// including union (anyOf) members. This lets CLI `-i` parsing coerce
// numeric-looking strings for union inputs such as `str | float`, where
// resolveSchemaType resolves to a non-numeric member.
func schemaAcceptsNumber(s *openapi3.Schema) bool {
	return schemaAcceptsTypes(s, "number", "integer")
}

// schemaAcceptsBool reports whether the schema accepts a boolean value,
// including composed schema members.
func schemaAcceptsBool(s *openapi3.Schema) bool {
	return schemaAcceptsTypes(s, "boolean")
}

func schemaAcceptsTypes(schema *openapi3.Schema, types ...string) bool {
	if schema == nil {
		return false
	}
	for _, schemaType := range types {
		if schema.Type != nil && schema.Type.Is(schemaType) {
			return true
		}
	}
	for _, refs := range []openapi3.SchemaRefs{schema.AnyOf, schema.AllOf, schema.OneOf} {
		for _, ref := range refs {
			if ref.Value != nil && schemaAcceptsTypes(ref.Value, types...) {
				return true
			}
		}
	}
	return false
}

func parseJSONBool(value string) (bool, bool) {
	switch value {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

// lookupPropertySchema returns the schema for the named input property, or nil
// when the component is unknown or does not declare that property.
func lookupPropertySchema(component *openapi3.SchemaRef, key string) *openapi3.Schema {
	if component == nil || component.Value == nil {
		return nil
	}
	property := component.Value.Properties[key]
	if property == nil {
		return nil
	}
	return property.Value
}

// arrayItemSchema returns the item schema of an array property, or nil when the
// schema is not an array or does not declare items.
func arrayItemSchema(schema *openapi3.Schema) *openapi3.Schema {
	if schema == nil {
		return nil
	}
	if schema.Items != nil && schema.Items.Value != nil {
		return schema.Items.Value
	}
	itemSchemas := append(arrayItemSchemas(schema.AnyOf), arrayItemSchemas(schema.OneOf)...)
	if len(itemSchemas) > 0 {
		return &openapi3.Schema{AnyOf: itemSchemas}
	}
	for _, ref := range schema.AllOf {
		if ref.Value != nil {
			if item := arrayItemSchema(ref.Value); item != nil {
				return item
			}
		}
	}
	return nil
}

func arrayItemSchemas(refs openapi3.SchemaRefs) openapi3.SchemaRefs {
	items := make(openapi3.SchemaRefs, 0, len(refs))
	for _, ref := range refs {
		if ref.Value == nil {
			continue
		}
		if item := arrayItemSchema(ref.Value); item != nil {
			items = append(items, &openapi3.SchemaRef{Value: item})
		}
	}
	return items
}

// coerceScalarValue converts a raw CLI string to the scalar type described by
// schema (integer, number, or boolean). It returns the original string when
// the type is unknown or the value does not parse, leaving type mismatches for
// validation to report.
func coerceScalarValue(val string, schema *openapi3.Schema) any {
	if schema == nil {
		return val
	}
	if b, ok := parseJSONBool(val); ok && schemaAcceptsBool(schema) {
		if shouldUseScalarCandidate(b, schema) {
			return b
		}
	}
	if !schemaAcceptsNumber(schema) {
		return val
	}
	if n, err := strconv.ParseInt(val, 10, 32); err == nil {
		candidate := int32(n)
		if shouldUseScalarCandidate(candidate, schema) {
			return candidate
		}
	}
	if !schemaAcceptsFloat(schema) {
		return val
	}
	if f, err := strconv.ParseFloat(val, 32); err == nil {
		candidate := float32(f)
		if shouldUseScalarCandidate(candidate, schema) {
			return candidate
		}
	}
	return val
}

func shouldUseScalarCandidate(value any, schema *openapi3.Schema) bool {
	return !schemaAcceptsString(schema) || scalarCandidateIsValid(value, schema)
}

func scalarCandidateIsValid(value any, schema *openapi3.Schema) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	var normalized any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return false
	}
	return schema.VisitJSON(normalized, openapi3.VisitAsRequest()) == nil
}

// resolveSchemaType walks through allOf/anyOf/$ref wrappers to find a schema
// that has a concrete Type set. This is needed because the static schema gen
// emits allOf:[{$ref: "#/components/schemas/Foo"}] for enum/choices fields,
// where the referenced schema carries the type (e.g. "integer") but the wrapper does not.
func resolveSchemaType(s *openapi3.Schema) *openapi3.Schema {
	if s.Type != nil && s.Type.Slice() != nil {
		return s
	}
	// Check allOf entries
	for _, ref := range s.AllOf {
		if ref.Value != nil && ref.Value.Type != nil && ref.Value.Type.Slice() != nil {
			return ref.Value
		}
	}
	// Check anyOf entries
	for _, ref := range s.AnyOf {
		if ref.Value != nil && ref.Value.Type != nil && ref.Value.Type.Slice() != nil {
			return ref.Value
		}
	}
	return s
}
