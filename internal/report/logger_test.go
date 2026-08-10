package report

import (
	"compress/gzip"
	"os"
	"strconv"
	"strings"
	"testing"
)

func writeGzipFile(path, content string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	if _, err := gz.Write([]byte(content)); err != nil {
		return err
	}
	return gz.Close()
}

func TestLogAndReadBack(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Log(map[string]any{"ts": 1.0, "combo": "fast", "success": true})
	l.Flush()

	res, err := l.Read(50, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	items := res["items"].([]map[string]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0]["combo"] != "fast" {
		t.Fatalf("expected combo fast, got %v", items[0]["combo"])
	}
}

func TestNewestFirst(t *testing.T) {
	dir := t.TempDir()
	l, _ := New(dir)
	defer l.Close()
	l.Log(map[string]any{"ts": 1.0, "n": 1})
	l.Log(map[string]any{"ts": 2.0, "n": 2})
	l.Flush()

	res, _ := l.Read(50, 0, nil)
	items := res["items"].([]map[string]any)
	if len(items) != 2 || items[0]["n"].(float64) != 2 || items[1]["n"].(float64) != 1 {
		t.Fatalf("expected newest first, got %v", items)
	}
}

func TestPaginationLimitOffset(t *testing.T) {
	dir := t.TempDir()
	l, _ := New(dir)
	defer l.Close()
	for i := float64(1); i <= 5; i++ {
		l.Log(map[string]any{"ts": i, "n": i})
	}
	l.Flush()

	res, _ := l.Read(2, 1, nil)
	items := res["items"].([]map[string]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	// newest-first: [5,4,3,2,1]; offset 1 -> [4,3]
	if items[0]["n"].(float64) != 4 || items[1]["n"].(float64) != 3 {
		t.Fatalf("unexpected pagination: %v", items)
	}
	hasMore := res["has_more"].(bool)
	if !hasMore {
		t.Fatal("expected has_more true")
	}
}

func TestSuccessFilter(t *testing.T) {
	dir := t.TempDir()
	l, _ := New(dir)
	defer l.Close()
	l.Log(map[string]any{"ts": 1, "success": true})
	l.Log(map[string]any{"ts": 2, "success": false})
	l.Flush()

	ok := true
	res, _ := l.Read(50, 0, &ok)
	items := res["items"].([]map[string]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 success item, got %d", len(items))
	}
}

// TestRotationDeletesOldestAndKeepsNewerReadable verifies that after rotating
// past backupCount segments, the oldest is gone but everything else remains
// fully readable — this is the "log rolling" property: because each segment's
// first chunk is always a checkpoint (no cross-segment dictionary
// dependency), deleting an old segment can never break a newer one.
func TestRotationDeletesOldestAndKeepsNewerReadable(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.maxBytes = 1 // force rotation after every single chunk write
	l.backupCount = 2

	// Each batch of batchMaxRecords triggers one immediate chunk write
	// (no need to wait for the 2s timer).
	total := 0
	for batch := 0; batch < 5; batch++ {
		for i := 0; i < batchMaxRecords; i++ {
			total++
			l.Log(map[string]any{"ts": float64(total), "n": total})
		}
	}
	l.Flush()

	res, err := l.Read(1000, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	items := res["items"].([]map[string]any)

	// Only the last backupCount+1 segments' worth of records should survive
	// (active + 2 backups); the oldest batch(es) must have been rotated away.
	if len(items) >= total {
		t.Fatalf("expected some old records to be rotated away, got all %d of %d", len(items), total)
	}
	if len(items) == 0 {
		t.Fatal("expected at least the newest records to survive rotation")
	}
	// The newest record must always be present and first (newest-first order).
	newest := items[0]["n"].(float64)
	if newest != float64(total) {
		t.Fatalf("expected newest record n=%d first, got %v", total, newest)
	}

	// Only backupCount rotated segments (plus the active one) should exist on disk.
	for i := l.backupCount + 1; i <= l.backupCount+3; i++ {
		p := dir + "/" + segmentFilename + "." + strconv.Itoa(i)
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("segment %s should have been deleted by rotation", p)
		}
	}
}

// TestCheckpointBoundaryReadsCorrectly verifies records remain correctly
// readable across a checkpointInterval boundary (i.e. reading a chunk that
// requires walking back through several chained chunks to the nearest
// checkpoint still reconstructs the right content).
func TestCheckpointBoundaryReadsCorrectly(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	// Write more than 2*checkpointInterval chunks worth of records so we
	// cross at least one checkpoint boundary within a single segment.
	total := 0
	numChunks := checkpointInterval*2 + 3
	for c := 0; c < numChunks; c++ {
		for i := 0; i < batchMaxRecords; i++ {
			total++
			l.Log(map[string]any{"ts": float64(total), "n": total})
		}
	}
	l.Flush()

	res, err := l.Read(total+10, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	items := res["items"].([]map[string]any)
	if len(items) != total {
		t.Fatalf("expected %d records, got %d", total, len(items))
	}
	// newest-first: items[0] should be the last one logged.
	if items[0]["n"].(float64) != float64(total) {
		t.Fatalf("expected newest-first n=%d, got %v", total, items[0]["n"])
	}
	if items[len(items)-1]["n"].(float64) != 1 {
		t.Fatalf("expected oldest record n=1 at the end, got %v", items[len(items)-1]["n"])
	}
	// Spot-check a record whose chunk requires walking back across the
	// checkpoint boundary to decode.
	one, err := l.ReadOne(float64(checkpointInterval*batchMaxRecords + 1))
	if err != nil {
		t.Fatal(err)
	}
	if one == nil {
		t.Fatal("expected to find record straddling the checkpoint boundary")
	}
}

// TestReadsLegacyPlainJSONLFormat verifies that pre-existing plain-JSONL log
// files (the format used before chunked/zstd storage was introduced) are
// still readable after upgrading, even though new writes never use that
// format anymore.
func TestReadsLegacyPlainJSONLFormat(t *testing.T) {
	dir := t.TempDir()
	legacy := dir + "/" + legacySegmentFilename
	content := `{"ts":1,"combo":"legacy-old"}` + "\n" + `{"ts":2,"combo":"legacy-new"}` + "\n"
	if err := os.WriteFile(legacy, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	res, err := l.Read(50, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	items := res["items"].([]map[string]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 legacy items, got %d: %v", len(items), items)
	}
	if items[0]["combo"] != "legacy-new" || items[1]["combo"] != "legacy-old" {
		t.Fatalf("expected legacy records newest-first, got %v", items)
	}
}

// TestNewWritesGoToChunkedFormatNotLegacy verifies that after upgrading (with
// pre-existing legacy files present), NEW records are appended to the
// chunked format, and both old and new records are visible together.
func TestNewWritesGoToChunkedFormatNotLegacy(t *testing.T) {
	dir := t.TempDir()
	legacy := dir + "/" + legacySegmentFilename
	if err := os.WriteFile(legacy, []byte(`{"ts":1,"combo":"legacy"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Log(map[string]any{"ts": 2.0, "combo": "new-chunked"})
	l.Flush()

	// New data must be in the chunked segment, not appended to the legacy file.
	legacyContent, _ := os.ReadFile(legacy)
	if strings.Contains(string(legacyContent), "new-chunked") {
		t.Fatal("new record leaked into the legacy plain-JSONL file")
	}
	metas, err := scanChunkIndex(l.active)
	if err != nil || len(metas) == 0 {
		t.Fatalf("expected new record to be written as a chunk in the active segment, metas=%v err=%v", metas, err)
	}

	res, _ := l.Read(50, 0, nil)
	items := res["items"].([]map[string]any)
	if len(items) != 2 {
		t.Fatalf("expected both legacy and new records visible, got %d: %v", len(items), items)
	}
}

// TestReadsLegacyGzipArchives verifies old rotated gzip archives
// (requests.jsonl.N.gz) from before the chunked format are still readable.
func TestReadsLegacyGzipArchives(t *testing.T) {
	dir := t.TempDir()
	var buf strings.Builder
	buf.WriteString(`{"ts":1,"combo":"archived"}` + "\n")
	gzPath := dir + "/" + legacySegmentFilename + ".1.gz"
	if err := writeGzipFile(gzPath, buf.String()); err != nil {
		t.Fatal(err)
	}

	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Log(map[string]any{"ts": 2.0, "combo": "current"})
	l.Flush()

	res, _ := l.Read(50, 0, nil)
	items := res["items"].([]map[string]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 items (1 chunked + 1 legacy gz), got %d: %v", len(items), items)
	}
	if items[0]["combo"] != "current" || items[1]["combo"] != "archived" {
		t.Fatalf("expected newest-first order across formats, got %v", items)
	}
}

// TestNewToleratesCrashMidChunkWrite verifies that if the process crashed
// while a chunk was only partially written to the active segment, reopening
// the Logger doesn't error out, and all previously-complete chunks are still
// readable (only the truncated trailing chunk is dropped).
func TestNewToleratesCrashMidChunkWrite(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	l.Log(map[string]any{"ts": 1.0, "combo": "safe"})
	l.Flush()
	l.Close()

	// Simulate a crash mid-write of a second chunk: append some garbage bytes
	// that look like the start of a chunk header but are incomplete.
	f, err := os.OpenFile(l.active, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte{0x31, 0x43, 0x52, 0x5A, 0x00, 0x00}) // partial header, magic bytes reversed on purpose to be "present but incomplete"
	f.Close()

	l2, err := New(dir)
	if err != nil {
		t.Fatalf("New() should tolerate a truncated trailing chunk, got err: %v", err)
	}
	defer l2.Close()

	res, err := l2.Read(50, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	items := res["items"].([]map[string]any)
	if len(items) != 1 || items[0]["combo"] != "safe" {
		t.Fatalf("expected the one complete record to survive, got %v", items)
	}
}
