package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/configstore"
	fsrepo "github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/filesystem"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/gitrepo"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/installdb"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/statestore"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/tx"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

// marketNameFromURL derives a market name from a git URL.
// e.g. "https://github.com/aa/bb.git" -> "aa/bb"
//
//	"git@gitlab.com:aa/bb/cc.git"  -> "aa/bb/cc"
func marketNameFromURL(url string) (string, error) {
	normalized := domain.NormalizeURL(url)
	// normalized is "github.com/aa/bb" — strip the host
	idx := strings.Index(normalized, "/")
	if idx < 0 || idx == len(normalized)-1 {
		return "", &domain.DomainError{
			Code:    "INVALID_MARKET_URL",
			Message: "cannot derive market name from URL: " + url,
		}
	}
	return normalized[idx+1:], nil
}

// isSkillPath returns true if the path is under the given skills folder.
// If skillsPath is empty, it defaults to "skills".
func isSkillPath(path, skillsPath string) bool {
	if skillsPath == "" {
		skillsPath = "skills"
	}
	return strings.HasPrefix(filepath.ToSlash(path), skillsPath+"/") || filepath.ToSlash(path) == skillsPath
}

// marketDirName converts a market name (which may contain /) to a safe
// directory name by replacing / with --.
func marketDirName(name string) string {
	return strings.ReplaceAll(name, "/", "--")
}

type App struct {
	git          gitrepo.GitRepo
	fs           fsrepo.Filesystem
	cfg          configstore.ConfigStore
	state        statestore.StateStore
	idb          installdb.InstallDB
	configPath   string
	cacheDir     string
	transformers domain.TransformerRegistry
	toolMappings configstore.ToolMappingStore
	txm          tx.Manager
}

func New(git gitrepo.GitRepo, fs fsrepo.Filesystem, cfg configstore.ConfigStore, state statestore.StateStore, idb installdb.InstallDB, configPath, cacheDir string, opts ...AppOption) *App {
	a := &App{
		git:        git,
		fs:         fs,
		cfg:        cfg,
		state:      state,
		idb:        idb,
		configPath: configPath,
		cacheDir:   cacheDir,
	}
	for _, opt := range opts {
		opt(a)
	}
	if a.transformers == nil {
		a.transformers = domain.TransformerRegistry{}
	}
	return a
}

// AppOption configures optional App dependencies.
type AppOption func(*App)

// WithTransformers sets the transformer registry for multi-tool support.
func WithTransformers(reg domain.TransformerRegistry) AppOption {
	return func(a *App) { a.transformers = reg }
}

// WithToolMappings sets the tool mapping store for multi-tool support.
// WithToolMappings sets the tool mapping store for multi-tool support.
func WithToolMappings(store configstore.ToolMappingStore) AppOption {
	return func(a *App) { a.toolMappings = store }
}

// WithTxManager sets the transactional filesystem manager used to make
// install/update/remove flows atomic. Defaults to a manager rooted at
// <cacheDir>/staging when unset.
func WithTxManager(m tx.Manager) AppOption {
	return func(a *App) { a.txm = m }
}

// projectPath returns the absolute project root derived from cfg.LocalPath.
// LocalPath is typically ".claude/" — the project root is its parent.
func projectPath(localPath string) string {
	abs, err := filepath.Abs(localPath)
	if err != nil {
		return filepath.Dir(filepath.Clean(localPath))
	}
	return filepath.Dir(abs)
}

func (a *App) clonePath(marketName string) string {
	return filepath.Join(a.cacheDir, marketDirName(marketName))
}

func findMarketConfig(cfg domain.Config, name string) *domain.MarketConfig {
	for i := range cfg.Markets {
		if cfg.Markets[i].Name == name {
			return &cfg.Markets[i]
		}
	}
	return nil
}

func marketConfigToMarket(mc domain.MarketConfig, cloneDir string) domain.Market {
	return domain.Market{
		Name:       mc.Name,
		URL:        mc.URL,
		Branch:     mc.Branch,
		ClonePath:  filepath.Join(cloneDir, marketDirName(mc.Name)),
		Trusted:    mc.Trusted,
		ReadOnly:   mc.ReadOnly,
		SkillsOnly: mc.SkillsOnly,
		SkillsPath: mc.SkillsPath,
	}
}

