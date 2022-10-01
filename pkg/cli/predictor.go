package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/image"
	"github.com/replicate/cog/pkg/predict"
	"github.com/replicate/cog/pkg/util/console"
)

var (
	predictor *predict.Predictor
)

func buildOrLoadPredictor(args []string) error {
	imageName := ""
	volumes := []docker.Volume{}
	gpus := ""

	if len(args) == 0 {
		// Build image

		cfg, projectDir, err := config.GetConfig(projectDirFlag)
		if err != nil {
			return err
		}

		if imageName, err = image.BuildBase(cfg, projectDir, buildProgressOutput); err != nil {
			return err
		}

		// Base image doesn't have /src in it, so mount as volume
		volumes = append(volumes, docker.Volume{
			Source:      projectDir,
			Destination: "/src",
		})

		if cfg.Build.GPU {
			gpus = "all"
		}

	} else {
		// Use existing image
		imageName = args[0]

		exists, err := docker.ImageExists(imageName)
		if err != nil {
			return fmt.Errorf("Failed to determine if %s exists: %w", imageName, err)
		}
		if !exists {
			console.Infof("Pulling image: %s", imageName)
			if err := docker.Pull(imageName); err != nil {
				return fmt.Errorf("Failed to pull %s: %w", imageName, err)
			}
		}
		conf, err := image.GetConfig(imageName)
		if err != nil {
			return err
		}
		if conf.Build.GPU {
			gpus = "all"
		}
	}

	console.Info("")
	console.Infof("Starting Docker image %s and running setup()...", imageName)

	p := predict.NewPredictor(docker.RunOptions{
		GPUs:    gpus,
		Image:   imageName,
		Volumes: volumes,
	})

	predictor = &p

	if err := predictor.Start(os.Stderr); err != nil {
		return err
	}

	return nil
}

func stopPredictor() {
	if predictor != nil {
		console.Info("Stopping container...")
		if err := predictor.Stop(); err != nil {
			console.Warnf("Failed to stop container: %s", err)
		}
	}
}

func handleSignalHandler(signal os.Signal) {
	fmt.Println("Caught signal: ", signal)
	stopPredictor()
	os.Exit(0)
}

func catchSIGINT() {
	captureSignal := make(chan os.Signal, 1)
	signal.Notify(captureSignal, syscall.SIGINT)
	handleSignalHandler(<-captureSignal)
}
