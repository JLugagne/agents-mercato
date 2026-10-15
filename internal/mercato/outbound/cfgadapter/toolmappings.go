package cfgadapter

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/configstore"
)

var _ configstore.ToolMappingStore = (*ToolMappingStoreAdapter)(nil)

type ToolMappingStoreAdapter struct{}

func NewToolMappingStore() *ToolMappingStoreAdapter { return &ToolMappingStoreAdapter{} }

func (a *ToolMappingStoreAdapter) LoadToolMappings(path string) (domain.ToolMapping, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return a.DefaultToolMappings(), nil
		}
		return a.DefaultToolMappings(), fmt.Errorf("read tool mappings: %w", err)
	}
	var m domain.ToolMapping
	if err := yaml.Unmarshal(data, &m); err != nil {
		fmt.Fprintf(os.Stderr, "warning: corrupt tool-mappings file %s: %v, using defaults\n", path, err)
		return a.DefaultToolMappings(), nil
	}
	if m.Models == nil {
		m.Models = make(map[string]map[string]string)
	}
	if m.Tools == nil {
		m.Tools = make(map[string]map[string]string)
	}
	dropLegacyInitValues(m.Models, legacyInitModels)
	dropLegacyInitValues(m.Tools, legacyInitTools)
	return m, nil
}

func (a *ToolMappingStoreAdapter) SaveToolMappings(path string, m domain.ToolMapping) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory for tool mappings: %w", err)
	}
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal tool mappings: %w", err)
	}
	return writeFileAtomic(path, data, 0644)
}

func (a *ToolMappingStoreAdapter) ToolMappingsExist(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// DefaultToolMappings returns empty overrides: per-tool defaults live in the
// transformers, and this file only holds the user's own overrides.
func (a *ToolMappingStoreAdapter) DefaultToolMappings() domain.ToolMapping {
	return domain.ToolMapping{
		Models: make(map[string]map[string]string),
		Tools:  make(map[string]map[string]string),
	}
}

var legacyInitModels = map[string]map[string]string{
	"opus":   {"opencode": "anthropic/claude-opus-4-20250514", "cursor": "", "windsurf": "", "codex": "", "gemini": ""},
	"sonnet": {"opencode": "anthropic/claude-sonnet-4-20250514", "cursor": "", "windsurf": "", "codex": "", "gemini": ""},
	"haiku":  {"opencode": "anthropic/claude-haiku-4-5-20251001", "cursor": "", "windsurf": "", "codex": "", "gemini": ""},
}

var legacyInitTools = map[string]map[string]string{
	"Read": {"opencode": "read"}, "Edit": {"opencode": "edit"}, "Bash": {"opencode": "bash"},
	"Write": {"opencode": "write"}, "Glob": {"opencode": "glob"}, "Grep": {"opencode": "grep"},
}

func dropLegacyInitValues(m, legacy map[string]map[string]string) {
	for key, byTool := range m {
		for tool, v := range byTool {
			if lv, ok := legacy[key][tool]; ok && lv == v {
				delete(byTool, tool)
			}
		}
		if len(byTool) == 0 {
			delete(m, key)
		}
	}
}
