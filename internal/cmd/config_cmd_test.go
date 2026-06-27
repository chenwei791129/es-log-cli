package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenwei791129/es-log-cli/internal/config"
)

func writeMultiContextConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const twoContexts = `contexts:
  - name: prod
    server: https://es-prod:9200
    auth:
      type: apikey
      api-key: ${ES_PROD_KEY}
  - name: staging
    server: https://es-staging:9200
    auth:
      type: basic
      username: elastic
      password: ${MISSING_VAR}
`

func TestGetContexts(t *testing.T) {
	cfg := writeMultiContextConfig(t, twoContexts)
	res := runCLI(t, context.Background(), "config", "get-contexts", "--config", cfg)
	if res.code != 0 {
		t.Fatalf("exit %d: %s", res.code, res.stderr)
	}
	if !strings.Contains(res.stdout, "prod") || !strings.Contains(res.stdout, "staging") {
		t.Errorf("missing context names: %q", res.stdout)
	}
}

// TestGetContextsUnsetVarTolerant asserts get-contexts succeeds even though
// `staging` references an unset variable (task 2.2).
func TestGetContextsUnsetVarTolerant(t *testing.T) {
	t.Setenv("MISSING_VAR", "")
	_ = os.Unsetenv("MISSING_VAR")
	cfg := writeMultiContextConfig(t, twoContexts)
	res := runCLI(t, context.Background(), "config", "get-contexts", "--config", cfg)
	if res.code != 0 {
		t.Errorf("get-contexts should tolerate unset var, exit %d: %s", res.code, res.stderr)
	}
}

// TestGetContextsJSONArray asserts the output is a JSON string array (task 4.6).
func TestGetContextsJSONArray(t *testing.T) {
	cfg := writeMultiContextConfig(t, twoContexts)
	res := runCLI(t, context.Background(), "config", "get-contexts", "-o", "json", "--config", cfg)
	var names []string
	if err := json.Unmarshal([]byte(res.stdout), &names); err != nil {
		t.Fatalf("not a JSON string array: %v\n%s", err, res.stdout)
	}
	if len(names) != 2 || names[0] != "prod" || names[1] != "staging" {
		t.Errorf("names = %v", names)
	}
}

// TestConfigViewCommandRedaction asserts secrets are redacted and never printed
// raw, without requiring env expansion (task 2.5).
func TestConfigViewCommandRedaction(t *testing.T) {
	body := `contexts:
  - name: prod
    server: https://es:9200
    auth:
      type: apikey
      api-key: rawsecret
`
	cfg := writeMultiContextConfig(t, body)
	res := runCLI(t, context.Background(), "config", "view", "--config", cfg)
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	if !strings.Contains(res.stdout, "***") {
		t.Errorf("expected redaction marker: %q", res.stdout)
	}
	if strings.Contains(res.stdout, "rawsecret") {
		t.Errorf("raw secret leaked: %q", res.stdout)
	}
}

// TestContextResolution covers flag precedence, env fallback, and the missing
// context error (task 2.3).
func TestContextResolution(t *testing.T) {
	stub := newESStub(t, func(r recordedReq) (int, string) {
		if r.path == "/_alias" {
			return 200, `{}`
		}
		return 200, `{"data_streams":[]}`
	})
	cfg := writeTestConfig(t, stub.url()) // single context "test"

	// Missing context -> exit 2, empty stdout.
	t.Setenv("ES_LOG_CONTEXT", "")
	_ = os.Unsetenv("ES_LOG_CONTEXT")
	res := runCLI(t, context.Background(), "ls", "--config", cfg)
	if res.code != 2 || res.stdout != "" {
		t.Errorf("missing context: exit %d stdout %q", res.code, res.stdout)
	}

	// Env fallback -> reaches the cluster (exit 0).
	t.Setenv("ES_LOG_CONTEXT", "test")
	res = runCLI(t, context.Background(), "ls", "--config", cfg)
	if res.code != 0 {
		t.Errorf("env fallback: exit %d (%s)", res.code, res.stderr)
	}

	// Flag wins over a bogus env value -> still exit 0 (flag precedence).
	t.Setenv("ES_LOG_CONTEXT", "does-not-exist")
	res = runCLI(t, context.Background(), "ls", "-c", "test", "--config", cfg)
	if res.code != 0 {
		t.Errorf("flag precedence: exit %d (%s)", res.code, res.stderr)
	}
}

// isEnvRef reports whether s is a ${VAR} reference.
func isEnvRef(s string) bool {
	return strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}")
}

