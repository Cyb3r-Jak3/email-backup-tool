# generate-key

Creates an age or OpenPGP key pair for encrypting backups — see
[Stage 2: Encrypt](../stages/encrypt.md) for how encryption works and how the
resulting public key is used.

```bash
./imap-backup-tool generate-key                        # age key pair (default)
./imap-backup-tool generate-key --type pgp \
  --name "Jane Doe" --email jane@example.com            # OpenPGP key pair
```

## Flags

| CLI flag | Environment variable | Default | Description |
|---|---|---|---|
| `--type`, `-t` | `EMAIL_BACKUP_KEY_TYPE` | `age` | Key type to generate: `age` or `pgp`. |
| `--output`, `-o` | — | `email-backup-key` | Output path prefix; writes `PREFIX.key` (private) and `PREFIX.pub` (public). |
| `--name` | — | `IMAP Backup` | User ID name recorded in an OpenPGP key. |
| `--email` | — | — | User ID email recorded in an OpenPGP key. |
| `--force` | — | `false` | Overwrite existing key files. |

## What you get

Two files are written:

- **`PREFIX.key`** — the private key. This is what reads the backup back —
  keep it safe and offline. Never put it in the config file.
- **`PREFIX.pub`** — the public key. Put its path in `encrypt.public_key_file`
  (or pass it to `--public-key-file`) so backups get encrypted to it.
