package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/cespare/xxhash/v2"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

// detectDrift compares installed files at a location against the cached clone
// at the version recorded in installdb. Returns list of files that differ.
// detectDrift compares installed files at a location against the cached clone
// at the version recorded in installdb. Returns the union of modified and
// locally-deleted files (legacy single-list view).
//
// New callers should prefer detectDriftSplit which returns the two
// categories separately.
func (a *App) detectDrift(pkg domain.InstalledPackage, location, clonePath, branch string) []string {
	modified, deleted := a.detectDriftSplit(pkg, location, clonePath, branch)
	if len(deleted) == 0 {
		return modified
	}
	if len(modified) == 0 {
		return deleted
	}
	return append(modified, deleted...)
}

// detectDriftSplit compares installed files at a location against the cached
// clone at the version recorded in installdb. Returns two lists:
//   - modified: files that exist on disk but whose content hash differs from
//     what was recorded at install time
//   - deleted: files that were recorded as installed but whose on-disk copy
//     no longer exists (user-side deletion, not upstream removal)
//
// detectDriftSplit compares installed files at a location against the cached
// clone at the version recorded in installdb. Returns two lists:
//   - modified: files that exist on disk but whose content hash differs from
//     what was recorded at install time
//   - deleted: files that were recorded as installed but whose on-disk copy
//     no longer exists (user-side deletion, not upstream removal)
//
// detectDriftSplit compares installed files at a location against the cached
// clone at the version recorded in installdb. Returns two lists:
//   - modified: files that exist on disk but whose content hash differs from
//     what was recorded at install time
//   - deleted: files that were recorded as installed but whose on-disk copy
//     no longer exists (user-side deletion, not upstream removal)
//
// All filesystem reads go through a.fs so the behaviour is identical between
// the v2 (per-location hash) and legacy (git-comparison) paths and stays
// fully fakeable in tests.
func (a *App) detectDriftSplit(pkg domain.InstalledPackage, location, clonePath, branch string) (modified, deleted []string) {
	classify := func(path string, err error, target *[]string) {
		if os.IsNotExist(err) {
			deleted = append(deleted, path)
		} else {
			*target = append(*target, path)
		}
	}

	// Prefer per-location file hashes (v2 schema). Fall back to git-comparison
	// for legacy packages whose locations have no Files list yet.
	loc := pkg.FindLocation(location)
	if loc != nil && len(loc.Files) > 0 && loc.Type == domain.RuntimeTypeClaudeCode {
		for _, f := range loc.Files {
			abs := filepath.Join(location, f.Path)
			data, err := a.fs.ReadFile(abs)
			if err != nil {
				classify(f.Path, err, &modified)
				continue
			}
			if xxhashHex(data) != f.XXH {
				modified = append(modified, f.Path)
			}
		}
		return modified, deleted
	}

	localBase := filepath.Join(location, ".claude")

	for _, skill := range pkg.Files.Skills {
		// First check if we can read the original SKILL.md at the recorded version.
		// If the original doesn't exist at that ref, skip drift check for this skill.
		repoRelPath := a.skillFileRepoPath(pkg.Profile, skill, "SKILL.md")
		_, origErr := a.git.ReadFileAtRef(clonePath, branch, repoRelPath, pkg.Version)
		if origErr != nil {
			continue
		}

		skillDir := filepath.Join(localBase, "skills", skill)
		entries, err := a.fs.ReadDir(skillDir)
		if err != nil {
			classify("skills/"+skill, err, &modified)
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			fileRepoPath := a.skillFileRepoPath(pkg.Profile, skill, entry.Name())
			originalContent, err := a.git.ReadFileAtRef(clonePath, branch, fileRepoPath, pkg.Version)
			if err != nil {
				continue
			}

			localPath := filepath.Join(skillDir, entry.Name())
			localContent, err := a.fs.ReadFile(localPath)
			if err != nil {
				classify("skills/"+skill+"/"+entry.Name(), err, &modified)
				continue
			}

			if xxhash.Sum64(localContent) != xxhash.Sum64(originalContent) {
				modified = append(modified, "skills/"+skill+"/"+entry.Name())
			}
		}
	}
	for _, agent := range pkg.Files.Agents {
		repoRelPath := a.agentFileRepoPath(pkg.Profile, agent)
		originalContent, err := a.git.ReadFileAtRef(clonePath, branch, repoRelPath, pkg.Version)
		if err != nil {
			continue
		}

		localPath := filepath.Join(localBase, "agents", agent)
		localContent, err := a.fs.ReadFile(localPath)
		if err != nil {
			classify("agents/"+agent, err, &modified)
			continue
		}

		if xxhash.Sum64(localContent) != xxhash.Sum64(originalContent) {
			modified = append(modified, "agents/"+agent)
		}
	}
	return modified, deleted
}

// skillFileRepoPath derives the repo-relative path for a skill file given a
// package profile and the skill leaf name.
// Profile "dev/go" + skill "bar" + file "SKILL.md" -> "dev/go/skills/bar/SKILL.md"
// Profile ""       + skill "bar" + file "SKILL.md" -> "skills/bar/SKILL.md"
// Profile "skills/foo" + skill "foo" + file "SKILL.md" -> "skills/foo/SKILL.md"
func (a *App) skillFileRepoPath(profile, skill, filename string) string {
	// When the profile itself is a skill directory (e.g. "skills/azure-aigateway"),
	// the skill name is already embedded in the profile path. Appending
	// "/skills/<name>" again would produce a broken double path.
	if strings.HasSuffix(profile, "/"+skill) {
		parts := strings.Split(profile, "/")
		for _, p := range parts {
			if p == "skills" {
				return profile + "/" + filename
			}
		}
	}
	if profile != "" {
		return profile + "/skills/" + skill + "/" + filename
	}
	return "skills/" + skill + "/" + filename
}

// agentFileRepoPath derives the repo-relative path for an agent file given a
// package profile and the agent filename.
// Profile "dev/go" + agent "foo.md" -> "dev/go/agents/foo.md"
// Profile ""       + agent "foo.md" -> "agents/foo.md"
func (a *App) agentFileRepoPath(profile, agent string) string {
	if profile != "" {
		return profile + "/agents/" + agent
	}
	return "agents/" + agent
}

