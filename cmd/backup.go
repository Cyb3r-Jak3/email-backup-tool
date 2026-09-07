package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/Cyb3r-Jak3/email-backup-tool/stages"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

// BackupCommand returns the "backup" command, which runs the three stages end
// to end: download the mail, encrypt it, and store it.
func BackupCommand() *cli.Command {
	flags := connectionFlags()
	flags = append(flags, encryptFlags()...)
	flags = append(flags, storeFlags()...)
	flags = append(flags,
		&cli.StringFlag{
			Name:    "mailbox",
			Aliases: []string{"m"},
			Usage:   "Back up only this `MAILBOX` instead of every mailbox on the account",
			Sources: flagSources(KeyIMAPMailbox, "EMAIL_MAILBOX"),
		},
		&cli.BoolFlag{
			Name:  "attachments",
			Usage: "Also save each attachment as its own file next to the message",
			Value: false,
		},
		&cli.DurationFlag{
			Name:    "duration",
			Usage:   "How far back to look for messages when there is no timestamp file, e.g. `72h`",
			Value:   stages.DefaultLookback,
			Sources: cli.EnvVars("EMAIL_BACKUP_DURATION"),
		},
		&cli.BoolFlag{
			Name:  "all",
			Usage: "Back up every message, ignoring the timestamp file and --duration",
		},
		&cli.StringFlag{
			Name:  "timestamp-file",
			Usage: "`PATH` of the file recording the last backed up message's timestamp",
			Value: stages.DefaultWatermarkFile,
		},
	)
	return &cli.Command{
		Name:   "backup",
		Usage:  "Back up emails from the IMAP server",
		Action: Backup,
		Flags:  flags,
	}
}

// Backup assembles the three stages and runs them, then records the timestamp
// of the newest message stored so the next run resumes from there.
func Backup(ctx context.Context, cmd *cli.Command) error {
	req, err := fetchRequest(cmd)
	if err != nil {
		return err
	}

	fetcher, err := buildFetcher(cmd)
	if err != nil {
		return fmt.Errorf("stage 1: %w", err)
	}
	defer func() {
		if err := fetcher.Close(); err != nil {
			logger.Warn("Closing the mail source failed", zap.Error(err))
		}
	}()

	transformer, err := buildTransformer(ctx, cmd)
	if err != nil {
		return fmt.Errorf("stage 2: %w", err)
	}
	sink, err := buildSink(ctx, cmd)
	if err != nil {
		return fmt.Errorf("stage 3: %w", err)
	}

	pipeline := &stages.Pipeline{
		Fetch:     fetcher,
		Transform: transformer,
		Store:     sink,
		Logger:    logger,
	}
	result, err := pipeline.Run(ctx, req)
	if err != nil {
		return err
	}

	// The watermark is only advanced on a fully successful run, so a failure
	// part way through means the next run retries the same window rather than
	// skipping the messages it never stored.
	if result.Latest.IsZero() {
		logger.Info("No new messages, leaving the timestamp file unchanged")
		return nil
	}
	path := cmd.String("timestamp-file")
	version, _ := ctx.Value(VersionContextKey).(string)
	if err := stages.WriteWatermark(path, result.Latest, version); err != nil {
		return err
	}
	logger.Info("Recorded the last message timestamp",
		zap.String("file", path),
		zap.Time("timestamp", result.Latest),
	)
	return nil
}

// fetchRequest decides which messages this run asks for.
//
// The timestamp file left by the previous run wins, so repeated runs are
// incremental with no flags at all. --duration is the fallback when that file
// is missing, and also overrides it when given explicitly, which is how a run
// is made to reach further back than the last one got to. --all ignores both.
func fetchRequest(cmd *cli.Command) (stages.FetchRequest, error) {
	req := stages.FetchRequest{
		Mailbox:         cmd.String("mailbox"),
		WithAttachments: cmd.Bool("attachments"),
	}
	if cmd.Bool("all") {
		logger.Info("Backing up every message (--all)")
		return req, nil
	}

	duration := cmd.Duration("duration")
	path := cmd.String("timestamp-file")

	if cmd.IsSet("duration") {
		req.Since = time.Now().Add(-duration)
		logger.Info("Backing up messages from the requested window",
			zap.Duration("duration", duration),
			zap.Time("since", req.Since),
		)
		return req, nil
	}

	stamp, found, err := stages.ReadWatermark(path)
	if err != nil {
		return req, err
	}
	if found {
		req.Since = stamp
		logger.Info("Resuming from the last backed up message",
			zap.String("timestamp_file", path),
			zap.Time("since", req.Since),
		)
		return req, nil
	}

	req.Since = time.Now().Add(-duration)
	logger.Info("No timestamp file yet, falling back to the default window",
		zap.String("timestamp_file", path),
		zap.Duration("duration", duration),
		zap.Time("since", req.Since),
	)
	return req, nil
}
