package kong

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/predict"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

// TrainCmd implements `cog train [image]` (hidden).
type TrainCmd struct {
	Image string `arg:"" optional:"" help:"Docker image to train on"`

	Input  []string `help:"Inputs, in the form name=value" short:"i" name:"input"`
	Env    []string `help:"Environment variables, in the form name=value" short:"e"`
	Output string   `help:"Output path" short:"o" default:"weights"`

	GPUFlags   `embed:""`
	BuildFlags `embed:""`
}

func (c *TrainCmd) Run(g *Globals) error {
	ctx := contextFromGlobals(g)

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	imageName := ""
	volumes := []command.Volume{}
	gpus := c.GPUs

	resolver := model.NewResolver(dockerClient, registry.NewRegistryClient())

	if c.Image == "" {
		src, err := model.NewSource(c.ConfigFile)
		if err != nil {
			return err
		}
		m, err := resolver.BuildBase(ctx, src, c.BuildFlags.BuildBaseOptions())
		if err != nil {
			return err
		}
		imageName = m.ImageRef()
		volumes = append(volumes, command.Volume{Source: src.ProjectDir, Destination: "/src"})
		if gpus == "" && m.HasGPU() {
			gpus = "all"
		}
	} else {
		imageName = c.Image
		ref, err := model.ParseRef(imageName)
		if err != nil {
			return err
		}
		m, err := resolver.Pull(ctx, ref)
		if err != nil {
			return err
		}
		if gpus == "" && m.HasGPU() {
			gpus = "all"
		}
	}

	console.Info("")
	console.Infof("Starting Docker image %s...", imageName)

	predictor, err := predict.NewPredictor(ctx, command.RunOptions{
		GPUs:    gpus,
		Image:   imageName,
		Volumes: volumes,
		Env:     c.Env,
		Args:    []string{"python", "-m", "cog.server.http", "--x-mode", "train"},
	}, true, dockerClient)
	if err != nil {
		return err
	}

	go func() {
		captureSignal := make(chan os.Signal, 1)
		signal.Notify(captureSignal, syscall.SIGINT)
		<-captureSignal
		console.Info("Stopping container...")
		if err := predictor.Stop(ctx); err != nil {
			console.Warnf("Failed to stop container: %s", err)
		}
	}()

	const setupTimeout = 5 * time.Minute
	if err := predictor.Start(ctx, os.Stderr, setupTimeout); err != nil {
		return err
	}

	defer func() {
		console.Debugf("Stopping container...")
		if err := predictor.Stop(context.Background()); err != nil {
			console.Warnf("Failed to stop container: %s", err)
		}
	}()

	return predictIndividualInputs(*predictor, c.Input, c.Output, false, true)
}
