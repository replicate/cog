package dockertest

import (
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/stretchr/testify/require"
)

type Ref struct {
	t   *testing.T
	ref name.Reference
}

func NewRef(t *testing.T) Ref {
	t.Helper()

	repoName := strings.ToLower(t.Name())
	// Replace any characters that aren't valid in a docker image repo name with underscore
	// Valid characters are: a-z, 0-9, ., _, -, /
	repoName = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' || r == '/' {
			return r
		}
		return '_'
	}, repoName)

	ref, err := name.ParseReference(repoName, name.WithDefaultRegistry(""))
	require.NoError(t, err, "Failed to create reference for test")

	return Ref{t: t, ref: ref}
}

func (r Ref) WithTag(tagName string) Ref {
	tagRef := r.ref.Context().Tag(tagName)
	return Ref{t: r.t, ref: tagRef}
}

func (r Ref) WithDigest(digest string) Ref {
	digestRef := r.ref.Context().Digest(digest)
	return Ref{t: r.t, ref: digestRef}
}

func (r Ref) WithRegistry(registry string) Ref {
	reg, err := name.NewRegistry(registry)
	require.NoError(r.t, err, "Failed to create registry for test")

	repo := r.ref.Context()
	repo.Registry = reg
	var newRef name.Reference
	switch r.ref.(type) {
	case name.Tag:
		newRef = repo.Tag(r.ref.Identifier())
	case name.Digest:
		newRef = repo.Digest(r.ref.Identifier())
	default:
		require.Fail(r.t, "Unsupported reference type")
	}

	return Ref{t: r.t, ref: newRef}
}

func (r Ref) WithoutRegistry() Ref {
	repo := r.ref.Context()
	repo.Registry = name.Registry{}
	var newRef name.Reference
	switch r.ref.(type) {
	case name.Tag:
		newRef = repo.Tag(r.ref.Identifier())
	case name.Digest:
		newRef = repo.Digest(r.ref.Identifier())
	default:
		require.Fail(r.t, "Unsupported reference type")
	}

	return Ref{t: r.t, ref: newRef}
}

func (r Ref) String() string {
	return r.ref.Name()
}
