package store

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Cyb3r-Jak3/email-backup-tool/stages"
	"go.uber.org/zap"
)

func artifact(path, data string) *stages.Artifact {
	return &stages.Artifact{Path: path, Data: []byte(data)}
}

func TestLocalWritesNestedArtifacts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "mail")
	sink, err := NewLocal(root, zap.NewNop())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	if err := sink.Store(context.Background(), artifact("INBOX/2026/09/msg.eml", "hello")); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(root, "INBOX", "2026", "09", "msg.eml"))
	if err != nil {
		t.Fatalf("reading the stored file: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("stored %q, want %q", got, "hello")
	}
}

func TestLocalRefusesPathsOutsideTheRoot(t *testing.T) {
	root := t.TempDir()
	sink, err := NewLocal(filepath.Join(root, "mail"), zap.NewNop())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	if err := sink.Store(context.Background(), artifact("../../escaped.eml", "nope")); err == nil {
		t.Fatal("Store accepted a path escaping the root directory")
	}
	if _, err := os.Stat(filepath.Join(root, "escaped.eml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a file was written outside the root directory")
	}
}

func TestTarWritesReadableArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.tar")
	sink, err := NewTar(path, zap.NewNop())
	if err != nil {
		t.Fatalf("NewTar: %v", err)
	}
	want := map[string]string{
		"INBOX/2026/09/one.eml": "first",
		"INBOX/2026/09/two.eml": "second",
	}
	for name, data := range want {
		if err := sink.Store(context.Background(), artifact(name, data)); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		closeErr := file.Close()
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}()
	got := readTar(t, file)
	if len(got) != len(want) {
		t.Fatalf("archive holds %d entries, want %d", len(got), len(want))
	}
	for name, data := range want {
		if got[name] != data {
			t.Errorf("entry %s = %q, want %q", name, got[name], data)
		}
	}
}

func TestTarCompressesByExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.tar.gz")
	sink, err := NewTar(path, zap.NewNop())
	if err != nil {
		t.Fatalf("NewTar: %v", err)
	}
	if err := sink.Store(context.Background(), artifact("INBOX/one.eml", "first")); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("archive is not gzip compressed: %v", err)
	}
	if got := readTar(t, gz); got["INBOX/one.eml"] != "first" {
		t.Fatalf("entry = %q, want %q", got["INBOX/one.eml"], "first")
	}
}

func TestTarCloseIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.tar")
	sink, err := NewTar(path, zap.NewNop())
	if err != nil {
		t.Fatalf("NewTar: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// The pipeline closes stage 3 itself, and a command may defer a close too.
	if err := sink.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// countingSink records calls so the multi-sink's fan-out can be observed.
type countingSink struct {
	name     string
	stored   int
	closed   int
	storeErr error
	closeErr error
}

func (c *countingSink) Name() string { return c.name }
func (c *countingSink) Store(context.Context, *stages.Artifact) error {
	c.stored++
	return c.storeErr
}
func (c *countingSink) Close() error {
	c.closed++
	return c.closeErr
}

func TestMultiFansOutAndClosesEverything(t *testing.T) {
	first := &countingSink{name: "first"}
	second := &countingSink{name: "second"}
	sink, err := NewMulti(first, second)
	if err != nil {
		t.Fatalf("NewMulti: %v", err)
	}
	if err := sink.Store(context.Background(), artifact("one.eml", "data")); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if first.stored != 1 || second.stored != 1 {
		t.Fatalf("stored %d/%d times, want 1/1", first.stored, second.stored)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if first.closed != 1 || second.closed != 1 {
		t.Fatalf("closed %d/%d times, want 1/1", first.closed, second.closed)
	}
}

func TestMultiClosesRemainingSinksAfterAFailure(t *testing.T) {
	failing := &countingSink{name: "failing", closeErr: errors.New("flush failed")}
	other := &countingSink{name: "other"}
	sink, err := NewMulti(failing, other)
	if err != nil {
		t.Fatalf("NewMulti: %v", err)
	}
	if err := sink.Close(); err == nil {
		t.Fatal("Close hid a backend failure")
	}
	if other.closed != 1 {
		t.Fatal("a failing backend stopped the others from being closed")
	}
}

func TestNewMultiUnwrapsASingleSink(t *testing.T) {
	only := &countingSink{name: "only"}
	sink, err := NewMulti(only)
	if err != nil {
		t.Fatalf("NewMulti: %v", err)
	}
	if sink.Name() != "only" {
		t.Fatalf("Name = %q, want the wrapped sink's own name", sink.Name())
	}
	if _, err := NewMulti(); err == nil {
		t.Fatal("NewMulti accepted an empty set of backends")
	}
}

// readTar reads an archive into a map of entry name to contents.
func readTar(t *testing.T, r io.Reader) map[string]string {
	t.Helper()
	out := map[string]string{}
	reader := tar.NewReader(r)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("reading archive: %v", err)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("reading entry %s: %v", header.Name, err)
		}
		out[header.Name] = string(data)
	}
}
