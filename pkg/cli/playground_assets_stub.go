//go:build !playground_assets

package cli

import "testing/fstest"

var playgroundUI = fstest.MapFS{
	"playground/index.html": {Data: []byte("<!doctype html><title>Cog Playground</title>")},
}

const playgroundAssetsBuilt = false
