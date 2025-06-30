package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/spf13/cobra"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/replicate/cog/pkg/util/files"
)

// images to download and push to the registry. Keep the images sizes small since they're stored in git.
// For reference, the `alpine:latest` image for `linux/amd64` ~3.5MB compressed.
var images = []struct {
	Image          string
	Platforms      []string
	SinglePlatform string
}{
	{
		Image: "alpine:latest",
		Platforms: []string{
			"linux/amd64",
			"linux/arm64",
		},
	},
}

// relative to the root of the repo
var destinationDir string = "pkg/registry_testhelpers/testdata"

func main() {
	rootCmd := &cobra.Command{
		Use: "test-registry-util",
	}
	rootCmd.PersistentFlags().StringVar(&destinationDir, "storage-dir", destinationDir, "path to the directory where the registry will store its data")

	rootCmd.AddCommand(
		&cobra.Command{
			Use: "init",
			RunE: func(cmd *cobra.Command, args []string) error {
				return runAndInit(cmd.Context(), destinationDir)
			},
		},
	)
	rootCmd.AddCommand(
		&cobra.Command{
			Use: "catalog",
			RunE: func(cmd *cobra.Command, args []string) error {
				return runAndCatalog(cmd.Context(), destinationDir)
			},
		},
	)

	rootCmd.AddCommand(
		&cobra.Command{
			Use: "run",
			RunE: func(cmd *cobra.Command, args []string) error {
				ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
				defer cancel()

				c, port, err := startRegistryTC(cmd.Context(), destinationDir)
				if err != nil {
					return err
				}
				defer func() {
					if err := c.Terminate(cmd.Context()); err != nil {
						fmt.Println("Failed to terminate registry:", err)
					}
				}()

				fmt.Println("Registry running at", fmt.Sprintf("localhost:%d", port))

				<-ctx.Done()
				return nil
			},
		},
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println("Failed to run:", err)
		os.Exit(1)
	}
}

func runAndInit(ctx context.Context, dstDir string) error {
	if empty, err := files.IsEmpty(dstDir); err != nil {
		return fmt.Errorf("failed to check if destination directory is empty: %w", err)
	} else if !empty {
		return fmt.Errorf("destination directory %s is not empty", dstDir)
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "test-registry-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	reg, hostPort, err := startRegistryTC(ctx, tmpDir)
	if err != nil {
		return err
	}
	defer func() {
		if err := reg.Terminate(ctx); err != nil {
			fmt.Println("Failed to terminate registry:", err)
		}
	}()

	addr := fmt.Sprintf("localhost:%d", hostPort)
	for _, src := range images {
		destRepo := fmt.Sprintf("%s/%s", addr, strings.Split(src.Image, ":")[0]) // e.g. localhost:5000/alpine
		tagPart := strings.Split(src.Image, ":")[1]

		if src.SinglePlatform != "" {
			osArch := strings.SplitN(src.SinglePlatform, "/", 2)
			plat := v1.Platform{OS: osArch[0], Architecture: osArch[1]}

			// Pull source image for specified platform
			srcRef, err := name.ParseReference(src.Image)
			if err != nil {
				return fmt.Errorf("parse reference: %w", err)
			}
			srcImg, err := remote.Image(srcRef, remote.WithPlatform(plat), remote.WithContext(ctx))
			if err != nil {
				return err
			}

			// Push with desired tag
			destRef, err := name.ParseReference(fmt.Sprintf("%s:%s", destRepo, tagPart), name.Insecure)
			if err != nil {
				return fmt.Errorf("parse reference: %w", err)
			}
			if err := remote.Write(destRef, srcImg,
				remote.WithContext(ctx), remote.WithAuth(authn.Anonymous)); err != nil {
				return fmt.Errorf("write %s: %w", destRef, err)
			}
			fmt.Printf("✅ pushed single-platform image %s\n", destRef.Name())
			continue
		}

		var idx v1.ImageIndex = mutate.IndexMediaType(empty.Index, types.OCIImageIndex) // start empty

		for _, platStr := range src.Platforms {
			osArch := strings.SplitN(platStr, "/", 2)
			plat := v1.Platform{OS: osArch[0], Architecture: osArch[1]}

			// 1. pull source manifest for this platform
			srcRef, err := name.ParseReference(src.Image)
			if err != nil {
				return fmt.Errorf("parse reference: %w", err)
			}
			srcImg, err := remote.Image(srcRef, remote.WithPlatform(plat), remote.WithContext(ctx))
			if err != nil {
				return err
			}

			// 2. push it *by digest* into the new registry
			digest, _ := srcImg.Digest()
			destDigestRef, err := name.ParseReference(fmt.Sprintf("%s@%s", destRepo, digest.String()), name.Insecure)
			if err != nil {
				return fmt.Errorf("parse reference: %w", err)
			}
			if err := remote.Write(destDigestRef, srcImg,
				remote.WithContext(ctx), remote.WithAuth(authn.Anonymous)); err != nil {
				return fmt.Errorf("write %s: %w", destDigestRef, err)
			}

			// 3. add it to the (soon‑to‑be) index
			idx = mutate.AppendManifests(idx,
				mutate.IndexAddendum{Add: srcImg, Descriptor: v1.Descriptor{Platform: &plat}})

			fmt.Printf("✅ pushed %s for %s/%s\n", destDigestRef.Name(), plat.OS, plat.Architecture)
		}

		// 4. push the assembled index and tag it
		indexTag, err := name.ParseReference(fmt.Sprintf("%s:%s", destRepo, tagPart), name.Insecure)
		if err != nil {
			return fmt.Errorf("parse reference: %w", err)
		}
		if err := remote.WriteIndex(indexTag, idx,
			remote.WithContext(ctx), remote.WithAuth(authn.Anonymous)); err != nil {
			return fmt.Errorf("write index %s: %w", indexTag, err)
		}
		fmt.Printf("🏷️  tagged multi-arch index %s\n", indexTag.Name())
	}

	fmt.Println("Copying registry data to", dstDir)
	if err := os.CopyFS(dstDir, os.DirFS(tmpDir)); err != nil {
		return fmt.Errorf("failed to copy registry data: %w", err)
	}

	if err := catalog(ctx, addr); err != nil {
		return fmt.Errorf("catalog tree: %w", err)
	}

	return nil
}

func runAndCatalog(ctx context.Context, dir string) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	reg, _, err := startRegistryTC(ctx, dir)
	if err != nil {
		return err
	}
	defer func() {
		if err := reg.Terminate(ctx); err != nil {
			fmt.Println("Failed to terminate registry:", err)
		}
	}()

	if err := catalog(ctx, reg.RegistryName); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}

	return nil
}

