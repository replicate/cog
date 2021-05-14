package server

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/serving"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/assert"
)

func TestQueueBuild(t *testing.T) {
	cpuConcurrency := 3
	gpuConcurrency := 1
	buildStartChans := map[string]chan int{
		"cpu": make(chan int),
		"gpu": make(chan int),
	}
	buildCompleteChans := map[string]chan int{
		"cpu": make(chan int),
		"gpu": make(chan int),
	}
	buildResultChans := map[string]chan *BuildResult{
		"cpu": make(chan *BuildResult),
		"gpu": make(chan *BuildResult),
	}
	counters := map[string]*int32{
		"cpu": int32p(0),
		"gpu": int32p(0),
	}

	config := &model.Config{
		Model: "model.py:Model",
		Environment: &model.Environment{
			BuildRequiresGPU: true,
			Architectures:    []string{"cpu", "gpu"},
		},
		Examples: []*model.Example{{
			Input:  map[string]string{},
			Output: "",
		}},
	}
	err := config.ValidateAndCompleteConfig()
	require.NoError(t, err)

	testRunFunc := func(example *serving.Example) *serving.Result {
		return &serving.Result{
			Values: map[string]serving.ResultValue{
				"output": {
					Buffer:   bytes.NewBuffer([]byte("hello world")),
					MimeType: "text/plain",
				},
			},
			SetupTime:       100,
			RunTime:         0.01,
			UsedMemoryBytes: 5000,
			UsedCPUSecs:     0.1,
		}
	}
	servingPlatform := serving.NewMockServingPlatform(0, testRunFunc, map[string]*model.RunArgument{})

	imageBuildFunc := func(ctx context.Context, dir string, dockerfileContents string, name string, useGPU bool, logWriter logger.Logger) (tag string, err error) {
		arch := "cpu"
		if useGPU {
			arch = "gpu"
		}
		counter := counters[arch]
		value := atomic.AddInt32(counter, 1)
		buildStartChans[arch] <- int(value)
		completeCounter := <-buildCompleteChans[arch]
		assert.Equal(t, int(value), completeCounter)
		return "image-" + arch, nil
	}
	dockerImageBuilder := docker.NewMockImageBuilder(imageBuildFunc)

	logWriter := logger.NewConsoleLogger()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue := NewBuildQueue(servingPlatform, dockerImageBuilder, cpuConcurrency, gpuConcurrency)
	queue.Start(ctx)

	submitBuild := func(arch string) {
		result, err := queue.Build(ctx, "", "name", "id", arch, config, logWriter)
		require.NoError(t, err)
		buildResultChans[arch] <- result
	}

	go submitBuild("cpu")
	require.Equal(t, <-buildStartChans["cpu"], 1)
	time.Sleep(10 * time.Millisecond)
	require.Equal(t, 1, len(queue.archSemaphores["cpu"]))
	require.Equal(t, 0, len(queue.archSemaphores["gpu"]))
	buildCompleteChans["cpu"] <- 1

	result := <-buildResultChans["cpu"]
	time.Sleep(10 * time.Millisecond)
	require.Equal(t, 0, len(queue.archSemaphores["cpu"]))
	require.Equal(t, "image-cpu", result.image.URI)
	require.Equal(t, "cpu", result.image.Arch)

	go submitBuild("cpu")
	require.Equal(t, <-buildStartChans["cpu"], 2)
	go submitBuild("gpu")
	require.Equal(t, <-buildStartChans["gpu"], 1)
	go submitBuild("cpu")
	require.Equal(t, <-buildStartChans["cpu"], 3)

	time.Sleep(10 * time.Millisecond)
	require.Equal(t, 2, len(queue.archSemaphores["cpu"]))
	require.Equal(t, 1, len(queue.archSemaphores["gpu"]))

	go submitBuild("cpu")
	require.Equal(t, <-buildStartChans["cpu"], 4)
	time.Sleep(10 * time.Millisecond)
	require.Equal(t, 3, len(queue.archSemaphores["cpu"]))

	// block
	go submitBuild("cpu")
	go submitBuild("gpu")
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, 3, len(queue.archSemaphores["cpu"]))
	require.Equal(t, 1, len(queue.archSemaphores["gpu"]))

	buildCompleteChans["cpu"] <- 2
	<-buildResultChans["cpu"]

	require.Equal(t, <-buildStartChans["cpu"], 5)
	time.Sleep(10 * time.Millisecond)
	require.Equal(t, 3, len(queue.archSemaphores["cpu"]))

	buildCompleteChans["gpu"] <- 1
	<-buildResultChans["gpu"]

	require.Equal(t, <-buildStartChans["gpu"], 2)
	time.Sleep(10 * time.Millisecond)
	require.Equal(t, 1, len(queue.archSemaphores["gpu"]))

	buildCompleteChans["cpu"] <- 3
	<-buildResultChans["cpu"]

	time.Sleep(10 * time.Millisecond)
	require.Equal(t, 2, len(queue.archSemaphores["cpu"]))

	buildCompleteChans["cpu"] <- 4
	<-buildResultChans["cpu"]
	time.Sleep(10 * time.Millisecond)
	require.Equal(t, 1, len(queue.archSemaphores["cpu"]))

	buildCompleteChans["gpu"] <- 2
	<-buildResultChans["gpu"]
	time.Sleep(10 * time.Millisecond)
	require.Equal(t, 0, len(queue.archSemaphores["gpu"]))

	buildCompleteChans["cpu"] <- 5
	<-buildResultChans["cpu"]
	time.Sleep(10 * time.Millisecond)
	require.Equal(t, 0, len(queue.archSemaphores["cpu"]))
}

func int32p(x int32) *int32 {
	return &x
}
