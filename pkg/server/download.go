package server

import (
	"bytes"
	"net/http"
	"time"

	"github.com/replicate/cog/pkg/console"
)

func (s *Server) SendModelPackage(w http.ResponseWriter, r *http.Request) {
	user, name, id := getRepoVars(r)
	console.Info("Received download request for %s/%s/%s", user, name, id)
	modTime := time.Now() // TODO

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

	content, err := s.store.Download(user, name, id)
	if err != nil {
		console.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	console.Info("Downloaded %d bytes", len(content))
	http.ServeContent(w, r, id+".zip", modTime, bytes.NewReader(content))
}
