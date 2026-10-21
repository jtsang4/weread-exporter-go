package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jtsang4/weread-exporter-go/internal/version"
)

func newVersionCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:           "version",
		Short:         "Print the version and build information",
		Args:          noArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		Run: func(*cobra.Command, []string) {
			fmt.Fprint(e.streams.Out, version.Get().String())
		},
	}
}
