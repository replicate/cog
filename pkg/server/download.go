package server

import (
	"bytes"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gorilla/mux"

	"github.com/replicate/cog/pkg/storage"
	"github.com/replicate/cog/pkg/util/console"
)

func (s *Server) DownloadVersion(w http.ResponseWriter, r *http.Request) {
	user, name, id := getModelVars(r)
	modTime := time.Now() // TODO

	content, err := s.store.Download(user, name, id)
	if err != nil {
		if err == storage.NotFound {
			w.WriteHeader(http.StatusNotFound)
		} else {
			console.Error(err.Error())
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}
	console.Debugf("Downloaded %d bytes", len(content))
	http.ServeContent(w, r, id+".zip", modTime, bytes.NewReader(content))
}

func (s *Server) DownloadFile(w http.ResponseWriter, r *http.Request) {
	user, name, id := getModelVars(r)
	vars := mux.Vars(r)
	path := vars["path"]
	modTime := time.Now() // TODO

	content, err := s.store.DownloadFile(user, name, id, path)
	if err != nil {
		if err == storage.NotFound {
			w.WriteHeader(http.StatusNotFound)
		} else {
			console.Error(err.Error())
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}
	filename := filepath.Base(path)
	console.Debugf("Downloaded %d bytes", len(content))
	http.ServeContent(w, r, filename, modTime, bytes.NewReader(content))
}
