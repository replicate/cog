//go:build ignore

package ops

import (
	"github.com/moby/buildkit/client/llb"

	"github.com/replicate/cog/pkg/model/factory/types"
)

func Download(source string, containerPath string) *downloader {
	return &downloader{
		source:        source,
		containerPath: containerPath,
	}
}

type downloader struct {
	source        string
	containerPath string
}

func (op *downloader) Apply(ctx types.Context, base llb.State) (llb.State, error) {
	intermediate := base

	target := llb.HTTP(op.source, llb.Filename("download.bin"), llb.Chmod(0x755))

	return intermediate.File(llb.Copy(target, "/download.bin", op.containerPath)), nil
}
