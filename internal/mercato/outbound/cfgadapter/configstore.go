package cfgadapter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/configstore"
)

var _ configstore.ConfigStore = (*ConfigStoreAdapter)(nil)

type ConfigStoreAdapter struct{}

func NewConfigStore() *ConfigStoreAdapter { return &ConfigStoreAdapter{} }

func (c *ConfigStoreAdapter) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (c *ConfigStoreAdapter) Load(path string) (domain.Config, error) {
	var cfg domain.Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("load config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	if cfg.LocalPath == "" {
		cfg.LocalPath = ".claude/"
	}
	if cfg.ConflictPolicy == "" {
		cfg.ConflictPolicy = "block"
	}
	if cfg.DriftPolicy == "" {
		cfg.DriftPolicy = "prompt"
	}
	if cfg.StaleAfter == 0 {
		cfg.StaleAfter = 7 * 24 * 60 * 60 * 1e9
	}
	if len(cfg.Tools) == 0 {
		cfg.Tools = map[string]bool{"claude": true}
	}
	for i := range cfg.Markets {
		if cfg.Markets[i].Branch == "" {
			cfg.Markets[i].Branch = "main"
		}
	}
	return cfg, nil
}

func (c *ConfigStoreAdapter) Save(path string, cfg domain.Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return writeFileAtomic(path, data, 0600)
}

func (c *ConfigStoreAdapter) AddMarket(path string, market domain.MarketConfig) error {
	cfg, err := c.Load(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	cfg.Markets = append(cfg.Markets, market)
	return c.Save(path, cfg)
}

func (c *ConfigStoreAdapter) RemoveMarket(path string, name string) error {
	cfg, err := c.Load(path)
	if err != nil {
		return err
	}
	markets := make([]domain.MarketConfig, 0, len(cfg.Markets))
	for _, m := range cfg.Markets {
		if m.Name != name {
			markets = append(markets, m)
		}
	}
	cfg.Markets = markets
	return c.Save(path, cfg)
}

func (c *ConfigStoreAdapter) SetConfigField(path string, key string, value string) error {
	cfg, err := c.Load(path)
	if err != nil {
		return err
	}
	switch {
	case key == "ssh_enabled":
		v := value == "true" || value == "1"
		cfg.SSHEnabled = &v
	case key == "local_path":
		cfg.LocalPath = value
	case key == "conflict_policy":
		cfg.ConflictPolicy = value
	case key == "drift_policy":
		cfg.DriftPolicy = value
	case strings.HasPrefix(key, "tools."):
		toolName := strings.TrimPrefix(key, "tools.")
		if toolName == "" {
			return fmt.Errorf("missing tool name in key: %s", key)
		}
		if cfg.Tools == nil {
			cfg.Tools = make(map[string]bool)
		}
		cfg.Tools[toolName] = value == "true" || value == "1"
	default:
		return fmt.Errorf("unknown config field: %s", key)
	}
	return c.Save(path, cfg)
}

func (c *ConfigStoreAdapter) LoadProjectConfig(projectDir string) (domain.ProjectConfig, error) {
	var pc domain.ProjectConfig
	data, err := os.ReadFile(filepath.Join(projectDir, ".mct.yml"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return pc, nil
		}
		return pc, fmt.Errorf("load project config: %w", err)
	}
	if err := yaml.Unmarshal(data, &pc); err != nil {
		return pc, fmt.Errorf("parse project config: %w", err)
	}
	return pc, nil
}

func (c *ConfigStoreAdapter) SetMarketProperty(path string, marketName string, key string, value string) error {
	cfg, err := c.Load(path)
	if err != nil {
		return err
	}
	found := false
	for i := range cfg.Markets {
		if cfg.Markets[i].Name == marketName {
			found = true
			switch key {
			case "name":
				cfg.Markets[i].Name = value
			case "branch":
				cfg.Markets[i].Branch = value
			case "url":
				cfg.Markets[i].URL = value
			case "trusted":
				cfg.Markets[i].Trusted = value == "true"
			case "read_only":
				cfg.Markets[i].ReadOnly = value == "true"
			case "skills_only":
				cfg.Markets[i].SkillsOnly = value == "true"
			case "skills_path":
				cfg.Markets[i].SkillsPath = value
			default:
				return fmt.Errorf("unknown market property: %s", key)
			}
			break
		}
	}
	if !found {
		return fmt.Errorf("market %q not found", marketName)
	}
	return c.Save(path, cfg)
}
