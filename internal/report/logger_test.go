package report

import (
	"testing"
)

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
