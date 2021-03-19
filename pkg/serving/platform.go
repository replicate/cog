package serving

import (
	"github.com/replicate/cog/pkg/model"
)

type Platform interface {
	Deploy(mod *model.Model, target model.Target, logWriter func(string)) (Deployment, error)
}

type Deployment interface {
	RunInference(input *Example) (*Result, error)
	Help() (*HelpResponse, error)
	Undeploy() error
}

type Example struct {
	Values map[string]string
}

type Result struct {
	Values map[string]string
}

type ArgumentType string

const (
	ArgumentTypeString ArgumentType = "str"
	ArgumentTypeInt    ArgumentType = "int"
	ArgumentTypePath   ArgumentType = "Path"
)

type ArgumentHelp struct {
	Type    ArgumentType `json:"type"`
	Default *string      `json:"default"`
	Help    *string      `json:"help"`
}

type HelpResponse struct {
	Arguments map[string]*ArgumentHelp `json:"arguments"`
}
