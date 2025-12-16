package tests

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/coglet/internal/config"
	"github.com/replicate/cog/coglet/internal/loggingtest"
	"github.com/replicate/cog/coglet/internal/runner"
	"github.com/replicate/cog/coglet/internal/server"
	"github.com/replicate/cog/coglet/internal/service"
)

// This file implements the basis for the test harness. It is used to test the
// runtime server.

// httpTestServerWrapper wraps httptest.Server to implement service.HTTPServer interface
type httpTestServerWrapper struct {
	*httptest.Server
	closeCh   chan struct{}
	closeOnce sync.Once
}

func newHTTPTestServerWrapper(srv *httptest.Server) *httpTestServerWrapper {
	return &httpTestServerWrapper{
		Server:  srv,
		closeCh: make(chan struct{}),
	}
}

func (w *httpTestServerWrapper) ListenAndServe() error {
	// Block until Close() is called
	<-w.closeCh
	return nil
}

func (w *httpTestServerWrapper) Close() error {
	// Signal ListenAndServe to return (safe close)
	w.closeOnce.Do(func() {
		close(w.closeCh)
	})
	// Also close the httptest server
	w.Server.Close()
	return nil
}

const (
	procedureFilePathURITemplate = "file://%s/python/tests/procedures/%s"
)

// Test-Suite Wide variables.
var (
	basePath       string
	legacyCog      = new(bool)
	proceduresPath string

	portMatchRegex = regexp.MustCompile(`http://[^:]+:(\d+)`)

	// Process tracking for cleanup
	testProcesses   = make(map[int]*os.Process)
	testProcessesMu sync.Mutex
)

// testHarnessResponse is a wrapper around the PredictionResponse that adds the Logs field
// as a string for easier testing. this more directly mirrors what a downstream consumer would
// see rather than the LogSlice type.
type testHarnessResponse struct {
	runner.PredictionResponse

	Logs string `json:"logs"`
}
type webhookData struct {
	Method   string
	Path     string
	Response testHarnessResponse
}

type uploadData struct {
	Method      string
	Path        string
	ContentType string
	Body        []byte
}

type testHarnessReceiver struct {
	*httptest.Server

	mu              sync.Mutex
	webhookRequests []webhookData
	uploadRequests  []uploadData

	webhookReceiverChan chan webhookData
	uploadReceiverChan  chan uploadData
}

func (tr *testHarnessReceiver) webhookHandler(t *testing.T) http.HandlerFunc { //nolint:thelper // this wont be called directly via test, it is called as a webhook receiver
	return func(w http.ResponseWriter, r *http.Request) {
		tr.mu.Lock()
		defer tr.mu.Unlock()
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Equal(t, http.MethodPost, r.Method)
		var resp testHarnessResponse
		err = json.Unmarshal(body, &resp)
		assert.NoError(t, err)
		message := webhookData{
			Method:   r.Method,
			Path:     r.URL.Path,
			Response: resp,
		}
		tr.webhookRequests = append(tr.webhookRequests, message)
		tr.webhookReceiverChan <- message
	}
}

func (tr *testHarnessReceiver) uploadHandler(t *testing.T) http.HandlerFunc { //nolint:thelper // this wont be called directly via test, it is called as a upload receiver
	return func(w http.ResponseWriter, r *http.Request) {
		tr.mu.Lock()
		defer tr.mu.Unlock()
		body, err := io.ReadAll(r.Body)
		// NOTE: Assertions here are to catch cases where the uploader does the wrong thing,
		// as we may or may not be able to catch an error in the upload from functional-style
		// testing, this can start going away once we migrate (where appropriate) to using the
		// unit-tests.
		assert.NoError(t, err)
		assert.True(t, slices.Contains([]string{http.MethodPut, http.MethodPost}, r.Method))
		message := uploadData{
			Method:      r.Method,
			Path:        r.URL.Path,
			ContentType: r.Header.Get("Content-Type"),
			Body:        body,
		}
		tr.uploadRequests = append(tr.uploadRequests, message)
		tr.uploadReceiverChan <- message
	}
}

