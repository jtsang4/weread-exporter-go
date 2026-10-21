package export

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/jtsang4/weread-exporter-go/internal/assets"
	"github.com/jtsang4/weread-exporter-go/internal/book"
	"github.com/jtsang4/weread-exporter-go/internal/markdown"
)

const (
	epubMimetype = "application/epub+zip"
	epubLang     = "zh-CN"
	epubRoot     = "EPUB/" // directory of the package inside the container
	xmlHeader    = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"
)

// EPUBOptions tunes WriteEPUB.
type EPUBOptions struct {
	// ExtraCSS is appended to the built-in style sheet.
	ExtraCSS string
	// Now is the modification time recorded in the package; zero means time.Now().
	Now time.Time
}

type epubChapter struct {
	file   string
	doc    string
	remote bool // references images on the web (needs the remote-resources property)
}

type epubImage struct {
	cacheName string // file name in the book's images directory
	zipName   string // file name inside the EPUB (extension follows the content)
	mime      string
	used      bool
}

// WriteEPUB writes the book as an EPUB 3 package (with an NCX for older readers).
//
// The container is assembled by hand: the "mimetype" entry is stored first,
// uncompressed and without extra fields as the OCF specification demands.
// Chapters are converted from Markdown to well-formed XHTML, sub-headings listed
// in the book's table of contents get anchors, and the table of contents is
// written as a nested navigation document and NCX.
func WriteEPUB(w io.Writer, src Source, opts EPUBOptions) error {
	if err := src.Validate(); err != nil {
		return err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC().Truncate(time.Second)
	meta := src.Meta
	log := src.logger()

	images, err := inventoryImages(src.Store)
	if err != nil {
		return err
	}

	// Convert the chapters first: only then do we know which images are used.
	chapters := make([]epubChapter, len(meta.Chapters))
	tocInput := make([]chapterTOC, len(meta.Chapters))
	for i, ch := range meta.Chapters {
		md, err := src.chapterMarkdown(i)
		if err != nil {
			return err
		}
		html, err := markdown.ToHTML(md)
		if err != nil {
			return fmt.Errorf("export: convert chapter %d %q: %w", i+1, ch.Title, err)
		}
		nodes, err := markdown.ParseFragment(html)
		if err != nil {
			return fmt.Errorf("export: parse chapter %d %q: %w", i+1, ch.Title, err)
		}

		titles := make([]string, len(ch.Anchors))
		for j, a := range ch.Anchors {
			titles[j] = a.Title
		}
		ids := markdown.AssignAnchorIDs(nodes, titles, "t")

		nodes = markdown.RewriteImages(nodes, func(srcAttr string) (string, bool) {
			name, local := strings.CutPrefix(srcAttr, "images/")
			if !local {
				return srcAttr, true // remote image or something unexpected: leave it alone
			}
			img, ok := images[name]
			if !ok {
				log.Warn("chapter refers to an image that is not in the cache; dropping it",
					"chapter", ch.Title, "image", name)
				return "", false
			}
			img.used = true
			return "images/" + img.zipName, true
		})

		file := fmt.Sprintf("chap_%04d.xhtml", i+1)
		chapters[i] = epubChapter{
			file:   file,
			doc:    xhtmlDocument(ch.Title, markdown.SerializeXHTML(nodes), ""),
			remote: markdown.HasRemoteImages(nodes),
		}
		anchors := make([]anchorTOC, 0, len(ch.Anchors))
		for j, a := range ch.Anchors {
			if strings.TrimSpace(a.Title) == "" {
				continue
			}
			anchors = append(anchors, anchorTOC{Title: a.Title, ID: ids[j]})
		}
		tocInput[i] = chapterTOC{File: file, Title: displayTitle(ch.Title, i), Level: ch.Level, Anchors: anchors}
	}
	toc := buildTOC(tocInput)

	cover := loadCover(src)
	identifier := "urn:uuid:" + uuidV5(namespaceURL, "https://weread.qq.com/web/bookDetail/"+src.BookID)

	zw := zip.NewWriter(w)
	if err := writeMimetype(zw); err != nil {
		return err
	}
	put := func(name string, data string) error { return putFile(zw, now, name, []byte(data)) }

	if err := put("META-INF/container.xml", containerXML); err != nil {
		return err
	}
	css := assets.EPUBCSS
	if strings.TrimSpace(opts.ExtraCSS) != "" {
		css += "\n" + opts.ExtraCSS
	}
	if err := put(epubRoot+"style/default.css", css); err != nil {
		return err
	}

	var usedImages []*epubImage
	for _, img := range images {
		if img.used {
			usedImages = append(usedImages, img)
		}
	}
	sort.Slice(usedImages, func(i, j int) bool { return usedImages[i].zipName < usedImages[j].zipName })

	if cover != nil {
		coverPage := xhtmlDocument(meta.Title, fmt.Sprintf(
			`<div class="cover" style="text-align:center;"><img src="images/%s" alt="%s" style="max-width:100%%;height:auto;" /></div>`,
			cover.zipName, escapeAttr("封面")), ` epub:type="cover"`)
		if err := put(epubRoot+"cover.xhtml", coverPage); err != nil {
			return err
		}
		if err := putFile(zw, now, epubRoot+"images/"+cover.zipName, cover.data); err != nil {
			return err
		}
	}

	if err := put(epubRoot+"nav.xhtml", navDocument(meta.Title, toc, cover != nil, chapters[0].file)); err != nil {
		return err
	}
	if err := put(epubRoot+"toc.ncx", ncxDocument(meta.Title, identifier, toc)); err != nil {
		return err
	}
	for _, ch := range chapters {
		if err := put(epubRoot+ch.file, ch.doc); err != nil {
			return err
		}
	}
	for _, img := range usedImages {
		if err := copyImage(zw, now, src.Store, img); err != nil {
			return err
		}
	}
	if err := put(epubRoot+"content.opf", packageDocument(src, identifier, now, chapters, usedImages, cover)); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("export: finish epub: %w", err)
	}
	return nil
}

