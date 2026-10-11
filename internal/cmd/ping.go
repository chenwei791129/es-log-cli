package cmd

import (
	"github.com/spf13/cobra"

	"github.com/chenwei791129/es-log-cli/internal/config"
	"github.com/chenwei791129/es-log-cli/internal/esclient"
)

// newPingCommand builds the `ping` subcommand, which verifies connectivity and
// authentication against the active context's cluster and reports the server
// version.
func newPingCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "ping",
		Short: "Check connectivity and authentication for a context and report the server version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := opts.loadConfig()
			if err != nil {
				return err
			}
			p := opts.printer(cmd)
			client, ctx, err := opts.buildClient(cfg, p)
			if err != nil {
				return err
			}
			server := config.RedactServerURL(ctx.Server)
			v, err := client.Info(cmd.Context())
			if err != nil {
				return newExitError(exitConn, "ping %s failed: %v", server, err)
			}
			if !p.Quiet {
				fprintln(cmd.OutOrStdout(), "ok:", server, "("+describeVersion(v)+")")
			}
			return nil
		},
	}
}

// describeVersion renders the server version for the ping line, e.g.
// "Elasticsearch 9.5.5", "opensearch 2.11.0", or "version unknown".
func describeVersion(v esclient.Version) string {
	switch {
	case v.Number == "":
		return "version unknown"
	case v.Distribution != "":
		return v.Distribution + " " + v.Number
	default:
		return "Elasticsearch " + v.Number
	}
}
