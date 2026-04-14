package patcher

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPatch(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Test case 1: Patch with SDK version
	t.Run("patch sdk version", func(t *testing.T) {
		cogYAML := filepath.Join(tmpDir, "cog1.yaml")
		content := `build:
  python_version: "3.10"
predict: predict.py
`
		if err := os.WriteFile(cogYAML, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		if err := Patch(cogYAML, "0.16.12", nil); err != nil {
			t.Fatalf("Patch failed: %v", err)
		}

		data, err := os.ReadFile(cogYAML)
		if err != nil {
			t.Fatal(err)
		}

		if !contains(string(data), "sdk_version: 0.16.12") {
			t.Errorf("Expected sdk_version in output, got:\n%s", string(data))
		}
	})

	// Test case 2: Patch with overrides
	t.Run("patch with overrides", func(t *testing.T) {
		cogYAML := filepath.Join(tmpDir, "cog2.yaml")
		content := `build:
  python_version: "3.10"
predict: predict.py
`
		if err := os.WriteFile(cogYAML, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		overrides := map[string]any{
			"build": map[string]any{
				"system_packages": []string{"ffmpeg"},
			},
		}

		if err := Patch(cogYAML, "", overrides); err != nil {
			t.Fatalf("Patch failed: %v", err)
		}

		data, err := os.ReadFile(cogYAML)
		if err != nil {
			t.Fatal(err)
		}

		if !contains(string(data), "system_packages") {
			t.Errorf("Expected system_packages in output, got:\n%s", string(data))
		}
	})

	// Test case 3: Patch with both SDK version and overrides
	t.Run("patch sdk and overrides", func(t *testing.T) {
		cogYAML := filepath.Join(tmpDir, "cog3.yaml")
		content := `build:
  python_version: "3.10"
predict: predict.py
`
		if err := os.WriteFile(cogYAML, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		overrides := map[string]any{
			"predict": "new_predict.py",
		}

		if err := Patch(cogYAML, "0.16.12", overrides); err != nil {
			t.Fatalf("Patch failed: %v", err)
		}

		data, err := os.ReadFile(cogYAML)
		if err != nil {
			t.Fatal(err)
		}

		result := string(data)
		if !contains(result, "sdk_version: 0.16.12") {
			t.Errorf("Expected sdk_version in output, got:\n%s", result)
		}
		if !contains(result, "new_predict.py") {
			t.Errorf("Expected new_predict.py in output, got:\n%s", result)
		}
	})
}

func TestDeepMerge(t *testing.T) {
	tests := []struct {
		name     string
		base     map[string]any
		override map[string]any
		want     map[string]any
	}{
		{
			name:     "simple merge",
			base:     map[string]any{"a": 1},
			override: map[string]any{"b": 2},
			want:     map[string]any{"a": 1, "b": 2},
		},
		{
			name:     "nested merge",
			base:     map[string]any{"build": map[string]any{"python": "3.10"}},
			override: map[string]any{"build": map[string]any{"sdk": "0.16"}},
			want:     map[string]any{"build": map[string]any{"python": "3.10", "sdk": "0.16"}},
		},
		{
			name:     "override replaces",
			base:     map[string]any{"a": 1},
			override: map[string]any{"a": 2},
			want:     map[string]any{"a": 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deepMerge(tt.base, tt.override)
			if !mapsEqual(got, tt.want) {
				t.Errorf("deepMerge() = %v, want %v", got, tt.want)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		bv, ok := b[k]
		if !ok {
			return false
		}
		vma, vmaOK := v.(map[string]any)
		vmb, vmbOK := bv.(map[string]any)
		if vmaOK && vmbOK {
			if !mapsEqual(vma, vmb) {
				return false
			}
		} else if v != bv {
			return false
		}
	}
	return true
}
