// Package store holds stage 3 implementations: the storage backends a backup is
// written to. Each implements stages.Sink, so a run can send its artifacts to a
// local directory, a tar archive, an S3-compatible bucket, or several at once.
package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Cyb3r-Jak3/email-backup-tool/stages"
	"go.uber.org/zap"
)

// Local is stage 3 writing artifacts as files under a directory.
type Local struct {
	root   string
	logger *zap.Logger
}

// compile-time check that Local satisfies stage 3.
var _ stages.Sink = (*Local)(nil)

// NewLocal prepares a directory sink, creating the root directory if needed.
func NewLocal(directory string, logger *zap.Logger) (*Local, error) {
	if directory == "" {
		return nil, fmt.Errorf("save.local.directory is required")
	}
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return nil, fmt.Errorf("creating %s: %w", directory, err)
	}
	logger.Info("Storing backup in a local directory", zap.String("directory", directory))
	return &Local{root: directory, logger: logger}, nil
}

// Name implements stages.Sink.
func (l *Local) Name() string { return "local:" + l.root }

// Store implements stages.Sink by writing the artifact to its path under the
// root directory.
func (l *Local) Store(_ context.Context, artifact *stages.Artifact) error {
	target, err := l.resolve(artifact.Path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(target), err)
	}
	if err := os.WriteFile(target, artifact.Data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", target, err)
	}
	return nil
}

// Close implements stages.Sink. Files are written as they arrive, so there is
// nothing to finalise.
func (l *Local) Close() error { return nil }

// resolve turns an artifact path into an absolute file path, refusing anything
// that would land outside the root directory.
func (l *Local) resolve(path string) (string, error) {
	target := filepath.Join(l.root, filepath.FromSlash(path))
	rel, err := filepath.Rel(l.root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact path %q escapes %s", path, l.root)
	}
	return target, nil
}
