package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"go.uber.org/zap"

	"github.com/replicate/go/httpclient"

	"github.com/replicate/cog/coglet/internal/config"
	"github.com/replicate/cog/coglet/internal/logging"
	"github.com/replicate/cog/coglet/internal/version"
	"github.com/replicate/cog/coglet/internal/webhook"
)

var (
	LogRegex      = regexp.MustCompile(`^\[pid=(?P<pid>[^]]+)] (?P<msg>.*)$`)
	ResponseRegex = regexp.MustCompile(`^response-(?P<pid>\S+)-(?P<epoch>\d+).json$`)
	CancelFmt     = "cancel-%s"
	ErrNoCommand  = errors.New("no command available")
)

// watchPredictionResponses watches for response files specific to a prediction using inotify + fallback polling + IPC drain
func (r *Runner) watchPredictionResponses(ctx context.Context, predictionID string, pending *PendingPrediction) {
	defer close(pending.watcherDone)

	log := r.logger.Sugar()
	responsePattern := fmt.Sprintf("response-%s-", predictionID)

	// TODO: Setup inotify watcher for the working directory (if available)
	// var inotifyEvents <-chan fsnotify.Event

	// Fallback polling timer - resets when we get inotify/IPC events
	pollTimer := time.NewTicker(100 * time.Millisecond)
	defer pollTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Tracew("response watcher canceled", "prediction_id", predictionID)
			return

		// TODO: Add inotify case when implemented
		// case event := <-inotifyEvents:
		//     if strings.HasPrefix(event.Name, responsePattern) {
		//         pollTimer.Reset(100 * time.Millisecond) // Reset polling timer
		//         r.processResponseFiles(predictionID, pending, responsePattern, log)
		//     }

		case <-pending.outputNotify:
			// Drain IPC OUTPUT notifications - when inotify available, we blackhole these
			// When inotify unavailable, this triggers immediate processing
			// TODO: Only process if inotify unavailable
			log.Tracew("received OUTPUT IPC notification", "prediction_id", predictionID)
			pollTimer.Reset(100 * time.Millisecond) // Reset polling timer since we got an event
			if err := r.processResponseFiles(predictionID, pending, responsePattern, log); err != nil {
				log.Errorw("failed to process response files from IPC", "prediction_id", predictionID, "error", err)
			}

		case <-pollTimer.C:
			// Fallback polling - only triggers if no recent inotify/IPC events
			if err := r.processResponseFiles(predictionID, pending, responsePattern, log); err != nil {
				log.Errorw("failed to process response files from polling", "prediction_id", predictionID, "error", err)
			}
		}

		// Check if prediction completed after processing and exit if so
		pending.mu.Lock()
		completed := pending.response.Status.IsCompleted()
		pending.mu.Unlock()
		if completed {
			log.Tracew("prediction completed, watcher exiting", "prediction_id", predictionID)
			return
		}
	}
}

