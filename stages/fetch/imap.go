// Package fetch holds stage 1 implementations: the sources a backup downloads
// mail from. Each implements stages.Fetcher, so adding another source means
// adding a type here and selecting it in the backup command.
package fetch

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/Cyb3r-Jak3/email-backup-tool/stages"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"go.uber.org/zap"
)

// TLS modes accepted by IMAPOptions.TLS.
const (
	// TLSStartTLS upgrades a plaintext connection with STARTTLS.
	TLSStartTLS = "STARTTLS"
	// TLSImplicit wraps the connection in TLS from the first byte.
	TLSImplicit = "SSL"
	// TLSNone disables TLS entirely.
	TLSNone = "NONE"
)

// fetchBatchSize is how many messages are requested from the server per FETCH.
// Messages are streamed within a batch, so this only bounds the size of the UID
// set sent, not the memory held.
const fetchBatchSize = 100

// IMAPOptions describes the server an IMAP fetcher connects to.
type IMAPOptions struct {
	// Server is the "host:port" address of the IMAP server.
	Server string
	// Username and Password authenticate the session.
	Username string
	Password string
	// TLS is one of TLSStartTLS, TLSImplicit or TLSNone.
	TLS string
	// CACertFile optionally replaces the system trust store when verifying
	// the server certificate.
	CACertFile string
	// Insecure disables server certificate verification.
	Insecure bool
	// Logger receives progress. Required.
	Logger *zap.Logger
}

// IMAP is stage 1 backed by an IMAP server.
type IMAP struct {
	client *imapclient.Client
	logger *zap.Logger
	server string
}

// compile-time check that IMAP satisfies stage 1.
var _ stages.Fetcher = (*IMAP)(nil)

// NewIMAP dials the server and logs in. The returned fetcher owns the
// connection and releases it in Close.
func NewIMAP(opts IMAPOptions) (*IMAP, error) {
	if opts.Server == "" {
		return nil, fmt.Errorf("no IMAP server configured (set login.imap.host/port, --server, or IMAP_SERVER)")
	}
	if opts.Username == "" {
		return nil, fmt.Errorf("no IMAP username configured (set login.imap.username, --username, or IMAP_USERNAME)")
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: opts.Insecure, //nolint:gosec // explicit opt-in via flag/config
	}
	if opts.CACertFile != "" {
		caCert, err := os.ReadFile(opts.CACertFile) //nolint:gosec // path is user supplied by design
		if err != nil {
			return nil, fmt.Errorf("reading CA cert %s: %w", opts.CACertFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("no certificates found in %s", opts.CACertFile)
		}
		tlsConfig.RootCAs = pool
	}
	clientOptions := &imapclient.Options{TLSConfig: tlsConfig}

	var (
		client *imapclient.Client
		err    error
	)
	switch opts.TLS {
	case TLSStartTLS:
		client, err = imapclient.DialStartTLS(opts.Server, clientOptions)
	case TLSNone:
		client, err = imapclient.DialInsecure(opts.Server, clientOptions)
	default:
		client, err = imapclient.DialTLS(opts.Server, clientOptions)
	}
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", opts.Server, err)
	}

	if err := client.Login(opts.Username, opts.Password).Wait(); err != nil {
		// The connection is closed here because the caller has no fetcher to
		// call Close on.
		_ = client.Close()
		return nil, fmt.Errorf("logging in as %s: %w", opts.Username, err)
	}
	opts.Logger.Info("Successfully logged in to IMAP server",
		zap.String("server", opts.Server),
		zap.String("username", opts.Username),
	)
	return &IMAP{client: client, logger: opts.Logger, server: opts.Server}, nil
}

// Name implements stages.Fetcher.
func (f *IMAP) Name() string { return "imap" }

// Close implements stages.Fetcher, logging out of the server.
func (f *IMAP) Close() error {
	if f.client == nil {
		return nil
	}
	if err := f.client.Logout().Wait(); err != nil {
		// A failed logout still leaves the socket to clean up.
		_ = f.client.Close()
		return fmt.Errorf("logging out of %s: %w", f.server, err)
	}
	return f.client.Close()
}

// Fetch implements stages.Fetcher. It walks the requested mailboxes, asking the
// server for messages newer than req.Since, and emits each one.
func (f *IMAP) Fetch(ctx context.Context, req stages.FetchRequest, emit func(*stages.Message) error) error {
	mailboxes, err := f.mailboxes(req.Mailbox)
	if err != nil {
		return err
	}
	f.logger.Info("Found mailboxes", zap.Int("count", len(mailboxes)), zap.Strings("mailboxes", mailboxes))

	for _, mailbox := range mailboxes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := f.fetchMailbox(ctx, mailbox, req, emit); err != nil {
			return err
		}
	}
	return nil
}

// mailboxes returns the mailboxes to back up: the one requested, or every
// selectable mailbox the account exposes.
func (f *IMAP) mailboxes(only string) ([]string, error) {
	if only != "" {
		return []string{only}, nil
	}
	// "*" rather than "%" so nested mailboxes are included, not just the top
	// level of the hierarchy.
	list, err := f.client.List("", "*", nil).Collect()
	if err != nil {
		return nil, fmt.Errorf("listing mailboxes: %w", err)
	}
	names := make([]string, 0, len(list))
	for _, entry := range list {
		// The no-select attribute marks a pure hierarchy node holding no mail.
		if hasAttr(entry.Attrs, imap.MailboxAttrNoSelect) {
			continue
		}
		names = append(names, entry.Mailbox)
	}
	return names, nil
}

