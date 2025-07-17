//go:build ignore

package factory

import (
	"bytes"
	"fmt"
	"io"

	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend/gateway/client"
	gatewayClient "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/solver/pb"

	"github.com/replicate/cog/pkg/dockerfile"
	"github.com/replicate/cog/pkg/model/factory/ops"
	"github.com/replicate/cog/pkg/model/factory/state"
	"github.com/replicate/cog/pkg/model/factory/types"
)

type PythonStack struct {
	*types.BuildEnv
}

func (stack *PythonStack) Solve(ctx types.Context, feClient gatewayClient.Client) (llb.State, error) {
	baseImg := "debian:bookworm-slim"

	baseState, err := ops.ResolveBaseImage(ctx, feClient, ctx.Platform, baseImg)
	if err != nil {
		return llb.State{}, err
	}

	intermediate, err := state.WithConfig(ctx, baseState,
		state.WithExposedPort("5000/tcp"),
		state.WithEntrypoint([]string{"/usr/bin/tini", "--"}),
		state.WithCmd([]string{"python", "-m", "cog.server.http"}),
		state.WithWorkingDir("/model-src"),
	)
	if err != nil {
		return llb.State{}, err
	}

	return ops.Do(
		ops.Layer("sysdeps",
			ops.NewAptInstall("tini"),
			// don't install pget for these tests since pget in PATH _forces_ a download from github releases!
			// ops.Download("https://github.com/replicate/pget/releases/latest/download/pget_Linux_x86_64", "/usr/local/bin/pget"),
		),
		ops.Layer("python",
			stack.installPython("3.12"),
		),
		ops.Layer("venv+model-deps",
			stack.initVENV(),
			stack.installModelDeps(),
		),
		ops.Layer("model",
			stack.installModel(),
			stack.installSchema(),
		),
		ops.Layer("hacks",
			stack.installFakePip(),
		),
	).Apply(ctx, intermediate)
}

func (stack *PythonStack) installPython(version string) ops.Operation {
	return ops.OpFunc(func(ctx types.Context, base llb.State) (llb.State, error) {

		intermediate := base
		uvCache := llb.AsPersistentCacheDir("uv-cache", llb.CacheMountLocked)

		intermediate = intermediate.AddEnv("UV_COMPILE_BYTECODE", "1")
		intermediate = intermediate.AddEnv("UV_LINK_MODE", "copy")
		intermediate = intermediate.AddEnv("UV_PYTHON_INSTALL_DIR", "/python")
		intermediate = intermediate.AddEnv("UV_PYTHON_PREFERENCE", "only-managed")

		intermediate = intermediate.Run(
			llb.Shlexf("/uv/uv python install %s", version),
			llb.AddMount("/uv", llb.Image("ghcr.io/astral-sh/uv:latest", llb.Platform(ctx.Platform), llb.ResolveModePreferLocal)),
			llb.AddMount("/root/.cache/uv", llb.Scratch(), uvCache),
		).Root()

		diff := llb.Diff(base, intermediate)
		final := base.File(
			llb.Copy(diff, "/python", "/python"),
			llb.WithCustomNamef("wat install python %s", version),
		)

		return final, nil
	})
}

func (stack *PythonStack) initVENV() ops.Operation {
	return ops.OpFunc(func(ctx types.Context, base llb.State) (llb.State, error) {

		intermediate := base
		uvCache := llb.AsPersistentCacheDir("uv-cache", llb.CacheMountLocked)

		intermediate = intermediate.AddEnv("UV_COMPILE_BYTECODE", "1")
		intermediate = intermediate.AddEnv("UV_LINK_MODE", "copy")
		intermediate = intermediate.AddEnv("UV_PYTHON_INSTALL_DIR", "/python")
		intermediate = intermediate.AddEnv("UV_PYTHON_PREFERENCE", "only-managed")

		intermediate = intermediate.Run(
			llb.Shlexf("/uv/uv venv /venv --python %s", "3.12"),
			llb.WithCustomName("init venv"),
			llb.AddMount("/uv", llb.Image("ghcr.io/astral-sh/uv:latest", llb.Platform(ctx.Platform))),
			llb.AddMount("/root/.cache/uv", llb.Scratch(), uvCache),
		).Root()

		intermediate, err := state.PrependPath(ctx, intermediate, "/venv/bin")
		if err != nil {
			return llb.State{}, err
		}

		return intermediate, nil
	})
}

