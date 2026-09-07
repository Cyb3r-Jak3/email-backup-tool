package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

func runInit(t *testing.T, args ...string) error {
	t.Helper()
	logger = zap.NewNop()
	app := &cli.Command{Name: "test", Commands: []*cli.Command{InitCommand()}}
	return app.Run(t.Context(), append([]string{"test", "init"}, args...))
}

func TestInitWritesAParsableAndValidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	if err := runInit(t, "--output", path); err != nil {
		t.Fatalf("init: %v", err)
	}

	file, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("the written template must parse and validate on its own: %v", err)
	}
	if file.Version != LatestConfigVersion {
		t.Fatalf("Version = %d, want %d", file.Version, LatestConfigVersion)
	}
}

func TestInitRefusesToOverwriteWithoutForce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	if err := runInit(t, "--output", path); err != nil {
		t.Fatalf("first init: %v", err)
	}
	if err := runInit(t, "--output", path); err == nil {
		t.Fatal("expected the second init to fail without --force")
	}
	if err := runInit(t, "--output", path, "--force"); err != nil {
		t.Fatalf("init --force: %v", err)
	}
}

func TestInitCreatesOutputDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "config.yaml")

	if err := runInit(t, "--output", path); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}
