package config

import (
	"fmt"
	"regexp"
	"strings"
)

// ScriptCompileFunc is called during Build to validate each payload script.
// Set this to script.Compile at program startup to enable compile-time validation.
// Default is a no-op so the config package has no import dependency on script.
var ScriptCompileFunc func(src string) error

func compileScript(src string) (any, error) {
	if ScriptCompileFunc == nil {
		return nil, nil
	}
	return nil, ScriptCompileFunc(src)
}

// normalize returns a lowercased string.
func lower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// asStringList coerces a single string or list into []string.
func asStringList(v any, ctx string) ([]string, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case string:
		return []string{t}, nil
	case []any:
		out := make([]string, 0, len(t))
		for i, item := range t {
			s, ok := item.(string)
			if !ok {
				return nil, errf("%s[%d] must be a string", ctx, i)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, errf("%s must be a string or list", ctx)
	}
}

func asMap(v any, ctx string) (map[string]any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errf("%s must be a mapping", ctx)
	}
	return m, nil
}

func buildConfig(raw map[string]any) (*AppConfig, error) {
	cfg := &AppConfig{}

	// ---- providers ----
	providersRaw, ok := raw["providers"].([]any)
	if !ok || len(providersRaw) == 0 {
		return nil, errf("configuration must contain at least one entry in 'providers'")
	}
	providerNames := map[string]bool{}
	for i, pv := range providersRaw {
		p, err := asMap(pv, fmt.Sprintf("providers[%d]", i))
		if err != nil {
			return nil, err
		}
		pc, err := buildProvider(p, i)
		if err != nil {
			return nil, err
		}
		if providerNames[pc.Name] {
			return nil, errf("duplicate provider name: %q", pc.Name)
		}
		providerNames[pc.Name] = true
		cfg.Providers = append(cfg.Providers, *pc)
	}

	// ---- combos ----
	combosRaw, ok := raw["combos"].([]any)
	if !ok || len(combosRaw) == 0 {
		return nil, errf("configuration must contain at least one entry in 'combos'")
	}
	providerMap := map[string]*ProviderConfig{}
	for i := range cfg.Providers {
		providerMap[cfg.Providers[i].Name] = &cfg.Providers[i]
	}
	comboNames := map[string]bool{}
	for i, cv := range combosRaw {
		c, err := asMap(cv, fmt.Sprintf("combos[%d]", i))
		if err != nil {
			return nil, err
		}
		cc, err := buildCombo(c, i, providerMap, comboNames)
		if err != nil {
			return nil, err
		}
		cfg.Combos = append(cfg.Combos, *cc)
	}

	// ---- general ----
	if v, ok := raw["general"]; ok && v != nil {
		gm, err := asMap(v, "general")
		if err != nil {
			return nil, err
		}
		// api_keys
		if av, ok := gm["api_keys"]; ok {
			arr, ok := av.([]any)
			if !ok {
				return nil, errf("'general.api_keys' must be a list")
			}
			for i, kv := range arr {
				km, err := asMap(kv, fmt.Sprintf("general.api_keys[%d]", i))
				if err != nil {
					return nil, err
				}
				key := strings.TrimSpace(strVal(km["key"]))
				if key == "" {
					continue
				}
				cfg.General.APIKeys = append(cfg.General.APIKeys, APIKeyEntry{Key: key})
			}
		}
		// proxy
		if pv, ok := gm["proxy"]; ok && pv != nil {
			pm, err := asMap(pv, "general.proxy")
			if err != nil {
				return nil, err
			}
			cfg.General.Proxy = &ProxyConfig{
				URL:      strings.TrimSpace(strVal(pm["url"])),
				Disabled: toBool(pm["disabled"]),
			}
		}
		// request_timeout_seconds: total per-request timeout, in seconds.
		//   - field omitted            -> RequestTimeoutSeconds stays 0, runtime default (600s) applies.
		//   - explicit 0               -> disables the timeout entirely (waits indefinitely); stored
		//                                 as the sentinel -1 internally to distinguish from "omitted".
		//   - explicit positive value  -> used as-is.
		//   - explicit negative value  -> rejected.
		if tv, ok := gm["request_timeout_seconds"]; ok {
			timeout := intDefault(tv, 600)
			if timeout < 0 {
				return nil, errf("'general.request_timeout_seconds' must be >= 0, got %d", timeout)
			}
			if timeout == 0 {
				cfg.General.RequestTimeoutSeconds = RequestTimeoutDisabled
			} else {
				cfg.General.RequestTimeoutSeconds = timeout
			}
		}
	}

	// ---- verbose_logging ----
	if v, ok := raw["verbose_logging"]; ok {
		cfg.VerboseLogging = toBool(v)
	}

	// ---- payload_scripts ----
	if v, ok := raw["payload_scripts"]; ok {
		arr, ok := v.([]any)
		if !ok {
			return nil, errf("'payload_scripts' must be a list")
		}
		for i, sv := range arr {
			sm, err := asMap(sv, fmt.Sprintf("payload_scripts[%d]", i))
			if err != nil {
				return nil, err
			}
			src := strVal(sm["script"])
			if _, err := compileScript(src); err != nil {
				name := strVal(sm["name"])
				if name == "" {
					name = fmt.Sprintf("payload_scripts[%d]", i)
				}
				return nil, errf("payload script %q: %v", name, err)
			}
			cfg.PayloadScripts = append(cfg.PayloadScripts, PayloadScript{
				Name:    strVal(sm["name"]),
				Enabled: toBoolDefault(sm["enabled"], true),
				Script:  src,
			})
		}
	}

	return cfg, nil
}