func (stack *PythonStack) installModelDeps() ops.Operation {
	return ops.OpFunc(func(ctx types.Context, base llb.State) (llb.State, error) {
		intermediate := base

		intermediate = intermediate.AddEnv("UV_COMPILE_BYTECODE", "1")
		intermediate = intermediate.AddEnv("UV_LINK_MODE", "copy")
		intermediate = intermediate.AddEnv("UV_PYTHON_INSTALL_DIR", "/python")
		intermediate = intermediate.AddEnv("UV_PYTHON_PREFERENCE", "only-managed")

		// Create UV cache mount for faster builds
		uvCache := llb.AsPersistentCacheDir("uv-cache", llb.CacheMountLocked)
		uvImage := llb.Image("ghcr.io/astral-sh/uv:latest", llb.Platform(ctx.Platform), llb.ResolveModePreferLocal)

		// Get the embedded cog wheel file
		wheelData, wheelFilename, err := dockerfile.ReadWheelFile()
		if err != nil {
			return base, fmt.Errorf("failed to read embedded cog wheel: %w", err)
		}

		// Copy the wheel file to the container
		wheelPath := "/tmp/" + wheelFilename
		intermediate = intermediate.File(
			llb.Mkfile(wheelPath, 0x644, wheelData),
			llb.WithCustomName("copy-cog-wheel"),
		)

		// Install the cog wheel file and pydantic dependency
		intermediate = intermediate.Run(
			llb.Shlexf("/uv/uv pip install --python /venv/bin/python %s 'pydantic>=1.9,<3'", wheelPath),
			llb.AddMount("/root/.cache/uv", llb.Scratch(), uvCache),
			llb.AddMount("/uv", uvImage),
			llb.WithCustomName("uv-install-cog-wheel"),
		).Root()

		intermediate = intermediate.File(llb.Rm(wheelPath))

		// If Python requirements are specified, install them as well
		if ctx.Config.Build.PythonRequirements != "" {
			intermediate = intermediate.Run(
				llb.Shlexf("/uv/uv pip install --python /venv/bin/python -r %s", ctx.Config.Build.PythonRequirements),
				llb.AddMount("/root/.cache/uv", llb.Scratch(), uvCache),
				llb.AddMount("/uv", uvImage),
				llb.WithCustomNamef("uv-install-requirements %s", ctx.Config.Build.PythonRequirements),
			).Root()
		}

		diff := llb.Diff(base, intermediate)
		final := base.File(
			llb.Copy(diff, "/", "/", llb.WithExcludePatterns([]string{"/root/.cache"})),
			llb.WithCustomName("install model deps"),
		)

		return final, nil
	})
}

func (stack *PythonStack) installModel() ops.Operation {
	return ops.OpFunc(func(ctx types.Context, base llb.State) (llb.State, error) {
		intermediate := base

		// why do we need to do this twice?
		intermediate = intermediate.Dir("/model-src")

		// Copy the context files
		intermediate = intermediate.File(
			llb.Copy(
				llb.Local("context"),
				".",
				".",
				llb.WithExcludePatterns([]string{".cog", "__pycache__"}),
			),
			// llb.IgnoreCache,
			llb.WithCustomName("copy context"),
		)

		return intermediate, nil
	})
}

func (stack *PythonStack) installSchema() ops.Operation {
	return ops.OpFunc(func(ctx types.Context, base llb.State) (llb.State, error) {
		intermediate := base

		schemaData, err := stack.generateSchemaInContainer(ctx, intermediate)
		if err != nil {
			return llb.State{}, fmt.Errorf("failed to generate schema: %w", err)
		}

		intermediate, err = state.SetLabel(ctx, intermediate, types.LabelOpenAPISchema, string(schemaData))
		if err != nil {
			return llb.State{}, fmt.Errorf("failed to set label: %w", err)
		}
		intermediate = intermediate.File(llb.Mkfile("schema.json", 0x644, schemaData))

		return intermediate, nil
	})
}