func catalog(ctx context.Context, addr string) error {
	opts := []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuth(authn.Anonymous), // local registry
	}

	reg, err := name.NewRegistry(addr, name.Insecure)
	if err != nil {
		return fmt.Errorf("new registry: %w", err)
	}

	// first, list all repositories
	repos, err := remote.Catalog(ctx, reg, opts...)
	if err != nil {
		return err
	}

	for _, repoName := range repos {
		repo := reg.Repo(repoName)

		// second, list all tags
		tagNames, err := remote.List(repo, opts...)
		if err != nil {
			return err
		}

		for _, tagName := range tagNames {
			// third, get the manifest
			ref, err := name.ParseReference(fmt.Sprintf("%s/%s:%s", addr, repoName, tagName))
			if err != nil {
				return fmt.Errorf("parse reference: %w", err)
			}
			desc, err := remote.Get(ref, opts...)
			if err != nil {
				return err
			}

			repoTag := fmt.Sprintf("%s:%s", ref.Context().RepositoryStr(), ref.Identifier())

			switch mt := desc.Descriptor.MediaType; mt {
			case types.OCIImageIndex, types.DockerManifestList:

				fmt.Printf("%s %s\n  index -> %s\n", repoTag, mt, desc.Descriptor.Digest)

				idx, _ := desc.ImageIndex()
				im, _ := idx.IndexManifest()
				for _, m := range im.Manifests {
					fmt.Printf("  %s -> %s\n",
						m.Platform.String(),
						m.Digest,
					)
				}

			default: // single‑platform image
				fmt.Printf("%s %s\n  single platform image -> %s\n", repoTag, mt, desc.Descriptor.Digest)
			}
		}
	}
	return nil

}

func startRegistryTC(ctx context.Context, dir string) (*registry.RegistryContainer, int, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get absolute path: %w", err)
	}

	reg, err := registry.Run(ctx,
		"registry:3",
		testcontainers.WithHostConfigModifier(func(hostConfig *container.HostConfig) {
			hostConfig.Mounts = []mount.Mount{
				{
					Type:   "bind",
					Source: dir,
					Target: "/var/lib/registry",
				},
			}
		}),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/v2/").WithPort("5000/tcp").
				WithStartupTimeout(10*time.Second),
		),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("start registry: %w", err)
	}

	port, err := reg.MappedPort(ctx, "5000/tcp")
	if err != nil {
		if err := reg.Terminate(ctx); err != nil {
			fmt.Println("Failed to terminate registry:", err)
		}
		return nil, 0, fmt.Errorf("mapped port: %w", err)
	}
	return reg, port.Int(), nil
}
