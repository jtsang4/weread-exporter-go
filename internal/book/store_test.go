package book

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore(t.TempDir(), "08232ac0720befa90825d88")
	if err := s.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStoreLayoutMatchesThePythonCache(t *testing.T) {
	s := NewStore("cache", "abc")
	want := map[string]string{
		s.MetaPath():          filepath.Join("cache", "abc", "meta.json"),
		s.CoverPath():         filepath.Join("cache", "abc", "cover.jpg"),
		s.ChaptersDir():       filepath.Join("cache", "abc", "chapters"),
		s.ImagesDir():         filepath.Join("cache", "abc", "images"),
		s.ChapterPath(0, 41):  filepath.Join("cache", "abc", "chapters", "1-41.md"),
		s.ChapterPath(38, 79): filepath.Join("cache", "abc", "chapters", "39-79.md"),
		s.ImagePath("x.jpg"):  filepath.Join("cache", "abc", "images", "x.jpg"),
	}
	for got, w := range want {
		if got != w {
			t.Errorf("got %q, want %q", got, w)
		}
	}
}

func TestStoreMetaRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.LoadMeta(); !errors.Is(err, ErrNoMeta) {
		t.Fatalf("LoadMeta on an empty store: %v", err)
	}

	want := &Meta{Title: "t", Chapters: []Chapter{{ID: 1, Title: "c", Level: 1, Anchors: []Anchor{}}}}
	if err := s.SaveMeta(want); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadMeta()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, err %v", got, err)
	}

	// An empty file (what an interrupted write of the original tool left behind) counts as missing.
	if err := os.WriteFile(s.MetaPath(), []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadMeta(); !errors.Is(err, ErrNoMeta) {
		t.Fatalf("LoadMeta on an empty file: %v", err)
	}
	if err := os.WriteFile(s.MetaPath(), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadMeta(); err == nil || errors.Is(err, ErrNoMeta) {
		t.Fatalf("LoadMeta on a corrupt file: %v", err)
	}
}

func TestStoreChapters(t *testing.T) {
	s := newTestStore(t)
	if s.HasChapter(0, 41) {
		t.Fatal("no chapter captured yet")
	}
	if err := s.SaveChapter(0, 41, "ab\n"); err != nil { // 3 bytes: counts as empty like the original
		t.Fatal(err)
	}
	if s.HasChapter(0, 41) {
		t.Fatal("a chapter of 3 bytes must count as not captured")
	}
	if err := s.SaveChapter(0, 41, "# Title\n"); err != nil {
		t.Fatal(err)
	}
	if !s.HasChapter(0, 41) {
		t.Fatal("chapter should be captured")
	}
	got, err := s.ReadChapter(0, 41)
	if err != nil || got != "# Title\n" {
		t.Fatalf("ReadChapter = %q, %v", got, err)
	}
	if _, err := s.ReadChapter(5, 99); err == nil {
		t.Fatal("reading a missing chapter must fail")
	}
}

func TestStoreRewriteChapterKeepsTheRawCaptureOnce(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveChapter(0, 41, "raw capture"); err != nil {
		t.Fatal(err)
	}
	if err := s.RewriteChapter(0, 41, "first rewrite"); err != nil {
		t.Fatal(err)
	}
	if err := s.RewriteChapter(0, 41, "second rewrite"); err != nil {
		t.Fatal(err)
	}

	if got, _ := s.ReadChapter(0, 41); got != "second rewrite" {
		t.Fatalf("chapter = %q", got)
	}
	backup, err := os.ReadFile(s.ChapterPath(0, 41) + ".bak")
	if err != nil || string(backup) != "raw capture" {
		t.Fatalf("backup = %q, %v: the first rewrite must keep the raw capture, later ones must not replace it", backup, err)
	}
}

func TestStoreIsProcessed(t *testing.T) {
	s := newTestStore(t)
	if s.IsProcessed(0, 41) {
		t.Fatal("a chapter that does not exist cannot be processed")
	}
	if err := s.SaveChapter(0, 41, "raw capture"); err != nil {
		t.Fatal(err)
	}
	if s.IsProcessed(0, 41) {
		t.Fatal("a raw capture is not processed")
	}
	if err := s.RewriteChapter(0, 41, "processed"); err != nil {
		t.Fatal(err)
	}
	if !s.IsProcessed(0, 41) {
		t.Fatal("a rewritten chapter is processed")
	}
}

func TestStoreSaveChapterForgetsTheBackupOfTheReplacedCapture(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveChapter(0, 41, "old capture"); err != nil {
		t.Fatal(err)
	}
	if err := s.RewriteChapter(0, 41, "old, processed"); err != nil {
		t.Fatal(err)
	}

	// The chapter is captured again (for instance after the cached file was deleted).
	if err := s.SaveChapter(0, 41, "new capture"); err != nil {
		t.Fatal(err)
	}
	if s.IsProcessed(0, 41) {
		t.Fatal("the new capture has not been processed; the stale backup must be gone")
	}
	if got, _ := s.ReadChapter(0, 41); got != "new capture" {
		t.Fatalf("chapter = %q", got)
	}
	if err := s.RewriteChapter(0, 41, "new, processed"); err != nil {
		t.Fatal(err)
	}
	backup, _ := os.ReadFile(s.ChapterPath(0, 41) + ".bak")
	if string(backup) != "new capture" {
		t.Fatalf("backup = %q, want the new raw capture", backup)
	}
}

func TestStoreRewriteChapterWithoutChapterFails(t *testing.T) {
	if err := newTestStore(t).RewriteChapter(0, 1, "x"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestStoreImages(t *testing.T) {
	s := newTestStore(t)
	for _, bad := range []string{"", ".", "..", "../x.jpg", "a/b.jpg", `a\b.jpg`} {
		if bad == `a\b.jpg` && filepath.Separator != '\\' {
			continue // a backslash is an ordinary character on Unix
		}
		if err := s.SaveImage(bad, []byte("x")); err == nil {
			t.Errorf("SaveImage(%q) must be rejected", bad)
		}
	}

	if got := s.FindImage("abc"); got != "" {
		t.Fatalf("FindImage on an empty store = %q", got)
	}
	if err := s.SaveImage("abc.png", []byte("png")); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveImage("abd.jpg", []byte("jpg")); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveImage("empty.jpg", nil); err != nil {
		t.Fatal(err)
	}
	if got := s.FindImage("abc"); got != "abc.png" {
		t.Fatalf("FindImage(abc) = %q", got)
	}
	if got := s.FindImage("ab"); got != "" {
		t.Fatalf("FindImage must not match prefixes of the hash: %q", got)
	}
	if got := s.FindImage("empty"); got != "" {
		t.Fatalf("empty files must not count: %q", got)
	}

	if err := os.Mkdir(filepath.Join(s.ImagesDir(), "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.ImagesDir(), ".hidden"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	names, err := s.ListImages()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"abc.png", "abd.jpg", "empty.jpg"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("ListImages = %v, want %v", names, want)
	}
}

func TestStoreListImagesWithoutDirectory(t *testing.T) {
	names, err := NewStore(t.TempDir(), "x").ListImages()
	if err != nil || names != nil {
		t.Fatalf("names=%v err=%v", names, err)
	}
}

func TestStoreCover(t *testing.T) {
	s := newTestStore(t)
	if s.HasCover() {
		t.Fatal("no cover yet")
	}
	if err := s.SaveCover([]byte("jpeg bytes")); err != nil {
		t.Fatal(err)
	}
	if !s.HasCover() {
		t.Fatal("cover should exist")
	}
}
