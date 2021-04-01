package server

import (
	"encoding/json"
	"net/http"

	"github.com/replicate/cog/pkg/console"
)

func (s *Server) SendModelMetadata(w http.ResponseWriter, r *http.Request) {
	user, name, id := getRepoVars(r)
	console.Info("Received get request for %s/%s/%s", user, name, id)

	mod, err := s.db.GetModel(user, name, id)
	if err != nil {
		console.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if mod == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(mod); err != nil {
		console.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}
