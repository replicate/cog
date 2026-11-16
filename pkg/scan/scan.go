// Package scan scans every file in every layer of a Docker save archive.
package scan

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	// The official github.com/gitleaks/gitleaks repository retains this module path.
	"github.com/zricethezav/gitleaks/v8/detect"
)

const (
	maxFileBytes  = 8 << 20
	maxTotalBytes = 8 << 30
	maxLayerBytes = 2 << 30
	maxEntries    = 1000000
)

// Finding deliberately omits the secret, matching text, and original filename.
// Paths are hashes because filenames themselves can contain credentials.
type Finding struct {
	Path string `json:"path"`
	Rule string `json:"rule"`
	Line int    `json:"line"`
}

type budget struct{ remaining int64 }

func (b *budget) take(n int64) error {
	if n > b.remaining {
		return errors.New("incomplete scan: total byte limit exceeded")
	}
	b.remaining -= n
	return nil
}

func safePath(s string) string {
	hash := sha256.Sum256([]byte(s))
	return fmt.Sprintf("sha256:%x", hash[:8])
}

// Archive scans a Docker save stream. Any skipped, malformed, or oversized input
// fails closed: findings from an incomplete scan are never returned as a clean result.
func Archive(r io.Reader) ([]Finding, error) {
	detector, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		return nil, fmt.Errorf("initialize scanner: %w", err)
	}
	detector.Redact = 100

	var (
		b        = &budget{remaining: maxTotalBytes}
		outer    = &countReader{r: r, b: b}
		tr       = tar.NewReader(outer)
		seen     = map[string]bool{}
		manifest []struct {
			Config string
			Layers []string
		}
		findings []Finding
		entries  = 0
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
		if seen[h.Name] {
			return nil, errors.New("incomplete scan: duplicate archive entry")
		}
		seen[h.Name] = true
		switch {
		case h.Name == "manifest.json":
			data, e := readFile(tr, h.Size)
			if e != nil {
				return nil, e
			}
			appendFindings(detector, data, "manifest", &findings)
			if e = json.Unmarshal(data, &manifest); e != nil {
				return nil, errors.New("incomplete scan: invalid manifest")
			}
		case strings.HasSuffix(h.Name, "/layer.tar") || strings.HasSuffix(h.Name, ".tar"):
			if h.Size > maxLayerBytes {
				return nil, errors.New("incomplete scan: layer byte limit exceeded")
			}
			label := "layer " + safePath(h.Name)
			if e = scanLayer(tr, h.Size, label, detector, b, &findings); e != nil {
				return nil, e
			}
		case strings.HasSuffix(h.Name, ".json"):
			data, e := readFile(tr, h.Size)
			if e != nil {
				return nil, e
			}
			appendFindings(detector, data, "config "+safePath(h.Name), &findings)
		default:
			data, e := readFile(tr, h.Size)
			if e != nil {
				return nil, e
			}
			appendFindings(detector, data, "metadata "+safePath(h.Name), &findings)
		}
	}
	if _, err := io.Copy(io.Discard, outer); err != nil {
		return nil, errors.New("incomplete scan: image archive byte limit exceeded")
	}
	if len(manifest) == 0 {
		return nil, errors.New("incomplete scan: missing image manifest")
	}
	for _, m := range manifest {
		if !strings.HasSuffix(m.Config, ".json") || !seen[m.Config] {
			return nil, errors.New("incomplete scan: missing image config")
		}
		for _, l := range m.Layers {
			if !strings.HasSuffix(l, ".tar") || !seen[l] {
				return nil, errors.New("incomplete scan: missing layer")
			}
		}
	}
	return findings, nil
}

func scanHeader(d *detect.Detector, h *tar.Header, label string, out *[]Finding) error {
	data, err := json.Marshal(h)
	if err != nil || len(data) > maxFileBytes {
		return errors.New("incomplete scan: oversized or invalid archive header")
	}
	appendFindings(d, data, label, out)
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
func appendFindings(d *detect.Detector, data []byte, path string, out *[]Finding) {
	// Gitleaks v8 requires Fragment to set the first line of in-memory content.
	for _, f := range d.Detect(detect.Fragment{Raw: string(data), StartLine: 1}) { //nolint:staticcheck // v8 marks Fragment deprecated ahead of v9
		*out = append(*out, Finding{Path: path, Rule: f.RuleID, Line: f.StartLine})
	}
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
	counted := &countReader{r: reader, b: b}
	tr := tar.NewReader(counted)
	entries := 0
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
		data, err := readFile(tr, h.Size)
		if err != nil {
			return err
		}
		appendFindings(d, data, label+":"+safePath(h.Name), out)
	}
	if counted.err != nil {
		return counted.err
	}
	// Consume and validate the complete compressed stream (including gzip checksum).
	if _, err := io.Copy(io.Discard, counted); err != nil {
		return errors.New("incomplete scan: truncated or oversized layer")
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
	if int64(len(p)) > c.b.remaining+1 {
		p = p[:c.b.remaining+1]
	}
	n, e := c.r.Read(p)
	if err := c.b.take(int64(n)); err != nil {
		c.err = err
		return 0, err
	}
	return n, e
}
