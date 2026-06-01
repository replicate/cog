// Package paths resolves on-disk locations for Cog's per-user caches.
//
// Callers SHOULD route every cache lookup through this package so a single
// environment variable (COG_CACHE_DIR) can relocate all of Cog's caches in
// one step — useful when the default cache directory lives on a different
// filesystem than the user's project tree and hardlinking would fail with
// EXDEV.
package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// envCacheDir lets users override the default cache root. When set, it
// replaces the default entirely.
const envCacheDir = "COG_CACHE_DIR"

// envXDGCache is the XDG Base Directory Specification cache-home
// variable. Respected on every platform (not just Linux) so users who
// have set it get what they expect.
const envXDGCache = "XDG_CACHE_HOME"

// WeightsStoreDir returns the directory that backs the local
// content-addressed weight file store.
//
// Resolution order:
//
//  1. $COG_CACHE_DIR/weights, if COG_CACHE_DIR is set.
//  2. $XDG_CACHE_HOME/cog/weights, if set.
//  3. $HOME/.cache/cog/weights otherwise — on every platform.
//
// Note the deliberate deviation from os.UserCacheDir, which returns
// $HOME/Library/Caches on macOS. Dev tooling conventionally lives under
// ~/.cache or ~/.<toolname>, not ~/Library, so we follow suit.
//
// WeightsStoreDir does not create the directory; callers that need it
// to exist should MkdirAll it themselves (FileStore does).
func WeightsStoreDir() (string, error) {
	if dir := os.Getenv(envCacheDir); dir != "" {
		return filepath.Join(dir, "weights"), nil
	}
	if dir := os.Getenv(envXDGCache); dir != "" {
		return filepath.Join(dir, "cog", "weights"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	if home == "" {
		return "", errors.New("user home dir is empty")
	}
	return filepath.Join(home, ".cache", "cog", "weights"), nil
}
