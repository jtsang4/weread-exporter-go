package cli

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtsang4/weread-exporter-go/internal/browser"
	"github.com/jtsang4/weread-exporter-go/internal/fakesite"
	"github.com/jtsang4/weread-exporter-go/internal/weread"
)

// The end-to-end tests drive the whole program, including a real headless
// Chrome, against the fake WeRead site. They are skipped without Chrome and
// with -short.

func requireChrome(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("end-to-end tests are skipped with -short")
	}
	if _, err := browser.FindChrome(""); err != nil {
		t.Skipf("no Chrome available: %v", err)
	}
}

// e2eBook is a three chapter book whose reader pages draw real canvas text.
func e2eBook() fakesite.Book {
	chapter := func(uid int64, title string, lines ...string) fakesite.Chapter {
		screen := []fakesite.Line{fakesite.At("Section of "+title, 24, 40, 100)}
		for i, text := range lines {
			screen = append(screen, fakesite.Body(text, float64(180+i*80)))
		}
		return fakesite.Chapter{UID: uid, Title: title, Screens: [][]fakesite.Line{screen}, LastFooter: "下一章", Words: 200}
	}
	return fakesite.Book{
		ID: bookID, Title: "End to End", Author: "Test Author", Intro: "A book for the tests.",
		Chapters: []fakesite.Chapter{
			chapter(41, "Chapter One", "The first paragraph of chapter one.", "A second paragraph."),
			chapter(42, "Chapter Two", "Chapter two begins here."),
			chapter(43, "Chapter Three", "The last words of the book."),
		},
	}
}

func zipEntry(t *testing.T, path, name string) string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = rc.Close() }()
			data, err := io.ReadAll(rc)
			if err != nil {
				t.Fatal(err)
			}
			return string(data)
		}
	}
	t.Fatalf("%s has no entry %s", path, name)
	return ""
}

func TestEndToEndExport(t *testing.T) {
	requireChrome(t)
	f := newFixture(t)
	f.site.AddBook(e2eBook())
	f.site.SetMode(int(weread.ReaderModeHorizontal)) // the account starts in the paged reader

	args := f.args("-b", "1234567", "--headless", "--no-sandbox", "--yes", "--load-interval", "0",
		"--load-timeout", "30", "--output-dir", f.out, "-o", "epub", "-o", "pdf", "-o", "txt", "-o", "md", "-v")
	code, out, errOut := run(t, args...)
	if code != ExitOK {
		t.Fatalf("exit %d\nstdout: %s\nstderr:\n%s", code, out, errOut)
	}

	want := []string{"End to End.epub", "End to End.pdf", "End to End.txt", "End to End.md"}
	var got []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		got = append(got, filepath.Base(line))
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("stdout lists %v, want %v\n%s", got, want, errOut)
	}

	epub := filepath.Join(f.out, "End to End.epub")
	if first := zipEntry(t, epub, "mimetype"); first != "application/epub+zip" {
		t.Errorf("mimetype = %q", first)
	}
	chapters := ""
	zr, err := zip.OpenReader(epub)
	if err != nil {
		t.Fatal(err)
	}
	for _, zf := range zr.File {
		if strings.HasSuffix(zf.Name, ".xhtml") {
			chapters += zipEntry(t, epub, zf.Name)
		}
	}
	_ = zr.Close()
	for _, text := range []string{"The first paragraph of chapter one.", "Chapter two begins here.", "The last words of the book."} {
		if !strings.Contains(chapters, text) {
			t.Errorf("the EPUB lacks %q", text)
		}
	}

	pdf, err := os.ReadFile(filepath.Join(f.out, "End to End.pdf"))
	if err != nil || !strings.HasPrefix(string(pdf), "%PDF-") || len(pdf) < 2000 {
		t.Errorf("pdf: %d bytes, %v", len(pdf), err)
	}
	if txt, _ := os.ReadFile(filepath.Join(f.out, "End to End.txt")); !strings.Contains(string(txt), "A second paragraph.") {
		t.Errorf("txt = %q", txt)
	}
	md, _ := os.ReadFile(filepath.Join(f.out, "End to End.md"))
	for _, text := range []string{"## Chapter One", "## Section of Chapter Two", "The last words of the book."} {
		if !strings.Contains(string(md), text) {
			t.Errorf("md lacks %q:\n%s", text, md)
		}
	}

	// The account is back in the paged reader and nothing is pending.
	if f.site.Mode() != int(weread.ReaderModeHorizontal) {
		t.Errorf("the account's reader mode is %d after the export", f.site.Mode())
	}
	if got := f.site.ModeChanges(); len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Errorf("mode changes = %v, want [0 1]", got)
	}
	if _, err := os.Stat(filepath.Join(f.cache, "reader-mode-restore.json")); !os.IsNotExist(err) {
		t.Errorf("the restore marker is still there: %v", err)
	}
	if leaked := f.site.Leaked(); len(leaked) != 0 {
		t.Errorf("telemetry reached the server: %v", leaked)
	}

	// Run again: everything is cached and no browser is started.
	hits := f.site.ReaderHits()
	code, out, errOut = run(t, append(args, "--overwrite")...)
	if code != ExitOK {
		t.Fatalf("second run: exit %d\n%s", code, errOut)
	}
	if f.site.ReaderHits() != hits {
		t.Errorf("the second run loaded %d reader pages; the cache should make the browser unnecessary", f.site.ReaderHits()-hits)
	}
	if !strings.Contains(errOut, "every chapter is cached") {
		t.Errorf("stderr:\n%s", errOut)
	}
}

func TestEndToEndResumesAnInterruptedExport(t *testing.T) {
	requireChrome(t)
	f := newFixture(t)
	f.site.AddBook(e2eBook())

	// Chapter 3 is not readable (members only), so the first run stops there.
	paywalled := e2eBook()
	paywalled.Chapters[2].Paywalled = true
	f.site.AddBook(paywalled)
	args := f.args("-b", "1234567", "--headless", "--no-sandbox", "--yes", "--load-interval", "0", "--load-timeout", "30",
		"--output-dir", f.out, "-o", "txt")
	code, _, errOut := run(t, args...)
	if code != ExitFailure || !strings.Contains(errOut, "--chapters") {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	if f.site.Mode() != int(weread.ReaderModeHorizontal) {
		t.Fatal("the reader mode must be restored after the failure")
	}

	// The user buys the book; the export finishes with chapters 1 and 2 from the cache.
	f.site.AddBook(e2eBook())
	hits := f.site.ReaderHits()
	code, out, errOut := run(t, args...)
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	if loaded := f.site.ReaderHits() - hits; loaded != 1 {
		t.Errorf("%d reader pages were loaded, want only the missing chapter\n%s", loaded, errOut)
	}
	txt, _ := os.ReadFile(strings.TrimSpace(out))
	for _, text := range []string{"The first paragraph of chapter one.", "The last words of the book."} {
		if !strings.Contains(string(txt), text) {
			t.Errorf("txt lacks %q", text)
		}
	}
}