// processResponseFiles handles response file processing for a specific prediction
func (r *Runner) processResponseFiles(predictionID string, pending *PendingPrediction, responsePattern string, log *logging.SugaredLogger) error {
	entries, err := os.ReadDir(r.runnerCtx.workingdir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	for _, entry := range entries {
		// Only process files that match this prediction's pattern
		if !strings.HasPrefix(entry.Name(), responsePattern) {
			continue
		}

		// Verify it matches the full regex pattern
		m := ResponseRegex.FindStringSubmatch(entry.Name())
		if len(m) < 2 || m[1] != predictionID {
			continue
		}

		if err := r.handleSingleResponse(entry.Name(), predictionID, pending, log); err != nil {
			log.Errorw("failed to handle response file", "file", entry.Name(), "prediction_id", predictionID, "error", err)
		}
	}
	return nil
}

// handleSingleResponse processes a single response file for a prediction
func (r *Runner) handleSingleResponse(filename, predictionID string, pending *PendingPrediction, log *logging.SugaredLogger) error {
	filePath := path.Join(r.runnerCtx.workingdir, filename)

	// Read and parse response file
	responseData, err := os.ReadFile(filePath) //nolint:gosec // expected dynamic path
	if err != nil {
		return err
	}

	var response PredictionResponse
	if err := json.Unmarshal(responseData, &response); err != nil {
		return err
	}

	// Python response doesn't include ID/Input/etc, so merge from request
	response.populateFromRequest(pending.request)

	// Add logs if available from pending prediction
	pending.mu.Lock()
	if len(pending.response.Logs) > 0 {
		response.Logs = pending.response.Logs
	}
	pending.mu.Unlock()

	// Delete response file to avoid duplicates
	if err := os.Remove(filePath); err != nil {
		log.Errorw("failed to remove response file", "path", filePath, "error", err)
	}

	// Process output if present
	if err := r.processResponseOutput(&response, pending, log); err != nil {
		response.Status = PredictionFailed
		response.Error = fmt.Sprintf("failed to process output: %v", err)
	}

	// Send webhooks and handle completion
	r.handleResponseWebhooksAndCompletion(&response, predictionID, pending, log)

	return nil
}

// processResponseOutput handles output path processing for a response
func (r *Runner) processResponseOutput(response *PredictionResponse, pending *PendingPrediction, log *logging.SugaredLogger) error {
	if response.Output == nil {
		return nil
	}

	paths := make([]string, 0)
	var outputFn func(string, *[]string) (string, error)

	// Determine output processing function based on configuration
	outputFn = OutputToBase64
	if r.runnerCtx.uploader != nil {
		if r.runnerCtx.uploader.client == nil {
			return fmt.Errorf("uploader client not initialized")
		}
		outputFn = func(s string, paths *[]string) (string, error) {
			return r.runnerCtx.uploader.processOutput(s, response.ID, paths)
		}
	}

	// Create cached output function to avoid processing same file multiple times
	cachedOutputFn := func(s string, paths *[]string) (string, error) {
		if cache, ok := pending.outputCache[s]; ok {
			return cache, nil
		}
		o, err := outputFn(s, paths)
		if err != nil {
			return "", err
		}
		pending.outputCache[s] = o
		return o, nil
	}

	// Process the output
	processedOutput, err := handlePath(response.Output, &paths, cachedOutputFn)
	if err != nil {
		return err
	}

	response.Output = processedOutput

	// Clean up processed files
	for _, p := range paths {
		if err := os.Remove(p); err != nil {
			log.Errorw("failed to remove output path", "path", p, "error", err)
		}
	}

	return nil
}

// handleResponseWebhooksAndCompletion sends webhooks and handles prediction completion
func (r *Runner) handleResponseWebhooksAndCompletion(response *PredictionResponse, predictionID string, pending *PendingPrediction, log *logging.SugaredLogger) {
	// Handle legacy compatibility: change "starting" to "processing" for webhook purposes
	webhookStatus := response.Status
	if response.Status == PredictionStarting {
		webhookStatus = PredictionProcessing
	}

	// Update pending response once with all necessary fields
	pending.mu.Lock()
	existingLogs := pending.response.Logs
	pending.response = *response
	pending.response.Status = webhookStatus

	// Preserve accumulated logs if new response doesn't have them
	if len(existingLogs) > 0 && len(response.Logs) == 0 {
		pending.response.Logs = existingLogs
	}

	// Restore request-derived fields and finalize if terminal
	pending.response.populateFromRequest(pending.request)
	completed := pending.response.Status.IsCompleted()
	if completed {
		if err := pending.response.finalizeResponse(); err != nil {
			log.Errorw("failed to finalize response", "error", err)
		}
	}
	pending.mu.Unlock()

	// Send appropriate webhooks
	r.sendStatusWebhook(pending, response, webhookStatus, log)

	// Handle terminal completion
	if completed {
		r.handleTerminalCompletion(response, pending, predictionID, log)
	}
}

// sendStatusWebhook sends webhooks based on prediction status
func (r *Runner) sendStatusWebhook(pending *PendingPrediction, response *PredictionResponse, status PredictionStatus, log *logging.SugaredLogger) {
	switch status {
	case PredictionStarting:
		// This case shouldn't happen due to compatibility transformation above
		log.Debugw("prediction started", "id", response.ID, "status", status)
		go func() { _ = pending.sendWebhook(webhook.EventStart) }()

	case PredictionProcessing:
		if response.Status == PredictionStarting {
			log.Debugw("prediction started", "id", response.ID, "status", "starting->processing")
			go func() { _ = pending.sendWebhook(webhook.EventStart) }()
		} else {
			log.Debugw("prediction processing", "id", response.ID, "status", status)
			if response.Output != nil {
				go func() { _ = pending.sendWebhook(webhook.EventOutput) }()
			} else {
				go func() { _ = pending.sendWebhook(webhook.EventLogs) }()
			}
		}
	}
}

// handleTerminalCompletion handles cleanup and response sending for completed predictions
func (r *Runner) handleTerminalCompletion(response *PredictionResponse, pending *PendingPrediction, predictionID string, log *logging.SugaredLogger) {
	log.Infow("prediction completed", "id", response.ID, "status", response.Status)

	// Prepare local response copy with all fields populated and finalized
	pending.mu.Lock()
	finalResponse := *response
	finalResponse.populateFromRequest(pending.request)
	pending.mu.Unlock()

	if err := finalResponse.finalizeResponse(); err != nil {
		log.Errorw("failed to finalize response", "error", err)
	}

	// Send response and close channel - use local copy to avoid race conditions
	pending.safeSend(finalResponse)
	pending.safeClose()

	// Clean up input paths
	for _, inputPath := range pending.inputPaths {
		if err := os.Remove(inputPath); err != nil {
			log.Errorw("failed to remove input path", "path", inputPath, "error", err)
		}
	}

	log.Tracew("prediction completed, watcher exiting", "prediction_id", predictionID)
}

type (
	killFunc                         func(int) error
	verifyProcessGroupTerminatedFunc func(int) error
)

const (
	DefaultRunnerID   = 0
	DefaultRunnerName = "default"
)

type Runner struct {
	ctx                context.Context //nolint:containedctx // this is a root context derived from the manager's context, this is expected to be embedded
	cancel             context.CancelFunc
	runnerCtx          RunnerContext
	cmd                *exec.Cmd
	status             Status
	schema             string
	doc                *openapi3.T
	setupResult        SetupResult
	logs               LogsSlice
	asyncPredict       bool
	maxConcurrency     int
	pending            map[string]*PendingPrediction
	killed             bool
	cleanupSlot        chan struct{}
	killFn             killFunc
	verifyFn           verifyProcessGroupTerminatedFunc
	procedureHash      string
	mu                 sync.RWMutex
	stopped            chan bool
	shutdownWhenIdle   atomic.Bool
	readyForShutdown   chan struct{} // closed when idle and ready to be stopped
	setupComplete      chan struct{} // closed on first READY after setup
	webhookSender      webhook.Sender
	logCaptureComplete chan struct{} // closed when both stdout/stderr capture complete
	cleanupTimeout     time.Duration
	forceShutdown      *config.ForceShutdownSignal

	logger *logging.Logger
}

func (r *Runner) String() string {
	// For procedure runners, return slot:procedure_url format expected by tests
	if r.procedureHash != "" {
		return fmt.Sprintf("%s:%s", r.runnerCtx.id, r.procedureHash)
	}
	return fmt.Sprintf("Runner{name=%s, status=%s}", r.runnerCtx.id, r.status)
}

func (r *Runner) getCmd() (*exec.Cmd, error) {
	if r.cmd == nil {
		return nil, ErrNoCommand
	}
	return r.cmd, nil
}

func (r *Runner) hasCapacity() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.pending) < r.maxConcurrency
}

