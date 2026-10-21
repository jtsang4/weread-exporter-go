package book

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jtsang4/weread-exporter-go/internal/fsutil"
)

// ErrNoMeta is returned by Store.LoadMeta when no (non-empty) meta.json exists.
var ErrNoMeta = errors.New("book: no cached meta.json")

// minChapterBytes is the size a cached chapter must exceed to count as captured
// (the original tool treated files of up to 3 bytes as empty).
const minChapterBytes = 3

// backupSuffix marks the raw capture a processed chapter was made from.
const backupSuffix = ".bak"

// Store is the cache directory of one book.
type Store struct {
	Dir string
}

// NewStore returns the store of bookID below cacheDir.
func NewStore(cacheDir, bookID string) *Store {
	return &Store{Dir: filepath.Join(cacheDir, bookID)}
}

// MetaPath is the path of meta.json.
func (s *Store) MetaPath() string { return filepath.Join(s.Dir, "meta.json") }

// CoverPath is the path of the downloaded cover image.
func (s *Store) CoverPath() string { return filepath.Join(s.Dir, "cover.jpg") }

// ChaptersDir holds one Markdown file per chapter.
func (s *Store) ChaptersDir() string { return filepath.Join(s.Dir, "chapters") }

// ImagesDir holds the localised chapter images.
func (s *Store) ImagesDir() string { return filepath.Join(s.Dir, "images") }

// ChapterPath is the Markdown file of the chapter at position index (0-based).
func (s *Store) ChapterPath(index int, chapterID int64) string {
	return filepath.Join(s.ChaptersDir(), fmt.Sprintf("%d-%d.md", index+1, chapterID))
}

// LoadMeta reads meta.json; it returns ErrNoMeta if it is missing or empty.
func (s *Store) LoadMeta() (*Meta, error) {
	data, err := os.ReadFile(s.MetaPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoMeta
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil, ErrNoMeta
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("book: parse %s: %w", s.MetaPath(), err)
	}
	return &m, nil
}

// SaveMeta writes meta.json atomically.
func (s *Store) SaveMeta(m *Meta) error {
	data, err := EncodeMeta(m)
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(s.MetaPath(), data, 0o644)
}

// HasChapter reports whether the chapter has already been captured.
func (s *Store) HasChapter(index int, chapterID int64) bool {
	return fsutil.Size(s.ChapterPath(index, chapterID)) > minChapterBytes
}

// ReadChapter returns the Markdown of a captured chapter.
func (s *Store) ReadChapter(index int, chapterID int64) (string, error) {
	data, err := os.ReadFile(s.ChapterPath(index, chapterID))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SaveChapter stores freshly captured Markdown. A backup left by an earlier
// RewriteChapter belongs to the replaced capture, so it is dropped: the new
// capture has not been post-processed yet.
func (s *Store) SaveChapter(index int, chapterID int64, markdown string) error {
	path := s.ChapterPath(index, chapterID)
	if err := fsutil.WriteFileAtomic(path, []byte(markdown), 0o644); err != nil {
		return err
	}
	if err := os.Remove(path + backupSuffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// IsProcessed reports whether the chapter went through RewriteChapter, that is
// whether its post-processed Markdown replaced the raw capture (which is kept
// next to it as "<chapter>.md.bak").
func (s *Store) IsProcessed(index int, chapterID int64) bool {
	return fsutil.IsFile(s.ChapterPath(index, chapterID) + backupSuffix)
}

// RewriteChapter replaces a chapter's Markdown with a processed version. The
// first time, the untouched capture is preserved as "<chapter>.md.bak".
func (s *Store) RewriteChapter(index int, chapterID int64, markdown string) error {
	path := s.ChapterPath(index, chapterID)
	backup := path + backupSuffix
	if !fsutil.IsFile(backup) {
		if err := os.Rename(path, backup); err != nil {
			return fmt.Errorf("book: back up %s: %w", path, err)
		}
	}
	return fsutil.WriteFileAtomic(path, []byte(markdown), 0o644)
}

// SaveImage stores a localised image.
func (s *Store) SaveImage(name string, data []byte) error {
	if name != filepath.Base(name) || name == "" || name == "." || name == ".." {
		return fmt.Errorf("book: invalid image name %q", name)
	}
	return fsutil.WriteFileAtomic(filepath.Join(s.ImagesDir(), name), data, 0o644)
}

// ImagePath is the path of a localised image.
func (s *Store) ImagePath(name string) string { return filepath.Join(s.ImagesDir(), name) }

// FindImage returns the name of a stored, non-empty image called "<prefix>.<ext>"
// (any extension), or "" if there is none.
func (s *Store) FindImage(prefix string) string {
	matches, err := filepath.Glob(filepath.Join(s.ImagesDir(), prefix+".*"))
	if err != nil {
		return ""
	}
	sort.Strings(matches)
	for _, m := range matches {
		if fsutil.Size(m) > 0 {
			return filepath.Base(m)
		}
	}
	return ""
}

// ListImages returns the sorted names of the files in the images directory.
func (s *Store) ListImages() ([]string, error) {
	entries, err := os.ReadDir(s.ImagesDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// HasCover reports whether a cover image has been downloaded.
func (s *Store) HasCover() bool { return fsutil.Size(s.CoverPath()) > 0 }

// SaveCover stores the cover image.
func (s *Store) SaveCover(data []byte) error {
	return fsutil.WriteFileAtomic(s.CoverPath(), data, 0o644)
}

// EnsureDirs creates the chapter and image directories.
func (s *Store) EnsureDirs() error {
	for _, dir := range []string{s.Dir, s.ChaptersDir(), s.ImagesDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}
