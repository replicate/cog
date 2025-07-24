package builder

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/testhelpers"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/dockertest"
	"github.com/replicate/cog/pkg/docker/testenv"
)

func TestIntegration_Build(t *testing.T) {
	t.Setenv("INTEGRATION", "1")
	testhelpers.RequireIntegrationSuite(t)

	env := testenv.New(t)

	provider, err := docker.NewAPIClient(t.Context(), docker.WithClient(env.DockerClient()))
	require.NoError(t, err)
	builder := NewBuildKitBuilder(provider)

	t.Run("ENV", func(t *testing.T) {
		t.Run("base ENV is preserved", func(t *testing.T) {
			env := env.ScopeT(t)

			baseTag, baseImage := env.Daemon().BuildImage(testenv.NewContextFromFS(t, fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"ENV PATH=/expected:/path",
						"ENV FOO=bar",
					}, "\n")),
				},
			}))

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: baseTag.Name()},
					},
				},
			}

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			assert.Equal(t, baseImage.Config.Env, imageConfig.Config.Env)
		})

		t.Run("base ENV can be appended", func(t *testing.T) {
			env := env.ScopeT(t)

			baseTag, baseImage := env.Daemon().BuildImage(testenv.NewContextFromFS(t, fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"ENV PATH=/expected:/path",
						"ENV FOO=bar",
					}, "\n")),
				},
			}))

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: baseTag.String()},
						Env: []string{
							"NAME=cosmo",
						},
					},
				},
			}

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			fmt.Println(baseImage.Config.Env)
			fmt.Println(imageConfig.Config.Env)

			assert.Equal(t, append(baseImage.Config.Env, "NAME=cosmo"), imageConfig.Config.Env)
		})

		t.Run("base ENV can be overwritten by stage", func(t *testing.T) {
			env := env.ScopeT(t)

			parsedTag, _ := env.Daemon().BuildImage(testenv.NewContextFromFS(t, fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"ENV PATH=/expected:/path",
						"ENV FOO=bar",
						"ENV NAME=cosmo",
					}, "\n")),
				},
			}))

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: parsedTag.String()},
						Env: []string{
							"NAME=dutch",
						},
					},
				},
			}

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			assert.Equal(t, "PATH=/expected:/path", imageConfig.Config.Env[0])
			assert.Equal(t, "FOO=bar", imageConfig.Config.Env[1])
			assert.Equal(t, "NAME=dutch", imageConfig.Config.Env[2])
		})

		t.Run("stage ENV can be overwritten by another stage", func(t *testing.T) {
			env := env.ScopeT(t)

			parsedTag, _ := env.Daemon().BuildImage(testenv.NewContextFromFS(t, fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"ENV PATH=/expected:/path",
						"ENV FOO=bar",
						"ENV NAME=cosmo",
					}, "\n")),
				},
			}))

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: parsedTag.String()},
						Env: []string{
							"NAME=dutch",
						},
					},
					{
						ID:     "stage",
						Source: plan.Input{Stage: "base"},
						Env: []string{
							"NAME=butters",
						},
					},
				},
			}

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			assert.Equal(t, "PATH=/expected:/path", imageConfig.Config.Env[0])
			assert.Equal(t, "FOO=bar", imageConfig.Config.Env[1])
			assert.Equal(t, "NAME=butters", imageConfig.Config.Env[2])
		})

		t.Run("unreferenced branch stage ENV does not impact final image", func(t *testing.T) {
			env := env.ScopeT(t)

			parsedTag, _ := env.Daemon().BuildImage(testenv.NewContextFromFS(t, fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"ENV STAGE=base",
					}, "\n")),
				},
			}))

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: parsedTag.String()},
					},
					{
						ID:     "branch",
						Source: plan.Input{Stage: "base"},
						Env: []string{
							"STAGE=branch",
						},
					},
					{
						ID:     "stage",
						Source: plan.Input{Stage: "base"},
					},
				},
			}

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			assert.Equal(t, "STAGE=base", imageConfig.Config.Env[1])
		})

		t.Run("can be set via an operation", func(t *testing.T) {
			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Scratch: true},
						Operations: []plan.Op{
							plan.SetEnv{
								Vars: map[string]string{
									"TEST_VAR": "test_value",
									"ANOTHER":  "another_value",
								},
							},
						},
					},
				},
			}

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			envVars := imageConfig.Config.Env
			assert.Contains(t, envVars, "TEST_VAR=test_value")
			assert.Contains(t, envVars, "ANOTHER=another_value")
		})

	})

	t.Run("Workdir", func(t *testing.T) {
		t.Run("unset base WORKDIR remains root", func(t *testing.T) {
			env := env.ScopeT(t)

			baseTag, _ := env.Daemon().BuildImage(testenv.NewContextFromFS(t, fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"LABEL test=test", // need to have something or we'll get a "No image was generated" error
					}, "\n")),
				},
			}))

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: baseTag.String()},
					},
				},
			}

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			assert.Equal(t, "/", imageConfig.Config.WorkingDir)
		})

		t.Run("base WORKDIR is preserved", func(t *testing.T) {
			env := env.ScopeT(t)

			baseTag, _ := env.Daemon().BuildImage(testenv.NewContextFromFS(t, fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"WORKDIR /expected",
					}, "\n")),
				},
			}))

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: baseTag.String()},
					},
				},
			}

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			assert.Equal(t, "/expected", imageConfig.Config.WorkingDir)
		})

		t.Run("base WORKDIR can be overwritten by stage", func(t *testing.T) {
			env := env.ScopeT(t)

			baseTag, _ := env.Daemon().BuildImage(testenv.NewContextFromFS(t, fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"WORKDIR /original",
					}, "\n")),
				},
			}))

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: baseTag.String()},
					},
					{
						ID:     "stage",
						Source: plan.Input{Stage: "base"},
						Dir:    "/updated",
					},
				},
			}

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			assert.Equal(t, "/updated", imageConfig.Config.WorkingDir)
		})

		t.Run("stage WORKDIR can be overwritten by another stage", func(t *testing.T) {
			env := env.ScopeT(t)

			baseTag, _ := env.Daemon().BuildImage(testenv.NewContextFromFS(t, fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"WORKDIR /original",
					}, "\n")),
				},
			}))

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: baseTag.String()},
					},
					{
						ID:     "stage",
						Source: plan.Input{Stage: "base"},
						Dir:    "/updated",
					},
					{
						ID:     "stage2",
						Source: plan.Input{Stage: "stage"},
						Dir:    "/updated2",
					},
				},
			}

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			assert.Equal(t, "/updated2", imageConfig.Config.WorkingDir)
		})

		t.Run("unreferenced branch stage WORKDIR does not impact final image", func(t *testing.T) {
			env := env.ScopeT(t)

			baseTag, _ := env.Daemon().BuildImage(testenv.NewContextFromFS(t, fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"WORKDIR /original",
					}, "\n")),
				},
			}))

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: baseTag.String()},
					},
					{
						ID:     "branch",
						Source: plan.Input{Stage: "base"},
						Dir:    "/updated-in-branch",
					},
					{
						ID:     "stage",
						Source: plan.Input{Stage: "base"},
					},
				},
			}

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			assert.Equal(t, "/original", imageConfig.Config.WorkingDir)
		})

		t.Run("state workdir can be overridden from the plan export config", func(t *testing.T) {
			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Scratch: true},
					},
				},
				Export: &plan.ExportConfig{
					WorkingDir: "/a/b/c",
				},
			}

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			assert.Equal(t, "/a/b/c", imageConfig.Config.WorkingDir)
		})
	})

	t.Run("Platform", func(t *testing.T) {
		t.Run("can be set on the plan", func(t *testing.T) {
			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "windows", Arch: "riscv64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Scratch: true},
					},
					{
						ID:     "stage",
						Source: plan.Input{Stage: "base"},
						Operations: []plan.Op{
							plan.SetEnv{
								Vars: map[string]string{
									"FOO": "bar",
								},
							},
						},
					},
				},
			}

			fmt.Println("plan", plan)

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			assert.Equal(t, "windows", imageConfig.Platform.OS)
			assert.Equal(t, "riscv64", imageConfig.Platform.Architecture)
		})

		t.Run("is preserved from the base image", func(t *testing.T) {
			env := env.ScopeT(t)

			baseTag, _ := env.Daemon().BuildImage(testenv.NewContextFromFS(t, fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"LABEL test=test",
					}, "\n")),
				},
			}), testenv.WithPlatform("linux/s390x"))

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "s390x"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: baseTag.String()},
					},
				},
			}

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			assert.Equal(t, "linux", imageConfig.Platform.OS)
			assert.Equal(t, "s390x", imageConfig.Platform.Architecture)
		})

		t.Run("applies constraints to referenced images", func(t *testing.T) {
			env := env.ScopeT(t)

			baseTag, _ := env.Daemon().BuildImage(testenv.NewContextFromFS(t, fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"LABEL test=test",
					}, "\n")),
				},
			}), testenv.WithPlatform("linux/s390x"))

			baseTag = env.Registry().ToRegistryRef(baseTag)

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "arm64"},
				// reference an image that doesn't match the image platform
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: baseTag.Name()},
					},
				},
			}

			_, _, err := builder.Build(t.Context(), plan, buildConfig)
			assert.ErrorContains(t, err, "not found")
		})
	})

	t.Run("User", func(t *testing.T) {
		t.Run("base image user is preserved", func(t *testing.T) {
			t.Skip("TODO: user is not working yet!")
			env := env.ScopeT(t)

			baseTag, _ := env.Daemon().BuildImage(testenv.NewContextFromFS(t, fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"USER 1111:2222",
					}, "\n")),
				},
			}))

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: baseTag.String()},
					},
				},
			}

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			assert.Equal(t, "1111:2222", imageConfig.Config.User)
		})

		t.Run("can be overridden from the plan export config", func(t *testing.T) {
			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Scratch: true},
					},
				},
				Export: &plan.ExportConfig{
					User: "1234:5678",
				},
			}

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			assert.Equal(t, "1234:5678", imageConfig.Config.User)
		})
	})

	t.Run("Copy", func(t *testing.T) {
		t.Run("from stage", func(t *testing.T) {
			env := env.ScopeT(t)

			baseTag, _ := env.Daemon().BuildImage(testenv.NewContextFromFS(t, fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"COPY <<EOF /test.txt",
						"hello world",
						"EOF",
					}, "\n")),
				},
			}))

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: baseTag.String()},
					},
					{
						ID:     "stage",
						Source: plan.Input{Stage: "base"},
						Operations: []plan.Op{
							plan.Copy{
								From: plan.Input{Stage: "base"},
								Src:  []string{"/test.txt"},
								Dest: "/copied.txt",
							},
						},
					},
				},
			}

			imageID, _, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			// Parse the image reference
			ref, err := name.ParseReference(imageID)
			require.NoError(t, err)

			// Verify the file was copied
			env.Daemon().AssertFileExists(t, ref, "/copied.txt")
			content, err := env.Daemon().FileContent(ref, "/copied.txt")
			require.NoError(t, err)
			assert.Equal(t, "hello world\n", string(content))
		})

		t.Run("from image", func(t *testing.T) {
			env := env.ScopeT(t)

			sourceTag, _ := env.Daemon().BuildImage(testenv.NewContextFromFS(t, fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"COPY <<EOF /source.txt",
						"source content",
						"EOF",
					}, "\n")),
				},
			}))

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Scratch: true},
						Operations: []plan.Op{
							plan.Copy{
								From: plan.Input{Image: sourceTag.String()},
								Src:  []string{"/source.txt"},
								Dest: "/dest.txt",
							},
						},
					},
				},
			}

			modelTag, _, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			// Parse the image reference
			ref, err := name.ParseReference(modelTag)
			require.NoError(t, err)

			// Verify the file was copied
			env.Daemon().AssertFileExists(t, ref, "/dest.txt")
			content, err := env.Daemon().FileContent(ref, "/dest.txt")
			require.NoError(t, err)
			assert.Equal(t, "source content\n", string(content))
		})

		t.Run("from URL", func(t *testing.T) {
			env := env.ScopeT(t)

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Scratch: true},
						Operations: []plan.Op{
							plan.Copy{
								From: plan.Input{URL: "http://localhost:5000/v2/"},
								Src:  []string{"/"},
								Dest: "/resp.json",
							},
						},
					},
				},
			}

			modelTag, _, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			// Parse the image reference
			ref, err := name.ParseReference(modelTag)
			require.NoError(t, err)

			// Verify the file was copied
			env.Daemon().AssertFileExists(t, ref, "/resp.json")
			content, err := env.Daemon().FileContent(ref, "/resp.json")
			require.NoError(t, err)
			assert.Equal(t, "{}", string(content))
		})

		t.Run("multiple files from local directory", func(t *testing.T) {
			env := env.ScopeT(t)

			// Create a temporary directory with multiple test files
			tempDir := t.TempDir()
			require.NoError(t, os.WriteFile(tempDir+"/alpha.txt", []byte("alpha content"), 0o644))
			require.NoError(t, os.WriteFile(tempDir+"/beta.txt", []byte("beta content"), 0o644))
			require.NoError(t, os.WriteFile(tempDir+"/gamma.txt", []byte("gamma content"), 0o644))

			buildConfig := &BuildConfig{
				ContextDir: tempDir,
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Scratch: true},
						Operations: []plan.Op{
							plan.Copy{
								From: plan.Input{Local: "context"},
								Src:  []string{"alpha.txt", "beta.txt"},
								Dest: "/files/",
							},
						},
					},
				},
				Contexts: map[string]*plan.BuildContext{
					"context": {
						Name:        "default",
						SourceBlock: "test",
						Description: "default build context",
						FS:          os.DirFS(tempDir),
					},
				},
			}

			modelTag, _, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			// Parse the image reference
			ref, err := name.ParseReference(modelTag)
			require.NoError(t, err)

			// Verify both files were copied
			env.Daemon().AssertFileExists(t, ref, "/files/alpha.txt")
			env.Daemon().AssertFileExists(t, ref, "/files/beta.txt")

			// Verify file contents
			alphaContent, err := env.Daemon().FileContent(ref, "/files/alpha.txt")
			require.NoError(t, err)
			assert.Equal(t, "alpha content", string(alphaContent))

			betaContent, err := env.Daemon().FileContent(ref, "/files/beta.txt")
			require.NoError(t, err)
			assert.Equal(t, "beta content", string(betaContent))

			// Verify gamma.txt was not copied
			env.Daemon().AssertFileNotExists(t, ref, "/files/gamma.txt")
		})

		t.Run("from subdirectory in local context", func(t *testing.T) {
			env := env.ScopeT(t)

			// Create a temporary directory with subdirectory structure
			tempDir := t.TempDir()
			subDir := tempDir + "/subdir"
			require.NoError(t, os.MkdirAll(subDir, 0o755))
			require.NoError(t, os.WriteFile(subDir+"/nested.txt", []byte("nested file content"), 0o644))

			buildConfig := &BuildConfig{
				ContextDir: tempDir,
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Scratch: true},
						Operations: []plan.Op{
							plan.Copy{
								From: plan.Input{Local: "."},
								Src:  []string{"subdir/nested.txt"},
								Dest: "/nested-copy.txt",
							},
						},
					},
				},
				Contexts: map[string]*plan.BuildContext{
					".": {
						Name:        "default",
						SourceBlock: "test",
						Description: "default build context",
						FS:          os.DirFS(tempDir),
					},
				},
			}

			modelTag, _, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			// Parse the image reference
			ref, err := name.ParseReference(modelTag)
			require.NoError(t, err)

			// Verify the nested file was copied
			env.Daemon().AssertFileExists(t, ref, "/nested-copy.txt")
			content, err := env.Daemon().FileContent(ref, "/nested-copy.txt")
			require.NoError(t, err)
			assert.Equal(t, "nested file content", string(content))
		})

		t.Run("with chown", func(t *testing.T) {
			t.Skip("TODO:chown not implemented in cogpack builder")
			env := env.ScopeT(t)

			baseTag, _ := env.Daemon().BuildImage(testenv.NewContextFromFS(t, fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"COPY <<EOF /test.txt",
						"test content",
						"EOF",
					}, "\n")),
				},
			}))

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: baseTag.String()},
					},
					{
						ID:     "final",
						Source: plan.Input{Stage: "base"},
						Operations: []plan.Op{
							plan.Copy{
								From:  plan.Input{Stage: "base"},
								Src:   []string{"/test.txt"},
								Dest:  "/owned.txt",
								Chown: "1000:1000",
							},
						},
					},
				},
			}

			modelTag, _, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			// Parse the image reference
			ref, err := name.ParseReference(modelTag)
			require.NoError(t, err)

			// Verify the file was copied with correct ownership
			env.Daemon().AssertFileExists(t, ref, "/owned.txt")

			// Check file ownership
			fileInfo, err := env.Daemon().FileInfo(ref, "/owned.txt")
			require.NoError(t, err)

			// Debug: print actual ownership
			t.Logf("File ownership - UID: %d, GID: %d", fileInfo.Uid, fileInfo.Gid)

			// TODO: The chown parameter is not currently implemented in the cogpack builder.
			// The applyCopyOp function in translate.go needs to be updated to pass
			// ownership options to llb.Copy when copy.Chown is specified.
			// Additionally, the Copy operation should add a Chmod parameter to match
			// the buildkit/dockerfile API for setting file permissions.
			// For now, we'll skip this assertion but keep the test as a reminder.
			t.Skip("Chown parameter not yet implemented in cogpack builder")
		})

		t.Run("with chmod", func(t *testing.T) {
			t.Skip("TODO: chmod not implemented in cogpack builder")
		})

		t.Run("with filter patterns", func(t *testing.T) {
			env := env.ScopeT(t)

			fs := fstest.MapFS{
				".cog/tmp/buildjunk.txt":        &fstest.MapFile{},
				"README.md":                     &fstest.MapFile{},
				"src/js/cache/a/b/c.tar":        &fstest.MapFile{},
				"src/js/cog.yaml":               &fstest.MapFile{},
				"src/js/predict.js":             &fstest.MapFile{},
				"src/py/.cog/tmp/buildjunk.txt": &fstest.MapFile{},
				"src/py/cog.yaml":               &fstest.MapFile{},
				"src/py/predict.py":             &fstest.MapFile{},
				"src/py/pyproject.toml":         &fstest.MapFile{},
				"src/rb/lib/predict.rb":         &fstest.MapFile{},
			}

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: env.Registry().ParseRef("local-alpine").Name()},
						Operations: []plan.Op{
							// copy files with only an exclude pattern
							plan.Copy{
								From: plan.Input{Local: "context"},
								Src:  []string{"/"},
								Dest: "/files/exclude/",
								Patterns: plan.FilePattern{
									Exclude: []string{
										"**/.cog",     // Any nested .cog directory
										"**/cache",    // Any nested cache directory
										"**/cog.yaml", // Any nested cog.yaml
									},
								},
								CreateDestPath: true,
							},
							plan.Exec{
								Command: "/bin/sh -c \"find /files/exclude -type f | sort > /ls-exclude.txt\"",
							},
							// copy files with only an include pattern
							plan.Copy{
								From: plan.Input{Local: "context"},
								Src:  []string{"/"},
								Dest: "/files/include/",
								Patterns: plan.FilePattern{
									Include: []string{
										"**/predict.*",
									},
								},
								CreateDestPath: true,
							},
							plan.Exec{
								Command: "/bin/sh -c \"find /files/include -type f | sort > /ls-include.txt\"",
							},
							// copy files with both an include and exclude pattern
							plan.Copy{
								From: plan.Input{Local: "context"},
								Src:  []string{"/"},
								Dest: "/files/mixed/",
								Patterns: plan.FilePattern{
									Include: []string{
										"src/py/**",
									},
									Exclude: []string{
										"**/.cog",
										"**/cog.yaml",
									},
								},
								CreateDestPath: true,
							},
							plan.Exec{
								Command: "/bin/sh -c \"find /files/mixed -type f | sort > /ls-mixed.txt\"",
							},
						},
					},
				},
				Contexts: map[string]*plan.BuildContext{
					"context": {
						Name:        "default",
						SourceBlock: "test",
						Description: "default build context",
						FS:          fs,
					},
				},
			}

			modelTag, _, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			readContents := func(path string) []string {
				ref, err := name.ParseReference(modelTag)
				require.NoError(t, err)
				content, err := env.Daemon().FileContent(ref, path)
				require.NoError(t, err)
				files := strings.Split(strings.TrimSpace(string(content)), "\n")
				slices.Sort(files)
				return files
			}

			assert.Equal(t,
				[]string{
					"/files/exclude/README.md",
					"/files/exclude/src/js/predict.js",
					"/files/exclude/src/py/predict.py",
					"/files/exclude/src/py/pyproject.toml",
					"/files/exclude/src/rb/lib/predict.rb",
				},
				readContents("/ls-exclude.txt"))

			assert.Equal(t,
				[]string{
					"/files/include/src/js/predict.js",
					"/files/include/src/py/predict.py",
					"/files/include/src/rb/lib/predict.rb",
				},
				readContents("/ls-include.txt"))

			assert.Equal(t,
				[]string{
					"/files/mixed/src/py/predict.py",
					"/files/mixed/src/py/pyproject.toml",
				},
				readContents("/ls-mixed.txt"))
		})

		t.Run("with wildcard patterns", func(t *testing.T) {
			env := env.ScopeT(t)

			fs := fstest.MapFS{
				"src/py/predict.py":     &fstest.MapFile{},
				"src/js/predict.js":     &fstest.MapFile{},
				"src/rb/lib/predict.rb": &fstest.MapFile{},
				"README.md":             &fstest.MapFile{},
			}

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: env.Registry().ParseRef("local-alpine").Name()},
						Operations: []plan.Op{
							plan.Copy{
								From: plan.Input{Local: "context"},
								// this will copy all the subdirectories _under_ src/ while preserving their structure
								Src:  []string{"src/*"},
								Dest: "/files/",
							},
							plan.Exec{
								Command: "/bin/sh -c \"find /files -type f | sort > /ls.txt\"",
							},
						},
					},
				},
				Contexts: map[string]*plan.BuildContext{
					"context": {
						Name:        "default",
						SourceBlock: "test",
						Description: "default build context",
						FS:          fs,
					},
				},
			}

			modelTag, _, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			ref, err := name.ParseReference(modelTag)
			require.NoError(t, err)

			content, err := env.Daemon().FileContent(ref, "/ls.txt")
			require.NoError(t, err)

			actualFiles := strings.Split(strings.TrimSpace(string(content)), "\n")
			expectedFiles := []string{
				"/files/lib/predict.rb",
				"/files/predict.js",
				"/files/predict.py",
			}
			slices.Sort(expectedFiles)
			slices.Sort(actualFiles)

			assert.Equal(t, expectedFiles, actualFiles)
		})
	})

	t.Run("Exec Operation", func(t *testing.T) {
		t.Run("simple exec", func(t *testing.T) {
			env := env.ScopeT(t)

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: env.Registry().ParseRef("local-alpine").Name()},
						Operations: []plan.Op{
							plan.Exec{
								Command: "/bin/sh -c \"echo 'hello' > /hello.txt\"",
							},
						},
					},
					{
						ID:     "another-exec",
						Source: plan.Input{Stage: "base"},
						Operations: []plan.Op{
							plan.Exec{
								Command: "/bin/sh -c \"cat /hello.txt | wc -m > /hello-length.txt\"",
							},
						},
					},
				},
				Contexts: map[string]*plan.BuildContext{},
			}

			modelTag, _, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			// Parse the image reference
			ref, err := name.ParseReference(modelTag)
			require.NoError(t, err)

			content, err := env.Daemon().FileContent(ref, "/hello.txt")
			require.NoError(t, err)
			assert.Equal(t, "hello\n", string(content))

			content, err = env.Daemon().FileContent(ref, "/hello-length.txt")
			require.NoError(t, err)
			assert.Equal(t, "6\n", string(content))
		})

		t.Run("exec with mount from local directory", func(t *testing.T) {
			env := env.ScopeT(t)

			tempDir := t.TempDir()
			require.NoError(t, os.WriteFile(tempDir+"/file1.txt", []byte("file1\n"), 0o644))
			require.NoError(t, os.WriteFile(tempDir+"/file2.txt", []byte("file2\n"), 0o644))
			contextFS := os.DirFS(tempDir)

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: env.Registry().ParseRef("local-alpine").Name()},
						Operations: []plan.Op{
							plan.Exec{
								Command: "/bin/sh -c \"cat /context-mount/file1.txt /context-mount/file2.txt > /combined.txt\"",
								Mounts: []plan.Mount{
									{
										Source: plan.Input{Local: "second-context"},
										Target: "/context-mount",
									},
								},
							},
						},
					},
				},
				Contexts: map[string]*plan.BuildContext{
					"second-context": {
						Name: "second-context",
						FS:   contextFS,
					},
				},
			}

			modelTag, _, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			// Parse the image reference
			ref, err := name.ParseReference(modelTag)
			require.NoError(t, err)

			// Verify the file was copied from local directory
			env.Daemon().AssertFileExists(t, ref, "/combined.txt")
			content, err := env.Daemon().FileContent(ref, "/combined.txt")
			require.NoError(t, err)
			assert.Equal(t, "file1\nfile2\n", string(content))
		})

		t.Run("with mount from in-memory FS", func(t *testing.T) {
			env := env.ScopeT(t)

			contextFS := fstest.MapFS{
				"file1.txt": &fstest.MapFile{
					Data: []byte("file1\n"),
					Mode: 0o644,
				},
				"file2.txt": &fstest.MapFile{
					Data: []byte("file2\n"),
					Mode: 0o644,
				},
			}

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: env.Registry().ParseRef("local-alpine").Name()},
						Operations: []plan.Op{
							plan.Exec{
								Command: "/bin/sh -c \"cat /context-mount/file1.txt /context-mount/file2.txt > /combined.txt\"",
								Mounts: []plan.Mount{
									{
										Source: plan.Input{Local: "second-context"},
										Target: "/context-mount",
									},
								},
							},
						},
					},
				},
				Contexts: map[string]*plan.BuildContext{
					"second-context": {
						Name: "second-context",
						FS:   contextFS,
					},
				},
			}

			modelTag, _, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			// Parse the image reference
			ref, err := name.ParseReference(modelTag)
			require.NoError(t, err)

			// Verify the file was copied from local directory
			env.Daemon().AssertFileExists(t, ref, "/combined.txt")
			content, err := env.Daemon().FileContent(ref, "/combined.txt")
			require.NoError(t, err)
			assert.Equal(t, "file1\nfile2\n", string(content))
		})

		t.Run("with mount from image", func(t *testing.T) {
			env := env.ScopeT(t)

			toolsTag, _ := env.Daemon().BuildImage(testenv.NewContextFromFS(t, fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"COPY --chmod=0755 <<EOF /scripts/script.sh",
						"#!/bin/bash",
						"echo 'tool executed'",
						"EOF",
					}, "\n")),
				},
			}))

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					// base stage from alpine
					{
						ID:     "base",
						Source: plan.Input{Image: env.Registry().ParseRef("local-alpine").Name()},
					},
					// main stage off base that runs commands off tools image
					{
						ID:     "main",
						Source: plan.Input{Stage: "base"},
						Operations: []plan.Op{
							plan.Exec{
								Command: "/bin/sh -c \"/bin/sh /mnt/tools/scripts/script.sh > /output.txt\"",
								Mounts: []plan.Mount{
									{
										Source: plan.Input{Image: toolsTag.Name()},
										Target: "/mnt/tools",
									},
								},
							},
						},
					},
				},
			}

			modelTag, _, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			ref, err := name.ParseReference(modelTag)
			require.NoError(t, err)

			content, err := env.Daemon().FileContent(ref, "/output.txt")
			require.NoError(t, err)
			assert.Equal(t, "tool executed\n", string(content))
		})

		t.Run("with mount from stage", func(t *testing.T) {
			env := env.ScopeT(t)

			toolsTag, _ := env.Daemon().BuildImage(testenv.NewContextFromFS(t, fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"COPY --chmod=0755 <<EOF /scripts/script.sh",
						"#!/bin/bash",
						"echo 'tool executed'",
						"EOF",
					}, "\n")),
				},
			}))

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					// base stage from alpine
					{
						ID:     "base",
						Source: plan.Input{Image: env.Registry().ParseRef("local-alpine").Name()},
					},
					// detached stage from tools image
					{
						ID:     "tools",
						Source: plan.Input{Image: toolsTag.Name()},
					},
					// main stage off base that runs commands off tools stage
					{
						ID:     "main",
						Source: plan.Input{Stage: "base"},
						Operations: []plan.Op{
							plan.Exec{
								Command: "/bin/sh -c \"/bin/sh /mnt/tools/scripts/script.sh > /output.txt\"",
								Mounts: []plan.Mount{
									{
										Source: plan.Input{Stage: "tools"},
										Target: "/mnt/tools",
									},
								},
							},
						},
					},
				},
			}

			modelTag, _, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			ref, err := name.ParseReference(modelTag)
			require.NoError(t, err)

			content, err := env.Daemon().FileContent(ref, "/output.txt")
			require.NoError(t, err)
			assert.Equal(t, "tool executed\n", string(content))
		})

		t.Run("has access to ENV vars", func(t *testing.T) {
			t.Skip("TODO: make sure Exec can access ENV from current state")
		})

		t.Run("can set ENV for only this op", func(t *testing.T) {
			t.Skip("TODO: make sure Exec can set ENV for only this op")
		})
	})

	t.Run("ExportConfig", func(t *testing.T) {
		t.Run("entrypoint and cmd", func(t *testing.T) {
			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Scratch: true},
					},
				},
				Export: &plan.ExportConfig{
					Entrypoint: []string{"/usr/bin/python3"},
					Cmd:        []string{"-m", "app"},
				},
			}

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			assert.Equal(t, []string{"/usr/bin/python3"}, imageConfig.Config.Entrypoint)
			assert.Equal(t, []string{"-m", "app"}, imageConfig.Config.Cmd)
		})

		t.Run("labels", func(t *testing.T) {
			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Scratch: true},
					},
				},
				Export: &plan.ExportConfig{
					Labels: map[string]string{
						"app.name":    "test-app",
						"app.version": "1.0.0",
					},
				},
			}

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			assert.Equal(t, "test-app", imageConfig.Config.Labels["app.name"])
			assert.Equal(t, "1.0.0", imageConfig.Config.Labels["app.version"])
		})

		t.Run("exposed ports", func(t *testing.T) {
			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Scratch: true},
					},
				},
				Export: &plan.ExportConfig{
					ExposedPorts: map[string]struct{}{
						"8080/tcp": {},
						"9090/udp": {},
					},
				},
			}

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			assert.Contains(t, imageConfig.Config.ExposedPorts, "8080/tcp")
			assert.Contains(t, imageConfig.Config.ExposedPorts, "9090/udp")
		})

	})

	t.Run("Operations", func(t *testing.T) {

		t.Run("mkfile operation", func(t *testing.T) {
			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Scratch: true},
						Operations: []plan.Op{
							plan.MkFile{
								Dest: "/test-file.txt",
								Data: []byte("test file content"),
								Mode: 0o644,
							},
						},
					},
				},
			}

			imageID, _, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			// Parse the image reference
			ref, err := name.ParseReference(imageID)
			require.NoError(t, err)

			// Verify the file was created
			env.Daemon().AssertFileExists(t, ref, "/test-file.txt")
			content, err := env.Daemon().FileContent(ref, "/test-file.txt")
			require.NoError(t, err)
			assert.Equal(t, "test file content", string(content))
		})
	})

	t.Run("BuildContexts", func(t *testing.T) {
		t.Run("copy from local build context", func(t *testing.T) {
			// Create a temporary directory with test files
			tempDir := t.TempDir()
			require.NoError(t, os.WriteFile(tempDir+"/test.txt", []byte("local file content"), 0o644))

			buildConfig := &BuildConfig{
				ContextDir: tempDir,
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Scratch: true},
						Operations: []plan.Op{
							plan.Copy{
								From: plan.Input{Local: "."},
								Src:  []string{"test.txt"},
								Dest: "/copied-local.txt",
							},
						},
					},
				},
				Contexts: map[string]*plan.BuildContext{
					".": {
						Name:        "default",
						SourceBlock: "test",
						Description: "default build context",
						FS:          os.DirFS(tempDir),
					},
				},
			}

			imageID, _, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			// Parse the image reference
			ref, err := name.ParseReference(imageID)
			require.NoError(t, err)

			// Verify the file was copied from build context
			env.Daemon().AssertFileExists(t, ref, "/copied-local.txt")
			content, err := env.Daemon().FileContent(ref, "/copied-local.txt")
			require.NoError(t, err)
			assert.Equal(t, "local file content", string(content))
		})

		t.Run("verify file not exists", func(t *testing.T) {
			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
			}

			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Scratch: true},
						Operations: []plan.Op{
							plan.MkFile{
								Dest: "/exists.txt",
								Data: []byte("this file exists"),
								Mode: 0o644,
							},
						},
					},
				},
			}

			imageID, _, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			// Parse the image reference
			ref, err := name.ParseReference(imageID)
			require.NoError(t, err)

			// Verify file existence and non-existence
			env.Daemon().AssertFileExists(t, ref, "/exists.txt")
			env.Daemon().AssertFileNotExists(t, ref, "/does-not-exist.txt")
			env.Daemon().AssertFileNotExists(t, ref, "/another-missing-file.txt")
		})
	})
}
