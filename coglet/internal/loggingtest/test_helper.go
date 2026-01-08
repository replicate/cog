package loggingtest

import (
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"github.com/replicate/cog/coglet/internal/logging"
)

// customTestLevelEncoder handles our custom Trace level display for tests
func customTestLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	switch level {
	case logging.TraceLevel:
		enc.AppendString("TRACE")
	default:
		zapcore.CapitalLevelEncoder(level, enc)
	}
}

// NewTestLogger creates a logger for tests that outputs to t.Logf
// Behaves exactly like zaptest.NewLogger but with trace support added
func NewTestLogger(t *testing.T) *logging.Logger {
	t.Helper()

	// Create test logger with custom level encoder
	zapLogger := zaptest.NewLogger(t,
		zaptest.Level(logging.TraceLevel),
		zaptest.WrapOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			// Replace the encoder to handle our custom trace level
			enc := zapcore.NewConsoleEncoder(zapcore.EncoderConfig{
				TimeKey:        "T",
				LevelKey:       "L",
				NameKey:        "N",
				CallerKey:      "C",
				MessageKey:     "M",
				StacktraceKey:  "S",
				LineEnding:     zapcore.DefaultLineEnding,
				EncodeLevel:    customTestLevelEncoder,
				EncodeTime:     zapcore.ISO8601TimeEncoder,
				EncodeDuration: zapcore.StringDurationEncoder,
				EncodeCaller:   zapcore.ShortCallerEncoder,
			})
			return zapcore.NewCore(enc, zapcore.AddSync(zaptest.NewTestingWriter(t)), logging.TraceLevel)
		})),
	)
	return &logging.Logger{Logger: zapLogger}
}
