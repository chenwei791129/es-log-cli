package cmd

import (
	"github.com/spf13/cobra"

	"github.com/chenwei791129/es-log-cli/internal/config"
)

// newPingCommand builds the `ping` subcommand, which verifies connectivity and
// authentication against the active context's cluster.
func newPingCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "ping",
		Short: "Check connectivity and authentication for a context",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := opts.loadConfig()
			if err != nil {
				return err
			}
			client, ctx, err := opts.buildClient(cfg)
			if err != nil {
				return err
			}
			server := config.RedactServerURL(ctx.Server)
			if err := client.Ping(cmd.Context()); err != nil {
				return newExitError(exitConn, "ping %s failed: %v", server, err)
			}
			p := opts.printer(cmd)
			if !p.Quiet {
				fprintln(cmd.OutOrStdout(), "ok:", server)
			}
			return nil
		},
	}
}
