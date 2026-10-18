package modelsdev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const testCatalog = `{
  "anthropic": {
    "id": "anthropic",
    "name": "Anthropic",
    "models": {
      "claude-opus-4-6": {
        "id": "claude-opus-4-6",
        "name": "Claude Opus 4.6",
        "limit": {"context": 1000000, "output": 128000},
        "cost": {"input": 5, "output": 25, "cache_read": 0.5, "cache_write": 6.25}
      }
    }
  },
  "openrouter": {
    "id": "openrouter",
    "name": "OpenRouter",
    "models": {
      "anthropic/claude-opus-4.6": {
        "id": "anthropic/claude-opus-4.6",
        "name": "Claude Opus 4.6 (OpenRouter)",
        "canonical_model_id": "anthropic/claude-opus-4-6",
        "limit": {"context": 200000, "output": 32000},
        "cost": {"input": 6, "output": 30}
      }
    }
  },
  "deepseek": {
    "id": "deepseek",
    "name": "DeepSeek",
    "models": {
      "deepseek-chat": {
        "id": "deepseek-chat",
        "name": "DeepSeek Chat",
        "limit": {"context": 131072, "output": 8192},
        "cost": {"input": 0.27, "output": 1.1, "cache_read": 0.07}
      },
      "deepseek-chat-2025-08-06": {
        "id": "deepseek-chat-2025-08-06",
        "name": "DeepSeek Chat (dated)",
        "limit": {"context": 65536, "output": 4096},
        "cost": {"input": 0.5, "output": 2}
      }
    }
  }
}`

func newTestClient(t *testing.T, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New(Options{BaseURL: srv.URL})
}

func TestRefreshParsesAndIndexes(t *testing.T) {
	c := newTestClient(t, testCatalog)
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := c.Status()
	if !st.Ready || st.Stale {
		t.Fatalf("unexpected status: %+v", st)
	}
	if st.ProviderCount != 3 || st.ModelCount != 4 {
		t.Fatalf("unexpected counts: %+v", st)
	}
	e, ok := c.Lookup("anthropic/claude-opus-4-6")
	if !ok {
		t.Fatal("lookup failed")
	}
	if e.ContextWindow == nil || *e.ContextWindow != 1000000 {
		t.Fatalf("context = %v", e.ContextWindow)
	}
	if e.Cost == nil || e.Cost.Input == nil || *e.Cost.Input != 5 {
		t.Fatalf("cost = %+v", e.Cost)
	}
	if e.Cost.CacheRead == nil || *e.Cost.CacheRead != 0.5 {
		t.Fatalf("cache read = %v", e.Cost.CacheRead)
	}
}

func TestSearchRanksExactFirst(t *testing.T) {
	c := newTestClient(t, testCatalog)
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	items := c.Search("claude opus", "", 10)
	if len(items) == 0 {
		t.Fatal("expected matches")
	}
	if items[0].ProviderID != "anthropic" {
		t.Fatalf("expected anthropic first, got %s/%s", items[0].ProviderID, items[0].ModelID)
	}
	for _, e := range items {
		if e.MatchScore < 0 || e.MatchScore > 1 {
			t.Fatalf("score out of range: %v", e.MatchScore)
		}
	}
	if got := c.Search("no-such-model", "", 10); len(got) != 0 {
		t.Fatalf("expected no matches, got %d", len(got))
	}
}

func TestSuggestExactAndDateSuffix(t *testing.T) {
	c := newTestClient(t, testCatalog)
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	items := c.Suggest("deepseek-chat", "deepseek", 5)
	if len(items) == 0 {
		t.Fatal("expected suggestions")
	}
	if items[0].SourceID != "deepseek/deepseek-chat" {
		t.Fatalf("expected exact match first, got %s", items[0].SourceID)
	}
	if items[0].MatchScore < 0.9 {
		t.Fatalf("exact match should auto-apply, score=%v", items[0].MatchScore)
	}

	// A dated ccrouter model ID should still rank the undated catalog entry.
	items = c.Suggest("deepseek-chat-2025-08-06", "deepseek", 5)
	top := items[0]
	if top.SourceID != "deepseek/deepseek-chat" && top.SourceID != "deepseek/deepseek-chat-2025-08-06" {
		t.Fatalf("unexpected top: %s", top.SourceID)
	}
	if top.Cost.Empty() {
		t.Fatalf("expected pricing, got %+v", top)
	}
}

