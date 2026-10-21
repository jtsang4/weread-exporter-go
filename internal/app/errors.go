package app

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	// ErrBookUnavailable means WeRead no longer offers the book ("sold out") and
	// the cache does not hold all of it.
	ErrBookUnavailable = errors.New("the book is no longer available on WeRead")

	// ErrLoginNeeded means a signed-in session is required but none could be
	// established without a visible browser window.
	ErrLoginNeeded = errors.New("a WeRead login is required")

	// ErrHorizontalReader means the account uses the paged reader and the
	// exporter may not (or could not) switch it.
	ErrHorizontalReader = errors.New("the account uses the paged (horizontal) reader")
)

// BatchError reports the books of a book list that failed.
type BatchError struct {
	Total  int
	Failed map[string]error // book id -> error
}

func (e *BatchError) Error() string {
	ids := make([]string, 0, len(e.Failed))
	for id := range e.Failed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	fmt.Fprintf(&b, "%d of %d books failed:", len(e.Failed), e.Total)
	for _, id := range ids {
		fmt.Fprintf(&b, "\n  %s: %v", id, e.Failed[id])
	}
	return b.String()
}

// Unwrap lets errors.Is find the individual failures.
func (e *BatchError) Unwrap() []error {
	ids := make([]string, 0, len(e.Failed))
	for id := range e.Failed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]error, 0, len(ids))
	for _, id := range ids {
		out = append(out, e.Failed[id])
	}
	return out
}
