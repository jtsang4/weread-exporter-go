package markdown

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	xhtml "golang.org/x/net/html"
)

// voidElements are written as <name /> in XHTML.
var voidElements = map[string]struct{}{
	"area": {}, "base": {}, "br": {}, "col": {}, "embed": {}, "hr": {}, "img": {}, "input": {},
	"link": {}, "meta": {}, "param": {}, "source": {}, "track": {}, "wbr": {},
}

// droppedElements never belong in an e-book chapter; they are removed together
// with their content. (The capture is plain book text, so this is hardening
// against hostile input rather than something that normally triggers.)
var droppedElements = map[string]struct{}{
	"script": {}, "style": {}, "iframe": {}, "object": {}, "embed": {}, "link": {}, "meta": {},
	"base": {}, "form": {}, "input": {}, "button": {}, "textarea": {}, "select": {},
	"noscript": {}, "template": {}, "frame": {}, "frameset": {},
}

var (
	xmlNamePattern  = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	attrNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]*$`)
)

// SerializeXHTML renders parsed nodes as well-formed XHTML: lower-case names,
// quoted attributes, XML escaping, self-closed void elements, no comments, and
// no characters that XML 1.0 forbids.
func SerializeXHTML(nodes []*xhtml.Node) string {
	var b strings.Builder
	for _, n := range nodes {
		writeXHTML(&b, n)
	}
	return b.String()
}

func writeXHTML(b *strings.Builder, n *xhtml.Node) {
	switch n.Type {
	case xhtml.TextNode:
		escapeText(b, n.Data)
	case xhtml.DocumentNode:
		writeChildren(b, n)
	case xhtml.ElementNode:
		if _, drop := droppedElements[n.Data]; drop {
			return
		}
		if !xmlNamePattern.MatchString(n.Data) {
			writeChildren(b, n) // unknown/odd tag: keep the content, lose the tag
			return
		}
		b.WriteByte('<')
		b.WriteString(n.Data)
		for _, a := range n.Attr {
			if !keepAttribute(a) {
				continue
			}
			b.WriteByte(' ')
			b.WriteString(strings.ToLower(a.Key))
			b.WriteString(`="`)
			escapeAttr(b, a.Val)
			b.WriteByte('"')
		}
		if _, void := voidElements[n.Data]; void {
			b.WriteString(" />")
			return
		}
		b.WriteByte('>')
		writeChildren(b, n)
		b.WriteString("</")
		b.WriteString(n.Data)
		b.WriteByte('>')
	}
	// comments, doctypes and processing instructions are dropped
}

func writeChildren(b *strings.Builder, n *xhtml.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		writeXHTML(b, c)
	}
}

func keepAttribute(a xhtml.Attribute) bool {
	if a.Namespace != "" || !attrNamePattern.MatchString(a.Key) {
		return false
	}
	key := strings.ToLower(a.Key)
	if strings.HasPrefix(key, "on") || key == "xmlns" {
		return false
	}
	if (key == "href" || key == "src") && strings.HasPrefix(strings.ToLower(strings.TrimSpace(a.Val)), "javascript:") {
		return false
	}
	return true
}

