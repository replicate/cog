package weightsource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPSource_Valid(t *testing.T) {
	src, err := NewHTTPSource("https://example.com/models/model.pth")
	require.NoError(t, err)
	require.NotNil(t, src)
	assert.Equal(t, "model.pth", src.filename)
}

func TestNewHTTPSource_HTTP(t *testing.T) {
	src, err := NewHTTPSource("http://example.com/model.bin")
	require.NoError(t, err)
	assert.Equal(t, "model.bin", src.filename)
}

func TestNewHTTPSource_NoFilename(t *testing.T) {
	_, err := NewHTTPSource("https://example.com/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no filename")
}

func TestNewHTTPSource_InvalidURL(t *testing.T) {
	_, err := NewHTTPSource("://bad")
	require.Error(t, err)
}

func TestNewHTTPSource_WrongScheme(t *testing.T) {
	_, err := NewHTTPSource("ftp://example.com/file.bin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected https or http")
}

func TestNewHTTPSource_RejectsCredentials(t *testing.T) {
	tests := []string{
		"https://user:pass@example.com/model.pth",
		"https://token@example.com/model.pth",
		"http://user:pass@example.com/model.bin",
	}
	for _, uri := range tests {
		t.Run(uri, func(t *testing.T) {
			_, err := NewHTTPSource(uri)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must not embed credentials")
		})
	}
}

func TestNormalizeHTTPURI_RejectsCredentials(t *testing.T) {
	_, err := normalizeHTTPURI("https://token:secret@github.com/org/repo/releases/download/v1/model.pth")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not embed credentials")
}

func TestHTTPSource_Inventory_WithETag(t *testing.T) {
	body := []byte("fake model weights")
	h := sha256.Sum256(body)
	expectedDigest := "sha256:" + hex.EncodeToString(h[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"abc123"`)
		w.Header().Set("Content-Length", "18")
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	src, err := NewHTTPSource(srv.URL + "/model.pth")
	require.NoError(t, err)

	inv, err := src.Inventory(context.Background())
	require.NoError(t, err)

	require.Len(t, inv.Files, 1)
	assert.Equal(t, "model.pth", inv.Files[0].Path)
	assert.Equal(t, int64(18), inv.Files[0].Size)
	assert.Equal(t, expectedDigest, inv.Files[0].Digest, "digest must always be computed")
	assert.Equal(t, Fingerprint(`etag:"abc123"`), inv.Fingerprint, "ETag used as fingerprint")
}

func TestHTTPSource_Inventory_NoETag_FallsBackToSHA256(t *testing.T) {
	body := []byte("fake model weights for hash")
	h := sha256.Sum256(body)
	expectedDigest := "sha256:" + hex.EncodeToString(h[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No ETag header.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	src, err := NewHTTPSource(srv.URL + "/weights.bin")
	require.NoError(t, err)

	inv, err := src.Inventory(context.Background())
	require.NoError(t, err)

	require.Len(t, inv.Files, 1)
	assert.Equal(t, "weights.bin", inv.Files[0].Path)
	assert.Equal(t, int64(len(body)), inv.Files[0].Size)
	assert.Equal(t, expectedDigest, inv.Files[0].Digest)
	assert.Equal(t, Fingerprint(expectedDigest), inv.Fingerprint)
}

func TestHTTPSource_Inventory_WeakETag_FallsBackToSHA256(t *testing.T) {
	body := []byte("fake model weights")
	h := sha256.Sum256(body)
	expectedDigest := "sha256:" + hex.EncodeToString(h[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `W/"weak-etag-123"`)
		w.Header().Set("Content-Length", "18")
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	src, err := NewHTTPSource(srv.URL + "/model.pth")
	require.NoError(t, err)

	inv, err := src.Inventory(t.Context())
	require.NoError(t, err)

	require.Len(t, inv.Files, 1)
	assert.Equal(t, expectedDigest, inv.Files[0].Digest)
	assert.Equal(t, Fingerprint(expectedDigest), inv.Fingerprint,
		"weak ETag must be ignored; fingerprint should fall back to sha256")
}

func TestHTTPSource_Open(t *testing.T) {
	body := []byte("model data here")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	src, err := NewHTTPSource(srv.URL + "/model.pth")
	require.NoError(t, err)

	rc, err := src.Open(context.Background(), "model.pth")
	require.NoError(t, err)
	defer rc.Close()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

func TestHTTPSource_Open_FollowsRedirects(t *testing.T) {
	body := []byte("redirected content")

	// CDN server that serves the actual content.
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer cdn.Close()

	// Origin server that 302-redirects to CDN (like GitHub releases).
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, cdn.URL+"/actual-file.pth", http.StatusFound)
	}))
	defer origin.Close()

	src, err := NewHTTPSource(origin.URL + "/model.pth")
	require.NoError(t, err)

	rc, err := src.Open(context.Background(), "model.pth")
	require.NoError(t, err)
	defer rc.Close()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

func TestHTTPSource_Inventory_HeadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	src, err := NewHTTPSource(srv.URL + "/missing.pth")
	require.NoError(t, err)

	_, err = src.Inventory(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestHTTPSource_Open_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	src, err := NewHTTPSource(srv.URL + "/forbidden.pth")
	require.NoError(t, err)

	_, err = src.Open(context.Background(), "forbidden.pth")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

func TestHTTPSource_Inventory_ContextCancelled(t *testing.T) {
	src, err := NewHTTPSource("https://example.com/model.pth")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = src.Inventory(ctx)
	require.Error(t, err)
}

func TestNormalizeHTTPURI(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: "https://github.com/xinntao/Real-ESRGAN/releases/download/v0.1.0/RealESRGAN_x4plus.pth",
			want:  "https://github.com/xinntao/Real-ESRGAN/releases/download/v0.1.0/RealESRGAN_x4plus.pth",
		},
		{
			input: "HTTP://Example.Com/file.bin",
			want:  "http://Example.Com/file.bin",
		},
		{
			input: "HTTPS://example.com/path/to/model.pth?token=abc",
			want:  "https://example.com/path/to/model.pth?token=abc",
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := normalizeHTTPURI(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFor_HTTPScheme(t *testing.T) {
	src, err := For("https://example.com/model.pth", "")
	require.NoError(t, err)
	_, ok := src.(*HTTPSource)
	require.True(t, ok, "expected *HTTPSource")
}

func TestNormalizeURI_HTTPScheme(t *testing.T) {
	got, err := NormalizeURI("https://example.com/model.pth")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/model.pth", got)
}
