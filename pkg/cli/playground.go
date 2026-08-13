package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/console"
)

const (
	// maxWebhookBody caps a single webhook payload relayed to the browser.
	maxWebhookBody = 10 * 1024 * 1024
	// maxPlaygroundHeaderMetadata keeps inspector metadata below browser header limits.
	maxPlaygroundHeaderMetadata = 32 * 1024
	// maxConcurrentWebhooks bounds memory and connection use from model callbacks.
	maxConcurrentWebhooks = 16
	// playgroundUpstreamHeaders identifies model response headers for the request inspector.
	playgroundUpstreamHeaders = "X-Cog-Upstream-Headers"
)

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
	hub           *eventHub
	webhookBase   string
	defaultTarget string
	webhookSlots  chan struct{}
}

// newPlaygroundServer builds a playground server with an initialized event hub.
func newPlaygroundServer(webhookBase, defaultTarget string) *playgroundServer {
	return &playgroundServer{
		hub:           newEventHub(),
		webhookBase:   webhookBase,
		defaultTarget: defaultTarget,
		webhookSlots:  make(chan struct{}, maxConcurrentWebhooks),
	}
}

// playgroundConfig holds the runtime settings for a playground instance.
type playgroundConfig struct {
	host        string
	port        int
	target      string
	webhookHost string
}

func cmdPlayground(cmd *cobra.Command, _ []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := playgroundConfig{
		host:        playgroundHost,
		port:        playgroundPort,
		target:      playgroundTarget,
		webhookHost: playgroundWebhookHost,
	}

	uiURL, srv, ln, err := startPlayground(ctx, cfg)
	if err != nil {
		return err
	}

	console.Infof("Cog playground running at %s", uiURL)
	console.Info("Press Ctrl+C to stop.")
	if !playgroundNoOpen {
		maybeOpenBrowser(uiURL)
	}

	return servePlayground(ctx, srv, ln, 5*time.Second)
}

// startPlayground binds and configures a playground server, returning its UI
// URL plus the started server and listener. The caller owns serving via
// servePlayground; binding happens here so a bind failure surfaces as a return
// error rather than a background failure.
func startPlayground(ctx context.Context, cfg playgroundConfig) (string, *http.Server, net.Listener, error) {
	uiFS, err := fs.Sub(playgroundUI, "playground")
	if err != nil {
		return "", nil, nil, fmt.Errorf("loading playground assets: %w", err)
	}

	ln, err := net.Listen("tcp", playgroundAddress(cfg.host, cfg.port))
	if err != nil {
		return "", nil, nil, fmt.Errorf("starting playground server: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	srvState := newPlaygroundServer(
		"http://"+playgroundAddress(cfg.webhookHost, port),
		cfg.target,
	)

	srv := &http.Server{
		Handler:           srvState.routes(uiFS),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 * 1024,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	browserHost := playgroundBrowserHost(cfg.host)
	uiURL := "http://" + playgroundAddress(browserHost, port) + "/"
	return uiURL, srv, ln, nil
}

func servePlayground(ctx context.Context, srv *http.Server, ln net.Listener, timeout time.Duration) error {
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			_ = srv.Close()
			<-serveErr
			return fmt.Errorf("shutting down playground server: %w", err)
		}
		if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func playgroundAddress(host string, port int) string {
	return net.JoinHostPort(normalizePlaygroundHost(host), strconv.Itoa(port))
}

func playgroundBrowserHost(host string) string {
	host = normalizePlaygroundHost(host)
	if host == "" {
		return "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsUnspecified() {
		return host
	}
	if ip.To4() == nil {
		return "::1"
	}
	return "127.0.0.1"
}

func normalizePlaygroundHost(host string) string {
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return host[1 : len(host)-1]
	}
	return host
}

// routes builds the HTTP handler: static UI, the reverse proxy, and the webhook
// sink + event relay.
func (s *playgroundServer) routes(uiFS fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServerFS(uiFS))
	mux.HandleFunc("/proxy/", handlePlaygroundProxy)
	mux.HandleFunc("/webhook/", s.handleWebhook)
	mux.HandleFunc("/events", s.handleEvents)
	mux.HandleFunc("/config", s.handleConfig)
	return protectPlayground(mux)
}

