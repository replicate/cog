package main

import (
	"github.com/docker/docker-credential-helpers/credentials"

	"github.com/replicate/cog/pkg/dockercredentialhelper"
)

func main() {
	credentials.Serve(dockercredentialhelper.Helper{})
}
