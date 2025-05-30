package monobeam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/vbauerster/mpb/v8"

	"github.com/replicate/cog/pkg/env"
	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/tools/uploader"
)

const (
	preUploadPath = "/uploads/pre-upload"
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

func (c *Client) PostPreUpload(ctx context.Context) error {
	preUploadUrl := baseURL()
	preUploadUrl.Path = preUploadPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, preUploadUrl.String(), nil)
	if err != nil {
		return util.WrapError(err, "create request")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return util.WrapError(err, "do request")
	}
	if resp.StatusCode != http.StatusOK {
		return errors.New("Bad response from pre upload: " + strconv.Itoa(resp.StatusCode))
	}
	return nil
}

func (c *Client) UploadFile(ctx context.Context, objectType string, digest string, path string, p *mpb.Progress, desc string) error {
	console.Debug("uploading file: " + path)

	// Start upload
	uploadUrl := startUploadURL(objectType, digest)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadUrl.String(), nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// A conflict means we have already uploaded this file.
	if resp.StatusCode == http.StatusConflict {
		return nil
	} else if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return errors.New("Bad response from monobeam for URL " + uploadUrl.String() + ": " + strconv.Itoa(resp.StatusCode))
	}

	// Decode the JSON payload
	decoder := json.NewDecoder(resp.Body)
	var data S3Config
	err = decoder.Decode(&data)
	if err != nil {
		return err
	}

	// Upload the file using tools/uploader/S3Uploader
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
	s3Uploader := uploader.NewS3Uploader(s3Client)
	err = s3Uploader.UploadObject(ctx, path, data.Bucket, data.Key, uploader.NewProgressConfig(p, desc))
	if err != nil {
		return err
	}

	_, err = p.Write([]byte(fmt.Sprintf("Waiting to verify %s was received by the server...\n", desc)))
	if err != nil {
		console.Debugf("failed to write to progress bar: %v", err)
	}

	// Begin verification
	verificationUrl := verificationURL(objectType, digest, data.Uuid)
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, verificationUrl.String(), nil)
	if err != nil {
		return err
	}
	beginResp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer beginResp.Body.Close()
	if beginResp.StatusCode != http.StatusCreated {
		return errors.New("Bad response from upload verification for " + data.Uuid + ": " + strconv.Itoa(resp.StatusCode))
	}

	// Check verification status
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, verificationUrl.String(), nil)
	if err != nil {
		return err
	}
	for i := 0; i < 100; i++ {
		// Clone request so we use a fresh copy every loop
		final, err := c.checkVerificationStatus(req.Clone(ctx), data.Uuid)
		if final {
			return err
		}
		time.Sleep(time.Second * 5)
	}

	return nil
}

func (c *Client) checkVerificationStatus(req *http.Request, uuid string) (bool, error) {
	checkResp, err := c.client.Do(req)
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
		return true, errors.New(fmt.Sprintf("Object %s failed to verify its hash.", uuid))
	}

	return false, nil
}

func baseURL() url.URL {
	return url.URL{
		Scheme: env.SchemeFromEnvironment(),
		Host:   env.MonobeamHostFromEnvironment(),
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
