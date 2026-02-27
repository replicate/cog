package schema

import (
	"encoding/json"
	"maps"
	"sort"
)

// GenerateOpenAPISchema produces a complete OpenAPI 3.0.2 specification
// from a PredictorInfo. The returned bytes are compact JSON.
func GenerateOpenAPISchema(info *PredictorInfo) ([]byte, error) {
	spec := buildOpenAPISpec(info)

	// Post-processing: remove title next to $ref, fix nullable anyOf
	removeTitleNextToRef(spec)
	fixNullableAnyOf(spec)

	return json.Marshal(spec)
}

// buildOpenAPISpec constructs the full OpenAPI 3.0.2 map.
func buildOpenAPISpec(info *PredictorInfo) map[string]any {
	inputSchema, enumSchemas := buildInputSchema(info)
	outputSchema := info.Output.JSONType()

	isTrain := info.Mode == ModeTrain

	var (
		endpoint     string
		requestName  string
		responseName string
		cancelEP     string
		summary      string
		description  string
		opID         string
		cancelOpID   string
		cancelParam  string
		inputKey     string
		outputKey    string
	)

	if isTrain {
		endpoint = "/trainings"
		requestName = "TrainingRequest"
		responseName = "TrainingResponse"
		cancelEP = "/trainings/{training_id}/cancel"
		summary = "Train"
		description = "Run a single training on the model"
		opID = "train_trainings_post"
		cancelOpID = "cancel_trainings__training_id__cancel_post"
		cancelParam = "training_id"
		inputKey = "TrainingInput"
		outputKey = "TrainingOutput"
	} else {
		endpoint = "/predictions"
		requestName = "PredictionRequest"
		responseName = "PredictionResponse"
		cancelEP = "/predictions/{prediction_id}/cancel"
		summary = "Predict"
		description = "Run a single prediction on the model"
		opID = "predict_predictions_post"
		cancelOpID = "cancel_predictions__prediction_id__cancel_post"
		cancelParam = "prediction_id"
		inputKey = "Input"
		outputKey = "Output"
	}

	// Build components/schemas
	components := newOrderedMapAny()

	// Input schema
	inputSchema["title"] = inputKey
	components.Set(inputKey, inputSchema)

	// Output schema
	components.Set(outputKey, outputSchema)

	// Enum schemas for choices
	for _, es := range enumSchemas {
		components.Set(es.name, es.schema)
	}

	inputRef := "#/components/schemas/" + inputKey
	outputRef := "#/components/schemas/" + outputKey

	// Request schema
	components.Set(requestName, map[string]any{
		"title": requestName,
		"type":  "object",
		"properties": map[string]any{
			"id":    map[string]any{"title": "Id", "type": "string"},
			"input": map[string]any{"$ref": inputRef},
		},
	})

	// Response schema
	components.Set(responseName, map[string]any{
		"title": responseName,
		"type":  "object",
		"properties": map[string]any{
			"input":        map[string]any{"$ref": inputRef},
			"output":       map[string]any{"$ref": outputRef},
			"id":           map[string]any{"title": "Id", "type": "string"},
			"version":      map[string]any{"title": "Version", "type": "string"},
			"created_at":   map[string]any{"title": "Created At", "type": "string", "format": "date-time"},
			"started_at":   map[string]any{"title": "Started At", "type": "string", "format": "date-time"},
			"completed_at": map[string]any{"title": "Completed At", "type": "string", "format": "date-time"},
			"status":       map[string]any{"title": "Status", "type": "string"},
			"error":        map[string]any{"title": "Error", "type": "string"},
			"logs":         map[string]any{"title": "Logs", "type": "string"},
			"metrics":      map[string]any{"title": "Metrics", "type": "object"},
		},
	})

	// Status enum
	components.Set("Status", map[string]any{
		"title":       "Status",
		"description": "An enumeration.",
		"enum":        []any{"starting", "processing", "succeeded", "canceled", "failed"},
		"type":        "string",
	})

	// Validation error schemas
	components.Set("HTTPValidationError", map[string]any{
		"title": "HTTPValidationError",
		"type":  "object",
		"properties": map[string]any{
			"detail": map[string]any{
				"title": "Detail",
				"type":  "array",
				"items": map[string]any{"$ref": "#/components/schemas/ValidationError"},
			},
		},
	})

	components.Set("ValidationError", map[string]any{
		"title":    "ValidationError",
		"required": []any{"loc", "msg", "type"},
		"type":     "object",
		"properties": map[string]any{
			"loc": map[string]any{
				"title": "Location",
				"type":  "array",
				"items": map[string]any{
					"anyOf": []any{
						map[string]any{"type": "string"},
						map[string]any{"type": "integer"},
					},
				},
			},
			"msg":  map[string]any{"title": "Message", "type": "string"},
			"type": map[string]any{"title": "Error Type", "type": "string"},
		},
	})

	requestRef := "#/components/schemas/" + requestName
	responseRef := "#/components/schemas/" + responseName

	// Build paths
	paths := newOrderedMapAny()

	// Root
	paths.Set("/", map[string]any{
		"get": map[string]any{
			"summary":     "Root",
			"operationId": "root__get",
			"responses": map[string]any{
				"200": map[string]any{
					"description": "Successful Response",
					"content":     map[string]any{"application/json": map[string]any{"schema": map[string]any{}}},
				},
			},
		},
	})

	// Health check
	paths.Set("/health-check", map[string]any{
		"get": map[string]any{
			"summary":     "Healthcheck",
			"operationId": "healthcheck_health_check_get",
			"responses": map[string]any{
				"200": map[string]any{
					"description": "Successful Response",
					"content":     map[string]any{"application/json": map[string]any{"schema": map[string]any{}}},
				},
			},
		},
	})

	// Main endpoint (predict or train)
	paths.Set(endpoint, map[string]any{
		"post": map[string]any{
			"summary":     summary,
			"description": description,
			"operationId": opID,
			"requestBody": map[string]any{
				"content": map[string]any{
					"application/json": map[string]any{
						"schema": map[string]any{"$ref": requestRef},
					},
				},
			},
			"responses": map[string]any{
				"200": map[string]any{
					"description": "Successful Response",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": responseRef},
						},
					},
				},
				"422": map[string]any{
					"description": "Validation Error",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/HTTPValidationError"},
						},
					},
				},
			},
		},
	})

	// Cancel endpoint
	paths.Set(cancelEP, map[string]any{
		"post": map[string]any{
			"summary":     "Cancel",
			"operationId": cancelOpID,
			"parameters": []any{
				map[string]any{
					"required": true,
					"schema":   map[string]any{"title": TitleCaseSingle(cancelParam), "type": "string"},
					"name":     cancelParam,
					"in":       "path",
				},
			},
			"responses": map[string]any{
				"200": map[string]any{
					"description": "Successful Response",
					"content":     map[string]any{"application/json": map[string]any{"schema": map[string]any{}}},
				},
				"422": map[string]any{
					"description": "Validation Error",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/HTTPValidationError"},
						},
					},
				},
			},
		},
	})

	return map[string]any{
		"openapi": "3.0.2",
		"info":    map[string]any{"title": "Cog", "version": "0.1.0"},
		"paths":   paths,
		"components": map[string]any{
			"schemas": components,
		},
	}
}

