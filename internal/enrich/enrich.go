// Package enrich resolves aircraft metadata (registration, type, operator)
// from an ICAO hex code. Neither the SBS feed nor the tar1090 JSON feed carries
// this metadata, so it is looked up on demand and cached.
package enrich

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dgraph-io/ristretto/v2"
)

// Metadata is the resolved identity of an aircraft. Any field may be empty if
// the upstream database does not have it.
type Metadata struct {
	Registration string
	Type         string // human-readable, e.g. "Boeing 737-800"
	ICAOType     string // ICAO type code, e.g. "B738"
	Manufacturer string
	Operator     string
}

// Empty reports whether no useful metadata was resolved.
func (m Metadata) Empty() bool {
	return m.Registration == "" && m.Type == "" && m.ICAOType == "" &&
		m.Manufacturer == "" && m.Operator == ""
}

// Successful lookups rarely change (a hex maps to one airframe), so cache them
// for a long time. Misses are cached briefly so we re-check aircraft that may
// simply be missing from the DB today without hammering the API on every alert.
const (
	successTTL = 7 * 24 * time.Hour
	missTTL    = time.Hour
)

// Client looks up metadata against a hexdb.io-compatible API and caches results.
type Client struct {
	baseURL string
	http    *http.Client
	cache   *ristretto.Cache[string, Metadata]
}

// New returns a Client querying baseURL (e.g. "https://hexdb.io").
func New(baseURL string) (*Client, error) {
	cache, err := ristretto.NewCache(&ristretto.Config[string, Metadata]{
		NumCounters: 1 << 14,
		MaxCost:     1 << 14,
		BufferItems: 64,
	})
	if err != nil {
		return nil, fmt.Errorf("ristretto: %w", err)
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 5 * time.Second},
		cache:   cache,
	}, nil
}

// hexdbResponse mirrors the hexdb.io /api/v1/aircraft/<hex> payload.
type hexdbResponse struct {
	ICAOTypeCode     string `json:"ICAOTypeCode"`
	Manufacturer     string `json:"Manufacturer"`
	ModeS            string `json:"ModeS"`
	RegisteredOwners string `json:"RegisteredOwners"`
	Registration     string `json:"Registration"`
	Type             string `json:"Type"`
}

// Lookup resolves metadata for an ICAO hex. It always returns a usable Metadata
// (possibly empty) plus any error encountered; callers may use the metadata
// even when err is non-nil.
func (c *Client) Lookup(ctx context.Context, hex string) (Metadata, error) {
	hex = strings.ToLower(strings.TrimSpace(hex))
	if hex == "" {
		return Metadata{}, nil
	}
	if md, ok := c.cache.Get(hex); ok {
		return md, nil
	}

	md, err := c.fetch(ctx, hex)
	if err != nil {
		// Negative-cache so a missing/erroring hex doesn't re-hit on every
		// position update within the alert cooldown window.
		c.cache.SetWithTTL(hex, Metadata{}, 1, missTTL)
		return Metadata{}, err
	}

	ttl := successTTL
	if md.Empty() {
		ttl = missTTL
	}
	c.cache.SetWithTTL(hex, md, 1, ttl)
	return md, nil
}

func (c *Client) fetch(ctx context.Context, hex string) (Metadata, error) {
	url := fmt.Sprintf("%s/api/v1/aircraft/%s", c.baseURL, hex)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Metadata{}, fmt.Errorf("build request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Metadata{}, fmt.Errorf("fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// hexdb returns 404 for unknown aircraft — treat as a clean miss, not an error.
	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, resp.Body)
		return Metadata{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return Metadata{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var r hexdbResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&r); err != nil {
		return Metadata{}, fmt.Errorf("decode: %w", err)
	}
	return Metadata{
		Registration: strings.TrimSpace(r.Registration),
		Type:         strings.TrimSpace(r.Type),
		ICAOType:     strings.TrimSpace(r.ICAOTypeCode),
		Manufacturer: strings.TrimSpace(r.Manufacturer),
		Operator:     strings.TrimSpace(r.RegisteredOwners),
	}, nil
}
