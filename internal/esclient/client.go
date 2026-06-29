// Package esclient is a read-only Elasticsearch client. It exposes only a fixed
// whitelist of read endpoints, each bound to a constant HTTP verb and path
// template. There is deliberately no generic request method that accepts a
// caller-supplied verb or path, so destructive operations are impossible to
// issue through this package by construction.
package esclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Config configures a read-only client.
type Config struct {
	Server string // base URL, e.g. https://es:9200
	Auth   AuthConfig
	TLS    TLSConfig
}

// Client issues only whitelisted read requests against one cluster.
type Client struct {
	base       string
	auth       AuthConfig
	httpClient *http.Client
}

// New builds a Client, configuring TLS from the provided config.
func New(cfg Config) (*Client, error) {
	if err := cfg.Auth.validate(); err != nil {
		return nil, err
	}
	tlsCfg, err := buildTLSConfig(cfg.TLS)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsCfg
	return &Client{
		base:       strings.TrimRight(cfg.Server, "/"),
		auth:       cfg.Auth,
		httpClient: &http.Client{Transport: transport},
	}, nil
}

// APIError represents a non-2xx Elasticsearch response.
type APIError struct {
	StatusCode int
	ErrType    string
	Body       string
}

func (e *APIError) Error() string {
	if e.ErrType != "" {
		return fmt.Sprintf("elasticsearch responded %d (%s)", e.StatusCode, e.ErrType)
	}
	return fmt.Sprintf("elasticsearch responded %d", e.StatusCode)
}

// Summary returns a short human-readable description of the failure.
func (e *APIError) Summary() string {
	if e.ErrType != "" {
		return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.ErrType)
	}
	return fmt.Sprintf("HTTP %d", e.StatusCode)
}

// do issues a single read request. It is unexported and the only callers are the
// whitelisted methods below, each passing a constant verb and path template.
func (c *Client) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.auth.apply(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			ErrType:    parseErrorType(data),
			Body:       string(data),
		}
	}
	return data, nil
}

// parseErrorType extracts error.type from an Elasticsearch error response body.
func parseErrorType(data []byte) string {
	var env struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &env); err == nil {
		return env.Error.Type
	}
	return ""
}

// Ping issues GET /_cluster/health, reporting an error on connection failure or
// rejected authentication.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodGet, "/_cluster/health", nil)
	return err
}

// AliasEntry is one alias returned by ListAliases.
type AliasEntry struct {
	Name       string
	IndexCount int
}

// ListAliases issues GET /_alias and returns each alias with the number of
// indices it points to.
func (c *Client) ListAliases(ctx context.Context) ([]AliasEntry, error) {
	data, err := c.do(ctx, http.MethodGet, "/_alias", nil)
	if err != nil {
		return nil, err
	}
	// Shape: {"<index>":{"aliases":{"<alias>":{...}}}}
	var raw map[string]struct {
		Aliases map[string]json.RawMessage `json:"aliases"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse aliases: %w", err)
	}
	counts := make(map[string]int)
	for _, idx := range raw {
		for alias := range idx.Aliases {
			counts[alias]++
		}
	}
	out := make([]AliasEntry, 0, len(counts))
	for name, n := range counts {
		out = append(out, AliasEntry{Name: name, IndexCount: n})
	}
	return out, nil
}

// DataStreamEntry is one datastream returned by ListDataStreams.
type DataStreamEntry struct {
	Name                string
	BackingIndicesCount int
}

// ListDataStreams issues GET /_data_stream and returns each datastream with the
// number of its backing indices.
func (c *Client) ListDataStreams(ctx context.Context) ([]DataStreamEntry, error) {
	data, err := c.do(ctx, http.MethodGet, "/_data_stream", nil)
	if err != nil {
		return nil, err
	}
	var raw struct {
		DataStreams []struct {
			Name    string            `json:"name"`
			Indices []json.RawMessage `json:"indices"`
		} `json:"data_streams"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse data streams: %w", err)
	}
	out := make([]DataStreamEntry, 0, len(raw.DataStreams))
	for _, ds := range raw.DataStreams {
		out = append(out, DataStreamEntry{Name: ds.Name, BackingIndicesCount: len(ds.Indices)})
	}
	return out, nil
}

