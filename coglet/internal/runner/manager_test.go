package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/coglet/internal/config"
	"github.com/replicate/cog/coglet/internal/loggingtest"
)

func TestNewManager(t *testing.T) {
	t.Parallel()

	t.Run("procedure mode", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name           string
			cfg            config.Config
			expectedMaxRun int
		}{
			{
				name: "one shot",
				cfg: config.Config{
					UseProcedureMode: true,
					OneShot:          true,
				},
				expectedMaxRun: 1,
			},
			{
				name: "explicit max runners",
				cfg: config.Config{
					UseProcedureMode: true,
					MaxRunners:       12,
				},
				expectedMaxRun: 12,
			},
			{
				name: "custom max runners",
				cfg: config.Config{
					UseProcedureMode: true,
					MaxRunners:       8,
				},
				expectedMaxRun: 8,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				logger := loggingtest.NewTestLogger(t)
				m := newManager(t.Context(), tt.cfg, logger)

				assert.True(t, m.cfg.UseProcedureMode)
				assert.Equal(t, tt.cfg.OneShot, m.cfg.OneShot)
				assert.Equal(t, tt.expectedMaxRun, cap(m.capacity))
				assert.Equal(t, tt.expectedMaxRun, cap(m.capacity))
				assert.Len(t, m.capacity, tt.expectedMaxRun)
			})
		}
	})

	t.Run("non-procedure mode", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{
			UseProcedureMode: false,
			MaxRunners:       10, // Should be ignored
		}

		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		assert.False(t, m.cfg.UseProcedureMode)
		assert.Equal(t, 1, cap(m.capacity))
		assert.Len(t, m.capacity, 1)
	})
}

func TestManager(t *testing.T) {
	t.Parallel()
	t.Run("IsStopped", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		assert.False(t, m.IsStopped())

		err := m.Stop()
		require.NoError(t, err)

		assert.True(t, m.IsStopped())
	})

	t.Run("exit code procedure mode", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{UseProcedureMode: true}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		exitCode := m.ExitCode()
		assert.Equal(t, 0, exitCode)
	})

	t.Run("exit code non-procedure mode no runner", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{UseProcedureMode: false}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		exitCode := m.ExitCode()
		assert.Equal(t, 0, exitCode)
	})
}

func TestManagerSlots(t *testing.T) {
	t.Parallel()

	t.Run("generates unique names", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{
			UseProcedureMode: true,
			MaxRunners:       10,
		}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		// Pre-fill some runners to force name collision checks
		wd, err := os.Getwd()
		require.NoError(t, err)
		testProcedureURL := "file://" + filepath.Join(wd, "../../python/tests/procedures/foo")
		for i := 0; i < 5; i++ {
			_, err := m.allocateRunnerSlot(testProcedureURL)
			require.NoError(t, err)
		}

		// Generate more names and ensure they're all unique
		generatedNames := make(map[string]bool)
		for i := 0; i < 5; i++ {
			r, err := m.allocateRunnerSlot(testProcedureURL)
			name := r.runnerCtx.id
			require.NoError(t, err)
			assert.NotContains(t, generatedNames, name, "Name %s was generated twice", name)
			generatedNames[name] = true
		}
	})

	t.Run("evicts idle runners when at capacity", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{
			UseProcedureMode: true,
			MaxRunners:       2,
		}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		// Fill to capacity with idle runners
		wd, err := os.Getwd()
		require.NoError(t, err)
		testProcedureURL := "file://" + filepath.Join(wd, "../../python/tests/procedures/foo")
		runners := []*Runner{}
		for i := 0; i < 2; i++ {
			r, err := m.allocateRunnerSlot(testProcedureURL)
			runners = append(runners, r)
			require.NoError(t, err)
			// Set runner to Ready status so it can be evicted
			r.status = StatusReady
		}

		// Should evict an idle runner and return a new unique name
		newProcedureURL := "file://" + filepath.Join(wd, "../../python/tests/procedures/bar")
		newRunner, err := m.allocateRunnerSlot(newProcedureURL)
		require.NoError(t, err)
		assert.NotContains(t, runners, newRunner)

		// Should still have 2 slots total, with the new runner in one slot
		activeRunners := m.Runners()
		assert.Len(t, activeRunners, 2) // Full slice length
		// The new runner should be in one of the slots
		assert.True(t, activeRunners[0] == newRunner || activeRunners[1] == newRunner)
	})
	t.Run("claim available slot", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{MaxRunners: 2, UseProcedureMode: true}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		err := m.claimSlot()
		require.NoError(t, err)
		assert.Len(t, m.capacity, 1)
	})

	t.Run("claim when no capacity", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{MaxRunners: 1}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		// Claim the only slot
		err := m.claimSlot()
		require.NoError(t, err)

		// Try to claim again
		err = m.claimSlot()
		assert.Equal(t, ErrNoCapacity, err)
	})

	t.Run("release slot", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{MaxRunners: 2, UseProcedureMode: true}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		// Claim a slot
		err := m.claimSlot()
		require.NoError(t, err)
		assert.Len(t, m.capacity, 1)

		// Release it
		m.releaseSlot()
		assert.Len(t, m.capacity, 2)
	})

	t.Run("release when channel full", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{MaxRunners: 1}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		// Channel starts full, try to release
		m.releaseSlot()
		assert.Len(t, m.capacity, 1)
	})
}

