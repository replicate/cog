package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/coglet/internal/config"
	"github.com/replicate/cog/coglet/internal/loggingtest"
)

func TestRunnerCapacity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		maxConcurrency int
		pendingCount   int
		want           bool
	}{
		{
			name:           "has capacity",
			maxConcurrency: 5,
			pendingCount:   3,
			want:           true,
		},
		{
			name:           "at capacity",
			maxConcurrency: 5,
			pendingCount:   5,
			want:           false,
		},
		{
			name:           "over capacity",
			maxConcurrency: 5,
			pendingCount:   7,
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &Runner{
				maxConcurrency: tt.maxConcurrency,
				pending:        make(map[string]*PendingPrediction),
				logger:         loggingtest.NewTestLogger(t),
			}

			// Add pending predictions
			for i := 0; i < tt.pendingCount; i++ {
				r.pending[string(rune('a'+i))] = &PendingPrediction{}
			}

			got := r.hasCapacity()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRunnerIdle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		pendingCount int
		want         bool
	}{
		{
			name:         "idle with no pending",
			pendingCount: 0,
			want:         true,
		},
		{
			name:         "busy with pending",
			pendingCount: 2,
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &Runner{
				pending: make(map[string]*PendingPrediction),
				logger:  loggingtest.NewTestLogger(t),
			}

			for i := 0; i < tt.pendingCount; i++ {
				r.pending[string(rune('a'+i))] = &PendingPrediction{}
			}

			got := r.Idle()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRunnerStart(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		r := &Runner{
			status: StatusStarting,
			cmd: &exec.Cmd{
				Path: "/bin/sleep",
				Args: []string{"sleep", "0.1"},
				Dir:  tempDir,
			},
			logger:             loggingtest.NewTestLogger(t),
			logCaptureComplete: make(chan struct{}),
		}

		ctx := context.Background()
		err := r.Start(ctx)
		require.NoError(t, err)

		t.Cleanup(func() {
			if cmd, err := r.getCmd(); err == nil && cmd.Process != nil {
				cmd.Process.Kill()
			}
		})
	})

	t.Run("wrong status", func(t *testing.T) {
		t.Parallel()

		r := &Runner{
			status:             StatusReady,
			logger:             loggingtest.NewTestLogger(t),
			logCaptureComplete: make(chan struct{}),
		}

		ctx := context.Background()
		err := r.Start(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "runner not in starting state")
	})

	t.Run("command fails", func(t *testing.T) {
		t.Parallel()

		r := &Runner{
			status: StatusStarting,
			cmd: &exec.Cmd{
				Path: "/nonexistent/command",
				Args: []string{"nonexistent"},
			},
			logger:             loggingtest.NewTestLogger(t),
			logCaptureComplete: make(chan struct{}),
		}

		ctx := context.Background()
		err := r.Start(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to start subprocess")
	})

	t.Run("no command", func(t *testing.T) {
		t.Parallel()

		r := &Runner{
			status:             StatusStarting,
			cmd:                nil,
			logger:             loggingtest.NewTestLogger(t),
			logCaptureComplete: make(chan struct{}),
		}

		ctx := context.Background()
		err := r.Start(ctx)
		require.ErrorIs(t, err, ErrNoCommand)
	})
}

func TestRunnerConfig(t *testing.T) {
	// This test can't be parallel because it modifies env vars
	originalWaitFile := os.Getenv("COG_WAIT_FILE")
	defer func() {
		if originalWaitFile == "" {
			os.Unsetenv("COG_WAIT_FILE")
		} else {
			os.Setenv("COG_WAIT_FILE", originalWaitFile)
		}
	}()

	t.Run("no wait file", func(t *testing.T) {
		os.Unsetenv("COG_WAIT_FILE")

		tempDir := t.TempDir()
		cogYaml := `{"build": {"python_version": "3.8"}, "predict": "predict.py:Predictor"}`
		err := os.WriteFile(path.Join(tempDir, "cog.yaml"), []byte(cogYaml), 0o644)
		require.NoError(t, err)

		r := &Runner{
			status: StatusStarting,
			runnerCtx: RunnerContext{
				workingdir: tempDir,
			},
			mu:                 sync.RWMutex{},
			logger:             loggingtest.NewTestLogger(t),
			logCaptureComplete: make(chan struct{}),
		}

		ctx := context.Background()
		err = r.Config(ctx)
		require.NoError(t, err)
	})

	t.Run("with wait file", func(t *testing.T) {
		tempDir := t.TempDir()
		waitFile := path.Join(tempDir, "wait.txt")
		os.Setenv("COG_WAIT_FILE", waitFile)

		cogYaml := `{"build": {"python_version": "3.8"}, "predict": "predict.py:Predictor"}`
		err := os.WriteFile(path.Join(tempDir, "cog.yaml"), []byte(cogYaml), 0o644)
		require.NoError(t, err)

		r := &Runner{
			status: StatusStarting,
			runnerCtx: RunnerContext{
				workingdir: tempDir,
			},
			mu:                 sync.RWMutex{},
			logger:             loggingtest.NewTestLogger(t),
			logCaptureComplete: make(chan struct{}),
		}

		// Create wait file in a goroutine after a short delay
		go func() {
			time.Sleep(10 * time.Millisecond)
			os.WriteFile(waitFile, []byte("ready"), 0o644)
		}()

		ctx := context.Background()
		err = r.Config(ctx)
		require.NoError(t, err)
	})

	t.Run("wrong status", func(t *testing.T) {
		os.Unsetenv("COG_WAIT_FILE")

		tempDir := t.TempDir()
		cogYaml := `{"build": {"python_version": "3.8"}, "predict": "predict.py:Predictor"}`
		err := os.WriteFile(path.Join(tempDir, "cog.yaml"), []byte(cogYaml), 0o644)
		require.NoError(t, err)

		r := &Runner{
			status: StatusReady,
			runnerCtx: RunnerContext{
				workingdir: tempDir,
			},
			mu:                 sync.RWMutex{},
			logger:             loggingtest.NewTestLogger(t),
			logCaptureComplete: make(chan struct{}),
		}

		ctx := context.Background()
		err = r.Config(ctx)
		require.NoError(t, err)
	})

	t.Run("context canceled", func(t *testing.T) {
		tempDir := t.TempDir()
		waitFile := path.Join(tempDir, "nonexistent.txt")
		os.Setenv("COG_WAIT_FILE", waitFile)

		r := &Runner{
			status:             StatusStarting,
			logger:             loggingtest.NewTestLogger(t),
			logCaptureComplete: make(chan struct{}),
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()

		err := r.Config(ctx)
		require.Error(t, err)
		assert.Equal(t, context.DeadlineExceeded, err)
	})
}

func TestRunnerStop(t *testing.T) {
	t.Parallel()

	t.Run("stop with pending predictions", func(t *testing.T) {
		t.Parallel()

		r := &Runner{
			status:  StatusReady,
			pending: make(map[string]*PendingPrediction),
			killFn:  func(pid int) error { return nil },
			stopped: make(chan bool),
			logger:  loggingtest.NewTestLogger(t),
		}

		// Add pending predictions
		r.pending["pred1"] = &PendingPrediction{
			c: make(chan PredictionResponse, 1),
		}
		r.pending["pred2"] = &PendingPrediction{
			c: make(chan PredictionResponse, 1),
		}

		err := r.Stop()
		require.NoError(t, err)
		assert.Equal(t, StatusDefunct, r.status)
		assert.Empty(t, r.pending)

		// Check that stopped channel is closed
		select {
		case <-r.stopped:
		default:
			t.Fatal("stopped channel should be closed")
		}
	})

	t.Run("stop already defunct", func(t *testing.T) {
		t.Parallel()

		r := &Runner{
			status:             StatusDefunct,
			logger:             loggingtest.NewTestLogger(t),
			logCaptureComplete: make(chan struct{}),
		}

		err := r.Stop()
		require.NoError(t, err)
	})

	t.Run("stop with running process", func(t *testing.T) {
		t.Parallel()

		killCalled := false
		cmd := exec.Command("echo", "test")
		r := &Runner{
			cmd:     cmd,
			status:  StatusReady,
			pending: make(map[string]*PendingPrediction),
			killFn: func(pid int) error {
				killCalled = true
				return nil
			},
			stopped: make(chan bool),
			logger:  loggingtest.NewTestLogger(t),
		}

		// Mock a running process
		r.cmd.Process = &os.Process{Pid: 12345}

		err := r.Stop()
		require.NoError(t, err)
		assert.True(t, killCalled)
		assert.Equal(t, StatusDefunct, r.status)
	})
}

func TestRunnerForceKill(t *testing.T) {
	t.Parallel()

	t.Run("kill running process", func(t *testing.T) {
		t.Parallel()

		killCalled := false
		cmd := exec.Command("echo", "test")
		r := &Runner{
			cmd: cmd,
			killFn: func(pid int) error {
				killCalled = true
				return nil
			},
			logger: loggingtest.NewTestLogger(t),
		}

		// Mock a running process
		r.cmd.Process = &os.Process{Pid: 12345}

		r.ForceKill()
		assert.True(t, killCalled)
		assert.True(t, r.killed)
	})

	t.Run("already killed", func(t *testing.T) {
		t.Parallel()

		killCallCount := 0
		cmd := exec.Command("echo", "test")
		r := &Runner{
			cmd:    cmd,
			killed: true,
			killFn: func(pid int) error {
				killCallCount++
				return nil
			},
			logger: loggingtest.NewTestLogger(t),
		}

		r.cmd.Process = &os.Process{Pid: 12345}
		r.ForceKill()
		assert.Equal(t, 0, killCallCount)
	})

	t.Run("no process", func(t *testing.T) {
		t.Parallel()

		killCallCount := 0
		r := &Runner{
			killFn: func(pid int) error {
				killCallCount++
				return nil
			},
			logger: loggingtest.NewTestLogger(t),
		}

		r.ForceKill()
		assert.Equal(t, 0, killCallCount)
		assert.False(t, r.killed)
	})

	t.Run("idempotent calls", func(t *testing.T) {
		t.Parallel()

		killCallCount := 0
		r := &Runner{
			killFn: func(pid int) error {
				killCallCount++
				return nil
			},
			cleanupSlot: make(chan struct{}, 1),
			logger:      loggingtest.NewTestLogger(t),
		}
		r.cleanupSlot <- struct{}{} // Initialize with token
		r.cmd = exec.Command("echo", "test")
		r.cmd.Process = &os.Process{Pid: 12345}

		r.ForceKill()
		r.ForceKill()
		r.ForceKill()

		assert.Equal(t, 1, killCallCount, "Should only kill once despite multiple calls")
		assert.True(t, r.killed, "Should mark runner as killed")
	})

	t.Run("with already exited process", func(t *testing.T) {
		t.Parallel()

		killCalled := false
		r := &Runner{
			cmd: &exec.Cmd{
				Process:      &os.Process{Pid: 12345},
				ProcessState: &os.ProcessState{}, // Non-nil means exited
			},
			killFn: func(pid int) error {
				killCalled = true
				return nil
			},
			logger: loggingtest.NewTestLogger(t),
		}

		r.ForceKill()

		assert.False(t, killCalled, "Should not kill already exited process")
		assert.False(t, r.killed, "Should not mark as killed")
	})
}

func TestRunnerPredict(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()

		r := &Runner{
			status:    StatusReady,
			pending:   make(map[string]*PendingPrediction),
			runnerCtx: RunnerContext{workingdir: tempDir},
			logger:    loggingtest.NewTestLogger(t),
		}
		predictionID, _ := PredictionID()

		req := PredictionRequest{
			ID:    predictionID,
			Input: map[string]any{"key": "value"},
			// CreatedAt and StartedAt would be set in the manager allocatePrediction step
			// so we need to set them directly here
			CreatedAt: time.Now().Format(config.TimeFormat),
			StartedAt: time.Now().Format(config.TimeFormat),
		}
		// Pre-allocate prediction
		r.pending[predictionID] = &PendingPrediction{
			c:       make(chan PredictionResponse, 1),
			request: req,
		}

		ch, initialResponse, err := r.predict(req.ID)
		require.NoError(t, err)
		require.NotNil(t, initialResponse)
		assert.Equal(t, PredictionStarting, initialResponse.Status)
		assert.NotEmpty(t, initialResponse.ID)
		assert.Equal(t, req.Input, initialResponse.Input)
		assert.NotEmpty(t, initialResponse.CreatedAt)
		assert.NotEmpty(t, initialResponse.StartedAt)
		assert.Equal(t, req.CreatedAt, initialResponse.CreatedAt)
		assert.Equal(t, req.StartedAt, initialResponse.StartedAt)
		assert.NotNil(t, ch)

		// Check request file was created
		requestFile := path.Join(tempDir, fmt.Sprintf("request-%s.json", predictionID))
		_, err = os.Stat(requestFile)
		assert.NoError(t, err)
	})

	t.Run("prediction not allocated", func(t *testing.T) {
		t.Parallel()

		r := &Runner{
			status:  StatusReady,
			pending: make(map[string]*PendingPrediction),
			logger:  loggingtest.NewTestLogger(t),
		}

		predictionID, _ := PredictionID()

		req := PredictionRequest{ID: predictionID}
		ch, initialResponse, err := r.predict(req.ID)
		require.Error(t, err)
		assert.Nil(t, ch)
		assert.Nil(t, initialResponse)
		assert.Contains(t, err.Error(), fmt.Sprintf("prediction %s not allocated", predictionID))
	})
}

func TestRunnerCancel(t *testing.T) {
	t.Parallel()
	t.Run("cancel success", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()

		r := &Runner{
			pending:   make(map[string]*PendingPrediction),
			runnerCtx: RunnerContext{workingdir: tempDir, id: "test-runner", tmpDir: tempDir},
			logger:    loggingtest.NewTestLogger(t),
		}

		r.pending["test-id"] = &PendingPrediction{}

		err := r.Cancel("test-id")
		require.NoError(t, err)

		// Check cancel file was created
		cancelFile := path.Join(tempDir, "cancel-test-id")
		_, err = os.Stat(cancelFile)
		assert.NoError(t, err)
	})

	t.Run("cancelprediction not found", func(t *testing.T) {
		t.Parallel()

		r := &Runner{
			pending: make(map[string]*PendingPrediction),
			logger:  loggingtest.NewTestLogger(t),
		}

		err := r.Cancel("nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prediction not found")
	})
}

