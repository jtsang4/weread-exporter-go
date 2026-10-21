// Package cli implements the weread-exporter command line.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jtsang4/weread-exporter-go/internal/app"
	"github.com/jtsang4/weread-exporter-go/internal/version"
)

// Exit statuses.
const (
	ExitOK          = 0
	ExitFailure     = 1   // the work failed
	ExitUsage       = 2   // the command line or the configuration is wrong
	ExitLoginNeeded = 3   // a WeRead login is needed and could not be established
	ExitInterrupted = 130 // 128 + SIGINT
)

// Streams are the standard streams of a run.
type Streams struct {
	In  io.Reader
	Out io.Writer // results: the files written, tables, versions
	Err io.Writer // progress log and error messages
}

// Main runs the command line of the process and returns its exit status.
// SIGINT and SIGTERM cancel the run (it cleans up first); a second signal ends
// the process at once.
func Main() int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			fmt.Fprintln(os.Stderr, "interrupted: cleaning up; press Ctrl-C again to quit at once")
			cancel()
			signal.Stop(signals) // the default behaviour is back: the next signal ends the process
		case <-ctx.Done():
		}
	}()
	return Execute(ctx, os.Args[1:], Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
}

// Execute runs the command line and returns the exit status. Errors are
// reported on streams.Err.
func Execute(ctx context.Context, args []string, streams Streams) int {
	if streams.In == nil {
		streams.In = os.Stdin
	}
	root := newRootCmd(&env{ctx: ctx, streams: streams})
	root.SetArgs(args)
	root.SetIn(streams.In)
	root.SetOut(streams.Out)
	root.SetErr(streams.Err)

	err := root.ExecuteContext(ctx)
	if err != nil {
		report(streams.Err, err)
	}
	return exitCode(err)
}

// report prints the error of a failed run.
func report(w io.Writer, err error) {
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(w, "weread-exporter: interrupted")
		return
	}
	fmt.Fprintf(w, "weread-exporter: %v\n", err)
	var usage *app.UsageError
	if errors.As(err, &usage) {
		fmt.Fprintln(w, "Run 'weread-exporter --help' for usage.")
	}
}

// exitCode maps an error to the exit status.
func exitCode(err error) int {
	var usage *app.UsageError
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, context.Canceled):
		return ExitInterrupted
	case errors.As(err, &usage):
		return ExitUsage
	case errors.Is(err, app.ErrLoginNeeded):
		return ExitLoginNeeded
	}
	return ExitFailure
}

func usageError(format string, args ...any) error {
	return &app.UsageError{Msg: fmt.Sprintf(format, args...)}
}

// env is what the commands share.
type env struct {
	ctx     context.Context
	streams Streams
}

// exporter builds an exporter for the flags of a command.
func (e *env) exporter(c *commonFlags, cfg app.Config) (*app.Exporter, *slog.Logger, error) {
	log, err := newLogger(e.streams.Err, c.verbose, c.logFormat)
	if err != nil {
		return nil, nil, err
	}
	exp, err := app.New(cfg, log, app.WithOutput(e.streams.Out), app.WithInput(e.streams.In))
	if err != nil {
		return nil, nil, err
	}
	return exp, log, nil
}

// noArgs rejects stray arguments as a usage error.
func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usageError("unexpected argument %q for %q", args[0], cmd.CommandPath())
	}
	return nil
}

func newRootCmd(e *env) *cobra.Command {
	var (
		common  commonFlags
		browser browserFlags
		export  exportFlags
	)
	info := version.Get()
	root := &cobra.Command{
		Use:   "weread-exporter",
		Short: "Export WeChat Read (微信读书) books to EPUB, PDF, MOBI, TXT and Markdown",
		Long: `weread-exporter exports a book of WeChat Read (weread.qq.com) to EPUB, PDF,
MOBI, TXT or Markdown.

It opens the book in Chrome, reads the text of every chapter, and builds the
e-book from it. The chapters are cached, so an interrupted export carries on
where it stopped, and a book that is cached completely is exported without a
browser. WeRead only shows the text that your own account is allowed to read:
free chapters, or the whole book if you own it or have a reading membership.

Sign in once with "weread-exporter login"; the login is saved and renewed.
Please read the notes on responsible use in the README: WeRead may restrict
accounts that read too fast, and the exported books are for your own use.`,
		Example: `  # sign in with the WeChat app (once)
  weread-exporter login

  # export a book as EPUB; the id is in the book's URL
  weread-exporter -b 34336681

  # several formats at once, from a book URL
  weread-exporter -b https://weread.qq.com/web/bookDetail/3300040626 -o epub -o pdf -o md

  # only the free preview chapters, then look at the table of contents
  weread-exporter info -b 34336681
  weread-exporter -b 34336681 --chapters 41-51`,
		Version:       info.Short(),
		Args:          noArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if export.bookID == "" {
				if cmd.Flags().NFlag() == 0 {
					return cmd.Help()
				}
				return usageError("no book given: use -b/--book-id with a book id or URL")
			}
			return e.runExport(&common, &browser, &export)
		},
	}
	root.SetVersionTemplate("weread-exporter {{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return &app.UsageError{Msg: err.Error()} })
	root.CompletionOptions.HiddenDefaultCmd = false

	common.register(root.Flags())
	browser.register(root.Flags())
	export.register(root.Flags())
	registerCompletions(root)

	root.AddCommand(
		e.newLoginCmd(),
		e.newLogoutCmd(),
		e.newInfoCmd(),
		e.newReaderModeCmd(),
		newVersionCmd(e),
	)
	return root
}

// registerCompletions teaches the shell completion about the flag values.
func registerCompletions(root *cobra.Command) {
	_ = root.RegisterFlagCompletionFunc("output-format", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return app.Formats(), cobra.ShellCompDirectiveNoFileComp
	})
	_ = root.RegisterFlagCompletionFunc("log-format", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{logText, logJSON}, cobra.ShellCompDirectiveNoFileComp
	})
}

// runExport is the main command.
func (e *env) runExport(c *commonFlags, b *browserFlags, x *exportFlags) error {
	exp, log, err := e.exporter(c, buildConfig(c, b, x))
	if err != nil {
		return err
	}
	results, runErr := exp.Run(e.ctx)

	files := 0
	for _, res := range results {
		for _, f := range res.Files {
			files++
			fmt.Fprintln(e.streams.Out, f)
		}
	}
	if runErr == nil {
		log.Info("done", "books", len(results), "files", files)
	}
	return runErr
}

// flagError turns the complaints of the flag parser into usage errors.
func flagError(_ *cobra.Command, err error) error {
	return &app.UsageError{Msg: err.Error()}
}
