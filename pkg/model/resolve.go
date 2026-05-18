package model

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"

	"github.com/replicate/cog/pkg/global"
)

// Environment overrides for the resolved model reference. Only
// consulted when the configuration produces a model ref; otherwise
// ignored.
const (
	// EnvModel is the full reference override. When set, it bypasses
	// every other source and the value is parsed as a complete ref
	// (registry/repo[:tag] or registry/repo@digest). If no tag is
	// present, an auto-generated timestamp tag is appended.
	EnvModel = "COG_MODEL"

	// EnvModelRegistry overrides only the registry host of the
	// resolved reference (e.g. "registry.example.com").
	EnvModelRegistry = "COG_MODEL_REGISTRY"

	// EnvModelRepo overrides only the repository path of the resolved
	// reference (e.g. "acct/testing/model"). The value must not include
	// a host, tag, or digest.
	EnvModelRepo = "COG_MODEL_REPO"

	// EnvModelTag overrides only the tag of the resolved reference.
	// User-supplied tags starting with "cog-" are rejected because that
	// prefix is reserved for tags Cog generates itself (cog-image.*,
	// cog-weight.*).
	EnvModelTag = "COG_MODEL_TAG"
)

// timestampTagFormat is ISO 8601 basic (compact) form, UTC. Example:
// 20260508T153042Z. The basic form has no separators so the value is
// a valid OCI tag as-is; Go's stdlib only ships the extended form
// (time.RFC3339, "2006-01-02T15:04:05Z07:00") which contains "-" and
// ":" — the colon is illegal in tags.
const timestampTagFormat = "20060102T150405Z"

// tagRegex enforces the OCI distribution-spec tag grammar. We pin to
// the spec rather than delegating to name.NewTag because the
// go-containerregistry parser is more permissive (it accepts a leading
// hyphen, for example) and we want to reject inputs the spec rules out.
var tagRegex = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)

// ResolvedRef is the fully composed model reference produced by
// ResolveModelRef. Exactly one of Tag or Digest is populated.
type ResolvedRef struct {
	// Registry is the registry host (e.g. "registry.example.com").
	Registry string
	// Repo is the repository path (e.g. "acct/testing/model").
	Repo string
	// Tag is the resolved tag (e.g. "20260508T153042Z" or "v2").
	// Empty for digest refs.
	Tag string
	// Digest is the OCI manifest digest (e.g. "sha256:abc..."), set
	// when the user pinned the ref with @digest. Empty for tag refs.
	Digest string
}

// String returns the canonical "registry/repo:tag" or
// "registry/repo@digest" form.
func (r *ResolvedRef) String() string {
	if r.Digest != "" {
		return r.Repository() + "@" + r.Digest
	}
	return r.Repository() + ":" + r.Tag
}

// Repository returns the bare "registry/repo" prefix, with no tag or
// digest. Use this as the base for constructing related refs (image
// manifest tag, weight manifest tag, weight repo@digest).
func (r *ResolvedRef) Repository() string {
	return r.Registry + "/" + r.Repo
}

// ErrNoModelRef is returned when no model reference can be determined
// from configuration or environment overrides. Callers handle this by
// either falling back to FormatImage (when an image: ref exists) or
// surfacing it to the user.
var ErrNoModelRef = errors.New("no model ref — set 'model' in cog.yaml or COG_MODEL")

// ErrImageModelEnvConflict is returned when cog.yaml has `image:` set
// and the COG_MODEL* env vars promote to a resolvable model ref.
// cog.yaml's schema enforces image:/model: as mutex, but env-var
// promotion bypasses that check.
var ErrImageModelEnvConflict = errors.New(
	"'image' in cog.yaml cannot be combined with COG_MODEL* env vars\n" +
		"  remove 'image' from cog.yaml (use 'model' instead), or\n" +
		"  unset COG_MODEL, COG_MODEL_REGISTRY, COG_MODEL_REPO, COG_MODEL_TAG",
)

// GenerateTimestampTag returns the current UTC time formatted as a
// compact ISO 8601 timestamp suitable for use as an OCI tag. Calls
// within the same second return identical values.
func GenerateTimestampTag() string {
	return time.Now().UTC().Format(timestampTagFormat)
}

// ResolveModelRef composes the final model reference from the
// cog.yaml `model` field plus any COG_MODEL* environment overrides.
//
// Resolution algorithm:
//
//  1. If COG_MODEL is set, it wins outright. Parse it and append a
//     timestamp tag if none was supplied. All other env vars are
//     ignored. This matches the "I know exactly what I want" path
//     used by CI and `cog push`-from-script workflows.
//  2. Otherwise, start from the cog.yaml `model` value (which may be
//     empty or partial) and layer the per-field env vars on top:
//     registry ← COG_MODEL_REGISTRY, repo ← COG_MODEL_REPO,
//     tag ← COG_MODEL_TAG (or a freshly generated timestamp).
//  3. If no repository can be determined, return ErrNoModelRef.
//  4. If a ref resolved and `image:` is also set, return
//     ErrImageModelEnvConflict.
//
// configImage and configModel are the raw cog.yaml `image` and
// `model` values; pass "" if absent. The function only reads
// environment variables — it never touches the filesystem or network.
func ResolveModelRef(configImage, configModel string) (*ResolvedRef, error) {
	ref, err := resolveModelRef(configModel)
	if err != nil {
		return nil, err
	}
	if ref != nil && configImage != "" {
		return nil, ErrImageModelEnvConflict
	}
	return ref, nil
}

