package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAndEnvOverrideWithoutReadingProdSecrets(t *testing.T) {
	t.Setenv("ECOMMERCE_APP_PRODUCT_CODE", "ecommerce-test")
	t.Setenv("ECOMMERCE_REDIS_ENABLED", "true")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	cfg, err := Load("missing-test-config")
	if err != nil {
		t.Fatalf("Load missing config with defaults: %v", err)
	}
	if cfg.App.ProductCode != "ecommerce-test" || !cfg.Redis.Enabled || cfg.Database.Driver != "sqlite" || cfg.Platform.BaseURL == "" {
		t.Fatalf("defaults/env override not applied: %+v", cfg)
	}
}

func TestLoadRejectsMalformedConfigFile(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "bad.yaml"), []byte("host: [unterminated"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if _, err := Load("bad"); err == nil {
		t.Fatalf("expected malformed config to fail")
	}
}
