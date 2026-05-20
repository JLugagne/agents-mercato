package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JLugagne/agents-mercato/assets"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

func (a *App) List(opts service.ListOpts) ([]domain.Entry, error) {
	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return nil, err
	}

	db, err := a.idb.Load(a.cacheDir)
	if err != nil {
		return nil, err
	}

	// Clean stale locations
	stale := db.CleanStaleLocations(func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.IsDir()
	})
	if len(stale) > 0 {
		if lockErr := a.idb.Lock(a.cacheDir); lockErr == nil {
			_ = a.idb.Save(a.cacheDir, db)
			_ = a.idb.Unlock(a.cacheDir)
		}
	}

	projectPath := projectPath(cfg.LocalPath)

	var entries []domain.Entry

	for _, im := range db.Markets {
		if opts.Market != "" && im.Market != opts.Market {
			continue
		}

		for _, pkg := range im.Packages {
			atLocation := false
			for _, loc := range pkg.Locations {
				if loc.Path == projectPath {
					atLocation = true
					break
				}
			}
			if !atLocation {
				continue
			}

			for _, skill := range pkg.Files.Skills {
				repoPath := a.skillFileRepoPath(pkg.Profile, skill, "SKILL.md")
				ref := domain.MctRef(im.Market + "@" + repoPath)
				entry := domain.Entry{
					Ref:       ref,
					Market:    im.Market,
					RelPath:   repoPath,
					Filename:  "SKILL.md",
					Type:      domain.EntryTypeSkill,
					Version:   domain.MctVersion(pkg.Version),
					Profile:   pkg.Profile,
					Installed: true,
				}
				if opts.Type != "" && entry.Type != opts.Type {
					continue
				}
				entries = append(entries, entry)
			}
			for _, agent := range pkg.Files.Agents {
				repoPath := a.agentFileRepoPath(pkg.Profile, agent)
				ref := domain.MctRef(im.Market + "@" + repoPath)
				entry := domain.Entry{
					Ref:       ref,
					Market:    im.Market,
					RelPath:   repoPath,
					Filename:  agent,
					Type:      domain.EntryTypeAgent,
					Version:   domain.MctVersion(pkg.Version),
					Profile:   pkg.Profile,
					Installed: true,
				}
				if opts.Type != "" && entry.Type != opts.Type {
					continue
				}
				entries = append(entries, entry)
			}
			for _, hook := range pkg.Files.Hooks {
				repoPath := a.hookFileRepoPath(pkg.Profile, hook)
				ref := domain.MctRef(im.Market + "@" + repoPath)
				entry := domain.Entry{
					Ref:       ref,
					Market:    im.Market,
					RelPath:   repoPath,
					Filename:  hook,
					Type:      domain.EntryTypeHook,
					Version:   domain.MctVersion(pkg.Version),
					Profile:   pkg.Profile,
					Installed: true,
				}
				if opts.Type != "" && entry.Type != opts.Type {
					continue
				}
				entries = append(entries, entry)
			}
			for _, cmd := range pkg.Files.Commands {
				repoPath := a.commandFileRepoPath(pkg.Profile, cmd)
				ref := domain.MctRef(im.Market + "@" + repoPath)
				entry := domain.Entry{
					Ref:       ref,
					Market:    im.Market,
					RelPath:   repoPath,
					Filename:  cmd,
					Type:      domain.EntryTypeCommand,
					Version:   domain.MctVersion(pkg.Version),
					Profile:   pkg.Profile,
					Installed: true,
				}
				if opts.Type != "" && entry.Type != opts.Type {
					continue
				}
				entries = append(entries, entry)
			}
		}
	}

	return entries, nil
}

func (a *App) ReadEntryContent(market, relPath string) ([]byte, error) {
	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return nil, err
	}

	mc := findMarketConfig(cfg, market)
	if mc == nil {
		return nil, domain.ErrMarketNotFound
	}

	clonePath := a.clonePath(market)
	return a.git.ReadFileAtRef(clonePath, mc.Branch, relPath, "HEAD")
}

// ListSkillDirFiles lists all files under a skill directory in the market repo
// and reads the content of .md files.
func (a *App) ListSkillDirFiles(market, dirPrefix string) ([]domain.SkillDirFile, error) {
	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return nil, err
	}

	mc := findMarketConfig(cfg, market)
	if mc == nil {
		return nil, domain.ErrMarketNotFound
	}

	clonePath := a.clonePath(market)
	files, err := a.git.ListDirFiles(clonePath, mc.Branch, dirPrefix)
	if err != nil {
		return nil, err
	}

	result := make([]domain.SkillDirFile, 0, len(files))
	for _, f := range files {
		sf := domain.SkillDirFile{
			Path: f,
			Name: filepath.Base(f),
		}
		if strings.HasSuffix(f, ".md") {
			content, err := a.git.ReadFileAtRef(clonePath, mc.Branch, f, "HEAD")
			if err == nil {
				sf.Content = string(content)
			}
		}
		result = append(result, sf)
	}
	return result, nil
}

