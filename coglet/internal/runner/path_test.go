package runner

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBase64Regex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		matches bool
		base64  string
	}{
		{
			name:    "valid data URL",
			input:   "data:text/plain;base64,SGVsbG8gV29ybGQ=",
			matches: true,
			base64:  "SGVsbG8gV29ybGQ=",
		},
		{
			name:    "valid image data URL",
			input:   "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
			matches: true,
			base64:  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
		},
		{
			name:    "no data URL",
			input:   "https://example.com/image.png",
			matches: false,
		},
		{
			name:    "invalid data URL",
			input:   "data:text/plain,not-base64",
			matches: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			matches := Base64Regex.FindStringSubmatch(tt.input)
			if tt.matches {
				require.Len(t, matches, 2)
				assert.Equal(t, tt.base64, matches[1])
			} else {
				assert.Nil(t, matches)
			}
		})
	}
}

func TestIsURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema *openapi3.SchemaRef
		want   bool
	}{
		{
			name: "URI string schema",
			schema: &openapi3.SchemaRef{
				Value: &openapi3.Schema{
					Type:   &openapi3.Types{"string"},
					Format: "uri",
				},
			},
			want: true,
		},
		{
			name: "regular string schema",
			schema: &openapi3.SchemaRef{
				Value: &openapi3.Schema{
					Type: &openapi3.Types{"string"},
				},
			},
			want: false,
		},
		{
			name: "integer schema",
			schema: &openapi3.SchemaRef{
				Value: &openapi3.Schema{
					Type: &openapi3.Types{"integer"},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := isURI(tt.schema)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBase64ToInput(t *testing.T) {
	t.Parallel()

	t.Run("converts base64 data URL to file", func(t *testing.T) {
		t.Parallel()

		content := "Hello, World!"
		b64Content := base64.StdEncoding.EncodeToString([]byte(content))
		dataURL := fmt.Sprintf("data:text/plain;base64,%s", b64Content)

		var paths []string
		result, err := Base64ToInput(dataURL, &paths)

		require.NoError(t, err)
		assert.NotEqual(t, dataURL, result)
		assert.Len(t, paths, 1)
		assert.Equal(t, result, paths[0])

		// Verify file exists and has correct content
		fileContent, err := os.ReadFile(result)
		require.NoError(t, err)
		assert.Equal(t, content, string(fileContent))

		// Check file permissions
		info, err := os.Stat(result)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o666), info.Mode().Perm())

		// Cleanup
		os.Remove(result)
	})

	t.Run("passes through non-data URLs", func(t *testing.T) {
		t.Parallel()

		tests := []string{
			"https://example.com/file.txt",
			"file:///tmp/test.txt",
			"just-a-string",
			"data:text/plain,not-base64",
		}

		for _, input := range tests {
			var paths []string
			result, err := Base64ToInput(input, &paths)

			require.NoError(t, err)
			assert.Equal(t, input, result)
			assert.Empty(t, paths)
		}
	})

	t.Run("handles invalid base64", func(t *testing.T) {
		t.Parallel()

		dataURL := "data:text/plain;base64,invalid-base64!"
		var paths []string
		result, err := Base64ToInput(dataURL, &paths)

		require.Error(t, err)
		assert.Empty(t, result)
		assert.Empty(t, paths)
	})
}

func TestURLToInput(t *testing.T) {
	t.Parallel()

	t.Run("downloads HTTP URL to file", func(t *testing.T) {
		t.Parallel()

		content := "Test content from server"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(content))
		}))
		defer server.Close()

		var paths []string
		result, err := URLToInput(server.URL+"/test.txt", &paths)

		require.NoError(t, err)
		assert.NotEqual(t, server.URL+"/test.txt", result)
		assert.Len(t, paths, 1)
		assert.Equal(t, result, paths[0])

		fileContent, err := os.ReadFile(result)
		require.NoError(t, err)
		assert.Equal(t, content, string(fileContent))

		info, err := os.Stat(result)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o666), info.Mode().Perm())

		os.Remove(result)
	})

	t.Run("preserves file extension", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("content"))
		}))
		defer server.Close()

		var paths []string
		result, err := URLToInput(server.URL+"/test.json", &paths)

		require.NoError(t, err)
		assert.True(t, strings.HasSuffix(result, ".json"))

		// Cleanup
		os.Remove(result)
	})

	t.Run("passes through non-HTTP URLs", func(t *testing.T) {
		t.Parallel()

		tests := []string{
			"file:///tmp/test.txt",
			"ftp://example.com/file.txt",
			"just-a-string",
		}

		for _, input := range tests {
			var paths []string
			result, err := URLToInput(input, &paths)

			require.NoError(t, err)
			assert.Equal(t, input, result)
			assert.Empty(t, paths)
		}
	})

	t.Run("handles server errors", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		var paths []string
		result, err := URLToInput(server.URL+"/test.txt", &paths)

		// URLToInput doesn't check HTTP status codes
		require.NoError(t, err)
		assert.Len(t, paths, 1)

		// Cleanup
		os.Remove(result)
	})
}

