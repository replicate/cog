package weightsource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/hashicorp/go-retryablehttp"

	"github.com/replicate/cog/pkg/util/console"
)

// HTTPSource is the Source implementation for https:// and http:// URIs.
//
// Each HTTPSource represents a single remote file. The filename is derived
// from the URL path basename (e.g. "RealESRGAN_x4plus.pth"). This
// supports GitHub Releases, S3 presigned URLs, university file servers,
// ONNX Model Zoo, and any plain HTTP download.
//
// Fingerprint strategy:
//   - HEAD request: use a strong ETag if present → "etag:<value>"
//   - No usable ETag: fall back to GET + sha256 hash → "sha256:<hex>"
//
// ETag is treated as a *cache hint*, not a content identity. A change
// in ETag triggers re-verification; a stable ETag short-circuits it.
// Two HTTP sources with identical content but different ETags will
// re-import unnecessarily but produce the same final artifact — the
// worst case is wasted work, never wrong content. Weak ETags
// (W/-prefixed, RFC 7232 §2.3) explicitly do not promise content
// identity, so we ignore them and fall through to sha256.
type HTTPSource struct {
	uri      string
	parsed   *url.URL
	filename string
	client   *http.Client
}

// NewHTTPSource constructs an HTTPSource bound to the given URL.
// It validates the URL parses correctly, has a non-empty path
// component, and does not embed credentials. No network calls are
// made at construction time.
//
// URIs with userinfo (https://user:pass@host/...) are rejected: the
// URI is recorded verbatim in weights.lock, which is checked into
// git, so embedded credentials would leak. Use a separate auth
// mechanism (Authorization header support is on the roadmap).
func NewHTTPSource(uri string) (*HTTPSource, error) {
	parsed, err := parseHTTPURI(uri)
	if err != nil {
		return nil, err
	}
	filename := path.Base(parsed.Path)
	if filename == "" || filename == "." || filename == "/" {
		return nil, fmt.Errorf("HTTP URI %q has no filename in path", uri)
	}
	return &HTTPSource{
		uri:      uri,
		parsed:   parsed,
		filename: filename,
		client:   newHTTPSourceClient(),
	}, nil
}

// parseHTTPURI parses uri and rejects anything outside the http/https
// scheme set or with embedded credentials. Centralizes the rules so
// that NewHTTPSource and normalizeHTTPURI cannot drift apart. The
// "no userinfo" rule exists because the URI lands in weights.lock —
// see NewHTTPSource for the full rationale.
func parseHTTPURI(uri string) (*url.URL, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("invalid HTTP URI %q: %w", uri, err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, fmt.Errorf("expected https or http scheme, got %q", parsed.Scheme)
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("HTTP URI must not embed credentials (user:pass@host)")
	}
	return parsed, nil
}

// newHTTPSourceClient returns an *http.Client with retry behavior
// matching HFSource: retries on 5xx, 429, and network errors.
func newHTTPSourceClient() *http.Client {
	rc := retryablehttp.NewClient()
	rc.RetryMax = 3
	rc.Logger = nil
	rc.CheckRetry = httpSourceCheckRetry
	return rc.StandardClient()
}

// httpSourceCheckRetry retries on 5xx and 429 but treats other status
// codes as permanent.
func httpSourceCheckRetry(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if err != nil {
		return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return true, nil
	}
	if resp.StatusCode >= 500 {
		return true, nil
	}
	return false, nil
}

// normalizeHTTPURI runs the same validation as NewHTTPSource (scheme
// allowlist, no userinfo) and returns the canonical string form.
// url.Parse already lowercases the scheme per RFC 3986, so no
// additional case normalization is needed.
func normalizeHTTPURI(uri string) (string, error) {
	parsed, err := parseHTTPURI(uri)
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
}

