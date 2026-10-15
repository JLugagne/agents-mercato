package domain

import (
	"encoding/json"
	"slices"
	"strings"
)

// InstallSchemaVersion is the current installed.json schema version.
// v1 (implicit) recorded Locations as []string and tool hashes in a flat
// ToolChecksums map. v2 records Locations as []InstalledLocation, each
// carrying a runtime type and a per-file {path, xxh} list.
const InstallSchemaVersion = 2

// InstallDatabase is the top-level structure persisted as installed.json.
type InstallDatabase struct {
	SchemaVersion int               `json:"schema_version"`
	Markets       []InstalledMarket `json:"markets"`
}

// InstalledMarket groups packages from one market.
type InstalledMarket struct {
	Market   string             `json:"market"`
	Packages []InstalledPackage `json:"packages"`
}

// InstalledPackage tracks a single installed profile/skill with its locations.
type InstalledPackage struct {
	Profile   string              `json:"profile"`
	Version   string              `json:"version"`
	Files     InstalledFiles      `json:"files"`
	Locations []InstalledLocation `json:"locations"`
}

// InstalledLocation records one project install of a package, including the
// runtime that consumed it (claude-code, cursor, opencode, ...) and the exact
// files written with their xxhash64 at install time.
//
// Version is the market commit the files at this location were last synced
// to. Empty on databases written before per-location versions existed, in
// which case the package Version applies.
type InstalledLocation struct {
	Path    string          `json:"path"`
	Type    string          `json:"type"`
	Version string          `json:"version,omitempty"`
	Files   []InstalledFile `json:"files,omitempty"`
}

