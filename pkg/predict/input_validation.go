package predict

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// HasInputComponent reports whether the schema has the Input (or TrainingInput)
// component needed by preflight validation, so callers can fall back to the
// runtime schema for images with minimal or missing labels.
func HasInputComponent(schema *openapi3.T, isTrain bool) bool {
	component, err := inputComponentForMode(schema, isTrain)
	return err == nil && component.Properties != nil
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
	inputMap, materializedFiles, err := inputs.materialize()
	if err != nil {
		return err
	}
	normalized, err := normalizeJSONMap(inputMap)
	if err != nil {
		return err
	}
	if err := ValidateInputMapForMode(normalized, schema, isTrain); err != nil {
		return err
	}
	// Reuse the exact file contents validated here when sending the request.
	maps.Copy(inputs, materializedFiles)
	return nil
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
	if err := rejectExplicitNulls(input, component, nil); err != nil {
		return err
	}
	if err := component.VisitJSON(input, openapi3.VisitAsRequest(), openapi3.MultiErrors()); err != nil {
		return formatInputValidationError(err)
	}
	return nil
}

// rejectExplicitNulls rejects explicit nulls that the runtime would reject.
// The runtime uses strict JSON Schema and ignores OpenAPI's nullable keyword.
func rejectExplicitNulls(value any, schema *openapi3.Schema, path []string) error {
	if value == nil {
		if schemaAllowsNullWithoutNullable(schema) {
			return nil
		}
		return fmt.Errorf("invalid input %q: must not be null (omit the input to use its default)", strings.Join(path, "."))
	}
	if schema == nil {
		return nil
	}

	switch value := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			propertySchema := nestedPropertySchema(schema, key)
			if propertySchema == nil {
				propertySchema = nestedAdditionalPropertySchema(schema)
			}
			if propertySchema == nil {
				continue
			}
			if err := rejectExplicitNulls(value[key], propertySchema, append(path, key)); err != nil {
				return err
			}
		}
	case []any:
		itemSchema := arrayItemSchema(schema)
		if itemSchema == nil {
			return nil
		}
		for i, item := range value {
			if err := rejectExplicitNulls(item, itemSchema, append(path, fmt.Sprintf("%d", i))); err != nil {
				return err
			}
		}
	}
	return nil
}

// schemaAllowsNullWithoutNullable reports whether JSON null is valid under
// strict JSON Schema, deliberately ignoring OpenAPI's nullable keyword so it
// mirrors the runtime validator.
//
// Only the Type, AllOf, and AnyOf branches fire for schemas the Cog generator
// emits today (typed fields, choices via allOf, unions via anyOf). The
// enum-with-null, OneOf, Not, and Const branches are defensive: the generator
// never produces those shapes. OneOf and Not are valid OpenAPI 3.0 and would
// become live only if the Cog type system grew; Const (and null in a type
// array) is 3.1-only. Note that a real move to 3.1 would more likely pass
// openapi3.EnableJSONSchema2020() to VisitJSON and drop the nullable keyword, so
// kin-openapi matches the runtime directly -- that could replace this walker
// rather than extend it.
func schemaAllowsNullWithoutNullable(schema *openapi3.Schema) bool {
	if schema == nil {
		return true
	}
	allowed := true
	if schema.Type != nil {
		allowed = false
		for _, schemaType := range schema.Type.Slice() {
			allowed = allowed || schemaType == "null"
		}
	}
	if len(schema.Enum) > 0 {
		containsNull := false
		for _, value := range schema.Enum {
			containsNull = containsNull || value == nil
		}
		allowed = allowed && containsNull
	}
	for _, ref := range schema.AllOf {
		if ref.Value != nil {
			allowed = allowed && schemaAllowsNullWithoutNullable(ref.Value)
		}
	}
	if len(schema.AnyOf) > 0 {
		anyAllows := false
		for _, ref := range schema.AnyOf {
			if ref.Value != nil {
				anyAllows = anyAllows || schemaAllowsNullWithoutNullable(ref.Value)
			}
		}
		allowed = allowed && anyAllows
	}
	// Defensive: Cog emits unions as anyOf and choices as allOf, never oneOf,
	// not, or const. oneOf and not are valid OpenAPI 3.0; const is 3.1-only.
	// These branches matter only if the generator starts emitting them.
	if len(schema.OneOf) > 0 {
		matching := 0
		for _, ref := range schema.OneOf {
			if ref.Value != nil && schemaAllowsNullWithoutNullable(ref.Value) {
				matching++
			}
		}
		allowed = allowed && matching == 1
	}
	if schema.Not != nil && schema.Not.Value != nil {
		allowed = allowed && !schemaAllowsNullWithoutNullable(schema.Not.Value)
	}
	if schema.Const != nil {
		allowed = false
	}
	return allowed
}

