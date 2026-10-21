package export

// tocEntry is one line of the table of contents; sections have children.
type tocEntry struct {
	Title    string
	Href     string
	Children []*tocEntry
}

// chapterTOC is what the table-of-contents builder needs to know about a chapter.
type chapterTOC struct {
	File    string
	Title   string
	Level   int
	Anchors []anchorTOC
}

// anchorTOC is a sub-heading listed in the book's table of contents.
type anchorTOC struct {
	Title string
	// ID is the id of the heading element inside the chapter document, or ""
	// when the heading could not be located (the entry then links to the chapter).
	ID string
}

func (c chapterTOC) entry() *tocEntry { return &tocEntry{Title: c.Title, Href: c.File} }

// buildTOC nests the chapters the way the original tool did:
//
//   - a top-level chapter (level 1) with anchors becomes a section whose
//     children are its anchors;
//   - the deeper chapters (level > 1) that follow are appended to the open
//     section;
//   - a deeper chapter that follows a plain top-level chapter turns that
//     chapter into a section;
//   - everything else is a plain entry.
//
// Unlike the original, a section that is still open at the end of the book is
// not lost, and a deeper chapter at the very start does not crash.
func buildTOC(chapters []chapterTOC) []*tocEntry {
	var toc []*tocEntry
	var section *tocEntry

	for _, ch := range chapters {
		if section != nil {
			if ch.Level > 1 {
				section.Children = append(section.Children, ch.entry())
				continue
			}
			toc = append(toc, section)
			section = nil
		}

		switch {
		case len(ch.Anchors) > 0:
			section = ch.entry()
			for _, a := range ch.Anchors {
				href := ch.File
				if a.ID != "" {
					href += "#" + a.ID
				}
				section.Children = append(section.Children, &tocEntry{Title: a.Title, Href: href})
			}
		case ch.Level > 1 && len(toc) > 0:
			section = toc[len(toc)-1]
			toc = toc[:len(toc)-1]
			section.Children = append(section.Children, ch.entry())
		default:
			toc = append(toc, ch.entry())
		}
	}
	if section != nil {
		toc = append(toc, section)
	}
	return toc
}

// tocDepth is the nesting depth of the table of contents (1 for a flat list).
func tocDepth(toc []*tocEntry) int {
	depth := 0
	for _, e := range toc {
		if d := 1 + tocDepth(e.Children); d > depth {
			depth = d
		}
	}
	return depth
}