const containerXML = xmlHeader + `<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="` + epubRoot + `content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>
`

// displayTitle never returns an empty title: navigation entries need a label.
func displayTitle(title string, index int) string {
	if strings.TrimSpace(title) == "" {
		return fmt.Sprintf("%d", index+1)
	}
	return title
}

// writeMimetype stores the "mimetype" entry the way OCF requires: first in the
// archive, not compressed, no extra field, no data descriptor.
func writeMimetype(zw *zip.Writer) error {
	data := []byte(epubMimetype)
	fw, err := zw.CreateRaw(&zip.FileHeader{
		Name:               "mimetype",
		Method:             zip.Store,
		CRC32:              crc32.ChecksumIEEE(data),
		CompressedSize64:   uint64(len(data)),
		UncompressedSize64: uint64(len(data)),
	})
	if err != nil {
		return fmt.Errorf("export: write mimetype: %w", err)
	}
	_, err = fw.Write(data)
	return err
}

func putFile(zw *zip.Writer, now time.Time, name string, data []byte) error {
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: now})
	if err != nil {
		return fmt.Errorf("export: add %s: %w", name, err)
	}
	if _, err := fw.Write(data); err != nil {
		return fmt.Errorf("export: write %s: %w", name, err)
	}
	return nil
}

func copyImage(zw *zip.Writer, now time.Time, store *book.Store, img *epubImage) error {
	f, err := os.Open(store.ImagePath(img.cacheName))
	if err != nil {
		return fmt.Errorf("export: open image: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Images are already compressed formats.
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: epubRoot + "images/" + img.zipName, Method: zip.Store, Modified: now})
	if err != nil {
		return fmt.Errorf("export: add image %s: %w", img.zipName, err)
	}
	if _, err := io.Copy(fw, f); err != nil {
		return fmt.Errorf("export: write image %s: %w", img.zipName, err)
	}
	return nil
}

// inventoryImages lists the cached images and works out each one's real media
// type. Caches written by the original tool name every image ".jpg" whatever its
// format; EPUB validators (and some readers) insist that extension and media
// type agree, so such files are stored under their proper extension.
func inventoryImages(store *book.Store) (map[string]*epubImage, error) {
	names, err := store.ListImages()
	if err != nil {
		return nil, fmt.Errorf("export: list images: %w", err)
	}
	images := make(map[string]*epubImage, len(names))
	for _, name := range names {
		head, err := readHead(store.ImagePath(name), 2048)
		if err != nil {
			return nil, fmt.Errorf("export: read image %s: %w", name, err)
		}
		if len(head) == 0 {
			continue // empty file: nothing to embed
		}
		img := &epubImage{cacheName: name, zipName: name, mime: book.MIMEForFile(name, head)}
		if kind, ok := book.SniffImage(head); ok {
			ext := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
			if ext != kind.Ext && !(ext == "jpeg" && kind.Ext == "jpg") {
				img.zipName = strings.TrimSuffix(name, path.Ext(name)) + "." + kind.Ext
			}
		}
		images[name] = img
	}
	return images, nil
}