// commandFileRepoPath derives the repo-relative path for a command file given a
// package profile and the command filename.
// Profile "dev/go" + command "foo.md" -> "dev/go/commands/foo.md"
// Profile ""       + command "foo.md" -> "commands/foo.md"
func (a *App) commandFileRepoPath(profile, command string) string {
	if profile != "" {
		return profile + "/commands/" + command
	}
	return "commands/" + command
}

// skillDirRepoPath returns the repo-relative directory path for a skill,
// accounting for profiles that are themselves skill directories.
// Profile "dev/go"        + skill "bar" -> "dev/go/skills/bar"
// Profile ""              + skill "bar" -> "skills/bar"
// Profile "skills/foo"    + skill "foo" -> "skills/foo"
func (a *App) skillDirRepoPath(profile, skill string) string {
	return strings.TrimSuffix(a.skillFileRepoPath(profile, skill, "SKILL.md"), "/SKILL.md")
}

// packageFileRefs returns all MctRefs for the files in an installed package.
func (a *App) packageFileRefs(market string, pkg domain.InstalledPackage) []domain.MctRef {
	refs := make([]domain.MctRef, 0, len(pkg.Files.Skills)+len(pkg.Files.Agents)+len(pkg.Files.Commands)+len(pkg.Files.Hooks))
	for _, skill := range pkg.Files.Skills {
		repoPath := a.skillFileRepoPath(pkg.Profile, skill, "SKILL.md")
		refs = append(refs, domain.MctRef(market+"@"+repoPath))
	}
	for _, agent := range pkg.Files.Agents {
		repoPath := a.agentFileRepoPath(pkg.Profile, agent)
		refs = append(refs, domain.MctRef(market+"@"+repoPath))
	}
	for _, cmd := range pkg.Files.Commands {
		repoPath := a.commandFileRepoPath(pkg.Profile, cmd)
		refs = append(refs, domain.MctRef(market+"@"+repoPath))
	}
	for _, hook := range pkg.Files.Hooks {
		repoPath := a.hookFileRepoPath(pkg.Profile, hook)
		refs = append(refs, domain.MctRef(market+"@"+repoPath))
	}
	return refs
}

func (a *App) Check(opts service.CheckOpts) ([]domain.EntryStatus, error) {
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

	var statuses []domain.EntryStatus

	for _, im := range db.Markets {
		if opts.Market != "" && im.Market != opts.Market {
			continue
		}

		mc := findMarketConfig(cfg, im.Market)
		if mc == nil {
			continue
		}

		clonePath := a.clonePath(im.Market)

		for _, pkg := range im.Packages {
			if pkg.FindLocation(projectPath) == nil {
				continue
			}

			modifiedFiles, deletedFiles := a.detectDriftSplit(pkg, projectPath, clonePath, mc.Branch)
			hasModified := len(modifiedFiles) > 0
			hasDeletedOnly := !hasModified && len(deletedFiles) > 0

			// Per-hook drift: a hook is "drifted" when its mct_id-tagged
			// body in settings.json no longer matches the recorded
			// checksum (or has been removed).
			driftedHooks := a.detectHookDrift(im.Market, pkg, projectPath)
			driftedHookSet := make(map[string]struct{}, len(driftedHooks))
			for _, h := range driftedHooks {
				driftedHookSet[h] = struct{}{}
			}

			headSHA, err := a.git.RemoteHEAD(clonePath, mc.Branch)
			hasUpdate := err == nil && headSHA != pkg.Version

			pkgState := domain.StateClean
			switch {
			case hasModified && hasUpdate:
				pkgState = domain.StateUpdateAndDrift
			case hasModified:
				pkgState = domain.StateDrift
			case hasDeletedOnly:
				pkgState = domain.StateLocallyDeleted
			case hasUpdate:
				pkgState = domain.StateUpdateAvailable
			}

			for _, ref := range a.packageFileRefs(im.Market, pkg) {
				_, refRelPath, _ := ref.Parse()
				refType := inferEntryType(refRelPath)

				state := pkgState
				if refType == domain.EntryTypeHook {
					hookFile := filepath.Base(refRelPath)
					_, drifted := driftedHookSet["hooks/"+hookFile]
					switch {
					case drifted && hasUpdate:
						state = domain.StateUpdateAndDrift
					case drifted:
						state = domain.StateDrift
					case hasUpdate:
						state = domain.StateUpdateAvailable
					default:
						state = domain.StateClean
					}
				}

				es := domain.EntryStatus{
					Ref:   ref,
					State: state,
				}

				entryForCheck := domain.Entry{
					Ref:      ref,
					Market:   im.Market,
					RelPath:  refRelPath,
					Filename: filepath.Base(refRelPath),
					Type:     refType,
					Profile:  pkg.Profile,
				}
				if toolStates := a.detectToolDrift(entryForCheck, pkg, projectPath); len(toolStates) > 0 {
					es.ToolStates = toolStates
				}

				statuses = append(statuses, es)
			}
		}
	}

	return statuses, nil
}

