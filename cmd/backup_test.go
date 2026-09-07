package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Cyb3r-Jak3/email-backup-tool/stages"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

// runFetchRequest parses args against the backup command's flags and returns
// the window the run would ask stage 1 for. The action is replaced so nothing
// connects to a server.
func runFetchRequest(t *testing.T, args ...string) stages.FetchRequest {
	t.Helper()
	logger = zap.NewNop()

	var got stages.FetchRequest
	command := BackupCommand()
	command.Action = func(_ context.Context, cmd *cli.Command) error {
		req, err := fetchRequest(cmd)
		got = req
		return err
	}
	// The config file must not leak into these cases from the working
	// directory, so point the lookup at a path that does not exist.
	app := &cli.Command{Name: "test", Commands: []*cli.Command{command}}
	if err := app.Run(t.Context(), append([]string{"test", "backup"}, args...)); err != nil {
		t.Fatalf("running backup: %v", err)
	}
	return got
}

func TestFetchRequestUsesTimestampFileWhenPresent(t *testing.T) {
	path := filepath.Join(t.TempDir(), stages.DefaultWatermarkFile)
	stamp := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	if err := stages.WriteWatermark(path, stamp, "1.0.0"); err != nil {
		t.Fatal(err)
	}

	req := runFetchRequest(t, "--timestamp-file", path)
	if !req.Since.Equal(stamp) {
		t.Fatalf("Since = %s, want the recorded timestamp %s", req.Since, stamp)
	}
}

func TestFetchRequestFallsBackToTheDefaultWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-file")

	before := time.Now()
	req := runFetchRequest(t, "--timestamp-file", path)
	expected := before.Add(-stages.DefaultLookback)

	if req.Since.Before(expected.Add(-time.Minute)) || req.Since.After(expected.Add(time.Minute)) {
		t.Fatalf("Since = %s, want roughly 24 hours ago (%s)", req.Since, expected)
	}
}

func TestFetchRequestDurationOverridesTheTimestampFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), stages.DefaultWatermarkFile)
	if err := stages.WriteWatermark(path, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC), "1.0.0"); err != nil {
		t.Fatal(err)
	}

	before := time.Now()
	req := runFetchRequest(t, "--timestamp-file", path, "--duration", "72h")
	expected := before.Add(-72 * time.Hour)

	if req.Since.Before(expected.Add(-time.Minute)) || req.Since.After(expected.Add(time.Minute)) {
		t.Fatalf("Since = %s, want roughly 72 hours ago (%s)", req.Since, expected)
	}
}

func TestFetchRequestAllIgnoresEveryBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), stages.DefaultWatermarkFile)
	if err := stages.WriteWatermark(path, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC), "1.0.0"); err != nil {
		t.Fatal(err)
	}

	req := runFetchRequest(t, "--timestamp-file", path, "--duration", "72h", "--all")
	if !req.Since.IsZero() || !req.All() {
		t.Fatalf("Since = %s, want no lower bound with --all", req.Since)
	}
}

func TestFetchRequestCarriesMailboxAndAttachments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "none")

	req := runFetchRequest(t, "--timestamp-file", path, "--mailbox", "INBOX/Archive", "--attachments")
	if req.Mailbox != "INBOX/Archive" {
		t.Errorf("Mailbox = %q, want INBOX/Archive", req.Mailbox)
	}
	if !req.WithAttachments {
		t.Error("WithAttachments = false, want true when --attachments is given")
	}

	req = runFetchRequest(t, "--timestamp-file", path)
	if req.WithAttachments {
		t.Error("WithAttachments = true by default, want attachments to be opt-in")
	}
}

func TestFetchRequestRejectsACorruptTimestampFile(t *testing.T) {
	logger = zap.NewNop()
	path := filepath.Join(t.TempDir(), stages.DefaultWatermarkFile)
	if err := os.WriteFile(path, []byte("not a timestamp"), 0o600); err != nil {
		t.Fatal(err)
	}

	command := BackupCommand()
	command.Action = func(_ context.Context, cmd *cli.Command) error {
		_, err := fetchRequest(cmd)
		return err
	}
	app := &cli.Command{Name: "test", Commands: []*cli.Command{command}}
	err := app.Run(context.Background(), []string{"test", "backup", "--timestamp-file", path})
	if err == nil {
		t.Fatal("a corrupt timestamp file was accepted; the run would silently pick the wrong window")
	}
}
