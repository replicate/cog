package server

import (
	"errors"
	"net/http"
)

var (
	ErrConflict    = errors.New("already running a prediction")
	ErrExists      = errors.New("prediction exists")
	ErrNotFound    = errors.New("prediction not found")
	ErrDefunct     = errors.New("server is defunct")
	ErrSetupFailed = errors.New("setup failed")
)

func NewServeMux(handler *Handler, useProcedureMode bool) *http.ServeMux {
	serveMux := http.NewServeMux()
	serveMux.HandleFunc("GET /{$}", handler.Root)
	serveMux.HandleFunc("GET /health-check", handler.HealthCheck)
	serveMux.HandleFunc("GET /openapi.json", handler.OpenAPI)

	if useProcedureMode {
		serveMux.HandleFunc("POST /procedures", handler.Predict)
		serveMux.HandleFunc("PUT /procedures/{id}", handler.Predict)
		serveMux.HandleFunc("POST /procedures/{id}/cancel", handler.Cancel)
	} else {
		serveMux.HandleFunc("POST /predictions", handler.Predict)
		serveMux.HandleFunc("PUT /predictions/{id}", handler.Predict)
		serveMux.HandleFunc("POST /predictions/{id}/cancel", handler.Cancel)
	}

	serveMux.HandleFunc("POST /_ipc", handler.HandleIPC)

	return serveMux
}
