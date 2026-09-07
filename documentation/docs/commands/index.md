# Commands

Besides `backup` and `login` (covered in [Stages](../stages/index.md)), a few
standalone commands round out the workflow around a backup:

| Command | Purpose |
|---|---|
| [`init`](init.md) | Create a template config file to fill in. |
| [`generate-key`](generate-key.md) | Create an age or OpenPGP key pair for encrypting backups. |
| [`decrypt`](decrypt.md) | Decrypt files a backup wrote. |

A typical first-time setup uses them in this order:

```bash
./imap-backup-tool init                    # 1. create a config file
./imap-backup-tool generate-key            # 2. create a key pair to encrypt to
./imap-backup-tool --config config.yaml backup --all --attachments
```
