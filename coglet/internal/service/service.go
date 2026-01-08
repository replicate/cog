package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/coglet/internal/config"
	"github.com/replicate/cog/coglet/internal/logging"
	"github.com/replicate/cog/coglet/internal/server"
)

// HTTPServer interface allows for mocking the HTTP server in tests
type HTTPServer interface {
	ListenAndServe() error
	Close() error
}

// Package-level variable for os.Exit to enable testing
var osExit = func(code int) {
	os.Exit(code)
}

// Service is the root lifecycle owner for the cog runtime
type Service struct {
	cfg config.Config

	// Lifecycle state
	started         chan struct{}
	stopped         chan struct{}
	shutdown        chan struct{}
	shutdownStarted atomic.Bool

	httpServer    HTTPServer
	handler       *server.Handler
	forceShutdown *config.ForceShutdownSignal

	logger *logging.Logger
}

type ServiceOption interface {
	Apply(s *Service)
}

type HTTPServerOption struct {
	HTTPServer HTTPServer
}

func (o HTTPServerOption) Apply(s *Service) {
	s.httpServer = o.HTTPServer
}

type HandlerOption struct {
	Handler *server.Handler
}

func (o HandlerOption) Apply(s *Service) {
	s.handler = o.Handler
}

var (
	_ ServiceOption = (*HTTPServerOption)(nil)
	_ ServiceOption = (*HandlerOption)(nil)
)

// New creates a new Service with the given configuration
func New(cfg config.Config, baseLogger *logging.Logger, opts ...ServiceOption) *Service {
	svc := &Service{
		cfg:      cfg,
		started:  make(chan struct{}),
		stopped:  make(chan struct{}),
		shutdown: make(chan struct{}),
		logger:   baseLogger.Named("service"),
	}
	for _, opt := range opts {
		opt.Apply(svc)
	}
	return svc
}

// Initialize sets up the service components (idempotent)
func (s *Service) Initialize(ctx context.Context) error {
	// Always create force shutdown signal, even if handler is already set
	if s.forceShutdown == nil {
		s.forceShutdown = config.NewForceShutdownSignal()
		s.cfg.ForceShutdown = s.forceShutdown
	}

	if err := s.initializeHandler(ctx); err != nil {
		return err
	}

	if err := s.initializeHTTPServer(ctx); err != nil {
		return err
	}

	return nil
}

// initializeHandler sets up the handler and force shutdown signal (always runs)
func (s *Service) initializeHandler(ctx context.Context) error {
	if s.handler != nil {
		return nil
	}

	log := s.logger.Sugar()
	log.Debug("initializing handler")

	h, err := server.NewHandler(ctx, s.cfg, s.logger)
	if err != nil {
		return fmt.Errorf("failed to create server handler: %w", err)
	}

	s.handler = h
	return nil
}

// initializeHTTPServer sets up the HTTP server if not already set
func (s *Service) initializeHTTPServer(ctx context.Context) error {
	if s.httpServer != nil {
		return nil
	}

	log := s.logger.Sugar()
	log.Debug("initializing HTTP server")

	mux := server.NewServeMux(s.handler, s.cfg.UseProcedureMode)
	s.httpServer = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(l net.Listener) context.Context { return ctx },
	}

	mux.HandleFunc("POST /shutdown", s.HandleShutdown)

	return nil
}

