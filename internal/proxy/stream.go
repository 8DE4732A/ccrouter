package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"ccrouter/internal/config"
	"ccrouter/internal/keys"
	"ccrouter/internal/translate"
)

// attemptStreaming handles a single streaming upstream attempt, writing the SSE
// stream directly to the client. Returns (done, matchedRule):
//   - done=true: response fully written (or fatal); no retry.
//   - matchedRule != nil: a health rule matched on the first SSE frame → rotate key.
func (s *Service) attemptStreaming(w http.ResponseWriter, r *http.Request, t0 time.Time, km *keys.Manager,
	targetURL string, headers http.Header, body []byte, providerName, model, combo, key,
	clientAPIFormat, upstreamAPIFormat, matchedPayload string,
	originalClientBody []byte,
	clientCtx, upstreamCtx map[string]any) (bool, *config.HealthCheckRule) {

	upstreamURL := targetURL
	if upstreamAPIFormat == "gemini" {
		upstreamURL = appendGeminiKey(targetURL, key)
	}
	req, err := http.NewRequestWithContext(r.Context(), "POST", upstreamURL, bytes.NewReader(body))
	if err != nil {
		jsonError(w, 500, "failed to build upstream request", "proxy_error")
		return true, nil
	}
	req.Header = headers

	resp, err := s.clientFor(providerName).Do(req)
	if err != nil {
		return s.networkError(w, t0, err, km, providerName, model, true, combo, key, clientAPIFormat, matchedPayload), nil
	}

	mediaType := resp.Header.Get("Content-Type")
	if mediaType == "" {
		mediaType = "text/event-stream"
	}
	statusCode := resp.StatusCode

	// If the upstream didn't return SSE, treat as a normal (possibly error) response.
	if !strings.Contains(strings.ToLower(mediaType), "text/event-stream") {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		matched := s.matchRotationRules(respBody, providerName, model)
		if matched != nil {
			return false, matched
		}
		httpOK := statusCode < 400

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

		if httpOK {
			km.RecordSuccess(key, model)
			usage := extractUsage(respBody, upstreamAPIFormat)
			s.record(combo, providerName, model, key, clientAPIFormat, true, &statusCode, true, "", usage, t0, nil, &matchedPayload)
		} else {
			errText := extractErrorText(respBody)
			s.record(combo, providerName, model, key, clientAPIFormat, true, &statusCode, false, "", map[string]any{}, t0, &errText, &matchedPayload)
		}
		if clientCtx != nil && upstreamCtx != nil {
			s.reportLog(combo, providerName, model, clientAPIFormat, true, statusCode, httpOK,
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

	// Buffer until first SSE frame boundary so we can check for errors.
	buf := bytes.Buffer{}
	chunk := make([]byte, 4096)
	reader := bufio.NewReader(resp.Body)
	var first []byte
	for {
		n, rerr := reader.Read(chunk)
		if n > 0 {
			buf.Write(chunk[:n])
			if bytes.Contains(buf.Bytes(), []byte("\n\n")) || bytes.Contains(buf.Bytes(), []byte("\r\n\r\n")) {
				break
			}
		}
		if rerr != nil {
			break // EOF
		}
	}
	first = buf.Bytes()

	matched := s.checkSSEError(first, providerName, model)
	if matched != nil {
		resp.Body.Close()
		return false, matched
	}

	// Determine success from status code (<400); RecordSuccess happens in finishStream
	// after the full stream is delivered (not before the stream has started).
	success := statusCode < 400

	// Set response headers once, before streaming.
	copyResponseHeader(w, resp.Header)
	w.Header().Set("Content-Type", mediaType)
	w.WriteHeader(statusCode)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// For verbose logging we accumulate the full SSE response bytes (only when enabled).
	var accumulator []byte
	doAccumulate := clientCtx != nil && upstreamCtx != nil

	// Per-chunk translator state (carried across SSE frames when translation is active).
	needTranslate := upstreamAPIFormat != clientAPIFormat
	var translateParam any
	ctx := req.Context()

	writeChunk := func(data []byte) bool {
		if !needTranslate || len(data) == 0 {
			_, werr := w.Write(data)
			if doAccumulate {
				accumulator = append(accumulator, data...)
			}
			return werr == nil
		}
		// Translate each complete SSE line individually.
		for _, line := range splitSSELines(data) {
			if len(line) == 0 {
				continue
			}
			translated := translate.TranslateResponseStream(
				ctx, clientAPIFormat, upstreamAPIFormat, model,
				originalClientBody, body, line, &translateParam,
			)
			for _, out := range translated {
				if len(out) == 0 {
					continue
				}
				_, werr := w.Write(out)
				if doAccumulate {
					accumulator = append(accumulator, out...)
				}
				if werr != nil {
					return false
				}
			}
		}
		return true
	}

	// Write buffered first frame, then stream the rest.
	holder := newUsageHolder()
	if len(first) > 0 {
		sniffUsageChunk(first, upstreamAPIFormat, holder)
		if !writeChunk(first) {
			resp.Body.Close()
			s.finishStream(combo, providerName, model, key, clientAPIFormat, statusCode, success, holder.usage, t0, matchedPayload, clientCtx, upstreamCtx, accumulator)
			return true, nil
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	// stream remaining body
	streamBuf := make([]byte, 4096)
	for {
		n, rerr := reader.Read(streamBuf)
		if n > 0 {
			sniffUsageChunk(streamBuf[:n], upstreamAPIFormat, holder)
			if !writeChunk(streamBuf[:n]) {
				resp.Body.Close()
				s.finishStream(combo, providerName, model, key, clientAPIFormat, statusCode, success, holder.usage, t0, matchedPayload, clientCtx, upstreamCtx, accumulator)
				return true, nil
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if rerr != nil {
			break
		}
	}
	resp.Body.Close()
	s.finishStream(combo, providerName, model, key, clientAPIFormat, statusCode, success, holder.usage, t0, matchedPayload, clientCtx, upstreamCtx, accumulator)
	return true, nil
}

// splitSSELines splits a raw SSE byte chunk into individual "data: ...\n\n" lines
// suitable for per-line translation. Empty sections between delimiters are skipped.
func splitSSELines(data []byte) [][]byte {
	var result [][]byte
	// Split on double-newline boundaries (SSE frame separator)
	parts := bytes.Split(data, []byte("\n\n"))
	for _, p := range parts {
		p = bytes.TrimSpace(p)
		if len(p) > 0 {
			result = append(result, append(p, '\n', '\n'))
		}
	}
	return result
}

func (s *Service) finishStream(combo, providerName, model, key, clientAPIFormat string,
	statusCode int, success bool, usage map[string]any, t0 time.Time, matchedPayload string,
	clientCtx, upstreamCtx map[string]any, accumulated []byte) {
	if success {
		s.KeyManagers[providerName].RecordSuccess(key, model)
	}
	s.record(combo, providerName, model, key, clientAPIFormat, true, &statusCode, success, "", usage, t0, nil, &matchedPayload)
	if clientCtx != nil && upstreamCtx != nil {
		s.reportLog(combo, providerName, model, clientAPIFormat, true, statusCode, success,
			int(time.Since(t0).Milliseconds()), clientCtx, upstreamCtx,
			map[string]any{
				"status_code": statusCode,
				"body":        tryParseJSON(accumulated),
			})
	}
}

// checkSSEError inspects the buffered first SSE chunk for a matching health rule.
func (s *Service) checkSSEError(data []byte, providerName, model string) *config.HealthCheckRule {
	text := string(data)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		jsonStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if jsonStr == "[DONE]" {
			continue
		}
		var parsed any
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
			continue
		}
		for _, cr := range s.providerRules[providerName] {
			if len(cr.rule.Models) > 0 && !containsString(cr.rule.Models, model) {
				continue
			}
			for _, v := range cr.expr.find(parsed) {
				if valueMatches(v, cr.rule) {
					return cr.rule
				}
			}
		}
	}
	return nil
}
