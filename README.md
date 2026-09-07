# Email Backup Tool

![GitHub Release](https://img.shields.io/github/v/release/Cyb3r-Jak3/email-backup-tool)
![GitHub Downloads (all assets, latest release)](https://img.shields.io/github/downloads/Cyb3r-Jak3/email-backup-tool/latest/total)
[![Dev](https://github.com/Cyb3r-Jak3/email-backup-tool/actions/workflows/dev.yaml/badge.svg?event=push)](https://github.com/Cyb3r-Jak3/email-backup-tool/actions/workflows/dev.yaml)

A command-line tool that backs up your email from an IMAP server, optionally encrypts it, and
stores it locally, in an S3-compatible bucket, or as a tar archive. Supports incremental backups,
attachments, and multiple mailboxes, and can encrypt backups with age or OpenPGP so a copy stays
readable with standard tools even without this project.

```bash
email-backup-tool init                                       # create a config file
email-backup-tool generate-key                                # create an encryption key pair
email-backup-tool --config imap-backup-tool.yaml backup --all --attachments
```

## Documentation

Full documentation — installation (GitHub Releases, Homebrew, Docker), configuration, and
command reference — is at **[email-backup-tool.cyberjake.xyz](https://email-backup-tool.cyberjake.xyz)**.