func (r *Runner) Idle() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.pending) == 0
}

func (r *Runner) WaitForStop() {
	<-r.stopped
}

func (r *Runner) GracefulShutdown() {
	log := r.logger.Sugar()
	if !r.shutdownWhenIdle.CompareAndSwap(false, true) {
		log.Tracew("graceful shutdown already initiated", "runner_id", r.runnerCtx.id)
		return
	}

	r.mu.RLock()
	shouldSignal := (r.status == StatusReady && len(r.pending) == 0)
	r.mu.RUnlock()

	log.Tracew("graceful shutdown initiated", "runner_id", r.runnerCtx.id, "status", r.status, "pending_count", len(r.pending), "should_signal", shouldSignal)

	if shouldSignal {
		if r.readyForShutdown == nil {
			log.Warnw("readyForShutdown channel is nil, cannot signal shutdown readiness", "runner_id", r.runnerCtx.id)
		} else {
			select {
			case <-r.readyForShutdown:
				log.Tracew("readyForShutdown already closed", "runner_id", r.runnerCtx.id)
			default:
				log.Tracew("closing readyForShutdown channel", "runner_id", r.runnerCtx.id)
				close(r.readyForShutdown)
			}
		}
	}
}

func (r *Runner) Start(ctx context.Context) error {
	log := r.logger.Sugar()
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.status != StatusStarting {
		return fmt.Errorf("runner not in starting state: %s", r.status)
	}

	cmd, err := r.getCmd()
	if err != nil {
		return err
	}

	log.Debug("starting runner subprocess")

	// Setup log capture BEFORE starting subprocess (like old code)
	if err := r.setupLogCapture(); err != nil {
		return fmt.Errorf("failed to setup log capture: %w", err)
	}

	if err := cmd.Start(); err != nil {
		log.Errorw("failed to start subprocess", "error", err)
		return fmt.Errorf("failed to start subprocess: %w", err)
	}

	log.Tracew("runner process started successfully", "pid", cmd.Process.Pid)

	return nil
}

