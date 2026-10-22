# weread-exporter

[![CI](https://github.com/jtsang4/weread-exporter-go/actions/workflows/ci.yml/badge.svg)](https://github.com/jtsang4/weread-exporter-go/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/jtsang4/weread-exporter-go?sort=semver)](https://github.com/jtsang4/weread-exporter-go/releases)
[![Go Reference](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

**中文** | [English](README.en.md)

把[微信读书](https://weread.qq.com/)网页版里你**有权阅读**的书导出成 **EPUB / PDF / MOBI / TXT / Markdown**。

这是 [drunkdream/weread-exporter](https://github.com/drunkdream/weread-exporter)（Python）的 Go 重写版：单个静态二进制文件，不需要 Python 和 pip，支持 Linux、macOS、Windows 的 amd64 与 arm64。

## 特性

- **单文件分发**：从 [Releases](https://github.com/jtsang4/weread-exporter-go/releases) 下载即可运行，运行时只需要一个 Chrome / Chromium / Edge。
- **五种输出格式**：EPUB 3、PDF、MOBI、TXT、Markdown，可一次生成多种。
- **断点续传**：每章抓取后立即缓存，中断后重新执行会接着抓；书已经完整缓存时不再启动浏览器。
- **登录一次，自动续期**：用微信扫码登录一次，登录信息保存在本地，导出期间自动续期。
- **自动切换阅读模式**：导出需要「上下滚动阅读」模式，工具会自动切换，并在结束（包括失败、中断、进程被杀后的下一次运行）时恢复原来的模式。
- **按章节导出**：`--chapters 1-3,7,10-`，配合 `info` 命令查看目录和缓存状态。
- **书单**：`-b` 可以直接给书单 ID，逐本导出，某一本失败不影响其他书。
- **缓存兼容 Python 版**：`cache/<书籍ID>/` 的目录结构、`cookie.txt` 的格式都与原版一致，已有缓存可以直接沿用。
- **安全写入**：所有输出文件先写临时文件再原子改名，不会留下写了一半的文件；已存在的输出默认不覆盖。

## 安装

### 下载预编译文件（推荐）

到 [Releases](https://github.com/jtsang4/weread-exporter-go/releases) 下载对应系统的压缩包（`linux` / `darwin`(macOS) / `windows` × `amd64` / `arm64`），解压后得到 `weread-exporter`（Windows 为 `weread-exporter.exe`）。用 `checksums.txt`（SHA-256）校验下载。Linux 另提供 `.deb` 和 `.rpm` 包（会安装 bash、zsh、fish 的命令补全）。压缩包里还有 `completions/`（命令补全脚本）和 `THIRD_PARTY_NOTICES.md`（第三方软件的许可证）；也可以自己生成补全脚本：`weread-exporter completion bash|zsh|fish|powershell`。

macOS 下载后如果被系统拦截，执行 `xattr -d com.apple.quarantine weread-exporter`。

### 使用 Go 安装

```bash
go install github.com/jtsang4/weread-exporter-go/cmd/weread-exporter@latest
```

需要 Go 1.26 或更新版本。

### 从源码构建

```bash
git clone https://github.com/jtsang4/weread-exporter-go.git
cd weread-exporter-go
make build        # 生成 bin/weread-exporter
# 没有 make 时：go build -o weread-exporter ./cmd/weread-exporter
```

### 运行环境

| 需要 | 用途 |
| --- | --- |
| Google Chrome、Chromium 或 Microsoft Edge | 必需。打开阅读页面抓取正文，生成 PDF。自动查找；也可以用 `--chrome-path` 或环境变量 `CHROME_PATH` 指定 |
| [Calibre](https://calibre-ebook.com/) 的 `ebook-convert`，或 `kindlegen` | 仅在输出 MOBI 时需要。用 `--mobi-converter` 指定其他位置 |

## 快速开始

```bash
# 1. 登录（只需要一次）：会打开 Chrome 显示二维码，用微信扫码并在手机上确认
weread-exporter login

# 2. 导出一本书，默认生成 EPUB，文件在 ./output/ 下
weread-exporter -b 34336681

# 同时生成多种格式
weread-exporter -b 34336681 -o epub -o pdf -o md -o txt
```

### 怎么获得书籍 ID

`-b` / `--book-id` 接受下面任何一种写法：

- 书籍详情页网址：`https://weread.qq.com/web/bookDetail/08232ac0720befa90825d88`（在 <https://weread.qq.com/> 搜索书名进入详情页，复制地址栏即可）
- 详情页网址里的那串 ID（如 `08232ac0720befa90825d88`）
- 阅读页网址：`https://weread.qq.com/web/reader/...`
- 数字 ID（如 `34336681`，工具会换算成网址里的 ID）
- 书单 ID（含 `_`，如 `352003193_7T4LYH9ib`）或书单页面网址 `https://weread.qq.com/misc/booklist/352003193_7T4LYH9ib`，导出书单里的所有书

## 用法

```text
weread-exporter [flags]                  导出一本书（或一个书单）
weread-exporter login                    扫码登录并保存登录信息
weread-exporter logout                   删除保存的登录信息
weread-exporter info -b BOOK             查看目录和缓存状态（不登录、不导出）
weread-exporter reader-mode [status|vertical|horizontal|restore]
                                         查看或修改账号的阅读模式
weread-exporter version                  版本信息
weread-exporter completion <shell>       生成 bash / zsh / fish / powershell 补全脚本
```

导出命令的常用参数（完整列表见 `weread-exporter --help`，每个子命令也有自己的 `--help`）：

| 参数 | 说明 |
| --- | --- |
| `-b, --book-id` | 要导出的书，见上文 |
| `-o, --output-format` | 输出格式：`epub`、`pdf`、`mobi`、`txt`、`md`，可重复；默认 `epub` |
| `--chapters` | 只导出部分章节，如 `1-3,7,10-`；序号是 `info` 命令列出的序号 |
| `--output-dir` | 输出目录，默认 `output` |
| `--cache-dir` | 缓存目录（登录信息和抓取的章节），默认 `cache` |
| `--overwrite` | 覆盖已存在的输出文件（默认保留） |
| `--css-file` | 追加到 EPUB 和 PDF 内置样式表之后的 CSS |
| `--load-interval` | 两章之间的间隔，默认 `30s`（见[负责任地使用](#负责任地使用)） |
| `--load-timeout` | 单个章节页面的加载超时，默认 `60s` |
| `--headless` | 不显示浏览器窗口（需要先用 `login` 登录） |
| `--no-sandbox` | 不使用 Chrome 沙箱（容器里、root 下需要） |
| `--chrome-path` | 浏览器可执行文件 |
| `--user-data-dir` | 保留 Chrome 的配置目录 |
| `--force-login` | 忽略已保存的登录，重新登录 |
| `--keep-reader-mode` | 不切换账号的阅读模式（账号是左右翻页时会直接失败） |
| `--mobi-converter` | EPUB 转 MOBI 的程序 |
| `--proxy-server` | 浏览器和 HTTP 请求使用的代理 |
| `--log-format` | 日志格式：`text` 或 `json`（日志写到标准错误） |
| `-v, --verbose` | 输出调试日志 |
| `-y, --yes` | 对提问一律回答 yes（例如浏览器看起来是自动化的时候继续） |

`--load-interval` 和 `--load-timeout` 可以写秒数（`30`）或时间长度（`500ms`、`2m`）。

### 例子

```bash
# 先看目录，再只导出前 5 章
weread-exporter info -b 34336681
weread-exporter -b 34336681 --chapters 1-5

# 机器可读的目录
weread-exporter info -b 34336681 --json

# 导出书单里所有的书
weread-exporter -b 352003193_7T4LYH9ib -o epub

# 已登录的服务器上，不显示窗口
weread-exporter -b 34336681 --headless --no-sandbox

# MOBI 由 EPUB 转换而来，所以也会生成 EPUB
weread-exporter -b 34336681 -o mobi
```

### 输出

- 文件写到 `--output-dir`，文件名是书名：`<书名>.epub`、`<书名>.pdf`、`<书名>.mobi`、`<书名>.txt`、`<书名>.md`。
- 只导出部分章节时，文件名会带上章节：`<书名> (chapters 1-5).epub`，避免被当成整本书。
- Markdown 是合并成一个文件的整本书，图片放在同目录的 `<书名>.assets/` 里。
- 命令结束后，**标准输出**逐行打印写入的文件路径，日志和进度写到**标准错误**，所以可以放心用管道：`weread-exporter -b ... | xargs ls -l`。
- 退出码：`0` 成功；`1` 失败；`2` 命令行或配置有误；`3` 需要登录但没能登录；`130` 被 Ctrl-C 中断。

## 工作原理

微信读书网页版把正文画在 `<canvas>` 里，页面上没有可复制的文本。工具用 Chrome DevTools Protocol 控制 Chrome，在阅读页加载前注入一段脚本，**Hook 掉 Canvas 的绘制函数**，截获每一次绘制的文字、字体、颜色和图片，还原成 Markdown，缓存到本地；然后再用缓存的 Markdown 生成各种格式。这与 Python 原版的思路相同，细节见 [docs/architecture.md](docs/architecture.md)。

因此：

- 只能导出**你的账号在网页版里能读到的内容**：免费章节、你已购买的书、会员卡可读的书。读不到的章节（没有开通会员的付费章节）会让导出停下并提示用 `--chapters` 选择可读的章节；已经抓取的章节会保留。
- 抓取要等页面一屏一屏「翻」完，所以**很慢**，这是有意为之，见下文。

### 阅读模式

网页版有两种阅读器：**左右翻页**（新账号的默认值）和**上下滚动**。能被抓取的只有上下滚动的版本，而阅读模式是**账号的设置**（保存在服务器上），不是浏览器的设置。所以导出开始时，工具会把账号切换到「上下滚动」，抓取完成后立刻切回原来的模式——即使失败或被中断。如果进程被强行杀死来不及恢复，工具在缓存目录里留有一个标记文件，下一次运行（或 `weread-exporter reader-mode restore`）会把模式恢复。

不想让工具动你的阅读设置：自己在微信读书网页版里选「上下滚动」，再加 `--keep-reader-mode`。

### 缓存

```text
cache/
├── cookie.txt                         登录 cookie（敏感！）
├── reader-mode-restore.json           仅在阅读模式被切换、尚未恢复时存在
├── diagnostics/                       抓取失败时保存的页面（HTML 和截图），分享前请先检查
└── <书籍ID>/
    ├── meta.json                      书名、作者、简介、目录
    ├── cover.jpg
    ├── chapters/<序号>-<章节uid>.md   每章的 Markdown（.md.bak 是处理前的原始抓取）
    └── images/                        章节里的图片
```

缓存与 Python 原版互通。想重新抓取某一章，删除它的 `.md` 文件即可；想整本重来，删除 `cache/<书籍ID>/` 目录（不会影响登录）。

## 常见问题

见 [docs/troubleshooting.md](docs/troubleshooting.md)：找不到 Chrome、登录超时、服务器上没有显示器、`reader-mode` 没恢复、读不了的章节、被限制访问等。

## 与 Python 版的区别

| | Python 版 | Go 版 |
| --- | --- | --- |
| 分发 | `pip install` + 依赖 | 单个静态二进制，多平台预编译 |
| 命令 | `python -m weread_exporter -b ID -o epub` | `weread-exporter -b ID -o epub`（参数基本相同） |
| 格式 | epub / pdf / mobi / txt | 另增 Markdown |
| 阅读模式 | 需要用户自己切到上下滚动 | 自动切换并恢复 |
| 全部章节已缓存 | 仍会打开浏览器 | 不启动浏览器 |
| 部分章节 | 不支持 | `--chapters` |
| 子命令 | 无 | `login`、`logout`、`info`、`reader-mode`、`version`、`completion` |
| 输出文件 | 直接写入 | 原子写入；已存在的默认不覆盖 |
| 缓存目录 | `cache/` | 兼容，可以直接共用 |
| 浏览器 | 自带的 Chrome 启动与代理脚本 | 直接用 DevTools Protocol（chromedp） |

## 负责任地使用

- 这个工具读取的是**你自己的账号有权阅读**的内容，**不绕过**付费或会员限制。导出的书仅供你个人学习、备份和跨设备阅读，请**不要传播**，尊重作者和出版方的版权。
- 微信读书的用户协议可能不允许自动化访问，**账号有被限制的风险，后果由使用者自己承担**。
- 抓取一章要等页面滚完，章与章之间默认再间隔 30 秒，这是为了模拟正常阅读的速度。**不建议调小 `--load-interval`**，更不要同时开多个实例导出同一个账号。
- 登录信息（`cache/cookie.txt`）等同于你的账号密码：不要提交到 Git，不要分享，不要贴到 issue 里。工具把它保存为仅自己可读写的文件，日志里不会出现 cookie 的值。
- 本工具与腾讯、微信读书没有任何关系。

## 参与开发

```bash
make check        # gofmt、go mod tidy、go vet、golangci-lint、许可证清单、go test -race
make cross        # 为全部 6 个平台交叉编译到 dist/
make help         # 全部任务
```

开发前请阅读 [CONTRIBUTING.md](CONTRIBUTING.md) 和 [docs/development.md](docs/development.md)；给 AI 编码助手的说明在 [AGENTS.md](AGENTS.md)。安全问题请按 [SECURITY.md](SECURITY.md) 私下报告。

## 致谢与许可

- 原项目与思路：[drunkdream/weread-exporter](https://github.com/drunkdream/weread-exporter)（MIT）。
- 本项目以 [MIT 许可证](LICENSE) 发布。