func (a *App) GetEntry(ref domain.MctRef) (domain.Entry, error) {
	marketName, relPath, err := ref.Parse()
	if err != nil {
		return domain.Entry{}, err
	}

	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return domain.Entry{}, err
	}

	db, err := a.idb.Load(a.cacheDir)
	if err != nil {
		return domain.Entry{}, err
	}

	projectPath := projectPath(cfg.LocalPath)

	im := db.FindMarket(marketName)
	if im == nil {
		return domain.Entry{}, domain.ErrEntryNotFound
	}

	for _, pkg := range im.Packages {
		atLocation := false
		for _, loc := range pkg.Locations {
			if loc.Path == projectPath {
				atLocation = true
				break
			}
		}
		if !atLocation {
			continue
		}

		if !a.entryFileExistsAt(cfg, relPath) {
			continue
		}

		entryType := inferEntryType(relPath)
		return domain.Entry{
			Ref:       ref,
			Market:    marketName,
			RelPath:   relPath,
			Filename:  filepath.Base(relPath),
			Type:      entryType,
			Version:   domain.MctVersion(pkg.Version),
			Profile:   pkg.Profile,
			Installed: true,
		}, nil
	}

	return domain.Entry{}, domain.ErrEntryNotFound
}

func (a *App) Add(ref domain.MctRef, opts service.AddOpts) (service.AddResult, error) {
	marketName, relPath, err := ref.Parse()
	if err != nil {
		return service.AddResult{}, err
	}

	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return service.AddResult{}, err
	}

	mc := findMarketConfig(cfg, marketName)
	if mc == nil {
		return service.AddResult{}, domain.ErrMarketNotFound
	}

	// Lock installdb
	if err := a.idb.Lock(a.cacheDir); err != nil {
		return service.AddResult{}, err
	}
	defer func() { _ = a.idb.Unlock(a.cacheDir) }()

	// Load installdb
	db, err := a.idb.Load(a.cacheDir)
	if err != nil {
		return service.AddResult{}, err
	}

	visited := make(map[domain.MctRef]bool)
	var result service.AddResult
	err = a.addInternal(ref, marketName, relPath, cfg, mc, &db, visited, opts, &result)
	return result, err
}

func (a *App) addInternal(
	ref domain.MctRef,
	marketName, relPath string,
	cfg domain.Config,
	mc *domain.MarketConfig,
	db *domain.InstallDatabase,
	visited map[domain.MctRef]bool,
	opts service.AddOpts,
	result *service.AddResult,
) error {
	// Normalize skill directory refs: skills/foo → skills/foo/SKILL.md
	if isSkillDirRef(relPath) {
		relPath = relPath + "/SKILL.md"
		ref = domain.MctRef(marketName + "@" + relPath)
	}

	// Cycle detection
	if visited[ref] {
		return nil
	}
	visited[ref] = true

	if isProfileRef(relPath) {
		opts.Profile = relPath
		return a.addProfile(marketName, relPath, mc, cfg, db, visited, opts, result)
	}

	profile := opts.Profile
	if profile == "" {
		profile = refProfile(ref)
	}

	projectPath := projectPath(cfg.LocalPath)

	entryType := inferEntryType(relPath)

	// Hooks: take a dedicated path. Hooks are JSON merged into
	// .claude/settings.json, not files copied to disk; ParseFrontmatter
	// would fail on a JSON snippet.
	if entryType == domain.EntryTypeHook {
		return a.addHook(ref, marketName, relPath, profile, cfg, mc, db, projectPath, opts, result)
	}

	// Check if already installed at this location.
	// Disk presence is the source of truth — pkg.Files is package-wide and
	// can carry over from a sibling location after RemoveLocation.
	pkg := db.FindPackage(marketName, profile)
	if pkg != nil {
		atLocation := pkg.FindLocation(projectPath) != nil
		if atLocation && a.entryFileExistsAt(cfg, relPath) {
			return domain.ErrEntryAlreadyInstalled
		}
	}

	clonePath := a.clonePath(marketName)

	content, err := a.git.ReadFileAtRef(clonePath, mc.Branch, relPath, "HEAD")
	if err != nil {
		return err
	}

	fm, err := domain.ParseFrontmatter(content)
	if err != nil {
		return domain.ErrInvalidFrontmatter.Wrap(err)
	}

	if entryType == domain.EntryTypeAgent {
		content = domain.StripRequiresSkills(content)
	}

	if opts.DryRun {
		return nil
	}

	localPath, err := a.resolveLocalPath(cfg, relPath)
	if err != nil {
		return err
	}

	// Open a per-package transaction. All file writes (claude target +
	// every enabled tool target) are buffered until commit. The install
	// database save runs as an OnCommit hook so disk and DB land together
	// or not at all.
	w, commit, rollback, err := a.beginWriter("add:" + string(ref))
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = rollback()
		}
	}()

	files, claudeFiles, err := a.installEntryFiles(w, clonePath, mc.Branch, relPath, localPath, content)
	if err != nil {
		return err
	}

	headSHA, err := a.git.RemoteHEAD(clonePath, mc.Branch)
	if err != nil {
		return err
	}

	entryForTransform := domain.Entry{
		Ref:      ref,
		Market:   marketName,
		RelPath:  relPath,
		Filename: filepath.Base(relPath),
		Type:     entryType,
		Profile:  profile,
	}
	var toolsOverride map[string]bool
	if len(opts.Tools) > 0 {
		toolsOverride = opts.Tools
	}
	twr := a.writeToToolTargets(w, entryForTransform, content, projectPath, toolsOverride)

	if result != nil {
		mergeAddResult(result, twr)
	}

	// AddOrUpdatePackage now replaces wholesale, so for a single-entry add we
	// must merge the new files with whatever this (location, runtime-type)
	// already holds from sibling entries before recording.
	existingPkg := db.FindPackage(marketName, profile)
	mergedPkgFiles := files
	if existingPkg != nil {
		mergedPkgFiles = domain.MergePackageFiles(existingPkg.Files, files)
	}

	mergedClaudeFiles := claudeFiles
	if existingPkg != nil {
		if loc := findLocationByPathAndType(existingPkg, projectPath, domain.RuntimeTypeClaudeCode); loc != nil {
			mergedClaudeFiles = domain.MergeLocationFiles(loc.Files, claudeFiles)
		}
	}

	db.AddOrUpdatePackage(marketName, profile, headSHA, mergedPkgFiles, domain.InstalledLocation{
		Path:  projectPath,
		Type:  domain.RuntimeTypeClaudeCode,
		Files: mergedClaudeFiles,
	})

	for toolName, toolFiles := range twr.ToolFiles {
		mergedToolFiles := toolFiles
		if pkg := db.FindPackage(marketName, profile); pkg != nil {
			if loc := findLocationByPathAndType(pkg, projectPath, toolName); loc != nil {
				mergedToolFiles = domain.MergeLocationFiles(loc.Files, toolFiles)
			}
		}
		db.AddOrUpdatePackage(marketName, profile, headSHA, mergedPkgFiles, domain.InstalledLocation{
			Path:  projectPath,
			Type:  toolName,
			Files: mergedToolFiles,
		})
	}

	// Stage the install database write so it lands in the same atomic
	// commit as the file changes.
	if err := a.stageDBSave(w, *db); err != nil {
		return err
	}
	if err := commit(); err != nil {
		return err
	}
	committed = true

	if !opts.NoDeps && len(fm.RequiresSkills) > 0 {
		if err := a.resolveDependencies(fm.RequiresSkills, marketName, profile, mc, cfg, db, visited, opts, result); err != nil {
			return err
		}
	}

	if !opts.NoDeps && entryType == domain.EntryTypeAgent && isProfileRef(profile) {
		if err := a.installProfileSkills(marketName, profile, mc, cfg, db, visited, opts, result); err != nil {
			return err
		}
	}

	return nil
}

