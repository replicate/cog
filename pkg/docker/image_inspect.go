package docker

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/docker/docker/api/types"
)

func ImageInspect(id string) (*types.ImageInspect, error) {
	cmd := exec.Command("docker", "image", "inspect", id)
	cmd.Env = os.Environ()

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
		return nil, fmt.Errorf("No image returned")
	}
	return &slice[0], nil
}
