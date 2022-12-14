package predict

import "github.com/sieve-data/cog/pkg/config"

type HelpResponse struct {
	Arguments map[string]*config.RunArgument `json:"arguments"`
}