func testHarnessReceiverServer(t *testing.T) *testHarnessReceiver {
	t.Helper()
	tr := &testHarnessReceiver{}
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", tr.webhookHandler(t))
	mux.HandleFunc("/upload/{filename}", tr.uploadHandler(t))
	// NOTE: buffered channels are used here to prevent issues arising from the handler
	// blocking while holding the lock. ~10 should be enough for the synthetic/small number
	// of requests in testing. Increase if needed. This allows the test to determine if it
	// wants to read from the channel or introspect the slice.
	tr.webhookReceiverChan = make(chan webhookData, 10)
	tr.uploadReceiverChan = make(chan uploadData, 10)
	tr.Server = httptest.NewServer(mux)
	t.Cleanup(tr.Close) // this is the same as tr.Server.Close()
	return tr
}

type cogRuntimeServerConfig struct {
	procedureMode             bool
	explicitShutdown          bool
	uploadURL                 string
	module                    string
	predictorClass            string
	concurrencyMax            int
	maxRunners                int
	runnerShutdownGracePeriod time.Duration

	envSet   map[string]string
	envUnset []string
}

func (cfg *cogRuntimeServerConfig) validate(t *testing.T) {
	t.Helper()
	if !cfg.procedureMode {
		assert.NotEmpty(t, cfg.module)
		assert.NotEmpty(t, cfg.predictorClass)
	}
}

// setupCogRuntime is a convenience function that returns the server without the handler
func setupCogRuntime(t *testing.T, cfg cogRuntimeServerConfig) *httptest.Server {
	t.Helper()
	s, _, _ := setupCogRuntimeServer(t, cfg)
	return s
}

// NewTestLogger returns a zap logger that writes JSON to `t.Logf`
// and uses your preferred field names/order.
// func NewTestLogger(t *testing.T, name string) *zap.Logger {
// 	t.Helper()

// 	encCfg := zapcore.EncoderConfig{
// 		// keys in the order you want them to appear
// 		LevelKey:      "severity",
// 		TimeKey:       "timestamp",
// 		NameKey:       "logger",
// 		CallerKey:     "caller",
// 		MessageKey:    "message",
// 		StacktraceKey: "stacktrace",

// 		LineEnding:     zapcore.DefaultLineEnding,
// 		EncodeLevel:    zapcore.LowercaseLevelEncoder, // "info","error",...
// 		EncodeCaller:   zapcore.ShortCallerEncoder,    // "file.go:123"
// 		EncodeDuration: zapcore.StringDurationEncoder,

// 		// zulu time
// 		EncodeTime: zapcore.TimeEncoderOfLayout("2006-01-02T15:04:05.000Z07:00"),
// 	}

// 	w := zapcore.AddSync(zaptest.NewTestingWriter(t))
// 	core := zapcore.NewCore(zapcore.NewJSONEncoder(encCfg), w, zapcore.DebugLevel)

// 	logger := zap.New(core,
// 		zap.AddCaller(),
// 		zap.AddStacktrace(zapcore.ErrorLevel),
// 	).Named(name)

// 	return logger
// }

