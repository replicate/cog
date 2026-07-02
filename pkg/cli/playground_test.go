package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestPlayground(t *testing.T) *httptest.Server {
	t.Helper()
	uiFS, err := fs.Sub(playgroundUI, "playground")
	require.NoError(t, err)
	s := &playgroundServer{hub: newEventHub(), webhookBase: "http://wh.example/cb"}
	ts := httptest.NewServer(s.routes(uiFS))
	t.Cleanup(ts.Close)
	return ts
}

// echoServer reports the received path and (forwarded) query as JSON.
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"path":%q,"query":%q}`, r.URL.Path, r.URL.RawQuery)
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

	resp2, err := http.Get(ts.URL + "/app.js")
	require.NoError(t, err)
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	assert.Contains(t, string(body2), "CogApi")

	// Styling is split across two embedded stylesheets (the vendored Kumo
	// design tokens and the playground's own styles); both must ship.
	for _, path := range []string{"/styles.css", "/vendor/kumo.css"} {
		resp, err := http.Get(ts.URL + path)
		require.NoError(t, err, "requesting %s", path)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode, "%s should be served", path)
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/css", "%s content-type", path)
		assert.Contains(t, string(body), "--color-kumo", "%s should contain Kumo tokens", path)
	}
}

func TestPlaygroundConfig(t *testing.T) {
	ts := newTestPlayground(t)
	resp, err := http.Get(ts.URL + "/config")
	require.NoError(t, err)
	defer resp.Body.Close()

	var config map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&config))
	assert.Equal(t, "http://wh.example/cb", config["webhookBase"])
}

func TestPlaygroundProxyHeaderTarget(t *testing.T) {
	ts := newTestPlayground(t)
	stub := echoServer(t)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/proxy/openapi.json?foo=bar&cog_target=ignored", nil)
	require.NoError(t, err)
	req.Header.Set("X-Cog-Target", stub.URL)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	var got map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, "/openapi.json", got["path"], "/proxy prefix should be stripped")
	assert.Equal(t, "foo=bar", got["query"], "cog_target should be removed from the forwarded query")
}

func TestPlaygroundProxyQueryTarget(t *testing.T) {
	ts := newTestPlayground(t)
	stub := echoServer(t)

	u := ts.URL + "/proxy/health-check?x=1&cog_target=" + url.QueryEscape(stub.URL)
	resp, err := http.Get(u)
	require.NoError(t, err)
	defer resp.Body.Close()

	var got map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, "/health-check", got["path"])
	assert.Equal(t, "x=1", got["query"])
}

func TestPlaygroundProxyMissingTarget(t *testing.T) {
	ts := newTestPlayground(t)
	resp, err := http.Get(ts.URL + "/proxy/health-check")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPlaygroundProxyInvalidTarget(t *testing.T) {
	ts := newTestPlayground(t)
	for _, target := range []string{"ftp://example.com", "garbage", ""} {
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
		t.Fatal("timed out waiting for relayed webhook event")
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
		t.Fatal("timed out waiting for relayed webhook event")
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
