// Package markdown post-processes the Markdown captured from the reader canvas
// and converts it to HTML for the different output formats.
package markdown

import "strings"

const fence = "```"

// Normalize turns the raw canvas capture into proper Markdown.
//
// The hook emits one line per canvas text row, so a paragraph arrives hard
// wrapped at the width of the reader column. Outside fenced code blocks the
// rows of a paragraph are joined without a separator (rows of English text carry
// their trailing space, rows of CJK text need none) and blank lines become
// paragraph breaks. Fenced code blocks are copied verbatim and forced onto their
// own lines.
//
// This is an exact port of the pre-processing step of the original Python tool;
// do not "improve" it without regenerating the golden files in testdata/.
func Normalize(text string) string {
	var out strings.Builder
	out.Grow(len(text) + 16)

	codeMode := false
	blankLine := false
	for _, line := range strings.Split(text, "\n") {
		switch {
		case line == fence:
			if !codeMode {
				out.WriteString("\n" + line + "\n")
			} else {
				out.WriteString(line + "\n")
			}
			codeMode = !codeMode
		case codeMode:
			out.WriteString(line + "\n")
		case line == "":
			blankLine = true
		case blankLine:
			out.WriteString("\n\n" + line)
			blankLine = false
		default:
			out.WriteString(line)
		}
	}
	out.WriteString("\n")
	return out.String()
}
