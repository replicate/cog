package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/global"
)

func newTestPlayground(t *testing.T) *httptest.Server {
	t.Helper()
	uiFS, err := fs.Sub(playgroundUI, "playground")
	require.NoError(t, err)
	s := newPlaygroundServer("http://wh.example/cb", "http://localhost:8393")
	ts := httptest.NewServer(s.routes(uiFS))
	t.Cleanup(ts.Close)
	return ts
}

// echoServer reports the received path and (forwarded) query as JSON.
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"path":%q,"escaped_path":%q,"query":%q}`, r.URL.Path, r.URL.EscapedPath(), r.URL.RawQuery)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestPlaygroundServesUI(t *testing.T) {
	ts := newTestPlayground(t)

	resp, err := http.Get(ts.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), "Cog Playground")

	assets := regexp.MustCompile(`(?:href|src)="([^"]+)"`).FindAllStringSubmatch(string(body), -1)
	require.NotEmpty(t, assets)
	for _, asset := range assets {
		path := asset[1]
		assetResp, err := http.Get(ts.URL + path)
		require.NoError(t, err, "requesting %s", path)
		assetResp.Body.Close()
		assert.Equal(t, http.StatusOK, assetResp.StatusCode, "%s should be served", path)
		if strings.HasSuffix(path, ".css") {
			assert.Contains(t, assetResp.Header.Get("Content-Type"), "text/css")
		} else if strings.HasSuffix(path, ".js") {
			assert.Contains(t, assetResp.Header.Get("Content-Type"), "javascript")
		}
	}
}

func TestStartPlayground(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	uiURL, srv, ln, err := startPlayground(ctx, playgroundConfig{
		host:        "127.0.0.1",
		port:        0,
		target:      "http://localhost:8393",
		webhookHost: "host.docker.internal",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Close() })

	serveDone := make(chan error, 1)
	go func() { serveDone <- servePlayground(ctx, srv, ln, time.Second) }()

	u, err := url.Parse(uiURL)
	require.NoError(t, err)
	resp, err := http.Get(uiURL)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), "Cog Playground")
	assert.Equal(t, "127.0.0.1", u.Hostname())
	assert.NotEqual(t, "8393", u.Port(), "playground should not bind the default model port")

	cancel()
	require.NoError(t, <-serveDone)
}

func TestStartPlaygroundForContainerWebhooks(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	uiURL, srv, ln, err := startPlayground(ctx, playgroundConfig{
		host:        "0.0.0.0",
		port:        0,
		target:      "http://127.0.0.1:8393",
		webhookHost: "host.docker.internal",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Close() })

	serveDone := make(chan error, 1)
	go func() { serveDone <- servePlayground(ctx, srv, ln, time.Second) }()

	resp, err := http.Get(uiURL + "config")
	require.NoError(t, err)
	defer resp.Body.Close()

	var config map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&config))
	assert.Equal(t, "http://127.0.0.1:8393", config["target"])
	assert.Equal(t, "http://host.docker.internal:"+strconv.Itoa(ln.Addr().(*net.TCPAddr).Port), config["webhookBase"])

	cancel()
	require.NoError(t, <-serveDone)
}

func TestServePlaygroundWaitsForHandlers(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	started := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	srv := &http.Server{
		Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
			close(canceled)
			<-release
		}),
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- servePlayground(ctx, srv, ln, time.Second) }()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		resp, requestErr := http.Get("http://" + ln.Addr().String())
		if requestErr == nil {
			resp.Body.Close()
		}
	}()

	<-started
	cancel()
	<-canceled
	select {
	case err := <-serveDone:
		require.FailNow(t, "server returned before the active handler finished", "%v", err)
	default:
	}
	close(release)
	require.NoError(t, <-serveDone)
	<-requestDone
}

func TestPlaygroundConfig(t *testing.T) {
	ts := newTestPlayground(t)
	resp, err := http.Get(ts.URL + "/config")
	require.NoError(t, err)
	defer resp.Body.Close()

	var config map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&config))
	assert.Equal(t, "http://wh.example/cb", config["webhookBase"])
	assert.Equal(t, "http://localhost:8393", config["target"])
	assert.Equal(t, global.Version, config["cogVersion"])
}

