package fetch

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Cyb3r-Jak3/email-backup-tool/stages"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"go.uber.org/zap"
)

const (
	testUser     = "backup@example.com"
	testPassword = "hunter2"
)

// testMessages are appended to the server's INBOX, spread over several days so
// the time window can be exercised.
var testMessages = []struct {
	subject string
	date    time.Time
}{
	{"Oldest message", time.Now().Add(-96 * time.Hour)},
	{"Two days ago", time.Now().Add(-48 * time.Hour)},
	{"This morning", time.Now().Add(-3 * time.Hour)},
}

// startTestServer runs an in-memory IMAP server on a random local port and
// returns its address. The server is shut down when the test ends.
func startTestServer(t *testing.T) string {
	t.Helper()

	memory := imapmemserver.New()
	user := imapmemserver.NewUser(testUser, testPassword)
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatalf("creating INBOX: %v", err)
	}
	if err := user.Create("Archive", nil); err != nil {
		t.Fatalf("creating Archive: %v", err)
	}
	memory.AddUser(user)

	for i, msg := range testMessages {
		raw := fmt.Sprintf("From: sender@example.com\r\nTo: %s\r\nSubject: %s\r\n"+
			"Date: %s\r\nContent-Type: text/plain\r\n\r\nbody %d\r\n",
			testUser, msg.subject, msg.date.Format(time.RFC1123Z), i)
		appendMessage(t, user, "INBOX", raw, msg.date)
	}
	// One message elsewhere, so a run over every mailbox differs from one over
	// a single mailbox.
	appendMessage(t, user, "Archive", "Subject: Archived\r\n\r\nold news\r\n", time.Now().Add(-time.Hour))

	server := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return memory.NewSession(), nil, nil
		},
		InsecureAuth: true,
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}},
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	return listener.Addr().String()
}

// appendMessage stores one message in a mailbox with a fixed internal date.
func appendMessage(t *testing.T, user *imapmemserver.User, mailbox, raw string, date time.Time) {
	t.Helper()
	literal := &memoryLiteral{Reader: strings.NewReader(raw), size: int64(len(raw))}
	if _, err := user.Append(mailbox, literal, &imap.AppendOptions{Time: date}); err != nil {
		t.Fatalf("appending to %s: %v", mailbox, err)
	}
}

// memoryLiteral adapts a string to the literal reader the server expects.
type memoryLiteral struct {
	*strings.Reader
	size int64
}

func (l *memoryLiteral) Size() int64 { return l.size }

// connect builds a fetcher pointed at the test server.
func connect(t *testing.T, address string) *IMAP {
	t.Helper()
	fetcher, err := NewIMAP(IMAPOptions{
		Server:   address,
		Username: testUser,
		Password: testPassword,
		TLS:      TLSNone,
		Logger:   zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("NewIMAP: %v", err)
	}
	t.Cleanup(func() { _ = fetcher.Close() })
	return fetcher
}

// collect runs a fetch and returns the messages it emitted.
func collect(t *testing.T, fetcher *IMAP, req stages.FetchRequest) []*stages.Message {
	t.Helper()
	var got []*stages.Message
	err := fetcher.Fetch(context.Background(), req, func(msg *stages.Message) error {
		got = append(got, msg)
		return nil
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	return got
}

func TestIMAPFetchesEveryMailbox(t *testing.T) {
	fetcher := connect(t, startTestServer(t))

	got := collect(t, fetcher, stages.FetchRequest{})
	if len(got) != len(testMessages)+1 {
		t.Fatalf("fetched %d messages, want %d across both mailboxes", len(got), len(testMessages)+1)
	}

	mailboxes := map[string]int{}
	for _, msg := range got {
		mailboxes[msg.Mailbox]++
		if len(msg.Raw) == 0 {
			t.Errorf("message %d in %s came back with no body", msg.UID, msg.Mailbox)
		}
	}
	if mailboxes["INBOX"] != len(testMessages) || mailboxes["Archive"] != 1 {
		t.Fatalf("messages per mailbox = %v, want 3 in INBOX and 1 in Archive", mailboxes)
	}
}

func TestIMAPHonoursTheTimeWindow(t *testing.T) {
	fetcher := connect(t, startTestServer(t))

	// A window of one day should reach only the most recent INBOX message.
	got := collect(t, fetcher, stages.FetchRequest{
		Mailbox: "INBOX",
		Since:   time.Now().Add(-24 * time.Hour),
	})
	if len(got) != 1 {
		t.Fatalf("fetched %d messages for a 24 hour window, want 1", len(got))
	}
	if got[0].Subject != "This morning" {
		t.Fatalf("fetched %q, want the most recent message", got[0].Subject)
	}
}

func TestIMAPRestrictsToOneMailbox(t *testing.T) {
	fetcher := connect(t, startTestServer(t))

	got := collect(t, fetcher, stages.FetchRequest{Mailbox: "Archive"})
	if len(got) != 1 {
		t.Fatalf("fetched %d messages from Archive, want 1", len(got))
	}
	if got[0].Mailbox != "Archive" {
		t.Fatalf("message came from %q, want Archive", got[0].Mailbox)
	}
}

func TestIMAPStopsWhenTheSinkFails(t *testing.T) {
	fetcher := connect(t, startTestServer(t))

	wanted := fmt.Errorf("stop here")
	err := fetcher.Fetch(context.Background(), stages.FetchRequest{}, func(*stages.Message) error {
		return wanted
	})
	if err == nil || !strings.Contains(err.Error(), "stop here") {
		t.Fatalf("Fetch returned %v, want the emit error to propagate", err)
	}
}

func TestIMAPRejectsBadCredentials(t *testing.T) {
	address := startTestServer(t)
	if _, err := NewIMAP(IMAPOptions{
		Server:   address,
		Username: testUser,
		Password: "wrong",
		TLS:      TLSNone,
		Logger:   zap.NewNop(),
	}); err == nil {
		t.Fatal("NewIMAP accepted a bad password")
	}
	if _, err := NewIMAP(IMAPOptions{Server: address, TLS: TLSNone, Logger: zap.NewNop()}); err == nil {
		t.Fatal("NewIMAP accepted an empty username")
	}
	if _, err := NewIMAP(IMAPOptions{Username: testUser, TLS: TLSNone, Logger: zap.NewNop()}); err == nil {
		t.Fatal("NewIMAP accepted an empty server address")
	}
}