func (a *App) ListMarkets() ([]domain.Market, error) {
	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return nil, err
	}
	markets := make([]domain.Market, len(cfg.Markets))
	for i, mc := range cfg.Markets {
		markets[i] = marketConfigToMarket(mc, a.cacheDir)
	}
	return markets, nil
}

func (a *App) GetMarket(name string) (domain.Market, error) {
	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return domain.Market{}, err
	}
	for _, mc := range cfg.Markets {
		if mc.Name == name {
			return marketConfigToMarket(mc, a.cacheDir), nil
		}
	}
	return domain.Market{}, domain.ErrMarketNotFound
}

func (a *App) MarketInfo(name string) (service.MarketInfoResult, error) {
	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return service.MarketInfoResult{}, err
	}

	found := findMarketConfig(cfg, name)
	if found == nil {
		return service.MarketInfoResult{}, domain.ErrMarketNotFound
	}

	market := marketConfigToMarket(*found, a.cacheDir)

	syncState, err := a.state.LoadSyncState(a.cacheDir)
	if err != nil {
		return service.MarketInfoResult{}, err
	}

	db, err := a.idb.Load(a.cacheDir)
	if err != nil {
		return service.MarketInfoResult{}, err
	}

	allFiles, err := a.git.ListFiles(market.ClonePath, found.Branch)
	if err != nil {
		allFiles = nil
	}
	var files []string
	for _, f := range allFiles {
		if found.SkillsOnly && !isSkillPath(f, found.SkillsPath) {
			continue
		}
		files = append(files, f)
	}

	installedCount := 0
	im := db.FindMarket(name)
	if im != nil {
		for _, pkg := range im.Packages {
			installedCount += len(pkg.Files.Skills) + len(pkg.Files.Agents)
		}
	}

	result := service.MarketInfoResult{
		Market:         market,
		EntryCount:     len(files),
		InstalledCount: installedCount,
	}

	if ms, ok := syncState.Markets[name]; ok {
		result.LastSynced = ms.LastSyncedAt
		result.Status = ms.Status
	}

	return result, nil
}

// parseTreeURL detects GitHub/GitLab "/tree/<branch>/<path>" URLs and extracts
// the clean repo URL, branch, and subpath. If the URL doesn't contain /tree/,
// it returns the original values unchanged.
func parseTreeURL(rawURL string) (repoURL, branch, subPath string) {
	// Match https://host/org/repo/tree/<branch>[/<path>]
	normalized := rawURL
	if idx := strings.Index(normalized, "://"); idx >= 0 {
		normalized = normalized[idx+3:]
	}
	parts := strings.Split(strings.TrimSuffix(normalized, "/"), "/")
	// Minimum: host/org/repo/tree/branch → 5 parts
	treeIdx := -1
	for i, p := range parts {
		if p == "tree" && i >= 3 {
			treeIdx = i
			break
		}
	}
	if treeIdx < 0 || treeIdx+1 >= len(parts) {
		return rawURL, "", ""
	}
	// Reconstruct the clean repo URL (scheme + host/org/repo)
	scheme := "https://"
	if schIdx := strings.Index(rawURL, "://"); schIdx >= 0 {
		scheme = rawURL[:schIdx+3]
	}
	repoURL = scheme + strings.Join(parts[:treeIdx], "/")
	branch = parts[treeIdx+1]
	if treeIdx+2 < len(parts) {
		subPath = strings.Join(parts[treeIdx+2:], "/")
	}
	return repoURL, branch, subPath
}

