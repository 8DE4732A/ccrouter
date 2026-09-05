package config

import (
	"os"
	"path/filepath"
	"testing"
)

const minimal = `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
    health_check_rules:
      - jsonpath: "$.error.type"
        match_value: "quota_exceeded_error"
        match_type: equals
        action: rotate
        cooldown_seconds: 60
        models: []
combos:
  - name: my-combo
    api_format: openai
    strategy: fill-first
    members:
      - provider: sn
        model: gpt-4o
`

const minimalDual = `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
      - api_format: anthropic
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
    health_check_rules: []
combos:
  - name: dual-combo
    api_format:
      - openai
      - anthropic
    strategy: fill-first
    members:
      - provider: sn
        model: my-model
`

func loadFromText(t *testing.T, text string) *AppConfig {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return cfg
}

func TestLoadsMinimalValid(t *testing.T) {
	cfg := loadFromText(t, minimal)
	if len(cfg.Providers) != 1 || cfg.Providers[0].Name != "sn" {
		t.Fatal("expected 1 provider named sn")
	}
	if cfg.Providers[0].APIs[0].APIFormat != "openai" {
		t.Fatal("expected openai format")
	}
	if len(cfg.Combos) != 1 || cfg.Combos[0].Name != "my-combo" {
		t.Fatal("expected 1 combo named my-combo")
	}
}

func TestProviderChatURLOpenAI(t *testing.T) {
	cfg := loadFromText(t, minimal)
	if got := cfg.Providers[0].GetChatURL("openai"); got != "https://upstream.test/v1/chat/completions" {
		t.Fatalf("unexpected url: %s", got)
	}
}

func TestProviderChatURLAnthropic(t *testing.T) {
	cfg := loadFromText(t, `
providers:
  - name: sn-ant
    api:
      - api_format: anthropic
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
    health_check_rules: []
combos:
  - name: ant-combo
    api_format: anthropic
    members:
      - provider: sn-ant
        model: claude-3
`)
	if got := cfg.Providers[0].GetChatURL("anthropic"); got != "https://upstream.test/v1/messages" {
		t.Fatalf("unexpected url: %s", got)
	}
}

func TestProviderDualFormat(t *testing.T) {
	cfg := loadFromText(t, minimalDual)
	p := &cfg.Providers[0]
	if !p.SupportsFormat("openai") || !p.SupportsFormat("anthropic") {
		t.Fatal("expected openai and anthropic support")
	}
	if got := p.GetChatURL("openai"); got != "https://upstream.test/v1/chat/completions" {
		t.Fatalf("unexpected openai url: %s", got)
	}
	if got := p.GetChatURL("anthropic"); got != "https://upstream.test/v1/messages" {
		t.Fatalf("unexpected anthropic url: %s", got)
	}
}

func TestComboDualAPIFormats(t *testing.T) {
	cfg := loadFromText(t, minimalDual)
	got := cfg.Combos[0].APIFormats()
	if len(got) != 2 || got[0] != "openai" || got[1] != "anthropic" {
		t.Fatalf("unexpected formats: %v", got)
	}
}

func TestComboSingleAPIFormatAsString(t *testing.T) {
	cfg := loadFromText(t, minimal)
	got := cfg.Combos[0].APIFormats()
	if len(got) != 1 || got[0] != "openai" {
		t.Fatalf("unexpected formats: %v", got)
	}
}

func TestProviderEmbeddingsFormat(t *testing.T) {
	const embYAML = `
providers:
  - name: sn-emb
    api:
      - api_format: openai-embeddings
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-emb
    health_check_rules: []
combos:
  - name: emb-combo
    api_format: openai-embeddings
    members:
      - provider: sn-emb
        model: text-embedding-3-small
`
	cfg := loadFromText(t, embYAML)
	p := &cfg.Providers[0]
	if !p.SupportsFormat("openai-embeddings") {
		t.Fatal("expected openai-embeddings support")
	}
	if got := p.GetChatURL("openai-embeddings"); got != "https://upstream.test/v1/embeddings" {
		t.Fatalf("unexpected embeddings url: %s", got)
	}
	c := &cfg.Combos[0]
	if got := c.APIFormats(); len(got) != 1 || got[0] != "openai-embeddings" {
		t.Fatalf("unexpected combo formats: %v", got)
	}
}