func (r *Runner) setupLogCapture() error {
	cmd, err := r.getCmd()
	if err != nil {
		return err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Use sync.WaitGroup to track when both stdout and stderr capture complete
	var wg sync.WaitGroup

	wg.Go(func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			r.logStdout(line)
		}
		r.logger.Trace("finished stdout log capture")
	})

	wg.Go(func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			r.logStderr(line)
		}
		r.logger.Trace("finished stderr log capture")
	})

	// Signal when both pipes are closed (with double-close protection)
	go func() {
		wg.Wait()

		// Guard against double close
		select {
		case <-r.logCaptureComplete:
			// Already closed
		default:
			close(r.logCaptureComplete)
		}
	}()

	return nil
}

// logStdout captures a line from stdout and mirrors to stdout
func (r *Runner) logStdout(line string) {
	r.captureLogLine(line)

	// Strip [pid=xxxxx] prefix before mirroring to stdout
	mirrorLine := stripPIDPrefix(line)
	_, _ = fmt.Fprintln(os.Stdout, mirrorLine) //nolint:forbidigo // mirror log to stdout
}

// logStderr captures a line from stderr and mirrors to stderr
func (r *Runner) logStderr(line string) {
	r.captureLogLine(line)

	// Strip [pid=xxxxx] prefix before mirroring to stderr
	mirrorLine := stripPIDPrefix(line)
	_, _ = fmt.Fprintln(os.Stderr, mirrorLine) //nolint:forbidigo // mirror log to stderr
}

func stripPIDPrefix(line string) string {
	if LogRegex.MatchString(line) {
		if m := LogRegex.FindStringSubmatch(line); m != nil {
			return m[2] // Extract message without pid prefix
		}
	}
	return line
}

// captureLogLine handles routing log lines like the old implementation
func (r *Runner) captureLogLine(line string) {
	log := r.logger.Sugar()

	switch {
	case LogRegex.MatchString(line):
		// Log has PID prefix - route to specific prediction
		if m := LogRegex.FindStringSubmatch(line); m != nil {
			pid := m[1]
			msg := m[2]
			r.mu.Lock()
			if pending, ok := r.pending[pid]; ok {
				pending.mu.Lock()
				if pending.response.Logs == nil {
					pending.response.Logs = make([]string, 0)
				}
				pending.response.Logs = append(pending.response.Logs, msg)
				pending.mu.Unlock()
				// Send webhook if prediction has started
				if pending.response.Status != "" {
					go func() { _ = pending.sendWebhook(webhook.EventLogs) }()
				}
			} else {
				log.Errorw("received log for non-existent prediction", "id", pid, "message", msg)
			}
			r.mu.Unlock()
		}
	case !strings.Contains(line, "[coglet]"):
		// No PID prefix and not coglet - route appropriately
		r.mu.Lock()
		if r.setupResult.Status == SetupSucceeded && len(r.pending) == 1 && !r.asyncPredict {
			// Route to single pending prediction
			for _, pending := range r.pending {
				pending.mu.Lock()
				if pending.response.Logs == nil {
					pending.response.Logs = make([]string, 0)
				}
				pending.response.Logs = append(pending.response.Logs, line)
				pending.mu.Unlock()
				// Send webhook if prediction has started
				if pending.response.Status != "" {
					go func() { _ = pending.sendWebhook(webhook.EventLogs) }()
				}
			}
		} else {
			// Add to runner logs for crash reporting
			r.logs = append(r.logs, line)
			r.setupResult.Logs = r.logs.String()
		}
		r.mu.Unlock()
	default:
		// [coglet] logs - don't route anywhere, just ignore for capture
	}
}

