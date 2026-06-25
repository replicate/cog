package weightsource

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseHFURI(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		wantRepo    string
		wantRef     string
		wantErrSubs string
	}{
		{"basic", "hf://myorg/mymodel", "myorg/mymodel", "main", ""},
		{"with tag ref", "hf://myorg/mymodel@v1.0", "myorg/mymodel", "v1.0", ""},
		{"with sha ref", "hf://myorg/mymodel@abc123def456", "myorg/mymodel", "abc123def456", ""},
		{"with branch ref", "hf://myorg/mymodel@feature/branch", "myorg/mymodel", "feature/branch", ""},
		{"long scheme", "huggingface://myorg/mymodel", "myorg/mymodel", "main", ""},
		{"long scheme with ref", "huggingface://myorg/mymodel@v2", "myorg/mymodel", "v2", ""},

		{"not hf scheme", "file:///data", "", "", "not an hf:// URI"},
		{"empty after prefix", "hf://", "", "", "empty hf:// URI"},
		{"no slash", "hf://justarepo", "", "", "expected org/repo"},
		{"too many slashes", "hf://a/b/c", "", "", "expected org/repo"},
		{"empty org", "hf:///repo", "", "", "expected org/repo"},
		{"empty repo name", "hf://org/", "", "", "expected org/repo"},
		{"empty ref", "hf://org/repo@", "", "", "empty ref"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo, ref, err := parseHFURI(tc.uri)
			if tc.wantErrSubs != "" {
				assert.ErrorContains(t, err, tc.wantErrSubs)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantRepo, repo)
			assert.Equal(t, tc.wantRef, ref)
		})
	}
}

// hfMock is a minimal mock of the HuggingFace Hub API. It serves:
//   - GET /api/models/{repo}/revision/{ref}       → revision response
//   - GET /api/models/{repo}/tree/{ref}?recursive=true → tree listing
//   - GET /{repo}/resolve/{ref}/{path}             → file content
type hfMock struct {
	commitSHA string
	tree      []hfTreeEntry
	files     map[string]string // path → content
}

func (m *hfMock) handler() http.Handler {
	mux := http.NewServeMux()

	// Revision endpoint.
	mux.HandleFunc("/api/models/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.Contains(path, "/revision/") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(hfRevisionResponse{SHA: m.commitSHA})
			return
		}
		if strings.Contains(path, "/tree/") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(m.tree)
			return
		}
		http.NotFound(w, r)
	})

	// Resolve/download endpoint: /{repo}/resolve/{ref}/{path...}
	// Pattern: strip the leading slash, expect "org/repo/resolve/ref/path..."
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		// Find "resolve/" in the path to extract the file path.
		parts := strings.SplitN(r.URL.Path, "/resolve/", 2)
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		// parts[1] is "ref/file/path" — strip the ref prefix.
		_, filePath, ok := strings.Cut(parts[1], "/")
		if !ok {
			http.NotFound(w, r)
			return
		}
		content, ok := m.files[filePath]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(content))
	})

	return mux
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

func newTestHFSource(t *testing.T, serverURL, repo, ref string) *HFSource {
	t.Helper()
	if ref == "" {
		ref = "main"
	}
	return &HFSource{
		repo:    repo,
		ref:     ref,
		baseURL: mustParseURL(t, serverURL),
		token:   "",
		client:  newHFHTTPClient(),
	}
}

func TestHFSource_Inventory_LFSFiles(t *testing.T) {
	mock := &hfMock{
		commitSHA: "abc123def456abc123def456abc123def456abc123",
		tree: []hfTreeEntry{
			{
				Type: "file",
				Path: "model.safetensors",
				Size: 1000,
				LFS:  &hfLFSInfo{OID: "aabbccdd" + strings.Repeat("00", 28), Size: 1000},
			},
			{
				Type: "file",
				Path: "weights/shard-00.bin",
				Size: 2000,
				LFS:  &hfLFSInfo{OID: "11223344" + strings.Repeat("00", 28), Size: 2000},
			},
		},
		files: map[string]string{},
	}
	ts := httptest.NewServer(mock.handler())
	defer ts.Close()

	src := newTestHFSource(t, ts.URL, "myorg/mymodel", "main")
	inv, err := src.Inventory(t.Context())
	require.NoError(t, err)

	assert.Equal(t, Fingerprint("commit:abc123def456abc123def456abc123def456abc123"), inv.Fingerprint)
	assert.Equal(t, "commit", inv.Fingerprint.Scheme())

	require.Len(t, inv.Files, 2)
	// Files are sorted by path.
	assert.Equal(t, "model.safetensors", inv.Files[0].Path)
	assert.Equal(t, int64(1000), inv.Files[0].Size)
	assert.Equal(t, "sha256:aabbccdd"+strings.Repeat("00", 28), inv.Files[0].Digest)

	assert.Equal(t, "weights/shard-00.bin", inv.Files[1].Path)
	assert.Equal(t, int64(2000), inv.Files[1].Size)
	assert.Equal(t, "sha256:11223344"+strings.Repeat("00", 28), inv.Files[1].Digest)
}

