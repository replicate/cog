package weightsource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/hashicorp/go-retryablehttp"
	"golang.org/x/sync/errgroup"
)

// HFScheme is the short URI scheme for HuggingFace Hub sources.
const HFScheme = "hf"

// HFSchemeLong is the long-form URI scheme alias for HuggingFace Hub.
const HFSchemeLong = "huggingface"

// hfDefaultBaseURL is the base URL for the HuggingFace Hub API. It can
// be overridden via the HF_ENDPOINT env var (useful for testing and
// mirrors).
const hfDefaultBaseURL = "https://huggingface.co"

// hfInlineDigestConcurrency controls how many inline (non-LFS) files
// are fetched concurrently during Inventory to compute their sha256.
const hfInlineDigestConcurrency = 4

// HFSource is the Source implementation for hf:// URIs.
//
// URI forms:
//
//	hf://org/repo         — follows main branch
//	hf://org/repo@ref     — ref is a branch, tag, or 40-char commit sha
//
// The source resolves the ref to a full commit sha at Inventory time and
// uses that pinned sha for all subsequent Open calls. Callers must call
// Inventory before Open to ensure content is pinned to a specific commit.
type HFSource struct {
	repo        string // "org/repo"
	ref         string // user-provided ref (branch, tag, or sha); defaults to "main"
	resolvedRef string // full commit sha, set by Inventory; Open uses this when non-empty
	baseURL     string
	token       string
	client      *http.Client
}

// NewHFSource constructs an HFSource bound to the given hf:// URI.
// It parses the URI and looks up auth from env vars but does not make
// any network calls — validation happens at Inventory time.
func NewHFSource(uri string) (*HFSource, error) {
	repo, ref, err := parseHFURI(uri)
	if err != nil {
		return nil, err
	}

	baseURL := os.Getenv("HF_ENDPOINT")
	if baseURL == "" {
		baseURL = hfDefaultBaseURL
	}

	token := os.Getenv("HF_TOKEN")
	if token == "" {
		token = os.Getenv("HUGGING_FACE_HUB_TOKEN")
	}

	return &HFSource{
		repo:    repo,
		ref:     ref,
		baseURL: baseURL,
		token:   token,
		client:  newHFHTTPClient(),
	}, nil
}

// newHFHTTPClient returns a standard *http.Client whose transport
// retries on 5xx, 429, and network errors with exponential backoff.
// The retryable behavior is provided by go-retryablehttp configured as
// a transport — callers use the standard http.Client API.
func newHFHTTPClient() *http.Client {
	rc := retryablehttp.NewClient()
	rc.RetryMax = 3
	rc.Logger = nil // Silence default logger; errors surface via return values.
	rc.CheckRetry = hfCheckRetry
	return rc.StandardClient()
}

// hfCheckRetry is a retryablehttp.CheckRetry that retries on 5xx and
// 429 but treats other 4xx status codes as permanent failures.
func hfCheckRetry(ctx context.Context, resp *http.Response, err error) (bool, error) {
	// Network errors: let the default policy decide (retries them).
	if err != nil {
		return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
	}
	// 429 Too Many Requests: retry.
	if resp.StatusCode == http.StatusTooManyRequests {
		return true, nil
	}
	// 5xx: retry.
	if resp.StatusCode >= 500 {
		return true, nil
	}
	// Everything else (2xx, 3xx, 4xx other than 429): do not retry.
	return false, nil
}

// normalizeHFURI returns the canonical hf:// form of an HF URI. It
// validates the URI, canonicalizes huggingface:// to hf://, and
// preserves the @ref suffix if present. The default ref ("main") is
// not appended — it is implied.
func normalizeHFURI(uri string) (string, error) {
	repo, ref, err := parseHFURI(uri)
	if err != nil {
		return "", err
	}
	if ref == "main" {
		return "hf://" + repo, nil
	}
	return "hf://" + repo + "@" + ref, nil
}

