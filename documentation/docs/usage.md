# Installation & Usage

`email-backup-tool` is a single, statically linked binary with no runtime
dependencies. Pick whichever of the options below fits how you run things.

## GitHub Releases

Every [release](https://github.com/Cyb3r-Jak3/email-backup-tool/releases)
publishes prebuilt binaries for Linux, macOS, Windows, and FreeBSD, covering
`amd64`, `arm64`, `386`, `ppc64le`, `s390x`, and `riscv64`.

1. Download the archive matching your OS and architecture, e.g.
   `email-backup-tool_linux_amd64.tar.gz` (`.zip` on Windows).
2. Extract it and put the `email-backup-tool` binary on your `PATH`.
3. Verify it runs:

    ```bash
    ./email-backup-tool --version
    ```

Each release also publishes a `checksums.txt` and detached GPG signatures for
every artifact, so a download can be verified before you run it.

## Homebrew (macOS / Linux)

The tool is distributed as a cask from the project's own tap:

```bash
brew tap Cyb3r-Jak3/tap
brew install --cask email-backup-tool
```

Shell completions for bash, zsh, and fish are installed alongside the binary.

```bash
brew upgrade --cask email-backup-tool   # update to the latest release
```

## Docker

Images are published to both Docker Hub and GitHub Container Registry on
every release, built for `linux/amd64`, `linux/arm64`, `linux/386`,
`linux/ppc64le`, `linux/s390x`, and `linux/riscv64`:

```bash
docker pull ghcr.io/cyb3r-jak3/email-backup-tool:latest
# or
docker pull cyb3rjak3/email-backup-tool:latest
```

A specific version can be pulled instead of `latest` with a `vX.Y.Z` tag.

The image has no entrypoint script and no default config or backup location,
so mount them in at run time. For example, with a config file at
`./config.yaml` and backups written to `./backups`:

```bash
docker run --rm \
  -v "$(pwd)/config.yaml:/config.yaml:ro" \
  -v "$(pwd)/backups:/backups" \
  ghcr.io/cyb3r-jak3/email-backup-tool:latest \
  --config /config.yaml backup --all --attachments
```

Point `save.local.directory` in the config file at `/backups` (the path
inside the container) so writes land in the mounted volume. To run the tool
on a schedule, drive this same command from your host's cron, a Kubernetes
`CronJob`, or similar — the image itself does not schedule anything.

## Next steps

Once the binary is installed, see [Commands](commands/index.md) for how to
create a config file (`init`), generate an encryption key
(`generate-key`), and run a backup — or jump straight to
[Stages](stages/index.md) for the full list of config options.
