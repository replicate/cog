package config

import (
	"testing"
)

// FuzzParseBytes fuzzes the YAML parsing layer to find inputs that cause
// panics during unmarshaling.
func FuzzParseBytes(f *testing.F) {
	// Seed with valid and edge-case YAML inputs
	f.Add([]byte(""))
	f.Add([]byte("build:\n  python_version: \"3.13\"\n"))
	f.Add([]byte("build:\n  run:\n    - \"echo hello\"\n"))
	f.Add([]byte("weights:\n  - name: foo\n    target: /weights\n    source:\n      uri: hf://model\n"))
	f.Add([]byte("build:\n  run:\n    - command: echo\n      mounts:\n        - type: bind\n          id: vol\n          target: /mnt\n"))
	f.Add([]byte("{\"build\": {\"python_version\": \"3.13\"}}"))
	f.Add([]byte("build:\n  gpu: true\n  cuda: \"12.4\"\n  python_version: \"3.13\"\n"))
	f.Add([]byte("concurrency:\n  max: 5\n"))
	f.Add([]byte("---\nnull\n"))
	f.Add([]byte("build:\n  run:\n    - ~\n"))
	f.Add([]byte("weights:\n  - name: a\n    target: /a\n    source: ~\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		// The parser should never panic — it may return errors for invalid YAML,
		// but it must not crash.
		_, _ = parseBytes(data)
	})
}

// FuzzFromYAML fuzzes the higher-level FromYAML function which parses YAML and
// converts it into a Config struct. This exercises both the YAML layer and the
// configFile-to-Config conversion.
func FuzzFromYAML(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("build:\n  python_version: \"3.13\"\n"))
	f.Add([]byte("build:\n  run:\n    - \"echo hello\"\n"))
	f.Add([]byte("weights:\n  - name: foo\n    target: /weights\n    source:\n      uri: hf://model\n"))
	f.Add([]byte("build:\n  run:\n    - command: echo\n      mounts:\n        - type: bind\n          id: vol\n          target: /mnt\n"))
	f.Add([]byte("{\"build\": {\"python_version\": \"3.13\"}}"))
	f.Add([]byte("build:\n  gpu: true\n  cuda: \"12.4\"\n  python_version: \"3.13\"\n"))
	f.Add([]byte("concurrency:\n  max: 5\n"))
	f.Add([]byte("---\nnull\n"))
	f.Add([]byte("build:\n  run:\n    - ~\n"))
	f.Add([]byte("weights:\n  - name: a\n    target: /a\n    source: ~\n"))
	f.Add([]byte("predict: predict.py:Predictor\ntrain: train.py:Trainer\n"))
	f.Add([]byte("environment:\n  - \"FOO=bar\"\n"))
	f.Add([]byte("build:\n  python_packages:\n    - torch==2.0.1\n  python_version: \"3.13\"\n"))
	f.Add([]byte("build:\n  python_requirements: requirements.txt\n  python_version: \"3.13\"\n"))
	f.Add([]byte("image: \"registry.example.com/acme/my-model\"\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, err := FromYAML(data)
		if err != nil {
			// Errors are expected for invalid input; panics are not.
			return
		}
		if cfg == nil {
			t.Fatal("FromYAML returned nil config without error")
		}
	})
}

// FuzzConfigComplete fuzzes the Complete() method by generating random Config
// structs from parsed YAML and calling Complete(). This catches panics in
// CUDA resolution, requirements loading, and environment parsing.
func FuzzConfigComplete(f *testing.F) {
	f.Add([]byte("build:\n  python_version: \"3.13\"\n"))
	f.Add([]byte("build:\n  gpu: true\n  python_version: \"3.13\"\n"))
	f.Add([]byte("build:\n  gpu: true\n  python_version: \"3.13\"\n  cuda: \"12.4\"\n"))
	f.Add([]byte("build:\n  python_version: \"3.13\"\n  python_packages:\n    - torch==2.0.1\n"))
	f.Add([]byte("build:\n  gpu: true\n  python_version: \"3.13\"\n  python_packages:\n    - torch==2.0.1\n"))
	f.Add([]byte("build:\n  python_version: \"3.13\"\n  python_requirements: reqs.txt\n"))
	f.Add([]byte("build:\n  python_version: \"3.13\"\n  run:\n    - \"echo hello\"\n"))
	f.Add([]byte("concurrency:\n  max: 3\n"))
	f.Add([]byte("environment:\n  - \"FOO=bar\"\n"))
	f.Add([]byte("weights:\n  - name: w\n    target: /weights\n    source:\n      uri: hf://model\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, err := FromYAML(data)
		if err != nil {
			return
		}
		if cfg == nil {
			return
		}
		// Complete may error for invalid configs (e.g. missing requirements file),
		// but it must never panic.
		_ = cfg.Complete(t.TempDir())
	})
}