// Run starts the service and blocks until shutdown
func (s *Service) Run(ctx context.Context) error {
	log := s.logger.Sugar()

	select {
	case <-s.started:
		log.Errorw("service already started")
		return nil
	default:
	}

	if s.httpServer == nil {
		return fmt.Errorf("service not initialized - call Initialize() first")
	}

	log.Infow("starting service",
		"use_procedure_mode", s.cfg.UseProcedureMode,
		"working_directory", s.cfg.WorkingDirectory,
	)

	eg, egCtx := errgroup.WithContext(ctx)

	// Start handler (which starts its internal runner manager)
	if err := s.handler.Start(egCtx); err != nil {
		return fmt.Errorf("failed to start handler: %w", err)
	}

	eg.Go(func() error {
		log.Info("starting HTTP server")
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP server failed: %w", err)
		}
		return nil
	})

	eg.Go(func() error { //nolint:contextcheck // uses long-lived errgroup context, not request context
		<-s.shutdown
		log.Info("initiating graceful shutdown")

		// Signal runners to shutdown gracefully and wait for them
		if s.handler != nil {
			log.Tracew("stopping runners gracefully")
			if err := s.handler.Stop(); err != nil {
				log.Errorw("error stopping handler", "error", err)
			}
		}

		// Close HTTP server to unblock the HTTP server goroutine
		if s.httpServer != nil {
			log.Info("closing HTTP server")
			if err := s.httpServer.Close(); err != nil {
				log.Errorw("error closing HTTP server", "error", err)
			}
		}

		return nil
	})

	// Monitor for context cancellation (handles external cancellation)
	eg.Go(func() error {
		select {
		case <-s.shutdown:
			// Shutdown was called, let shutdown handler deal with it
			return nil
		case <-egCtx.Done():
			// Only force immediate shutdown if graceful shutdown hasn't started
			if s.shutdownStarted.CompareAndSwap(false, true) {
				log.Trace("context canceled, forcing immediate shutdown")
				close(s.shutdown)
				// Context canceled = immediate hard shutdown, no grace period
				if err := s.httpServer.Close(); err != nil {
					log.Errorw("failed to close HTTP server", "error", err)
				}
			}
			return egCtx.Err()
		}
	})

	// Handle OS signals only in await-explicit-shutdown mode
	if s.cfg.AwaitExplicitShutdown {
		eg.Go(func() error {
			return s.handleSignals(egCtx)
		})
	}

	// Monitor for forced shutdown from cleanup failures
	eg.Go(func() error {
		defer log.Trace("force shutdown goroutine exiting")
		select {
		case <-s.forceShutdown.WatchForForceShutdown():
			log.Errorw("process cleanup failed, forcing immediate exit")
			osExit(1)
			return nil // This won't be reached, but needed for compile
		case <-s.shutdown:
			// Graceful shutdown initiated, exit normally
			return nil
		case <-egCtx.Done():
			return egCtx.Err()
		}
	})

	close(s.started)

	log.Trace("waiting for all service goroutines to complete")
	err := eg.Wait()
	log.Debug("all service goroutines completed")

	s.stop(ctx)

	return err
}

func (s *Service) HandleShutdown(w http.ResponseWriter, r *http.Request) {
	// Trigger graceful service shutdown - this will handle stopping runners gracefully
	s.Shutdown()
	w.WriteHeader(http.StatusOK)
}

// Shutdown initiates graceful shutdown of the service (non-blocking)
func (s *Service) Shutdown() {
	log := s.logger.Sugar()
	log.Info("shutdown requested")

	// Use atomic CAS to ensure only one shutdown
	if !s.shutdownStarted.CompareAndSwap(false, true) {
		log.Trace("already shutting down")
		return
	}

	close(s.shutdown)
}

// stop performs final cleanup after shutdown
func (s *Service) stop(ctx context.Context) {
	log := s.logger.Sugar()
	log.Info("stopping service")

	select {
	case <-s.stopped:
		log.Trace("service already stopped")
	default:
		close(s.stopped)
	}
}

// IsStarted returns true if the service has been started
func (s *Service) IsStarted() bool {
	select {
	case <-s.started:
		return true
	default:
		return false
	}
}

// IsStopped returns true if the service has been stopped
func (s *Service) IsStopped() bool {
	select {
	case <-s.stopped:
		return true
	default:
		return false
	}
}

// IsRunning returns true if the service is running (started but not stopped)
func (s *Service) IsRunning() bool {
	return s.IsStarted() && !s.IsStopped()
}

// handleSignals handles SIGTERM in await-explicit-shutdown mode
func (s *Service) handleSignals(ctx context.Context) error {
	log := s.logger.Sugar()
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM)

	select {
	case <-s.shutdown:
		return nil
	case <-ctx.Done():
		return nil
	case <-ch:
		log.Info("received SIGTERM, starting graceful shutdown")
		s.Shutdown()
		return nil
	}
}
