package factory

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/containerd/containerd/api/services/content/v1"
	"github.com/google/go-containerregistry/pkg/name"
	buildkitclient "github.com/moby/buildkit/client"
	"github.com/moby/buildkit/frontend/gateway/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/tonistiigi/fsutil"
	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/model/factory/state"
	"github.com/replicate/cog/pkg/model/factory/types"
	"github.com/replicate/cog/pkg/util"
)

func newBuildkitFactory(provider command.ClientProvider) (*buildkitFactory, error) {
	return &buildkitFactory{
		provider: provider,
	}, nil
}

type buildkitFactory struct {
	provider command.ClientProvider
}

func (f *buildkitFactory) Build(ctx context.Context, settings BuildSettings) (*model.Model, BuildInfo, error) {
	buildInfo := BuildInfo{
		FactoryBackend: "buildkit",
	}

	bkClient, err := f.provider.BuildKitClient(ctx)
	if err != nil {
		return nil, buildInfo, err
	}
	defer bkClient.Close()

	contextFS, err := fsutil.NewFS(settings.WorkingDir)
	if err != nil {
		return nil, buildInfo, fmt.Errorf("failed to create context FS: %w", err)
	}

	// define the root solve options
	solveOpt := buildkitclient.SolveOpt{
		Exports: []buildkitclient.ExportEntry{
			{
				Type: "moby",
				Attrs: map[string]string{
					"name": settings.Tag,
				},
			},
		},
		LocalMounts: map[string]fsutil.FS{
			"context": contextFS,
		},
	}

	productID := fmt.Sprintf("cog-model:%s", settings.Tag)

	// Create a status channel for build progress
	statusCh := make(chan *buildkitclient.SolveStatus)

	eg, egctx := errgroup.WithContext(ctx)
	eg.Go(docker.NewBuildKitSolveDisplay(statusCh, "plain"))

	var solveResp *buildkitclient.SolveResponse

	eg.Go(func() error {
		resp, err := bkClient.Build(
			egctx,
			solveOpt,
			productID,
			func(ctx context.Context, c client.Client) (*client.Result, error) {
				buildCtx := types.Context{
					Context:    ctx,
					Config:     settings.Config,
					WorkingDir: settings.WorkingDir,
					Platform:   settings.Platform,
					Client:     c,
				}

				stack := PythonStack{}
				buildInfo.Builder = "python"

				finalState, err := stack.Solve(buildCtx, c)
				if err != nil {
					return nil, err
				}

				def, err := finalState.Marshal(ctx)
				if err != nil {
					return nil, err
				}

				result, err := c.Solve(ctx, client.SolveRequest{
					Definition: def.ToPB(),
				})
				if err != nil {
					return nil, err
				}

				outputMeta, err := state.GetMeta(buildCtx, finalState)
				if err != nil {
					return nil, err
				}
				outputMeta.Labels[types.LabelVersion] = global.Version

				configJSON, err := json.Marshal(buildCtx.Config)
				if err != nil {
					return nil, fmt.Errorf("Failed to convert config to JSON: %w", err)
				}
				outputMeta.Labels[types.LabelConfig] = string(configJSON)

				fmt.Println("outputMeta")
				util.PrettyPrintJSON(outputMeta)

				outputImage := ocispec.Image{
					Config:   outputMeta.ToImageConfig(),
					Platform: settings.Platform,
					Author:   "cog",
				}

				iamgeBlob, err := json.Marshal(outputImage)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal image config: %w", err)
				}

				out := &client.Result{}
				// out.AddMeta("yo", []byte("yo"))
				out.SetRef(result.Ref)                          // filesystem
				out.AddMeta("containerimage.config", iamgeBlob) // config blob

				result.AddMeta("containerimage.config", iamgeBlob)

				return out, nil
			},
			statusCh,
		)
		if err != nil {
			return fmt.Errorf("failed to solve build: %w", err)
		}
		solveResp = resp
		return nil
	})

	if err := eg.Wait(); err != nil {
		return nil, buildInfo, err
	}

	fmt.Println("solveResp")
	util.PrettyPrintJSON(solveResp)

	descriptor, manifest, image, err := imageFromExporterResp(ctx, bkClient, solveResp.ExporterResponse)
	if err != nil {
		return nil, buildInfo, err
	}
	util.PrettyPrintJSON(descriptor)
	util.PrettyPrintJSON(manifest)
	util.PrettyPrintJSON(image)

	ref, err := name.ParseReference(settings.Tag)
	if err != nil {
		return nil, buildInfo, fmt.Errorf("failed to parse reference: %w", err)
	}

	return &model.Model{
		Ref:      ref,
		Source:   model.ModelSourceLocal,
		Config:   settings.Config,
		Manifest: manifest,
		Image:    *image,
	}, buildInfo, nil
}

func imageFromExporterResp(ctx context.Context, bkClient *buildkitclient.Client, exporterResp map[string]string) (*ocispec.Descriptor, *ocispec.Manifest, *ocispec.Image, error) {
	manifestDesc := exporterResp["containerimage.descriptor"]
	if manifestDesc == "" {
		return nil, nil, nil, fmt.Errorf("no manifest descriptor found in response")
	}

	data, err := base64.StdEncoding.DecodeString(manifestDesc)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode manifest descriptor: %w", err)
	}

	var descriptor ocispec.Descriptor
	if err := json.Unmarshal(data, &descriptor); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to parse manifest descriptor: %w", err)
	}

	manifestContent, err := readContent(ctx, bkClient, descriptor.Digest.String())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to read manifest content: %w", err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestContent, &manifest); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	// Get the config digest from the response
	configDigest := exporterResp["containerimage.config.digest"]
	if configDigest == "" {
		return nil, nil, nil, fmt.Errorf("no config digest found in response")
	}

	imageConfigData, err := readContent(ctx, bkClient, configDigest)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to read image config: %w", err)
	}

	var imageConfig ocispec.Image
	if err := json.Unmarshal(imageConfigData, &imageConfig); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to parse image config: %w", err)
	}

	return &descriptor, &manifest, &imageConfig, nil
}

func readContent(ctx context.Context, bkClient *buildkitclient.Client, digest string) ([]byte, error) {
	// Read the config content
	readClient, err := bkClient.ContentClient().Read(ctx, &content.ReadContentRequest{Digest: digest})
	if err != nil {
		return nil, fmt.Errorf("failed to read content: %w", err)
	}

	var buf bytes.Buffer

	// Read the config content
	for {
		msg, err := readClient.Recv()
		if err != nil {
			break
		}
		buf.Write(msg.Data)
	}

	return buf.Bytes(), nil
}