func (a *App) Refresh(opts service.RefreshOpts) ([]service.RefreshResult, error) {
	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return nil, err
	}

	syncState, err := a.state.LoadSyncState(a.cacheDir)
	if err != nil {
		return nil, err
	}

	var results []service.RefreshResult

	var prunedLocations []string
	if !opts.DryRun {
		pruned, pruneErr := a.pruneStaleLocations()
		if pruneErr != nil {
			return nil, pruneErr
		}
		prunedLocations = pruned
	}

	for _, mc := range cfg.Markets {
		if opts.Market != "" && mc.Name != opts.Market {
			continue
		}

		clonePath := a.clonePath(mc.Name)

		oldSHA := ""
		if ms, ok := syncState.Markets[mc.Name]; ok {
			oldSHA = ms.LastSyncedSHA
		}

		if !opts.DryRun {
			if err := a.state.SetMarketSyncDirty(a.cacheDir, mc.Name); err != nil {
				results = append(results, service.RefreshResult{
					Market: mc.Name,
					OldSHA: oldSHA,
					Err:    err,
				})
				continue
			}
		}

		newSHA, err := a.git.Fetch(clonePath, mc.Branch)
		if err != nil {
			results = append(results, service.RefreshResult{
				Market: mc.Name,
				OldSHA: oldSHA,
				Err:    err,
			})
			continue
		}

		var changedFiles int
		if oldSHA != "" {
			diffs, diffErr := a.git.DiffSinceCommit(clonePath, mc.Branch, oldSHA)
			if diffErr == nil {
				for _, d := range diffs {
					path := d.To
					if path == "" {
						path = d.From
					}
					if mc.SkillsOnly && !isSkillPath(path, mc.SkillsPath) {
						continue
					}
					changedFiles++
				}
			}
		}

		// Count updates available from installdb
		updatesAvailable := 0
		db, dbErr := a.idb.Load(a.cacheDir)
		if dbErr == nil {
			im := db.FindMarket(mc.Name)
			if im != nil {
				for _, pkg := range im.Packages {
					if pkg.Version != newSHA {
						updatesAvailable++
					}
				}
			}
		}

		var prunedFiles []string
		if !opts.DryRun {
			pf, pruneErr := a.pruneRemovedUpstreamFiles(mc.Name, mc.Branch, clonePath)
			if pruneErr == nil {
				prunedFiles = pf
			}
		}

		if !opts.DryRun {
			if err := a.state.SetMarketSyncClean(a.cacheDir, mc.Name, newSHA); err != nil {
				results = append(results, service.RefreshResult{
					Market: mc.Name,
					OldSHA: oldSHA,
					NewSHA: newSHA,
					Err:    err,
				})
				continue
			}
		}

		rr := service.RefreshResult{
			Market:           mc.Name,
			OldSHA:           oldSHA,
			NewSHA:           newSHA,
			ChangedFiles:     changedFiles,
			UpdatesAvailable: updatesAvailable,
			PrunedFiles:      prunedFiles,
		}
		if len(prunedLocations) > 0 {
			rr.PrunedLocations = prunedLocations
			prunedLocations = nil
		}
		results = append(results, rr)
	}

	if len(prunedLocations) > 0 {
		results = append(results, service.RefreshResult{PrunedLocations: prunedLocations})
	}

	return results, nil
}

func (a *App) Update(opts service.UpdateOpts) ([]service.UpdateResult, error) {
	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return nil, err
	}

	if err := a.idb.Lock(a.cacheDir); err != nil {
		return nil, err
	}
	defer func() { _ = a.idb.Unlock(a.cacheDir) }()

	db, err := a.idb.Load(a.cacheDir)
	if err != nil {
		return nil, err
	}

	var results []service.UpdateResult

	for _, mc := range cfg.Markets {
		if opts.Market != "" && mc.Name != opts.Market {
			continue
		}

		clonePath := a.clonePath(mc.Name)

		im := db.FindMarket(mc.Name)
		if im == nil {
			continue
		}

		var refRelPath string
		if opts.Ref != "" {
			refMarket, rp, refErr := opts.Ref.Parse()
			if refErr != nil || refMarket != mc.Name {
				continue
			}
			refRelPath = rp
		}

		for pi := range im.Packages {
			pkg := &im.Packages[pi]

			if pkg.Version == "" {
				continue
			}

			if refRelPath != "" && !fileInPackage(refRelPath, pkg.Files) {
				continue
			}

			diffs, err := a.git.DiffSinceCommit(clonePath, mc.Branch, pkg.Version)
			if err != nil {
				continue
			}

			affected := a.findAffectedPackages(im, diffs, mc)
			if _, ok := affected[pi]; !ok {
				continue
			}

			seen := make(map[string]bool)
			for _, location := range pkg.Locations {
				if seen[location.Path] {
					continue
				}
				seen[location.Path] = true
				r := a.updatePackageAtLocation(updateCtx{
					mc:        mc,
					im:        im,
					pkg:       pkg,
					pkgIdx:    pi,
					location:  location.Path,
					clonePath: clonePath,
					cfg:       cfg,
					opts:      opts,
					db:        &db,
				})
				results = append(results, r)
			}
		}
	}

	return results, nil
}

// updateCtx bundles the parameters needed by updatePackageAtLocation.
// updateCtx bundles the parameters needed by updatePackageAtLocation.
type updateCtx struct {
	mc        domain.MarketConfig
	im        *domain.InstalledMarket
	pkg       *domain.InstalledPackage
	pkgIdx    int
	location  string
	clonePath string
	cfg       domain.Config
	opts      service.UpdateOpts
	db        *domain.InstallDatabase
}

// findAffectedPackages identifies which installed packages in a market are
// touched by the given diffs. Returns a map of package index -> package.
func (a *App) findAffectedPackages(im *domain.InstalledMarket, diffs []domain.FileDiff, mc domain.MarketConfig) map[int]*domain.InstalledPackage {
	affected := make(map[int]*domain.InstalledPackage)

	for _, diff := range diffs {
		filePath := diff.To
		if filePath == "" {
			filePath = diff.From
		}

		if mc.SkillsOnly && !isSkillPath(filePath, mc.SkillsPath) {
			continue
		}

		if diff.Action == domain.DiffDelete {
			continue
		}

		for pi := range im.Packages {
			pkg := &im.Packages[pi]

			profilePath := pkg.Profile
			if profilePath != "" {
				prefix := profilePath + "/"
				if !strings.HasPrefix(filePath, prefix) && filePath != profilePath {
					if !fileInPackage(filePath, pkg.Files) {
						continue
					}
				}
			}

			if fileInPackage(filePath, pkg.Files) {
				affected[pi] = pkg
			}
		}
	}

	return affected
}

