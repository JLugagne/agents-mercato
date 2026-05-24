package commands

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigPath(t *testing.T) {
	p := defaultConfigPath()
	if strings.Contains(p, "~") {
		t.Errorf("defaultConfigPath must not contain a literal ~, got %q", p)
	}
	if !filepath.IsAbs(p) {
		t.Errorf("defaultConfigPath must be absolute, got %q", p)
	}
}

func TestDefaultCacheDir(t *testing.T) {
	p := defaultCacheDir()
	if strings.Contains(p, "~") {
		t.Errorf("defaultCacheDir must not contain a literal ~, got %q", p)
	}
	if !filepath.IsAbs(p) {
		t.Errorf("defaultCacheDir must be absolute, got %q", p)
	}
}

func TestNewRootCmdFlagDefaultsAreAbsolute(t *testing.T) {
	svc := Services{}
	cmd := NewRootCmd(svc)
	cf := cmd.PersistentFlags()

	configVal, err := cf.GetString("config")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(configVal, "~") {
		t.Errorf("--config default must not contain a literal ~, got %q", configVal)
	}
	if !filepath.IsAbs(configVal) {
		t.Errorf("--config default must be absolute, got %q", configVal)
	}
	if filepath.Base(configVal) != "config.yml" {
		t.Errorf("--config default should end with config.yml, got %q", configVal)
	}
	if !strings.HasSuffix(configVal, "/.config/mct/config.yml") && !strings.HasSuffix(configVal, "\\.config\\mct\\config.yml") {
		t.Errorf("--config default should be under .config/mct, got %q", configVal)
	}

	cacheVal, err := cf.GetString("cache")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cacheVal, "~") {
		t.Errorf("--cache default must not contain a literal ~, got %q", cacheVal)
	}
	if !filepath.IsAbs(cacheVal) {
		t.Errorf("--cache default must be absolute, got %q", cacheVal)
	}
	if !strings.HasSuffix(cacheVal, "/.cache/mct") && !strings.HasSuffix(cacheVal, "\\.cache\\mct") {
		t.Errorf("--cache default should be under .cache/mct, got %q", cacheVal)
	}
}
