//go:build playground_assets

package cli

import "embed"

//go:embed playground
var playgroundUI embed.FS

const playgroundAssetsBuilt = true