func TestManagerRunnerManagement(t *testing.T) {
	t.Parallel()
	t.Run("ActiveRunners", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{
			UseProcedureMode: true,
			MaxRunners:       4,
		}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		runner1 := &Runner{
			runnerCtx: RunnerContext{id: "runner1"},
		}
		runner2 := &Runner{
			runnerCtx: RunnerContext{id: "runner2"},
		}

		m.runners[0] = runner1
		m.runners[1] = runner2

		active := m.Runners()
		assert.Len(t, active, 4) // Full slice length in procedure mode
		assert.Equal(t, runner1, active[0])
		assert.Equal(t, runner2, active[1])
		assert.Nil(t, active[2])
		assert.Nil(t, active[3])
	})

	t.Run("ForceKillAll", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{
			UseProcedureMode: true,
			MaxRunners:       4,
		}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		killCalled1 := false
		killCalled2 := false

		runner1 := &Runner{
			runnerCtx: RunnerContext{id: "runner1"},
			killFn:    func(pid int) error { killCalled1 = true; return nil },
			logger:    loggingtest.NewTestLogger(t),
		}
		runner2 := &Runner{
			runnerCtx: RunnerContext{id: "runner2"},
			killFn:    func(pid int) error { killCalled2 = true; return nil },
			logger:    loggingtest.NewTestLogger(t),
		}

		// Mock running processes
		runner1.cmd = &exec.Cmd{Process: &os.Process{Pid: 12345}}
		runner2.cmd = &exec.Cmd{Process: &os.Process{Pid: 12346}}

		m.runners[0] = runner1
		m.runners[1] = runner2

		m.ForceKillAll()

		assert.True(t, killCalled1)
		assert.True(t, killCalled2)
	})
}

