package dockerfile

import "embed"

//go:embed embed/*.whl
var CogEmbed embed.FS
