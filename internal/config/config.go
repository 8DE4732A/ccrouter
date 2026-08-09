package config

import (
	"regexp"
	"strings"
)

// validClientFormats are formats the proxy exposes to clients (server routes exist for each).
var validClientFormats = map[string]bool{
	"openai":           true,
	"anthropic":        true,
	"openai-responses": true,
	"openai-images":    true,
}

// validAPIFormats includes all formats valid for upstream endpoints and upstream_api_format hints.
// Gemini is upstream-only: no client-facing route is registered for it.
var validAPIFormats = map[string]bool{
	"openai":           true,
	"anthropic":        true,
	"openai-responses": true,
	"openai-images":    true,
	"gemini":           true,
}

var validKeyStrategies = map[string]bool{
	"fill-first":  true,
	"round-robin": true,
}

var validMatchTypes = map[string]bool{
	"equals":   true,
	"contains": true,
	"regex":    true,
}

// KeyConfig is a single API key entry.
type KeyConfig struct {
	Key string `json:"key" yaml:"key"`
}

// HealthCheckRule triggers key rotation when a JSONPath expression matches.
type HealthCheckRule struct {
	Description     string   `json:"description" yaml:"description"`
	JSONPath        string   `json:"jsonpath" yaml:"jsonpath"`
	MatchValue      string   `json:"match_value" yaml:"match_value"`
	MatchType       string   `json:"match_type" yaml:"match_type"`
	Action          string   `json:"action" yaml:"action"`
	CooldownSeconds int      `json:"cooldown_seconds" yaml:"cooldown_seconds"`
	Models          []string `json:"models" yaml:"models"`

	// Compiled regex for match_type == "regex" (not serialized).
	regex *regexp.Regexp `json:"-" yaml:"-"`
}

// CompiledRegex returns the compiled regex for regex match rules, or nil.
func (r *HealthCheckRule) CompiledRegex() *regexp.Regexp { return r.regex }

type ApiEndpoint struct {
	APIFormat string `json:"api_format" yaml:"api_format"`
	BaseURL   string `json:"base_url" yaml:"base_url"`
}

// ChatURL returns the full upstream URL for this endpoint.
func (e *ApiEndpoint) ChatURL() string {
	base := strings.TrimRight(e.BaseURL, "/")
	switch e.APIFormat {
	case "anthropic":
		return base + "/messages"
	case "openai-responses":
		return base + "/responses"
	case "openai-images":
		return base + "/images/generations"
	case "gemini":
		// Gemini generateContent endpoint; model name is appended dynamically by the caller.
		return base + "/models"
	default:
		return base + "/chat/completions"
	}
}

type ProviderConfig struct {
	Name             string            `json:"name" yaml:"name"`
	APIs             []ApiEndpoint     `json:"api" yaml:"api"`
	MaxRetries       int               `json:"max_retries" yaml:"max_retries"`
	KeyStrategy      string            `json:"key_strategy" yaml:"key_strategy"`
	Keys             []KeyConfig       `json:"keys" yaml:"keys"`
	HealthCheckRules []HealthCheckRule `json:"health_check_rules" yaml:"health_check_rules"`
}

// SupportsFormat reports whether the provider has an endpoint for api_format.
func (p *ProviderConfig) SupportsFormat(fmt string) bool {
	for _, ep := range p.APIs {
		if ep.APIFormat == fmt {
			return true
		}
	}
	return false
}

// GetChatURL returns the upstream URL for api_format, or empty string.
func (p *ProviderConfig) GetChatURL(fmt string) string {
	for _, ep := range p.APIs {
		if ep.APIFormat == fmt {
			return ep.ChatURL()
		}
	}
	return ""
}

type ComboMember struct {
	Provider         string `json:"provider" yaml:"provider"`
	Model            string `json:"model" yaml:"model"`
	// UpstreamAPIFormat overrides the API format sent to this upstream provider.
	// When set to a value different from the combo's client-facing api_format,
	// ccrouter uses CLIProxyAPI translator to convert the request/response.
	// Example: combo accepts "anthropic" but this member's provider only speaks "openai" —
	// set upstream_api_format: openai and the translation happens automatically.
	UpstreamAPIFormat string `json:"upstream_api_format,omitempty" yaml:"upstream_api_format,omitempty"`
}

type ComboConfig struct {
	Name      string        `json:"name" yaml:"name"`
	APIFormat any           `json:"api_format" yaml:"api_format"` // string or []string
	Strategy  string        `json:"strategy" yaml:"strategy"`
	Members   []ComboMember `json:"members" yaml:"members"`
	Aliases   []string      `json:"aliases,omitempty" yaml:"aliases,omitempty"`
}

// APIFormats derives the accepted API formats from the APIFormat field,
// which is either a string or a []string (or a []any after YAML decode).
func (c *ComboConfig) APIFormats() []string {
	switch v := c.APIFormat.(type) {
	case string:
		return []string{v}
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

type PayloadScript struct {
	Name    string `json:"name" yaml:"name"`
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Script  string `json:"script" yaml:"script"`
}

type AppConfig struct {
	Providers      []ProviderConfig `json:"providers" yaml:"providers"`
	Combos         []ComboConfig    `json:"combos" yaml:"combos"`
	VerboseLogging bool             `json:"verbose_logging" yaml:"verbose_logging"`
	PayloadScripts []PayloadScript  `json:"payload_scripts" yaml:"payload_scripts"`
}
