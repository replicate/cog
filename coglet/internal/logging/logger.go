package logging

import (
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Custom log levels with Trace below Debug
const (
	TraceLevel = zapcore.Level(-8) // Below Debug (-4)
)

// customLowercaseLevelEncoder handles our custom Trace level display (lowercase)
func customLowercaseLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	switch level {
	case TraceLevel:
		enc.AppendString("trace")
	default:
		zapcore.LowercaseLevelEncoder(level, enc)
	}
}

// customColorLevelEncoder handles our custom Trace level display (with colors)
func customColorLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	switch level {
	case TraceLevel:
		enc.AppendString("\x1b[90mTRACE\x1b[0m") // Gray color for trace
	default:
		zapcore.CapitalColorLevelEncoder(level, enc)
	}
}

// Logger embeds zap.Logger and adds Trace level support
type Logger struct {
	*zap.Logger
}

// SugaredLogger embeds zap.SugaredLogger and adds Trace level support
type SugaredLogger struct {
	*zap.SugaredLogger
}

// New creates a new logger with the given name
func New(name string) *Logger {
	// Check if we should use development config (console format)
	logFormat := os.Getenv("LOG_FORMAT")
	isDevelopment := logFormat == "development" || logFormat == "console"

	var cfg zap.Config
	if isDevelopment {
		cfg = zap.NewDevelopmentConfig()
		cfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
		cfg.EncoderConfig.EncodeLevel = customColorLevelEncoder
	} else {
		cfg = zap.NewProductionConfig()
		cfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
		cfg.EncoderConfig.EncodeLevel = customLowercaseLevelEncoder
	}

	// Set log level from environment (COG_LOG_LEVEL takes precedence, fallback to LOG_LEVEL)
	logLevel := os.Getenv("COG_LOG_LEVEL")
	if logLevel == "" {
		logLevel = os.Getenv("LOG_LEVEL")
	}
	if logLevel != "" {
		level, err := parseLevel(logLevel)
		if err != nil {
			fmt.Printf("Failed to parse log level \"%s\": %s\n", logLevel, err) //nolint:forbidigo // logger setup error reporting
		} else {
			cfg.Level = zap.NewAtomicLevelAt(level)
		}
	}

	// Set output file if LOG_FILE is specified
	logFile := os.Getenv("LOG_FILE")
	if logFile != "" {
		cfg.OutputPaths = []string{logFile}
		cfg.ErrorOutputPaths = []string{logFile}
	} else {
		cfg.OutputPaths = []string{"stdout"}
		cfg.ErrorOutputPaths = []string{"stderr"}
	}

	// Common encoder config
	cfg.EncoderConfig.TimeKey = "timestamp"
	cfg.EncoderConfig.LevelKey = "severity"
	cfg.EncoderConfig.NameKey = "logger"
	cfg.EncoderConfig.CallerKey = "caller"
	cfg.EncoderConfig.MessageKey = "message"
	cfg.EncoderConfig.StacktraceKey = "stacktrace"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncoderConfig.EncodeDuration = zapcore.StringDurationEncoder
	cfg.EncoderConfig.EncodeCaller = zapcore.ShortCallerEncoder

	// Disable sampling for now (can be re-enabled later if needed)
	cfg.Sampling = nil

	zapLogger, err := cfg.Build()
	if err != nil {
		panic(fmt.Sprintf("Failed to build logger: %v", err))
	}

	return &Logger{Logger: zapLogger.Named(name)}
}

// parseLevel parses log level string including our custom "trace" level
func parseLevel(level string) (zapcore.Level, error) {
	switch strings.ToLower(level) {
	case "trace":
		return TraceLevel, nil
	case "debug":
		return zapcore.DebugLevel, nil
	case "info":
		return zapcore.InfoLevel, nil
	case "warn", "warning":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	default:
		return zapcore.InfoLevel, fmt.Errorf("unknown log level: %s", level)
	}
}

// Override Sugar to return our custom SugaredLogger
func (l *Logger) Sugar() *SugaredLogger {
	return &SugaredLogger{SugaredLogger: l.Logger.Sugar()}
}

// Override Named to return our custom Logger
func (l *Logger) Named(name string) *Logger {
	return &Logger{Logger: l.Logger.Named(name)}
}

// Override With to return our custom Logger
func (l *Logger) With(fields ...zap.Field) *Logger {
	return &Logger{Logger: l.Logger.With(fields...)}
}

// Override WithOptions to return our custom Logger
func (l *Logger) WithOptions(opts ...zap.Option) *Logger {
	return &Logger{Logger: l.Logger.WithOptions(opts...)}
}

// Add Trace method to Logger
func (l *Logger) Trace(msg string, fields ...zap.Field) {
	l.Log(TraceLevel, msg, fields...)
}

// Add Trace method to SugaredLogger
func (s *SugaredLogger) Trace(args ...any) {
	s.Log(TraceLevel, args...)
}

// Add Tracew method to SugaredLogger
func (s *SugaredLogger) Tracew(msg string, keysAndValues ...any) {
	s.Logw(TraceLevel, msg, keysAndValues...)
}

// Override With to return our custom SugaredLogger
func (s *SugaredLogger) With(args ...any) *SugaredLogger {
	return &SugaredLogger{SugaredLogger: s.SugaredLogger.With(args...)}
}

// Override Named to return our custom SugaredLogger
func (s *SugaredLogger) Named(name string) *SugaredLogger {
	return &SugaredLogger{SugaredLogger: s.SugaredLogger.Named(name)}
}
