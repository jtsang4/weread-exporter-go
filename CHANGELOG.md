# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/). The GitHub release notes of every tag are generated from the commit messages ([Conventional Commits](https://www.conventionalcommits.org/)); this file is the curated version.

## [Unreleased]

### Security

- Update `golang.org/x/net` to v0.62.0 to fix quadratic-time HTML parsing ([GO-2026-6860](https://pkg.go.dev/vuln/GO-2026-6860)).

## [0.3.1] - 2026-10-22

### Changed

- The GitHub Actions the pipeline uses are updated (checkout v7, setup-go v7, golangci-lint-action v9, upload-artifact v7, GoReleaser action v7, attest-build-provenance v4). Nothing in the program itself changes; this release exists so the refreshed release pipeline is exercised by a real tag before the next feature release.

## [0.3.0] - 2026-10-22

First release of the Go rewrite of [drunkdream/weread-exporter](https://github.com/drunkdream/weread-exporter).

### Added

- Export of a WeRead book (or a book list) to **EPUB 3, PDF, MOBI, TXT and Markdown**; several formats per run with `-o`.
- EPUB 3 packages that pass [EPUBCheck](https://www.w3.org/publishing/epubcheck/) without warnings; a picture that could not be downloaded is linked in the text (`[图片]`) because an EPUB may not embed images from the web.
- Tag-like book text (`if x<y and z>w`) is kept as text in EPUB, PDF and TXT; only the footnote markers written by the capture hook are markup.
- Capture of the chapter text through a hook on the canvas of the web reader, driven by Chrome, Chromium or Edge over the DevTools Protocol (chromedp).
- Cache layout and `cookie.txt` format compatible with the Python tool; a book that is cached completely is exported without starting a browser.
- Commands `login` (QR code), `logout`, `info` (table of contents and cache state, `--json`), `reader-mode` (`status`, `vertical`, `horizontal`, `restore`), `version` and `completion`.
- Automatic switch of the account's reader mode to the scrolling reader and a restore that also survives failures, interrupts and killed processes.
- Session renewal while exporting, so long exports do not run into an expired login.
- `--chapters` to export a part of a book; partial exports carry the chapters in the file name.
- Bounded retries per chapter and per browser, a login or reader mode problem in the middle of a book is repaired and the export goes on.
- Atomic writes of every output file; existing outputs are kept unless `--overwrite` is given.
- `--headless`, `--no-sandbox`, `--user-data-dir`, `--proxy-server`, `--css-file`, `--mobi-converter`, `--log-format json` and more; see `--help`.
- Releases for Linux, macOS and Windows on amd64 and arm64 (archives, checksums, `.deb` and `.rpm` packages) built by GoReleaser in GitHub Actions; CI with lint (ShellCheck and actionlint included), tests on three operating systems, EPUBCheck validation of the generated EPUBs, cross-compilation, CodeQL, govulncheck and weekly fuzzing of the parsers.
- Shell completions (bash, zsh, fish, PowerShell) and the third-party license notices in every archive; the packages install them.
- Documentation in Chinese and English, notes for contributors and for AI coding agents.

### Fixed

- Chrome on Windows: the version is still detected when another Chrome is already running (`chrome --version` then only prints "Opening in existing browser session."), so `--mock-user-agent` and the startup log no longer report version 0.
- A capture that runs into its deadline is reported as a timeout on Windows as well, instead of a plain "context canceled".

### Changed

- Compared with the Python tool: Markdown output, partial exports and automatic reader mode handling are new; the table is in the README.