// installEntryFiles writes skill or agent files to the local .claude/ directory
// (via the supplied writer) and returns the InstalledFiles record.
func (a *App) installEntryFiles(w txWriter, clonePath, branch, relPath, localPath string, content []byte) (domain.InstalledFiles, []domain.InstalledFile, error) {
	var files domain.InstalledFiles
	var written []domain.InstalledFile
	entryType := inferEntryType(relPath)

	// projectRel converts an absolute file path under the project tree into a
	// slash-separated path rooted at the project (e.g. ".claude/agents/foo.md").
	projectRel := func(abs string) string {
		root := filepath.Dir(filepath.Clean(localPath))
		for {
			if filepath.Base(root) == ".claude" {
				root = filepath.Dir(root)
				break
			}
			parent := filepath.Dir(root)
			if parent == root {
				break
			}
			root = parent
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return filepath.ToSlash(abs)
		}
		return filepath.ToSlash(rel)
	}

	if entryType == domain.EntryTypeSkill && isDirBasedSkill(relPath) {
		skillDirPath := filepath.Dir(relPath)
		dirFiles, err := a.git.ListDirFiles(clonePath, branch, skillDirPath)
		if err != nil {
			return files, written, err
		}
		for _, f := range dirFiles {
			fileContent, err := a.git.ReadFileAtRef(clonePath, branch, f, "HEAD")
			if err != nil {
				return files, written, err
			}
			fileDest := filepath.Join(localPath, filepath.Base(f))
			if err := w.WriteFile(fileDest, fileContent); err != nil {
				return files, written, err
			}
			written = append(written, domain.InstalledFile{
				Path: projectRel(fileDest),
				XXH:  xxhashHex(fileContent),
			})
		}
		files.Skills = append(files.Skills, filepath.Base(skillDirPath))
	} else if entryType == domain.EntryTypeSkill {
		if err := w.WriteFile(localPath, content); err != nil {
			return files, written, err
		}
		stem := strings.TrimSuffix(filepath.Base(relPath), ".md")
		files.Skills = append(files.Skills, stem)
		written = append(written, domain.InstalledFile{
			Path: projectRel(localPath),
			XXH:  xxhashHex(content),
		})
	} else if entryType == domain.EntryTypeCommand {
		if err := w.WriteFile(localPath, content); err != nil {
			return files, written, err
		}
		files.Commands = append(files.Commands, filepath.Base(relPath))
		written = append(written, domain.InstalledFile{
			Path: projectRel(localPath),
			XXH:  xxhashHex(content),
		})
	} else {
		if err := w.WriteFile(localPath, content); err != nil {
			return files, written, err
		}
		files.Agents = append(files.Agents, filepath.Base(relPath))
		written = append(written, domain.InstalledFile{
			Path: projectRel(localPath),
			XXH:  xxhashHex(content),
		})
	}

	return files, written, nil
}

