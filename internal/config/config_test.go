package config

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleYAML = `contexts:
  - name: prod
    server: https://es-prod:9200
    auth:
      type: apikey
      api-key: ${ES_PROD_API_KEY}
    tls:
      insecure-skip-verify: true
  - name: staging
    server: https://es-staging:9200
    auth:
      type: basic
      username: elastic
      password: ${ES_STAGING_PW}
`

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestConfigPathOverride asserts --config wins over $ES_LOG_CONFIG, which wins
// over the XDG default (task 2.1).
func TestConfigPathOverride(t *testing.T) {
	t.Setenv("ES_LOG_CONFIG", "/from/env.yaml")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	if got := ResolvePath("/from/flag.yaml"); got != "/from/flag.yaml" {
		t.Errorf("flag precedence: got %q", got)
	}
	if got := ResolvePath(""); got != "/from/env.yaml" {
		t.Errorf("env precedence: got %q", got)
	}

	t.Setenv("ES_LOG_CONFIG", "")
	if got := ResolvePath(""); got != "/xdg/es-log/config.yaml" {
		t.Errorf("xdg default: got %q", got)
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "es-log", "config.yaml")
	if got := ResolvePath(""); got != want {
		t.Errorf("home default: got %q, want %q", got, want)
	}
}

func TestLoadAndContextNames(t *testing.T) {
	cfg, err := Load(writeConfig(t, sampleYAML))
	if err != nil {
		t.Fatal(err)
	}
	names := cfg.ContextNames()
	if len(names) != 2 || names[0] != "prod" || names[1] != "staging" {
		t.Errorf("ContextNames = %v", names)
	}
	if _, ok := cfg.Find("prod"); !ok {
		t.Error("Find(prod) not found")
	}
	if _, ok := cfg.Find("nope"); ok {
		t.Error("Find(nope) should not be found")
	}
}

// TestEnvExpansion covers expansion-on-use and the unset-variable error
// (task 2.2). The "get-contexts succeeds despite unset" path is covered at the
// command level.
func TestEnvExpansion(t *testing.T) {
	cfg, err := Load(writeConfig(t, sampleYAML))
	if err != nil {
		t.Fatal(err)
	}
	prod, _ := cfg.Find("prod")

	t.Setenv("ES_PROD_API_KEY", "abc123")
	expanded, err := ExpandSecrets(*prod)
	if err != nil {
		t.Fatalf("expand defined: %v", err)
	}
	if expanded.Auth.APIKey != "abc123" {
		t.Errorf("api-key = %q, want abc123", expanded.Auth.APIKey)
	}

	staging, _ := cfg.Find("staging")
	// ES_STAGING_PW is unset -> error.
	if _, err := ExpandSecrets(*staging); err == nil {
		t.Error("expected error for unset variable in used context")
	}
}

// TestConfigViewRedaction asserts api-key/password redact to *** while username
// and server stay visible, and redaction never needs env expansion (task 2.5).
func TestConfigViewRedaction(t *testing.T) {
	cfg, err := Load(writeConfig(t, sampleYAML))
	if err != nil {
		t.Fatal(err)
	}
	prod, _ := cfg.Find("prod")
	red := Redact(*prod)
	if red.Auth.APIKey != "***" {
		t.Errorf("api-key not redacted: %q", red.Auth.APIKey)
	}

	staging, _ := cfg.Find("staging")
	redS := Redact(*staging)
	if redS.Auth.Password != "***" {
		t.Errorf("password not redacted: %q", redS.Auth.Password)
	}
	if redS.Auth.Username != "elastic" {
		t.Errorf("username should remain visible: %q", redS.Auth.Username)
	}
	if redS.Server != "https://es-staging:9200" {
		t.Errorf("server should remain visible: %q", redS.Server)
	}
}

// TestRedactServerURL asserts inline userinfo passwords in the server URL are
// masked while the rest of the URL (and credential-free URLs) stay intact.
func TestRedactServerURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://elastic:s3cret@es-prod:9200", "https://elastic:***@es-prod:9200"},
		{"https://es-prod:9200", "https://es-prod:9200"},
		{"https://user@es:9200", "https://user@es:9200"},
		{"elastic:s3cret@es:9200", "elastic:***@es:9200"},              // scheme-less
		{"https://u:p@es:9200/path@x", "https://u:***@es:9200/path@x"}, // '@' in path untouched
	}
	for _, c := range cases {
		if got := RedactServerURL(c.in); got != c.want {
			t.Errorf("RedactServerURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// Redact() must also scrub credentials embedded in the server URL.
	red := Redact(Context{Server: "https://elastic:s3cret@es:9200"})
	if red.Server != "https://elastic:***@es:9200" {
		t.Errorf("Redact did not scrub server creds: %q", red.Server)
	}
}
