package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	defaultMaxBytes  = 20 * 1024 * 1024 // 20 MB per segment before rotation
	defaultBackupCnt = 10
	segmentFilename  = "requests.log" // new chunked/zstd format

	// batchMaxRecords / batchMaxWait bound how long a record can sit in
	// memory before being flushed to disk as part of a chunk. Batching is
	// what lets consecutive records share a dictionary (see format.go); the
	// trade-off is that a crash can lose up to one batch's worth of records
	// that were queued but not yet flushed. Capping the wait at a couple of
	// seconds keeps that exposure small.
	batchMaxRecords = 10
	batchMaxWait    = 2 * time.Second
)

// Logger writes detailed request records as JSONL, compressed with chained
// zstd dictionaries and rotated by size. Records include full upstream
// headers, which may contain plaintext API keys — the underlying files
// should be treated the same as secrets.
//
// See format.go for the on-disk chunk format, the dictionary-chaining
// strategy, and the reasoning behind the chosen compression level.
type Logger struct {
	logDir      string
	maxBytes    int64
	backupCount int
	active      string

	fh                    *os.File
	fileSize              int64  // current size of the active segment file
	prevRaw               []byte // raw content of the last chunk written to the active segment, for dict chaining
	chunksSinceCheckpoint int

	records chan map[string]any
	dropped int
	mu      sync.Mutex
	done    chan struct{}
	wg      sync.WaitGroup
	queued  int
	written int
}

// New creates a Logger writing to logDir/requests.log.
func New(logDir string) (*Logger, error) {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, err
	}
	active := filepath.Join(logDir, segmentFilename)
	fh, err := os.OpenFile(active, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	st, err := fh.Stat()
	if err != nil {
		fh.Close()
		return nil, err
	}
	l := &Logger{
		logDir:      logDir,
		maxBytes:    defaultMaxBytes,
		backupCount: defaultBackupCnt,
		active:      active,
		fh:          fh,
		fileSize:    st.Size(),
		records:     make(chan map[string]any, 10000),
		done:        make(chan struct{}),
	}
	l.wg.Add(1)
	go l.run()
	return l, nil
}

// run is the background writer goroutine. It batches records into chunks of
// up to batchMaxRecords, flushing early if batchMaxWait elapses since the
// first record in the current batch arrived — so a chunk is written promptly
// even during quiet periods, bounding the data-loss window on crash.
func (l *Logger) run() {
	defer l.wg.Done()
	batch := make([]map[string]any, 0, batchMaxRecords)
	timer := time.NewTimer(batchMaxWait)
	timer.Stop()
	timerActive := false

	flush := func() {
		if len(batch) == 0 {
			return
		}
		l.writeBatch(batch)
		batch = batch[:0]
		if timerActive {
			timer.Stop()
			timerActive = false
		}
	}

	for {
		select {
		case rec := <-l.records:
			batch = append(batch, rec)
			if !timerActive {
				timer.Reset(batchMaxWait)
				timerActive = true
			}
			if len(batch) >= batchMaxRecords {
				flush()
			}
		case <-timer.C:
			timerActive = false
			flush()
		case <-l.done:
			// Drain whatever is still queued, then do a final flush.
			for {
				select {
				case rec := <-l.records:
					batch = append(batch, rec)
					if len(batch) >= batchMaxRecords {
						flush()
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

// writeBatch compresses and appends one chunk containing all of batch's
// records, then rotates the segment if it has grown past maxBytes.
func (l *Logger) writeBatch(batch []map[string]any) {
	var raw []byte
	firstTS, lastTS := 0.0, 0.0
	n := 0
	for _, rec := range batch {
		data, err := json.Marshal(rec)
		if err != nil {
			continue
		}
		raw = append(raw, data...)
		raw = append(raw, '\n')
		ts := tsOf(rec)
		if n == 0 {
			firstTS = ts
		}
		lastTS = ts
		n++
	}

	if n == 0 {
		l.mu.Lock()
		l.written += len(batch)
		l.mu.Unlock()
		return
	}

	checkpoint := l.prevRaw == nil || l.chunksSinceCheckpoint >= checkpointInterval
	chunk, err := encodeChunk(raw, n, firstTS, lastTS, l.prevRaw, checkpoint)
	if err != nil {
		l.mu.Lock()
		l.written += len(batch)
		l.mu.Unlock()
		return
	}
	if _, err := l.fh.Write(chunk); err != nil {
		l.mu.Lock()
		l.written += len(batch)
		l.mu.Unlock()
		return
	}
	_ = l.fh.Sync()

	l.fileSize += int64(len(chunk))
	l.prevRaw = raw
	if checkpoint {
		l.chunksSinceCheckpoint = 1
	} else {
		l.chunksSinceCheckpoint++
	}

	// Mark records as durably written only after the chunk has been fully
	// flushed to disk, so Flush() callers can rely on Read()/ReadOne() seeing
	// them immediately afterward.
	l.mu.Lock()
	l.written += len(batch)
	l.mu.Unlock()

	if l.fileSize >= l.maxBytes {
		l.rotate()
	}
}

func tsOf(rec map[string]any) float64 {
	if v, ok := rec["ts"].(float64); ok {
		return v
	}
	return 0
}

// rotate closes the active segment, shifts requests.log.1..N up by one
// (dropping the oldest beyond backupCount), and starts a fresh active
// segment. Because every segment's first chunk is a checkpoint (no
// dictionary dependency on any other segment), this is a plain rename/delete
// dance — no re-compression of old data is needed.
func (l *Logger) rotate() {
	_ = l.fh.Close()

	oldest := filepath.Join(l.logDir, segmentFilename+"."+strconv.Itoa(l.backupCount))
	_ = os.Remove(oldest)

	for i := l.backupCount - 1; i >= 1; i-- {
		src := filepath.Join(l.logDir, segmentFilename+"."+strconv.Itoa(i))
		dst := filepath.Join(l.logDir, segmentFilename+"."+strconv.Itoa(i+1))
		if _, err := os.Stat(src); err == nil {
			_ = os.Rename(src, dst)
		}
	}

	archive := filepath.Join(l.logDir, segmentFilename+".1")
	_ = os.Rename(l.active, archive)

	fh, err := os.OpenFile(l.active, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		fh, _ = os.OpenFile(l.active, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	}
	l.fh = fh
	l.fileSize = 0
	l.prevRaw = nil
	l.chunksSinceCheckpoint = 0
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

// Close flushes the queue (forcing a final partial-batch write) and closes
// the active file.
func (l *Logger) Close() {
	close(l.done)
	l.wg.Wait()
	_ = l.fh.Close()
}
