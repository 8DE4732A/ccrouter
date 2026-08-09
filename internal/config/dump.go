package config

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// Dump serializes an AppConfig to a plain map suitable for JSON/yaml output.
func Dump(cfg *AppConfig) map[string]any {
	// General config (api_keys + proxy)
	general := map[string]any{}
	if len(cfg.General.APIKeys) > 0 {
		apiKeys := make([]any, 0, len(cfg.General.APIKeys))
		for _, k := range cfg.General.APIKeys {
			apiKeys = append(apiKeys, map[string]any{"key": k.Key})
		}
		general["api_keys"] = apiKeys
	}
	if cfg.General.Proxy != nil {
		proxy := map[string]any{}
		if cfg.General.Proxy.URL != "" {
			proxy["url"] = cfg.General.Proxy.URL
		}
		if cfg.General.Proxy.Disabled {
			proxy["disabled"] = true
		}
		if len(proxy) > 0 {
			general["proxy"] = proxy
		}
	}

	providers := make([]any, 0, len(cfg.Providers))
	for _, p := range cfg.Providers {
		apis := make([]any, 0, len(p.APIs))
		for _, ep := range p.APIs {
			apis = append(apis, map[string]any{
				"api_format": ep.APIFormat,
				"base_url":   ep.BaseURL,
			})
		}
		keys := make([]any, 0, len(p.Keys))
		for _, k := range p.Keys {
			keys = append(keys, map[string]any{"key": k.Key})
		}
		rules := make([]any, 0, len(p.HealthCheckRules))
		for _, r := range p.HealthCheckRules {
			models := make([]any, len(r.Models))
			for i := range r.Models {
				models[i] = r.Models[i]
			}
			rules = append(rules, map[string]any{
				"description":      r.Description,
				"jsonpath":         r.JSONPath,
				"match_value":      r.MatchValue,
				"match_type":       r.MatchType,
				"action":           r.Action,
				"cooldown_seconds": r.CooldownSeconds,
				"models":           models,
			})
		}
		pm := map[string]any{
			"name":               p.Name,
			"api":                apis,
			"max_retries":        p.MaxRetries,
			"key_strategy":       p.KeyStrategy,
			"keys":               keys,
			"health_check_rules": rules,
		}
		// Per-provider proxy override.
		if p.Proxy != nil {
			pp := map[string]any{}
			if p.Proxy.URL != "" {
				pp["url"] = p.Proxy.URL
			}
			if p.Proxy.Disabled {
				pp["disabled"] = true
			}
			if len(pp) > 0 {
				pm["proxy"] = pp
			}
		}
		providers = append(providers, pm)
	}

	combos := make([]any, 0, len(cfg.Combos))
	for _, c := range cfg.Combos {
		members := make([]any, 0, len(c.Members))
		for _, m := range c.Members {
			mm := map[string]any{"provider": m.Provider, "model": m.Model}
			if m.UpstreamAPIFormat != "" {
				mm["upstream_api_format"] = m.UpstreamAPIFormat
			}
			members = append(members, mm)
		}
		combo := map[string]any{
			"name":       c.Name,
			"api_format": c.APIFormat,
			"strategy":   c.Strategy,
			"members":    members,
		}
		if len(c.Aliases) > 0 {
			aliases := make([]any, len(c.Aliases))
			for i := range c.Aliases {
				aliases[i] = c.Aliases[i]
			}
			combo["aliases"] = aliases
		}
		combos = append(combos, combo)
	}

	scripts := make([]any, 0, len(cfg.PayloadScripts))
	for _, s := range cfg.PayloadScripts {
		scripts = append(scripts, map[string]any{
			"name":    s.Name,
			"enabled": s.Enabled,
			"script":  s.Script,
		})
	}

	out := map[string]any{
		"providers":       providers,
		"combos":          combos,
		"verbose_logging": cfg.VerboseLogging,
		"payload_scripts": scripts,
	}
	if len(general) > 0 {
		out["general"] = general
	}
	return out
}

// ToYAML serializes an AppConfig to YAML text (used for atomic config write).
func ToYAML(cfg *AppConfig) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(Dump(cfg)); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
