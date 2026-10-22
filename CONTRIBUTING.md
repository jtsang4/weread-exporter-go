# Contributing

Thank you for helping! Bug reports, ideas, documentation fixes and code are all welcome. 欢迎使用中文或英文提 issue 和 PR。

Please be kind to others: see the [Code of Conduct](CODE_OF_CONDUCT.md). To report a security problem, follow [SECURITY.md](SECURITY.md) instead of opening a public issue.

## What this project is, and is not

weread-exporter saves the books that **your own WeRead account is allowed to read** for **your own use**, in open formats. It does not, and will not, get around WeRead's access rules: no paywall or membership bypass, no sharing of accounts or cookies, no bulk downloading of books that you cannot read in the web reader. Pull requests that go in that direction will be declined.

## Reporting a bug

Open an issue with the bug template and say

- the exact command you ran and the output with `-v`,
- the version (`weread-exporter version`), your operating system and your Chrome version,
- whether the problem is new (it worked before, with which version?).

For a capture that fails, look at the screenshot in `cache/diagnostics/` first; it often shows the reason (a login, a purchase prompt, a paged reader). **Before you attach anything, remove personal data**: your nickname and avatar are on the pages, and `cache/cookie.txt` is your login and must never be shared. Do not post the text of books.

Read [docs/troubleshooting.md](docs/troubleshooting.md) first: most problems are in there.

## Suggesting a change

Open an issue before you write a lot of code, so that we can agree on the direction. Small fixes can go straight to a pull request.

## Developing

[docs/development.md](docs/development.md) has the details. In short:

```bash
git clone https://github.com/jtsang4/weread-exporter-go.git
cd weread-exporter-go
make build          # bin/weread-exporter
make check          # gofmt, go vet, golangci-lint and go test -race: what CI runs
```

[AGENTS.md](AGENTS.md) lists the rules of the code base (the ones that protect users' accounts and data come first); it is written for coding agents, and it is just as good for people.

### Pull requests

1. Fork, create a branch from `main`, make one logical change.
2. Add tests. Everything that touches the browser or the site can be tested without an account with the fake site in `internal/fakesite`; tests must never contact weread.qq.com.
3. Run `make check`. If you changed the dependencies, run `make tidy notices` too.
4. If you changed capturing, login or the reader mode, also try it against the real site **with low volume** (one or two free chapters, the default pause between chapters), and check with `weread-exporter reader-mode` that your account's reader mode is as it was. Say in the pull request what you tried.
5. Update the documentation that is affected: `README.md` and `README.en.md` (both), the flag tables against `weread-exporter --help`, `docs/`, and `CHANGELOG.md` under `[Unreleased]`.
6. Write commit messages in the [Conventional Commits](https://www.conventionalcommits.org/) style (`feat(export): …`, `fix(browser): …`, `docs: …`); the release notes are generated from them.
7. Open the pull request and fill in the template.

A maintainer will review it; please be patient, and do not take change requests personally.

### Code style

`gofmt -s` and golangci-lint decide the formatting; the rest is in AGENTS.md. Keep functions small, wrap errors with `%w`, pass contexts, and explain *why* in comments.

## License

By contributing you agree that your contribution is licensed under the [MIT License](LICENSE) of the project.
