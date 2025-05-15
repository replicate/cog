package docker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/docker/docker/api/types/registry"
	buildkitclient "github.com/moby/buildkit/client"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth"
	"github.com/moby/buildkit/session/secrets/secretsprovider"
	"github.com/moby/buildkit/util/progress/progressui"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/replicate/cog/pkg/docker/command"
)

func prepareDockerfileDir(buildDir string, dockerfileContents string) (string, error) {
	dockerfilePath := filepath.Join(buildDir, "Dockerfile")
	err := os.WriteFile(dockerfilePath, []byte(dockerfileContents), 0o644)
	if err != nil {
		return "", err
	}
	return dockerfilePath, nil
}

func solveOptFromImageOptions(buildDir string, opts command.ImageBuildOptions) (buildkitclient.SolveOpt, error) {
	dockerfilePath, err := prepareDockerfileDir(buildDir, opts.DockerfileContents)
	if err != nil {
		return buildkitclient.SolveOpt{}, err
	}

	// first, configure the frontend, in this case, dockerfile.v0
	frontendAttrs := map[string]string{
		// filename is the path to the Dockerfile within the "dockerfile" LocalDir context
		"filename": filepath.Base(dockerfilePath),
		// target is the name of a stage in a multi-stage Dockerfile
		// "target": opts.Target,
		// Replicate only supports linux/amd64, but local Docker Engine could be running on ARM,
		// including Apple Silicon. Force it to linux/amd64 for now.
		"platform": "linux/amd64",
	}

	// disable cache if requested
	if opts.NoCache {
		frontendAttrs["no-cache"] = ""
	}

	// add labels to the image
	for k, v := range opts.Labels {
		frontendAttrs["label:"+k] = v
	}

	// add build args to the image
	for k, v := range opts.BuildArgs {
		if v == nil {
			continue
		}
		frontendAttrs["build-arg:"+k] = *v
	}

	solveOpts := buildkitclient.SolveOpt{
		Frontend:      "dockerfile.v0",
		FrontendAttrs: frontendAttrs,
		LocalDirs: map[string]string{
			"dockerfile": filepath.Dir(dockerfilePath),
			"context":    opts.WorkingDir,
		},
		// Docker Engine's worker only supports three exporters.
		// "moby" exporter works best for cog, since we want to keep images in
		// Docker Engine's image store. The others are exporting images to somewhere else.
		// https://github.com/moby/moby/blob/v20.10.24/builder/builder-next/worker/worker.go#L221
		Exports: []buildkitclient.ExportEntry{
			{Type: "moby", Attrs: map[string]string{"name": opts.ImageName}},
		},
	}

	// add auth provider to the session so the local engine can pull and push images
	solveOpts.Session = append(
		solveOpts.Session,
		newBuildkitAuthProvider("r8.im"),
	)

	// add secrets to the session
	if len(opts.BuildSecrets) > 0 {
		secrets := make(map[string][]byte)
		for k, v := range opts.BuildSecrets {
			secrets[k] = []byte(v)
		}

		solveOpts.Session = append(
			solveOpts.Session,
			secretsprovider.FromMap(secrets),
		)
	}

	return solveOpts, nil
}

func newDisplay(statusCh chan *buildkitclient.SolveStatus) func() error {
	return func() error {
		display, err := progressui.NewDisplay(
			os.Stderr,
			progressui.DisplayMode(os.Getenv("BUILDKIT_PROGRESS")),
			// progressui.WithPhase("BUILDINGGGGG"),
			// progressui.WithDesc("SOMETEXT", "SOMECONSOLE"),
		)
		if err != nil {
			return err
		}

		// UpdateFrom must not use the incoming context.
		// Canceling this context kills the reader of statusCh which blocks buildkit.Client's Solve() indefinitely.
		// Solve() closes statusCh at the end and UpdateFrom returns by reading the closed channel.
		//
		// See https://github.com/superfly/flyctl/pull/2682 for the context.
		_, err = display.UpdateFrom(context.Background(), statusCh)
		return err

	}
}

func newBuildkitAuthProvider(registryHosts ...string) session.Attachable {
	return &buildkitAuthProvider{
		registryHosts: sync.OnceValues(func() (map[string]registry.AuthConfig, error) {
			return loadRegistryAuths(context.Background(), registryHosts...)
		}),
		// token: token,
	}
}

type buildkitAuthProvider struct {
	registryHosts func() (map[string]registry.AuthConfig, error)
	// auths         map[string]registry.AuthConfig
	// token string
}

func (ap *buildkitAuthProvider) Register(server *grpc.Server) {
	auth.RegisterAuthServer(server, ap)
}

func (ap *buildkitAuthProvider) Credentials(ctx context.Context, req *auth.CredentialsRequest) (*auth.CredentialsResponse, error) {
	auths, err := ap.registryHosts()
	if err != nil {
		return nil, fmt.Errorf("failed to load registry auth configs: %w", err)
	}
	res := &auth.CredentialsResponse{}
	if a, ok := auths[req.Host]; ok {
		res.Username = a.Username
		res.Secret = a.Password
	}

	return res, nil
}

func (ap *buildkitAuthProvider) FetchToken(ctx context.Context, req *auth.FetchTokenRequest) (*auth.FetchTokenResponse, error) {
	return nil, status.Errorf(codes.Unavailable, "client side tokens disabled")
}

func (ap *buildkitAuthProvider) GetTokenAuthority(ctx context.Context, req *auth.GetTokenAuthorityRequest) (*auth.GetTokenAuthorityResponse, error) {
	return nil, status.Errorf(codes.Unavailable, "client side tokens disabled")
}

func (ap *buildkitAuthProvider) VerifyTokenAuthority(ctx context.Context, req *auth.VerifyTokenAuthorityRequest) (*auth.VerifyTokenAuthorityResponse, error) {
	return nil, status.Errorf(codes.Unavailable, "client side tokens disabled")
}
