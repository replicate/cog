package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"cloud.google.com/go/storage"
	"github.com/mholt/archiver/v3"
	log "github.com/sirupsen/logrus"
	"google.golang.org/api/cloudbuild/v1"
	"google.golang.org/api/option"

	"github.com/replicate/modelserver/pkg/global"
)

const waitSeconds = 1
const topLevelSourceDir = "source"

var tagRe = regexp.MustCompile("^([^:]+):(.+)$")

type CloudBuild struct {
	cb  *cloudbuild.Service
	gcs *storage.Client
	ctx context.Context
}

func NewCloudBuild() (c *CloudBuild, err error) {
	ctx := context.Background()
	authOption := option.WithTokenSource(global.TokenSource)

	c = &CloudBuild{ctx: context.Background()}

	c.cb, err = cloudbuild.NewService(ctx, authOption)
	if err != nil {
		return nil, err
	}

	c.gcs, err = storage.NewClient(c.ctx, authOption)
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (c *CloudBuild) Submit(directory string, hash string, imageTag string, dockerfilePath string) error {
	tarDir, err := os.MkdirTemp("/tmp", "tar")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tarDir)
	tarPath := filepath.Join(tarDir, hash + ".tar.gz")
	if err := archiver.Archive([]string{directory}, tarPath); err != nil {
		return err
	}

	obj, err := c.uploadToGcs(tarPath)
	if err != nil {
		return err
	}
	defer obj.Delete(c.ctx)
	gcsPath := obj.ObjectName()
	build := makeBuild(global.GCSBucket, gcsPath, imageTag, dockerfilePath)

	op, err := c.cb.Projects.Builds.Create(global.GCPProject, build).Do()
	if err != nil {
		return err
	}

	metadata, err := getBuildMetadata(op)
	if err != nil {
		return err
	}

	buildID := metadata.Build.Id
	logURL := metadata.Build.LogUrl

	log.Infof("Submitted to Cloud Build (id: %s, log url: %s)", buildID, logURL)

	build, err = c.waitForBuild(buildID)
	if err != nil {
		return err
	}

	if build.Status == "FAILURE" {
		return fmt.Errorf("Build failed")
	}

	log.Infof("Successfully pushed to %s", imageTag)

	return nil
}

func (c *CloudBuild) waitForBuild(buildID string) (build *cloudbuild.Build, err error) {
	for {
		build, err := c.cb.Projects.Builds.Get(global.GCPProject, buildID).Do()
		if err != nil {
			return nil, fmt.Errorf("Could not get build status: %v", err)
		}

		if s := build.Status; s != "WORKING" && s != "QUEUED" {
			return build, nil
		}

		time.Sleep(waitSeconds * time.Second)
	}
}

func (c *CloudBuild) uploadToGcs(tarPath string) (obj *storage.ObjectHandle, err error) {
	bucket := c.gcs.Bucket(global.GCSBucket)
	obj = bucket.Object("tmp/model-upload/" + filepath.Base(tarPath))

	w := obj.NewWriter(c.ctx)
	r, err := os.Open(tarPath)
	if err != nil {
		return nil, err
	}

	if _, err := io.Copy(w, r); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	return obj, nil
}

func makeBuild(gcsBucket string, gcsPath string, imageTag string, dockerfilePath string) *cloudbuild.Build {
	return &cloudbuild.Build{
		Source: &cloudbuild.Source{
			StorageSource: &cloudbuild.StorageSource{
				Bucket: gcsBucket,
				Object: gcsPath,
			},
		},
		Images: []string{imageTag},
		Steps: []*cloudbuild.BuildStep{
			{
				Name: "gcr.io/cloud-builders/docker",
				Args: []string{
					"build",
					"-t", imageTag,
					"-f", dockerfilePath,
					".",
				},
				Dir: topLevelSourceDir,
			},
		},
		Timeout: "1200s",
	}
}

func getBuildMetadata(op *cloudbuild.Operation) (*cloudbuild.BuildOperationMetadata, error) {
	if op.Metadata == nil {
		return nil, fmt.Errorf("missing Metadata in operation")
	}
	var metadata cloudbuild.BuildOperationMetadata
	if err := json.Unmarshal([]byte(op.Metadata), &metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}
