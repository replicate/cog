package monobeam

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/replicate/cog/pkg/env"
	"github.com/replicate/cog/pkg/util/console"
)

type Client struct {
	client *http.Client
}

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

func NewClient(client *http.Client) *Client {
	return &Client{
		client: client,
	}
}

func (c *Client) UploadFile(ctx context.Context, objectType string, digest string, path string) error {
	console.Debug("uploading file: " + path)

	uploadUrl := startUploadURL(objectType, digest)
	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadUrl.String(), nil)
	if err != nil {
		return err
	}
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
	for i := 0; i < 100; i++ {
		final, err := checkVerificationStatus(req, client)
		if final {
			return err
		}
		time.Sleep(time.Second * 5)
	}

	return nil
}

func baseURL() url.URL {
	return url.URL{
		Scheme: env.SchemeFromEnvironment(),
		Host:   HostFromEnvironment(),
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
