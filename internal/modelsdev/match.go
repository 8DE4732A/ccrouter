package modelsdev

import (
	"math"
	"regexp"
	"strings"
)

// dateSuffixRe matches the common release-date suffixes models carry, e.g.
// "-20250806", "-2024-08-06", "-2024-08" (also with "_", "." or "@").
var dateSuffixRe = regexp.MustCompile(`(?:[-_@](?:20\d{2}[-_.]?\d{2}[-_.]?\d{2}|20\d{2}[-_.]?\d{2}))$`)

// normalizeModelID strips a provider prefix and release-date suffixes so
// "openai/gpt-4o-2024-08-06" and "gpt-4o" compare equal.
func normalizeModelID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	for {
		stripped := dateSuffixRe.ReplaceAllString(s, "")
		if stripped == s {
			return s
		}
		s = stripped
	}
}

// providerMatches reports whether a ccrouter provider hint plausibly refers to
// the catalog entry's provider. It is only ever used to boost ranking.
func providerMatches(e Entry, hint string) bool {
	hint = strings.ToLower(strings.TrimSpace(hint))
	if hint == "" {
		return false
	}
	pid := strings.ToLower(e.ProviderID)
	pname := strings.ToLower(e.ProviderName)
	return pid == hint || pname == hint ||
		strings.Contains(pid, hint) || strings.Contains(pname, hint)
}

// searchScore ranks a catalog entry for a substring search. Every term has
// already been verified to be present; this only orders the matches.
func searchScore(e Entry, terms []string, providerHint string) float64 {
	model := strings.ToLower(e.ModelID)
	provider := strings.ToLower(e.ProviderID + " " + e.ProviderName)
	score := 0.2
	for _, t := range terms {
		switch {
		case model == t:
			score += 0.5
		case strings.HasPrefix(model, t):
			score += 0.3
		case strings.Contains(model, t):
			score += 0.2
		}
		if strings.Contains(provider, t) {
			score += 0.1
		}
	}
	if providerMatches(e, providerHint) {
		score += 0.15
	}
	return math.Min(score, 1)
}

// suggestScore ranks an entry for a member model ID. Scores are in [0,1]:
// an exact model ID reaches 1.0 even without a provider hint; the provider
// hint adds a bounded bonus. The admin UI auto-applies only when the top
// score is >= 0.9 and strictly greater than the runner-up, so ambiguous
// duplicates fall back to the picker.
func suggestScore(e Entry, raw, norm, providerHint, entryNorm string) float64 {
	rawLower := strings.ToLower(strings.TrimSpace(raw))
	model := strings.ToLower(e.ModelID)
	canon := strings.ToLower(e.CanonicalModelID)

	var score float64
	switch {
	case model == rawLower:
		score = 100
	case canon != "" && (canon == rawLower || canon == norm):
		score = 95
	case entryNorm == norm:
		score = 85
	case strings.HasPrefix(model, rawLower):
		score = 70
	case strings.Contains(model, rawLower):
		score = 55
	case canon != "" && strings.Contains(canon, norm):
		score = 50
	case strings.Contains(strings.ToLower(e.Name), rawLower):
		score = 40
	default:
		return 0
	}

	if providerHint != "" && providerMatches(e, providerHint) {
		score += 25
	}
	return math.Min(score/100, 1)
}
