package http

import (
	"errors"
	"net/http"
)

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
			if authorisation == BearerHeaderPrefix {
				return nil, errors.New("No token supplied for HTTP authorization. Have you run 'cog login'?")
			}
			req.Header.Set(AuthorizationHeader, authorisation)
		}
	}

	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}