func TestImageEditsFormat(t *testing.T) {
	const imgYAML = `
providers:
  - name: sn-img
    api:
      - api_format: openai-images
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-img
    health_check_rules: []
  - name: sn-edits
    api:
      - api_format: openai-image-edits
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-edits
    health_check_rules: []
combos:
  - name: img-combo
    api_format: openai-images
    members:
      - provider: sn-img
        model: dall-e-2
  - name: edits-combo
    api_format: openai-image-edits
    members:
      - provider: sn-edits
        model: dall-e-2
`
	cfg := loadFromText(t, imgYAML)
	pImg := &cfg.Providers[0]
	// Auto-inherited from openai-images
	if !pImg.SupportsFormat("openai-image-edits") {
		t.Fatal("expected openai-image-edits auto-inherited support for provider")
	}
	if got := pImg.GetChatURL("openai-image-edits"); got != "https://upstream.test/v1/images/edits" {
		t.Fatalf("unexpected edits url: %s", got)
	}

	pEdits := &cfg.Providers[1]
	// Explicit openai-image-edits
	if !pEdits.SupportsFormat("openai-image-edits") {
		t.Fatal("expected explicit openai-image-edits support for provider")
	}
	if got := pEdits.GetChatURL("openai-image-edits"); got != "https://upstream.test/v1/images/edits" {
		t.Fatalf("unexpected edits url: %s", got)
	}

	cImg := &cfg.Combos[0]
	// Combo auto-inherited support
	if !cImg.SupportsFormat("openai-image-edits") {
		t.Fatal("expected openai-image-edits auto-inherited support for combo")
	}

	cEdits := &cfg.Combos[1]
	// Combo explicit support
	if !cEdits.SupportsFormat("openai-image-edits") {
		t.Fatal("expected explicit openai-image-edits support for combo")
	}
}


func mustReject(t *testing.T, text string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(path, []byte(text), 0o644)
	if _, err := Load(path); err == nil {
		t.Fatalf("expected error, got none")
	}
}

