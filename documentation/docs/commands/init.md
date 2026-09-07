# init

Creates a template config file with placeholder values, so you have something
to edit instead of starting from a blank file.

```bash
./imap-backup-tool init
```

## Flags

| CLI flag | Default | Description |
|---|---|---|
| `--output`, `-o` | `imap-backup-tool.yaml` (or `--config` / `IMAP_BACKUP_TOOL_CONFIG` if set) | Path to write the template config file to. |
| `--force` | `false` | Overwrite an existing config file at that path. |

## After running

Open the written file and fill in your IMAP server, credentials, and where to
save the backup — see [Stages](../stages/index.md) for every available
config option. Then generate an encryption key with
[`generate-key`](generate-key.md) if you want the backup encrypted.
