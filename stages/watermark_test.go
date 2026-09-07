package stages

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatermarkRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultWatermarkFile)
	stamp := time.Date(2026, 9, 5, 8, 30, 15, 0, time.UTC)

	if err := WriteWatermark(path, stamp, "1.2.3"); err != nil {
		t.Fatalf("WriteWatermark: %v", err)
	}
	got, found, err := ReadWatermark(path)
	if err != nil {
		t.Fatalf("ReadWatermark: %v", err)
	}
	if !found {
		t.Fatal("ReadWatermark did not find the file it just wrote")
	}
	if !got.Equal(stamp) {
		t.Fatalf("read %s, want %s", got, stamp)
	}
}

func TestReadWatermarkMissingFileIsNotAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist")
	got, found, err := ReadWatermark(path)
	if err != nil {
		t.Fatalf("ReadWatermark on a missing file returned an error: %v", err)
	}
	if found || !got.IsZero() {
		t.Fatal("ReadWatermark reported a timestamp for a file that does not exist")
	}
}

func TestReadWatermarkRejectsGarbage(t *testing.T) {
	dir := t.TempDir()

	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, found, err := ReadWatermark(empty); err != nil || found {
		t.Fatalf("an empty file should read as absent, got found=%v err=%v", found, err)
	}

	bad := filepath.Join(dir, "bad")
	if err := os.WriteFile(bad, []byte("yesterday"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A corrupt watermark must fail loudly: silently treating it as absent
	// would quietly re-download or skip mail.
	if _, _, err := ReadWatermark(bad); err == nil {
		t.Fatal("ReadWatermark accepted an unparseable timestamp")
	}
}

func TestWrittenWatermarkIsReadableJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultWatermarkFile)
	stamp := time.Date(2026, 9, 5, 8, 30, 15, 0, time.FixedZone("CEST", 2*60*60))
	if err := WriteWatermark(path, stamp, "1.2.3"); err != nil {
		t.Fatalf("WriteWatermark: %v", err)
	}
	var wm watermarkFile
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &wm); err != nil {
		t.Fatalf("watermark file is not valid JSON: %v", err)
	}
	// The timestamp is meant to be human readable and always in UTC.
	if want := "2026-09-05T06:30:15Z"; !wm.LastMessageTimestamp.Equal(stamp) || wm.LastMessageTimestamp.Format(time.RFC3339) != want {
		t.Fatalf("got timestamp %s, want %s", wm.LastMessageTimestamp.Format(time.RFC3339), want)
	}
	if wm.Version != "1.2.3" {
		t.Fatalf("got version %q, want %q", wm.Version, "1.2.3")
	}
}
