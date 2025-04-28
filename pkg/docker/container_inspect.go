package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/docker/docker/api/types"
)

func ContainerInspect(ctx context.Context, id string) (*types.ContainerJSON, error) {
	cmd := exec.CommandContext(ctx, "docker", "container", "inspect", id)
	cmd.Env = os.Environ()

	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var slice []types.ContainerJSON
	err = json.Unmarshal(out, &slice)
	if err != nil {
		return nil, err
	}
	if len(slice) == 0 {
		return nil, fmt.Errorf("No container returned")
	}
	return &slice[0], nil
}