// TestConfigInitTemplateLoads asserts `config init` exits 0 and prints a template
// that loads through the config loader with both auth contexts populated at the
// field level — catching a key typo that would silently drop a field (task 3.1a).
func TestConfigInitTemplateLoads(t *testing.T) {
	res := runCLI(t, context.Background(), "config", "init")
	if res.code != 0 {
		t.Fatalf("exit %d: %s", res.code, res.stderr)
	}
	cfg, err := config.Load(writeMultiContextConfig(t, res.stdout))
	if err != nil {
		t.Fatalf("template does not load: %v", err)
	}
	var apikey, basic *config.Context
	for i := range cfg.Contexts {
		switch cfg.Contexts[i].Auth.Type {
		case "apikey":
			apikey = &cfg.Contexts[i]
		case "basic":
			basic = &cfg.Contexts[i]
		}
	}
	if apikey == nil {
		t.Fatal("template has no apikey context")
	}
	if !isEnvRef(apikey.Auth.APIKey) {
		t.Errorf("apikey api-key is not a ${...} ref: %q", apikey.Auth.APIKey)
	}
	if basic == nil {
		t.Fatal("template has no basic context")
	}
	if !isEnvRef(basic.Auth.Password) {
		t.Errorf("basic password is not a ${...} ref: %q", basic.Auth.Password)
	}
}

// TestConfigInitWritesNothing asserts `config init` reads and creates no file,
// succeeding even when the resolved --config path does not exist (task 3.1b).
func TestConfigInitWritesNothing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent", "config.yaml")
	res := runCLI(t, context.Background(), "config", "init", "--config", missing)
	if res.code != 0 {
		t.Fatalf("exit %d: %s", res.code, res.stderr)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("config init created %q (stat err=%v)", missing, err)
	}
	if _, err := os.Stat(filepath.Dir(missing)); !os.IsNotExist(err) {
		t.Errorf("config init created parent dir %q", filepath.Dir(missing))
	}

	// With no --config, it must also leave the default-resolved path untouched.
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("ES_LOG_CONFIG", "")
	_ = os.Unsetenv("ES_LOG_CONFIG")
	if res := runCLI(t, context.Background(), "config", "init"); res.code != 0 {
		t.Fatalf("default-path init exit %d: %s", res.code, res.stderr)
	}
	defaultPath := filepath.Join(xdg, "es-log", "config.yaml")
	if _, err := os.Stat(defaultPath); !os.IsNotExist(err) {
		t.Errorf("config init wrote default path %q", defaultPath)
	}
}

// TestConfigInitOutputFlag asserts -o is validated like every other subcommand
// (bogus -> exit 2, matching `config view`) while a valid value leaves the fixed
// template byte-for-byte unchanged (task 3.1c).
func TestConfigInitOutputFlag(t *testing.T) {
	bogus := runCLI(t, context.Background(), "config", "init", "-o", "bogus")
	if bogus.code != 2 {
		t.Errorf("bogus -o: exit %d, want 2 (%s)", bogus.code, bogus.stderr)
	}
	viewBogus := runCLI(t, context.Background(), "config", "view", "-o", "bogus",
		"--config", writeMultiContextConfig(t, twoContexts))
	if viewBogus.code != bogus.code {
		t.Errorf("init/view diverge on bogus -o: %d vs %d", bogus.code, viewBogus.code)
	}

	// Both the default output and -o json must emit the canonical template
	// verbatim — pinning the actual bytes, not merely asserting they equal each
	// other (which a fixed writer satisfies trivially).
	plain := runCLI(t, context.Background(), "config", "init")
	if plain.stdout != config.TemplateYAML {
		t.Errorf("default output is not the canonical template:\n%q", plain.stdout)
	}
	asJSON := runCLI(t, context.Background(), "config", "init", "-o", "json")
	if asJSON.stdout != config.TemplateYAML {
		t.Errorf("-o json changed the template:\n%q", asJSON.stdout)
	}
}

// TestConfigHelpDocumentsFormat asserts `config --help` documents the default
// path and override env, and embeds the contexts example (task 3.2).
func TestConfigHelpDocumentsFormat(t *testing.T) {
	res := runCLI(t, context.Background(), "config", "--help")
	for _, want := range []string{"~/.config/es-log/config.yaml", "ES_LOG_CONFIG", "contexts:"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("config --help missing %q\n%s", want, res.stdout)
		}
	}
}
