package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cespare/xxhash/v2"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

func (a *App) loadEnabledTools() map[string]bool {
	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return nil
	}

	globalTools := cfg.Tools

	projCfg, err := a.cfg.LoadProjectConfig(projectPath(cfg.LocalPath))
	if err != nil {
		return globalTools
	}

	return domain.MergeTools(globalTools, projCfg.Tools)
}

func (a *App) loadToolMappings() domain.ToolMapping {
	empty := domain.ToolMapping{
		Models: make(map[string]map[string]string),
		Tools:  make(map[string]map[string]string),
	}
	if a.toolMappings == nil {
		return empty
	}
	m, err := a.toolMappings.LoadToolMappings(filepath.Join(filepath.Dir(a.configPath), "tool-mappings.yml"))
	if err != nil {
		return empty
	}
	return m
}

type toolWriteResult struct {
	ToolWrites map[string]string
	ToolFiles  map[string][]domain.InstalledFile
	Warnings   []string
}

func (r *toolWriteResult) merge(o toolWriteResult) {
	if r.ToolWrites == nil {
		r.ToolWrites = map[string]string{}
	}
	if r.ToolFiles == nil {
		r.ToolFiles = map[string][]domain.InstalledFile{}
	}
	for k, v := range o.ToolWrites {
		if _, ok := r.ToolWrites[k]; !ok {
			r.ToolWrites[k] = v
		}
	}
	for k, v := range o.ToolFiles {
		r.ToolFiles[k] = append(r.ToolFiles[k], v...)
	}
	r.Warnings = append(r.Warnings, o.Warnings...)
}

func (a *App) checkToolName(tool string) error {
	if len(a.transformers) == 0 {
		return nil
	}
	if _, ok := a.transformers.Get(tool); !ok {
		return errors.Join(domain.ErrUnknownTool, fmt.Errorf("%q (supported: %s)", tool, strings.Join(a.transformers.Names(), ", ")))
	}
	return nil
}

func (a *App) projectExists(projectDir string) func(string) bool {
	return func(rel string) bool {
		_, err := a.fs.Stat(filepath.Join(projectDir, filepath.FromSlash(rel)))
		return err == nil
	}
}

func toolDetected(t domain.Transformer, exists func(string) bool) bool {
	markers := t.Detect()
	if len(markers) == 0 {
		return true
	}
	for _, m := range markers {
		if exists(m) {
			return true
		}
	}
	return false
}

func (a *App) enabledToolTransformers(toolsOverride map[string]bool) []domain.Transformer {
	enabled := toolsOverride
	if enabled == nil {
		enabled = a.loadEnabledTools()
	}
	var out []domain.Transformer
	for _, t := range a.transformers.EnabledTransformers(enabled) {
		if t.ToolName() != "claude" {
			out = append(out, t)
		}
	}
	return out
}

func (a *App) writeToToolTargets(w txWriter, entry domain.Entry, content []byte, assets []domain.TransformAsset, projectDir string, toolsOverride map[string]bool) toolWriteResult {
	return a.writeToolTargets(w, entry, content, assets, projectDir, toolsOverride, nil)
}