func TestHFSource_Inventory_InlineFiles(t *testing.T) {
	mock := &hfMock{
		commitSHA: "ffff" + strings.Repeat("00", 18),
		tree: []hfTreeEntry{
			{Type: "file", Path: "config.json", Size: 13},
			{Type: "file", Path: "tokenizer.json", Size: 5},
		},
		files: map[string]string{
			"config.json":    `{"key":"val"}`,
			"tokenizer.json": "hello",
		},
	}
	ts := httptest.NewServer(mock.handler())
	defer ts.Close()

	src := newTestHFSource(t, ts.URL, "myorg/mymodel", "main")
	inv, err := src.Inventory(t.Context())
	require.NoError(t, err)

	require.Len(t, inv.Files, 2)
	// Both should have sha256 digests computed from content.
	for _, f := range inv.Files {
		assert.True(t, strings.HasPrefix(f.Digest, "sha256:"), "digest should start with sha256: for %s", f.Path)
		assert.Len(t, strings.TrimPrefix(f.Digest, "sha256:"), 64, "digest hex should be 64 chars for %s", f.Path)
	}

	// Verify sizes match actual content.
	assert.Equal(t, "config.json", inv.Files[0].Path)
	assert.Equal(t, int64(13), inv.Files[0].Size)
	assert.Equal(t, "tokenizer.json", inv.Files[1].Path)
	assert.Equal(t, int64(5), inv.Files[1].Size)
}

func TestHFSource_Inventory_MixedLFSAndInline(t *testing.T) {
	mock := &hfMock{
		commitSHA: "aaaa" + strings.Repeat("00", 18),
		tree: []hfTreeEntry{
			{
				Type: "file",
				Path: "model.bin",
				Size: 5000,
				LFS:  &hfLFSInfo{OID: "deadbeef" + strings.Repeat("00", 28), Size: 5000},
			},
			{Type: "file", Path: "config.json", Size: 4},
			{Type: "directory", Path: "subdir"}, // should be skipped
		},
		files: map[string]string{
			"config.json": "test",
		},
	}
	ts := httptest.NewServer(mock.handler())
	defer ts.Close()

	src := newTestHFSource(t, ts.URL, "myorg/mymodel", "main")
	inv, err := src.Inventory(t.Context())
	require.NoError(t, err)

	require.Len(t, inv.Files, 2, "directory entry should be excluded")
	assert.Equal(t, "config.json", inv.Files[0].Path)
	assert.Equal(t, "model.bin", inv.Files[1].Path)
	assert.Equal(t, "sha256:deadbeef"+strings.Repeat("00", 28), inv.Files[1].Digest)
}

func TestHFSource_Inventory_FilesAreSorted(t *testing.T) {
	mock := &hfMock{
		commitSHA: "bbbb" + strings.Repeat("00", 18),
		tree: []hfTreeEntry{
			{Type: "file", Path: "z.txt", Size: 1, LFS: &hfLFSInfo{OID: strings.Repeat("aa", 32), Size: 1}},
			{Type: "file", Path: "a.txt", Size: 1, LFS: &hfLFSInfo{OID: strings.Repeat("bb", 32), Size: 1}},
			{Type: "file", Path: "m.txt", Size: 1, LFS: &hfLFSInfo{OID: strings.Repeat("cc", 32), Size: 1}},
		},
		files: map[string]string{},
	}
	ts := httptest.NewServer(mock.handler())
	defer ts.Close()

	src := newTestHFSource(t, ts.URL, "myorg/mymodel", "main")
	inv, err := src.Inventory(t.Context())
	require.NoError(t, err)

	require.Len(t, inv.Files, 3)
	assert.Equal(t, "a.txt", inv.Files[0].Path)
	assert.Equal(t, "m.txt", inv.Files[1].Path)
	assert.Equal(t, "z.txt", inv.Files[2].Path)
}

