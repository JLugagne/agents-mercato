package cfgadapter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

func TestToolMappings_LoadExistingValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool-mappings.yml")

	content := `models:
  gpt4:
    opencode: "openai/gpt-4"
    cursor: "gpt-4"
tools:
  Run:
    opencode: "run"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	adapter := NewToolMappingStore()
	m, err := adapter.LoadToolMappings(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(m.Models))
	}
	if m.Models["gpt4"]["opencode"] != "openai/gpt-4" {
		t.Errorf("expected opencode=openai/gpt-4, got %q", m.Models["gpt4"]["opencode"])
	}
	if m.Models["gpt4"]["cursor"] != "gpt-4" {
		t.Errorf("expected cursor=gpt-4, got %q", m.Models["gpt4"]["cursor"])
	}
	if len(m.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(m.Tools))
	}
	if m.Tools["Run"]["opencode"] != "run" {
		t.Errorf("expected opencode=run, got %q", m.Tools["Run"]["opencode"])
	}
}

func TestToolMappings_LoadMissingFile(t *testing.T) {
	adapter := NewToolMappingStore()
	m, err := adapter.LoadToolMappings("/nonexistent/path/tool-mappings.yml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	defaults := adapter.DefaultToolMappings()
	if len(m.Models) != len(defaults.Models) {
		t.Errorf("expected defaults models count %d, got %d", len(defaults.Models), len(m.Models))
	}
	if len(m.Tools) != len(defaults.Tools) {
		t.Errorf("expected defaults tools count %d, got %d", len(defaults.Tools), len(m.Tools))
	}
}

func TestToolMappings_LoadCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool-mappings.yml")

	if err := os.WriteFile(path, []byte("{{{{not valid yaml!!!!"), 0644); err != nil {
		t.Fatal(err)
	}

	adapter := NewToolMappingStore()
	m, err := adapter.LoadToolMappings(path)
	if err != nil {
		t.Fatalf("expected no error for corrupt file, got: %v", err)
	}
	defaults := adapter.DefaultToolMappings()
	if len(m.Models) != len(defaults.Models) {
		t.Errorf("expected defaults on corrupt file, got %d models", len(m.Models))
	}
}

func TestToolMappings_SaveAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "tool-mappings.yml")

	adapter := NewToolMappingStore()
	original := domain.ToolMapping{
		Models: map[string]map[string]string{
			"test-model": {"opencode": "test/model-1", "cursor": "tm1"},
		},
		Tools: map[string]map[string]string{
			"TestTool": {"opencode": "testtool"},
		},
	}

	if err := adapter.SaveToolMappings(path, original); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := adapter.LoadToolMappings(path)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if loaded.Models["test-model"]["opencode"] != "test/model-1" {
		t.Errorf("expected opencode=test/model-1, got %q", loaded.Models["test-model"]["opencode"])
	}
	if loaded.Models["test-model"]["cursor"] != "tm1" {
		t.Errorf("expected cursor=tm1, got %q", loaded.Models["test-model"]["cursor"])
	}
	if loaded.Tools["TestTool"]["opencode"] != "testtool" {
		t.Errorf("expected opencode=testtool, got %q", loaded.Tools["TestTool"]["opencode"])
	}
}

func TestToolMappings_Exist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool-mappings.yml")

	adapter := NewToolMappingStore()

	if adapter.ToolMappingsExist(path) {
		t.Error("expected false for non-existent file")
	}

	if err := os.WriteFile(path, []byte("models: {}"), 0644); err != nil {
		t.Fatal(err)
	}

	if !adapter.ToolMappingsExist(path) {
		t.Error("expected true for existing file")
	}
}

func TestToolMappings_LoadNilMapsInitialized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool-mappings.yml")

	// Write a valid YAML that produces nil maps (empty doc)
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	adapter := NewToolMappingStore()
	m, err := adapter.LoadToolMappings(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Models == nil {
		t.Error("expected non-nil Models after loading empty file")
	}
	if m.Tools == nil {
		t.Error("expected non-nil Tools after loading empty file")
	}
}