func TestOutputToBase64(t *testing.T) {
	t.Parallel()

	t.Run("converts file to base64 data URL", func(t *testing.T) {
		t.Parallel()

		content := "Test file content"
		tmpFile, err := os.CreateTemp("", "test-*.txt")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.WriteString(content)
		require.NoError(t, err)
		tmpFile.Close()

		fileURL := fmt.Sprintf("file://%s", tmpFile.Name())
		var paths []string
		result, err := OutputToBase64(fileURL, &paths)

		require.NoError(t, err)
		assert.NotEqual(t, fileURL, result)
		assert.Len(t, paths, 1)
		assert.Equal(t, tmpFile.Name(), paths[0])

		// Verify it's a valid data URL
		assert.True(t, strings.HasPrefix(result, "data:"))
		assert.Contains(t, result, "base64,")

		// Extract and verify base64 content
		parts := strings.Split(result, "base64,")
		require.Len(t, parts, 2)
		decoded, err := base64.StdEncoding.DecodeString(parts[1])
		require.NoError(t, err)
		assert.Equal(t, content, string(decoded))
	})

	t.Run("passes through non-file URLs", func(t *testing.T) {
		t.Parallel()

		tests := []string{
			"https://example.com/file.txt",
			"http://example.com/file.txt",
			"just-a-string",
		}

		for _, input := range tests {
			var paths []string
			result, err := OutputToBase64(input, &paths)

			require.NoError(t, err)
			assert.Equal(t, input, result)
			assert.Empty(t, paths)
		}
	})

	t.Run("handles non-existent file", func(t *testing.T) {
		t.Parallel()

		fileURL := "file:///non/existent/file.txt"
		var paths []string
		result, err := OutputToBase64(fileURL, &paths)

		require.Error(t, err)
		assert.Empty(t, result)
		assert.Empty(t, paths)
	})
}

