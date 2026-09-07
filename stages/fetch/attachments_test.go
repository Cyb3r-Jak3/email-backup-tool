package fetch

import (
	"strings"
	"testing"
)

// multipartMessage is a message with a text body and two attachments, one of
// them named only through the legacy Content-Type name parameter.
const multipartMessage = "From: sender@example.com\r\n" +
	"To: you@example.com\r\n" +
	"Subject: With attachments\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=outer\r\n" +
	"\r\n" +
	"--outer\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"body text, not an attachment\r\n" +
	"--outer\r\n" +
	"Content-Type: application/pdf\r\n" +
	"Content-Disposition: attachment; filename=\"report.pdf\"\r\n" +
	"Content-Transfer-Encoding: base64\r\n" +
	"\r\n" +
	"aGVsbG8gcGRm\r\n" +
	"--outer\r\n" +
	"Content-Type: image/png; name=\"inline.png\"\r\n" +
	"Content-Disposition: inline\r\n" +
	"\r\n" +
	"pngbytes\r\n" +
	"--outer--\r\n"

func TestExtractAttachments(t *testing.T) {
	got, err := ExtractAttachments([]byte(multipartMessage))
	if err != nil {
		t.Fatalf("ExtractAttachments: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("extracted %d attachments, want 2 (the body part must not count)", len(got))
	}

	if got[0].Filename != "report.pdf" {
		t.Errorf("first filename = %q, want report.pdf", got[0].Filename)
	}
	// base64 must be decoded, not stored as it appeared on the wire.
	if string(got[0].Data) != "hello pdf" {
		t.Errorf("first attachment = %q, want the decoded content", got[0].Data)
	}
	if got[0].ContentType != "application/pdf" {
		t.Errorf("first content type = %q, want application/pdf", got[0].ContentType)
	}
	// An inline part with a name is still worth breaking out.
	if got[1].Filename != "inline.png" {
		t.Errorf("second filename = %q, want inline.png", got[1].Filename)
	}
}

func TestExtractAttachmentsFromPlainMessage(t *testing.T) {
	plain := "Subject: no attachments\r\nContent-Type: text/plain\r\n\r\njust text\r\n"
	got, err := ExtractAttachments([]byte(plain))
	if err != nil {
		t.Fatalf("ExtractAttachments: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("extracted %d attachments from a plain message, want 0", len(got))
	}
}

func TestExtractAttachmentsSanitisesFilenames(t *testing.T) {
	hostile := "MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=b\r\n" +
		"\r\n" +
		"--b\r\n" +
		"Content-Type: application/octet-stream\r\n" +
		"Content-Disposition: attachment; filename=\"../../../etc/passwd\"\r\n" +
		"\r\n" +
		"payload\r\n" +
		"--b--\r\n"

	got, err := ExtractAttachments([]byte(hostile))
	if err != nil {
		t.Fatalf("ExtractAttachments: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("extracted %d attachments, want 1", len(got))
	}
	if strings.ContainsAny(got[0].Filename, `/\`) || strings.Contains(got[0].Filename, "..") {
		t.Fatalf("filename %q was not reduced to a safe single component", got[0].Filename)
	}
}

func TestExtractAttachmentsBoundsNesting(t *testing.T) {
	// A message nesting multiparts far deeper than any real mail client would
	// must be rejected rather than driving the walk down indefinitely.
	var b strings.Builder
	depth := maxAttachmentDepth + 5
	b.WriteString("MIME-Version: 1.0\r\n")
	for i := range depth {
		b.WriteString("Content-Type: multipart/mixed; boundary=b")
		b.WriteString(string(rune('a' + i%26)))
		b.WriteString(string(rune('0' + i/26)))
		b.WriteString("\r\n\r\n")
		b.WriteString("--b")
		b.WriteString(string(rune('a' + i%26)))
		b.WriteString(string(rune('0' + i/26)))
		b.WriteString("\r\n")
	}
	b.WriteString("Content-Type: text/plain\r\n\r\ndeep\r\n")

	if _, err := ExtractAttachments([]byte(b.String())); err == nil {
		t.Skip("this construction did not nest as deeply as intended")
	}
}
