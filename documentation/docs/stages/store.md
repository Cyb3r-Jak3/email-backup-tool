# Stage 3: Store

Stage 3 writes the downloaded (and optionally encrypted) mail somewhere
permanent. Every destination you configure is used at once — set a `save`
block with several backends (or pass several store flags) and the backup
writes to all of them.

A storage destination is always safely finalized once a run finishes, whether
it succeeded or not, so a tar archive is never left without a valid trailer
even after a failed run.

## Destinations

| Destination | Description |
|---|---|
| **Local directory** | Writes each message and attachment as a file under a directory, organized by mailbox and date. |
| **S3-compatible bucket** | Uploads each message and attachment as an object. Works with AWS and any S3-compatible service (MinIO, Ceph, Backblaze B2, …). |
| **Tar archive** | Writes each message and attachment as an entry in a single tar archive, gzip-compressed when the path ends in `.gz` or `.tgz`. |

A destination is considered active as soon as it has a value from any source
(CLI flag, environment variable, or the config file) — so listing several
backends under `save` produces a run that writes everywhere at once.

## Config options

### Local directory — `save.local`

| YAML key | CLI flag | Environment variable | Description |
|---|---|---|---|
| `save.local.directory` | `--directory`, `-d` | `IMAP_BACKUP_DIRECTORY` | Directory the backup is written to. Created if it does not exist. |

### S3-compatible bucket — `save.s3`

| YAML key | CLI flag | Environment variable | Description |
|---|---|---|---|
| `save.s3.bucket` | `--s3-bucket` | `IMAP_BACKUP_S3_BUCKET` | Destination bucket name. Required to activate this destination. |
| `save.s3.region` | `--s3-region` | `IMAP_BACKUP_S3_REGION`, `AWS_REGION` | Bucket's region. Defaults to `us-east-1` when unset (most compatible services ignore it). |
| `save.s3.access_key_id` | `--s3-access-key-id` | `IMAP_BACKUP_S3_ACCESS_KEY_ID` | Static access key ID. Omit, along with the secret key, to use the ambient AWS configuration (environment, shared config, instance role). |
| `save.s3.secret_access_key` | `--s3-secret-access-key` | `IMAP_BACKUP_S3_SECRET_ACCESS_KEY` | Static secret access key. Must be set together with `access_key_id`, or not at all. |
| `save.s3.endpoint` | `--s3-endpoint` | `IMAP_BACKUP_S3_ENDPOINT` | URL of an S3-compatible service to use instead of AWS. Forces path-style bucket addressing, which is what most compatible services need. |
| `save.s3.prefix` | `--s3-prefix` | `IMAP_BACKUP_S3_PREFIX` | Key prefix placed in front of every uploaded object. |

### Tar archive — `save.tar`

| YAML key | CLI flag | Environment variable | Description |
|---|---|---|---|
| `save.tar.path` | `--tar` | `IMAP_BACKUP_TAR` | Path of the archive to write. Gzip-compressed automatically when the path ends in `.gz` or `.tgz`. Truncated if it already exists — one run produces one archive. |

## File layout

Every destination stores mail under the same path structure:

```
<mailbox>/<year>/<month>/<timestamp>-<uid>-<subject-slug>.eml
<mailbox>/<year>/<month>/<timestamp>-<uid>-<subject-slug>.attachments/<filename>
```

organized by mailbox and month so a long-running backup does not pile millions
of entries into one directory or key prefix. If [encryption](encrypt.md) is
enabled, its suffix (`.age` or `.pgp`) is appended to the end of that path.
