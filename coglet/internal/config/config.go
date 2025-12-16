package config

import (
	"sync"
	"time"
)

const (
	TimeFormat = "2006-01-02T15:04:05.999999-07:00"
)

// Config holds all configuration for the cog runtime service
type Config struct {
	// Server configuration
	Host string
	Port int

	// Mode configuration
	UseProcedureMode      bool
	AwaitExplicitShutdown bool
	OneShot               bool

	// Directory configuration
	WorkingDirectory string
	UploadURL        string
	IPCUrl           string

	// Runner configuration
	MaxRunners                int
	PythonCommand             []string // Command to invoke Python, e.g., ["python3"] or ["uv", "run", "--directory", "/path", "python3"]
	RunnerShutdownGracePeriod time.Duration

	// Cleanup configuration
	CleanupTimeout     time.Duration
	CleanupDirectories []string // Directories to walk for cleanup of files owned by isolated UIDs

	// Environment configuration
	EnvSet   map[string]string
	EnvUnset []string

	// Force shutdown signal
	ForceShutdown *ForceShutdownSignal
}

// ForceShutdownSignal provides idempotent force shutdown signaling
type ForceShutdownSignal struct {
	mu        sync.Mutex
	ch        chan struct{}
	triggered bool
}

// NewForceShutdownSignal creates a new force shutdown signal
func NewForceShutdownSignal() *ForceShutdownSignal {
	return &ForceShutdownSignal{
		ch: make(chan struct{}),
	}
}

// WatchForForceShutdown returns a channel that closes when force shutdown is triggered
func (f *ForceShutdownSignal) WatchForForceShutdown() <-chan struct{} {
	return f.ch
}

// TriggerForceShutdown triggers force shutdown idempotently
func (f *ForceShutdownSignal) TriggerForceShutdown() bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.triggered {
		return false // Already triggered
	}

	f.triggered = true
	close(f.ch)
	return true // First trigger
}
