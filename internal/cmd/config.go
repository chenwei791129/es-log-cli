package cmd

import (
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/chenwei791129/es-log-cli/internal/config"
	"github.com/chenwei791129/es-log-cli/internal/output"
)

// configLongPrefix is the prose part of the `config` help body; the canonical
// template is appended verbatim so the two never drift.
const configLongPrefix = `Inspect configuration and scaffold a starter template.

es-log resolves its configuration file in this order of precedence:
  --config <path>
  $ES_LOG_CONFIG
  $XDG_CONFIG_HOME/es-log/config.yaml
  ~/.config/es-log/config.yaml

Run "es-log config init" to print a starter template, then redirect it to that
path, e.g. "es-log config init > ~/.config/es-log/config.yaml". The config is
read-only to es-log: it is never written or modified by any subcommand.

Example configuration:

`

// configDoc is the `config view` JSON document: the redacted contexts. The
// config types carry json tags so they marshal directly with no parallel structs.
type configDoc struct {
	Contexts []config.Context `json:"contexts"`
}

// newConfigCommand builds the `config` command group. It inspects configuration
// and can scaffold a template, but it never writes existing config: there are
// deliberately no set/use/delete subcommands and `init` only prints to stdout.
func newConfigCommand(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect configuration and scaffold a template",
		Long:  configLongPrefix + config.TemplateYAML,
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newGetContextsCommand(opts), newConfigViewCommand(opts), newConfigInitCommand(opts))
	return cmd
}

// newConfigInitCommand prints the canonical config template to stdout. It is a
// pure generator: it reads no file, writes no file, creates no directory, and
// registers no writing flag. It still validates -o through the shared formatFor
// path (an invalid value exits 2, matching every other subcommand) but ignores
// the validated value, since the template is fixed text regardless of format.
func newConfigInitCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Print a commented config template to stdout",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := opts.formatFor("config"); err != nil {
				return err
			}
			_, err := io.WriteString(cmd.OutOrStdout(), config.TemplateYAML)
			return err
		},
	}
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
