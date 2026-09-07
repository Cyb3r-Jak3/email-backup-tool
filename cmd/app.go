package cmd

import (
	"fmt"
	"runtime/debug"
	"sort"
	"time"

	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

type BuildArgs struct {
	StartTime time.Time
	Version   string
	Date      string
	Logger    *zap.Logger
	Commit    string
}

type ctxKey int

const (
	// VersionContextKey is the context key under which the app version string is stored.
	VersionContextKey ctxKey = iota
)

var (
	logger        *zap.Logger
	versionString = ""
)

func BuildApp(args BuildArgs) *cli.Command {
	logger = args.Logger
	if buildInfo, available := debug.ReadBuildInfo(); available {
		versionString = fmt.Sprintf("%s (built %s with %s, commit %s)", args.Version, args.Date, buildInfo.GoVersion, args.Commit)
	} else {
		versionString = fmt.Sprintf("%s (built %s, commit %s)", args.Version, args.Date, args.Commit)
		logger.Warn("Build info not available, using fallback version string")
	}
	app := &cli.Command{
		Name:    "email-backup-tool",
		Usage:   "A tool to backup IMAP email accounts",
		Version: versionString,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "config",
				Aliases:     []string{"c"},
				Usage:       "Path to a YAML config `FILE`. Defaults to ./email-backup-tool.yaml",
				Sources:     cli.EnvVars("EMAIL_BACKUP_TOOL_CONFIG"),
				Destination: &configPath,
			},
		},
		// Read the config file up front so an unreadable, malformed or
		// unsupported-version file fails loudly instead of silently sourcing
		// no values into the command flags.
		Before: checkConfig,
		Commands: []*cli.Command{
			InitCommand(),
			GenerateKeyCommand(),
			LoginCommand(),
			BackupCommand(),
			DecryptCommand(),
		},
		EnableShellCompletion: true,
	}
	sort.Sort(cli.FlagsByName(app.Flags))
	return app
}