// mergeAddResult populates an AddResult with tool write paths and warnings.
func mergeAddResult(result *service.AddResult, twr toolWriteResult) {
	if len(twr.ToolWrites) > 0 {
		if result.ToolWrites == nil {
			result.ToolWrites = make(map[string]string)
		}
		for k, v := range twr.ToolWrites {
			result.ToolWrites[k] = v
		}
	}
	result.Warnings = append(result.Warnings, twr.Warnings...)
}

// resolveDependencies installs all required skills for an entry, registering
// cross-market dependencies as needed.
func (a *App) resolveDependencies(
	deps []domain.SkillDep,
	marketName string,
	callerProfile string,
	callerMc *domain.MarketConfig,
	cfg domain.Config,
	db *domain.InstallDatabase,
	visited map[domain.MctRef]bool,
	opts service.AddOpts,
	result *service.AddResult,
) error {
	for _, dep := range deps {
		depMarket := marketName
		if dep.Market != "" {
			depMarketName, nameErr := marketNameFromURL(dep.Market)
			if nameErr != nil {
				return nameErr
			}
			depMarket = depMarketName

			depMc := findMarketConfig(cfg, depMarket)
			if depMc == nil {
				if opts.ConfirmMarket == nil || !opts.ConfirmMarket(dep.Market) {
					return &domain.DomainError{
						Code:    "MARKET_NOT_REGISTERED",
						Message: fmt.Sprintf("skill dependency requires market %q which is not registered", dep.Market),
					}
				}
				if _, addErr := a.AddMarket(dep.Market, service.AddMarketOpts{}); addErr != nil {
					return addErr
				}
				newCfg, err := a.cfg.Load(a.configPath)
				if err != nil {
					return err
				}
				cfg = newCfg
			}
		}

		depFile := dep.File
		// For same-market deps in a hierarchical market, paths in requires_skills
		// are relative to the caller's profile (e.g. "skills/foo/SKILL.md" inside
		// agent at profile "dev/agile-team" resolves to
		// "dev/agile-team/skills/foo/SKILL.md"). Skip when:
		//   - cross-market dep (it carries its own market context),
		//   - market is flat (skills_only),
		//   - caller is not actually under a profile directory (isProfileRef false),
		//   - dep path is already profile-rooted.
		if dep.Market == "" && callerMc != nil && !callerMc.SkillsOnly &&
			isProfileRef(callerProfile) && !strings.HasPrefix(depFile, callerProfile+"/") {
			depFile = callerProfile + "/" + depFile
		}
		if !strings.HasSuffix(depFile, ".md") {
			depFile = strings.TrimSuffix(depFile, "/") + "/SKILL.md"
		}
		// Sub-file deps inside a skill directory (e.g.
		// "skills/foo/references/x.md") are redundant: installing the parent
		// SKILL.md copies the whole skill tree. Redirect to that SKILL.md so
		// the skill is installed once instead of being re-entered through a
		// supporting file (which trips the on-disk "already installed" guard).
		if skillRoot := skillRootForSubFile(depFile); skillRoot != "" {
			depFile = skillRoot + "/SKILL.md"
		}
		skillRef := domain.MctRef(depMarket + "@" + depFile)

		depMc := findMarketConfig(cfg, depMarket)
		if depMc == nil {
			continue
		}

		_, depRelPath, err := skillRef.Parse()
		if err != nil {
			return err
		}

		depOpts := service.AddOpts{
			NoDeps:        true,
			ConfirmMarket: opts.ConfirmMarket,
		}
		if err := a.addInternal(skillRef, depMarket, depRelPath, cfg, depMc, db, visited, depOpts, result); err != nil {
			return err
		}
	}
	return nil
}

// installProfileSkills enumerates all skill entries in the given profile
// and installs any that are not already installed. This ensures that when an
// agent is installed, all sibling skills from the same profile are included.
func (a *App) installProfileSkills(
	marketName, profile string,
	mc *domain.MarketConfig,
	cfg domain.Config,
	db *domain.InstallDatabase,
	visited map[domain.MctRef]bool,
	opts service.AddOpts,
	result *service.AddResult,
) error {
	if profile == "" {
		return nil
	}

	clonePath := a.clonePath(marketName)
	mfiles, err := a.git.ReadMarketFiles(clonePath, mc.Branch)
	if err != nil {
		return err
	}

	prefix := profile + "/"
	for _, mf := range mfiles {
		if !strings.HasPrefix(mf.Path, prefix) {
			continue
		}
		if inferEntryType(mf.Path) != domain.EntryTypeSkill {
			continue
		}
		if isReadme(mf.Path) {
			continue
		}
		if !isSkillEntryPoint(mf.Path) {
			continue
		}

		fileRef := domain.MctRef(marketName + "@" + mf.Path)
		_, fileRelPath, err := fileRef.Parse()
		if err != nil {
			return err
		}

		skillOpts := service.AddOpts{
			Profile:       profile,
			NoDeps:        true,
			ConfirmMarket: opts.ConfirmMarket,
			DryRun:        opts.DryRun,
		}
		if err := a.addInternal(fileRef, marketName, fileRelPath, cfg, mc, db, visited, skillOpts, result); err != nil {
			if err == domain.ErrEntryAlreadyInstalled {
				continue
			}
			return err
		}
	}

	return nil
}

