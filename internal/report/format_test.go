package report

import (
	"bytes"
	"os"
	"testing"
)

func TestEncodeDecodeChunkRoundtrip(t *testing.T) {
	raw := []byte(`{"a":1}` + "\n" + `{"a":2}` + "\n")
	chunk, err := encodeChunk(raw, 2, 100, 200, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := parseChunkHeader(chunk)
	if err != nil {
		t.Fatal(err)
	}
	if meta.recordCount != 2 || meta.firstTS != 100 || meta.lastTS != 200 || !meta.checkpoint {
		t.Fatalf("unexpected meta: %+v", meta)
	}
	payload := chunk[chunkHeaderSz:]
	if int64(len(payload)) != meta.payloadLen {
		t.Fatalf("payload length mismatch: header says %d, got %d", meta.payloadLen, len(payload))
	}
	decoded, err := decodeChunkPayload(payload, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, raw) {
		t.Fatalf("decoded mismatch: got %q want %q", decoded, raw)
	}
}

func TestChunkDictionaryChainReducesSize(t *testing.T) {
	// A highly repetitive "conversation history" payload: each record repeats
	// almost all of the previous one's content plus a small delta, mimicking
	// coding-agent traffic (see PoC numbers in format.go's doc comment).
	base := bytes.Repeat([]byte("the quick brown fox jumps over the lazy dog. "), 2000)
	rec1 := append(append([]byte{}, base...), []byte("delta-1\n")...)
	rec2 := append(append([]byte{}, base...), []byte("delta-1\ndelta-2\n")...)

	chunk1, err := encodeChunk(rec1, 1, 1, 1, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	// chunk2 chained off chunk1's raw content.
	chunk2Chained, err := encodeChunk(rec2, 1, 2, 2, rec1, false)
	if err != nil {
		t.Fatal(err)
	}
	// chunk2 with no dictionary, for comparison.
	chunk2Standalone, err := encodeChunk(rec2, 1, 2, 2, nil, true)
	if err != nil {
		t.Fatal(err)
	}

	if len(chunk2Chained) >= len(chunk2Standalone) {
		t.Fatalf("expected chained chunk to be smaller: chained=%d standalone=%d",
			len(chunk2Chained), len(chunk2Standalone))
	}
	t.Logf("chunk1=%dB chunk2Chained=%dB chunk2Standalone=%dB (raw rec2=%dB)",
		len(chunk1), len(chunk2Chained), len(chunk2Standalone), len(rec2))

	// Verify the chained chunk still decodes correctly with the right dict.
	meta, err := parseChunkHeader(chunk2Chained)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeChunkPayload(chunk2Chained[chunkHeaderSz:], rec1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, rec2) {
		t.Fatal("chained chunk did not decode back to original content")
	}
	if meta.checkpoint {
		t.Fatal("chained chunk should not be marked as checkpoint")
	}
}

func TestDecodeChunkWithWrongDictFails(t *testing.T) {
	// The dictionary must be large/repetitive enough that the encoder
	// actually references it (small inputs get encoded as literals and never
	// touch the dictionary, which would make this test vacuous).
	dict := bytes.Repeat([]byte("the quick brown fox jumps over the lazy dog. "), 2000)
	raw := append(append([]byte{}, dict...), []byte("delta\n")...)
	chunk, err := encodeChunk(raw, 1, 1, 1, dict, false)
	if err != nil {
		t.Fatal(err)
	}
	payload := chunk[chunkHeaderSz:]

	// Decoding without any dict must fail outright (unknown dictionary ID).
	if _, err := decodeChunkPayload(payload, nil); err == nil {
		t.Fatal("expected decode to fail without the correct dictionary")
	}
	// Decoding with a wrong dictionary of the same size/id must be caught by
	// the CRC checksum (see WithEncoderCRC in format.go) rather than silently
	// returning corrupted content.
	wrongDict := bytes.Repeat([]byte("ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ "), 2000)
	if _, err := decodeChunkPayload(payload, wrongDict); err == nil {
		t.Fatal("expected decode to fail (CRC mismatch) with a mismatched dictionary")
	}
	// The correct dict must still work.
	decoded, err := decodeChunkPayload(payload, dict)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, raw) {
		t.Fatal("decoded mismatch with correct dict")
	}
}

func TestParseChunkHeaderRejectsGarbage(t *testing.T) {
	if _, err := parseChunkHeader([]byte("too short")); err == nil {
		t.Fatal("expected error for too-short header")
	}
	garbage := make([]byte, chunkHeaderSz)
	for i := range garbage {
		garbage[i] = 0xFF
	}
	if _, err := parseChunkHeader(garbage); err == nil {
		t.Fatal("expected error for bad magic number")
	}
}

func TestScanChunkIndexToleratesTruncatedTrailingChunk(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/segment"

	raw1 := []byte(`{"a":1}` + "\n")
	chunk1, _ := encodeChunk(raw1, 1, 1, 1, nil, true)
	raw2 := []byte(`{"a":2}` + "\n")
	chunk2, _ := encodeChunk(raw2, 1, 2, 2, raw1, false)

	// Simulate a crash mid-write: chunk1 fully written, chunk2 truncated.
	var buf bytes.Buffer
	buf.Write(chunk1)
	buf.Write(chunk2[:len(chunk2)/2])
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	metas, err := scanChunkIndex(path)
	if err != nil {
		t.Fatalf("scanChunkIndex should tolerate a truncated trailing chunk, got err: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("expected exactly 1 valid chunk recovered, got %d", len(metas))
	}
	if metas[0].recordCount != 1 || metas[0].firstTS != 1 {
		t.Fatalf("unexpected recovered chunk meta: %+v", metas[0])
	}
}
