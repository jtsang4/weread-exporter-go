package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jtsang4/weread-exporter-go/internal/book"
	"github.com/jtsang4/weread-exporter-go/internal/browser"
	"github.com/jtsang4/weread-exporter-go/internal/export"
	"github.com/jtsang4/weread-exporter-go/internal/fsutil"
)

// printHTMLName is the page the browser prints to PDF. It lives in the book's
// cache directory so that its relative image links resolve.
const printHTMLName = "book.print.html"

// source describes the captured book for the exporters.
func (e *Exporter) source(bookID string, meta *book.Meta, store *book.Store, positions []int, partial bool) export.Source {
	src := export.Source{BookID: bookID, Meta: meta, Store: store, Logger: e.log}
	if partial {
		src.Select = positions
	}
	return src
}

// outputBase is the file name (without extension) of the exported book. A
// partial export says so, to keep it from being taken for the whole book.
func (e *Exporter) outputBase(meta *book.Meta, partial bool) string {
	base := book.FormatFilename(meta.Title)
	if !partial {
		return base
	}
	spec, err := ParseChapterSpec(e.cfg.Chapters)
	if err != nil {
		return base
	}
	return fmt.Sprintf("%s (chapters %s)", base, spec)
}

// writeOutputs writes every requested format. Files that exist are kept unless
// --overwrite was given; every file appears atomically, complete or not at all.
func (e *Exporter) writeOutputs(ctx context.Context, src export.Source, base string, res *Result) error {
	if err := os.MkdirAll(e.cfg.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create the output directory: %w", err)
	}
	pathOf := func(format string) string { return filepath.Join(e.cfg.OutputDir, base+"."+format) }

	for _, format := range e.cfg.Formats {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := pathOf(format)
		if fsutil.Exists(path) && !e.cfg.Overwrite {
			e.log.Info("output exists; keeping it (use --overwrite to replace it)", "file", path)
			res.Skipped = append(res.Skipped, path)
			continue
		}

		var err error
		switch format {
		case FormatEPUB:
			err = fsutil.WriteAtomic(path, 0o644, func(w io.Writer) error {
				return export.WriteEPUB(w, src, export.EPUBOptions{ExtraCSS: e.extraCSS, Now: e.now()})
			})
		case FormatPDF:
			err = e.writePDF(ctx, src, path)
		case FormatMOBI:
			err = e.writeMOBI(ctx, pathOf(FormatEPUB), path)
		case FormatTXT:
			err = fsutil.WriteAtomic(path, 0o644, func(w io.Writer) error { return export.WriteTXT(w, src) })
		case FormatMarkdown:
			err = export.WriteMarkdown(path, src)
		default:
			err = fmt.Errorf("unsupported output format %q", format)
		}
		if err != nil {
			return fmt.Errorf("write %s: %w", filepath.Base(path), err)
		}
		e.log.Info("wrote", "file", path)
		res.Files = append(res.Files, path)
	}
	return nil
}

// writePDF renders the book to HTML, lets a headless Chrome print it and
// stores the PDF.
func (e *Exporter) writePDF(ctx context.Context, src export.Source, path string) error {
	html, err := export.PDFHTML(src, export.PDFOptions{ExtraCSS: e.extraCSS})
	if err != nil {
		return err
	}
	htmlPath := filepath.Join(src.Store.Dir, printHTMLName)
	if err := fsutil.WriteFileAtomic(htmlPath, []byte(html), 0o644); err != nil {
		return err
	}
	defer func() { _ = os.Remove(htmlPath) }()

	e.log.Info("printing the book to PDF; this takes a while for long books")
	pdf, err := e.printPDF(ctx, htmlPath, browser.PDFOptions{
		ChromePath: e.cfg.ChromePath,
		NoSandbox:  e.cfg.NoSandbox,
		Logger:     e.log,
	})
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, pdf, 0o644)
}

// writeMOBI converts the EPUB into MOBI with an external program. The result
// is built under a temporary name (keeping the extension, which selects the
// converter's output format) and moved into place when it is complete.
func (e *Exporter) writeMOBI(ctx context.Context, epubPath, path string) error {
	if e.mobiConverter == "" {
		return export.ErrNoMOBIConverter
	}
	if !fsutil.IsFile(epubPath) {
		return fmt.Errorf("the EPUB %s is missing; MOBI is converted from it", epubPath)
	}
	tmp := path[:len(path)-len(filepath.Ext(path))] + ".part" + filepath.Ext(path)
	_ = os.Remove(tmp)
	defer func() { _ = os.Remove(tmp) }()
	if err := export.ConvertMOBI(ctx, e.mobiConverter, epubPath, tmp, e.log); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