// updatePackageAtLocation performs the actual update of a single package at a
// single location: drift check, file replacement, and tool re-transform.
// updatePackageAtLocation performs the actual update of a single package at a
// single location: drift check, file replacement, and tool re-transform. It
// runs inside its own transaction so that on failure neither disk nor the
// install database mutate.
func (a *App) updatePackageAtLocation(ctx updateCtx) service.UpdateResult {
	oldVersion := domain.MctVersion(ctx.pkg.Version)

	driftFiles := a.detectDrift(*ctx.pkg, ctx.location, ctx.clonePath, ctx.mc.Branch)

	if len(driftFiles) > 0 {
		if ctx.opts.AllKeep {
			return service.UpdateResult{
				Ref:        a.packagePrimaryRef(ctx.mc.Name, ctx.pkg),
				Location:   ctx.location,
				Action:     "kept",
				OldVersion: oldVersion,
				DriftFiles: driftFiles,
			}
		}
		if !ctx.opts.AllMerge {
			return service.UpdateResult{
				Ref:        a.packagePrimaryRef(ctx.mc.Name, ctx.pkg),
				Location:   ctx.location,
				Action:     "drift",
				OldVersion: oldVersion,
				DriftFiles: driftFiles,
			}
		}
	}

	w, commit, rollback, err := a.beginWriter("update:" + ctx.mc.Name + ":" + ctx.pkg.Profile)
	if err != nil {
		return service.UpdateResult{
			Ref:      a.packagePrimaryRef(ctx.mc.Name, ctx.pkg),
			Location: ctx.location,
			Action:   "error",
			Err:      err,
		}
	}
	committed := false
	defer func() {
		if !committed {
			_ = rollback()
		}
	}()

	// Snapshot the previous claude-code file set BEFORE writing, so we can
	// diff and prune orphans (files removed in the upstream version).
	var oldClaudeFiles []domain.InstalledFile
	if loc := findLocationByPathAndType(ctx.pkg, ctx.location, domain.RuntimeTypeClaudeCode); loc != nil {
		oldClaudeFiles = append(oldClaudeFiles, loc.Files...)
	}

	newFiles, claudeFiles := a.copyUpdatedFiles(w, ctx)

	a.pruneRemovedFiles(w, ctx.location, oldClaudeFiles, claudeFiles)

	newSHA, err := a.git.RemoteHEAD(ctx.clonePath, ctx.mc.Branch)
	if err != nil {
		return service.UpdateResult{
			Ref:      a.packagePrimaryRef(ctx.mc.Name, ctx.pkg),
			Location: ctx.location,
			Action:   "error",
			Err:      err,
		}
	}

	// Replace package-level Files and the claude-code location's Files
	// with the new authoritative set.
	ctx.im.Packages[ctx.pkgIdx].Version = newSHA
	ctx.im.Packages[ctx.pkgIdx].Files = newFiles

	if loc := findLocationByPathAndType(&ctx.im.Packages[ctx.pkgIdx], ctx.location, domain.RuntimeTypeClaudeCode); loc != nil {
		loc.Files = claudeFiles
	} else {
		ctx.im.Packages[ctx.pkgIdx].Locations = append(ctx.im.Packages[ctx.pkgIdx].Locations, domain.InstalledLocation{
			Path:  ctx.location,
			Type:  domain.RuntimeTypeClaudeCode,
			Files: claudeFiles,
		})
	}

	a.reTransformToolTargets(w, ctx.mc, ctx.im, ctx.pkgIdx, newFiles, ctx.location)

	if err := a.stageDBSave(w, *ctx.db); err != nil {
		return service.UpdateResult{
			Ref:      a.packagePrimaryRef(ctx.mc.Name, ctx.pkg),
			Location: ctx.location,
			Action:   "error",
			Err:      err,
		}
	}
	if err := commit(); err != nil {
		return service.UpdateResult{
			Ref:      a.packagePrimaryRef(ctx.mc.Name, ctx.pkg),
			Location: ctx.location,
			Action:   "error",
			Err:      err,
		}
	}
	committed = true

	return service.UpdateResult{
		Ref:        a.packagePrimaryRef(ctx.mc.Name, ctx.pkg),
		Location:   ctx.location,
		Action:     "update",
		OldVersion: oldVersion,
		NewVersion: domain.MctVersion(newSHA),
		DriftFiles: driftFiles,
	}
}

// copyUpdatedFiles reads updated skill and agent files from the cached clone
// and writes them to the local project directory.
// copyUpdatedFiles reads updated skill, agent, command, and hook files from
// the cached clone and writes them to the local project directory.
func (a *App) copyUpdatedFiles(w txWriter, ctx updateCtx) (domain.InstalledFiles, []domain.InstalledFile) {
	var newFiles domain.InstalledFiles
	var written []domain.InstalledFile

	projectRel := func(abs string) string {
		rel, err := filepath.Rel(ctx.location, abs)
		if err != nil {
			return filepath.ToSlash(abs)
		}
		return filepath.ToSlash(rel)
	}

	for _, skill := range ctx.pkg.Files.Skills {
		skillDirPath := a.skillDirRepoPath(ctx.pkg.Profile, skill)
		dirFiles, err := a.git.ListDirFiles(ctx.clonePath, ctx.mc.Branch, skillDirPath)
		if err != nil || len(dirFiles) == 0 {
			continue
		}
		localSkillDir := filepath.Join(ctx.cfg.LocalPath, "skills", skill)
		wroteAny := false
		for _, f := range dirFiles {
			content, err := a.git.ReadFileAtRef(ctx.clonePath, ctx.mc.Branch, f, "HEAD")
			if err != nil {
				continue
			}
			rel, err := filepath.Rel(skillDirPath, f)
			if err != nil {
				continue
			}
			dest := filepath.Join(localSkillDir, rel)
			if err := w.WriteFile(dest, content); err != nil {
				continue
			}
			written = append(written, domain.InstalledFile{
				Path: projectRel(dest),
				XXH:  xxhashHex(content),
			})
			wroteAny = true
		}
		if wroteAny {
			newFiles.Skills = append(newFiles.Skills, skill)
		}
	}

	for _, agent := range ctx.pkg.Files.Agents {
		repoPath := a.agentFileRepoPath(ctx.pkg.Profile, agent)
		content, err := a.git.ReadFileAtRef(ctx.clonePath, ctx.mc.Branch, repoPath, "HEAD")
		if err != nil {
			continue
		}
		localPath := filepath.Join(ctx.cfg.LocalPath, "agents", agent)
		if err := w.WriteFile(localPath, content); err != nil {
			continue
		}
		written = append(written, domain.InstalledFile{
			Path: projectRel(localPath),
			XXH:  xxhashHex(content),
		})
		newFiles.Agents = append(newFiles.Agents, agent)
	}

	for _, cmd := range ctx.pkg.Files.Commands {
		repoPath := a.commandFileRepoPath(ctx.pkg.Profile, cmd)
		content, err := a.git.ReadFileAtRef(ctx.clonePath, ctx.mc.Branch, repoPath, "HEAD")
		if err != nil {
			continue
		}
		localPath := filepath.Join(ctx.cfg.LocalPath, "commands", cmd)
		if err := w.WriteFile(localPath, content); err != nil {
			continue
		}
		written = append(written, domain.InstalledFile{
			Path: projectRel(localPath),
			XXH:  xxhashHex(content),
		})
		newFiles.Commands = append(newFiles.Commands, cmd)
	}

	// Hooks: upstream body changes require splice-then-merge under the
	// same mct_id so existing entries with the old body get replaced.
	for _, hookFile := range ctx.pkg.Files.Hooks {
		repoPath := a.hookFileRepoPath(ctx.pkg.Profile, hookFile)
		content, err := a.git.ReadFileAtRef(ctx.clonePath, ctx.mc.Branch, repoPath, "HEAD")
		if err != nil {
			continue
		}
		snippet, err := domain.ParseHookSnippet(content)
		if err != nil {
			continue
		}
		ref := domain.MctRef(ctx.mc.Name + "@" + repoPath)
		settingsAbsPath := filepath.Join(ctx.cfg.LocalPath, "settings.json")
		if err := a.removeHookSnippet(w, ref, settingsAbsPath); err != nil {
			continue
		}
		hookWritten, err := a.installHookSnippet(w, ref, snippet, settingsAbsPath, hookFile)
		if err != nil {
			continue
		}
		written = append(written, hookWritten...)
		newFiles.Hooks = append(newFiles.Hooks, hookFile)
	}

	return newFiles, written
}