func (a *App) addProfile(
	marketName, relPath string,
	mc *domain.MarketConfig,
	cfg domain.Config,
	db *domain.InstallDatabase,
	visited map[domain.MctRef]bool,
	opts service.AddOpts,
	result *service.AddResult,
) error {
	clonePath := a.clonePath(marketName)
	mfiles, err := a.git.ReadMarketFiles(clonePath, mc.Branch)
	if err != nil {
		return err
	}

	projectPath := projectPath(cfg.LocalPath)

	prefix := relPath + "/"
	installedCount := 0
	skippedCount := 0

	for _, mf := range mfiles {
		if !strings.HasPrefix(mf.Path, prefix) {
			continue
		}
		if isReadme(mf.Path) {
			continue
		}
		if mc.SkillsOnly && !isSkillPath(mf.Path, mc.SkillsPath) {
			continue
		}
		fileRef := domain.MctRef(marketName + "@" + mf.Path)
		fileProfile := refProfile(fileRef)

		// Check if already installed at this location.
		// Disk presence is the source of truth — pkg.Files is package-wide
		// and can carry over from a sibling location after RemoveLocation.
		pkg := db.FindPackage(marketName, fileProfile)
		if pkg != nil {
			atLocation := false
			for _, loc := range pkg.Locations {
				if loc.Path == projectPath {
					atLocation = true
					break
				}
			}
			if atLocation && a.entryFileExistsAt(cfg, mf.Path) {
				skippedCount++
				continue
			}
		}

		_, fileRelPath, err := fileRef.Parse()
		if err != nil {
			return err
		}
		if err := a.addInternal(fileRef, marketName, fileRelPath, cfg, mc, db, visited, opts, result); err != nil {
			return err
		}
		installedCount++
	}

	if installedCount == 0 && skippedCount > 0 {
		return domain.ErrEntryAlreadyInstalled
	}
	return nil
}

// fileInPackage checks whether the given relPath corresponds to a file already
// listed in the InstalledFiles struct.
func fileInPackage(relPath string, files domain.InstalledFiles) bool {
	entryType := inferEntryType(relPath)
	switch entryType {
	case domain.EntryTypeAgent:
		name := filepath.Base(relPath)
		for _, a := range files.Agents {
			if a == name {
				return true
			}
		}
	case domain.EntryTypeCommand:
		name := filepath.Base(relPath)
		for _, c := range files.Commands {
			if c == name {
				return true
			}
		}
	case domain.EntryTypeSkill:
		var name string
		if isDirBasedSkill(relPath) {
			// e.g. "dev/go/skills/bar/SKILL.md" → skill name "bar"
			name = filepath.Base(filepath.Dir(relPath))
		} else {
			// e.g. "dev/go/skills/bar.md" → skill name "bar"
			name = strings.TrimSuffix(filepath.Base(relPath), ".md")
		}
		for _, s := range files.Skills {
			if s == name {
				return true
			}
		}
	}
	return false
}

// isSkillEntryPoint returns true when relPath is a skill's main entry file:
// either a flat skill (e.g. "skills/foo.md") or a dir-based SKILL.md
// (e.g. "skills/foo/SKILL.md"). Support files like "skills/foo/prompt.md"
// return false.
func isSkillEntryPoint(relPath string) bool {
	base := filepath.Base(relPath)
	if isDirBasedSkill(relPath) {
		return base == "SKILL.md"
	}
	return strings.HasSuffix(relPath, ".md")
}

// isDirBasedSkill returns true when relPath is a directory-based skill file
// (e.g. "skills/foo/SKILL.md" or "dev/go/skills/bar/SKILL.md") rather than a
// flat skill .md file (e.g. "skills/bar.md").
func isDirBasedSkill(relPath string) bool {
	parts := strings.Split(relPath, "/")
	for i, p := range parts {
		if p == "skills" && i+2 < len(parts) {
			// There's at least one more directory level between "skills" and the file
			return true
		}
	}
	return false
}

// skillRootForSubFile returns the skill directory path (e.g.
// "dev/go/skills/foo") when relPath points inside a directory-based skill at
// any depth other than the SKILL.md entry point — including
// "skills/foo/references/markers.md" or "skills/foo/scripts/check.sh".
// Returns "" for SKILL.md itself, flat-file skills, and non-skill paths.
func skillRootForSubFile(relPath string) string {
	parts := strings.Split(relPath, "/")
	for i, p := range parts {
		if p == "skills" && i+2 < len(parts) {
			tail := parts[i+2:]
			if len(tail) == 1 && tail[0] == "SKILL.md" {
				return ""
			}
			return strings.Join(parts[:i+2], "/")
		}
	}
	return ""
}

