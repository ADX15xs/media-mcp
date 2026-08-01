package config

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestLoadValidConfig(t *testing.T) {
	dir := t.TempDir()
	yml := `
default_supplier: agnes_ai
suppliers:
  agnes_ai:
    enabled: true
    type: image
    api_key: sk-test123
    base_url: https://api.agnes.ai/v1/images/generations
    model: agnes-2.1-flash
    size: "1024x1024"
`
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(yml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.DefaultSupplier != "agnes_ai" {
		t.Errorf("DefaultSupplier = %q, want %q", cfg.DefaultSupplier, "agnes_ai")
	}
	s, ok := cfg.Suppliers["agnes_ai"]
	if !ok {
		t.Fatal("missing supplier agnes_ai")
	}
	if !s.Enabled {
		t.Error("Enabled should be true")
	}
	if s.APIKey != "sk-test123" {
		t.Errorf("APIKey = %q", s.APIKey)
	}
	if s.BaseURL != "https://api.agnes.ai/v1/images/generations" {
		t.Errorf("BaseURL = %q", s.BaseURL)
	}
	if s.Model != "agnes-2.1-flash" {
		t.Errorf("Model = %q", s.Model)
	}
	if s.SupplierType != "image" {
		t.Errorf("SupplierType = %q", s.SupplierType)
	}
}

func TestLoadEnvVarExpansion(t *testing.T) {
	t.Setenv("TEST_API_KEY", "sk-env-expanded")
	t.Setenv("TEST_BASE_URL", "https://env.example.com/api")

	dir := t.TempDir()
	yml := `
suppliers:
  test_sup:
    enabled: true
    type: image
    api_key: ${TEST_API_KEY}
    base_url: ${TEST_BASE_URL}
    model: default-model
`
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(yml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	s := cfg.Suppliers["test_sup"]
	if s.APIKey != "sk-env-expanded" {
		t.Errorf("APIKey after expansion = %q, want %q", s.APIKey, "sk-env-expanded")
	}
	if s.BaseURL != "https://env.example.com/api" {
		t.Errorf("BaseURL after expansion = %q", s.BaseURL)
	}
}

func TestLoadEnvVarMissing(t *testing.T) {
	os.Unsetenv("MISSING_VAR")

	dir := t.TempDir()
	yml := `
suppliers:
  test_sup:
    enabled: true
    type: image
    api_key: ${MISSING_VAR}
    base_url: https://example.com
`
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(yml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	s := cfg.Suppliers["test_sup"]
	// Missing var should be left as-is (validation will catch it later)
	if s.APIKey != "${MISSING_VAR}" {
		t.Errorf("unexpanded APIKey = %q, want %q", s.APIKey, "${MISSING_VAR}")
	}
}

func TestLoadEmptySuppliers(t *testing.T) {
	dir := t.TempDir()
	yml := `suppliers: {}`
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(yml), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for empty suppliers")
	}
}

func TestLoadMissingAPIKey(t *testing.T) {
	dir := t.TempDir()
	yml := `
suppliers:
  test_sup:
    enabled: true
    type: image
    api_key: ""
    base_url: https://example.com
`
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(yml), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for empty api_key")
	}
}

func TestLoadMissingBaseURL(t *testing.T) {
	dir := t.TempDir()
	yml := `
suppliers:
  test_sup:
    enabled: true
    type: image
    api_key: sk-test
    base_url: ""
`
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(yml), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for empty base_url")
	}
}

func TestLoadInvalidType(t *testing.T) {
	dir := t.TempDir()
	yml := `
suppliers:
  test_sup:
    enabled: true
    type: audio
    api_key: sk-test
    base_url: https://example.com
`
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(yml), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
}

func TestLoadDisabledSupplier(t *testing.T) {
	dir := t.TempDir()
	yml := `
suppliers:
  test_sup:
    enabled: false
    type: image
    api_key: ""
    base_url: ""
`
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(yml), 0644); err != nil {
		t.Fatal(err)
	}

	// Disabled suppliers are not validated for api_key/base_url
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Suppliers["test_sup"].Enabled {
		t.Error("expected supplier to be disabled")
	}
}

func TestLoadDefaultSupplierFallback(t *testing.T) {
	dir := t.TempDir()
	yml := `
suppliers:
  senseNova:
    enabled: true
    type: image
    api_key: sk-test
    base_url: https://api.example.com
`
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(yml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.DefaultSupplier != "senseNova" {
		t.Errorf("DefaultSupplier = %q, want %q", cfg.DefaultSupplier, "senseNova")
	}
}

func TestExpandEnvVars(t *testing.T) {
	t.Setenv("FOO", "hello")
	t.Setenv("BAR", "world")

	input := []byte("${FOO} ${BAR} ${NOT_SET}")
	output := expandEnvVars(input)

	expected := "hello world ${NOT_SET}"
	if string(output) != expected {
		t.Errorf("expandEnvVars = %q, want %q", string(output), expected)
	}
}

func TestSupplier(t *testing.T) {
	cfg := &GlobalConfig{
		DefaultSupplier: "sup1",
		Suppliers: map[string]SupplierConfig{
			"sup1": {Enabled: true, APIKey: "key1", BaseURL: "https://a.com"},
			"sup2": {Enabled: false, APIKey: "key2", BaseURL: "https://b.com"},
		},
	}

	s := cfg.Supplier("sup1")
	if s == nil || s.APIKey != "key1" {
		t.Errorf("Supplier(\"sup1\") = %v", s)
	}

	s = cfg.Supplier("nonexistent")
	if s != nil {
		t.Errorf("Supplier(\"nonexistent\") = %v, want nil", s)
	}
}

func TestEnabledSuppliers(t *testing.T) {
	cfg := &GlobalConfig{
		Suppliers: map[string]SupplierConfig{
			"sup1": {Enabled: true},
			"sup2": {Enabled: false},
			"sup3": {Enabled: true},
		},
	}

	names := cfg.EnabledSuppliers()
	sort.Strings(names)
	if len(names) != 2 {
		t.Fatalf("EnabledSuppliers() = %v, want 2", names)
	}
	if names[0] != "sup1" || names[1] != "sup3" {
		t.Errorf("EnabledSuppliers() = %v, want [sup1 sup3]", names)
	}
}

func TestDefaultConfigPath(t *testing.T) {
	path := DefaultConfigPath()
	if path == "" {
		t.Error("DefaultConfigPath() returned empty string")
	}
	if filepath.Base(path) != "config.yml" {
		t.Errorf("DefaultConfigPath() = %q, should end with config.yml", path)
	}
}