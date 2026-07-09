package config

import (
	"testing"
)

func TestParseConfig(t *testing.T) {
	data := []byte(`
version: 1
analyzer:
  rules:
    - name: "error-ignored"
      enabled: true
    - name: "fmt-print"
      enabled: false
  exclude_paths:
    - "vendor/**"
    - "**/*_test.go"
llm:
  provider: "gemini"
  model: "gemini-1.5-pro"
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Version != 1 {
		t.Errorf("expected version 1, got %d", cfg.Version)
	}
	if len(cfg.Analyzer.Rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(cfg.Analyzer.Rules))
	}
	if cfg.Analyzer.Rules[0].Name != "error-ignored" || !cfg.Analyzer.Rules[0].Enabled {
		t.Errorf("unexpected rule[0]: %+v", cfg.Analyzer.Rules[0])
	}
	if cfg.Analyzer.Rules[1].Name != "fmt-print" || cfg.Analyzer.Rules[1].Enabled {
		t.Errorf("unexpected rule[1]: %+v", cfg.Analyzer.Rules[1])
	}
	if len(cfg.Analyzer.ExcludePaths) != 2 {
		t.Errorf("expected 2 exclude paths, got %d", len(cfg.Analyzer.ExcludePaths))
	}
	if cfg.LLM.Provider != "gemini" {
		t.Errorf("expected provider 'gemini', got '%s'", cfg.LLM.Provider)
	}
	if cfg.LLM.Model != "gemini-1.5-pro" {
		t.Errorf("expected model 'gemini-1.5-pro', got '%s'", cfg.LLM.Model)
	}
}

func TestParseConfig_Invalid(t *testing.T) {
	data := []byte(`
version: 1
analyzer:
  rules:
    - name: "error-ignored"
      enabled: "invalid_boolean"
`)
	_, err := Parse(data)
	if err == nil {
		t.Errorf("expected error parsing invalid yaml")
	}
}
