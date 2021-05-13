package server

import (
	"github.com/replicate/cog/pkg/database"
	"github.com/replicate/cog/pkg/model"
)

type APIVersion struct {
	model.Version
	Images []*model.Image `json:"images"`
}

// TODO(bfirsh): it should be possible to get user and name from version
func createAPIVersion(db database.Database, version *model.Version, user, name string) (*APIVersion, error) {
	images := []*model.Image{}
	for _, arch := range version.Config.Environment.Architectures {
		image, err := db.GetImage(user, name, version.ID, arch)
		if err != nil {
			return nil, err
		}
		images = append(images, image)
	}
	return &APIVersion{Version: *version, Images: images}, nil
}
