package model

import (
	"strings"
)

// Tag namespace conventions:
//
// All tags Cog generates live under the ReservedTagPrefix ("cog-").
// Within that prefix, dots separate structural segments and hyphens
// live inside segments. The fixed shapes are:
//
//	cog-weight.{sanitized-name}.{12-char-digest-prefix}
//	cog-image.{timestamp}
//
// User-supplied model index tags carry no prefix (e.g. "v2",
// "20260508T153042Z"). Tags starting with "cog-" are rejected at the
// CLI/env-var entry point so the cog-* namespace stays exclusively
// Cog's. The trailing dot in the prefix constants is what gives us a
// machine-parseable namespace: any "cog-x.…" tag belongs to type "x".
const (
	// ReservedTagPrefix is the prefix Cog uses on every tag it
	// auto-generates. User-supplied tags starting with this prefix
	// are rejected.
	ReservedTagPrefix = "cog-"

	// weightTagPrefix is the prefix for weight artifact manifest
	// tags. The trailing dot makes the prefix unambiguous so
	// ParseWeightTag can split on the first dot after it.
	weightTagPrefix = ReservedTagPrefix + "weight."

	// imageTagPrefix is the prefix for image manifest tags pushed as
	// part of a bundle.
	imageTagPrefix = ReservedTagPrefix + "image."

	// maxTagSegmentLen caps a sanitized segment so the full tag
	// stays under the OCI tag length limit (128 chars). 24 chars
	// of overhead covers the prefix + dot + 12-char short digest
	// with room to spare.
	maxTagSegmentLen = 104

	// unnamedSegment is the placeholder returned when a name
	// sanitizes to the empty string. Picking a fixed value keeps
	// the resulting tag valid rather than blowing up at the
	// registry boundary.
	unnamedSegment = "unnamed"
)

// WeightTag returns the OCI tag for a weight manifest combining the
// sanitized weight name and the 12-char prefix of digest. digest is
// expected as "sha256:…"; if it's empty or missing the algorithm
// prefix the tag omits the digest segment and returns
// "cog-weight.{name}". The no-digest form is a defensive fallback —
// the real push path always has a SetDigest by the time it gets here
// — so the result lives in the cog-weight namespace but does NOT
// round-trip through ParseWeightTag, which requires both segments.
func WeightTag(name, digest string) string {
	sanitized := SanitizeTagSegment(name)
	short := ShortDigest(digest)
	if short == "" {
		return weightTagPrefix + sanitized
	}
	return weightTagPrefix + sanitized + "." + short
}

// ImageTag returns the OCI tag for an image manifest pushed as part
// of a bundle. timestamp is typically the model index push timestamp,
// e.g. GenerateTimestampTag(); ImageTag does not validate that the
// input is itself a valid OCI tag — callers feeding generated tags
// produce valid output by construction, and rejecting at this layer
// would just duplicate the timestamp-generator contract. Garbage in,
// garbage out: it is the caller's responsibility to supply input that
// makes the combined tag conform to the OCI tag grammar.
func ImageTag(timestamp string) string {
	return imageTagPrefix + timestamp
}

// SanitizeTagSegment makes a string safe for use as a single segment
// (between dots) within an OCI tag. The full tag grammar is
// [a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}; since we use dots as namespace
// separators ourselves, segments must restrict to [a-zA-Z0-9_-].
//
// Sanitization rules:
//   - Replace any character not in [a-zA-Z0-9_-] with '-'.
//   - Collapse consecutive '-' into a single '-'.
//   - Strip leading and trailing '-'.
//   - If the result is empty, return "unnamed" so the tag stays valid.
//   - Truncate to maxTagSegmentLen so the full tag fits within OCI limits.
//
// The result is always non-empty and always starts with a character
// in [a-zA-Z0-9_], which keeps it valid as the leading segment of an
// OCI tag.
func SanitizeTagSegment(s string) string {
	if s == "" {
		return unnamedSegment
	}

	// First pass: replace invalid chars with '-'.
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isValidSegmentChar(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}

	// Second pass: collapse consecutive '-' and strip leading/trailing.
	collapsed := collapseHyphens(b.String())
	collapsed = strings.Trim(collapsed, "-")

	if collapsed == "" {
		return unnamedSegment
	}

	if len(collapsed) > maxTagSegmentLen {
		collapsed = strings.TrimRight(collapsed[:maxTagSegmentLen], "-")
		if collapsed == "" {
			return unnamedSegment
		}
	}

	return collapsed
}

// isValidSegmentChar reports whether r is allowed inside a tag
// segment (between dots).
func isValidSegmentChar(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '_' || r == '-':
		return true
	}
	return false
}

// collapseHyphens returns s with runs of '-' replaced by a single '-'.
func collapseHyphens(s string) string {
	if !strings.Contains(s, "--") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	prevHyphen := false
	for i := range len(s) {
		c := s[i]
		if c == '-' {
			if prevHyphen {
				continue
			}
			prevHyphen = true
		} else {
			prevHyphen = false
		}
		b.WriteByte(c)
	}
	return b.String()
}

// IsReservedTag reports whether tag uses the Cog-reserved prefix
// ("cog-"). Tags matching this are rejected when supplied by users
// via cog.yaml or COG_MODEL_TAG so the namespace stays exclusively
// Cog's.
func IsReservedTag(tag string) bool {
	return strings.HasPrefix(tag, ReservedTagPrefix)
}

// ParseWeightTag extracts the sanitized name and short digest from a
// weight manifest tag of the form "cog-weight.{name}.{short}". It
// returns ok=false for any tag that doesn't match the weight prefix or
// doesn't carry both segments.
//
// The short digest is the third dot-separated segment — that's where
// WeightTag puts it. We deliberately split on '.', not '-', because
// dots are the namespace separator; hyphens inside the name segment
// (e.g. "my-model") must stay intact.
func ParseWeightTag(tag string) (name, shortDigest string, ok bool) {
	rest, ok := strings.CutPrefix(tag, weightTagPrefix)
	if !ok {
		return "", "", false
	}
	name, shortDigest, ok = strings.Cut(rest, ".")
	if !ok || name == "" || shortDigest == "" {
		return "", "", false
	}
	// A weight tag has exactly two segments after the prefix; reject
	// anything with extra dots so callers can rely on the shape.
	if strings.Contains(shortDigest, ".") {
		return "", "", false
	}
	return name, shortDigest, true
}

// ParseImageTag extracts the timestamp from an image manifest tag of
// the form "cog-image.{timestamp}". Returns ok=false for any tag that
// doesn't match the image prefix or has an empty timestamp.
func ParseImageTag(tag string) (timestamp string, ok bool) {
	rest, ok := strings.CutPrefix(tag, imageTagPrefix)
	if !ok || rest == "" {
		return "", false
	}
	return rest, true
}
