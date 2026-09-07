# decrypt

Decrypts one or more files a backup wrote, using the private key matching the
public key the backup was encrypted with. See
[Stage 2: Encrypt](../stages/encrypt.md) for how encryption is chosen — since
both age and OpenPGP are standard formats, this command is a convenience only:
`age -d -i KEY FILE` or `gpg -d FILE` read the same files just as well.

```bash
./imap-backup-tool decrypt -i imap-backup-key.key FILE...
```

## Flags

| CLI flag | Environment variable | Description |
|---|---|---|
| `--private-key-file`, `-i` | `IMAP_BACKUP_PRIVATE_KEY_FILE` | Path to the private key file matching the backup's public key. Required. |
| `--passphrase` | `IMAP_BACKUP_PASSPHRASE` | Passphrase unlocking a protected OpenPGP key. Ignored for age. |
| `--output`, `-o` | — | Directory to write decrypted files to (default: alongside each input). |
| `--force` | — | Overwrite existing output files. |

Reaching for the wrong key is caught up front: if a file's format doesn't
match the key you gave, the command reports that instead of a decryption
error, so a mismatched key doesn't just look like corrupted mail.
