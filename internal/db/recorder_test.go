package db

import (
	"path/filepath"
	"testing"
)

func strp(s string) *string { return &s }
func intp(i int) *int       { return &i }

func TestRecordAndQueryList(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sense-roll.db")
	rec, err := NewRecorder(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	rec.Record(&Row{
		TS: 100.0, Combo: strp("fast"), Provider: strp("sn"), Model: strp("m"),
		KeyPrefix: strp("sk-"), APIFormat: strp("openai"), IsStream: 0,
		Success: 1, TotalTokens: intp(10), DurationMs: intp(5),
	})
	// Wait for background writer
	rec.Flush()

	res, err := QueryList(dbPath, 50, 0, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	items := res["items"].([]map[string]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if res["total"].(float64) != 1 {
		t.Fatalf("expected total 1, got %v", res["total"])
	}
}

func TestQueryStatsGroupedByCombo(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sense-roll.db")
	rec, err := NewRecorder(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()
	rec.Record(&Row{TS: 1, Combo: strp("fast"), Success: 1, TotalTokens: intp(10)})
	rec.Record(&Row{TS: 2, Combo: strp("fast"), Success: 0})
	rec.Flush()

	rows, err := QueryStats(dbPath, "combo", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if row["group_key"] != "fast" {
		t.Fatalf("expected group fast, got %v", row["group_key"])
	}
	if row["total"].(float64) != 2 {
		t.Fatalf("expected total 2, got %v", row["total"])
	}
	if row["total_tokens"].(float64) != 10 {
		t.Fatalf("expected total_tokens 10, got %v", row["total_tokens"])
	}
}

func TestQueryTrend(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sense-roll.db")
	rec, _ := NewRecorder(dbPath)
	defer rec.Close()
	rec.Record(&Row{TS: 1000, Combo: strp("fast"), Success: 1, TotalTokens: intp(5)})
	rec.Record(&Row{TS: 1010, Combo: strp("fast"), Success: 1, TotalTokens: intp(7)})
	rec.Flush()

	rows, err := QueryTrend(dbPath, "minute", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(rows))
	}
	if rows[0]["total"].(float64) != 2 {
		t.Fatalf("expected total 2 in bucket, got %v", rows[0]["total"])
	}
}

func TestQueryListFilters(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sense-roll.db")
	rec, _ := NewRecorder(dbPath)
	defer rec.Close()
	rec.Record(&Row{TS: 1, Combo: strp("fast"), Success: 1})
	rec.Record(&Row{TS: 2, Combo: strp("slow"), Success: 0})
	rec.Flush()

	combo := "fast"
	res, err := QueryList(dbPath, 50, 0, &combo, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res["total"].(float64) != 1 {
		t.Fatalf("expected 1 filtered, got %v", res["total"])
	}
}

func TestPathForConfig(t *testing.T) {
	got := PathForConfig("/a/b/config.yaml")
	want := filepath.Join("/a/b", "sense-roll.db")
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}
