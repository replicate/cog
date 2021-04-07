package server

import (
	"encoding/json"
	"net/http"

	"github.com/replicate/cog/pkg/console"
)

func (s *Server) ListModels(w http.ResponseWriter, r *http.Request) {
	user, name, _ := getRepoVars(r)
	console.Infof("Received list request for %s%s", user, name)

	models, err := s.db.ListModels(user, name)
	if err != nil {
		console.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(models); err != nil {
		console.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}
