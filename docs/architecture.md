# Architecture

This document explains how weread-exporter works and why it is built the way it is. It is meant for contributors; users should read the [README](../README.md).

## The problem

The web reader of WeChat Read draws the text of a book on a `<canvas>`. The page contains no text to copy, the images are drawn separately, and the text is only drawn when the page is shown. There is no public API for the content of a book.

The only way to get the text out is to **watch the drawing**: the reader calls `CanvasRenderingContext2D.fillText(text, x, y)` for every line it shows, with the font and the colour set in the context. Font size and position tell headings from paragraphs; the colour tells highlighted passages from plain text. The idea is that of the Python original ([drunkdream/weread-exporter](https://github.com/drunkdream/weread-exporter)); this port keeps its rules, so that the Markdown of a chapter is the same.

## Data flow

```text
                    +---------------------------+
 weread-exporter -b |  internal/cli (cobra)     |  flags, log, exit status
                    +-------------+-------------+
                                  |
                    +-------------v-------------+
                    |  internal/app             |  orchestration, sessions,
                    |  Exporter.Run             |  reader mode, retries
                    +--+--------+-----------+---+
                       |        |           |
        +--------------v-+   +--v------+  +-v---------------+
        | internal/weread|   | internal|  | internal/browser|
        | HTTP: book info|   | /book   |  | Chrome via CDP: |
        | login renewal, |   | cache   |  | login, hook,    |
        | reader mode    |   | store   |  | capture, PDF    |
        +----------------+   +----+----+  +-----------------+
                                  |
                    +-------------v-------------+
                    | internal/markdown         |  normalise, localise images
                    +-------------+-------------+
                                  |
                    +-------------v-------------+
                    | internal/export           |  EPUB, PDF HTML, TXT, MD, MOBI
                    +---------------------------+
```

One run of `Exporter.Run` for one book:

1. **Resolve** the reference (`weread.ParseRef`): hashed id, numeric id (converted with the same hash the web site uses, `weread.Hash`), reader URL, or book list.
2. **Describe** the book: fetch the book detail page over plain HTTP and read the server-rendered `window.__INITIAL_STATE__` (title, author, table of contents with levels and word counts). The result is cached as `meta.json`; when the site cannot be reached the cached one is used.
3. **Select** the chapters (`--chapters`) and find those that are not cached yet (`book.Store.HasChapter`).
4. **Capture** the missing chapters, if any (see below). If there are none, no browser is started.
5. **Post-process** every selected chapter once: `markdown.Normalize` joins the lines of a paragraph the way the Python tool did (checked against golden files made by the original code, see `testdata/`), and `markdown.LocalizeImages` downloads the images into the cache. The raw capture is kept as `<chapter>.md.bak`, which also marks the chapter as processed.
6. **Write** the outputs in the order EPUB, PDF, MOBI, TXT, Markdown. Every file is written to a temporary file and renamed.

## Capturing a chapter

`browser.Launch` starts Chrome through chromedp. Before any page loads it:

- injects `internal/assets/stealth.js` (hides the usual automation fingerprints) and `internal/assets/hook.js` (the canvas hook: it wraps `getContext('2d')` of the canvases of the reader page in a Proxy, turns the `fillText` stream into Markdown, and exposes it as `canvasContextHandler`);
- answers the telemetry requests of the reader (`/hera/`, `/sentry/`, `/river/single`) locally instead of sending them to the server, as the Python tool does (`rules.go`);
- sets the cookie `wr_theme=dark`: the colour thresholds of the hook are calibrated for the dark theme. The theme is never saved to the account;
- checks whether the browser looks headless or automated and, if so, asks for a confirmation (`--yes` answers it).

`Session.CaptureChapter` then opens the reader URL and loops over the pages of the chapter. The footer button of the vertical reader tells where it is: `下一页` (next page: click it and wait until the text stops changing), `下一章` (the chapter is complete), `登录...` (the reader wants a login: `ErrLoginRequired`), anything else (for example a purchase prompt: `ChapterUnavailableError`). The last chapter of a book has no footer button; the capture waits for `FooterTimeout` and takes that as the end.

A page that shows the paged reader (`.wr_horizontalReader`) fails with `ErrHorizontalReader`. Failed captures save the page (HTML and a screenshot) in `<cache>/diagnostics/`.

### Reader mode

The reader layout (paged or scrolling) is a setting of the **account**, stored by the server (`config.useHorizonReader`); the hook only works in the scrolling layout, and visitors that are not signed in always get the paged one. Hence `app.ensureVertical` reads the mode (from the server-rendered state of a reader page), writes a marker file `<cache>/reader-mode-restore.json` **before** it switches (`POST /web/user_config/modify`), and `restoreReaderMode` switches back and removes the marker. The restore runs

- right after the capture of a book (before the long post-processing),
- in a `defer` of `Run` (also after errors and Ctrl-C, with a context that outlives the cancellation),
- at the start of the next `Run` and in `weread-exporter reader-mode restore`, if the marker is still there (the process was killed).

The marker holds the account (`vid`); a restore is refused with the login of a different account.

### Sessions

`wr_skey`, the cookie that authenticates requests, expires after about an hour. `app.keepSessionFresh` renews it (`POST /web/login/renewal`) about every 20 minutes during a capture, and `refreshSession` does so before the calls that need a valid session. The renewed cookies are saved to `cookie.txt` and handed to the browser. If the reader asks for a login nevertheless, `handleLoginRequired` re-injects the cookies, and failing that opens the QR code login again (if the browser is visible), at most twice per chapter.

### Failure handling

- A chapter is retried up to 3 times with a growing pause; a browser that keeps failing is restarted, at most 3 times per book.
- Problems that are not the chapter's fault (login, reader mode) are repaired without using up an attempt, with a bounded number of rounds.
- A chapter that cannot be read (paywall) stops the book with a message; everything captured before it is kept.
- In a book list a failing book does not stop the next, unless the failure would repeat (no login, paged reader) or the run was cancelled. The result is a `BatchError`.

## The cache

The layout is the one of the Python tool, so the two can share a directory:

```text
cache/
├── cookie.txt                     JSON {"name": "value"}, mode 0600
├── reader-mode-restore.json       pending restore, mode 0600
├── diagnostics/                   dumps of failed captures
└── <hashed book id>/
    ├── meta.json                  {"title","author","cover","intro","chapters":[{"id","title","level","words","anchors"}]}
    ├── cover.jpg
    ├── chapters/<position>-<chapter uid>.md       position is 1-based
    ├── chapters/<position>-<chapter uid>.md.bak   the raw capture of a processed chapter
    └── images/<md5 of the url>.<ext>
```

A chapter counts as captured when its `.md` is larger than 3 bytes (the Python tool's rule). `SaveChapter` (a fresh capture) drops a stale `.bak`; `IsProcessed` is "a `.bak` exists". The two states are therefore recoverable after a crash at any point: a raw capture is processed again on the next run, and a processed one is not processed twice.

## Outputs

| Format | Package | Notes |
| --- | --- | --- |
| EPUB | `export/epub.go` | EPUB 3 with an EPUB 2 NCX for old readers; a cover page, a nav document built from the table of contents (levels and in-chapter anchors), images, `epub.css` plus the user's CSS. Validated with EPUBCheck. |
| PDF | `export/pdf.go`, `browser/pdf.go` | The book is rendered to one HTML file (`book.print.html`, written to the book's cache directory so relative image links work), and a separate headless Chrome with scripts disabled prints it with `Page.printToPDF`; the page size and margins come from the `@page` rule of `pdf.css`. |
| MOBI | `export/mobi.go` | Converted from the EPUB with Calibre's `ebook-convert` or `kindlegen`; the converter is looked up **before** anything is captured. |
| TXT | `export/text.go` | Plain text, chapter titles on their own line. |
| Markdown | `export/text.go` | The whole book in one file; images are copied to `<title>.assets/`. |

## Packages

| Package | Responsibility |
| --- | --- |
| `cmd/weread-exporter` | `main`, nothing else |
| `internal/cli` | cobra commands and flags, logging setup, exit statuses, signals |
| `internal/app` | the orchestration described above; `Config`, `Exporter`, `Run`, `Login`, `Logout`, `Info`, reader mode |
| `internal/browser` | chromedp: launch, hook injection, capture, QR login, cookies, PDF printing, Chrome discovery |
| `internal/weread` | the site's HTTP side: client with retries, `ParseRef`, `Hash`, book detail and book list parsing, cookies, reader mode, session renewal |
| `internal/book` | `Meta`, `Chapter`, the cache `Store`, file names, image sniffing |
| `internal/markdown` | normalisation, image localisation, Markdown to XHTML/HTML (goldmark) |
| `internal/export` | the output formats |
| `internal/assets` | embedded scripts and style sheets |
| `internal/fsutil` | atomic file writes and small helpers |
| `internal/version` | build information (set by the linker) |
| `internal/fakesite` | a fake WeRead (HTTP server) used by the tests |

## Testing

- **Unit tests** in every package; the Python tool's behaviour is pinned with golden files generated by the original code (`testdata/generators/*.py`: the id hash and the Markdown normalisation).
- **`internal/fakesite`** imitates the pieces of weread.qq.com the program talks to: book pages with `__INITIAL_STATE__`, reader pages that draw text on a canvas and have a footer button, login cookies and renewal, the reader mode setting, a book list. With it, `internal/browser`, `internal/app` and `internal/cli` run against **a real headless Chrome and no account**.
- **Live checks** against the real site are manual and kept small (see [development.md](development.md)); no test or CI job talks to weread.qq.com.

## Decisions

- **chromedp, not Playwright or Rod.** One pure Go dependency, no driver download, the DevTools Protocol is all that is needed.
- **No HTML scraping of the book text.** The text is not in the HTML; the capture hook is the only reliable source.
- **Account-wide reader mode switching** instead of asking users to change it: the Python tool needed this setting by hand and failed in confusing ways when it was wrong. The marker file makes the side effect recoverable.
- **Cache compatibility** with the Python tool: someone who captured books with it can carry on with the same `cache/` directory (chapters, metadata, images and the saved login) instead of capturing everything again.
- **`slog` and a small pretty handler** instead of a logging library; **`cobra`** because the tool has subcommands and completion.
