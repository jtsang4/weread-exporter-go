package fsutil

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestWriteFileAtomicCreatesDirectoriesAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "out.txt")
	if err := WriteFileAtomic(path, []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "hello" {
		t.Fatalf("content = %q, err = %v", got, err)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(path); st.Mode().Perm() != 0o640 {
			t.Fatalf("mode = %v", st.Mode().Perm())
		}
	}
	if names := listDir(t, filepath.Dir(path)); len(names) != 1 || names[0] != "out.txt" {
		t.Fatalf("directory holds %v", names)
	}
}

func TestWriteFileAtomicReplacesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	for _, content := range []string{"first", "second, longer"} {
		if err := WriteFileAtomic(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(path); string(got) != content {
			t.Fatalf("content = %q, want %q", got, content)
		}
	}
}

func TestWriteAtomicKeepsOldContentWhenWriterFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := WriteFileAtomic(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	err := WriteAtomic(path, 0o644, func(w io.Writer) error {
		_, _ = w.Write([]byte("partial garbage"))
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "original" {
		t.Fatalf("content = %q: a failed write must not touch the destination", got)
	}
	if names := listDir(t, dir); len(names) != 1 {
		t.Fatalf("temporary files left behind: %v", names)
	}
}

func TestPredicatesAndSize(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing")

	if !Exists(file) || !Exists(dir) || Exists(missing) {
		t.Fatal("Exists")
	}
	if !IsFile(file) || IsFile(dir) || IsFile(missing) {
		t.Fatal("IsFile")
	}
	if IsDir(file) || !IsDir(dir) || IsDir(missing) {
		t.Fatal("IsDir")
	}
	if Size(file) != 5 || Size(missing) != -1 {
		t.Fatalf("Size = %d / %d", Size(file), Size(missing))
	}
}

func TestReadFileIfExists(t *testing.T) {
	dir := t.TempDir()
	if data, err := ReadFileIfExists(filepath.Join(dir, "missing")); data != nil || err != nil {
		t.Fatalf("missing: %v %v", data, err)
	}
	file := filepath.Join(dir, "f")
	_ = os.WriteFile(file, []byte("x"), 0o644)
	if data, err := ReadFileIfExists(file); string(data) != "x" || err != nil {
		t.Fatalf("existing: %q %v", data, err)
	}
	if _, err := ReadFileIfExists(dir); err == nil {
		t.Fatal("reading a directory must fail")
	}
}
