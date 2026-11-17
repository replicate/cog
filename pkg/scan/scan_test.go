package scan

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
				Rule: tt.rule, Line: 1, SourcePath: "app/.env",
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

// A real docker save uses content-addressed blobs, not layer.tar paths.
func TestScanRejectsManifestAsLayer(t *testing.T) {
	manifest := []byte(`[{"Config":"config.json","Layers":["manifest.json"]}]`)
	archive := tarData(t, map[string][]byte{"manifest.json": manifest, "config.json": []byte("{}")})
	_, err := Archive(bytes.NewReader(archive))
	require.ErrorContains(t, err, "missing layer")
}

func TestScanBlobsBeforeManifest(t *testing.T) {
	layer := tarData(t, map[string][]byte{"secrets/token.txt": []byte(testSecret)})
	manifest := []byte(`[ {"Config":"blobs/sha256/config", "Layers":["blobs/sha256/layer"]} ]`)
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for _, entry := range []struct {
		name string
		data []byte
	}{
		{"blobs/sha256/layer", layer}, {"blobs/sha256/config", []byte(`{"Env":["TOKEN=` + testSecret + `"]}`)}, {"manifest.json", manifest},
	} {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: entry.name, Mode: 0o600, Size: int64(len(entry.data))}))
		_, err := tw.Write(entry.data)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	findings, err := Archive(bytes.NewReader(archive.Bytes()))
	require.NoError(t, err)
	require.Contains(t, findings, Finding{Path: "layer " + safePath("blobs/sha256/layer") + ":" + safePath("secrets/token.txt"), Rule: "github-fine-grained-pat", Line: 1, SourcePath: "secrets/token.txt"})
	require.Contains(t, findings, Finding{Path: "config " + safePath("blobs/sha256/config"), Rule: "github-fine-grained-pat", Line: 1, SourcePath: "blobs/sha256/config"})
}

func TestScanPathOnlyRuleRedactsName(t *testing.T) {
	name := "keys/credential.p12"
	findings, err := Archive(bytes.NewReader(imageData(t, tarData(t, map[string][]byte{name: []byte("not a secret")}), []byte("{}"))))
	require.NoError(t, err)
	require.Contains(t, findings, Finding{Path: "layer " + safePath("layer/layer.tar") + ":" + safePath(name), Rule: "pkcs12-file", SourcePath: name})
	serialized, err := json.Marshal(findings)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), name)
}

func TestScanLargeFileAndLineNumbers(t *testing.T) {
	content := strings.Repeat("safe filler line\n", maxFileBytes/17+1) + testSecret + "\n"
	findings, err := Archive(bytes.NewReader(imageData(t, tarData(t, map[string][]byte{"large.txt": []byte(content)}), []byte("{}"))))
	require.NoError(t, err)
	require.Contains(t, findings, Finding{Path: "layer " + safePath("layer/layer.tar") + ":" + safePath("large.txt"), Rule: "github-fine-grained-pat", Line: strings.Count(content, "\n"), SourcePath: "large.txt"})
}

func TestScanLargeFileAcrossMultipleWindows(t *testing.T) {
	content := strings.Repeat("safe filler line\n", 1100000) + testSecret + "\n"
	findings, err := Archive(bytes.NewReader(imageData(t, tarData(t, map[string][]byte{"multi.txt": []byte(content)}), []byte("{}"))))
	require.NoError(t, err)
	require.Contains(t, findings, Finding{Path: "layer " + safePath("layer/layer.tar") + ":" + safePath("multi.txt"), Rule: "github-fine-grained-pat", Line: 1100001, SourcePath: "multi.txt"})
}

func TestScanOversizedSingleLineFailsClosed(t *testing.T) {
	layer := tarData(t, map[string][]byte{"long.txt": bytes.Repeat([]byte("x"), maxFileBytes+1)})
	findings, err := Archive(bytes.NewReader(imageData(t, layer, []byte("{}"))))
	require.ErrorContains(t, err, "line exceeds scanning window")
	require.Nil(t, findings)
}

func TestScanSecretStraddlingWindow(t *testing.T) {
	prefix := strings.Repeat("safe filler line\n", maxFileBytes/17)
	// Put the token at the boundary of the first read window.
	content := prefix + "TOKEN=" + testSecret + "\n" + strings.Repeat("safe filler line\n", 100)
	findings, err := Archive(bytes.NewReader(imageData(t, tarData(t, map[string][]byte{"boundary.txt": []byte(content)}), []byte("{}"))))
	require.NoError(t, err)
	require.Contains(t, findings, Finding{Path: "layer " + safePath("layer/layer.tar") + ":" + safePath("boundary.txt"), Rule: "github-fine-grained-pat", Line: maxFileBytes/17 + 1, SourcePath: "boundary.txt"})
}

