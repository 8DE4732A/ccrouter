// Package modelsdev provides an in-memory, TTL-cached view of the public
// models.dev catalog (https://models.dev/api.json), used by the admin UI to
// suggest context limits and pricing for combo members.
//
// The client never sits on the request hot path: cost is computed from the
// prices snapshotted into the ccrouter config, so a models.dev outage only
// degrades the admin suggestions.
package modelsdev

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"ccrouter/internal/config"
)

const (
	// DefaultBaseURL is the public models.dev origin.
	DefaultBaseURL = "https://models.dev"
	// DefaultTimeout bounds a single catalog fetch.
	DefaultTimeout = 10 * time.Second
	// DefaultTTL is how long a fetched snapshot is considered fresh. The
	// upstream API advertises Cache-Control: max-age=3600.
	DefaultTTL = time.Hour

	// maxResponseBytes caps a catalog download so a broken or hostile
	// upstream cannot exhaust memory.
	maxResponseBytes = 64 << 20

	defaultLimit = 20
	maxLimit     = 100

	// refreshBackoff prevents hammering a failing catalog endpoint: after a
	// failed or in-flight attempt, background refreshes wait this long.
	refreshBackoff = 30 * time.Second
)

// Options configures a Client.
type Options struct {
	BaseURL    string
	Timeout    time.Duration
	TTL        time.Duration
	Disabled   bool
	HTTPClient *http.Client
	// Now is injectable for tests; nil means time.Now.
	Now func() time.Time
}

func (o Options) withDefaults() Options {
	if o.BaseURL == "" {
		o.BaseURL = DefaultBaseURL
	}
	o.BaseURL = strings.TrimRight(o.BaseURL, "/")
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.TTL <= 0 {
		o.TTL = DefaultTTL
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: o.Timeout}
	}
	return o
}

// Entry is one provider-scoped model offering.
type Entry struct {
	SourceID         string                  `json:"source_id"` // "<provider id>/<model id>"
	ProviderID       string                  `json:"provider_id"`
	ProviderName     string                  `json:"provider_name"`
	ModelID          string                  `json:"model_id"`
	Name             string                  `json:"name"`
	CanonicalModelID string                  `json:"canonical_model_id,omitempty"`
	ContextWindow    *int                    `json:"context_window,omitempty"`
	MaxOutputTokens  *int                    `json:"max_output_tokens,omitempty"`
	Cost             *config.ComboMemberCost `json:"cost,omitempty"`
	MatchScore       float64                 `json:"match_score,omitempty"`
}

// Status describes the catalog snapshot currently held by the client.
type Status struct {
	Ready         bool   `json:"ready"`
	Stale         bool   `json:"stale"`
	Disabled      bool   `json:"disabled"`
	FetchedAt     int64  `json:"fetched_at"`
	LastError     string `json:"last_error,omitempty"`
	ProviderCount int    `json:"provider_count"`
	ModelCount    int    `json:"model_count"`
	BaseURL       string `json:"base_url"`
}

type snapshot struct {
	entries       []Entry
	bySource      map[string]int
	providerCount int
	searchBlob    []string // lowercased searchable text, parallel to entries
	normalized    []string // normalized model ID, parallel to entries
}

// Client holds a cached catalog snapshot. All methods are safe for concurrent
// use; lookups never touch the network.
type Client struct {
	mu        sync.RWMutex
	opts      Options
	snap      *snapshot
	fetchedAt time.Time
	lastErr   error
	// lastAttempt bounds retry frequency while the upstream is failing.
	lastAttempt time.Time
	closed      bool
	// generation increments whenever the configuration changes. A fetch that
	// started under an older generation must not publish its result.
	generation uint64

	baseCtx context.Context
	cancel  context.CancelFunc

	refreshMu sync.Mutex
}

// New creates a client. Call Start to fetch the catalog in the background.
func New(opts Options) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{opts: opts.withDefaults(), baseCtx: ctx, cancel: cancel}
}

// Start triggers one non-blocking background fetch when enabled.
func (c *Client) Start() {
	c.mu.RLock()
	disabled, closed := c.opts.Disabled, c.closed
	c.mu.RUnlock()
	if disabled || closed {
		return
	}
	go func() { _ = c.refresh(c.baseCtx) }()
}

// Close cancels in-flight fetches and marks the client closed.
func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	c.cancel()
}

