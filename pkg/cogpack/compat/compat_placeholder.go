package compat

import (
	"context"
	"embed"
	"fmt"

	"github.com/Masterminds/semver/v3"
)

//go:embed data
var datafs embed.FS

type PythonRelease struct {
	Name          string          `csv:"name"`
	Version       *semver.Version `csv:"version"`
	IsDeprecated  bool            `csv:"is_deprecated"`
	LatestVersion *semver.Version `csv:"latest_version"`
}

func LoadPythonVersions(ctx context.Context) ([]PythonRelease, error) {
	r, err := datafs.Open("data/python.csv")
	if err != nil {
		return nil, err
	}
	defer r.Close()

	return readCSV[PythonRelease](r)
}

func ResolvePython(versionConstraint string) (PythonRelease, error) {
	releases, err := LoadPythonVersions(context.Background())
	if err != nil {
		return PythonRelease{}, err
	}

	constraint, err := semver.NewConstraint(versionConstraint)
	if err != nil {
		return PythonRelease{}, err
	}

	for _, r := range releases {
		if constraint.Check(r.Version) {
			return r, nil
		}
	}

	return PythonRelease{}, fmt.Errorf("no python version found for %s", versionConstraint)
}
