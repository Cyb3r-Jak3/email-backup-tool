package stages

import (
	"strings"
	"unicode"
)

// maxSlugLength caps the subject-derived part of a path so that a long subject
// plus a deep mailbox name cannot push a destination path over a filesystem's
// per-component limit.
const maxSlugLength = 60

// Slug reduces arbitrary text, typically a subject line, to a lowercase
// filename fragment made only of letters, digits and single dashes.
func Slug(text string) string {
	var b strings.Builder
	lastDash := true // leading dashes are dropped
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case !lastDash && b.Len() < maxSlugLength:
			b.WriteRune('-')
			lastDash = true
		}
		if b.Len() >= maxSlugLength {
			break
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "no-subject"
	}
	return slug
}

// SafePath makes a mailbox name usable as a path prefix. IMAP hierarchy
// separators become directory separators, and everything that could escape the
// destination or upset a filesystem is replaced.
func SafePath(name string) string {
	if name == "" {
		return "unknown"
	}
	// IMAP servers use "/" or "." as the hierarchy delimiter; both become a
	// path separator so the on-disk layout mirrors the mailbox tree.
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.ReplaceAll(name, ".", "/")
	parts := strings.Split(name, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = SafeFilename(part)
		if part == "" || part == "." || part == ".." {
			continue
		}
		clean = append(clean, part)
	}
	if len(clean) == 0 {
		return "unknown"
	}
	return strings.Join(clean, "/")
}

// SafeFilename reduces one path element, such as an attachment filename, to a
// single component that cannot traverse out of the destination.
func SafeFilename(name string) string {
	// Only the base name is kept: an attachment claiming to be
	// "../../etc/passwd" becomes "passwd".
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7f:
			// control characters are dropped
		case strings.ContainsRune(`<>:"|?*`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	trimmed := strings.Trim(strings.TrimSpace(b.String()), ".")
	if trimmed == "" {
		return ""
	}
	return trimmed
}