// Inventory resolves the remote file's metadata.
//
// The inventory contains exactly one file: the URL's path basename.
// A HEAD request is tried first for ETag (used as fingerprint for cheap
// change detection) and Content-Length. The file is then GET+sha256-hashed
// to produce a real content digest — every InventoryFile must have a
// non-empty Digest for the downstream store and packer to work correctly.
//
// When no ETag is available, the sha256 digest doubles as the fingerprint.
func (s *HTTPSource) Inventory(ctx context.Context) (Inventory, error) {
	if err := ctx.Err(); err != nil {
		return Inventory{}, err
	}

	console.Debugf("http: resolving %s", s.uri)

	// HEAD for ETag and Content-Length.
	etag, headSize := s.headMetadata(ctx)

	// GET + sha256 to produce the content digest. This is always needed
	// because the store requires a real digest for content-addressing.
	digest, actualSize, err := s.hashFile(ctx)
	if err != nil {
		return Inventory{}, err
	}

	size := actualSize
	if size <= 0 && headSize > 0 {
		size = headSize
	}

	// Use ETag as fingerprint when available (cheap change detection
	// without re-hashing). Fall back to the content digest.
	fp := Fingerprint(digest)
	if etag != "" {
		fp = Fingerprint("etag:" + etag)
	}

	console.Debugf("http: %s — size=%d, fingerprint=%s", s.filename, size, fp)
	return Inventory{
		Files: []InventoryFile{{
			Path:   s.filename,
			Size:   size,
			Digest: digest,
		}},
		Fingerprint: fp,
	}, nil
}

// headMetadata does a best-effort HEAD request to extract a strong
// ETag and Content-Length. Returns empty/zero on any failure — the
// caller falls back to GET+hash. Weak ETags (W/"...") are dropped;
// see HTTPSource doc for why.
//
// Failures are logged at debug level to aid retro debugging without
// surfacing as user-visible errors.
func (s *HTTPSource) headMetadata(ctx context.Context) (etag string, size int64) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, s.uri, nil)
	if err != nil {
		console.Debugf("http: HEAD %s: build request failed: %v", s.uri, err)
		return "", -1
	}
	resp, err := s.client.Do(req) //nolint:gosec // URL from parsed http:// URI
	if err != nil {
		console.Debugf("http: HEAD %s: %v", s.uri, err)
		return "", -1
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		console.Debugf("http: HEAD %s returned HTTP %d (will GET+hash)", s.uri, resp.StatusCode)
		return "", -1
	}
	rawETag := resp.Header.Get("ETag")
	if strings.HasPrefix(rawETag, "W/") {
		console.Debugf("http: HEAD %s returned weak ETag %s (ignored)", s.uri, rawETag)
		rawETag = ""
	}
	return rawETag, resp.ContentLength
}

// Open returns a reader that streams the file from the URL. Go's
// http.Client follows redirects by default, handling GitHub's
// 302→CDN pattern transparently.
//
// Open does NOT verify the response body against the inventory digest;
// the store performs that verification on PutFile (see store.PutFile).
// If a mutable URL serves different bytes between Inventory() and
// Open(), the digest mismatch will surface during ingress, not here.
//
// For HTTP sources without ETag and without Content-Length the file
// is hashed once during Inventory() and then streamed again on Open()
// — a 2x bandwidth cost. We accept that today rather than caching to
// disk between phases; for multi-GB downloads from sources like that,
// expect a re-download on every import.
func (s *HTTPSource) Open(ctx context.Context, _ string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.uri, nil)
	if err != nil {
		return nil, fmt.Errorf("create GET request: %w", err)
	}

	resp, err := s.client.Do(req) //nolint:gosec // URL from parsed http:// URI
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", s.uri, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("GET %s returned HTTP %d", s.uri, resp.StatusCode)
	}

	return resp.Body, nil
}

// hashFile does a full GET and sha256-hashes the response body,
// returning the digest and actual byte count.
func (s *HTTPSource) hashFile(ctx context.Context) (digest string, size int64, err error) {
	rc, err := s.Open(ctx, s.filename)
	if err != nil {
		return "", 0, err
	}
	defer rc.Close() //nolint:errcheck

	h := sha256.New()
	n, err := io.Copy(h, rc)
	if err != nil {
		return "", 0, fmt.Errorf("hash %s: %w", s.uri, err)
	}

	return "sha256:" + hex.EncodeToString(h.Sum(nil)), n, nil
}
