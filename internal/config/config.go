package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/adrg/xdg"
	"gopkg.in/yaml.v3"
)

const (
	AppName        = "llmail"
	ConfigFile     = "config.yaml"
	DefaultProfile = "default"
)

// activeProfile controls path and keyring isolation.
// "default" maps to the base llmail directory for backward compatibility.
var activeProfile = DefaultProfile

// SetProfile sets the active profile name. Must be called before any
// path or credential functions. An empty string is treated as "default".
func SetProfile(name string) {
	if name == "" {
		name = DefaultProfile
	}
	activeProfile = name
}

// Profile returns the active profile name.
func Profile() string {
	return activeProfile
}

type PasswordStorage string

const (
	PasswordStorageKeyring   PasswordStorage = "keyring"
	PasswordStorageEncrypted PasswordStorage = "encrypted"
)

type Capabilities struct {
	GmailExtensions bool `yaml:"gmail_extensions"`
	Sort            bool `yaml:"sort"`
	Thread          bool `yaml:"thread"`
}

type AccountConfig struct {
	Host              string          `yaml:"host"`
	Port              int             `yaml:"port"`
	TLS               bool            `yaml:"tls"`
	Username          string          `yaml:"username"`
	PasswordStorage   PasswordStorage `yaml:"password_storage"`
	EncryptedPassword string          `yaml:"encrypted_password,omitempty"`
	Capabilities      Capabilities    `yaml:"capabilities"`
}

type IndexAccountConfig struct {
	Folders []string `yaml:"folders"`
}

type IndexConfig struct {
	Enabled             bool                          `yaml:"enabled"`
	Mode                string                        `yaml:"mode"` // "none", "all", "selected"
	Accounts            map[string]IndexAccountConfig `yaml:"accounts,omitempty"`
	SyncIntervalMinutes int                           `yaml:"sync_interval_minutes"`
	BatchSize           int                           `yaml:"batch_size"`
}

type GuardConfig struct {
	Enabled   bool    `yaml:"enabled"`
	ModelPath string  `yaml:"model_path,omitempty"`
	Threshold float64 `yaml:"threshold,omitempty"`
}

type ImageConfig struct {
	MaxSizeBytes int `yaml:"max_size_bytes"` // 0 = no limit
}

type LLMConfig struct {
	Provider          string          `yaml:"provider"`                    // "openai", "anthropic", "openrouter", "ollama", "openai-compatible"
	Model             string          `yaml:"model"`                       // e.g. "gpt-4o", "claude-sonnet-4-20250514"
	BaseURL           string          `yaml:"base_url,omitempty"`          // custom endpoint (required for ollama/openrouter/openai-compatible)
	APIKeyStorage     PasswordStorage `yaml:"api_key_storage,omitempty"`   // "keyring" or "encrypted"
	EncryptedAPIKey   string          `yaml:"encrypted_api_key,omitempty"` // only when storage is "encrypted"
	MaxTokens         int             `yaml:"max_tokens,omitempty"`        // default 4096
	Temperature       float64         `yaml:"temperature,omitempty"`       // default 0.7
}

type Config struct {
	Version  int                      `yaml:"version"`
	Accounts map[string]AccountConfig `yaml:"accounts"`
	Index    IndexConfig              `yaml:"index"`
	LLM      LLMConfig               `yaml:"llm,omitempty"`
	Guard    GuardConfig             `yaml:"guard,omitempty"`
	Image    ImageConfig             `yaml:"image,omitempty"`

	mu   sync.RWMutex
	path string
}

func ConfigDir() string {
	base := filepath.Join(xdg.ConfigHome, AppName)
	if activeProfile != DefaultProfile {
		return filepath.Join(base, "profiles", activeProfile)
	}
	return base
}

func DataDir() string {
	base := filepath.Join(xdg.DataHome, AppName)
	if activeProfile != DefaultProfile {
		return filepath.Join(base, "profiles", activeProfile)
	}
	return base
}

func DefaultConfigPath() string {
	return filepath.Join(ConfigDir(), ConfigFile)
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := &Config{path: path}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Accounts == nil {
		cfg.Accounts = make(map[string]AccountConfig)
	}
	if cfg.Index.BatchSize == 0 {
		cfg.Index.BatchSize = 100
	}
	if cfg.Index.SyncIntervalMinutes == 0 {
		cfg.Index.SyncIntervalMinutes = 15
	}
	if cfg.Guard.Threshold == 0 {
		cfg.Guard.Threshold = 0.80
	}

	return cfg, nil
}

func (c *Config) Save(path string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if path == "" {
		path = c.path
	}
	if path == "" {
		path = DefaultConfigPath()
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	c.path = path
	return nil
}

func (c *Config) GetAccount(name string) (AccountConfig, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	acc, ok := c.Accounts[name]
	return acc, ok
}

func (c *Config) SetAccount(name string, acc AccountConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Accounts == nil {
		c.Accounts = make(map[string]AccountConfig)
	}
	c.Accounts[name] = acc
}

func (c *Config) DeleteAccount(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.Accounts, name)
}

func (c *Config) AccountNames() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, 0, len(c.Accounts))
	for name := range c.Accounts {
		names = append(names, name)
	}
	return names
}

func NewDefault() *Config {
	return &Config{
		Version:  1,
		Accounts: make(map[string]AccountConfig),
		Index: IndexConfig{
			Mode:                "none",
			SyncIntervalMinutes: 15,
			BatchSize:           100,
		},
	}
}