func setupCogRuntimeServer(t *testing.T, cfg cogRuntimeServerConfig) (*httptest.Server, *server.Handler, *service.Service) {
	t.Helper()
	cfg.validate(t)
	tempDir := t.TempDir()
	if cfg.procedureMode {
		t.Logf("procedure mode")
	}
	t.Logf("Working directory: %s", tempDir)

	// Use uv to manage Python environments automatically.
	// basePath points to coglet/, repoRoot points to the main cog repo.
	repoRoot := path.Dir(basePath)

	// Build PythonCommand for coglet tests using uv run
	// Use --project instead of --directory to avoid changing the working directory
	// (the coglet runner needs to run in the tempDir where predict.py is located)
	pythonCommand := []string{"uv", "run", "--project", basePath, "--no-dev", "--extra", "test", "python3"}
	t.Logf("using uv with project: %s", basePath)

	// NOTE(morgan): this is a special case, we need the IPCUrl which is homed on the server before we create the handler. Create a nil
	// handler server and then set the handler after.
	s := httptest.NewServer(nil)
	t.Cleanup(s.Close)

	envSet := make(map[string]string)
	for k, v := range cfg.envSet {
		envSet[k] = v
	}

	serverCfg := config.Config{
		UseProcedureMode:          cfg.procedureMode,
		AwaitExplicitShutdown:     cfg.explicitShutdown,
		UploadURL:                 cfg.uploadURL,
		WorkingDirectory:          tempDir,
		IPCUrl:                    s.URL + "/_ipc",
		EnvSet:                    envSet,
		EnvUnset:                  cfg.envUnset,
		PythonCommand:             pythonCommand,
		MaxRunners:                cfg.maxRunners,
		RunnerShutdownGracePeriod: cfg.runnerShutdownGracePeriod, // Use configured grace period or 0 for immediate cleanup
	}
	concurrencyMax := max(cfg.concurrencyMax, 1)
	t.Logf("concurrency max: %d", concurrencyMax)

	if cfg.procedureMode {
		if cfg.maxRunners > 0 {
			t.Logf("max runners: %d", cfg.maxRunners)
		} else {
			t.Logf("max runners: %d (default)", runtime.NumCPU()*4)
		}
	}

	if !cfg.procedureMode {
		writeCogConfig(t, tempDir, cfg.predictorClass, concurrencyMax)
		linkPythonModule(t, basePath, tempDir, cfg.module)
	}

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	// FIXME: This is a hack to cover shutdown logic that is expected. This
	// is more compatbility for the migration away from `cog_test`
	go func() {
		<-ctx.Done()
		s.Close()
	}()

	// NOTE(morgan): We now have the IPCUrl, so we can create the handler.
	// FIXME: This should be done over unix sockets instead of HTTP, it resolves
	// the chicken and egg problem of needing the IPCUrl to create the handler.
	if *legacyCog {
		// Setup the legacy cog server wrapped in a http.ReverseProxy
		// this is just python cog running, this also means that the returned
		// handler is nil since it doesn't really exist as the "handler" object
		// we wire into the serveMux, this means procedure mode doesn't work under
		// legacy cog.
		if cfg.procedureMode {
			t.Fatalf("procedure mode is not supported under legacy cog")
		}
		// Use uv run from repo root for legacy cog (the original Python cog package)
		// Use --project instead of --directory to avoid changing the working directory
		legacyPythonCmd := []string{"uv", "run", "--project", repoRoot, "python3"}
		port, err := startLegacyCogServer(t, ctx, legacyPythonCmd, tempDir, cfg.uploadURL)
		require.NoError(t, err)
		target, _ := url.Parse(fmt.Sprintf("http://localhost:%d", port))
		handler := httputil.NewSingleHostReverseProxy(target)

		s.Config.Handler = handler
		return s, nil, nil
	}

	logger := loggingtest.NewTestLogger(t).Named("harness-test")

	// Create handler with service shutdown function instead of test context cancel
	handler, err := server.NewHandler(t.Context(), serverCfg, logger)
	require.NoError(t, err)

	// Start the handler so runner manager initializes
	err = handler.Start(ctx)
	require.NoError(t, err)

	// Create the server mux and set the handler on the service
	mux := server.NewServeMux(handler, serverCfg.UseProcedureMode)
	s.Config.Handler = mux

	httpWrapper := newHTTPTestServerWrapper(s)
	svc := service.New(
		serverCfg,
		logger,
		service.HTTPServerOption{HTTPServer: httpWrapper},
		service.HandlerOption{Handler: handler},
	)

	// Initialize service (will skip handler and HTTP server creation since we set them)
	err = svc.Initialize(ctx)
	require.NoError(t, err)

	// Register the shutdown endpoint on the mux since service is *not* initializing the HTTP server
	// we should probably move this to the service but that can defer to another PR.
	mux.HandleFunc("POST /shutdown", svc.HandleShutdown)

	go func() {
		_ = svc.Run(t.Context())
	}()

	// Wait for service to start
	require.Eventually(t, func() bool {
		return svc.IsStarted()
	}, 10*time.Second, 100*time.Millisecond, "service should start")

	// Ensure cleanup of runners on test completion
	t.Cleanup(func() {
		if handler != nil {
			handler.ForceKillAll()
		}
	})

	return s, handler, svc
}

