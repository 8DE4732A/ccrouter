package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"ccrouter/internal/combos"
	"ccrouter/internal/config"
	"ccrouter/internal/db"
	"ccrouter/internal/keys"
	"ccrouter/internal/report"
	"ccrouter/internal/script"
	"ccrouter/internal/translate"
)

// hopByHopHeaders are stripped from forwarded requests and responses.
var hopByHopHeaders = map[string]bool{
	"connection":          true,
	"keep-alive":          true,
	"transfer-encoding":   true,
	"te":                  true,
	"trailer":             true,
	"upgrade":             true,
	"proxy-authorization": true,
	"proxy-authenticate":  true,
}

var responseHeadersToIgnore = func() map[string]bool {
	m := map[string]bool{}
	for k := range hopByHopHeaders {
		m[k] = true
	}
	m["content-encoding"] = true
	m["content-length"] = true
	return m
}()

// Service is the core proxy with two-level retry: combo member selection + key rotation.
type Service struct {
	Config          *config.AppConfig
	KeyManagers     map[string]*keys.Manager
	Router          *combos.Router
	ProviderClients map[string]*http.Client // per-provider http.Client
	Recorder        *db.Recorder
	Report          *report.Logger
	verbose         bool

	providers      map[string]*config.ProviderConfig
	providerRules  map[string][]compiledRule
	payloadScripts []compiledScript
}

type compiledScript struct {
	name    string
	enabled bool
	prog    *script.Program
}

type compiledRule struct {
	expr jsonPathExpr
	rule *config.HealthCheckRule
}

// New builds a Service from config and managers.
func New(cfg *config.AppConfig, kms map[string]*keys.Manager, router *combos.Router,
	providerClients map[string]*http.Client, rec *db.Recorder, rl *report.Logger) (*Service, error) {
	s := &Service{
		Config:          cfg,
		KeyManagers:     kms,
		Router:          router,
		ProviderClients: providerClients,
		Recorder:        rec,
		Report:          rl,
		verbose:         cfg.VerboseLogging,
		providers:       make(map[string]*config.ProviderConfig),
		providerRules:   make(map[string][]compiledRule),
	}

	// Compile payload scripts at startup — fail fast on syntax errors.
	for i, ps := range cfg.PayloadScripts {
		prog, err := script.Compile(ps.Script)
		if err != nil {
			return nil, fmt.Errorf("payload_scripts[%d] %q: %w", i, ps.Name, err)
		}
		s.payloadScripts = append(s.payloadScripts, compiledScript{
			name:    ps.Name,
			enabled: ps.Enabled,
			prog:    prog,
		})
	}

	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		s.providers[p.Name] = p
		for j := range p.HealthCheckRules {
			r := &p.HealthCheckRules[j]
			expr, err := compileJSONPath(r.JSONPath)
			if err != nil {
				return nil, fmt.Errorf("invalid jsonpath %q in provider %q: %v", r.JSONPath, p.Name, err)
			}
			s.providerRules[p.Name] = append(s.providerRules[p.Name], compiledRule{expr: expr, rule: r})
		}
	}
	return s, nil
}

