package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/segmentio/ksuid"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/serving"
	"github.com/replicate/cog/pkg/util/console"
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
	outputChansLock    sync.RWMutex
	cancelChansLock    sync.RWMutex
}

type BuildJob struct {
	messageID string
	dir       string
	name      string
	id        string
	config    *model.Config
	arch      string
}

type BuildResult struct {
	image      *model.Image
	testResult *serving.TestResult
}

type JobOutput struct {
	logInfo     *string
	logDebug    *string
	logError    *string
	logStatus   *string
	error       error
	buildResult *BuildResult
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

func (q *BuildQueue) Start(ctx context.Context) {
	for _, arch := range []string{"cpu", "gpu"} {
		go q.startHandler(ctx, arch)
	}
}

func (q *BuildQueue) startHandler(ctx context.Context, arch string) {
	for {
		select {
		case job := <-q.jobChans[arch]:
			go func() {
				sem := q.archSemaphores[arch]
				sem <- struct{}{}
				defer func() { <-sem }()
				q.handleJob(job)
			}()
		case <-ctx.Done():
			return
		}
	}
}

// Build pushes per-arch BuildJobs onto the build queue's job channels
// and creates result channels for those jobs. It then waits for
// results on the newly created result channels.
func (q *BuildQueue) Build(ctx context.Context, dir string, name string, id string, arch string, config *model.Config, logWriter logger.Logger) (*BuildResult, error) {
	messageID := ksuid.New().String()

	q.outputChansLock.Lock()
	q.outputChans[messageID] = make(chan *JobOutput)
	q.outputChansLock.Unlock()

	q.cancelChansLock.Lock()
	q.cancelChans[messageID] = make(chan struct{})
	q.cancelChansLock.Unlock()

	defer func() {
		q.outputChansLock.Lock()
		delete(q.outputChans, messageID)
		q.outputChansLock.Unlock()
	}()
	defer func() {
		q.cancelChansLock.Lock()
		delete(q.cancelChans, messageID)
		q.cancelChansLock.Unlock()
	}()

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

	result, err := q.waitForResult(messageID, cancelReceivedChan, logWriter)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (q *BuildQueue) waitForResult(messageID string, cancelReceivedChan <-chan struct{}, logWriter logger.Logger) (*BuildResult, error) {
	q.outputChansLock.RLock()
	outputChan := q.outputChans[messageID]
	q.outputChansLock.RUnlock()
	q.cancelChansLock.RLock()
	cancelChan := q.cancelChans[messageID]
	q.cancelChansLock.RUnlock()

	queueState := QueueStateQueued
	ticker := time.NewTicker(10 * time.Second)

	for {
		select {
		case <-ticker.C:
			switch queueState {
			case QueueStateQueued:
				logWriter.Infof("Build is queued")
				queueState = QueueStateStillQueued
			case QueueStateStillQueued:
				logWriter.Infof("Build is still waiting in queue")
			}
		case message := <-outputChan:
			queueState = QueueStateRunning
			ticker.Stop()
			switch {
			case message.buildResult != nil:
				return message.buildResult, nil
			case message.error != nil:
				return nil, message.error
			case message.logError != nil:
				logWriter.WriteError(errors.New(*message.logError))
			case message.logInfo != nil:
				logWriter.Info(*message.logInfo)
			case message.logDebug != nil:
				logWriter.Debug(*message.logDebug)
			case message.logStatus != nil:
				logWriter.WriteStatus(*message.logStatus)
			default:
				console.Warnf("Invalid message: %v", message)
			}
		case <-cancelReceivedChan:
			ticker.Stop()
			cancelChan <- struct{}{}
		}
	}
}

func (q *BuildQueue) handleJob(job *BuildJob) {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		q.cancelChansLock.RLock()
		cancelChan := q.cancelChans[job.messageID]
		q.cancelChansLock.RUnlock()

		<-cancelChan
		console.Debugf("Cancelling build")
		cancel()
	}()

	q.outputChansLock.RLock()
	outChan := q.outputChans[job.messageID]
	q.outputChansLock.RUnlock()

	logWriter := NewQueueLogger(outChan)
	logWriter.WriteStatus("Building image")

	imageURI, err := q.buildDockerImage(ctx, job, logWriter)
	if err != nil {
		outChan <- &JobOutput{error: err}
		return
	}

	testResult, err := serving.TestVersion(ctx, q.servingPlatform, imageURI, job.config.Examples, job.dir, job.arch == "gpu", logWriter)
	if err != nil {
		// TODO(andreas): return other response than 500 if validation fails
		outChan <- &JobOutput{error: err}
		return
	}

	if err := q.dockerImageBuilder.Push(ctx, imageURI, logWriter); err != nil {
		outChan <- &JobOutput{error: err}
		return
	}

	outChan <- &JobOutput{
		buildResult: &BuildResult{
			image: &model.Image{
				URI:          imageURI,
				Arch:         job.arch,
				RunArguments: testResult.RunArgs,
				TestStats:    testResult.Stats,
				Created:      time.Now(),
			},
			testResult: testResult,
		},
	}
}

func (q *BuildQueue) buildDockerImage(ctx context.Context, job *BuildJob, logWriter logger.Logger) (string, error) {
	generator := docker.NewDockerfileGenerator(job.config, job.arch, job.dir)
	dockerfileContents, err := generator.Generate()
	if err != nil {
		return "", fmt.Errorf("Failed to generate Dockerfile for %s: %w", job.arch, err)
	}
	defer generator.Cleanup()
	useGPU := job.config.Environment.BuildRequiresGPU && job.arch == "gpu"
	uri, err := q.dockerImageBuilder.Build(ctx, job.dir, dockerfileContents, job.name, useGPU, logWriter)
	if err != nil {
		return "", fmt.Errorf("Failed to build Docker image: %w", err)
	}
	return uri, nil
}

type QueueLogger struct {
	arch string
	ch   chan *JobOutput
}

func NewQueueLogger(ch chan *JobOutput) *QueueLogger {
	return &QueueLogger{ch: ch}
}

func (l *QueueLogger) Info(line string) {
	l.ch <- &JobOutput{logInfo: &line}
}

func (l *QueueLogger) Debug(line string) {
	l.ch <- &JobOutput{logDebug: &line}
}

func (l *QueueLogger) Infof(line string, args ...interface{}) {
	line = fmt.Sprintf(line, args...)
	l.ch <- &JobOutput{logInfo: &line}
}

func (l *QueueLogger) Debugf(line string, args ...interface{}) {
	line = fmt.Sprintf(line, args...)
	l.ch <- &JobOutput{logDebug: &line}
}

func (l *QueueLogger) WriteStatus(status string, args ...interface{}) {
	line := fmt.Sprintf(status, args...)
	l.ch <- &JobOutput{logStatus: &line}
}

func (l *QueueLogger) WriteError(err error) {
	line := err.Error()
	l.ch <- &JobOutput{logError: &line}
}

func (l *QueueLogger) WriteVersion(version *model.Version) {
	panic("Unexpected call to WriteVersion")
}