func buildProvider(p map[string]any, idx int) (*ProviderConfig, error) {
	name := strings.TrimSpace(strVal(p["name"]))
	if name == "" {
		return nil, errf("providers[%d].name must not be empty", idx)
	}
	pc := &ProviderConfig{Name: name}

	// api endpoints
	apiRaw, ok := p["api"].([]any)
	if !ok || len(apiRaw) == 0 {
		return nil, errf("providers[%d].api must be a non-empty list", idx)
	}
	seenFormats := map[string]bool{}
	for k, ev := range apiRaw {
		em, err := asMap(ev, fmt.Sprintf("providers[%d].api[%d]", idx, k))
		if err != nil {
			return nil, err
		}
		af := lower(strVal(em["api_format"]))
		if !validAPIFormats[af] {
			return nil, errf("providers[%d].api[%d].api_format must be one of: openai, anthropic, openai-responses, openai-images, gemini", idx, k)
		}
		if seenFormats[af] {
			return nil, errf("providers[%d].api: duplicate api_format %q", idx, af)
		}
		seenFormats[af] = true
		baseURL := strings.TrimSpace(strVal(em["base_url"]))
		if baseURL == "" {
			return nil, errf("providers[%d].api[%d].base_url must not be empty", idx, k)
		}
		pc.APIs = append(pc.APIs, ApiEndpoint{APIFormat: af, BaseURL: baseURL})
	}

	pc.MaxRetries = intDefault(p["max_retries"], 3)
	if pc.MaxRetries < 0 {
		return nil, errf("providers[%d].max_retries must be >= 0", idx)
	}

	pc.KeyStrategy = lower(strVal(p["key_strategy"]))
	if pc.KeyStrategy == "" {
		pc.KeyStrategy = "fill-first"
	}
	if !validKeyStrategies[pc.KeyStrategy] {
		return nil, errf("providers[%d].key_strategy must be one of: fill-first, round-robin", idx)
	}

	// keys
	keysRaw, ok := p["keys"].([]any)
	if !ok || len(keysRaw) == 0 {
		return nil, errf("providers[%d] must contain at least one key in 'keys'", idx)
	}
	for j, kv := range keysRaw {
		km, err := asMap(kv, fmt.Sprintf("providers[%d].keys[%d]", idx, j))
		if err != nil {
			return nil, err
		}
		key, ok := km["key"].(string)
		if !ok || strings.TrimSpace(key) == "" {
			return nil, errf("providers[%d].keys[%d].key must not be empty", idx, j)
		}
		pc.Keys = append(pc.Keys, KeyConfig{Key: key})
	}

	// health_check_rules
	if v, ok := p["health_check_rules"]; ok {
		arr, ok := v.([]any)
		if !ok {
			return nil, errf("providers[%d].health_check_rules must be a list", idx)
		}
		for j, rv := range arr {
			rm, err := asMap(rv, fmt.Sprintf("providers[%d].health_check_rules[%d]", idx, j))
			if err != nil {
				return nil, err
			}
			rule, err := buildRule(rm, fmt.Sprintf("providers[%d].health_check_rules[%d]", idx, j))
			if err != nil {
				return nil, err
			}
			pc.HealthCheckRules = append(pc.HealthCheckRules, *rule)
		}
	}

	// proxy (optional per-provider override)
	if v, ok := p["proxy"]; ok && v != nil {
		pm, err := asMap(v, fmt.Sprintf("providers[%d].proxy", idx))
		if err != nil {
			return nil, err
		}
		pc.Proxy = &ProxyConfig{
			URL:      strings.TrimSpace(strVal(pm["url"])),
			Disabled: toBool(pm["disabled"]),
		}
	}

	return pc, nil
}

