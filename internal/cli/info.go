package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/jtsang4/weread-exporter-go/internal/app"
)

func (e *env) newInfoCmd() *cobra.Command {
	var (
		common commonFlags
		bookID string
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "info -b BOOK",
		Short: "Show a book's table of contents and what is cached",
		Long: `Show the title, author and chapters of a book, and which chapters are in the
cache already. For a book list it shows the books. Nothing is exported and no
login is needed.

The chapter numbers are the ones --chapters takes.`,
		Example: `  weread-exporter info -b 34336681
  weread-exporter info -b https://weread.qq.com/web/bookDetail/3300040626 --json`,
		Args:          noArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(*cobra.Command, []string) error {
			if bookID == "" {
				return usageError("no book given: use -b/--book-id with a book id or URL")
			}
			cfg := buildConfig(&common, nil, &exportFlags{bookID: bookID})
			exp, _, err := e.exporter(&common, cfg)
			if err != nil {
				return err
			}
			info, err := exp.Info(e.ctx)
			if err != nil {
				return err
			}
			if asJSON {
				return writeInfoJSON(e.streams.Out, info)
			}
			writeInfoText(e.streams.Out, info)
			return nil
		},
	}
	cmd.SetFlagErrorFunc(flagError)
	common.register(cmd.Flags())
	registerBookFlag(cmd.Flags(), &bookID, "look up")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

type jsonChapter struct {
	Number int    `json:"number"`
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Level  int    `json:"level"`
	Words  int    `json:"words"`
	Cached bool   `json:"cached"`
}

type jsonBook struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Author    string        `json:"author"`
	Intro     string        `json:"intro"`
	SoldOut   bool          `json:"sold_out"`
	FromCache bool          `json:"from_cache"`
	Chapters  []jsonChapter `json:"chapters"`
}

type jsonListBook struct {
	ID     string `json:"id"`
	BookID string `json:"book_id"`
	Title  string `json:"title"`
}

type jsonList struct {
	ListID string         `json:"list_id"`
	Books  []jsonListBook `json:"books"`
}

func writeInfoJSON(w io.Writer, info *app.Info) error {
	var v any
	if b := info.Book; b != nil {
		out := jsonBook{
			ID: b.ID, Title: b.Meta.Title, Author: b.Meta.Author, Intro: b.Meta.Intro,
			SoldOut: b.SoldOut, FromCache: b.FromCache, Chapters: make([]jsonChapter, len(b.Meta.Chapters)),
		}
		for i, ch := range b.Meta.Chapters {
			out.Chapters[i] = jsonChapter{Number: i + 1, ID: ch.ID, Title: ch.Title, Level: ch.Level, Words: ch.Words, Cached: b.Cached[i]}
		}
		v = out
	} else {
		out := jsonList{ListID: info.ListID, Books: make([]jsonListBook, len(info.List))}
		for i, entry := range info.List {
			out.Books[i] = jsonListBook{ID: entry.ID, BookID: entry.BookID, Title: entry.Title}
		}
		v = out
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func writeInfoText(w io.Writer, info *app.Info) {
	if b := info.Book; b != nil {
		fmt.Fprintf(w, "%s\n", b.Meta.Title)
		fmt.Fprintf(w, "  Author:   %s\n", b.Meta.Author)
		fmt.Fprintf(w, "  Book id:  %s\n", b.ID)
		status := "on sale"
		if b.SoldOut {
			status = "sold out: WeRead no longer offers this book, only cached chapters can be exported"
		}
		fmt.Fprintf(w, "  Status:   %s\n", status)
		fmt.Fprintf(w, "  Chapters: %d, %d of them cached\n", len(b.Meta.Chapters), b.CachedCount())
		if b.FromCache {
			fmt.Fprintln(w, "  (the site could not be reached; this comes from the cache)")
		}
		fmt.Fprintln(w)

		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
		fmt.Fprintln(tw, "No.\tWords\tCached\t  Title")
		for i, ch := range b.Meta.Chapters {
			cached := "no"
			if b.Cached[i] {
				cached = "yes"
			}
			indent := ""
			if ch.Level > 1 {
				indent = strings.Repeat("  ", ch.Level-1)
			}
			fmt.Fprintf(tw, "%d\t%d\t%s\t  %s%s\n", i+1, ch.Words, cached, indent, ch.Title)
		}
		_ = tw.Flush()
		return
	}

	fmt.Fprintf(w, "Book list %s: %d books\n\n", info.ListID, len(info.List))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "No.\tBook id\tTitle")
	for i, entry := range info.List {
		fmt.Fprintf(tw, "%d\t%s\t%s\n", i+1, entry.ID, entry.Title)
	}
	_ = tw.Flush()
}
