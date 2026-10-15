package app

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cespare/xxhash/v2"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/gitrepo"
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
func (a *App) flatSkillDrift(profile, skill, localBase, clonePath, branch, version string) (string, bool, error) {
	original, err := a.git.ReadFileAtRef(clonePath, branch, a.flatSkillRepoPath(profile, skill), version)
	if err != nil {
		return "", false, nil
	}
	rel := "skills/" + skill + "/SKILL.md"
	local, err := a.fs.ReadFile(filepath.Join(localBase, filepath.FromSlash(rel)))
	if err != nil {
		rel = "skills/" + skill
		local, err = a.fs.ReadFile(filepath.Join(localBase, "skills", skill))
	}
	if err != nil {
		return rel, true, err
	}
	return rel, xxhash.Sum64(local) != xxhash.Sum64(original), nil
}

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
	loc := findLocationByPathAndType(&pkg, location, domain.RuntimeTypeClaudeCode)
	if loc != nil && len(loc.Files) > 0 {
		for _, f := range loc.Files {
			if isHookPseudoPath(f.Path) {
				continue
			}
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
	version := pkg.VersionAt(location)

	for _, skill := range pkg.Files.Skills {
		// First check if we can read the original SKILL.md at the recorded version.
		// If the original doesn't exist at that ref, skip drift check for this skill.
		repoRelPath := a.skillFileRepoPath(pkg.Profile, skill, "SKILL.md")
		_, origErr := a.git.ReadFileAtRef(clonePath, branch, repoRelPath, version)
		if origErr != nil {
			if path, drifted, err := a.flatSkillDrift(pkg.Profile, skill, localBase, clonePath, branch, version); drifted {
				classify(path, err, &modified)
			}
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
			originalContent, err := a.git.ReadFileAtRef(clonePath, branch, fileRepoPath, version)
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
		originalContent, err := a.git.ReadFileAtRef(clonePath, branch, repoRelPath, version)
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
	if profile == "agents/"+agent || strings.HasSuffix(profile, "/agents/"+agent) {
		return profile
	}
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
	if profile == "commands/"+command || strings.HasSuffix(profile, "/commands/"+command) {
		return profile
	}
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

func (a *App) skillEntryRepoPath(pkg domain.InstalledPackage, skill string) string {
	if pkg.Files.IsFlatSkill(skill) || strings.HasSuffix(pkg.Profile, "/"+skill+".md") || pkg.Profile == skill+".md" {
		return a.flatSkillRepoPath(pkg.Profile, skill)
	}
	return a.skillFileRepoPath(pkg.Profile, skill, "SKILL.md")
}

// flatSkillRepoPath returns the repo-relative path of a single-file skill
// ("skills/<name>.md"), whose profile may be the file path itself.
func (a *App) flatSkillRepoPath(profile, skill string) string {
	if profile == skill+".md" || strings.HasSuffix(profile, "/"+skill+".md") {
		return profile
	}
	if profile != "" {
		return profile + "/skills/" + skill + ".md"
	}
	return "skills/" + skill + ".md"
}

func isHookPseudoPath(path string) bool {
	return strings.Contains(path, "#hooks/")
}

// packageOwnsPath reports whether a repo-relative path belongs to one of the
// entries installed from the package.
func (a *App) packageOwnsPath(pkg domain.InstalledPackage, path string) bool {
	for _, skill := range pkg.Files.Skills {
		if strings.HasPrefix(path, a.skillDirRepoPath(pkg.Profile, skill)+"/") || path == a.flatSkillRepoPath(pkg.Profile, skill) {
			return true
		}
	}
	for _, agent := range pkg.Files.Agents {
		if path == a.agentFileRepoPath(pkg.Profile, agent) {
			return true
		}
	}
	for _, cmd := range pkg.Files.Commands {
		if path == a.commandFileRepoPath(pkg.Profile, cmd) {
			return true
		}
	}
	for _, hook := range pkg.Files.Hooks {
		if path == a.hookFileRepoPath(pkg.Profile, hook) {
			return true
		}
	}
	return false
}

// hasUpstreamChange reports whether any file of pkg changed between version
// and headSHA. Removed entries are left to pruneRemovedUpstreamFiles. A
// version missing from the clone (re-clone, shallow history) counts as
// changed so the package is refreshed from HEAD.
func (a *App) hasUpstreamChange(clonePath string, mc domain.MarketConfig, pkg domain.InstalledPackage, version, headSHA string) bool {
	if version == "" || version == headSHA {
		return false
	}
	diffs, err := a.git.DiffSinceCommit(clonePath, mc.Branch, version)
	if err != nil {
		return true
	}
	for _, d := range diffs {
		paths := []string{d.From, d.To}
		if d.Action == domain.DiffDelete && skillRootForSubFile(d.From) == "" {
			paths = nil
		}
		for _, path := range paths {
			if path == "" || (mc.SkillsOnly && !isSkillPath(path, mc.SkillsPath)) {
				continue
			}
			if a.packageOwnsPath(pkg, path) {
				return true
			}
		}
	}
	return false
}

// upstreamFileExists reports whether path exists on the market branch. It
// only returns false on a successful lookup, never on a git failure.
func (a *App) upstreamFileExists(clonePath, branch, path string) (bool, error) {
	dir := filepath.ToSlash(filepath.Dir(path))
	if dir == "." {
		dir = ""
	}
	files, err := a.git.ListDirFiles(clonePath, branch, dir)
	if err != nil {
		return false, err
	}
	return slices.Contains(files, path), nil
}

type upstreamEntry struct{ kind, name string }

// removedUpstreamEntries lists the package entries confirmed absent from the
// market. A git failure never counts as a removal, so callers may delete
// local files based on the result.
func (a *App) removedUpstreamEntries(clonePath, branch string, pkg domain.InstalledPackage) []upstreamEntry {
	gone := func(path string) bool {
		_, err := a.git.FileVersion(clonePath, path)
		return errors.Is(err, gitrepo.ErrFileNotFound)
	}
	var out []upstreamEntry
	for _, skill := range pkg.Files.Skills {
		files, err := a.git.ListDirFiles(clonePath, branch, a.skillDirRepoPath(pkg.Profile, skill))
		if err == nil && len(files) == 0 && gone(a.flatSkillRepoPath(pkg.Profile, skill)) {
			out = append(out, upstreamEntry{"skill", skill})
		}
	}
	for _, agent := range pkg.Files.Agents {
		if gone(a.agentFileRepoPath(pkg.Profile, agent)) {
			out = append(out, upstreamEntry{"agent", agent})
		}
	}
	for _, cmd := range pkg.Files.Commands {
		if gone(a.commandFileRepoPath(pkg.Profile, cmd)) {
			out = append(out, upstreamEntry{"command", cmd})
		}
	}
	for _, hook := range pkg.Files.Hooks {
		if gone(a.hookFileRepoPath(pkg.Profile, hook)) {
			out = append(out, upstreamEntry{"hook", hook})
		}
	}
	return out
}

// packageFileRefs returns all MctRefs for the files in an installed package.
func (a *App) packageFileRefs(market string, pkg domain.InstalledPackage) []domain.MctRef {
	refs := make([]domain.MctRef, 0, len(pkg.Files.Skills)+len(pkg.Files.Agents)+len(pkg.Files.Commands)+len(pkg.Files.Hooks))
	for _, skill := range pkg.Files.Skills {
		repoPath := a.skillEntryRepoPath(pkg, skill)
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

	db, err := a.loadInstallDBWithoutStale()
	if err != nil {
		return nil, err
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
		headSHA, headErr := a.git.RemoteHEAD(clonePath, mc.Branch)

		for _, pkg := range im.Packages {
			if pkg.FindLocation(projectPath) == nil {
				continue
			}

			modifiedFiles, deletedFiles := a.detectDriftSplit(pkg, projectPath, clonePath, mc.Branch)
			hasModified := len(modifiedFiles) > 0
			hasDeletedOnly := !hasModified && len(deletedFiles) > 0

			driftedHooks := a.detectHookDrift(im.Market, pkg, projectPath)
			driftedHookSet := make(map[string]struct{}, len(driftedHooks))
			for _, h := range driftedHooks {
				driftedHookSet[h] = struct{}{}
			}

			hasUpdate := headErr == nil && a.hasUpstreamChange(clonePath, *mc, pkg, pkg.VersionAt(projectPath), headSHA)

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
					for _, path := range pkg.LocationPaths() {
						if a.hasUpstreamChange(clonePath, mc, pkg, pkg.VersionAt(path), newSHA) {
							updatesAvailable++
							break
						}
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

	migrated, err := a.Migrate(service.MigrateOpts{AllLocations: opts.AllLocations, DryRun: opts.DryRun})
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

	currentProject := projectPath(cfg.LocalPath)
	results := migrationUpdateResults(migrated, opts.DryRun)

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

		headSHA, err := a.git.RemoteHEAD(clonePath, mc.Branch)
		if err != nil {
			continue
		}

		for pi := range im.Packages {
			pkg := &im.Packages[pi]

			if refRelPath != "" && !fileInPackage(refRelPath, pkg.Files) {
				continue
			}

			seen := make(map[string]bool)
			for _, location := range pkg.LocationPaths() {
				if seen[location] {
					continue
				}
				seen[location] = true

				version := pkg.VersionAt(location)
				if version == "" {
					continue
				}

				if a.hasUpstreamChange(clonePath, mc, *pkg, version, headSHA) {
					if !opts.AllLocations && location != currentProject {
						continue
					}
				} else {
					if !opts.AllMerge && !opts.AllKeep {
						continue
					}
					mod, del := a.detectDriftSplit(*pkg, location, clonePath, mc.Branch)
					if len(mod) == 0 && len(del) == 0 {
						continue
					}
				}

				if opts.DryRun {
					results = append(results, service.UpdateResult{
						Ref:        a.packagePrimaryRef(mc.Name, pkg),
						Location:   location,
						Action:     "would_update",
						OldVersion: domain.MctVersion(version),
						NewVersion: domain.MctVersion(headSHA),
						DriftFiles: a.detectDrift(*pkg, location, clonePath, mc.Branch),
					})
					continue
				}

				r := a.updatePackageAtLocation(updateCtx{
					mc:        mc,
					im:        im,
					pkg:       pkg,
					pkgIdx:    pi,
					location:  location,
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

// updatePackageAtLocation performs the actual update of a single package at a
// single location: drift check, file replacement, and tool re-transform.
// updatePackageAtLocation performs the actual update of a single package at a
// single location: drift check, file replacement, and tool re-transform. It
// runs inside its own transaction so that on failure neither disk nor the
// install database mutate.
func (a *App) updatePackageAtLocation(ctx updateCtx) service.UpdateResult {
	oldVersion := domain.MctVersion(ctx.pkg.VersionAt(ctx.location))

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

	newFiles, claudeFiles, err := a.copyUpdatedFiles(w, ctx)
	if err != nil {
		return service.UpdateResult{
			Ref:      a.packagePrimaryRef(ctx.mc.Name, ctx.pkg),
			Location: ctx.location,
			Action:   "error",
			Err:      err,
		}
	}

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

	toolWarnings := a.reTransformToolTargets(w, ctx.mc, ctx.im, ctx.pkgIdx, newFiles, ctx.location, "HEAD", nil)
	ctx.im.Packages[ctx.pkgIdx].SetVersionAt(ctx.location, newSHA)

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
		Warnings:   toolWarnings,
	}
}

// copyUpdatedFiles reads updated skill, agent, command, and hook files from
// the cached clone and writes them to the local project directory. Entries
// confirmed absent upstream are dropped; any other read failure aborts the
// update so a transient git error never prunes installed files.
func (a *App) copyUpdatedFiles(w txWriter, ctx updateCtx) (domain.InstalledFiles, []domain.InstalledFile, error) {
	var newFiles domain.InstalledFiles
	var written []domain.InstalledFile

	projectRel := func(abs string) string {
		rel, err := filepath.Rel(ctx.location, abs)
		if err != nil {
			return filepath.ToSlash(abs)
		}
		return filepath.ToSlash(rel)
	}
	readUpstream := func(repoPath string) ([]byte, bool, error) {
		content, err := a.git.ReadFileAtRef(ctx.clonePath, ctx.mc.Branch, repoPath, "HEAD")
		if err == nil {
			return content, true, nil
		}
		exists, existsErr := a.upstreamFileExists(ctx.clonePath, ctx.mc.Branch, repoPath)
		if existsErr != nil || exists {
			return nil, false, errors.Join(errors.New("read "+repoPath), err, existsErr)
		}
		return nil, false, nil
	}
	writeFile := func(dest string, content []byte) error {
		if err := w.WriteFile(dest, content); err != nil {
			return err
		}
		written = append(written, domain.InstalledFile{Path: projectRel(dest), XXH: xxhashHex(content)})
		return nil
	}

	for _, skill := range ctx.pkg.Files.Skills {
		skillDirPath := a.skillDirRepoPath(ctx.pkg.Profile, skill)
		dirFiles, err := a.git.ListDirFiles(ctx.clonePath, ctx.mc.Branch, skillDirPath)
		if err != nil {
			return newFiles, written, err
		}
		if len(dirFiles) == 0 {
			content, found, err := readUpstream(a.flatSkillRepoPath(ctx.pkg.Profile, skill))
			if err != nil {
				return newFiles, written, err
			}
			if !found {
				continue
			}
			skillMd, err := prepareFlatSkillDir(w, filepath.Join(ctx.location, ".claude", "skills", skill))
			if err != nil {
				return newFiles, written, err
			}
			if err := writeFile(skillMd, content); err != nil {
				return newFiles, written, err
			}
			newFiles.Skills = append(newFiles.Skills, skill)
			newFiles.FlatSkills = append(newFiles.FlatSkills, skill)
			continue
		}
		localSkillDir := filepath.Join(ctx.location, ".claude", "skills", skill)
		for _, f := range dirFiles {
			content, err := a.git.ReadFileAtRef(ctx.clonePath, ctx.mc.Branch, f, "HEAD")
			if err != nil {
				return newFiles, written, err
			}
			rel, err := filepath.Rel(skillDirPath, f)
			if err != nil {
				return newFiles, written, err
			}
			if err := writeFile(filepath.Join(localSkillDir, rel), content); err != nil {
				return newFiles, written, err
			}
		}
		newFiles.Skills = append(newFiles.Skills, skill)
	}

	for _, agent := range ctx.pkg.Files.Agents {
		content, found, err := readUpstream(a.agentFileRepoPath(ctx.pkg.Profile, agent))
		if err != nil {
			return newFiles, written, err
		}
		if !found {
			continue
		}
		if err := writeFile(filepath.Join(ctx.location, ".claude", "agents", agent), domain.StripRequiresSkills(content)); err != nil {
			return newFiles, written, err
		}
		newFiles.Agents = append(newFiles.Agents, agent)
	}

	for _, cmd := range ctx.pkg.Files.Commands {
		content, found, err := readUpstream(a.commandFileRepoPath(ctx.pkg.Profile, cmd))
		if err != nil {
			return newFiles, written, err
		}
		if !found {
			continue
		}
		if err := writeFile(filepath.Join(ctx.location, ".claude", "commands", cmd), content); err != nil {
			return newFiles, written, err
		}
		newFiles.Commands = append(newFiles.Commands, cmd)
	}

	for _, hookFile := range ctx.pkg.Files.Hooks {
		repoPath := a.hookFileRepoPath(ctx.pkg.Profile, hookFile)
		content, found, err := readUpstream(repoPath)
		if err != nil {
			return newFiles, written, err
		}
		if !found {
			continue
		}
		snippet, err := domain.ParseHookSnippet(content)
		if err != nil {
			return newFiles, written, err
		}
		ref := domain.MctRef(ctx.mc.Name + "@" + repoPath)
		settingsAbsPath := filepath.Join(ctx.location, ".claude", "settings.json")
		if err := a.removeHookSnippet(w, ref, settingsAbsPath); err != nil {
			return newFiles, written, err
		}
		hookWritten, err := a.installHookSnippet(w, ref, snippet, settingsAbsPath, hookFile)
		if err != nil {
			return newFiles, written, err
		}
		written = append(written, hookWritten...)
		newFiles.Hooks = append(newFiles.Hooks, hookFile)
	}

	return newFiles, written, nil
}

func (a *App) reTransformToolTargets(w txWriter, mc domain.MarketConfig, im *domain.InstalledMarket, pkgIdx int, files domain.InstalledFiles, location, ref string, skip func(string) bool) []string {
	pkg := &im.Packages[pkgIdx]
	clonePath := a.clonePath(mc.Name)
	scoped := domain.InstalledPackage{Profile: pkg.Profile, Files: files}

	type source struct {
		repoPath  string
		entryType domain.EntryType
	}
	var sources []source
	for _, skill := range files.Skills {
		sources = append(sources, source{a.skillEntryRepoPath(scoped, skill), domain.EntryTypeSkill})
	}
	for _, agent := range files.Agents {
		sources = append(sources, source{a.agentFileRepoPath(pkg.Profile, agent), domain.EntryTypeAgent})
	}
	for _, cmd := range files.Commands {
		sources = append(sources, source{a.commandFileRepoPath(pkg.Profile, cmd), domain.EntryTypeCommand})
	}
	for _, hook := range files.Hooks {
		sources = append(sources, source{a.hookFileRepoPath(pkg.Profile, hook), domain.EntryTypeHook})
	}

	var all toolWriteResult
	for _, src := range sources {
		content, err := a.git.ReadFileAtRef(clonePath, mc.Branch, src.repoPath, ref)
		if err != nil {
			continue
		}
		if src.entryType == domain.EntryTypeAgent {
			content = domain.StripRequiresSkills(content)
		}
		var assets []domain.TransformAsset
		if src.entryType == domain.EntryTypeSkill {
			assets = a.skillAssetsAt(clonePath, mc.Branch, src.repoPath, ref)
		}
		entry := domain.Entry{
			Ref:      domain.MctRef(mc.Name + "@" + src.repoPath),
			Market:   mc.Name,
			RelPath:  src.repoPath,
			Filename: path.Base(src.repoPath),
			Type:     src.entryType,
			Profile:  pkg.Profile,
		}
		all.merge(a.writeToolTargets(w, entry, content, assets, location, nil, skip))
	}
	return append(all.Warnings, syncToolFiles(w, pkg, location, all.ToolFiles)...)
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
		AllKeep:        opts.AllKeep,
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

func (a *App) upstreamEntrySource(pkg domain.InstalledPackage, kind, name string) string {
	switch kind {
	case "skill":
		return a.skillEntryRepoPath(pkg, name)
	case "agent":
		return a.agentFileRepoPath(pkg.Profile, name)
	case "command":
		return a.commandFileRepoPath(pkg.Profile, name)
	case "hook":
		return a.hookFileRepoPath(pkg.Profile, name)
	}
	return ""
}

func distinctLocationPaths(pkg domain.InstalledPackage) []string {
	seen := map[string]bool{}
	var out []string
	for _, loc := range pkg.Locations {
		if !seen[loc.Path] {
			seen[loc.Path] = true
			out = append(out, loc.Path)
		}
	}
	return out
}

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
		for _, e := range a.removedUpstreamEntries(clonePath, branch, pkg) {
			removals = append(removals, removal{profile: pkg.Profile, kind: e.kind, name: e.name})
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

		if source := a.upstreamEntrySource(*pkg, r.kind, r.name); source != "" {
			for _, locPath := range distinctLocationPaths(*pkg) {
				if _, err := removeToolFiles(w, pkg, locPath, source); err != nil {
					_ = rollback()
					return nil, err
				}
			}
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
			pkg.Files.FlatSkills = dropName(pkg.Files.FlatSkills, r.name)

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
			content, readErr := a.git.ReadFileAtRef(clonePath, mc.Branch, repoPath, pkg.VersionAt(f.Location))
			if readErr != nil {
				if flat := a.flatSkillRepoPathForInstalled(pkg.Profile, f.RelPath); flat != "" {
					content, readErr = a.git.ReadFileAtRef(clonePath, mc.Branch, flat, pkg.VersionAt(f.Location))
				}
			}
			if readErr != nil {
				writtenResults = append(writtenResults, service.RestoreResult{File: f, Action: "failed", Err: readErr})
				continue
			}
			if inferEntryType(repoPath) == domain.EntryTypeAgent {
				content = domain.StripRequiresSkills(content)
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
			return a.flatSkillRepoPath(profile, body)
		}
		return a.skillFileRepoPath(profile, body[:slash], body[slash+1:])
	default:
		return ""
	}
}

func (a *App) flatSkillRepoPathForInstalled(profile, relPath string) string {
	body, ok := strings.CutPrefix(relPath, ".claude/skills/")
	if !ok {
		return ""
	}
	name, ok := strings.CutSuffix(body, "/SKILL.md")
	if !ok {
		name = body
	}
	if name == "" || strings.Contains(name, "/") {
		return ""
	}
	return a.flatSkillRepoPath(profile, name)
}

// errMarketNotFound, errPackageNotFound and errUnknownFileShape are
// sentinel errors returned by RestoreDeleted to keep its result rows
// machine-readable.
var (
	errMarketNotFound   = errors.New("market not found in config")
	errPackageNotFound  = errors.New("package not found in install database")
	errUnknownFileShape = errors.New("unrecognized installed file path shape")
)
