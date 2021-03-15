package serving

import (
	"github.com/replicate/modelserver/pkg/model"
)

type Platform interface {
	Deploy(mod *model.Model, target model.Target) (Deployment, error)
}

type Deployment interface {
	RunInference(input *Example) (*Result, error)
	Undeploy() error
}

type Example struct {
	Values map[string]string
}

type Result struct {
	Values map[string]string
}