// parseHFURI parses "hf://org/repo" or "huggingface://org/repo" (with
// optional @ref suffix) and returns the repo and ref components.
func parseHFURI(uri string) (repo, ref string, err error) {
	var rest string
	switch {
	case strings.HasPrefix(uri, "hf://"):
		rest = strings.TrimPrefix(uri, "hf://")
	case strings.HasPrefix(uri, "huggingface://"):
		rest = strings.TrimPrefix(uri, "huggingface://")
	default:
		return "", "", fmt.Errorf("not an hf:// URI: %q", uri)
	}
	if rest == "" {
		return "", "", fmt.Errorf("empty hf:// URI")
	}

	// Split off @ref suffix if present.
	repo = rest
	if idx := strings.LastIndex(rest, "@"); idx > 0 {
		repo = rest[:idx]
		ref = rest[idx+1:]
		if ref == "" {
			return "", "", fmt.Errorf("empty ref in hf:// URI: %q", uri)
		}
	}

	// Validate repo has exactly one slash (org/name).
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid hf:// repo %q: expected org/repo", repo)
	}

	if ref == "" {
		ref = "main"
	}
	return repo, ref, nil
}

// Inventory calls the HuggingFace Hub API to list files and resolve the
// ref to a pinned commit sha. For LFS/xet-tracked files the sha256
// digest comes from the API response (free, no download). Inline files
// (small, git-tracked) are fetched and hashed.
//
// The fingerprint is "commit:<full-sha>".
func (s *HFSource) Inventory(ctx context.Context) (Inventory, error) {
	if err := ctx.Err(); err != nil {
		return Inventory{}, err
	}

	// 1. Resolve ref → commit sha and pin for subsequent Open calls.
	commitSHA, err := s.resolveRef(ctx)
	if err != nil {
		return Inventory{}, fmt.Errorf("resolve ref %q for %s: %w", s.ref, s.repo, err)
	}
	s.resolvedRef = commitSHA

	// 2. Fetch the recursive tree listing at the resolved commit.
	entries, err := s.listTree(ctx, commitSHA)
	if err != nil {
		return Inventory{}, fmt.Errorf("list tree for %s@%s: %w", s.repo, commitSHA, err)
	}

	// 3. Build inventory files. LFS entries have digests already;
	//    inline entries need to be fetched and hashed.
	files, err := s.buildInventoryFiles(ctx, commitSHA, entries)
	if err != nil {
		return Inventory{}, err
	}

	sortInventoryFiles(files)

	return Inventory{
		Files:       files,
		Fingerprint: Fingerprint("commit:" + commitSHA),
	}, nil
}

// Open returns a reader that streams the file from the HuggingFace CDN.
// It follows the redirect from the resolve endpoint to the appropriate
// backend (LFS CDN, xet cas-bridge, or inline git blob).
//
// Open uses the commit sha resolved during Inventory, so file content
// is pinned to the same revision that was inventoried. If Inventory has
// not been called, Open falls back to the original ref.
func (s *HFSource) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ref := s.resolvedRef
	if ref == "" {
		ref = s.ref
	}
	return s.fetchFile(ctx, ref, path)
}

// hfRevisionResponse is the subset of the /api/models/{repo}/revision/{ref}
// response we need.
type hfRevisionResponse struct {
	SHA string `json:"sha"`
}

// resolveRef calls the Hub API to resolve a ref (branch/tag/sha) to the
// full 40-char commit sha.
func (s *HFSource) resolveRef(ctx context.Context) (string, error) {
	url := fmt.Sprintf("%s/api/models/%s/revision/%s", s.baseURL, s.repo, s.ref)

	body, err := s.doGet(ctx, url)
	if err != nil {
		return "", err
	}
	defer body.Close()

	var resp hfRevisionResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return "", fmt.Errorf("decode revision response: %w", err)
	}
	if resp.SHA == "" {
		return "", fmt.Errorf("empty sha in revision response for %s@%s", s.repo, s.ref)
	}
	return resp.SHA, nil
}

// hfTreeEntry represents one file in the recursive tree listing.
type hfTreeEntry struct {
	Type string     `json:"type"`
	Path string     `json:"path"`
	Size int64      `json:"size"`
	LFS  *hfLFSInfo `json:"lfs,omitempty"`
}

// hfLFSInfo carries the LFS pointer metadata. Present for both LFS and
// xet-tracked files (the Hub API exposes sha256 in the lfs field for
// both).
type hfLFSInfo struct {
	OID  string `json:"oid"`  // sha256 of the full file
	Size int64  `json:"size"`
}

