package cli

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/util/console"
)

var (
	serveHost        = "0.0.0.0"
	servePort        = 5000
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

	if err := buildOrLoadPredictor(args); err != nil {
		return err
	}

	go catchSIGINT()
	defer stopPredictor()

	return reallyServeHTTP()
}

func predictHandler(w http.ResponseWriter, r *http.Request) {

	base := predictor.GetURL("")
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

	mux := http.NewServeMux()

	if serveStatic != "" {
		console.Infof("Serving static files from %s", serveStatic)
		mux.HandleFunc("/predictions", predictHandler)
		mux.HandleFunc("/docs", predictHandler)
		mux.HandleFunc("/openapi.json", predictHandler)
		mux.Handle("/", http.FileServer(http.Dir(serveStatic)))
	} else {
		mux.HandleFunc("/", predictHandler)
	}

	listenAddr := fmt.Sprintf("%s:%d", serveHost, servePort)

	if serveDisableCors {
		console.Info("CORS is disabled")
		console.Infof("Serving model on %s ...", listenAddr)
		return http.ListenAndServe(listenAddr, CORS(mux))
	}

	console.Infof("Serving model on %s ...", listenAddr)
	return http.ListenAndServe(listenAddr, mux)
}

func CORS(m *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == "OPTIONS" {
			w.Header().Set("Access-Control-Allow-Methods", "*")
			w.Header().Set("Access-Control-Allow-Headers", "*")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			return
		} else {
			m.ServeHTTP(w, r)
		}
	})
}
