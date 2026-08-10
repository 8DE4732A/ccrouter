package proxy

import (
	"testing"

	"ccrouter/internal/config"
)

func TestSplitSSEFrame(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		atEOF   bool
		wantAdv int
		wantTok []byte
	}{
		{
			name:    "single frame \\n\\n",
			data:    []byte("data: {\"a\":1}\n\ndata: {\"b\":2}\n\n"),
			wantAdv: 15,
			wantTok: []byte("data: {\"a\":1}\n\n"),
		},
		{
			name:    "single frame \\r\\n\\r\\n",
			data:    []byte("data: {\"a\":1}\r\n\r\ndata: {\"b\":2}\r\n\r\n"),
			wantAdv: 17,
			wantTok: []byte("data: {\"a\":1}\r\n\r\n"),
		},
		{
			name:    "incomplete frame no EOF",
			data:    []byte("data: {\"a\":1}"),
			atEOF:   false,
			wantAdv: 0,
			wantTok: nil,
		},
		{
			name:    "incomplete frame at EOF",
			data:    []byte("data: {\"a\":1}"),
			atEOF:   true,
			wantAdv: 13,
			wantTok: []byte("data: {\"a\":1}"),
		},
		{
			name:    "empty at EOF",
			data:    []byte{},
			atEOF:   true,
			wantAdv: 0,
			wantTok: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adv, tok, err := splitSSEFrame(tc.data, tc.atEOF)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if adv != tc.wantAdv {
				t.Errorf("advance: got %d, want %d", adv, tc.wantAdv)
			}
			if string(tok) != string(tc.wantTok) {
				t.Errorf("token: got %q, want %q", tok, tc.wantTok)
			}
		})
	}
}

