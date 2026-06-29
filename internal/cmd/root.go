// Package cmd wires the es-log cobra command tree, global flags, context
// resolution, and layered exit-code handling.
package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/chenwei791129/es-log-cli/internal/config"
	"github.com/chenwei791129/es-log-cli/internal/esclient"
	"github.com/chenwei791129/es-log-cli/internal/output"
)

// globalOptions holds the values of the root persistent flags, shared with every
// subcommand.
type globalOptions struct {
	contextName string
	output      string
	quiet       bool
	configPath  string
}

// NewRootCommand builds the root command with all subcommands mounted and the
// documented global persistent flags registered.
func NewRootCommand() *cobra.Command {
	opts := &globalOptions{}
	root := &cobra.Command{
		Use:           "es-log",
		Short:         "Read-only Elasticsearch log query CLI for AI agents",
		Long:          "es-log is a read-only Elasticsearch CLI that exposes only safe query operations, with agent-friendly JSONL output and layered exit codes.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	pf := root.PersistentFlags()
	pf.StringVarP(&opts.contextName, "context", "c", "", "context name to use (overrides $ES_LOG_CONTEXT)")
	pf.StringVarP(&opts.output, "output", "o", "", "output format: jsonl|json|table")
	pf.BoolVar(&opts.quiet, "quiet", false, "suppress non-result output (warnings, progress)")
	pf.StringVar(&opts.configPath, "config", "", "config file path (overrides $ES_LOG_CONFIG)")

	root.AddCommand(
		newConfigCommand(opts),
		newLsCommand(opts),
		newSearchCommand(opts),
		newFieldsCommand(opts),
		newPingCommand(opts),
		newVersionCommand(),
	)
	return root
}

// Execute runs the root command with the given args and writers, returning the
// process exit code.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	root := NewRootCommand()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		return reportError(stderr, err)
	}
	return exitOK
}

// printer builds an output.Printer bound to the command's writers.
func (o *globalOptions) printer(cmd *cobra.Command) *output.Printer {
	return &output.Printer{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr(), Quiet: o.quiet}
}

// formatFor returns the resolved output format for a command, validating any
// explicit --output value.
func (o *globalOptions) formatFor(command string) (string, error) {
	if o.output == "" {
		return output.DefaultFor(command), nil
	}
	if !output.Valid(o.output) {
		return "", newExitError(exitUsage, "invalid output format %q: want jsonl, json, or table", o.output)
	}
	return o.output, nil
}

// loadConfig resolves the config path and loads the configuration file.
func (o *globalOptions) loadConfig() (*config.Config, error) {
	path := config.ResolvePath(o.configPath)
	cfg, err := config.Load(path)
	if err != nil {
		return nil, newExitError(exitUsage, "%v", err)
	}
	return cfg, nil
}

// resolveContextName returns the active context name from --context or
// $ES_LOG_CONTEXT, in that order.
func (o *globalOptions) resolveContextName() string {
	if o.contextName != "" {
		return o.contextName
	}
	return os.Getenv("ES_LOG_CONTEXT")
}

// buildClient resolves the active context, expands its secrets, and constructs a
// read-only Elasticsearch client. It is used by ES-contacting commands.
func (o *globalOptions) buildClient(cfg *config.Config) (*esclient.Client, config.Context, error) {
	name := o.resolveContextName()
	if name == "" {
		return nil, config.Context{}, newExitError(exitUsage,
			"no context selected: pass --context/-c or set $ES_LOG_CONTEXT\navailable contexts: %v",
			cfg.ContextNames())
	}
	ctx, ok := cfg.Find(name)
	if !ok {
		return nil, config.Context{}, newExitError(exitUsage,
			"context %q not found\navailable contexts: %v", name, cfg.ContextNames())
	}
	expanded, err := config.ExpandSecrets(*ctx)
	if err != nil {
		return nil, config.Context{}, newExitError(exitUsage, "context %q: %v", name, err)
	}
	client, err := esclient.New(esclient.Config{
		Server: expanded.Server,
		Auth: esclient.AuthConfig{
			Type:     expanded.Auth.Type,
			APIKey:   expanded.Auth.APIKey,
			Username: expanded.Auth.Username,
			Password: expanded.Auth.Password,
		},
		TLS: esclient.TLSConfig{
			CACert:             expanded.TLS.CACert,
			InsecureSkipVerify: expanded.TLS.InsecureSkipVerify,
		},
	})
	if err != nil {
		return nil, config.Context{}, newExitError(exitUsage, "context %q: %v", name, err)
	}
	return client, expanded, nil
}

// fprintln writes a line to the given writer, ignoring write errors (output is
// best-effort for terminal printing).
func fprintln(w io.Writer, args ...any) {
	_, _ = fmt.Fprintln(w, args...)
}
