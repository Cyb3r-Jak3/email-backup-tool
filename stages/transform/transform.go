// Package transform holds stage 2 implementations: what happens to a downloaded
// artifact on its way to storage. Each implements stages.Transformer, so the
// encrypting and pass-through behaviors are interchangeable and a future
// transformer (compression, redaction) slots in the same way.
//
// Two encryption schemes are supported, both of them standard formats with
// their own well established command line tools. That matters more for a backup
// than any detail of the cryptography: mail encrypted today must still be
// readable years from now, on a machine where this tool may not exist.
//
//	age  the default, decryptable with `age -d -i KEY FILE`
//	pgp  for existing keyrings and hardware keys, decryptable with `gpg -d FILE`
//
// Which one is used is decided by the key itself, in DetectScheme, so there is
// no separate setting to keep in step with the key material.
package transform

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/Cyb3r-Jak3/email-backup-tool/stages"
)

// Scheme names an encryption format.
type Scheme string

const (
	// SchemeAge is age encryption, the default.
	SchemeAge Scheme = "age"
	// SchemePGP is OpenPGP encryption.
	SchemePGP Scheme = "pgp"
)

// Suffix is appended to the path of an artifact encrypted with the scheme, so
// the format of a stored file is obvious from its name and the standard tools
// recognize it.
func (s Scheme) Suffix() string { return "." + string(s) }

// Markers identifying key material.
const (
	// ageRecipientPrefix begins an age public key.
	ageRecipientPrefix = "age1"
	// ageIdentityPrefix begins an age private key.
	ageIdentityPrefix = "AGE-SECRET-KEY-1"
	// pgpPublicBlock and pgpPrivateBlock open an armoured OpenPGP key.
	pgpPublicBlock  = "-----BEGIN PGP PUBLIC KEY BLOCK-----"
	pgpPrivateBlock = "-----BEGIN PGP PRIVATE KEY BLOCK-----" //nolint:gosec // this is a constant, not a secret
)

// Encryptor is stage 2 encrypting each artifact to a public key. Only the
// holder of the matching private key can read the backup afterwards; this tool
// never needs the private key to make one.
type Encryptor interface {
	stages.Transformer
	// Scheme reports the format artifacts are written in.
	Scheme() Scheme
	// Recipients describes who the backup can be opened by, for logging.
	Recipients() string
}

// Decryptor reverses an Encryptor given the private key.
type Decryptor interface {
	// Scheme reports the format the decryptor reads.
	Scheme() Scheme
	// Open decrypts one stored artifact.
	Open(ciphertext []byte) ([]byte, error)
}

// NewEncryptor builds stage 2 for the given public key material, choosing the
// scheme from the key itself. source names the key in errors and logs.
func NewEncryptor(keyMaterial []byte, source string) (Encryptor, error) {
	scheme, err := DetectScheme(keyMaterial)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	switch scheme {
	case SchemeAge:
		encryptor, err := NewAgeEncryptor(keyMaterial)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
		return encryptor, nil
	case SchemePGP:
		encryptor, err := NewPGPEncryptor(keyMaterial)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
		return encryptor, nil
	default:
		return nil, fmt.Errorf("%s: unsupported scheme %q", source, scheme)
	}
}

// NewDecryptor builds the counterpart to NewEncryptor from private key
// material. passphrase unlocks a protected OpenPGP key and is ignored by age.
func NewDecryptor(keyMaterial []byte, passphrase, source string) (Decryptor, error) {
	scheme, err := DetectScheme(keyMaterial)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	switch scheme {
	case SchemeAge:
		decryptor, err := NewAgeDecryptor(keyMaterial)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
		return decryptor, nil
	case SchemePGP:
		decryptor, err := NewPGPDecryptor(keyMaterial, passphrase)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
		return decryptor, nil
	default:
		return nil, fmt.Errorf("%s: unsupported scheme %q", source, scheme)
	}
}

// DetectScheme identifies key material, public or private. The key's own format
// selects the scheme, so there is no way for a configured scheme and a
// configured key to disagree.
func DetectScheme(keyMaterial []byte) (Scheme, error) {
	text := string(keyMaterial)
	switch {
	case strings.Contains(text, pgpPublicBlock), strings.Contains(text, pgpPrivateBlock):
		return SchemePGP, nil
	case hasLinePrefix(text, ageRecipientPrefix), hasLinePrefix(text, ageIdentityPrefix):
		return SchemeAge, nil
	default:
		return "", fmt.Errorf("unrecognized key: expected an age key (%s… or %s…) or an armoured OpenPGP key",
			ageRecipientPrefix, ageIdentityPrefix)
	}
}

// Markers identifying an encrypted file, as written by each scheme.
var (
	// ageFileHeader opens every age file.
	ageFileHeader = []byte("age-encryption.org/v1")
	// pgpArmorHeader opens an armoured OpenPGP message.
	pgpArmorHeader = []byte("-----BEGIN PGP MESSAGE-----")
)

// pgpSessionKeyPacket is the first byte of a binary OpenPGP message: a
// public-key encrypted session key packet in the new packet format.
const pgpSessionKeyPacket = 0xC1

// DetectCiphertextScheme identifies the format of a stored file, reporting
// false when it is not recognised. It lets the decrypt command tell someone
// they have reached for the wrong key rather than passing an OpenPGP message
// to an age parser and surfacing whatever that happens to complain about.
func DetectCiphertextScheme(ciphertext []byte) (Scheme, bool) {
	switch {
	case bytes.HasPrefix(ciphertext, ageFileHeader):
		return SchemeAge, true
	case bytes.HasPrefix(ciphertext, pgpArmorHeader):
		return SchemePGP, true
	case len(ciphertext) > 0 && ciphertext[0] == pgpSessionKeyPacket:
		return SchemePGP, true
	default:
		return "", false
	}
}

// hasLinePrefix reports whether any line of text starts with prefix, ignoring
// blank lines and the comment lines age writes above a key.
func hasLinePrefix(text, prefix string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// PassThrough is stage 2 doing nothing: artifacts reach storage exactly as they
// were downloaded. It is what a run without a configured public key uses, so
// the pipeline never has to special-case a missing transformer.
type PassThrough struct{}

// compile-time check that PassThrough satisfies stage 2.
var _ stages.Transformer = PassThrough{}

// Name implements stages.Transformer.
func (PassThrough) Name() string { return "none (unencrypted)" }

// Transform implements stages.Transformer by returning the artifact unchanged.
func (PassThrough) Transform(_ context.Context, artifact *stages.Artifact) (*stages.Artifact, error) {
	return artifact, nil
}

// sealed builds the artifact an encryptor returns, marking the path with the
// scheme's suffix so stored files are self-describing.
func sealed(artifact *stages.Artifact, scheme Scheme, ciphertext []byte) *stages.Artifact {
	return &stages.Artifact{
		Path:    artifact.Path + scheme.Suffix(),
		Data:    ciphertext,
		Message: artifact.Message,
	}
}
