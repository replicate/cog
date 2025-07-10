package factory

import (
	"fmt"
	"strings"
)

// type BuildEnv struct {
// 	ProjectDir string
// 	Config     *config.Config
// 	BaseImage  *base_images.BaseImage
// }

// type BuildOp interface {

// }

type PythonPackages struct {
	Packages []string
}

type PythonRequirements struct {
	Requirements []string
}

func (d *PythonRequirements) Apply(stage *stage) error {
	// stage.RunCommand("uv pip install -r requirements.txt --venv /venv")

	var torchPackages []string
	var otherPackages []string

	fmt.Println("requirements", d.Requirements)

	for _, pkg := range d.Requirements {
		if strings.HasPrefix(pkg, "torch") {
			torchPackages = append(torchPackages, pkg)
		} else {
			otherPackages = append(otherPackages, pkg)
		}
	}

	if len(torchPackages) > 0 {
		stage.RunCommand("UV_TORCH_BACKEND=auto uv pip install --python /venv/bin/python " + strings.Join(torchPackages, " "))
	}

	if len(otherPackages) > 0 {
		stage.RunCommand("uv pip install " + strings.Join(otherPackages, " ") + " --venv /venv")
	}

	return nil
}
