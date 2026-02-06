package version

import (
	"fmt"
	"strconv"
	"strings"
)

type Version struct {
	Major    int
	Minor    int
	Patch    *int
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
		patch, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil, fmt.Errorf("Invalid patch version %s: %w", parts[2], err)
		}
		// We assign a pointer here to handle cases where the patch version is not
		// explicitly assigned and we need to compare versions without patches to
		// versions with patches.
		version.Patch = new(int)
		*version.Patch = patch
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
	case v.Major == other.Major &&
		v.Minor == other.Minor &&
		v.PatchVersion() > other.PatchVersion():
		return true
	default:
		return false
	}
}

func (v *Version) Equal(other *Version) bool {
	return v.Major == other.Major &&
		v.Minor == other.Minor &&
		v.PatchVersion() == other.PatchVersion() &&
		v.Metadata == other.Metadata
}

func (v *Version) GreaterOrEqual(other *Version) bool {
	return v.Greater(other) || v.Equal(other)
}

func (v *Version) EqualMinor(other *Version) bool {
	return v.Major == other.Major && v.Minor == other.Minor
}

func (v *Version) HasPatch() bool {
	return v.Patch != nil
}

func (v *Version) PatchVersion() int {
	if v.Patch == nil {
		return 0
	}
	return *v.Patch
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
	leftVersion, err := NewVersion(v1)
	if err != nil {
		return v1 == v2
	}
	rightVersion, err := NewVersion(v2)
	if err != nil {
		return v1 == v2
	}
	return leftVersion.GreaterOrEqual(rightVersion)
}

func (v *Version) Matches(other *Version) bool {
	switch {
	case v.Major != other.Major:
		return false
	case v.Minor != other.Minor:
		return false
	case v.HasPatch() && other.HasPatch() && *v.Patch != *other.Patch:
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

func StripModifier(v string) string {
	modifierSplit := strings.Split(v, "+")
	return modifierSplit[0]
}
