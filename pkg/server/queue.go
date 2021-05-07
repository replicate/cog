package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/segmentio/ksuid"

	"github.com/replicate/cog/pkg/console"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/serving"
)

const (
	QueueStateQueued      = "queued"
	QueueStateStillQueued = "still queued"
	QueueStateRunning     = "running"
)

type BuildQueue struct {
	servingPlatform    serving.Platform
	dockerImageBuilder docker.ImageBuilder
	jobChans           map[string]chan *BuildJob
	archSemaphores     map[string]chan struct{}
	outputChans        map[string]chan *JobOutput
	cancelChans        map[string]chan struct{}
}

type BuildJob struct {
	messageID string
	dir       string
	name      string
	id        string
	config    *model.Config
	arch      string
}

type JobOutput struct {
	LogInfo   *string
	LogDebug  *string
	LogError  *string
	LogStatus *string
	JobResult *JobResult
}

type JobResult struct {
	Artifact   *model.Artifact
	TestResult *serving.TestResult
	Error      error
	Arch       string
}

type BuildResult struct {
	Artifacts  []*model.Artifact
	RunArgs    map[string]*model.RunArgument
	ModelStats *model.Stats
}

func NewBuildQueue(servingPlatform serving.Platform, dockerImageBuilder docker.ImageBuilder, cpuConcurrency int, gpuConcurrency int) *BuildQueue {
	return &BuildQueue{
		servingPlatform:    servingPlatform,
		dockerImageBuilder: dockerImageBuilder,
		jobChans: map[string]chan *BuildJob{
			"cpu": make(chan *BuildJob),
			"gpu": make(chan *BuildJob),
		},
		archSemaphores: map[string]chan struct{}{
			"cpu": make(chan struct{}, cpuConcurrency),
			"gpu": make(chan struct{}, gpuConcurrency),
		},
		outputChans: make(map[string]chan *JobOutput),
		cancelChans: make(map[string]chan struct{}),
	}
}

func (q *BuildQueue) Start() {
	go func() {
		for _, arch := range []string{"cpu", "gpu"} {
			arch := arch
			go func() {
				for {
					job := <-q.jobChans[arch]
					go func() {
						sem := q.archSemaphores[arch]
						sem <- struct{}{}
						defer func() { <-sem }()
						q.handleJob(job)
					}()
				}
			}()
		}
	}()
}

// Build pushes per-arch BuildJobs onto the build queue's job channels
// and creates result channels for those jobs. It then waits for
// results on the newly created result channels.
func (q *BuildQueue) Build(ctx context.Context, dir string, name string, id string, config *model.Config, logWriter logger.Logger) (*BuildResult, error) {
	resultChan := make(chan *JobResult)
	messageIDs := map[string]string{}
	for _, arch := range config.Environment.Architectures {
		arch := arch

		messageID := ksuid.New().String()
		messageIDs[arch] = messageID
		outputChan := make(chan *JobOutput)
		q.outputChans[messageID] = outputChan
		cancelChan := make(chan struct{})
		q.cancelChans[messageID] = cancelChan

		q.jobChans[arch] <- &BuildJob{
			messageID: messageID,
			dir:       dir,
			id:        id,
			name:      name,
			config:    config,
			arch:      arch,
		}

		cancelReceivedChan := make(chan struct{})
		go func() {
			<-ctx.Done()
			cancelReceivedChan <- struct{}{}
		}()

		go func() {
			defer delete(q.outputChans, messageID)
			defer delete(q.cancelChans, messageID)

			queueState := QueueStateQueued
			ticker := time.NewTicker(10 * time.Second)

			for {
				select {
				case <-ticker.C:
					switch queueState {
					case QueueStateQueued:
						logWriter.Infof("[%s] Build is queued", arch)
						queueState = QueueStateStillQueued
					case QueueStateStillQueued:
						logWriter.Infof("[%s] Build is still waiting in queue", arch)
					}
				case message := <-outputChan:
					queueState = QueueStateRunning
					ticker.Stop()
					switch {
					case message.JobResult != nil:
						resultChan <- message.JobResult
						return
					case message.LogError != nil:
						logWriter.WriteError(errors.New(*message.LogError))
					case message.LogInfo != nil:
						logWriter.Info(*message.LogInfo)
					case message.LogDebug != nil:
						logWriter.Debug(*message.LogDebug)
					case message.LogStatus != nil:
						logWriter.WriteStatus(*message.LogStatus)
					default:
						console.Warnf("Invalid message: %v", message)
					}
				case <-cancelReceivedChan:
					ticker.Stop()
					cancelChan <- struct{}{}
				}
			}
		}()
	}

	results := []*JobResult{}
	for range config.Environment.Architectures {
		result := <-resultChan
		if result.Error != nil {

			// TODO(andreas): cancel other build
			return nil, result.Error
		}
		results = append(results, result)
	}

	if err := saveExamples(results, dir, config); err != nil {
		return nil, err
	}

	return mergeBuildResults(results), nil
}

