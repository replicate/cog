package server

import (
	"encoding/json"
	"net/http"

	"github.com/replicate/cog/pkg/console"
	"github.com/replicate/cog/pkg/model"
)

func (s *Server) SendModelMetadata(w http.ResponseWriter, r *http.Request) {
	user, name, id := getRepoVars(r)
	console.Infof("Received get request for %s/%s/%s", user, name, id)

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
	images := []*model.Image{}
	for _, arch := range mod.Config.Environment.Architectures {
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
		Model  *model.Model   `json:"model"`
		Images []*model.Image `json:"images"`
	}{}
	body.Model = mod
	body.Images = images
	if err := json.NewEncoder(w).Encode(body); err != nil {
		console.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}
