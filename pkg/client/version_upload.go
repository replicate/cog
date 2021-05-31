package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/schollz/progressbar/v3"

	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/zip"
)

func (c *Client) UploadVersion(mod *model.Model, projectDir string) (*model.Version, error) {
	hashes, err := c.getModCacheHashes(mod)
	if err != nil {
		return nil, err
	}

	bodyReader, bodyWriter := io.Pipe()
	client := &http.Client{
		Transport: &http.Transport{
			// we need to disable keepalive. there's a bug i (andreas) haven't
			// been able to get to the bottom of, where keep-alive requests
			// are missing content-type
			// TODO(andreas): this still breaks from time to time
			DisableKeepAlives: true,
		},
	}
	url := newURL(mod, "v1/models/%s/%s/versions/", mod.User, mod.Name)
	req, err := c.newRequest(http.MethodPut, url, bodyReader)
	if err != nil {
		return nil, err
	}

	bar := progressbar.DefaultBytes(
		-1,
		"uploading",
	)
	// HACK: make sure there's always a new line. progress bar doesn't always close in time.
	defer fmt.Println()

	zipReader, zipWriter := io.Pipe()

	// TODO error handling
	go func() {
		// archiver requires final slash, so normalize path to have trailing slash
		projectDir := strings.TrimSuffix(projectDir, "/") + "/"
		z := zip.NewCachingZip()
		err := z.WriterArchive(projectDir, io.MultiWriter(zipWriter, bar), hashes)
		if err != nil {
			console.Fatal(err.Error())
		}
		// MultiWriter is a Writer not a WriteCloser, but zipWriter is a WriteCloser, so we need to Close it ourselves
		if err = zipWriter.Close(); err != nil {
			console.Fatal(err.Error())
		}
	}()
	go func() {
		err := uploadFile(req, "file", "version.zip", zipReader, bodyWriter)
		if err != nil {
			console.Error(err.Error())
		}
	}()

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() // Also closed at end of function for error handling

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("Model does not exist: %s", mod.String())
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("You are not authorized to write to model %s. Did you run cog login?", mod.String())
	}
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("Failed to read response body: %w", err)
		}
		return nil, fmt.Errorf("Push failed. %s", body)
	}

	var version *model.Version
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			if err == io.ErrUnexpectedEOF {
				return nil, fmt.Errorf("Unexpected EOF")
			}
			// TODO(andreas): how to catch this error without comparing strings?
			if strings.Contains(err.Error(), "stream error") && strings.Contains(err.Error(), "INTERNAL_ERROR") {
				return nil, fmt.Errorf("Lost contact with server. Please try again")
			}
			console.Warn(err.Error())
		}
		msg := new(logger.Message)
		if err := json.Unmarshal(line, msg); err != nil {
			console.Debug(string(line))
			console.Warnf("Failed to parse console message: %s", err)
			continue
		}
		switch msg.Type {
		case logger.MessageTypeError:
			return nil, fmt.Errorf("Error: %s", msg.Text)
		case logger.MessageTypeLogLine:
			console.Debug(msg.Text)
		case logger.MessageTypeDebugLine:
			console.Debug(msg.Text)
		case logger.MessageTypeStatus:
			console.Debug(msg.Text)
		case logger.MessageTypeVersion:
			version = msg.Version
		}
	}

	if version == nil {
		return nil, fmt.Errorf("Failed to build version")
	}
	if err := resp.Body.Close(); err != nil {
		return nil, fmt.Errorf("Error closing response writer: %w", err)
	}
	return version, nil
}

func (c *Client) getModCacheHashes(mod *model.Model) ([]string, error) {
	url := newURL(mod, "v1/models/%s/%s/cache-hashes/", mod.User, mod.Name)
	req, err := c.newRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() // Also closed at end of function for error handling
	if resp.StatusCode == http.StatusNotFound {
		return []string{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Server returned status %d: %s", resp.StatusCode, body)
	}
	hashes := []string{}
	if err := json.NewDecoder(resp.Body).Decode(&hashes); err != nil {
		return nil, err
	}
	if err := resp.Body.Close(); err != nil {
		return nil, fmt.Errorf("Error closing response writer: %w", err)
	}
	return hashes, nil
}

func uploadFile(req *http.Request, key, filename string, file io.ReadCloser, body io.WriteCloser) error {
	mwriter := multipart.NewWriter(body)
	req.Header.Add("Content-Type", mwriter.FormDataContentType())
	defer mwriter.Close() // Also closed at end of function for error handling

	w, err := mwriter.CreateFormFile(key, filename)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, file); err != nil {
		return err
	}

	if err := mwriter.Close(); err != nil {
		return err
	}
	return body.Close()
}