func startLegacyCogServer(t *testing.T, ctx context.Context, pythonCmd []string, tempDir, uploadUrl string) (int, error) { //nolint:revive // always send T first, allow context to follow T
	t.Helper()
	// pythonCmd is e.g. ["uv", "run", "--directory", "/path", "python3"]
	// We append the Python args: -m cog.server.http [--upload-url=...]
	pythonArgs := []string{"-m", "cog.server.http"}
	if uploadUrl != "" {
		pythonArgs = append(pythonArgs, fmt.Sprintf("--upload-url=%s", uploadUrl))
	}

	// Build command: pythonCmd[0] as executable, pythonCmd[1:] + pythonArgs as arguments
	allArgs := append(pythonCmd[1:], pythonArgs...)           //nolint:gocritic // intentional append to new slice
	cmd := exec.CommandContext(ctx, pythonCmd[0], allArgs...) //nolint:gosec // pythonCmd from test config
	cmd.Dir = tempDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(), "PORT=0", "PYTHONUNBUFFERED=1", "COG_LOG_LEVEL=DEBUG")
	stdErrLogs, err := cmd.StderrPipe()
	require.NoError(t, err)
	err = cmd.Start()
	require.NoError(t, err)

	// Track process for cleanup
	if cmd.Process != nil {
		trackTestProcess(cmd.Process)
	}

	t.Cleanup(func() {
		stdErrLogs.Close()
		process := cmd.Process
		if process != nil {
			untrackTestProcess(process)
			killProcessGroup(process.Pid, process)
		}
	})

	// We need to do some lifting here to get the port from the logs
	type portResult struct {
		port int
		err  error
	}
	portChan := make(chan portResult, 1)
	go func() {
		port, err := parseLegacyCogServerLogsForPort(t, stdErrLogs)
		if err != nil {
			portChan <- portResult{port: -1, err: err}
			return
		}
		portChan <- portResult{port: port, err: nil}
		// discard the rest of the logs
		io.Copy(io.Discard, stdErrLogs)
	}()

	var port int
	select {
	case result := <-portChan:
		require.NoError(t, result.err, "failed to parse port from legacy cog server logs")
		port = result.port
	case <-time.After(10 * time.Second):
		t.Fatalf("timeout scanning port from legacy cog server logs")
	}
	return port, nil
}

func parseLegacyCogServerLogsForPort(t *testing.T, logs io.ReadCloser) (int, error) {
	t.Helper()
	scanner := bufio.NewScanner(logs)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "Uvicorn running on") {
			matches := portMatchRegex.FindStringSubmatch(line)
			if len(matches) > 0 {
				port, err := strconv.Atoi(matches[1])
				if err != nil {
					return 0, err
				}
				t.Logf("cog server running on port: %d", port)
				return port, nil
			}
		}
	}
	t.Fatalf("could not find port in logs")
	return 0, fmt.Errorf("could not find port in logs")
}

type cogConfig struct {
	Predict     string `json:"predict"`
	Concurrency struct {
		Max int `json:"max"`
	} `json:"concurrency,omitempty"`
}

