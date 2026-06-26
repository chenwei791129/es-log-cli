package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// envVarPattern matches ${VAR} references inside string values.
var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ResolvePath determines the config file path with precedence:
// --config flag > $ES_LOG_CONFIG > $XDG_CONFIG_HOME/es-log/config.yaml >
// ~/.config/es-log/config.yaml.
func ResolvePath(flagPath string) string {
	if flagPath != "" {
		return flagPath
	}
	if env := os.Getenv("ES_LOG_CONFIG"); env != "" {
		return env
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "es-log", "config.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return filepath.Join(home, ".config", "es-log", "config.yaml")
}

// ExpandSecrets returns a copy of ctx with ${ENV_VAR} references expanded in its
// string fields using the process environment. A reference to an unset variable
// is an error — this is called only for the context actually being used.
func ExpandSecrets(ctx Context) (Context, error) {
	out := ctx
	fields := []*string{
		&out.Server,
		&out.Auth.APIKey,
		&out.Auth.Username,
		&out.Auth.Password,
		&out.TLS.CACert,
	}
	for _, f := range fields {
		expanded, err := expandValue(*f)
		if err != nil {
			return Context{}, err
		}
		*f = expanded
	}
	return out, nil
}

// expandValue replaces every ${VAR} in s with its environment value, returning
// an error if any referenced variable is unset.
func expandValue(s string) (string, error) {
	var firstErr error
	result := envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := envVarPattern.FindStringSubmatch(match)[1]
		val, ok := os.LookupEnv(name)
		if !ok {
			if firstErr == nil {
				firstErr = fmt.Errorf("environment variable %s is not set", name)
			}
			return match
		}
		return val
	})
	if firstErr != nil {
		return "", firstErr
	}
	return result, nil
}
