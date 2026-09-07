package fetch

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Cyb3r-Jak3/email-backup-tool/stages"
	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset" // register the non-UTF-8 charsets
	"github.com/emersion/go-message/mail"
)

// maxAttachmentDepth bounds how far into nested multiparts the walk goes, so a
// hostile or malformed message cannot drive it into unbounded recursion.
const maxAttachmentDepth = 16

// ExtractAttachments walks a raw RFC 5322 message and returns its attachment
// parts. The raw message is stored in full regardless; this only breaks the
// attachments out as separate files for callers that asked for them.
//
// Parts whose individual content cannot be decoded are skipped rather than
// failing the message, since one unreadable part should not cost the backup the
// rest of them.
func ExtractAttachments(raw []byte) ([]stages.Attachment, error) {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil {
		// message.Read reports unknown charsets and encodings as errors while
		// still returning a usable entity.
		if entity == nil || !message.IsUnknownCharset(err) {
			return nil, fmt.Errorf("reading message: %w", err)
		}
	}
	var out []stages.Attachment
	if err := walkParts(entity, 0, &out); err != nil {
		return out, err
	}
	return out, nil
}

// walkParts appends every attachment part of entity to out, descending into
// nested multipart containers.
func walkParts(entity *message.Entity, depth int, out *[]stages.Attachment) error {
	if depth > maxAttachmentDepth {
		return fmt.Errorf("message nests multiparts more than %d deep", maxAttachmentDepth)
	}
	if multipart := entity.MultipartReader(); multipart != nil {
		for {
			part, err := multipart.NextPart()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("reading multipart: %w", err)
			}
			if err := walkParts(part, depth+1, out); err != nil {
				return err
			}
		}
	}

	filename, ok := attachmentName(entity.Header)
	if !ok {
		return nil
	}
	data, err := io.ReadAll(entity.Body)
	if err != nil {
		// A part that will not decode is skipped: the raw message still holds
		// it, so nothing is actually lost from the backup.
		return nil //nolint:nilerr
	}
	*out = append(*out, stages.Attachment{
		Filename:    filename,
		ContentType: contentType(entity.Header),
		Data:        data,
	})
	return nil
}

// attachmentName reports the filename of an attachment part, and false for
// parts that are body text rather than attachments. A part counts as an
// attachment when it is dispositioned as one, or when it is inline but carries
// a filename (as embedded images typically do).
func attachmentName(header message.Header) (string, bool) {
	partHeader := mail.AttachmentHeader{Header: header}
	disposition, params, err := partHeader.ContentDisposition()
	if err != nil {
		return "", false
	}
	name := params["filename"]
	if name == "" {
		// Some senders only set the legacy name parameter on Content-Type.
		if _, typeParams, err := partHeader.ContentType(); err == nil {
			name = typeParams["name"]
		}
	}
	if !strings.EqualFold(disposition, "attachment") && name == "" {
		return "", false
	}
	safe := stages.SafeFilename(name)
	if safe == "" {
		// The caller names unnamed attachments positionally.
		return "", true
	}
	return safe, true
}

// contentType returns the part's MIME type, or an empty string if the header
// does not parse.
func contentType(header message.Header) string {
	mediaType, _, err := header.ContentType()
	if err != nil {
		return ""
	}
	return mediaType
}
