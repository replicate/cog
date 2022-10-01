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

	"github.com/replicate/cog/pkg/predict"
	"github.com/replicate/cog/pkg/util/console"
)

var (
	serveHost        = "0.0.0.0"
	servePort        = 5000
	servePredictor   *predict.Predictor
	serveDisableCors = false
	serveStatic      string
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
	cmd.Flags().IntVarP(&servePort, "port", "p", 5000, "Port to serve on")
	cmd.Flags().StringVarP(&serveHost, "host", "H", "0.0.0.0", "Host to listen on")
	cmd.Flags().BoolVar(&serveDisableCors, "disable-cors", false, "Disable CORS allows SPA to run on different host")
	cmd.Flags().StringVar(&serveStatic, "static", "", "Serve static files (SPA) from given directory")
	return cmd
}

func cmdServe(cmd *cobra.Command, args []string) error {
	var err error

	servePredictor, err = buildOrLoadPredictor(args)

	if err != nil {
		return err
	}
	defer func() {
		console.Debugf("Stopping container...")
		if err := servePredictor.Stop(); err != nil {
			console.Warnf("Failed to stop container: %s", err)
		}
	}()

	return reallyServeHTTP()
}

// FIXME(ja): this pattern might be useful in predict commands
func serveSignalHandler(signal os.Signal) {
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

func predictHandler(w http.ResponseWriter, r *http.Request) {

	if serveDisableCors {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	base := servePredictor.GetURL("")
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
}

func reallyServeHTTP() error {
	console.Info("")

	if serveDisableCors {
		console.Info("CORS is disabled")
	}

	mux := http.NewServeMux()

	if serveStatic != "" {
		mux.HandleFunc("/predictions", predictHandler)
		mux.HandleFunc("/docs", predictHandler)
		mux.HandleFunc("/openapi.json", predictHandler)
		mux.Handle("/", http.FileServer(http.Dir(serveStatic)))
	} else {
		mux.HandleFunc("/", predictHandler)
	}

	go initServeSignals()

	listenAddr := fmt.Sprintf("%s:%d", serveHost, servePort)
	console.Infof("Serving model on %s ...", listenAddr)

	return http.ListenAndServe(listenAddr, mux)
}
