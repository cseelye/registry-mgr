package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration for registry-mgr tools.
type Config struct {
	RegistryURL string `yaml:"registry_url"`
	Username    string `yaml:"username"`
	Password    string `yaml:"password"`

	// WebUI-specific
	Port       int    `yaml:"port"`
	ListenAddr string `yaml:"listen_addr"`
}

// Default returns a Config with default values applied.
func Default() *Config {
	return &Config{
		Port:       5080,
		ListenAddr: "0.0.0.0",
	}
}

// Load builds a Config by layering: yaml file → credentials file → environment variables.
// CLI flags are applied on top by the caller.
func Load(configFile string) (*Config, error) {
	cfg := Default()

	// Layer 1: YAML config file
	if configFile != "" {
		data, err := os.ReadFile(configFile)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
		if err == nil {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("parsing config file: %w", err)
			}
		}
	}

	// Layer 2: environment variables
	if v := os.Getenv("REGISTRY_URL"); v != "" {
		cfg.RegistryURL = v
	}
	if v := os.Getenv("REGISTRY_CREDENTIALS"); v != "" {
		parts := strings.SplitN(v, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid REGISTRY_CREDENTIALS format: expected username:password")
		}
		cfg.Username = parts[0]
		cfg.Password = parts[1]
	}
	if v := os.Getenv("WEBUI_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil && port > 0 {
			cfg.Port = port
		}
	}
	if v := os.Getenv("WEBUI_LISTEN"); v != "" {
		cfg.ListenAddr = v
	}

	return cfg, nil
}

