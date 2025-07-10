package model

// // setup commands

// func NewModelBuilder(ctx context.Context, provider command.ClientProvider, config *config.Config, workingDir string) (*ModelBuilder, error) {
// 	bc, err := provider.BuildKitClient(ctx)
// 	if err != nil {
// 		return nil, err
// 	}

// 	return &ModelBuilder{
// 		provider:   provider,
// 		Config:     config,
// 		WorkingDir: workingDir,
// 		bc:         bc,
// 		platform: ocispec.Platform{
// 			OS:           "linux",
// 			Architecture: "amd64",
// 		},
// 		exportImage: &ocispec.Image{
// 			Platform: ocispec.Platform{
// 				OS:           "linux",
// 				Architecture: "amd64",
// 			},
// 			Config: ocispec.ImageConfig{
// 				Labels: map[string]string{},
// 			},
// 		},
// 	}, nil
// }

// type ModelBuilder struct {
// 	Config     *config.Config
// 	WorkingDir string
// 	provider   command.ClientProvider
// 	bc         *buildkitclient.Client

// 	platform ocispec.Platform

// 	exportImage *ocispec.Image
// 	baseEnv     []string
// }

// // tag := "r8.im/pipelines-runtime:latest"
// func (mb *ModelBuilder) solveBase(ctx context.Context) (string, llb.State, error) {
// 	// tag := "pipeline-base:latest"
// 	// tag := "local/pipelines-base-standalone:latest"
// 	// tag := "pipelines-base-standalone"
// 	tag := "cog-pipelines-base"
// 	fmt.Println("base tag", tag)

// 	// Get environment variables from the base image
// 	baseEnv, err := mb.getImageEnvironment(ctx, tag)
// 	if err != nil {
// 		fmt.Printf("Warning: failed to get environment from base image %s: %v\n", tag, err)
// 		fmt.Printf("Continuing with default environment...\n")
// 		// Continue with build even if we can't get base environment
// 	} else {
// 		fmt.Printf("Successfully retrieved %d environment variables from base image\n", len(baseEnv))
// 		fmt.Println(baseEnv)
// 		mb.baseEnv = baseEnv
// 	}

// 	return tag, llb.Image(tag, llb.Platform(mb.platform)), nil
// }

// func squash2(name string, base, target llb.State) (llb.State, error) {
// 	diff := llb.Diff(base, target)
// 	out := base.File(llb.Copy(diff, "/", "/"), llb.WithCustomName(name))

// 	return out, nil
// }

// func (mb *ModelBuilder) solveSystemDeps(ctx context.Context, base llb.State) (llb.State, error) {
// 	packages := mb.Config.Build.SystemPackages
// 	if len(packages) == 0 {
// 		return base, nil
// 	}

// 	aptCache := llb.AsPersistentCacheDir("apt-cache", llb.CacheMountLocked)
// 	pkgList := strings.Join(packages, " ")

// 	// 1. apt-get update
// 	intermediate := base.Run(
// 		llb.Shlex("apt-get update -qq"),
// 		llb.AddMount("/var/cache/apt", llb.Scratch(), aptCache),
// 		llb.WithCustomName("apt-update"),
// 	).Root()

// 	// 2. apt-get install
// 	intermediate = intermediate.Run(
// 		llb.Shlex(fmt.Sprintf("apt-get install -qqy --no-install-recommends %s", pkgList)),
// 		llb.AddMount("/var/cache/apt", llb.Scratch(), aptCache),
// 		llb.WithCustomNamef("apt-install %s", pkgList),
// 	).Root()

// 	// 3. cleanup
// 	intermediate = intermediate.Run(
// 		llb.Shlex("rm -rf /var/lib/apt/lists/*"),
// 		llb.WithCustomName("apt-clean"),
// 	).Root()

// 	// install uv
// 	// intermediate = intermediate.File(
// 	// 	// llb.Copy(
// 	// 		llb.WithCustomName("install python"),
// 	// 	// 	"/usr/local/bin/uv",
// 	// 	// 	"/usr/local/bin/uv",
// 	// 	// ),
// 	// 	llb.AddMount()
// 	// )

// 	// pythonVersion := mb.Config.Build.PythonVersion
// 	// if pythonVersion == "" {
// 	// 	pythonVersion = "3.12"
// 	// }

