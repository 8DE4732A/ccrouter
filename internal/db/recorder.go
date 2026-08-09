package db

import (
	"database/sql"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

// Row is a request record to be inserted.
type Row struct {
	TS               float64
	Combo            *string
	Provider         *string
	Model            *string
	KeyPrefix        *string
	APIFormat        *string
	IsStream         int
	StatusCode       *int
	Success          int
	MatchedRule      *string
	PromptTokens     *int
	CompletionTokens *int
	TotalTokens      *int
	CacheReadTokens  *int
	CacheWriteTokens *int
	DurationMs       *int
	Error            *string
	MatchedPayload   *string
}

var ddl = `
CREATE TABLE IF NOT EXISTS requests (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  ts               REAL    NOT NULL,
  combo            TEXT,
  provider         TEXT,
  model            TEXT,
  key_prefix       TEXT,
  api_format       TEXT,
  is_stream        INTEGER NOT NULL DEFAULT 0,
  status_code      INTEGER,
  success          INTEGER NOT NULL DEFAULT 0,
  matched_rule     TEXT,
  prompt_tokens    INTEGER,
  completion_tokens INTEGER,
  total_tokens     INTEGER,
  cache_read_tokens  INTEGER,
  cache_write_tokens INTEGER,
  duration_ms      INTEGER,
  error            TEXT,
  matched_payload  TEXT
);
CREATE INDEX IF NOT EXISTS idx_requests_ts       ON requests(ts);
CREATE INDEX IF NOT EXISTS idx_requests_combo    ON requests(combo);
CREATE INDEX IF NOT EXISTS idx_requests_prov_mdl ON requests(provider, model);
`

var migrations = []string{
	"ALTER TABLE requests ADD COLUMN cache_read_tokens  INTEGER",
	"ALTER TABLE requests ADD COLUMN cache_write_tokens INTEGER",
	"ALTER TABLE requests ADD COLUMN matched_payload    TEXT",
}

// Recorder writes request records to SQLite with a background writer goroutine.
type Recorder struct {
	dbPath    string
	writeConn *sql.DB
	records   chan *Row
	dropped   int
	mu        sync.Mutex
	queued    int64
	written   int64
	done      chan struct{}
	wg        sync.WaitGroup
}

// NewRecorder opens (or creates) the SQLite DB at dbPath.
func NewRecorder(dbPath string) (*Recorder, error) {
	conn, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(ddl); err != nil {
		conn.Close()
		return nil, err
	}
	for _, m := range migrations {
		_, _ = conn.Exec(m) // ignore "duplicate column" errors
	}
	r := &Recorder{
		dbPath:    dbPath,
		writeConn: conn,
		records:   make(chan *Row, 10000),
		done:      make(chan struct{}),
	}
	r.wg.Add(1)
	go r.run()
	return r, nil
}

func (r *Recorder) run() {
	defer r.wg.Done()
	// Use a timer to periodically commit pending WAL transactions for
	// efficient batching, and always on drain.
	drain := func() {
		for {
			select {
			case row := <-r.records:
				r.insert(row)
			default:
				return
			}
		}
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case row := <-r.records:
			r.insert(row)
		case <-ticker.C:
			drain()
		case <-r.done:
			drain()
			return
		}
	}
}

func (r *Recorder) insert(row *Row) {
	_, err := r.writeConn.Exec(insertSQL, row.TS, row.Combo, row.Provider, row.Model,
		row.KeyPrefix, row.APIFormat, row.IsStream, row.StatusCode, row.Success, row.MatchedRule,
		row.PromptTokens, row.CompletionTokens, row.TotalTokens, row.CacheReadTokens,
		row.CacheWriteTokens, row.DurationMs, row.Error, row.MatchedPayload)
	if err == nil {
		atomic.AddInt64(&r.written, 1)
	}
}

var insertSQL = `INSERT INTO requests
  (ts, combo, provider, model, key_prefix, api_format, is_stream,
   status_code, success, matched_rule,
   prompt_tokens, completion_tokens, total_tokens,
   cache_read_tokens, cache_write_tokens,
   duration_ms, error, matched_payload)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// Record enqueues a row for writing (non-blocking). Returns false if dropped.
func (r *Recorder) Record(row *Row) bool {
	select {
	case r.records <- row:
		atomic.AddInt64(&r.queued, 1)
		return true
	default:
		r.mu.Lock()
		r.dropped++
		r.mu.Unlock()
		return false
	}
}

// Flush waits until all enqueued records have been written to the DB.
func (r *Recorder) Flush() {
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt64(&r.written) < atomic.LoadInt64(&r.queued) {
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// DroppedCount returns the number of dropped records.
func (r *Recorder) DroppedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dropped
}

// QueueSize returns the current number of buffered records.
func (r *Recorder) QueueSize() int {
	return int(atomic.LoadInt64(&r.queued) - atomic.LoadInt64(&r.written))
}

// DBPath returns the database file path.
func (r *Recorder) DBPath() string { return r.dbPath }

// Close flushes the queue and shuts down the writer.
func (r *Recorder) Close() {
	close(r.done)
	r.wg.Wait()
	r.writeConn.Close()
}

// PathForConfig returns the DB path sibling to the given config path.
func PathForConfig(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "sense-roll.db")
}
