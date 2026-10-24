package proxy

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zlib"
	"github.com/klauspost/compress/zstd"

	"ccrouter/internal/combos"
	"ccrouter/internal/config"
	"ccrouter/internal/db"
	"ccrouter/internal/keys"
)

func newRelayService(t *testing.T, rec *db.Recorder, providers ...config.ProviderConfig) *Service {
	t.Helper()
	members := make([]config.ComboMember, 0, len(providers))
	kms := map[string]*keys.Manager{}
	for _, p := range providers {
		members = append(members, config.ComboMember{Provider: p.Name, Model: "m"})
		kms[p.Name] = keys.NewManager(p.Name, nil, p.KeyStrategy)
	}
	cfg := &config.AppConfig{
		Providers: providers,
		Combos: []config.ComboConfig{{
			Name: "my-combo", APIFormat: providers[0].APIs[0].APIFormat, Strategy: "fill-first", Members: members,
		}},
	}
	svc, err := New(cfg, kms, combos.NewRouter(cfg.Combos), map[string]*http.Client{}, rec, nil)
	if err != nil {
		t.Fatalf("build service: %v", err)
	}
	return svc
}

func relayProvider(name, apiFormat, baseURL, compression string) config.ProviderConfig {
	return config.ProviderConfig{
		Name:               name,
		Relay:              true,
		APIs:               []config.ApiEndpoint{{APIFormat: apiFormat, BaseURL: baseURL}},
		MaxRetries:         3,
		KeyStrategy:        "fill-first",
		RequestCompression: compression,
		HealthCheckRules:   []config.HealthCheckRule{},
	}
}

func chatRequest(t *testing.T, target string, body map[string]any) *http.Request {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", target, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestRelayForwardsClientCredentials(t *testing.T) {
	var got http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer upstream.Close()

	dbPath := filepath.Join(t.TempDir(), "relay.db")
	rec, err := db.NewRecorder(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	svc := newRelayService(t, rec, relayProvider("relay", "openai", upstream.URL, ""))

	req := chatRequest(t, "/v1/chat/completions", map[string]any{"model": "my-combo", "messages": []any{}})
	req.Header.Set("Authorization", "Bearer user-key")
	req.Header.Set("X-Api-Key", "user-key-2")
	req.Header.Set("X-Goog-Api-Key", "user-key-3")
	w := httptest.NewRecorder()
	svc.Handle(w, req, "openai", false)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if got.Get("Authorization") != "Bearer user-key" {
		t.Fatalf("Authorization = %q", got.Get("Authorization"))
	}
	if got.Get("X-Api-Key") != "user-key-2" {
		t.Fatalf("X-Api-Key = %q", got.Get("X-Api-Key"))
	}
	if got.Get("X-Goog-Api-Key") != "user-key-3" {
		t.Fatalf("X-Goog-Api-Key = %q", got.Get("X-Goog-Api-Key"))
	}

	// Relay rows must not persist the client's credential.
	res, err := db.QueryList(dbPath, 10, 0, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	items := res["items"].([]map[string]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 row, got %d", len(items))
	}
	if items[0]["key_prefix"] != nil {
		t.Fatalf("key_prefix = %v, want nil", items[0]["key_prefix"])
	}
}

func TestRelayGeminiUsesClientQueryKey(t *testing.T) {
	var gotPath, gotQuery, gotHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotHeader = r.Header.Get("X-Goog-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`)
	}))
	defer upstream.Close()

	svc := newRelayService(t, nil, relayProvider("relay", "gemini", upstream.URL, ""))
	req := chatRequest(t, "/v1beta/models/my-combo:generateContent?key=client-key", map[string]any{"contents": []any{}})
	w := httptest.NewRecorder()
	svc.Handle(w, req, "gemini", false)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.HasSuffix(gotPath, "/models/m:generateContent") {
		t.Fatalf("path = %q", gotPath)
	}
	if strings.Count(gotQuery, "key=") != 1 || !strings.Contains(gotQuery, "key=client-key") {
		t.Fatalf("query = %q", gotQuery)
	}
	if gotHeader != "" {
		t.Fatalf("unexpected synthesized header: %q", gotHeader)
	}
}

func TestRelayGeminiHeaderOnlyAddsNoQueryKey(t *testing.T) {
	var gotQuery, gotHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotHeader = r.Header.Get("X-Goog-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[]}`)
	}))
	defer upstream.Close()

	svc := newRelayService(t, nil, relayProvider("relay", "gemini", upstream.URL, ""))
	req := chatRequest(t, "/v1beta/models/my-combo:generateContent", map[string]any{"contents": []any{}})
	req.Header.Set("X-Goog-Api-Key", "header-key")
	w := httptest.NewRecorder()
	svc.Handle(w, req, "gemini", false)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if strings.Contains(gotQuery, "key=") {
		t.Fatalf("unexpected query key: %q", gotQuery)
	}
	if gotHeader != "header-key" {
		t.Fatalf("header = %q", gotHeader)
	}
}

