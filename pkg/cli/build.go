package cli

import (
	"bytes"
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

	fmt.Println("--> Uploading", projectDir)

	bodyReader, bodyWriter := io.Pipe()

	client := &http.Client{}

	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:8080/v1/packages/upload", bodyReader)
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
		fmt.Println()
		fmt.Println("--> Building package...")
	}()

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	body := &bytes.Buffer{}
	if _, err = body.ReadFrom(resp.Body); err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, body)
	}

	fmt.Println("--> Done!")
	fmt.Println(body)

	return err
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
