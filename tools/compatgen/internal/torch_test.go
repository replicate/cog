package internal

import (
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/env"
)

func TestFetchTorchPackages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content, err := os.ReadFile("torch_test.html")
		if err != nil {
			log.Fatalf("Error reading file: %v", err)
		}
		w.Write(content)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	url, err := url.Parse(server.URL)
	require.NoError(t, err)
	t.Setenv(env.SchemeEnvVarName, url.Scheme)
	t.Setenv(env.PytorchHostEnvVarName, url.Host)

	torchPackages, err := FetchTorchPackages("torch")
	require.NoError(t, err)
	torch271Packages := []TorchPackage{}
	for _, pkg := range torchPackages {
		if strings.Contains(pkg.Name, "2.7.1+cu128") {
			torch271Packages = append(torch271Packages, pkg)
		}
	}
	cuda128 := "12.8"

	require.Equal(t, []TorchPackage{
		{
			Name:          "2.7.1+cu128",
			Version:       "2.7.1",
			Variant:       "cu128",
			CUDA:          &cuda128,
			PythonVersion: "3.10",
		},
		{
			Name:          "2.7.1+cu128",
			Version:       "2.7.1",
			Variant:       "cu128",
			CUDA:          &cuda128,
			PythonVersion: "3.11",
		},
		{
			Name:          "2.7.1+cu128",
			Version:       "2.7.1",
			Variant:       "cu128",
			CUDA:          &cuda128,
			PythonVersion: "3.12",
		},
		{
			Name:          "2.7.1+cu128",
			Version:       "2.7.1",
			Variant:       "cu128",
			CUDA:          &cuda128,
			PythonVersion: "3.13",
		},
		{
			Name:          "2.7.1+cu128",
			Version:       "2.7.1",
			Variant:       "cu128",
			CUDA:          &cuda128,
			PythonVersion: "3.9",
		},
	}, torch271Packages)
}

func TestIsValidPytorchVersionFormat(t *testing.T) {
	name, version, variant, pythonVersion, platform, err := ExtractSubFeaturesFromPytorchVersion("torch-2.7.1+cpu.cxx11.abi-cp312-cp312-linux_x86_64.whl")
	require.NoError(t, err)
	require.Equal(t, "2.7.1+cpu.cxx11.abi", name)
	require.Equal(t, "2.7.1", version)
	require.Equal(t, "cpu.cxx11.abi", variant)
	require.Equal(t, "312", pythonVersion)
	require.Equal(t, "linux_x86_64", platform)
}

func TestIsValidPytorchVersionFormatWithOldVersion(t *testing.T) {
	name, version, variant, pythonVersion, platform, err := ExtractSubFeaturesFromPytorchVersion("torch-1.10.0+cpu-cp39-cp39-linux_x86_64.whl")
	require.NoError(t, err)
	require.Equal(t, "1.10.0+cpu", name)
	require.Equal(t, "1.10.0", version)
	require.Equal(t, "cpu", variant)
	require.Equal(t, "39", pythonVersion)
	require.Equal(t, "linux_x86_64", platform)
}
