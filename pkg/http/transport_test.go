package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTransportAddsHeaders(t *testing.T) {
	// Setup mock http server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	const testHeader = "X-Test-Header"
	const testValue = "TestValue"
	transport := Transport{
		headers: map[string]string{
			testHeader: testValue,
		},
	}
	req, err := http.NewRequest("GET", server.URL, nil)
	require.NoError(t, err)
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, resp.Request.Header.Get(testHeader), testValue)
}
