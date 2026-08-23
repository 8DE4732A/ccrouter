package combos

import (
	"testing"

	"ccrouter/internal/config"
)

func makeCombo(name, strategy string, members [][2]string) *config.ComboConfig {
	c := &config.ComboConfig{
		Name:      name,
		APIFormat: "openai",
		Strategy:  strategy,
	}
	for _, m := range members {
		c.Members = append(c.Members, config.ComboMember{Provider: m[0], Model: m[1]})
	}
	return c
}

func comboSlice(cs ...*config.ComboConfig) []config.ComboConfig {
	out := make([]config.ComboConfig, len(cs))
	for i, c := range cs {
		out[i] = *c
	}
	return out
}

func TestIsCombo(t *testing.T) {
	r := NewRouter(comboSlice(makeCombo("fast", "fill-first", [][2]string{{"sn", "flash"}})))
	if !r.IsCombo("fast") {
		t.Fatal("expected fast to be a combo")
	}
	if r.IsCombo("unknown") {
		t.Fatal("expected unknown not to be combo")
	}
}

func TestFillFirstReturnsFirstMember(t *testing.T) {
	r := NewRouter(comboSlice(makeCombo("fast", "fill-first", [][2]string{{"sn", "flash"}, {"ds", "chat"}})))
	p, m := r.NextMember("fast", map[[2]string]bool{})
	if p != "sn" || m != "flash" {
		t.Fatalf("expected (sn,flash), got (%s,%s)", p, m)
	}
}

func TestFillFirstSkipsAttempted(t *testing.T) {
	r := NewRouter(comboSlice(makeCombo("fast", "fill-first", [][2]string{{"sn", "flash"}, {"ds", "chat"}})))
	p, m := r.NextMember("fast", map[[2]string]bool{{"sn", "flash"}: true})
	if p != "ds" || m != "chat" {
		t.Fatalf("expected (ds,chat), got (%s,%s)", p, m)
	}
}

func TestFillFirstEmptyWhenAllAttempted(t *testing.T) {
	r := NewRouter(comboSlice(makeCombo("fast", "fill-first", [][2]string{{"sn", "flash"}, {"ds", "chat"}})))
	p, _ := r.NextMember("fast", map[[2]string]bool{{"sn", "flash"}: true, {"ds", "chat"}: true})
	if p != "" {
		t.Fatalf("expected empty provider, got %q", p)
	}
}

func TestRoundRobinDistributes(t *testing.T) {
	r := NewRouter(comboSlice(makeCombo("fast", "round-robin", [][2]string{{"sn", "flash"}, {"ds", "chat"}})))
	p1, _ := r.NextMember("fast", map[[2]string]bool{})
	p2, _ := r.NextMember("fast", map[[2]string]bool{})
	if p1 == p2 {
		t.Fatalf("expected different members, both %q", p1)
	}
}

func TestListCombos(t *testing.T) {
	r := NewRouter(comboSlice(
		makeCombo("fast", "fill-first", [][2]string{{"sn", "flash"}}),
		makeCombo("slow", "fill-first", [][2]string{{"sn", "r1"}}),
	))
	got := map[string]bool{}
	for _, n := range r.List() {
		got[n] = true
	}
	if !got["fast"] || !got["slow"] || len(got) != 2 {
		t.Fatalf("unexpected list: %v", r.List())
	}
}

func makeGroupCombo(name, ownedBy string, isDef bool, strategy string, members [][2]string, aliases ...string) *config.ComboConfig {
	c := &config.ComboConfig{
		Name:      name,
		OwnedBy:   ownedBy,
		IsDefault: isDef,
		APIFormat: "openai",
		Strategy:  strategy,
		Aliases:   aliases,
	}
	for _, m := range members {
		c.Members = append(c.Members, config.ComboMember{Provider: m[0], Model: m[1]})
	}
	return c
}

func TestOwnedByGroupingRouter(t *testing.T) {
	cDefault := makeGroupCombo("fast", "default", true, "fill-first", [][2]string{{"sn", "flash-default"}}, "fast-alias")
	cTeamA := makeGroupCombo("fast", "team-a", false, "fill-first", [][2]string{{"ds", "chat-teama"}}, "team-alias")

	r := NewRouter(comboSlice(cDefault, cTeamA))

	// Default combo accessible via "fast" and alias "fast-alias"
	if !r.IsCombo("fast") {
		t.Fatal("expected 'fast' to be a valid combo in default group")
	}
	if !r.IsCombo("fast-alias") {
		t.Fatal("expected 'fast-alias' to be a valid combo alias in default group")
	}
	p, m := r.NextMember("fast", map[[2]string]bool{})
	if p != "sn" || m != "flash-default" {
		t.Fatalf("expected (sn, flash-default), got (%s, %s)", p, m)
	}

	// Non-default combo MUST be accessed via "team-a/fast" or "team-a/team-alias"
	if !r.IsCombo("team-a/fast") {
		t.Fatal("expected 'team-a/fast' to be a valid combo")
	}
	if !r.IsCombo("team-a/team-alias") {
		t.Fatal("expected 'team-a/team-alias' to be a valid combo alias")
	}
	p, m = r.NextMember("team-a/fast", map[[2]string]bool{})
	if p != "ds" || m != "chat-teama" {
		t.Fatalf("expected (ds, chat-teama), got (%s, %s)", p, m)
	}

	// Alias resolution for non-default group
	p, m = r.NextMember("team-a/team-alias", map[[2]string]bool{})
	if p != "ds" || m != "chat-teama" {
		t.Fatalf("expected alias to route to (ds, chat-teama), got (%s, %s)", p, m)
	}

	// Non-existent group
	if r.IsCombo("team-b/fast") {
		t.Fatal("expected 'team-b/fast' to be unknown")
	}
}

func TestOwnedByRoundRobinIsolation(t *testing.T) {
	cDefault := makeGroupCombo("fast", "default", true, "round-robin", [][2]string{{"p1", "m1"}, {"p2", "m2"}})
	cTeamA := makeGroupCombo("fast", "team-a", false, "round-robin", [][2]string{{"pA", "mA"}, {"pB", "mB"}})

	r := NewRouter(comboSlice(cDefault, cTeamA))

	// First call to default advances default index
	p1, _ := r.NextMember("fast", map[[2]string]bool{})
	// First call to team-a advances team-a index independently
	pa1, _ := r.NextMember("team-a/fast", map[[2]string]bool{})

	p2, _ := r.NextMember("fast", map[[2]string]bool{})
	pa2, _ := r.NextMember("team-a/fast", map[[2]string]bool{})

	if p1 == p2 {
		t.Fatalf("expected default to rotate: %s vs %s", p1, p2)
	}
	if pa1 == pa2 {
		t.Fatalf("expected team-a to rotate: %s vs %s", pa1, pa2)
	}
}

