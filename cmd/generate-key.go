package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/Cyb3r-Jak3/email-backup-tool/stages/transform"
	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

// File name suffixes for a generated key pair. The private half is the one to
// guard; the public half goes in the config.
const (
	privateKeySuffix = ".key"
	publicKeySuffix  = ".pub"
)

// GenerateKeyCommand returns the "generate-key" command, which creates the key
// pair stage 2 encrypts to.
func GenerateKeyCommand() *cli.Command {
	return &cli.Command{
		Name:  "generate-key",
		Usage: "Generate an age or OpenPGP key pair for encrypting backups",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "type",
				Aliases: []string{"t"},
				Usage:   "Key type to generate: `age` or pgp",
				Value:   string(transform.SchemeAge),
				Sources: cli.EnvVars("EMAIL_BACKUP_KEY_TYPE"),
			},
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Usage:   "Output path `PREFIX`; writes PREFIX.key and PREFIX.pub",
				Value:   "email-backup-key",
			},
			&cli.StringFlag{
				Name:  "name",
				Usage: "User ID `NAME` recorded in an OpenPGP key",
				Value: "IMAP Backup",
			},
			&cli.StringFlag{
				Name:  "email",
				Usage: "User ID `EMAIL` recorded in an OpenPGP key",
			},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Overwrite existing key files",
			},
		},
		Action: generateKey,
	}
}

func generateKey(_ context.Context, cmd *cli.Command) error {
	keyType := transform.Scheme(strings.ToLower(cmd.String("type")))
	prefix := cmd.String("output")
	privatePath := prefix + privateKeySuffix
	publicPath := prefix + publicKeySuffix

	if !cmd.Bool("force") {
		for _, path := range []string{privatePath, publicPath} {
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists, use --force to overwrite", path)
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("checking %s: %w", path, err)
			}
		}
	}

	var private, public []byte
	var err error
	switch keyType {
	case transform.SchemeAge:
		private, public, err = generateAgeKey()
	case transform.SchemePGP:
		private, public, err = generatePGPKey(cmd.String("name"), cmd.String("email"))
	default:
		return fmt.Errorf("unsupported key type %q, must be one of: %s, %s",
			keyType, transform.SchemeAge, transform.SchemePGP)
	}
	if err != nil {
		return err
	}

	if err := writeKeyFile(privatePath, private, 0o600); err != nil {
		return err
	}
	if err := writeKeyFile(publicPath, public, 0o644); err != nil {
		return err
	}

	logger.Info("Wrote key pair",
		zap.String("type", string(keyType)),
		zap.String("private_key", privatePath),
		zap.String("public_key", publicPath),
	)
	fmt.Printf("Private key: %s  (keep this safe, it is what reads the backup)\n", privatePath)
	fmt.Printf("Public key:  %s  (put this in encrypt.public_key_file)\n", publicPath)
	return nil
}

// generateAgeKey creates an age identity, in the same file layout age-keygen
// writes, so both halves work with the age CLI unchanged.
func generateAgeKey() (private, public []byte, err error) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, nil, fmt.Errorf("generating age key: %w", err)
	}
	recipient := identity.Recipient().String()
	private = fmt.Appendf(nil, "# created: %s\n# public key: %s\n%s\n",
		time.Now().Format(time.RFC3339), recipient, identity)
	public = fmt.Appendf(nil, "%s\n", recipient)
	return private, public, nil
}

// generatePGPKey creates an OpenPGP key pair, armoured so it can be imported
// into a keyring with `gpg --import`.
func generatePGPKey(name, email string) (private, public []byte, err error) {
	// Curve 25519 rather than RSA: smaller, faster, and the modern default for
	// new OpenPGP keys.
	config := &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA}
	entity, err := openpgp.NewEntity(name, "email-backup-tool", email, config)
	if err != nil {
		return nil, nil, fmt.Errorf("generating openpgp key: %w", err)
	}

	// SerializePrivate computes the identity and subkey self-signatures a
	// freshly generated entity does not have yet, and stores them on the
	// entity. It has to run before Serialize, or the public half would be
	// written without them and no OpenPGP implementation would accept it.
	private, err = armorKey(openpgp.PrivateKeyType, func(w io.Writer) error {
		return entity.SerializePrivate(w, nil)
	})
	if err != nil {
		return nil, nil, err
	}
	public, err = armorKey(openpgp.PublicKeyType, entity.Serialize)
	if err != nil {
		return nil, nil, err
	}
	return private, public, nil
}

// armorKey wraps a serialized OpenPGP key in ASCII armour.
func armorKey(blockType string, serialize func(io.Writer) error) ([]byte, error) {
	var body bytes.Buffer
	if err := serialize(&body); err != nil {
		return nil, fmt.Errorf("serializing openpgp key: %w", err)
	}
	var out bytes.Buffer
	writer, err := armor.Encode(&out, blockType, nil)
	if err != nil {
		return nil, fmt.Errorf("armouring openpgp key: %w", err)
	}
	if _, err := writer.Write(body.Bytes()); err != nil {
		return nil, fmt.Errorf("armouring openpgp key: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("armouring openpgp key: %w", err)
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// writeKeyFile writes one half of a key pair, creating the directory it lives
// in and truncating any file already there.
func writeKeyFile(path string, data []byte, mode os.FileMode) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return file.Close()
}
