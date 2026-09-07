package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
	"time"

	"github.com/Cyb3r-Jak3/email-backup-tool/cmd"
	"go.uber.org/zap"
)

var (
	version = "DEV"
	date    = "unknown"
	commit  = "unknown"
)

func main() {
	startTime := time.Now()
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Printf("Error creating logger: %s\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := logger.Sync(); err != nil {
			var pathErr *fs.PathError
			if !errors.Is(err, syscall.ENOTTY) && !errors.As(err, &pathErr) {
				fmt.Printf("Error syncing logger: %s\n", err)
			}
		}
	}()
	app := cmd.BuildApp(
		cmd.BuildArgs{
			Version:   version,
			Date:      date,
			Logger:    logger,
			StartTime: startTime,
			Commit:    commit,
		})
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), cmd.VersionContextKey, version))
	defer cancel()
	err = app.Run(ctx, os.Args)
	logger.Debug("Run took", zap.Duration("duration", time.Since(startTime)))
	if err != nil {
		fmt.Printf("Error running app: %s\n", err)
		os.Exit(1)
	}
}
