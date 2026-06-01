package internal

import (
	"cmp"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/env"
)

func TestFetchTorchPackages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content, err := os.ReadFile("torch_test.html")
		if err != nil {
			log.Fatalf("Error reading file: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		w.Write(content)
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
	}, torch271Packages)
}

func TestParseTorchInstallStringNormalizesIndexURLAndSupportedPythons(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/whl/cu126/torch":
			_, _ = w.Write([]byte(`<a>torch-2.10.0%2Bcu126-cp313-cp313-manylinux_2_28_x86_64.whl</a><a>torch-2.10.0%2Bcu126-cp314-cp314-manylinux_2_28_x86_64.whl</a>`))
		case "/whl/cu126/torchvision":
			_, _ = w.Write([]byte(`<a>torchvision-0.25.0%2Bcu126-cp313-cp313-manylinux_2_28_x86_64.whl</a><a>torchvision-0.25.0%2Bcu126-cp314-cp314-manylinux_2_28_x86_64.whl</a>`))
		case "/whl/cu126/torchaudio":
			_, _ = w.Write([]byte(`<a>torchaudio-2.10.0%2Bcu126-cp313-cp313-manylinux_2_28_x86_64.whl</a><a>torchaudio-2.10.0%2Bcu126-cp314-cp314-manylinux_2_28_x86_64.whl</a>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cuda126 := "12.6"
	compat, err := parseTorchInstallString(
		"pip install torch==2.10.0 torchvision==0.25.0 torchaudio==2.10.0 --index-url "+server.URL+"/whl/cu126",
		map[string]string{"torch": "", "torchvision": "", "torchaudio": ""},
		&cuda126,
	)

	require.NoError(t, err)
	require.Equal(t, "2.10.0+cu126", compat.Torch)
	require.Equal(t, server.URL+"/whl/cu126/", compat.ExtraIndexURL)
	require.Equal(t, []string{"3.13", "3.14"}, compat.Pythons)
}

func TestFetchTorchPackagesSkipsNonCPythonWheelLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`
<a href="../lit-15.0.7.tar.gz">../lit-15.0.7.tar.gz</a>
<a href="../torchtext-0.6.0.tar.gz">
../torchtext-0.6.0.tar.gz</a>
<a href="torch-2.10.0%2Bcu126-py3-none-linux_x86_64.whl">torch-2.10.0%2Bcu126-py3-none-linux_x86_64.whl</a>
<a href="torch-2.10.0%2Bcu126-cp313-cp313-manylinux_2_28_x86_64.whl">torch-2.10.0%2Bcu126-cp313-cp313-manylinux_2_28_x86_64.whl</a>
`))
	}))
	defer server.Close()

	packages, err := fetchTorchPackagesFromURL(server.URL)

	require.NoError(t, err)
	cuda126 := "12.6"
	require.Equal(t, []TorchPackage{
		{
			Name:          "2.10.0+cu126",
			Version:       "2.10.0",
			Variant:       "cu126",
			CUDA:          &cuda126,
			PythonVersion: "3.13",
		},
	}, packages)
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
	name, version, variant, pythonVersion, platform, err := ExtractSubFeaturesFromPytorchVersion("torch-1.10.0+cpu-cp310-cp310-linux_x86_64.whl")
	require.NoError(t, err)
	require.Equal(t, "1.10.0+cpu", name)
	require.Equal(t, "1.10.0", version)
	require.Equal(t, "cpu", variant)
	require.Equal(t, "310", pythonVersion)
	require.Equal(t, "linux_x86_64", platform)
}

func TestIsValidPytorchAudioVersionFormat(t *testing.T) {
	name, version, variant, pythonVersion, platform, err := ExtractSubFeaturesFromPytorchVersion("torchaudio-2.7.1+xpu-cp313-cp313t-win_amd64.whl")
	require.NoError(t, err)
	require.Equal(t, "2.7.1+xpu", name)
	require.Equal(t, "2.7.1", version)
	require.Equal(t, "xpu", variant)
	require.Equal(t, "313", pythonVersion)
	require.Equal(t, "win_amd64", platform)
}

func TestIsValidPytorchAudioVersionFormatBasic(t *testing.T) {
	name, version, variant, pythonVersion, platform, err := ExtractSubFeaturesFromPytorchVersion("torchaudio-0.8.1-cp39-none-win_amd64.whl")
	require.NoError(t, err)
	require.Equal(t, "0.8.1", name)
	require.Equal(t, "0.8.1", version)
	require.Equal(t, "", variant)
	require.Equal(t, "39", pythonVersion)
	require.Equal(t, "win_amd64", platform)
}

func TestIsValidPytorchVisionVersionFormatPostRelease(t *testing.T) {
	name, version, variant, pythonVersion, platform, err := ExtractSubFeaturesFromPytorchVersion("torchvision-0.4.1.post2-cp37-cp37m-macosx_10_9_x86_64.whl")
	require.NoError(t, err)
	require.Equal(t, "0.4.1.post2", name)
	require.Equal(t, "0.4.1", version)
	require.Equal(t, "", variant)
	require.Equal(t, "37", pythonVersion)
	require.Equal(t, "macosx_10_9_x86_64", platform)
}

func TestIsValidPytorchVisionEarlyVersion(t *testing.T) {
	name, version, variant, pythonVersion, platform, err := ExtractSubFeaturesFromPytorchVersion("torchvision-0.14.1+cu116-cp310-cp310-linux_x86_64.whl")
	require.NoError(t, err)
	require.Equal(t, "0.14.1+cu116", name)
	require.Equal(t, "0.14.1", version)
	require.Equal(t, "cu116", variant)
	require.Equal(t, "310", pythonVersion)
	require.Equal(t, "linux_x86_64", platform)
}

func TestIsValidPytorchAudioEarlyVersion(t *testing.T) {
	name, version, variant, pythonVersion, platform, err := ExtractSubFeaturesFromPytorchVersion("torchaudio-0.9.1-cp39-cp39-linux_x86_64.whl")
	require.NoError(t, err)
	require.Equal(t, "0.9.1", name)
	require.Equal(t, "0.9.1", version)
	require.Equal(t, "", variant)
	require.Equal(t, "39", pythonVersion)
	require.Equal(t, "linux_x86_64", platform)
}

func TestURLEncodedVersion(t *testing.T) {
	name, version, variant, pythonVersion, platform, err := ExtractSubFeaturesFromPytorchVersion("torchtext-0.17.0%2Bcpu-cp39-cp39-win_amd64.whl")
	require.NoError(t, err)
	require.Equal(t, "0.17.0+cpu", name)
	require.Equal(t, "0.17.0", version)
	require.Equal(t, "cpu", variant)
	require.Equal(t, "39", pythonVersion)
	require.Equal(t, "win_amd64", platform)
}

func TestVersionUnderFolder(t *testing.T) {
	name, version, variant, pythonVersion, platform, err := ExtractSubFeaturesFromPytorchVersion("cu111/torch-1.8.0%2Bcu111-cp36-cp36m-linux_x86_64.whl")
	require.NoError(t, err)
	require.Equal(t, "1.8.0+cu111", name)
	require.Equal(t, "1.8.0", version)
	require.Equal(t, "cu111", variant)
	require.Equal(t, "36", pythonVersion)
	require.Equal(t, "linux_x86_64", platform)
}

func TestPythonMVersion(t *testing.T) {
	name, version, variant, pythonVersion, platform, err := ExtractSubFeaturesFromPytorchVersion("torchaudio-0.7.2-cp36-cp36m-linux_x86_64.whl")
	require.NoError(t, err)
	require.Equal(t, "0.7.2", name)
	require.Equal(t, "0.7.2", version)
	require.Equal(t, "", variant)
	require.Equal(t, "36", pythonVersion)
	require.Equal(t, "linux_x86_64", platform)
}

func TestTorchCompatibilitySortingIsDeterministic(t *testing.T) {
	cuda126 := "12.6"
	cuda128 := "12.8"
	cpu := "cpu"

	compats := []config.TorchCompatibility{
		{Torch: "2.10.0+cu128", Torchvision: "0.25.0", Torchaudio: "2.10.0", CUDA: &cuda128, ExtraIndexURL: "https://download.pytorch.org/whl/cu128/"},
		{Torch: "2.10.0+cpu", Torchvision: "0.25.0", Torchaudio: "2.10.0", CUDA: &cpu, ExtraIndexURL: "https://download.pytorch.org/whl/cpu/"},
		{Torch: "2.11.0+cu126", Torchvision: "0.26.0", Torchaudio: "2.11.0", CUDA: &cuda126, ExtraIndexURL: "https://download.pytorch.org/whl/cu126/"},
		{Torch: "2.10.0+cu126", Torchvision: "0.25.0", Torchaudio: "2.10.0", CUDA: &cuda126, ExtraIndexURL: "https://download.pytorch.org/whl/cu126/"},
		{Torch: "2.11.0+cpu", Torchvision: "0.26.0", Torchaudio: "2.11.0", CUDA: nil, ExtraIndexURL: "https://download.pytorch.org/whl/cpu/"},
		{Torch: "2.11.0+cu128", Torchvision: "0.26.0", Torchaudio: "2.11.0", CUDA: &cuda128, ExtraIndexURL: "https://download.pytorch.org/whl/cu128/"},
	}

	// Apply the same sort logic used in FetchTorchCompatibilityMatrix
	slices.SortFunc(compats, func(a, b config.TorchCompatibility) int {
		aCuda := ""
		bCuda := ""
		if a.CUDA != nil {
			aCuda = *a.CUDA
		}
		if b.CUDA != nil {
			bCuda = *b.CUDA
		}
		return cmp.Or(
			cmp.Compare(a.Torch, b.Torch),
			cmp.Compare(a.Torchvision, b.Torchvision),
			cmp.Compare(a.Torchaudio, b.Torchaudio),
			cmp.Compare(aCuda, bCuda),
			cmp.Compare(a.ExtraIndexURL, b.ExtraIndexURL),
			cmp.Compare(a.FindLinks, b.FindLinks),
		)
	})

	require.Equal(t, []config.TorchCompatibility{
		{Torch: "2.10.0+cpu", Torchvision: "0.25.0", Torchaudio: "2.10.0", CUDA: &cpu, ExtraIndexURL: "https://download.pytorch.org/whl/cpu/"},
		{Torch: "2.10.0+cu126", Torchvision: "0.25.0", Torchaudio: "2.10.0", CUDA: &cuda126, ExtraIndexURL: "https://download.pytorch.org/whl/cu126/"},
		{Torch: "2.10.0+cu128", Torchvision: "0.25.0", Torchaudio: "2.10.0", CUDA: &cuda128, ExtraIndexURL: "https://download.pytorch.org/whl/cu128/"},
		{Torch: "2.11.0+cpu", Torchvision: "0.26.0", Torchaudio: "2.11.0", CUDA: nil, ExtraIndexURL: "https://download.pytorch.org/whl/cpu/"},
		{Torch: "2.11.0+cu126", Torchvision: "0.26.0", Torchaudio: "2.11.0", CUDA: &cuda126, ExtraIndexURL: "https://download.pytorch.org/whl/cu126/"},
		{Torch: "2.11.0+cu128", Torchvision: "0.26.0", Torchaudio: "2.11.0", CUDA: &cuda128, ExtraIndexURL: "https://download.pytorch.org/whl/cu128/"},
	}, compats)
}
