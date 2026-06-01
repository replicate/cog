package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSemverValidation(t *testing.T) {
	valid := []string{
		"0.17.0",
		"1.0.0",
		"0.17.0-rc.1",
		"0.17.0-alpha.2",
		"0.17.0-beta.3",
		"0.17.0-dev.4",
		"10.20.30",
		"0.0.1-pre.0",
	}
	for _, v := range valid {
		assert.True(t, semverRe.MatchString(v), "should accept %q", v)
	}

	invalid := []string{
		"",
		"v0.17.0",
		"0.17",
		"0.17.0.1",
		"abc",
		"0.17.0-",
		"0.17.0-rc 1",
		" 0.17.0",
		"0.17.0 ",
	}
	for _, v := range invalid {
		assert.False(t, semverRe.MatchString(v), "should reject %q", v)
	}
}

func TestParseCargoVersion(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
		wantErr bool
	}{
		{
			name: "workspace cargo.toml",
			content: `[workspace]
resolver = "2"
members = ["coglet", "coglet-python"]

[workspace.package]
version = "0.17.0-rc.2"
edition = "2024"
`,
			want: "0.17.0-rc.2",
		},
		{
			name: "simple package",
			content: `[package]
name = "foo"
version = "1.2.3"
`,
			want: "1.2.3",
		},
		{
			name:    "no version field",
			content: "[package]\nname = \"foo\"\n",
			wantErr: true,
		},
		{
			name:    "empty content",
			content: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCargoVersion(tt.content)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReplaceVersionInCargo(t *testing.T) {
	original := `[workspace]
resolver = "2"

[workspace.package]
version = "0.17.0-rc.2"
edition = "2024"
`

	t.Run("replaces version", func(t *testing.T) {
		result, err := replaceVersionInCargo(original, "0.17.0-rc.2", "0.18.0")
		require.NoError(t, err)
		assert.Contains(t, result, `version = "0.18.0"`)
		assert.NotContains(t, result, `version = "0.17.0-rc.2"`)

		// Verify the rest of the content is unchanged.
		assert.Contains(t, result, `resolver = "2"`)
		assert.Contains(t, result, `edition = "2024"`)
	})

	t.Run("old version not found", func(t *testing.T) {
		_, err := replaceVersionInCargo(original, "9.9.9", "1.0.0")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not contain")
	})

	t.Run("round-trips through parse", func(t *testing.T) {
		result, err := replaceVersionInCargo(original, "0.17.0-rc.2", "1.0.0-beta.5")
		require.NoError(t, err)

		parsed, err := parseCargoVersion(result)
		require.NoError(t, err)
		assert.Equal(t, "1.0.0-beta.5", parsed)
	})
}
