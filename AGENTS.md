# AGENTS.md

Guidance for AI coding agents and for people working in this repository. Humans: [CONTRIBUTING.md](CONTRIBUTING.md) and [docs/development.md](docs/development.md) say the same things with more words.

## What this is

`weread-exporter` exports a book of WeChat Read (微信读书, weread.qq.com) to EPUB, PDF, MOBI, TXT and Markdown. WeRead draws the text on a `<canvas>`, so the tool drives Chrome (chromedp), injects a script that hooks `fillText`, turns the drawing calls into Markdown, caches the chapters on disk and builds the e-books from the cache. It is a Go rewrite of the Python tool [drunkdream/weread-exporter](https://github.com/drunkdream/weread-exporter) and keeps its cache layout and Markdown rules.

Single binary, `CGO_ENABLED=0`, Go 1.26+ (see `go.mod`), module `github.com/jtsang4/weread-exporter-go`, releases for Linux/macOS/Windows on amd64 and arm64. Read [docs/architecture.md](docs/architecture.md) before changing the capture, session or reader mode code.

## Commands

```bash
make build                  # bin/weread-exporter
make check                  # gofmt -s, go mod tidy, go vet, golangci-lint, notices, go test -race ./...   (run before every commit)
make test-short             # no browser needed, seconds
make test                   # all tests; browser tests need Chrome/Chromium/Edge
make cross                  # the six release platforms into dist/
make tidy notices           # after changing dependencies: go.mod/go.sum and THIRD_PARTY_NOTICES.md
make snapshot               # complete GoReleaser build without publishing
make run ARGS="info -b 34336681"
go test ./internal/app -run TestRunWithCustomCSS -v -count=1      # one test
```

Tools: golangci-lint v2 (version pinned in `.github/workflows/ci.yml`), optional GoReleaser v2, actionlint, ShellCheck, EPUBCheck (run it before changing `internal/export/epub.go`).

## Repository map

| Path | What is there |
| --- | --- |
| `cmd/weread-exporter` | `main` only |
| `internal/cli` | cobra commands and flags (`flags.go`), logging, exit statuses (`cli.go`), signals |
| `internal/app` | `Config`, `Exporter.Run`, capture loop with retries (`capture.go`), sessions (`session.go`), reader mode (`readermode.go`), outputs (`outputs.go`), typed errors (`errors.go`) |
| `internal/browser` | chromedp: launch, hook injection, `CaptureChapter`, QR login, cookies, request rules, PDF printing, Chrome discovery |
| `internal/weread` | the HTTP side: client, `ParseRef`, `Hash` (id hash), book detail and book list parsing, cookies, reader mode, renewal |
| `internal/book` | `Meta`, `Chapter`, the cache `Store`, file names, image sniffing |
| `internal/markdown` | `Normalize`, image localisation, Markdown to (X)HTML |
| `internal/export` | EPUB, PDF HTML, TXT/Markdown, MOBI conversion |
| `internal/assets` | embedded `hook.js`, `stealth.js`, `headless_check.js`, `epub.css`, `pdf.css` |
| `internal/fsutil` | atomic writes |
| `internal/fakesite` | a fake WeRead (httptest server) for the tests |
| `internal/version` | build information set through linker flags |
| `testdata` | golden files produced by the Python original, and their generators |
| `docs` | architecture, development and troubleshooting guides |
| `scripts` | `notices.sh` (writes `THIRD_PARTY_NOTICES.md`), `completions.sh` (used by GoReleaser) |
| `.github`, `.goreleaser.yaml` | CI, release and CodeQL workflows, dependabot, issue forms, the release pipeline |

## Invariants: do not break these

1. **Credentials stay local.** `cache/cookie.txt` (and any Chrome profile kept with `--user-data-dir`) is the user's WeRead login. `/cache/` is git-ignored; never print the contents, put cookie values in logs, errors, issues, tests or commits, and do not copy them into fixtures. `cookie.txt` is written with mode 0600. If you find a login on the machine you work on, use it only for the small live checks below and do not read it out.
2. **The reader mode is restored.** The mode is an *account* setting on WeRead's server, and the capture only works in the vertical (scrolling) one. Whatever switches it must write `cache/reader-mode-restore.json` first and restore it on every exit path (normal end, error, Ctrl-C, and at the start of the next run if the process was killed). If you change anything near `ensureVertical` / `restoreReaderMode`, add a test for the failure path. After any live check, run `weread-exporter reader-mode`: it must say what it said before.
3. **Be gentle with the real site.** WeRead may restrict accounts that read fast. No test, CI job or script may talk to weread.qq.com; live checks are manual, small (one or two chapters, free ones), never looped, and use the defaults or slower (`--load-interval`). Do not lower the defaults (30 s between chapters). The fake site covers everything else.
4. **The cache layout is a compatibility promise** with the Python tool: `cache/<hashed book id>/{meta.json,cover.jpg,chapters/<position>-<uid>.md[.bak],images/}`, `cache/cookie.txt` as JSON `{"name": "value"}`; a chapter counts as captured when its `.md` is larger than 3 bytes; `.bak` marks a processed chapter.
5. **Writes are atomic.** Output and cache files are written to a temporary file and renamed (`fsutil`); an interrupted run never leaves a half-written book. Existing outputs are kept unless `--overwrite`.
6. **stdout is for results, stderr is for logs.** stdout carries only the paths of written files, tables and JSON, so that it can be piped. Exit statuses: 0 ok, 1 failed, 2 usage/configuration, 3 login needed, 130 interrupted.
7. **Nothing waits forever or retries forever.** Every blocking call takes a `context.Context`; retries are bounded (3 attempts per chapter, 3 browser restarts per book, 2 login rounds, 1 reader mode round); cleanup after a cancellation runs on a context without the cancellation (`detached`) with its own timeout. Goroutines end when the context ends; run `go test -race`.
8. **Python-compatible text.** The id hash and `markdown.Normalize` are pinned by golden files made by the original code. Do not "improve" them without regenerating the files and saying so in the commit message.
9. **Pure Go.** The build must work with `CGO_ENABLED=0` for all six platforms; think twice before adding a dependency, and run `make tidy notices` when you do (the notices ship with every binary because BSD and Apache licenses require it).

## Conventions

- `gofmt -s`; imports in three groups, the module's own last (`goimports -local github.com/jtsang4/weread-exporter-go`, enforced by golangci-lint).
- Errors: wrap with `%w`; lower-case messages without a final period; messages for users say what to do next. Typed/sentinel errors live in `internal/app/errors.go` and `internal/browser/errors.go`; `internal/cli` maps them to exit statuses.
- Logging: `log/slog` with the logger passed in (no globals). Never log secrets.
- Tests: table-driven, `t.TempDir()`, no writes outside it, no network except the fake site on loopback. A test that starts Chrome calls `requireChrome(t)`.
- Comments explain *why*; exported identifiers have doc comments.
- Commits follow [Conventional Commits](https://www.conventionalcommits.org/): `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `chore`, `ci`. The release notes are generated from them (`feat`, `fix` and `perf` get their own sections, `refactor` and others land under "Other changes", `docs`, `test`, `chore`, `ci` and `style` are left out). One logical change per commit.
- User-visible changes update, in the same commit: `README.md` (中文) and `README.en.md` (they must say the same), the flag tables against `weread-exporter --help`, `docs/` if relevant, `CHANGELOG.md` under `[Unreleased]`, and this file if an invariant or a command changed.

## Testing

| Layer | Needs |
| --- | --- |
| unit and golden-file tests (every package) | nothing |
| `internal/app` tests (stub browser, fake site) | nothing |
| `internal/browser` tests, `internal/cli/e2e_test.go` (real headless Chrome against `internal/fakesite`) | Chrome; skipped with `-short` or without Chrome |
| `internal/export/epubcheck_test.go` (W3C EPUBCheck on generated EPUBs, warnings fail) | `epubcheck` on the `PATH`; skipped otherwise and with `-short`; CI does not run it |

`WEREAD_TEST_REQUIRE_CHROME=1` makes a missing Chrome a failure instead of a skip (CI sets it). Leftover test browsers have `chromedp-runner` in their command line (`pkill -f chromedp-runner`); never kill all Chrome processes: the maintainer may be using one.

When the site, a flag, or the capture changes: extend `internal/fakesite`, write the test against it, then (only if needed) do a tiny live check as described in [docs/development.md](docs/development.md).

Regenerate golden files only on purpose: `python3 testdata/generators/wr_hash.py > testdata/wr_hash_golden.json`, `python3 testdata/generators/normalize.py > testdata/normalize_golden.json`.

## Release

Tag `vX.Y.Z` on `main` and push the tag: `.github/workflows/release.yml` runs GoReleaser (`.goreleaser.yaml`): archives, `.deb`/`.rpm`, `checksums.txt`, GitHub release notes, provenance attestation. Move the `[Unreleased]` entries of `CHANGELOG.md` under the new version first. Check locally with `make snapshot`. CI is `.github/workflows/ci.yml` (lint on the minimum Go of `go.mod`, tests on Linux/macOS/Windows and builds with the newest Go, a test job on the minimum Go, GoReleaser check, govulncheck) and `codeql.yml`; golangci-lint cannot read the standard library of a Go release newer than its own build, so the lint job stays on the minimum. Do not push tags or branches unless the maintainer asked for it.

## Gotchas

- `--chapters` and the `info` table use **1-based positions**, not WeRead's chapter UIDs (the numbers in the cache file names after the dash).
- The reader's footer button decides what happens next: `下一页` next page, `下一章` chapter complete, `登录…` login wanted, anything else means the chapter is not readable (paywall: a locked chapter shows a button such as `会员卡 19元/月 · 开通后即可阅读`, next to `下一章`). The last chapter of a book has no button; the capture waits `FooterTimeout` (60 s) and takes that as the end.
- `wr_skey` (the session cookie) lives about an hour; the tool renews it every ~20 minutes. Renewal is `POST /web/login/renewal`; the reader mode is `POST /web/user_config/modify`.
- The theme cookie `wr_theme=dark` is set in the *browser* for the hook's colour thresholds and must never be saved to the account or the cookie file.
- Headless Chrome cannot show the login QR code: `login` needs a window, exports can be headless afterwards.
- Two exports for one account at the same time fight over the reader mode.
- Not every platform can be tried locally: `make cross` and `GOOS=windows go vet ./...` catch compile errors, the CI matrix runs the tests on Linux, macOS and Windows. Keep paths portable (`filepath`, never hard-coded `/`), and remember that Windows has no POSIX file modes and reserves some file names (`internal/book/filename.go`).
