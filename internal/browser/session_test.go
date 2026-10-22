package browser

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestContextErr(t *testing.T) {
	// A deadline that ran out must survive as DeadlineExceeded even though
	// chromedp reported a plain cancellation (it does on Windows).
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	err := contextErr(ctx, errors.New("browser: open http://x: context canceled"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}

	if got := contextErr(context.Background(), io.EOF); !errors.Is(got, io.EOF) {
		t.Errorf("a live context should keep the operation's error, got %v", got)
	}
	if got := contextErr(ctx, nil); got != nil {
		t.Errorf("a successful operation stays nil, got %v", got)
	}
}