// writeCogConfig creates a cog.yaml file that contains json-ified version of the config.
// As JSON is a strict subset of YAML, this allows us to stdlib instead of needing external
// yaml-specific dependencies for a very basic cog.yaml
func writeCogConfig(t *testing.T, tempDir, predictorClass string, concurrencyMax int) {
	t.Helper()
	conf := cogConfig{
		Predict: "predict.py:" + predictorClass,
	}
	if concurrencyMax > 0 {
		conf.Concurrency = struct {
			Max int `json:"max"`
		}{Max: concurrencyMax}
	}
	cogConfigFilePath := path.Join(tempDir, "cog.yaml")

	// Debug logging

	cogConfigFile, err := os.OpenFile(cogConfigFilePath, os.O_CREATE|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	defer cogConfigFile.Close()

	err = json.NewEncoder(cogConfigFile).Encode(conf)
	require.NoError(t, err)
}

// linkPythonModule links the python module into the temp directory.
// FIXME: this is a hack to provide compatibility with the `cog_test` test harness while we migrate to in-process testing.
func linkPythonModule(t *testing.T, basePath, tempDir, module string) {
	t.Helper()

	// Try runners directory first (for backward compatibility)
	runnersPath := path.Join(basePath, "python", "tests", "runners")
	srcPath := path.Join(runnersPath, fmt.Sprintf("%s.py", module))

	// If not found in runners, try schemas directory
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		schemasPath := path.Join(basePath, "python", "tests", "schemas")
		srcPath = path.Join(schemasPath, fmt.Sprintf("%s.py", module))
	}

	dstPath := path.Join(tempDir, "predict.py")

	// Debug logging
	t.Logf("Linking Python module: %s -> %s\n", srcPath, dstPath)
	err := os.Symlink(srcPath, dstPath)
	require.NoError(t, err)
}

func healthCheck(t *testing.T, testServer *httptest.Server) server.HealthCheck {
	t.Helper()
	hcURL := testServer.URL + "/health-check"
	resp, err := http.Get(hcURL) //nolint:gosec // URL from test server
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var hc server.HealthCheck
	err = json.Unmarshal(body, &hc)
	require.NoError(t, err)
	return hc
}

func waitForSetupComplete(t *testing.T, testServer *httptest.Server, expectedStatus runner.Status, expectedSetupStatus runner.SetupStatus) server.HealthCheck {
	t.Helper()

	timer := time.NewTicker(10 * time.Millisecond)
	defer timer.Stop()

	for range timer.C {
		hc := healthCheck(t, testServer)
		if hc.Status != runner.StatusStarting.String() {
			assert.Equal(t, expectedStatus.String(), hc.Status)
			assert.Equal(t, expectedSetupStatus, hc.Setup.Status)
			return hc
		}
	}
	return server.HealthCheck{}
}

func waitForReady(t *testing.T, testServer *httptest.Server) server.HealthCheck {
	t.Helper()
	timer := time.NewTicker(10 * time.Millisecond)
	defer timer.Stop()
	for range timer.C {
		hc := healthCheck(t, testServer)
		if hc.Status == runner.StatusReady.String() {
			return hc
		}
	}
	return server.HealthCheck{}
}

func httpPredictionRequest(t *testing.T, runtimeServer *httptest.Server, prediction runner.PredictionRequest) *http.Request {
	t.Helper()
	assert.Empty(t, prediction.ID)
	return httpPredictionReq(t, http.MethodPost, runtimeServer, prediction)
}

func httpPredictionRequestWithID(t *testing.T, runtimeServer *httptest.Server, prediction runner.PredictionRequest) *http.Request {
	t.Helper()
	assert.NotEmpty(t, prediction.ID)
	return httpPredictionReq(t, http.MethodPost, runtimeServer, prediction)
}

