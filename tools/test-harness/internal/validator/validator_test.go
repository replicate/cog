package validator

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/tools/test-harness/internal/manifest"
)

func TestValidateExact(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expect   manifest.Expectation
		wantPass bool
	}{
		{
			name:     "exact match",
			output:   "hello world",
			expect:   manifest.Expectation{Type: "exact", Value: "hello world"},
			wantPass: true,
		},
		{
			name:     "exact match with whitespace",
			output:   "  hello world  \n",
			expect:   manifest.Expectation{Type: "exact", Value: "hello world"},
			wantPass: true,
		},
		{
			name:     "mismatch",
			output:   "hello world",
			expect:   manifest.Expectation{Type: "exact", Value: "goodbye world"},
			wantPass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Validate(tt.output, tt.expect)
			assert.Equal(t, tt.wantPass, result.Passed, "message: %s", result.Message)
		})
	}
}

func TestValidateContains(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expect   manifest.Expectation
		wantPass bool
	}{
		{
			name:     "contains substring",
			output:   "hello world",
			expect:   manifest.Expectation{Type: "contains", Value: "world"},
			wantPass: true,
		},
		{
			name:     "does not contain",
			output:   "hello world",
			expect:   manifest.Expectation{Type: "contains", Value: "goodbye"},
			wantPass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Validate(tt.output, tt.expect)
			assert.Equal(t, tt.wantPass, result.Passed, "message: %s", result.Message)
		})
	}
}

func TestValidateRegex(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expect   manifest.Expectation
		wantPass bool
	}{
		{
			name:     "matches pattern",
			output:   "hello 123 world",
			expect:   manifest.Expectation{Type: "regex", Pattern: `\d+`},
			wantPass: true,
		},
		{
			name:     "does not match",
			output:   "hello world",
			expect:   manifest.Expectation{Type: "regex", Pattern: `\d+`},
			wantPass: false,
		},
		{
			name:     "invalid pattern",
			output:   "hello",
			expect:   manifest.Expectation{Type: "regex", Pattern: `[`},
			wantPass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Validate(tt.output, tt.expect)
			assert.Equal(t, tt.wantPass, result.Passed, "message: %s", result.Message)
		})
	}
}

func TestValidateFileExists(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-*.png")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	tests := []struct {
		name     string
		output   string
		expect   manifest.Expectation
		wantPass bool
	}{
		{
			name:     "file exists no mime check",
			output:   tmpFile.Name(),
			expect:   manifest.Expectation{Type: "file_exists"},
			wantPass: true,
		},
		{
			name:     "file exists with correct mime",
			output:   tmpFile.Name(),
			expect:   manifest.Expectation{Type: "file_exists", Mime: "image/png"},
			wantPass: true,
		},
		{
			name:     "file exists with wrong mime",
			output:   tmpFile.Name(),
			expect:   manifest.Expectation{Type: "file_exists", Mime: "image/jpeg"},
			wantPass: false,
		},
		{
			name:     "file does not exist",
			output:   "/nonexistent/path/file.png",
			expect:   manifest.Expectation{Type: "file_exists"},
			wantPass: false,
		},
		{
			name:     "url passes",
			output:   "https://example.com/image.png",
			expect:   manifest.Expectation{Type: "file_exists"},
			wantPass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Validate(tt.output, tt.expect)
			assert.Equal(t, tt.wantPass, result.Passed, "message: %s", result.Message)
		})
	}
}

func TestValidateNotEmpty(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		wantPass bool
	}{
		{
			name:     "non-empty",
			output:   "hello",
			wantPass: true,
		},
		{
			name:     "empty",
			output:   "",
			wantPass: false,
		},
		{
			name:     "whitespace only",
			output:   "   \n\t  ",
			wantPass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Validate(tt.output, manifest.Expectation{Type: "not_empty"})
			assert.Equal(t, tt.wantPass, result.Passed, "message: %s", result.Message)
		})
	}
}

func TestValidateJSONMatch(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expect   manifest.Expectation
		wantPass bool
	}{
		{
			name:   "exact json match",
			output: `{"name": "test", "value": 123}`,
			expect: manifest.Expectation{
				Type:  "json_match",
				Match: map[string]any{"name": "test"},
			},
			wantPass: true,
		},
		{
			name:   "nested match",
			output: `{"data": {"nested": "value"}}`,
			expect: manifest.Expectation{
				Type:  "json_match",
				Match: map[string]any{"data": map[string]any{"nested": "value"}},
			},
			wantPass: true,
		},
		{
			name:   "mismatch",
			output: `{"name": "test"}`,
			expect: manifest.Expectation{
				Type:  "json_match",
				Match: map[string]any{"name": "other"},
			},
			wantPass: false,
		},
		{
			name:     "invalid json",
			output:   `not json`,
			expect:   manifest.Expectation{Type: "json_match", Match: map[string]any{}},
			wantPass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Validate(tt.output, tt.expect)
			assert.Equal(t, tt.wantPass, result.Passed, "message: %s", result.Message)
		})
	}
}

func TestValidateJSONKeys(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expect   manifest.Expectation
		wantPass bool
	}{
		{
			name:     "has required keys",
			output:   `{"a": 1, "b": 2}`,
			expect:   manifest.Expectation{Type: "json_keys", Keys: []string{"a", "b"}},
			wantPass: true,
		},
		{
			name:     "missing key",
			output:   `{"a": 1}`,
			expect:   manifest.Expectation{Type: "json_keys", Keys: []string{"a", "b"}},
			wantPass: false,
		},
		{
			name:     "non-empty object no required keys",
			output:   `{"a": 1}`,
			expect:   manifest.Expectation{Type: "json_keys"},
			wantPass: true,
		},
		{
			name:     "empty object",
			output:   `{}`,
			expect:   manifest.Expectation{Type: "json_keys"},
			wantPass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Validate(tt.output, tt.expect)
			assert.Equal(t, tt.wantPass, result.Passed, "message: %s", result.Message)
		})
	}
}

func TestIsSubset(t *testing.T) {
	tests := []struct {
		name     string
		subset   map[string]any
		superset map[string]any
		want     bool
	}{
		{
			name:     "exact match",
			subset:   map[string]any{"a": 1},
			superset: map[string]any{"a": 1},
			want:     true,
		},
		{
			name:     "subset",
			subset:   map[string]any{"a": 1},
			superset: map[string]any{"a": 1, "b": 2},
			want:     true,
		},
		{
			name:     "not subset",
			subset:   map[string]any{"a": 1},
			superset: map[string]any{"a": 2},
			want:     false,
		},
		{
			name:     "nested match",
			subset:   map[string]any{"a": map[string]any{"b": 1}},
			superset: map[string]any{"a": map[string]any{"b": 1, "c": 2}},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isSubset(tt.subset, tt.superset))
		})
	}
}
