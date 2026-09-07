#!/bin/sh
set -e
rm -rf completions
mkdir completions
task build
for sh in bash zsh fish; do
	./email-backup-tool completion "$sh" >"completions/email-backup-tool.$sh"
done