func (r *Runner) Config(ctx context.Context) error {
	waitFile := os.Getenv("COG_WAIT_FILE")
	if waitFile != "" {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			if _, err := os.Stat(waitFile); err == nil {
				break
			}
			select {
			case <-ticker.C:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	// Read cog.yaml and setup configuration like the old code
	cogYaml, err := ReadCogYaml(r.runnerCtx.workingdir)
	if err != nil {
		return fmt.Errorf("failed to read cog.yaml: %w", err)
	}

	// Extract module and predictor name from cog.yaml predict field
	moduleName := "predict"
	predictorName := "Predictor"
	if cogYaml.Predict != "" {
		if mod, pred, err := cogYaml.PredictModuleAndPredictor(); err == nil {
			moduleName = mod
			predictorName = pred
		}
	}

	// Default to 1 if not set in cog.yaml, regardless whether async predict or not
	maxConcurrency := max(1, cogYaml.Concurrency.Max)

	// Send metrics
	go r.sendRunnerMetric(*cogYaml)

	// Create config.json for the coglet process
	configJSON := map[string]any{
		"module_name":     moduleName,
		"predictor_name":  predictorName,
		"max_concurrency": maxConcurrency,
	}

	configPath := filepath.Join(r.runnerCtx.workingdir, "config.json")
	configData, err := json.Marshal(configJSON)
	if err != nil {
		return fmt.Errorf("failed to marshal config.json: %w", err)
	}

	if err := os.WriteFile(configPath, configData, 0o644); err != nil { //nolint:gosec // 0o644 is the correct permissions for non-root consumer
		return fmt.Errorf("failed to write config.json: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Update max concurrency from cog.yaml
	r.maxConcurrency = maxConcurrency

	// Status remains StatusStarting until IPC "READY" signal
	return nil
}

func (r *Runner) sendRunnerMetric(cogYaml CogYaml) {
	log := r.logger.Sugar()
	// FIXME: wire this up through more than os.getenv
	endpoint := os.Getenv("COG_METRICS_ENDPOINT")
	if endpoint == "" {
		return
	}
	data := map[string]any{
		"gpu":         cogYaml.Build.GPU,
		"fast":        cogYaml.Build.Fast,
		"cog_runtime": cogYaml.Build.CogRuntime,
		"version":     version.Version(),
	}
	payload := MetricsPayload{
		Source: "cog-runtime",
		Type:   "runner",
		Data:   data,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Errorw("failed to marshal payload", "error", err)
		return
	}
	resp, err := httpclient.ApplyRetryPolicy(http.DefaultClient).Post(endpoint, "application/json", bytes.NewBuffer(body))
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Errorw("failed to send runner metrics", "error", err)
	}
	defer resp.Body.Close()
}

func (r *Runner) Stop() error {
	log := r.logger.Sugar()
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.status == StatusDefunct {
		return nil
	}

	// Close all pending prediction channels to avoid goroutine leaks
	for id, pending := range r.pending {
		response := PredictionResponse{
			ID:     id,
			Status: PredictionFailed,
			Input:  pending.request.Input,
			Error:  "runner stopped",
		}
		response.populateFromRequest(pending.request)
		pending.safeSend(response)
		pending.safeClose()

		// Clean up input paths
		for _, inputPath := range pending.inputPaths {
			err := os.Remove(inputPath)
			if err != nil {
				log.Errorw("failed to remove input path", "path", inputPath, "error", err)
			}
		}

		delete(r.pending, id)
	}

	if cmd, err := r.getCmd(); err == nil && cmd.Process != nil && cmd.ProcessState == nil {
		if err := r.killFn(cmd.Process.Pid); err != nil {
			return fmt.Errorf("failed to kill process: %w", err)
		}
	}

	r.status = StatusDefunct

	if err := r.runnerCtx.Cleanup(); err != nil {
		return fmt.Errorf("failed to cleanup runner context: %w", err)
	}

	select {
	case <-r.stopped:
	default:
		close(r.stopped)
	}

	return nil
}

func (r *Runner) ForceKill() {
	log := r.logger.Sugar()
	r.mu.Lock()
	defer r.mu.Unlock()

	cmd, err := r.getCmd()
	if r.killed || err != nil || cmd.Process == nil || cmd.ProcessState != nil {
		return
	}

	pid := cmd.Process.Pid

	// In non-procedure mode, use cleanup token system for proper isolation cleanup
	if r.forceShutdown == nil {
		// Non-procedure mode: simple kill without cleanup token system
		err = r.killFn(pid)
		if err != nil {
			log.Errorw("failed to kill process", "pid", pid, "error", err)
			// Mark runner as defunct on kill failure
			r.status = StatusDefunct
		}
		r.killed = true
		log.Infow("force killed runner process", "pid", pid)
		return
	}

	// Procedure mode: use cleanup token system for proper isolation cleanup
	// Try to acquire cleanup token
	var gotToken bool
	select {
	case <-r.cleanupSlot:
		gotToken = true
		log.Tracew("acquired cleanup token for force kill", "pid", pid)
	default:
		log.Tracew("cleanup already in progress, skipping force kill", "pid", pid)
		return
	}

	err = r.killFn(pid)
	if err != nil {
		log.Errorw("failed to kill process", "pid", pid, "error", err)
		// Mark runner as defunct on kill failure to prevent it from being marked ready again
		r.status = StatusDefunct
		r.killed = true
		// Return cleanup token on kill failure
		if gotToken {
			select {
			case r.cleanupSlot <- struct{}{}:
			default:
			}
		}
		return
	}
	r.killed = true

	// Start background verification if we got the cleanup token
	if gotToken {
		go r.verifyProcessCleanup(pid)
	}
}

func (r *Runner) verifyProcessCleanup(pid int) {
	log := r.logger.Sugar()
	log.Infow("starting process cleanup verification", "pid", pid)

	timeout := r.cleanupTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-r.stopped:
		log.Infow("process cleanup verified successfully", "pid", pid)
		select {
		case r.cleanupSlot <- struct{}{}:
		default:
		}
		return

	case <-timer.C:
		log.Errorw("process cleanup timeout exceeded, forcing server exit",
			"pid", pid, "timeout", timeout)
		if r.forceShutdown.TriggerForceShutdown() {
			log.Errorw("triggered force shutdown signal")
		}
		return
	}
}

// predict returns a channel that will receive the prediction response and an initial prediction response
// populated with the relevant fields from the request
func (r *Runner) predict(reqID string) (chan PredictionResponse, *PredictionResponse, error) {
	log := r.logger.Sugar()
	r.mu.Lock()
	defer r.mu.Unlock()

	log.Tracew("runner.predict called", "prediction_id", reqID, "status", r.status)

	// Prediction must be pre-allocated by manager
	pending, exists := r.pending[reqID]
	if !exists {
		return nil, nil, fmt.Errorf("prediction %s not allocated", reqID)
	}

	if pending.request.ID != reqID {
		return nil, nil, fmt.Errorf("prediction ID mismatch: expected %s, got %s", reqID, pending.request.ID)
	}

	pending.mu.Lock()
	defer pending.mu.Unlock()

	log.Tracew("prediction found in pending", "prediction_id", reqID)

	// Process input paths (base64 and URL inputs)
	inputPaths := make([]string, 0)
	if r.doc == nil {
		log.Errorw("OpenAPI schema not available for input processing - cannot convert base64 or URL inputs", "prediction_id", reqID)
	} else {
		// Process base64 inputs first, then URL inputs (to allow URL inputs to reference base64-decoded files)
		input, err := ProcessInputPaths(pending.request.Input, r.doc, &inputPaths, Base64ToInput)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to process base64 inputs: %w", err)
		}

		input, err = ProcessInputPaths(input, r.doc, &inputPaths, URLToInput)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to process URL inputs: %w", err)
		}
		pending.request.Input = input
	}
	pending.inputPaths = inputPaths

	// Write prediction request to file
	requestFile := fmt.Sprintf("request-%s.json", reqID)
	log.Debugw("writing prediction request file", "prediction_id", reqID, "file", requestFile)
	requestPath := path.Join(r.runnerCtx.workingdir, requestFile)

	requestData, err := json.Marshal(pending.request)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if err := os.WriteFile(requestPath, requestData, 0o644); err != nil { //nolint:gosec // #nosec G304 -- TODO[md]: validate requestPath is within workingdir
		return nil, nil, fmt.Errorf("failed to write request file: %w", err)
	}

	log.Tracew("wrote prediction request file", "prediction_id", reqID, "path", requestPath, "working_dir", r.runnerCtx.workingdir, "request_data", string(requestData))

	// Debug: Check if file actually exists and list directory contents
	if _, err := os.Stat(requestPath); err != nil { // #nosec G304 -- path derived from controlled workingdir via path.Join
		log.Tracew("ERROR: written request file does not exist", "prediction_id", reqID, "path", requestPath, "error", err)
	} else {
		log.Tracew("confirmed request file exists", "prediction_id", reqID, "path", requestPath)
	}

	// Debug: List all files in working directory
	if entries, err := os.ReadDir(r.runnerCtx.workingdir); err == nil {
		fileNames := make([]string, len(entries))
		for i, entry := range entries {
			fileNames[i] = entry.Name()
		}
		log.Tracew("working directory contents after write", "prediction_id", reqID, "working_dir", r.runnerCtx.workingdir, "files", fileNames)
	}

	log.Tracew("returning prediction channel", "prediction_id", reqID)
	initialResponse := &PredictionResponse{
		Status: PredictionStarting,
	}
	initialResponse.populateFromRequest(pending.request)
	return pending.c, initialResponse, nil
}

