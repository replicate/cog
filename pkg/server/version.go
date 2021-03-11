package server

import (
	"fmt"
	"strconv"
	"strings"
)

type Version struct {
	Major    int
	Minor    int
	Patch    int
	Metadata string
}

func NewVersion(s string) (version *Version, err error) {
	plusParts := strings.SplitN(s, "+", 2)
	number := plusParts[0]
	parts := strings.Split(number, ".")
	if len(parts) > 3 {
		return nil, fmt.Errorf("Version must not have more than 3 parts: %s", s)
	}
	version = new(Version)
	version.Major, err = strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("Invalid major version %s: %w", parts[0], err)
	}
	if len(parts) >= 2 {
		version.Minor, err = strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("Invalid minor version %s: %w", parts[1], err)
		}
	}
	if len(parts) >= 3 {
		version.Patch, err = strconv.Atoi(parts[2])
		if err != nil {
			return nil, fmt.Errorf("Invalid patch version %s: %w", parts[2], err)
		}
	}

	if len(plusParts) == 2 {
		version.Metadata = plusParts[1]
	}

	return version, nil
}

func MustVersion(s string) *Version {
	version, err := NewVersion(s)
	if err != nil {
		panic(fmt.Sprintf("%s", err))
	}
	return version
}

func (v *Version) Greater(other *Version) bool {
	switch {
	case v.Major > other.Major:
		return true
	case v.Major == other.Major && v.Minor > other.Minor:
		return true
	case v.Major == other.Major && v.Minor == other.Minor && v.Patch > other.Patch:
		return true
	default:
		return false
	}
}