// reTransformToolTargets re-transforms all files in a package to enabled tool
// targets and updates the tool checksums in the install database.
// reTransformToolTargets re-transforms all files in a package to enabled tool
// targets and updates the tool checksums in the install database.
func (a *App) reTransformToolTargets(w txWriter, mc domain.MarketConfig, im *domain.InstalledMarket, pkgIdx int, files domain.InstalledFiles, location string) {
	pkg := &im.Packages[pkgIdx]

	// Snapshot per-tool old file sets BEFORE re-writing so we can prune
	// orphans (files dropped from the new version) per tool.
	oldByTool := make(map[string][]domain.InstalledFile)
	for _, loc := range pkg.Locations {
		if loc.Path != location || loc.Type == domain.RuntimeTypeClaudeCode {
			continue
		}
		oldByTool[loc.Type] = append([]domain.InstalledFile(nil), loc.Files...)
	}

	newByTool := make(map[string][]domain.InstalledFile)
	accumulate := func(twr toolWriteResult) {
		for toolName, toolFiles := range twr.ToolFiles {
			newByTool[toolName] = append(newByTool[toolName], toolFiles...)
		}
	}

	for _, skill := range files.Skills {
		repoPath := a.skillFileRepoPath(pkg.Profile, skill, "SKILL.md")
		content, err := a.git.ReadFileAtRef(a.clonePath(mc.Name), mc.Branch, repoPath, "HEAD")
		if err != nil {
			continue
		}
		entry := domain.Entry{
			Ref:      domain.MctRef(mc.Name + "@" + repoPath),
			Market:   mc.Name,
			RelPath:  repoPath,
			Filename: "SKILL.md",
			Type:     domain.EntryTypeSkill,
			Profile:  pkg.Profile,
		}
		accumulate(a.writeToToolTargets(w, entry, content, location, nil))
	}

	for _, agent := range files.Agents {
		repoPath := a.agentFileRepoPath(pkg.Profile, agent)
		content, err := a.git.ReadFileAtRef(a.clonePath(mc.Name), mc.Branch, repoPath, "HEAD")
		if err != nil {
			continue
		}
		entry := domain.Entry{
			Ref:      domain.MctRef(mc.Name + "@" + repoPath),
			Market:   mc.Name,
			RelPath:  repoPath,
			Filename: agent,
			Type:     domain.EntryTypeAgent,
			Profile:  pkg.Profile,
		}
		accumulate(a.writeToToolTargets(w, entry, content, location, nil))
	}

	// Prune per-tool: files in old\new are gone from the new version.
	for toolName, oldFiles := range oldByTool {
		a.pruneRemovedFiles(w, location, oldFiles, newByTool[toolName])
	}

	// Replace each tool location's Files with the new authoritative set.
	// Drop locations whose tool produced no files this round.
	for toolName, toolFiles := range newByTool {
		loc := findLocationByPathAndType(pkg, location, toolName)
		if loc == nil {
			pkg.Locations = append(pkg.Locations, domain.InstalledLocation{
				Path:  location,
				Type:  toolName,
				Files: toolFiles,
			})
		} else {
			loc.Files = toolFiles
		}
	}
}

// mergeToolChecksums merges new checksums into a package's ToolChecksums map.
// findLocationByPathAndType returns the location entry for the given (path,
// type) tuple, or nil. Locations may share a path across runtime types.
func findLocationByPathAndType(pkg *domain.InstalledPackage, path, runtimeType string) *domain.InstalledLocation {
	for i := range pkg.Locations {
		if pkg.Locations[i].Path == path && pkg.Locations[i].Type == runtimeType {
			return &pkg.Locations[i]
		}
	}
	return nil
}

// packagePrimaryRef returns a representative MctRef for a package, using the
// first skill or agent file.
func (a *App) packagePrimaryRef(market string, pkg *domain.InstalledPackage) domain.MctRef {
	refs := a.packageFileRefs(market, *pkg)
	if len(refs) > 0 {
		return refs[0]
	}
	return domain.MctRef(market + "@" + pkg.Profile)
}

func (a *App) Sync(opts service.SyncOpts) ([]service.SyncResult, error) {
	refreshResults, err := a.Refresh(service.RefreshOpts{
		Market: opts.Market,
		DryRun: opts.DryRun,
		CI:     opts.CI,
	})
	if err != nil {
		return nil, err
	}

	updateResults, err := a.Update(service.UpdateOpts{
		Market:         opts.Market,
		DryRun:         opts.DryRun,
		CI:             opts.CI,
		AcceptBreaking: opts.AcceptBreaking,
		AllMerge:       opts.AllMerge,
		AllLocations:   opts.AllLocations,
	})
	if err != nil {
		return nil, err
	}

	updatesByMarket := make(map[string][]service.UpdateResult)
	for _, u := range updateResults {
		updatesByMarket[u.Ref.Market()] = append(updatesByMarket[u.Ref.Market()], u)
	}

	results := make([]service.SyncResult, 0, len(refreshResults))
	for _, r := range refreshResults {
		results = append(results, service.SyncResult{
			Refresh: r,
			Updates: updatesByMarket[r.Market],
		})
	}

	return results, nil
}

func (a *App) SyncState() (domain.SyncState, error) {
	return a.state.LoadSyncState(a.cacheDir)
}

