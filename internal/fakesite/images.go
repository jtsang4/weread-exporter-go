package fakesite

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"sync"
)

var (
	imageOnce sync.Once
	pngData   []byte
	jpegData  []byte
)

func buildImages() {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for x := 0; x < 16; x++ {
		for y := 0; y < 16; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 16), G: uint8(y * 16), B: 128, A: 255})
		}
	}
	var p, j bytes.Buffer
	if err := png.Encode(&p, img); err != nil {
		panic(err)
	}
	if err := jpeg.Encode(&j, img, nil); err != nil {
		panic(err)
	}
	pngData, jpegData = p.Bytes(), j.Bytes()
}

// PNG is a small valid PNG image.
func PNG() []byte {
	imageOnce.Do(buildImages)
	return append([]byte(nil), pngData...)
}

// JPEG is a small valid JPEG image.
func JPEG() []byte {
	imageOnce.Do(buildImages)
	return append([]byte(nil), jpegData...)
}