func TestScanDockerSaveWhenAvailable(t *testing.T) {
	if testing.Short() {
		t.Skip("Docker integration test")
	}
	images, err := exec.Command("docker", "image", "ls", "--format", "{{.Repository}}:{{.Tag}}").Output()
	if err != nil {
		t.Skipf("Docker unavailable: %v", err)
	}
	var image string
	for candidate := range strings.SplitSeq(string(images), "\n") {
		if strings.HasPrefix(candidate, "alpine:") || strings.HasPrefix(candidate, "busybox:") {
			image = candidate
			break
		}
	}
	if image == "" {
		t.Skip("no small local Docker image; will not pull")
	}
	cmd := exec.Command("docker", "save", image)
	f, err := os.CreateTemp(t.TempDir(), "docker-save-*")
	require.NoError(t, err)
	cmd.Stdout = f
	require.NoError(t, cmd.Run())
	_, err = f.Seek(0, io.SeekStart)
	require.NoError(t, err)
	_, err = Archive(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

// The jobs use a barrier so a serial scanner cannot accidentally pass this test.
func TestScanJobsConcurrentAndBounded(t *testing.T) {
	const jobs = 24
	var active, peak atomic.Int32
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	result := make(chan struct {
		findings []Finding
		err      error
	}, 1)
	go func() {
		findings, err := scanJobs(jobs, func() (func(int) ([]Finding, error), error) {
			return func(i int) ([]Finding, error) {
				n := active.Add(1)
				defer active.Add(-1)
				for old := peak.Load(); n > old; old = peak.Load() {
					if peak.CompareAndSwap(old, n) {
						break
					}
				}
				if i < 8 {
					started <- struct{}{}
				}
				<-release
				return []Finding{{Line: i}}, nil
			}, nil
		})
		result <- struct {
			findings []Finding
			err      error
		}{findings, err}
	}()
	for range 8 {
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			require.FailNow(t, "eight jobs did not run concurrently")
		}
	}
	releaseOnce.Do(func() { close(release) })
	got := <-result
	require.NoError(t, got.err)
	require.EqualValues(t, 8, peak.Load())
	require.Len(t, got.findings, jobs)
	for i, f := range got.findings {
		require.Equal(t, i, f.Line, "results must follow input order")
	}
}

func TestScanJobsFailureDoesNotReturnClean(t *testing.T) {
	findings, err := scanJobs(24, func() (func(int) ([]Finding, error), error) {
		return func(i int) ([]Finding, error) {
			if i == 0 {
				return nil, errors.New("incomplete scan: bad layer")
			}
			return []Finding{{Line: i}}, nil
		}, nil
	})
	require.ErrorContains(t, err, "bad layer")
	require.Nil(t, findings)
}

func TestScanJobsInitializationFailure(t *testing.T) {
	findings, err := scanJobs(24, func() (func(int) ([]Finding, error), error) {
		return nil, errors.New("detector failed")
	})
	require.ErrorContains(t, err, "detector failed")
	require.Nil(t, findings)
}

func TestSharedBudgetConcurrent(t *testing.T) {
	b := &budget{remaining: 100}
	var wg sync.WaitGroup
	var succeeded atomic.Int32
	for range 40 {
		wg.Go(func() {
			if b.take(1) == nil {
				succeeded.Add(1)
			}
		})
	}
	wg.Wait()
	require.EqualValues(t, 40, succeeded.Load())
	require.EqualValues(t, 60, b.left())
	require.ErrorContains(t, b.take(61), "total byte limit exceeded")
	require.EqualValues(t, 60, b.left(), "failed reservations must not consume budget")
}

func TestScanArchiveEntryOrderWithMetadata(t *testing.T) {
	layer := tarData(t, map[string][]byte{"token": []byte(testSecret)})
	manifest := []byte(`[{"Config":"config.json","Layers":["layer.tar"]}]`)
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for _, item := range []struct {
		name string
		data []byte
	}{
		{"metadata.txt", []byte(testSecret)},
		{"layer.tar", layer},
		{"config.json", []byte(testSecret)},
		{"manifest.json", manifest},
	} {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: item.name, Mode: 0o600, Size: int64(len(item.data))}))
		_, err := tw.Write(item.data)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	for range 3 {
		findings, err := Archive(bytes.NewReader(archive.Bytes()))
		require.NoError(t, err)
		var paths []string
		for _, f := range findings {
			if f.Rule == "github-fine-grained-pat" {
				paths = append(paths, f.Path)
			}
		}
		require.Equal(t, []string{
			"metadata " + safePath("metadata.txt"),
			"layer " + safePath("layer.tar") + ":" + safePath("token"),
			"config " + safePath("config.json"),
		}, paths)
	}
}