func (a *App) AddMarket(url string, opts service.AddMarketOpts) (service.AddMarketResult, error) {
	var result service.AddMarketResult

	// Parse /tree/<branch>/<path> from GitHub/GitLab URLs.
	repoURL, treeBranch, treeSubPath := parseTreeURL(url)
	url = repoURL
	if treeBranch != "" && opts.Branch == "" {
		opts.Branch = treeBranch
	}
	if treeSubPath != "" && opts.SkillsPath == "" {
		opts.SkillsPath = treeSubPath
	}

	name, err := marketNameFromURL(url)
	if err != nil {
		return result, err
	}

	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return result, err
	}

	for _, mc := range cfg.Markets {
		if mc.Name == name {
			return result, domain.ErrMarketAlreadyExists
		}
		if domain.NormalizeURL(mc.URL) == domain.NormalizeURL(url) {
			return result, &domain.DomainError{
				Code:    domain.ErrMarketURLExists.Code,
				Message: fmt.Sprintf("market %q already uses URL %s", mc.Name, mc.URL),
			}
		}
	}

	clonePath := a.clonePath(name)
	if fsrepo.DirExists(a.fs, clonePath) {
		return result, domain.ErrCloneExists
	}

	cloned := false
	if !opts.NoClone {
		if err := a.git.ValidateRemote(url); err != nil {
			return result, domain.ErrMarketUnreachable.Wrap(err)
		}

		if err := a.git.Clone(url, clonePath); err != nil {
			return result, err
		}
		cloned = true
	}

	// cleanupClone removes the clone directory if we created it and something fails.
	cleanupClone := func() {
		if cloned {
			_ = a.fs.RemoveAll(clonePath)
		}
	}

	branch := opts.Branch

	var skillsOnly bool
	skillsPath := opts.SkillsPath

	if !opts.NoClone {
		if branch == "" {
			detected, err := a.git.DefaultBranch(clonePath)
			if err != nil {
				branch = "main"
			} else {
				branch = detected
			}
		}

		sha, err := a.git.RemoteHEAD(clonePath, branch)
		if err != nil {
			cleanupClone()
			return result, err
		}

		if err := a.state.SetMarketSyncClean(a.cacheDir, name, sha); err != nil {
			cleanupClone()
			return result, err
		}

		skillsOnly = detectSkillsOnly(a.git, clonePath, branch, skillsPath)
		if skillsOnly {
			skillsPath = detectedSkillsPath(a.git, clonePath, branch, skillsPath)
			if err := pruneCloneForSkills(a.fs, clonePath, skillsPath); err != nil {
				cleanupClone()
				return result, err
			}
		}
		result = countMarketEntries(a.git, clonePath, branch, skillsPath, skillsOnly)

		if !skillsOnly && result.Profiles == 0 && result.Agents == 0 && result.Skills == 0 {
			cleanupClone()
			return result, domain.ErrMarketIncompatible
		}
	}

	if branch == "" {
		branch = "main"
	}

	mc := domain.MarketConfig{
		Name:       name,
		URL:        url,
		Branch:     branch,
		Trusted:    opts.Trusted,
		ReadOnly:   opts.ReadOnly,
		SkillsOnly: skillsOnly,
		SkillsPath: skillsPath,
	}
	if err := a.cfg.AddMarket(a.configPath, mc); err != nil {
		cleanupClone()
		return result, err
	}
	return result, nil
}

// pruneCloneForSkills removes everything from the clone directory except
// .git and the skills folder. This keeps symlinks working while removing unneeded files.
func pruneCloneForSkills(fs fsrepo.Filesystem, clonePath, skillsPath string) error {
	if skillsPath == "" {
		skillsPath = "skills"
	}
	// Keep the top-level directory of the skills path (e.g. "src" for "src/skills").
	keepDir := strings.SplitN(skillsPath, "/", 2)[0]
	entries, err := fs.ListDir(clonePath)
	if err != nil {
		return err
	}
	for _, name := range entries {
		if name == ".git" || name == keepDir {
			continue
		}
		if err := fs.RemoveAll(filepath.Join(clonePath, name)); err != nil {
			return err
		}
	}
	return nil
}

