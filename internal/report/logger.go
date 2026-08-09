package report

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultMaxBytes  = 20 * 1024 * 1024 // 20 MB
	defaultBackupCnt = 10
	logFilename      = "requests.jsonl"
)

// Logger writes detailed request records as JSONL with size-based rotation.
// Records include full upstream headers which may contain plaintext API keys.
type Logger struct {
	logDir      string
	maxBytes    int64
	backupCount int
	active      string
	fh          *os.File
	records     chan map[string]any
	dropped     int
	mu          sync.Mutex
	done        chan struct{}
	wg          sync.WaitGroup
	queued      int
	written     int
}

// New creates a Logger writing to logDir/requests.jsonl.
func New(logDir string) (*Logger, error) {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, err
	}
	active := filepath.Join(logDir, logFilename)
	fh, err := os.OpenFile(active, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	l := &Logger{
		logDir:      logDir,
		maxBytes:    defaultMaxBytes,
		backupCount: defaultBackupCnt,
		active:      active,
		fh:          fh,
		records:     make(chan map[string]any, 10000),
		done:        make(chan struct{}),
	}
	l.wg.Add(1)
	go l.run()
	return l, nil
}

func (l *Logger) run() {
	defer l.wg.Done()
	for {
		select {
		case rec := <-l.records:
			l.write(rec)
		case <-l.done:
			for {
				select {
				case rec := <-l.records:
					l.write(rec)
				default:
					return
				}
			}
		}
	}
}

func (l *Logger) write(rec map[string]any) {
	data, err := json.Marshal(rec)
	if err != nil {
		l.mu.Lock()
		l.written++
		l.mu.Unlock()
		return
	}
	line := append(data, '\n')
	if _, err := l.fh.Write(line); err != nil {
		l.mu.Lock()
		l.written++
		l.mu.Unlock()
		return
	}
	_ = l.fh.Sync()
	l.mu.Lock()
	l.written++
	l.mu.Unlock()
	st, err := os.Stat(l.active)
	if err == nil && st.Size() >= l.maxBytes {
		l.rotate()
	}
}

func (l *Logger) rotate() {
	// close active file
	_ = l.fh.Close()

	oldest := filepath.Join(l.logDir, logFilename+"."+strconv.Itoa(l.backupCount)+".gz")
	_ = os.Remove(oldest)

	for i := l.backupCount - 1; i >= 1; i-- {
		src := filepath.Join(l.logDir, logFilename+"."+strconv.Itoa(i)+".gz")
		dst := filepath.Join(l.logDir, logFilename+"."+strconv.Itoa(i+1)+".gz")
		if _, err := os.Stat(src); err == nil {
			_ = os.Rename(src, dst)
		}
	}

	archive := filepath.Join(l.logDir, logFilename+".1.gz")
	if fin, err := os.Open(l.active); err == nil {
		if fout, gerr := os.Create(archive); gerr == nil {
			gz := gzip.NewWriter(fout)
			_, _ = bufio.NewReader(fin).WriteTo(gz)
			_ = gz.Close()
			_ = fout.Close()
		}
		_ = fin.Close()
	}

	fh, err := os.OpenFile(l.active, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		// recover by reopening append
		fh, _ = os.OpenFile(l.active, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	}
	l.fh = fh
}

// Log enqueues a record for writing (non-blocking).
func (l *Logger) Log(rec map[string]any) {
	select {
	case l.records <- rec:
		l.mu.Lock()
		l.queued++
		l.mu.Unlock()
	default:
		l.mu.Lock()
		l.dropped++
		l.mu.Unlock()
	}
}

// QueueLen returns the current number of buffered records.
func (l *Logger) QueueLen() int { return len(l.records) }

// Flush waits until all enqueued records have been written.
func (l *Logger) Flush() {
	for i := 0; i < 10000; i++ {
		l.mu.Lock()
		done := l.written >= l.queued
		l.mu.Unlock()
		if done {
			return
		}
		time.Sleep(1 * time.Millisecond)
	}
}

// DroppedCount returns the number of dropped records.
func (l *Logger) DroppedCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dropped
}

// Read returns up to limit metadata-only records starting at offset, newest first.
// The request/response detail fields are stripped — use ReadOne to fetch a single full record.
func (l *Logger) Read(limit, offset int, success *bool) (map[string]any, error) {
	need := limit + 1
	collected := []map[string]any{}
	skipped := 0

	for rec := range l.iterFiles() {
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
	for rec := range l.iterFiles() {
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

// iterFiles yields records newest-to-oldest across active + rotating archives.
func (l *Logger) iterFiles() <-chan map[string]any {
	ch := make(chan map[string]any)
	go func() {
		defer close(ch)
		if _, err := os.Stat(l.active); err == nil {
			for rec := range readFileReversed(l.active) {
				ch <- rec
			}
		}
		for i := 1; i <= l.backupCount; i++ {
			archive := filepath.Join(l.logDir, logFilename+"."+strconv.Itoa(i)+".gz")
			if _, err := os.Stat(archive); err != nil {
				break
			}
			for rec := range readGzipReversed(archive) {
				ch <- rec
			}
		}
	}()
	return ch
}

func readFileReversed(path string) <-chan map[string]any {
	ch := make(chan map[string]any)
	go func() {
		defer close(ch)
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
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
	}()
	return ch
}

func readGzipReversed(path string) <-chan map[string]any {
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
	}()
	return ch
}

// Close flushes the queue and closes the file.
func (l *Logger) Close() {
	close(l.done)
	l.wg.Wait()
	_ = l.fh.Close()
}