func httpPredictionReq(t *testing.T, method string, runtimeServer *httptest.Server, prediction runner.PredictionRequest) *http.Request {
	t.Helper()
	if prediction.CreatedAt != "" {
		t.Logf("using existing created_at: %s", prediction.CreatedAt)
		// verify that created_at is a valid time
		_, err := time.Parse(config.TimeFormat, prediction.CreatedAt)
		require.NoError(t, err)
	}
	prediction.CreatedAt = time.Now().Format(config.TimeFormat)

	serverURL := runtimeServer.URL + "/predictions"
	body, err := json.Marshal(prediction)
	require.NoError(t, err)
	req, err := http.NewRequest(method, serverURL, bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if prediction.Webhook != "" {
		req.Header.Set("Prefer", "respond-async")
	}
	return req
}

func TestMain(m *testing.M) {
	_, b, _, _ := runtime.Caller(0)
	basePath = path.Dir(path.Dir(path.Dir(b)))
	isLegacy, err := strconv.ParseBool(os.Getenv("LEGACY_COG"))
	if err == nil {
		legacyCog = &isLegacy
	}
	proceduresPath = path.Join(basePath, "python", "tests", "procedures")

	// Set up signal handling for cleanup
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		cleanupAllTestProcesses()
		os.Exit(1)
	}()

	code := m.Run()
	cleanupAllTestProcesses()
	os.Exit(code)
}

func trackTestProcess(p *os.Process) {
	if p == nil {
		return
	}
	testProcessesMu.Lock()
	defer testProcessesMu.Unlock()
	testProcesses[p.Pid] = p
}

func untrackTestProcess(p *os.Process) {
	if p == nil {
		return
	}
	testProcessesMu.Lock()
	defer testProcessesMu.Unlock()
	delete(testProcesses, p.Pid)
}

func cleanupAllTestProcesses() {
	// Kill any remaining child processes (coglets, etc.)
	killAllChildProcesses()

	// Also clean up tracked processes (legacy cog servers)
	testProcessesMu.Lock()
	defer testProcessesMu.Unlock()

	for pid, p := range testProcesses {
		// First check if process is still alive before attempting to kill
		if err := p.Signal(syscall.Signal(0)); err != nil {
			// Process already exited, just remove from tracking
			delete(testProcesses, pid)
			continue
		}
		killProcessGroup(pid, p)
		delete(testProcesses, pid)
	}
}

