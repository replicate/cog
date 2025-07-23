package testenv

import (
	"bytes"

	"github.com/docker/docker/api/types/registry"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/require"
)

type TestRegistry struct {
	env *DockerTestEnv
}

func (r *TestRegistry) ToRegistryRef(ref name.Reference) name.Reference {
	t := r.env.t
	t.Helper()

	return r.ParseRef(ref.Name())
}

func (r *TestRegistry) ParseRef(s string) name.Reference {
	t := r.env.t
	t.Helper()

	ref, err := name.ParseReference(s, name.WithDefaultRegistry(r.env.internalRegistryHost))
	require.NoError(t, err, "failed to parse reference")

	return ref
}

func (r *TestRegistry) RegistryRef() name.Registry {
	t := r.env.t
	t.Helper()

	ref, err := name.NewRegistry(r.env.externalRegistryHost)
	require.NoError(t, err, "failed to create registry reference")

	return ref
}

func (r *TestRegistry) RegistryAuthConfig() registry.AuthConfig {
	return registry.AuthConfig{}
}

func (r *TestRegistry) EncodedRegistryAuth() string {
	t := r.env.t
	t.Helper()

	authConfig := r.RegistryAuthConfig()
	encodedAuth, err := registry.EncodeAuthConfig(authConfig)
	require.NoError(t, err, "failed to encode auth config")
	return encodedAuth
}

func (r *TestRegistry) ListRepositories() []string {
	t := r.env.t
	t.Helper()

	repos, err := remote.Catalog(t.Context(), r.RegistryRef(), remote.WithAuth(authn.Anonymous))
	require.NoError(t, err, "failed to list repositories")

	return repos
}

func (r *TestRegistry) ListTags(ref string) []string {
	t := r.env.t
	t.Helper()

	tags, err := remote.List(r.RegistryRef().Repo(ref), remote.WithContext(t.Context()), remote.WithAuth(authn.Anonymous))
	require.NoError(t, err, "failed to list tags")

	return tags
}

func (r *TestRegistry) convertRefToInternal(ref name.Reference) name.Reference {
	t := r.env.t
	t.Helper()

	if tagRef, ok := ref.(name.Tag); ok {
		return r.RegistryRef().Repo(tagRef.Context().RepositoryStr()).Tag(tagRef.Identifier())
	}

	if digestRef, ok := ref.(name.Digest); ok {
		return r.RegistryRef().Repo(digestRef.Context().RepositoryStr()).Digest(digestRef.Identifier())
	}

	require.Fail(t, "not a tag or digest")
	return nil
}

func (r *TestRegistry) RegistryImageExists(ref name.Reference, opts ...remote.Option) bool {
	t := r.env.t
	t.Helper()

	internalRef := r.convertRefToInternal(ref)

	_, err := remote.Head(internalRef, append(opts, remote.WithContext(t.Context()), remote.WithAuth(authn.Anonymous))...)
	return err == nil
}

func (r *TestRegistry) ParseReference(s string) name.Reference {
	t := r.env.t
	t.Helper()

	ref, err := name.ParseReference(s, name.WithDefaultRegistry(r.env.internalRegistryHost))
	require.NoError(t, err, "failed to parse reference")

	return ref
}

func (r *TestRegistry) RegistryGetDescriptor(ref name.Reference, opts ...remote.Option) *remote.Descriptor {
	t := r.env.t
	t.Helper()

	internalRef := r.convertRefToInternal(ref)

	descriptor, err := remote.Get(internalRef, append(opts, remote.WithContext(t.Context()), remote.WithAuth(authn.Anonymous))...)
	require.NoError(t, err, "failed to get descriptor")

	return descriptor
}

func (r *TestRegistry) RegistryGetManifest(ref name.Reference, opts ...remote.Option) *v1.Manifest {
	t := r.env.t
	t.Helper()

	internalRef := r.convertRefToInternal(ref)

	descriptor, err := remote.Get(internalRef, append(opts, remote.WithContext(t.Context()), remote.WithAuth(authn.Anonymous))...)
	require.NoError(t, err, "failed to get descriptor")

	manifest, err := v1.ParseManifest(bytes.NewReader(descriptor.Manifest))
	require.NoError(t, err, "failed to parse manifest")

	return manifest
}

func (r *TestRegistry) RegistryGetImage(ref name.Reference, opts ...remote.Option) v1.Image {
	t := r.env.t
	t.Helper()

	internalRef := r.convertRefToInternal(ref)

	img, err := remote.Image(internalRef, append(opts, remote.WithContext(t.Context()), remote.WithAuth(authn.Anonymous))...)
	require.NoError(t, err, "failed to get image")

	return img
}