// isSkillDirRef returns true when relPath points to a skill directory
// (e.g. "skills/azure-ai") rather than a concrete file (e.g. "skills/azure-ai/SKILL.md").
func isSkillDirRef(relPath string) bool {
	if strings.HasSuffix(relPath, ".md") {
		return false
	}
	return inferEntryType(relPath) == domain.EntryTypeSkill
}

// isProfileRef returns true when relPath is a profile directory path
// (e.g. "dev/go-hexagonal") rather than a concrete file path.
// A profile path never ends in ".md" and has no "agents" or "skills" segment.
// isProfileRef returns true when relPath is a profile directory path
// (e.g. "dev/go-hexagonal") rather than a concrete file path.
// A profile path never ends in ".md" or ".json" and has no
// agents/skills/commands/hooks segment.
func isProfileRef(relPath string) bool {
	if strings.HasSuffix(relPath, ".md") || strings.HasSuffix(relPath, ".json") {
		return false
	}
	for _, seg := range strings.Split(relPath, "/") {
		switch seg {
		case "agents", "skills", "commands", "hooks":
			return false
		}
	}
	return true
}

func (a *App) Remove(ref domain.MctRef, opts service.RemoveOpts) (service.RemoveResult, error) {
	marketName, relPath, err := ref.Parse()
	if err != nil {
		return service.RemoveResult{}, err
	}
	if isSkillDirRef(relPath) {
		relPath = relPath + "/SKILL.md"
		ref = domain.MctRef(marketName + "@" + relPath)
	}

	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return service.RemoveResult{}, err
	}

	if err := a.idb.Lock(a.cacheDir); err != nil {
		return service.RemoveResult{}, err
	}
	defer func() { _ = a.idb.Unlock(a.cacheDir) }()

	db, err := a.idb.Load(a.cacheDir)
	if err != nil {
		return service.RemoveResult{}, err
	}

	profile := refProfile(ref)

	pkg := db.FindPackage(marketName, profile)
	if pkg == nil {
		return service.RemoveResult{}, domain.ErrEntryNotInstalled
	}

	projectPath := projectPath(cfg.LocalPath)

	entryType := inferEntryType(relPath)
	entryForRemove := domain.Entry{
		Ref:      ref,
		Market:   marketName,
		RelPath:  relPath,
		Filename: filepath.Base(relPath),
		Type:     entryType,
		Profile:  profile,
	}

	w, commit, rollback, err := a.beginWriter("remove:" + string(ref))
	if err != nil {
		return service.RemoveResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = rollback()
		}
	}()

	var removeResult service.RemoveResult
	removeResult.ToolsRemoved = append(removeResult.ToolsRemoved, "claude")

	// Hooks need a dedicated removal path: splice from settings.json by
	// mct_id and update the package's Hooks slice in installdb. The
	// generic deleteInstalledFiles + RemoveLocation flow does not work
	// because hooks share a single settings.json file across multiple
	// hook installs in the same package.
	if entryType == domain.EntryTypeHook {
		hookFile := filepath.Base(relPath)
		settingsAbsPath := filepath.Join(cfg.LocalPath, "settings.json")
		if err := a.removeHookSnippet(w, ref, settingsAbsPath); err != nil {
			return service.RemoveResult{}, err
		}
		// Update installdb: drop hookFile from pkg.Files.Hooks and from
		// the location's Files list.
		if err := a.dropHookFromPackage(&db, marketName, profile, projectPath, hookFile); err != nil {
			return service.RemoveResult{}, err
		}
		if err := a.stageDBSave(w, db); err != nil {
			return service.RemoveResult{}, err
		}
		if err := commit(); err != nil {
			return service.RemoveResult{}, err
		}
		committed = true
		return removeResult, nil
	}

	if opts.AllLocations {
		seen := make(map[string]bool)
		var paths []string
		for _, loc := range pkg.Locations {
			if seen[loc.Path] {
				continue
			}
			seen[loc.Path] = true
			paths = append(paths, loc.Path)
			localPath := filepath.Join(loc.Path, filepath.Base(cfg.LocalPath))
			if err := a.deleteInstalledFiles(w, localPath, pkg.Files); err != nil {
				return service.RemoveResult{}, err
			}
			tr := a.removeFromToolTargets(w, entryForRemove, loc.Path)
			removeResult.ToolsRemoved = append(removeResult.ToolsRemoved, tr.Removed...)
		}
		for _, p := range paths {
			db.RemoveLocation(marketName, profile, p)
		}
	} else {
		if pkg.FindLocation(projectPath) == nil {
			return service.RemoveResult{}, domain.ErrEntryNotInstalled
		}

		if err := a.deleteInstalledFiles(w, cfg.LocalPath, pkg.Files); err != nil {
			return service.RemoveResult{}, err
		}

		tr := a.removeFromToolTargets(w, entryForRemove, projectPath)
		removeResult.ToolsRemoved = append(removeResult.ToolsRemoved, tr.Removed...)

		db.RemoveLocation(marketName, profile, projectPath)
	}

	if err := a.stageDBSave(w, db); err != nil {
		return service.RemoveResult{}, err
	}
	if err := commit(); err != nil {
		return service.RemoveResult{}, err
	}
	committed = true

	return removeResult, nil
}

