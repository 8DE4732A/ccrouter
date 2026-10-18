package report

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// legacySegmentFilename is the old (pre-chunked) log format: a plain JSONL
// active file plus gzip-compressed rotated backups
// (requests.jsonl.1.gz .. requests.jsonl.N.gz). New writes never use this
// format, but any pre-existing files are still readable so upgrading
// ccrouter doesn't lose log history.
const legacySegmentFilename = "requests.jsonl"

// Read returns up to limit metadata-only records starting at offset, newest
// first. The request/response detail fields are stripped — use ReadOne to
// fetch a single full record.
func (l *Logger) Read(limit, offset int, success *bool) (map[string]any, error) {
	need := limit + 1
	collected := []map[string]any{}
	skipped := 0

	l.iterAllReversed(func(rec map[string]any) bool {
		if success != nil && rec["success"] != nil && boolFrom(rec["success"]) != *success {
			return true
		}
		if skipped < offset {
			skipped++
			return true
		}
		collected = append(collected, stripDetail(rec))
		return len(collected) < need
	})
	hasMore := len(collected) > limit
	return map[string]any{"items": collected[:min(limit, len(collected))], "has_more": hasMore}, nil
}

// ReadOne returns the single full record whose ts matches, or nil if not found.
func (l *Logger) ReadOne(ts float64) (map[string]any, error) {
	var found map[string]any
	l.iterAllReversed(func(rec map[string]any) bool {
		if tsVal, ok := rec["ts"].(float64); ok && tsVal == ts {
			found = rec
			return false
		}
		return true
	})
	return found, nil
}

// stripDetail removes the large request/response body fields for list views.
func stripDetail(rec map[string]any) map[string]any {
	out := make(map[string]any, len(rec))
	for k, v := range rec {
		if k == "request" || k == "response" {
			continue
		}
		out[k] = v
	}
	return out
}

func boolFrom(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	}
	return false
}

// iterAllReversed calls yield for each record, newest-to-oldest, across:
//  1. the active chunked segment + its rotated backups (requests.zrc,
//     requests.zrc.1 .. N), and then
//  2. any legacy plain-JSONL segments left over from before the chunked
//     format was introduced (requests.jsonl, requests.jsonl.1.gz .. N.gz).
//
// New segments are exhausted before falling back to legacy ones, which is
// correct because legacy files (if any) are always strictly older than every
// chunked segment (they stopped being written to the moment ccrouter
// upgraded to this format).
//
// Iteration is synchronous: yield returning false stops it immediately and
// unwinds the active reader, so its file is closed and its decoded buffers
// become garbage. Callers that only need the newest few records can stop
// early without leaking anything.
func (l *Logger) iterAllReversed(yield func(map[string]any) bool) {
	l.writeMu.Lock()
	active := l.active
	logDir := l.logDir
	backupCount := l.backupCount
	l.writeMu.Unlock()

	var chunked []string
	if _, err := os.Stat(active); err == nil {
		chunked = append(chunked, active)
	}
	for i := 1; i <= backupCount; i++ {
		seg := filepath.Join(logDir, segmentFilename+"."+strconv.Itoa(i))
		if _, err := os.Stat(seg); err != nil {
			break
		}
		chunked = append(chunked, seg)
	}
	for _, path := range chunked {
		if !readSegmentReversed(path, yield) {
			return
		}
	}

	var legacy []string
	legacyActive := filepath.Join(logDir, legacySegmentFilename)
	if _, err := os.Stat(legacyActive); err == nil {
		legacy = append(legacy, legacyActive)
	}
	for i := 1; i <= backupCount; i++ {
		archive := filepath.Join(logDir, legacySegmentFilename+"."+strconv.Itoa(i)+".gz")
		if _, err := os.Stat(archive); err != nil {
			break
		}
		legacy = append(legacy, archive)
	}
	for _, path := range legacy {
		if !readLegacyFileReversed(path, yield) {
			return
		}
	}
}

// readSegmentReversed calls yield for all records in one chunked segment
// file, newest-to-oldest, and reports whether iteration completed (true) or
// the consumer stopped early (false). It scans the (cheap) chunk header index
// once, then walks checkpoint groups from last to first; within a group, the
// shared dictionary-chain prefix is decoded exactly once (via
// decodeCheckpointGroup) and every chunk's lines are then emitted in reverse
// order — so scanning an entire large segment backward is still only
// O(chunks), not O(chunks * checkpointInterval).
func readSegmentReversed(path string, yield func(map[string]any) bool) bool {
	metas, err := scanChunkIndex(path)
	if err != nil || len(metas) == 0 {
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()

	idx := len(metas) - 1
	for idx >= 0 {
		// Find the checkpoint group containing idx: [groupStart, idx].
		groupStart := idx
		for groupStart > 0 && !metas[groupStart].checkpoint {
			groupStart--
		}
		group, err := decodeCheckpointGroup(f, metas, idx)
		if err != nil {
			return true
		}
		// group[k] is the decoded content of chunk (groupStart+k). Emit
		// newest-first, releasing each raw chunk once its lines have been
		// copied out.
		for k := len(group) - 1; k >= 0; k-- {
			var lines []string
			emitLines(group[k], func(line string) { lines = append(lines, line) })
			group[k] = nil
			for i := len(lines) - 1; i >= 0; i-- {
				var rec map[string]any
				if err := json.Unmarshal([]byte(lines[i]), &rec); err == nil {
					if !yield(rec) {
						return false
					}
				}
			}
		}
		idx = groupStart - 1
	}
	return true
}

// readLegacyFileReversed calls yield for all records in a legacy segment,
// newest-to-oldest. Plain JSONL and gzip-compressed (.gz) files are both
// supported; the format is selected by extension. It reports whether
// iteration completed (true) or the consumer stopped early (false).
func readLegacyFileReversed(path string, yield func(map[string]any) bool) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()

	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gr, err := gzip.NewReader(f)
		if err != nil {
			return true
		}
		defer gr.Close()
		r = gr
	}
	// A truncated gzip stream can still yield a usable prefix of records.
	// Emit whatever decompressed successfully instead of dropping the whole
	// segment on a read error.
	data, _ := io.ReadAll(r)
	return emitReversedLines(data, yield)
}

func emitReversedLines(data []byte, yield func(map[string]any) bool) bool {
	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err == nil {
			if !yield(rec) {
				return false
			}
		}
	}
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