func (stack *PythonStack) generateSchemaInContainer(ctx types.Context, base llb.State) ([]byte, error) {
	def, err := base.Marshal(ctx)
	if err != nil {
		return nil, err
	}

	// fmt.Println("generate Schema solve")
	res, err := ctx.Client.Solve(ctx, client.SolveRequest{
		Definition: def.ToPB(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to solve build: %w", err)
	}

	container, err := ctx.Client.NewContainer(ctx, client.NewContainerRequest{
		Platform: &pb.Platform{
			OS:           ctx.Platform.OS,
			Architecture: ctx.Platform.Architecture,
			Variant:      ctx.Platform.Variant,
			OSVersion:    ctx.Platform.OSVersion,
			OSFeatures:   ctx.Platform.OSFeatures,
		},
		Mounts: []client.Mount{
			{
				Dest: "/",
				Ref:  res.Ref,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}
	defer func() {
		if err := container.Release(ctx); err != nil {
			fmt.Printf("failed to release container: %T\n", err)
			fmt.Printf("failed to release container: %s\n", err)
		}
	}()

	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()

	var stdout bytes.Buffer
	go func() {
		_, _ = io.Copy(&stdout, stdoutR)
		stdoutR.Close()
	}()
	var stderr bytes.Buffer
	go func() {
		_, _ = io.Copy(&stderr, stderrR)
		stderrR.Close()
	}()

	env, err := state.GetEnv(ctx, base)
	if err != nil {
		return nil, fmt.Errorf("failed to get env: %w", err)
	}

	process, err := container.Start(ctx, client.StartRequest{
		Cwd:    "/model-src",
		Args:   []string{"python", "-m", "cog.command.openapi_schema"},
		Stdout: stdoutW,
		Stderr: stderrW,
		Env:    env,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	if err := process.Wait(); err != nil {
		return nil, fmt.Errorf("failed to wait for process: (STDOUT: %s, STDERR: %s) %w", stdout.String(), stderr.String(), err)
	}

	stdoutStr := stdout.String()
	stderrStr := stderr.String()

	if stderrStr != "" {
		return nil, fmt.Errorf("stderr: %s", stderrStr)
	}

	// fmt.Println("stdout", stdoutStr)

	return []byte(stdoutStr), nil
}

// r8 runtime overrides the entrypoint with a script that may run `pip`, which these self contained images don't have. This
// operation injects a noop `pip` module into the python venv so boot won't fail.
func (stack *PythonStack) installFakePip() ops.Operation {
	return ops.OpFunc(func(ctx types.Context, base llb.State) (llb.State, error) {
		intermediate := base

		// Create the dummy pip __main__.py content
		pipMainContent := []byte(`#!/usr/bin/env python3
import sys
import os
# Dummy pip that handles all commands silently
if os.environ.get('DEBUG_DUMMY_PIP'):
    print(f'dummy-pip: ignoring command: {" ".join(sys.argv)}', file=sys.stderr)
sys.exit(0)
`)

		// Create the __init__.py content (empty)
		initContent := []byte("")

		intermediate = intermediate.File(
			llb.Mkdir("/venv/lib/python3.12/site-packages/pip", 0x755),
			llb.WithCustomName("create-pip-directory"),
		)

		// Create the directory and files
		intermediate = intermediate.Run(
			llb.Shlex("mkdir -p /venv/lib/python3.12/site-packages/pip"),
			llb.WithCustomName("create-pip-directory"),
		).Root()

		intermediate = intermediate.File(
			llb.Mkfile("/venv/lib/python3.12/site-packages/pip/__main__.py", 0x644, pipMainContent),
			llb.WithCustomName("create-pip-main"),
		)

		intermediate = intermediate.File(
			llb.Mkfile("/venv/lib/python3.12/site-packages/pip/__init__.py", 0x644, initContent),
			llb.WithCustomName("create-pip-init"),
		)

		return intermediate, nil
	})
}
