package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds all application configuration
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Backup   BackupConfig   `yaml:"backup"`
	Logging  LoggingConfig  `yaml:"logging"`
	Auth     AuthConfig     `yaml:"auth"`
	Security SecurityConfig `yaml:"security"`
	DataDir  string         `yaml:"-"`
}

type ServerConfig struct {
	Port int `yaml:"port"`
}

type BackupConfig struct {
	DefaultDir string `yaml:"default_dir"`
}

type LoggingConfig struct {
	RetentionDays int `yaml:"retention_days"`
}

type AuthConfig struct {
	Password string `yaml:"password"` // login password (plaintext in config)
}

type SecurityConfig struct {
	EncryptKey string `yaml:"encrypt_key"` // AES-256 key (hex, 64 chars)
}

// DefaultConfig returns sensible defaults
func DefaultConfig(dataDir string) *Config {
	return &Config{
		Server:  ServerConfig{Port: 8080},
		Backup:  BackupConfig{DefaultDir: filepath.Join(dataDir, "backups")},
		Logging: LoggingConfig{RetentionDays: 30},
		Auth:    AuthConfig{Password: "admin"},
		Security: SecurityConfig{EncryptKey: ""}, // auto-generated if empty
		DataDir: dataDir,
	}
}

// Load reads config.yaml, or creates a default one
func Load(dataDir string) (*Config, error) {
	// Try config.yaml next to the data directory first, then inside
	paths := []string{
		filepath.Join(dataDir, "..", "config.yaml"),
		filepath.Join(dataDir, "config.yaml"),
		"config.yaml",
	}

	cfg := DefaultConfig(dataDir)
	var loaded bool
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return cfg, err
		}
		cfg.DataDir = dataDir
		loaded = true
		break
	}

	if !loaded {
		// Write default config
		savePath := paths[0]
		os.MkdirAll(filepath.Dir(savePath), 0755)
		SaveTo(cfg, savePath)
	}

	os.MkdirAll(cfg.Backup.DefaultDir, 0755)
	return cfg, nil
}

// Save writes the current config to the default location (project root)
func Save(cfg *Config) error {
	return SaveTo(cfg, filepath.Join(cfg.DataDir, "..", "config.yaml"))
}

// SaveTo writes config to a specific path
func SaveTo(cfg *Config, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600) // owner read/write only
}
