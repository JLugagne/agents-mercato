package app

import (
	"os"
	"path/filepath"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

// Doctor performs a read-only audit of every installed package and returns
// a structured report. It NEVER touches disk or the install database, NEVER
// performs network I/O. It uses the cached clones as-is — run mct refresh
// first if you need a fresh upstream view.
//
// Findings:
//   - ModifiedFiles: local files whose content hash differs from what was
//     recorded at install time
//   - LocallyDeleted: recorded files that are missing on disk
//   - StaleLocations: project paths in the install DB that no longer exist
//   - UpstreamRemoved: pkg.Files entries whose source vanished from the
//     cached clone at HEAD
//   - OrphanedPackages: packages whose market is no longer in the user
//     config — sync can never reconcile them
func (a *App) Doctor(opts service.DoctorOpts) (service.DoctorReport, error) {
	report := service.DoctorReport{}

	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return report, err
	}
	db, err := a.idb.Load(a.cacheDir)
	if err != nil {
		return report, err
	}

	// Build a set of market names in config for orphan detection.
	configuredMarkets := make(map[string]*domain.MarketConfig, len(cfg.Markets))
	for i := range cfg.Markets {
		configuredMarkets[cfg.Markets[i].Name] = &cfg.Markets[i]
	}

	for _, im := range db.Markets {
		if opts.Market != "" && im.Market != opts.Market {
			continue
		}

		mc, configured := configuredMarkets[im.Market]
		clonePath := a.clonePath(im.Market)

		for _, pkg := range im.Packages {
			for _, loc := range pkg.Locations {
				// Stale location detection
				info, statErr := a.fs.Stat(loc.Path)
				if statErr != nil && os.IsNotExist(statErr) {
					report.StaleLocations = append(report.StaleLocations, service.DoctorLocation{
						Market: im.Market, Profile: pkg.Profile, Location: loc.Path,
					})
					continue
				}
				if statErr == nil && !info.IsDir() {
					// Treat a non-directory as stale too.
					report.StaleLocations = append(report.StaleLocations, service.DoctorLocation{
						Market: im.Market, Profile: pkg.Profile, Location: loc.Path,
					})
					continue
				}

				// Orphan detection (still a real location, but no market)
				if !configured {
					report.OrphanedPackages = append(report.OrphanedPackages, service.DoctorPackage{
						Market: im.Market, Profile: pkg.Profile, Location: loc.Path,
					})
					continue
				}

				// Drift split
				modified, deleted := a.detectDriftSplit(pkg, loc.Path, clonePath, mc.Branch)
				for _, p := range modified {
					report.ModifiedFiles = append(report.ModifiedFiles, service.DoctorFile{
						Market: im.Market, Profile: pkg.Profile, Location: loc.Path, Path: p,
					})
				}
				for _, p := range deleted {
					report.LocallyDeleted = append(report.LocallyDeleted, service.DoctorFile{
						Market: im.Market, Profile: pkg.Profile, Location: loc.Path, Path: p,
					})
				}
			}

			// Upstream-removed scan: same logic as pruneRemovedUpstreamFiles
			// but read-only, no fetch.
			if !configured {
				continue
			}
			report.UpstreamRemoved = append(report.UpstreamRemoved, a.scanUpstreamRemoved(im.Market, pkg, clonePath, mc.Branch)...)
		}
	}

	return report, nil
}

// scanUpstreamRemoved returns one DoctorFile per pkg.Files entry whose
// source no longer exists in the cached clone at HEAD. Mirrors the detection
// half of pruneRemovedUpstreamFiles without any mutation.
func (a *App) scanUpstreamRemoved(market string, pkg domain.InstalledPackage, clonePath, branch string) []service.DoctorFile {
	var out []service.DoctorFile
	add := func(kind, name string) {
		out = append(out, service.DoctorFile{
			Market: market, Profile: pkg.Profile,
			Path: kind + "/" + name, Kind: kind,
		})
	}

	for _, e := range a.removedUpstreamEntries(clonePath, branch, pkg) {
		add(e.kind, e.name)
	}

	// Stamp the location only when there's at least one finding — the
	// upstream pruning is per-package, not per-location.
	if len(pkg.Locations) > 0 {
		for i := range out {
			out[i].Location = pkg.Locations[0].Path
		}
	}
	return out
}

// Compile-time assertion that App satisfies DoctorQueries.
var _ service.DoctorQueries = (*App)(nil)

// silence unused import (filepath needed for future expansions)
var _ = filepath.Join
