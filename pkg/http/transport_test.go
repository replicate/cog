package http

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestTransportOnlyAddsHeaderIfMissing(t *testing.T) {
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
	const expectedValue = "ExpectedValue"
	req, err := http.NewRequest("GET", server.URL, nil)
	req.Header.Set(testHeader, expectedValue)
	require.NoError(t, err)
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, resp.Request.Header.Get(testHeader), expectedValue)
}

func TestTransportSendsErrorWithMissingToken(t *testing.T) {
	// Setup mock http server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	u, err := url.Parse(server.URL)
	require.NoError(t, err)

	transport := Transport{
		authentication: map[string]string{
			u.Host: BearerHeaderPrefix + "",
		},
	}
	req, err := http.NewRequest("GET", server.URL, nil)
	require.NoError(t, err)
	resp, err := transport.RoundTrip(req)
	require.Error(t, err)
	require.Nil(t, resp)
}
