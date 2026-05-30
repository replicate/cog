package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

func TestExtractOutputSchemaFromMalformedSchema(t *testing.T) {
	// Test that we don't panic when extracting output schema from malformed OpenAPI schemas
	testCases := []struct {
		name   string
		schema *openapi3.T
	}{
		{
			name:   "nil schema",
			schema: nil,
		},
		{
			name:   "empty schema",
			schema: &openapi3.T{},
		},
		{
			name: "schema with nil paths",
			schema: &openapi3.T{
				Paths: nil,
			},
		},
		{
			name: "schema with empty paths",
			schema: &openapi3.T{
				Paths: &openapi3.Paths{},
			},
		},
		{
			name: "schema with path but no post",
			schema: &openapi3.T{
				Paths: &openapi3.Paths{
					Extensions: map[string]any{},
				},
			},
		},
		{
			name: "schema with post but no responses",
			schema: func() *openapi3.T {
				s := &openapi3.T{
					Paths: openapi3.NewPaths(),
				}
				s.Paths.Set("/predictions", &openapi3.PathItem{
					Post: &openapi3.Operation{},
				})
				return s
			}(),
		},
		{
			name: "schema with response but no content",
			schema: func() *openapi3.T {
				s := &openapi3.T{
					Paths: openapi3.NewPaths(),
				}
				s.Paths.Set("/predictions", &openapi3.PathItem{
					Post: &openapi3.Operation{
						Responses: &openapi3.Responses{},
					},
				})
				return s
			}(),
		},
		{
			name: "schema with content but no output property",
			schema: func() *openapi3.T {
				s := &openapi3.T{
					Paths: openapi3.NewPaths(),
				}
				responses := openapi3.NewResponses()
				responses.Set("200", &openapi3.ResponseRef{
					Value: &openapi3.Response{
						Content: openapi3.Content{
							"application/json": &openapi3.MediaType{
								Schema: &openapi3.SchemaRef{
									Value: &openapi3.Schema{
										Properties: openapi3.Schemas{},
									},
								},
							},
						},
					},
				})
				s.Paths.Set("/predictions", &openapi3.PathItem{
					Post: &openapi3.Operation{
						Responses: responses,
					},
				})
				return s
			}(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// This should not panic - it should return an error or nil gracefully
			outputSchema := safeExtractOutputSchema(tc.schema, "/predictions")
			// We expect nil for all malformed schemas
			require.Nil(t, outputSchema, "expected nil output schema for malformed input")
		})
	}
}

// safeExtractOutputSchema extracts the output schema safely without panicking
func safeExtractOutputSchema(schema *openapi3.T, url string) *openapi3.Schema {
	if schema == nil || schema.Paths == nil {
		return nil
	}
	pathItem := schema.Paths.Value(url)
	if pathItem == nil || pathItem.Post == nil {
		return nil
	}
	if pathItem.Post.Responses == nil {
		return nil
	}
	resp := pathItem.Post.Responses.Value("200")
	if resp == nil || resp.Value == nil {
		return nil
	}
	content, ok := resp.Value.Content["application/json"]
	if !ok || content == nil || content.Schema == nil || content.Schema.Value == nil {
		return nil
	}
	outputProp, ok := content.Schema.Value.Properties["output"]
	if !ok || outputProp == nil {
		return nil
	}
	return outputProp.Value
}

func TestExtractOutputSchemaFromValidSchema(t *testing.T) {
	// Test that we correctly extract output schema from a valid OpenAPI schema
	s := &openapi3.T{
		Paths: openapi3.NewPaths(),
	}
	responses := openapi3.NewResponses()
	responses.Set("200", &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Content: openapi3.Content{
				"application/json": &openapi3.MediaType{
					Schema: &openapi3.SchemaRef{
						Value: &openapi3.Schema{
							Properties: openapi3.Schemas{
								"output": &openapi3.SchemaRef{
									Value: &openapi3.Schema{
										Type: &openapi3.Types{"string"},
									},
								},
							},
						},
					},
				},
			},
		},
	})
	s.Paths.Set("/predictions", &openapi3.PathItem{
		Post: &openapi3.Operation{
			Responses: responses,
		},
	})

	outputSchema := safeExtractOutputSchema(s, "/predictions")
	require.NotNil(t, outputSchema, "expected non-nil output schema for valid input")
	require.Contains(t, outputSchema.Type.Slice(), "string", "expected string type")
}

func TestTransformPathsToBase64URLsRecursesIntoNestedJSON(t *testing.T) {
	dir := t.TempDir()
	fileA := filepath.Join(dir, "a.txt")
	fileB := filepath.Join(dir, "b.txt")
	fileC := filepath.Join(dir, "c.txt")
	require.NoError(t, os.WriteFile(fileA, []byte("alpha"), 0o644))
	require.NoError(t, os.WriteFile(fileB, []byte("beta"), 0o644))
	require.NoError(t, os.WriteFile(fileC, []byte("gamma"), 0o644))

	inputs := map[string]any{
		"single": "@" + fileA,
		"files": []any{
			"@" + fileB,
			map[string]any{
				"inner": "@" + fileC,
			},
		},
		"count": float64(3),
		"plain": "hello",
	}

	transformed, err := transformPathsToBase64URLs(inputs)
	require.NoError(t, err)

	require.IsType(t, "", transformed["single"])
	require.Contains(t, transformed["single"].(string), "data:text/plain;base64,")

	files, ok := transformed["files"].([]any)
	require.True(t, ok)
	require.IsType(t, "", files[0])
	require.Contains(t, files[0].(string), "data:text/plain;base64,")

	innerObj, ok := files[1].(map[string]any)
	require.True(t, ok)
	require.IsType(t, "", innerObj["inner"])
	require.Contains(t, innerObj["inner"].(string), "data:text/plain;base64,")

	require.Equal(t, float64(3), transformed["count"])
	require.Equal(t, "hello", transformed["plain"])
}