func TestPlaygroundRejectsRemoteUIAndProxyRequests(t *testing.T) {
	uiFS, err := fs.Sub(playgroundUI, "playground")
	require.NoError(t, err)
	s := newPlaygroundServer("http://wh.example/cb", "")

	for _, path := range []string{"/", "/config", "/proxy/health-check"} {
		req := httptest.NewRequest(http.MethodGet, "http://playground.example"+path, nil)
		req.RemoteAddr = "192.0.2.10:1234"
		resp := httptest.NewRecorder()
		s.routes(uiFS).ServeHTTP(resp, req)
		assert.Equal(t, http.StatusForbidden, resp.Code, path)
	}
}

func TestPlaygroundAllowsRemoteWebhookRequests(t *testing.T) {
	uiFS, err := fs.Sub(playgroundUI, "playground")
	require.NoError(t, err)
	s := newPlaygroundServer("http://wh.example/cb", "")
	ch := s.hub.subscribe("token")
	defer s.hub.unsubscribe("token", ch)

	req := httptest.NewRequest(http.MethodPost, "http://playground.example/webhook/token", strings.NewReader(`{"status":"succeeded"}`))
	req.RemoteAddr = "192.0.2.10:1234"
	resp := httptest.NewRecorder()
	s.routes(uiFS).ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestPlaygroundProxyHeaderTarget(t *testing.T) {
	ts := newTestPlayground(t)
	stub := echoServer(t)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/proxy/openapi.json?foo=bar", nil)
	require.NoError(t, err)
	req.Header.Set("X-Cog-Target", stub.URL)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	var got map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, "/openapi.json", got["path"], "/proxy prefix should be stripped")
	assert.Equal(t, "foo=bar", got["query"])
}

func TestPlaygroundProxyRejectsQueryTarget(t *testing.T) {
	ts := newTestPlayground(t)
	stub := echoServer(t)

	u := ts.URL + "/proxy/health-check?x=1&cog_target=" + url.QueryEscape(stub.URL)
	resp, err := http.Get(u)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPlaygroundProxyPreservesEscapedPathSegments(t *testing.T) {
	ts := newTestPlayground(t)
	stub := echoServer(t)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/proxy/predictions/custom%2Fid/cancel", nil)
	require.NoError(t, err)
	req.Header.Set("X-Cog-Target", stub.URL+"/api")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	var got map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, "/api/predictions/custom/id/cancel", got["path"])
	assert.Equal(t, "/api/predictions/custom%2Fid/cancel", got["escaped_path"])
}

