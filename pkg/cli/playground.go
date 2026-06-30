package cli

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/util/console"
)

//go:embed playground-ui
var playgroundUI embed.FS

// maxWebhookBody caps a single webhook payload relayed to the browser.
const maxWebhookBody = 10 * 1024 * 1024

var (
	playgroundPort        = 0
	playgroundTarget      = "http://localhost:8393"
	playgroundHost        = "127.0.0.1"
	playgroundWebhookHost = "host.docker.internal"
	playgroundNoOpen      = false
)

func newPlaygroundCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "playground",
		Short: "Open a browser playground for talking to a running model",
		Long: `Open a browser playground for talking to a running model.

Starts a local web server that serves a schema-driven UI (a Postman-like tool
for Cog models). Point it at any running Cog HTTP API -- for example one started
with 'cog serve' -- and the playground reflects that model's inputs and outputs
from its OpenAPI schema in real time.

Requests are reverse-proxied through this server, so the target API does not
need to set CORS headers. The server also hosts a webhook sink so async
predictions can be observed in the browser.

Async/webhook testing against a containerized model requires the webhook URL to
be reachable from inside the container. On Docker Desktop the default
'host.docker.internal' works once the server listens on a reachable interface
(e.g. --host 0.0.0.0).`,
		Example: `  # Start a model API in one terminal
  cog serve -p 8393

  # Open the playground pointing at it
  cog playground --target http://localhost:8393`,
		RunE:       cmdPlayground,
		Args:       cobra.MaximumNArgs(0),
		SuggestFor: []string{"ui", "gui"},
	}

	cmd.Flags().IntVarP(&playgroundPort, "port", "p", playgroundPort, "Port to listen on (0 picks a free port)")
	cmd.Flags().StringVar(&playgroundTarget, "target", playgroundTarget, "Default target model API URL")
	cmd.Flags().StringVar(&playgroundHost, "host", playgroundHost, "Address to bind (use 0.0.0.0 to receive webhooks from containers)")
	cmd.Flags().StringVar(&playgroundWebhookHost, "webhook-host", playgroundWebhookHost, "Hostname the model uses to reach this server for webhooks")
	cmd.Flags().BoolVar(&playgroundNoOpen, "no-open", playgroundNoOpen, "Do not open the browser automatically")

	return cmd
}

// playgroundServer holds the runtime state for a playground instance.
type playgroundServer struct {
	hub         *eventHub
	webhookBase string
}

func cmdPlayground(cmd *cobra.Command, _ []string) error {
	uiFS, err := fs.Sub(playgroundUI, "playground-ui")
	if err != nil {
		return fmt.Errorf("loading playground assets: %w", err)
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", playgroundHost, playgroundPort))
	if err != nil {
		return fmt.Errorf("starting playground server: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	srvState := &playgroundServer{
		hub:         newEventHub(),
		webhookBase: fmt.Sprintf("http://%s:%d", playgroundWebhookHost, port),
	}

	mux := srvState.routes(uiFS)

	browserHost := playgroundHost
	if browserHost == "0.0.0.0" || browserHost == "" {
		browserHost = "127.0.0.1"
	}
	uiURL := fmt.Sprintf("http://%s:%d/?target=%s", browserHost, port, url.QueryEscape(playgroundTarget))
	console.Infof("Cog playground running at %s", uiURL)
	console.Info("Press Ctrl+C to stop.")
	if !playgroundNoOpen {
		maybeOpenBrowser(uiURL)
	}

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Shut down gracefully when the command's context is canceled (Ctrl+C).
	go func() {
		<-cmd.Context().Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// routes builds the HTTP handler: static UI, the reverse proxy, and the webhook
// sink + event relay.
func (s *playgroundServer) routes(uiFS fs.FS) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServerFS(uiFS))
	mux.HandleFunc("/proxy/", handlePlaygroundProxy)
	mux.HandleFunc("/webhook/", s.handleWebhook)
	mux.HandleFunc("/events", s.handleEvents)
	mux.HandleFunc("/config", s.handleConfig)
	return mux
}

// handleConfig reports runtime configuration the UI needs, notably the webhook
// base URL the model should call back on.
func (s *playgroundServer) handleConfig(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"webhookBase": s.webhookBase})
}