// SearchResponse is the parsed _search response.
type SearchResponse struct {
	// Shards carries the _shards summary; Failed and Failures let callers detect
	// and surface partial shard failures rather than silently treating a 200 with
	// failed shards as a complete result.
	Shards struct {
		Failed   int               `json:"failed"`
		Failures []json.RawMessage `json:"failures"`
	} `json:"_shards"`
	Hits struct {
		Total struct {
			Value int `json:"value"`
		} `json:"total"`
		Hits []Hit `json:"hits"`
	} `json:"hits"`
	// Aggregations is the raw aggregations block, exposed verbatim so the command
	// layer owns all aggregation rendering. It is nil when the response carries no
	// aggregations.
	Aggregations json.RawMessage `json:"aggregations"`
}

// Hit is a single search hit including its envelope and source.
type Hit struct {
	ID     string            `json:"_id"`
	Index  string            `json:"_index"`
	Score  *float64          `json:"_score"`
	Source json.RawMessage   `json:"_source"`
	Sort   []json.RawMessage `json:"sort"`
}

// Search issues POST /<target>/_search with the given request body. Pagination
// (search_after) is the caller's responsibility via the request body; this stays
// on the single _search endpoint.
func (c *Client) Search(ctx context.Context, target string, body []byte) (*SearchResponse, error) {
	data, err := c.do(ctx, http.MethodPost, "/"+escapeTarget(target)+"/_search", body)
	if err != nil {
		return nil, err
	}
	var resp SearchResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse search response: %w", err)
	}
	return &resp, nil
}

// GetMaxResultWindow issues GET /<target>/_settings/index.max_result_window with
// include_defaults and flat_settings, returning the minimum window across all
// matched indices. The found flag is false when no explicit value is present so
// the caller can fall back to the default of 10000.
func (c *Client) GetMaxResultWindow(ctx context.Context, target string) (window int, found bool, err error) {
	path := "/" + escapeTarget(target) + "/_settings/index.max_result_window?include_defaults=true&flat_settings=true"
	data, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return 0, false, err
	}
	// Shape: {"<index>":{"settings":{"index.max_result_window":"5000"},
	//                    "defaults":{"index.max_result_window":"10000"}}}
	var raw map[string]struct {
		Settings map[string]string `json:"settings"`
		Defaults map[string]string `json:"defaults"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return 0, false, fmt.Errorf("parse max_result_window: %w", err)
	}
	const key = "index.max_result_window"
	minWindow := -1
	for _, idx := range raw {
		val, ok := idx.Settings[key]
		if !ok {
			val, ok = idx.Defaults[key]
		}
		if !ok || val == "" {
			continue
		}
		n, convErr := strconv.Atoi(val)
		if convErr != nil {
			continue
		}
		if minWindow == -1 || n < minWindow {
			minWindow = n
		}
	}
	if minWindow == -1 {
		return 0, false, nil
	}
	return minWindow, true, nil
}

// escapeTarget percent-encodes only characters that would break URL structure
// (whitespace, '?', '#', '/', etc.) while leaving Elasticsearch-meaningful
// target characters — '*' wildcards, ',' multi-target lists, ':' remote-cluster
// prefixes — literal so the cluster receives the intended index expression.
func escapeTarget(target string) string {
	var b strings.Builder
	for i := 0; i < len(target); i++ {
		c := target[i]
		if isTargetSafe(c) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// isTargetSafe reports whether a byte may appear unescaped in a target path
// segment: alphanumerics plus the punctuation Elasticsearch index expressions use.
func isTargetSafe(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '-', '_', '.', '*', ',', ':', '+', '(', ')':
		return true
	}
	return false
}