// protectPlayground keeps the UI and user-directed proxy private even when the
// server listens on 0.0.0.0 for container webhook callbacks. Remote webhook
// deliveries are the only requests exempt from the browser-origin checks.
func protectPlayground(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setPlaygroundSecurityHeaders(w, r.URL.Path)
		if strings.HasPrefix(r.URL.Path, "/webhook/") {
			next.ServeHTTP(w, r)
			return
		}
		if !isLoopbackRemote(r.RemoteAddr) || !isLoopbackHost(r.Host) || isCrossSiteRequest(r) {
			http.Error(w, "playground UI and proxy are only available from this browser", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func setPlaygroundSecurityHeaders(w http.ResponseWriter, path string) {
	csp := "default-src 'self'; connect-src 'self'; img-src 'self' data: http: https:; media-src 'self' data: http: https:; script-src 'self'; style-src 'self' 'unsafe-inline'; worker-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
	if strings.HasPrefix(path, "/assets/validation.worker-") && strings.HasSuffix(path, ".js") {
		// Ajv compiles dynamic model schemas. Confine the required code generation
		// to this network-isolated worker rather than relaxing the page policy.
		csp = "default-src 'none'; script-src 'self' 'unsafe-eval'; connect-src 'none'; base-uri 'none'; form-action 'none'"
	}
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
}

func isLoopbackHost(hostport string) bool {
	host := hostport
	if parsed, _, err := net.SplitHostPort(hostport); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isCrossSiteRequest(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	return err != nil || !strings.EqualFold(parsed.Host, r.Host) || (parsed.Scheme != "http" && parsed.Scheme != "https")
}

func isLoopbackRemote(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// handleConfig reports runtime configuration the UI needs, notably the webhook
// base URL the model should call back on.
func (s *playgroundServer) handleConfig(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"target":      s.defaultTarget,
		"webhookBase": s.webhookBase,
		"cogVersion":  global.Version,
	})
}

// handleWebhook receives a webhook delivery from a model and relays its body to
// any browser subscribed to the matching token's event stream.
func (s *playgroundServer) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/webhook/")
	if token == "" || strings.Contains(token, "/") || len(token) > 128 {
		http.Error(w, "missing token", http.StatusNotFound)
		return
	}
	if !s.hub.hasSubscribers(token) {
		http.Error(w, "no browser is ready to receive this webhook", http.StatusServiceUnavailable)
		return
	}
	select {
	case s.webhookSlots <- struct{}{}:
		defer func() { <-s.webhookSlots }()
	default:
		http.Error(w, "too many concurrent webhooks", http.StatusTooManyRequests)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			http.Error(w, "webhook body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "cannot read webhook body", http.StatusBadRequest)
		return
	}
	if !s.hub.publish(token, body) {
		http.Error(w, "no browser is ready to receive this webhook", http.StatusServiceUnavailable)
		return
	}
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
			writeSSEData(w, msg)
			flusher.Flush()
		case <-keepAlive.C:
			_, _ = fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		}
	}
}

// writeSSEData emits a payload as a single SSE event, prefixing every line with
// "data: ". This preserves embedded newlines without letting them terminate the
// event early or inject additional SSE fields (e.g. a spoofed "event:" line).
func writeSSEData(w io.Writer, msg []byte) {
	normalized := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(string(msg))
	for line := range strings.SplitSeq(normalized, "\n") {
		_, _ = fmt.Fprintf(w, "data: %s\n", line)
	}
	_, _ = fmt.Fprint(w, "\n")
}

// handlePlaygroundProxy reverse-proxies /proxy/* to the target model API. The
// target origin is taken from the X-Cog-Target header set by the playground UI.
// Proxying keeps the browser same-origin, sidestepping CORS, and streams SSE
// responses through unbuffered.
func handlePlaygroundProxy(w http.ResponseWriter, r *http.Request) {
	rawTarget := r.Header.Get("X-Cog-Target")
	if rawTarget == "" {
		writeProxyError(w, http.StatusBadRequest, "no target API set")
		return
	}

	target, err := url.Parse(strings.TrimRight(rawTarget, "/"))
	if err != nil || target.Host == "" || target.User != nil || target.Fragment != "" || (target.Scheme != "http" && target.Scheme != "https") {
		writeProxyError(w, http.StatusBadRequest, "invalid target API URL")
		return
	}

	proxy := &httputil.ReverseProxy{
		FlushInterval: -1, // flush immediately so SSE streams in real time
		Rewrite: func(pr *httputil.ProxyRequest) {
			// Forward the path after /proxy.
			escapedPath := strings.TrimPrefix(pr.In.URL.EscapedPath(), "/proxy")
			if escapedPath == "" {
				escapedPath = "/"
			}
			path, err := url.PathUnescape(escapedPath)
			if err != nil {
				path = strings.TrimPrefix(pr.In.URL.Path, "/proxy")
			}
			pr.Out.URL.Path = path
			pr.Out.URL.RawPath = escapedPath
			pr.SetURL(target)
			pr.Out.Host = target.Host
			pr.Out.Header.Del("X-Cog-Target")
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Del("Origin")
			pr.Out.Header.Del("Referer")
		},
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del("Clear-Site-Data")
			resp.Header.Del("Service-Worker-Allowed")
			resp.Header.Del("Set-Cookie")
			// Drop redirects so browser URL normalization cannot pivot off the
			// intended target (fetch uses redirect: "manual" as a second line).
			if resp.StatusCode >= 300 && resp.StatusCode < 400 {
				resp.Header.Del("Location")
			}
			upstreamHeaders := resp.Header.Clone()
			upstreamHeaders.Del(playgroundUpstreamHeaders)
			resp.Header.Del(playgroundUpstreamHeaders)
			encodedHeaders, err := json.Marshal(upstreamHeaders)
			if err != nil {
				return fmt.Errorf("encode upstream response headers: %w", err)
			}
			metadata := base64.RawURLEncoding.EncodeToString(encodedHeaders)
			if len(metadata) <= maxPlaygroundHeaderMetadata {
				resp.Header.Set(playgroundUpstreamHeaders, metadata)
			}
			resp.Header.Set("Cache-Control", "no-store")
			return nil
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
	w.Header().Set("Cache-Control", "no-store")
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
	if err := cmd.Start(); err == nil {
		go func() { _ = cmd.Wait() }()
	}
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
	ch := make(chan []byte, 4)
	if h.subs[token] == nil {
		h.subs[token] = make(map[chan []byte]struct{})
	}
	h.subs[token][ch] = struct{}{}
	return ch
}

func (h *eventHub) hasSubscribers(token string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[token]) > 0
}

func (h *eventHub) unsubscribe(token string, ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs := h.subs[token]
	if subs == nil {
		return
	}
	delete(subs, ch)
	if len(subs) == 0 {
		delete(h.subs, token)
	}
}

// publish delivers msg to every subscriber of token, best-effort. It reports
// whether at least one subscriber received it. Delivery is non-blocking: a
// subscriber whose buffer is momentarily full is skipped rather than stalling
// the webhook. The depth-buffered channels absorb any realistic burst for a
// single-user tool.
func (h *eventHub) publish(token string, msg []byte) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	delivered := false
	for ch := range h.subs[token] {
		select {
		case ch <- msg:
			delivered = true
		default:
		}
	}
	return delivered
}