func TestRelayHealthRuleFallsThroughToNextMember(t *testing.T) {
	var secondCalls int
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"quota"}}`)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer second.Close()

	p1 := relayProvider("relay-1", "openai", first.URL, "")
	p1.HealthCheckRules = []config.HealthCheckRule{{
		Description: "quota", Action: "rotate", MatchType: "equals",
		HTTPStatusCodes: []int{http.StatusTooManyRequests},
	}}
	p2 := relayProvider("relay-2", "openai", second.URL, "")

	svc := newRelayService(t, nil, p1, p2)
	req := chatRequest(t, "/v1/chat/completions", map[string]any{"model": "my-combo"})
	w := httptest.NewRecorder()
	svc.Handle(w, req, "openai", false)

	if w.Code != 200 || secondCalls != 1 {
		t.Fatalf("status=%d calls=%d body=%s", w.Code, secondCalls, w.Body.String())
	}
}

func TestBuildRelayHeadersPreservesAuth(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer user-key")
	req.Header.Set("X-Api-Key", "user-key-2")
	req.Header.Set("X-Goog-Api-Key", "user-key-3")
	req.Header.Set("Accept-Encoding", "br")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Content-Length", "999")

	h := buildRelayHeaders(req)
	if h.Get("Authorization") != "Bearer user-key" || h.Get("X-Api-Key") != "user-key-2" || h.Get("X-Goog-Api-Key") != "user-key-3" {
		t.Fatalf("credentials not preserved: %+v", h)
	}
	for _, k := range []string{"Accept-Encoding", "Connection", "Content-Length"} {
		if h.Get(k) != "" {
			t.Fatalf("%s should be stripped, got %q", k, h.Get(k))
		}
	}
}

func TestOutboundCompression(t *testing.T) {
	payloadContains := `"model":"m"`

	decompressors := map[string]func([]byte) ([]byte, error){
		"gzip": func(b []byte) ([]byte, error) {
			r, err := gzip.NewReader(bytes.NewReader(b))
			if err != nil {
				return nil, err
			}
			defer r.Close()
			return io.ReadAll(r)
		},
		"deflate": func(b []byte) ([]byte, error) {
			r, err := zlib.NewReader(bytes.NewReader(b))
			if err != nil {
				return nil, err
			}
			defer r.Close()
			return io.ReadAll(r)
		},
		"br": func(b []byte) ([]byte, error) {
			return io.ReadAll(brotli.NewReader(bytes.NewReader(b)))
		},
		"zstd": func(b []byte) ([]byte, error) {
			r, err := zstd.NewReader(bytes.NewReader(b))
			if err != nil {
				return nil, err
			}
			defer r.Close()
			return io.ReadAll(r)
		},
	}

	for method, decompress := range decompressors {
		t.Run(method, func(t *testing.T) {
			var gotEncoding string
			var gotBody []byte
			var gotLength int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotEncoding = r.Header.Get("Content-Encoding")
				gotLength = r.ContentLength
				raw, _ := io.ReadAll(r.Body)
				decoded, err := decompress(raw)
				if err != nil {
					t.Errorf("decompress: %v", err)
					return
				}
				gotBody = decoded
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
			}))
			defer upstream.Close()

			svc := newRelayService(t, nil, relayProvider("relay", "openai", upstream.URL, method))
			req := chatRequest(t, "/v1/chat/completions", map[string]any{"model": "my-combo", "messages": []any{}})
			w := httptest.NewRecorder()
			svc.Handle(w, req, "openai", false)

			if w.Code != 200 {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			if gotEncoding != method {
				t.Fatalf("Content-Encoding = %q, want %q", gotEncoding, method)
			}
			if !strings.Contains(string(gotBody), payloadContains) {
				t.Fatalf("decoded body = %s", gotBody)
			}
			if gotLength <= 0 {
				t.Fatalf("ContentLength = %d", gotLength)
			}
		})
	}
}

func TestOutboundCompressionSkipsNonJSON(t *testing.T) {
	svc := newRelayService(t, nil, relayProvider("relay", "openai", "http://127.0.0.1:1", "gzip"))
	body := []byte(`{"a":1}`)
	for _, ct := range []string{
		"multipart/form-data; boundary=x",
		"text/plain",
		"application/octet-stream",
	} {
		h := http.Header{}
		h.Set("Content-Type", ct)
		got := svc.compressUpstreamBody("relay", body, h)
		if h.Get("Content-Encoding") != "" || !bytes.Equal(got, body) {
			t.Fatalf("ct=%q: encoding=%q compressed=%v", ct, h.Get("Content-Encoding"), !bytes.Equal(got, body))
		}
	}

	h := http.Header{}
	h.Set("Content-Type", "application/json")
	got := svc.compressUpstreamBody("relay", body, h)
	if h.Get("Content-Encoding") != "gzip" || bytes.Equal(got, body) {
		t.Fatalf("expected gzip, got encoding=%q", h.Get("Content-Encoding"))
	}
}
