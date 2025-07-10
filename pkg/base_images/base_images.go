package base_images

import (
	"errors"
	"sort"
	"strings"

	"github.com/hashicorp/go-version"
)

type Accelerator string

const (
	AcceleratorCPU Accelerator = "cpu"
	AcceleratorGPU Accelerator = "gpu"
)

type BaseImage struct {
	Accelerator   Accelerator
	PythonVersion *version.Version
	UbuntuVersion *version.Version
	CudaVersion   *version.Version
	CuDNN         bool
	RunTag        string
	DevTag        string
}

type filter func(img *BaseImage) bool

type Constraint func() (filter, error)

func ForAccelerator(accelerator Accelerator) Constraint {
	return func() (filter, error) {
		return func(img *BaseImage) bool {
			return img.Accelerator == accelerator
		}, nil
	}
}

func UbuntuConstraint(spec string) Constraint {
	return func() (filter, error) {
		checks, err := parseConstraints(spec)
		if err != nil {
			return nil, err
		}

		return func(img *BaseImage) bool {
			return img.UbuntuVersion != nil && checks(img.UbuntuVersion)
		}, nil
	}
}

func PythonConstraint(spec string) Constraint {
	return func() (filter, error) {
		checks, err := parseConstraints(spec)
		if err != nil {
			return nil, err
		}

		return func(img *BaseImage) bool {
			return img.PythonVersion != nil && checks(img.PythonVersion)
		}, nil
	}
}

func CudaConstraint(spec string) Constraint {
	return func() (filter, error) {
		checks, err := parseConstraints(spec)
		if err != nil {
			return nil, err
		}

		return func(img *BaseImage) bool {
			return img.CudaVersion != nil && checks(img.CudaVersion)
		}, nil
	}
}

var (
	// ErrNoMatch is returned when no base image satisfies the provided constraints.
	ErrNoMatch = errors.New("no base image matches the provided constraints")
)

// ResolveBaseImage returns the best matching base image for the given constraints.
// The "best" image is chosen by preferring newer versions of Python, then CUDA, then Ubuntu.
func ResolveBaseImage(constraints ...Constraint) (*BaseImage, error) {
	idx, err := defaultIndex()
	if err != nil {
		return nil, err
	}

	images, err := idx.Query(constraints...)
	if err != nil {
		return nil, err
	}

	switch len(images) {
	case 0:
		return nil, ErrNoMatch
	case 1:
		return images[0], nil
	}

	// If multiple images satisfy constraints, pick the most recent.
	sort.Slice(images, func(i, j int) bool {
		a, b := images[i], images[j]

		if c := compareVersionPointers(a.PythonVersion, b.PythonVersion); c != 0 {
			return c > 0 // descending
		}
		if c := compareVersionPointers(a.CudaVersion, b.CudaVersion); c != 0 {
			return c > 0
		}
		if c := compareVersionPointers(a.UbuntuVersion, b.UbuntuVersion); c != 0 {
			return c > 0
		}
		return false
	})

	return images[0], nil
}

func Query(constraints ...Constraint) ([]*BaseImage, error) {
	idx, err := defaultIndex()
	if err != nil {
		return nil, err
	}

	return idx.Query(constraints...)
}

func compareVersionPointers(a, b *version.Version) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	default:
		return a.Compare(b)
	}
}

func parseConstraints(c string) (func(v *version.Version) bool, error) {
	var constraints []version.Constraints
	for _, spec := range strings.Split(c, "||") {
		constraintSet, err := version.NewConstraint(spec)
		if err != nil {
			return nil, err
		}
		constraints = append(constraints, constraintSet)
	}
	return func(v *version.Version) bool {
		for _, constraint := range constraints {
			if constraint.Check(v) {
				return true
			}
		}
		return false
	}, nil
}
