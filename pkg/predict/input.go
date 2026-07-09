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
	Bool   *bool
}

type Inputs map[string]Input

func NewInputsForMode(keyVals map[string][]string, schema *openapi3.T, isTrain bool) (Inputs, error) {
	schemaKey := "Input"
	if isTrain {
		schemaKey = "TrainingInput"
	}
	var inputComponent *openapi3.SchemaRef
	for name, component := range schema.Components.Schemas {
		if name == schemaKey {
			inputComponent = component
			break
		}
	}
	// Fallback: if TrainingInput not found, try Input (legacy schemas)
	if inputComponent == nil && isTrain {
		for name, component := range schema.Components.Schemas {
			if name == "Input" {
				inputComponent = component
				break
			}
		}
	}

	input := Inputs{}
	for key, vals := range keyVals {
		// Resolve allOf/$ref to find the actual type. cog-schema-gen emits
		// allOf:[{$ref: ...}] for choices/enums, where the referenced schema
		// has the concrete type.
		originalSchema := lookupPropertySchema(inputComponent, key)

		if len(vals) == 1 {
			val := vals[0]
			if strings.HasPrefix(val, "@") {
				file := val[1:]
				input[key] = Input{File: &file}
				continue
			}
			// Coerce the value to its schema type when we know it, so the
			// runtime (and preflight validation) receive the intended type.
			if originalSchema != nil {
				propertySchema := resolveSchemaType(originalSchema)
				switch {
				case propertySchema.Type.Is("object"):
					encodedVal := json.RawMessage(val)
					input[key] = Input{Json: &encodedVal}
					continue
				case propertySchema.Type.Is("array"):
					var parsed any
					if err := json.Unmarshal([]byte(val), &parsed); err == nil {
						t := reflect.TypeOf(parsed)
						if t != nil && (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) {
							encodedVal := json.RawMessage(val)
							input[key] = Input{Json: &encodedVal}
							continue
						}
					}
					if schemaAcceptsString(originalSchema) && scalarCandidateIsValid(val, originalSchema) {
						break
					}
					// A single repeated-style value (e.g. `-i nums=1`) becomes a
					// one-element array; coerce it to the array's item type.
					arr := []any{coerceScalarValue(val, arrayItemSchema(originalSchema))}
					if !scalarCandidateIsValid(arr, originalSchema) && schemaAcceptsString(originalSchema) {
						break
					}
					input[key] = Input{Array: &arr}
					continue
				}
				coerced := coerceScalarValue(val, originalSchema)
				switch value := coerced.(type) {
				case bool:
					input[key] = Input{Bool: &value}
					continue
				case int32:
					input[key] = Input{Int: &value}
					continue
				case float32:
					input[key] = Input{Float: &value}
					continue
				}
			}
			input[key] = Input{String: &val}
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
				return keyVals, fmt.Errorf("input %q: %w", key, err)
			}
			keyVals[key] = dataURL
		case input.Array != nil:
			// Handle array elements, which may be file paths (strings prefixed
			// with '@') or values already coerced to their schema type.
			values := make([]any, len(*input.Array))
			for i, elem := range *input.Array {
				if str, ok := elem.(string); ok && strings.HasPrefix(str, "@") {
					dataURL, err := fileToDataURL(str[1:]) // strip '@' prefix
					if err != nil {
						return keyVals, fmt.Errorf("input %q: %w", key, err)
					}
					values[i] = dataURL
					continue
				}
				values[i] = elem
			}
			keyVals[key] = values
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

// schemaAcceptsString reports whether the schema accepts a string value,
// including union (anyOf) members. This lets CLI `-i` parsing fall back to a
// string member when a numeric parse fails for unions such as `float | str`,
// where resolveSchemaType resolves to the numeric member.
func schemaAcceptsString(s *openapi3.Schema) bool {
	if s == nil {
		return false
	}
	if s.Type != nil && s.Type.Is("string") {
		return true
	}
	for _, ref := range s.AnyOf {
		if ref.Value != nil && schemaAcceptsString(ref.Value) {
			return true
		}
	}
	for _, ref := range s.AllOf {
		if ref.Value != nil && schemaAcceptsString(ref.Value) {
			return true
		}
	}
	for _, ref := range s.OneOf {
		if ref.Value != nil && schemaAcceptsString(ref.Value) {
			return true
		}
	}
	return false
}

// schemaAcceptsFloat reports whether the schema accepts a floating-point
// value, including union (anyOf) members. Unlike schemaAcceptsNumber, it does
// not match integer-only members, so CLI `-i` parsing can decide whether a
// fractional value like `1.5` is valid for unions such as `int | float`
// (accepts float) versus `str | int` (does not).
func schemaAcceptsFloat(s *openapi3.Schema) bool {
	if s == nil {
		return false
	}
	if s.Type != nil && s.Type.Is("number") {
		return true
	}
	for _, ref := range s.AnyOf {
		if ref.Value != nil && schemaAcceptsFloat(ref.Value) {
			return true
		}
	}
	for _, ref := range s.AllOf {
		if ref.Value != nil && schemaAcceptsFloat(ref.Value) {
			return true
		}
	}
	for _, ref := range s.OneOf {
		if ref.Value != nil && schemaAcceptsFloat(ref.Value) {
			return true
		}
	}
	return false
}

// schemaAcceptsNumber reports whether the schema accepts a numeric value,
// including union (anyOf) members. This lets CLI `-i` parsing coerce
// numeric-looking strings for union inputs such as `str | float`, where
// resolveSchemaType resolves to a non-numeric member.
func schemaAcceptsNumber(s *openapi3.Schema) bool {
	if s == nil {
		return false
	}
	if s.Type != nil && (s.Type.Is("number") || s.Type.Is("integer")) {
		return true
	}
	for _, ref := range s.AnyOf {
		if ref.Value != nil && schemaAcceptsNumber(ref.Value) {
			return true
		}
	}
	for _, ref := range s.AllOf {
		if ref.Value != nil && schemaAcceptsNumber(ref.Value) {
			return true
		}
	}
	for _, ref := range s.OneOf {
		if ref.Value != nil && schemaAcceptsNumber(ref.Value) {
			return true
		}
	}
	return false
}

// schemaAcceptsBool reports whether the schema accepts a boolean value,
// including union (anyOf) members.
func schemaAcceptsBool(s *openapi3.Schema) bool {
	if s == nil {
		return false
	}
	if s.Type != nil && s.Type.Is("boolean") {
		return true
	}
	for _, ref := range s.AnyOf {
		if ref.Value != nil && schemaAcceptsBool(ref.Value) {
			return true
		}
	}
	for _, ref := range s.AllOf {
		if ref.Value != nil && schemaAcceptsBool(ref.Value) {
			return true
		}
	}
	for _, ref := range s.OneOf {
		if ref.Value != nil && schemaAcceptsBool(ref.Value) {
			return true
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
	var itemSchemas openapi3.SchemaRefs
	for _, refs := range []openapi3.SchemaRefs{schema.AnyOf, schema.OneOf} {
		for _, ref := range refs {
			if ref.Value != nil {
				if item := arrayItemSchema(ref.Value); item != nil {
					itemSchemas = append(itemSchemas, &openapi3.SchemaRef{Value: item})
				}
			}
		}
	}
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

// coerceScalarValue converts a raw CLI string to the scalar type described by
// schema (integer, number, or boolean). It returns the original string when
// the type is unknown or the value does not parse, leaving type mismatches for
// validation to report.
func coerceScalarValue(val string, schema *openapi3.Schema) any {
	if schema == nil {
		return val
	}
	if b, ok := parseJSONBool(val); ok && schemaAcceptsBool(schema) {
		if scalarCandidateIsValid(b, schema) || !schemaAcceptsString(schema) {
			return b
		}
	}
	if schemaAcceptsNumber(schema) {
		if n, err := strconv.ParseInt(val, 10, 32); err == nil {
			candidate := int32(n)
			if scalarCandidateIsValid(candidate, schema) || !schemaAcceptsString(schema) {
				return candidate
			}
		}
		if schemaAcceptsFloat(schema) {
			if f, err := strconv.ParseFloat(val, 32); err == nil {
				candidate := float32(f)
				if scalarCandidateIsValid(candidate, schema) || !schemaAcceptsString(schema) {
					return candidate
				}
			}
		}
	}
	return val
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
