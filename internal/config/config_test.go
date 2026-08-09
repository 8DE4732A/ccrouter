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
}

func TestDumpIncludesVerboseLogging(t *testing.T) {
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
verbose_logging: true
`
	cfg := loadFromText(t, raw)
	if !cfg.VerboseLogging {
		t.Fatal("expected verbose_logging true")
	}
	d := Dump(cfg)
	if d["verbose_logging"] != true {
		t.Fatal("expected verbose_logging true in dump")
	}
}
