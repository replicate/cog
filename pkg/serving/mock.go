package serving

import (
	"context"
	"time"

	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
)

type MockRunFunc func(*Example) *Result

type MockServingPlatform struct {
	bootDuration  time.Duration
	run           MockRunFunc
	helpArguments map[string]*model.RunArgument
}

func NewMockServingPlatform(bootDuration time.Duration, run MockRunFunc, helpArguments map[string]*model.RunArgument) *MockServingPlatform {
	return &MockServingPlatform{
		bootDuration:  bootDuration,
		run:           run,
		helpArguments: helpArguments,
	}
}

func (m *MockServingPlatform) Deploy(ctx context.Context, imageTag string, useGPU bool, logWriter logger.Logger) (Deployment, error) {
	time.Sleep(m.bootDuration)
	return &MockServingDeployment{platform: m}, nil
}

type MockServingDeployment struct {
	platform *MockServingPlatform
}

func (d *MockServingDeployment) RunInference(ctx context.Context, input *Example, logWriter logger.Logger) (*Result, error) {
	return d.platform.run(input), nil
}

func (d *MockServingDeployment) Help(ctx context.Context, logWriter logger.Logger) (*HelpResponse, error) {
	return &HelpResponse{d.platform.helpArguments}, nil
}

func (d *MockServingDeployment) Undeploy() error {
	return nil
}
