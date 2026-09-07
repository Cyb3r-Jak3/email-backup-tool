# Stage 2: Encrypt

Stage 2 optionally encrypts each downloaded message and attachment before it's
stored. If no key is configured, mail is stored unencrypted — but the run
warns loudly about it, on both the console and in the log, since storing mail
unencrypted should always be a deliberate choice.

## Standard formats only

Encryption uses standard, widely supported formats — never a bespoke,
proprietary container — so a backup stays readable with ordinary tools even if
this project is unavailable:

| Scheme | Decrypt with | Stored suffix |
|---|---|---|
| **age** (default) | `age -d -i KEY FILE` | `.age` |
| **OpenPGP** | `gpg -d FILE` | `.pgp` |

**The key material itself selects the scheme** — there is no separate setting
that could contradict the key:

- an age public key (`age1…`) selects age
- an armoured OpenPGP public key block selects OpenPGP

PEM keys from an older, retired scheme are rejected with a message pointing at
`generate-key`.

## Config options

These map to the optional `encrypt` block of the config file. Set **at most
one** of `public_key_file`, `public_key_string`, or `public_key_fingerprint`.

| YAML key | CLI flag | Environment variable | Default | Description |
|---|---|---|---|---|
| `encrypt.public_key_file` | `--public-key-file` | `IMAP_BACKUP_PUBLIC_KEY_FILE` | — | Path to a local age or OpenPGP public key file. |
| `encrypt.public_key_string` | `--public-key` | `IMAP_BACKUP_PUBLIC_KEY` | — | An age or armoured OpenPGP public key, given inline. |
| `encrypt.public_key_fingerprint` | `--public-key-fingerprint` | `IMAP_BACKUP_PUBLIC_KEY_FINGERPRINT` | — | Full 40-hex-digit v4 OpenPGP fingerprint to fetch from `public_key_server`. Spaces are ignored. Short/long key IDs are rejected — they aren't collision resistant. |
| `encrypt.public_key_server` | `--public-key-server` | `IMAP_BACKUP_PUBLIC_KEY_SERVER` | `keys.openpgp.org` | Keyserver host `public_key_fingerprint` is looked up on. Requires `public_key_fingerprint` to be set. |

Config-file validation only checks a fingerprint's *shape* at load time — it
never hits the network there, since validation runs on every command (even
ones that never encrypt anything). The keyserver is actually queried once,
when `backup` runs.

## Generating a key pair

Use the [`generate-key`](../commands/generate-key.md) command to create the
age or OpenPGP key pair used above.

## Decrypting a backup

Use the [`decrypt`](../commands/decrypt.md) command to read an encrypted
backup back with its private key.