// fetchMailbox backs up a single mailbox.
func (f *IMAP) fetchMailbox(ctx context.Context, mailbox string, req stages.FetchRequest, emit func(*stages.Message) error) error {
	// Read-only: a backup must never mark mail as seen.
	selected, err := f.client.Select(mailbox, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return fmt.Errorf("selecting mailbox %s: %w", mailbox, err)
	}
	if selected.NumMessages == 0 {
		f.logger.Debug("Mailbox is empty", zap.String("mailbox", mailbox))
		return nil
	}

	uids, err := f.search(req)
	if err != nil {
		return fmt.Errorf("searching mailbox %s: %w", mailbox, err)
	}
	f.logger.Info("Backing up mailbox",
		zap.String("mailbox", mailbox),
		zap.Uint32("total_messages", selected.NumMessages),
		zap.Int("matching_messages", len(uids)),
	)
	if len(uids) == 0 {
		return nil
	}

	for start := 0; start < len(uids); start += fetchBatchSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(start+fetchBatchSize, len(uids))
		if err := f.fetchBatch(mailbox, uids[start:end], req, emit); err != nil {
			return err
		}
	}
	return nil
}

// search asks the server which UIDs fall inside the request. IMAP's SINCE
// compares dates only, so the window it returns is a superset that message
// narrows down to the exact instant.
func (f *IMAP) search(req stages.FetchRequest) ([]imap.UID, error) {
	criteria := &imap.SearchCriteria{}
	if !req.All() {
		criteria.Since = req.Since
	}
	data, err := f.client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, err
	}
	return data.AllUIDs(), nil
}

// fetchBatch downloads one batch of messages and emits those that really are
// newer than the requested instant.
func (f *IMAP) fetchBatch(mailbox string, uids []imap.UID, req stages.FetchRequest, emit func(*stages.Message) error) error {
	// Peek so the backup does not set the seen flag on the user's mail.
	section := &imap.FetchItemBodySection{Peek: true}
	options := &imap.FetchOptions{
		UID:          true,
		Envelope:     true,
		InternalDate: true,
		BodySection:  []*imap.FetchItemBodySection{section},
	}

	cmd := f.client.Fetch(imap.UIDSetNum(uids...), options)
	defer func() {
		closeErr := cmd.Close()
		if closeErr != nil {
			f.logger.Error("closing fetch command", zap.Error(closeErr))
		}
	}()

	for {
		data := cmd.Next()
		if data == nil {
			break
		}
		buf, err := data.Collect()
		if err != nil {
			return fmt.Errorf("fetching messages from %s: %w", mailbox, err)
		}
		msg, keep := f.message(mailbox, buf, section, req)
		if !keep {
			continue
		}
		if err := emit(msg); err != nil {
			return err
		}
	}
	return cmd.Close()
}

// message converts a fetched buffer into a stages.Message, reporting false for
// messages the exact time bound excludes or that carried no body.
func (f *IMAP) message(mailbox string, buf *imapclient.FetchMessageBuffer, section *imap.FetchItemBodySection, req stages.FetchRequest) (*stages.Message, bool) {
	date := buf.InternalDate
	if date.IsZero() && buf.Envelope != nil {
		date = buf.Envelope.Date
	}
	// IMAP SINCE has date granularity, so a run resuming from a watermark part
	// way through a day gets back messages it already has; drop them here.
	if !req.All() && !date.IsZero() && !date.After(req.Since) {
		return nil, false
	}

	raw := buf.FindBodySection(section)
	if raw == nil {
		f.logger.Warn("Message returned no body, skipping",
			zap.String("mailbox", mailbox),
			zap.Uint32("uid", uint32(buf.UID)),
		)
		return nil, false
	}

	subject := ""
	if buf.Envelope != nil {
		subject = buf.Envelope.Subject
	}
	msg := &stages.Message{
		Mailbox: mailbox,
		UID:     uint32(buf.UID),
		Date:    date,
		Subject: subject,
		Raw:     raw,
	}

	if req.WithAttachments {
		attachments, err := ExtractAttachments(raw)
		if err != nil {
			// A message whose MIME structure cannot be walked is still worth
			// backing up in full, so this is a warning rather than an error.
			f.logger.Warn("Could not extract attachments",
				zap.String("mailbox", mailbox),
				zap.Uint32("uid", uint32(buf.UID)),
				zap.Error(err),
			)
		}
		msg.Attachments = attachments
	}

	f.logger.Debug("Fetched message",
		zap.String("mailbox", mailbox),
		zap.Uint32("uid", uint32(buf.UID)),
		zap.Time("date", date),
		zap.String("subject", subject),
		zap.Int("bytes", len(raw)),
		zap.Int("attachments", len(msg.Attachments)),
	)
	return msg, true
}

// hasAttr reports whether attrs contains attr.
func hasAttr(attrs []imap.MailboxAttr, attr imap.MailboxAttr) bool {
	for _, a := range attrs {
		if a == attr {
			return true
		}
	}
	return false
}