func TestRunnerString(t *testing.T) {
	t.Parallel()

	t.Run("String", func(t *testing.T) {
		t.Parallel()

		r := &Runner{
			runnerCtx: RunnerContext{id: "test-runner"},
			status:    StatusReady,
			logger:    loggingtest.NewTestLogger(t),
		}

		got := r.String()
		want := "Runner{name=test-runner, status=READY}"
		assert.Equal(t, want, got)
	})
}

func TestRunnerIPC(t *testing.T) {
	t.Parallel()

	t.Run("READY status", func(t *testing.T) {
		t.Parallel()

		r := &Runner{
			status: StatusStarting,
			logger: loggingtest.NewTestLogger(t),
			runnerCtx: RunnerContext{
				id:         "test-runner",
				workingdir: t.TempDir(),
			},
			setupComplete: make(chan struct{}),
		}

		err := r.HandleIPC("READY")
		require.NoError(t, err)
		assert.Equal(t, StatusReady, r.status)
	})

	t.Run("READY when already ready", func(t *testing.T) {
		t.Parallel()

		r := &Runner{
			status: StatusReady,
			logger: loggingtest.NewTestLogger(t),
		}

		err := r.HandleIPC("READY")
		require.NoError(t, err)
		assert.Equal(t, StatusReady, r.status)
	})

	t.Run("BUSY status", func(t *testing.T) {
		t.Parallel()

		r := &Runner{
			status: StatusReady,
			logger: loggingtest.NewTestLogger(t),
		}

		err := r.HandleIPC("BUSY")
		require.NoError(t, err)
		assert.Equal(t, StatusBusy, r.status)
	})

	t.Run("OUTPUT status", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()

		r := &Runner{
			runnerCtx: RunnerContext{workingdir: tempDir},
			pending:   make(map[string]*PendingPrediction),
			logger:    loggingtest.NewTestLogger(t),
		}

		err := r.HandleIPC("OUTPUT")
		require.NoError(t, err)
	})

	t.Run("unknown status", func(t *testing.T) {
		t.Parallel()

		r := &Runner{
			logger: loggingtest.NewTestLogger(t),
		}

		err := r.HandleIPC("UNKNOWN")
		require.NoError(t, err)
	})
}

