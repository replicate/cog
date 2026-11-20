// Package scan scans every file in every layer of a Docker save archive.
package scan

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	// The official github.com/gitleaks/gitleaks repository retains this module path.
	"github.com/zricethezav/gitleaks/v8/detect"
)

const (
	manifestFilename = "manifest.json"
	maxFileBytes     = 8 << 20
	maxTotalBytes    = 8 << 30
	maxLayerBytes    = maxTotalBytes
	maxEntries       = 1000000
)

// Finding deliberately omits the secret, matching text, and original filename.
// Paths are hashes because filenames themselves can contain credentials.
type Finding struct {
	Path       string `json:"path"`
	Rule       string `json:"rule"`
	Line       int    `json:"line"`
	SourcePath string `json:"-"`
}

type budget struct {
	mu        sync.Mutex
	remaining int64
}

func (b *budget) left() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.remaining
}

func (b *budget) take(n int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n > b.remaining {
		return errors.New("incomplete scan: total byte limit exceeded")
	}
	b.remaining -= n
	return nil
}

// Gitleaks loads its default config through process-global Viper state.
var detectorConfigMu sync.Mutex

func newDetector() (*detect.Detector, error) {
	detectorConfigMu.Lock()
	defer detectorConfigMu.Unlock()
	d, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		return nil, err
	}
	d.Redact = 100
	return d, nil
}

