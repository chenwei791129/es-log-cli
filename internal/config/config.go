// Package config loads the flat es-log configuration file, resolves the active
// context (without any hidden current-context state), expands ${ENV_VAR} secrets
// on use, and redacts secrets for display.
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration document. It deliberately has no
// current-context field — context selection is always explicit.
type Config struct {
	Contexts []Context `yaml:"contexts"`
}

// Context describes one Elasticsearch cluster connection (flat, non-kubectl).
// The json tags mirror the file's hyphenated keys so a redacted Context can be
// marshaled directly for `config view`.
type Context struct {
	Name   string `yaml:"name" json:"name"`
	Server string `yaml:"server" json:"server"`
	Auth   Auth   `yaml:"auth" json:"auth"`
	TLS    TLS    `yaml:"tls" json:"tls"`
}

// Auth holds per-context authentication settings.
type Auth struct {
	Type     string `yaml:"type" json:"type,omitempty"` // apikey | basic
	APIKey   string `yaml:"api-key" json:"api-key,omitempty"`
	Username string `yaml:"username" json:"username,omitempty"`
	Password string `yaml:"password" json:"password,omitempty"`
}

// TLS holds per-context TLS settings.
type TLS struct {
	CACert             string `yaml:"ca-cert" json:"ca-cert,omitempty"`
	InsecureSkipVerify bool   `yaml:"insecure-skip-verify" json:"insecure-skip-verify,omitempty"`
}

// Load reads and parses the configuration file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	return &cfg, nil
}

// ContextNames returns the names of all configured contexts in file order.
func (c *Config) ContextNames() []string {
	names := make([]string, 0, len(c.Contexts))
	for _, ctx := range c.Contexts {
		names = append(names, ctx.Name)
	}
	return names
}

// Find returns the context with the given name.
func (c *Config) Find(name string) (*Context, bool) {
	for i := range c.Contexts {
		if c.Contexts[i].Name == name {
			return &c.Contexts[i], true
		}
	}
	return nil, false
}

// Redact returns a copy of ctx with secret fields replaced by "***". It does not
// expand environment variables, so it never fails on unset variables.
func Redact(ctx Context) Context {
	out := ctx
	if out.Auth.APIKey != "" {
		out.Auth.APIKey = "***"
	}
	if out.Auth.Password != "" {
		out.Auth.Password = "***"
	}
	out.Server = RedactServerURL(out.Server)
	return out
}

// RedactServerURL masks any password embedded in a server URL's userinfo
// (e.g. https://user:secret@host -> https://user:***@host), leaving the rest of
// the URL intact. It works on the raw string rather than url.Parse so it also
// covers scheme-less values (user:secret@host) where url.Parse would not populate
// userinfo. A URL without inline credentials is returned unchanged.
func RedactServerURL(server string) string {
	rest, prefix := server, ""
	if i := strings.Index(server, "://"); i >= 0 {
		prefix, rest = server[:i+3], server[i+3:]
	}
	// The authority ends at the first '/', '?', or '#'; '@' beyond it is path data.
	authEnd := len(rest)
	for _, sep := range []string{"/", "?", "#"} {
		if j := strings.Index(rest, sep); j >= 0 && j < authEnd {
			authEnd = j
		}
	}
	auth, tail := rest[:authEnd], rest[authEnd:]
	at := strings.LastIndex(auth, "@")
	if at < 0 {
		return server // no userinfo
	}
	userinfo, host := auth[:at], auth[at:]
	colon := strings.Index(userinfo, ":")
	if colon < 0 {
		return server // username only, no password to mask
	}
	return prefix + userinfo[:colon] + ":***" + host + tail
}
