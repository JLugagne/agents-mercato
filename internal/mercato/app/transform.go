package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cespare/xxhash/v2"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

// loadEnabledTools loads the global config Tools map, merges with the project
// .mct.yml override if present, and returns the final enabled-tools map.
func (a *App) loadEnabledTools() map[string]bool {
	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return nil
	}

	globalTools := cfg.Tools

	projCfg, err := a.cfg.LoadProjectConfig(projectPath(cfg.LocalPath))
	if err != nil {
		// No project config or error reading it — use global only.
		return globalTools
	}

	return domain.MergeTools(globalTools, projCfg.Tools)
}

// loadToolMappings loads tool mappings from the config dir, falling back to
// defaults if the mapping store is not configured.
func (a *App) loadToolMappings() domain.ToolMapping {
	if a.toolMappings == nil {
		return domain.ToolMapping{
			Models: make(map[string]map[string]string),
			Tools:  make(map[string]map[string]string),
		}
	}
	mappingsPath := filepath.Join(filepath.Dir(a.configPath), "tool-mappings.yml")
	m, err := a.toolMappings.LoadToolMappings(mappingsPath)
	if err != nil {
		return domain.ToolMapping{
			Models: make(map[string]map[string]string),
			Tools:  make(map[string]map[string]string),
		}
	}
	return m
}

// toolWriteResult holds the full result of writing to tool targets.
type toolWriteResult struct {
	ToolWrites map[string]string                 // tool -> relative output path
	ToolFiles  map[string][]domain.InstalledFile // tool -> files written (one per tool, but kept as a slice for symmetry)
	Warnings   []string
}

// writeToToolTargets transforms and writes the entry content to all enabled
// tool targets via the supplied writer. It returns per-tool file records
// (path + xxhash) and warnings. The Claude tool is skipped here since it is
// handled by the existing code path. If toolsOverride is non-nil, it is used
// instead of the config-based tools.
func (a *App) writeToToolTargets(w txWriter, entry domain.Entry, content []byte, projectDir string, toolsOverride map[string]bool) toolWriteResult {
	enabledTools := toolsOverride
	if enabledTools == nil {
		enabledTools = a.loadEnabledTools()
	}
	if len(enabledTools) == 0 {
		return toolWriteResult{}
	}

	transformers := a.transformers.EnabledTransformers(enabledTools)
	if len(transformers) == 0 {
		return toolWriteResult{}
	}

	mappings := a.loadToolMappings()

	toolWrites := make(map[string]string)
	toolFiles := make(map[string][]domain.InstalledFile)
	var warnings []string

	for _, t := range transformers {
		toolName := t.ToolName()

		// Skip claude — it's handled by the existing code path.
		if toolName == "claude" {
			continue
		}

		if !t.SupportsEntry(entry.Type) {
			warnings = append(warnings, fmt.Sprintf("%s: skipped (does not support %ss)", toolName, entry.Type))
			continue
		}

		result := t.Transform(entry, content, mappings)
		if result.Skipped {
			reason := result.SkipReason
			if reason == "" {
				reason = "skipped"
			}
			warnings = append(warnings, fmt.Sprintf("%s: %s", toolName, reason))
			continue
		}

		outPath := filepath.Join(projectDir, result.OutputPath)

		// Check if the parent dot-directory exists (e.g. .cursor/, .windsurf/).
		// If not, skip silently with a warning.
		dotDir := toolDotDir(result.OutputPath)
		if dotDir != "" {
			dotDirAbs := filepath.Join(projectDir, dotDir)
			if _, err := a.fs.Stat(dotDirAbs); err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: skipped (%s does not exist)", toolName, dotDir))
				continue
			}
		}

		if err := w.WriteFile(outPath, result.Content); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: write error: %v", toolName, err))
			continue
		}

		toolWrites[toolName] = result.OutputPath
		toolFiles[toolName] = append(toolFiles[toolName], domain.InstalledFile{
			Path: filepath.ToSlash(result.OutputPath),
			XXH:  xxhashHex(result.Content),
		})
		warnings = append(warnings, result.Warnings...)
	}

	return toolWriteResult{
		ToolWrites: toolWrites,
		ToolFiles:  toolFiles,
		Warnings:   warnings,
	}
}

