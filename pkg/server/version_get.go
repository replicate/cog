package server

import (
	"encoding/json"
	"net/http"

	"github.com/replicate/cog/pkg/util/console"
)

func (s *Server) GetVersion(w http.ResponseWriter, r *http.Request) {
	user, name, id := getModelVars(r)
	console.Debugf("Received get request for %s/%s/%s", user, name, id)

	version, err := s.db.GetVersion(user, name, id)
	if err != nil {
		console.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if version == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")

	body, err := createAPIVersion(s.db, version, user, name)
	if err != nil {
		console.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		console.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}
