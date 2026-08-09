package keys

import (
	"sync"
	"time"
)

// ModelCooldown tracks cooldown state for a (key, model) pair.
type ModelCooldown struct {
	LastErrorAt     float64
	CooldownSeconds int
}

// KeyStats tracks usage statistics for a single key.
type KeyStats struct {
	KeyPrefix  string
	UseCount   int
	ErrorCount int
	LastUsedAt *float64
}

// Manager manages a provider's keys with (key, model) granularity cooldown.
// Thread-safe: all public methods acquire the internal lock.
type Manager struct {
	ProviderName string
	keys         []string
	strategy     string
	rrIndex      int
	mu           sync.Mutex
	stats        []KeyStats
	cooldowns    map[[2]any]ModelCooldown // key:(index, model)
}

// NewManager creates a key manager for provider.
func NewManager(providerName string, keys []string, strategy string) *Manager {
	stats := make([]KeyStats, len(keys))
	for i, k := range keys {
		stats[i] = KeyStats{KeyPrefix: prefix(k)}
	}
	if strategy == "" {
		strategy = "fill-first"
	}
	return &Manager{
		ProviderName: providerName,
		keys:         keys,
		strategy:     strategy,
		stats:        stats,
		cooldowns:    make(map[[2]any]ModelCooldown),
	}
}

func prefix(k string) string {
	if len(k) > 8 {
		return k[:8]
	}
	return k
}

func (m *Manager) isAvailable(idx int, model string, now float64) bool {
	cd, ok := m.cooldowns[[2]any{idx, model}]
	if !ok {
		return true
	}
	return now-cd.LastErrorAt >= float64(cd.CooldownSeconds)
}

// Get returns the next usable key for model, or "" if all are cooling down.
func (m *Manager) Get(model string, attempted map[string]bool) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := float64(time.Now().UnixNano()) / 1e9
	if m.strategy == "fill-first" {
		for i, key := range m.keys {
			if !attempted[key] && m.isAvailable(i, model, now) {
				return key
			}
		}
		return ""
	}
	// round-robin
	for i := 0; i < len(m.keys); i++ {
		m.rrIndex = (m.rrIndex + 1) % len(m.keys)
		key := m.keys[m.rrIndex]
		if !attempted[key] && m.isAvailable(m.rrIndex, model, now) {
			return key
		}
	}
	return ""
}

// RecordError marks key as cooling down for model for cooldownSeconds.
func (m *Manager) RecordError(key string, model string, cooldownSeconds int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.index(key)
	if idx < 0 {
		return
	}
	m.stats[idx].ErrorCount++
	now := float64(time.Now().UnixNano()) / 1e9
	t := now
	m.stats[idx].LastUsedAt = &t
	m.cooldowns[[2]any{idx, model}] = ModelCooldown{LastErrorAt: now, CooldownSeconds: cooldownSeconds}
}

// RecordSuccess records a successful use of key for model.
func (m *Manager) RecordSuccess(key string, model string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.index(key)
	if idx < 0 {
		return
	}
	m.stats[idx].UseCount++
	now := float64(time.Now().UnixNano()) / 1e9
	t := now
	m.stats[idx].LastUsedAt = &t
}

func (m *Manager) index(key string) int {
	for i, k := range m.keys {
		if k == key {
			return i
		}
	}
	return -1
}

// Stats returns per-key statistics with model cooldown details.
func (m *Manager) Stats() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := float64(time.Now().UnixNano()) / 1e9
	keysInfo := make([]any, 0, len(m.keys))
	for i := range m.keys {
		st := m.stats[i]
		modelCooldowns := map[string]any{}
		for pair, cd := range m.cooldowns {
			ki, ok := pair[0].(int)
			if !ok || ki != i {
				continue
			}
			model, _ := pair[1].(string)
			elapsed := now - cd.LastErrorAt
			remaining := float64(cd.CooldownSeconds) - elapsed
			if remaining > 0 {
				modelCooldowns[model] = map[string]any{"available": false, "seconds_remaining": round1(remaining)}
			} else {
				modelCooldowns[model] = map[string]any{"available": true}
			}
		}
		entry := map[string]any{
			"key_prefix":      st.KeyPrefix,
			"use_count":       st.UseCount,
			"error_count":     st.ErrorCount,
			"last_used_at":    st.LastUsedAt,
			"model_cooldowns": modelCooldowns,
		}
		keysInfo = append(keysInfo, entry)
	}
	return map[string]any{
		"provider": m.ProviderName,
		"strategy": m.strategy,
		"keys":     keysInfo,
	}
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}

// MergeStats migrates per-key stats and unexpired cooldowns from old into this manager,
// matching by key string. Caller ensures old is no longer written.
func (m *Manager) MergeStats(old *Manager) {
	now := float64(time.Now().UnixNano()) / 1e9
	old.mu.Lock()
	oldKeyIndex := make(map[string]int, len(old.keys))
	for i, k := range old.keys {
		oldKeyIndex[k] = i
	}
	oldCooldowns := make(map[[2]any]ModelCooldown, len(old.cooldowns))
	for k, v := range old.cooldowns {
		oldCooldowns[k] = v
	}
	old.mu.Unlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	for i, key := range m.keys {
		oldIdx, ok := oldKeyIndex[key]
		if !ok {
			continue
		}
		os := old.stats[oldIdx]
		m.stats[i].UseCount = os.UseCount
		m.stats[i].ErrorCount = os.ErrorCount
		m.stats[i].LastUsedAt = os.LastUsedAt
		for pair, cd := range oldCooldowns {
			ki, ok := pair[0].(int)
			if !ok || ki != oldIdx {
				continue
			}
			model, _ := pair[1].(string)
			if now-cd.LastErrorAt < float64(cd.CooldownSeconds) {
				m.cooldowns[[2]any{i, model}] = cd
			}
		}
	}
}

// Keys returns a copy of the key strings.
func (m *Manager) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.keys))
	copy(out, m.keys)
	return out
}

// TotalKeys returns the number of keys.
func (m *Manager) TotalKeys() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.keys)
}
