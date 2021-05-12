package server

import (
	"encoding/json"
	"net/http"

	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/util/console"
)

func (s *Server) SendVersionMetadata(w http.ResponseWriter, r *http.Request) {
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
	images := []*model.Image{}
	for _, arch := range version.Config.Environment.Architectures {
		image, err := s.db.GetImage(user, name, id, arch)
		if err != nil {
			console.Error(err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		images = append(images, image)
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")

	body := struct {
		Version *model.Version `json:"version"`
		Images  []*model.Image `json:"images"`
	}{}
	body.Version = version
	body.Images = images
	if err := json.NewEncoder(w).Encode(body); err != nil {
		console.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}
