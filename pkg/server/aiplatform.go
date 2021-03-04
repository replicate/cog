package server

import (
	"context"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
	"google.golang.org/api/ml/v1"
	"google.golang.org/api/option"

	"github.com/replicate/modelserver/pkg/global"
)

type AIPlatform struct {
	service *ml.Service
}

func NewAIPlatform() (*AIPlatform, error) {
	ai := new(AIPlatform)
	var err error
	ai.service, err = ml.NewService(context.Background(), option.WithEndpoint("https://us-central1-ml.googleapis.com"), option.WithHTTPClient(oauth2.NewClient(context.Background(), global.TokenSource)))
	if err != nil {
		return nil, fmt.Errorf("Failed to initialize AI Platform service: %w", err)
	}

	return ai, nil
}

func (ai *AIPlatform) Deploy(dockerTag string, version string) (string, error) {
	log.Info("Creating AI Platform version")

	op, err := ai.service.Projects.Models.Versions.Create(
		"projects/replicate/models/modelserver_example2",
		&ml.GoogleCloudMlV1__Version{
			Name:        version,
			MachineType: "n1-standard-4",
			Container: &ml.GoogleCloudMlV1__ContainerSpec{
				Image: dockerTag,
				Ports: []*ml.GoogleCloudMlV1__ContainerPort{{
					ContainerPort: 5000,
				}},
				Args: []string{"bash", "-c", "cd /code && python -c 'from infer import Model; Model().start_server()'"},
			},
			Routes: &ml.GoogleCloudMlV1__RouteMap{
				Health:  "/ping",
				Predict: "/infer-ai-platform",
			},
		},
	).Do()
	if err != nil {
		return "", fmt.Errorf("Failed to create model version: %w", err)
	}

	log.Info("Waiting for AI Platform version to become available")
	if err := ai.waitForVersionOp(context.Background(), op); err != nil {
		return "", err
	}

	aiPlatformEndpoint := "TODO"
	return aiPlatformEndpoint, nil
}

func (ai *AIPlatform) waitForVersionOp(ctx context.Context, op *ml.GoogleLongrunning__Operation) error {
	for {
		op, err := ai.service.Projects.Operations.Get(op.Name).Do()
		if err != nil {
			return err
		}
		if op.Error != nil {
			return fmt.Errorf("Failed to create version (error code: %d): %s", op.Error.Code, op.Error.Message)
		}

		if op.Done {
			return nil
		}

		t := time.NewTimer(1 * time.Second)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
			break
		}
	}
}
