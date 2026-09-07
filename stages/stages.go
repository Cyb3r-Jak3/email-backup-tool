// Package stages defines the three interchangeable stages a backup run is made
// of, and the Pipeline that drives them:
//
//	stage 1  Fetcher      log in to a mail source and download messages
//	stage 2  Transformer  optionally encrypt what was downloaded
//	stage 3  Sink         write the result to a storage backend
//
// Every stage is reached only through the interface declared here, so an
// implementation can be swapped without touching the other two: the IMAP
// fetcher can be replaced by a Maildir one, the encrypting transformer by the
// pass-through, and the local-directory sink by S3 or tar. Implementations live
// in the stages/fetch, stages/transform and stages/store subpackages.
package stages

import (
	"context"
	"fmt"
	"time"
)

// Message is one downloaded mail item as it leaves stage 1. Raw is the
// complete RFC 5322 message as the server returned it, which already contains
// any attachments; Attachments is populated only when the fetch request asks
// for them to be broken out as separate files as well.
type Message struct {
	// Mailbox is the folder the message was found in, e.g. "INBOX".
	Mailbox string
	// UID is the message's unique identifier within Mailbox.
	UID uint32
	// Date is the message's timestamp on the server (its internal date). It
	// is what the incremental backup watermark is derived from.
	Date time.Time
	// Subject is used for logging and for naming the stored object.
	Subject string
	// Raw is the full message source.
	Raw []byte
	// Attachments holds the message's attachment parts, extracted only when
	// requested.
	Attachments []Attachment
}

// Attachment is one attachment part broken out of a Message.
type Attachment struct {
	// Filename is the name advertised by the message part, already reduced to
	// a single safe path element.
	Filename string
	// ContentType is the part's MIME type, for logging.
	ContentType string
	// Data is the decoded attachment content.
	Data []byte
}

// Artifact is a single named blob traveling through stages 2 and 3. A Message
// is flattened into one artifact for the message itself plus one per extracted
// attachment, so the transformer and the sink never have to know anything about
// mail structure.
type Artifact struct {
	// Path is the destination-relative path, using forward slashes. Sinks map
	// it onto their own namespace (a file path, an object key, a tar entry).
	Path string
	// Data is the artifact's content.
	Data []byte
	// Message is the message the artifact was derived from, for logging and
	// for sinks that want to record metadata.
	Message *Message
}

// FetchRequest is the window of mail stage 1 is asked for.
type FetchRequest struct {
	// Since limits the fetch to messages newer than this instant. The zero
	// value means no lower bound, i.e. every message.
	Since time.Time
	// Mailbox restricts the fetch to a single mailbox. Empty means every
	// mailbox the account exposes.
	Mailbox string
	// WithAttachments asks the fetcher to also break attachments out of each
	// message into Message.Attachments.
	WithAttachments bool
}

// All reports whether the request has no lower time bound.
func (r FetchRequest) All() bool { return r.Since.IsZero() }

// Fetcher is stage 1: it connects to a mail source and hands each message that
// falls inside the request to emit, in no guaranteed order. Returning an error
// from emit aborts the fetch with that error.
type Fetcher interface {
	// Name identifies the implementation in logs, e.g. "imap".
	Name() string
	// Fetch downloads the requested messages, calling emit once per message.
	Fetch(ctx context.Context, req FetchRequest, emit func(*Message) error) error
	// Close releases the connection to the source.
	Close() error
}

// Transformer is stage 2: it rewrites an artifact on its way to storage. The
// encrypting implementation replaces Data with ciphertext and adds a suffix to
// Path; the pass-through implementation returns the artifact unchanged.
type Transformer interface {
	// Name identifies the implementation in logs, e.g. "encrypt(rsa)".
	Name() string
	// Transform returns the artifact to store in place of the one given.
	Transform(ctx context.Context, artifact *Artifact) (*Artifact, error)
}

// Sink is stage 3: it writes finished artifacts to a storage backend. Close is
// always called once a run finishes, successfully or not, so a sink that
// buffers (tar) can finalise its output.
type Sink interface {
	// Name identifies the implementation in logs, e.g. "local".
	Name() string
	// Store writes one artifact.
	Store(ctx context.Context, artifact *Artifact) error
	// Close flushes and releases the backend.
	Close() error
}

// Artifacts flattens a message into the artifacts that represent it: the
// message source itself, followed by one per extracted attachment.
func (m *Message) Artifacts() []*Artifact {
	base := m.basePath()
	out := make([]*Artifact, 0, 1+len(m.Attachments))
	out = append(out, &Artifact{Path: base + ".eml", Data: m.Raw, Message: m})
	for i, attachment := range m.Attachments {
		name := attachment.Filename
		if name == "" {
			name = fmt.Sprintf("attachment-%d", i+1)
		}
		out = append(out, &Artifact{
			Path:    fmt.Sprintf("%s.attachments/%s", base, name),
			Data:    attachment.Data,
			Message: m,
		})
	}
	return out
}

// basePath is the destination path of a message without its extension. Messages
// are filed under their mailbox and month so a long running backup does not pile
// millions of entries into one directory or key prefix.
func (m *Message) basePath() string {
	date := m.Date.UTC()
	if m.Date.IsZero() {
		date = time.Now().UTC()
	}
	return fmt.Sprintf("%s/%04d/%02d/%s-%d-%s",
		SafePath(m.Mailbox),
		date.Year(), int(date.Month()),
		date.Format("20060102T150405Z"),
		m.UID,
		Slug(m.Subject),
	)
}
