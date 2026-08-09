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

	// writeFrame processes exactly one complete SSE frame.
	//
	// A frame is either:
	//   - "data: {...}\n\n"                        (OpenAI / Gemini style)
	//   - "event: foo\ndata: {...}\n\n"            (Anthropic style)
	//
	// When translation is needed, we extract the data: line from the frame
	// regardless of whether an event: prefix is present, so that the translator
	// always receives "data: <json>" as its input. This fixes the case where
	// an Anthropic upstream returns event:-prefixed frames that were previously
	// silently dropped because the translator checks for a "data:" prefix.
	writeFrame := func(frame []byte) bool {
		if !needTranslate || len(frame) == 0 {
			_, werr := w.Write(frame)
			if doAccumulate {
				accumulator = append(accumulator, frame...)
			}
			return werr == nil
		}

		// Extract the data: line from the frame.
		// Handles both single-line "data: {...}" and multi-line "event: ...\ndata: {...}" formats.
		dataLine := extractSSEDataLine(frame)
		if dataLine == nil {
			// No data: line (comment, keepalive, or event:-only frame) — pass through as-is.
			_, werr := w.Write(frame)
			if doAccumulate {
				accumulator = append(accumulator, frame...)
			}
			return werr == nil
		}

		// Build the translation input: always "data: <json>" so translators can strip the prefix.
		translationLine := dataLine
		if upstreamAPIFormat == "openai" {
			// Normalize non-standard reasoning field names (e.g. "reasoning" → "reasoning_content")
			// before handing off to the translator.
			normalized := normalizeOpenAIReasoningField(stripSSEPrefix(dataLine))
			if !bytes.HasPrefix(normalized, []byte("data:")) {
				normalized = append([]byte("data: "), normalized...)
			}
			translationLine = normalized
		}

		translated := translate.TranslateResponseStream(
			ctx, clientAPIFormat, upstreamAPIFormat, model,
			originalClientBody, body, translationLine, &translateParam,
		)
		for _, out := range translated {
			if len(out) == 0 {
				continue
			}
			// CLIProxyAPI translators may return either:
			//   (a) a complete SSE frame: "event: ...\ndata: ...\n\n"
			//   (b) bare JSON bytes (legacy path)
			// Only wrap case (b) with "data: ...\n\n".
			isSSEFrame := bytes.HasPrefix(out, []byte("data:")) || bytes.HasPrefix(out, []byte("event:"))
			if !isSSEFrame {
				out = append([]byte("data: "), append(out, '\n', '\n')...)
			}
			_, werr := w.Write(out)
			if doAccumulate {
				accumulator = append(accumulator, out...)
			}
			if werr != nil {
				return false
			}
		}
		return true
	}

	// Write buffered first frame(s), then stream the rest.
	holder := newUsageHolder()

	// processFrames splits buffered data into complete SSE frames and processes each one.
	// Used for the initial buffered first-frame data which may contain multiple frames.
	processFrames := func(data []byte) bool {
		for _, frame := range splitSSEFrames(data) {
			sniffUsageChunk(frame, upstreamAPIFormat, holder)
			if !writeFrame(frame) {
				return false
			}
		}
		return true
	}
	if len(first) > 0 {
		if !processFrames(first) {
			resp.Body.Close()
			s.finishStream(combo, providerName, model, key, clientAPIFormat, statusCode, success, holder.usage, t0, matchedPayload, clientCtx, upstreamCtx, accumulator)
			return true, nil
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	// Stream remaining frames using a scanner that splits on SSE frame boundaries (\n\n).
	// This ensures each Scan() token is exactly one complete SSE frame, preventing
	// content loss when TCP packet boundaries fall inside an SSE frame.
	scanner := bufio.NewScanner(reader)
	scanner.Split(splitSSEFrame)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		frame := scanner.Bytes()
		if len(bytes.TrimSpace(frame)) == 0 {
			continue
		}
		sniffUsageChunk(frame, upstreamAPIFormat, holder)
		if !writeFrame(frame) {
			resp.Body.Close()
			s.finishStream(combo, providerName, model, key, clientAPIFormat, statusCode, success, holder.usage, t0, matchedPayload, clientCtx, upstreamCtx, accumulator)
			return true, nil
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	resp.Body.Close()
	s.finishStream(combo, providerName, model, key, clientAPIFormat, statusCode, success, holder.usage, t0, matchedPayload, clientCtx, upstreamCtx, accumulator)
	return true, nil
}

// splitSSEFrame is a bufio.SplitFunc that splits on SSE frame boundaries (\n\n or \r\n\r\n).
// Each returned token is one complete SSE frame including its trailing newlines.
func splitSSEFrame(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	// \r\n\r\n first (more specific)
	if i := bytes.Index(data, []byte("\r\n\r\n")); i >= 0 {
		return i + 4, data[:i+4], nil
	}
	// \n\n
	if i := bytes.Index(data, []byte("\n\n")); i >= 0 {
		return i + 2, data[:i+2], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// splitSSEFrames splits buffered SSE data into individual complete frames.
// Used for the initial first-frame buffer which may contain multiple frames.
func splitSSEFrames(data []byte) [][]byte {
	var result [][]byte
	parts := bytes.Split(data, []byte("\n\n"))
	for _, p := range parts {
		p = bytes.TrimSpace(p)
		if len(p) > 0 {
			result = append(result, append(p, '\n', '\n'))
		}
	}
	return result
}

// extractSSEDataLine extracts the "data: ..." line from a complete SSE frame.
// Handles both single-line frames ("data: {...}") and multi-line frames
// ("event: foo\ndata: {...}") as used by Anthropic upstreams.
// Returns the full "data: <content>" line (with prefix), or nil if not found.
func extractSSEDataLine(frame []byte) []byte {
	for _, line := range bytes.Split(frame, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, []byte("data:")) {
			return line
		}
	}
	return nil
}

// stripSSEPrefix removes the "data: " prefix from a data line, returning raw JSON.
func stripSSEPrefix(line []byte) []byte {
	line = bytes.TrimSpace(line)
	if bytes.HasPrefix(line, []byte("data:")) {
		return bytes.TrimSpace(line[5:])
	}
	return line
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
