package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/alecthomas/kong"

	"github.com/replicate/cog/coglet/internal/config"
	"github.com/replicate/cog/coglet/internal/logging"
	"github.com/replicate/cog/coglet/internal/runner"
	"github.com/replicate/cog/coglet/internal/service"
	"github.com/replicate/cog/coglet/internal/version"
)

type ServerCmd struct {
	Host                      string        `help:"Host address to bind the HTTP server to" default:"0.0.0.0" env:"COG_HOST"`
	Port                      int           `help:"Port number for the HTTP server" default:"5000" env:"COG_PORT"`
	UseProcedureMode          bool          `help:"Enable procedure mode for concurrent predictions" name:"use-procedure-mode" env:"COG_USE_PROCEDURE_MODE"`
	AwaitExplicitShutdown     bool          `help:"Wait for explicit shutdown signal instead of auto-shutdown" name:"await-explicit-shutdown" env:"COG_AWAIT_EXPLICIT_SHUTDOWN"`
	OneShot                   bool          `help:"Enable one-shot mode (single runner, wait for cleanup before ready)" name:"one-shot" env:"COG_ONE_SHOT"`
	UploadURL                 string        `help:"Base URL for uploading prediction output files" name:"upload-url" env:"COG_UPLOAD_URL"`
	WorkingDirectory          string        `help:"Override the working directory for predictions" name:"working-directory" env:"COG_WORKING_DIRECTORY"`
	RunnerShutdownGracePeriod time.Duration `help:"Grace period before force-killing prediction runners" name:"runner-shutdown-grace-period" default:"600s" env:"COG_RUNNER_SHUTDOWN_GRACE_PERIOD"`
	CleanupTimeout            time.Duration `help:"Maximum time to wait for process cleanup before hard exit" name:"cleanup-timeout" default:"10s" env:"COG_CLEANUP_TIMEOUT"`
	MaxRunners                int           `help:"Maximum number of runners to allow (0 for auto-detect)" name:"max-runners" env:"COG_MAX_RUNNERS" default:"0"`
}

type SchemaCmd struct{}

type TestCmd struct{}

type CLI struct {
	Server ServerCmd `cmd:"" help:"Start the Cog HTTP server for serving predictions"`
	Schema SchemaCmd `cmd:"" help:"Generate OpenAPI schema from model definition"`
	Test   TestCmd   `cmd:"" help:"Run model tests to verify functionality"`
}

// buildServiceConfig converts CLI ServerCmd to service configuration
func buildServiceConfig(s *ServerCmd) (config.Config, error) {
	log := logging.New("cog-config").Sugar()

	logLevel := log.Level()
	log.Debugw("log level", "level", logLevel)
	// One-shot mode requires procedure mode
	if s.OneShot && !s.UseProcedureMode {
		log.Fatal("one-shot mode requires procedure mode")
		return config.Config{}, fmt.Errorf("one-shot mode requires procedure mode, use --use-procedure-mode")
	}

	// Procedure mode implies await explicit shutdown
	awaitExplicitShutdown := s.AwaitExplicitShutdown
	if s.UseProcedureMode {
		awaitExplicitShutdown = true
	}

	// Resolve working directory
	workingDir := s.WorkingDirectory
	if workingDir == "" {
		var err error
		workingDir, err = os.Getwd()
		if err != nil {
			log.Errorw("failed to get current working directory", "error", err)
			return config.Config{}, fmt.Errorf("failed to get current working directory: %w", err)
		}
	}

	cfg := config.Config{
		Host:                      s.Host,
		Port:                      s.Port,
		UseProcedureMode:          s.UseProcedureMode,
		AwaitExplicitShutdown:     awaitExplicitShutdown,
		OneShot:                   s.OneShot,
		WorkingDirectory:          workingDir,
		UploadURL:                 s.UploadURL,
		IPCUrl:                    fmt.Sprintf("http://localhost:%d/_ipc", s.Port),
		MaxRunners:                s.MaxRunners,
		RunnerShutdownGracePeriod: s.RunnerShutdownGracePeriod,
		CleanupTimeout:            s.CleanupTimeout,
		CleanupDirectories:        []string{"/tmp"},
	}

	log.Infow("service configuration",
		"use_procedure_mode", cfg.UseProcedureMode,
		"await_explicit_shutdown", cfg.AwaitExplicitShutdown,
		"one_shot", cfg.OneShot,
		"upload_url", cfg.UploadURL,
		"working_directory", cfg.WorkingDirectory,
		"max_runners", cfg.MaxRunners,
	)

	return cfg, nil
}

func (s *ServerCmd) Run() error {
	// Create base logger
	baseLogger := logging.New("cog")
	log := baseLogger.Sugar()

	// Build service configuration
	cfg, err := buildServiceConfig(s)
	if err != nil {
		return err
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	log.Infow("starting Cog HTTP server", "addr", addr, "version", version.Version(), "pid", os.Getpid())

	// Create service with base logger
	svc := service.New(cfg, baseLogger)

	// Create root context for the entire service
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize service components
	if err := svc.Initialize(ctx); err != nil {
		return err
	}

	return svc.Run(ctx)
}

func (s *SchemaCmd) Run() error {
	log := logging.New("cog-schema").Sugar()

	wd, err := os.Getwd()
	if err != nil {
		log.Errorw("failed to get working directory", "error", err)
		return err
	}
	y, err := runner.ReadCogYaml(wd)
	if err != nil {
		log.Errorw("failed to read cog.yaml", "error", err)
		return err
	}
	m, c, err := y.PredictModuleAndPredictor()
	if err != nil {
		log.Errorw("failed to parse predict", "error", err)
		return err
	}
	bin, err := exec.LookPath("python3")
	if err != nil {
		log.Errorw("failed to find python3", "error", err)
		return err
	}
	return syscall.Exec(bin, []string{bin, "-m", "coglet.schema", m, c}, os.Environ()) //nolint:gosec // expected subprocess launched with variable
}

func (t *TestCmd) Run() error {
	log := logging.New("cog-test").Sugar()

	wd, err := os.Getwd()
	if err != nil {
		log.Errorw("failed to get working directory", "error", err)
		return err
	}
	y, err := runner.ReadCogYaml(wd)
	if err != nil {
		log.Errorw("failed to read cog.yaml", "error", err)
		return err
	}
	m, c, err := y.PredictModuleAndPredictor()
	if err != nil {
		log.Errorw("failed to parse predict", "error", err)
		return err
	}
	bin, err := exec.LookPath("python3")
	if err != nil {
		log.Errorw("failed to find python3", "error", err)
		return err
	}
	return syscall.Exec(bin, []string{bin, "-m", "coglet.test", m, c}, os.Environ()) //nolint:gosec // expected subprocess launched with variable
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("cog"),
		kong.Description("Cog runtime for serving machine learning models via HTTP API"),
		kong.UsageOnError(),
	)

	err := ctx.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err) //nolint:forbidigo // main function error handling
		os.Exit(1)
	}
}