// 	// intermediate = intermediate.AddEnv("ENV UV_PYTHON_INSTALL_DIR", "/python")

// 	// intermediate = intermediate.Run(
// 	// 	llb.Shlexf("/uv/uv python install %s", pythonVersion),
// 	// 	llb.WithCustomName("install python"),
// 	// 	llb.AddMount("/uv", llb.Image("ghcr.io/astral-sh/uv:latest", llb.Platform(mb.platform))),
// 	// ).Root()

// 	// intermediate = intermediate.Run(

// 	// 	llb.Shlex("uv install --frozen"),
// 	// 	llb.WithCustomName("install uv"),
// 	// ).Root()

// 	return squash2("system-deps", base, intermediate)
// }

// func (mb *ModelBuilder) solveModelDeps(ctx context.Context, base llb.State) (llb.State, error) {
// 	// Check for deprecated python_packages
// 	if len(mb.Config.Build.PythonPackages) > 0 {
// 		return base, fmt.Errorf("python_packages is no longer supported, use python_requirements instead")
// 	}

// 	// Create UV cache mount for faster builds
// 	uvCache := llb.AsPersistentCacheDir("uv-cache", llb.CacheMountLocked)

// 	// Set working directory to /model-src where the requirements.txt will be copied
// 	intermediate := base.Dir("/model-src")

// 	// Get the embedded cog wheel file
// 	wheelData, wheelFilename, err := dockerfile.ReadWheelFile()
// 	if err != nil {
// 		return base, fmt.Errorf("failed to read embedded cog wheel: %w", err)
// 	}

// 	// Copy the wheel file to the container
// 	wheelPath := "/tmp/" + wheelFilename
// 	intermediate = intermediate.File(
// 		llb.Mkfile(wheelPath, 0644, wheelData),
// 		llb.WithCustomName("copy-cog-wheel"),
// 	)

// 	// Install the cog wheel file and pydantic dependency
// 	intermediate = intermediate.Run(
// 		llb.Shlexf("/bin/uv pip install --python /venv/bin/python %s 'pydantic>=1.9,<3'", wheelPath),
// 		llb.AddMount("/root/.cache/uv", llb.Scratch(), uvCache),
// 		llb.WithCustomName("uv-install-cog-wheel"),
// 	).Root()

// 	// If Python requirements are specified, install them as well
// 	if mb.Config.Build.PythonRequirements != "" {
// 		intermediate = intermediate.Run(
// 			llb.Shlexf("/bin/uv pip install --python /venv/bin/python -r %s", mb.Config.Build.PythonRequirements),
// 			llb.AddMount("/root/.cache/uv", llb.Scratch(), uvCache),
// 			llb.WithCustomNamef("uv-install-requirements %s", mb.Config.Build.PythonRequirements),
// 		).Root()
// 	}

// 	return squash2("model-deps", base, intermediate)
// }

// func (mb *ModelBuilder) solveModel(ctx context.Context, c client.Client, base llb.State) (llb.State, error) {
// 	// Copy the working directory to /src
// 	intermediate := base.Dir("/model-src")

// 	workingDir, err := intermediate.GetDir(ctx)
// 	if err != nil {
// 		return llb.State{}, fmt.Errorf("failed to get model base: %w", err)
// 	}
// 	fmt.Println("modelBase dir", workingDir)

// 	// Copy the context files
// 	intermediate = intermediate.File(
// 		llb.Copy(
// 			llb.Local("context"),
// 			".",
// 			".",
// 		),
// 		llb.IgnoreCache,
// 	)

// 	schemaData, err := mb.generateSchemaInContainer(ctx, c, intermediate)
// 	if err != nil {
// 		return llb.State{}, fmt.Errorf("failed to generate schema: %w", err)
// 	}

// 	mb.exportImage.Config.Labels[global.LabelNamespace+"openapi_schema"] = string(schemaData)

// 	intermediate = intermediate.File(llb.Mkfile("schema.json", 0x644, schemaData))

// 	return squash2("model", base, intermediate)
// }

// func (mb *ModelBuilder) solvePipelineModel(ctx context.Context, c client.Client, base llb.State) (llb.State, error) {
// 	// Copy the working directory to /src
// 	intermediate := base.Dir("/model-src")

