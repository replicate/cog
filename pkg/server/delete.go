package server

import (
	"net/http"

	"github.com/replicate/cog/pkg/util/console"
)

func (s *Server) DeleteVersion(w http.ResponseWriter, r *http.Request) {
	user, name, id := getRepoVars(r)
	console.Infof("Received delete request for %s/%s/%s", user, name, id)

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

	if err := s.store.Delete(user, name, id); err != nil {
		console.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if err := s.db.DeleteVersion(user, name, id); err != nil {
		console.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Deleted " + id))
}