// enumSchema pairs a name with its schema for choices fields.
type enumSchema struct {
	name   string
	schema map[string]any
}

// buildInputSchema builds the Input schema object and any enum schemas for choices.
func buildInputSchema(info *PredictorInfo) (map[string]any, []enumSchema) {
	properties := newOrderedMapAny()
	var required []string
	var enums []enumSchema

	info.Inputs.Entries(func(name string, field InputField) {
		prop := newOrderedMapAny()

		// x-order for field ordering
		prop.Set("x-order", field.Order)

		if len(field.Choices) > 0 {
			// Choices -> use allOf with $ref to enum schema
			enumName := TitleCaseSingle(name)
			enumType := field.FieldType.Primitive.JSONType()
			typeStr, _ := enumType["type"].(string)
			if typeStr == "" {
				typeStr = "string"
			}

			choiceValues := make([]any, len(field.Choices))
			for i, c := range field.Choices {
				choiceValues[i] = c.ToJSON()
			}

			enums = append(enums, enumSchema{
				name: enumName,
				schema: map[string]any{
					"title":       enumName,
					"description": "An enumeration.",
					"enum":        choiceValues,
					"type":        typeStr,
				},
			})

			prop.Set("allOf", []any{
				map[string]any{"$ref": "#/components/schemas/" + enumName},
			})
		} else {
			// Regular field — inline type
			prop.Set("title", TitleCase(name))
			typeSchema := field.FieldType.JSONType()
			// Merge type schema keys into prop in sorted order for determinism
			keys := make([]string, 0, len(typeSchema))
			for k := range typeSchema {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				prop.Set(k, typeSchema[k])
			}
		}

		// Required?
		if field.IsRequired() {
			required = append(required, name)
		}

		// Default value
		if field.Default != nil {
			prop.Set("default", field.Default.ToJSON())
		}

		// Nullable
		if field.FieldType.Repetition == Optional {
			prop.Set("nullable", true)
		}

		// Description
		if field.Description != nil {
			prop.Set("description", *field.Description)
		}

		// Numeric constraints
		if field.GE != nil {
			prop.Set("minimum", *field.GE)
		}
		if field.LE != nil {
			prop.Set("maximum", *field.LE)
		}

		// String constraints
		if field.MinLength != nil {
			prop.Set("minLength", *field.MinLength)
		}
		if field.MaxLength != nil {
			prop.Set("maxLength", *field.MaxLength)
		}
		if field.Regex != nil {
			prop.Set("pattern", *field.Regex)
		}

		// Deprecated
		if field.Deprecated != nil && *field.Deprecated {
			prop.Set("deprecated", true)
		}

		properties.Set(name, prop)
	})

	inputSchema := map[string]any{
		"title":      "Input",
		"type":       "object",
		"properties": properties,
	}

	if len(required) > 0 {
		inputSchema["required"] = required
	}

	return inputSchema, enums
}