func TestKillFunctions(t *testing.T) {
	t.Parallel()

	t.Run("defaultKillFunc", func(t *testing.T) {
		t.Parallel()

		// Test with invalid PID
		err := defaultKillFunc(-1)
		require.Error(t, err)
	})

	t.Run("verifyProcessGroupTerminated", func(t *testing.T) {
		t.Parallel()

		mockVerifyFn := func(pid int) error {
			if pid == 12345 {
				return fmt.Errorf("process group still exists")
			}
			return nil
		}

		err := mockVerifyFn(99999)
		require.NoError(t, err)

		err = mockVerifyFn(12345)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "process group still exists")
	})
}

func TestIsTerminalStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status string
		want   bool
	}{
		{"succeeded", true},
		{"failed", true},
		{"canceled", true},
		{"processing", false},
		{"starting", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()

			got := PredictionStatus(tt.status).IsCompleted()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWaitForStop(t *testing.T) {
	t.Parallel()

	t.Run("channel already closed", func(t *testing.T) {
		t.Parallel()

		r := &Runner{
			stopped: make(chan bool),
			logger:  loggingtest.NewTestLogger(t),
		}
		close(r.stopped)

		done := make(chan struct{})
		go func() {
			r.WaitForStop()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("WaitForStop should have returned immediately")
		}
	})

	t.Run("channel closed later", func(t *testing.T) {
		t.Parallel()

		r := &Runner{
			stopped: make(chan bool),
			logger:  loggingtest.NewTestLogger(t),
		}

		done := make(chan struct{})
		go func() {
			r.WaitForStop()
			close(done)
		}()

		// Close the channel after a delay
		go func() {
			time.Sleep(10 * time.Millisecond)
			close(r.stopped)
		}()

		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("WaitForStop should have returned after channel was closed")
		}
	})
}

func TestNewRunner(t *testing.T) {
	t.Parallel()

	t.Run("creates valid runner", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()

		// Create command for the runner
		cmd := &exec.Cmd{
			Path: "/bin/echo",
			Args: []string{"echo", "test"},
		}

		// Create runner context
		uploader := newUploader("http://localhost:8000/upload")
		runnerCtx := RunnerContext{
			id:         "test-runner",
			workingdir: tempDir,
			uploader:   uploader,
		}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		cfg := config.Config{}
		r, err := NewRunner(ctx, cancel, runnerCtx, cmd, 1, cfg, loggingtest.NewTestLogger(t))
		require.NoError(t, err)

		assert.Equal(t, "test-runner", r.runnerCtx.id)
		assert.Equal(t, StatusStarting, r.status)
		assert.Equal(t, 1, r.maxConcurrency)
		assert.Equal(t, tempDir, r.runnerCtx.workingdir)
		assert.Equal(t, "http://localhost:8000/upload", r.runnerCtx.uploader.uploadURL)
		assert.NotNil(t, r.pending)
		assert.NotNil(t, r.cleanupSlot)
		assert.Len(t, r.cleanupSlot, 1) // Should start with token available
		assert.Equal(t, cmd, r.cmd)

		// Clean up working directory
		t.Cleanup(func() {
			os.RemoveAll(tempDir)
		})
	})

	t.Run("stores command correctly", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()

		// Create command with python3
		cmd := &exec.Cmd{
			Path: "/usr/bin/python3",
			Args: []string{"python3", "-m", "coglet"},
		}

		runnerCtx := RunnerContext{
			id:         "test-runner",
			workingdir: tempDir,
		}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		cfg := config.Config{}
		r, err := NewRunner(ctx, cancel, runnerCtx, cmd, 1, cfg, loggingtest.NewTestLogger(t))
		require.NoError(t, err)

		// Should store the command correctly
		assert.Equal(t, cmd, r.cmd)
		assert.Contains(t, r.cmd.Args, "python3")

		t.Cleanup(func() {
			os.RemoveAll(tempDir)
		})
	})

	t.Run("initializes correctly", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		cmd := &exec.Cmd{Path: "/bin/echo"}
		runnerCtx := RunnerContext{
			id:         "test",
			workingdir: tempDir,
		}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		cfg := config.Config{}
		r, err := NewRunner(ctx, cancel, runnerCtx, cmd, 1, cfg, loggingtest.NewTestLogger(t))
		require.NoError(t, err)
		require.NotNil(t, r)

		assert.Equal(t, StatusStarting, r.status)
		assert.NotNil(t, r.pending)
		assert.NotNil(t, r.cleanupSlot)
		assert.Len(t, r.cleanupSlot, 1)

		t.Cleanup(func() {
			os.RemoveAll(tempDir)
		})
	})
}