func TestPlaygroundProxyForwardsRequestAndResponse(t *testing.T) {
	ts := newTestPlayground(t)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/predictions/p1", r.URL.Path)
		assert.Equal(t, `{"input":{"prompt":"hello"}}`, string(body))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Empty(t, r.Header.Get("X-Cog-Target"))
		assert.Empty(t, r.Header.Get("Authorization"))
		assert.Empty(t, r.Header.Get("Cookie"))
		w.Header().Set("X-Upstream", "preserved")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Set-Cookie", "session=upstream")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"detail":"invalid input"}`))
	}))
	t.Cleanup(stub.Close)

	req, err := http.NewRequest(http.MethodPut, ts.URL+"/proxy/predictions/p1", bytes.NewBufferString(`{"input":{"prompt":"hello"}}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("Cookie", "session=local-secret")
	req.Header.Set("X-Cog-Target", stub.URL+"/api")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	assert.Equal(t, "preserved", resp.Header.Get("X-Upstream"))
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	assert.Empty(t, resp.Header.Get("Set-Cookie"))
	encodedHeaders, err := base64.RawURLEncoding.DecodeString(resp.Header.Get(playgroundUpstreamHeaders))
	require.NoError(t, err)
	var upstreamHeaders http.Header
	require.NoError(t, json.Unmarshal(encodedHeaders, &upstreamHeaders))
	assert.Equal(t, "preserved", upstreamHeaders.Get("X-Upstream"))
	assert.Equal(t, "SAMEORIGIN", upstreamHeaders.Get("X-Frame-Options"))
	assert.Equal(t, "public, max-age=3600", upstreamHeaders.Get("Cache-Control"))
	assert.Empty(t, upstreamHeaders.Get("Set-Cookie"))
	assert.JSONEq(t, `{"detail":"invalid input"}`, string(body))
}

func TestPlaygroundProxyRoutesConcurrentWorkspacesIndependently(t *testing.T) {
	ts := newTestPlayground(t)
	models := map[string]*httptest.Server{}
	for _, name := range []string{"first", "second"} {
		models[name] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, name)
		}))
		t.Cleanup(models[name].Close)
	}

	errs := make(chan error, 40)
	var requests sync.WaitGroup
	for name, model := range models {
		for range 20 {
			requests.Go(func() {
				req, err := http.NewRequest(http.MethodGet, ts.URL+"/proxy/identity", nil)
				if err != nil {
					errs <- err
					return
				}
				req.Header.Set("X-Cog-Target", model.URL)
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					errs <- err
					return
				}
				body, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if readErr != nil {
					errs <- readErr
					return
				}
				if string(body) != name {
					errs <- fmt.Errorf("request for %s reached %q", name, body)
				}
			})
		}
	}
	requests.Wait()
	close(errs)
	for err := range errs {
		assert.NoError(t, err)
	}
}

func TestPlaygroundProxyOmitsOversizedHeaderMetadata(t *testing.T) {
	ts := newTestPlayground(t)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Large", strings.Repeat("x", maxPlaygroundHeaderMetadata))
		w.Header().Set(playgroundUpstreamHeaders, "spoofed")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(stub.Close)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/proxy/health-check", nil)
	require.NoError(t, err)
	req.Header.Set("X-Cog-Target", stub.URL)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, strings.Repeat("x", maxPlaygroundHeaderMetadata), resp.Header.Get("X-Large"))
	assert.Empty(t, resp.Header.Get(playgroundUpstreamHeaders))
}

func TestPlaygroundProxyMissingTarget(t *testing.T) {
	ts := newTestPlayground(t)
	resp, err := http.Get(ts.URL + "/proxy/health-check")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
}

func TestPlaygroundProxyInvalidTarget(t *testing.T) {
	ts := newTestPlayground(t)
	for _, target := range []string{"ftp://example.com", "garbage", "", "http://user@example.com", "http://example.com/#fragment"} {
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/proxy/health-check", nil)
		require.NoError(t, err)
		if target != "" {
			req.Header.Set("X-Cog-Target", target)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "target %q should be rejected", target)
	}
}

func TestPlaygroundWebhookRejectsNonPost(t *testing.T) {
	ts := newTestPlayground(t)
	resp, err := http.Get(ts.URL + "/webhook/token")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
	assert.Equal(t, http.MethodPost, resp.Header.Get("Allow"))
}

func TestPlaygroundRecognizesIPv6Loopback(t *testing.T) {
	assert.True(t, isLoopbackRemote("[::1]:8080"))
	assert.False(t, isLoopbackRemote("not-an-address"))
	assert.True(t, isLoopbackHost("[::1]:8080"))
	assert.True(t, isLoopbackHost("localhost:8080"))
	assert.False(t, isLoopbackHost("playground.example:8080"))
}

func TestPlaygroundFormatsIPv6Address(t *testing.T) {
	assert.Equal(t, "[::1]:8080", playgroundAddress("::1", 8080))
	assert.Equal(t, "[::1]:8080", playgroundAddress("[::1]", 8080))
}

func TestPlaygroundUsesLoopbackBrowserHostForWildcard(t *testing.T) {
	assert.Equal(t, "127.0.0.1", playgroundBrowserHost("0.0.0.0"))
	assert.Equal(t, "::1", playgroundBrowserHost("::"))
	assert.Equal(t, "::1", playgroundBrowserHost("[::]"))
}

func TestPlaygroundRejectsCrossSiteBrowserRequests(t *testing.T) {
	ts := newTestPlayground(t)

	for name, mutate := range map[string]func(*http.Request){
		"host": func(req *http.Request) { req.Host = "attacker.example" },
		"origin": func(req *http.Request) {
			req.Header.Set("Origin", "https://attacker.example")
		},
		"fetch metadata": func(req *http.Request) {
			req.Header.Set("Sec-Fetch-Site", "cross-site")
		},
	} {
		t.Run(name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, ts.URL+"/config", nil)
			require.NoError(t, err)
			mutate(req)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		})
	}
}

func TestPlaygroundSetsBrowserSecurityHeaders(t *testing.T) {
	ts := newTestPlayground(t)
	resp, err := http.Get(ts.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Contains(t, resp.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'")
	assert.Contains(t, resp.Header.Get("Content-Security-Policy"), "script-src 'self'")
	assert.NotContains(t, resp.Header.Get("Content-Security-Policy"), "cdnjs.cloudflare.com")
	assert.NotContains(t, resp.Header.Get("Content-Security-Policy"), "'unsafe-eval'")
	assert.Equal(t, "no-referrer", resp.Header.Get("Referrer-Policy"))
	assert.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))

	recorder := httptest.NewRecorder()
	workers, err := fs.Glob(playgroundUI, "playground/assets/validation.worker-*.js")
	require.NoError(t, err)
	require.Len(t, workers, 1)
	request := httptest.NewRequest(http.MethodGet, "/"+strings.TrimPrefix(workers[0], "playground/"), nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Host = "127.0.0.1"
	protectPlayground(http.NotFoundHandler()).ServeHTTP(recorder, request)
	workerCSP := recorder.Header().Get("Content-Security-Policy")
	assert.Contains(t, workerCSP, "script-src 'self' 'unsafe-eval'")
	assert.Contains(t, workerCSP, "connect-src 'none'")
}

func TestPlaygroundProxyUnreachableTarget(t *testing.T) {
	ts := newTestPlayground(t)
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close() // now refuses connections

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/proxy/health-check", nil)
	require.NoError(t, err)
	req.Header.Set("X-Cog-Target", deadURL)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
}

func TestPlaygroundProxyStreamsSSE(t *testing.T) {
	ts := newTestPlayground(t)
	sse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "event: start\ndata: {\"a\":1}\n\n")
		w.(http.Flusher).Flush()
	}))
	t.Cleanup(sse.Close)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/proxy/predictions", nil)
	require.NoError(t, err)
	req.Header.Set("X-Cog-Target", sse.URL)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "event: start")
	assert.Contains(t, string(body), `data: {"a":1}`)
}

func TestPlaygroundWebhookRelay(t *testing.T) {
	ts := newTestPlayground(t)
	const token = "tok123"

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+"/events?token="+token, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	// The subscription is registered before headers are flushed, so by now the
	// hub has our channel; deliver a webhook and expect it relayed.
	got := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if line := scanner.Text(); strings.HasPrefix(line, "data: ") {
				got <- strings.TrimPrefix(line, "data: ")
				return
			}
		}
	}()

	whResp, err := http.Post(ts.URL+"/webhook/"+token, "application/json",
		strings.NewReader(`{"status":"succeeded","id":"p1"}`))
	require.NoError(t, err)
	whResp.Body.Close()
	assert.Equal(t, http.StatusOK, whResp.StatusCode)

	select {
	case data := <-got:
		assert.JSONEq(t, `{"status":"succeeded","id":"p1"}`, data)
	case <-time.After(2 * time.Second):
		require.FailNow(t, "timed out waiting for relayed webhook event")
	}
}

// A payload containing newlines must be framed as one SSE event with a
// "data: " prefix per line, not terminate the event early or inject fields.
func TestPlaygroundWebhookRelayPreservesNewlines(t *testing.T) {
	ts := newTestPlayground(t)
	const token = "tok-nl"

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+"/events?token="+token, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	got := make(chan []string, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		var data []string
		for scanner.Scan() {
			line := scanner.Text()
			if after, ok := strings.CutPrefix(line, "data: "); ok {
				data = append(data, after)
			} else if line == "" && len(data) > 0 {
				got <- data
				return
			}
		}
	}()

	whResp, err := http.Post(ts.URL+"/webhook/"+token, "application/json",
		strings.NewReader("{\"a\":1}\n{\"b\":2}"))
	require.NoError(t, err)
	whResp.Body.Close()

	select {
	case data := <-got:
		assert.Equal(t, []string{`{"a":1}`, `{"b":2}`}, data)
	case <-time.After(2 * time.Second):
		require.FailNow(t, "timed out waiting for relayed webhook event")
	}
}

func TestWriteSSEDataNormalizesLineEndings(t *testing.T) {
	for name, input := range map[string]string{
		"LF":   "first\nsecond",
		"CRLF": "first\r\nsecond",
		"CR":   "first\rsecond",
	} {
		t.Run(name, func(t *testing.T) {
			var output strings.Builder
			writeSSEData(&output, []byte(input))
			assert.Equal(t, "data: first\ndata: second\n\n", output.String())
		})
	}
}

func TestPlaygroundEventsMissingToken(t *testing.T) {
	ts := newTestPlayground(t)
	resp, err := http.Get(ts.URL + "/events")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPlaygroundWebhookMissingToken(t *testing.T) {
	ts := newTestPlayground(t)
	resp, err := http.Post(ts.URL+"/webhook/", "application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestPlaygroundWebhookRejectsOversizedBody(t *testing.T) {
	uiFS, err := fs.Sub(playgroundUI, "playground")
	require.NoError(t, err)
	// Keep a subscriber active so the handler reaches its body-size check.
	s := newPlaygroundServer("http://wh.example/cb", "")
	ch := s.hub.subscribe("token")
	defer s.hub.unsubscribe("token", ch)
	ts := httptest.NewServer(s.routes(uiFS))
	t.Cleanup(ts.Close)
	resp, err := http.Post(ts.URL+"/webhook/token", "application/json",
		strings.NewReader(strings.Repeat("x", maxWebhookBody+1)))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
}

func TestPlaygroundWebhookRejectsExcessConcurrency(t *testing.T) {
	s := newPlaygroundServer("http://wh.example/cb", "")
	ch := s.hub.subscribe("token")
	defer s.hub.unsubscribe("token", ch)
	for range maxConcurrentWebhooks {
		s.webhookSlots <- struct{}{}
	}

	req := httptest.NewRequest(http.MethodPost, "/webhook/token", strings.NewReader("{}"))
	recorder := httptest.NewRecorder()
	s.handleWebhook(recorder, req)

	assert.Equal(t, http.StatusTooManyRequests, recorder.Code)
}

func TestPlaygroundWebhookRejectsMissingSubscriber(t *testing.T) {
	ts := newTestPlayground(t)
	resp, err := http.Post(ts.URL+"/webhook/token", "application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestPlaygroundProxyStripsRedirectLocation(t *testing.T) {
	for name, location := range map[string]string{
		"absolute":          "http://169.254.169.254/latest/meta-data/",
		"protocol-relative": "//169.254.169.254/latest/meta-data/",
		"triple-slash":      "///169.254.169.254/latest/meta-data/",
		"backslash":         `\\169.254.169.254\latest\meta-data\`,
	} {
		t.Run(name, func(t *testing.T) {
			ts := newTestPlayground(t)
			stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", location)
				w.WriteHeader(http.StatusFound)
			}))
			t.Cleanup(stub.Close)

			req, err := http.NewRequest(http.MethodGet, ts.URL+"/proxy/health-check", nil)
			require.NoError(t, err)
			req.Header.Set("X-Cog-Target", stub.URL)
			// Do not follow redirects; we want the proxy response as the browser would with redirect:manual.
			client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			}}
			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusFound, resp.StatusCode)
			assert.Empty(t, resp.Header.Get("Location"))
		})
	}
}

// A stalled subscriber (full buffer) must not block or deny delivery to a
// healthy one: publish is best-effort and non-blocking.
func TestEventHubPublishDeliversDespiteStalledSubscriber(t *testing.T) {
	hub := newEventHub()
	stalled := hub.subscribe("token")
	healthy := hub.subscribe("token")
	// Fill the stalled subscriber's buffer so any send to it would block.
	for len(stalled) < cap(stalled) {
		stalled <- []byte("fill")
	}

	assert.True(t, hub.publish("token", []byte("msg")), "healthy subscriber should receive the message")

	select {
	case msg := <-healthy:
		assert.Equal(t, []byte("msg"), msg)
	case <-time.After(time.Second):
		require.FailNow(t, "healthy subscriber did not receive the message")
	}
}

func TestEventHubPublishReturnsFalseWithoutSubscribers(t *testing.T) {
	hub := newEventHub()
	assert.False(t, hub.publish("token", []byte("msg")))
}