func (a *App) writeToolTargets(w txWriter, entry domain.Entry, content []byte, assets []domain.TransformAsset, projectDir string, toolsOverride map[string]bool, skip func(string) bool) toolWriteResult {
	transformers := a.enabledToolTransformers(toolsOverride)
	res := toolWriteResult{ToolWrites: map[string]string{}, ToolFiles: map[string][]domain.InstalledFile{}}
	if len(transformers) == 0 {
		return res
	}

	mappings := a.loadToolMappings()
	exists := a.projectExists(projectDir)
	shared := map[string]bool{}

	for _, t := range transformers {
		toolName := t.ToolName()
		if !toolDetected(t, exists) {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: skipped (%s does not exist in project)", toolName, strings.Join(t.Detect(), " or ")))
			continue
		}
		out := t.Transform(domain.TransformInput{Entry: entry, Content: content, Assets: assets, Mappings: mappings, Exists: exists})
		if out.Skipped {
			reason := out.SkipReason
			if reason == "" {
				reason = "skipped"
			}
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: skipped (%s)", toolName, reason))
			continue
		}
		res.Warnings = append(res.Warnings, out.Warnings...)

		for _, f := range out.Files {
			if skip != nil && skip(f.Path) {
				continue
			}
			owner := toolName
			if f.IsShared() {
				owner = domain.RuntimeTypeSharedAgents
				if shared[f.Path] {
					continue
				}
				shared[f.Path] = true
			}
			if err := w.WriteFile(filepath.Join(projectDir, filepath.FromSlash(f.Path)), f.Content); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s: write %s: %v", toolName, f.Path, err))
				continue
			}
			res.ToolFiles[owner] = append(res.ToolFiles[owner], domain.InstalledFile{Path: f.Path, XXH: xxhashHex(f.Content), Source: entry.RelPath})
			if _, ok := res.ToolWrites[toolName]; !ok {
				res.ToolWrites[toolName] = f.Path
			}
		}

		for _, p := range out.Hooks {
			data, err := applyHookPatch(w, projectDir, p)
			if err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s: hook %s: %v", toolName, p.File, err))
				continue
			}
			res.ToolFiles[toolName] = append(res.ToolFiles[toolName], domain.InstalledFile{Path: p.Anchor(), XXH: xxhashHex(data), Source: entry.RelPath, Data: data})
			if _, ok := res.ToolWrites[toolName]; !ok {
				res.ToolWrites[toolName] = p.File
			}
		}
	}
	return res
}

func recordToolFiles(db *domain.InstallDatabase, market, profile, headSHA string, pkgFiles domain.InstalledFiles, projectPath string, twr toolWriteResult) {
	for _, toolName := range sortedKeys(twr.ToolFiles) {
		merged := twr.ToolFiles[toolName]
		if pkg := db.FindPackage(market, profile); pkg != nil {
			if loc := findLocationByPathAndType(pkg, projectPath, toolName); loc != nil {
				merged = domain.MergeLocationFiles(loc.Files, merged)
			}
		}
		db.AddOrUpdatePackage(market, profile, headSHA, pkgFiles, domain.InstalledLocation{
			Path:  projectPath,
			Type:  toolName,
			Files: merged,
		})
	}
}

func isToolLocation(loc domain.InstalledLocation, projectDir string) bool {
	return loc.Path == projectDir && loc.Type != domain.RuntimeTypeClaudeCode
}

func removeToolRecord(w txWriter, projectDir string, f domain.InstalledFile) error {
	if f.IsHookPatch() {
		if len(f.Data) == 0 {
			return nil
		}
		return removeHookPatch(w, projectDir, f.Path, f.Data)
	}
	abs := filepath.Join(projectDir, filepath.FromSlash(f.Path))
	if dir, ok := toolSkillDir(f.Path); ok {
		abs = filepath.Join(projectDir, filepath.FromSlash(dir))
		if err := w.DeleteAll(abs); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := w.DeleteFile(abs); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func toolSkillDir(rel string) (string, bool) {
	parts := strings.Split(rel, "/")
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] == "skills" && strings.HasPrefix(parts[0], ".") {
			return strings.Join(parts[:i+2], "/"), true
		}
	}
	return "", false
}

func removeToolFiles(w txWriter, pkg *domain.InstalledPackage, projectDir, source string) ([]string, error) {
	var removedTools []string
	var errs []error
	kept := pkg.Locations[:0]
	for _, loc := range pkg.Locations {
		if !isToolLocation(loc, projectDir) {
			kept = append(kept, loc)
			continue
		}
		var remaining []domain.InstalledFile
		removed := false
		for _, f := range loc.Files {
			if source != "" && f.Source != source {
				remaining = append(remaining, f)
				continue
			}
			if err := removeToolRecord(w, projectDir, f); err != nil {
				errs = append(errs, errors.Join(fmt.Errorf("%s: %s", loc.Type, f.Path), err))
				remaining = append(remaining, f)
				continue
			}
			removed = true
		}
		if removed && loc.Type != domain.RuntimeTypeSharedAgents {
			removedTools = append(removedTools, loc.Type)
		}
		if len(remaining) > 0 {
			loc.Files = remaining
			kept = append(kept, loc)
		}
	}
	pkg.Locations = kept
	return removedTools, errors.Join(errs...)
}