// Handle processes a single client request for the given API format.
func (s *Service) Handle(w http.ResponseWriter, r *http.Request, apiFormat string, forceNonStream bool) {
	t0 := time.Now()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		jsonError(w, 400, "failed to read request body", "proxy_error")
		return
	}

	isStream := false
	if !forceNonStream {
		isStream = isStreamingRequest(r, body)
	}
	requestedModel := extractModel(body)

	// Capture client context once for verbose logging (zero-cost when disabled).
	var clientCtx map[string]any
	if s.verbose && s.Report != nil {
		clientCtx = map[string]any{
			"method":  r.Method,
			"path":    r.URL.Path,
			"headers": headerToMap(r.Header),
			"body":    tryParseJSON(body),
		}
	}

	combo := s.Router.GetCombo(requestedModel)
	if combo == nil {
		jsonError(w, 400, fmt.Sprintf("unknown combo: %q", requestedModel), "proxy_error")
		return
	}
	if !containsString(combo.APIFormats(), apiFormat) {
		jsonError(w, 400, fmt.Sprintf("combo %q supports formats %v, but request was sent to %q endpoint",
			requestedModel, combo.APIFormats(), apiFormat), "proxy_error")
		return
	}

	attemptedMembers := map[[2]string]bool{}

	for {
		providerName, model, upstreamFmt := s.Router.NextMemberWithFmt(requestedModel, attemptedMembers)
		if providerName == "" {
			jsonError(w, 503, fmt.Sprintf("all providers exhausted for combo %q", requestedModel), "proxy_error")
			return
		}

		km := s.KeyManagers[providerName]
		providerCfg := s.providers[providerName]

		// Resolve the upstream API format to use for this member:
		//   1. If provider natively supports the client format → use it directly (no translation).
		//   2. Else if upstream_api_format hint is set and provider has that endpoint → use hint.
		//   3. Else fall back to the first available endpoint on the provider.
		// Translation only happens when the resolved upstream format differs from apiFormat.
		upstreamFmt = resolveUpstreamFormat(providerCfg, apiFormat, upstreamFmt)
		targetURL := providerCfg.GetChatURL(upstreamFmt)
		if targetURL == "" {
			jsonError(w, 502, fmt.Sprintf("provider %q has no usable endpoint for request format %q", providerName, apiFormat), "proxy_error")
			return
		}

		// Translate request body when the resolved upstream format differs from the client format.
		clientBody := rewriteModel(body, model)
		upstreamBody := clientBody
		var translationTag string // e.g. "anthropic→openai", appended to matched_payload
		if upstreamFmt != apiFormat {
			upstreamBody = translate.TranslateRequest(apiFormat, upstreamFmt, model, clientBody, isStream)
			translationTag = apiFormat + "→" + upstreamFmt
		}

		attemptedKeys := map[string]bool{}
		maxAttempts := providerCfg.MaxRetries + 1

		handled := false
		for i := 0; i < maxAttempts; i++ {
			key := km.Get(model, attemptedKeys)
			if key == "" {
				break
			}
			attemptedKeys[key] = true
			headers := buildHeaders(r, key, upstreamFmt)

			// Run enabled payload scripts in order (chained).
			// Scripts operate on the upstream body (already translated).
			actualBody, headers, scriptTag := s.runPayloadScripts(
				upstreamBody, headers, requestedModel, r.URL.Path, providerName, model, upstreamFmt,
			)
			// Combine translation tag and script tag into a single matched_payload string.
			matchedPayload := joinTags(translationTag, scriptTag)

			// Build upstream context for verbose logging.
			var upstreamCtx map[string]any
			if clientCtx != nil {
				upstreamCtx = map[string]any{
					"url":     targetURL,
					"headers": headerToMap(headers),
					"body":    tryParseJSON(actualBody),
				}
			}

			done, matched := s.attempt(w, r, t0, km, targetURL, headers, actualBody,
				providerName, model, isStream, requestedModel, key, apiFormat, upstreamFmt, matchedPayload,
				clientBody, clientCtx, upstreamCtx)
			if done {
				handled = true
				break
			}
			if matched != nil {
				// Health rule triggered: cool down this key and record the rotation event.
				km.RecordError(key, model, matched.CooldownSeconds)
				log.Printf("Rotation: provider=%s key=%.8s model=%s rule=%q cooldown=%ds",
					providerName, key, model, matched.Description, matched.CooldownSeconds)
				s.record(requestedModel, providerName, model, key, apiFormat,
					isStream, nil, false, matched.Description,
					map[string]any{}, t0, nil, &matchedPayload)
				continue
			}
		}
		if handled {
			return
		}
		log.Printf("All keys exhausted: provider=%s model=%s, trying next member", providerName, model)
		attemptedMembers[[2]string{providerName, model}] = true
	}
}

