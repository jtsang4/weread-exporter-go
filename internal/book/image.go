package book

import (
	"bytes"
	"net/http"
	"path/filepath"
	"strings"
)

// ImageType describes a recognised image format.
type ImageType struct {
	MIME string
	Ext  string // without the dot; "jpg" for JPEG
}

var imageTypes = map[string]ImageType{
	"image/jpeg": {MIME: "image/jpeg", Ext: "jpg"},
	"image/png":  {MIME: "image/png", Ext: "png"},
	"image/gif":  {MIME: "image/gif", Ext: "gif"},
	"image/webp": {MIME: "image/webp", Ext: "webp"},
	"image/bmp":  {MIME: "image/bmp", Ext: "bmp"},
}

// SniffImage identifies the image format from the content (not from the URL or
// the Content-Type header, both of which are unreliable for CDN images). It
// reports false for anything that is not an image, for example an HTML error page.
func SniffImage(data []byte) (ImageType, bool) {
	if len(data) == 0 {
		return ImageType{}, false
	}
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	if t, ok := imageTypes[http.DetectContentType(head)]; ok {
		return t, true
	}
	trimmed := bytes.TrimLeft(head, "\xef\xbb\xbf \t\r\n")
	if bytes.HasPrefix(trimmed, []byte("<svg")) ||
		(bytes.HasPrefix(trimmed, []byte("<?xml")) && bytes.Contains(data[:min(len(data), 2048)], []byte("<svg"))) {
		return ImageType{MIME: "image/svg+xml", Ext: "svg"}, true
	}
	return ImageType{}, false
}

// MIMEForFile guesses the media type of a stored image: by content when the
// data is available, otherwise by file extension. Caches written by the
// original tool name every image ".jpg" regardless of its real format, so the
// content wins over the name.
func MIMEForFile(name string, data []byte) string {
	if t, ok := SniffImage(data); ok {
		return t.MIME
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".bmp":
		return "image/bmp"
	default:
		return "image/jpeg"
	}
}
