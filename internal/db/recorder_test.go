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

func TestMcpTokensCRUD(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "mcp-test.db")
	rec, err := NewRecorder(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	// 1. Initial get should be nil
	tok, err := rec.GetMcpToken("github", "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != nil {
		t.Fatalf("expected nil token, got %#v", tok)
	}

	// 2. Save token
	exp := int64(1234567890)
	tok = &McpUserToken{
		Provider:     "github",
		UserID:       "alice",
		AccessToken:  "gho_test123",
		RefreshToken: "ghr_test456",
		TokenType:    "Bearer",
		Scopes:       "repo,read:user",
		ExpiresAt:    &exp,
	}
	if err := rec.SaveMcpToken(tok); err != nil {
		t.Fatalf("save token error: %v", err)
	}
	if tok.ID == 0 {
		t.Fatalf("expected non-zero ID after save")
	}

	// 3. Retrieve token
	gotTok, err := rec.GetMcpToken("github", "alice")
	if err != nil {
		t.Fatalf("get token error: %v", err)
	}
	if gotTok == nil || gotTok.AccessToken != "gho_test123" || *gotTok.ExpiresAt != exp {
		t.Fatalf("unexpected token retrieved: %#v", gotTok)
	}

	// 4. Update token (upsert)
	gotTok.AccessToken = "gho_updated"
	if err := rec.SaveMcpToken(gotTok); err != nil {
		t.Fatalf("upsert token error: %v", err)
	}
	gotTok2, err := rec.GetMcpToken("github", "alice")
	if err != nil || gotTok2.AccessToken != "gho_updated" {
		t.Fatalf("expected updated token, got %#v, err: %v", gotTok2, err)
	}

	// 5. Add a second user
	tokBob := &McpUserToken{
		Provider:    "github",
		UserID:      "bob",
		AccessToken: "gho_bob",
	}
	if err := rec.SaveMcpToken(tokBob); err != nil {
		t.Fatalf("save bob token error: %v", err)
	}

	// 6. List tokens
	all, err := rec.ListMcpTokens()
	if err != nil {
		t.Fatalf("list tokens error: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(all))
	}

	// 7. Delete token
	if err := rec.DeleteMcpToken("github", "alice"); err != nil {
		t.Fatalf("delete token error: %v", err)
	}
	afterDel, err := rec.GetMcpToken("github", "alice")
	if err != nil || afterDel != nil {
		t.Fatalf("expected nil after delete, got %#v, err: %v", afterDel, err)
	}
}

func TestMcpRecordAndQueryList(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sense-roll.db")
	rec, err := NewRecorder(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	rec.RecordMcp(&McpRow{
		TS:         100.0,
		Combo:      "dev-tools",
		Transport:  "http",
		Method:     "tools/call",
		ToolName:   strp("exa__search"),
		Provider:   strp("exa"),
		UserID:     strp("alice"),
		Arguments:  strp(`{"q":"golang"}`),
		Result:     strp(`{"hits":[]}`),
		DurationMs: intp(25),
		StatusCode: intp(200),
		Success:    1,
	})
	rec.RecordMcp(&McpRow{
		TS:         101.0,
		Combo:      "dev-tools",
		Transport:  "http",
		Method:     "tools/call",
		ToolName:   strp("gh__issue"),
		Provider:   strp("github"),
		UserID:     strp("bob"),
		DurationMs: intp(40),
		StatusCode: intp(401),
		Success:    0,
		Error:      strp("auth required"),
	})
	rec.Flush()

	// 1. Query all
	res, err := QueryMcpList(dbPath, 10, 0, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res["total"].(int) != 2 {
		t.Fatalf("expected total 2, got %v", res["total"])
	}
	items := res["items"].([]McpRequestItem)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	// 2. Filter by user_id
	userBob := "bob"
	resBob, err := QueryMcpList(dbPath, 10, 0, nil, nil, nil, &userBob, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resBob["total"].(int) != 1 {
		t.Fatalf("expected 1 record for bob, got %v", resBob["total"])
	}

	// 3. Filter by success
	failOnly := false
	resFail, err := QueryMcpList(dbPath, 10, 0, nil, nil, nil, nil, nil, nil, nil, &failOnly, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resFail["total"].(int) != 1 {
		t.Fatalf("expected 1 failed record, got %v", resFail["total"])
	}

	// 4. Test QueryMcpOverviewStats
	overview, err := QueryMcpOverviewStats(dbPath, nil, nil)
	if err != nil {
		t.Fatalf("overview stats error: %v", err)
	}
	if overview["total_calls"].(float64) != 2 {
		t.Fatalf("expected total_calls 2, got %v", overview["total_calls"])
	}
	if overview["success_calls"].(float64) != 1 {
		t.Fatalf("expected success_calls 1, got %v", overview["success_calls"])
	}

	// 5. Test QueryMcpStats
	stats, err := QueryMcpStats(dbPath, "combo", nil, nil)
	if err != nil {
		t.Fatalf("stats query error: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 combo group, got %d", len(stats))
	}
	if stats[0]["group_key"] != "dev-tools" {
		t.Fatalf("expected combo 'dev-tools', got %v", stats[0]["group_key"])
	}

	// 6. Test QueryMcpTrend
	trend, err := QueryMcpTrend(dbPath, "hour", nil, nil)
	if err != nil {
		t.Fatalf("trend query error: %v", err)
	}
	if len(trend) == 0 {
		t.Fatalf("expected trend data, got 0 rows")
	}
}