// attempt performs one upstream call. Returns (done, matchedRule):
//   - done=true when the request was fully resolved (response written or fatal error),
//     and no further retry should occur.
//   - matchedRule != nil when a health rule triggered (rotate key, continue).
//
// clientAPIFormat is what the client sent; upstreamAPIFormat is what the upstream received.
// When they differ the response is back-translated before forwarding to the client.
func (s *Service) attempt(w http.ResponseWriter, r *http.Request, t0 time.Time, km *keys.Manager, targetURL string,
	headers http.Header, body []byte, providerName, model string, isStream bool,
	combo, key, clientAPIFormat, upstreamAPIFormat, matchedPayload string,
	originalClientBody []byte,
	clientCtx, upstreamCtx map[string]any) (bool, *config.HealthCheckRule) {

	if isStream {
		return s.attemptStreaming(w, r, t0, km, targetURL, headers, body, providerName, model, combo, key, clientAPIFormat, upstreamAPIFormat, matchedPayload, originalClientBody, clientCtx, upstreamCtx)
	}
	return s.attemptNonStreaming(w, t0, km, targetURL, headers, body, providerName, model, combo, key, clientAPIFormat, upstreamAPIFormat, matchedPayload, originalClientBody, clientCtx, upstreamCtx)
}

func (s *Service) attemptNonStreaming(w http.ResponseWriter, t0 time.Time, km *keys.Manager,
	targetURL string, headers http.Header, body []byte, providerName, model, combo, key,
	clientAPIFormat, upstreamAPIFormat, matchedPayload string,
	originalClientBody []byte,
	clientCtx, upstreamCtx map[string]any) (bool, *config.HealthCheckRule) {

	req, err := http.NewRequest("POST", targetURL, bytes.NewReader(body))
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("failed to build upstream request: %v", err), "proxy_error")
		return true, nil
	}
	req.Header = headers

	client := s.clientFor(providerName)
	resp, err := client.Do(req)
	if err != nil {
		return s.networkError(w, t0, err, km, providerName, model, false, combo, key, clientAPIFormat, matchedPayload), nil
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	matched := s.matchRotationRules(respBody, providerName, model)
	if matched != nil {
		return false, matched
	}

	statusCode := resp.StatusCode
	httpOK := statusCode < 400

	// Extract usage from the original upstream response before any translation.
	// The translator does not carry usage fields across formats, so we must
	// read from the native response body using the upstream format.
	var usage map[string]any
	if httpOK {
		usage = extractUsage(respBody, upstreamAPIFormat)
		km.RecordSuccess(key, model)
	} else {
		usage = map[string]any{}
	}

	// Back-translate response when formats differ (upstream → client).
	outBody := respBody
	if httpOK && upstreamAPIFormat != clientAPIFormat {
		var param any
		if translated := translate.TranslateResponseNonStream(
			req.Context(), clientAPIFormat, upstreamAPIFormat, model,
			originalClientBody, body, respBody, &param,
		); len(translated) > 0 {
			outBody = translated
		}
	}
	var errText *string
	if !httpOK {
		t := extractErrorText(respBody)
		errText = &t
	}
	s.record(combo, providerName, model, key, clientAPIFormat, false, &statusCode, httpOK, "", usage, t0, errText, &matchedPayload)

	// Verbose logging.
	if clientCtx != nil && upstreamCtx != nil {
		s.reportLog(combo, providerName, model, clientAPIFormat, false, statusCode, httpOK,
			int(time.Since(t0).Milliseconds()), clientCtx, upstreamCtx,
			map[string]any{
				"status_code": statusCode,
				"headers":     headerToMap(resp.Header),
				"body":        tryParseJSON(outBody),
			})
	}

	copyResponseHeader(w, resp.Header)
	w.WriteHeader(statusCode)
	_, _ = w.Write(outBody)
	return true, nil
}