func TestProcedureRunnerCreation(t *testing.T) {
	t.Parallel()

	t.Run("creates procedure runner with context and command", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()

		// Create command for procedure runner
		cmd := &exec.Cmd{
			Path: "/usr/bin/python3",
			Args: []string{"python3", "-m", "coglet", "--name", "proc-runner"},
		}

		// Create runner context for procedure runner
		uploader := newUploader("http://localhost:8000/upload")
		runnerCtx := RunnerContext{
			id:         "proc-runner",
			workingdir: tempDir,
			uploader:   uploader,
		}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		cfg := config.Config{}
		r, err := NewRunner(ctx, cancel, runnerCtx, cmd, 1, cfg, loggingtest.NewTestLogger(t))
		require.NoError(t, err)

		assert.Equal(t, "proc-runner", r.runnerCtx.id)
		assert.Equal(t, StatusStarting, r.status)
		assert.Equal(t, tempDir, r.runnerCtx.workingdir)
		assert.Equal(t, "http://localhost:8000/upload", r.runnerCtx.uploader.uploadURL)

		t.Cleanup(func() {
			os.RemoveAll(tempDir)
		})
	})
}

func TestMergeEnv(t *testing.T) {
	t.Parallel()

	t.Run("basic merge", func(t *testing.T) {
		t.Parallel()

		baseEnv := []string{"PATH=/bin", "HOME=/home/user", "LANG=en_US"}
		envSet := map[string]string{"NEW_VAR": "new_value", "PATH": "/usr/bin"}
		envUnset := []string{"LANG"}

		result := mergeEnv(baseEnv, envSet, envUnset)

		// Convert result back to map for easier testing
		resultMap := make(map[string]string)
		for _, env := range result {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				resultMap[parts[0]] = parts[1]
			}
		}

		assert.Equal(t, "/usr/bin", resultMap["PATH"], "Should override existing var")
		assert.Equal(t, "/home/user", resultMap["HOME"], "Should keep existing var")
		assert.Equal(t, "new_value", resultMap["NEW_VAR"], "Should add new var")
		assert.NotContains(t, resultMap, "LANG", "Should remove unset var")
	})

	t.Run("handles malformed env entries", func(t *testing.T) {
		t.Parallel()

		baseEnv := []string{"VALID=value", "INVALID", "ALSO_VALID=another"}
		envSet := map[string]string{}
		envUnset := []string{}

		result := mergeEnv(baseEnv, envSet, envUnset)

		resultMap := make(map[string]string)
		for _, env := range result {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				resultMap[parts[0]] = parts[1]
			}
		}

		assert.Equal(t, "value", resultMap["VALID"])
		assert.Equal(t, "another", resultMap["ALSO_VALID"])
		assert.NotContains(t, resultMap, "INVALID", "Should skip malformed entries")
	})

	t.Run("empty inputs", func(t *testing.T) {
		t.Parallel()

		result := mergeEnv([]string{}, map[string]string{}, []string{})
		assert.Empty(t, result)
	})

	t.Run("only additions", func(t *testing.T) {
		t.Parallel()

		envSet := map[string]string{"NEW1": "val1", "NEW2": "val2"}
		result := mergeEnv([]string{}, envSet, []string{})

		assert.Len(t, result, 2)
		resultMap := make(map[string]string)
		for _, env := range result {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				resultMap[parts[0]] = parts[1]
			}
		}

		assert.Equal(t, "val1", resultMap["NEW1"])
		assert.Equal(t, "val2", resultMap["NEW2"])
	})
}

