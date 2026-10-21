package cli

import (
	"context"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/jtsang4/weread-exporter-go/internal/app"
)

// Log formats.
const (
	logText = "text"
	logJSON = "json"
)

// newLogger builds the logger that writes progress to w (the standard error).
func newLogger(w io.Writer, verbose bool, format string) (*slog.Logger, error) {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	switch format {
	case "", logText:
		return slog.New(&prettyHandler{mu: &sync.Mutex{}, w: w, level: level}), nil
	case logJSON:
		return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})), nil
	}
	return nil, &app.UsageError{Msg: "unsupported --log-format " + strconv.Quote(format) + " (use text or json)"}
}

// prettyHandler prints one line per record, meant for people:
//
//	15:04:05 INFO  capturing chapter n=3 of=39 title="第三章 起源"
type prettyHandler struct {
	mu    *sync.Mutex
	w     io.Writer
	level slog.Leveler
	attrs []slog.Attr // from WithAttrs, keys already carry their group prefix
	group string      // prefix of the attributes of records: "a.b."
}

func (h *prettyHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level.Level() }

func (h *prettyHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	if !r.Time.IsZero() {
		b.WriteString(r.Time.Format("15:04:05"))
		b.WriteByte(' ')
	}
	level := r.Level.String()
	b.WriteString(level)
	for i := len(level); i < 5; i++ {
		b.WriteByte(' ')
	}
	b.WriteByte(' ')
	b.WriteString(r.Message)
	for _, a := range h.attrs {
		writeAttr(&b, "", a)
	}
	r.Attrs(func(a slog.Attr) bool {
		writeAttr(&b, h.group, a)
		return true
	})
	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *prettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := *h
	c.attrs = append([]slog.Attr(nil), h.attrs...)
	for _, a := range attrs {
		a.Key = h.group + a.Key
		c.attrs = append(c.attrs, a)
	}
	return &c
}

func (h *prettyHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	c := *h
	c.group = h.group + name + "."
	return &c
}

func writeAttr(b *strings.Builder, prefix string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}
	if a.Value.Kind() == slog.KindGroup {
		nested := prefix
		if a.Key != "" {
			nested += a.Key + "."
		}
		for _, ga := range a.Value.Group() {
			writeAttr(b, nested, ga)
		}
		return
	}
	b.WriteByte(' ')
	b.WriteString(prefix)
	b.WriteString(a.Key)
	b.WriteByte('=')
	b.WriteString(formatValue(a.Value.String()))
}

// formatValue quotes a value that would be ambiguous in a line of key=value
// pairs. Backslashes stay as they are: they are common in Windows paths.
func formatValue(s string) string {
	if !needsQuoting(s) {
		return s
	}
	for _, r := range s {
		if r == '"' || !unicode.IsPrint(r) && !unicode.IsSpace(r) || r == '\n' || r == '\r' || r == '\t' {
			return strconv.Quote(s)
		}
	}
	return `"` + s + `"`
}

func needsQuoting(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if r == '"' || r == '=' || unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return true
		}
	}
	return false
}
