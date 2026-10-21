package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jtsang4/weread-exporter-go/internal/weread"
)

func (e *env) newReaderModeCmd() *cobra.Command {
	var common commonFlags
	cmd := &cobra.Command{
		Use:   "reader-mode",
		Short: "Show or change the reader mode of your WeRead account",
		Long: `Show or change the reader mode of your WeRead account.

The web reader has two layouts: the paged, two-column "horizontal" reader (the
default for new accounts) and the scrolling, single-column "vertical" reader
(上下滚动阅读). The mode is a setting of the account, not of the browser, and the
exporter can only read the vertical layout. So an export switches the account to
vertical and puts the previous mode back when it finishes, even if it fails or is
interrupted. If the process is killed before it could do that, the next run (or
"weread-exporter reader-mode restore") restores the mode.

Without a subcommand this shows the current mode.`,
		Args:          noArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          func(*cobra.Command, []string) error { return e.showReaderMode(&common) },
	}
	cmd.SetFlagErrorFunc(flagError)
	common.register(cmd.PersistentFlags())

	cmd.AddCommand(
		&cobra.Command{
			Use:           "status",
			Short:         "Show the current reader mode (the default)",
			Args:          noArgs,
			SilenceUsage:  true,
			SilenceErrors: true,
			RunE:          func(*cobra.Command, []string) error { return e.showReaderMode(&common) },
		},
		e.setReaderModeCmd(&common, "vertical", weread.ReaderModeVertical, "Switch the account to the scrolling, single-column reader"),
		e.setReaderModeCmd(&common, "horizontal", weread.ReaderModeHorizontal, "Switch the account to the paged, two-column reader"),
		&cobra.Command{
			Use:   "restore",
			Short: "Undo the switch an interrupted export left behind",
			Long: `Put the account's reader mode back to what it was before an export that was
killed (or could not reach the site) switched it to vertical. Does nothing if
nothing is pending.`,
			Args:          noArgs,
			SilenceUsage:  true,
			SilenceErrors: true,
			RunE: func(*cobra.Command, []string) error {
				exp, _, err := e.exporter(&common, buildConfig(&common, nil, nil))
				if err != nil {
					return err
				}
				restored, err := exp.RestoreReaderMode(e.ctx)
				if err != nil {
					return err
				}
				if restored {
					fmt.Fprintln(e.streams.Out, "The reader mode was restored.")
				} else {
					fmt.Fprintln(e.streams.Out, "Nothing to restore.")
				}
				return nil
			},
		},
	)
	return cmd
}

func (e *env) setReaderModeCmd(common *commonFlags, name string, mode weread.ReaderMode, short string) *cobra.Command {
	return &cobra.Command{
		Use:           name,
		Short:         short,
		Args:          noArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(*cobra.Command, []string) error {
			exp, _, err := e.exporter(common, buildConfig(common, nil, nil))
			if err != nil {
				return err
			}
			if err := exp.SetReaderMode(e.ctx, mode); err != nil {
				return err
			}
			fmt.Fprintf(e.streams.Out, "The account now uses the %s reader.\n", mode)
			return nil
		},
	}
}

func (e *env) showReaderMode(common *commonFlags) error {
	exp, _, err := e.exporter(common, buildConfig(common, nil, nil))
	if err != nil {
		return err
	}
	status, err := exp.ReaderMode(e.ctx)
	if err != nil {
		return err
	}
	desc := map[weread.ReaderMode]string{
		weread.ReaderModeVertical:   "scrolling, single column; what the exporter reads",
		weread.ReaderModeHorizontal: "paged, two columns; the exporter switches it for the duration of an export",
	}[status.Mode]
	fmt.Fprintf(e.streams.Out, "Reader mode: %s (%s)\n", status.Mode, desc)
	if p := status.Pending; p != nil {
		fmt.Fprintf(e.streams.Out, "An export that did not finish switched it on %s; `weread-exporter reader-mode restore` puts it back to %s.\n",
			p.Since.Local().Format("2006-01-02 15:04"), p.Restore)
	}
	return nil
}
