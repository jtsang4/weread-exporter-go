package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (e *env) newLoginCmd() *cobra.Command {
	var (
		common  commonFlags
		browser browserFlags
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in to WeRead with the WeChat app and save the login",
		Long: `Sign in to WeRead and save the login (the cookies) in the cache directory.

If the saved login still works nothing happens. Otherwise Chrome opens the
WeRead page and shows a QR code: scan it with the WeChat app and confirm on the
phone. The login is renewed automatically while you export, so this is rarely
needed; use --force-login to replace a login that works.

A headless Chrome cannot show the QR code, so run this command (without
--headless) once, and use --headless for the exports afterwards.`,
		Example: `  weread-exporter login
  weread-exporter login --force-login`,
		Args:          noArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(*cobra.Command, []string) error {
			exp, _, err := e.exporter(&common, buildConfig(&common, &browser, nil))
			if err != nil {
				return err
			}
			res, err := exp.Login(e.ctx)
			if err != nil {
				return err
			}
			if res.AlreadySignedIn {
				fmt.Fprintf(e.streams.Out, "Already signed in as %s (login file: %s)\n", res.User.Name, res.CookieFile)
			} else {
				fmt.Fprintf(e.streams.Out, "Signed in as %s; the login was saved to %s\n", res.User.Name, res.CookieFile)
			}
			return nil
		},
	}
	cmd.SetFlagErrorFunc(flagError)
	common.register(cmd.Flags())
	browser.register(cmd.Flags())
	return cmd
}

func (e *env) newLogoutCmd() *cobra.Command {
	var common commonFlags
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Delete the saved WeRead login",
		Long: `Delete the saved login (the cookie file). Chapters that were captured stay in
the cache. This does not sign the account out on weread.qq.com or in your
browser.`,
		Args:          noArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(*cobra.Command, []string) error {
			exp, _, err := e.exporter(&common, buildConfig(&common, nil, nil))
			if err != nil {
				return err
			}
			removed, err := exp.Logout(e.ctx)
			if err != nil {
				return err
			}
			if removed {
				fmt.Fprintf(e.streams.Out, "Deleted the saved login %s\n", exp.Config().CookieFile)
			} else {
				fmt.Fprintf(e.streams.Out, "There was no saved login (%s)\n", exp.Config().CookieFile)
			}
			return nil
		},
	}
	cmd.SetFlagErrorFunc(flagError)
	common.register(cmd.Flags())
	return cmd
}
