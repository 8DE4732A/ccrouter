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
	case []string:
		return t, nil
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

	type parsedGroup struct {
		ownedBy string
		isDef   bool
		combos  []*ComboConfig
	}

	var groups []*parsedGroup
	groupMap := map[string]*parsedGroup{}

	for i, cv := range combosRaw {
		cm, err := asMap(cv, fmt.Sprintf("combos[%d]", i))
		if err != nil {
			return nil, err
		}

		// Check if this is a group (contains "combos" list)
		if innerCombosRaw, isGroup := cm["combos"].([]any); isGroup {
			ownedBy := strings.TrimSpace(strVal(cm["owned_by"]))
			if ownedBy == "" {
				ownedBy = "default"
			}
			if strings.Contains(ownedBy, "/") {
				return nil, errf("combos[%d].owned_by %q must not contain '/'", i, ownedBy)
			}
			isDef := toBool(cm["default"]) || toBool(cm["is_default"])
			if len(innerCombosRaw) == 0 {
				return nil, errf("combos[%d] group %q must contain at least one combo", i, ownedBy)
			}
			grp, exists := groupMap[ownedBy]
			if !exists {
				grp = &parsedGroup{ownedBy: ownedBy, isDef: isDef}
				groups = append(groups, grp)
				groupMap[ownedBy] = grp
			}
			if isDef {
				grp.isDef = true
			}

			groupComboNames := map[string]bool{}
			for _, existing := range grp.combos {
				groupComboNames[existing.Name] = true
				for _, a := range existing.Aliases {
					groupComboNames[a] = true
				}
			}

			for j, innerCV := range innerCombosRaw {
				innerCM, err := asMap(innerCV, fmt.Sprintf("combos[%d].combos[%d]", i, j))
				if err != nil {
					return nil, err
				}
				cc, err := buildCombo(innerCM, fmt.Sprintf("combos[%d].combos[%d]", i, j), providerMap, groupComboNames)
				if err != nil {
					return nil, err
				}
				cc.OwnedBy = ownedBy
				grp.combos = append(grp.combos, cc)
			}
		} else {
			// Flat combo
			ownedBy := strings.TrimSpace(strVal(cm["owned_by"]))
			if ownedBy == "" {
				ownedBy = "default"
			}
			if strings.Contains(ownedBy, "/") {
				return nil, errf("combos[%d].owned_by %q must not contain '/'", i, ownedBy)
			}
			isDef := toBool(cm["default"]) || toBool(cm["is_default"])

			grp, exists := groupMap[ownedBy]
			if !exists {
				grp = &parsedGroup{ownedBy: ownedBy, isDef: isDef}
				groups = append(groups, grp)
				groupMap[ownedBy] = grp
			}
			if isDef {
				grp.isDef = true
			}

			groupComboNames := map[string]bool{}
			for _, existing := range grp.combos {
				groupComboNames[existing.Name] = true
				for _, a := range existing.Aliases {
					groupComboNames[a] = true
				}
			}

			cc, err := buildCombo(cm, fmt.Sprintf("combos[%d]", i), providerMap, groupComboNames)
			if err != nil {
				return nil, err
			}
			cc.OwnedBy = ownedBy
			grp.combos = append(grp.combos, cc)
		}
	}

	// Resolve the default group
	defaultCount := 0
	for _, grp := range groups {
		if grp.isDef {
			defaultCount++
		}
	}
	if defaultCount > 1 {
		return nil, errf("multiple default combo groups configured")
	}
	if defaultCount == 0 {
		foundDefault := false
		for _, grp := range groups {
			if strings.EqualFold(grp.ownedBy, "default") {
				grp.isDef = true
				foundDefault = true
				break
			}
		}
		if !foundDefault && len(groups) > 0 {
			groups[0].isDef = true
		}
	}

	// Validate full IDs across all groups and assign to cfg.Combos
	fullModelNames := map[string]bool{}
	for _, grp := range groups {
		for _, cc := range grp.combos {
			cc.OwnedBy = grp.ownedBy
			cc.IsDefault = grp.isDef

			fullPrimary := cc.FullName()
			if fullModelNames[fullPrimary] {
				return nil, errf("duplicate model identifier %q across groups", fullPrimary)
			}
			fullModelNames[fullPrimary] = true

			for _, fullAlias := range cc.FullAliases() {
				if fullModelNames[fullAlias] {
					return nil, errf("duplicate model identifier %q across groups", fullAlias)
				}
				fullModelNames[fullAlias] = true
			}

			cfg.Combos = append(cfg.Combos, *cc)
		}
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
		// admin_password
		if ap, ok := gm["admin_password"]; ok {
			cfg.General.AdminPassword = strings.TrimSpace(strVal(ap))
		}
		// external_url (or base_url)
		if eu, ok := gm["external_url"]; ok {
			cfg.General.ExternalURL = strings.TrimRight(strings.TrimSpace(strVal(eu)), "/")
		} else if bu, ok := gm["base_url"]; ok {
			cfg.General.ExternalURL = strings.TrimRight(strings.TrimSpace(strVal(bu)), "/")
		}
	}

	// ---- logging / verbose_logging ----
	cfg.Logging = LoggingConfig{
		Dir:              "logs",
		MaxFileSizeMB:    20,
		MaxBackups:       10,
		CompressionLevel: "best",
	}

	if logRaw, ok := raw["logging"].(map[string]any); ok {
		if v, ok := logRaw["enabled"]; ok {
			cfg.Logging.Enabled = toBool(v)
		}
		if d := strings.TrimSpace(strVal(logRaw["dir"])); d != "" {
			cfg.Logging.Dir = d
		}
		if sz, ok := logRaw["max_file_size_mb"]; ok {
			s := intDefault(sz, 0)
			if s <= 0 {
				return nil, errf("logging.max_file_size_mb must be > 0")
			}
			cfg.Logging.MaxFileSizeMB = s
		}
		if bk, ok := logRaw["max_backups"]; ok {
			b := intDefault(bk, 0)
			if b <= 0 {
				return nil, errf("logging.max_backups must be > 0")
			}
			cfg.Logging.MaxBackups = b
		}
		if lvlRaw, ok := logRaw["compression_level"]; ok {
			lvl := strings.ToLower(strings.TrimSpace(strVal(lvlRaw)))
			if lvl != "fastest" && lvl != "default" && lvl != "better" && lvl != "best" {
				return nil, errf("logging.compression_level must be one of: fastest, default, better, best")
			}
			cfg.Logging.CompressionLevel = lvl
		}
	}

	if v, ok := raw["verbose_logging"]; ok {
		var hasLoggingEnabled bool
		if lm, ok := raw["logging"].(map[string]any); ok {
			_, hasLoggingEnabled = lm["enabled"]
		}
		if !hasLoggingEnabled {
			cfg.Logging.Enabled = toBool(v)
		}
	}
	cfg.VerboseLogging = cfg.Logging.Enabled

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

	// ---- mcp_providers ----
	mcpProviderNames := map[string]bool{}
	if v, ok := raw["mcp_providers"]; ok && v != nil {
		arr, ok := v.([]any)
		if !ok {
			return nil, errf("'mcp_providers' must be a list")
		}
		for i, pv := range arr {
			pm, err := asMap(pv, fmt.Sprintf("mcp_providers[%d]", i))
			if err != nil {
				return nil, err
			}
			p, err := buildMcpProvider(pm, i, mcpProviderNames)
			if err != nil {
				return nil, err
			}
			cfg.McpProviders = append(cfg.McpProviders, *p)
		}
	}

	// ---- mcp_combos ----
	mcpComboNames := map[string]bool{}
	if v, ok := raw["mcp_combos"]; ok && v != nil {
		arr, ok := v.([]any)
		if !ok {
			return nil, errf("'mcp_combos' must be a list")
		}
		for i, cv := range arr {
			cm, err := asMap(cv, fmt.Sprintf("mcp_combos[%d]", i))
			if err != nil {
				return nil, err
			}
			c, err := buildMcpCombo(cm, i, mcpComboNames, mcpProviderNames)
			if err != nil {
				return nil, err
			}
			cfg.McpCombos = append(cfg.McpCombos, *c)
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
			return nil, errf("providers[%d].api[%d].api_format must be one of: openai, anthropic, openai-responses, openai-images, openai-image-edits, openai-embeddings, gemini", idx, k)
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

func buildCombo(c map[string]any, ctx string, providerMap map[string]*ProviderConfig, comboNames map[string]bool) (*ComboConfig, error) {
	name := strings.TrimSpace(strVal(c["name"]))
	if name == "" {
		return nil, errf("%s.name must not be empty", ctx)
	}
	if strings.Contains(name, "/") {
		return nil, errf("%s.name %q must not contain '/'", ctx, name)
	}
	if comboNames[name] {
		return nil, errf("duplicate combo name: %q in %s", name, ctx)
	}

	cc := &ComboConfig{Name: name}

	// aliases
	if v, ok := c["aliases"]; ok {
		aliases, err := asStringList(v, fmt.Sprintf("%s.aliases", ctx))
		if err != nil {
			return nil, err
		}
		for _, a := range aliases {
			a = strings.TrimSpace(a)
			if a == "" {
				return nil, errf("%s.aliases entry must not be empty", ctx)
			}
			if strings.Contains(a, "/") {
				return nil, errf("%s.aliases %q must not contain '/'", ctx, a)
			}
			if comboNames[a] {
				return nil, errf("%s.aliases %q conflicts with an existing combo name or alias", ctx, a)
			}
			comboNames[a] = true
			cc.Aliases = append(cc.Aliases, a)
		}
	}

	// api_format (string or list)
	rawFormats, err := asStringList(c["api_format"], fmt.Sprintf("%s.api_format", ctx))
	if err != nil {
		return nil, err
	}
	if len(rawFormats) == 0 {
		return nil, errf("%s.api_format must be a non-empty string or list", ctx)
	}
	formats := []string{}
	for _, f := range rawFormats {
		f = lower(f)
		if !validClientFormats[f] {
			return nil, errf("%s.api_format %q is not a valid client-facing format (valid: openai, anthropic, openai-responses, openai-images, openai-image-edits, openai-embeddings, gemini)", ctx, f)
		}
		formats = append(formats, f)
	}
	cc.APIFormat = formatsToConfig(formats)

	cc.Strategy = lower(strVal(c["strategy"]))
	if cc.Strategy == "" {
		cc.Strategy = "fill-first"
	}
	if !validKeyStrategies[cc.Strategy] {
		return nil, errf("%s.strategy must be one of: fill-first, round-robin", ctx)
	}

	// members
	membersRaw, ok := c["members"].([]any)
	if !ok || len(membersRaw) == 0 {
		return nil, errf("%s must contain at least one entry in 'members'", ctx)
	}
	for j, mv := range membersRaw {
		mm, err := asMap(mv, fmt.Sprintf("%s.members[%d]", ctx, j))
		if err != nil {
			return nil, err
		}
		providerName := strings.TrimSpace(strVal(mm["provider"]))
		if providerName == "" {
			return nil, errf("%s.members[%d].provider must not be empty", ctx, j)
		}
		prov, ok := providerMap[providerName]
		if !ok {
			return nil, errf("%s.members[%d].provider %q is not defined in providers", ctx, j, providerName)
		}
		model := strings.TrimSpace(strVal(mm["model"]))
		if model == "" {
			return nil, errf("%s.members[%d].model must not be empty", ctx, j)
		}

		// upstream_api_format: optional hint for which upstream endpoint to prefer when the
		// provider does not natively support the client-facing format. At runtime the proxy
		// resolves the actual format with: native → hint → first endpoint.
		upstreamFmt := lower(strVal(mm["upstream_api_format"]))
		if upstreamFmt != "" {
			if !validAPIFormats[upstreamFmt] {
				return nil, errf("%s.members[%d].upstream_api_format %q is not a known format", ctx, j, upstreamFmt)
			}
			// The hint must actually exist on the provider (otherwise it's a typo).
			if !prov.SupportsFormat(upstreamFmt) {
				return nil, errf("%s.members[%d].upstream_api_format %q is not available on provider %q", ctx, j, upstreamFmt, providerName)
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

var knownMcpProviderKeys = map[string]bool{
	"name": true, "transport": true, "command": true, "args": true,
	"env": true, "working_dir": true, "url": true, "headers": true,
	"timeout_seconds": true, "auth_mode": true, "auth": true, "proxy": true,
}

var knownMcpAuthKeys = map[string]bool{
	"mode": true, "type": true, "isolated": true, "api_key": true,
	"header": true, "header_name": true, "header_value": true, "key": true,
	"client_id": true, "client_secret": true,
	"auth_url": true, "authorization_url": true, "token_url": true,
	"scopes": true, "redirect_url": true,
}

var knownMcpComboKeys = map[string]bool{
	"name": true, "owned_by": true, "description": true,
	"user_id_header": true, "members": true,
}

var knownMcpComboMemberKeys = map[string]bool{
	"provider": true, "prefix": true, "tools": true,
	"resources": true, "prompts": true,
}

func buildMcpProvider(pm map[string]any, idx int, providerNames map[string]bool) (*McpProviderConfig, error) {
	for k := range pm {
		if !knownMcpProviderKeys[k] {
			return nil, errf("mcp_providers[%d]: unknown key %q", idx, k)
		}
	}

	name := strings.TrimSpace(strVal(pm["name"]))
	if name == "" {
		return nil, errf("mcp_providers[%d].name must not be empty", idx)
	}
	if providerNames[name] {
		return nil, errf("duplicate mcp provider name %q", name)
	}
	transport := lower(strVal(pm["transport"]))
	if transport != "stdio" && transport != "sse" && transport != "streamablehttp" {
		return nil, errf("mcp_providers[%d].transport must be one of: stdio, sse, streamablehttp", idx)
	}

	timeoutSec := 30
	if tv, ok := pm["timeout_seconds"]; ok {
		timeoutSec = intDefault(tv, 30)
		if timeoutSec <= 0 {
			return nil, errf("mcp_providers[%d].timeout_seconds must be > 0, got %d", idx, timeoutSec)
		}
	}

	p := &McpProviderConfig{
		Name:           name,
		Transport:      transport,
		TimeoutSeconds: timeoutSec,
	}

	if transport == "stdio" {
		cmd := strings.TrimSpace(strVal(pm["command"]))
		if cmd == "" {
			return nil, errf("mcp_providers[%d].command is required for stdio transport", idx)
		}
		p.Command = cmd
		if av, ok := pm["args"]; ok {
			args, err := asStringList(av, fmt.Sprintf("mcp_providers[%d].args", idx))
			if err != nil {
				return nil, err
			}
			p.Args = args
		}
		if ev, ok := pm["env"].(map[string]any); ok {
			env := make(map[string]string, len(ev))
			for k, v := range ev {
				env[k] = strVal(v)
			}
			p.Env = env
		}
		p.WorkingDir = strings.TrimSpace(strVal(pm["working_dir"]))
	} else {
		// sse or streamablehttp
		u := strings.TrimSpace(strVal(pm["url"]))
		if u == "" {
			return nil, errf("mcp_providers[%d].url is required for %s transport", idx, transport)
		}
		p.URL = u
		if hv, ok := pm["headers"].(map[string]any); ok {
			headers := make(map[string]string, len(hv))
			for k, v := range hv {
				headers[k] = strVal(v)
			}
			p.Headers = headers
		}
	}

	authMode := lower(strVal(pm["auth_mode"]))
	var av map[string]any
	if rawAuth, ok := pm["auth"]; ok && rawAuth != nil {
		authMap, err := asMap(rawAuth, fmt.Sprintf("mcp_providers[%d].auth", idx))
		if err != nil {
			return nil, err
		}
		av = authMap
		for k := range av {
			if !knownMcpAuthKeys[k] {
				return nil, errf("mcp_providers[%d].auth: unknown key %q", idx, k)
			}
		}
		// If auth.isolated is specified, it takes precedence
		if isVal, hasIsolated := av["isolated"]; hasIsolated {
			if toBool(isVal) {
				authMode = "isolated"
			} else {
				authMode = "shared"
			}
		}
	}

	if authMode == "" {
		authMode = "shared"
	}
	if authMode != "shared" && authMode != "isolated" {
		return nil, errf("mcp_providers[%d].auth_mode must be 'shared' or 'isolated'", idx)
	}
	p.AuthMode = authMode

	if len(av) > 0 {
		authModeType := lower(strVal(av["mode"]))
		if authModeType == "" {
			authModeType = lower(strVal(av["type"]))
		}
		if authModeType == "" {
			authModeType = "none"
		}
		if authModeType != "none" && authModeType != "api_key" && authModeType != "oauth2" {
			return nil, errf("mcp_providers[%d].auth.mode must be one of: none, api_key, oauth2, got %q", idx, authModeType)
		}

		// Validation (#9): oauth2 must be used with isolated auth mode
		if authModeType == "oauth2" && authMode != "isolated" {
			return nil, errf("mcp_providers[%d].auth: oauth2 authentication requires isolated=true (or auth_mode: \"isolated\")", idx)
		}

		ac := &McpAuthConfig{
			Mode:     authModeType,
			Type:     authModeType,
			Isolated: (authMode == "isolated"),
		}
		switch authModeType {
		case "api_key":
			key := strings.TrimSpace(strVal(av["api_key"]))
			if key == "" {
				key = strings.TrimSpace(strVal(av["header_value"]))
			}
			if key == "" {
				key = strings.TrimSpace(strVal(av["key"]))
			}
			if key == "" {
				return nil, errf("mcp_providers[%d].auth.api_key must not be empty", idx)
			}
			ac.APIKey = key
			hdr := strings.TrimSpace(strVal(av["header"]))
			if hdr == "" {
				hdr = strings.TrimSpace(strVal(av["header_name"]))
			}
			ac.Header = stringDefault(hdr, "Authorization")
		case "oauth2":
			clientID := strings.TrimSpace(strVal(av["client_id"]))
			if clientID == "" {
				return nil, errf("mcp_providers[%d].auth.client_id must not be empty", idx)
			}
			ac.ClientID = clientID
			ac.ClientSecret = strings.TrimSpace(strVal(av["client_secret"]))

			authURL := strings.TrimSpace(strVal(av["auth_url"]))
			if authURL == "" {
				authURL = strings.TrimSpace(strVal(av["authorization_url"]))
			}
			if authURL == "" {
				return nil, errf("mcp_providers[%d].auth.auth_url must not be empty", idx)
			}
			ac.AuthURL = authURL
			ac.AuthorizationURL = authURL

			tokenURL := strings.TrimSpace(strVal(av["token_url"]))
			if tokenURL == "" {
				return nil, errf("mcp_providers[%d].auth.token_url must not be empty", idx)
			}
			ac.TokenURL = tokenURL

			if sv, ok := av["scopes"]; ok {
				scopes, err := asStringList(sv, fmt.Sprintf("mcp_providers[%d].auth.scopes", idx))
				if err != nil {
					return nil, err
				}
				ac.Scopes = scopes
			}
			ac.RedirectURL = strings.TrimSpace(strVal(av["redirect_url"]))
		}
		p.Auth = ac
	}

	if pv, ok := pm["proxy"]; ok && pv != nil {
		pxm, err := asMap(pv, fmt.Sprintf("mcp_providers[%d].proxy", idx))
		if err != nil {
			return nil, err
		}
		p.Proxy = &ProxyConfig{
			URL:      strings.TrimSpace(strVal(pxm["url"])),
			Disabled: toBool(pxm["disabled"]),
		}
	}

	providerNames[name] = true
	return p, nil
}

func buildMcpCombo(cm map[string]any, idx int, comboNames map[string]bool, mcpProviderNames map[string]bool) (*McpComboConfig, error) {
	for k := range cm {
		if !knownMcpComboKeys[k] {
			return nil, errf("mcp_combos[%d]: unknown key %q", idx, k)
		}
	}

	name := strings.TrimSpace(strVal(cm["name"]))
	if name == "" {
		return nil, errf("mcp_combos[%d].name must not be empty", idx)
	}
	if strings.Contains(name, "/") {
		return nil, errf("mcp_combos[%d].name %q must not contain '/'", idx, name)
	}
	if comboNames[name] {
		return nil, errf("duplicate mcp combo name %q", name)
	}

	cc := &McpComboConfig{
		Name:         name,
		OwnedBy:      strings.TrimSpace(strVal(cm["owned_by"])),
		Description:  strings.TrimSpace(strVal(cm["description"])),
		UserIDHeader: stringDefault(strings.TrimSpace(strVal(cm["user_id_header"])), "X-User-Id"),
	}

	membersRaw, ok := cm["members"].([]any)
	if !ok || len(membersRaw) == 0 {
		return nil, errf("mcp_combos[%d] must contain at least one member", idx)
	}

	for mi, mv := range membersRaw {
		mm, err := asMap(mv, fmt.Sprintf("mcp_combos[%d].members[%d]", idx, mi))
		if err != nil {
			return nil, err
		}
		for k := range mm {
			if !knownMcpComboMemberKeys[k] {
				return nil, errf("mcp_combos[%d].members[%d]: unknown key %q", idx, mi, k)
			}
		}

		prov := strings.TrimSpace(strVal(mm["provider"]))
		if prov == "" {
			return nil, errf("mcp_combos[%d].members[%d].provider must not be empty", idx, mi)
		}
		if !mcpProviderNames[prov] {
			return nil, errf("mcp_combos[%d].members[%d]: unknown provider %q", idx, mi, prov)
		}

		member := McpComboMemberConfig{
			Provider: prov,
			Prefix:   strings.TrimSpace(strVal(mm["prefix"])),
		}

		if tv, ok := mm["tools"]; ok {
			tools, err := asStringList(tv, fmt.Sprintf("mcp_combos[%d].members[%d].tools", idx, mi))
			if err != nil {
				return nil, err
			}
			member.Tools = tools
		} else {
			member.Tools = []string{"*"}
		}

		if rv, ok := mm["resources"]; ok {
			res, err := asStringList(rv, fmt.Sprintf("mcp_combos[%d].members[%d].resources", idx, mi))
			if err != nil {
				return nil, err
			}
			member.Resources = res
		} else {
			member.Resources = []string{"*"}
		}

		if pv, ok := mm["prompts"]; ok {
			prompts, err := asStringList(pv, fmt.Sprintf("mcp_combos[%d].members[%d].prompts", idx, mi))
			if err != nil {
				return nil, err
			}
			member.Prompts = prompts
		} else {
			member.Prompts = []string{"*"}
		}

		cc.Members = append(cc.Members, member)
	}

	comboNames[name] = true
	return cc, nil
}
