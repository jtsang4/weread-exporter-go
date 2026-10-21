// Package fsutil holds small file-system helpers shared by the exporter.
package fsutil

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to path so that readers never observe a partially
// written file: the data goes to a temporary file in the same directory which
// is then renamed over path.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	return WriteAtomic(path, perm, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// WriteAtomic is WriteFileAtomic for streamed content. The destination is only
// touched when write returns nil.
func WriteAtomic(path string, perm os.FileMode, write func(w io.Writer) error) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if err = write(tmp); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmpName, path, err)
	}
	return nil
}

// Exists reports whether path exists (file or directory).
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsFile reports whether path exists and is a regular file.
func IsFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

// IsDir reports whether path exists and is a directory.
func IsDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// Size returns the size of the file at path, or -1 if it cannot be stat'ed.
func Size(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return st.Size()
}

// ReadFileIfExists returns the content of path, or (nil, nil) if it does not exist.
func ReadFileIfExists(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return data, err
}