// Reconfigure applies new options, typically after a config hot reload. A
// changed base URL discards the old snapshot so stale catalog data can never
// be surfaced (or snapshotted into config) for the new endpoint.
func (c *Client) Reconfigure(opts Options) {
	opts = opts.withDefaults()
	c.mu.Lock()
	defer c.mu.Unlock()
	changedURL := opts.BaseURL != c.opts.BaseURL
	// Only invalidate in-flight work when the effective configuration really
	// changed. A hot reload rebuilds the HTTP client every time, so comparing
	// client pointers would discard an in-progress multi-MB download on every
	// unrelated config save.
	changed := changedURL || opts.Disabled != c.opts.Disabled ||
		opts.Timeout != c.opts.Timeout || opts.TTL != c.opts.TTL
	oldClient := c.opts.HTTPClient
	c.opts = opts
	if changed {
		c.generation++
	}
	if oldClient != nil && oldClient != opts.HTTPClient {
		oldClient.CloseIdleConnections()
	}
	if changedURL {
		c.snap = nil
		c.fetchedAt = time.Time{}
		c.lastErr = nil
	}
}

// Refresh synchronously fetches a new snapshot, returning the resulting
// status. If another refresh is in flight it waits for it first.
func (c *Client) Refresh(ctx context.Context) (Status, error) {
	c.refreshMu.Lock()
	err := c.fetchAndSwap(ctx)
	c.refreshMu.Unlock()
	return c.Status(), err
}

func (c *Client) refresh(ctx context.Context) error {
	// TryLock makes TTL refreshes non-blocking: if a fetch is already
	// running, there is nothing to do.
	if !c.refreshMu.TryLock() {
		return nil
	}
	defer c.refreshMu.Unlock()
	return c.fetchAndSwap(ctx)
}

func (c *Client) fetchAndSwap(ctx context.Context) error {
	c.mu.RLock()
	opts, closed, gen, baseCtx := c.opts, c.closed, c.generation, c.baseCtx
	c.mu.RUnlock()
	if closed || opts.Disabled {
		return nil
	}

	c.mu.Lock()
	c.lastAttempt = opts.Now()
	c.mu.Unlock()

	// Close (via baseCtx) cancels an in-flight fetch; context.AfterFunc avoids
	// a watcher goroutine per request.
	reqCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(baseCtx, cancel)
	snap, err := fetchSnapshot(reqCtx, opts)
	stop()
	cancel()

	now := opts.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.generation != gen {
		// The endpoint changed or the client closed while fetching: this
		// result belongs to a stale configuration and must not be published.
		return nil
	}
	if err != nil {
		c.lastErr = err
		return err
	}
	c.snap = snap
	c.fetchedAt = now
	c.lastErr = nil
	return nil
}

// Status returns the current snapshot state without fetching.
func (c *Client) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	st := Status{
		Ready:    c.snap != nil,
		Disabled: c.opts.Disabled,
		BaseURL:  c.opts.BaseURL,
	}
	if c.snap != nil {
		st.ProviderCount = c.snap.providerCount
		st.ModelCount = len(c.snap.entries)
		st.Stale = c.staleLocked()
	}
	if !c.fetchedAt.IsZero() {
		st.FetchedAt = c.fetchedAt.Unix()
	}
	if c.lastErr != nil {
		st.LastError = c.lastErr.Error()
	}
	return st
}

