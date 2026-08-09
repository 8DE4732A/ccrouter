package keys

import "testing"

func TestFillFirstAlwaysReturnsFirstAvailable(t *testing.T) {
	km := NewManager("sn", []string{"key-1", "key-2"}, "fill-first")
	if got := km.Get("model-a", map[string]bool{}); got != "key-1" {
		t.Fatalf("expected key-1, got %q", got)
	}
	if got := km.Get("model-a", map[string]bool{}); got != "key-1" {
		t.Fatalf("expected key-1 again, got %q", got)
	}
}

func TestRoundRobinRotatesAcrossRequests(t *testing.T) {
	km := NewManager("sn", []string{"key-1", "key-2"}, "round-robin")
	first := km.Get("model-a", map[string]bool{})
	second := km.Get("model-a", map[string]bool{})
	if first == second {
		t.Fatalf("expected different keys, both %q", first)
	}
}

func TestAttemptedKeysAreSkipped(t *testing.T) {
	km := NewManager("sn", []string{"key-1", "key-2"}, "fill-first")
	if got := km.Get("model-a", map[string]bool{"key-1": true}); got != "key-2" {
		t.Fatalf("expected key-2, got %q", got)
	}
}

func TestRecordErrorPutsKeyInCooldownForModel(t *testing.T) {
	km := NewManager("sn", []string{"key-1", "key-2"}, "fill-first")
	km.RecordError("key-1", "flash", 3600)
	if got := km.Get("flash", map[string]bool{}); got != "key-2" {
		t.Fatalf("expected key-2 (key-1 in cooldown), got %q", got)
	}
}

func TestCooldownIsModelSpecific(t *testing.T) {
	km := NewManager("sn", []string{"key-1"}, "fill-first")
	km.RecordError("key-1", "flash", 3600)
	if got := km.Get("flash", map[string]bool{}); got != "" {
		t.Fatalf("expected no key for flash (cooldown), got %q", got)
	}
	if got := km.Get("other-model", map[string]bool{}); got != "key-1" {
		t.Fatalf("expected key-1 for other-model, got %q", got)
	}
}

func TestReturnsEmptyWhenAllKeysCoolingDown(t *testing.T) {
	km := NewManager("sn", []string{"key-1", "key-2"}, "fill-first")
	km.RecordError("key-1", "flash", 3600)
	km.RecordError("key-2", "flash", 3600)
	if got := km.Get("flash", map[string]bool{}); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestCooldownExpires(t *testing.T) {
	km := NewManager("sn", []string{"key-1"}, "fill-first")
	km.RecordError("key-1", "flash", 0)
	if got := km.Get("flash", map[string]bool{}); got != "key-1" {
		t.Fatalf("expected key-1 (cooldown 0 expires), got %q", got)
	}
}

func TestRecordSuccessIncrementsUseCount(t *testing.T) {
	km := NewManager("sn", []string{"key-1"}, "fill-first")
	km.RecordSuccess("key-1", "flash")
	km.RecordSuccess("key-1", "flash")
	st := km.Stats()
	keysInfo := st["keys"].([]any)
	first := keysInfo[0].(map[string]any)
	if first["use_count"].(int) != 2 {
		t.Fatalf("expected use_count 2, got %v", first["use_count"])
	}
	if first["error_count"].(int) != 0 {
		t.Fatalf("expected error_count 0, got %v", first["error_count"])
	}
}

func TestGetStatsShowsModelCooldown(t *testing.T) {
	km := NewManager("sn", []string{"key-1"}, "fill-first")
	km.RecordError("key-1", "flash", 3600)
	st := km.Stats()
	keysInfo := st["keys"].([]any)
	first := keysInfo[0].(map[string]any)
	cds := first["model_cooldowns"].(map[string]any)
	flash, ok := cds["flash"].(map[string]any)
	if !ok {
		t.Fatal("expected flash cooldown entry")
	}
	if flash["available"].(bool) {
		t.Fatal("expected flash to be unavailable")
	}
	if secs, ok := flash["seconds_remaining"].(float64); !ok || secs < 3500 {
		t.Fatalf("expected seconds_remaining > 3500, got %v", flash["seconds_remaining"])
	}
}

func TestMergeStatsPreservesUnexpiredCooldown(t *testing.T) {
	old := NewManager("sn", []string{"key-1"}, "fill-first")
	old.RecordError("key-1", "flash", 3600)
	new := NewManager("sn", []string{"key-1"}, "fill-first")
	new.MergeStats(old)
	if got := new.Get("flash", map[string]bool{}); got != "" {
		t.Fatalf("expected cooldown preserved, got key %q", got)
	}
}

func TestMergeStatsRemovedKeyIgnored(t *testing.T) {
	old := NewManager("sn", []string{"key-1", "key-gone"}, "fill-first")
	old.RecordSuccess("key-gone", "flash")
	new := NewManager("sn", []string{"key-1"}, "fill-first")
	new.MergeStats(old)
	st := new.Stats()
	keysInfo := st["keys"].([]any)
	if len(keysInfo) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keysInfo))
	}
}