func readHead(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, n)
	got, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	return buf[:got], nil
}

type epubCover struct {
	zipName string
	mime    string
	data    []byte
}

// loadCover reads the cached cover; a missing or broken cover is not fatal.
func loadCover(src Source) *epubCover {
	data, err := os.ReadFile(src.Store.CoverPath())
	if err != nil || len(data) == 0 {
		src.logger().Warn("book has no cover image; continuing without one")
		return nil
	}
	kind, ok := book.SniffImage(data)
	if !ok {
		src.logger().Warn("cached cover is not an image; continuing without one")
		return nil
	}
	return &epubCover{zipName: "cover." + kind.Ext, mime: kind.MIME, data: data}
}

func escapeText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func escapeAttr(s string) string { return escapeText(s) }

// xhtmlDocument wraps a body fragment into a complete XHTML document that links
// the book's style sheet. bodyAttrs is inserted into the <body> tag.
func xhtmlDocument(title, body, bodyAttrs string) string {
	var b strings.Builder
	b.WriteString(xmlHeader)
	b.WriteString("<!DOCTYPE html>\n")
	b.WriteString(`<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" lang="` + epubLang + `" xml:lang="` + epubLang + `">` + "\n")
	b.WriteString("<head>\n<meta charset=\"utf-8\" />\n<title>")
	b.WriteString(escapeText(title))
	b.WriteString("</title>\n<link rel=\"stylesheet\" type=\"text/css\" href=\"style/default.css\" />\n</head>\n<body")
	b.WriteString(bodyAttrs)
	b.WriteString(">\n")
	b.WriteString(body)
	b.WriteString("\n</body>\n</html>\n")
	return b.String()
}

func navDocument(title string, toc []*tocEntry, hasCover bool, firstChapter string) string {
	var list strings.Builder
	writeNavList(&list, toc, 1)

	var landmarks strings.Builder
	landmarks.WriteString("<ol>\n")
	if hasCover {
		landmarks.WriteString(`<li><a epub:type="cover" href="cover.xhtml">封面</a></li>` + "\n")
	}
	landmarks.WriteString(`<li><a epub:type="bodymatter" href="` + escapeAttr(firstChapter) + `">正文</a></li>` + "\n</ol>")

	body := `<nav epub:type="toc" id="toc" role="doc-toc">` + "\n<h1>目录</h1>\n" + list.String() + "</nav>\n" +
		`<nav epub:type="landmarks" id="landmarks" hidden="hidden">` + "\n<h2>导航</h2>\n" + landmarks.String() + "\n</nav>"
	return xhtmlDocument(title, body, "")
}

func writeNavList(b *strings.Builder, entries []*tocEntry, depth int) {
	indent := strings.Repeat("  ", depth)
	b.WriteString(indent + "<ol>\n")
	for _, e := range entries {
		b.WriteString(indent + `  <li><a href="` + escapeAttr(e.Href) + `">` + escapeText(e.Title) + "</a>")
		if len(e.Children) > 0 {
			b.WriteString("\n")
			writeNavList(b, e.Children, depth+2)
			b.WriteString(indent + "  ")
		}
		b.WriteString("</li>\n")
	}
	b.WriteString(indent + "</ol>\n")
}

