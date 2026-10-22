#!/bin/sh
# Writes the shell completion scripts into completions/. GoReleaser runs this
# before it builds (see .goreleaser.yaml) and puts them into the release archives
# and the .deb and .rpm packages.
#
#   sh scripts/completions.sh
set -eu

cd "$(dirname "$0")/.."
rm -rf completions
mkdir completions
for shell in bash zsh fish powershell; do
	go run ./cmd/weread-exporter completion "$shell" >"completions/weread-exporter.$shell"
done
