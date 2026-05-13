package model

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ociTagRegex is the OCI distribution-spec tag grammar, duplicated
// here so we can assert that every tag the helpers produce matches.
// Keeping the regex local to the test avoids tying production code to
// the spec at runtime — that's the env-var validator's job.
var ociTagRegex = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)

func TestWeightTag(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		digest   string
		expected string
	}{
		{
			name:     "simple name with digest",
			input:    "resnet50",
			digest:   "sha256:52924993c7ef0123456789abcdef0123456789abcdef0123456789abcdef0123",
			expected: "cog-weight.resnet50.52924993c7ef",
		},
		{
			name:     "name with spaces and parens sanitized",
			input:    "my model (v2)",
			digest:   "sha256:abc123456789def0123456789abcdef0123456789abcdef0123456789abcdef0",
			expected: "cog-weight.my-model-v2.abc123456789",
		},
		{
			name:     "empty name falls back to unnamed",
			input:    "",
			digest:   "sha256:abc123456789def0123456789abcdef0123456789abcdef0123456789abcdef0",
			expected: "cog-weight.unnamed.abc123456789",
		},
		{
			name:     "missing digest omits digest segment",
			input:    "resnet50",
			digest:   "",
			expected: "cog-weight.resnet50",
		},
		{
			name:     "digest without algorithm prefix omits digest segment",
			input:    "resnet50",
			digest:   "abc123",
			expected: "cog-weight.resnet50",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := WeightTag(tc.input, tc.digest)
			assert.Equal(t, tc.expected, got)
			assert.Regexp(t, ociTagRegex, got, "generated tag must be a valid OCI tag")
		})
	}
}

func TestImageTag(t *testing.T) {
	tests := []struct {
		name      string
		timestamp string
		expected  string
	}{
		{
			name:      "iso timestamp",
			timestamp: "20260508T153042Z",
			expected:  "cog-image.20260508T153042Z",
		},
		{
			name:      "generated timestamp round-trips through regex",
			timestamp: GenerateTimestampTag(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ImageTag(tc.timestamp)
			if tc.expected != "" {
				assert.Equal(t, tc.expected, got)
			}
			assert.True(t, strings.HasPrefix(got, "cog-image."))
			assert.Regexp(t, ociTagRegex, got, "generated tag must be a valid OCI tag")
		})
	}
}

func TestSanitizeTagSegment(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"already valid", "resnet50", "resnet50"},
		{"with spaces and bang", "hello world!", "hello-world"},
		{"strips leading and trailing hyphens", "---foo---", "foo"},
		{"collapses consecutive hyphens", "a---b---c", "a-b-c"},
		{"empty becomes unnamed", "", "unnamed"},
		{"only invalid chars becomes unnamed", "!!!", "unnamed"},
		{"underscores preserved", "my_name_here", "my_name_here"},
		{"unicode replaced", "résumé", "r-sum"},
		{"dots replaced (we own dot-as-separator)", "a.b.c", "a-b-c"},
		{"slashes replaced", "user/model", "user-model"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeTagSegment(tc.input)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestSanitizeTagSegment_TruncatesLongInput(t *testing.T) {
	input := strings.Repeat("a", 200)
	got := SanitizeTagSegment(input)
	assert.LessOrEqual(t, len(got), maxTagSegmentLen)
	assert.Equal(t, strings.Repeat("a", maxTagSegmentLen), got)
}

func TestSanitizeTagSegment_TruncationTrimsTrailingHyphen(t *testing.T) {
	// 103 valid chars + a long run of '-' so the truncation boundary
	// lands inside the hyphen run. Without the trailing-trim, the
	// result would end in '-' which is technically valid mid-tag
	// but ugly and would re-trip the strip rule if used as a
	// standalone tag.
	input := strings.Repeat("a", 103) + strings.Repeat("-", 50) + "tail"
	got := SanitizeTagSegment(input)
	assert.False(t, strings.HasSuffix(got, "-"),
		"sanitized segment should not end in hyphen even after truncation; got %q", got)
	assert.LessOrEqual(t, len(got), maxTagSegmentLen)
}

func TestParseWeightTag(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantName   string
		wantDigest string
		wantOK     bool
	}{
		{
			name:       "valid weight tag",
			input:      "cog-weight.resnet50.abc123456789",
			wantName:   "resnet50",
			wantDigest: "abc123456789",
			wantOK:     true,
		},
		{
			name:       "name with internal hyphens",
			input:      "cog-weight.my-model-v2.abc123456789",
			wantName:   "my-model-v2",
			wantDigest: "abc123456789",
			wantOK:     true,
		},
		{
			name:   "not a weight tag",
			input:  "something-else",
			wantOK: false,
		},
		{
			name:   "image tag",
			input:  "cog-image.20260508T153042Z",
			wantOK: false,
		},
		{
			name:   "weight prefix only",
			input:  "cog-weight.",
			wantOK: false,
		},
		{
			name:   "weight prefix plus name but no digest",
			input:  "cog-weight.resnet50",
			wantOK: false,
		},
		{
			name:   "weight tag with extra dots is rejected",
			input:  "cog-weight.name.digest.extra",
			wantOK: false,
		},
		{
			name:   "user tag that happens to share a substring",
			input:  "v2-cog-weight.x.y",
			wantOK: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name, digest, ok := ParseWeightTag(tc.input)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantName, name)
			assert.Equal(t, tc.wantDigest, digest)
		})
	}
}

func TestParseWeightTag_RoundTripsThroughWeightTag(t *testing.T) {
	cases := []struct {
		name   string
		digest string
	}{
		{"resnet50", "sha256:52924993c7ef0123456789abcdef0123456789abcdef0123456789abcdef0123"},
		{"my-model-v2", "sha256:abc123456789def0123456789abcdef0123456789abcdef0123456789abcdef0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tag := WeightTag(tc.name, tc.digest)
			gotName, gotShort, ok := ParseWeightTag(tag)
			require.True(t, ok, "tag %q should parse", tag)
			assert.Equal(t, tc.name, gotName)
			assert.Equal(t, ShortDigest(tc.digest), gotShort)
		})
	}
}

func TestParseImageTag(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantTimestamp string
		wantOK        bool
	}{
		{
			name:          "valid image tag",
			input:         "cog-image.20260508T153042Z",
			wantTimestamp: "20260508T153042Z",
			wantOK:        true,
		},
		{
			name:   "not an image tag",
			input:  "something-else",
			wantOK: false,
		},
		{
			name:   "weight tag",
			input:  "cog-weight.foo.bar",
			wantOK: false,
		},
		{
			name:   "image prefix only",
			input:  "cog-image.",
			wantOK: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ts, ok := ParseImageTag(tc.input)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantTimestamp, ts)
		})
	}
}

func TestIsReservedTag(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"cog-foo", true},
		{"cog-image.20260508T153042Z", true},
		{"cog-weight.x.y", true},
		{"cog-", true},
		{"v2", false},
		{"20260508T153042Z", false},
		{"latest", false},
		{"", false},
		// Substring match must not trigger; the prefix must be at
		// position 0.
		{"my-cog-tag", false},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, IsReservedTag(tc.input))
		})
	}
}
