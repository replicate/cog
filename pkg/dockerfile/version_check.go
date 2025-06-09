package dockerfile

import (
	"regexp"
)

// Version string in the form x.y.z for Monobase R8_*_VERSION
// We do not support suffixes like -alpha1 or +cu124
var versionRegex = regexp.MustCompile(`^(?P<major>\d+)(\.(?P<minor>\d+)(\.(?P<patch>\d+))?)?$`)

func parse(s string) (string, string, string) {
	m := versionRegex.FindStringSubmatch(s)
	if m == nil {
		return "", "", ""
	}
	major := m[versionRegex.SubexpIndex("major")]
	minor := m[versionRegex.SubexpIndex("minor")]
	patch := m[versionRegex.SubexpIndex("patch")]
	return major, minor, patch

}

func CheckMajorOnly(s string) bool {
	major, minor, patch := parse(s)
	return major != "" && minor == "" && patch == ""
}

func CheckMajorMinorOnly(s string) bool {
	major, minor, patch := parse(s)
	return major != "" && minor != "" && patch == ""
}

func CheckMajorMinorPatch(s string) bool {
	major, minor, patch := parse(s)
	return major != "" && minor != "" && patch != ""
}
