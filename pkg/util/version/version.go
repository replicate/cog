package version

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

func (v *Version) Equal(other *Version) bool {
	return v.Major == other.Major && v.Minor == other.Minor && v.Patch == other.Patch && v.Metadata == other.Metadata
}

func (v *Version) GreaterOrEqual(other *Version) bool {
	return v.Greater(other) || v.Equal(other)
}

func (v *Version) EqualMinor(other *Version) bool {
	return v.Major == other.Major && v.Minor == other.Minor
}

func Equal(v1 string, v2 string) bool {
	return MustVersion(v1).Equal(MustVersion(v2))
}

func EqualMinor(v1 string, v2 string) bool {
	return MustVersion(v1).EqualMinor(MustVersion(v2))
}

func Greater(v1 string, v2 string) bool {
	return MustVersion(v1).Greater(MustVersion(v2))
}

func GreaterOrEqual(v1 string, v2 string) bool {
	return MustVersion(v1).GreaterOrEqual(MustVersion(v2))
}

func (v *Version) Matches(other *Version) bool {
	switch {
	case v.Major != other.Major:
		return false
	case v.Minor != other.Minor:
		return false
	case v.Patch != 0 && v.Patch != other.Patch:
		return false
	default:
		return true
	}
}

func Matches(v1 string, v2 string) bool {
	return MustVersion(v1).Matches(MustVersion(v2))
}

func StripPatch(v string) string {
	ver := MustVersion(v)
	return fmt.Sprintf("%d.%d", ver.Major, ver.Minor)
}
