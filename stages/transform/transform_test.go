package transform

import (
	"bytes"
	"context"
	"io"
	"testing"

	"filippo.io/age"
	"github.com/Cyb3r-Jak3/email-backup-tool/stages"
	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// keyPair is public and private key material for one scheme.
type keyPair struct {
	public  []byte
	private []byte
}

// ageKeys generates an age identity in the on-disk format the age CLI uses.
func ageKeys(t *testing.T) keyPair {
	t.Helper()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generating age key: %v", err)
	}
	return keyPair{
		public:  []byte("# a comment the parser must ignore\n" + identity.Recipient().String() + "\n"),
		private: []byte("# created: today\n" + identity.String() + "\n"),
	}
}

// pgpKeys generates an armoured OpenPGP key pair.
func pgpKeys(t *testing.T) keyPair {
	t.Helper()
	entity, err := openpgp.NewEntity("Test Backup", "", "backup@example.com",
		&packet.Config{Algorithm: packet.PubKeyAlgoEdDSA})
	if err != nil {
		t.Fatalf("generating openpgp key: %v", err)
	}
	return keyPair{
		public:  armorFor(t, openpgp.PublicKeyType, entity.Serialize),
		private: armorFor(t, openpgp.PrivateKeyType, func(w io.Writer) error { return entity.SerializePrivateWithoutSigning(w, nil) }),
	}
}

