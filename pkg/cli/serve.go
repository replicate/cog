package cli

import (
	"embed"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"tailscale.com/tsnet"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/image"
	"github.com/replicate/cog/pkg/util/console"
)

//go:embed form/*
var content embed.FS

var tailscale string
var proxyPort int

func newServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve [image]",
		Short: "Serve cog from within a Docker container",
		RunE:  serve,
		Args:  cobra.MaximumNArgs(1),
	}
	addBuildProgressOutputFlag(cmd)
	addDockerfileFlag(cmd)
	addUseCudaBaseImageFlag(cmd)
	addGpusFlag(cmd)

	flags := cmd.Flags()
	// Flags after first argment are considered args and passed to command

	// This is called `publish` for consistency with `docker run`
	cmd.Flags().StringArrayVarP(&runPorts, "publish", "p", []string{"5000"}, "Publish a container's port to the host, e.g. -p 8000")
	cmd.Flags().StringArrayVarP(&envFlags, "env", "e", []string{}, "Environment variables, in the form name=value")
	cmd.Flags().StringVarP(&tailscale, "tailscale", "t", "", "Use Tailscale name expose funnel...")

	flags.SetInterspersed(false)

	return cmd
}

func serve(cmd *cobra.Command, args []string) error {
	imageName := ""
	volumes := []docker.Volume{}
	gpus := gpusFlag

	if len(args) == 0 {
		// Build image

		cfg, projectDir, err := config.GetConfig(projectDirFlag)
		if err != nil {
			return err
		}

		imageName, err = image.BuildBase(cfg, projectDir, buildUseCudaBaseImage, buildProgressOutput)
		if err != nil {
			return err
		}
		volumes = append(volumes, docker.Volume{Source: projectDir, Destination: "/src"})

		gpus = ""
		if gpusFlag != "" {
			gpus = gpusFlag
		} else if cfg.Build.GPU {
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
		if gpus == "" && conf.Build.GPU {
			gpus = "all"
		}

		args = args[1:]
	}

	runOptions := docker.RunOptions{
		Args:    args,
		Env:     envFlags,
		GPUs:    gpus,
		Image:   imageName,
		Volumes: volumes,
		Workdir: "/src",
	}

	for _, portString := range runPorts {
		port, err := strconv.Atoi(portString)
		if err != nil {
			return err
		}

		runOptions.Ports = append(runOptions.Ports, docker.Port{HostPort: port, ContainerPort: 5000})
	}

	console.Info("")
	console.Infof("Running '%s' in Docker with the current directory mounted as a volume...", strings.Join(args, " "))

	go func() {
		handler, err := buildHandler(fmt.Sprintf("http://localhost:%s", runPorts[0]))
		if err != nil {
			return
		}
		fmt.Println("Proxy to listen on port", 8080)

		if tailscale != "" {
			s := &tsnet.Server{Hostname: tailscale}
			defer s.Close()

			ln, err := s.ListenFunnel("tcp", ":443") // does TLS
			if err != nil {
				log.Fatal(err)
			}
			defer ln.Close()

			log.Fatal(http.Serve(ln, handler))
		}

		err = proxy(8080, handler)
		if err != nil {
			console.Error(err.Error())
		}
	}()

	fmt.Println(runOptions)
	err := docker.Run(runOptions)
	// Only retry if we're using a GPU but but the user didn't explicitly select a GPU with --gpus
	// If the user specified the wrong GPU, they are explicitly selecting a GPU and they'll want to hear about it
	if runOptions.GPUs == "all" && err == docker.ErrMissingDeviceDriver {
		console.Info("Missing device driver, re-trying without GPU")

		runOptions.GPUs = ""
		err = docker.Run(runOptions)
	}

	return err
}

func proxy(listenPort int, handler http.Handler) error {
	fmt.Println("Proxy is listening on port", listenPort)
	http.Handle("/", handler)
	return http.ListenAndServe(":"+strconv.Itoa(listenPort), nil)
}

func buildHandler(targetHost string) (http.Handler, error) {
	targetURL, err := url.Parse(targetHost)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Print("Request: ")
		fmt.Print("Request: ")
		fmt.Print("Request: ")
		fmt.Print("Request: ")
		fmt.Println(r.URL.Path)
		if strings.HasPrefix(r.URL.Path, "/form") {
			http.FileServer(http.FS(content)).ServeHTTP(w, r)
		} else {
			proxy.ServeHTTP(w, r)
		}
	}), nil
}
