// Command gen-schema regenerates config.schema.json from the ConfigV1 struct
// tags in cmd/configfile.go. Run it with `go generate ./...` (see the
// TestConfigSchemaUpToDate fails the build if the committed file is stale.
//
//go:generate directive on ConfigV1) after changing the config schema; cmd's
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Cyb3r-Jak3/email-backup-tool/cmd"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	schema, err := cmd.GenerateConfigSchema()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	root, err := repoRoot()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "config.schema.json"), data, 0o644) //nolint:gosec // This is a build tool, not a security-sensitive operation.
}

// repoRoot finds the module root by walking up from the working directory
// looking for go.mod, so `go generate ./...` writes the schema to the same
// place regardless of which directory it's invoked from.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find go.mod above %s", dir)
		}
		dir = parent
	}
}