// pruneRemovedFiles deletes files present in oldFiles but not newFiles
// (matched by Path). After deletion it walks the parent directory chain
// upward, removing any directory that becomes empty, stopping at the
// project root. Missing files are ignored — the on-disk truth may have
// already drifted from the recorded set.
//
// projectRoot is the project base (typically cfg.LocalPath's parent so
// we don't accidentally remove .claude/). InstalledFile.Path is rooted
// at projectRoot.
// pruneRemovedFiles deletes files present in oldFiles but not newFiles
// (matched by Path). After deletion it walks the parent directory chain
// upward, removing any directory that becomes empty, stopping at the
// project root. Missing files are ignored — the on-disk truth may have
// already drifted from the recorded set.
//
// projectRoot is the project base (typically cfg.LocalPath's parent so
// we don't accidentally remove .claude/). InstalledFile.Path is rooted
// at projectRoot.
// pruneRemovedFiles removes files that were present in the old version but
// are gone from the new version. Empty parent directories are cleaned up
// best-effort (directly via os, not via the tx — they're metadata, not
// content).
//
// projectRoot is the project base (typically cfg.LocalPath's parent so
// we don't accidentally remove .claude/). InstalledFile.Path is rooted
// at projectRoot.
func (a *App) pruneRemovedFiles(w txWriter, projectRoot string, oldFiles, newFiles []domain.InstalledFile) {
	if len(oldFiles) == 0 {
		return
	}
	keep := make(map[string]bool, len(newFiles))
	for _, f := range newFiles {
		keep[f.Path] = true
	}
	for _, f := range oldFiles {
		if keep[f.Path] {
			continue
		}
		abs := filepath.Join(projectRoot, filepath.FromSlash(f.Path))
		if err := w.DeleteFile(abs); err != nil && !os.IsNotExist(err) {
			continue
		}
		dir := filepath.Dir(abs)
		for dir != projectRoot && dir != "." && dir != string(filepath.Separator) {
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) > 0 {
				break
			}
			if err := os.Remove(dir); err != nil {
				break
			}
			dir = filepath.Dir(dir)
		}
	}
}

// hookFileRepoPath derives the repo-relative path for a hook json file
// given a package profile and the hook filename.
// Profile "dev/go" + hook "go-vet.json" -> "dev/go/hooks/go-vet.json"
// Profile ""       + hook "go-vet.json" -> "hooks/go-vet.json"
func (a *App) hookFileRepoPath(profile, hook string) string {
	return hookFileRepoPathStandalone(profile, hook)
}

// pruneStaleLocations scans the install database and removes any Location
// whose project directory no longer exists on disk. It does not touch any
// file (the directory is already gone) — it only cleans the DB. Returns the
// list of removed location paths (with their ref) for reporting.
// pruneStaleLocations scans the install database and removes any Location
// whose project directory no longer exists on the filesystem. It does not
// touch any file (the directory is already gone) — it only cleans the DB.
// Returns the list of removed location paths (with their ref) for reporting.
func (a *App) pruneStaleLocations() ([]string, error) {
	if err := a.idb.Lock(a.cacheDir); err != nil {
		return nil, err
	}
	defer func() { _ = a.idb.Unlock(a.cacheDir) }()

	db, err := a.idb.Load(a.cacheDir)
	if err != nil {
		return nil, err
	}

	type staleEntry struct {
		market   string
		profile  string
		location string
	}
	var stale []staleEntry
	for _, im := range db.Markets {
		for _, pkg := range im.Packages {
			for _, loc := range pkg.Locations {
				info, statErr := a.fs.Stat(loc.Path)
				if statErr == nil && info.IsDir() {
					continue
				}
				if statErr != nil && !os.IsNotExist(statErr) {
					continue
				}
				stale = append(stale, staleEntry{
					market:   im.Market,
					profile:  pkg.Profile,
					location: loc.Path,
				})
			}
		}
	}

	if len(stale) == 0 {
		return nil, nil
	}

	for _, s := range stale {
		db.RemoveLocation(s.market, s.profile, s.location)
	}

	w, commit, rollback, err := a.beginWriter("prune-stale-locations")
	if err != nil {
		return nil, err
	}
	if err := a.stageDBSave(w, db); err != nil {
		_ = rollback()
		return nil, err
	}
	if err := commit(); err != nil {
		return nil, err
	}

	pruned := make([]string, 0, len(stale))
	for _, s := range stale {
		pruned = append(pruned, s.market+"@"+s.profile+" -> "+s.location)
	}
	return pruned, nil
}

