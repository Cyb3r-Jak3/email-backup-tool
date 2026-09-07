package cmd

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

// configTemplate is the content written by the "init" command: a minimal,
// valid config with placeholder credentials to fill in.
//
//go:embed templates/config.yaml
var configTemplate []byte

// InitCommand returns the "init" command, which writes a template config file
// so a new user has something to edit rather than starting from example.yaml
// in the repo.
func InitCommand() *cli.Command {
	return &cli.Command{
		Name:  "init",
		Usage: "Create a template config file",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Usage:   "Path to write the template config `FILE`",
			},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Overwrite an existing config file",
			},
		},
		Action: initConfig,
	}
}

func initConfig(_ context.Context, cmd *cli.Command) error {
	path := cmd.String("output")
	if path == "" {
		path = resolveConfigPath()
	}

	if !cmd.Bool("force") {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists, use --force to overwrite", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("checking %s: %w", path, err)
		}
	}

	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, configTemplate, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	logger.Info("Wrote template config", zap.String("path", path))
	fmt.Printf("Wrote template config: %s\n", path)
	fmt.Println("Edit it to fill in your IMAP server, credentials, and where to save the backup.")
	return nil
}
