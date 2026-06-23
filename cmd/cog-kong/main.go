package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/alecthomas/kong"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/console"
)

// Build-time variables. Initialized from global defaults; overridden by -ldflags at build time.
var (
	version   = global.Version
	commit    = global.Commit
	buildTime = global.BuildTime
)

// CLI is the root command struct. Kong parses into this.
type CLI struct {
	Globals

	Build BuildCmd `cmd:"" help:"Build an image from cog.yaml."`
	Push  PushCmd  `cmd:"" help:"Build and push model in current directory to a Docker registry."`
}

func main() {
	ctx, cancel := newCancellationContext()

	var cli CLI

	initOpts := []kong.Option{
		// CLI metadata and variable interpolation for struct tags
		kong.Name("cog"),
		kong.Description("Containers for machine learning."),
		kong.Vars{
			"version":          fmt.Sprintf("cog version %s (built %s)", version, buildTime),
			"commit":           commit,
			"progress_default": progressDefault(),
			"registry_default": global.DefaultReplicateRegistryHost,
		},
		kong.UsageOnError(),

		// bindings for lazily injecting dependencies into Run() methods
		kong.BindTo(ctx, (*context.Context)(nil)),
		kong.BindSingletonProvider(provideDockerClient),
		kong.BindToProvider(provideRegistryClient),
		kong.BindSingletonProvider(provideProviderRegistry),
	}

	parser, err := kong.New(&cli, initOpts...)
	if err != nil {
		// Fatal error creating the parser — this is a bug, so panic to get a stack trace.
		panic(err)
	}

	kctx, err := parser.Parse(os.Args[1:])

	// Unable to parse input to a valid command
	if err != nil {
		// If the command isn't runnable (i.e. `cog`) just print help and exit 0 (matches Cobra behavior).
		var parseErr *kong.ParseError
		// Exit code 80 is kong's internal code for "no runnable command selected" (e.g. bare `cog` with no subcommand).
		if errors.As(err, &parseErr) && parseErr.ExitCode() == 80 && strings.HasPrefix(parseErr.Error(), "expected") {
			_ = parseErr.Context.PrintUsage(false)
			return
		}

		// otherwise it's a real parse error (e.g. unexpected command or flag), so print the error and exit non-zero.
		parser.FatalIfErrorf(err)
	}

	err = kctx.Run()
	cancel()
	// command returned an error. Print and exit non-zero.
	if err != nil {
		parser.FatalIfErrorf(err)
	}
}

func newCancellationContext() (context.Context, context.CancelFunc) {
	// First signal cancels the context, giving commands a chance to clean up.
	// Second signal force-exits immediately.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	go func() {
		// Block until the first signal cancels the context.
		<-ctx.Done()

		// Now register for the second signal after the first one has been received.
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

		console.Debugf("Shutting down. Signal again to force quit.")

		<-sig
		console.Warnf("Forced exit")
		os.Exit(1)
	}()

	return ctx, cancel
}
