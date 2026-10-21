// Package assets embeds the static files shipped inside the binary: the canvas
// hook injected into the WeRead reader and the default style sheets.
package assets

import _ "embed"

// HookJS is the script injected into every new document. It intercepts the
// reader's canvas drawing calls and rebuilds Markdown from them.
//
//go:embed hook.js
var HookJS string

// PDFCSS is the default style sheet used for PDF output.
//
//go:embed pdf.css
var PDFCSS string

// EPUBCSS is the default style sheet bundled into EPUB output.
//
//go:embed epub.css
var EPUBCSS string
