# Troubleshooting / 常见问题

First, run the failing command again with `-v` (debug log). Failed captures also leave the page (HTML and a screenshot) in `cache/diagnostics/`; look at the screenshot before you open an issue, and **remove personal data** (your nickname and avatar are on the page) before you share it.

先加上 `-v` 重新执行一次。抓取失败时，页面的 HTML 和截图会保存在 `cache/diagnostics/`，提 issue 前请先看一下截图，并去掉个人信息（页面上有你的昵称和头像）。

- [Chrome not found](#chrome-not-found--找不到-chrome)
- [Login](#login--登录)
- [The reader mode](#the-reader-mode--阅读模式)
- [A chapter cannot be read](#a-chapter-cannot-be-read--读不了的章节)
- [Servers, containers and headless mode](#servers-containers-and-headless-mode--服务器容器与无界面模式)
- [Slow, blocked or restricted](#slow-blocked-or-restricted--太慢被限制)
- [Output problems](#output-problems--输出问题)
- [Cleaning up](#cleaning-up--清理)

## Chrome not found / 找不到 Chrome

```text
browser: Google Chrome (or Chromium / Microsoft Edge) was not found; install it, put it on PATH, or pass --chrome-path / set CHROME_PATH
```

Install Chrome, Chromium or Edge, or tell the tool where it is:

```bash
weread-exporter -b ... --chrome-path "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
export CHROME_PATH=/usr/bin/chromium     # the same, for every run
```

Snap and Flatpak builds of Chromium may refuse to be started by another program; use the `.deb`/`.rpm` of Chrome instead.

## Login / 登录

**`a WeRead login is required` / 需要登录.** The web reader only shows the scrolling layout to a signed-in account, so a login is needed for every export. Run `weread-exporter login` (without `--headless`), scan the QR code with WeChat and confirm on the phone. You have five minutes (`browser: timed out waiting for the login to complete` means nobody scanned in time: run it again).

**The login stops working after a while.** The session is renewed automatically while the tool runs. If you do not use it for weeks the login expires for good; run `weread-exporter login` again, or add `--force-login` to an export.

**Where is the login?** In `cache/cookie.txt` (`--cookie-file` changes it). It is as sensitive as a password. `weread-exporter logout` deletes it.

**Chrome 136 and later and `--use-default-profile`.** Chrome refuses remote debugging on your everyday profile since version 136 (`browser: Chrome 136 and later refuse remote debugging on the default profile`). Use `--user-data-dir some/dir`: Chrome keeps its own profile there, and you sign in once.

**The QR code never appears / the window closes at once.** Run `weread-exporter login -v --debug-browser`. If your network needs a proxy, pass `--proxy-server http://127.0.0.1:7890` (it applies to Chrome and to the tool's own requests).

## The reader mode / 阅读模式

The export needs the *scrolling* reader (上下滚动阅读); the paged one (左右翻页) cannot be captured. The mode is a setting of your account, so the tool switches it for the export and switches it back. See the README.

**`the account uses the paged (horizontal) reader`.** You gave `--keep-reader-mode`, or the switch did not work. Either leave out `--keep-reader-mode`, or switch yourself once: `weread-exporter reader-mode vertical` (and `reader-mode horizontal` to go back afterwards).

**My reader is still scrolling after an export.** The process was probably killed before it could restore the mode. Run `weread-exporter reader-mode restore`; the next export does the same automatically (the pending restore is recorded in `cache/reader-mode-restore.json`). You can also change the mode by hand in the web reader's settings.

**`the saved login belongs to a different account than the one whose reader mode changed`.** Sign in with the account that was switched, then run `reader-mode restore` again; or delete `cache/reader-mode-restore.json` if you already changed the mode by hand.

**Do not run two exports for the same account at the same time.** The mode is shared by both, and the first one to finish switches it back under the other's feet.

## A chapter cannot be read / 读不了的章节

```text
chapter 9 "第1章 起步" cannot be read: browser: the chapter cannot be read (reader footer button "…"); it is probably not free and not owned by this account
```

The web reader does not show the text of this chapter to your account: it is not free, and you neither bought the book nor have a reading membership that covers it. The tool does not (and cannot) get around that. Use `info` to see the table of contents and export what you can read:

```bash
weread-exporter info -b ...
weread-exporter -b ... --chapters 1-8
```

Everything captured before the problem stays in the cache.

**`the book is no longer available on WeRead`.** The book was taken off sale. Chapters that are in the cache can still be exported (`--chapters`); the others cannot be captured any more.

**The text looks cut off.** The tool warns (`the captured text looks too short`) when a chapter has far fewer characters than the table of contents promises. Delete that chapter's `.md` file in `cache/<book id>/chapters/` and export again.

## Servers, containers and headless mode / 服务器、容器与无界面模式

The QR code needs a visible window, so **sign in on a machine with a screen**, then copy `cache/cookie.txt` (or the whole cache directory) to the server:

```bash
# on your computer
weread-exporter login
scp cache/cookie.txt server:~/weread/cache/cookie.txt

# on the server
weread-exporter -b 34336681 --headless --no-sandbox --cache-dir ~/weread/cache
```

- `--no-sandbox` is needed when Chrome runs as root and in most containers.
- Without a display, a visible Chrome fails with `no display available for a visible Chrome window`; use `--headless`.
- `--headless` needs a saved login: a headless browser cannot show the QR code.
- If the browser looks automated, the tool asks whether to go on, because WeRead may restrict accounts it recognises as bots. `-y` answers yes. Think about it before you do.
- Docker: install Chrome or Chromium in the image, set `CHROME_PATH` if needed, and pass `--headless --no-sandbox`. Mount the cache and output directories as volumes. Give the container a big enough `/dev/shm` (`--shm-size=1g`) for the PDF output of long books.

## Slow, blocked or restricted / 太慢、被限制

- **It is slow on purpose.** Every chapter is read page by page, and 30 seconds pass between two chapters (`--load-interval`). A book of 40 chapters takes about 20 minutes plus the time to read the pages. Do not lower the interval: WeRead may restrict accounts that read too fast.
- **Interrupt and continue.** Ctrl-C stops cleanly (the reader mode is restored); run the same command again and it carries on with the chapters that are not cached.
- **`capturing the chapter failed` warnings** are retried (3 attempts per chapter, and the browser is restarted if it keeps failing). If a chapter fails every time, raise `--load-timeout 120`, check the screenshot in `cache/diagnostics/`, and try again later.
- **The page asks for verification, or the account is restricted.** Stop, open weread.qq.com in your normal browser, and see what it says. Wait before you try again, and do not run exports in a loop.

## Output problems / 输出问题

- **MOBI: `cannot produce MOBI`.** MOBI is converted from the EPUB by Calibre's `ebook-convert` or by `kindlegen`; install [Calibre](https://calibre-ebook.com/) (`brew install --cask calibre`, `apt install calibre`, ...) or point `--mobi-converter` at the program. The check happens before anything is captured. Most Kindles read EPUB directly nowadays (send it with Amazon's "Send to Kindle").
- **PDF takes long or fails.** The PDF is printed by a separate headless Chrome, which needs time and memory for long books (a few minutes for a thousand pages). `--chrome-path` and `--no-sandbox` apply to it as well. Try EPUB if you only need to read on a device.
- **An output file is not updated.** Existing outputs are kept; add `--overwrite`.
- **The EPUB does not look right in my reader.** Add your own CSS with `--css-file my.css` (it is appended to the built-in style sheet), and tell us which reader it was.
- **The file names.** Characters that file systems do not allow are percent-encoded as the Python tool did (an ASCII `:` in a title becomes `%3a`; the full-width `：` is kept), and very long titles are cut. A partial export has the chapters in its name (`<title> (chapters 1-5).epub`).

## Cleaning up / 清理

| To remove | Do |
| --- | --- |
| the login | `weread-exporter logout` |
| everything cached for one book | delete `cache/<book id>/` |
| all captured books | delete the `cache/<book id>/` directories; keep `cookie.txt` if you want to stay signed in |
| the diagnostics | delete `cache/diagnostics/` |
| a Chrome that was left behind after the program was killed with `kill -9` | quit it from the task manager; it uses a temporary profile directory that the system cleans up |