func TestManagerCapacity(t *testing.T) {
	t.Parallel()

	t.Run("procedure mode", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{
			UseProcedureMode: true,
			MaxRunners:       5,
		}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		capacity := m.Capacity()
		assert.Equal(t, 5, capacity)

		availableCapacity := m.AvailableCapacity()
		assert.Equal(t, 5, availableCapacity)

		err := m.claimSlot()
		require.NoError(t, err)
		err = m.claimSlot()
		require.NoError(t, err)

		capacity = m.Capacity()
		assert.Equal(t, 3, capacity)

		availableCapacity = m.AvailableCapacity()
		assert.Equal(t, 3, availableCapacity)
	})

	t.Run("non-procedure mode", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{
			UseProcedureMode: false,
			MaxRunners:       5,
		}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		capacity := m.Capacity()
		assert.Equal(t, 1, capacity)

		availableCapacity := m.AvailableCapacity()
		assert.Equal(t, 1, availableCapacity)
	})

	t.Run("reads max concurrency from cog.yaml", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		cogYamlContent := `{"concurrency": {"max": 5}, "predict": "predict.py:predict"}`

		err := os.WriteFile(filepath.Join(tempDir, "cog.yaml"), []byte(cogYamlContent), 0o644)
		require.NoError(t, err)

		cfg := config.Config{
			UseProcedureMode: false,
			WorkingDirectory: tempDir,
		}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		// In non-procedure mode, newManager should read cog.yaml and set capacity accordingly
		assert.Equal(t, 5, m.AvailableCapacity())
	})

	t.Run("uses fallback when cog.yaml missing", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()

		cfg := config.Config{
			UseProcedureMode: false,
			WorkingDirectory: tempDir,
		}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		// In non-procedure mode, newManager should use fallback concurrency when cog.yaml missing
		assert.Equal(t, 1, m.AvailableCapacity())
	})

	t.Run("uses fallback when cog.yaml invalid", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		err := os.WriteFile(filepath.Join(tempDir, "cog.yaml"), []byte("invalid yaml content"), 0o644)
		require.NoError(t, err)

		cfg := config.Config{
			UseProcedureMode: false,
			WorkingDirectory: tempDir,
		}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		// In non-procedure mode, newManager should use fallback concurrency when cog.yaml invalid
		assert.Equal(t, 1, m.AvailableCapacity())
	})
}

func TestManagerConcurrency(t *testing.T) {
	t.Parallel()

	cfg := config.Config{MaxRunners: 4, UseProcedureMode: true}
	logger := loggingtest.NewTestLogger(t)
	m := newManager(t.Context(), cfg, logger)

	// Claim 2 slots
	err := m.claimSlot()
	require.NoError(t, err)
	err = m.claimSlot()
	require.NoError(t, err)

	concurrency := m.Concurrency()
	assert.Equal(t, 4, concurrency.Max)
	assert.Equal(t, 2, concurrency.Current)
}

func TestManagerStatusNonProcedureMode(t *testing.T) {
	t.Parallel()

	t.Run("no default runner", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{UseProcedureMode: false}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		status := m.Status()
		assert.Equal(t, "STARTING", status)
	})

	t.Run("with ready runner in default slot", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{UseProcedureMode: false}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)
		runner := &Runner{
			runnerCtx: RunnerContext{id: DefaultRunnerName},
			status:    StatusReady,
		}
		m.runners[0] = runner

		status := m.Status()
		assert.Equal(t, "READY", status)
	})
}

func TestManagerStatusProcedureMode(t *testing.T) {
	t.Parallel()

	t.Run("has capacity", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{
			UseProcedureMode: true,
			MaxRunners:       2,
		}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		status := m.Status()
		assert.Equal(t, "READY", status)
	})

	t.Run("no capacity", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{
			UseProcedureMode: true,
			MaxRunners:       1,
		}
		logger := loggingtest.NewTestLogger(t)
		m := NewManager(t.Context(), cfg, logger)

		// Claim the only slot
		err := m.claimSlot()
		require.NoError(t, err)

		status := m.Status()
		assert.Equal(t, "BUSY", status)
	})
}

func TestManagerSetupResult(t *testing.T) {
	t.Parallel()

	t.Run("procedure mode", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{UseProcedureMode: true}
		logger := loggingtest.NewTestLogger(t)
		m := NewManager(t.Context(), cfg, logger)

		result := m.SetupResult()
		assert.Equal(t, SetupSucceeded, result.Status)
	})
}

func TestManagerPredictionHandling(t *testing.T) {
	t.Parallel()

	t.Run("cancel prediction", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		cfg := config.Config{
			WorkingDirectory: tempDir,
		}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		ctx, cancel := context.WithCancel(context.Background())
		runner := &Runner{
			ctx:         ctx,
			cancel:      cancel,
			runnerCtx:   RunnerContext{id: "test-runner", workingdir: tempDir},
			pending:     make(map[string]*PendingPrediction),
			cleanupSlot: make(chan struct{}, 1),
			logger:      loggingtest.NewTestLogger(t),
		}
		m.runners[0] = runner

		runner.pending["test-id"] = &PendingPrediction{}

		err := m.CancelPrediction("test-id")
		require.NoError(t, err)
	})

	t.Run("cancel prediction, prediction not found", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		runner := &Runner{
			pending: make(map[string]*PendingPrediction),
		}
		m.runners[0] = runner

		err := m.CancelPrediction("nonexistent")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrPredictionNotFound)
	})
	t.Run("run prediction no capacity", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{MaxRunners: 1}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		// Claim the only slot
		err := m.claimSlot()
		require.NoError(t, err)

		req := PredictionRequest{ID: "test-id"}
		_, err = m.PredictSync(req)
		require.Error(t, err)
		assert.Equal(t, ErrNoCapacity, err)
	})
}

