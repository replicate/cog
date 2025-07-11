package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/env"
	r8_errors "github.com/replicate/cog/pkg/errors"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/web"
)

const DraftsPrefix = "draft:"

var (
	ErrorBadResponseNewVersionEndpoint = errors.New("Bad response from new version endpoint")
	ErrorBadDraftFormat                = errors.New("Bad draft format")
	ErrorBadDraftUsernameDigestFormat  = errors.New("Bad draft username/digest format")
)

type Client struct {
	dockerCommand command.Command
	client        *http.Client
	tokens        map[string]string
	webClient     *web.Client
}

type Version struct {
	Id string `json:"id"`
}

type CreateRelease struct {
	Version string `json:"version"`
}

type Model struct {
	LatestVersion Version `json:"latest_version"`
}

type SubError struct {
	Detail  string `json:"detail"`
	Pointer string `json:"pointer"`
}

type Error struct {
	Detail string     `json:"detail"`
	Errors []SubError `json:"errors"`
	Status int        `json:"status"`
	Title  string     `json:"title"`
}

func NewClient(dockerCommand command.Command, client *http.Client, webClient *web.Client) *Client {
	return &Client{
		dockerCommand: dockerCommand,
		client:        client,
		tokens:        map[string]string{},
		webClient:     webClient,
	}
}

func (c *Client) PostNewPipeline(ctx context.Context, image string, tarball *bytes.Buffer) error {
	id, err := c.postNewVersion(ctx, image, tarball)
	if err != nil {
		return err
	}

	return c.postNewRelease(ctx, id, image)
}

func (c *Client) PullSource(ctx context.Context, image string, tarFileProcess func(*tar.Header, *tar.Reader) error) error {
	if strings.HasPrefix(image, DraftsPrefix) {
		username, digest, err := decomposeDraftSlug(image)
		if err != nil {
			return err
		}
		return c.getDraftSource(ctx, username, digest, tarFileProcess)
	}

	_, entity, name, tag, err := decomposeImageName(image)
	if err != nil {
		return err
	}

	// Check if we require the tag
	if tag == "" {
		model, err := c.getModel(ctx, entity, name)
		if err != nil {
			return err
		}
		tag = model.LatestVersion.Id
	}

	// Fetch the source
	return c.getSource(ctx, entity, name, tag, tarFileProcess)
}

func (c *Client) provideToken(ctx context.Context, entity string) (string, error) {
	token, ok := c.tokens[entity]
	if !ok {
		webToken, err := c.webClient.FetchAPIToken(ctx, entity)
		if err != nil {
			return "", err
		}
		token = webToken
		c.tokens[entity] = token
	}
	return token, nil
}

func (c *Client) postNewVersion(ctx context.Context, image string, tarball *bytes.Buffer) (string, error) {
	// Fetch manifest
	manifest, err := c.dockerCommand.Inspect(ctx, image)
	if err != nil {
		return "", util.WrapError(err, "failed to inspect docker image")
	}

	// Fetch token
	_, entity, name, _, err := decomposeImageName(image)
	if err != nil {
		return "", err
	}
	token, err := c.provideToken(ctx, entity)
	if err != nil {
		return "", err
	}

	// Create form data body
	body := new(bytes.Buffer)
	mp := multipart.NewWriter(body)
	defer mp.Close()

	if err := mp.WriteField("openapi_schema", manifest.Config.Labels[command.CogOpenAPISchemaLabelKey]); err != nil {
		return "", err
	}

	var gzipBuf bytes.Buffer
	gzipWriter := gzip.NewWriter(&gzipBuf)
	_, err = io.Copy(gzipWriter, bytes.NewReader(tarball.Bytes()))
	if err != nil {
		return "", err
	}
	if err := gzipWriter.Close(); err != nil {
		return "", err
	}

	part, err := mp.CreateFormFile("source_archive", "source_archive.tar.gz")
	if err != nil {
		return "", err
	}

	_, err = io.Copy(part, bytes.NewReader(gzipBuf.Bytes()))
	if err != nil {
		return "", err
	}
	mp.Close()

	versionURL := newVersionsURL(entity, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, versionURL.String(), bytes.NewReader(body.Bytes()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mp.FormDataContentType())
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	// Make the request
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		var bodyError Error
		if err := json.NewDecoder(resp.Body).Decode(&bodyError); err != nil {
			return "", err
		}
		if len(bodyError.Errors) > 0 && bodyError.Errors[0].Detail != "" {
			return "", errors.New(bodyError.Errors[0].Detail)
		}
		return "", ErrorBadResponseNewVersionEndpoint
	}

	var version Version
	if err := json.NewDecoder(resp.Body).Decode(&version); err != nil {
		return "", err
	}

	return version.Id, nil
}

