# Contributing

This project is a Go CLI. See [CLAUDE.md](CLAUDE.md) for the architecture overview (the three
pipeline stages, config resolution, encryption, incremental backups) — read that first if you're
not familiar with the codebase.

## Getting started

```bash
go build ./...
go test ./...
go vet ./...
gofmt -w cmd stages   # main.go has CRLF endings; gofmt -l always flags it, leave it alone
```

Running the tool needs a config file — see `config.example.yaml` and `example.yaml` (every
supported key, annotated).

## Adding a mail source, encryption scheme, or storage backend

Write the implementation in its subpackage (`stages/fetch`, `stages/transform`, `stages/store`)
against the `Fetcher`/`Transformer`/`Sink` interface in [stages/stages.go](stages/stages.go), then
wire it in as the chosen implementation in
[cmd/build_stages.go](cmd/build_stages.go) — that file is the *only* place flags and config become
stages. `stages/*` never imports `cmd`; builders take plain option structs, never a `cli.Command`
or config type.

## Adding a new config option (and its flag)

A config value in this project flows through several places that all have to agree. Use an
existing option as a template — e.g. `save.s3.prefix` / `--s3-prefix` — and follow the same
pattern for a new one:

1. **Add the field to the config struct** in [cmd/configfile.go](cmd/configfile.go) (`ConfigV1`
   or one of its nested structs, e.g. `IMAPLoginV1`, `EncryptV1`, `SaveS3V1`). Give it a `yaml`
   tag and matching `json` tag, and schema tags as needed:
   - `desc:"..."` — required, becomes the JSON Schema description.
   - `enum:"A,B,C"`, `pattern:"..."`, `min:"N"`, `max:"N"`, `default:"..."` — optional, mirrored
     into the generated schema.
   - `omitempty` on the yaml tag for anything not always present; add `required:"true"` on the
     tag if a field must be set even though it isn't a pointer to a struct.

2. **Add a logical key** in [cmd/config_keys.go](cmd/config_keys.go), e.g.
   `KeySaveS3Prefix = "save.s3.prefix"`. This is the vocabulary shared between flags and every
   config schema version — it must stay independent of the exact YAML layout so a future
   `ConfigV2` can rename or relocate the underlying field without changing any flag.

3. **Resolve the key** in `(*ConfigV1) Lookup` in the same file: add a `case` that reads the new
   field off the struct and returns `(value, true)` only when it's actually set (empty string is
   a lookup *miss*, not an empty override — see the `nonEmpty` helper). Reuse the existing
   `imapString`/`encryptString` helpers, or the equivalent, for fields nested under a pointer
   struct.

4. **Add the CLI flag** in [cmd/flags.go](cmd/flags.go) (or inline in the command file if it's
   specific to one command, e.g. [cmd/backup.go](cmd/backup.go)). Give it:
   - `Sources: flagSources(KeyYourNewKey, "ENV_VAR_NAME")` — this wires up the
     command line > environment > YAML file precedence automatically.
   - A `Usage` string and, if the flag takes a bounded value, a `Validator`.

5. **Validate cross-field constraints** (if any) in `(*ConfigV1) Validate()` in
   [cmd/configfile.go](cmd/configfile.go) — e.g. "required if sibling field is set", format
   checks that can't be expressed as a schema `pattern`. `Validate` runs on every command via the
   root `Before` hook, so avoid anything that needs network access or is expensive; do that work
   lazily where the value is actually consumed (see `buildTransformer`'s keyserver fetch for the
   pattern).

6. **Regenerate the JSON Schema**:

   ```bash
   go generate ./cmd/...
   ```

   This rewrites [config.schema.json](config.schema.json) from `ConfigV1`'s struct tags. Commit
   the regenerated file — `TestConfigSchemaUpToDate` in `cmd` fails the build if it's stale. If
   the new field needs a schema constraint that can't fall out of a single struct's own tags
   (e.g. "at most one of X, Y, Z"), add it to the small `schemaOverrides` map at the top of
   [cmd/schema.go](cmd/schema.go) instead.

7. **Document the option** in `example.yaml` (and `config.example.yaml` if it's a commonly-used
   one), so the annotated reference stays complete.

8. **Test flag precedence** in the relevant `cmd` test — build the real command, set `logger =
   zap.NewNop()`, and assert flag > env > config-file > default resolves the way you expect (see
   existing tests in `cmd` for the pattern).

### Adding a config schema version

If a change can't be expressed as an additive field on `ConfigV1` (e.g. renaming or restructuring
existing fields), add a `ConfigV2` implementing `VersionedConfig` and register it in
`newVersionedConfig` ([cmd/configfile.go](cmd/configfile.go)). Existing logical keys must still
resolve through `ConfigV2.Lookup` so flags don't need to change. Note `GenerateConfigSchema` only
covers `ConfigV1` today — a new version needs its own schema handling if one is added.

## Documentation

The user-facing docs site lives in [documentation/](documentation/) — a separate MkDocs
(Material theme) project, not the same thing as this file or CLAUDE.md. Build or preview it
locally with the `task` targets:

```bash
task docs          # build the static site into documentation/site
task docs-serve    # serve it locally at http://localhost:8000 with live reload (via Docker)
```

It's organized by nav section under `documentation/docs/`:

- `usage.md` — installing the binary (GitHub Releases, Homebrew, Docker).
- `stages/` — one page per pipeline stage (`fetch.md`, `encrypt.md`, `store.md`), each documenting
  that stage's config keys/flags/env vars alongside its behavior.
- `commands/` — one page per standalone command (`init.md`, `generate-key.md`, `decrypt.md`).

If a change adds or renames a config option, flag, or command, update the matching page —
`documentation/mkdocs.yml`'s `nav` list is what makes a new page show up in the sidebar, so add an
entry there too. Nothing currently fails the build if these drift from the code (unlike
`config.schema.json`, which `TestConfigSchemaUpToDate` enforces), so treat keeping them in sync as
part of the change, not a follow-up.

These docs are written for end users: describe behavior and config in plain terms, and don't
reference Go interfaces, internal type names, or source files the way CLAUDE.md does.

## Testing notes

- `stages/fetch` tests run against a real in-process IMAP server
  (`go-imap/v2/imapserver/imapmemserver`), not mocks — extend `startTestServer` in
  [stages/fetch/imap_test.go](stages/fetch/imap_test.go) rather than faking the IMAP client.
- Encryption changes need a genuine round trip *and* a format check with real `gpg`/`age` — a
  self-contained round trip can pass on a key real tooling rejects (see the `SerializePrivate` vs
  `SerializePrivateWithoutSigning` note in CLAUDE.md).
- The end-to-end suite in [e2e/](e2e/) drives the real CLI against GreenMail (IMAP+SMTP) and
  RustFS (S3-compatible) via Docker Compose. It's opt-in and needs Docker:

  ```bash
  go test -tags e2e ./e2e/...
  ```

## Gotchas

- Backups select mailboxes read-only and fetch with `Peek` — never introduce a code path that
  sets the seen flag on the user's mail.
- The S3 sink has no test coverage against a live service; only its construction is exercised.
