package cmd

import (
	"strconv"

	"github.com/spf13/cobra"

	"github.com/chenwei791129/es-log-cli/internal/config"
	"github.com/chenwei791129/es-log-cli/internal/output"
)

// configDoc is the `config view` JSON document: the redacted contexts. The
// config types carry json tags so they marshal directly with no parallel structs.
type configDoc struct {
	Contexts []config.Context `json:"contexts"`
}

// newConfigCommand builds the `config` command group (read-only inspection only;
// there are deliberately no write/use/delete subcommands).
func newConfigCommand(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect configuration (read-only)",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newGetContextsCommand(opts), newConfigViewCommand(opts))
	return cmd
}

// newGetContextsCommand lists the names of all configured contexts. It does not
// require a context to be selected and never expands secrets.
func newGetContextsCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get-contexts",
		Short: "List configured context names",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := opts.loadConfig()
			if err != nil {
				return err
			}
			format, err := opts.formatFor("config")
			if err != nil {
				return err
			}
			names := cfg.ContextNames()
			out := cmd.OutOrStdout()
			switch format {
			case output.FormatTable:
				rows := make([][]string, 0, len(names))
				for _, n := range names {
					rows = append(rows, []string{n})
				}
				return output.RenderTable(out, []string{"NAME"}, rows)
			case output.FormatJSONL:
				return output.WriteJSONLines(out, names)
			default:
				return output.WriteJSON(out, names)
			}
		},
	}
}

// newConfigViewCommand prints the resolved configuration with secrets redacted.
// It never expands ${ENV_VAR}, so it cannot fail on unset variables.
func newConfigViewCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "view",
		Short: "Print resolved configuration with secrets redacted",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := opts.loadConfig()
			if err != nil {
				return err
			}
			format, err := opts.formatFor("config")
			if err != nil {
				return err
			}
			doc := configDoc{Contexts: make([]config.Context, 0, len(cfg.Contexts))}
			for _, c := range cfg.Contexts {
				doc.Contexts = append(doc.Contexts, config.Redact(c))
			}
			out := cmd.OutOrStdout()
			if format == output.FormatTable {
				rows := make([][]string, 0, len(doc.Contexts))
				for _, c := range doc.Contexts {
					rows = append(rows, []string{
						c.Name, c.Server, c.Auth.Type,
						c.Auth.Username, c.Auth.APIKey, c.Auth.Password,
						c.TLS.CACert, strconv.FormatBool(c.TLS.InsecureSkipVerify),
					})
				}
				return output.RenderTable(out, []string{
					"NAME", "SERVER", "AUTH", "USERNAME", "API-KEY", "PASSWORD", "CA-CERT", "INSECURE",
				}, rows)
			}
			if format == output.FormatJSONL {
				// Single object emitted as one compact line for line-oriented consumers.
				return output.WriteJSONLines(out, []configDoc{doc})
			}
			return output.WriteJSON(out, doc)
		},
	}
}