// InstalledFile is one file written into a location, with its xxhash64 at
// the time of install or last update. Path is project-relative.
//
// For files written by a Transformer, Source is the repo path of the entry
// that produced the file. A Path containing "#" is a HookPatch anchor
// (HookPatch.Anchor) and Data holds the exact JSON entry appended there.
type InstalledFile struct {
	Path   string          `json:"path"`
	XXH    string          `json:"xxh"`
	Source string          `json:"source,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// IsHookPatch reports whether the record is a JSON patch inside a shared file
// rather than a file of its own.
func (f InstalledFile) IsHookPatch() bool {
	return strings.Contains(f.Path, "#")
}

// InstalledFiles lists the leaf names of installed skills and agents.
// This is package-level metadata used to drive sync diffs and refs; the
// authoritative on-disk file list lives in InstalledLocation.Files.
type InstalledFiles struct {
	Skills []string `json:"skills,omitempty"`
	// FlatSkills lists the Skills that come from a single-file skill
	// (skills/<name>.md) instead of a skill directory; both are installed as
	// .claude/skills/<name>/SKILL.md.
	FlatSkills []string `json:"flat_skills,omitempty"`
	Agents     []string `json:"agents,omitempty"`
	Commands   []string `json:"commands,omitempty"`
	Hooks      []string `json:"hooks,omitempty"`
}

func (f InstalledFiles) IsFlatSkill(name string) bool {
	return slices.Contains(f.FlatSkills, name)
}

// FindPackage returns a pointer to the installed package for the given market
// and profile, or nil if not found.
func (db *InstallDatabase) FindPackage(market, profile string) *InstalledPackage {
	m := db.FindMarket(market)
	if m == nil {
		return nil
	}
	for i := range m.Packages {
		if m.Packages[i].Profile == profile {
			return &m.Packages[i]
		}
	}
	return nil
}

// FindMarket returns a pointer to the installed market entry, or nil if not found.
func (db *InstallDatabase) FindMarket(market string) *InstalledMarket {
	for i := range db.Markets {
		if db.Markets[i].Market == market {
			return &db.Markets[i]
		}
	}
	return nil
}

// LocationPaths returns just the project paths for a package's locations.
// Callers that previously iterated pkg.Locations as []string can use this.
func (p *InstalledPackage) LocationPaths() []string {
	out := make([]string, 0, len(p.Locations))
	for _, loc := range p.Locations {
		out = append(out, loc.Path)
	}
	return out
}

// FindLocation returns a pointer to the location entry for the given project
// path, or nil if absent.
func (p *InstalledPackage) FindLocation(path string) *InstalledLocation {
	for i := range p.Locations {
		if p.Locations[i].Path == path {
			return &p.Locations[i]
		}
	}
	return nil
}

// AddOrUpdatePackage upserts a package in the database. If the market doesn't
// exist it is created. If the package doesn't exist it is created. The
// location is appended (with the given runtime type and file list) only if
// not already present; an existing location entry is overwritten.
// AddOrUpdatePackage upserts a package in the database. The market and
// package are created if missing. The (location.Path, location.Type) entry
// is replaced wholesale: callers must pass the FULL set of files that
// should be present at this location after the call. The package-level
// Files list is likewise replaced (it is a derived index over per-location
// files, not an accumulator).
//
// For incremental adds (e.g. installing a sibling skill into a profile
// that already has skills installed), the caller is responsible for
// merging the existing location's Files with the new entry's files
// before calling — see MergedLocationFiles / MergedPackageFiles helpers.
func (db *InstallDatabase) AddOrUpdatePackage(market, profile, version string, files InstalledFiles, location InstalledLocation) {
	m := db.FindMarket(market)
	if m == nil {
		db.Markets = append(db.Markets, InstalledMarket{Market: market})
		m = &db.Markets[len(db.Markets)-1]
	}

	var pkg *InstalledPackage
	for i := range m.Packages {
		if m.Packages[i].Profile == profile {
			pkg = &m.Packages[i]
			break
		}
	}
	if pkg == nil {
		m.Packages = append(m.Packages, InstalledPackage{Profile: profile})
		pkg = &m.Packages[len(m.Packages)-1]
	}

	location.Version = version
	if pkg.FindLocation(location.Path) != nil {
		location.Version = pkg.VersionAt(location.Path)
	}
	pkg.pinLegacyLocationVersions()
	pkg.Version = version
	pkg.Files = files

	for i := range pkg.Locations {
		if pkg.Locations[i].Path == location.Path && pkg.Locations[i].Type == location.Type {
			pkg.Locations[i] = location
			return
		}
	}
	pkg.Locations = append(pkg.Locations, location)
}

// VersionAt returns the market commit the files at the given project path
// were synced to, falling back to the package Version for legacy entries.
func (p *InstalledPackage) VersionAt(path string) string {
	for _, loc := range p.Locations {
		if loc.Path == path && loc.Version != "" {
			return loc.Version
		}
	}
	return p.Version
}

// SetVersionAt records that every runtime location at path is now synced to
// version, without moving the other project paths forward.
func (p *InstalledPackage) SetVersionAt(path, version string) {
	p.pinLegacyLocationVersions()
	for i := range p.Locations {
		if p.Locations[i].Path == path {
			p.Locations[i].Version = version
		}
	}
	p.Version = version
}

func (p *InstalledPackage) pinLegacyLocationVersions() {
	for i := range p.Locations {
		if p.Locations[i].Version == "" {
			p.Locations[i].Version = p.VersionAt(p.Locations[i].Path)
		}
	}
}

// mergeLocationFiles merges incoming file entries into existing, replacing
// the hash for any path that already appears.
// MergeLocationFiles returns the union of existing and incoming, with
// incoming taking precedence on hash conflicts. Used by callers that
// install a single new entry into a location that already holds files
// from sibling entries — they must compose the full set themselves
// before calling AddOrUpdatePackage.
func MergeLocationFiles(existing, incoming []InstalledFile) []InstalledFile {
	if len(incoming) == 0 {
		return append([]InstalledFile(nil), existing...)
	}
	out := append([]InstalledFile(nil), existing...)
	for _, f := range incoming {
		replaced := false
		for i := range out {
			if out[i].Path == f.Path && (!f.IsHookPatch() || out[i].Source == f.Source) {
				out[i] = f
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, f)
		}
	}
	return out
}

// MergePackageFiles returns the union of existing and incoming leaf-name
// lists. Used alongside MergeLocationFiles for incremental adds.
func MergePackageFiles(existing, incoming InstalledFiles) InstalledFiles {
	out := InstalledFiles{
		Skills:     append([]string(nil), existing.Skills...),
		FlatSkills: append([]string(nil), existing.FlatSkills...),
		Agents:     append([]string(nil), existing.Agents...),
		Commands:   append([]string(nil), existing.Commands...),
		Hooks:      append([]string(nil), existing.Hooks...),
	}
	for _, s := range incoming.FlatSkills {
		if !slices.Contains(out.FlatSkills, s) {
			out.FlatSkills = append(out.FlatSkills, s)
		}
	}
	for _, s := range incoming.Skills {
		if !slices.Contains(out.Skills, s) {
			out.Skills = append(out.Skills, s)
		}
	}
	for _, a := range incoming.Agents {
		if !slices.Contains(out.Agents, a) {
			out.Agents = append(out.Agents, a)
		}
	}
	for _, c := range incoming.Commands {
		if !slices.Contains(out.Commands, c) {
			out.Commands = append(out.Commands, c)
		}
	}
	for _, h := range incoming.Hooks {
		if !slices.Contains(out.Hooks, h) {
			out.Hooks = append(out.Hooks, h)
		}
	}
	return out
}

// RemoveLocation removes a location from a package. If the package has no
// remaining locations it is removed. If the market has no remaining packages
// it is removed.
func (db *InstallDatabase) RemoveLocation(market, profile, location string) {
	mi := -1
	for i := range db.Markets {
		if db.Markets[i].Market == market {
			mi = i
			break
		}
	}
	if mi < 0 {
		return
	}

	pi := -1
	for i := range db.Markets[mi].Packages {
		if db.Markets[mi].Packages[i].Profile == profile {
			pi = i
			break
		}
	}
	if pi < 0 {
		return
	}

	pkg := &db.Markets[mi].Packages[pi]
	locs := pkg.Locations[:0]
	for _, loc := range pkg.Locations {
		if loc.Path != location {
			locs = append(locs, loc)
		}
	}
	pkg.Locations = locs

	if len(pkg.Locations) == 0 {
		db.Markets[mi].Packages = append(db.Markets[mi].Packages[:pi], db.Markets[mi].Packages[pi+1:]...)
	}

	if len(db.Markets[mi].Packages) == 0 {
		db.Markets = append(db.Markets[:mi], db.Markets[mi+1:]...)
	}
}

// CleanStaleLocations removes locations where the directory no longer exists.
// It returns the list of removed paths and cleans up empty packages and markets.
func (db *InstallDatabase) CleanStaleLocations(dirExists func(string) bool) []string {
	var removed []string

	for mi := len(db.Markets) - 1; mi >= 0; mi-- {
		m := &db.Markets[mi]
		for pi := len(m.Packages) - 1; pi >= 0; pi-- {
			pkg := &m.Packages[pi]
			locs := pkg.Locations[:0]
			for _, loc := range pkg.Locations {
				if dirExists(loc.Path) {
					locs = append(locs, loc)
				} else {
					removed = append(removed, loc.Path)
				}
			}
			pkg.Locations = locs

			if len(pkg.Locations) == 0 {
				m.Packages = append(m.Packages[:pi], m.Packages[pi+1:]...)
			}
		}

		if len(m.Packages) == 0 {
			db.Markets = append(db.Markets[:mi], db.Markets[mi+1:]...)
		}
	}

	return removed
}
