package cmd

import (
	"github.com/Cyb3r-Jak3/email-backup-tool/stages/transform"
	"github.com/urfave/cli/v3"
)

// connectionFlags are the flags describing the mail source, shared by every
// command that logs in. They are defined once so the login check and the backup
// run cannot drift apart.
func connectionFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "IMAP server address (e.g., `imap.example.com:993`)",
			Sources: flagSources(KeyIMAPServer, "IMAP_SERVER"),
		},
		&cli.StringFlag{
			Name:    "username",
			Aliases: []string{"u"},
			Usage:   "Username for IMAP login",
			Sources: flagSources(KeyIMAPUsername, "IMAP_USERNAME"),
		},
		&cli.StringFlag{
			Name:    "password",
			Aliases: []string{"p"},
			Usage:   "Password for IMAP login",
			Sources: flagSources(KeyIMAPPassword, "IMAP_PASSWORD"),
		},
		&cli.StringFlag{
			Name:    "tls",
			Aliases: []string{"t"},
			Usage:   "TLS configuration (STARTTLS, SSL, or NONE)",
			Value:   TLSImplicit,
			Sources: flagSources(KeyIMAPTLS, "IMAP_TLS"),
			Validator: func(value string) error {
				_, err := NormalizeTLS(value)
				return err
			},
		},
		&cli.StringFlag{
			Name:    "ca-cert",
			Usage:   "Path to a CA certificate `FILE` used to verify the server",
			Sources: flagSources(KeyIMAPCACert, "IMAP_CA_CERT"),
		},
		&cli.BoolFlag{
			Name:    "insecure",
			Aliases: []string{"k"},
			Usage:   "Allow insecure server connections when using SSL",
			Sources: flagSources(KeyIMAPInsecure, "IMAP_INSECURE"),
		},
	}
}

// encryptFlags select the stage 2 recipient key.
func encryptFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "public-key-file",
			Usage:   "Path to a PEM public key `FILE` to encrypt the backup to",
			Sources: flagSources(KeyEncryptPublicKeyFile, "IMAP_BACKUP_PUBLIC_KEY_FILE"),
		},
		&cli.StringFlag{
			Name:    "public-key",
			Usage:   "PEM public `KEY` to encrypt the backup to, given inline",
			Sources: flagSources(KeyEncryptPublicKeyString, "IMAP_BACKUP_PUBLIC_KEY"),
		},
		&cli.StringFlag{
			Name:    "public-key-fingerprint",
			Usage:   "`FINGERPRINT` of an OpenPGP public key to fetch from --public-key-server and encrypt the backup to",
			Sources: flagSources(KeyEncryptPublicKeyFingerprint, "IMAP_BACKUP_PUBLIC_KEY_FINGERPRINT"),
		},
		&cli.StringFlag{
			Name:    "public-key-server",
			Usage:   "Keyserver `HOST` that --public-key-fingerprint is looked up on",
			Value:   transform.DefaultKeyServer,
			Sources: flagSources(KeyEncryptPublicKeyServer, "IMAP_BACKUP_PUBLIC_KEY_SERVER"),
		},
	}
}

// storeFlags select the stage 3 destinations. A destination is used when its
// flag has a value, whether that came from the command line, the environment or
// the save block of the config file, so several may be active at once.
func storeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "directory",
			Aliases: []string{"d"},
			Usage:   "Write the backup to a local `DIRECTORY`",
			Sources: flagSources(KeySaveLocalDirectory, "IMAP_BACKUP_DIRECTORY"),
		},
		&cli.StringFlag{
			Name:    "tar",
			Usage:   "Write the backup to a tar archive at `PATH` (gzipped if it ends in .gz or .tgz)",
			Sources: flagSources(KeySaveTarPath, "IMAP_BACKUP_TAR"),
		},
		&cli.StringFlag{
			Name:    "s3-bucket",
			Usage:   "Write the backup to the S3-compatible `BUCKET`",
			Sources: flagSources(KeySaveS3Bucket, "IMAP_BACKUP_S3_BUCKET"),
		},
		&cli.StringFlag{
			Name:    "s3-region",
			Usage:   "`REGION` of the S3 bucket",
			Sources: flagSources(KeySaveS3Region, "IMAP_BACKUP_S3_REGION", "AWS_REGION"),
		},
		&cli.StringFlag{
			Name:    "s3-endpoint",
			Usage:   "`URL` of an S3-compatible service to use instead of AWS",
			Sources: flagSources(KeySaveS3Endpoint, "IMAP_BACKUP_S3_ENDPOINT"),
		},
		&cli.StringFlag{
			Name:    "s3-prefix",
			Usage:   "Key `PREFIX` placed in front of every uploaded object",
			Sources: flagSources(KeySaveS3Prefix, "IMAP_BACKUP_S3_PREFIX"),
		},
		&cli.StringFlag{
			Name:    "s3-access-key-id",
			Usage:   "Access key `ID` for the S3 bucket; falls back to the ambient AWS configuration",
			Sources: flagSources(KeySaveS3AccessKey, "IMAP_BACKUP_S3_ACCESS_KEY_ID"),
		},
		&cli.StringFlag{
			Name:    "s3-secret-access-key",
			Usage:   "Secret access `KEY` for the S3 bucket; falls back to the ambient AWS configuration",
			Sources: flagSources(KeySaveS3SecretKey, "IMAP_BACKUP_S3_SECRET_ACCESS_KEY"),
		},
	}
}