// pruneRemovedUpstreamFiles inspects every package installed from the given
// market and removes locally any skill/agent/command/hook whose source file
// no longer exists at the market's HEAD after fetch. For each removed file
// it deletes the on-disk content across all locations, drops it from
// pkg.Files and from each location's recorded files. Hooks are spliced from
// settings.json via removeHookSnippet using their mct_id.
//
// Returns the human-readable list of pruned items (one entry per
// market@profile#kind/name removed). Any partial failure aborts and rolls
// back the transaction.
// pruneRemovedUpstreamFiles inspects every package installed from the given
// market and removes locally any skill/agent/command/hook whose source file
// no longer exists at the market's HEAD after fetch. For each removed file
// it deletes the on-disk content across all locations, drops it from
// pkg.Files and from each location's recorded files. Hooks are spliced from
// settings.json via removeHookSnippet using their mct_id.
//
// Returns a human-readable list of pruned items (one entry per
// market@profile#kind/name removed). Any partial failure aborts and rolls
// back the transaction.
// pruneRemovedUpstreamFiles inspects every package installed from the given
// market and removes locally any skill/agent/command/hook whose source file
// no longer exists at the market's HEAD after fetch. For each removed file
// it deletes the on-disk content across all locations, drops it from
// pkg.Files and from each location's recorded files. Hooks are spliced from
// settings.json via removeHookSnippet using their mct_id.
//
// Returns a human-readable list of pruned items (one entry per
// market@profile#kind/name removed). Any partial failure aborts and rolls
// back the transaction.
func (a *App) pruneRemovedUpstreamFiles(marketName, branch, clonePath string) ([]string, error) {
	if err := a.idb.Lock(a.cacheDir); err != nil {
		return nil, err
	}
	defer func() { _ = a.idb.Unlock(a.cacheDir) }()

	dbVal, err := a.idb.Load(a.cacheDir)
	if err != nil {
		return nil, err
	}
	db := &dbVal

	im := db.FindMarket(marketName)
	if im == nil {
		return nil, nil
	}

	type removal struct {
		profile string
		kind    string
		name    string
	}
	var removals []removal

	for _, pkg := range im.Packages {
		for _, skill := range pkg.Files.Skills {
			dir := a.skillDirRepoPath(pkg.Profile, skill)
			files, err := a.git.ListDirFiles(clonePath, branch, dir)
			if err != nil || len(files) == 0 {
				removals = append(removals, removal{profile: pkg.Profile, kind: "skill", name: skill})
			}
		}
		for _, agent := range pkg.Files.Agents {
			path := a.agentFileRepoPath(pkg.Profile, agent)
			if _, err := a.git.FileVersion(clonePath, path); err != nil {
				removals = append(removals, removal{profile: pkg.Profile, kind: "agent", name: agent})
			}
		}
		for _, cmd := range pkg.Files.Commands {
			path := a.commandFileRepoPath(pkg.Profile, cmd)
			if _, err := a.git.FileVersion(clonePath, path); err != nil {
				removals = append(removals, removal{profile: pkg.Profile, kind: "command", name: cmd})
			}
		}
		for _, hook := range pkg.Files.Hooks {
			path := a.hookFileRepoPath(pkg.Profile, hook)
			if _, err := a.git.FileVersion(clonePath, path); err != nil {
				removals = append(removals, removal{profile: pkg.Profile, kind: "hook", name: hook})
			}
		}
	}

	if len(removals) == 0 {
		return nil, nil
	}

	w, commit, rollback, err := a.beginWriter("prune-upstream:" + marketName)
	if err != nil {
		return nil, err
	}

	relTo := func(root, abs string) string {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return filepath.ToSlash(abs)
		}
		return filepath.ToSlash(rel)
	}
	dropName := func(list []string, name string) []string {
		out := list[:0]
		for _, s := range list {
			if s != name {
				out = append(out, s)
			}
		}
		return out
	}
	dropFileByPath := func(list []domain.InstalledFile, rel string) []domain.InstalledFile {
		out := list[:0]
		for _, f := range list {
			if f.Path != rel {
				out = append(out, f)
			}
		}
		return out
	}
	dropFilesByPrefix := func(list []domain.InstalledFile, prefix string) []domain.InstalledFile {
		out := list[:0]
		for _, f := range list {
			if f.Path == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(f.Path, prefix) {
				continue
			}
			out = append(out, f)
		}
		return out
	}

	pruned := make([]string, 0, len(removals))
	for _, r := range removals {
		pkg := db.FindPackage(marketName, r.profile)
		if pkg == nil {
			continue
		}

		switch r.kind {
		case "skill":
			for li := range pkg.Locations {
				loc := &pkg.Locations[li]
				skillDir := filepath.Join(loc.Path, ".claude", "skills", r.name)
				if err := w.DeleteAll(skillDir); err != nil && !os.IsNotExist(err) {
					_ = rollback()
					return nil, err
				}
				prefix := relTo(loc.Path, skillDir) + "/"
				loc.Files = dropFilesByPrefix(loc.Files, prefix)
			}
			pkg.Files.Skills = dropName(pkg.Files.Skills, r.name)

		case "agent":
			for li := range pkg.Locations {
				loc := &pkg.Locations[li]
				abs := filepath.Join(loc.Path, ".claude", "agents", r.name)
				if err := w.DeleteFile(abs); err != nil && !os.IsNotExist(err) {
					_ = rollback()
					return nil, err
				}
				loc.Files = dropFileByPath(loc.Files, relTo(loc.Path, abs))
			}
			pkg.Files.Agents = dropName(pkg.Files.Agents, r.name)

		case "command":
			for li := range pkg.Locations {
				loc := &pkg.Locations[li]
				abs := filepath.Join(loc.Path, ".claude", "commands", r.name)
				if err := w.DeleteFile(abs); err != nil && !os.IsNotExist(err) {
					_ = rollback()
					return nil, err
				}
				loc.Files = dropFileByPath(loc.Files, relTo(loc.Path, abs))
			}
			pkg.Files.Commands = dropName(pkg.Files.Commands, r.name)

		case "hook":
			repoPath := a.hookFileRepoPath(r.profile, r.name)
			ref := domain.MctRef(marketName + "@" + repoPath)
			locs := make([]string, 0, len(pkg.Locations))
			for _, loc := range pkg.Locations {
				locs = append(locs, loc.Path)
			}
			for _, locPath := range locs {
				settings := filepath.Join(locPath, ".claude", "settings.json")
				if err := a.removeHookSnippet(w, ref, settings); err != nil {
					_ = rollback()
					return nil, err
				}
				if err := a.dropHookFromPackage(db, marketName, r.profile, locPath, r.name); err != nil {
					_ = rollback()
					return nil, err
				}
			}
		}

		pruned = append(pruned, marketName+"@"+r.profile+"#"+r.kind+"/"+r.name)
	}

	// Drop empty packages and cascade empty markets.
	if im2 := db.FindMarket(marketName); im2 != nil {
		kept := im2.Packages[:0]
		for _, pkg := range im2.Packages {
			if len(pkg.Files.Skills) == 0 && len(pkg.Files.Agents) == 0 && len(pkg.Files.Commands) == 0 && len(pkg.Files.Hooks) == 0 {
				continue
			}
			kept = append(kept, pkg)
		}
		im2.Packages = kept
	}

	if err := a.stageDBSave(w, *db); err != nil {
		_ = rollback()
		return nil, err
	}
	if err := commit(); err != nil {
		return nil, err
	}
	return pruned, nil
}

