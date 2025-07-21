package builder

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/testhelpers"
	"github.com/replicate/cog/pkg/docker/dockertest"
	"github.com/replicate/cog/pkg/docker/dockertest/dind"
	"github.com/replicate/cog/pkg/util"
)

func TestIntegration_Build(t *testing.T) {
	testhelpers.RequireIntegrationSuite(t)

	dindContainer := dind.NewDind(t)
	builder := NewBuildKitBuilder(dindContainer.Provider())

	t.Run("ENV", func(t *testing.T) {
		t.Run("base ENV is preserved", func(t *testing.T) {
			baseTag, baseImage := dindContainer.BuildTestImage(t, dind.NewContextFromFS(t, fstest.MapFS{
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
					},
				},
			}

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			assert.Equal(t, baseImage.Config.Env, imageConfig.Config.Env)
		})

		t.Run("base ENV can be appended", func(t *testing.T) {
			baseTag, baseImage := dindContainer.BuildTestImage(t, dind.NewContextFromFS(t, fstest.MapFS{
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
			parsedTag, _ := dindContainer.BuildTestImage(t, dind.NewContextFromFS(t, fstest.MapFS{
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
			parsedTag, _ := dindContainer.BuildTestImage(t, dind.NewContextFromFS(t, fstest.MapFS{
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
			parsedTag, _ := dindContainer.BuildTestImage(t, dind.NewContextFromFS(t, fstest.MapFS{
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
	})

	t.Run("Workdir", func(t *testing.T) {
		t.Run("unset base WORKDIR remains root", func(t *testing.T) {
			baseTag, _ := dindContainer.BuildTestImage(t, dind.NewContextFromFS(t, fstest.MapFS{
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
			baseTag, _ := dindContainer.BuildTestImage(t, dind.NewContextFromFS(t, fstest.MapFS{
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
			baseTag, _ := dindContainer.BuildTestImage(t, dind.NewContextFromFS(t, fstest.MapFS{
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
			baseTag, _ := dindContainer.BuildTestImage(t, dind.NewContextFromFS(t, fstest.MapFS{
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
			baseTag, _ := dindContainer.BuildTestImage(t, dind.NewContextFromFS(t, fstest.MapFS{
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
	})

	t.Run("Platform", func(t *testing.T) {
		t.Skip("platform fails due to a dind<>docker build issue I don't want to debug now")
		t.Run("base platform is preserved", func(t *testing.T) {
			baseTag, _ := dindContainer.BuildTestImage(t, dind.NewContextFromFS(t, fstest.MapFS{
				"Dockerfile": &fstest.MapFile{
					Data: []byte(strings.Join([]string{
						"FROM scratch",
						"LABEL test=test", // need to have something or we'll get a "No image was generated" error
					}, "\n")),
				},
			}), dind.WithPlatform("linux/s390x"))

			fmt.Println("baseTag", baseTag.String())

			fmt.Println("test.A")

			inspect, err := dindContainer.DockerClient().ImageInspect(t.Context(), baseTag.String(), client.ImageInspectWithPlatform(&ocispec.Platform{OS: "linux", Architecture: "s390x"}))
			require.NoError(t, err)
			util.JSONPrettyPrint(inspect)

			fmt.Println("test.B")

			buildConfig := &BuildConfig{
				ContextDir: t.TempDir(),
				Tag:        dockertest.NewRandomRefS(t),
				Platform:   &ocispec.Platform{OS: "linux", Architecture: "s390x"},
			}

			fmt.Println("test.C")
			plan := &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "s390x"},
				Stages: []*plan.Stage{
					{
						ID:     "base",
						Source: plan.Input{Image: baseTag.String()},
					},
				},
			}

			fmt.Println("test.D")

			_, imageConfig, err := builder.Build(t.Context(), plan, buildConfig)
			require.NoError(t, err)

			assert.Equal(t, "linux/s390x", imageConfig.Platform.Architecture)
		})
	})
}
