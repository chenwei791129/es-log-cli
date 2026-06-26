package esclient

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
)

// Authentication mode identifiers.
const (
	AuthAPIKey = "apikey"
	AuthBasic  = "basic"
)

// AuthConfig selects one of the two supported authentication modes. Secrets are
// expected to be already env-expanded by the caller.
type AuthConfig struct {
	Type     string // AuthAPIKey | AuthBasic | "" (none)
	APIKey   string
	Username string
	Password string
}

// TLSConfig holds per-context TLS settings.
type TLSConfig struct {
	CACert             string // path to a PEM CA bundle
	InsecureSkipVerify bool
}

// validate rejects unknown authentication modes and modes missing their
// required secret(s). An empty type means no authentication (valid for unsecured
// clusters); only apikey and basic are otherwise supported.
func (a AuthConfig) validate() error {
	switch a.Type {
	case "":
		return nil
	case AuthAPIKey:
		if a.APIKey == "" {
			return fmt.Errorf("auth.type %q requires a non-empty api-key", AuthAPIKey)
		}
	case AuthBasic:
		if a.Username == "" || a.Password == "" {
			return fmt.Errorf("auth.type %q requires non-empty username and password", AuthBasic)
		}
	default:
		return fmt.Errorf("unsupported auth.type %q: want %q or %q", a.Type, AuthAPIKey, AuthBasic)
	}
	return nil
}

// apply sets the Authorization header for an outgoing request based on the
// configured auth mode. No header is set when no mode is selected.
func (a AuthConfig) apply(req *http.Request) {
	switch a.Type {
	case AuthAPIKey:
		req.Header.Set("Authorization", "ApiKey "+a.APIKey)
	case AuthBasic:
		req.SetBasicAuth(a.Username, a.Password)
	}
}

// buildTLSConfig translates TLSConfig into a *tls.Config, loading a custom CA
// bundle when configured.
func buildTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	out := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.InsecureSkipVerify, //nolint:gosec // honored only when the context opts in
	}
	if cfg.CACert != "" {
		pem, err := os.ReadFile(cfg.CACert)
		if err != nil {
			return nil, fmt.Errorf("read ca-cert %q: %w", cfg.CACert, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ca-cert %q contains no valid certificate", cfg.CACert)
		}
		out.RootCAs = pool
	}
	return out, nil
}