// 	workingDir, err := intermediate.GetDir(ctx)
// 	if err != nil {
// 		return llb.State{}, fmt.Errorf("failed to get model base: %w", err)
// 	}
// 	fmt.Println("modelBase dir", workingDir)

// 	// Copy the context files
// 	intermediate = intermediate.File(
// 		llb.Copy(
// 			llb.Local("context"),
// 			".",
// 			".",
// 		),
// 		llb.IgnoreCache,
// 	)

// 	schemaData, err := mb.generateSchemaInContainer(ctx, c, intermediate)
// 	if err != nil {
// 		return llb.State{}, fmt.Errorf("failed to generate schema: %w", err)
// 	}

// 	mb.exportImage.Config.Labels[global.LabelNamespace+"openapi_schema"] = string(schemaData)

// 	intermediate = intermediate.File(llb.Mkfile("schema.json", 0x644, schemaData))

// 	return squash2("pipeline-model", base, intermediate)
// }

// // func (mb *ModelBuilder) generateSchema(ctx context.Context, target llb.State) (llb.State, error) {
// // 	schema := target.Run(
// // 		llb.Shlex("python -m cog.command.openapi_schema > /schema.json"),
// // 		llb.WithCustomName("generate-schema"),
// // 	).Root()

// // 	return schema, nil
// // }

// type result struct {
// 	state  *llb.State
// 	labels map[string]string
// }

// type buildFunc func(ctx context.Context, c client.Client, baseState, intermediateState llb.State) (llb.State, error)

// func (b *ModelBuilder) Build(ctx context.Context, tag string) (*Model, error) {
// 	fmt.Println("Building model with buildkit client")

// 	bc, err := b.provider.BuildKitClient(ctx)
// 	if err != nil {
// 		return nil, err
// 	}

// 	contextFS, err := fsutil.NewFS(b.WorkingDir)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to create context FS: %w", err)
// 	}

// 	// Create solve options
// 	solveOpt := buildkitclient.SolveOpt{
// 		Exports: []buildkitclient.ExportEntry{
// 			{
// 				Type: "moby",
// 				Attrs: map[string]string{
// 					"name": tag,
// 				},
// 			},
// 		},
// 		LocalMounts: map[string]fsutil.FS{
// 			"context": contextFS,
// 		},
// 	}

// 	// Create a status channel for build progress
// 	statusCh := make(chan *buildkitclient.SolveStatus)

// 	eg, egctx := errgroup.WithContext(ctx)

// 	eg.Go(docker.NewBuildKitSolveDisplay(statusCh, "plain"))

// 	var exporterResp map[string]string

// 	eg.Go(func() error {
// 		x, err := bc.Build(
// 			egctx,
// 			solveOpt,
// 			"wat",
// 			func(ctx context.Context, c client.Client) (*client.Result, error) {
// 				_, base, err := b.solveBase(ctx)
// 				if err != nil {
// 					return nil, err
// 				}

// 				systemDepsLayer, err := b.solveSystemDeps(ctx, base)
// 				if err != nil {
// 					return nil, err
// 				}

// 				modelDeps, err := b.solveModelDeps(ctx, systemDepsLayer)
// 				if err != nil {
// 					return nil, err
// 				}

// 				model, err := b.solveModel(ctx, c, modelDeps)
// 				if err != nil {
// 					return nil, err
// 				}

// 				// Get the definition from the state
// 				def, err := model.Marshal(ctx)
// 				if err != nil {
// 					return nil, fmt.Errorf("failed to marshal state: %w", err)
// 				}
// 				res, err := c.Solve(ctx, client.SolveRequest{Definition: def.ToPB()})
// 				if err != nil {
// 					return nil, fmt.Errorf("failed to solve build: %w", err)
// 				}

// 				b.exportImage.Config.Entrypoint = []string{"/usr/bin/tini", "--"}
// 				b.exportImage.Config.Cmd = []string{"python", "-m", "cog.server.http"}
// 				b.exportImage.Config.ExposedPorts = map[string]struct{}{
// 					"5000/tcp": {},
// 				}
// 				b.exportImage.Config.WorkingDir = "/model-src"