// handleWebhook receives a webhook delivery from a model and relays its body to
// any browser subscribed to the matching token's event stream.
func (s *playgroundServer) handleWebhook(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/webhook/")
	if token == "" {
		http.Error(w, "missing token", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		http.Error(w, "cannot read webhook body", http.StatusBadRequest)
		return
	}
	s.hub.publish(token, body)
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprint(w, "{}")
}

// handleEvents streams relayed webhook payloads to the browser over SSE.
func (s *playgroundServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Subscribe before flushing headers so the caller is guaranteed to be
	// receiving by the time it observes the response (no missed events).
	ch := s.hub.subscribe(token)
	defer s.hub.unsubscribe(token, ch)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			_, _ = fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-keepAlive.C:
			_, _ = fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		}
	}
}

// handlePlaygroundProxy reverse-proxies /proxy/* to the target model API. The
// target origin is taken from the X-Cog-Target header (set by fetch requests)
// or the cog_target query parameter (used for plain navigations like the schema
// link). Proxying keeps the browser same-origin, sidestepping CORS, and streams
// SSE responses through unbuffered.
func handlePlaygroundProxy(w http.ResponseWriter, r *http.Request) {
	rawTarget := r.Header.Get("X-Cog-Target")
	if rawTarget == "" {
		rawTarget = r.URL.Query().Get("cog_target")
	}
	if rawTarget == "" {
		writeProxyError(w, http.StatusBadRequest, "no target API set")
		return
	}

	target, err := url.Parse(strings.TrimRight(rawTarget, "/"))
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		writeProxyError(w, http.StatusBadRequest, "invalid target API URL")
		return
	}

	proxy := &httputil.ReverseProxy{
		FlushInterval: -1, // flush immediately so SSE streams in real time
		Rewrite: func(pr *httputil.ProxyRequest) {
			// Forward the path after /proxy, dropping the cog_target hint.
			path := strings.TrimPrefix(pr.In.URL.Path, "/proxy")
			if path == "" {
				path = "/"
			}
			pr.Out.URL.Path = path
			query := pr.Out.URL.Query()
			query.Del("cog_target")
			pr.Out.URL.RawQuery = query.Encode()
			pr.SetURL(target)
			pr.Out.Host = target.Host
			pr.Out.Header.Del("X-Cog-Target")
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeProxyError(w, http.StatusBadGateway, "cannot reach target API: "+err.Error())
		},
	}
	// The proxy target is user-specified by design (a local model API); SSRF to
	// it is the intended behavior of this dev tool, not a vulnerability.
	proxy.ServeHTTP(w, r) //nolint:gosec // user-directed proxy target is intentional
}

func writeProxyError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// message is server-controlled text; encode minimally as a JSON string.
	_, _ = fmt.Fprintf(w, `{"error":%q}`, message)
}

// maybeOpenBrowser best-effort opens a URL in the default browser.
func maybeOpenBrowser(target string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	_ = cmd.Start()
}

// eventHub fans out relayed webhook payloads to browser SSE subscribers keyed
// by an opaque token.
type eventHub struct {
	mu   sync.Mutex
	subs map[string]map[chan []byte]struct{}
}

func newEventHub() *eventHub {
	return &eventHub{subs: make(map[string]map[chan []byte]struct{})}
}

func (h *eventHub) subscribe(token string) chan []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan []byte, 32)
	if h.subs[token] == nil {
		h.subs[token] = make(map[chan []byte]struct{})
	}
	h.subs[token][ch] = struct{}{}
	return ch
}

func (h *eventHub) unsubscribe(token string, ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs := h.subs[token]
	if subs == nil {
		return
	}
	delete(subs, ch)
	close(ch)
	if len(subs) == 0 {
		delete(h.subs, token)
	}
}

func (h *eventHub) publish(token string, msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[token] {
		// Non-blocking: drop for a slow subscriber rather than stall the model.
		select {
		case ch <- msg:
		default:
		}
	}
}
