package app

import (
	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

func (a *App) removeHookSnippet(w txWriter, ref domain.MctRef, settingsAbsPath string) error {
	st, err := loadHookState(w.ReadFile, settingsAbsPath)
	if err != nil {
		return err
	}
	st.removeOwned(st.resolve(), mctIDForRef(ref))
	return st.save(w, settingsAbsPath)
}

// dropHookFromPackage removes hookFile from pkg.Files.Hooks and from the
// matching location's Files list (the entry whose Path ends in
// "#hooks/<hookFile>"). If the package's Hooks slice becomes empty AND
// there are no other files in the package at this location, the location
// itself is removed.
func (a *App) dropHookFromPackage(db *domain.InstallDatabase, market, profile, location, hookFile string) error {
	pkg := db.FindPackage(market, profile)
	if pkg == nil {
		return nil
	}
	// Remove the hook leaf from package-wide list.
	filtered := pkg.Files.Hooks[:0]
	for _, h := range pkg.Files.Hooks {
		if h != hookFile {
			filtered = append(filtered, h)
		}
	}
	pkg.Files.Hooks = filtered

	// Remove the per-location InstalledFile entry whose Path encodes this
	// hookFile. Other hooks in the same package keep their entries.
	loc := findLocationByPathAndType(pkg, location, domain.RuntimeTypeClaudeCode)
	if loc != nil {
		suffix := "#hooks/" + hookFile
		filteredFiles := loc.Files[:0]
		for _, f := range loc.Files {
			if len(f.Path) >= len(suffix) && f.Path[len(f.Path)-len(suffix):] == suffix {
				continue
			}
			filteredFiles = append(filteredFiles, f)
		}
		loc.Files = filteredFiles
		// If the package has no remaining files at this location AND no
		// remaining package-level files of any kind, drop the location
		// entirely so List/Check don't keep returning it.
		if len(loc.Files) == 0 && len(pkg.Files.Skills) == 0 && len(pkg.Files.Agents) == 0 && len(pkg.Files.Commands) == 0 && len(pkg.Files.Hooks) == 0 {
			db.RemoveLocation(market, profile, location)
		}
	}
	return nil
}
