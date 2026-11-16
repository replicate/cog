package cli

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func scanTestTar(t *testing.T, files map[string][]byte) []byte {
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

func TestScanCommandNeedsImage(t *testing.T) {
	cmd := newScanCommand()
	cmd.SetArgs(nil)
	require.Error(t, cmd.Execute())
}

func TestScanCommandJSONAndFindingsExit(t *testing.T) {
	layer := scanTestTar(t, map[string][]byte{"secret.txt": []byte("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")})
	manifest := []byte(`[{"Config":"config.json","Layers":["layer/layer.tar"]}]`)
	archive := scanTestTar(t, map[string][]byte{"config.json": []byte("{}"), "manifest.json": manifest, "layer/layer.tar": layer})
	var out bytes.Buffer
	err := runScan(context.Background(), "local:test", true, &out, func(context.Context, string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(archive)), nil
	})
	require.ErrorIs(t, err, errScanFindings)
	require.NotContains(t, out.String(), "github_pat_")
	var result struct {
		Version  int `json:"version"`
		Findings []struct {
			Rule string `json:"rule"`
		} `json:"findings"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, 1, result.Version)
	require.NotEmpty(t, result.Findings)
	out.Reset()
	err = runScan(context.Background(), "local:test", true, &out, func(context.Context, string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("bad")), nil
	})
	require.ErrorContains(t, err, "incomplete scan")
	require.Empty(t, out.String())
}