func (s *Service) networkError(w http.ResponseWriter, t0 time.Time, err error, km *keys.Manager,
	providerName, model string, isStream bool, combo, key, apiFormat, matchedPayload string) bool {
	var netErr net.Error
	var statusCode int
	var msg string
	if errors.As(err, &netErr) && netErr.Timeout() {
		statusCode = 504
		msg = "upstream timeout"
	} else {
		statusCode = 502
		msg = "upstream connection failed"
	}
	s.record(combo, providerName, model, key, apiFormat, isStream, &statusCode, false, "", map[string]any{}, t0, &msg, &matchedPayload)
	jsonError(w, statusCode, msg, "proxy_error")
	return true
}

// reportLog writes a verbose JSONL record when verbose logging is enabled.
func (s *Service) reportLog(
	combo, provider, model, apiFormat string,
	isStream bool, statusCode int, success bool, durationMs int,
	clientCtx, upstreamCtx, responseCtx map[string]any,
) {
	if !s.verbose || s.Report == nil {
		return
	}
	s.Report.Log(map[string]any{
		"ts":          float64(time.Now().UnixNano()) / 1e9,
		"combo":       combo,
		"provider":    provider,
		"model":       model,
		"api_format":  apiFormat,
		"is_stream":   isStream,
		"status_code": statusCode,
		"success":     success,
		"duration_ms": durationMs,
		"request": map[string]any{
			"client":   clientCtx,
			"upstream": upstreamCtx,
		},
		"response": responseCtx,
	})
}

// ---- Response wiring helpers ----

func copyResponseHeader(w http.ResponseWriter, h http.Header) {
	for k, vs := range h {
		if responseHeadersToIgnore[strings.ToLower(k)] {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, code int, msg, typ string) {
	writeJSON(w, code, map[string]any{"error": msg, "type": typ})
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ---- Request helpers ----

func extractModel(body []byte) string {
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return ""
	}
	m, _ := data["model"].(string)
	return m
}

func rewriteModel(body []byte, model string) []byte {
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return body
	}
	data["model"] = model
	out, err := json.Marshal(data)
	if err != nil {
		return body
	}
	return out
}

func isStreamingRequest(r *http.Request, body []byte) bool {
	accept := strings.ToLower(r.Header.Get("Accept"))
	if strings.Contains(accept, "text/event-stream") {
		return true
	}
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.Contains(ct, "text/event-stream") {
		return true
	}
	if strings.Contains(ct, "application/json") && len(body) > 0 {
		var data map[string]any
		if err := json.Unmarshal(body, &data); err != nil {
			return false
		}
		if v, ok := data["stream"].(bool); ok && v {
			return true
		}
	}
	return false
}

func buildHeaders(r *http.Request, apiKey string, upstreamAPIFormat string) http.Header {
	h := http.Header{}
	for k, vs := range r.Header {
		kl := strings.ToLower(k)
		if hopByHopHeaders[kl] {
			continue
		}
		// Remove Accept-Encoding: let Go's http.Client handle compression negotiation
		// and transparent decompression. If we forward the client's Accept-Encoding,
		// the upstream may return gzip/br and Go won't auto-decompress it.
		if kl == "accept-encoding" {
			continue
		}
		// Remove x-api-key / Authorization: we set the correct upstream auth below.
		if kl == "x-api-key" || kl == "authorization" {
			continue
		}
		for _, v := range vs {
			h.Add(k, v)
		}
	}

	// Set upstream auth header in the format the upstream API expects.
	// Anthropic endpoints use "x-api-key"; all others use "Authorization: Bearer".
	if upstreamAPIFormat == "anthropic" {
		h.Set("X-Api-Key", apiKey)
	} else {
		h.Set("Authorization", "Bearer "+apiKey)
	}

	h.Del("Host")
	h.Del("Content-Length")
	return h
}

func extractErrorText(body []byte) string {
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return truncateBytes(body, 200)
	}
	e, ok := data["error"]
	if !ok {
		return truncateBytes(body, 200)
	}
	if em, ok := e.(map[string]any); ok {
		if msg, ok := em["message"].(string); ok && msg != "" {
			return msg
		}
		b, _ := json.Marshal(em)
		return string(b)
	}
	return fmt.Sprintf("%v", e)
}

