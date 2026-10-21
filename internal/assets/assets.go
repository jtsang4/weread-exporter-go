// Package assets embeds the static files shipped inside the binary: the canvas
// hook injected into the WeRead reader, the browser fingerprint patches and the
// default style sheets.
package assets

import _ "embed"

// HookJS is the script injected into every new document. It intercepts the
// reader's canvas drawing calls and rebuilds Markdown from them.
//
//go:embed hook.js
var HookJS string

// StealthJS is injected into every new document before any page script runs
// and hides the differences between a DevTools-driven Chrome and a regular one.
//
//go:embed stealth.js
var StealthJS string

// HeadlessCheckJS is an expression that evaluates to the list of reasons why
// the browser looks headless or automated (empty when it does not).
//
//go:embed headless_check.js
var HeadlessCheckJS string

// PDFCSS is the default style sheet used for PDF output.
//
//go:embed pdf.css
var PDFCSS string

// EPUBCSS is the default style sheet bundled into EPUB output.
//
//go:embed epub.css
var EPUBCSS string
