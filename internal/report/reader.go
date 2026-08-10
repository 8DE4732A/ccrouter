package report

import (
	"compress/gzip"
	"encoding/json"
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

	for rec := range l.iterAllReversed() {
		if success != nil && (rec["success"] != nil && boolFrom(rec["success"]) != *success) {
			continue
		}
		if skipped < offset {
			skipped++
			continue
		}
		collected = append(collected, stripDetail(rec))
		if len(collected) >= need {
			break
		}
	}
	hasMore := len(collected) > limit
	return map[string]any{"items": collected[:min(limit, len(collected))], "has_more": hasMore}, nil
}

// ReadOne returns the single full record whose ts matches, or nil if not found.
func (l *Logger) ReadOne(ts float64) (map[string]any, error) {
	for rec := range l.iterAllReversed() {
		if tsVal, ok := rec["ts"].(float64); ok && tsVal == ts {
			return rec, nil
		}
	}
	return nil, nil
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

// iterAllReversed yields records newest-to-oldest across:
//  1. the active chunked segment + its rotated backups (requests.zrc,
//     requests.zrc.1 .. N), and then
//  2. any legacy plain-JSONL segments left over from before the chunked
//     format was introduced (requests.jsonl, requests.jsonl.1.gz .. N.gz).
//
// New segments are exhausted before falling back to legacy ones, which is
// correct because legacy files (if any) are always strictly older than every
// chunked segment (they stopped being written to the moment ccrouter
// upgraded to this format).
func (l *Logger) iterAllReversed() <-chan map[string]any {
	ch := make(chan map[string]any)
	go func() {
		defer close(ch)
		if _, err := os.Stat(l.active); err == nil {
			for rec := range readSegmentReversed(l.active) {
				ch <- rec
			}
		}
		for i := 1; i <= l.backupCount; i++ {
			seg := filepath.Join(l.logDir, segmentFilename+"."+strconv.Itoa(i))
			if _, err := os.Stat(seg); err != nil {
				break
			}
			for rec := range readSegmentReversed(seg) {
				ch <- rec
			}
		}
		// Legacy plain-JSONL format, read-only, for backward compatibility.
		legacyActive := filepath.Join(l.logDir, legacySegmentFilename)
		if _, err := os.Stat(legacyActive); err == nil {
			for rec := range readLegacyPlainReversed(legacyActive) {
				ch <- rec
			}
		}
		for i := 1; i <= l.backupCount; i++ {
			archive := filepath.Join(l.logDir, legacySegmentFilename+"."+strconv.Itoa(i)+".gz")
			if _, err := os.Stat(archive); err != nil {
				break
			}
			for rec := range readLegacyGzipReversed(archive) {
				ch <- rec
			}
		}
	}()
	return ch
}

// readSegmentReversed yields all records in one chunked segment file,
// newest-to-oldest. It scans the (cheap) chunk header index once, then walks
// checkpoint groups from last to first; within a group, the shared
// dictionary-chain prefix is decoded exactly once (via decodeCheckpointGroup)
// and every chunk's lines are then emitted in reverse order — so scanning an
// entire large segment backward is still only O(chunks), not O(chunks *
// checkpointInterval).
func readSegmentReversed(path string) <-chan map[string]any {
	ch := make(chan map[string]any)
	go func() {
		defer close(ch)
		metas, err := scanChunkIndex(path)
		if err != nil || len(metas) == 0 {
			return
		}
		f, err := os.Open(path)
		if err != nil {
			return
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
				return
			}
			// group[k] is the decoded content of chunk (groupStart+k).
			for k := len(group) - 1; k >= 0; k-- {
				var lines []string
				emitLines(group[k], func(line string) { lines = append(lines, line) })
				for i := len(lines) - 1; i >= 0; i-- {
					var rec map[string]any
					if err := json.Unmarshal([]byte(lines[i]), &rec); err == nil {
						ch <- rec
					}
				}
			}
			idx = groupStart - 1
		}
	}()
	return ch
}

func readLegacyPlainReversed(path string) <-chan map[string]any {
	ch := make(chan map[string]any)
	go func() {
		defer close(ch)
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		emitReversedLines(ch, data)
	}()
	return ch
}

func readLegacyGzipReversed(path string) <-chan map[string]any {
	ch := make(chan map[string]any)
	go func() {
		defer close(ch)
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		gr, err := gzip.NewReader(f)
		if err != nil {
			return
		}
		defer gr.Close()
		var data []byte
		buf := make([]byte, 1<<16)
		for {
			n, err := gr.Read(buf)
			data = append(data, buf[:n]...)
			if err != nil {
				break
			}
		}
		emitReversedLines(ch, data)
	}()
	return ch
}

func emitReversedLines(ch chan<- map[string]any, data []byte) {
	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err == nil {
			ch <- rec
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