func TestExtractSSEDataLine(t *testing.T) {
	tests := []struct {
		name  string
		frame []byte
		want  []byte
	}{
		{
			name:  "data-only frame",
			frame: []byte("data: {\"type\":\"message\"}\n\n"),
			want:  []byte("data: {\"type\":\"message\"}"),
		},
		{
			name:  "event+data frame (anthropic style)",
			frame: []byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\"}\n\n"),
			want:  []byte("data: {\"type\":\"content_block_delta\"}"),
		},
		{
			name:  "no data line (comment)",
			frame: []byte(": keepalive\n\n"),
			want:  nil,
		},
		{
			name:  "data DONE frame",
			frame: []byte("data: [DONE]\n\n"),
			want:  []byte("data: [DONE]"),
		},
		{
			name:  "event-only frame no data",
			frame: []byte("event: ping\n\n"),
			want:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractSSEDataLine(tc.frame)
			if string(got) != string(tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStripSSEPrefix(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  []byte
	}{
		{
			name:  "data: prefix",
			input: []byte("data: {\"a\":1}"),
			want:  []byte("{\"a\":1}"),
		},
		{
			name:  "data: with spaces",
			input: []byte("data:  {\"a\":1} "),
			want:  []byte("{\"a\":1}"),
		},
		{
			name:  "no data prefix",
			input: []byte("{\"a\":1}"),
			want:  []byte("{\"a\":1}"),
		},
		{
			name:  "event: line unchanged",
			input: []byte("event: message_start"),
			want:  []byte("event: message_start"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stripSSEPrefix(tc.input)
			if string(got) != string(tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSplitSSEFrameCRLF(t *testing.T) {
	// The scanner must correctly split CRLF-terminated frames, including
	// multiple frames delivered in a single Read().
	data := []byte("data: {\"a\":1}\r\n\r\ndata: {\"b\":2}\r\n\r\ndata: [DONE]\r\n\r\n")
	var frames []string
	pos := 0
	for pos < len(data) {
		adv, tok, err := splitSSEFrame(data[pos:], false)
		if err != nil || adv == 0 {
			t.Fatalf("splitSSEFrame: adv=%d err=%v at pos %d", adv, err, pos)
		}
		frames = append(frames, string(tok))
		pos += adv
	}
	if len(frames) != 3 {
		t.Fatalf("expected 3 CRLF frames, got %d: %q", len(frames), frames)
	}
	if frames[0] != "data: {\"a\":1}\r\n\r\n" {
		t.Errorf("frame[0]: got %q", frames[0])
	}
	if frames[1] != "data: {\"b\":2}\r\n\r\n" {
		t.Errorf("frame[1]: got %q", frames[1])
	}
	if frames[2] != "data: [DONE]\r\n\r\n" {
		t.Errorf("frame[2]: got %q", frames[2])
	}
}

func TestSplitSSEFrameMixedDeliveries(t *testing.T) {
	// Simulate input where a complete frame is followed by a partial frame
	// (as if a TCP boundary split the stream mid-frame). The SplitFunc must
	// return the first complete frame and let the scanner advance; the leftover
	// partial bytes are buffered until the next Scan().
	cases := []struct {
		name    string
		data    []byte
		wantAdv int
		wantTok []byte
	}{
		{
			name:    "complete frame then partial",
			data:    []byte("data: {\"a\":1}\n\ndata: {\"b\":2"),
			wantAdv: 15,
			wantTok: []byte("data: {\"a\":1}\n\n"),
		},
		{
			name:    "frame then partial json",
			data:    []byte("data: {\"a\":1}\n\ndata: {\"b\":2}\n\ndata: {\"c\""),
			wantAdv: 15,
			wantTok: []byte("data: {\"a\":1}\n\n"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adv, tok, err := splitSSEFrame(tc.data, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if adv != tc.wantAdv {
				t.Errorf("advance: got %d, want %d", adv, tc.wantAdv)
			}
			if string(tok) != string(tc.wantTok) {
				t.Errorf("token: got %q, want %q", tok, tc.wantTok)
			}
		})
	}
}

// newServiceWithStatusCodeRule builds a minimal Service with a single
// http_status_codes-only rule registered for "prov", for testing checkSSEError.
func newServiceWithStatusCodeRule(codes []int) *Service {
	rule := &config.HealthCheckRule{
		Description:     "status rule",
		HTTPStatusCodes: codes,
		Action:          "rotate",
		CooldownSeconds: 60,
	}
	return &Service{
		providerRules: map[string][]compiledRule{
			"prov": {{rule: rule}}, // no JSONPath expr compiled — status-only rule
		},
	}
}

// TestCheckSSEErrorStatusCodeMatchesOnEmptyFirstFrame verifies that an
// http_status_codes rule fires even when the first SSE frame is empty (e.g.
// the upstream closed the connection immediately after a 429, before sending
// any "data:" line). This must match the behavior of matchRotationRules
// (the non-streaming path), which checks status codes unconditionally.
func TestCheckSSEErrorStatusCodeMatchesOnEmptyFirstFrame(t *testing.T) {
	s := newServiceWithStatusCodeRule([]int{429})
	if matched := s.checkSSEError(nil, "prov", "model", 429); matched == nil {
		t.Fatal("expected status-code rule to match on empty first frame, got nil")
	}
}

// TestCheckSSEErrorStatusCodeMatchesOnNonJSONFirstFrame verifies the rule fires
// even when the first frame contains bytes that aren't a parseable "data:" JSON
// line (e.g. an HTML error page or plain text body from a proxy/CDN).
func TestCheckSSEErrorStatusCodeMatchesOnNonJSONFirstFrame(t *testing.T) {
	s := newServiceWithStatusCodeRule([]int{429})
	if matched := s.checkSSEError([]byte("<html>rate limited</html>"), "prov", "model", 429); matched == nil {
		t.Fatal("expected status-code rule to match on non-JSON first frame, got nil")
	}
}

// TestCheckSSEErrorStatusCodeNoMatchOnDifferentCode verifies the rule doesn't
// fire when the status code doesn't match any configured code.
func TestCheckSSEErrorStatusCodeNoMatchOnDifferentCode(t *testing.T) {
	s := newServiceWithStatusCodeRule([]int{429})
	if matched := s.checkSSEError(nil, "prov", "model", 200); matched != nil {
		t.Fatal("expected no match for status 200 with rule on [429]")
	}
}
