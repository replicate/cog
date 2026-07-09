package predict

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// HasInputComponent reports whether the schema carries the Input (or, for
// training, TrainingInput) component this validator needs. Existing images may
// ship a minimal or malformed-but-parseable OpenAPI schema label; when the
// component is absent the caller should fall back to the runtime schema
// instead of failing preflight validation.
func HasInputComponent(schema *openapi3.T, isTrain bool) bool {
	_, err := inputComponentForMode(schema, isTrain)
	return err == nil
}

// ValidateInputsForMode validates CLI inputs after schema-directed coercion.
func ValidateInputsForMode(inputs Inputs, schema *openapi3.T, isTrain bool) error {
	component, err := inputComponentForMode(schema, isTrain)
	if err != nil {
		return err
	}
	if err := validateKnownInputNames(inputs, component); err != nil {
		return err
	}
	inputMap, err := inputs.toMap()
	if err != nil {
		return err
	}
	normalized, err := normalizeJSONMap(inputMap)
	if err != nil {
		return err
	}
	return ValidateInputMapForMode(normalized, schema, isTrain)
}

// ValidateInputMapForMode validates an already JSON-shaped input map.
func ValidateInputMapForMode(input map[string]any, schema *openapi3.T, isTrain bool) error {
	component, err := inputComponentForMode(schema, isTrain)
	if err != nil {
		return err
	}
	if err := ValidateInputNamesForMode(input, schema, isTrain); err != nil {
		return err
	}
	if err := component.VisitJSON(input, openapi3.VisitAsRequest(), openapi3.MultiErrors()); err != nil {
		return formatInputValidationError(err)
	}
	return nil
}

// ValidateInputNamesForMode validates top-level input names without reading or converting values.
func ValidateInputNamesForMode(input map[string]any, schema *openapi3.T, isTrain bool) error {
	component, err := inputComponentForMode(schema, isTrain)
	if err != nil {
		return err
	}
	return validateKnownInputs(input, component)
}

func inputComponentForMode(schema *openapi3.T, isTrain bool) (*openapi3.Schema, error) {
	if schema == nil {
		return nil, fmt.Errorf("OpenAPI schema is required to validate inputs")
	}
	if schema.Components == nil {
		return nil, fmt.Errorf("OpenAPI schema is missing components")
	}
	if isTrain {
		if component := schema.Components.Schemas["TrainingInput"]; component != nil && component.Value != nil {
			return component.Value, nil
		}
	}
	component := schema.Components.Schemas["Input"]
	if component == nil || component.Value == nil {
		if isTrain {
			return nil, fmt.Errorf("OpenAPI schema is missing TrainingInput or Input component")
		}
		return nil, fmt.Errorf("OpenAPI schema is missing Input component")
	}
	return component.Value, nil
}

func validateKnownInputNames(inputs Inputs, component *openapi3.Schema) error {
	inputMap := make(map[string]any, len(inputs))
	for key := range inputs {
		inputMap[key] = nil
	}
	return validateKnownInputs(inputMap, component)
}

func validateKnownInputs(input map[string]any, component *openapi3.Schema) error {
	var unknown []string
	for key := range input {
		if _, ok := component.Properties[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	valid := make([]string, 0, len(component.Properties))
	for key := range component.Properties {
		valid = append(valid, key)
	}
	sort.Strings(valid)
	if len(valid) == 0 {
		if len(unknown) == 1 {
			return fmt.Errorf("unknown input %q; model does not accept inputs", unknown[0])
		}
		quoted := make([]string, len(unknown))
		for i, key := range unknown {
			quoted[i] = fmt.Sprintf("%q", key)
		}
		return fmt.Errorf("unknown inputs %s; model does not accept inputs", strings.Join(quoted, ", "))
	}
	if len(unknown) == 1 {
		return fmt.Errorf("unknown input %q; valid inputs are: %s", unknown[0], strings.Join(valid, ", "))
	}
	quoted := make([]string, len(unknown))
	for i, key := range unknown {
		quoted[i] = fmt.Sprintf("%q", key)
	}
	return fmt.Errorf("unknown inputs %s; valid inputs are: %s", strings.Join(quoted, ", "), strings.Join(valid, ", "))
}

func normalizeJSONMap(input map[string]any) (map[string]any, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to encode inputs for validation: %w", err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, fmt.Errorf("failed to decode inputs for validation: %w", err)
	}
	return normalized, nil
}

func formatInputValidationError(err error) error {
	var multi openapi3.MultiError
	if errors.As(err, &multi) {
		messages := make([]string, 0, len(multi))
		for _, validationErr := range multi {
			messages = append(messages, formatSchemaValidationError(validationErr))
		}
		if len(messages) == 1 {
			return errors.New(messages[0])
		}
		return fmt.Errorf("input validation failed:\n- %s", strings.Join(messages, "\n- "))
	}
	return errors.New(formatSchemaValidationError(err))
}

func formatSchemaValidationError(err error) string {
	var schemaErr *openapi3.SchemaError
	if errors.As(err, &schemaErr) {
		reason := schemaErr.Reason
		if reason == "" {
			reason = fmt.Sprintf("doesn't match schema %q", schemaErr.SchemaField)
		}
		if missingInput, ok := missingRequiredInput(reason); ok {
			return fmt.Sprintf("missing required input %q", missingInput)
		}
		path := schemaErr.JSONPointer()
		if len(path) > 0 {
			inputName := strings.Join(path, ".")
			if typeErr, ok := formatTypeValidationError(schemaErr); ok {
				return fmt.Sprintf("invalid input %q: %s", inputName, typeErr)
			}
			if allowed, ok := enumValues(schemaErr); ok {
				return fmt.Sprintf("invalid input %q: must be one of: %s", inputName, formatEnumValues(allowed))
			}
			return fmt.Sprintf("invalid input %q: %s", inputName, reason)
		}
		return reason
	}
	return err.Error()
}

// enumValues walks the error and its Origin chain looking for an enum
// validation failure, returning the allowed values. This surfaces the useful
// values even when the failure is wrapped (e.g. an enum referenced via allOf).
func enumValues(err error) ([]any, bool) {
	for err != nil {
		var schemaErr *openapi3.SchemaError
		if errors.As(err, &schemaErr) {
			if schemaErr.SchemaField == "enum" && schemaErr.Schema != nil && len(schemaErr.Schema.Enum) > 0 {
				return schemaErr.Schema.Enum, true
			}
			err = schemaErr.Origin
			continue
		}
		err = errors.Unwrap(err)
	}
	return nil, false
}

func formatEnumValues(values []any) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprintf("%v", v)
	}
	return strings.Join(parts, ", ")
}

func missingRequiredInput(reason string) (string, bool) {
	if !strings.HasPrefix(reason, "property \"") || !strings.HasSuffix(reason, "\" is missing") {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimPrefix(reason, "property \""), "\" is missing"), true
}

func formatTypeValidationError(err *openapi3.SchemaError) (string, bool) {
	if err.Schema == nil || err.Schema.Type == nil || !strings.HasPrefix(err.Reason, "value must be ") {
		return "", false
	}
	return fmt.Sprintf("expected %s, got %s", formatExpectedTypes(err.Schema.Type.Slice()), jsonTypeName(err.Value)), true
}

func formatExpectedTypes(types []string) string {
	if len(types) == 0 {
		return "valid JSON value"
	}
	if len(types) == 1 {
		return types[0]
	}
	return strings.Join(types[:len(types)-1], ", ") + " or " + types[len(types)-1]
}

func jsonTypeName(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", value)
	}
}
