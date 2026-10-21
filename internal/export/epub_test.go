package export

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"strings"
	"testing"
)

type epubFiles map[string][]byte

func buildEPUB(t *testing.T, src Source, opts EPUBOptions) ([]byte, *zip.Reader, epubFiles) {
	t.Helper()
	opts.Now = testNow
	var buf bytes.Buffer
	if err := WriteEPUB(&buf, src, opts); err != nil {
		t.Fatalf("WriteEPUB: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := epubFiles{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name] = data
	}
	return buf.Bytes(), zr, files
}

func assertWellFormedXML(t *testing.T, name string, data []byte) {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	for {
		if _, err := dec.Token(); errors.Is(err, io.EOF) {
			return
		} else if err != nil {
			t.Fatalf("%s is not well-formed XML: %v", name, err)
		}
	}
}

func TestEPUBContainerLayout(t *testing.T) {
	raw, zr, files := buildEPUB(t, testSource(t), EPUBOptions{})

	first := zr.File[0]
	if first.Name != "mimetype" || first.Method != zip.Store || len(first.Extra) != 0 {
		t.Fatalf("first entry = %q method=%d extra=%d bytes; want an uncompressed mimetype without extra field", first.Name, first.Method, len(first.Extra))
	}
	// OCF: the literal "mimetype" and its content sit at fixed offsets so that
	// tools can identify the file type without unzipping.
	if string(raw[30:38]) != "mimetype" || string(raw[38:38+len(epubMimetype)]) != epubMimetype {
		t.Fatalf("mimetype is not stored at the start of the archive: %q", raw[:80])
	}
	if first.Flags&0x8 != 0 {
		t.Fatal("mimetype must not use a data descriptor")
	}

	for name, data := range files {
		switch {
		case name == "mimetype", strings.HasSuffix(name, ".css"), strings.HasPrefix(name, "EPUB/images/"):
		default:
			assertWellFormedXML(t, name, data)
		}
	}
	for _, name := range []string{
		"META-INF/container.xml", "EPUB/content.opf", "EPUB/toc.ncx", "EPUB/nav.xhtml", "EPUB/cover.xhtml",
		"EPUB/style/default.css", "EPUB/chap_0001.xhtml", "EPUB/chap_0005.xhtml",
	} {
		if _, ok := files[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	contains(t, string(files["META-INF/container.xml"]), `full-path="EPUB/content.opf"`)
}

// The package document must be internally consistent: every manifest item
// exists in the archive, the spine only references manifest items, and the
// declared media types match the real content.
func TestEPUBPackageIsConsistent(t *testing.T) {
	_, zr, files := buildEPUB(t, testSource(t), EPUBOptions{})

	var pkg struct {
		Identifier string `xml:"metadata>identifier"`
		Title      string `xml:"metadata>title"`
		Creator    string `xml:"metadata>creator"`
		Language   string `xml:"metadata>language"`
		Source     string `xml:"metadata>source"`
		Metas      []struct {
			Name     string `xml:"name,attr"`
			Property string `xml:"property,attr"`
			Content  string `xml:"content,attr"`
			Value    string `xml:",chardata"`
		} `xml:"metadata>meta"`
		Manifest []struct {
			ID         string `xml:"id,attr"`
			Href       string `xml:"href,attr"`
			MediaType  string `xml:"media-type,attr"`
			Properties string `xml:"properties,attr"`
		} `xml:"manifest>item"`
		Spine struct {
			TOC   string `xml:"toc,attr"`
			Items []struct {
				IDRef string `xml:"idref,attr"`
			} `xml:"itemref"`
		} `xml:"spine"`
	}
	if err := xml.Unmarshal(files["EPUB/content.opf"], &pkg); err != nil {
		t.Fatal(err)
	}

	if pkg.Title != "Python编程：从入门到实践 & <第2版>" || pkg.Creator != "埃里克·马瑟斯" || pkg.Language != "zh-CN" {
		t.Fatalf("metadata = %+v", pkg)
	}
	if !strings.HasPrefix(pkg.Identifier, "urn:uuid:") || len(pkg.Identifier) != len("urn:uuid:")+36 {
		t.Fatalf("identifier = %q", pkg.Identifier)
	}
	if pkg.Source != "https://weread.qq.com/web/bookDetail/08232ac0720befa90825d88" {
		t.Fatalf("source = %q", pkg.Source)
	}

	var modified, coverMeta string
	for _, m := range pkg.Metas {
		if m.Property == "dcterms:modified" {
			modified = m.Value
		}
		if m.Name == "cover" {
			coverMeta = m.Content
		}
	}
	if modified != "2026-10-22T08:30:15Z" {
		t.Fatalf("dcterms:modified = %q", modified)
	}

	ids := map[string]string{} // id -> href
	foundNav, foundCoverImage := false, false
	for _, it := range pkg.Manifest {
		if _, dup := ids[it.ID]; dup {
			t.Errorf("duplicate manifest id %q", it.ID)
		}
		ids[it.ID] = it.Href
		data, ok := files["EPUB/"+it.Href]
		if !ok {
			t.Errorf("manifest item %s (%s) is not in the archive", it.ID, it.Href)
			continue
		}
		if strings.HasPrefix(it.MediaType, "image/") {
			want := map[string]string{".jpg": "image/jpeg", ".png": "image/png"}[path.Ext(it.Href)]
			if it.MediaType != want {
				t.Errorf("%s: media type %s does not match extension %s", it.Href, it.MediaType, path.Ext(it.Href))
			}
			// the content must really be what the media type says
			sig := map[string]string{"image/jpeg": "\xff\xd8\xff", "image/png": "\x89PNG"}[it.MediaType]
			if !bytes.HasPrefix(data, []byte(sig)) {
				t.Errorf("%s: content does not look like %s", it.Href, it.MediaType)
			}
		}
		if strings.Contains(it.Properties, "nav") {
			foundNav = true
		}
		if strings.Contains(it.Properties, "cover-image") {
			foundCoverImage = true
			if it.ID != coverMeta {
				t.Errorf("cover meta points at %q but the cover-image item is %q", coverMeta, it.ID)
			}
		}
	}
	if !foundNav || !foundCoverImage {
		t.Fatalf("nav=%v cover-image=%v", foundNav, foundCoverImage)
	}
	if _, ok := ids[pkg.Spine.TOC]; !ok {
		t.Errorf("spine toc=%q is not a manifest item", pkg.Spine.TOC)
	}
	for _, ref := range pkg.Spine.Items {
		if _, ok := ids[ref.IDRef]; !ok {
			t.Errorf("spine references unknown item %q", ref.IDRef)
		}
	}
	// cover page first, then the navigation document, then the five chapters.
	if len(pkg.Spine.Items) != 7 || pkg.Spine.Items[0].IDRef != "cover" || pkg.Spine.Items[1].IDRef != "nav" {
		t.Fatalf("spine = %+v", pkg.Spine.Items)
	}

	// every file in the archive other than the container plumbing is in the manifest
	inManifest := map[string]bool{"EPUB/content.opf": true}
	for _, href := range ids {
		inManifest["EPUB/"+href] = true
	}
	for _, f := range zr.File {
		if f.Name == "mimetype" || strings.HasPrefix(f.Name, "META-INF/") {
			continue
		}
		if !inManifest[f.Name] {
			t.Errorf("%s is in the archive but not in the manifest", f.Name)
		}
	}
}

func TestEPUBImagesAreRenamedDroppedAndFlagged(t *testing.T) {
	_, _, files := buildEPUB(t, testSource(t), EPUBOptions{})

	// The PNG that the cache stored as .jpg is embedded under .png and the chapter follows.
	legacy := strings.TrimSuffix(imgLegacyPNG, ".jpg")
	if _, ok := files["EPUB/images/"+legacy+".png"]; !ok {
		t.Fatal("PNG data was not stored under a .png name")
	}
	if _, ok := files["EPUB/images/"+imgLegacyPNG]; ok {
		t.Fatal("the misleading .jpg name must not be used")
	}
	if _, ok := files["EPUB/images/"+imgRealJPG]; !ok {
		t.Fatal("regular JPEG missing")
	}
	if _, ok := files["EPUB/images/"+imgUnused]; ok {
		t.Fatal("an image no chapter refers to must not be embedded")
	}

	chapter := string(files["EPUB/chap_0002.xhtml"])
	contains(t, chapter,
		`<img src="images/`+legacy+`.png" alt="" />`,
		`<img src="images/`+imgRealJPG+`" alt="" />`,
		`<img src="https://cdn.example.invalid/remote.png" alt="" />`,
	)
	notContains(t, chapter, "missing.jpg")

	opf := string(files["EPUB/content.opf"])
	contains(t, opf, `<item id="chap0002" href="chap_0002.xhtml" media-type="application/xhtml+xml" properties="remote-resources"/>`)
	notContains(t, opf, `id="chap0001" href="chap_0001.xhtml" media-type="application/xhtml+xml" properties`)
}

func TestEPUBChapterContent(t *testing.T) {
	_, _, files := buildEPUB(t, testSource(t), EPUBOptions{})

	first := string(files["EPUB/chap_0001.xhtml"])
	contains(t, first,
		`<?xml version="1.0" encoding="UTF-8"?>`,
		"<!DOCTYPE html>",
		`<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" lang="zh-CN" xml:lang="zh-CN">`,
		`<title>版权信息</title>`,
		`href="style/default.css"`,
		"<h1>版权信息</h1>",
		"<sup>1</sup>",
	)
	notContains(t, first, `lang="hr"`)

	second := string(files["EPUB/chap_0002.xhtml"])
	contains(t, second,
		`<h2 id="t1">1.1 搭建环境</h2>`,
		`<h2 id="t2">1.2 运行</h2>`,
		"<code>python</code>",
		"<pre><code>print('hi &lt; there')\n</code></pre>",
	)
	notContains(t, second, "epub-code")

	if !strings.Contains(string(files["EPUB/style/default.css"]), "code {") {
		t.Fatal("stylesheet lacks the code rule")
	}
	if strings.Contains(string(files["EPUB/style/default.css"]), "epub-code {") {
		t.Fatal("stylesheet still styles the custom <epub-code> element")
	}
}

func TestEPUBNavigationIsNested(t *testing.T) {
	_, _, files := buildEPUB(t, testSource(t), EPUBOptions{})

	nav := string(files["EPUB/nav.xhtml"])
	contains(t, nav,
		`<nav epub:type="toc" id="toc" role="doc-toc">`,
		`<a href="chap_0001.xhtml">版权信息</a>`,
		// chapter 2 is a section: its own anchors, then the deeper chapter 3
		`<a href="chap_0002.xhtml">第1章 起步</a>`,
		`<a href="chap_0002.xhtml#t1">1.1 搭建环境</a>`,
		`<a href="chap_0002.xhtml#t2">1.2 运行</a>`,
		`<a href="chap_0003.xhtml">1.1 小节</a>`,
		// chapter 4 is plain and turned into a section by the deeper chapter 5
		`<a href="chap_0004.xhtml">第2章 变量</a>`,
		`<a href="chap_0005.xhtml">2.1 字符串</a>`,
		`epub:type="landmarks"`, `epub:type="cover" href="cover.xhtml"`,
	)

	// The last section (chapter 4 with its child 5) must not be lost.
	idx4 := strings.Index(nav, "chap_0004.xhtml")
	idx5 := strings.Index(nav, "chap_0005.xhtml")
	if idx4 < 0 || idx5 < idx4 {
		t.Fatalf("chapters 4/5 are not nested in order:\n%s", nav)
	}

	ncx := string(files["EPUB/toc.ncx"])
	contains(t, ncx,
		`<meta name="dtb:depth" content="2"/>`,
		`<content src="chap_0002.xhtml#t1"/>`,
		`<navPoint id="navpoint-1" playOrder="1">`,
		`<docTitle><text>Python编程：从入门到实践 &amp; &lt;第2版&gt;</text></docTitle>`,
	)
	// seven entries (chapters 1-5 plus the two anchors); play order runs 1..7 without gaps
	for n := 1; n <= 7; n++ {
		if !strings.Contains(ncx, `playOrder="`+string(rune('0'+n))+`"`) {
			t.Errorf("NCX lacks playOrder %d", n)
		}
	}
	notContains(t, ncx, `playOrder="8"`)
}

func TestEPUBEscapesMetadata(t *testing.T) {
	_, _, files := buildEPUB(t, testSource(t), EPUBOptions{})
	opf := string(files["EPUB/content.opf"])
	contains(t, opf,
		"<dc:title>Python编程：从入门到实践 &amp; &lt;第2版&gt;</dc:title>",
		"<dc:description>一本&#34;书&#34;的简介 &amp; more</dc:description>",
	)
}

func TestEPUBWithoutCover(t *testing.T) {
	src := testSource(t)
	if err := removeFile(src.Store.CoverPath()); err != nil {
		t.Fatal(err)
	}
	_, _, files := buildEPUB(t, src, EPUBOptions{})
	if _, ok := files["EPUB/cover.xhtml"]; ok {
		t.Fatal("cover page written without a cover")
	}
	opf := string(files["EPUB/content.opf"])
	notContains(t, opf, "cover-image", `name="cover"`, "<guide>", `idref="cover"`)
	assertWellFormedXML(t, "content.opf", files["EPUB/content.opf"])
	assertWellFormedXML(t, "nav.xhtml", files["EPUB/nav.xhtml"])
	notContains(t, string(files["EPUB/nav.xhtml"]), "cover.xhtml")
}

func TestEPUBExtraCSS(t *testing.T) {
	_, _, files := buildEPUB(t, testSource(t), EPUBOptions{ExtraCSS: "body { font-size: 20px; }"})
	css := string(files["EPUB/style/default.css"])
	if !strings.HasSuffix(strings.TrimSpace(css), "body { font-size: 20px; }") {
		t.Fatalf("extra CSS was not appended: ...%s", css[max(0, len(css)-80):])
	}
}

func TestEPUBIsDeterministic(t *testing.T) {
	src := testSource(t)
	a, _, _ := buildEPUB(t, src, EPUBOptions{})
	b, _, _ := buildEPUB(t, src, EPUBOptions{})
	if !bytes.Equal(a, b) {
		t.Fatal("the same input and timestamp must give identical bytes")
	}
}

func TestEPUBRequiresEveryChapter(t *testing.T) {
	src := testSource(t)
	if err := removeFile(src.Store.ChapterPath(2, src.Meta.Chapters[2].ID)); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := WriteEPUB(&buf, src, EPUBOptions{})
	if err == nil || !strings.Contains(err.Error(), "has not been captured") {
		t.Fatalf("err = %v", err)
	}
}
