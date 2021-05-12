package server

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/replicate/cog/pkg/util/console"
)

func (s *Server) GetDisplayTokenURL(w http.ResponseWriter, r *http.Request) {
	resp := &struct {
		URL string `json:"url"`
	}{}
	if s.authDelegate != "" {
		resp.URL = s.authDelegate + "/display-token"
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		console.Errorf("Failed to decode response json: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (s *Server) VerifyToken(w http.ResponseWriter, r *http.Request) {
	if s.authDelegate == "" {
		console.Error("Attempted to verify auth token but server has no auth delegate")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		console.Errorf("Failed to parse form: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	token := r.FormValue("token")
	resp, err := http.PostForm(s.authDelegate+"/verify-token", url.Values{
		"token": []string{token},
	})
	if err != nil {
		console.Errorf("Failed to verify token: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	body := &struct {
		Username string `json:"username"`
	}{}
	if err := json.NewDecoder(resp.Body).Decode(body); err != nil {
		console.Errorf("Failed decode json: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		console.Errorf("Failed encode json: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func (s *Server) checkReadAccess(handler http.HandlerFunc) http.HandlerFunc {
	return s.checkAccess(handler, "read")
}

func (s *Server) checkWriteAccess(handler http.HandlerFunc) http.HandlerFunc {
	return s.checkAccess(handler, "write")
}

func (s *Server) checkAccess(handler http.HandlerFunc, mode string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if handler == nil {
			handler = func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("OK"))
				return
			}
		}

		if s.authDelegate == "" {
			handler(w, r)
			return
		}
		user, model, versionID := getModelVars(r)

		token := ""
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			tokenBase64 := strings.Split(authHeader, "Bearer ")[1]
			tokenBytes, err := base64.StdEncoding.DecodeString(tokenBase64)
			if err != nil {
				console.Errorf("Failed to decode token: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
			}
			token = string(tokenBytes)
		}

		values := url.Values{
			"mode":  []string{mode},
			"user":  []string{user},
			"model": []string{model},
		}
		if versionID != "" {
			values["version_id"] = []string{versionID}
		}
		if token != "" {
			values["token"] = []string{token}
		}
		resp, err := http.PostForm(s.authDelegate+"/check-access", values)
		if err != nil {
			console.Errorf("Auth request failed: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if resp.StatusCode == http.StatusOK {
			handler(w, r)
			return
		}
		console.Warnf("Not authorized to %s %s/%s:%s", mode, user, model, versionID)
		w.WriteHeader(resp.StatusCode)
		if _, err := io.Copy(w, resp.Body); err != nil {
			console.Errorf("Failed to copy body: %v", err)
		}
		return
	}
}