func TestRunnerTempDirectoryCleanup(t *testing.T) {
	t.Parallel()

	t.Run("cleans up temp directory", func(t *testing.T) {
		t.Parallel()

		workdir := t.TempDir()
		cmd := exec.Command("echo", "test")
		r := &Runner{
			runnerCtx: RunnerContext{workingdir: workdir},
			cmd:       cmd,
			pending:   make(map[string]*PendingPrediction),
			killFn:    func(pid int) error { return nil },
			stopped:   make(chan bool),
			logger:    loggingtest.NewTestLogger(t),
		}

		tmpDir, err := os.MkdirTemp("", "test-cog-runner-tmp-")
		require.NoError(t, err, "Failed to create temp directory")

		// Set tmpDir to test cleanup
		r.runnerCtx.tmpDir = tmpDir

		testFile := path.Join(tmpDir, "test-file.txt")
		err = os.WriteFile(testFile, []byte("test content"), 0o644)
		require.NoError(t, err, "Failed to create test file")

		_, err = os.Stat(tmpDir)
		assert.False(t, os.IsNotExist(err), "Temp directory should exist before cleanup")

		_, err = os.Stat(testFile)
		assert.False(t, os.IsNotExist(err), "Test file should exist before cleanup")

		err = r.Stop()
		require.NoError(t, err, "Runner.Stop() should not error")

		time.Sleep(1 * time.Millisecond)

		_, err = os.Stat(tmpDir)
		assert.True(t, os.IsNotExist(err), "Temp directory should be cleaned up after Stop()")
	})

	t.Run("handles valid working directory", func(t *testing.T) {
		t.Parallel()

		workdir := t.TempDir()
		cmd := exec.Command("echo", "test")
		r := &Runner{
			runnerCtx: RunnerContext{workingdir: workdir},
			cmd:       cmd,
			pending:   make(map[string]*PendingPrediction),
			killFn:    func(pid int) error { return nil },
			stopped:   make(chan bool),
			logger:    loggingtest.NewTestLogger(t),
		}

		err := r.Stop()
		assert.NoError(t, err, "Runner.Stop() should not error with valid working directory")
	})
}