func (a *App) deleteInstalledFiles(w txWriter, localPath string, files domain.InstalledFiles) error {
	for _, skill := range files.Skills {
		if err := w.DeleteAll(filepath.Join(localPath, "skills", skill)); err != nil {
			return err
		}
	}
	for _, agent := range files.Agents {
		if err := w.DeleteFile(filepath.Join(localPath, "agents", agent)); err != nil {
			return err
		}
	}
	for _, cmd := range files.Commands {
		if err := w.DeleteFile(filepath.Join(localPath, "commands", cmd)); err != nil {
			return err
		}
	}
	// Hooks are spliced from settings.json by the caller (App.Remove
	// dispatches to removeHookSnippet because the splice needs the ref to
	// compute the mct_id; that information isn't on InstalledFiles). Skip
	// here so we don't accidentally delete the settings.json file.
	return nil
}

func (a *App) Prune(opts service.PruneOpts) ([]service.PruneResult, error) {
	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return nil, err
	}

	// Lock installdb
	if err := a.idb.Lock(a.cacheDir); err != nil {
		return nil, err
	}
	defer func() { _ = a.idb.Unlock(a.cacheDir) }()

	// Load installdb
	db, err := a.idb.Load(a.cacheDir)
	if err != nil {
		return nil, err
	}

	projectPath := projectPath(cfg.LocalPath)

	var results []service.PruneResult

	for _, im := range db.Markets {
		mc := findMarketConfig(cfg, im.Market)
		if mc == nil {
			continue
		}
		clonePath := a.clonePath(im.Market)

		for _, pkg := range im.Packages {
			// Check if this package is installed at the current project path
			atLocation := false
			for _, loc := range pkg.Locations {
				if loc.Path == projectPath {
					atLocation = true
					break
				}
			}
			if !atLocation {
				continue
			}

			// Check if any of the files still exist in the cached clone
			allGone := true
			for _, skill := range pkg.Files.Skills {
				// Try to find the skill file in the clone
				_, err := a.git.FileVersion(clonePath, skill)
				if err == nil {
					allGone = false
					break
				}
			}
			if allGone {
				for _, agent := range pkg.Files.Agents {
					_, err := a.git.FileVersion(clonePath, agent)
					if err == nil {
						allGone = false
						break
					}
				}
			}

			if !allGone {
				continue
			}

			ref := domain.MctRef(pkg.Profile)

			if opts.AllKeep {
				results = append(results, service.PruneResult{Ref: ref, Action: "kept"})
			} else if opts.AllRemove {
				w, commit, rollback, err := a.beginWriter("prune:" + im.Market + ":" + pkg.Profile)
				if err != nil {
					results = append(results, service.PruneResult{Ref: ref, Action: "remove", Err: err})
					continue
				}
				if err := a.deleteInstalledFiles(w, cfg.LocalPath, pkg.Files); err != nil {
					_ = rollback()
					results = append(results, service.PruneResult{Ref: ref, Action: "remove", Err: err})
					continue
				}
				db.RemoveLocation(im.Market, pkg.Profile, projectPath)
				if err := a.stageDBSave(w, db); err != nil {
					_ = rollback()
					results = append(results, service.PruneResult{Ref: ref, Action: "remove", Err: err})
					continue
				}
				if err := commit(); err != nil {
					results = append(results, service.PruneResult{Ref: ref, Action: "remove", Err: err})
					continue
				}
				results = append(results, service.PruneResult{Ref: ref, Action: "removed"})
			}
		}
	}

	return results, nil
}

