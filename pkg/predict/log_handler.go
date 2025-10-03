package predict

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"sync"

	"github.com/replicate/cog/pkg/util/console"
)

// LogEntry represents a structured log entry from the container
type LogEntry struct {
	Severity  string `json:"severity"`
	Timestamp string `json:"timestamp"`
	Logger    string `json:"logger"`
	Caller    string `json:"caller"`
	Message   string `json:"message"`
	// Additional fields are ignored but preserved
}

// LogHandler implements io.Writer and processes container stderr output
// It parses JSON logs and routes them to appropriate console levels,
// while handling unstructured logs gracefully.
type LogHandler struct {
	mu sync.Mutex
}

// NewLogHandler creates a new LogHandler
func NewLogHandler() *LogHandler {
	return &LogHandler{}
}

// Write implements io.Writer interface
func (lh *LogHandler) Write(p []byte) (n int, err error) {
	lh.mu.Lock()
	defer lh.mu.Unlock()

	// TEMPORARY: Tee raw output to stderr for debugging
	os.Stderr.WriteString("RAW: " + string(p))

	// Process each line
	scanner := bufio.NewScanner(strings.NewReader(string(p)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		lh.processLine(line)
	}

	return len(p), nil
}

// processLine processes a single log line
func (lh *LogHandler) processLine(line string) {
	// Try to parse as JSON first
	if lh.TryParseJSONLog(line) {
		return
	}

	// Handle unstructured logs
	lh.handleUnstructuredLog(line)
}

// TryParseJSONLog attempts to parse the line as a JSON log entry
// Returns true if successfully parsed and handled
// This is exported for testing purposes
func (lh *LogHandler) TryParseJSONLog(line string) bool {
	var entry LogEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return false
	}

	// Route based on severity level
	switch strings.ToLower(entry.Severity) {
	case "debug":
		console.Debug(entry.Message)
	case "info":
		console.Debug(entry.Message) // Info logs from container go to debug level
	case "warn", "warning":
		console.Warn(entry.Message)
	case "error":
		console.Error(entry.Message)
	case "fatal":
		console.Error(entry.Message) // Fatal logs from container go to error level
	default:
		// Unknown severity, treat as info
		console.Debug(entry.Message)
	}

	return true
}

// handleUnstructuredLog handles non-JSON log lines
func (lh *LogHandler) handleUnstructuredLog(line string) {
	// Check for common error patterns
	lowerLine := strings.ToLower(line)

	// Route based on content patterns
	switch {
	case strings.Contains(lowerLine, "error") || strings.Contains(lowerLine, "failed") || strings.Contains(lowerLine, "exception"):
		console.Error(line)
	case strings.Contains(lowerLine, "warning") || strings.Contains(lowerLine, "warn"):
		console.Warn(line)
	case strings.Contains(lowerLine, "debug"):
		console.Debug(line)
	default:
		// Default to debug level for unstructured logs
		// This prevents cluttering the user's output with container internals
		console.Debug(line)
	}
}
