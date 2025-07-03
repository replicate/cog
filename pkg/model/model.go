package model

import "github.com/replicate/cog/pkg/config"

type Model struct {
	Name   string
	Config *config.Config
}