// detectSkillsOnly checks if a repo is a skills-only market by looking for
// a "skills/" or "skills-catalog/" directory containing <skill-name>/SKILL.md files.
// If skillsPath is provided, only that path is checked.
func detectSkillsOnly(git gitrepo.GitRepo, clonePath, branch, skillsPath string) bool {
	candidates := []string{"skills", "skills-catalog"}
	if skillsPath != "" {
		candidates = []string{skillsPath}
	}
	files, err := git.ListFiles(clonePath, branch)
	if err != nil {
		return false
	}
	for _, candidate := range candidates {
		depth := len(strings.Split(candidate, "/"))
		for _, f := range files {
			parts := strings.Split(filepath.ToSlash(f), "/")
			if len(parts) == depth+2 && strings.Join(parts[:depth], "/") == candidate && parts[len(parts)-1] == "SKILL.md" {
				return true
			}
		}
	}
	return false
}

// detectedSkillsPath returns the actual skills path found in the repo
// ("skills" or "skills-catalog"), or the explicit skillsPath if provided.
func detectedSkillsPath(git gitrepo.GitRepo, clonePath, branch, skillsPath string) string {
	if skillsPath != "" {
		return skillsPath
	}
	files, err := git.ListFiles(clonePath, branch)
	if err != nil {
		return "skills"
	}
	for _, candidate := range []string{"skills", "skills-catalog"} {
		depth := len(strings.Split(candidate, "/"))
		for _, f := range files {
			parts := strings.Split(filepath.ToSlash(f), "/")
			if len(parts) == depth+2 && strings.Join(parts[:depth], "/") == candidate && parts[len(parts)-1] == "SKILL.md" {
				return candidate
			}
		}
	}
	return "skills"
}

func countMarketEntries(git gitrepo.GitRepo, clonePath, branch, skillsPath string, skillsOnly bool) service.AddMarketResult {
	files, err := git.ListFiles(clonePath, branch)
	if err != nil {
		return service.AddMarketResult{}
	}

	profiles := make(map[string]struct{})
	var result service.AddMarketResult

	for _, f := range files {
		if skillsOnly && !isSkillPath(f, skillsPath) {
			continue
		}
		parts := strings.Split(filepath.ToSlash(f), "/")
		if len(parts) >= 2 {
			profiles[parts[0]+"/"+parts[1]] = struct{}{}
		}
		for _, p := range parts {
			switch p {
			case "agents":
				result.Agents++
			case "skills":
				result.Skills++
			}
		}
	}
	result.Profiles = len(profiles)
	return result
}

func (a *App) RemoveMarket(name string, opts service.RemoveMarketOpts) error {
	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return err
	}

	if findMarketConfig(cfg, name) == nil {
		return domain.ErrMarketNotFound
	}

	if !opts.Force {
		db, err := a.idb.Load(a.cacheDir)
		if err != nil {
			return err
		}

		im := db.FindMarket(name)
		if im != nil && len(im.Packages) > 0 {
			var installedRefs []string
			for _, pkg := range im.Packages {
				for _, ref := range a.packageFileRefs(name, pkg) {
					installedRefs = append(installedRefs, string(ref))
				}
			}

			if len(installedRefs) > 0 {
				return &domain.DomainError{
					Code:    "MARKET_HAS_INSTALLED_ENTRIES",
					Message: fmt.Sprintf("market %q has installed entries: %s", name, strings.Join(installedRefs, ", ")),
				}
			}
		}
	}

	if err := a.cfg.RemoveMarket(a.configPath, name); err != nil {
		return err
	}

	syncState, err := a.state.LoadSyncState(a.cacheDir)
	if err != nil {
		return err
	}
	delete(syncState.Markets, name)
	if err := a.state.SaveSyncState(a.cacheDir, syncState); err != nil {
		return err
	}

	if !opts.KeepCache {
		clonePath := a.clonePath(name)
		if err := a.fs.RemoveAll(clonePath); err != nil {
			return err
		}
	}

	return nil
}

func (a *App) SetMarketProperty(name, key, value string) error {
	return a.cfg.SetMarketProperty(a.configPath, name, key, value)
}

func (a *App) SetConfigField(key, value string) error {
	if tool, ok := strings.CutPrefix(key, "tools."); ok {
		if err := a.checkToolName(tool); err != nil {
			return err
		}
	}
	return a.cfg.SetConfigField(a.configPath, key, value)
}

func (a *App) GetConfig() (domain.Config, error) {
	return a.cfg.Load(a.configPath)
}