// 				// Use environment variables from base image if available
// 				if len(b.baseEnv) > 0 {
// 					// Start with base environment and add our custom variables
// 					envVars := make([]string, 0, len(b.baseEnv)+1)
// 					envVars = append(envVars, b.baseEnv...)

// 					// Add PYTHONUNBUFFERED if not already present
// 					hasUnbuffered := false
// 					for _, env := range b.baseEnv {
// 						if strings.HasPrefix(env, "PYTHONUNBUFFERED=") {
// 							hasUnbuffered = true
// 							break
// 						}
// 					}
// 					if !hasUnbuffered {
// 						envVars = append(envVars, "PYTHONUNBUFFERED=1")
// 					}

// 					b.exportImage.Config.Env = envVars
// 				} else {
// 					// Fallback to default environment if base env not available
// 					b.exportImage.Config.Env = []string{
// 						"PYTHONUNBUFFERED=1",
// 						"PATH=/venv/bin:$PATH",
// 					}
// 				}
// 				for _, layer := range b.exportImage.History {
// 					fmt.Println("history.layer", layer)
// 				}

// 				cfgJSON, err := json.Marshal(b.exportImage)
// 				if err != nil {
// 					return nil, fmt.Errorf("failed to marshal image config: %w", err)
// 				}

// 				// // util.PrettyPrintJSON(res)

// 				// fmt.Println("================ INSPECT REF ================")
// 				// if ref, ok := res.FindRef("model"); ok {
// 				// 	fmt.Println("found model ref", ref)
// 				// } else {
// 				// 	fmt.Println("model ref not found")
// 				// }

// 				// if ref, ok := res.FindRef("not-found"); ok {
// 				// 	fmt.Println("found not-found ref", ref)
// 				// } else {
// 				// 	fmt.Println("not-found ref not found")
// 				// }

// 				// // ref.Evaluate(ctx)
// 				// // fmt.Println("ref", ref)
// 				// // res.Ref.Evaluate(ctx)
// 				// // for name, ref := range res.Refs {
// 				// // 	fmt.Println("ref", name, ref)

// 				// // }
// 				// fmt.Println("===============================================")

// 				//------------------------------------------------------------------
// 				// 3) Return result with rootfs + custom config blob
// 				//------------------------------------------------------------------
// 				out := &client.Result{}
// 				out.AddMeta("yo", []byte("yo"))
// 				out.SetRef(res.Ref)                           // filesystem
// 				out.AddMeta("containerimage.config", cfgJSON) // config blob

// 				// (optional) keep your role slice for later mutate step
// 				// out.AddMeta("rep.layer.roles", []byte(strings.Join(diffRole, ",")))

// 				return out, nil
// 			},
// 			statusCh,
// 		)
// 		if err != nil {
// 			return fmt.Errorf("failed to solve build: %w", err)
// 		}
// 		fmt.Println("x", x)
// 		exporterResp = x.ExporterResponse

// 		return nil
// 	})

// 	if err := eg.Wait(); err != nil {
// 		return nil, err
// 	}

// 	util.PrettyPrintJSON(exporterResp)

// 	manifestDescriptor, manifest, err := b.manifestFromExporterResp(ctx, exporterResp)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to get manifest descriptor: %w", err)
// 	}

// 	fmt.Println("=== manifest descriptor ===")
// 	util.PrettyPrintJSON(manifestDescriptor)

// 	fmt.Println("=== manifest content ===")
// 	util.PrettyPrintJSON(manifest)

// 	// Get the config digest from the response
// 	configDigest := exporterResp["containerimage.config.digest"]
// 	if configDigest == "" {
// 		return nil, fmt.Errorf("no config digest found in response")
// 	}

// 	imageConfig, err := b.readImageConfig(ctx, configDigest)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to read image config: %w", err)
// 	}

// 	util.PrettyPrintJSON(imageConfig)

// 	fmt.Println("info")
// 	info, err := bc.ContentClient().Info(ctx, &content.InfoRequest{
// 		Digest: string(imageConfig.RootFS.DiffIDs[4]),
// 	})
// 	if err != nil {
// 		fmt.Printf("error getting info: %s\n", err)
// 	}
// 	fmt.Printf("info: %+v\n", info)

