package app

import (
	"path/filepath"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

func (a *App) installHookSnippet(
	w txWriter,
	ref domain.MctRef,
	snippet domain.HookSnippet,
	settingsAbsPath string,
	hookFile string,
) ([]domain.InstalledFile, error) {
	mctID := mctIDForRef(ref)
	st, err := loadHookState(w.ReadFile, settingsAbsPath)
	if err != nil {
		return nil, err
	}
	resolved := st.resolve()
	if st.conflicts(resolved, snippet.Event, snippet.Matcher, mctID) {
		return nil, domain.ErrConflictHookEventMatcher
	}
	st.removeOwned(resolved, mctID)
	handlers, err := st.add(ref, snippet)
	if err != nil {
		return nil, err
	}
	if err := st.save(w, settingsAbsPath); err != nil {
		return nil, err
	}

	checksum, err := hookChecksum(snippet.Matcher, handlers)
	if err != nil {
		return nil, err
	}
	return []domain.InstalledFile{
		{Path: relativeProjectPath(settingsAbsPath) + "#hooks/" + hookFile, XXH: checksum},
	}, nil
}

// relativeProjectPath turns an absolute settings.json path into the
// project-relative slash-separated form recorded in InstalledFile.Path
// (e.g. ".claude/settings.json").
func relativeProjectPath(absSettingsPath string) string {
	return projectRelFromClaude(absSettingsPath)
}

// addHook is the dedicated install path for hook entries. It parses the
// JSON snippet, opens a transaction, runs the settings.json merge through
// installHookSnippet, and records a single per-package leaf-name entry.
func (a *App) addHook(
	ref domain.MctRef,
	marketName, relPath, profile string,
	cfg domain.Config,
	mc *domain.MarketConfig,
	db *domain.InstallDatabase,
	projectPath string,
	opts service.AddOpts,
	result *service.AddResult,
) error {
	pkg := db.FindPackage(marketName, profile)
	if pkg != nil && pkg.FindLocation(projectPath) != nil {
		hookFile := filepath.Base(relPath)
		for _, h := range pkg.Files.Hooks {
			if h == hookFile {
				return domain.ErrEntryAlreadyInstalled
			}
		}
	}

	clonePath := a.clonePath(marketName)
	content, err := a.git.ReadFileAtRef(clonePath, mc.Branch, relPath, "HEAD")
	if err != nil {
		return err
	}

	snippet, err := domain.ParseHookSnippet(content)
	if err != nil {
		return err
	}

	if opts.DryRun {
		return nil
	}

	settingsAbsPath, err := a.resolveLocalPath(cfg, relPath)
	if err != nil {
		return err
	}
	hookFile := filepath.Base(relPath)

	w, commit, rollback, err := a.beginWriter("add-hook:" + string(ref))
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = rollback()
		}
	}()

	written, err := a.installHookSnippet(w, ref, snippet, settingsAbsPath, hookFile)
	if err != nil {
		return err
	}

	headSHA, err := a.git.RemoteHEAD(clonePath, mc.Branch)
	if err != nil {
		return err
	}

	files := domain.InstalledFiles{Hooks: []string{hookFile}}
	existingPkg := db.FindPackage(marketName, profile)
	mergedPkgFiles := files
	if existingPkg != nil {
		mergedPkgFiles = domain.MergePackageFiles(existingPkg.Files, files)
	}

	mergedClaudeFiles := written
	if existingPkg != nil {
		if loc := findLocationByPathAndType(existingPkg, projectPath, domain.RuntimeTypeClaudeCode); loc != nil {
			mergedClaudeFiles = domain.MergeLocationFiles(loc.Files, written)
		}
	}

	db.AddOrUpdatePackage(marketName, profile, headSHA, mergedPkgFiles, domain.InstalledLocation{
		Path:  projectPath,
		Type:  domain.RuntimeTypeClaudeCode,
		Files: mergedClaudeFiles,
	})

	var toolsOverride map[string]bool
	if len(opts.Tools) > 0 {
		toolsOverride = opts.Tools
	}
	twr := a.writeToToolTargets(w, domain.Entry{
		Ref:      ref,
		Market:   marketName,
		RelPath:  relPath,
		Filename: hookFile,
		Type:     domain.EntryTypeHook,
		Profile:  profile,
	}, content, nil, projectPath, toolsOverride)
	if result != nil {
		mergeAddResult(result, twr)
	}
	recordToolFiles(db, marketName, profile, headSHA, mergedPkgFiles, projectPath, twr)

	if err := a.stageDBSave(w, *db); err != nil {
		return err
	}
	if err := commit(); err != nil {
		return err
	}
	committed = true

	if result != nil {
		if result.ToolWrites == nil {
			result.ToolWrites = make(map[string]string)
		}
		result.ToolWrites[domain.RuntimeTypeClaudeCode] = relativeProjectPath(settingsAbsPath)
	}

	return nil
}
