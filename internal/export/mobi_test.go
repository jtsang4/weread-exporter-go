package export

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts are not available on Windows")
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFindMOBIConverter(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "my-converter", "exit 0\n")
	if got, err := FindMOBIConverter(script); err != nil || got != script {
		t.Fatalf("explicit converter: %q, %v", got, err)
	}
	if _, err := FindMOBIConverter(filepath.Join(dir, "does-not-exist")); err == nil {
		t.Fatal("a missing explicit converter must be an error")
	}

	t.Setenv("PATH", dir) // only our directory: neither ebook-convert nor kindlegen
	if runtime.GOOS == "darwin" {
		if _, err := os.Stat("/Applications/calibre.app/Contents/MacOS/ebook-convert"); err == nil {
			t.Skip("calibre is installed in /Applications")
		}
	}
	if _, err := FindMOBIConverter(""); !errors.Is(err, ErrNoMOBIConverter) {
		t.Fatalf("err = %v, want ErrNoMOBIConverter", err)
	}

	writeScript(t, dir, "ebook-convert", "exit 0\n")
	got, err := FindMOBIConverter("")
	if err != nil || filepath.Base(got) != "ebook-convert" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestConvertMOBIWithEbookConvert(t *testing.T) {
	dir := t.TempDir()
	// ebook-convert <input> <output>
	conv := writeScript(t, dir, "ebook-convert", `cp "$1" "$2"`+"\n")
	epub := filepath.Join(dir, "in.epub")
	mobi := filepath.Join(dir, "out", "book.mobi")
	if err := os.WriteFile(epub, []byte("epub-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(mobi), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ConvertMOBI(context.Background(), conv, epub, mobi, quietLogger); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(mobi); string(got) != "epub-bytes" {
		t.Fatalf("mobi = %q", got)
	}
}

func TestConvertMOBIWithKindlegenWarningExitCode(t *testing.T) {
	dir := t.TempDir()
	// kindlegen <input> -o <name>: writes <name> next to the input and exits 1 on warnings.
	conv := writeScript(t, dir, "kindlegen", `cp "$1" "$3"; exit 1`+"\n")
	epub := filepath.Join(dir, "in", "in.epub")
	if err := os.MkdirAll(filepath.Dir(epub), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(epub, []byte("epub-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	mobi := filepath.Join(dir, "out", "book.mobi")
	if err := os.MkdirAll(filepath.Dir(mobi), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ConvertMOBI(context.Background(), conv, epub, mobi, quietLogger); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(mobi); string(got) != "epub-bytes" {
		t.Fatalf("mobi = %q", got)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(epub), "book.mobi")); err == nil {
		t.Fatal("kindlegen's intermediate output was not moved away")
	}
}

func TestConvertMOBIReportsFailures(t *testing.T) {
	dir := t.TempDir()
	conv := writeScript(t, dir, "ebook-convert", `echo "conversion exploded" >&2; exit 3`+"\n")
	epub := filepath.Join(dir, "in.epub")
	_ = os.WriteFile(epub, []byte("x"), 0o644)
	err := ConvertMOBI(context.Background(), conv, epub, filepath.Join(dir, "out.mobi"), quietLogger)
	if err == nil || !strings.Contains(err.Error(), "conversion exploded") {
		t.Fatalf("err = %v", err)
	}

	silent := writeScript(t, dir, "ebook-convert2", "exit 0\n") // "succeeds" without output
	err = ConvertMOBI(context.Background(), silent, epub, filepath.Join(dir, "none.mobi"), quietLogger)
	if err == nil || !strings.Contains(err.Error(), "no output") {
		t.Fatalf("err = %v", err)
	}
}

func TestConvertMOBIHonoursCancellation(t *testing.T) {
	dir := t.TempDir()
	conv := writeScript(t, dir, "ebook-convert", "sleep 30\n")
	epub := filepath.Join(dir, "in.epub")
	_ = os.WriteFile(epub, []byte("x"), 0o644)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ConvertMOBI(ctx, conv, epub, filepath.Join(dir, "out.mobi"), quietLogger); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
