package runner

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/replicate/go/httpclient"
)

var (
	Base64Regex           = regexp.MustCompile(`^data:.*;base64,(?P<base64>.*)$`)
	ErrSchemaNotAvailable = errors.New("OpenAPI schema not available for input processing")
)

func isURI(s *openapi3.SchemaRef) bool {
	return s.Value.Type.Is("string") && s.Value.Format == "uri"
}

// ProcessInputPaths processes the input paths and discards the now unused paths from the input.
// Note that we return the input, but the expectation is that input will be mutated in-place. This function
// returns ErrSchemaNotAvailable if the OpenAPI schema is not available. It is up to the caller to decide how
// handles this error (e.g. log a warning and proceed without path processing).
func ProcessInputPaths(input any, doc *openapi3.T, paths *[]string, fn func(string, *[]string) (string, error)) (any, error) {
	if doc == nil {
		return input, ErrSchemaNotAvailable
	}

	schema, ok := doc.Components.Schemas["Input"]
	if !ok {
		return input, nil
	}

	// Input is always a `dict[str, Any]`
	m, ok := input.(map[string]any)
	if !ok {
		return input, nil
	}

	for k, v := range m {
		p, ok := schema.Value.Properties[k]
		if !ok {
			continue
		}
		switch {
		case isURI(p):
			// field: Path or field: Optional[Path]
			if s, ok := v.(string); ok {
				o, err := fn(s, paths)
				if err != nil {
					return nil, err
				}
				m[k] = o
			}
		case p.Value.Type.Is("array") && isURI(p.Value.Items):
			// field: list[Path]
			if xs, ok := v.([]any); ok {
				for i, x := range xs {
					if s, ok := x.(string); ok {
						o, err := fn(s, paths)
						if err != nil {
							return nil, err
						}
						xs[i] = o
					}
				}
			}
		case p.Value.Type.Is("object"):
			// field is Any with custom coder, e.g. dataclass, JSON, or Pydantic
			// No known schema, try to handle all attributes
			o, err := handlePath(v, paths, fn)
			if err != nil {
				return nil, err
			}
			m[k] = o
		}
	}
	return input, nil
}

func handlePath(jsonVal any, paths *[]string, fn func(string, *[]string) (string, error)) (any, error) {
	switch v := jsonVal.(type) {
	case string:
		return fn(v, paths)
	case []any:
		for i, x := range v {
			if s, ok := x.(string); ok {
				o, err := fn(s, paths)
				if err != nil {
					return nil, err
				}
				v[i] = o
			} else {
				o, err := handlePath(v[i], paths, fn)
				if err != nil {
					return nil, err
				}
				v[i] = o
			}
		}
		return v, nil
	case map[string]any:
		for key, value := range v {
			if s, ok := value.(string); ok {
				o, err := fn(s, paths)
				if err != nil {
					return nil, err
				}
				v[key] = o
			} else {
				o, err := handlePath(v[key], paths, fn)
				if err != nil {
					return nil, err
				}
				v[key] = o
			}
		}
		return v, nil
	default:
		return jsonVal, nil
	}
}

// Base64ToInput converts base64 data URLs to temporary files
func Base64ToInput(s string, paths *[]string) (string, error) {
	m := Base64Regex.FindStringSubmatch(s)
	if m == nil {
		return s, nil
	}
	bs, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "cog-input-")
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck // in error case, there isn't anything we can do.
	if _, err := f.Write(bs); err != nil {
		return "", err
	}
	*paths = append(*paths, f.Name())
	if err := os.Chmod(f.Name(), 0o666); err != nil { //nolint:gosec // TODO: evaluate if 0o666 is correct mode
		return "", err
	}
	return f.Name(), nil
}

// URLToInput downloads HTTP URLs to temporary files
func URLToInput(s string, paths *[]string) (string, error) {
	u, err := url.Parse(s)
	if err != nil {
		return s, nil
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return s, nil
	}
	f, err := os.CreateTemp("", fmt.Sprintf("cog-input-*%s", filepath.Ext(u.Path)))
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck // in error case, there isn't anything we can do.
	resp, err := http.DefaultClient.Get(s)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", err
	}
	*paths = append(*paths, f.Name())
	if err := os.Chmod(f.Name(), 0o666); err != nil { //nolint:gosec // TODO: evaluate if 0o666 is correct mode
		return "", err
	}
	return f.Name(), nil
}

// OutputToBase64 converts file paths to base64 data URLs
func OutputToBase64(s string, paths *[]string) (string, error) {
	u, err := url.Parse(s)
	if err != nil {
		return s, nil
	}
	if u.Scheme != "file" {
		return s, nil
	}
	p := u.Path

	bs, err := os.ReadFile(p) //nolint:gosec // expected dynamic path
	if err != nil {
		return "", err
	}
	*paths = append(*paths, p)

	mt := mimetype.Detect(bs)
	b64 := base64.StdEncoding.EncodeToString(bs)
	return fmt.Sprintf("data:%s;base64,%s", mt, b64), nil
}

// uploader handles file uploads with a long-lived HTTP client
type uploader struct {
	client    *http.Client
	uploadURL string
}

// newUploader creates a new uploader instance
func newUploader(uploadURL string) *uploader {
	return &uploader{
		client:    httpclient.ApplyRetryPolicy(http.DefaultClient),
		uploadURL: uploadURL,
	}
}

// processOutput uploads a file and returns the upload URL
func (u *uploader) processOutput(s, predictionID string, paths *[]string) (string, error) {
	if u.client == nil {
		return "", fmt.Errorf("uploader client not initialized")
	}

	parsedURL, err := url.Parse(s)
	if err != nil {
		return s, nil
	}
	if parsedURL.Scheme != "file" {
		return s, nil
	}
	p := parsedURL.Path

	bs, err := os.ReadFile(p) //nolint:gosec // expected dynamic path
	if err != nil {
		return "", err
	}
	*paths = append(*paths, p)
	filename := path.Base(p)
	uploadURL := strings.TrimSuffix(u.uploadURL, "/")
	uUpload := uploadURL + "/" + filename
	req, err := http.NewRequest(http.MethodPut, uUpload, bytes.NewReader(bs))
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Prediction-ID", predictionID)
	req.Header.Set("Content-Type", mimetype.Detect(bs).String())
	resp, err := u.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("failed to upload file: status %s", resp.Status)
	}
	location := resp.Header.Get("Location")
	if location == "" {
		// In case upload server does not respond with Location
		location = uUpload
	}
	return location, nil
}