func (r *Runner) Cancel(pid string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if _, exists := r.pending[pid]; !exists {
		return fmt.Errorf("prediction not found: %s", pid)
	}

	// Write cancel file for Python IPC - slot will be freed when Python responds
	cancelFile := fmt.Sprintf(CancelFmt, pid)
	cancelPath := path.Join(r.runnerCtx.workingdir, cancelFile)
	return os.WriteFile(cancelPath, []byte{}, 0o644) //nolint:gosec // 0o644 is the correct permissions for non-root consumer
}

func (r *Runner) updateStatus(statusStr string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	status, err := StatusFromString(statusStr)
	if err != nil {
		return err
	}
	r.status = status

	// Close readyForShutdown channel when idle and shutdown requested
	if status == StatusReady && r.shutdownWhenIdle.Load() && len(r.pending) == 0 {
		select {
		case <-r.readyForShutdown:
			// Already closed
		default:
			close(r.readyForShutdown)
		}
	}

	return nil
}

func (r *Runner) HandleIPC(status string) error {
	switch status {
	case "READY":
		if r.status == StatusStarting {
			r.updateSchema()
			r.updateSetupResult()
			// Close setupComplete channel to signal first READY after setup
			r.mu.Lock()
			select {
			case <-r.setupComplete:
				// Already closed
			default:
				close(r.setupComplete)
			}
			r.mu.Unlock()
		}
		if err := r.updateStatus(status); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}
	case "BUSY":
		if err := r.updateStatus(status); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}
	case "OUTPUT":
		// Notify all active prediction watchers of OUTPUT event
		r.mu.RLock()
		for _, pending := range r.pending {
			select {
			case pending.outputNotify <- struct{}{}:
				// Notification sent
			default:
				// Channel full, skip (watcher will poll anyway)
			}
		}
		r.mu.RUnlock()
	}
	return nil
}

