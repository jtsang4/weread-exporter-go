# weread-exporter

[![CI](https://github.com/jtsang4/weread-exporter-go/actions/workflows/ci.yml/badge.svg)](https://github.com/jtsang4/weread-exporter-go/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/jtsang4/weread-exporter-go?sort=semver)](https://github.com/jtsang4/weread-exporter-go/releases)
[![Go Reference](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

[中文](README.md) | **English**

Export the books you are **allowed to read** on [WeChat Read](https://weread.qq.com/) (微信读书, WeRead) to **EPUB, PDF, MOBI, TXT and Markdown**.

This is a Go rewrite of [drunkdream/weread-exporter](https://github.com/drunkdream/weread-exporter) (Python): one static binary, no Python or pip, for Linux, macOS and Windows on amd64 and arm64.

## Features

- **One file**: download it from [Releases](https://github.com/jtsang4/weread-exporter-go/releases) and run it. At run time it only needs Chrome, Chromium or Edge.
- **Five output formats**: EPUB 3, PDF, MOBI, TXT and Markdown, several at once if you like.
- **Resumable**: every chapter is cached as soon as it is captured. An interrupted export carries on where it stopped, and a book that is cached completely is exported without starting a browser.
- **Sign in once, stay signed in**: scan a QR code with WeChat once; the login is stored locally and renewed during the export.
- **Reader mode handled for you**: the export needs the scrolling reader. The tool switches your account to it and puts the previous mode back when it finishes: also after a failure, after Ctrl-C, and (at the next run) after the process was killed.
- **Chapter selection**: `--chapters 1-3,7,10-`, with an `info` command that shows the table of contents and what is cached.
- **Book lists**: pass a book list id and every book is exported; one failing book does not stop the others.
- **Cache compatible with the Python tool**: the layout of `cache/<book id>/` and the format of `cookie.txt` are the same, so an existing cache keeps working.
- **Safe writes**: every output file is written under a temporary name and renamed when complete; files that exist are not overwritten unless you ask.

## Install

### Download a binary (recommended)

Download the archive for your system from [Releases](https://github.com/jtsang4/weread-exporter-go/releases) (`linux`, `darwin` (macOS) or `windows`, on `amd64` or `arm64`), unpack it and run `weread-exporter` (`weread-exporter.exe` on Windows). Verify the download against `checksums.txt` (SHA-256). `.deb` and `.rpm` packages are provided for Linux.

If macOS blocks the downloaded binary, run `xattr -d com.apple.quarantine weread-exporter`.

### With Go

```bash
go install github.com/jtsang4/weread-exporter-go/cmd/weread-exporter@latest
```

This needs Go 1.26 or later.

### From source

```bash
git clone https://github.com/jtsang4/weread-exporter-go.git
cd weread-exporter-go
make build        # writes bin/weread-exporter
```

### Requirements

| Needed | For |
| --- | --- |
| Google Chrome, Chromium or Microsoft Edge | Required. Reads the chapters in the reader and prints the PDF. It is found automatically; use `--chrome-path` or the `CHROME_PATH` environment variable to point to a specific one |
| [Calibre](https://calibre-ebook.com/)'s `ebook-convert`, or `kindlegen` | Only for MOBI output. Use `--mobi-converter` for another location |

## Quick start

```bash
# 1. Sign in (once): Chrome opens and shows a QR code; scan it with WeChat and confirm on the phone
weread-exporter login

# 2. Export a book; EPUB is the default, the file is written to ./output/
weread-exporter -b 34336681

# Several formats at once
weread-exporter -b 34336681 -o epub -o pdf -o md -o txt
```

### Finding the book id

`-b` / `--book-id` takes any of these:

- the URL of the book's detail page: `https://weread.qq.com/web/bookDetail/3300040626` (search for the book on <https://weread.qq.com/>, open it, copy the address)
- the id at the end of that URL (for example `08232ac0720befa90825d88`)
- the URL of a reader page: `https://weread.qq.com/web/reader/...`
- a numeric id (for example `34336681`; it is converted to the id used in URLs)
- a book list id (contains `_`, for example `352003193_7T4LYH9ib`) or the URL of a book list page `https://weread.qq.com/misc/booklist/352003193_7T4LYH9ib`: every book of the list is exported

## Usage

```text
weread-exporter [flags]                  export a book (or a book list)
weread-exporter login                    sign in with the QR code and save the login
weread-exporter logout                   delete the saved login
weread-exporter info -b BOOK             show the table of contents and the cache state (no login, no export)
weread-exporter reader-mode [status|vertical|horizontal|restore]
                                         show or change the reader mode of the account
weread-exporter version                  version information
weread-exporter completion <shell>       shell completion script for bash, zsh, fish or powershell
```

The common flags of the export command (`weread-exporter --help` lists all of them, every subcommand has its own `--help`):

| Flag | Meaning |
| --- | --- |
| `-b, --book-id` | the book to export, see above |
| `-o, --output-format` | `epub`, `pdf`, `mobi`, `txt`, `md`; repeat it for several; default `epub` |
| `--chapters` | export only some chapters, e.g. `1-3,7,10-`; the numbers are the ones `info` lists |
| `--output-dir` | where the files go, default `output` |
| `--cache-dir` | login and captured chapters, default `cache` |
| `--overwrite` | replace output files that exist (they are kept by default) |
| `--css-file` | CSS appended to the built-in style sheet of EPUB and PDF |
| `--load-interval` | pause between two chapters, default `30s` (see [Responsible use](#responsible-use)) |
| `--load-timeout` | how long one chapter page may take to load, default `60s` |
| `--headless` | no browser window (needs a login saved with `login`) |
| `--no-sandbox` | start Chrome without its sandbox (containers, running as root) |
| `--chrome-path` | the browser executable |
| `--user-data-dir` | keep Chrome's profile in this directory |
| `--force-login` | ignore the saved login and sign in again |
| `--keep-reader-mode` | never switch the account's reader mode (fails if the account uses the paged reader) |
| `--mobi-converter` | program that converts EPUB to MOBI |
| `--proxy-server` | proxy for the browser and the HTTP requests |
| `--log-format` | `text` or `json`; the log goes to the standard error |
| `-v, --verbose` | log debugging details |
| `-y, --yes` | answer questions with yes (for example to carry on with a browser that looks automated) |

`--load-interval` and `--load-timeout` take seconds (`30`) or a duration (`500ms`, `2m`).

### Examples

```bash
# look at the table of contents, then export the first five chapters only
weread-exporter info -b 34336681
weread-exporter -b 34336681 --chapters 1-5

# the table of contents as JSON
weread-exporter info -b 34336681 --json

# every book of a book list
weread-exporter -b 352003193_7T4LYH9ib -o epub

# on a server where you are signed in already: no window
weread-exporter -b 34336681 --headless --no-sandbox

# MOBI is converted from EPUB, so the EPUB is written as well
weread-exporter -b 34336681 -o mobi
```

### Output

- Files go to `--output-dir` and are named after the book: `<title>.epub`, `<title>.pdf`, `<title>.mobi`, `<title>.txt`, `<title>.md`.
- A partial export says so in the name, `<title> (chapters 1-5).epub`, so it is not taken for the whole book.
- The Markdown output is the whole book in one file; its images are in `<title>.assets/` next to it.
- The **standard output** lists the files that were written, one per line; the log and the progress go to the **standard error**, so pipes are safe: `weread-exporter -b ... | xargs ls -l`.
- Exit status: `0` success; `1` failure; `2` wrong command line or configuration; `3` a login is needed and could not be established; `130` interrupted with Ctrl-C.

## How it works

The web reader of WeRead draws the text on a `<canvas>`; the page has no text to copy. The tool controls Chrome through the DevTools Protocol and injects a script before the reader loads that **hooks the canvas drawing functions**. It collects every text, font, colour and image that is drawn, rebuilds Markdown from it and caches that on disk. The e-books are made from the cached Markdown. This is the idea of the Python original; see [docs/architecture.md](docs/architecture.md) for the details.

So:

- You can only export **what your account can read in the web reader**: free chapters, books you bought, books your reading membership covers. A chapter that cannot be read (a paid chapter without membership) stops the export with a hint to use `--chapters`; the chapters captured before it stay in the cache.
- Capturing has to scroll through every chapter, so it is **slow**. That is on purpose, see below.

### Reader mode

The web reader has two layouts: the **paged** one (the default of new accounts) and the **scrolling** one. Only the scrolling layout can be captured, and the mode is a setting of the **account** (stored on the server), not of the browser. So an export switches your account to scrolling when it starts and switches back as soon as the capture is done, whatever happens. If the process is killed before it could do that, a marker file in the cache directory makes the next run (or `weread-exporter reader-mode restore`) restore the mode.

If you do not want the tool to touch your settings, choose "scrolling" in the web reader yourself and add `--keep-reader-mode`.

### Cache

```text
cache/
├── cookie.txt                         the login cookies (sensitive!)
├── reader-mode-restore.json           exists only while a switched reader mode is not restored yet
├── diagnostics/                       pages (HTML and screenshot) saved when a capture failed; check them before you share them
└── <book id>/
    ├── meta.json                      title, author, introduction, table of contents
    ├── cover.jpg
    ├── chapters/<n>-<chapter uid>.md  Markdown of each chapter (.md.bak is the raw capture before processing)
    └── images/                        the images of the chapters
```

The cache is interchangeable with the Python original. To capture a chapter again, delete its `.md` file; to start a book over, delete `cache/<book id>/` (your login is not affected).

## Troubleshooting

See [docs/troubleshooting.md](docs/troubleshooting.md): Chrome not found, login time-outs, servers without a display, a reader mode that was not restored, chapters that cannot be read, restricted accounts, and more.

## Differences from the Python tool

| | Python | Go |
| --- | --- | --- |
| Distribution | `pip install` and dependencies | one static binary, prebuilt for every platform |
| Command | `python -m weread_exporter -b ID -o epub` | `weread-exporter -b ID -o epub` (the flags are mostly the same) |
| Formats | epub / pdf / mobi / txt | plus Markdown |
| Reader mode | you switch to scrolling yourself | switched and restored automatically |
| Everything cached | still opens a browser | no browser |
| Some chapters | not supported | `--chapters` |
| Subcommands | none | `login`, `logout`, `info`, `reader-mode`, `version`, `completion` |
| Output files | written in place | atomic; existing ones are kept |
| Cache directory | `cache/` | compatible, can be shared |
| Browser | its own Chrome launcher and proxy | the DevTools Protocol directly (chromedp) |

## Responsible use

- The tool reads what **your own account is allowed to read** and does **not** get around paywalls or membership limits. Keep the exported books for your own study, backup and reading on your own devices: **do not share them**, and respect the rights of authors and publishers.
- The terms of service of WeRead may forbid automated access, and **your account may be restricted. You use the tool at your own risk.**
- Capturing a chapter waits for the page to scroll through it, and the default pause between two chapters is 30 seconds so that the pace looks like reading. **Do not lower `--load-interval`**, and never run several exports for the same account at the same time.
- The login (`cache/cookie.txt`) is as sensitive as your password: do not commit it, share it or paste it into an issue. The tool stores it in a file only you can read and write, and the log never shows the values of cookies.
- This project is not affiliated with Tencent or WeRead.

## Contributing

```bash
make check        # gofmt, go vet, golangci-lint, go test -race
make cross        # cross-compile for the six platforms into dist/
make help         # every task
```

Please read [CONTRIBUTING.md](CONTRIBUTING.md) and [docs/development.md](docs/development.md) first; notes for AI coding agents are in [AGENTS.md](AGENTS.md). Report security problems privately as described in [SECURITY.md](SECURITY.md).

## Credits and license

- The original project and the idea: [drunkdream/weread-exporter](https://github.com/drunkdream/weread-exporter) (MIT).
- Released under the [MIT License](LICENSE).