// scanJobs keeps detector state local to each worker and stores results by job
// index. On any failure it stops dispatching, waits for workers, and discards
// partial findings rather than reporting an incomplete scan as clean.
func scanJobs(count int, newWorker func() (func(int) ([]Finding, error), error)) ([]Finding, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jobs := make(chan int)
	failures := make(chan error, 1)
	var once sync.Once
	fail := func(err error) {
		once.Do(func() {
			failures <- err
			cancel()
		})
	}
	results := make([][]Finding, count)
	var wg sync.WaitGroup
	for range min(count, 8) {
		wg.Go(func() {
			scan, err := newWorker()
			if err != nil {
				fail(err)
				return
			}
			for {
				select {
				case <-ctx.Done():
					return
				case i, ok := <-jobs:
					if !ok {
						return
					}
					findings, err := scan(i)
					if err != nil {
						fail(err)
						return
					}
					results[i] = findings
				}
			}
		})
	}
dispatch:
	for i := range count {
		select {
		case <-ctx.Done():
			break dispatch
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()
	select {
	case err := <-failures:
		return nil, err
	default:
	}
	var findings []Finding
	for _, result := range results {
		findings = append(findings, result...)
	}
	return findings, nil
}

func safePath(s string) string {
	hash := sha256.Sum256([]byte(s))
	return fmt.Sprintf("sha256:%x", hash[:8])
}

// Archive scans a Docker save stream. Any skipped, malformed, or oversized input
// fails closed: findings from an incomplete scan are never returned as a clean result.
func Archive(r io.Reader) ([]Finding, error) {
	detector, err := newDetector()
	if err != nil {
		return nil, fmt.Errorf("initialize scanner: %w", err)
	}

	// Spool entries until manifest.json identifies the config and layer paths.
	// Docker save may put content-addressed blobs before the manifest.
	spool, err := os.CreateTemp("", "cog-scan-*")
	if err != nil {
		return nil, fmt.Errorf("incomplete scan: create archive spool: %w", err)
	}
	defer func() {
		_ = spool.Close()
		_ = os.Remove(spool.Name()) //nolint:gosec // path returned by CreateTemp, not archive input
	}()

	type entry struct {
		name         string
		offset, size int64
	}

	var (
		archiveBudget = &budget{remaining: maxTotalBytes}
		layerBudget   = &budget{remaining: maxTotalBytes}
		outer         = &countReader{r: r, b: archiveBudget}
		tr            = tar.NewReader(outer)
		seen          = map[string]entry{}
		order         []entry
		manifest      []struct {
			Config string
			Layers []string
		}
		findings []Finding
		entries  int
	)

	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, fmt.Errorf("incomplete scan: invalid image archive: %w", e)
		}
		entries++
		if entries > maxEntries {
			return nil, errors.New("incomplete scan: too many archive entries")
		}
		if h.Size < 0 || h.Size > maxTotalBytes {
			return nil, errors.New("incomplete scan: image archive byte limit exceeded")
		}
		if e := scanHeader(detector, h, "image header "+safePath(h.Name), &findings); e != nil {
			return nil, e
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		if _, ok := seen[h.Name]; ok {
			return nil, errors.New("incomplete scan: duplicate archive entry")
		}
		if h.Name == manifestFilename {
			data, e := readFile(tr, h.Size)
			if e != nil {
				return nil, e
			}
			appendFindings(detector, data, manifestFilename, "manifest", 1, &findings, nil)
			if e = json.Unmarshal(data, &manifest); e != nil {
				return nil, errors.New("incomplete scan: invalid manifest")
			}
			seen[h.Name] = entry{name: h.Name}
			continue
		}
		offset, e := spool.Seek(0, io.SeekCurrent)
		if e != nil {
			return nil, fmt.Errorf("incomplete scan: spool offset: %w", e)
		}
		written, e := io.CopyN(spool, tr, h.Size)
		if e != nil || written != h.Size {
			return nil, errors.New("incomplete scan: truncated image entry")
		}
		item := entry{name: h.Name, offset: offset, size: h.Size}
		seen[h.Name] = item
		order = append(order, item)
	}
	trailing, err := io.Copy(io.Discard, outer)
	if err != nil {
		return nil, errors.New("incomplete scan: image archive byte limit exceeded")
	}
	if trailing != 0 {
		return nil, errors.New("incomplete scan: trailing image archive data")
	}
	if len(manifest) == 0 {
		return nil, errors.New("incomplete scan: missing image manifest")
	}
	configs, layers := map[string]bool{}, map[string]bool{}
	for _, m := range manifest {
		if m.Config == "" || m.Config == manifestFilename {
			return nil, errors.New("incomplete scan: missing image config")
		}
		if _, ok := seen[m.Config]; !ok {
			return nil, errors.New("incomplete scan: missing image config")
		}
		configs[m.Config] = true
		for _, l := range m.Layers {
			if _, ok := seen[l]; !ok || l == manifestFilename {
				return nil, errors.New("incomplete scan: missing layer")
			}
			layers[l] = true
		}
	}
	entryFindings, err := scanJobs(len(order), func() (func(int) ([]Finding, error), error) {
		workerDetector, err := newDetector()
		if err != nil {
			return nil, fmt.Errorf("initialize scanner: %w", err)
		}
		return func(i int) ([]Finding, error) {
			item := order[i]
			section := io.NewSectionReader(spool, item.offset, item.size)
			var out []Finding
			var scanErr error
			switch {
			case layers[item.name]:
				if item.size > maxLayerBytes {
					return nil, errors.New("incomplete scan: layer byte limit exceeded")
				}
				scanErr = scanLayer(section, item.size, "layer "+safePath(item.name), workerDetector, layerBudget, &out)
			case configs[item.name]:
				scanErr = scanFile(section, item.size, item.name, "config "+safePath(item.name), workerDetector, &out)
			default:
				scanErr = scanFile(section, item.size, item.name, "metadata "+safePath(item.name), workerDetector, &out)
			}
			return out, scanErr
		}, nil
	})
	if err != nil {
		return nil, err
	}
	return append(findings, entryFindings...), nil
}

func scanHeader(d *detect.Detector, h *tar.Header, label string, out *[]Finding) error {
	data, err := json.Marshal(h)
	if err != nil || len(data) > maxFileBytes {
		return errors.New("incomplete scan: oversized or invalid archive header")
	}
	appendFindings(d, data, h.Name, label, 1, out, nil)
	return nil
}

func readFile(r io.Reader, n int64) ([]byte, error) {
	if n > maxFileBytes {
		return nil, errors.New("incomplete scan: file byte limit exceeded")
	}
	data, e := io.ReadAll(io.LimitReader(r, n))
	if e != nil || int64(len(data)) != n {
		return nil, errors.New("incomplete scan: truncated file")
	}
	return data, nil
}
func appendFindings(d *detect.Detector, data []byte, source, path string, line int, out *[]Finding, duplicates map[string]bool) {
	// Gitleaks needs the original path for path-only and path-scoped rules.
	for _, f := range d.Detect(detect.Fragment{Raw: string(data), FilePath: source, StartLine: line}) { //nolint:staticcheck // v8 marks Fragment deprecated ahead of v9
		key := fmt.Sprintf("%s:%s:%d:%d:%s", path, f.RuleID, f.StartLine, f.StartColumn, f.Match)
		if duplicates != nil && duplicates[key] {
			continue
		}
		if duplicates != nil {
			duplicates[key] = true
		}
		*out = append(*out, Finding{Path: path, Rule: f.RuleID, Line: f.StartLine, SourcePath: source})
	}
}

// scanFile reads bounded windows aligned to complete lines. A line longer than
// the window fails closed rather than silently dropping a possible finding.
func scanFile(r io.Reader, n int64, source, label string, d *detect.Detector, out *[]Finding) error {
	const overlap = 64 << 10
	var (
		remaining  = n
		pending    = make([]byte, 0, maxFileBytes)
		prefix     = []byte(nil)
		line       = 1
		duplicates = map[string]bool{}
	)

	for remaining > 0 {
		capacity := maxFileBytes - len(pending)
		if capacity == 0 {
			return errors.New("incomplete scan: line exceeds scanning window")
		}
		if int64(capacity) > remaining {
			capacity = int(remaining)
		}
		start := len(pending)
		pending = append(pending, make([]byte, capacity)...)
		got, e := io.ReadFull(r, pending[start:])
		if e != nil || got != capacity {
			return errors.New("incomplete scan: truncated file")
		}
		remaining -= int64(got)
		if remaining == 0 {
			appendFindings(d, append(prefix, pending...), source, label, line, out, duplicates)
			return nil
		}
		cut := strings.LastIndexByte(string(pending), '\n') + 1
		if cut == 0 {
			return errors.New("incomplete scan: line exceeds scanning window")
		}
		chunk := append(append([]byte(nil), prefix...), pending[:cut]...)
		appendFindings(d, chunk, source, label, line, out, duplicates)
		// Retain complete trailing lines so bounded multiline matches spanning
		// adjacent windows can still be detected.
		keep := cut - overlap
		keep = max(keep, 0)
		if keep > 0 {
			keep = strings.IndexByte(string(pending[keep:cut]), '\n') + keep + 1
		}
		line += strings.Count(string(prefix), "\n") + strings.Count(string(pending[:keep]), "\n")
		prefix = append(prefix[:0], pending[keep:cut]...)
		pending = append(pending[:0], pending[cut:]...)
	}
	appendFindings(d, prefix, source, label, line, out, duplicates)
	return nil
}

func scanLayer(r io.Reader, n int64, label string, d *detect.Detector, b *budget, out *[]Finding) error {
	var (
		lr       = &io.LimitedReader{R: r, N: n}
		br       = bufio.NewReader(lr)
		magic, e = br.Peek(2)
	)

	if e != nil {
		return errors.New("incomplete scan: truncated layer")
	}
	var reader io.Reader = br
	if magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return errors.New("incomplete scan: invalid gzip layer")
		}
		defer gz.Close()
		reader = gz
	}
	// Count decompressed bytes as well as compressed bytes to bound expansion.
	var (
		counted = &countReader{r: reader, b: b}
		tr      = tar.NewReader(counted)
		entries = 0
	)

	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("incomplete scan: invalid or truncated layer")
		}
		entries++
		if entries > maxEntries {
			return errors.New("incomplete scan: too many layer entries")
		}
		if err := scanHeader(d, h, label+":"+safePath(h.Name)+":header", out); err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg {
			// Sparse and unknown entries may carry file data. Do not report a
			// clean scan if the reader cannot interpret their contents.
			switch h.Typeflag {
			case tar.TypeDir, tar.TypeSymlink, tar.TypeLink, tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
				if h.Size != 0 {
					return errors.New("incomplete scan: unsupported layer entry with data")
				}
			default:
				return errors.New("incomplete scan: unsupported layer entry")
			}
			continue
		}
		if err := scanFile(tr, h.Size, h.Name, label+":"+safePath(h.Name), d, out); err != nil {
			return err
		}
	}
	if counted.err != nil {
		return counted.err
	}
	// Consume and validate the complete compressed stream (including gzip checksum).
	trailing, err := io.Copy(io.Discard, counted)
	if err != nil {
		return errors.New("incomplete scan: truncated or oversized layer")
	}
	if trailing != 0 {
		return errors.New("incomplete scan: trailing layer data")
	}
	if counted.err != nil {
		return counted.err
	}
	if lr.N != 0 {
		return errors.New("incomplete scan: trailing layer data")
	}
	return nil
}

type countReader struct {
	r   io.Reader
	b   *budget
	err error
}

func (c *countReader) Read(p []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	if available := c.b.left(); int64(len(p)) > available+1 {
		p = p[:available+1]
	}
	n, e := c.r.Read(p)
	if err := c.b.take(int64(n)); err != nil {
		c.err = err
		return 0, err
	}
	return n, e
}
