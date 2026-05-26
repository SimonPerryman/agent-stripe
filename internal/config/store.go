// Package config persists account aliases and defaults. The config file
// lives at os.UserConfigDir()/agent-stripe/config.json (0600) and holds
// only `{alias, mode, keychain_ref}` — never the secret.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const appDir = "agent-stripe"

// Mode is "test" or "live", derived from the API key prefix.
type Mode string

const (
	ModeTest Mode = "test"
	ModeLive Mode = "live"
)

// Account holds the on-disk record for one alias.
type Account struct {
	Alias           string `json:"alias"`
	Mode            Mode   `json:"mode"`
	KeychainRef     string `json:"keychainRef"`
	RequireLiveFlag *bool  `json:"requireLiveFlag,omitempty"`
}

// Config is the full on-disk structure.
type Config struct {
	DefaultAccount string             `json:"defaultAccount,omitempty"`
	Accounts       map[string]Account `json:"accounts"`
}

// Path returns the resolved config file path.
func Path() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appDir, "config.json"), nil
}

// Load reads the config from disk, returning an empty config if it does not exist.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	return loadFrom(p)
}

func loadFrom(p string) (*Config, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{Accounts: map[string]Account{}}, nil
		}
		return nil, fmt.Errorf("reading %s: %w", p, err)
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", p, err)
	}
	if c.Accounts == nil {
		c.Accounts = map[string]Account{}
	}
	return &c, nil
}

// Save writes the config atomically (temp + rename) at mode 0600.
func Save(c *Config) error {
	p, err := Path()
	if err != nil {
		return err
	}
	return saveTo(p, c)
}

func saveTo(p string, c *Config) error {
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "config.*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, p)
}

// DeriveMode returns ModeTest / ModeLive from the API key prefix.
// Accepts secret keys (sk_*) and restricted keys (rk_*).
func DeriveMode(key string) (Mode, error) {
	switch {
	case strings.HasPrefix(key, "sk_test_"), strings.HasPrefix(key, "rk_test_"):
		return ModeTest, nil
	case strings.HasPrefix(key, "sk_live_"), strings.HasPrefix(key, "rk_live_"):
		return ModeLive, nil
	}
	return "", errors.New("key must start with sk_test_, sk_live_, rk_test_, or rk_live_")
}
