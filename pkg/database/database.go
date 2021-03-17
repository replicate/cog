package database

import (
	"github.com/replicate/cog/pkg/model"
)

type Database interface {
	InsertModel(mod *model.Model) error
	ListModels() ([]*model.Model, error)
	GetModelByID(id string) (*model.Model, error)
}
