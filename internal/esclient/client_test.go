package esclient

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// certToPEM encodes an x509 certificate into PEM bytes for use as a CA file.
func certToPEM(t *testing.T, cert *x509.Certificate) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

// recordingServer captures the verb/path of every request and replies with body.
type captured struct {
	method string
	path   string
	query  string
	header http.Header
}

func newStub(t *testing.T, status int, body string, sink *[]captured) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*sink = append(*sink, captured{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Clone()})
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func newClient(t *testing.T, server string, auth AuthConfig) *Client {
	t.Helper()
	c, err := New(Config{Server: server, Auth: auth})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestListAliases(t *testing.T) {
	var reqs []captured
	srv := newStub(t, 200, `{}`, &reqs)
	defer srv.Close()

	if _, err := newClient(t, srv.URL, AuthConfig{}).ListAliases(context.Background()); err != nil {
		t.Fatalf("ListAliases: %v", err)
	}
	if reqs[0].method != http.MethodGet || reqs[0].path != "/_alias" {
		t.Errorf("got %s %s, want GET /_alias", reqs[0].method, reqs[0].path)
	}
}

func TestListDataStreams(t *testing.T) {
	var reqs []captured
	srv := newStub(t, 200, `{"data_streams":[]}`, &reqs)
	defer srv.Close()

	if _, err := newClient(t, srv.URL, AuthConfig{}).ListDataStreams(context.Background()); err != nil {
		t.Fatalf("ListDataStreams: %v", err)
	}
	if reqs[0].method != http.MethodGet || reqs[0].path != "/_data_stream" {
		t.Errorf("got %s %s, want GET /_data_stream", reqs[0].method, reqs[0].path)
	}
}

func TestAuthApiKey(t *testing.T) {
	var reqs []captured
	srv := newStub(t, 200, `{}`, &reqs)
	defer srv.Close()

	c := newClient(t, srv.URL, AuthConfig{Type: AuthAPIKey, APIKey: "secret-key"})
	if _, err := c.ListAliases(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := reqs[0].header.Get("Authorization"); got != "ApiKey secret-key" {
		t.Errorf("Authorization = %q, want %q", got, "ApiKey secret-key")
	}
}

func TestAuthBasic(t *testing.T) {
	var reqs []captured
	srv := newStub(t, 200, `{}`, &reqs)
	defer srv.Close()

	c := newClient(t, srv.URL, AuthConfig{Type: AuthBasic, Username: "elastic", Password: "pw"})
	if _, err := c.ListAliases(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("elastic:pw"))
	if got := reqs[0].header.Get("Authorization"); got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
}

func TestTLSInsecureSkipVerify(t *testing.T) {
	var reqs []captured
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs = append(reqs, captured{r.Method, r.URL.Path, r.URL.RawQuery, nil})
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c, err := New(Config{Server: srv.URL, TLS: TLSConfig{InsecureSkipVerify: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListAliases(context.Background()); err != nil {
		t.Fatalf("insecure skip verify connection failed: %v", err)
	}
}

func TestTLSCustomCA(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	// Write the server's CA (its own cert) to a file and trust it.
	caFile := t.TempDir() + "/ca.pem"
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	pemBytes := certToPEM(t, srv.Certificate())
	if err := os.WriteFile(caFile, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := New(Config{Server: srv.URL, TLS: TLSConfig{CACert: caFile}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListAliases(context.Background()); err != nil {
		t.Fatalf("custom CA connection failed: %v", err)
	}
}

func TestPing(t *testing.T) {
	var reqs []captured
	srv := newStub(t, 200, `{"status":"green"}`, &reqs)
	defer srv.Close()
	if err := newClient(t, srv.URL, AuthConfig{}).Ping(context.Background()); err != nil {
		t.Fatalf("Ping healthy: %v", err)
	}
	if reqs[0].method != http.MethodGet || reqs[0].path != "/_cluster/health" {
		t.Errorf("got %s %s, want GET /_cluster/health", reqs[0].method, reqs[0].path)
	}

	var reqs2 []captured
	bad := newStub(t, 401, `{"error":"unauthorized"}`, &reqs2)
	defer bad.Close()
	if err := newClient(t, bad.URL, AuthConfig{}).Ping(context.Background()); err == nil {
		t.Error("Ping should fail on auth rejection")
	}
}

func TestGetMaxResultWindow(t *testing.T) {
	// Single index: flat-keyed value parsed.
	var reqs []captured
	body := `{"idx-1":{"settings":{"index.max_result_window":"5000"}}}`
	srv := newStub(t, 200, body, &reqs)
	defer srv.Close()

	w, found, err := newClient(t, srv.URL, AuthConfig{}).GetMaxResultWindow(context.Background(), "app-logs")
	if err != nil {
		t.Fatal(err)
	}
	if !found || w != 5000 {
		t.Errorf("got (%d, %v), want (5000, true)", w, found)
	}
	if !strings.Contains(reqs[0].query, "flat_settings=true") {
		t.Errorf("query %q missing flat_settings=true", reqs[0].query)
	}
	if reqs[0].path != "/app-logs/_settings/index.max_result_window" {
		t.Errorf("path = %q", reqs[0].path)
	}

	// Multiple indices: minimum is returned.
	var reqs2 []captured
	multi := `{"idx-1":{"settings":{"index.max_result_window":"10000"}},"idx-2":{"settings":{"index.max_result_window":"5000"}}}`
	srv2 := newStub(t, 200, multi, &reqs2)
	defer srv2.Close()
	w2, found2, err := newClient(t, srv2.URL, AuthConfig{}).GetMaxResultWindow(context.Background(), "app-logs")
	if err != nil {
		t.Fatal(err)
	}
	if !found2 || w2 != 5000 {
		t.Errorf("multi-index got (%d, %v), want (5000, true)", w2, found2)
	}
}

func TestSearchPostsToTarget(t *testing.T) {
	var reqs []captured
	srv := newStub(t, 200, `{"hits":{"total":{"value":0},"hits":[]}}`, &reqs)
	defer srv.Close()

	_, err := newClient(t, srv.URL, AuthConfig{}).Search(context.Background(), "app-logs", []byte(`{"size":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if reqs[0].method != http.MethodPost || reqs[0].path != "/app-logs/_search" {
		t.Errorf("got %s %s, want POST /app-logs/_search", reqs[0].method, reqs[0].path)
	}
}

// TestSearchParsesAggregations asserts the response's aggregations block is
// exposed as raw JSON and that _shards.failed/failures are parsed for callers to
// detect partial shard failures.
func TestSearchParsesAggregations(t *testing.T) {
	var reqs []captured
	body := `{"_shards":{"total":5,"successful":4,"failed":1,"failures":[{"shard":0,"index":"i","reason":{"type":"illegal_argument_exception","reason":"Fielddata is disabled on [ip]"}}]},` +
		`"hits":{"total":{"value":12},"hits":[]},` +
		`"aggregations":{"group":{"buckets":[{"key":"timeout","doc_count":42}]}}}`
	srv := newStub(t, 200, body, &reqs)
	defer srv.Close()

	resp, err := newClient(t, srv.URL, AuthConfig{}).Search(context.Background(), "app-logs", []byte(`{"size":0,"aggs":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(resp.Aggregations, &got); err != nil {
		t.Fatalf("aggregations not exposed as raw JSON: %v (%s)", err, resp.Aggregations)
	}
	if _, ok := got["group"]; !ok {
		t.Errorf("aggregations missing group block: %s", resp.Aggregations)
	}
	if resp.Shards.Failed != 1 {
		t.Errorf("_shards.failed = %d, want 1", resp.Shards.Failed)
	}
	if len(resp.Shards.Failures) != 1 {
		t.Errorf("_shards.failures len = %d, want 1", len(resp.Shards.Failures))
	}
}

func TestSearch404IsAPIError(t *testing.T) {
	var reqs []captured
	srv := newStub(t, 404, `{"error":{"type":"index_not_found_exception"}}`, &reqs)
	defer srv.Close()

	_, err := newClient(t, srv.URL, AuthConfig{}).Search(context.Background(), "missing", []byte(`{}`))
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("want *APIError, got %T", err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", apiErr.StatusCode)
	}
}

// TestReadOnlyWhitelistLock enumerates the client's exported methods and asserts
// the set is exactly the read-only whitelist with no generic request method.
func TestReadOnlyWhitelistLock(t *testing.T) {
	want := []string{"GetMaxResultWindow", "ListAliases", "ListDataStreams", "Ping", "Search"}

	var got []string
	ct := reflect.TypeFor[*Client]()
	for i := 0; i < ct.NumMethod(); i++ {
		got = append(got, ct.Method(i).Name)
	}
	sort.Strings(got)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("exported method set = %v, want %v", got, want)
	}
	for _, forbidden := range []string{"Do", "Request", "Raw", "Call", "Execute"} {
		if _, ok := ct.MethodByName(forbidden); ok {
			t.Errorf("forbidden generic method %q is exported", forbidden)
		}
	}
}

func TestTLSConfigForServer(t *testing.T) {
	// Sanity: a built client honors insecure flag in its transport.
	c, err := New(Config{Server: "https://example:9200", TLS: TLSConfig{InsecureSkipVerify: true}})
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := c.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type %T", c.httpClient.Transport)
	}
	if tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify not propagated to transport")
	}
}

// TestUnsupportedAuthType asserts New rejects an unknown auth.type and modes
// missing their required secrets, while accepting valid configurations.
func TestUnsupportedAuthType(t *testing.T) {
	bad := []AuthConfig{
		{Type: "ApiKey"},                 // unknown type (wrong case)
		{Type: AuthAPIKey},               // apikey missing the key
		{Type: AuthBasic, Username: "u"}, // basic missing the password
		{Type: AuthBasic, Password: "p"}, // basic missing the username
	}
	for _, a := range bad {
		if _, err := New(Config{Server: "http://es:9200", Auth: a}); err == nil {
			t.Errorf("New should reject auth %+v", a)
		}
	}
	good := []AuthConfig{
		{},                              // no auth
		{Type: AuthAPIKey, APIKey: "k"}, // apikey with key
		{Type: AuthBasic, Username: "u", Password: "p"}, // basic with creds
	}
	for _, a := range good {
		if _, err := New(Config{Server: "http://es:9200", Auth: a}); err != nil {
			t.Errorf("auth %+v should be accepted: %v", a, err)
		}
	}
}

// TestSearchEscapesTarget asserts a target containing URL-significant characters
// is percent-escaped so it cannot break the request path.
func TestSearchEscapesTarget(t *testing.T) {
	var reqs []captured
	srv := newStub(t, 200, `{"hits":{"total":{"value":0},"hits":[]}}`, &reqs)
	defer srv.Close()

	_, err := newClient(t, srv.URL, AuthConfig{}).Search(context.Background(), "logs ?x", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	// The '?' must not become a query separator, and the server must decode the
	// escaped path back to the intact target segment.
	if reqs[0].query != "" {
		t.Errorf("'?' leaked into query string: %q", reqs[0].query)
	}
	if reqs[0].path != "/logs ?x/_search" {
		t.Errorf("decoded path = %q, want /logs ?x/_search", reqs[0].path)
	}
}

// TestSearchPreservesWildcardTarget asserts '*' wildcards and ',' multi-target
// lists reach Elasticsearch literally rather than being percent-escaped.
func TestSearchPreservesWildcardTarget(t *testing.T) {
	for _, tgt := range []string{"logs-*", "app,web"} {
		var reqs []captured
		srv := newStub(t, 200, `{"hits":{"total":{"value":0},"hits":[]}}`, &reqs)
		if _, err := newClient(t, srv.URL, AuthConfig{}).Search(context.Background(), tgt, []byte(`{}`)); err != nil {
			srv.Close()
			t.Fatal(err)
		}
		if reqs[0].path != "/"+tgt+"/_search" {
			t.Errorf("target %q: path = %q, want /%s/_search", tgt, reqs[0].path, tgt)
		}
		srv.Close()
	}
}