func buildRule(rm map[string]any, ctx string) (*HealthCheckRule, error) {
	action := lower(strVal(rm["action"]))
	if action == "" {
		action = "rotate"
	}
	if action != "rotate" {
		return nil, errf("%s.action must be 'rotate', got %q", ctx, action)
	}

	// http_status_codes: optional list of HTTP status codes to match.
	httpStatusCodes := []int{}
	if v, ok := rm["http_status_codes"]; ok && v != nil {
		arr, ok := v.([]any)
		if !ok {
			return nil, errf("%s.http_status_codes must be a list of integers", ctx)
		}
		for i, cv := range arr {
			code := intDefault(cv, 0)
			if code < 100 || code > 599 {
				return nil, errf("%s.http_status_codes[%d] must be a valid HTTP status code (100-599), got %v", ctx, i, cv)
			}
			httpStatusCodes = append(httpStatusCodes, code)
		}
	}

	matchType := lower(strVal(rm["match_type"]))
	if matchType == "" {
		matchType = "equals"
	}
	if !validMatchTypes[matchType] {
		return nil, errf("%s.match_type must be one of: equals, contains, regex", ctx)
	}
	matchValue := strVal(rm["match_value"])
	if matchValue == "" && len(httpStatusCodes) == 0 {
		matchValue = "quota_exceeded_error"
	}
	cooldown := intDefault(rm["cooldown_seconds"], 60)
	if cooldown < 0 {
		return nil, errf("%s.cooldown_seconds must be >= 0", ctx)
	}

	var compiled *regexp.Regexp
	if matchType == "regex" {
		var err error
		compiled, err = regexp.Compile(matchValue)
		if err != nil {
			return nil, errf("%s.match_value is not a valid regex: %v", ctx, err)
		}
	}

	models := []string{}
	if v, ok := rm["models"]; ok {
		arr, ok := v.([]any)
		if !ok {
			return nil, errf("%s.models must be a list", ctx)
		}
		for _, mv := range arr {
			s, ok := mv.(string)
			if ok {
				models = append(models, s)
			}
		}
	}

	jsonPath := strVal(rm["jsonpath"])
	if jsonPath == "" && len(httpStatusCodes) == 0 {
		jsonPath = "$.error.type" // default for backward compatibility
	}

	return &HealthCheckRule{
		Description:     strVal(rm["description"]),
		JSONPath:        jsonPath,
		MatchValue:      matchValue,
		MatchType:       matchType,
		Action:          action,
		CooldownSeconds: cooldown,
		Models:          models,
		HTTPStatusCodes: httpStatusCodes,
		regex:           compiled,
	}, nil
}

