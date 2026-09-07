package stages

import (
	"path"
	"strings"
	"testing"
	"time"
)

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Hello World":                   "hello-world",
		"  Re: [URGENT] Invoice #42!  ": "re-urgent-invoice-42",
		"":                              "no-subject",
		"???":                           "no-subject",
		"ünïcödé sübject":               "ünïcödé-sübject",
		strings.Repeat("long ", 40):     "long-long-long-long-long-long-long-long-long-long-long-long",
	}
	for input, want := range cases {
		if got := Slug(input); got != want {
			t.Errorf("Slug(%q) = %q, want %q", input, got, want)
		}
	}
	if got := Slug(strings.Repeat("a", 500)); len(got) > maxSlugLength {
		t.Errorf("Slug did not cap length: got %d characters", len(got))
	}
}

func TestSafePathMapsMailboxHierarchy(t *testing.T) {
	cases := map[string]string{
		"INBOX":            "INBOX",
		"INBOX.Sent Items": "INBOX/Sent Items",
		"INBOX/Archive":    "INBOX/Archive",
		"":                 "unknown",
		"...":              "unknown",
	}
	for input, want := range cases {
		if got := SafePath(input); got != want {
			t.Errorf("SafePath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSafePathRejectsTraversal(t *testing.T) {
	for _, input := range []string{"../../etc", "..", "INBOX/../../../root", `..\..\windows`} {
		got := SafePath(input)
		if strings.Contains(got, "..") {
			t.Errorf("SafePath(%q) = %q, which can still escape the destination", input, got)
		}
	}
}

func TestSafeFilename(t *testing.T) {
	cases := map[string]string{
		"report.pdf":            "report.pdf",
		"../../etc/passwd":      "passwd",
		`..\..\windows\sys.dll`: "sys.dll",
		"a:b|c?.txt":            "a_b_c_.txt",
		"..":                    "",
		"":                      "",
	}
	for input, want := range cases {
		if got := SafeFilename(input); got != want {
			t.Errorf("SafeFilename(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMessageArtifactPathIsContained(t *testing.T) {
	msg := &Message{
		Mailbox: "../../evil",
		UID:     3,
		Date:    time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Subject: "../../../etc/passwd",
		Raw:     []byte("body"),
	}
	got := msg.Artifacts()[0].Path
	if strings.Contains(got, "..") {
		t.Fatalf("artifact path %q can escape the destination", got)
	}
	if path.IsAbs(got) {
		t.Fatalf("artifact path %q is absolute", got)
	}
}

func TestMessageArtifactPathUsesMessageDate(t *testing.T) {
	msg := &Message{
		Mailbox: "INBOX",
		UID:     11,
		Date:    time.Date(2026, 3, 9, 15, 4, 5, 0, time.UTC),
		Subject: "Quarterly report",
		Raw:     []byte("body"),
	}
	want := "INBOX/2026/03/20260309T150405Z-11-quarterly-report.eml"
	if got := msg.Artifacts()[0].Path; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}
