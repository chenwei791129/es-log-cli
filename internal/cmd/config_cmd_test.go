package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