func TestUploaderProcessOutput(t *testing.T) {
	t.Parallel()

	t.Run("uploads file and returns location", func(t *testing.T) {
		t.Parallel()

		content := "Test file content for upload"
		var uploadedContent []byte
		var uploadedHeaders http.Header
		var uploadedPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uploadedHeaders = r.Header.Clone()
			uploadedPath = r.URL.Path
			body, _ := io.ReadAll(r.Body)
			uploadedContent = body
			w.Header().Set("Location", "https://cdn.example.com/uploaded/file.txt")
			w.WriteHeader(http.StatusCreated)
		}))
		defer server.Close()

		tmpFile, err := os.CreateTemp("", "test-*.txt")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.WriteString(content)
		require.NoError(t, err)
		tmpFile.Close()

		uploader := newUploader(server.URL + "/upload")
		fileURL := fmt.Sprintf("file://%s", tmpFile.Name())
		var paths []string
		result, err := uploader.processOutput(fileURL, "test-prediction-123", &paths)

		require.NoError(t, err)
		assert.Equal(t, "https://cdn.example.com/uploaded/file.txt", result)
		assert.Len(t, paths, 1)
		assert.Equal(t, tmpFile.Name(), paths[0])

		// Verify upload details
		assert.Equal(t, content, string(uploadedContent))
		assert.Equal(t, "test-prediction-123", uploadedHeaders.Get("X-Prediction-ID"))
		assert.True(t, strings.HasPrefix(uploadedHeaders.Get("Content-Type"), "text/"))
		assert.True(t, strings.HasSuffix(uploadedPath, filepath.Base(tmpFile.Name())))
	})

	t.Run("falls back to upload URL when no location header", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		tmpFile, err := os.CreateTemp("", "test-*.txt")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.WriteString("content")
		require.NoError(t, err)
		tmpFile.Close()

		uploader := newUploader(server.URL + "/upload")
		fileURL := fmt.Sprintf("file://%s", tmpFile.Name())
		var paths []string
		result, err := uploader.processOutput(fileURL, "test-prediction", &paths)

		require.NoError(t, err)
		expectedURL := fmt.Sprintf("%s/upload/%s", server.URL, filepath.Base(tmpFile.Name()))
		assert.Equal(t, expectedURL, result)
	})

	t.Run("passes through non-file URLs", func(t *testing.T) {
		t.Parallel()

		uploader := newUploader("https://upload.example.com")
		tests := []string{
			"https://example.com/file.txt",
			"http://example.com/file.txt",
			"just-a-string",
		}

		for _, input := range tests {
			var paths []string
			result, err := uploader.processOutput(input, "test", &paths)

			require.NoError(t, err)
			assert.Equal(t, input, result)
			assert.Empty(t, paths)
		}
	})

	t.Run("handles upload errors", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		tmpFile, err := os.CreateTemp("", "test-*.txt")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.WriteString("content")
		require.NoError(t, err)
		tmpFile.Close()

		// Use a regular HTTP client without retries for faster test
		uploader := &uploader{
			client:    &http.Client{},
			uploadURL: server.URL + "/upload",
		}
		fileURL := fmt.Sprintf("file://%s", tmpFile.Name())
		var paths []string
		result, err := uploader.processOutput(fileURL, "test", &paths)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to upload file")
		assert.Empty(t, result)
	})
}