func truncateBytes(b []byte, n int) string {
	if len(b) > n {
		b = b[:n]
	}
	return string(b)
}

// ---- Payload scripts ----

// runPayloadScripts executes enabled scripts in order (chained).
// Returns rewritten body bytes, updated headers, and a summary string for DB recording.
func (s *Service) runPayloadScripts(
	body []byte,
	headers http.Header,
	combo, path, provider, model, apiFormat string,
) ([]byte, http.Header, string) {
	if len(s.payloadScripts) == 0 {
		return body, headers, ""
	}

	// Convert http.Header → map[string]string for script env (flat, lowercase key wins).
	hmap := make(map[string]string, len(headers))
	for k, vs := range headers {
		if len(vs) > 0 {
			hmap[strings.ToLower(k)] = vs[0]
		}
	}

	var parts []string
	for _, cs := range s.payloadScripts {
		if !cs.enabled {
			continue
		}
		var status string
		body, hmap, status = script.RunBytes(cs.prog, body, hmap, combo, path, provider, model, apiFormat)
		if status != "" {
			label := cs.name
			if label == "" {
				label = "script"
			}
			parts = append(parts, label+":"+status)
		}
	}

	// Rebuild http.Header from mutated map.
	for k := range headers {
		kl := strings.ToLower(k)
		if newVal, ok := hmap[kl]; ok {
			headers[k] = []string{newVal}
			delete(hmap, kl)
		} else {
			delete(headers, k)
		}
	}
	// Any newly added keys.
	for k, v := range hmap {
		headers.Set(k, v)
	}

	summary := strings.Join(parts, ", ")
	return body, headers, summary
}

// headerToMap converts http.Header to a flat map[string]string (first value per key).
func headerToMap(h http.Header) map[string]string {
	m := make(map[string]string, len(h))
	for k, vs := range h {
		if len(vs) > 0 {
			m[k] = vs[0]
		}
	}
	return m
}

// tryParseJSON returns the parsed JSON value if body is valid JSON, else the raw string.
func tryParseJSON(b []byte) any {
	var v any
	if err := json.Unmarshal(b, &v); err == nil {
		return v
	}
	return string(b)
}

// resolveUpstreamFormat decides which API format to use when sending to the upstream provider.
//
// Priority:
//  1. Provider natively supports the client format → use it as-is (no translation needed).
//  2. A hint (upstream_api_format from config) is set and provider has that endpoint → use hint.
//  3. Fallback: use the first endpoint the provider has (translate from client format to that).
func resolveUpstreamFormat(p *config.ProviderConfig, clientFmt, hint string) string {
	// 1. Native support — no translation.
	if p.SupportsFormat(clientFmt) {
		return clientFmt
	}
	// 2. Explicit hint, if valid.
	if hint != "" && p.SupportsFormat(hint) {
		return hint
	}
	// 3. First available endpoint as fallback.
	if len(p.APIs) > 0 {
		return p.APIs[0].APIFormat
	}
	return clientFmt // should never reach here after validation
}

// joinTags concatenates non-empty tag strings with ", ".
func joinTags(tags ...string) string {
	var parts []string
	for _, t := range tags {
		if t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, ", ")
}

// clientFor returns the per-provider http.Client, falling back to a default.
func (s *Service) clientFor(providerName string) *http.Client {
	if c, ok := s.ProviderClients[providerName]; ok {
		return c
	}
	return http.DefaultClient
}
