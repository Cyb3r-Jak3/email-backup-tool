$ErrorActionPreference = "Stop"

Remove-Item -Recurse -Force -ErrorAction SilentlyContinue completions
New-Item -ItemType Directory -Path completions | Out-Null

task build

foreach ($sh in "bash", "zsh", "fish") {
	& ./email-backup-tool completion $sh | Out-File -Encoding utf8 -FilePath "completions/email-backup-tool.$sh"
}