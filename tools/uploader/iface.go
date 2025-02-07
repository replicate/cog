package uploader

import (
	"context"
	"strings"

	"github.com/vbauerster/mpb/v8"
)

type Uploader interface {
	UploadObject(ctx context.Context, objectPath, bucket, key string, p ProgressConfig) error
}

type ProgressConfig struct {
	progress   *mpb.Progress
	descriptor string
	prefixLen  int
}

func NewProgressConfig(progress *mpb.Progress, descriptor string) ProgressConfig {
	return ProgressConfig{
		progress:   progress,
		descriptor: descriptor,
		prefixLen:  20,
	}
}

func (p *ProgressConfig) GetPrefix() string {
	prefix := p.descriptor
	if len(prefix) > p.prefixLen {
		prefix = prefix[:p.prefixLen]
	}
	if len(prefix) < p.prefixLen {
		prefix += strings.Repeat(" ", p.prefixLen-len(prefix))
	}
	return prefix
}
