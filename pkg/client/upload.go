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

	"github.com/replicate/cog/pkg/console"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/zip"
)

func (c *Client) UploadModel(repo *model.Repo, projectDir string) (*model.Model, error) {
	hashes, err := c.getRepoCacheHashes(repo)
	if err != nil {
		return nil, err
	}

	bodyReader, bodyWriter := io.Pipe()
	client := &http.Client{
		Transport: &http.Transport{
			// we need to disable keepalive. there's a bug i (andreas) haven't
			// been able to get to the bottom of, where keep-alive requests
			// are missing content-type
			DisableKeepAlives: true,
		},
	}
	url, err := c.getURL(repo, "v1/repos/%s/%s/models/", repo.User, repo.Name)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPut, url, bodyReader)
	if err != nil {
		return nil, err
	}

	bar := progressbar.DefaultBytes(
		-1,
		"uploading",
	)

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
		err := uploadFile(req, "file", "model.zip", zipReader, bodyWriter)
		if err != nil {
			console.Fatal(err.Error())
		}
		console.Info("Building model...")
	}()

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var mod *model.Model
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			if err == io.ErrUnexpectedEOF {
				console.Warn("Unexpected EOF")
				break
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
			console.Info(msg.Text)
		case logger.MessageTypeDebugLine:
			console.Debug(msg.Text)
		case logger.MessageTypeStatus:
			console.Info(msg.Text)
		case logger.MessageTypeModel:
			mod = msg.Model
		}
	}

	if mod == nil {
		return nil, fmt.Errorf("Failed to build model")
	}
	return mod, nil
}

func (c *Client) getRepoCacheHashes(repo *model.Repo) ([]string, error) {
	url, err := c.getURL(repo, "v1/repos/%s/%s/cache-hashes/", repo.User, repo.Name)
	if err != nil {
		return nil, err
	}
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
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
	return hashes, nil
}

func uploadFile(req *http.Request, key, filename string, file io.ReadCloser, body io.WriteCloser) error {
	mwriter := multipart.NewWriter(body)
	req.Header.Add("Content-Type", mwriter.FormDataContentType())
	defer mwriter.Close()

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