func armorFor(t *testing.T, blockType string, serialize func(io.Writer) error) []byte {
	t.Helper()
	var body bytes.Buffer
	if err := serialize(&body); err != nil {
		t.Fatalf("serialising key: %v", err)
	}
	var out bytes.Buffer
	writer, err := armor.Encode(&out, blockType, nil)
	if err != nil {
		t.Fatalf("armouring key: %v", err)
	}
	if _, err := writer.Write(body.Bytes()); err != nil {
		t.Fatalf("armouring key: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("armouring key: %v", err)
	}
	return out.Bytes()
}

// schemes returns a key pair per supported scheme.
func schemes(t *testing.T) map[Scheme]keyPair {
	t.Helper()
	return map[Scheme]keyPair{
		SchemeAge: ageKeys(t),
		SchemePGP: pgpKeys(t),
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	plaintexts := map[string][]byte{
		"empty":  {},
		"short":  []byte("Subject: hello\r\n\r\nbody"),
		"binary": bytes.Repeat([]byte{0x00, 0xff, 0x7f}, 5000),
	}

	for scheme, keys := range schemes(t) {
		for size, plaintext := range plaintexts {
			t.Run(string(scheme)+"/"+size, func(t *testing.T) {
				encryptor, err := NewEncryptor(keys.public, "test key")
				if err != nil {
					t.Fatalf("NewEncryptor: %v", err)
				}
				if encryptor.Scheme() != scheme {
					t.Fatalf("scheme = %q, want %q", encryptor.Scheme(), scheme)
				}

				artifact := &stages.Artifact{Path: "INBOX/2026/09/msg.eml", Data: plaintext}
				out, err := encryptor.Transform(context.Background(), artifact)
				if err != nil {
					t.Fatalf("Transform: %v", err)
				}
				if want := artifact.Path + scheme.Suffix(); out.Path != want {
					t.Fatalf("path = %q, want %q", out.Path, want)
				}
				if len(plaintext) > 0 && bytes.Contains(out.Data, plaintext) {
					t.Fatal("ciphertext still contains the plaintext")
				}

				decryptor, err := NewDecryptor(keys.private, "", "test key")
				if err != nil {
					t.Fatalf("NewDecryptor: %v", err)
				}
				opened, err := decryptor.Open(out.Data)
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				if !bytes.Equal(opened, plaintext) {
					t.Fatalf("round trip changed the data: got %d bytes, want %d", len(opened), len(plaintext))
				}
			})
		}
	}
}

func TestEncryptedFilesAreInTheStandardFormat(t *testing.T) {
	keys := schemes(t)

	// An age file must start with the age v1 header, so `age -d` reads it.
	encryptor, err := NewEncryptor(keys[SchemeAge].public, "test key")
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	out, err := encryptor.Transform(context.Background(), &stages.Artifact{Path: "m.eml", Data: []byte("hi")})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if !bytes.HasPrefix(out.Data, []byte("age-encryption.org/v1\n")) {
		t.Fatalf("age output does not begin with the age v1 header: %q", firstBytes(out.Data))
	}

	// An OpenPGP message must begin with a public-key encrypted session key
	// packet, so `gpg -d` reads it. Tag 1 in the new packet format is 0xC1.
	encryptor, err = NewEncryptor(keys[SchemePGP].public, "test key")
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	out, err = encryptor.Transform(context.Background(), &stages.Artifact{Path: "m.eml", Data: []byte("hi")})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if len(out.Data) == 0 || out.Data[0] != 0xC1 {
		t.Fatalf("openpgp output does not begin with a session key packet: %q", firstBytes(out.Data))
	}
}

func TestDecryptRejectsTamperingAndWrongKeys(t *testing.T) {
	for scheme, keys := range schemes(t) {
		t.Run(string(scheme), func(t *testing.T) {
			encryptor, err := NewEncryptor(keys.public, "test key")
			if err != nil {
				t.Fatalf("NewEncryptor: %v", err)
			}
			out, err := encryptor.Transform(context.Background(),
				&stages.Artifact{Path: "m.eml", Data: []byte("a message worth protecting")})
			if err != nil {
				t.Fatalf("Transform: %v", err)
			}
			decryptor, err := NewDecryptor(keys.private, "", "test key")
			if err != nil {
				t.Fatalf("NewDecryptor: %v", err)
			}

			// Corrupting the body must be caught: both formats authenticate it.
			tampered := append([]byte(nil), out.Data...)
			tampered[len(tampered)-1] ^= 0x01
			if _, err := decryptor.Open(tampered); err == nil {
				t.Error("Open accepted a container with a corrupted body")
			}
			if _, err := decryptor.Open(out.Data[:len(out.Data)/2]); err == nil {
				t.Error("Open accepted a truncated container")
			}

			// A different key of the same scheme must not open it.
			var other keyPair
			if scheme == SchemeAge {
				other = ageKeys(t)
			} else {
				other = pgpKeys(t)
			}
			otherDecryptor, err := NewDecryptor(other.private, "", "other key")
			if err != nil {
				t.Fatalf("NewDecryptor: %v", err)
			}
			if _, err := otherDecryptor.Open(out.Data); err == nil {
				t.Error("Open accepted an unrelated private key")
			}
		})
	}
}

func TestDetectScheme(t *testing.T) {
	keys := schemes(t)
	cases := map[string]struct {
		material []byte
		want     Scheme
	}{
		"age public":  {keys[SchemeAge].public, SchemeAge},
		"age private": {keys[SchemeAge].private, SchemeAge},
		"pgp public":  {keys[SchemePGP].public, SchemePGP},
		"pgp private": {keys[SchemePGP].private, SchemePGP},
	}
	for name, tc := range cases {
		got, err := DetectScheme(tc.material)
		if err != nil {
			t.Errorf("DetectScheme(%s): %v", name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("DetectScheme(%s) = %q, want %q", name, got, tc.want)
		}
	}
}

func TestEncryptorRejectsPrivateKeyMaterialForPGP(t *testing.T) {
	// Handing the encryptor a private key is a configuration mistake worth
	// catching; for PGP the public half is what belongs in the config.
	keys := pgpKeys(t)
	decryptor, err := NewDecryptor(keys.public, "", "test key")
	if err == nil {
		t.Fatalf("NewDecryptor accepted a public key: %v", decryptor)
	}
}

func TestPassThroughLeavesArtifactAlone(t *testing.T) {
	artifact := &stages.Artifact{Path: "INBOX/msg.eml", Data: []byte("raw message")}
	out, err := PassThrough{}.Transform(context.Background(), artifact)
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if out.Path != artifact.Path || !bytes.Equal(out.Data, artifact.Data) {
		t.Fatal("PassThrough changed the artifact")
	}
}

// firstBytes trims data for an error message.
func firstBytes(data []byte) []byte {
	if len(data) > 24 {
		return data[:24]
	}
	return data
}

func TestDetectCiphertextScheme(t *testing.T) {
	for scheme, keys := range schemes(t) {
		encryptor, err := NewEncryptor(keys.public, "test key")
		if err != nil {
			t.Fatalf("NewEncryptor: %v", err)
		}
		out, err := encryptor.Transform(context.Background(),
			&stages.Artifact{Path: "m.eml", Data: []byte("some mail")})
		if err != nil {
			t.Fatalf("Transform: %v", err)
		}
		got, ok := DetectCiphertextScheme(out.Data)
		if !ok {
			t.Errorf("DetectCiphertextScheme did not recognise a %s file", scheme)
			continue
		}
		if got != scheme {
			t.Errorf("DetectCiphertextScheme = %q, want %q", got, scheme)
		}
	}

	if _, ok := DetectCiphertextScheme([]byte("From: someone -- plain mail")); ok {
		t.Error("DetectCiphertextScheme claimed plaintext was encrypted")
	}
	if _, ok := DetectCiphertextScheme(nil); ok {
		t.Error("DetectCiphertextScheme claimed empty data was encrypted")
	}
}