func (r *Runner) updateSchema() {
	r.mu.Lock()
	defer r.mu.Unlock()

	log := r.logger.Sugar()
	schemaPath := filepath.Join(r.runnerCtx.workingdir, "openapi.json")
	log.Tracew("attempting to read openapi.json", "path", schemaPath)

	if schemaData, err := os.ReadFile(schemaPath); err == nil { //nolint:gosec // expected dynamic path
		r.schema = string(schemaData)
		log.Tracew("successfully read openapi.json", "schema_length", len(schemaData))

		// Parse the schema for use in ProcessInputPaths
		if doc, parseErr := openapi3.NewLoader().LoadFromData(schemaData); parseErr == nil {
			r.doc = doc
			log.Tracew("successfully parsed openapi schema for ProcessInputPaths")
		} else {
			log.Errorw("failed to parse openapi schema", "error", parseErr)
		}
	} else {
		log.Tracew("failed to read openapi.json", "error", err)
	}
}

func (r *Runner) updateSetupResult() {
	log := r.logger.Sugar()

	// First get the logs from rotateLogs (before acquiring lock)
	logs := r.rotateLogs()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Convert logs string to slice of strings by splitting on newlines
	logLines := make([]string, 0)
	if logs != "" {
		for _, line := range strings.Split(logs, "\n") {
			if strings.TrimSpace(line) != "" {
				logLines = append(logLines, line)
			}
		}
	}

	// Set logs first (original pattern)
	r.setupResult.Logs = strings.Join(logLines, "\n")
	if r.setupResult.Logs != "" {
		r.setupResult.Logs += "\n"
	}

	setupResultPath := filepath.Join(r.runnerCtx.workingdir, "setup_result.json")
	log.Trace("reading setup_result.json", "path", setupResultPath)

	// Try to read additional setup result data from file
	var setupResultFromFile SetupResult
	if err := r.readJSON(setupResultPath, &setupResultFromFile); err != nil {
		log.Tracew("failed to read setup_result.json, assuming success", "error", err)
		// If setup_result.json doesn't exist, assume setup succeeded and status is ready
		r.setupResult.Status = SetupSucceeded
		r.setupResult.Schema = "" // Will be populated by updateSchema if available
		r.status = StatusReady
		log.Tracew("setup result not found, assuming success", "status", r.status.String())
		return
	}

	log.Tracew("successfully read setup_result.json", "status", setupResultFromFile.Status, "schema_length", len(setupResultFromFile.Schema))

	// Update setup result with data from file, preserving logs that were already set
	r.setupResult.Status = setupResultFromFile.Status
	r.setupResult.Schema = setupResultFromFile.Schema

	switch r.setupResult.Status {
	case SetupSucceeded:
		r.status = StatusReady
		log.Debugw("setup succeeded", "status", r.status.String())
	case SetupFailed:
		r.status = StatusSetupFailed
		log.Debugw("setup failed", "status", r.status.String())
	default:
		r.setupResult.Status = SetupFailed
		r.status = StatusSetupFailed
		log.Debugw("unknown setup status, defaulting to failed", "status", r.status.String())
	}
}