// listTree fetches the recursive tree listing at the given commit sha.
// NOTE: the HF Hub API paginates large repos. This implementation does
// not follow pagination yet — repos with very many files may return an
// incomplete listing. A follow-up should add cursor-based pagination.
func (s *HFSource) listTree(ctx context.Context, commitSHA string) ([]hfTreeEntry, error) {
	url := fmt.Sprintf("%s/api/models/%s/tree/%s?recursive=true", s.baseURL, s.repo, commitSHA)

	body, err := s.doGet(ctx, url)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var entries []hfTreeEntry
	if err := json.NewDecoder(body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode tree response: %w", err)
	}
	return entries, nil
}

// buildInventoryFiles converts tree entries into InventoryFiles. LFS
// entries use the lfs.oid as the digest directly. Inline entries are
// fetched and hashed with bounded concurrency.
func (s *HFSource) buildInventoryFiles(ctx context.Context, commitSHA string, entries []hfTreeEntry) ([]InventoryFile, error) {
	var lfsFiles []InventoryFile
	var inlineEntries []hfTreeEntry

	for _, e := range entries {
		if e.Type != "file" {
			continue
		}
		if e.LFS != nil && e.LFS.OID != "" {
			lfsFiles = append(lfsFiles, InventoryFile{
				Path:   e.Path,
				Size:   e.LFS.Size,
				Digest: "sha256:" + e.LFS.OID,
			})
		} else {
			inlineEntries = append(inlineEntries, e)
		}
	}

	// Hash inline files with bounded concurrency.
	inlineFiles, err := s.hashInlineFiles(ctx, commitSHA, inlineEntries)
	if err != nil {
		return nil, err
	}

	return append(lfsFiles, inlineFiles...), nil
}

// hashInlineFiles fetches and hashes inline (non-LFS) files with
// bounded concurrency via errgroup.
func (s *HFSource) hashInlineFiles(ctx context.Context, commitSHA string, entries []hfTreeEntry) ([]InventoryFile, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	files := make([]InventoryFile, len(entries))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(hfInlineDigestConcurrency)

	for i, e := range entries {
		g.Go(func() error {
			f, err := s.hashOneInlineFile(ctx, commitSHA, e)
			if err != nil {
				return err
			}
			files[i] = f
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return files, nil
}

// hashOneInlineFile fetches one inline file from the resolve endpoint
// and computes its sha256 while reading.
func (s *HFSource) hashOneInlineFile(ctx context.Context, commitSHA string, entry hfTreeEntry) (InventoryFile, error) {
	rc, err := s.fetchFile(ctx, commitSHA, entry.Path)
	if err != nil {
		return InventoryFile{}, fmt.Errorf("fetch inline file %s: %w", entry.Path, err)
	}
	defer rc.Close()

	h := sha256.New()
	n, err := io.Copy(h, rc)
	if err != nil {
		return InventoryFile{}, fmt.Errorf("hash inline file %s: %w", entry.Path, err)
	}

	return InventoryFile{
		Path:   entry.Path,
		Size:   n,
		Digest: "sha256:" + hex.EncodeToString(h.Sum(nil)),
	}, nil
}

// fetchFile streams one file from the resolve endpoint. The endpoint
// issues a 302 to the appropriate backend (LFS CDN, xet, or inline).
func (s *HFSource) fetchFile(ctx context.Context, ref, path string) (io.ReadCloser, error) {
	url := fmt.Sprintf("%s/%s/resolve/%s/%s", s.baseURL, s.repo, ref, path)
	return s.doGet(ctx, url)
}

// doGet performs an HTTP GET with retries (via the retrying transport)
// and returns the response body. The caller must close the body.
//
// Non-retryable 4xx responses are translated into specific errors:
//   - 401 → auth hint
//   - 403 → permissions hint
//   - 404 → not-found
//   - others → raw status + snippet of body
func (s *HFSource) doGet(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}

	resp, err := s.client.Do(req) //nolint:gosec // G704: URL is constructed from parsed hf:// URI components, not arbitrary user input
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", url, err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.Body, nil
	}

	// Non-2xx: read a snippet of the body for diagnostics, then close.
	errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	_ = resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("authentication failed for %s (HTTP 401): set HF_TOKEN or HUGGING_FACE_HUB_TOKEN", url)
	case http.StatusForbidden:
		return nil, fmt.Errorf("access denied for %s (HTTP 403): check repo visibility and token permissions", url)
	case http.StatusNotFound:
		return nil, fmt.Errorf("not found: %s (HTTP 404)", url)
	default:
		return nil, fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, url, string(errBody))
	}
}
