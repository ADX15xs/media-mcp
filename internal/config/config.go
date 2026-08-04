package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// SupplierConfig is the raw configuration for one supplier.
type SupplierConfig struct {
	Enabled      bool                   `yaml:"enabled"`
	SupplierType string                 `yaml:"type"` // "image", "video", or "both"
	APIKey       string                 `yaml:"api_key"`
	BaseURL      string                 `yaml:"base_url"`
	Model        string                 `yaml:"model"`
	Models       []string               `yaml:"models"`        // alternative models
	Size         string                 `yaml:"size"`          // default image size, e.g. "2752x1536"
	Sizes        []string               `yaml:"sizes"`         // alternative sizes
	AUTHMethod   string                 `yaml:"auth_method"`   // "bearer", "basic", "custom_header"
	CustomHeader string                 `yaml:"custom_header"` // header name if auth_method != bearer
	Headers      map[string]string      `yaml:"headers"`       // extra static headers
	Extra        map[string]interface{} `yaml:"extra"`         // arbitrary extra fields passed to the adapter
}

// GlobalConfig is the root YAML structure.
type GlobalConfig struct {
	DefaultSupplier string                    `yaml:"default_supplier"`
	Suppliers       map[string]SupplierConfig `yaml:"suppliers"`
}

// Load reads and expands ${VAR} references in the given file path.
func Load(path string) (*GlobalConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	// Expand ${ENV_VAR} references with a single regex pass.
	data = expandEnvVars(data)

	var cfg GlobalConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Validate.
	if cfg.DefaultSupplier == "" {
		cfg.DefaultSupplier = "senseNova"
	}
	if len(cfg.Suppliers) == 0 {
		return nil, fmt.Errorf("no suppliers defined in config")
	}
	for name, s := range cfg.Suppliers {
		if !s.Enabled {
			continue
		}
		if s.APIKey == "" {
			return nil, fmt.Errorf("supplier %q: api_key is required but empty", name)
		}
		if s.BaseURL == "" {
			return nil, fmt.Errorf("supplier %q: base_url is required but empty", name)
		}
		if s.SupplierType != "image" && s.SupplierType != "video" && s.SupplierType != "both" {
			return nil, fmt.Errorf("supplier %q: type must be image, video, or both", name)
		}
	}

	return &cfg, nil
}

var envVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// expandEnvVars replaces ${VAR_NAME} and ${VAR_NAME:-default} references with
// the corresponding environment variable value (or the default when unset).
func expandEnvVars(data []byte) []byte {
	return envVarRe.ReplaceAllFunc(data, func(match []byte) []byte {
		sub := envVarRe.FindSubmatch(match)
		key := string(sub[1])
		if val, ok := os.LookupEnv(key); ok && val != "" {
			return []byte(val)
		}
		if sub[2] != nil {
			return sub[2]
		}
		return match
	})
}

// Supplier returns the config for the named supplier, or nil if not found.
func (c *GlobalConfig) Supplier(name string) *SupplierConfig {
	s, ok := c.Suppliers[name]
	if !ok {
		return nil
	}
	return &s
}

// EnabledSuppliers returns all enabled supplier names.
func (c *GlobalConfig) EnabledSuppliers() []string {
	var names []string
	for n, s := range c.Suppliers {
		if s.Enabled {
			names = append(names, n)
		}
	}
	return names
}

// DefaultConfigPath resolves the path to config.yml relative to the executable.
func DefaultConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return filepath.Join(".", "config.yml")
	}
	return filepath.Join(filepath.Dir(exe), "config.yml")
}
