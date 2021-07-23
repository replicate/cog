package predict

import "github.com/replicate/cog/pkg/model"

type HelpResponse struct {
	Arguments map[string]*model.RunArgument `json:"arguments"`
}
