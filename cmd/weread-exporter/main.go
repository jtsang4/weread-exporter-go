// Command weread-exporter exports WeChat Read (weread.qq.com) books to EPUB,
// PDF, MOBI, TXT and Markdown.
package main

import (
	"os"

	"github.com/jtsang4/weread-exporter-go/internal/cli"
)

func main() {
	os.Exit(cli.Main())
}
