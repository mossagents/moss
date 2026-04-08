// Package registry provides a client for the Moss skill marketplace
// and a local cache for installed skills.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/mossagents/moss/skill"
)

// RegistryEntry describes a skill available in a remote registry.
type RegistryEntry struct {
	// Metadata mirrors skill.Metadata but is extended with registry-specific fields.
	skill.Metadata
	Author      string    `json:"author,omitempty"`
	License     string    `json:"license,omitempty"`
	Downloads   int       `json:"downloads,omitempty"`
	PublishedAt time.Time `json:"published_at,omitempty"`
	ArchiveURL  string    `json:"archive_url,omitempty"` // URL of the downloadable .zip archive
	Checksum    string    `json:"checksum,omitempty"`    // SHA-256 hex of the archive
}

// SearchOptions controls registry search queries.
type SearchOptions struct {
	Query  string // free-text search
	Limit  int    // max results (0 = server default)
	Offset int    // pagination offset
}

// RegistryClient fetches skill metadata from a remote marketplace.
type RegistryClient interface {
	// List returns all skills available in the registry.
	List(ctx context.Context) ([]RegistryEntry, error)
	// Search searches for skills matching opts.
	Search(ctx context.Context, opts SearchOptions) ([]RegistryEntry, error)
	// Get returns detail for a single skill by name and version ("" = latest).
	Get(ctx context.Context, name, version string) (*RegistryEntry, error)
}

// HTTPRegistryClient is a RegistryClient backed by a REST API.
type HTTPRegistryClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewHTTPRegistryClient creates a new registry client pointing at baseURL.
func NewHTTPRegistryClient(baseURL string) *HTTPRegistryClient {
	return &HTTPRegistryClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *HTTPRegistryClient) List(ctx context.Context) ([]RegistryEntry, error) {
	return c.fetchEntries(ctx, "/skills", nil)
}

func (c *HTTPRegistryClient) Search(ctx context.Context, opts SearchOptions) ([]RegistryEntry, error) {
	params := url.Values{}
	if opts.Query != "" {
		params.Set("q", opts.Query)
	}
	if opts.Limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}
	if opts.Offset > 0 {
		params.Set("offset", fmt.Sprintf("%d", opts.Offset))
	}
	return c.fetchEntries(ctx, "/skills/search", params)
}

func (c *HTTPRegistryClient) Get(ctx context.Context, name, version string) (*RegistryEntry, error) {
	path := "/skills/" + url.PathEscape(name)
	if version != "" {
		path += "@" + url.PathEscape(version)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("registry: skill %q@%q not found", name, version)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("registry: status %d", resp.StatusCode)
	}
	var entry RegistryEntry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return nil, fmt.Errorf("registry: decode: %w", err)
	}
	return &entry, nil
}

func (c *HTTPRegistryClient) fetchEntries(ctx context.Context, path string, params url.Values) ([]RegistryEntry, error) {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("registry: status %d", resp.StatusCode)
	}
	var entries []RegistryEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("registry: decode: %w", err)
	}
	return entries, nil
}

// compile-time check
var _ RegistryClient = (*HTTPRegistryClient)(nil)
