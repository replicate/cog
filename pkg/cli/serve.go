package cli

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/image"
	"github.com/replicate/cog/pkg/predict"
	"github.com/replicate/cog/pkg/util/console"
)

var (
	// inputFlags []string
	// outPath    string
	serveHost      = "0.0.0.0"
	servePort      = 5000
	servePredictor *predict.Predictor
)

func newServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve [image]",
		Short: "Serve a model",
		Long: `Serve a model using docker.

If 'image' is passed, it will serve the model from on that Docker image.
It must be an image that has been built by Cog.

Otherwise, it will build the model in the current directory and serve
the model on that.`,
		RunE:       cmdServe,
		Args:       cobra.MaximumNArgs(1),
		SuggestFor: []string{"infer"},
	}
	addBuildProgressOutputFlag(cmd)
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "Output path")
	cmd.Flags().IntVarP(&servePort, "port", "p", 5000, "Port to serve on")
	cmd.Flags().StringVarP(&serveHost, "host", "H", "0.0.0.0", "Host to listen on")

	return cmd
}

// FIXME(ja): 99% of this is copied from cmdPredict
func cmdServe(cmd *cobra.Command, args []string) error {
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

	predictor := predict.NewPredictor(docker.RunOptions{
		GPUs:    gpus,
		Image:   imageName,
		Volumes: volumes,
	})
	if err := predictor.Start(os.Stderr); err != nil {
		return err
	}

	servePredictor = &predictor

	reallyServeHTTP()

	return nil
}

// FIXME(ja): this pattern might be useful in predict commands
func serveSignalHandler(signal os.Signal) {
	fmt.Printf("\nCaught signal: %+v\n", signal)

	if err := servePredictor.Stop(); err != nil {
		console.Warnf("Failed to stop container: %s", err)
	}
	os.Exit(0)
}

func initServeSignals() {
	captureSignal := make(chan os.Signal, 1)
	signal.Notify(captureSignal, syscall.SIGINT)
	serveSignalHandler(<-captureSignal)
}

func reallyServeHTTP() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {

		// always say yes to CORS
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		base := servePredictor.GetUrl("")
		u, err := url.Parse(base + r.URL.Path)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		proxy := &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				req.URL = u
				req.Host = u.Host
				req.Header.Set("X-Forwarded-Host", req.Header.Get("Host"))
				req.Header.Set("Host", u.Host)
			},
		}
		proxy.ServeHTTP(w, r)
	})

	go initServeSignals()

	listenAddr := fmt.Sprintf("%s:%d", serveHost, servePort)

	console.Info("")
	console.Infof("Serving model on %s ...", listenAddr)

	err := http.ListenAndServe(listenAddr, nil)
	if err != nil {
		console.Warnf("Failed to start server: %s", err)
	}
}