// Search finds entries matching every whitespace-separated term in q. An
// empty query returns the first limit entries in catalog order. providerHint
// only boosts matching providers; it never filters, because ccrouter provider
// names rarely equal models.dev provider IDs.
func (c *Client) Search(q, providerHint string, limit int) []Entry {
	snap := c.usableSnapshot()
	if snap == nil {
		return []Entry{}
	}
	limit = clampLimit(limit)
	terms := strings.Fields(strings.ToLower(q))
	if len(terms) == 0 {
		out := make([]Entry, 0, limit)
		for _, e := range snap.entries {
			if len(out) >= limit {
				break
			}
			out = append(out, e)
		}
		return out
	}

	matches := make([]Entry, 0, 16)
	for i, e := range snap.entries {
		haystack := snap.searchBlob[i]
		ok := true
		for _, term := range terms {
			if !strings.Contains(haystack, term) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		e.MatchScore = searchScore(e, terms, providerHint)
		matches = append(matches, e)
	}
	return rankAndLimit(matches, limit)
}

// Suggest ranks catalog entries for a ccrouter member's model ID. providerHint
// is the ccrouter provider name; it only boosts entries, never filters them,
// because provider names rarely match models.dev provider IDs.
func (c *Client) Suggest(modelID, providerHint string, limit int) []Entry {
	snap := c.usableSnapshot()
	if snap == nil || strings.TrimSpace(modelID) == "" {
		return []Entry{}
	}
	limit = clampLimit(limit)
	norm := normalizeModelID(modelID)
	normHint := strings.ToLower(strings.TrimSpace(providerHint))

	entries := make([]Entry, 0, 8)
	for i, e := range snap.entries {
		if e.ContextWindow == nil && e.MaxOutputTokens == nil && e.Cost.Empty() {
			continue
		}
		score := suggestScore(e, modelID, norm, normHint, snap.normalized[i])
		if score <= 0 {
			continue
		}
		e.MatchScore = score
		entries = append(entries, e)
	}
	return rankAndLimit(entries, limit)
}

// Lookup returns the entry with the exact source ID ("provider/model").
func (c *Client) Lookup(sourceID string) (Entry, bool) {
	snap := c.usableSnapshot()
	if snap == nil {
		return Entry{}, false
	}
	idx, ok := snap.bySource[sourceID]
	if !ok {
		return Entry{}, false
	}
	return snap.entries[idx], true
}

// staleLocked reports whether the held snapshot should be refreshed. The
// caller must hold c.mu.
func (c *Client) staleLocked() bool {
	return c.snap == nil || c.fetchedAt.IsZero() || c.opts.Now().Sub(c.fetchedAt) > c.opts.TTL
}

func (c *Client) usableSnapshot() *snapshot {
	c.mu.RLock()
	if c.closed || c.opts.Disabled {
		c.mu.RUnlock()
		return nil
	}
	stale := c.staleLocked()
	// Bound retry frequency while the upstream is failing: without this every
	// admin search would start another timing-out download.
	backoff := !c.lastAttempt.IsZero() && c.opts.Now().Sub(c.lastAttempt) < refreshBackoff
	snap := c.snap
	c.mu.RUnlock()

	if stale && !backoff {
		// Refresh in the background; the caller gets the previous snapshot (if
		// any) immediately and retries on its next poll.
		go func() { _ = c.refresh(c.baseCtx) }()
	}
	return snap
}

// rankAndLimit sorts scored entries and truncates to limit.
func rankAndLimit(entries []Entry, limit int) []Entry {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].MatchScore != entries[j].MatchScore {
			return entries[i].MatchScore > entries[j].MatchScore
		}
		return lessEntry(entries[i], entries[j])
	})
	if len(entries) > limit {
		return entries[:limit]
	}
	return entries
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

func lessEntry(a, b Entry) bool {
	if a.ProviderName != b.ProviderName {
		return a.ProviderName < b.ProviderName
	}
	return a.ModelID < b.ModelID
}

// ---------------------------------------------------------------------------
// Catalog fetching
// ---------------------------------------------------------------------------

type rawProvider struct {
	ID     string              `json:"id"`
	Name   string              `json:"name"`
	Models map[string]rawModel `json:"models"`
}

type rawModel struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	CanonicalModelID string `json:"canonical_model_id"`
	Limit            struct {
		Context *int `json:"context"`
		Output  *int `json:"output"`
	} `json:"limit"`
	Cost *config.ComboMemberCost `json:"cost"`
}

func fetchSnapshot(ctx context.Context, opts Options) (*snapshot, error) {
	// withDefaults guarantees a positive timeout.
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opts.BaseURL+"/api.json", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ccrouter/modelsdev")

	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("models.dev: unexpected status %d", resp.StatusCode)
	}

	var providers map[string]rawProvider
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes))
	if err := dec.Decode(&providers); err != nil {
		return nil, fmt.Errorf("models.dev: decode catalog: %w", err)
	}

	entries := make([]Entry, 0, 4096)
	for mapID, p := range providers {
		providerID := p.ID
		if providerID == "" {
			providerID = mapID
		}
		providerName := p.Name
		if providerName == "" {
			providerName = providerID
		}
		for mapModelID, m := range p.Models {
			modelID := m.ID
			if modelID == "" {
				modelID = mapModelID
			}
			e := Entry{
				SourceID:         providerID + "/" + modelID,
				ProviderID:       providerID,
				ProviderName:     providerName,
				ModelID:          modelID,
				Name:             m.Name,
				CanonicalModelID: m.CanonicalModelID,
				ContextWindow:    m.Limit.Context,
				MaxOutputTokens:  m.Limit.Output,
			}
			if !m.Cost.Empty() {
				e.Cost = m.Cost
			}
			entries = append(entries, e)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return lessEntry(entries[i], entries[j]) })

	snap := &snapshot{
		entries:    entries,
		bySource:   make(map[string]int, len(entries)),
		searchBlob: make([]string, len(entries)),
		normalized: make([]string, len(entries)),
	}
	providerSeen := make(map[string]struct{}, len(providers))
	for i := range entries {
		e := &entries[i]
		snap.bySource[e.SourceID] = i
		snap.searchBlob[i] = strings.ToLower(e.ProviderID + " " + e.ProviderName + " " + e.ModelID + " " + e.Name + " " + e.CanonicalModelID)
		snap.normalized[i] = normalizeModelID(e.ModelID)
		providerSeen[e.ProviderID] = struct{}{}
	}
	snap.providerCount = len(providerSeen)
	return snap, nil
}