func (a *App) Init(opts service.InitOpts) error {
	localPath := opts.LocalPath
	if localPath == "" {
		localPath = "."
	}

	if a.cfg.Exists(a.configPath) {
		return domain.ErrAlreadyInitialized
	}

	// Ensure cache and config directories exist.
	if err := a.fs.MkdirAll(a.cacheDir); err != nil {
		return err
	}
	configDir := filepath.Dir(a.configPath)
	if configDir != "" && configDir != "." {
		if err := a.fs.MkdirAll(configDir); err != nil {
			return err
		}
	}

	markets := opts.Markets
	if markets == nil {
		defaults, err := loadDefaultSkillMarkets()
		if err != nil {
			return err
		}
		markets = defaults
	}

	cfg := domain.Config{
		LocalPath: localPath,
		Tools:     map[string]bool{"claude": true},
		Markets:   []domain.MarketConfig{},
	}

	for _, url := range markets {
		name, err := marketNameFromURL(url)
		if err != nil {
			return err
		}
		clonePath := filepath.Join(a.cacheDir, marketDirName(name))
		if _, statErr := os.Stat(clonePath); statErr == nil {
			// Clone already exists (e.g. previous failed init), just fetch.
			branch, brErr := a.git.DefaultBranch(clonePath)
			if brErr != nil {
				return brErr
			}
			if _, fetchErr := a.git.Fetch(clonePath, branch); fetchErr != nil {
				return fetchErr
			}
		} else {
			if err := a.git.Clone(url, clonePath); err != nil {
				return err
			}
		}

		branch, err := a.git.DefaultBranch(clonePath)
		if err != nil {
			return err
		}
		mc := domain.MarketConfig{
			Name:   name,
			URL:    url,
			Branch: branch,
		}
		cfg.Markets = append(cfg.Markets, mc)

		sha, err := a.git.RemoteHEAD(clonePath, branch)
		if err != nil {
			return err
		}
		if err := a.state.SetMarketSyncClean(a.cacheDir, name, sha); err != nil {
			return err
		}
	}

	if err := a.cfg.Save(a.configPath, cfg); err != nil {
		return err
	}

	// Write default tool-mappings.yml if it doesn't already exist.
	if a.toolMappings != nil {
		mappingsPath := filepath.Join(configDir, "tool-mappings.yml")
		if !a.toolMappings.ToolMappingsExist(mappingsPath) {
			defaults := a.toolMappings.DefaultToolMappings()
			if err := a.toolMappings.SaveToolMappings(mappingsPath, defaults); err != nil {
				return err
			}
		}
	}

	return nil
}

type defaultMarket struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func loadDefaultSkillMarkets() ([]string, error) {
	data, err := assets.FS.ReadFile("skills.json")
	if err != nil {
		return nil, fmt.Errorf("reading embedded skills.json: %w", err)
	}
	var markets []defaultMarket
	if err := json.Unmarshal(data, &markets); err != nil {
		return nil, fmt.Errorf("parsing embedded skills.json: %w", err)
	}
	urls := make([]string, len(markets))
	for i, m := range markets {
		urls[i] = m.URL
	}
	return urls, nil
}

func (a *App) resolveLocalPath(cfg domain.Config, relPath string) (string, error) {
	cleaned := filepath.Clean(relPath)
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", &domain.DomainError{
			Code:    "UNSAFE_PATH",
			Message: "entry path must be relative and cannot traverse upward: " + relPath,
		}
	}

	filename := filepath.Base(cleaned)
	parts := strings.Split(cleaned, string(filepath.Separator))
	for i, p := range parts {
		if p == "agents" {
			return filepath.Join(cfg.LocalPath, p, filename), nil
		}
		if p == "commands" {
			return filepath.Join(cfg.LocalPath, p, filename), nil
		}
		if p == "hooks" {
			// Hooks are merged into a single settings.json file. The
			// "local path" returned here is the settings.json path that
			// the install/remove helpers operate on.
			return filepath.Join(cfg.LocalPath, "settings.json"), nil
		}
		if p == "skills" {
			// For directory-based skills: return <localPath>/skills/<skillname>/
			if i+2 < len(parts) {
				skillName := parts[i+1]
				return filepath.Join(cfg.LocalPath, p, skillName), nil
			}
			// Flat file: skills/bar.md → <localPath>/skills/bar
			stem := strings.TrimSuffix(filename, ".md")
			return filepath.Join(cfg.LocalPath, p, stem), nil
		}
	}
	return filepath.Join(cfg.LocalPath, filename), nil
}

// refProfile extracts the profile portion from a full ref (without the market prefix).
// "market@seg1/seg2/agents/foo.md" -> "seg1/seg2"
// "market@skills/foo/SKILL.md"     -> "skills/foo"
// Falls back to "" if there are fewer than 2 path segments.
func refProfile(ref domain.MctRef) string {
	_, relPath, err := ref.Parse()
	if err != nil {
		return string(ref)
	}
	parts := strings.Split(relPath, "/")
	if len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return ""
}

func inferEntryType(relPath string) domain.EntryType {
	parts := strings.Split(relPath, "/")
	for _, p := range parts {
		switch p {
		case "agents":
			return domain.EntryTypeAgent
		case "skills":
			return domain.EntryTypeSkill
		case "commands":
			return domain.EntryTypeCommand
		case "hooks":
			// Hooks are .json snippets under hooks/. The case where a
			// non-.json file ends up here (e.g. README.md inside a hooks
			// directory) is treated as not-a-hook so the lint walker can
			// still classify or skip it explicitly.
			if strings.HasSuffix(relPath, ".json") {
				return domain.EntryTypeHook
			}
			return ""
		}
	}
	return ""
}

// entryFileExistsAt reports whether the on-disk target for relPath (an agent
// or skill entry) already exists at the local install path. This is the
// authoritative "is this entry installed at this location" check — pkg.Files
// in the install DB is package-wide and stays populated when other locations
// of the same package remain after a RemoveLocation.
func (a *App) entryFileExistsAt(cfg domain.Config, relPath string) bool {
	target, err := a.resolveLocalPath(cfg, relPath)
	if err != nil {
		return false
	}
	_, err = os.Stat(target)
	return err == nil
}
