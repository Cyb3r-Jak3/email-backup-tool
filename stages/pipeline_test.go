package stages

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

// fakeFetcher is a stage 1 stand-in that emits a fixed list of messages.
type fakeFetcher struct {
	messages []*Message
	err      error
	gotReq   FetchRequest
}

func (f *fakeFetcher) Name() string { return "fake" }
func (f *fakeFetcher) Close() error { return nil }
func (f *fakeFetcher) Fetch(_ context.Context, req FetchRequest, emit func(*Message) error) error {
	f.gotReq = req
	for _, msg := range f.messages {
		if err := emit(msg); err != nil {
			return err
		}
	}
	return f.err
}

// upperTransformer is a stage 2 stand-in that is visibly not the identity.
type upperTransformer struct{}

func (upperTransformer) Name() string { return "upper" }
func (upperTransformer) Transform(_ context.Context, a *Artifact) (*Artifact, error) {
	return &Artifact{Path: a.Path + ".up", Data: []byte(strings.ToUpper(string(a.Data))), Message: a.Message}, nil
}

// recordingSink is a stage 3 stand-in that remembers what it was given.
type recordingSink struct {
	stored   []*Artifact
	closed   bool
	storeErr error
}

func (s *recordingSink) Name() string { return "recording" }
func (s *recordingSink) Store(_ context.Context, a *Artifact) error {
	if s.storeErr != nil {
		return s.storeErr
	}
	s.stored = append(s.stored, a)
	return nil
}
func (s *recordingSink) Close() error {
	s.closed = true
	return nil
}

func testMessage(uid uint32, date time.Time) *Message {
	return &Message{
		Mailbox: "INBOX",
		UID:     uid,
		Date:    date,
		Subject: fmt.Sprintf("Message %d", uid),
		Raw:     fmt.Appendf(nil, "raw body %d", uid),
	}
}

func TestPipelineRunsAllThreeStages(t *testing.T) {
	older := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	fetcher := &fakeFetcher{messages: []*Message{testMessage(1, older), testMessage(2, newer)}}
	sink := &recordingSink{}

	pipeline := &Pipeline{Fetch: fetcher, Transform: upperTransformer{}, Store: sink, Logger: zap.NewNop()}
	result, err := pipeline.Run(context.Background(), FetchRequest{Since: older.Add(-time.Hour)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Messages != 2 || result.Artifacts != 2 {
		t.Fatalf("result = %d messages / %d artifacts, want 2/2", result.Messages, result.Artifacts)
	}
	if !result.Latest.Equal(newer) {
		t.Fatalf("Latest = %s, want the newest message date %s", result.Latest, newer)
	}
	if !sink.closed {
		t.Fatal("stage 3 was not closed")
	}
	// Stage 2 really ran between stage 1 and stage 3.
	for _, artifact := range sink.stored {
		if !strings.HasSuffix(artifact.Path, ".eml.up") {
			t.Fatalf("stored path %q did not go through the transformer", artifact.Path)
		}
		if got := string(artifact.Data); got != strings.ToUpper(got) {
			t.Fatalf("stored data %q was not transformed", got)
		}
	}
}

func TestPipelineFansMessagesIntoArtifacts(t *testing.T) {
	msg := testMessage(7, time.Date(2026, 9, 5, 8, 30, 0, 0, time.UTC))
	msg.Attachments = []Attachment{
		{Filename: "report.pdf", Data: []byte("pdf")},
		{Filename: "", Data: []byte("unnamed")},
	}
	sink := &recordingSink{}
	pipeline := &Pipeline{
		Fetch:     &fakeFetcher{messages: []*Message{msg}},
		Transform: PassThroughForTest{},
		Store:     sink,
		Logger:    zap.NewNop(),
	}
	result, err := pipeline.Run(context.Background(), FetchRequest{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Messages != 1 || result.Artifacts != 3 {
		t.Fatalf("result = %d messages / %d artifacts, want 1/3", result.Messages, result.Artifacts)
	}

	want := []string{
		"INBOX/2026/09/20260905T083000Z-7-message-7.eml",
		"INBOX/2026/09/20260905T083000Z-7-message-7.attachments/report.pdf",
		"INBOX/2026/09/20260905T083000Z-7-message-7.attachments/attachment-2",
	}
	for i, artifact := range sink.stored {
		if artifact.Path != want[i] {
			t.Errorf("artifact %d path = %q, want %q", i, artifact.Path, want[i])
		}
	}
}

func TestPipelineClosesSinkOnFetchError(t *testing.T) {
	sink := &recordingSink{}
	pipeline := &Pipeline{
		Fetch:     &fakeFetcher{err: errors.New("connection reset")},
		Transform: PassThroughForTest{},
		Store:     sink,
		Logger:    zap.NewNop(),
	}
	if _, err := pipeline.Run(context.Background(), FetchRequest{}); err == nil {
		t.Fatal("Run returned no error for a failing stage 1")
	}
	if !sink.closed {
		t.Fatal("stage 3 was not closed after a stage 1 failure, leaving output unfinalised")
	}
}

func TestPipelineStopsOnStoreError(t *testing.T) {
	pipeline := &Pipeline{
		Fetch:     &fakeFetcher{messages: []*Message{testMessage(1, time.Now())}},
		Transform: PassThroughForTest{},
		Store:     &recordingSink{storeErr: errors.New("disk full")},
		Logger:    zap.NewNop(),
	}
	result, err := pipeline.Run(context.Background(), FetchRequest{})
	if err == nil {
		t.Fatal("Run returned no error for a failing stage 3")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("error %q does not mention the underlying cause", err)
	}
	// A run that failed must not report a watermark, or the next run would
	// skip the messages this one never stored.
	if !result.Latest.IsZero() {
		t.Fatalf("Latest = %s, want the zero time after a failure", result.Latest)
	}
}

func TestPipelineRejectsMissingStage(t *testing.T) {
	pipeline := &Pipeline{Fetch: &fakeFetcher{}, Store: &recordingSink{}, Logger: zap.NewNop()}
	if _, err := pipeline.Run(context.Background(), FetchRequest{}); err == nil {
		t.Fatal("Run accepted a pipeline with no stage 2")
	}
}

// PassThroughForTest is the identity transformer, kept here so the stages
// package tests do not import one of its own implementations.
type PassThroughForTest struct{}

func (PassThroughForTest) Name() string { return "none" }
func (PassThroughForTest) Transform(_ context.Context, a *Artifact) (*Artifact, error) {
	return a, nil
}
