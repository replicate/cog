package server

import (
	"encoding/json"
	"net/http"

	"github.com/replicate/cog/pkg/util/console"
)

func (s *Server) ListVersions(w http.ResponseWriter, r *http.Request) {
	user, name, _ := getModelVars(r)
	console.Debugf("Received list request for %s%s", user, name)

	versions, err := s.db.ListVersions(user, name)
	if err != nil {
		console.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	result := []*APIVersion{}
	for _, version := range versions {
		apiVersion, err := createAPIVersion(s.db, version, user, name)
		if err != nil {
			console.Error(err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		result = append(result, apiVersion)
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(result); err != nil {
		console.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}