func TestRunnerConfigCreatesConfigJSON(t *testing.T) {
	t.Parallel()

	// Create temporary directory
	tempDir := t.TempDir()

	// Create cog.yaml file
	cogYamlPath := filepath.Join(tempDir, "cog.yaml")
	cogYamlContent := `{"predict": "predict.py:TestPredictor", "concurrency": {"max": 2}}`
	err := os.WriteFile(cogYamlPath, []byte(cogYamlContent), 0o644)
	require.NoError(t, err)

	// Create runner
	runnerCtx := RunnerContext{
		id:         "test-runner",
		workingdir: tempDir,
		tmpDir:     tempDir,
		uploader:   nil,
	}

	// Create a dummy command (won't be executed)
	cmd := exec.Command("echo", "test")
	cmd.Dir = tempDir

	runner := &Runner{
		runnerCtx:      runnerCtx,
		cmd:            cmd,
		status:         StatusStarting,
		maxConcurrency: 1,
		pending:        make(map[string]*PendingPrediction),
		killFn:         func(pid int) error { return nil },
		stopped:        make(chan bool),
		logger:         loggingtest.NewTestLogger(t),
	}

	// Call Config method
	ctx := context.Background()
	err = runner.Config(ctx)
	require.NoError(t, err)

	// Verify config.json was created
	configPath := filepath.Join(tempDir, "config.json")
	assert.FileExists(t, configPath, "config.json should be created")

	// Verify config.json content
	configData, err := os.ReadFile(configPath)
	require.NoError(t, err)

	var cfg map[string]any
	err = json.Unmarshal(configData, &cfg)
	require.NoError(t, err)

	assert.Equal(t, "predict", cfg["module_name"])
	assert.Equal(t, "TestPredictor", cfg["predictor_name"])
	assert.Equal(t, float64(2), cfg["max_concurrency"]) //nolint:testifylint // JSON unmarshals numbers as float64

	// Verify runner max concurrency was updated
	assert.Equal(t, 2, runner.maxConcurrency)
}

