# Stage 1: Fetch

Stage 1 connects to a mail source and downloads messages. Currently the only
supported source is **IMAP**.

Mailboxes are opened **read-only**, so a backup run never marks the account's
mail as read. Messages are downloaded and saved one at a time, so even a very
large mailbox never has to fit in memory all at once.

## What it does

- Logs in to the server over `STARTTLS`, implicit TLS, or a plaintext connection.
- Lists every mailbox on the account (or just the one requested).
- Narrows each mailbox down to messages newer than the requested window (see
  [Incremental backups](#incremental-backups) below), then re-checks each
  message's exact timestamp so a run resuming mid-day doesn't skip or repeat
  the boundary message.
- Downloads the full content of every matching message.
- Optionally extracts attachments into their own files next to the message
  (`--attachments`).

## Config options

These map to the `login.imap` block of the config file.

| YAML key | CLI flag | Environment variable | Default | Description |
|---|---|---|---|---|
| `login.imap.host` + `login.imap.port` | `--server`, `-s` | `IMAP_SERVER` | — | IMAP server address, e.g. `imap.example.com:993`. Host and port combine into one `host:port` value. |
| `login.imap.username` | `--username`, `-u` | `IMAP_USERNAME` | — | IMAP account username. Required. |
| `login.imap.password` | `--password`, `-p` | `IMAP_PASSWORD` | — | IMAP account password. |
| `login.imap.tls` | `--tls`, `-t` | `IMAP_TLS` | `SSL` | TLS mode: `STARTTLS`, `SSL` (implicit TLS), or `NONE`. `TLS`/`IMPLICIT`/`SMARTTLS` are accepted as aliases. |
| `login.imap.ca_cert_file` | `--ca-cert` | `IMAP_CA_CERT` | — | Path to a CA certificate used to verify the server, in place of the system trust store. |
| `login.imap.ignore_server_cert` | `--insecure`, `-k` | `IMAP_INSECURE` | `false` | Skip server certificate verification entirely. |
| `login.imap.mailbox` | `--mailbox`, `-m` | `EMAIL_MAILBOX` | *(every mailbox)* | Back up only this mailbox instead of the whole account. |
| — | `--attachments` | — | `false` | Also save each attachment as its own file next to the message. |

`login.imap.host` and `login.imap.username` are required; the config file fails
validation at startup without them.

### Incremental backups

Every field below is a `backup`-only flag — there is no matching config key,
since the timestamp file is meant to be driven automatically.

| CLI flag | Default | Description |
|---|---|---|
| `--duration` | `24h` | How far back to look when there is no timestamp file. Also overrides the timestamp file when passed explicitly. |
| `--all` | `false` | Back up every message, ignoring the timestamp file and `--duration` entirely. |
| `--timestamp-file` | `email-backup-tool-watermark.json` | Path of the file recording the last backed up message's timestamp. |

Precedence for the fetch window is: `--all` → an explicitly passed `--duration`
→ the timestamp file → `--duration`'s default of 24h. The timestamp file is
only written **after a fully successful run**, so a run that fails part way
through retries the same window on its next attempt instead of skipping mail
it never stored.

## Try it

```bash
./imap-backup-tool --config test.yaml login                       # credential/TLS check only
./imap-backup-tool --config test.yaml backup --mailbox INBOX
./imap-backup-tool --config test.yaml backup --all --attachments
```
