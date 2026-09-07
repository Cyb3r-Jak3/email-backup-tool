package transform

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"filippo.io/age"
	"github.com/Cyb3r-Jak3/email-backup-tool/stages"
)

// AgeEncryptor is stage 2 using age encryption, the default scheme. Stored
// files are ordinary age files: `age -d -i KEY FILE` reads them, with no need
// for this tool.
type AgeEncryptor struct {
	recipients []age.Recipient
	names      []string
}

// compile-time check that AgeEncryptor satisfies stage 2.
var _ Encryptor = (*AgeEncryptor)(nil)

// NewAgeEncryptor parses one or more age public keys. The format is the one the
// age CLI uses for recipient files: one key per line, blank lines and lines
// starting with "#" ignored, so a file written by `age-keygen` can be used as
// it stands.
//
// Only native age keys are accepted. SSH keys are deliberately not supported,
// following age's own advice for new integrations.
func NewAgeEncryptor(keyMaterial []byte) (*AgeEncryptor, error) {
	recipients, err := age.ParseRecipients(bytes.NewReader(keyMaterial))
	if err != nil {
		return nil, fmt.Errorf("parsing age recipients: %w", err)
	}
	if len(recipients) == 0 {
		return nil, fmt.Errorf("no age recipients found")
	}
	return &AgeEncryptor{recipients: recipients, names: recipientNames(keyMaterial)}, nil
}

// Name implements stages.Transformer.
func (e *AgeEncryptor) Name() string {
	return fmt.Sprintf("encrypt (age, %d recipient(s))", len(e.recipients))
}

// Scheme implements Encryptor.
func (e *AgeEncryptor) Scheme() Scheme { return SchemeAge }

// Recipients implements Encryptor.
func (e *AgeEncryptor) Recipients() string { return strings.Join(e.names, ", ") }

// Transform implements stages.Transformer, replacing the artifact's content
// with an age file.
func (e *AgeEncryptor) Transform(_ context.Context, artifact *stages.Artifact) (*stages.Artifact, error) {
	var out bytes.Buffer
	writer, err := age.Encrypt(&out, e.recipients...)
	if err != nil {
		return nil, fmt.Errorf("starting age encryption: %w", err)
	}
	if _, err := writer.Write(artifact.Data); err != nil {
		return nil, fmt.Errorf("encrypting %s: %w", artifact.Path, err)
	}
	// age only writes its final authentication tag on close, so a file from an
	// unclosed writer would be silently truncated.
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("finishing age encryption of %s: %w", artifact.Path, err)
	}
	return sealed(artifact, SchemeAge, out.Bytes()), nil
}

// AgeDecryptor reverses AgeEncryptor with an age identity.
type AgeDecryptor struct {
	identities []age.Identity
}

// compile-time check that AgeDecryptor satisfies Decryptor.
var _ Decryptor = (*AgeDecryptor)(nil)

// NewAgeDecryptor parses one or more age private keys, in the same one-per-line
// format the age CLI accepts for identity files.
func NewAgeDecryptor(keyMaterial []byte) (*AgeDecryptor, error) {
	identities, err := age.ParseIdentities(bytes.NewReader(keyMaterial))
	if err != nil {
		return nil, fmt.Errorf("parsing age identities: %w", err)
	}
	if len(identities) == 0 {
		return nil, fmt.Errorf("no age identities found")
	}
	return &AgeDecryptor{identities: identities}, nil
}

// Scheme implements Decryptor.
func (d *AgeDecryptor) Scheme() Scheme { return SchemeAge }

// Open implements Decryptor.
func (d *AgeDecryptor) Open(ciphertext []byte) ([]byte, error) {
	reader, err := age.Decrypt(bytes.NewReader(ciphertext), d.identities...)
	if err != nil {
		return nil, fmt.Errorf("decrypting age file: %w", err)
	}
	plaintext, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("reading age file: %w", err)
	}
	return plaintext, nil
}

// recipientNames lists the age keys found in key material, for logging. The
// keys are public, so recording them is safe and makes it easy to confirm which
// recipient a backup was addressed to.
func recipientNames(keyMaterial []byte) []string {
	var names []string
	for line := range strings.SplitSeq(string(keyMaterial), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, ageRecipientPrefix) {
			names = append(names, line)
		}
	}
	return names
}