func TestProcessInputPaths(t *testing.T) {
	t.Parallel()

	t.Run("processes URI fields in schema", func(t *testing.T) {
		t.Parallel()

		// Create a simple OpenAPI schema
		doc := &openapi3.T{
			Components: &openapi3.Components{
				Schemas: map[string]*openapi3.SchemaRef{
					"Input": {
						Value: &openapi3.Schema{
							Type: &openapi3.Types{"object"},
							Properties: map[string]*openapi3.SchemaRef{
								"image": {
									Value: &openapi3.Schema{
										Type:   &openapi3.Types{"string"},
										Format: "uri",
									},
								},
								"text": {
									Value: &openapi3.Schema{
										Type: &openapi3.Types{"string"},
									},
								},
							},
						},
					},
				},
			},
		}

		input := map[string]any{
			"image": "data:image/png;base64,SGVsbG8=",
			"text":  "Hello World",
		}

		var paths []string
		mockFn := func(s string, paths *[]string) (string, error) {
			if strings.HasPrefix(s, "data:") {
				*paths = append(*paths, "/tmp/processed-"+s[:10])
				return "/tmp/processed-file", nil
			}
			return s, nil
		}

		result, err := ProcessInputPaths(input, doc, &paths, mockFn)
		require.NoError(t, err)

		resultMap, ok := result.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "/tmp/processed-file", resultMap["image"])
		assert.Equal(t, "Hello World", resultMap["text"]) // unchanged
		assert.Len(t, paths, 1)
	})

	t.Run("processes array URI fields", func(t *testing.T) {
		t.Parallel()

		doc := &openapi3.T{
			Components: &openapi3.Components{
				Schemas: map[string]*openapi3.SchemaRef{
					"Input": {
						Value: &openapi3.Schema{
							Type: &openapi3.Types{"object"},
							Properties: map[string]*openapi3.SchemaRef{
								"images": {
									Value: &openapi3.Schema{
										Type: &openapi3.Types{"array"},
										Items: &openapi3.SchemaRef{
											Value: &openapi3.Schema{
												Type:   &openapi3.Types{"string"},
												Format: "uri",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		input := map[string]any{
			"images": []any{
				"data:image/png;base64,SGVsbG8=",
				"https://example.com/image.jpg",
			},
		}

		var paths []string
		mockFn := func(s string, paths *[]string) (string, error) {
			*paths = append(*paths, "/tmp/processed-"+s[:10])
			return "/tmp/processed-" + s[:10], nil
		}

		result, err := ProcessInputPaths(input, doc, &paths, mockFn)
		require.NoError(t, err)

		resultMap, ok := result.(map[string]any)
		require.True(t, ok)
		images, ok := resultMap["images"].([]any)
		require.True(t, ok)
		assert.Len(t, images, 2)
		assert.Len(t, paths, 2)
	})

	t.Run("handles nil doc gracefully", func(t *testing.T) {
		t.Parallel()

		input := map[string]any{"test": "value"}
		var paths []string
		mockFn := func(s string, paths *[]string) (string, error) {
			return s, nil
		}

		result, err := ProcessInputPaths(input, nil, &paths, mockFn)
		require.ErrorIs(t, err, ErrSchemaNotAvailable)
		assert.Equal(t, input, result)
		assert.Empty(t, paths)
	})

	t.Run("handles missing Input schema", func(t *testing.T) {
		t.Parallel()

		doc := &openapi3.T{
			Components: &openapi3.Components{
				Schemas: map[string]*openapi3.SchemaRef{},
			},
		}

		input := map[string]any{"test": "value"}
		var paths []string
		mockFn := func(s string, paths *[]string) (string, error) {
			return s, nil
		}

		result, err := ProcessInputPaths(input, doc, &paths, mockFn)
		require.NoError(t, err)
		assert.Equal(t, input, result)
		assert.Empty(t, paths)
	})

	t.Run("handles non-map input", func(t *testing.T) {
		t.Parallel()

		doc := &openapi3.T{
			Components: &openapi3.Components{
				Schemas: map[string]*openapi3.SchemaRef{
					"Input": {
						Value: &openapi3.Schema{
							Type: &openapi3.Types{"object"},
						},
					},
				},
			},
		}

		input := "not a map"
		var paths []string
		mockFn := func(s string, paths *[]string) (string, error) {
			return s, nil
		}

		result, err := ProcessInputPaths(input, doc, &paths, mockFn)
		require.NoError(t, err)
		assert.Equal(t, input, result)
		assert.Empty(t, paths)
	})
}

func TestHandlePath(t *testing.T) {
	t.Parallel()

	mockFn := func(s string, paths *[]string) (string, error) {
		if s == "error" {
			return "", fmt.Errorf("mock error")
		}
		*paths = append(*paths, s)
		return "processed-" + s, nil
	}

	t.Run("handles string input", func(t *testing.T) {
		t.Parallel()

		var paths []string
		result, err := handlePath("test", &paths, mockFn)

		require.NoError(t, err)
		assert.Equal(t, "processed-test", result)
		assert.Len(t, paths, 1)
		assert.Equal(t, "test", paths[0])
	})

	t.Run("handles array input", func(t *testing.T) {
		t.Parallel()

		input := []any{"test1", "test2", []any{"nested"}}
		var paths []string
		result, err := handlePath(input, &paths, mockFn)

		require.NoError(t, err)
		resultArray, ok := result.([]any)
		require.True(t, ok)
		assert.Len(t, resultArray, 3)
		assert.Equal(t, "processed-test1", resultArray[0])
		assert.Equal(t, "processed-test2", resultArray[1])

		nestedArray, ok := resultArray[2].([]any)
		require.True(t, ok)
		assert.Equal(t, "processed-nested", nestedArray[0])
	})

	t.Run("handles map input", func(t *testing.T) {
		t.Parallel()

		input := map[string]any{
			"key1": "value1",
			"key2": map[string]any{
				"nested": "value2",
			},
		}
		var paths []string
		result, err := handlePath(input, &paths, mockFn)

		require.NoError(t, err)
		resultMap, ok := result.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "processed-value1", resultMap["key1"])

		nestedMap, ok := resultMap["key2"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "processed-value2", nestedMap["nested"])
	})

	t.Run("handles other types", func(t *testing.T) {
		t.Parallel()

		tests := []any{123, true, nil}

		for _, input := range tests {
			var paths []string
			result, err := handlePath(input, &paths, mockFn)

			require.NoError(t, err)
			assert.Equal(t, input, result)
			assert.Empty(t, paths)
		}
	})

	t.Run("propagates errors", func(t *testing.T) {
		t.Parallel()

		var paths []string
		result, err := handlePath("error", &paths, mockFn)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "mock error")
		assert.Empty(t, result)
	})
}