func TestSuggestDuplicateModelIDsStayAmbiguous(t *testing.T) {
	const body = `{
      "a": {"id":"a","name":"Provider A","models":{"shared":{"id":"shared","name":"Shared",
        "limit":{"context":1000,"output":100},"cost":{"input":1,"output":2}}}},
      "b": {"id":"b","name":"Provider B","models":{"shared":{"id":"shared","name":"Shared",
        "limit":{"context":2000,"output":200},"cost":{"input":3,"output":4}}}}
    }`
	c := newTestClient(t, body)
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	items := c.Suggest("shared", "", 10)
	if len(items) != 2 {
		t.Fatalf("expected both providers, got %d", len(items))
	}
	if items[0].MatchScore != items[1].MatchScore {
		t.Fatalf("duplicates should tie: %v vs %v", items[0].MatchScore, items[1].MatchScore)
	}
	if items[0].MatchScore < 0.9 {
		t.Fatalf("exact match should score high: %v", items[0].MatchScore)
	}
}

func TestRefreshFailureKeepsPreviousSnapshot(t *testing.T) {
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(testCatalog))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL})
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	fail = true
	if _, err := c.Refresh(context.Background()); err == nil {
		t.Fatal("expected refresh error")
	}
	st := c.Status()
	if !st.Ready || st.LastError == "" {
		t.Fatalf("stale snapshot should survive: %+v", st)
	}
	if _, ok := c.Lookup("deepseek/deepseek-chat"); !ok {
		t.Fatal("previous snapshot lost")
	}
}

func TestStaleStatusWithInjectedClock(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	c := newTestClient(t, testCatalog)
	c.mu.Lock()
	c.opts.Now = func() time.Time { return now }
	c.opts.TTL = time.Minute
	c.mu.Unlock()

	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if st := c.Status(); !st.Stale {
		t.Fatalf("expected stale status: %+v", st)
	}
}

func TestDisabledClient(t *testing.T) {
	c := New(Options{Disabled: true, BaseURL: "http://127.0.0.1:1"})
	c.Start()
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("disabled refresh should be a no-op: %v", err)
	}
	if got := c.Search("anything", "", 10); len(got) != 0 {
		t.Fatalf("disabled search should be empty, got %d", len(got))
	}
	if st := c.Status(); !st.Disabled || st.Ready {
		t.Fatalf("unexpected status: %+v", st)
	}
}

func TestOversizedResponseIsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		chunk := make([]byte, 1<<20)
		for i := range chunk {
			chunk[i] = ' '
		}
		for i := 0; i < 65; i++ {
			_, _ = w.Write(chunk)
		}
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL})
	if _, err := c.Refresh(context.Background()); err == nil {
		t.Fatal("expected decode error for oversized body")
	}
}

func TestReconfigureDiscardsOldSnapshot(t *testing.T) {
	c := newTestClient(t, testCatalog)
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.Reconfigure(Options{BaseURL: "http://127.0.0.1:1"})
	st := c.Status()
	if st.Ready {
		t.Fatalf("old snapshot should be discarded after URL change: %+v", st)
	}
	if _, ok := c.Lookup("deepseek/deepseek-chat"); ok {
		t.Fatal("lookup must not serve the old endpoint's catalog")
	}
}

func TestReconfigureDropsInFlightFetch(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(testCatalog))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL})
	go func() { _ = c.refresh(context.Background()) }()
	<-started

	c.Reconfigure(Options{BaseURL: "http://127.0.0.1:1"})
	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.Status().Ready {
			t.Fatal("in-flight result from the previous endpoint must be dropped")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
