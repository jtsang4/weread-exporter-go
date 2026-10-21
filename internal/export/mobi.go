package export

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNoMOBIConverter is returned when no program that can produce MOBI is installed.
var ErrNoMOBIConverter = errors.New("no MOBI converter found: install calibre (ebook-convert) or Amazon's kindlegen, or pass --mobi-converter")

// FindMOBIConverter picks the program that converts EPUB to MOBI: the explicit
// path if given, otherwise calibre's ebook-convert, otherwise kindlegen.
func FindMOBIConverter(explicit string) (string, error) {
	if explicit != "" {
		path, err := exec.LookPath(explicit)
		if err != nil {
			return "", fmt.Errorf("mobi converter %q: %w", explicit, err)
		}
		return path, nil
	}
	for _, name := range []string{"ebook-convert", "kindlegen"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	// calibre's macOS bundle does not put its tools on PATH.
	const macBundle = "/Applications/calibre.app/Contents/MacOS/ebook-convert"
	if _, err := os.Stat(macBundle); err == nil {
		return macBundle, nil
	}
	return "", ErrNoMOBIConverter
}

// ConvertMOBI converts an EPUB into a MOBI file using an external converter
// (calibre's ebook-convert or kindlegen; see FindMOBIConverter).
func ConvertMOBI(ctx context.Context, converter, epubPath, mobiPath string, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	var (
		cmd     *exec.Cmd
		produce string // where the converter leaves its result
	)
	switch name := strings.ToLower(strings.TrimSuffix(filepath.Base(converter), filepath.Ext(converter))); {
	case strings.HasPrefix(name, "kindlegen"):
		// kindlegen only takes a bare output name and writes it next to the input.
		produce = filepath.Join(filepath.Dir(epubPath), filepath.Base(mobiPath))
		cmd = exec.CommandContext(ctx, converter, filepath.Base(epubPath), "-o", filepath.Base(mobiPath))
		cmd.Dir = filepath.Dir(epubPath)
	default:
		produce = mobiPath
		cmd = exec.CommandContext(ctx, converter, epubPath, mobiPath)
	}

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	log.Info("converting to MOBI", "converter", filepath.Base(converter))
	err := cmd.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// kindlegen exits with 1 for "finished with warnings"; judge by the result.
	if info, statErr := os.Stat(produce); statErr != nil || info.Size() == 0 {
		if err == nil {
			err = errors.New("the converter produced no output")
		}
		return fmt.Errorf("export: mobi conversion failed: %w\n%s", err, tail(out.String(), 1500))
	}
	if filepath.Clean(produce) != filepath.Clean(mobiPath) {
		if err := moveFile(produce, mobiPath); err != nil {
			return fmt.Errorf("export: move mobi into place: %w", err)
		}
	}
	return nil
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

func moveFile(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(from)
}
