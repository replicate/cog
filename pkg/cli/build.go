package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/mholt/archiver/v3"
	"github.com/schollz/progressbar/v3"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
)

func newBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build Cog package",
		RunE:  buildPackage,
		Args:  cobra.NoArgs,
	}

	return cmd
}

func buildPackage(cmd *cobra.Command, args []string) error {
	projectDir, err := os.Getwd()
	if err != nil {
		return err
	}

	if _, err := os.Stat(path.Join(projectDir, "cog.yaml")); os.IsNotExist(err) {
		return fmt.Errorf("cog.yaml does not exist in %s. Are you in the right directory?", projectDir)
	}

	log.Infof("--> Uploading %s", projectDir)

	bodyReader, bodyWriter := io.Pipe()

	client := &http.Client{}

	req, err := http.NewRequest(http.MethodPost, remoteHost()+"/v1/packages/upload", bodyReader)
	if err != nil {
		return err
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
		return err
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
			log.Warnf("Failed to parse log message: %s", err)
			continue
		}
		switch msg.Type {
		case logger.MessageTypeError:
			return fmt.Errorf("Error: %s", msg.Text)
		case logger.MessageTypeLogLine:
			log.Debug(msg.Text)
		case logger.MessageTypeStatus:
			log.Info("--> " + msg.Text)
		case logger.MessageTypeModel:
			mod = msg.Model
		}
	}

	if mod == nil {
		return fmt.Errorf("Failed to build model")
	}
	fmt.Printf("Successfully built %s\n", mod.ID)
	return nil
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
