package scan

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

const testSecret = "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"

func tarData(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	for name, data := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data))}))
		_, err := tw.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	return b.Bytes()
}

func imageData(t *testing.T, layer []byte, config []byte) []byte {
	t.Helper()
	manifest, err := json.Marshal([]map[string]any{{"Config": "config.json", "Layers": []string{"layer/layer.tar"}}})
	require.NoError(t, err)
	return tarData(t, map[string][]byte{"manifest.json": manifest, "config.json": config, "layer/layer.tar": layer})
}

func TestScanDeletedLayerAndConfig(t *testing.T) {
	layer := tarData(t, map[string][]byte{"deleted.txt": []byte(testSecret), ".wh.deleted.txt": nil})
	result, err := Archive(bytes.NewReader(imageData(t, layer, []byte(`{"Env":["TOKEN=`+testSecret+`"]}`))))
	require.NoError(t, err)
	require.NotEmpty(t, result)
	paths := map[string]bool{}
	for _, f := range result {
		paths[f.Path] = true
		require.NotContains(t, f.Path, testSecret)
		require.NotContains(t, f.Rule, testSecret)
	}
	require.True(t, paths["layer "+safePath("layer/layer.tar")+":"+safePath("deleted.txt")])
	require.True(t, paths["config "+safePath("config.json")])
}

func TestScanCompressedLayerAndSymlinkMetadata(t *testing.T) {
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: testSecret}))
	require.NoError(t, tw.Close())
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	_, err := gz.Write(raw.Bytes())
	require.NoError(t, err)
	require.NoError(t, gz.Close())
	findings, err := Archive(bytes.NewReader(imageData(t, compressed.Bytes(), []byte("{}"))))
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	for _, f := range findings {
		require.NotContains(t, f.Path, testSecret)
	}
}

// These deterministic strings have provider-shaped syntax but are not live keys.
func fakeKeyBytes(seed string) string {
	sum := sha512.Sum512([]byte("cog secret scan test fixture: " + seed))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestScanProviderShapedKeys(t *testing.T) {
	tests := []struct {
		name string
		key  string
		rule string
	}{
		{
			name: "OpenAI",
			key:  "sk-proj-" + fakeKeyBytes("openai first")[:74] + "T3BlbkFJ" + fakeKeyBytes("openai second")[:74],
			rule: "openai-api-key",
		},
		{
			name: "Anthropic",
			key:  "sk-ant-api03-" + fakeKeyBytes("anthropic first") + fakeKeyBytes("anthropic second")[:7] + "AA",
			rule: "anthropic-api-key",
		},
		{
			name: "Google",
			key:  "AIza" + fakeKeyBytes("google")[:35],
			rule: "gcp-api-key",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layer := tarData(t, map[string][]byte{"app/.env": []byte("API_KEY=" + tt.key + "\n")})
			findings, err := Archive(bytes.NewReader(imageData(t, layer, []byte("{}"))))
			require.NoError(t, err)
			require.NotEmpty(t, findings, "Gitleaks must recognize this provider-shaped fixture")
			rules := make([]string, 0, len(findings))
			for _, finding := range findings {
				rules = append(rules, finding.Rule)
				require.NotContains(t, finding.Path, tt.key)
			}
			require.Contains(t, rules, tt.rule)
			require.Contains(t, findings, Finding{
				Path: "layer " + safePath("layer/layer.tar") + ":" + safePath("app/.env"),
				Rule: tt.rule, Line: 1,
			})
		})
	}
}

func TestScanUnsupportedLayerEntryIsIncomplete(t *testing.T) {
	var layer bytes.Buffer
	writer := tar.NewWriter(&layer)
	require.NoError(t, writer.WriteHeader(&tar.Header{
		Name: "unrecognized-file", Typeflag: 'Z', Size: int64(len(testSecret)),
	}))
	_, err := writer.Write([]byte(testSecret))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	_, err = Archive(bytes.NewReader(imageData(t, layer.Bytes(), []byte("{}"))))
	require.ErrorContains(t, err, "unsupported layer entry")
}

func TestScanClean(t *testing.T) {
	result, err := Archive(bytes.NewReader(imageData(t, tarData(t, map[string][]byte{"hello": []byte("hello world")}), []byte(`{"Env":[]}`))))
	require.NoError(t, err)
	require.Empty(t, result)
}

func TestScanIncomplete(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"oversize file", imageData(t, tarData(t, map[string][]byte{"big": bytes.Repeat([]byte("x"), maxFileBytes+1)}), []byte("{}"))},
		{"invalid layer", imageData(t, []byte("not a tar"), []byte("{}"))},
		{"missing manifest", tarData(t, map[string][]byte{"config.json": []byte("{}")})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Archive(bytes.NewReader(tt.data))
			require.ErrorContains(t, err, "incomplete scan")
		})
	}
	_, err := Archive(io.LimitReader(bytes.NewReader(imageData(t, tarData(t, map[string][]byte{"a": []byte("b")}), []byte("{}"))), 100))
	require.ErrorContains(t, err, "incomplete scan")
}
