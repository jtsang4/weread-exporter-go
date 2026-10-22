# Security policy

## Supported versions

Only the latest release receives security fixes. Please update before you report a problem.

## Reporting a vulnerability

**Do not open a public issue for a security problem.**

Use GitHub's private vulnerability reporting: open the repository's **Security** tab and choose **Report a vulnerability**. If that button is not available, open a public issue that only says you would like to report a vulnerability (no details) and a maintainer will arrange a private channel.

Please include what you found, how to reproduce it, the version (`weread-exporter version`) and your operating system. We aim to reply within a week and to keep you informed until a fix is released and the advisory is published. Credit is given if you want it.

## What is in scope

weread-exporter runs on your computer, stores your WeRead login and controls a browser, so these matter most:

- **Credential handling**: the login cookies in `cache/cookie.txt` and the browser profile being written world-readable, logged, sent anywhere but weread.qq.com, or leaked into error messages or diagnostics.
- **Untrusted input from the site or from books**: titles, chapter names, image URLs and text are turned into file names, paths inside an EPUB, HTML for the PDF and Markdown. Path traversal, writing outside the output and cache directories, script execution in the generated files, or running programs because of crafted content are vulnerabilities.
- **The release pipeline**: tampering with the released binaries, the checksums or the build provenance.
- **Dependencies** with known vulnerabilities that affect the tool (`govulncheck` runs in CI).

## What is out of scope

- Anything that needs control of your computer or your account already.
- Options that you give on the command line on purpose, such as `--chrome-path`, `--mobi-converter` and `--no-sandbox`: they run what you tell them to run.
- WeRead restricting an account that read too fast, bypasses of WeRead's access rules, and the content of the books.

## Good practice for users

- Keep `cache/` private: it holds your login. The directory is git-ignored here; keep it out of your own repositories, backups you share and bug reports.
- Prefer the release archives with their `checksums.txt`, or build from a tag.
- Use `weread-exporter logout` on shared machines.