func killAllChildProcesses() {
	cogletPids := findCogletProcesses()
	ourPid := os.Getpid()
	ourPpid := os.Getppid()
	ourPgid, _ := syscall.Getpgid(ourPid)

	for _, pid := range cogletPids {
		// Never kill ourselves, our parent, or PID 1
		if pid == ourPid || pid == ourPpid || pid == 1 {
			continue
		}

		// Check if killing this process group would kill us.
		// This is important because pgrep -f "coglet" matches our test binary path,
		// and syscall.Kill(-pid, SIGKILL) kills the entire process group.
		pidPgid, err := syscall.Getpgid(pid)
		if err == nil && pidPgid == ourPgid {
			continue
		}

		// Create process handle to validate we can signal it
		if process, err := os.FindProcess(pid); err == nil {
			// Try to send signal 0 to check if we can signal this process
			if err := process.Signal(syscall.Signal(0)); err == nil {
				// Kill the process group first, then individual process
				syscall.Kill(-pid, syscall.SIGKILL)
				syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	}
}

func findCogletProcesses() []int {
	// Try pgrep first (more efficient)
	if cogletPids := findCogletWithPgrep(); len(cogletPids) > 0 {
		return cogletPids
	}

	// Fallback to ps command
	return findCogletWithPs()
}

func findCogletWithPgrep() []int {
	var pids []int
	cmd := exec.Command("pgrep", "-f", "coglet")
	output, err := cmd.Output()
	if err != nil {
		return pids
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		if pid, err := strconv.Atoi(line); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

func findCogletWithPs() []int {
	var pids []int
	var cmd *exec.Cmd

	if runtime.GOOS == "darwin" {
		cmd = exec.Command("ps", "-ax", "-o", "pid,command")
	} else {
		cmd = exec.Command("ps", "-e", "-o", "pid,cmd")
	}

	output, err := cmd.Output()
	if err != nil {
		return pids
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines[1:] { // Skip header
		if !strings.Contains(line, "coglet") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}

		if pid, err := strconv.Atoi(fields[0]); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

func killProcessGroup(pid int, p *os.Process) {
	// Verify this is actually our process before killing
	if !isOurProcess(p) {
		return
	}

	// Kill process group first - this should handle most child processes
	syscall.Kill(-pid, syscall.SIGKILL)

	if runtime.GOOS == "darwin" {
		// macOS: additional cleanup steps since signals don't always propagate cleanly
		syscall.Kill(-pid, syscall.SIGTERM) // Fallback
		time.Sleep(100 * time.Millisecond)  // Give processes time to exit
		syscall.Kill(-pid, syscall.SIGKILL) // Final attempt
	}

	// Individual process kill as final fallback for both platforms
	p.Kill()
}

func isOurProcess(p *os.Process) bool {
	if p == nil {
		return false
	}

	// Get our own PID to avoid killing ourselves or our parent
	ourPid := os.Getpid()
	parentPid := os.Getppid()

	// Never kill ourselves, our parent, or PID 1
	if p.Pid == ourPid || p.Pid == parentPid || p.Pid == 1 {
		return false
	}

	// Send signal 0 to check if process exists and we can signal it
	err := p.Signal(syscall.Signal(0))
	if err != nil {
		// Process is gone or we can't signal it
		return false
	}

	return true
}

// safeCloseChannel is a helper function to close a channel only if it is not already closed.
// it assumes that a single goroutine owns closing the channel and should only be used in
// the test harness in that scenario.
func safeCloseChannel(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

// ValidateTerminalResponse validates that a terminal response has all required fields
func ValidateTerminalResponse(t *testing.T, response *testHarnessResponse) {
	t.Helper()

	require.NotNil(t, response, "response cannot be nil")

	// Validate terminal status
	terminalStatuses := []runner.PredictionStatus{runner.PredictionSucceeded, runner.PredictionFailed, runner.PredictionCanceled}
	require.Contains(t, terminalStatuses, response.Status, "response status %q is not terminal", response.Status)

	// Validate required fields
	if !*legacyCog {
		require.NotEmpty(t, response.ID, "response.ID is required but empty")
	}
	require.NotEmpty(t, response.CreatedAt, "response.CreatedAt is required but empty")
	require.NotEmpty(t, response.StartedAt, "response.StartedAt is required but empty")
	require.NotEmpty(t, response.CompletedAt, "response.CompletedAt is required but empty")

	// Validate time formats
	_, err := time.Parse(config.TimeFormat, response.CreatedAt)
	require.NoError(t, err, "response.CreatedAt has invalid format")
	_, err = time.Parse(config.TimeFormat, response.StartedAt)
	require.NoError(t, err, "response.StartedAt has invalid format")
	_, err = time.Parse(config.TimeFormat, response.CompletedAt)
	require.NoError(t, err, "response.CompletedAt has invalid format")

	// Validate metrics
	require.NotNil(t, response.Metrics, "response.Metrics is required but nil")
	predictTime, exists := response.Metrics["predict_time"]
	require.True(t, exists, "response.Metrics must contain 'predict_time' field")
	predictTimeFloat, ok := predictTime.(float64)
	require.True(t, ok, "response.Metrics['predict_time'] must be a float64, got %T", predictTime)
	require.GreaterOrEqual(t, predictTimeFloat, 0.0, "response.Metrics['predict_time'] must be non-negative")
}