// toolRemoveResult holds the result of removing tool-specific files.
type toolRemoveResult struct {
	Removed []string // tool names successfully removed from
	Errors  []error
}

// removeFromToolTargets removes tool-specific files for the given entry from
// all enabled tool directories.
// removeFromToolTargets removes tool-specific files for the given entry from
// all enabled tool directories via the supplied writer.
func (a *App) removeFromToolTargets(w txWriter, entry domain.Entry, projectDir string) toolRemoveResult {
	enabledTools := a.loadEnabledTools()
	if len(enabledTools) == 0 {
		return toolRemoveResult{}
	}

	transformers := a.transformers.EnabledTransformers(enabledTools)
	var result toolRemoveResult

	for _, t := range transformers {
		if t.ToolName() == "claude" {
			continue
		}
		if !t.SupportsEntry(entry.Type) {
			continue
		}

		outPath := filepath.Join(projectDir, t.OutputPath(entry))
		if err := w.DeleteFile(outPath); err != nil {
			// Ignore file-not-found type errors.
			if !strings.Contains(err.Error(), "no such file") && !strings.Contains(err.Error(), "not exist") {
				result.Errors = append(result.Errors, fmt.Errorf("%s: %w", t.ToolName(), err))
			}
		} else {
			result.Removed = append(result.Removed, t.ToolName())
		}
	}
	return result
}

// xxhashHex returns the hex-encoded xxhash64 of data.
func xxhashHex(data []byte) string {
	return fmt.Sprintf("%016x", xxhash.Sum64(data))
}

// toolDotDir extracts the leading dot-directory from a path like ".cursor/rules/foo.mdc".
// Returns "" if there is no leading dot-directory.
func toolDotDir(relPath string) string {
	parts := strings.SplitN(filepath.ToSlash(relPath), "/", 2)
	if len(parts) > 0 && strings.HasPrefix(parts[0], ".") {
		return parts[0]
	}
	return ""
}

// detectToolDrift compares per-tool files at a project location against their
// recorded xxhash. Returns a map of toolName → EntryState. Reads from the
// per-location InstalledFile entries (not the legacy ToolChecksums map).
func (a *App) detectToolDrift(entry domain.Entry, pkg domain.InstalledPackage, projectDir string) map[string]domain.EntryState {
	enabledTools := a.loadEnabledTools()
	if len(enabledTools) == 0 {
		return nil
	}

	transformers := a.transformers.EnabledTransformers(enabledTools)
	states := make(map[string]domain.EntryState)

	for _, t := range transformers {
		toolName := t.ToolName()
		if toolName == "claude" {
			continue
		}
		if !t.SupportsEntry(entry.Type) {
			continue
		}

		// Look up the location entry for (projectDir, toolName).
		var loc *domain.InstalledLocation
		for i := range pkg.Locations {
			if pkg.Locations[i].Path == projectDir && pkg.Locations[i].Type == toolName {
				loc = &pkg.Locations[i]
				break
			}
		}
		if loc == nil || len(loc.Files) == 0 {
			continue
		}

		expectedPath := filepath.ToSlash(t.OutputPath(entry))
		var expectedXXH string
		found := false
		for _, f := range loc.Files {
			if f.Path == expectedPath {
				expectedXXH = f.XXH
				found = true
				break
			}
		}
		if !found {
			continue
		}

		outPath := filepath.Join(projectDir, t.OutputPath(entry))
		localContent, err := a.fs.ReadFile(outPath)
		if err != nil {
			states[toolName] = domain.StateDrift
			continue
		}

		if xxhashHex(localContent) != expectedXXH {
			states[toolName] = domain.StateDrift
		} else {
			states[toolName] = domain.StateClean
		}
	}

	if len(states) == 0 {
		return nil
	}
	return states
}