// 	// Print layer information
// 	fmt.Println("\nLayer Information:")
// 	for i, diffID := range imageConfig.RootFS.DiffIDs {
// 		if i >= len(manifest.Layers) {
// 			fmt.Printf("Layer %d: %s (diffID) - no corresponding layer in manifest\n", i, diffID)
// 			continue
// 		}

// 		layer := manifest.Layers[i]
// 		var sizeStr string
// 		if layer.Size < 0 {
// 			sizeStr = "unknown"
// 		} else {
// 			sizeStr = humanize.Bytes(uint64(layer.Size))
// 		}
// 		fmt.Printf("Layer %d: %s (diffID) - size: %s (layer digest: %s)\n", i, diffID, sizeStr, layer.Digest.String())
// 	}

// 	ref, err := name.ParseReference(tag)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to parse reference: %w", err)
// 	}

// 	return &Model{
// 		ref:      ref,
// 		Image:    imageConfig,
// 		Manifest: manifest,
// 	}, nil
// }

// func (mb *ModelBuilder) generateSchemaInContainer(ctx context.Context, c client.Client, target llb.State) ([]byte, error) {
// 	def, err := target.Marshal(ctx)
// 	if err != nil {
// 		return nil, err
// 	}

// 	fmt.Println("generate Schema solve")
// 	res, err := c.Solve(ctx, client.SolveRequest{
// 		Definition: def.ToPB(),
// 	})
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to solve build: %w", err)
// 	}

// 	fmt.Println("A")
// 	container, err := c.NewContainer(ctx, client.NewContainerRequest{
// 		Platform: &pb.Platform{
// 			OS:           "linux",
// 			Architecture: "amd64",
// 		},
// 		Mounts: []client.Mount{
// 			{
// 				Dest: "/",
// 				Ref:  res.Ref,
// 			},
// 		},
// 	})
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to create container: %w", err)
// 	}
// 	defer func() {
// 		if err := container.Release(ctx); err != nil {
// 			fmt.Printf("failed to release container: %T\n", err)
// 			fmt.Printf("failed to release container: %s\n", err)
// 		}
// 	}()

// 	fmt.Println("B")

// 	stdoutR, stdoutW := io.Pipe()
// 	stderrR, stderrW := io.Pipe()

// 	var stdout bytes.Buffer
// 	go func() {
// 		_, _ = io.Copy(&stdout, stdoutR)
// 		stdoutR.Close()
// 	}()
// 	var stderr bytes.Buffer
// 	go func() {
// 		_, _ = io.Copy(&stderr, stderrR)
// 		stderrR.Close()
// 	}()

// 	// Use environment variables from base image if available
// 	var envVars []string
// 	var pythonPath string

// 	if len(mb.baseEnv) > 0 {
// 		// Use environment from base image
// 		envVars = make([]string, len(mb.baseEnv))
// 		copy(envVars, mb.baseEnv)

// 		// Extract Python path from base environment
// 		pythonPath = "/venv/bin/python" // default from Dockerfile
// 		for _, env := range mb.baseEnv {
// 			if strings.HasPrefix(env, "PATH=") {
// 				pathValue := strings.TrimPrefix(env, "PATH=")
// 				// Look for python in the first directory of PATH
// 				if strings.Contains(pathValue, "/venv/bin") {
// 					pythonPath = "/venv/bin/python"
// 				} else if strings.Contains(pathValue, "/pipelines-runtime/.venv/bin") {
// 					pythonPath = "/pipelines-runtime/.venv/bin/python"
// 				}
// 				break
// 			}
// 		}
// 	} else {
// 		// Fallback to default environment if base env not available
// 		envVars = []string{
// 			"PATH=/venv/bin:$PATH",
// 			"PYTHONUNBUFFERED=1",
// 		}
// 		pythonPath = "/venv/bin/python"
// 	}

// 	process, err := container.Start(ctx, client.StartRequest{
// 		Cwd:    "/model-src",
// 		Args:   []string{pythonPath, "-m", "cog.command.openapi_schema"},
// 		Stdout: stdoutW,
// 		Stderr: stderrW,
// 		Env:    envVars,
// 	})
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to start container: %w", err)
// 	}
// 	fmt.Println("C")

