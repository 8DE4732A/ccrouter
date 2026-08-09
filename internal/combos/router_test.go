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

func TestUnknownCombo(t *testing.T) {
	r := NewRouter(comboSlice(makeCombo("fast", "fill-first", [][2]string{{"sn", "flash"}})))
	p, _ := r.NextMember("ghost", map[[2]string]bool{})
	if p != "" {
		t.Fatalf("expected empty for unknown combo, got %q", p)
	}
}
