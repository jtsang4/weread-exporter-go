package markdown

import (
	"context"
	"crypto/md5" //nolint:gosec // cache file names, not security
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jtsang4/weread-exporter-go/internal/book"
)

// remoteImageMarker is what the tool looks for to find images that still point
// at the web: "](https://…)" of a Markdown image or link.
const remoteImageMarker = "](https://"

// ImageFetcher downloads one image.
type ImageFetcher func(ctx context.Context, url string) ([]byte, error)

// ImageStore persists localised images and lets the localiser reuse earlier downloads.
type ImageStore interface {
	// SaveImage stores data under name in the book's image directory.
	SaveImage(name string, data []byte) error
	// FindImage returns the name of an already stored image whose file name
	// starts with prefix + ".", or "" if there is none.
	FindImage(prefix string) string
}

// LocalizeStats reports what LocalizeImages did.
type LocalizeStats struct {
	Downloaded int
	Reused     int
	Failed     int
}

// ImageName returns the cache file name (without extension) for an image URL.
func ImageName(url string) string {
	sum := md5.Sum([]byte(url)) //nolint:gosec // see import comment
	return hex.EncodeToString(sum[:])
}

// LocalizeImages downloads every "](https://…)" target of the Markdown into the
// image store and rewrites the link to "images/<md5(url)>.<ext>", like the
// original tool. Images that cannot be fetched are logged and left pointing at
// the web. Images already present in the store are reused, so re-running the
// export does not hit the network again.
func LocalizeImages(ctx context.Context, text string, fetch ImageFetcher, store ImageStore, log *slog.Logger) (string, LocalizeStats) {
	if log == nil {
		log = slog.Default()
	}
	var stats LocalizeStats
	out := text
	pos := 0
	for pos < len(out) {
		idx := strings.Index(out[pos:], remoteImageMarker)
		if idx < 0 {
			break
		}
		pos += idx
		end := strings.IndexByte(out[pos:], ')')
		if end < 0 {
			break
		}
		end += pos
		url := out[pos+2 : end]

		name, err := localizeOne(ctx, url, fetch, store, &stats, log)
		if err != nil {
			log.Warn("image not localised, keeping the remote link", "url", url, "error", err)
			stats.Failed++
			pos += len(remoteImageMarker)
			continue
		}
		replacement := "images/" + name
		out = out[:pos+2] + replacement + out[end:]
		pos += 2 + len(replacement)
	}
	return out, stats
}

func localizeOne(ctx context.Context, url string, fetch ImageFetcher, store ImageStore, stats *LocalizeStats, log *slog.Logger) (string, error) {
	prefix := ImageName(url)
	if existing := store.FindImage(prefix); existing != "" {
		stats.Reused++
		return existing, nil
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}
	log.Info("downloading image", "url", url)
	data, err := fetch(ctx, url)
	if err != nil {
		return "", err
	}
	kind, ok := book.SniffImage(data)
	if !ok {
		return "", fmt.Errorf("response is not an image (%d bytes)", len(data))
	}
	name := prefix + "." + kind.Ext
	if err := store.SaveImage(name, data); err != nil {
		return "", err
	}
	stats.Downloaded++
	return name, nil
}
