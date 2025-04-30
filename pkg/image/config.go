package image

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/docker/docker/api/types/image"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
)

func CogConfigFromManifest(ctx context.Context, manifest *image.InspectResponse) (*config.Config, error) {
	configString := manifest.Config.Labels[command.CogConfigLabelKey]
	if configString == "" {
		// Deprecated. Remove for 1.0.
		configString = manifest.Config.Labels["org.cogmodel.config"]
	}
	if configString == "" {
		// TODO[md]: find the tag/ref and return that in the error instead of the ID
		return nil, fmt.Errorf("Image %s does not appear to be a Cog model", friendlyName(manifest))
	}
	conf := new(config.Config)
	if err := json.Unmarshal([]byte(configString), conf); err != nil {
		// TODO[md]: find the tag/ref and return that in the error instead of the ID
		return nil, fmt.Errorf("Failed to parse config from %s: %w", friendlyName(manifest), err)
	}
	return conf, nil
}

func friendlyName(manifest *image.InspectResponse) string {
	// this appears to get the base image name, which we don't really want
	// name := manifest.Config.Labels["org.opencontainers.image.title"]
	// if name != "" {
	// 	return name
	// }

	if len(manifest.RepoTags) > 0 {
		return manifest.RepoTags[0]
	}

	return manifest.ID
}
