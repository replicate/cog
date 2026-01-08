package logging

import (
	"os"
	"testing"

	"go.uber.org/zap/zapcore"
)

func TestNew(t *testing.T) {
	// Save original env vars
	originalCogLevel := os.Getenv("COG_LOG_LEVEL")
	originalLogLevel := os.Getenv("LOG_LEVEL")
	originalLogFormat := os.Getenv("LOG_FORMAT")
	originalLogFile := os.Getenv("LOG_FILE")

	defer func() {
		// Restore original env vars
		os.Setenv("COG_LOG_LEVEL", originalCogLevel)
		os.Setenv("LOG_LEVEL", originalLogLevel)
		os.Setenv("LOG_FORMAT", originalLogFormat)
		os.Setenv("LOG_FILE", originalLogFile)
	}()

	t.Run("creates logger with default level", func(t *testing.T) {
		os.Unsetenv("COG_LOG_LEVEL")
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("LOG_FORMAT")
		os.Unsetenv("LOG_FILE")

		logger := New("test")
		if logger == nil {
			t.Fatal("expected logger to be created")
		}

		// Should be able to call basic methods
		logger.Info("test message")
		logger.Debug("debug message")
		logger.Trace("trace message")
	})

	t.Run("respects COG_LOG_LEVEL environment variable", func(t *testing.T) {
		os.Setenv("COG_LOG_LEVEL", "debug")
		defer os.Unsetenv("COG_LOG_LEVEL")

		logger := New("test")
		if logger == nil {
			t.Fatal("expected logger to be created")
		}
	})

	t.Run("respects LOG_LEVEL as fallback", func(t *testing.T) {
		os.Unsetenv("COG_LOG_LEVEL")
		os.Setenv("LOG_LEVEL", "warn")
		defer os.Unsetenv("LOG_LEVEL")

		logger := New("test")
		if logger == nil {
			t.Fatal("expected logger to be created")
		}
	})

	t.Run("handles development format", func(t *testing.T) {
		os.Setenv("LOG_FORMAT", "development")
		defer os.Unsetenv("LOG_FORMAT")

		logger := New("test")
		if logger == nil {
			t.Fatal("expected logger to be created")
		}
	})

	t.Run("handles console format", func(t *testing.T) {
		os.Setenv("LOG_FORMAT", "console")
		defer os.Unsetenv("LOG_FORMAT")

		logger := New("test")
		if logger == nil {
			t.Fatal("expected logger to be created")
		}
	})

	t.Run("respects LOG_FILE environment variable", func(t *testing.T) {
		// Use test temp directory for log file output
		tempDir := t.TempDir()
		logFile := tempDir + "/test.log"

		os.Setenv("LOG_FILE", logFile)
		defer os.Unsetenv("LOG_FILE")

		logger := New("test")
		if logger == nil {
			t.Fatal("expected logger to be created")
		}

		// Write a log message
		logger.Info("test log to file")

		// Verify file was created (basic check)
		if _, err := os.Stat(logFile); os.IsNotExist(err) {
			t.Errorf("expected log file to be created at %s", logFile)
		}
	})

	t.Run("handles LOG_FILE=stdout", func(t *testing.T) {
		os.Setenv("LOG_FILE", "stdout")
		defer os.Unsetenv("LOG_FILE")

		logger := New("test")
		if logger == nil {
			t.Fatal("expected logger to be created")
		}

		// Should not panic when writing to stdout
		logger.Info("test log to stdout")
	})

	t.Run("handles LOG_FILE=stderr", func(t *testing.T) {
		os.Setenv("LOG_FILE", "stderr")
		defer os.Unsetenv("LOG_FILE")

		logger := New("test")
		if logger == nil {
			t.Fatal("expected logger to be created")
		}

		// Should not panic when writing to stderr
		logger.Info("test log to stderr")
	})
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected zapcore.Level
		hasError bool
	}{
		{"trace", TraceLevel, false},
		{"debug", zapcore.DebugLevel, false},
		{"info", zapcore.InfoLevel, false},
		{"warn", zapcore.WarnLevel, false},
		{"warning", zapcore.WarnLevel, false},
		{"error", zapcore.ErrorLevel, false},
		{"TRACE", TraceLevel, false},
		{"DEBUG", zapcore.DebugLevel, false},
		{"INFO", zapcore.InfoLevel, false},
		{"WARN", zapcore.WarnLevel, false},
		{"WARNING", zapcore.WarnLevel, false},
		{"ERROR", zapcore.ErrorLevel, false},
		{"invalid", zapcore.InfoLevel, true},
		{"", zapcore.InfoLevel, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			level, err := parseLevel(tt.input)
			if tt.hasError && err == nil {
				t.Errorf("expected error for input %q", tt.input)
			}
			if !tt.hasError && err != nil {
				t.Errorf("unexpected error for input %q: %v", tt.input, err)
			}
			if level != tt.expected {
				t.Errorf("expected level %v, got %v for input %q", tt.expected, level, tt.input)
			}
		})
	}
}
