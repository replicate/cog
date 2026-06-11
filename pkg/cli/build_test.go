package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveBuildImageNameFallsBackToModel(t *testing.T) {
	name := ResolveBuildImageName("", "r8.im/user/model", "", "/tmp/project")
	require.Equal(t, "r8.im/user/model", name)
}

func TestResolveBuildImageNamePrefersConfigImageOverModel(t *testing.T) {
	name := ResolveBuildImageName("config-image", "r8.im/user/model", "", "/tmp/project")
	require.Equal(t, "config-image", name)
}

func TestResolveBuildImageNamePrefersTag(t *testing.T) {
	name := ResolveBuildImageName("config-image", "r8.im/user/model", "custom:tag", "/tmp/project")
	require.Equal(t, "custom:tag", name)
}

func TestUseCogBaseImageExplicitness(t *testing.T) {
	falseValue := false
	opts := BuildFlagsOptions{UseCogBaseImage: &falseValue}
	require.NotNil(t, opts.UseCogBaseImage)
	require.False(t, *opts.UseCogBaseImage)

	opts = BuildFlagsOptions{}
	require.Nil(t, opts.UseCogBaseImage)
}

func TestBuildFlagsOptionsModelBuildOptions(t *testing.T) {
	opts := BuildFlagsOptions{
		NoCache:          true,
		SeparateWeights:  true,
		Secrets:          []string{"id=foo"},
		ProgressOutput:   "plain",
		UseCudaBaseImage: "false",
		OpenAPISchema:    "schema.json",
		DockerfileFile:   "Dockerfile",
		Strip:            true,
		Precompile:       true,
	}
	annotations := map[string]string{"k": "v"}
	bo := opts.ModelBuildOptions("my-image", annotations)

	require.Equal(t, "my-image", bo.ImageName)
	require.True(t, bo.NoCache)
	require.True(t, bo.SeparateWeights)
	require.Equal(t, []string{"id=foo"}, bo.Secrets)
	require.Equal(t, "plain", bo.ProgressOutput)
	require.Equal(t, "false", bo.UseCudaBaseImage)
	require.Equal(t, "schema.json", bo.SchemaFile)
	require.Equal(t, "Dockerfile", bo.DockerfileFile)
	require.True(t, bo.Strip)
	require.True(t, bo.Precompile)
	require.Equal(t, annotations, bo.Annotations)
}