// resolveModelRef does the env+config composition. ResolveModelRef
// wraps it with the image:/model: mode-mix check.
func resolveModelRef(configModel string) (*ResolvedRef, error) {
	if full := os.Getenv(EnvModel); full != "" {
		return resolveFromFullRef(full)
	}

	envRegistry := os.Getenv(EnvModelRegistry)
	envRepo := os.Getenv(EnvModelRepo)
	envTag := os.Getenv(EnvModelTag)

	if err := validateRegistryEnv(envRegistry); err != nil {
		return nil, err
	}
	if err := validateRepoEnv(envRepo); err != nil {
		return nil, err
	}
	if err := validateTagEnv(envTag); err != nil {
		return nil, err
	}

	// Parse the cog.yaml base. An empty configModel is fine — it just
	// means every component must come from env vars.
	var baseRegistry, baseRepo string
	if configModel != "" {
		parsedBase, err := name.NewRepository(configModel, name.Insecure)
		if err != nil {
			return nil, fmt.Errorf("invalid 'model' in cog.yaml: %w", err)
		}
		baseRegistry = parsedBase.RegistryStr()
		baseRepo = parsedBase.RepositoryStr()
	}

	registry := cmp.Or(envRegistry, baseRegistry, global.ReplicateRegistryHost)
	repo := cmp.Or(envRepo, baseRepo)
	if repo == "" {
		return nil, ErrNoModelRef
	}
	tag := cmp.Or(envTag, GenerateTimestampTag())

	return buildResolved(registry, repo, tag)
}

// resolveFromFullRef parses a complete COG_MODEL value. Pre-seeding
// name.WithDefaultTag with a fresh timestamp means an input without a
// tag silently picks up the auto-versioned tag, with no need to
// re-scan the source string. Digest refs pass through unchanged
// because the caller is pinning to a specific manifest.
func resolveFromFullRef(full string) (*ResolvedRef, error) {
	parsed, err := ParseRef(full, Insecure(), WithDefaultTag(GenerateTimestampTag()))
	if err != nil {
		return nil, fmt.Errorf("invalid %s value: %w", EnvModel, err)
	}
	return &ResolvedRef{
		Registry: parsed.Registry(),
		Repo:     parsed.Repository(),
		Tag:      parsed.Tag(),
		Digest:   parsed.Digest(),
	}, nil
}

// buildResolved composes a tag-based ResolvedRef and runs the result
// through name.NewTag to catch combinations that only fail when
// stitched together (an OCI-illegal tag, for example). Digest refs
// don't pass through here — resolveFromFullRef returns them directly.
func buildResolved(registry, repo, tag string) (*ResolvedRef, error) {
	r := &ResolvedRef{Registry: registry, Repo: repo, Tag: tag}
	if _, err := name.NewTag(r.String(), name.Insecure); err != nil {
		return nil, fmt.Errorf("invalid resolved ref %q: %w", r.String(), err)
	}
	return r, nil
}

// validateRegistryEnv checks that COG_MODEL_REGISTRY is just a host
// (no path, tag, or digest). The pre-check on / and @ exists so the
// error message names the env var; name.NewRegistry handles the rest.
func validateRegistryEnv(v string) error {
	if v == "" {
		return nil
	}
	if strings.ContainsAny(v, "/@") {
		return fmt.Errorf("invalid %s %q: must be a bare host (no path, tag, or digest)", EnvModelRegistry, v)
	}
	if _, err := name.NewRegistry(v, name.Insecure); err != nil {
		return fmt.Errorf("invalid %s %q: %w", EnvModelRegistry, v, err)
	}
	return nil
}

// validateRepoEnv checks that COG_MODEL_REPO is a bare repository
// path. The pre-check on : is load-bearing — name.NewRepository
// accepts "host:port/repo", so without it "user/model:v1" would be
// silently misparsed.
func validateRepoEnv(v string) error {
	if v == "" {
		return nil
	}
	if strings.ContainsAny(v, ":@") {
		return fmt.Errorf("invalid %s %q: must be a bare repository path (no host, tag, or digest)", EnvModelRepo, v)
	}
	if _, err := name.NewRepository(v, name.Insecure); err != nil {
		return fmt.Errorf("invalid %s %q: %w", EnvModelRepo, v, err)
	}
	return nil
}

// validateTagEnv checks that COG_MODEL_TAG is a valid OCI tag and
// does not use the reserved "cog-" prefix.
func validateTagEnv(v string) error {
	if v == "" {
		return nil
	}
	if IsReservedTag(v) {
		return fmt.Errorf("invalid %s %q: %q is a reserved prefix — choose a different tag", EnvModelTag, v, ReservedTagPrefix)
	}
	if !tagRegex.MatchString(v) {
		return fmt.Errorf("invalid %s %q: must match OCI tag regex %s", EnvModelTag, v, tagRegex.String())
	}
	return nil
}
