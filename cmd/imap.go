package cmd

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

// LoginCommand returns the "login" command, which checks that stage 1 can reach
// the server and authenticate without downloading anything.
func LoginCommand() *cli.Command {
	return &cli.Command{
		Name:   "login",
		Usage:  "Check that the IMAP server accepts the configured credentials",
		Action: LoginAction,
		Flags:  connectionFlags(),
	}
}

// LoginAction builds stage 1 and immediately closes it, so a successful run
// means the credentials and TLS settings are good.
func LoginAction(_ context.Context, cmd *cli.Command) error {
	fetcher, err := buildFetcher(cmd)
	if err != nil {
		return err
	}
	if err := fetcher.Close(); err != nil {
		return fmt.Errorf("closing connection: %w", err)
	}
	logger.Info("Login check succeeded", zap.String("source", fetcher.Name()))
	return nil
}
