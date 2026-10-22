# Development guide

How to build, test, debug and release weread-exporter. Read [architecture.md](architecture.md) first for the big picture.

## Setup

You need

- Go 1.26 or later (`go.mod` says which; CI also runs everything with the newest Go release),
- Chrome, Chromium or Edge (the browser tests and the PDF output),
- `make`,
- [golangci-lint](https://golangci-lint.run/) v2 (`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0`; the version CI uses is in `.github/workflows/ci.yml`),
- optional: [GoReleaser](https://goreleaser.com/) v2 and [actionlint](https://github.com/rhysd/actionlint) to check the release and workflow files, [ShellCheck](https://www.shellcheck.net/) for `scripts/*.sh`, and [EPUBCheck](https://www.w3.org/publishing/epubcheck/) to validate EPUB files.

```bash
git clone https://github.com/jtsang4/weread-exporter-go.git
cd weread-exporter-go
make build && ./bin/weread-exporter --help
```

## Everyday commands

```bash
make help          # every task
make check         # what CI checks: gofmt, go mod tidy, vet, golangci-lint, notices, go test -race ./...
make test          # go test ./...          (browser tests are skipped without Chrome)
make test-short    # go test -short ./...   (no browser at all)
make cover         # coverage.txt and a summary
make notices       # regenerate THIRD_PARTY_NOTICES.md after the dependencies changed
make completions   # shell completion scripts into completions/ (GoReleaser does this itself)
make cross         # the six release platforms into dist/
make snapshot      # a complete GoReleaser build without publishing
make run ARGS="info -b 34336681"
```

Run one test: `go test ./internal/app -run TestRunWithCustomCSS -v -count=1`.

## Tests

| Layer | Where | Needs |
| --- | --- | --- |
| Unit tests | every package | nothing |
| Golden files | `internal/weread` (id hash), `internal/markdown` (normalisation) | nothing; the expected values come from the Python implementation |
| App tests | `internal/app` | the fake site and a stub browser; no Chrome |
| Browser tests | `internal/browser` | a real headless Chrome against the fake site |
| End-to-end tests | `internal/cli/e2e_test.go` | a real headless Chrome; the whole program from the command line to EPUB, PDF, TXT and Markdown files |

- Browser and end-to-end tests are skipped with `-short` and when Chrome is missing. `WEREAD_TEST_REQUIRE_CHROME=1` turns a missing Chrome into a failure (CI sets it), so the tests cannot be skipped by accident.
- Tests never talk to weread.qq.com. `internal/fakesite` is an `httptest` server that imitates what the program uses: book pages with `window.__INITIAL_STATE__`, reader pages that draw text onto a canvas and have a footer button, a book list, login cookies and renewal, the reader mode setting, telemetry endpoints (it records what leaks to it). Extend it when you teach the program about a new part of the site.
- Always use `t.TempDir()`; never write below the repository directory.
- A test that starts Chrome should use `NoSandbox: true` and a short timeout; look at `testOptions` in `internal/browser/integration_test.go`.
- If a test run is killed, Chrome processes may be left behind; they run with a temporary `--user-data-dir` named `chromedp-runner…`. Kill exactly those (`pkill -f chromedp-runner`), not every Chrome.

### Golden files

`testdata/wr_hash_golden.json` and `testdata/normalize_golden.json` were produced by the original Python code (copied verbatim into `testdata/generators/`):

```bash
python3 testdata/generators/wr_hash.py       > testdata/wr_hash_golden.json
python3 testdata/generators/normalize.py     > testdata/normalize_golden.json
```

Change them only if the Python behaviour is meant to be left behind, and say so in the commit message.

## Checking against the real site

Automated tests cannot tell you whether WeRead changed its pages. When you touch the capture, login or reader mode code, try it for real, **with low volume**:

1. `weread-exporter login` (once; the login is saved in `cache/cookie.txt`).
2. `weread-exporter info -b <book>` (no login needed) and choose one or two short chapters.
3. Export them into scratch directories, reusing the login:

   ```bash
   weread-exporter -b <book> --chapters 3 -o md -v --load-interval 8 \
     --cookie-file cache/cookie.txt --cache-dir /tmp/wr-cache --output-dir /tmp/wr-out
   ```

4. Look at the output, then run `weread-exporter reader-mode`: **it must say what it said before** (usually horizontal). If it does not, run `reader-mode restore` and fix the bug.

Rules: a few chapters per session, never in a loop, never against books you do not want in your reading history, never in CI. A free preview chapter of any book will do. Do not paste cookies, page dumps with your nickname, or book text into issues and commits.

## Debugging

- `-v` shows debug logs; `--debug-browser` forwards the page's console; `--log-format json` is for machines.
- Failed captures write `cache/diagnostics/<date>-<time>-<page>.html` and `.png`.
- To watch the browser, run without `--headless`. To keep Chrome's profile, add `--user-data-dir /tmp/profile`.
- `-race` finds most concurrency bugs quickly: `go test -race ./internal/browser ./internal/app`.

## Conventions

- Plain Go, `gofmt -s`, imports grouped with the module's own last (`goimports -local github.com/jtsang4/weread-exporter-go`).
- Errors: wrap with `%w`, start in lower case, no trailing period; user-facing errors say what to do.
- Logging: `log/slog` only, never print secrets (cookie values, tokens). stdout is for results (file paths, tables), stderr is for logs.
- Context: every blocking function takes a `context.Context` first; cleanup that must run after cancellation uses `context.WithoutCancel` with a timeout (see `detached`).
- Keep dependencies few; discuss new ones in the PR.
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/) (`feat(export): …`, `fix(browser): …`, `docs: …`, `test: …`, `chore: …`, `ci: …`, `refactor: …`): the release notes are generated from them.

## Changing behaviour: checklist

- [ ] Tests for the change (fake site if the program talks to the site).
- [ ] `make check` passes.
- [ ] Dependencies changed? `make tidy notices` (CI fails when `go.mod`, `go.sum` or `THIRD_PARTY_NOTICES.md` are stale).
- [ ] The flag tables of `README.md` and `README.en.md`, and `docs/`, agree with `weread-exporter --help`.
- [ ] `CHANGELOG.md` has a line under `[Unreleased]`.
- [ ] `AGENTS.md` still tells the truth.
- [ ] For a change of the cache layout: the Python tool's cache still loads.

## Releasing

Releases are made by GoReleaser from a tag (`.github/workflows/release.yml`).

1. `make check` and `make snapshot` locally on `main`.
2. Move the entries of `[Unreleased]` in `CHANGELOG.md` under a new `## [X.Y.Z] - YYYY-MM-DD` heading, commit (`docs: changelog for vX.Y.Z`).
3. `git tag -a vX.Y.Z -m "vX.Y.Z" && git push origin main vX.Y.Z`.
4. The workflow runs the short tests, builds Linux, macOS and Windows binaries for amd64 and arm64, packs them (`tar.gz`, `zip` for Windows, with the license, third-party notices, READMEs and shell completions), builds `.deb` and `.rpm` packages, writes `checksums.txt`, creates the GitHub release with notes generated from the commits, and attests the build provenance. It uses the newest Go release, so the binaries contain the latest standard library fixes.
5. Check the release page; download one archive, run `weread-exporter version`.

Tags with a suffix (`v1.2.0-rc.1`) become pre-releases. Binaries get their version from the tag through linker flags (`internal/version`); `go install …@vX.Y.Z` gets it from the module version.
