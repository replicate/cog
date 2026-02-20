package internal

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/anaskhan96/soup"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/replicate/cog/pkg/config"
)

func FetchCUDABaseImages(ctx context.Context) ([]config.CUDABaseImage, error) {
	url := "https://hub.docker.com/v2/repositories/nvidia/cuda/tags/?page_size=1000&name=devel-ubuntu&ordering=last_updated"
	tags, err := fetchCUDABaseImageTags(url)
	if err != nil {
		return nil, err
	}

	var images []config.CUDABaseImage
	for _, tag := range tags {
		image, err := parseCUDABaseImage(ctx, tag)
		if err != nil {
			return nil, err
		}
		images = append(images, *image)
	}

	// stable sort for deterministic output
	slices.SortFunc(images, func(a, b config.CUDABaseImage) int {
		return cmp.Or(
			cmp.Compare(a.CUDA, b.CUDA),
			cmp.Compare(a.CuDNN, b.CuDNN),
			cmp.Compare(a.Ubuntu, b.Ubuntu),
			cmp.Compare(a.Tag, b.Tag),
		)
	})

	return images, nil
}

func fetchCUDABaseImageTags(url string) ([]string, error) {
	tags := []string{}

	resp, err := soup.Get(url)
	if err != nil {
		return tags, fmt.Errorf("Failed to download %s: %w", url, err)
	}

	var results struct {
		Next    *string
		Results []struct {
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(resp), &results); err != nil {
		return tags, fmt.Errorf("Failed parse CUDA images json: %w", err)
	}

	for _, result := range results.Results {
		tag := result.Name
		if strings.Contains(tag, "-cudnn") && !strings.HasSuffix(tag, "-rc") {
			tags = append(tags, tag)
		}
	}

	// recursive case for pagination
	if results.Next != nil {
		nextURL := *results.Next
		nextTags, err := fetchCUDABaseImageTags(nextURL)
		if err != nil {
			return tags, err
		}
		tags = append(tags, nextTags...)
	}

	sort.Sort(sort.Reverse(sort.StringSlice(tags)))

	return tags, nil
}

// parseCUDABaseImage fetches the Docker image config for an nvidia/cuda tag
// and extracts CUDA and CuDNN versions from environment variables. This is
// necessary because newer nvidia/cuda tags no longer include the CuDNN version
// in the tag itself (e.g. "12.9.1-cudnn-devel-ubuntu24.04" instead of
// "12.6.3-cudnn9-devel-ubuntu22.04").
func parseCUDABaseImage(ctx context.Context, tag string) (*config.CUDABaseImage, error) {
	fmt.Println("parsing", tag)

	baseImg := &config.CUDABaseImage{
		Tag:     tag,
		IsDevel: strings.Contains(tag, "-devel"),
	}

	if parts := strings.Split(tag, "ubuntu"); len(parts) == 2 {
		baseImg.Ubuntu = parts[1]
	} else {
		return nil, fmt.Errorf("invalid tag, must end in ubuntu<version>: %q", tag)
	}

	ref, err := name.ParseReference(fmt.Sprintf("nvidia/cuda:%s", tag))
	if err != nil {
		return nil, fmt.Errorf("failed to parse reference %s: %w", tag, err)
	}

	img, err := remote.Image(ref, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return nil, fmt.Errorf("failed to get image %s: %w", tag, err)
	}

	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("failed to get config file %s: %w", tag, err)
	}

	for _, envVal := range cfg.Config.Env {
		parts := strings.SplitN(envVal, "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch parts[0] {
		case "CUDA_VERSION":
			baseImg.CUDA = parts[1]
		case "NV_CUDNN_VERSION":
			// downstream code expects only the major version component
			baseImg.CuDNN = strings.Split(parts[1], ".")[0]
		}
	}

	if baseImg.CuDNN == "" {
		return nil, fmt.Errorf("no CuDNN version found in image config for tag %s", tag)
	}

	return baseImg, nil
}
