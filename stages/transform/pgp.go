package transform

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Cyb3r-Jak3/email-backup-tool/stages"
	"github.com/ProtonMail/go-crypto/openpgp"
)

// PGPEncryptor is stage 2 using OpenPGP. It exists for keys that already live
// in a keyring or on a hardware token: stored files are ordinary OpenPGP
// messages that `gpg -d` reads, so the private key can stay on a smartcard and
// never touch disk.
type PGPEncryptor struct {
	recipients openpgp.EntityList
}

// compile-time check that PGPEncryptor satisfies stage 2.
var _ Encryptor = (*PGPEncryptor)(nil)

// NewPGPEncryptor parses an armoured or binary OpenPGP public key. A key ring
// holding several keys encrypts to all of them.
func NewPGPEncryptor(keyMaterial []byte) (*PGPEncryptor, error) {
	entities, err := readKeyRing(keyMaterial)
	if err != nil {
		return nil, err
	}
	// A key that cannot encrypt (a signing-only key, or one whose encryption
	// subkey has expired) would fail on the first message rather than here, so
	// it is rejected up front.
	for _, entity := range entities {
		if _, ok := entity.EncryptionKey(time.Now()); !ok {
			return nil, fmt.Errorf("openpgp key %s has no usable encryption subkey", entityName(entity))
		}
	}
	return &PGPEncryptor{recipients: entities}, nil
}

// Name implements stages.Transformer.
func (e *PGPEncryptor) Name() string {
	return fmt.Sprintf("encrypt (pgp, %d recipient(s))", len(e.recipients))
}

// Scheme implements Encryptor.
func (e *PGPEncryptor) Scheme() Scheme { return SchemePGP }

// Recipients implements Encryptor.
func (e *PGPEncryptor) Recipients() string {
	names := make([]string, 0, len(e.recipients))
	for _, entity := range e.recipients {
		names = append(names, entityName(entity))
	}
	return strings.Join(names, ", ")
}

// Transform implements stages.Transformer, replacing the artifact's content
// with a binary OpenPGP message.
func (e *PGPEncryptor) Transform(_ context.Context, artifact *stages.Artifact) (*stages.Artifact, error) {
	var out bytes.Buffer
	// The message is not signed: a backup proves nothing about who wrote the
	// mail, and signing would need a private key this tool should never hold.
	writer, err := openpgp.Encrypt(&out, e.recipients, nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("starting openpgp encryption: %w", err)
	}
	if _, err := writer.Write(artifact.Data); err != nil {
		return nil, fmt.Errorf("encrypting %s: %w", artifact.Path, err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("finishing openpgp encryption of %s: %w", artifact.Path, err)
	}
	return sealed(artifact, SchemePGP, out.Bytes()), nil
}

// PGPDecryptor reverses PGPEncryptor with a private key.
type PGPDecryptor struct {
	identities openpgp.EntityList
}

// compile-time check that PGPDecryptor satisfies Decryptor.
var _ Decryptor = (*PGPDecryptor)(nil)

// NewPGPDecryptor parses an OpenPGP private key, unlocking it with passphrase
// when it is protected.
func NewPGPDecryptor(keyMaterial []byte, passphrase string) (*PGPDecryptor, error) {
	entities, err := readKeyRing(keyMaterial)
	if err != nil {
		return nil, err
	}
	for _, entity := range entities {
		if entity.PrivateKey == nil {
			return nil, fmt.Errorf("openpgp key %s is a public key, not a private one", entityName(entity))
		}
		if !entity.PrivateKey.Encrypted {
			continue
		}
		if passphrase == "" {
			return nil, fmt.Errorf("openpgp key %s is passphrase protected, pass --passphrase", entityName(entity))
		}
		if err := entity.DecryptPrivateKeys([]byte(passphrase)); err != nil {
			return nil, fmt.Errorf("unlocking openpgp key %s: %w", entityName(entity), err)
		}
	}
	return &PGPDecryptor{identities: entities}, nil
}

// Scheme implements Decryptor.
func (d *PGPDecryptor) Scheme() Scheme { return SchemePGP }

// Open implements Decryptor.
func (d *PGPDecryptor) Open(ciphertext []byte) ([]byte, error) {
	details, err := openpgp.ReadMessage(bytes.NewReader(ciphertext), d.identities, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypting openpgp message: %w", err)
	}
	plaintext, err := io.ReadAll(details.UnverifiedBody)
	if err != nil {
		return nil, fmt.Errorf("reading openpgp message: %w", err)
	}
	return plaintext, nil
}

// readKeyRing parses OpenPGP keys in either armoured or binary form.
func readKeyRing(keyMaterial []byte) (openpgp.EntityList, error) {
	var (
		entities openpgp.EntityList
		err      error
	)
	if bytes.Contains(keyMaterial, []byte("-----BEGIN PGP")) {
		entities, err = openpgp.ReadArmoredKeyRing(bytes.NewReader(keyMaterial))
	} else {
		entities, err = openpgp.ReadKeyRing(bytes.NewReader(keyMaterial))
	}
	if err != nil {
		return nil, fmt.Errorf("parsing openpgp key: %w", err)
	}
	if len(entities) == 0 {
		return nil, fmt.Errorf("no openpgp keys found")
	}
	return entities, nil
}

// entityName describes a key by its first user ID, falling back to its key ID.
func entityName(entity *openpgp.Entity) string {
	if identity := entity.PrimaryIdentity(); identity != nil && identity.Name != "" {
		return fmt.Sprintf("%s (%X)", identity.Name, entity.PrimaryKey.KeyId)
	}
	return fmt.Sprintf("%X", entity.PrimaryKey.KeyId)
}