func ncxDocument(title, identifier string, toc []*tocEntry) string {
	var b strings.Builder
	b.WriteString(xmlHeader)
	b.WriteString(`<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1" xml:lang="` + epubLang + `">` + "\n<head>\n")
	b.WriteString(`<meta name="dtb:uid" content="` + escapeAttr(identifier) + `"/>` + "\n")
	b.WriteString(fmt.Sprintf(`<meta name="dtb:depth" content="%d"/>`+"\n", max(tocDepth(toc), 1)))
	b.WriteString(`<meta name="dtb:totalPageCount" content="0"/>` + "\n")
	b.WriteString(`<meta name="dtb:maxPageNumber" content="0"/>` + "\n</head>\n")
	b.WriteString("<docTitle><text>" + escapeText(title) + "</text></docTitle>\n<navMap>\n")
	order := 0
	var walk func(entries []*tocEntry, depth int)
	walk = func(entries []*tocEntry, depth int) {
		indent := strings.Repeat("  ", depth)
		for _, e := range entries {
			order++
			b.WriteString(fmt.Sprintf(`%s<navPoint id="navpoint-%d" playOrder="%d">`+"\n", indent, order, order))
			b.WriteString(indent + "  <navLabel><text>" + escapeText(e.Title) + "</text></navLabel>\n")
			b.WriteString(indent + `  <content src="` + escapeAttr(e.Href) + `"/>` + "\n")
			walk(e.Children, depth+1)
			b.WriteString(indent + "</navPoint>\n")
		}
	}
	walk(toc, 1)
	b.WriteString("</navMap>\n</ncx>\n")
	return b.String()
}

func packageDocument(src Source, identifier string, now time.Time, chapters []epubChapter, images []*epubImage, cover *epubCover) string {
	meta := src.Meta
	var b strings.Builder
	b.WriteString(xmlHeader)
	b.WriteString(`<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="book-id" xml:lang="` + epubLang + `">` + "\n")
	b.WriteString(`  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">` + "\n")
	b.WriteString(`    <dc:identifier id="book-id">` + escapeText(identifier) + "</dc:identifier>\n")
	b.WriteString("    <dc:title>" + escapeText(meta.Title) + "</dc:title>\n")
	if strings.TrimSpace(meta.Author) != "" {
		b.WriteString("    <dc:creator>" + escapeText(meta.Author) + "</dc:creator>\n")
	}
	b.WriteString("    <dc:language>" + epubLang + "</dc:language>\n")
	if strings.TrimSpace(meta.Intro) != "" {
		b.WriteString("    <dc:description>" + escapeText(meta.Intro) + "</dc:description>\n")
	}
	if src.BookID != "" {
		b.WriteString("    <dc:source>https://weread.qq.com/web/bookDetail/" + escapeText(src.BookID) + "</dc:source>\n")
	}
	b.WriteString(`    <meta property="dcterms:modified">` + now.Format("2006-01-02T15:04:05Z") + "</meta>\n")
	if cover != nil {
		b.WriteString(`    <meta name="cover" content="cover-image"/>` + "\n")
	}
	b.WriteString("  </metadata>\n  <manifest>\n")
	b.WriteString(`    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>` + "\n")
	b.WriteString(`    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>` + "\n")
	b.WriteString(`    <item id="style" href="style/default.css" media-type="text/css"/>` + "\n")
	if cover != nil {
		b.WriteString(`    <item id="cover-image" href="images/` + escapeAttr(cover.zipName) + `" media-type="` + cover.mime + `" properties="cover-image"/>` + "\n")
		b.WriteString(`    <item id="cover" href="cover.xhtml" media-type="application/xhtml+xml"/>` + "\n")
	}
	for i, ch := range chapters {
		props := ""
		if ch.remote {
			props = ` properties="remote-resources"`
		}
		b.WriteString(fmt.Sprintf(`    <item id="chap%04d" href="%s" media-type="application/xhtml+xml"%s/>`+"\n", i+1, escapeAttr(ch.file), props))
	}
	for i, img := range images {
		b.WriteString(fmt.Sprintf(`    <item id="img%04d" href="images/%s" media-type="%s"/>`+"\n", i+1, escapeAttr(img.zipName), img.mime))
	}
	b.WriteString("  </manifest>\n  <spine toc=\"ncx\">\n")
	if cover != nil {
		b.WriteString(`    <itemref idref="cover"/>` + "\n")
	}
	b.WriteString(`    <itemref idref="nav"/>` + "\n")
	for i := range chapters {
		b.WriteString(fmt.Sprintf(`    <itemref idref="chap%04d"/>`+"\n", i+1))
	}
	b.WriteString("  </spine>\n")
	if cover != nil {
		b.WriteString("  <guide>\n    <reference type=\"cover\" title=\"封面\" href=\"cover.xhtml\"/>\n  </guide>\n")
	}
	b.WriteString("</package>\n")
	return b.String()
}
