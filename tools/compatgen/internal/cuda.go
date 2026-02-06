package internal

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/anaskhan96/soup"

	"github.com/replicate/cog/pkg/config"
)

func FetchCUDABaseImages() ([]config.CUDABaseImage, error) {
	url := "https://hub.docker.com/v2/repositories/nvidia/cuda/tags/?page_size=1000&name=devel-ubuntu&ordering=last_updated"
	tags, err := fetchCUDABaseImageTags(url)
	if err != nil {
		return nil, err
	}

	images := []config.CUDABaseImage{}
	for _, tag := range tags {
		image, err := parseCUDABaseImage(tag)
		if err != nil {
			return nil, err
		}
		images = append(images, *image)
	}

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

func parseCUDABaseImage(tag string) (*config.CUDABaseImage, error) {
	parts := strings.Split(tag, "-")
	if len(parts) != 4 {
		return nil, fmt.Errorf("Tag must be in the format <cudaVersion>-cudnn<cudnnVersion>-{devel,runtime}-ubuntu<ubuntuVersion>. Invalid tag: %s", tag)
	}

	return &config.CUDABaseImage{
		Tag:     tag,
		CUDA:    parts[0],
		CuDNN:   strings.Split(parts[1], "cudnn")[1],
		IsDevel: parts[2] == "devel",
		Ubuntu:  strings.Split(parts[3], "ubuntu")[1],
	}, nil
}