func buildCombo(c map[string]any, idx int, providerMap map[string]*ProviderConfig, comboNames map[string]bool) (*ComboConfig, error) {
	name := strings.TrimSpace(strVal(c["name"]))
	if name == "" {
		return nil, errf("combos[%d].name must not be empty", idx)
	}
	if comboNames[name] {
		return nil, errf("duplicate combo name: %q", name)
	}

	cc := &ComboConfig{Name: name}

	// aliases
	if v, ok := c["aliases"]; ok {
		aliases, err := asStringList(v, fmt.Sprintf("combos[%d].aliases", idx))
		if err != nil {
			return nil, err
		}
		for _, a := range aliases {
			a = strings.TrimSpace(a)
			if a == "" {
				return nil, errf("combos[%d].aliases entry must not be empty", idx)
			}
			if comboNames[a] {
				return nil, errf("combos[%d].aliases %q conflicts with an existing combo name or alias", idx, a)
			}
			comboNames[a] = true
			cc.Aliases = append(cc.Aliases, a)
		}
	}

	// api_format (string or list)
	rawFormats, err := asStringList(c["api_format"], fmt.Sprintf("combos[%d].api_format", idx))
	if err != nil {
		return nil, err
	}
	if len(rawFormats) == 0 {
		return nil, errf("combos[%d].api_format must be a non-empty string or list", idx)
	}
	formats := []string{}
	for _, f := range rawFormats {
		f = lower(f)
		if !validClientFormats[f] {
			return nil, errf("combos[%d].api_format %q is not a valid client-facing format (valid: openai, anthropic, openai-responses, openai-images)", idx, f)
		}
		formats = append(formats, f)
	}
	cc.APIFormat = formatsToConfig(formats)

	cc.Strategy = lower(strVal(c["strategy"]))
	if cc.Strategy == "" {
		cc.Strategy = "fill-first"
	}
	if !validKeyStrategies[cc.Strategy] {
		return nil, errf("combos[%d].strategy must be one of: fill-first, round-robin", idx)
	}

	// members
	membersRaw, ok := c["members"].([]any)
	if !ok || len(membersRaw) == 0 {
		return nil, errf("combos[%d] must contain at least one entry in 'members'", idx)
	}
	for j, mv := range membersRaw {
		mm, err := asMap(mv, fmt.Sprintf("combos[%d].members[%d]", idx, j))
		if err != nil {
			return nil, err
		}
		providerName := strings.TrimSpace(strVal(mm["provider"]))
		if providerName == "" {
			return nil, errf("combos[%d].members[%d].provider must not be empty", idx, j)
		}
		prov, ok := providerMap[providerName]
		if !ok {
			return nil, errf("combos[%d].members[%d].provider %q is not defined in providers", idx, j, providerName)
		}
		model := strings.TrimSpace(strVal(mm["model"]))
		if model == "" {
			return nil, errf("combos[%d].members[%d].model must not be empty", idx, j)
		}

		// upstream_api_format: optional hint for which upstream endpoint to prefer when the
		// provider does not natively support the client-facing format. At runtime the proxy
		// resolves the actual format with: native → hint → first endpoint.
		upstreamFmt := lower(strVal(mm["upstream_api_format"]))
		if upstreamFmt != "" {
			if !validAPIFormats[upstreamFmt] {
				return nil, errf("combos[%d].members[%d].upstream_api_format %q is not a known format", idx, j, upstreamFmt)
			}
			// The hint must actually exist on the provider (otherwise it's a typo).
			if !prov.SupportsFormat(upstreamFmt) {
				return nil, errf("combos[%d].members[%d].upstream_api_format %q is not available on provider %q", idx, j, upstreamFmt, providerName)
			}
		}
		// Provider must support at least one API format (validated elsewhere), so we always
		// have a fallback endpoint. No need to check each client format here — translation
		// covers the gap at request time.

		cc.Members = append(cc.Members, ComboMember{Provider: providerName, Model: model, UpstreamAPIFormat: upstreamFmt})
	}

	comboNames[name] = true
	return cc, nil
}

func formatsToConfig(f []string) any {
	if len(f) == 1 {
		return f[0]
	}
	out := make([]any, len(f))
	for i := range f {
		out[i] = f[i]
	}
	return out
}

func toBool(v any) bool { return toBoolDefault(v, false) }

func toBoolDefault(v any, def bool) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true")
	case int:
		return t != 0
	default:
		return def
	}
}

func intDefault(v any, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		var n int
		if _, err := fmt.Sscanf(t, "%d", &n); err == nil {
			return n
		}
	}
	return def
}

func strVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func stringDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
