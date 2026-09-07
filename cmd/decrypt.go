package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Cyb3r-Jak3/email-backup-tool/stages/transform"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

// DecryptCommand returns the "decrypt" command, which reverses stage 2 for
// stored files. It is a convenience only: backups are written in standard
// formats, so `age -d -i KEY FILE` and `gpg -d FILE` read them just as well,
// which is the point of using those formats in the first place.
func DecryptCommand() *cli.Command {
	return &cli.Command{
		Name:      "decrypt",
		Usage:     "Decrypt backed up files with the private key",
		ArgsUsage: "FILE...",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "private-key-file",
				Aliases:  []string{"key", "i"},
				Usage:    "Path to the private key `FILE` matching the backup's public key",
				Required: true,
				Sources:  cli.EnvVars("IMAP_BACKUP_PRIVATE_KEY_FILE"),
			},
			&cli.StringFlag{
				Name:    "passphrase",
				Usage:   "`PASSPHRASE` unlocking a protected OpenPGP key",
				Sources: cli.EnvVars("IMAP_BACKUP_PASSPHRASE"),
			},
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Usage:   "`DIRECTORY` to write the decrypted files to (default: alongside each input)",
			},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Overwrite existing output files",
			},
		},
		Action: decrypt,
	}
}

func decrypt(_ context.Context, cmd *cli.Command) error {
	files := cmd.Args().Slice()
	if len(files) == 0 {
		return fmt.Errorf("no files given, pass the paths of the encrypted files to decrypt")
	}

	keyPath := cmd.String("private-key-file")
	keyMaterial, err := os.ReadFile(keyPath) //nolint:gosec // path is user supplied by design
	if err != nil {
		return fmt.Errorf("reading private key %s: %w", keyPath, err)
	}
	decryptor, err := transform.NewDecryptor(keyMaterial, cmd.String("passphrase"), keyPath)
	if err != nil {
		return err
	}
	logger.Info("Decrypting with private key",
		zap.String("file", keyPath),
		zap.String("scheme", string(decryptor.Scheme())),
	)

	outputDir := cmd.String("output")
	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0o750); err != nil {
			return fmt.Errorf("creating %s: %w", outputDir, err)
		}
	}

	for _, path := range files {
		ciphertext, err := os.ReadFile(path) //nolint:gosec // path is user supplied by design
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		// Reaching for the wrong key is an easy mistake, and the underlying
		// parsers describe it badly, so it is caught here.
		if found, ok := transform.DetectCiphertextScheme(ciphertext); ok && found != decryptor.Scheme() {
			return fmt.Errorf("%s is %s encrypted, but %s is a%s %s key",
				path, found, keyPath, vowel(decryptor.Scheme()), decryptor.Scheme())
		}
		plaintext, err := decryptor.Open(ciphertext)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		target := decryptedPath(path, decryptor.Scheme(), outputDir)
		if !cmd.Bool("force") {
			if _, err := os.Stat(target); err == nil { //nolint:gosec // This is a user-supplied path, not a secret in the source code.
				return fmt.Errorf("%s already exists, use --force to overwrite", target)
			}
		}
		if err := os.WriteFile(target, plaintext, 0o600); err != nil { //nolint:gosec // This is a user-supplied path, not a secret in the source code.
			return fmt.Errorf("writing %s: %w", target, err)
		}
		logger.Info("Decrypted file", zap.String("input", path), zap.String("output", target))
		fmt.Println(target)
	}
	return nil
}

// decryptedPath is where a decrypted file is written: the input path with the
// scheme's suffix removed, optionally relocated into an output directory.
func decryptedPath(path string, scheme transform.Scheme, outputDir string) string {
	target := strings.TrimSuffix(path, scheme.Suffix())
	if target == path {
		// The file was not named with the suffix, so add one rather than
		// silently overwriting the input.
		target = path + ".decrypted"
	}
	if outputDir != "" {
		target = filepath.Join(outputDir, filepath.Base(target))
	}
	return target
}

// vowel returns "n" when the scheme name needs "an" rather than "a".
func vowel(scheme transform.Scheme) string {
	if strings.HasPrefix(string(scheme), "a") {
		return "n"
	}
	return ""
}