func syncToolFiles(w txWriter, pkg *domain.InstalledPackage, projectDir string, fresh map[string][]domain.InstalledFile) []string {
	var warnings []string
	keep := map[string]bool{}
	for _, files := range fresh {
		for _, f := range files {
			keep[toolRecordKey(f)] = true
		}
	}
	var kept []domain.InstalledLocation
	for _, loc := range pkg.Locations {
		if !isToolLocation(loc, projectDir) {
			kept = append(kept, loc)
			continue
		}
		for _, f := range loc.Files {
			if keep[toolRecordKey(f)] {
				continue
			}
			if modifiedLocally(w, projectDir, f) {
				warnings = append(warnings, fmt.Sprintf("%s: %s is no longer managed by mct but was modified locally, kept", loc.Type, f.Path))
				continue
			}
			if err := removeToolRecord(w, projectDir, f); err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: remove %s: %v", loc.Type, f.Path, err))
			}
		}
	}
	for _, toolName := range sortedKeys(fresh) {
		if len(fresh[toolName]) == 0 {
			continue
		}
		kept = append(kept, domain.InstalledLocation{Path: projectDir, Type: toolName, Files: fresh[toolName]})
	}
	pkg.Locations = kept
	return warnings
}

func modifiedLocally(w txWriter, projectDir string, f domain.InstalledFile) bool {
	if f.IsHookPatch() {
		return false
	}
	data, err := w.ReadFile(filepath.Join(projectDir, filepath.FromSlash(f.Path)))
	return err == nil && xxhashHex(data) != f.XXH
}

func toolRecordKey(f domain.InstalledFile) string {
	if f.IsHookPatch() {
		return f.Path + "\x00" + f.XXH
	}
	return f.Path
}

func (a *App) detectToolDrift(entry domain.Entry, pkg domain.InstalledPackage, projectDir string) map[string]domain.EntryState {
	states := make(map[string]domain.EntryState)
	for _, loc := range pkg.Locations {
		if !isToolLocation(loc, projectDir) {
			continue
		}
		bySource := false
		for _, f := range loc.Files {
			if f.Source != "" {
				bySource = true
				break
			}
		}
		for _, f := range loc.Files {
			if bySource && f.Source != entry.RelPath {
				continue
			}
			state := a.toolRecordState(projectDir, f)
			if prev, ok := states[loc.Type]; !ok || prev == domain.StateClean {
				states[loc.Type] = state
			}
		}
	}
	if len(states) == 0 {
		return nil
	}
	return states
}

func (a *App) toolRecordState(projectDir string, f domain.InstalledFile) domain.EntryState {
	if f.IsHookPatch() {
		present, err := hookPatchPresent(a.fs.ReadFile, projectDir, f.Path, f.Data)
		if err != nil || !present {
			return domain.StateDrift
		}
		return domain.StateClean
	}
	data, err := a.fs.ReadFile(filepath.Join(projectDir, filepath.FromSlash(f.Path)))
	if err != nil || xxhashHex(data) != f.XXH {
		return domain.StateDrift
	}
	return domain.StateClean
}

func (a *App) skillAssets(clonePath, branch, relPath string) []domain.TransformAsset {
	return a.skillAssetsAt(clonePath, branch, relPath, "HEAD")
}

func (a *App) skillAssetsAt(clonePath, branch, relPath, ref string) []domain.TransformAsset {
	if !isDirBasedSkill(relPath) {
		return nil
	}
	dir := filepath.ToSlash(filepath.Dir(relPath))
	files, err := a.git.ListDirFiles(clonePath, branch, dir)
	if err != nil {
		return nil
	}
	var assets []domain.TransformAsset
	for _, f := range files {
		rel := strings.TrimPrefix(filepath.ToSlash(f), dir+"/")
		if rel == "SKILL.md" || rel == f {
			continue
		}
		content, err := a.git.ReadFileAtRef(clonePath, branch, f, ref)
		if err != nil {
			continue
		}
		assets = append(assets, domain.TransformAsset{Path: rel, Content: content})
	}
	return assets
}

func xxhashHex(data []byte) string {
	return fmt.Sprintf("%016x", xxhash.Sum64(data))
}
