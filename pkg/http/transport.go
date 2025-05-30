package http

import "net/http"

const AuthorizationHeader = "Authorization"

type Transport struct {
	headers        map[string]string
	authentication map[string]string
	base           http.RoundTripper
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Write standard headers
	for k, v := range t.headers {
		if req.Header.Get(k) == "" {
			req.Header.Set(k, v)
		}
	}

	// Write authentication
	if req.Header.Get(AuthorizationHeader) == "" {
		authorisation, ok := t.authentication[req.URL.Host]
		if ok {
			req.Header.Set(AuthorizationHeader, authorisation)
		}
	}

	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}
