package combos

import (
	"sync"

	"ccrouter/internal/config"
)

// Router routes requests to (provider, model) members based on combo config.
// Thread-safe: round-robin indices are guarded by a lock.
type Router struct {
	combos    map[string]*config.ComboConfig
	aliases   map[string]string
	rrIndices map[string]int
	mu        sync.Mutex
}

// NewRouter builds a Router from a list of combo configs.
func NewRouter(combos []config.ComboConfig) *Router {
	r := &Router{
		combos:    make(map[string]*config.ComboConfig),
		aliases:   make(map[string]string),
		rrIndices: make(map[string]int),
	}
	for i := range combos {
		c := &combos[i]
		r.combos[c.Name] = c
		for _, a := range c.Aliases {
			r.aliases[a] = c.Name
		}
		r.rrIndices[c.Name] = 0
	}
	return r
}

func (r *Router) resolve(name string) string {
	if canon, ok := r.aliases[name]; ok {
		return canon
	}
	return name
}

// IsCombo reports whether name resolves to a known combo.
func (r *Router) IsCombo(name string) bool {
	_, ok := r.combos[r.resolve(name)]
	return ok
}

// GetCombo returns the resolved combo config, or nil.
func (r *Router) GetCombo(name string) *config.ComboConfig {
	return r.combos[r.resolve(name)]
}

// NextMember returns the next (provider, model) pair not yet in attempted.
// Returns "" provider when all members have been attempted.
func (r *Router) NextMember(name string, attempted map[[2]string]bool) (string, string) {
	p, m, _ := r.NextMemberWithFmt(name, attempted)
	return p, m
}

// NextMemberWithFmt returns the next (provider, model, upstreamAPIFormat) tuple not yet attempted.
// upstreamAPIFormat is empty when the member has no override (caller should fall back to the request format).
func (r *Router) NextMemberWithFmt(name string, attempted map[[2]string]bool) (string, string, string) {
	canonical := r.resolve(name)
	combo := r.combos[canonical]
	if combo == nil || len(combo.Members) == 0 {
		return "", "", ""
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if combo.Strategy == "fill-first" {
		for _, m := range combo.Members {
			if !attempted[[2]string{m.Provider, m.Model}] {
				return m.Provider, m.Model, m.UpstreamAPIFormat
			}
		}
		return "", "", ""
	}

	// round-robin: advance once, scan all members
	idx := r.rrIndices[canonical]
	for i := 0; i < len(combo.Members); i++ {
		idx = (idx + 1) % len(combo.Members)
		m := combo.Members[idx]
		if !attempted[[2]string{m.Provider, m.Model}] {
			r.rrIndices[canonical] = idx
			return m.Provider, m.Model, m.UpstreamAPIFormat
		}
	}
	return "", "", ""
}

// List returns canonical combo names (aliases are transparent shortcuts).
func (r *Router) List() []string {
	out := make([]string, 0, len(r.combos))
	for name := range r.combos {
		out = append(out, name)
	}
	return out
}
