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

func relayCombo(name, ownedBy string, aliases []string, members [][2]string) *config.ComboConfig {
	c := makeCombo(name, "fill-first", members)
	c.Relay = true
	c.OwnedBy = ownedBy
	c.Aliases = aliases
	return c
}

func TestResolveRequest(t *testing.T) {
	normal := makeCombo("fast", "fill-first", [][2]string{{"sn", "flash"}})
	relay := relayCombo("byok", "", nil, [][2]string{{"sn", ""}})
	relayAlias := relayCombo("oa", "", []string{"openai"}, [][2]string{{"sn", ""}})
	owned := relayCombo("byok", "team", nil, [][2]string{{"sn", ""}})
	longest := relayCombo("b", "a", nil, [][2]string{{"sn", ""}}) // full name a/b

	r := NewRouter(comboSlice(normal, relay, relayAlias, owned, longest))

	cases := []struct {
		request   string
		wantCombo string
		wantModel string
		wantOK    bool
	}{
		{"fast", "fast", "", true},
		{"byok", "byok", "", true},
		{"byok/gpt-4o", "byok", "gpt-4o", true},
		{"byok/anthropic/claude-3.5", "byok", "anthropic/claude-3.5", true},
		{"openai/gpt-4o", "oa", "gpt-4o", true},
		{"team/byok", "team/byok", "", true},
		{"team/byok/gpt-4o", "team/byok", "gpt-4o", true},
		{"a/b/gpt-4o", "a/b", "gpt-4o", true},
		{"byok/", "", "", false},
		{"fast/gpt-4o", "", "", false},
		{"unknown/gpt-4o", "", "", false},
	}
	for _, tc := range cases {
		combo, model, ok := r.ResolveRequest(tc.request)
		if ok != tc.wantOK {
			t.Fatalf("%q: ok=%v want %v", tc.request, ok, tc.wantOK)
		}
		if !ok {
			continue
		}
		if combo.FullName() != tc.wantCombo || model != tc.wantModel {
			t.Fatalf("%q: got (%s,%q), want (%s,%q)", tc.request,
				combo.FullName(), model, tc.wantCombo, tc.wantModel)
		}
	}
}

func TestNextMemberRelayEmptyModelBookkeeping(t *testing.T) {
	relay := relayCombo("byok", "", nil, [][2]string{{"sn", ""}, {"ds", ""}})
	r := NewRouter(comboSlice(relay))

	provider, model, _ := r.NextMemberWithFmt("byok", map[[2]string]bool{})
	if provider != "sn" || model != "" {
		t.Fatalf("first member = (%q,%q)", provider, model)
	}
	attempted := map[[2]string]bool{{"sn", ""}: true}
	provider, model, _ = r.NextMemberWithFmt("byok", attempted)
	if provider != "ds" || model != "" {
		t.Fatalf("second member = (%q,%q)", provider, model)
	}
}

func TestMemberRelayMatchesByProvider(t *testing.T) {
	cost := &config.ComboMemberCost{Input: func() *float64 { v := 1.0; return &v }()}
	relay := relayCombo("byok", "", nil, [][2]string{{"sn", ""}})
	relay.Members[0].Cost = cost
	normal := makeCombo("fast", "fill-first", [][2]string{{"sn", "flash"}})
	normal.Members[0].Cost = cost
	r := NewRouter(comboSlice(relay, normal))

	m, ok := r.Member("byok", "sn", "any-upstream-model")
	if !ok || m.Cost != cost {
		t.Fatalf("relay cost member = %+v, ok=%v", m, ok)
	}
	if _, ok := r.Member("fast", "sn", "wrong-model"); ok {
		t.Fatal("normal combo must match the exact model")
	}
	if m, ok := r.Member("fast", "sn", "flash"); !ok || m.Cost != cost {
		t.Fatalf("normal cost member = %+v, ok=%v", m, ok)
	}
}
