package predict

import "github.com/replicate/cog/pkg/config"

type HelpResponse struct {
	Arguments map[string]*config.RunArgument `json:"arguments"`
}