// TestPerPredictionWatcher tests the new per-prediction response watcher architecture
func TestPerPredictionWatcher(t *testing.T) {
	t.Parallel()

	t.Run("ProcessResponseFiles", func(t *testing.T) {
		t.Parallel()

		// Setup temp directory with response files
		tempDir := t.TempDir()
		predictionID, _ := PredictionID()

		// Create response files - one for our prediction, one for another
		responseFile1 := fmt.Sprintf("response-%s-00001.json", predictionID)
		responseFile2 := "response-other-prediction-00001.json"
		responseFile3 := "not-a-response-file.json"

		response := PredictionResponse{
			Status: PredictionSucceeded,
			Output: "test output",
		}
		responseData, err := json.Marshal(response)
		require.NoError(t, err)

		err = os.WriteFile(filepath.Join(tempDir, responseFile1), responseData, 0o644)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(tempDir, responseFile2), responseData, 0o644)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(tempDir, responseFile3), responseData, 0o644)
		require.NoError(t, err)

		// Setup runner with mock working directory
		logger := loggingtest.NewTestLogger(t)
		runner := &Runner{
			runnerCtx: RunnerContext{workingdir: tempDir},
			logger:    logger,
		}

		// Setup pending prediction
		pending := &PendingPrediction{
			request:      PredictionRequest{ID: predictionID},
			outputCache:  make(map[string]string),
			c:            make(chan PredictionResponse, 1),
			watcherDone:  make(chan struct{}),
			outputNotify: make(chan struct{}, 1),
		}

		// Test processResponseFiles only processes files for this prediction
		responsePattern := fmt.Sprintf("response-%s-", predictionID)
		err = runner.processResponseFiles(predictionID, pending, responsePattern, logger.Sugar())
		require.NoError(t, err)

		// Verify only our prediction's response file was processed (deleted)
		assert.NoFileExists(t, filepath.Join(tempDir, responseFile1))
		assert.FileExists(t, filepath.Join(tempDir, responseFile2)) // Other prediction's file remains
		assert.FileExists(t, filepath.Join(tempDir, responseFile3)) // Non-response file remains
	})

	t.Run("HandleSingleResponse", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		predictionID, _ := PredictionID()
		filename := fmt.Sprintf("response-%s-00001.json", predictionID)
		filePath := filepath.Join(tempDir, filename)

		// Create response file
		response := PredictionResponse{
			Status: PredictionProcessing,
			Output: "partial output",
		}
		responseData, err := json.Marshal(response)
		require.NoError(t, err)
		err = os.WriteFile(filePath, responseData, 0o644)
		require.NoError(t, err)

		// Setup runner
		logger := loggingtest.NewTestLogger(t)
		runner := &Runner{
			runnerCtx: RunnerContext{workingdir: tempDir},
			logger:    logger,
		}

		// Setup pending prediction
		pending := &PendingPrediction{
			request:      PredictionRequest{ID: predictionID, Input: map[string]any{"test": "input"}},
			response:     PredictionResponse{Logs: []string{"existing log"}},
			outputCache:  make(map[string]string),
			c:            make(chan PredictionResponse, 1),
			watcherDone:  make(chan struct{}),
			outputNotify: make(chan struct{}, 1),
			mu:           sync.Mutex{},
		}

		// Test handleSingleResponse
		err = runner.handleSingleResponse(filename, predictionID, pending, logger.Sugar())
		require.NoError(t, err)

		// Verify response file was deleted
		assert.NoFileExists(t, filePath)

		// Verify pending response was updated
		pending.mu.Lock()
		assert.Equal(t, PredictionProcessing, pending.response.Status)
		assert.Equal(t, "partial output", pending.response.Output)
		assert.Equal(t, predictionID, pending.response.ID)
		assert.Equal(t, map[string]any{"test": "input"}, pending.response.Input)
		assert.Equal(t, LogsSlice{"existing log"}, pending.response.Logs) // Logs preserved
		pending.mu.Unlock()
	})

	t.Run("WatcherExitsOnTerminalResponse", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		predictionID, _ := PredictionID()

		// Setup runner
		logger := loggingtest.NewTestLogger(t)
		runner := &Runner{
			runnerCtx: RunnerContext{workingdir: tempDir},
			logger:    logger,
			mu:        sync.RWMutex{},
		}

		// Setup pending prediction with context cancellation
		ctx, cancel := context.WithCancel(context.Background())
		pending := &PendingPrediction{
			request:      PredictionRequest{ID: predictionID},
			response:     PredictionResponse{},
			outputCache:  make(map[string]string),
			mu:           sync.Mutex{},
			c:            make(chan PredictionResponse, 1),
			outputNotify: make(chan struct{}, 1),
			cancel:       cancel,
			watcherDone:  make(chan struct{}),
		}

		// Start watcher in goroutine
		go runner.watchPredictionResponses(ctx, predictionID, pending)

		// Create terminal response file
		filename := fmt.Sprintf("response-%s-00001.json", predictionID)
		filePath := filepath.Join(tempDir, filename)
		response := PredictionResponse{
			Status: PredictionSucceeded,
			Output: "final output",
		}
		responseData, err := json.Marshal(response)
		require.NoError(t, err)
		err = os.WriteFile(filePath, responseData, 0o644)
		require.NoError(t, err)

		// Send OUTPUT notification to trigger processing
		select {
		case pending.outputNotify <- struct{}{}:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Failed to send OUTPUT notification")
		}

		// Wait for watcher to exit
		select {
		case <-pending.watcherDone:
			// Success - watcher exited
		case <-time.After(500 * time.Millisecond):
			t.Fatal("Watcher did not exit after terminal response")
		}

		// Verify response was sent to channel
		select {
		case resp := <-pending.c:
			assert.Equal(t, PredictionSucceeded, resp.Status)
			assert.Equal(t, "final output", resp.Output)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("No response sent to channel")
		}
	})

	t.Run("WatcherContinuesOnNonTerminalResponse", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		predictionID, _ := PredictionID()

		// Setup runner
		logger := loggingtest.NewTestLogger(t)
		runner := &Runner{
			runnerCtx: RunnerContext{workingdir: tempDir},
			logger:    logger,
			mu:        sync.RWMutex{},
		}

		// Setup pending prediction
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		pending := &PendingPrediction{
			request:      PredictionRequest{ID: predictionID},
			response:     PredictionResponse{},
			outputCache:  make(map[string]string),
			mu:           sync.Mutex{},
			c:            make(chan PredictionResponse, 1),
			outputNotify: make(chan struct{}, 1),
			cancel:       cancel,
			watcherDone:  make(chan struct{}),
		}

		// Start watcher
		go runner.watchPredictionResponses(ctx, predictionID, pending)

		// Create non-terminal response file
		filename := fmt.Sprintf("response-%s-00001.json", predictionID)
		filePath := filepath.Join(tempDir, filename)
		response := PredictionResponse{
			Status: PredictionProcessing,
			Output: "intermediate output",
		}
		responseData, err := json.Marshal(response)
		require.NoError(t, err)
		err = os.WriteFile(filePath, responseData, 0o644)
		require.NoError(t, err)

		// Send OUTPUT notification
		select {
		case pending.outputNotify <- struct{}{}:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Failed to send OUTPUT notification")
		}

		// Wait a bit for processing
		time.Sleep(50 * time.Millisecond)

		// Verify watcher is still running (watcherDone not closed)
		select {
		case <-pending.watcherDone:
			t.Fatal("Watcher exited on non-terminal response")
		default:
			// Success - watcher is still running
		}

		// Verify no response sent to channel for non-terminal
		select {
		case <-pending.c:
			t.Fatal("Response sent to channel for non-terminal status")
		default:
			// Success - no response sent
		}

		// Verify pending response was updated
		pending.mu.Lock()
		assert.Equal(t, PredictionProcessing, pending.response.Status)
		assert.Equal(t, "intermediate output", pending.response.Output)
		pending.mu.Unlock()

		// Cancel to cleanup
		cancel()

		// Wait for watcher to exit via cancellation
		select {
		case <-pending.watcherDone:
			// Success
		case <-time.After(200 * time.Millisecond):
			t.Fatal("Watcher did not exit after context cancellation")
		}
	})
}