func mustAccept(t *testing.T, text string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(path, []byte(text), 0o644)
	if _, err := Load(path); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestRejectsUnknownAPIFormatInProvider(t *testing.T) {
	mustReject(t, `
providers:
  - name: sn
    api:
      - api_format: grpc
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
    health_check_rules: []
combos:
  - name: c
    api_format: openai
    members:
      - provider: sn
        model: m
`)
}

// TestComboFormatUnsupportedByProviderIsAllowedWithTranslation verifies that a combo
// can declare a client-facing format the provider doesn't natively support — translation
// handles the gap at request time via CLIProxyAPI translator.
func TestComboFormatUnsupportedByProviderIsAllowedWithTranslation(t *testing.T) {
	mustAccept(t, `
providers:
  - name: sn-openai
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
    health_check_rules: []
combos:
  - name: c
    api_format: anthropic
    members:
      - provider: sn-openai
        model: m
`)
}

func TestRejectsUndefinedProviderInCombo(t *testing.T) {
	mustReject(t, `
providers:
  - name: real-provider
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
    health_check_rules: []
combos:
  - name: c
    api_format: openai
    members:
      - provider: ghost-provider
        model: m
`)
}

func TestRejectsInvalidRegex(t *testing.T) {
	mustReject(t, `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
    health_check_rules:
      - jsonpath: "$.error.type"
        match_value: "([invalid"
        match_type: regex
        cooldown_seconds: 60
combos:
  - name: c
    api_format: openai
    members:
      - provider: sn
        model: m
`)
}

func TestRejectsDuplicateProviderName(t *testing.T) {
	mustReject(t, `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-2
combos:
  - name: c
    api_format: openai
    members:
      - provider: sn
        model: m
`)
}

func TestDumpRoundtrip(t *testing.T) {
	cfg := loadFromText(t, minimal)
	dumped := Dump(cfg)
	if len(dumped["providers"].([]any)) != 1 {
		t.Fatal("expected 1 provider in dump")
	}
	// Re-build from dumped map must succeed.
	rebuilt, err := Build(dumped)
	if err != nil {
		t.Fatalf("rebuild from dump: %v", err)
	}
	if rebuilt.Providers[0].Name != "sn" {
		t.Fatal("expected sn")
	}
}

func TestVerboseLoggingDefaultFalse(t *testing.T) {
	cfg := loadFromText(t, minimal)
	if cfg.VerboseLogging {
		t.Fatal("expected verbose_logging false by default")
	}
	if cfg.Logging.Enabled {
		t.Fatal("expected logging.enabled false by default")
	}
	if cfg.Logging.Dir != "logs" || cfg.Logging.MaxFileSizeMB != 20 || cfg.Logging.MaxBackups != 10 || cfg.Logging.CompressionLevel != "best" {
		t.Fatalf("unexpected logging defaults: %+v", cfg.Logging)
	}
}

func TestLoggingConfigLegacyVerboseLogging(t *testing.T) {
	const raw = `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
    health_check_rules: []
combos:
  - name: c
    api_format: openai
    members:
      - provider: sn
        model: m
verbose_logging: true
`
	cfg := loadFromText(t, raw)
	if !cfg.VerboseLogging || !cfg.Logging.Enabled {
		t.Fatalf("expected both VerboseLogging and Logging.Enabled to be true, got %v / %v", cfg.VerboseLogging, cfg.Logging.Enabled)
	}
}

func TestLoggingConfigFull(t *testing.T) {
	const raw = `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
    health_check_rules: []
combos:
  - name: c
    api_format: openai
    members:
      - provider: sn
        model: m
logging:
  enabled: true
  dir: "/var/log/ccrouter"
  max_file_size_mb: 50
  max_backups: 20
  compression_level: "better"
`
	cfg := loadFromText(t, raw)
	if !cfg.Logging.Enabled || !cfg.VerboseLogging {
		t.Fatal("expected logging enabled")
	}
	if cfg.Logging.Dir != "/var/log/ccrouter" {
		t.Fatalf("unexpected dir: %s", cfg.Logging.Dir)
	}
	if cfg.Logging.MaxFileSizeMB != 50 {
		t.Fatalf("unexpected max_file_size_mb: %d", cfg.Logging.MaxFileSizeMB)
	}
	if cfg.Logging.MaxBackups != 20 {
		t.Fatalf("unexpected max_backups: %d", cfg.Logging.MaxBackups)
	}
	if cfg.Logging.CompressionLevel != "better" {
		t.Fatalf("unexpected compression_level: %s", cfg.Logging.CompressionLevel)
	}

	d := Dump(cfg)
	rebuilt, err := Build(d)
	if err != nil {
		t.Fatalf("rebuild failed: %v", err)
	}
	if rebuilt.Logging.Dir != "/var/log/ccrouter" || rebuilt.Logging.MaxFileSizeMB != 50 || rebuilt.Logging.MaxBackups != 20 || rebuilt.Logging.CompressionLevel != "better" || !rebuilt.Logging.Enabled {
		t.Fatalf("roundtrip mismatch: %+v", rebuilt.Logging)
	}
}

func TestLoggingConfigValidation(t *testing.T) {
	mustReject(t, `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
combos:
  - name: c
    api_format: openai
    members:
      - provider: sn
        model: m
logging:
  max_file_size_mb: 0
`)

	mustReject(t, `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
combos:
  - name: c
    api_format: openai
    members:
      - provider: sn
        model: m
logging:
  max_backups: -1
`)

	mustReject(t, `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
combos:
  - name: c
    api_format: openai
    members:
      - provider: sn
        model: m
logging:
  compression_level: "invalid_level"
`)
}

func TestResolveLogDir(t *testing.T) {
	// Absolute path
	if got := ResolveLogDir("/a/b/config.yaml", "/var/log/ccrouter"); got != "/var/log/ccrouter" {
		t.Fatalf("expected /var/log/ccrouter, got %s", got)
	}

	// Relative path with configPath
	if got := ResolveLogDir("/a/b/config.yaml", "logs"); got != "/a/b/logs" {
		t.Fatalf("expected /a/b/logs, got %s", got)
	}

	// Empty dir defaults to logs
	if got := ResolveLogDir("/a/b/config.yaml", ""); got != "/a/b/logs" {
		t.Fatalf("expected /a/b/logs for empty dir, got %s", got)
	}
}

func TestTestDirWritable(t *testing.T) {
	dir := t.TempDir()
	validDir := filepath.Join(dir, "sub", "logdir")
	if err := TestDirWritable(validDir); err != nil {
		t.Fatalf("expected valid dir to be writable, got: %v", err)
	}

	// Create a regular file, and try to use it as a directory -> should fail
	filePath := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	invalidDir := filepath.Join(filePath, "cannot_create_inside_file")
	if err := TestDirWritable(invalidDir); err == nil {
		t.Fatal("expected error when trying to create dir inside a file, got nil")
	}
}


// TestRequestTimeoutHoursRoundtrip verifies the request_timeout_seconds field is
// parsed, validated, and round-tripped through Dump/Build.
func TestRequestTimeoutRoundtrip(t *testing.T) {
	raw := `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
    health_check_rules: []
combos:
  - name: c
    api_format: openai
    members:
      - provider: sn
        model: m
general:
  request_timeout_seconds: 900
`
	cfg := loadFromText(t, raw)
	if cfg.General.RequestTimeoutSeconds != 900 {
		t.Fatalf("expected request_timeout_seconds 900, got %d", cfg.General.RequestTimeoutSeconds)
	}
	d := Dump(cfg)
	general := d["general"].(map[string]any)
	if general["request_timeout_seconds"] != 900 {
		t.Fatalf("expected request_timeout_seconds 900 in dump, got %v", general["request_timeout_seconds"])
	}
	rebuilt, err := Build(d)
	if err != nil {
		t.Fatalf("rebuild from dump: %v", err)
	}
	if rebuilt.General.RequestTimeoutSeconds != 900 {
		t.Fatalf("expected 900 after rebuild, got %d", rebuilt.General.RequestTimeoutSeconds)
	}
}

// TestRequestTimeoutUnsetDefaultsZero verifies that an unset timeout stays 0
// (the caller applies the default at runtime).
func TestRequestTimeoutUnsetDefaultsZero(t *testing.T) {
	cfg := loadFromText(t, minimal)
	if cfg.General.RequestTimeoutSeconds != 0 {
		t.Fatalf("expected 0 when unset, got %d", cfg.General.RequestTimeoutSeconds)
	}
}

// TestRejectsInvalidRequestTimeout verifies a negative timeout is rejected.
func TestRejectsInvalidRequestTimeout(t *testing.T) {
	mustReject(t, `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
    health_check_rules: []
combos:
  - name: c
    api_format: openai
    members:
      - provider: sn
        model: m
general:
  request_timeout_seconds: -5
`)
}

// TestRequestTimeoutZeroMeansDisabled verifies an explicit 0 disables the
// timeout (represented internally as RequestTimeoutDisabled) and round-trips
// back to 0 through Dump/Build.
func TestRequestTimeoutZeroMeansDisabled(t *testing.T) {
	raw := `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
    health_check_rules: []
combos:
  - name: c
    api_format: openai
    members:
      - provider: sn
        model: m
general:
  request_timeout_seconds: 0
`
	cfg := loadFromText(t, raw)
	if cfg.General.RequestTimeoutSeconds != RequestTimeoutDisabled {
		t.Fatalf("expected RequestTimeoutDisabled sentinel, got %d", cfg.General.RequestTimeoutSeconds)
	}
	d := Dump(cfg)
	general := d["general"].(map[string]any)
	if general["request_timeout_seconds"] != 0 {
		t.Fatalf("expected request_timeout_seconds 0 in dump, got %v", general["request_timeout_seconds"])
	}
	rebuilt, err := Build(d)
	if err != nil {
		t.Fatalf("rebuild from dump: %v", err)
	}
	if rebuilt.General.RequestTimeoutSeconds != RequestTimeoutDisabled {
		t.Fatalf("expected RequestTimeoutDisabled after rebuild, got %d", rebuilt.General.RequestTimeoutSeconds)
	}
}

// TestHTTPStatusCodeRuleRoundtrip verifies http_status_codes is parsed, dumped,
// and rebuilt correctly.
func TestHTTPStatusCodeRuleRoundtrip(t *testing.T) {
	raw := `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
    health_check_rules:
      - description: any 429 or 5xx
        http_status_codes: [429, 500, 502, 503]
        action: rotate
        cooldown_seconds: 60
combos:
  - name: c
    api_format: openai
    members:
      - provider: sn
        model: m
`
	cfg := loadFromText(t, raw)
	rule := cfg.Providers[0].HealthCheckRules[0]
	if len(rule.HTTPStatusCodes) != 4 || rule.HTTPStatusCodes[0] != 429 {
		t.Fatalf("expected http_status_codes [429 500 502 503], got %v", rule.HTTPStatusCodes)
	}
	// jsonpath should stay empty when only status codes are configured.
	if rule.JSONPath != "" {
		t.Fatalf("expected empty jsonpath, got %q", rule.JSONPath)
	}
	d := Dump(cfg)
	ruleDump := d["providers"].([]any)[0].(map[string]any)["health_check_rules"].([]any)[0].(map[string]any)
	codesAny, ok := ruleDump["http_status_codes"].([]any)
	if !ok || len(codesAny) != 4 {
		t.Fatalf("expected http_status_codes in dump, got %T %v", ruleDump["http_status_codes"], ruleDump["http_status_codes"])
	}
	rebuilt, err := Build(d)
	if err != nil {
		t.Fatalf("rebuild from dump: %v", err)
	}
	got := rebuilt.Providers[0].HealthCheckRules[0].HTTPStatusCodes
	if len(got) != 4 || got[0] != 429 {
		t.Fatalf("expected [429 500 502 503] after rebuild, got %v", got)
	}
}

func TestRejectsInvalidHTTPStatusCode(t *testing.T) {
	mustReject(t, `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
    health_check_rules:
      - http_status_codes: [99]
combos:
  - name: c
    api_format: openai
    members:
      - provider: sn
        model: m
`)
}

func TestGroupedCombosWithOwnedBy(t *testing.T) {
	raw := `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
    health_check_rules: []
combos:
  - owned_by: default
    default: true
    combos:
      - name: fast
        api_format: openai
        members:
          - provider: sn
            model: deepseek-flash
        aliases: ["fast-alias"]
  - owned_by: team-a
    combos:
      - name: fast
        api_format: openai
        members:
          - provider: sn
            model: deepseek-chat
        aliases: ["teama-alias"]
`
	cfg := loadFromText(t, raw)
	if len(cfg.Combos) != 2 {
		t.Fatalf("expected 2 combos, got %d", len(cfg.Combos))
	}

	c0 := cfg.Combos[0]
	if c0.Name != "fast" || c0.OwnedBy != "default" || !c0.IsDefault {
		t.Fatalf("c0 mismatch: %+v", c0)
	}
	if c0.FullName() != "fast" {
		t.Fatalf("expected full name 'fast', got %q", c0.FullName())
	}
	if len(c0.FullAliases()) != 1 || c0.FullAliases()[0] != "fast-alias" {
		t.Fatalf("unexpected full aliases: %v", c0.FullAliases())
	}

	c1 := cfg.Combos[1]
	if c1.Name != "fast" || c1.OwnedBy != "team-a" || c1.IsDefault {
		t.Fatalf("c1 mismatch: %+v", c1)
	}
	if c1.FullName() != "team-a/fast" {
		t.Fatalf("expected full name 'team-a/fast', got %q", c1.FullName())
	}
	if len(c1.FullAliases()) != 1 || c1.FullAliases()[0] != "team-a/teama-alias" {
		t.Fatalf("unexpected full aliases: %v", c1.FullAliases())
	}

	// Test dump roundtrip
	dumped := Dump(cfg)
	rebuilt, err := Build(dumped)
	if err != nil {
		t.Fatalf("rebuild grouped combos: %v", err)
	}
	if len(rebuilt.Combos) != 2 {
		t.Fatalf("expected 2 combos after rebuild, got %d", len(rebuilt.Combos))
	}
	if rebuilt.Combos[0].FullName() != "fast" || rebuilt.Combos[1].FullName() != "team-a/fast" {
		t.Fatalf("rebuilt combo names mismatch: %s, %s", rebuilt.Combos[0].FullName(), rebuilt.Combos[1].FullName())
	}
}

func TestRejectsMultipleDefaultGroups(t *testing.T) {
	mustReject(t, `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
combos:
  - owned_by: g1
    default: true
    combos:
      - name: fast
        api_format: openai
        members: [{provider: sn, model: m}]
  - owned_by: g2
    default: true
    combos:
      - name: fast
        api_format: openai
        members: [{provider: sn, model: m}]
`)
}

func TestRejectsSlashInOwnedByOrName(t *testing.T) {
	mustReject(t, `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
combos:
  - owned_by: team/a
    combos:
      - name: fast
        api_format: openai
        members: [{provider: sn, model: m}]
`)

	mustReject(t, `
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
combos:
  - owned_by: teama
    combos:
      - name: group/fast
        api_format: openai
        members: [{provider: sn, model: m}]
`)
}

func TestAdminPasswordConfig(t *testing.T) {
	text := `
general:
  admin_password: "super-secret-password"
providers:
  - name: sn
    api:
      - api_format: openai
        base_url: "https://upstream.test/v1"
    keys:
      - key: sk-1
combos:
  - name: my-combo
    api_format: openai
    strategy: fill-first
    members:
      - provider: sn
        model: gpt-4o
`
	cfg := loadFromText(t, text)
	if cfg.General.AdminPassword != "super-secret-password" {
		t.Fatalf("expected admin_password to be 'super-secret-password', got %q", cfg.General.AdminPassword)
	}

	dumped := Dump(cfg)
	gen, ok := dumped["general"].(map[string]any)
	if !ok {
		t.Fatalf("expected general map in dumped config, got %#v", dumped["general"])
	}
	if gen["admin_password"] != "super-secret-password" {
		t.Fatalf("expected dumped admin_password to be 'super-secret-password', got %#v", gen["admin_password"])
	}

	// Empty password should not be present in Dump
	cfg.General.AdminPassword = ""
	dumpedEmpty := Dump(cfg)
	if genEmpty, ok := dumpedEmpty["general"].(map[string]any); ok {
		if _, exists := genEmpty["admin_password"]; exists {
			t.Fatalf("expected empty admin_password to not be in dump")
		}
	}
}
