package docker

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/replicate/cog/pkg/util/console"
)

var ErrNoSuchImage = errors.New("No image returned")

func ImageInspect(id string) (*types.ImageInspect, error) {
	cmd := exec.Command("docker", "image", "inspect", id)
	cmd.Env = os.Environ()
	console.Debug("$ " + strings.Join(cmd.Args, " "))
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var slice []types.ImageInspect
	err = json.Unmarshal(out, &slice)
	if err != nil {
		return nil, err
	}
	if len(slice) == 0 {
		return nil, ErrNoSuchImage
	}
	return &slice[0], nil
}
