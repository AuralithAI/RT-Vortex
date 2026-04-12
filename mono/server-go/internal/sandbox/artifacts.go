package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ArtifactKind identifies the type of build artifact.
type ArtifactKind string

const (
	ArtifactTestReport ArtifactKind = "test_report"
	ArtifactCoverage   ArtifactKind = "coverage"
	ArtifactBinary     ArtifactKind = "binary"
	ArtifactLog        ArtifactKind = "log"
	ArtifactGeneric    ArtifactKind = "generic"
)

// BuildArtifact is a single file or blob extracted from a build container.
type BuildArtifact struct {
	ID        uuid.UUID    `json:"id"`
	BuildID   uuid.UUID    `json:"build_id"`
	Kind      ArtifactKind `json:"kind"`
	Path      string       `json:"path"`
	SizeBytes int64        `json:"size_bytes"`
	Data      []byte       `json:"-"`
	CreatedAt time.Time    `json:"created_at"`
}

// ArtifactCollectorConfig describes which files to extract from the container.
type ArtifactCollectorConfig struct {
	Paths []string `json:"paths"`
}

// MaxArtifactSize is the maximum size of a single artifact (10 MB).
const MaxArtifactSize = 10 * 1024 * 1024

// MaxArtifactsPerBuild limits the number of artifacts collected.
const MaxArtifactsPerBuild = 20

// MaxTotalArtifactBytes limits total artifact data per build (50 MB).
const MaxTotalArtifactBytes = 50 * 1024 * 1024

// DefaultArtifactPaths are well-known build output locations collected
// when no explicit paths are configured.
var DefaultArtifactPaths = []string{
	"build/reports/tests/",
	"target/surefire-reports/",
	"target/site/jacoco/",
	"coverage/",
	"htmlcov/",
	".coverage",
	"test-results/",
	"junit.xml",
	"test-report.xml",
	"coverage.xml",
	"lcov.info",
	"cover.out",
	"cobertura.xml",
}

// ClassifyArtifact determines the artifact kind from its path.
func ClassifyArtifact(path string) ArtifactKind {
	lower := strings.ToLower(path)
	base := strings.ToLower(filepath.Base(path))

	if strings.Contains(lower, "coverage") ||
		strings.Contains(lower, "jacoco") ||
		strings.Contains(lower, "htmlcov") ||
		base == ".coverage" ||
		base == "lcov.info" ||
		base == "cover.out" ||
		base == "cobertura.xml" ||
		base == "coverage.xml" {
		return ArtifactCoverage
	}

	if strings.Contains(lower, "test-report") ||
		strings.Contains(lower, "test-results") ||
		strings.Contains(lower, "surefire-reports") ||
		base == "junit.xml" ||
		base == "test-report.xml" {
		return ArtifactTestReport
	}

	if base == "build.log" || base == "output.log" {
		return ArtifactLog
	}

	return ArtifactGeneric
}

// CollectArtifacts copies files from a stopped container by running
// `docker cp` and returns the extracted artifacts.
func CollectArtifacts(ctx context.Context, containerName string, buildID uuid.UUID, paths []string) ([]*BuildArtifact, error) {
	if len(paths) == 0 {
		paths = DefaultArtifactPaths
	}

	var artifacts []*BuildArtifact
	var totalBytes int64

	for _, p := range paths {
		if len(artifacts) >= MaxArtifactsPerBuild {
			break
		}

		srcPath := containerName + ":" + p

		cmd := exec.CommandContext(ctx, "docker", "cp", srcPath, "-")
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		if err := cmd.Run(); err != nil {
			continue
		}

		extracted, err := extractTarEntries(stdout.Bytes(), buildID, p)
		if err != nil {
			continue
		}

		for _, a := range extracted {
			if totalBytes+a.SizeBytes > MaxTotalArtifactBytes {
				break
			}
			if len(artifacts) >= MaxArtifactsPerBuild {
				break
			}
			totalBytes += a.SizeBytes
			artifacts = append(artifacts, a)
		}
	}

	return artifacts, nil
}

// extractTarEntries reads a tar stream (as returned by docker cp) and
// returns BuildArtifact entries for each file.
func extractTarEntries(data []byte, buildID uuid.UUID, basePath string) ([]*BuildArtifact, error) {
	reader := bytes.NewReader(data)
	tr := tar.NewReader(reader)

	var artifacts []*BuildArtifact

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return artifacts, fmt.Errorf("tar read: %w", err)
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		if header.Size > MaxArtifactSize {
			continue
		}

		buf := make([]byte, header.Size)
		if _, err := io.ReadFull(tr, buf); err != nil {
			continue
		}

		fullPath := filepath.Join(basePath, header.Name)

		artifacts = append(artifacts, &BuildArtifact{
			ID:        uuid.New(),
			BuildID:   buildID,
			Kind:      ClassifyArtifact(fullPath),
			Path:      fullPath,
			SizeBytes: header.Size,
			Data:      buf,
			CreatedAt: time.Now().UTC(),
		})

		if len(artifacts) >= MaxArtifactsPerBuild {
			break
		}
	}

	return artifacts, nil
}

// WorkspaceArchive creates a gzipped tar archive from a changeset map
// (file path → content).  This archive is injected into the container
// at /workspace so the build runs against the proposed changes.
func WorkspaceArchive(changeset map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for path, content := range changeset {
		if content == "" {
			continue
		}

		cleanPath := strings.TrimLeft(path, "/")
		data := []byte(content)

		header := &tar.Header{
			Name:    cleanPath,
			Mode:    0644,
			Size:    int64(len(data)),
			ModTime: time.Now().UTC(),
		}

		if err := tw.WriteHeader(header); err != nil {
			tw.Close()
			gw.Close()
			return nil, fmt.Errorf("artifact: tar header: %w", err)
		}

		if _, err := tw.Write(data); err != nil {
			tw.Close()
			gw.Close()
			return nil, fmt.Errorf("artifact: tar write: %w", err)
		}
	}

	if err := tw.Close(); err != nil {
		gw.Close()
		return nil, fmt.Errorf("artifact: tar close: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("artifact: gzip close: %w", err)
	}

	return buf.Bytes(), nil
}

// WorkspaceSize returns the total byte size of a changeset.
func WorkspaceSize(changeset map[string]string) int64 {
	var total int64
	for _, content := range changeset {
		total += int64(len(content))
	}
	return total
}

// MaxWorkspaceBytes is the maximum size of workspace changeset (100 MB).
const MaxWorkspaceBytes int64 = 100 * 1024 * 1024
