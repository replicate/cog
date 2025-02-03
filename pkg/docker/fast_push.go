package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/requirements"
	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/weights"
)

const weightsObjectType = "weights"
const filesObjectType = "files"
const requirementsTarFile = "requirements.tar.zst"
const schemeEnv = "R8_PUSH_SCHEME"
const hostEnv = "R8_PUSH_HOST"

type S3Config struct {
	Key             string `json:"key"`
	Bucket          string `json:"bucket"`
	Endpoint        string `json:"endpoint"`
	AccessKeyId     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
	Expires         int64  `json:"expires"`
	Uuid            string `json:"uuid"`
}

type VerificationStatus struct {
	Verified bool `json:"verified"`
	Complete bool `json:"complete"`
}

func userAgent() string {
	return fmt.Sprintf("Cog/%s", global.Version)
}

func FastPush(ctx context.Context, image string, projectDir string, command Command) error {
	g, _ := errgroup.WithContext(ctx)

	token, err := command.LoadLoginToken(global.ReplicateRegistryHost)
	if err != nil {
		return fmt.Errorf("load login token error: %w", err)
	}

	tmpDir := filepath.Join(projectDir, ".cog", "tmp")
	weights, err := weights.ReadFastWeights(tmpDir)
	if err != nil {
		return fmt.Errorf("read weights error: %w", err)
	}
	// Upload weights
	for _, weight := range weights {
		g.Go(func() error {
			return uploadFile(ctx, weightsObjectType, weight.Digest, weight.Path, token)
		})
	}

	aptTarFile, err := CurrentAptTarball(tmpDir)
	if err != nil {
		return fmt.Errorf("current apt tarball error: %w", err)
	}
	// Upload apt tar file
	if aptTarFile != "" {
		hash, err := util.SHA256HashFile(aptTarFile)
		if err != nil {
			return err
		}
		g.Go(func() error {
			return uploadFile(ctx, filesObjectType, hash, aptTarFile, token)
		})
	}

	requirementsFile, err := requirements.CurrentRequirements(tmpDir)
	if err != nil {
		return err
	}
	// Upload python packages.
	if requirementsFile != "" {
		pythonTar, err := createPythonPackagesTarFile(image, tmpDir, command)
		if err != nil {
			return err
		}

		hash, err := util.SHA256HashFile(pythonTar)
		if err != nil {
			return err
		}
		g.Go(func() error {
			return uploadFile(ctx, filesObjectType, hash, pythonTar, token)
		})
	} else {
		requirementsTarFile := filepath.Join(tmpDir, requirementsTarFile)
		_, err = os.Stat(requirementsTarFile)
		if !errors.Is(err, os.ErrNotExist) {
			err = os.Remove(requirementsTarFile)
			if err != nil {
				return err
			}
		}
	}

	// Upload user /src.
	srcTar, err := createSrcTarFile(image, tmpDir, command)
	if err != nil {
		return fmt.Errorf("create src tarfile: %w", err)
	}
	hash, err := util.SHA256HashFile(srcTar)
	if err != nil {
		return err
	}
	g.Go(func() error {
		return uploadFile(ctx, filesObjectType, hash, srcTar, token)
	})

	// Wait for uploads
	return g.Wait()
}

func baseURL() url.URL {
	scheme := os.Getenv(schemeEnv)
	if scheme == "" {
		scheme = "https"
	}
	host := os.Getenv(hostEnv)
	if host == "" {
		host = "monobeam.replicate.delivery"
	}
	return url.URL{
		Scheme: scheme,
		Host:   host,
	}
}

func startUploadURL(objectType string, digest string) url.URL {
	uploadUrl := baseURL()
	uploadUrl.Path = strings.Join([]string{"", "uploads", objectType, "sha256", digest}, "/")
	return uploadUrl
}

func verificationURL(objectType string, digest string, uuid string) url.URL {
	verificationUrl := baseURL()
	verificationUrl.Path = strings.Join([]string{"", "uploads", objectType, "sha256", digest, uuid, "verification"}, "/")
	return verificationUrl
}

func checkVerificationStatus(req *http.Request, client *http.Client) (bool, error) {
	checkResp, err := client.Do(req)
	if err != nil {
		return true, err
	}
	defer checkResp.Body.Close()

	// Decode the JSON payload
	decoder := json.NewDecoder(checkResp.Body)
	var verificationStatus VerificationStatus
	err = decoder.Decode(&verificationStatus)
	if err != nil {
		return true, err
	}

	// OK status means the server has finished verification.
	if checkResp.StatusCode == http.StatusOK {
		if verificationStatus.Verified && verificationStatus.Complete {
			return true, nil
		}
		return true, errors.New("Object failed to verify its hash.")
	}

	return false, nil
}

func uploadFile(ctx context.Context, objectType string, digest string, path string, token string) error {
	console.Debug("uploading file: " + path)

	uploadUrl := startUploadURL(objectType, digest)
	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadUrl.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", userAgent())
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// A conflict means we have already uploaded this file.
	if resp.StatusCode == http.StatusConflict {
		return nil
	} else if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return errors.New("Bad response: " + strconv.Itoa(resp.StatusCode))
	}

	// Decode the JSON payload
	decoder := json.NewDecoder(resp.Body)
	var data S3Config
	err = decoder.Decode(&data)
	if err != nil {
		return err
	}

	// Open the file for uploading
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Upload the file using an S3 client
	console.Debug("multi-part uploading file: " + path)
	cfg := aws.NewConfig()
	cfg.BaseEndpoint = &data.Endpoint
	cfg.Region = "auto"
	cfg.Credentials = credentials.StaticCredentialsProvider{
		Value: aws.Credentials{
			AccessKeyID:     data.AccessKeyId,
			SecretAccessKey: data.SecretAccessKey,
			SessionToken:    data.SessionToken,
			Expires:         time.Unix(data.Expires, 0),
		},
	}
	s3Client := s3.NewFromConfig(*cfg)
	uploader := manager.NewUploader(s3Client, func(u *manager.Uploader) {
		u.PartSize = 64 * 1024 * 1024 // 64MB per part
	})

	uploadParams := &s3.PutObjectInput{
		Bucket: aws.String(data.Bucket),
		Key:    aws.String(data.Key),
		Body:   file,
	}
	_, err = uploader.Upload(ctx, uploadParams)
	if err != nil {
		return err
	}

	// Begin verification
	verificationUrl := verificationURL(objectType, digest, data.Uuid)
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, verificationUrl.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", userAgent())
	beginResp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer beginResp.Body.Close()
	if beginResp.StatusCode != http.StatusCreated {
		return errors.New("Bad response from upload verification: " + strconv.Itoa(resp.StatusCode))
	}

	// Check verification status
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, verificationUrl.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", userAgent())
	for i := 0; i < 100; i++ {
		final, err := checkVerificationStatus(req, client)
		if final {
			return err
		}
		time.Sleep(time.Second * 5)
	}

	return nil
}

func createPythonPackagesTarFile(image string, tmpDir string, command Command) (string, error) {
	return command.CreateTarFile(image, tmpDir, requirementsTarFile, "root/.venv")
}

func createSrcTarFile(image string, tmpDir string, command Command) (string, error) {
	return command.CreateTarFile(image, tmpDir, "src.tar.zst", "src")
}