// ---------------------------------------------------------------------------
// Post-processing (mirrors openapi_schema.py fixups)
// ---------------------------------------------------------------------------

// removeTitleNextToRef removes "title" from any map that also has "$ref".
// OpenAPI 3.0 doesn't allow sibling keywords next to $ref.
func removeTitleNextToRef(v any) {
	switch val := v.(type) {
	case map[string]any:
		if _, hasRef := val["$ref"]; hasRef {
			delete(val, "title")
		}
		for _, child := range val {
			removeTitleNextToRef(child)
		}
	case *orderedMapAny:
		if _, hasRef := val.Get("$ref"); hasRef {
			val.Delete("title")
		}
		val.Entries(func(_ string, child any) {
			removeTitleNextToRef(child)
		})
	case []any:
		for _, child := range val {
			removeTitleNextToRef(child)
		}
	}
}

// fixNullableAnyOf converts {"anyOf": [{"type": T}, {"type": "null"}]} to
// {"type": T, "nullable": true}. OpenAPI 3.0 uses nullable instead of union-with-null.
func fixNullableAnyOf(v any) {
	switch val := v.(type) {
	case map[string]any:
		// Recurse first
		for _, child := range val {
			fixNullableAnyOf(child)
		}
		// Check for anyOf with null pattern
		anyOf, ok := val["anyOf"].([]any)
		if !ok || len(anyOf) != 2 {
			return
		}
		var nonNull map[string]any
		hasNull := false
		for _, variant := range anyOf {
			m, ok := variant.(map[string]any)
			if !ok {
				return
			}
			if t, _ := m["type"].(string); t == "null" {
				hasNull = true
			} else {
				nonNull = m
			}
		}
		if hasNull && nonNull != nil {
			delete(val, "anyOf")
			maps.Copy(val, nonNull)
			val["nullable"] = true
		}
	case *orderedMapAny:
		// Recurse first
		val.Entries(func(_ string, child any) {
			fixNullableAnyOf(child)
		})
		// Check for anyOf with null pattern
		anyOfRaw, ok := val.Get("anyOf")
		if !ok {
			return
		}
		anyOf, ok := anyOfRaw.([]any)
		if !ok || len(anyOf) != 2 {
			return
		}
		var nonNull map[string]any
		hasNull := false
		for _, variant := range anyOf {
			m, ok := variant.(map[string]any)
			if !ok {
				return
			}
			if t, _ := m["type"].(string); t == "null" {
				hasNull = true
			} else {
				nonNull = m
			}
		}
		if hasNull && nonNull != nil {
			val.Delete("anyOf")
			for k, v := range nonNull {
				val.Set(k, v)
			}
			val.Set("nullable", true)
		}
	case []any:
		for _, child := range val {
			fixNullableAnyOf(child)
		}
	}
}

// ---------------------------------------------------------------------------
// orderedMapAny — ordered map with JSON marshaling that preserves key order.
// Used for schema properties where field ordering matters.
// ---------------------------------------------------------------------------

type orderedMapAny struct {
	keys   []string
	values map[string]any
}

func newOrderedMapAny() *orderedMapAny {
	return &orderedMapAny{values: make(map[string]any)}
}

func (m *orderedMapAny) Set(key string, value any) {
	if _, exists := m.values[key]; !exists {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

func (m *orderedMapAny) Get(key string) (any, bool) {
	v, ok := m.values[key]
	return v, ok
}

func (m *orderedMapAny) Delete(key string) {
	if _, exists := m.values[key]; !exists {
		return
	}
	delete(m.values, key)
	for i, k := range m.keys {
		if k == key {
			m.keys = append(m.keys[:i], m.keys[i+1:]...)
			break
		}
	}
}

func (m *orderedMapAny) Entries(fn func(key string, value any)) {
	for _, k := range m.keys {
		fn(k, m.values[k])
	}
}

// MarshalJSON produces a JSON object with keys in insertion order.
func (m *orderedMapAny) MarshalJSON() ([]byte, error) {
	buf := []byte{'{'}
	for i, k := range m.keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		keyBytes, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf = append(buf, keyBytes...)
		buf = append(buf, ':')
		valBytes, err := json.Marshal(m.values[k])
		if err != nil {
			return nil, err
		}
		buf = append(buf, valBytes...)
	}
	buf = append(buf, '}')
	return buf, nil
}