// 	if err := process.Wait(); err != nil {
// 		fmt.Println("stdout", stdout.String())
// 		fmt.Println("stderr", stderr.String())
// 		return nil, fmt.Errorf("failed to wait for process: %w", err)
// 	}

// 	fmt.Println("D")

// 	stdoutStr := stdout.String()
// 	stderrStr := stderr.String()

// 	// fmt.Println("stdout", stdoutStr)
// 	// fmt.Println("stderr", stderrStr)

// 	if stderrStr != "" {
// 		return nil, fmt.Errorf("stderr: %s", stderrStr)
// 	}

// 	fmt.Println("stdout", stdoutStr)

// 	return []byte(stdoutStr), nil
// }

// func (b *ModelBuilder) readContent(ctx context.Context, digest string) ([]byte, error) {
// 	// Read the config content
// 	readClient, err := b.bc.ContentClient().Read(ctx, &content.ReadContentRequest{Digest: digest})
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to read content: %w", err)
// 	}

// 	var buf bytes.Buffer

// 	// Read the config content
// 	for {
// 		msg, err := readClient.Recv()
// 		if err != nil {
// 			break
// 		}
// 		buf.Write(msg.Data)
// 	}

// 	return buf.Bytes(), nil
// }

// func (b *ModelBuilder) readImageConfig(ctx context.Context, digest string) (ocispec.Image, error) {
// 	content, err := b.readContent(ctx, digest)
// 	if err != nil {
// 		return ocispec.Image{}, fmt.Errorf("failed to read content: %w", err)
// 	}

// 	var imageConfig ocispec.Image
// 	if err := json.Unmarshal(content, &imageConfig); err != nil {
// 		return ocispec.Image{}, fmt.Errorf("failed to parse image config: %w", err)
// 	}

// 	return imageConfig, nil
// }

// func (b *ModelBuilder) manifestFromExporterResp(ctx context.Context, exporterResp map[string]string) (*ocispec.Descriptor, *ocispec.Manifest, error) {
// 	manifestDesc := exporterResp["containerimage.descriptor"]
// 	if manifestDesc == "" {
// 		return nil, nil, fmt.Errorf("no manifest descriptor found in response")
// 	}

// 	data, err := base64.StdEncoding.DecodeString(manifestDesc)
// 	if err != nil {
// 		return nil, nil, fmt.Errorf("failed to decode manifest descriptor: %w", err)
// 	}

// 	var descriptor ocispec.Descriptor
// 	if err := json.Unmarshal(data, &descriptor); err != nil {
// 		return nil, nil, fmt.Errorf("failed to parse manifest descriptor: %w", err)
// 	}

// 	manifestContent, err := b.readContent(ctx, descriptor.Digest.String())
// 	if err != nil {
// 		return nil, nil, fmt.Errorf("failed to read manifest content: %w", err)
// 	}

// 	var manifest ocispec.Manifest
// 	if err := json.Unmarshal(manifestContent, &manifest); err != nil {
// 		return nil, nil, fmt.Errorf("failed to parse manifest: %w", err)
// 	}

// 	return &descriptor, &manifest, nil
// }

// func (mb *ModelBuilder) getImageEnvironment(ctx context.Context, imageRef string) ([]string, error) {
// 	// Try local image inspection first using Docker API
// 	dockerClient := mb.provider
// 	inspect, err := dockerClient.Inspect(ctx, imageRef)
// 	if err == nil {
// 		// Successfully inspected local image
// 		return inspect.Config.Env, nil
// 	}

// 	// If local inspection failed (image not found locally), try remote inspection
// 	if command.IsNotFoundError(err) {
// 		fmt.Printf("Image %s not found locally, trying remote inspection...\n", imageRef)
// 		return getEnvFromRemoteImage(imageRef)
// 	}

// 	// Other error occurred during local inspection
// 	return nil, fmt.Errorf("failed to inspect image %s: %w", imageRef, err)
// }

// func getEnvFromRemoteImage(imageRef string) ([]string, error) {
// 	ref, err := name.ParseReference(imageRef)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to parse image reference: %w", err)
// 	}

// 	img, err := remote.Image(ref)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to get remote image: %w", err)
// 	}

// 	cfg, err := img.ConfigFile()
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to get image config: %w", err)
// 	}

// 	return cfg.Config.Env, nil
// }