func (c *Client) postNewRelease(ctx context.Context, id string, image string) error {
	_, entity, name, _, err := decomposeImageName(image)
	if err != nil {
		return err
	}

	token, err := c.provideToken(ctx, entity)
	if err != nil {
		return err
	}

	releaseURL := newReleaseURL(entity, name)
	createRelease := CreateRelease{
		Version: id,
	}
	buf := new(bytes.Buffer)
	err = json.NewEncoder(buf).Encode(createRelease)
	if err != nil {
		return fmt.Errorf("Unable to encode JSON request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, releaseURL.String(), buf)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	// Make the request
	releaseResp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer releaseResp.Body.Close()

	if releaseResp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("Bad response: %s attempting to create a release", strconv.Itoa(releaseResp.StatusCode))
	}

	return nil
}

func (c *Client) getSource(ctx context.Context, entity string, name string, tag string, tarFileProcess func(*tar.Header, *tar.Reader) error) error {
	token, err := c.provideToken(ctx, entity)
	if err != nil {
		return err
	}

	sourceURL := newSourceURL(entity, name, tag)
	return c.downloadTarball(ctx, token, sourceURL, strings.Join([]string{entity, name}, "/"), tarFileProcess)
}

func (c *Client) getDraftSource(ctx context.Context, username string, digest string, tarFileProcess func(*tar.Header, *tar.Reader) error) error {
	token, err := c.provideToken(ctx, username)
	if err != nil {
		return err
	}

	draftURL := newDraftSourceURL(digest)
	return c.downloadTarball(ctx, token, draftURL, DraftsPrefix+strings.Join([]string{username, digest}, "/"), tarFileProcess)
}

func (c *Client) downloadTarball(ctx context.Context, token string, url url.URL, slug string, tarFileProcess func(*tar.Header, *tar.Reader) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	// Make the request
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("Entity %s does not have a source package associated with it.", slug)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("Bad response: %s attempting to fetch the image source", strconv.Itoa(resp.StatusCode))
	}

	tr := tar.NewReader(resp.Body)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if err := tarFileProcess(header, tr); err != nil {
			return err
		}
	}

	return nil
}

func (c *Client) getModel(ctx context.Context, entity string, name string) (*Model, error) {
	token, err := c.provideToken(ctx, entity)
	if err != nil {
		return nil, err
	}

	modelURL := newModelURL(entity, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	// Make the request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("Bad response: %s attempting to fetch the models versions", strconv.Itoa(resp.StatusCode))
	}

	var model Model
	if err := json.NewDecoder(resp.Body).Decode(&model); err != nil {
		return nil, err
	}

	return &model, nil
}

func apiBaseURL() url.URL {
	return url.URL{
		Scheme: env.SchemeFromEnvironment(),
		Host:   env.APIHostFromEnvironment(),
	}
}

func newVersionsURL(entity string, name string) url.URL {
	newVersionUrl := apiBaseURL()
	newVersionUrl.Path = strings.Join([]string{"", "v1", "models", entity, name, "versions"}, "/")
	return newVersionUrl
}

func newReleaseURL(entity string, name string) url.URL {
	newReleaseUrl := apiBaseURL()
	newReleaseUrl.Path = strings.Join([]string{"", "v1", "models", entity, name, "releases"}, "/")
	return newReleaseUrl
}

func newSourceURL(entity string, name string, tag string) url.URL {
	newSourceUrl := apiBaseURL()
	newSourceUrl.Path = strings.Join([]string{"", "v1", "models", entity, name, "versions", tag, "source"}, "/")
	return newSourceUrl
}

func newModelURL(entity string, name string) url.URL {
	newModelUrl := apiBaseURL()
	newModelUrl.Path = strings.Join([]string{"", "v1", "models", entity, name}, "/")
	return newModelUrl
}

func newDraftSourceURL(digest string) url.URL {
	newDraftSourceUrl := apiBaseURL()
	newDraftSourceUrl.Path = strings.Join([]string{"", "v1", "drafts", digest, "source"}, "/")
	return newDraftSourceUrl
}

func decomposeImageName(image string) (string, string, string, string, error) {
	imageComponents := strings.Split(image, "/")

	// Attempt normalisation of image
	if len(imageComponents) == 2 && imageComponents[0] != global.ReplicateRegistryHost {
		imageComponents = append([]string{global.ReplicateRegistryHost}, imageComponents...)
	}

	if len(imageComponents) != 3 {
		return "", "", "", "", r8_errors.ErrorBadRegistryURL
	}
	if imageComponents[0] != global.ReplicateRegistryHost {
		return "", "", "", "", r8_errors.ErrorBadRegistryHost
	}
	tagComponents := strings.Split(imageComponents[2], ":")
	tag := ""
	if len(tagComponents) == 2 {
		tag = tagComponents[1]
	}
	return imageComponents[0], imageComponents[1], tagComponents[0], tag, nil
}

func decomposeDraftSlug(slug string) (string, string, error) {
	slugComponents := strings.Split(slug, ":")
	if len(slugComponents) != 2 {
		return "", "", ErrorBadDraftFormat
	}

	draftComponents := strings.Split(slugComponents[1], "/")
	if len(draftComponents) != 2 {
		return "", "", ErrorBadDraftUsernameDigestFormat
	}

	return draftComponents[0], draftComponents[1], nil
}
