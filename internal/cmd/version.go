package cmd

import "github.com/spf13/cobra"

// version is the binary version string, overridable at build time via
// -ldflags "-X github.com/chenwei791129/es-log-cli/internal/cmd.version=...".
var version = "dev"

// newVersionCommand builds the `version` subcommand, which needs no context.
func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the es-log version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fprintln(cmd.OutOrStdout(), version)
			return nil
		},
	}
}
