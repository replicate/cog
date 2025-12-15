package loggingtest

import (
	"os"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/replicate/cog/coglet/internal/logging"
)

func TestLoggerMethods(t *testing.T) {
	logger := NewTestLogger(t)

	// Test all logger methods
	logger.Trace("trace message", zap.String("key", "value"))
	logger.Debug("debug message", zap.String("key", "value"))
	logger.Info("info message", zap.String("key", "value"))
	logger.Warn("warn message", zap.String("key", "value"))
	logger.Error("error message", zap.String("key", "value"))
}

func TestSugaredLoggerMethods(t *testing.T) {
	logger := NewTestLogger(t)
	sugar := logger.Sugar()

	// Test all sugared logger methods
	sugar.Trace("trace message")
	sugar.Tracew("trace message", "key", "value")
	sugar.Debug("debug message")
	sugar.Debugw("debug message", "key", "value")
	sugar.Info("info message")
	sugar.Infow("info message", "key", "value")
	sugar.Warn("warn message")
	sugar.Warnw("warn message", "key", "value")
	sugar.Error("error message")
	sugar.Errorw("error message", "key", "value")
}

func TestLoggerChaining(t *testing.T) {
	logger := NewTestLogger(t)

	// Test Named returns our custom Logger
	namedLogger := logger.Named("child")
	if namedLogger == nil {
		t.Fatal("expected named logger to be created")
	}

	// Test With returns our custom Logger
	withLogger := logger.With(zap.String("component", "test"))
	if withLogger == nil {
		t.Fatal("expected with logger to be created")
	}

	// Test WithOptions returns our custom Logger
	optionsLogger := logger.WithOptions(zap.AddCaller())
	if optionsLogger == nil {
		t.Fatal("expected options logger to be created")
	}

	// Test that chained loggers have trace support
	namedLogger.Trace("named trace")
	withLogger.Trace("with trace")
	optionsLogger.Trace("options trace")
}

func TestSugaredLoggerChaining(t *testing.T) {
	logger := NewTestLogger(t)
	sugar := logger.Sugar()

	// Test With returns our custom SugaredLogger with Trace support
	withSugar := sugar.With("component", "test")
	withSugar.Trace("trace with sugar chaining")
	withSugar.Tracew("tracew with sugar chaining", "key", "value")

	// Test Named returns our custom SugaredLogger with Trace support
	namedSugar := sugar.Named("child")
	namedSugar.Trace("trace with named sugar")
	namedSugar.Tracew("tracew with named sugar", "key", "value")

	// Test chaining both With and Named
	chainedSugar := sugar.With("component", "test").Named("child")
	chainedSugar.Trace("trace with full chaining")
	chainedSugar.Tracew("tracew with full chaining", "key", "value")
}

func TestTraceLevel(t *testing.T) {
	// Verify TraceLevel is below DebugLevel
	if logging.TraceLevel >= zapcore.DebugLevel {
		t.Errorf("TraceLevel (%d) should be below DebugLevel (%d)", logging.TraceLevel, zapcore.DebugLevel)
	}

	// Create a logger with trace level
	os.Setenv("COG_LOG_LEVEL", "trace")
	defer os.Unsetenv("COG_LOG_LEVEL")

	logger := NewTestLogger(t)

	// Test that trace methods exist and can be called
	logger.Trace("trace message")
	sugar := logger.Sugar()
	sugar.Trace("sugared trace")
	sugar.Tracew("sugared trace with fields", "key", "value")
}