// DetectDeleted scans the install database and returns every file that was
// recorded as installed but is missing on disk (locally deleted by the user
// or otherwise vanished). It does NOT touch anything — read-only.
//
// Hooks are spliced into settings.json rather than written as standalone
// files; their "deletion" is hook drift, handled separately by detectHookDrift,
// so they are excluded here to avoid false positives.
func (a *App) DetectDeleted(opts service.DetectDeletedOpts) ([]service.DeletedFile, error) {
	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return nil, err
	}

	db, err := a.idb.Load(a.cacheDir)
	if err != nil {
		return nil, err
	}

	projectPath := projectPath(cfg.LocalPath)

	var deleted []service.DeletedFile
	for _, im := range db.Markets {
		if opts.Market != "" && im.Market != opts.Market {
			continue
		}
		for _, pkg := range im.Packages {
			for _, loc := range pkg.Locations {
				if loc.Path != projectPath {
					continue
				}
				if loc.Type != domain.RuntimeTypeClaudeCode {
					continue
				}
				for _, f := range loc.Files {
					// Skip hook entries — their "presence" is in settings.json
					// under an mct_id, not a standalone file.
					if strings.Contains(f.Path, "#hooks/") {
						continue
					}
					abs := filepath.Join(loc.Path, f.Path)
					if _, err := a.fs.Stat(abs); err != nil && os.IsNotExist(err) {
						deleted = append(deleted, service.DeletedFile{
							Market:   im.Market,
							Profile:  pkg.Profile,
							Location: loc.Path,
							RelPath:  f.Path,
							XXH:      f.XXH,
						})
					}
				}
			}
		}
	}
	return deleted, nil
}

// RestoreDeleted reinstalls files that were recorded as installed but went
// missing on disk. Each file is read from the cached clone at the package's
// recorded version and written back via the transactional writer; the
// install database xxhash is left untouched (the original hash is restored).
//
// On DryRun, no write happens and every result is "would_restore".
func (a *App) RestoreDeleted(files []service.DeletedFile, opts service.RestoreOpts) ([]service.RestoreResult, error) {
	if len(files) == 0 {
		return nil, nil
	}

	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return nil, err
	}

	if err := a.idb.Lock(a.cacheDir); err != nil {
		return nil, err
	}
	defer func() { _ = a.idb.Unlock(a.cacheDir) }()

	db, err := a.idb.Load(a.cacheDir)
	if err != nil {
		return nil, err
	}

	results := make([]service.RestoreResult, 0, len(files))

	// Group by (market, profile, location) for tx granularity.
	type key struct{ market, profile, location string }
	groups := make(map[key][]service.DeletedFile)
	order := []key{}
	for _, f := range files {
		k := key{f.Market, f.Profile, f.Location}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], f)
	}

	for _, k := range order {
		group := groups[k]
		mc := findMarketConfig(cfg, k.market)
		if mc == nil {
			for _, f := range group {
				results = append(results, service.RestoreResult{File: f, Action: "failed", Err: errMarketNotFound})
			}
			continue
		}
		pkg := db.FindPackage(k.market, k.profile)
		if pkg == nil {
			for _, f := range group {
				results = append(results, service.RestoreResult{File: f, Action: "failed", Err: errPackageNotFound})
			}
			continue
		}
		clonePath := a.clonePath(k.market)

		if opts.DryRun {
			for _, f := range group {
				results = append(results, service.RestoreResult{File: f, Action: "would_restore"})
			}
			continue
		}

		w, commit, rollback, err := a.beginWriter("restore-deleted:" + k.market + ":" + k.profile)
		if err != nil {
			for _, f := range group {
				results = append(results, service.RestoreResult{File: f, Action: "failed", Err: err})
			}
			continue
		}

		groupErr := error(nil)
		writtenResults := make([]service.RestoreResult, 0, len(group))
		for _, f := range group {
			repoPath := a.deletedFileRepoPath(pkg.Profile, f.RelPath)
			if repoPath == "" {
				writtenResults = append(writtenResults, service.RestoreResult{File: f, Action: "failed", Err: errUnknownFileShape})
				continue
			}
			content, readErr := a.git.ReadFileAtRef(clonePath, mc.Branch, repoPath, pkg.Version)
			if readErr != nil {
				writtenResults = append(writtenResults, service.RestoreResult{File: f, Action: "failed", Err: readErr})
				continue
			}
			abs := filepath.Join(f.Location, f.RelPath)
			if writeErr := w.WriteFile(abs, content); writeErr != nil {
				groupErr = writeErr
				writtenResults = append(writtenResults, service.RestoreResult{File: f, Action: "failed", Err: writeErr})
				break
			}
			writtenResults = append(writtenResults, service.RestoreResult{File: f, Action: "restored"})
		}

		if groupErr != nil {
			_ = rollback()
		} else if err := commit(); err != nil {
			for i := range writtenResults {
				if writtenResults[i].Action == "restored" {
					writtenResults[i].Action = "failed"
					writtenResults[i].Err = err
				}
			}
		}
		results = append(results, writtenResults...)
	}

	return results, nil
}

// deletedFileRepoPath maps a recorded local relative path (e.g.
// ".claude/agents/foo.md", ".claude/skills/bar/SKILL.md",
// ".claude/commands/baz.md") back to the upstream repo-relative path so
// the restore flow can read the original content.
//
// Returns "" if the shape is unrecognized (defensive — callers report a
// failed restore for that entry).
// deletedFileRepoPath maps a recorded local relative path (e.g.
// ".claude/agents/foo.md", ".claude/skills/bar/SKILL.md",
// ".claude/commands/baz.md") back to the upstream repo-relative path so
// the restore flow can read the original content.
//
// Returns "" if the shape is unrecognized (defensive — callers report a
// failed restore for that entry).
func (a *App) deletedFileRepoPath(profile, relPath string) string {
	const claudePrefix = ".claude/"
	rest := strings.TrimPrefix(relPath, claudePrefix)
	if rest == relPath {
		return ""
	}
	switch {
	case strings.HasPrefix(rest, "agents/"):
		return a.agentFileRepoPath(profile, strings.TrimPrefix(rest, "agents/"))
	case strings.HasPrefix(rest, "commands/"):
		return a.commandFileRepoPath(profile, strings.TrimPrefix(rest, "commands/"))
	case strings.HasPrefix(rest, "skills/"):
		// "skills/<name>/<file>" — split into skill + file for skillFileRepoPath.
		body := strings.TrimPrefix(rest, "skills/")
		slash := strings.Index(body, "/")
		if slash < 0 {
			return ""
		}
		return a.skillFileRepoPath(profile, body[:slash], body[slash+1:])
	default:
		return ""
	}
}

// errMarketNotFound, errPackageNotFound and errUnknownFileShape are
// sentinel errors returned by RestoreDeleted to keep its result rows
// machine-readable.
var (
	errMarketNotFound   = errors.New("market not found in config")
	errPackageNotFound  = errors.New("package not found in install database")
	errUnknownFileShape = errors.New("unrecognized installed file path shape")
)