// nestedPropertySchema resolves a declared object property's schema, projecting
// through allOf/anyOf/oneOf composition so nested nulls are checked against it.
//
// Defensive for inputs: Cog inputs are flat and dict/Any is an opaque
// {"type":"object"} with no declared properties, so this returns nil for
// generated schemas and nested values stay unconstrained. Typed nested
// properties are valid OpenAPI 3.0; this matters only if the Cog type system
// grows to emit them (e.g. nested models).
func nestedPropertySchema(schema *openapi3.Schema, key string) *openapi3.Schema {
	constraints := openapi3.SchemaRefs{}
	if property := declaredPropertySchema(schema, key); property != nil {
		constraints = append(constraints, &openapi3.SchemaRef{Value: property})
	}
	if properties := nestedPropertySchemas(schema.AllOf, key); len(properties) > 0 {
		constraints = append(constraints, properties...)
	}
	if properties := nestedPropertySchemas(schema.AnyOf, key); len(properties) > 0 {
		constraints = append(constraints, &openapi3.SchemaRef{Value: &openapi3.Schema{AnyOf: properties}})
	}
	if properties := nestedPropertySchemas(schema.OneOf, key); len(properties) > 0 {
		constraints = append(constraints, &openapi3.SchemaRef{Value: &openapi3.Schema{OneOf: properties}})
	}
	if len(constraints) == 0 {
		return nil
	}
	if len(constraints) == 1 {
		return constraints[0].Value
	}
	return &openapi3.Schema{AllOf: constraints}
}

func nestedPropertySchemas(refs openapi3.SchemaRefs, key string) openapi3.SchemaRefs {
	properties := make(openapi3.SchemaRefs, 0, len(refs))
	for _, ref := range refs {
		if ref.Value == nil {
			continue
		}
		property := nestedPropertySchema(ref.Value, key)
		if property == nil {
			property = &openapi3.Schema{}
		}
		properties = append(properties, &openapi3.SchemaRef{Value: property})
	}
	return properties
}

func declaredPropertySchema(schema *openapi3.Schema, key string) *openapi3.Schema {
	if property := schema.Properties[key]; property != nil && property.Value != nil {
		return property.Value
	}
	return nil
}

func additionalPropertySchema(schema *openapi3.Schema) *openapi3.Schema {
	if schema.AdditionalProperties.Schema != nil && schema.AdditionalProperties.Schema.Value != nil {
		return schema.AdditionalProperties.Schema.Value
	}
	return nil
}

// nestedAdditionalPropertySchema resolves a typed additionalProperties schema,
// projecting through composition.
//
// Defensive: Cog emits dict/Any as an opaque object with no typed
// additionalProperties, so this is unused for generated schemas today. Typed
// additionalProperties is valid OpenAPI 3.0; needed only if the Cog type system
// grows to emit typed maps.
func nestedAdditionalPropertySchema(schema *openapi3.Schema) *openapi3.Schema {
	if additional := additionalPropertySchema(schema); additional != nil {
		return additional
	}
	if schemas := nestedAdditionalPropertySchemas(schema.AllOf); len(schemas) > 0 {
		return &openapi3.Schema{AllOf: schemas}
	}
	if schemas := nestedAdditionalPropertySchemas(schema.AnyOf); len(schemas) > 0 {
		return &openapi3.Schema{AnyOf: schemas}
	}
	if schemas := nestedAdditionalPropertySchemas(schema.OneOf); len(schemas) > 0 {
		return &openapi3.Schema{OneOf: schemas}
	}
	return nil
}

func nestedAdditionalPropertySchemas(refs openapi3.SchemaRefs) openapi3.SchemaRefs {
	schemas := make(openapi3.SchemaRefs, 0, len(refs))
	for _, ref := range refs {
		if ref.Value == nil {
			continue
		}
		additional := nestedAdditionalPropertySchema(ref.Value)
		if additional == nil {
			additional = &openapi3.Schema{}
		}
		schemas = append(schemas, &openapi3.SchemaRef{Value: additional})
	}
	return schemas
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

// validateKnownInputs rejects inputs not declared by the model.
// The CLI is stricter than the runtime so typos fail before build/start.
func validateKnownInputs(input map[string]any, component *openapi3.Schema) error {
	var unknown []string
	for key := range input {
		if declaredPropertySchema(component, key) == nil {
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

	var suffix string
	if len(valid) == 0 {
		suffix = "model does not accept inputs"
	} else {
		suffix = fmt.Sprintf("valid inputs are: %s", strings.Join(valid, ", "))
	}
	if len(unknown) == 1 {
		return fmt.Errorf("unknown input %q; %s", unknown[0], suffix)
	}
	return fmt.Errorf("unknown inputs %s; %s", quoteJoin(unknown), suffix)
}

// quoteJoin renders keys as a comma-separated list of %q-quoted values.
func quoteJoin(keys []string) string {
	quoted := make([]string, len(keys))
	for i, key := range keys {
		quoted[i] = fmt.Sprintf("%q", key)
	}
	return strings.Join(quoted, ", ")
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
		path := schemaErr.JSONPointer()
		if missingInput, ok := missingRequiredInput(schemaErr); ok {
			if len(path) > 0 {
				missingInput = strings.Join(path, ".")
			}
			return fmt.Sprintf("missing required input %q", missingInput)
		}
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

// enumValues finds allowed enum values, even when the enum error is wrapped.
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

// missingRequiredInput extracts the property name from a required-field error.
func missingRequiredInput(err *openapi3.SchemaError) (string, bool) {
	if err.SchemaField != "required" {
		return "", false
	}
	reason := err.Reason
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
