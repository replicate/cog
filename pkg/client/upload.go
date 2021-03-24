package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/mholt/archiver/v3"
	"github.com/schollz/progressbar/v3"
	log "github.com/sirupsen/logrus"

	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
)

func (c *Client) UploadPackage(repo *model.Repo, projectDir string) (*model.Model, error) {
	bodyReader, bodyWriter := io.Pipe()

	client := &http.Client{}

	url := fmt.Sprintf("http://%s/v1/repos/%s/%s/packages/", repo.Host, repo.User, repo.Name)
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
		z := archiver.Zip{ImplicitTopLevelFolder: false}
		err := z.WriterArchive([]string{projectDir}, io.MultiWriter(zipWriter, bar))
		if err != nil {
			log.Fatal(err)
		}
		// MultiWriter is a Writer not a WriteCloser, but zipWriter is a WriteCloser, so we need to Close it ourselves
		if err = zipWriter.Close(); err != nil {
			log.Fatal(err)
		}
	}()
	go func() {
		err := uploadFile(req, "file", "package.zip", zipReader, bodyWriter)
		if err != nil {
			log.Fatal(err)
		}
		log.Info("--> Building package...")
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
				log.Warn("Unexpected EOF")
				break
			}
			log.Warn(err)
		}
		msg := new(logger.Message)
		if err := json.Unmarshal(line, msg); err != nil {
			log.Debug(string(line))
			log.Warnf("Failed to parse log message: %s", err)
			continue
		}
		switch msg.Type {
		case logger.MessageTypeError:
			return nil, fmt.Errorf("Error: %s", msg.Text)
		case logger.MessageTypeLogLine:
			log.Debug(msg.Text)
		case logger.MessageTypeStatus:
			log.Info("--> " + msg.Text)
		case logger.MessageTypeModel:
			mod = msg.Model
		}
	}

	if mod == nil {
		return nil, fmt.Errorf("Failed to build model")
	}
	return mod, nil
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
