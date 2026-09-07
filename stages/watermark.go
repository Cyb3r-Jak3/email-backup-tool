package stages

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// DefaultWatermarkFile is the name of the file holding the timestamp of the
// most recent message a previous run downloaded. Its presence is what makes a
// run incremental: the next backup asks the source for everything after it.
const DefaultWatermarkFile = "email-backup-tool-watermark.json"

// DefaultLookback is how far back a run reaches when there is no watermark file
// and no --duration was given.
const DefaultLookback = 24 * time.Hour

// watermarkFile is the on-disk JSON shape of the watermark file.
type watermarkFile struct {
	LastMessageTimestamp time.Time `json:"last_message_timestamp"`
	Version              string    `json:"version"`
}

// ReadWatermark returns the timestamp recorded at path. A missing file is not
// an error: the zero time is returned with found=false, leaving the caller to
// fall back to its duration window.
func ReadWatermark(path string) (stamp time.Time, found bool, err error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is user supplied by design
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, fmt.Errorf("reading %s: %w", path, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return time.Time{}, false, nil
	}
	var wm watermarkFile
	if err := json.Unmarshal(data, &wm); err != nil {
		return time.Time{}, false, fmt.Errorf("parsing watermark in %s: %w", path, err)
	}
	if wm.LastMessageTimestamp.IsZero() {
		return time.Time{}, false, fmt.Errorf("parsing watermark in %s: missing last_message_timestamp", path)
	}
	return wm.LastMessageTimestamp.UTC(), true, nil
}

// WriteWatermark records stamp and the version of email-backup-tool that
// produced it at path, so the next run starts from just after the newest
// message this one stored.
func WriteWatermark(path string, stamp time.Time, version string) error {
	wm := watermarkFile{
		LastMessageTimestamp: stamp.UTC(),
		Version:              version,
	}
	data, err := json.MarshalIndent(wm, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding watermark for %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
