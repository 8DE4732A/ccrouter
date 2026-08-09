package proxy

import (
	"testing"
)

func TestSplitSSEFrame(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		atEOF    bool
		wantAdv  int
		wantTok  []byte
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

func TestSplitSSEFrames(t *testing.T) {
	data := []byte("data: {\"a\":1}\n\ndata: {\"b\":2}\n\n: comment\n\ndata: [DONE]\n\n")
	frames := splitSSEFrames(data)
	if len(frames) != 4 {
		t.Fatalf("expected 4 frames, got %d: %q", len(frames), frames)
	}
	if string(frames[0]) != "data: {\"a\":1}\n\n" {
		t.Errorf("frame[0]: got %q", frames[0])
	}
	if string(frames[1]) != "data: {\"b\":2}\n\n" {
		t.Errorf("frame[1]: got %q", frames[1])
	}
	if string(frames[2]) != ": comment\n\n" {
		t.Errorf("frame[2]: got %q", frames[2])
	}
	if string(frames[3]) != "data: [DONE]\n\n" {
		t.Errorf("frame[3]: got %q", frames[3])
	}
}
