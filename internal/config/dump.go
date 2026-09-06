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
	if cfg.General.RequestTimeoutSeconds == RequestTimeoutDisabled {
		general["request_timeout_seconds"] = 0
	} else if cfg.General.RequestTimeoutSeconds > 0 {
		general["request_timeout_seconds"] = cfg.General.RequestTimeoutSeconds
	}
	if cfg.General.AdminPassword != "" {
		general["admin_password"] = cfg.General.AdminPassword
	}
	if cfg.General.ExternalURL != "" {
		general["external_url"] = cfg.General.ExternalURL
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
			ruleMap := map[string]any{
				"description":      r.Description,
				"jsonpath":         r.JSONPath,
				"match_value":      r.MatchValue,
				"match_type":       r.MatchType,
				"action":           r.Action,
				"cooldown_seconds": r.CooldownSeconds,
				"models":           models,
			}
			if len(r.HTTPStatusCodes) > 0 {
				codes := make([]any, len(r.HTTPStatusCodes))
				for i, c := range r.HTTPStatusCodes {
					codes[i] = c
				}
				ruleMap["http_status_codes"] = codes
			}
			rules = append(rules, ruleMap)
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

	type groupEntry struct {
		ownedBy string
		isDef   bool
		items   []any
	}
	var groupList []groupEntry
	groupIndex := map[string]int{}

	for _, c := range cfg.Combos {
		ownedBy := c.OwnedBy
		if ownedBy == "" {
			ownedBy = "default"
		}
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

		idx, exists := groupIndex[ownedBy]
		if !exists {
			idx = len(groupList)
			groupIndex[ownedBy] = idx
			groupList = append(groupList, groupEntry{
				ownedBy: ownedBy,
				isDef:   c.IsDefault,
				items:   []any{combo},
			})
		} else {
			groupList[idx].items = append(groupList[idx].items, combo)
		}
	}

	combos := make([]any, 0, len(groupList))
	for _, g := range groupList {
		grp := map[string]any{
			"owned_by": g.ownedBy,
			"combos":   g.items,
		}
		if g.isDef {
			grp["default"] = true
		}
		combos = append(combos, grp)
	}

	scripts := make([]any, 0, len(cfg.PayloadScripts))
	for _, s := range cfg.PayloadScripts {
		scripts = append(scripts, map[string]any{
			"name":    s.Name,
			"enabled": s.Enabled,
			"script":  s.Script,
		})
	}

	logging := map[string]any{
		"enabled":           cfg.Logging.Enabled,
		"dir":               cfg.Logging.Dir,
		"max_file_size_mb":  cfg.Logging.MaxFileSizeMB,
		"max_backups":       cfg.Logging.MaxBackups,
		"compression_level": cfg.Logging.CompressionLevel,
	}

	mcpProviders := make([]any, 0, len(cfg.McpProviders))
	for _, p := range cfg.McpProviders {
		pm := map[string]any{
			"name":      p.Name,
			"transport": p.Transport,
		}
		if p.TimeoutSeconds > 0 {
			pm["timeout_seconds"] = p.TimeoutSeconds
		}
		if p.AuthMode != "" {
			pm["auth_mode"] = p.AuthMode
		}
		if p.Transport == "stdio" {
			pm["command"] = p.Command
			if len(p.Args) > 0 {
				pm["args"] = p.Args
			}
			if len(p.Env) > 0 {
				pm["env"] = p.Env
			}
			if p.WorkingDir != "" {
				pm["working_dir"] = p.WorkingDir
			}
		} else {
			pm["url"] = p.URL
			if len(p.Headers) > 0 {
				pm["headers"] = p.Headers
			}
		}
		if p.Auth != nil {
			mode := p.Auth.Mode
			if mode == "" {
				mode = p.Auth.Type
			}
			am := map[string]any{"mode": mode}
			if p.AuthMode == "isolated" || p.Auth.Isolated {
				am["isolated"] = true
			}
			if mode == "api_key" {
				am["api_key"] = p.Auth.APIKey
				if p.Auth.Header != "" {
					am["header"] = p.Auth.Header
				}
			} else if mode == "oauth2" {
				am["client_id"] = p.Auth.ClientID
				if p.Auth.ClientSecret != "" {
					am["client_secret"] = p.Auth.ClientSecret
				}
				authURL := p.Auth.AuthURL
				if authURL == "" {
					authURL = p.Auth.AuthorizationURL
				}
				am["auth_url"] = authURL
				am["token_url"] = p.Auth.TokenURL
				if len(p.Auth.Scopes) > 0 {
					am["scopes"] = p.Auth.Scopes
				}
				if p.Auth.RedirectURL != "" {
					am["redirect_url"] = p.Auth.RedirectURL
				}
			}
			pm["auth"] = am
		}
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
		mcpProviders = append(mcpProviders, pm)
	}

	mcpCombos := make([]any, 0, len(cfg.McpCombos))
	for _, c := range cfg.McpCombos {
		members := make([]any, 0, len(c.Members))
		for _, m := range c.Members {
			mm := map[string]any{
				"provider": m.Provider,
			}
			if m.Prefix != "" {
				mm["prefix"] = m.Prefix
			}
			if len(m.Tools) > 0 {
				mm["tools"] = m.Tools
			}
			if len(m.Resources) > 0 {
				mm["resources"] = m.Resources
			}
			if len(m.Prompts) > 0 {
				mm["prompts"] = m.Prompts
			}
			members = append(members, mm)
		}
		cm := map[string]any{
			"name":           c.Name,
			"user_id_header": c.UserIDHeader,
			"members":        members,
		}
		if c.OwnedBy != "" {
			cm["owned_by"] = c.OwnedBy
		}
		if c.Description != "" {
			cm["description"] = c.Description
		}
		mcpCombos = append(mcpCombos, cm)
	}

	out := map[string]any{
		"providers":       providers,
		"combos":          combos,
		"verbose_logging": cfg.VerboseLogging,
		"logging":         logging,
		"payload_scripts": scripts,
	}
	if len(mcpProviders) > 0 {
		out["mcp_providers"] = mcpProviders
	}
	if len(mcpCombos) > 0 {
		out["mcp_combos"] = mcpCombos
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
