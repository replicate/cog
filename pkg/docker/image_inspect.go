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
		if ee, ok := err.(*exec.ExitError); ok {
			// TODO(andreas): this is fragile in case the
			// error message changes
			if strings.Contains(string(ee.Stderr), "No such image") {
				return nil, ErrNoSuchImage
			}
		}
		return nil, err
	}
	var slice []types.ImageInspect
	err = json.Unmarshal(out, &slice)
	if err != nil {
		return nil, err
	}
	// There may be some Docker versions where a missing image
	// doesn't return exit code 1, but progresses to output an
	// empty list.
	if len(slice) == 0 {
		return nil, ErrNoSuchImage
	}
	return &slice[0], nil
}