func (r *Runner) rotateLogs() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	allLogs := r.logs.String()
	r.logs = r.logs[:0]
	return allLogs
}

func (r *Runner) readJSON(filePath string, target any) error {
	data, err := os.ReadFile(filePath) //nolint:gosec // expected dynamic path
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func defaultKillFunc(pid int) error {
	return syscall.Kill(-pid, syscall.SIGTERM)
}

func verifyProcessGroupTerminated(pid int) error {
	err := syscall.Kill(-pid, 0)
	if err != nil {
		if err == syscall.ESRCH {
			return nil
		}
		return fmt.Errorf("unexpected error checking process group: %w", err)
	}
	return fmt.Errorf("process group still exists")
}

// NewRunner creates a new runner instance with the given context
func NewRunner(ctx context.Context, ctxCancel context.CancelFunc, runnerCtx RunnerContext, command *exec.Cmd, maxConcurrency int, cfg config.Config, logger *logging.Logger) (*Runner, error) {
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}
	runnerLogger := logger.Named("runner")
	runnerLogger = runnerLogger.With(zap.String("runner_id", runnerCtx.id))

	r := &Runner{
		ctx:                ctx,
		cancel:             ctxCancel,
		runnerCtx:          runnerCtx,
		cmd:                command,
		status:             StatusStarting,
		maxConcurrency:     maxConcurrency,
		pending:            make(map[string]*PendingPrediction),
		killFn:             defaultKillFunc,
		verifyFn:           verifyProcessGroupTerminated,
		cleanupSlot:        make(chan struct{}, 1),
		stopped:            make(chan bool),
		readyForShutdown:   make(chan struct{}),
		setupComplete:      make(chan struct{}),
		logCaptureComplete: make(chan struct{}),
		cleanupTimeout:     cfg.CleanupTimeout,
		forceShutdown:      cfg.ForceShutdown,
		logger:             runnerLogger,
	}

	r.cleanupSlot <- struct{}{}
	return r, nil
}

// mergeEnv merges environment variables according to the configuration
func mergeEnv(env []string, envSet map[string]string, envUnset []string) []string {
	environment := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			environment[parts[0]] = parts[1]
		}
	}
	for k, v := range envSet {
		environment[k] = v
	}
	for _, k := range envUnset {
		delete(environment, k)
	}
	finalEnv := make([]string, 0, len(environment))
	for k, v := range environment {
		finalEnv = append(finalEnv, fmt.Sprintf("%s=%s", k, v))
	}
	return finalEnv
}