func TestHFSource_Inventory_Stable(t *testing.T) {
	mock := &hfMock{
		commitSHA: "cccc" + strings.Repeat("00", 18),
		tree: []hfTreeEntry{
			{Type: "file", Path: "a.txt", Size: 5},
		},
		files: map[string]string{"a.txt": "hello"},
	}
	ts := httptest.NewServer(mock.handler())
	defer ts.Close()

	src := newTestHFSource(t, ts.URL, "myorg/mymodel", "main")

	inv1, err := src.Inventory(t.Context())
	require.NoError(t, err)
	inv2, err := src.Inventory(t.Context())
	require.NoError(t, err)

	assert.Equal(t, inv1.Fingerprint, inv2.Fingerprint, "fingerprint must be stable across calls")
	assert.Equal(t, inv1.Files, inv2.Files, "file list must be stable across calls")
}

func TestHFSource_Inventory_ContextCanceled(t *testing.T) {
	mock := &hfMock{
		commitSHA: "dddd" + strings.Repeat("00", 18),
		tree:      []hfTreeEntry{},
		files:     map[string]string{},
	}
	ts := httptest.NewServer(mock.handler())
	defer ts.Close()

	src := newTestHFSource(t, ts.URL, "myorg/mymodel", "main")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := src.Inventory(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestHFSource_Open(t *testing.T) {
	mock := &hfMock{
		commitSHA: "eeee" + strings.Repeat("00", 18),
		tree:      []hfTreeEntry{},
		files: map[string]string{
			"model.bin":    "model-bytes",
			"sub/data.bin": "nested-data",
		},
	}
	ts := httptest.NewServer(mock.handler())
	defer ts.Close()

	src := newTestHFSource(t, ts.URL, "myorg/mymodel", "main")

	t.Run("top level", func(t *testing.T) {
		rc, err := src.Open(t.Context(), "model.bin")
		require.NoError(t, err)
		defer rc.Close()
		b, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, "model-bytes", string(b))
	})

	t.Run("nested path", func(t *testing.T) {
		rc, err := src.Open(t.Context(), "sub/data.bin")
		require.NoError(t, err)
		defer rc.Close()
		b, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, "nested-data", string(b))
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := src.Open(t.Context(), "nope.bin")
		assert.ErrorContains(t, err, "404")
	})

	t.Run("canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := src.Open(ctx, "model.bin")
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// TestHFSource_Open_UsesResolvedRef verifies that Open uses the commit
// sha resolved during Inventory, not the original mutable ref. This
// prevents content drift between Inventory and Open.
func TestHFSource_Open_UsesResolvedRef(t *testing.T) {
	const resolvedSHA = "aaaa" + "bbbb" + "cccc" + "dddd" + "eeee" + "ffff" + "0000" + "1111" + "2222" + "3333"
	var resolveRef string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// Revision endpoint.
		if strings.Contains(path, "/revision/") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(hfRevisionResponse{SHA: resolvedSHA})
			return
		}
		// Tree endpoint.
		if strings.Contains(path, "/tree/") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]hfTreeEntry{
				{Type: "file", Path: "data.bin", Size: 3, LFS: &hfLFSInfo{OID: strings.Repeat("ab", 32), Size: 3}},
			})
			return
		}
		// Resolve/download endpoint — capture the ref used.
		if parts := strings.SplitN(path, "/resolve/", 2); len(parts) == 2 {
			ref, _, _ := strings.Cut(parts[1], "/")
			resolveRef = ref
			_, _ = w.Write([]byte("abc"))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	src := newTestHFSource(t, ts.URL, "myorg/mymodel", "main")

	// Before Inventory, Open falls back to the original ref.
	rc, err := src.Open(t.Context(), "data.bin")
	require.NoError(t, err)
	_ = rc.Close()
	assert.Equal(t, "main", resolveRef, "before Inventory, Open should use original ref")

	// Run Inventory to resolve the ref.
	_, err = src.Inventory(t.Context())
	require.NoError(t, err)

	// After Inventory, Open should use the resolved commit sha.
	rc, err = src.Open(t.Context(), "data.bin")
	require.NoError(t, err)
	_ = rc.Close()
	assert.Equal(t, resolvedSHA, resolveRef, "after Inventory, Open should use resolved commit sha")
}