func escapeText(b *strings.Builder, s string) {
	for _, r := range s {
		switch {
		case !isXMLChar(r):
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
}

func escapeAttr(b *strings.Builder, s string) {
	for _, r := range s {
		switch {
		case !isXMLChar(r):
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteString("&#" + strconv.Itoa(int(r)) + ";")
		default:
			b.WriteRune(r)
		}
	}
}

// isXMLChar reports whether r is allowed in an XML 1.0 document.
func isXMLChar(r rune) bool {
	switch {
	case r == 0x9 || r == 0xA || r == 0xD:
		return true
	case r >= 0x20 && r <= 0xD7FF:
		return true
	case r >= 0xE000 && r <= 0xFFFD:
		return true
	case r >= 0x10000 && r <= 0x10FFFF:
		return true
	}
	return false
}

// RewriteImages calls fn for the src of every <img>. fn returns the new src, or
// keep=false to remove the image from the tree altogether. It returns the
// (possibly shorter) list of top-level nodes; callers must use that instead of
// the slice they passed in.
func RewriteImages(nodes []*xhtml.Node, fn func(src string) (newSrc string, keep bool)) []*xhtml.Node {
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		for c := n.FirstChild; c != nil; {
			next := c.NextSibling // c may be unlinked below
			if c.Type == xhtml.ElementNode && c.Data == "img" {
				for i := range c.Attr {
					if c.Attr[i].Key != "src" {
						continue
					}
					if newSrc, keep := fn(c.Attr[i].Val); keep {
						c.Attr[i].Val = newSrc
					} else {
						n.RemoveChild(c)
					}
					break
				}
			} else {
				walk(c)
			}
			c = next
		}
	}
	return underOneParent(nodes, walk)
}

// underOneParent runs walk on a parent that holds the given top-level nodes:
// the top-level nodes of a fragment have no common parent, and walk can only
// remove or replace the children of the node it is given. It returns what is
// left of the top-level nodes.
func underOneParent(nodes []*xhtml.Node, walk func(parent *xhtml.Node)) []*xhtml.Node {
	holder := &xhtml.Node{Type: xhtml.ElementNode, Data: "div"}
	for _, n := range nodes {
		if n.Parent != nil {
			n.Parent.RemoveChild(n)
		}
		holder.AppendChild(n)
	}
	walk(holder)

	var out []*xhtml.Node
	for c := holder.FirstChild; c != nil; {
		next := c.NextSibling
		holder.RemoveChild(c)
		out = append(out, c)
		c = next
	}
	return out
}

// ImageSources lists the src of every <img> in document order.
func ImageSources(nodes []*xhtml.Node) []string {
	var srcs []string
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Data == "img" {
			if src := attr(n, "src"); src != "" {
				srcs = append(srcs, src)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	return srcs
}

// LinkRemoteImages replaces every <img> that points at http(s) with a link to
// the same address that reads text, and returns the new top-level nodes and
// the number of images replaced. An EPUB must not embed pictures from outside
// the package (EPUBCheck reports RSC-006 even for items flagged with
// remote-resources), but a link is always valid and still leads to the picture.
func LinkRemoteImages(nodes []*xhtml.Node, text string) ([]*xhtml.Node, int) {
	replaced := 0
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		for c := n.FirstChild; c != nil; {
			next := c.NextSibling // c is replaced below
			if c.Type == xhtml.ElementNode && c.Data == "img" {
				if src := strings.TrimSpace(attr(c, "src")); isRemote(src) {
					link := &xhtml.Node{Type: xhtml.ElementNode, Data: "a", Attr: []xhtml.Attribute{{Key: "href", Val: src}}}
					link.AppendChild(&xhtml.Node{Type: xhtml.TextNode, Data: text})
					n.InsertBefore(link, c)
					n.RemoveChild(c)
					replaced++
				}
			} else {
				walk(c)
			}
			c = next
		}
	}
	return underOneParent(nodes, walk), replaced
}

// isRemote reports whether src points at the web rather than into the package.
func isRemote(src string) bool {
	v := strings.ToLower(strings.TrimSpace(src))
	return strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://")
}

// AssignAnchorIDs makes the sub-headings listed in the book's table of contents
// linkable. For every title (in order) it looks for the next heading or
// paragraph whose entire text equals the title, ignoring white space, and gives
// it the id "<prefix><i+1>". The returned slice holds the id per title, or ""
// when the title was not found in the chapter.
//
// (The original tool did this with a text substitution on the serialised HTML,
// which silently failed whenever the title contained a full-width space.)
func AssignAnchorIDs(nodes []*xhtml.Node, titles []string, prefix string) []string {
	ids := make([]string, len(titles))

	var candidates []*xhtml.Node
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			switch n.Data {
			case "h1", "h2", "h3", "h4", "h5", "h6", "p":
				candidates = append(candidates, n)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}

	next := 0
	for i, title := range titles {
		want := squashSpace(title)
		if want == "" {
			continue
		}
		for j := next; j < len(candidates); j++ {
			if squashSpace(nodeText(candidates[j])) != want {
				continue
			}
			id := prefix + strconv.Itoa(i+1)
			if existing := attr(candidates[j], "id"); existing != "" {
				id = existing
			} else {
				candidates[j].Attr = append(candidates[j].Attr, xhtml.Attribute{Key: "id", Val: id})
			}
			ids[i] = id
			next = j + 1
			break
		}
	}
	return ids
}

func attr(n *xhtml.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func nodeText(n *xhtml.Node) string {
	var b strings.Builder
	collectText(&b, n)
	return b.String()
}

// squashSpace removes all white space (including full-width spaces).
func squashSpace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}
