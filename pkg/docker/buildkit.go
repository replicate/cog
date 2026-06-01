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
	"github.com/tonistiigi/fsutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	cogconfig "github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/util/console"
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
		"syntax":   "docker/dockerfile:1",
		// TODO[md]: support multi-stage target
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

	// Add SOURCE_DATE_EPOCH if Epoch is set
	if opts.Epoch != nil && *opts.Epoch >= 0 {
		frontendAttrs["build-arg:SOURCE_DATE_EPOCH"] = fmt.Sprintf("%d", *opts.Epoch)
	}

	localMounts := make(map[string]fsutil.FS)

	// Dockerfile mount — filtered to only the Dockerfile so that other
	// files in the same directory (e.g. when DockerfileDir points at
	// .cog/build/ alongside wheels and requirements) are not exposed
	// through this mount.
	dockerfileFS, err := fsutil.NewFS(filepath.Dir(dockerfilePath))
	if err != nil {
		return buildkitclient.SolveOpt{}, fmt.Errorf("create dockerfile fs: %w", err)
	}
	dockerfileFS, err = fsutil.NewFilterFS(dockerfileFS, &fsutil.FilterOpt{
		IncludePatterns: []string{filepath.Base(dockerfilePath)},
	})
	if err != nil {
		return buildkitclient.SolveOpt{}, fmt.Errorf("filter dockerfile fs: %w", err)
	}
	localMounts["dockerfile"] = dockerfileFS

	// Context mount — optionally filtered by ExcludePatterns so callers
	// can prevent large directories (e.g. .cog/) from being sent to the
	// daemon without mutating .dockerignore on disk.

	contextDir := opts.ContextDir

	// Use WorkingDir as context if ContextDir is relative to ensure consistency with CLI client
	if opts.WorkingDir != "" && !filepath.IsAbs(opts.ContextDir) {
		contextDir = filepath.Join(opts.WorkingDir, opts.ContextDir)
	}

	if contextDir != "" {
		var contextFS fsutil.FS
		contextFS, err = fsutil.NewFS(contextDir)
		if err != nil {
			return buildkitclient.SolveOpt{}, fmt.Errorf("create context fs: %w", err)
		}
		if len(opts.ExcludePatterns) > 0 {
			contextFS, err = fsutil.NewFilterFS(contextFS, &fsutil.FilterOpt{
				ExcludePatterns: opts.ExcludePatterns,
			})
			if err != nil {
				return buildkitclient.SolveOpt{}, fmt.Errorf("create filtered context fs: %w", err)
			}
		}
		localMounts["context"] = contextFS
	}

	// Add user-supplied build contexts, but don't overwrite 'dockerfile' or 'context'
	for name, dir := range opts.BuildContexts {
		if name == "dockerfile" || name == "context" {
			console.Warnf("build context name collision: %q", name)
			continue
		}
		bcFS, fsErr := fsutil.NewFS(dir)
		if fsErr != nil {
			return buildkitclient.SolveOpt{}, fmt.Errorf("create build context fs %q: %w", name, fsErr)
		}
		localMounts[name] = bcFS
		// Tell the dockerfile frontend about this build context
		frontendAttrs["context:"+name] = "local:" + name
	}

	// Set exporter attributes
	exporterAttrs := map[string]string{
		"name": opts.ImageName,
	}

	// if SOURCE_DATE_EPOCH is present in the build args, tell the frontend to rewrite timestamps
	if _, ok := frontendAttrs["build-arg:SOURCE_DATE_EPOCH"]; ok {
		exporterAttrs["rewrite-timestamp"] = "true"
	}

	solveOpts := buildkitclient.SolveOpt{
		Frontend:      "dockerfile.v0",
		FrontendAttrs: frontendAttrs,
		LocalMounts:   localMounts,
		// Docker Engine's worker only supports three exporters.
		// "moby" exporter works best for cog, since we want to keep images in
		// Docker Engine's image store. The others are exporting images to somewhere else.
		// https://github.com/moby/moby/blob/v20.10.24/builder/builder-next/worker/worker.go#L221
		Exports: []buildkitclient.ExportEntry{
			{Type: "moby", Attrs: exporterAttrs},
		},
	}

	// add auth provider to the session so the local engine can pull and push images
	solveOpts.Session = append(
		solveOpts.Session,
		newBuildkitAuthProvider("r8.im"),
	)

	// add secrets to the session
	if len(opts.Secrets) > 0 {
		// TODO[md]: support secrets direct from input in addition to env+file
		store, err := ParseSecretsFromHost(opts.WorkingDir, opts.Secrets)
		if err != nil {
			return buildkitclient.SolveOpt{}, fmt.Errorf("failed to parse secrets: %w", err)
		}
		solveOpts.Session = append(solveOpts.Session, secretsprovider.NewSecretProvider(store))
	}

	// Set cache imports/exports to match DockerCommand logic
	// If cogconfig.BuildXCachePath is set, use local cache; otherwise, use inline
	if cogconfig.BuildXCachePath != "" {
		solveOpts.CacheImports = []buildkitclient.CacheOptionsEntry{
			{Type: "local", Attrs: map[string]string{"src": cogconfig.BuildXCachePath}},
		}
		solveOpts.CacheExports = []buildkitclient.CacheOptionsEntry{
			{Type: "local", Attrs: map[string]string{"dest": cogconfig.BuildXCachePath}},
		}
	} else {
		solveOpts.CacheExports = []buildkitclient.CacheOptionsEntry{
			{Type: "inline"},
		}
	}

	return solveOpts, nil
}

func newDisplay(statusCh chan *buildkitclient.SolveStatus, displayMode string) func() error {
	return func() error {
		display, err := progressui.NewDisplay(
			os.Stderr,
			progressui.DisplayMode(displayMode),
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
		// TODO[md]: here's where we'd set the token from config rather than fetching from the credentials helper
		// token: token,
	}
}

type buildkitAuthProvider struct {
	registryHosts func() (map[string]registry.AuthConfig, error)
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