func (q *BuildQueue) handleJob(job *BuildJob) {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		cancelChan := q.cancelChans[job.messageID]
		<-cancelChan
		console.Info("Cancelling build")
		cancel()
	}()

	outChan := q.outputChans[job.messageID]
	logWriter := NewQueueLogger(job.arch, outChan)
	logWriter.WriteStatus("Building image")

	artifact, err := q.buildDockerImage(ctx, job, logWriter)
	if err != nil {
		outChan <- &JobOutput{JobResult: &JobResult{Error: err}}
		return
	}

	testResult, err := serving.TestModel(ctx, q.servingPlatform, artifact.URI, job.config, job.dir, job.arch == "gpu", logWriter)
	if err != nil {
		// TODO(andreas): return other response than 500 if validation fails
		outChan <- &JobOutput{JobResult: &JobResult{Error: err}}
		return
	}

	if err := q.dockerImageBuilder.Push(ctx, artifact.URI, logWriter); err != nil {
		outChan <- &JobOutput{JobResult: &JobResult{Error: err}}
		return
	}

	outChan <- &JobOutput{JobResult: &JobResult{
		Artifact:   artifact,
		TestResult: testResult,
		Arch:       job.arch,
	}}
}

func (q *BuildQueue) buildDockerImage(ctx context.Context, job *BuildJob, logWriter logger.Logger) (*model.Artifact, error) {
	generator := &docker.DockerfileGenerator{Config: job.config, Arch: job.arch, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	dockerfileContents, err := generator.Generate()
	if err != nil {
		return nil, fmt.Errorf("Failed to generate Dockerfile for %s: %w", job.arch, err)
	}
	useGPU := job.config.Environment.BuildRequiresGPU && job.arch == "gpu"
	tag, err := q.dockerImageBuilder.Build(ctx, job.dir, dockerfileContents, job.name, useGPU, logWriter)
	if err != nil {
		return nil, fmt.Errorf("Failed to build Docker image: %w", err)
	}
	var target string
	switch job.arch {
	case "cpu":
		target = model.TargetDockerCPU
	case "gpu":
		target = model.TargetDockerGPU
	}
	return &model.Artifact{
		Target: target,
		URI:    tag,
	}, nil
}

func mergeBuildResults(results []*JobResult) *BuildResult {
	result := &BuildResult{
		Artifacts:  []*model.Artifact{},
		RunArgs:    results[0].TestResult.RunArgs,
		ModelStats: new(model.Stats),
	}
	for _, res := range results {
		result.Artifacts = append(result.Artifacts, res.Artifact)
		stats := res.TestResult.Stats
		switch res.Arch {
		case "cpu":
			result.ModelStats.BootTimeCPU = stats.BootTimeCPU
			result.ModelStats.SetupTimeCPU = stats.SetupTimeCPU
			result.ModelStats.RunTimeCPU = stats.RunTimeCPU
			result.ModelStats.MemoryUsageCPU = stats.MemoryUsageCPU
			result.ModelStats.CPUUsageCPU = stats.CPUUsageCPU
		case "gpu":
			result.ModelStats.BootTimeGPU = stats.BootTimeGPU
			result.ModelStats.SetupTimeGPU = stats.SetupTimeGPU
			result.ModelStats.RunTimeGPU = stats.RunTimeGPU
			result.ModelStats.MemoryUsageGPU = stats.MemoryUsageGPU
			result.ModelStats.CPUUsageGPU = stats.CPUUsageGPU
		}
	}
	return result
}

func saveExamples(results []*JobResult, dir string, config *model.Config) error {
	// get the examples from the first result
	result := results[0].TestResult

	if len(result.NewExampleOutputs) > 0 {
		config.Examples = result.Examples

		for outputPath, outputBytes := range result.NewExampleOutputs {
			exampleDir := filepath.Dir(outputPath)
			if err := os.MkdirAll(filepath.Join(dir, exampleDir), 0755); err != nil {
				return fmt.Errorf("Failed to make output dir: %w", err)
			}
			if err := os.WriteFile(filepath.Join(dir, outputPath), outputBytes, 0644); err != nil {
				return fmt.Errorf("Failed to write output: %w", err)
			}
		}
	}
	return nil
}

type QueueLogger struct {
	arch string
	ch   chan *JobOutput
}

func NewQueueLogger(arch string, ch chan *JobOutput) *QueueLogger {
	return &QueueLogger{arch: arch, ch: ch}
}

func (l *QueueLogger) Info(line string) {
	line = fmt.Sprintf("[%s] ", l.arch) + line
	l.ch <- &JobOutput{LogInfo: &line}
}

func (l *QueueLogger) Debug(line string) {
	line = fmt.Sprintf("[%s] ", l.arch) + line
	l.ch <- &JobOutput{LogDebug: &line}
}

func (l *QueueLogger) Infof(line string, args ...interface{}) {
	line = fmt.Sprintf(line, args...)
	line = fmt.Sprintf("[%s] ", l.arch) + line
	l.ch <- &JobOutput{LogInfo: &line}
}

func (l *QueueLogger) Debugf(line string, args ...interface{}) {
	line = fmt.Sprintf(line, args...)
	line = fmt.Sprintf("[%s] ", l.arch) + line
	l.ch <- &JobOutput{LogDebug: &line}
}

func (l *QueueLogger) WriteStatus(status string, args ...interface{}) {
	line := fmt.Sprintf(status, args...)
	line = fmt.Sprintf("[%s] ", l.arch) + line
	l.ch <- &JobOutput{LogStatus: &line}
}

func (l *QueueLogger) WriteError(err error) {
	line := err.Error()
	line = fmt.Sprintf("[%s] ", l.arch) + line
	l.ch <- &JobOutput{LogError: &line}
}

func (l *QueueLogger) WriteModel(mod *model.Model) {
	panic("Unexpected call to WriteModel")
}
