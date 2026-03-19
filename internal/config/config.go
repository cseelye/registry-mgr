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
	RegistryURL     string `yaml:"registry_url"`
	Username        string `yaml:"username"`
	Password        string `yaml:"password"`
	CredentialsFile string `yaml:"credentials_file"`

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

	// Layer 2: credentials file (path may come from yaml or env)
	credFile := cfg.CredentialsFile
	if v := os.Getenv("REGISTRY_MGR_CREDENTIALS_FILE"); v != "" {
		credFile = v
	}
	if credFile != "" {
		if err := ApplyCredentialsFile(cfg, credFile); err != nil {
			return nil, err
		}
	}

	// Layer 3: environment variables
	if v := os.Getenv("REGISTRY_MGR_URL"); v != "" {
		cfg.RegistryURL = v
	}
	if v := os.Getenv("REGISTRY_MGR_USERNAME"); v != "" {
		cfg.Username = v
	}
	if v := os.Getenv("REGISTRY_MGR_PASSWORD"); v != "" {
		cfg.Password = v
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

// ApplyCredentialsFile reads a "username:password" credentials file and sets
// cfg.Username and cfg.Password.
func ApplyCredentialsFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading credentials file: %w", err)
	}
	line := strings.TrimSpace(string(data))
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid credentials file format: expected username:password")
	}
	cfg.Username = parts[0]
	cfg.Password = parts[1]
	return nil
}