func TestManagerRunnerIPC(t *testing.T) {
	t.Parallel()

	t.Run("runner exists", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		tempDir := t.TempDir()
		ctx, cancel := context.WithCancel(context.Background())
		runner := &Runner{
			ctx:           ctx,
			cancel:        cancel,
			runnerCtx:     RunnerContext{id: "test-runner", workingdir: tempDir},
			status:        StatusStarting,
			pending:       make(map[string]*PendingPrediction),
			cleanupSlot:   make(chan struct{}, 1),
			logger:        loggingtest.NewTestLogger(t),
			setupComplete: make(chan struct{}),
		}
		m.runners[0] = runner

		err := m.HandleRunnerIPC("test-runner", "READY")
		require.NoError(t, err)
		assert.Equal(t, StatusReady, runner.status)
	})

	t.Run("runner not found", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		err := m.HandleRunnerIPC("nonexistent", "READY")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrRunnerNotFound)
	})
}

func TestManagerStop(t *testing.T) {
	t.Parallel()

	t.Run("stop with runners", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{
			UseProcedureMode: true,
			MaxRunners:       4,
		}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		runner1 := &Runner{
			runnerCtx: RunnerContext{id: "runner1"},
			status:    StatusReady,
			pending:   make(map[string]*PendingPrediction),
			killFn:    func(pid int) error { return nil },
			stopped:   make(chan bool),
			logger:    loggingtest.NewTestLogger(t),
		}
		runner2 := &Runner{
			runnerCtx: RunnerContext{id: "runner2"},
			status:    StatusReady,
			pending:   make(map[string]*PendingPrediction),
			killFn:    func(pid int) error { return nil },
			stopped:   make(chan bool),
			logger:    loggingtest.NewTestLogger(t),
		}

		m.runners[0] = runner1
		m.runners[1] = runner2

		err := m.Stop()
		require.NoError(t, err)

		// Check that manager is stopped
		assert.True(t, m.IsStopped())

		// Check that runners are stopped
		assert.Equal(t, StatusDefunct, runner1.status)
		assert.Equal(t, StatusDefunct, runner2.status)
	})

	t.Run("stop multiple times", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		err1 := m.Stop()
		require.NoError(t, err1)

		err2 := m.Stop()
		require.NoError(t, err2)

		assert.True(t, m.IsStopped())
	})
}

func TestManagerSchema(t *testing.T) {
	t.Parallel()

	t.Run("procedure mode", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{UseProcedureMode: true}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		schema, ok := m.Schema()
		assert.True(t, ok)
		assert.NotEmpty(t, schema)
	})

	t.Run("non-procedure mode no runner", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{UseProcedureMode: false}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		schema, ok := m.Schema()
		assert.False(t, ok)
		assert.Empty(t, schema)
	})

	t.Run("non-procedure mode with runner", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{UseProcedureMode: false}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		runner := &Runner{
			runnerCtx: RunnerContext{id: DefaultRunnerName},
			schema:    "test-schema",
		}
		m.runners[0] = runner

		schema, ok := m.Schema()
		assert.True(t, ok)
		assert.Equal(t, "test-schema", schema)
	})

	t.Run("non-procedure mode runner not ready", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{UseProcedureMode: false}
		logger := loggingtest.NewTestLogger(t)
		m := newManager(t.Context(), cfg, logger)

		runner := &Runner{
			runnerCtx: RunnerContext{id: DefaultRunnerName},
			schema:    "",
		}
		m.runners[0] = runner

		schema, ok := m.Schema()
		assert.False(t, ok)
		assert.Empty(t, schema)
	})
}
