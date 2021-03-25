package database

import (
	"github.com/replicate/cog/pkg/model"
)

type Database interface {
	InsertModel(user string, name string, id string, mod *model.Model) error
	GetModel(user string, name string, id string) (*model.Model, error)
	ListModels(user string, name string) ([]*model.Model, error)
	DeleteModel(user string, name string, id string) error
}
