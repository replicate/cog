package factory

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/google/uuid"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"
)

type BuildSettings struct {
	Tag        string
	WorkingDir string
	Config     *config.Config
	Platform   ocispec.Platform

	// dockerfile factory settings, many will get moved to the top section once buildkit factory supports it
	Monobase         bool
	SeparateWeights  bool
	UseCudaBaseImage string
	SchemaFile       string
	DockerfileFile   string
	Precompile       bool
	Strip            bool
	UseCogBaseImage  *bool
	LocalImage       bool
	NoCache          bool
	BuildSecrets     []string
	ProgressOutput   string
	PredictBuild     bool // tell the dockerfile factory that this model is for predict so it can cut corners
	Annotations      map[string]string
}

// type BuildInfo map[string]any

type BuildInfo struct {
	Duration time.Duration
	BuildID  string

	Builder string

	FactoryBackend string

	// does the build include the model source? this only applies to dockerfile builds for predict/train
	BaseImageOnly bool
}

type Factory struct {
	impl factoryImpl
}

func (f *Factory) Build(ctx context.Context, settings BuildSettings) (*model.Model, BuildInfo, error) {
	// buildInfo := BuildInfo{}

	startTime := time.Now()
	buildID := f.newBuildID(settings)

	// TODO[md]: not sure we want this in every label since it changes the image even though the underlying layers are identical...
	if settings.Annotations == nil {
		settings.Annotations = map[string]string{}
	}
	settings.Annotations["build_id"] = buildID

	model, buildInfo, err := f.impl.Build(ctx, settings)
	buildInfo.Duration = time.Since(startTime)
	buildInfo.BuildID = buildID

	return model, buildInfo, err
}

func (f *Factory) newBuildID(settings BuildSettings) string {
	// generating a uuid v7 only errors in extreme cases, like system clock issues,
	// resource exhaustion, or entropy exhaustion.
	if id, err := uuid.NewV7(); err == nil {
		return id.String()
	}

	// fallback to a uuid v4 which is even less likely to fail
	if id, err := uuid.NewRandom(); err == nil {
		return id.String()
	}

	// finally, return a best-effort unique string from the build context & timestamp
	hash := sha256.Sum256([]byte(settings.WorkingDir + settings.Tag))
	return fmt.Sprintf("build-%x-%d", hash[:8], time.Now().UnixNano())
}

func New(provider command.Command) (Factory, error) {
	if util.EnvIsTruthy("COG_BUILDKIT_FACTORY") {
		if clientProvider, ok := provider.(command.ClientProvider); ok {
			impl, err := newBuildkitFactory(clientProvider)
			if err != nil {
				return Factory{}, err
			}
			return Factory{impl: impl}, nil
		}
		console.Warnf("COG_BUILDKIT_FACTORY is set, but provider does not implement command.ClientProvider. Falling back to dockerfile factory.")
	}
	return Factory{impl: newDockerfileFactory(provider)}, nil
}

// factoryImpl is the interface that the buildkit & dockerfile factories implement
type factoryImpl interface {
	Build(ctx context.Context, settings BuildSettings) (*model.Model, BuildInfo, error)
}
