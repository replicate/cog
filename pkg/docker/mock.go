package docker

import (
	"context"

	"github.com/replicate/cog/pkg/logger"
)

type MockBuildFunc func(ctx context.Context, dir string, dockerfileContents string, name string, useGPU bool, logWriter logger.Logger) (tag string, err error)

type MockImageBuilder struct {
	buildFunc MockBuildFunc
}

func NewMockImageBuilder(buildFunc MockBuildFunc) *MockImageBuilder {
	return &MockImageBuilder{buildFunc}
}

func (m *MockImageBuilder) Build(ctx context.Context, dir string, dockerfileContents string, name string, useGPU bool, logWriter logger.Logger) (tag string, err error) {
	return m.buildFunc(ctx, dir, dockerfileContents, name, useGPU, logWriter)
}

func (m *MockImageBuilder) Push(ctx context.Context, tag string, logWriter logger.Logger) error {
	return nil
}
