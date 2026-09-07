package store

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Cyb3r-Jak3/email-backup-tool/stages"
	"go.uber.org/zap"
)

// Tar is stage 3 writing artifacts as entries in a tar archive. The archive is
// gzip compressed when its path ends in .gz or .tgz.
type Tar struct {
	path   string
	file   *os.File
	gzip   *gzip.Writer
	tar    *tar.Writer
	logger *zap.Logger
	closed bool
}

// compile-time check that Tar satisfies stage 3.
var _ stages.Sink = (*Tar)(nil)

// NewTar creates the archive. It is truncated if it already exists, since one
// run produces one archive.
func NewTar(path string, logger *zap.Logger) (*Tar, error) {
	if path == "" {
		return nil, fmt.Errorf("save.tar.path is required")
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // path is user supplied by design
	if err != nil {
		return nil, fmt.Errorf("creating %s: %w", path, err)
	}

	sink := &Tar{path: path, file: file, logger: logger}
	var out io.Writer = file
	if compressed(path) {
		sink.gzip = gzip.NewWriter(file)
		out = sink.gzip
	}
	sink.tar = tar.NewWriter(out)
	logger.Info("Storing backup in a tar archive",
		zap.String("path", path),
		zap.Bool("gzip", sink.gzip != nil),
	)
	return sink, nil
}

// Name implements stages.Sink.
func (t *Tar) Name() string { return "tar:" + t.path }

// Store implements stages.Sink by appending the artifact as an archive entry.
func (t *Tar) Store(_ context.Context, artifact *stages.Artifact) error {
	modified := time.Now()
	if artifact.Message != nil && !artifact.Message.Date.IsZero() {
		modified = artifact.Message.Date
	}
	header := &tar.Header{
		Name:    artifact.Path,
		Mode:    0o600,
		Size:    int64(len(artifact.Data)),
		ModTime: modified,
		Format:  tar.FormatPAX, // PAX keeps long paths and sub-second times intact
	}
	if err := t.tar.WriteHeader(header); err != nil {
		return fmt.Errorf("writing tar header for %s: %w", artifact.Path, err)
	}
	if _, err := t.tar.Write(artifact.Data); err != nil {
		return fmt.Errorf("writing tar entry %s: %w", artifact.Path, err)
	}
	return nil
}

// Close implements stages.Sink, finalising the archive. A tar file is only
// valid once its trailer is written, so this matters even on a failed run.
func (t *Tar) Close() error {
	if t.closed {
		return nil
	}
	t.closed = true
	if err := t.tar.Close(); err != nil {
		_ = t.file.Close()
		return fmt.Errorf("closing tar %s: %w", t.path, err)
	}
	if t.gzip != nil {
		if err := t.gzip.Close(); err != nil {
			_ = t.file.Close()
			return fmt.Errorf("closing gzip %s: %w", t.path, err)
		}
	}
	if err := t.file.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", t.path, err)
	}
	return nil
}

// compressed reports whether the archive path asks for gzip compression.
func compressed(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".gz") || strings.HasSuffix(lower, ".tgz")
}
