package sandbox

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// CacheConfig describes how to mount a dependency cache for a build system.
type CacheConfig struct {
	VolumeName    string // Docker volume name
	ContainerPath string // mount point inside the container
}

// buildSystemCachePaths maps build system names to the container-internal
// directory that holds downloaded dependencies.  Mounting a persistent
// Docker volume at this path avoids re-downloading on every build.
var buildSystemCachePaths = map[string]string{
	"go":      "/go/pkg/mod",
	"node":    "/home/builder/.npm",
	"python":  "/home/builder/.cache/pip",
	"gradle":  "/home/builder/.gradle/caches",
	"maven":   "/home/builder/.m2/repository",
	"rust":    "/home/builder/.cargo/registry",
	"cmake":   "/home/builder/.cache/cmake",
	"make":    "",
	"custom":  "",
	"unknown": "",
}

// CacheVolumePrefix is the Docker volume name prefix for all sandbox caches.
const CacheVolumePrefix = "rtvortex-depcache"

// MaxCacheVolumes is a safety limit on the number of cache volumes to
// prevent unbounded disk usage.  Each volume is per (repo, build_system).
const MaxCacheVolumes = 500

// ResolveCacheConfig returns a CacheConfig for the given build system and
// repo.  Returns nil if the build system does not support caching.
func ResolveCacheConfig(repoID, buildSystem string) *CacheConfig {
	containerPath, ok := buildSystemCachePaths[buildSystem]
	if !ok || containerPath == "" {
		return nil
	}

	return &CacheConfig{
		VolumeName:    cacheVolumeName(repoID, buildSystem),
		ContainerPath: containerPath,
	}
}

// CacheDockerArgs returns the docker volume-mount arguments for the cache,
// or nil if no cache is configured.
func CacheDockerArgs(cfg *CacheConfig) []string {
	if cfg == nil {
		return nil
	}
	return []string{
		"-v", fmt.Sprintf("%s:%s", cfg.VolumeName, cfg.ContainerPath),
	}
}

// cacheVolumeName generates a deterministic Docker volume name from repo + build system.
// Format: rtvortex-depcache-{sha256prefix}-{build_system}
// The sha256 prefix avoids leaking the repo ID into volume names.
func cacheVolumeName(repoID, buildSystem string) string {
	h := sha256.Sum256([]byte(repoID))
	prefix := fmt.Sprintf("%x", h[:6])
	return fmt.Sprintf("%s-%s-%s", CacheVolumePrefix, prefix, sanitiseBuildSystem(buildSystem))
}

func sanitiseBuildSystem(bs string) string {
	bs = strings.ToLower(bs)
	bs = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, bs)
	if len(bs) > 20 {
		bs = bs[:20]
	}
	return bs
}