func TestHFSource_AuthHeader(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(hfRevisionResponse{SHA: "abcd" + strings.Repeat("00", 18)})
	}))
	defer ts.Close()

	src := &HFSource{
		repo:    "org/repo",
		ref:     "main",
		baseURL: mustParseURL(t, ts.URL),
		token:   "hf_test_token_123",
		client:  newHFHTTPClient(),
	}

	_, err := src.resolveRef(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "Bearer hf_test_token_123", gotAuth)
}

func TestHFSource_NoAuthHeader_WhenNoToken(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(hfRevisionResponse{SHA: "abcd" + strings.Repeat("00", 18)})
	}))
	defer ts.Close()

	src := &HFSource{
		repo:    "org/repo",
		ref:     "main",
		baseURL: mustParseURL(t, ts.URL),
		token:   "",
		client:  newHFHTTPClient(),
	}

	_, err := src.resolveRef(t.Context())
	require.NoError(t, err)
	assert.Empty(t, gotAuth)
}

func TestHFSource_HTTPErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantSub    string
	}{
		{"401 auth", http.StatusUnauthorized, "HF_TOKEN"},
		{"403 forbidden", http.StatusForbidden, "permissions"},
		{"404 not found", http.StatusNotFound, "HTTP 404"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.statusCode)
			}))
			defer ts.Close()

			src := newTestHFSource(t, ts.URL, "org/repo", "main")
			_, err := src.Inventory(t.Context())
			assert.ErrorContains(t, err, tc.wantSub)
		})
	}
}

func TestHFSource_HTTP500_Retries(t *testing.T) {
	var attempts atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n <= 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Succeed on second attempt.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(hfRevisionResponse{SHA: "abcd" + strings.Repeat("00", 18)})
	}))
	defer ts.Close()

	src := newTestHFSource(t, ts.URL, "org/repo", "main")
	sha, err := src.resolveRef(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "abcd"+strings.Repeat("00", 18), sha)
	assert.Equal(t, int32(2), attempts.Load(), "should have retried once")
}

func TestHFSource_Open_EscapesPathComponents(t *testing.T) {
	// Verify that file paths with special characters are properly
	// URL-escaped when sent to the server. Go's net/http server
	// decodes percent-encoding in r.URL.Path, so we capture the raw
	// request line from the underlying connection via RequestURI.
	var gotRequestURI string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequestURI = r.RequestURI
		_, _ = w.Write([]byte("data"))
	}))
	defer ts.Close()

	src := newTestHFSource(t, ts.URL, "org/repo", "main")

	// File path with spaces and special chars.
	rc, err := src.Open(t.Context(), "sub dir/model file.bin")
	require.NoError(t, err)
	_ = rc.Close()

	assert.Contains(t, gotRequestURI, "sub%20dir/model%20file.bin",
		"path components should be individually escaped")
}

func TestHFSource_BuildURL(t *testing.T) {
	src := &HFSource{baseURL: mustParseURL(t, "https://huggingface.co")}
	tests := []struct {
		name     string
		segments []string
		want     string
	}{
		{"simple", []string{"api", "models", "org/repo", "revision", "main"}, "https://huggingface.co/api/models/org/repo/revision/main"},
		{"cleans dots", []string{"api", "models", "org/repo", "revision", ".."}, "https://huggingface.co/api/models/org/repo"},
		{"cleans double slash", []string{"api", "models", "org/repo", "", "tree", "main"}, "https://huggingface.co/api/models/org/repo/tree/main"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := src.buildURL(tc.segments...)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestHFSource_BuildURLWithQuery(t *testing.T) {
	src := &HFSource{baseURL: mustParseURL(t, "https://huggingface.co")}
	got := src.buildURLWithQuery("recursive=true", "api", "models", "org/repo", "tree", "abc123")
	assert.Equal(t, "https://huggingface.co/api/models/org/repo/tree/abc123?recursive=true", got)
}

func TestFor_HFSchemes(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		wantRepo string
		wantRef  string
	}{
		{"hf short", "hf://org/repo", "org/repo", "main"},
		{"hf short with ref", "hf://org/repo@v1.0", "org/repo", "v1.0"},
		{"huggingface long", "huggingface://org/repo", "org/repo", "main"},
		{"huggingface long with ref", "huggingface://org/repo@v2.0", "org/repo", "v2.0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src, err := For(tc.uri, "")
			require.NoError(t, err)
			hf, ok := src.(*HFSource)
			require.True(t, ok, "expected *HFSource, got %T", src)
			assert.Equal(t, tc.wantRepo, hf.repo)
			assert.Equal(t, tc.wantRef, hf.ref)
		})
	}
}
