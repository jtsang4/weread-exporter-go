package book

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func encodedImages(t *testing.T) map[string][]byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})

	out := map[string][]byte{}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	out["png"] = append([]byte(nil), b.Bytes()...)
	b.Reset()
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	out["jpg"] = append([]byte(nil), b.Bytes()...)
	b.Reset()
	if err := gif.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	out["gif"] = append([]byte(nil), b.Bytes()...)
	return out
}

func TestSniffImage(t *testing.T) {
	mimes := map[string]string{"png": "image/png", "jpg": "image/jpeg", "gif": "image/gif"}
	for ext, data := range encodedImages(t) {
		got, ok := SniffImage(data)
		if !ok || got.Ext != ext || got.MIME != mimes[ext] {
			t.Errorf("%s: SniffImage = %+v, %v", ext, got, ok)
		}
	}

	webp := append([]byte("RIFF\x24\x00\x00\x00WEBPVP8 "), make([]byte, 32)...)
	if got, ok := SniffImage(webp); !ok || got.Ext != "webp" {
		t.Errorf("webp: %+v, %v", got, ok)
	}
	svg := []byte("<?xml version=\"1.0\"?>\n<svg xmlns=\"http://www.w3.org/2000/svg\"/>")
	if got, ok := SniffImage(svg); !ok || got.Ext != "svg" {
		t.Errorf("svg: %+v, %v", got, ok)
	}
	if got, ok := SniffImage([]byte("  <svg/>")); !ok || got.MIME != "image/svg+xml" {
		t.Errorf("bare svg: %+v, %v", got, ok)
	}
}

func TestSniffImageRejectsNonImages(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":  nil,
		"html":   []byte("<!DOCTYPE html><html><body>403 Forbidden</body></html>"),
		"json":   []byte(`{"errCode":-1}`),
		"text":   []byte("hello world"),
		"binary": {0x00, 0x01, 0x02, 0x03},
		"xml":    []byte("<?xml version=\"1.0\"?><error/>"),
	} {
		if got, ok := SniffImage(data); ok {
			t.Errorf("%s: accepted as %+v", name, got)
		}
	}
}

func TestMIMEForFile(t *testing.T) {
	images := encodedImages(t)
	// The original tool called every image ".jpg"; the content decides.
	if got := MIMEForFile("x.jpg", images["png"]); got != "image/png" {
		t.Errorf("png named .jpg: %q", got)
	}
	if got := MIMEForFile("x.png", images["jpg"]); got != "image/jpeg" {
		t.Errorf("jpg named .png: %q", got)
	}
	// Unknown content: fall back to the extension.
	for name, want := range map[string]string{
		"a.png": "image/png", "a.GIF": "image/gif", "a.webp": "image/webp", "a.svg": "image/svg+xml",
		"a.bmp": "image/bmp", "a.jpg": "image/jpeg", "a": "image/jpeg",
	} {
		if got := MIMEForFile(name, []byte("??")); got != want {
			t.Errorf("MIMEForFile(%q) = %q, want %q", name, got, want)
		}
	}
}
