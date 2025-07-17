//go:build ignore

package ops

import (
	"encoding/json"
	"fmt"

	"github.com/distribution/reference"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/client/llb/sourceresolver"
	gatewayClient "github.com/moby/buildkit/frontend/gateway/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/replicate/cog/pkg/model/factory/state"
	"github.com/replicate/cog/pkg/model/factory/types"
)

func ResolveBaseImage(ctx types.Context, feClient gatewayClient.Client, platform ocispec.Platform, ref string) (llb.State, error) {
	named, err := reference.ParseNormalizedNamed(ref)
	if err != nil {
		return llb.State{}, fmt.Errorf("failed to parse reference: %w", err)
	}
	// TODO[md]: is this necessary???
	named = reference.TagNameOnly(named)

	resolvedRef, _, blob, err := feClient.ResolveImageConfig(ctx, named.String(), sourceresolver.Opt{
		Platform: &platform,
		ImageOpt: &sourceresolver.ResolveImageOpt{
			ResolveMode: llb.ResolveModePreferLocal.String(),
		},
	})
	if err != nil {
		return llb.State{}, fmt.Errorf("failed to resolve base image: %w", err)
	}

	var img ocispec.Image
	if err := json.Unmarshal(blob, &img); err != nil {
		return llb.State{}, fmt.Errorf("failed to unmarshal image config: %w", err)
	}

	meta := state.MetaFromImage(&img)

	baseState := llb.Image(resolvedRef, llb.Platform(platform))
	return state.WithMeta(baseState, meta), nil
}
