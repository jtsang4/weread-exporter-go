package markdown

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type memStore struct {
	mu    sync.Mutex
	files map[string][]byte
}

func newMemStore() *memStore { return &memStore{files: map[string][]byte{}} }

func (m *memStore) SaveImage(name string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[name] = data
	return nil
}

func (m *memStore) FindImage(prefix string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name := range m.files {
		if strings.HasPrefix(name, prefix+".") {
			return name
		}
	}
	return ""
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestImageNameIsTheMD5OfTheURL(t *testing.T) {
	// echo -n https://a/b.png | md5
	if got := ImageName("https://a/b.png"); len(got) != 32 || got != ImageName("https://a/b.png") || got == ImageName("https://a/c.png") {
		t.Fatalf("ImageName = %q", got)
	}
	if got := ImageName(""); got != "d41d8cd98f00b204e9800998ecf8427e" {
		t.Fatalf("md5 of the empty string = %q", got)
	}
}

func TestLocalizeImagesRewritesLinksAndStoresFiles(t *testing.T) {
	data := pngBytes(t)
	var fetched []string
	fetch := func(_ context.Context, url string) ([]byte, error) {
		fetched = append(fetched, url)
		return data, nil
	}
	store := newMemStore()

	in := "text\n\n![](https://cdn.example.com/a.png)\n\nmiddle [link](https://example.com/page) and ![x](https://cdn.example.com/b.png)\n"
	out, stats := LocalizeImages(context.Background(), in, fetch, store, quiet)

	nameA := ImageName("https://cdn.example.com/a.png") + ".png"
	nameB := ImageName("https://cdn.example.com/b.png") + ".png"
	nameLink := ImageName("https://example.com/page") + ".png"
	want := "text\n\n![](images/" + nameA + ")\n\nmiddle [link](images/" + nameLink + ") and ![x](images/" + nameB + ")\n"
	if out != want {
		t.Fatalf("out = %q\nwant  %q", out, want)
	}
	if stats.Downloaded != 3 || stats.Failed != 0 || stats.Reused != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if len(fetched) != 3 || len(store.files) != 3 {
		t.Fatalf("fetched=%v stored=%d", fetched, len(store.files))
	}
}

func TestLocalizeImagesReusesStoredImages(t *testing.T) {
	url := "https://cdn.example.com/a.png"
	store := newMemStore()
	_ = store.SaveImage(ImageName(url)+".gif", []byte("earlier download"))

	fetch := func(context.Context, string) ([]byte, error) {
		t.Fatal("must not download an image that is already stored")
		return nil, nil
	}
	out, stats := LocalizeImages(context.Background(), "![]("+url+")\n", fetch, store, quiet)
	if want := "![](images/" + ImageName(url) + ".gif)\n"; out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
	if stats.Reused != 1 || stats.Downloaded != 0 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestLocalizeImagesKeepsRemoteLinkOnFailure(t *testing.T) {
	good := pngBytes(t)
	fetch := func(_ context.Context, url string) ([]byte, error) {
		switch {
		case strings.HasSuffix(url, "/bad.png"):
			return nil, errors.New("HTTP 500")
		case strings.HasSuffix(url, "/html.png"):
			return []byte("<html>forbidden</html>"), nil
		}
		return good, nil
	}
	store := newMemStore()
	in := "![](https://x/bad.png)\n![](https://x/html.png)\n![](https://x/ok.png)\n"
	out, stats := LocalizeImages(context.Background(), in, fetch, store, quiet)

	want := "![](https://x/bad.png)\n![](https://x/html.png)\n![](images/" + ImageName("https://x/ok.png") + ".png)\n"
	if out != want {
		t.Fatalf("out = %q\nwant  %q", out, want)
	}
	if stats.Failed != 2 || stats.Downloaded != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestLocalizeImagesLeavesPlainTextAndHTTPLinksAlone(t *testing.T) {
	fetch := func(context.Context, string) ([]byte, error) {
		t.Fatal("nothing to download here")
		return nil, nil
	}
	in := "no images\n[http link](http://example.com/x)\nunterminated ](https://example.com/x\n"
	out, stats := LocalizeImages(context.Background(), in, fetch, newMemStore(), quiet)
	if out != in || stats != (LocalizeStats{}) {
		t.Fatalf("out = %q, stats = %+v", out, stats)
	}
}

func TestLocalizeImagesStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fetch := func(context.Context, string) ([]byte, error) {
		t.Fatal("a cancelled context must not start downloads")
		return nil, nil
	}
	in := "![](https://x/a.png)\n"
	out, stats := LocalizeImages(ctx, in, fetch, newMemStore(), quiet)
	if out != in || stats.Failed != 1 {
		t.Fatalf("out = %q, stats = %+v", out, stats)
	}
}