func TestForceKillCleanupFailures(t *testing.T) {
	// These tests cannot run in parallel because they modify package-level osExit variable

	t.Run("non-procedure mode kill failure marks runner defunct", func(t *testing.T) {
		killCalled := false
		killError := fmt.Errorf("kill failed")

		r := &Runner{
			cmd: &exec.Cmd{
				Process: &os.Process{Pid: 12345},
			},
			killFn: func(pid int) error {
				killCalled = true
				return killError
			},
			status:        StatusReady,
			forceShutdown: nil, // Non-procedure mode
			logger:        loggingtest.NewTestLogger(t),
		}

		r.ForceKill()

		assert.True(t, killCalled)
		assert.True(t, r.killed)
		assert.Equal(t, StatusDefunct, r.status)
	})

	t.Run("procedure mode kill failure marks runner defunct and returns token", func(t *testing.T) {
		killCalled := false
		killError := fmt.Errorf("kill failed")
		forceShutdown := config.NewForceShutdownSignal()

		r := &Runner{
			cmd: &exec.Cmd{
				Process: &os.Process{Pid: 12345},
			},
			killFn: func(pid int) error {
				killCalled = true
				return killError
			},
			status:        StatusReady,
			cleanupSlot:   make(chan struct{}, 1),
			forceShutdown: forceShutdown,
			logger:        loggingtest.NewTestLogger(t),
		}

		// Initialize cleanup slot with token
		r.cleanupSlot <- struct{}{}

		r.ForceKill()

		assert.True(t, killCalled)
		assert.True(t, r.killed)
		assert.Equal(t, StatusDefunct, r.status)
		// Verify cleanup token was returned
		assert.Len(t, r.cleanupSlot, 1)
	})

	t.Run("procedure mode cleanup success returns token", func(t *testing.T) {
		forceShutdown := config.NewForceShutdownSignal()

		r := &Runner{
			cleanupTimeout: 1 * time.Millisecond,
			forceShutdown:  forceShutdown,
			cleanupSlot:    make(chan struct{}, 1),
			stopped:        make(chan bool),
			logger:         loggingtest.NewTestLogger(t),
		}

		// Start verification process
		go r.verifyProcessCleanup(12345)

		// Signal process stopped to trigger cleanup completion
		close(r.stopped)

		// Give a moment for cleanup to complete
		time.Sleep(1 * time.Millisecond)

		// Verify cleanup token was returned
		assert.Len(t, r.cleanupSlot, 1)
	})
}
