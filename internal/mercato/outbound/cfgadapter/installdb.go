package cfgadapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cespare/xxhash/v2"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/installdb"
)

var _ installdb.InstallDB = (*InstallDBAdapter)(nil)

// InstallDBAdapter persists installed.json. Lock/Unlock use an OS-level
// exclusive lock on installed.lock (flock on unix, LockFileEx on Windows),
// so a crashed process never leaves a stale lock behind and Unlock only
// releases a lock this adapter acquired.
type InstallDBAdapter struct {
	lockTimeout time.Duration
	lockPoll    time.Duration
	locks       fileLocker
}

func NewInstallDB() *InstallDBAdapter {
	return &InstallDBAdapter{lockTimeout: 5 * time.Second, lockPoll: 100 * time.Millisecond}
}

func (a *InstallDBAdapter) Load(cacheDir string) (domain.InstallDatabase, error) {
	path := filepath.Join(cacheDir, "installed.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return domain.InstallDatabase{SchemaVersion: domain.InstallSchemaVersion, Markets: []domain.InstalledMarket{}}, nil
	}
	if err != nil {
		return domain.InstallDatabase{}, fmt.Errorf("read install database: %w", err)
	}

	// Probe schema_version without committing to a shape.
	var probe struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return domain.InstallDatabase{}, fmt.Errorf("parse install database: %w", err)
	}

	if probe.SchemaVersion >= domain.InstallSchemaVersion {
		var db domain.InstallDatabase
		if err := json.Unmarshal(data, &db); err != nil {
			return domain.InstallDatabase{}, fmt.Errorf("parse install database: %w", err)
		}
		if db.Markets == nil {
			db.Markets = []domain.InstalledMarket{}
		}
		return db, nil
	}

	// schema_version is missing or 1 — migrate.
	db, migrated, err := migrateV1(data)
	if err != nil {
		return domain.InstallDatabase{}, err
	}
	if migrated {
		// Persist migrated DB so subsequent loads are fast.
		if saveErr := a.Save(cacheDir, db); saveErr != nil {
			// Non-fatal: return the migrated in-memory DB even if we can't persist.
			_ = saveErr
		}
	}
	return db, nil
}

// legacyInstallDB mirrors the v1 schema for migration purposes.
type legacyInstallDB struct {
	Markets []legacyMarket `json:"markets"`
}

type legacyMarket struct {
	Market   string          `json:"market"`
	Packages []legacyPackage `json:"packages"`
}

type legacyPackage struct {
	Profile       string                `json:"profile"`
	Version       string                `json:"version"`
	Files         domain.InstalledFiles `json:"files"`
	Locations     []string              `json:"locations"`
	ToolChecksums map[string]string     `json:"tool_checksums,omitempty"`
}

// migrateV1 converts a legacy v1 install database into the current schema.
// For each legacy location it walks the recorded skills/agents on disk,
// hashes every file present, classifies it by its leading dot-folder, and
// splits into one InstalledLocation per detected runtime type. ToolChecksums
// from the legacy record seed the hash for the matching tool's primary file
// when the file itself can't be read.
func migrateV1(data []byte) (domain.InstallDatabase, bool, error) {
	var legacy legacyInstallDB
	if err := json.Unmarshal(data, &legacy); err != nil {
		return domain.InstallDatabase{}, false, fmt.Errorf("parse install database: %w", err)
	}

	out := domain.InstallDatabase{
		SchemaVersion: domain.InstallSchemaVersion,
		Markets:       make([]domain.InstalledMarket, 0, len(legacy.Markets)),
	}

	for _, lm := range legacy.Markets {
		nm := domain.InstalledMarket{Market: lm.Market}
		for _, lp := range lm.Packages {
			np := domain.InstalledPackage{
				Profile: lp.Profile,
				Version: lp.Version,
				Files:   lp.Files,
			}
			for _, locPath := range lp.Locations {
				np.Locations = append(np.Locations, migrateLocation(locPath, lp)...)
			}
			nm.Packages = append(nm.Packages, np)
		}
		out.Markets = append(out.Markets, nm)
	}
	if out.Markets == nil {
		out.Markets = []domain.InstalledMarket{}
	}
	return out, true, nil
}

// migrateLocation walks the on-disk files for a legacy location and returns
// one InstalledLocation per detected runtime type. If no files can be read
// for a known runtime, falls back to a single claude-code location with no
// file hashes (best-effort, will refresh on next install/update).
func migrateLocation(locPath string, lp legacyPackage) []domain.InstalledLocation {
	byType := make(map[string]*domain.InstalledLocation)

	add := func(runtimeType, projectRelPath string, hash string) {
		loc, ok := byType[runtimeType]
		if !ok {
			loc = &domain.InstalledLocation{Path: locPath, Type: runtimeType}
			byType[runtimeType] = loc
		}
		for i := range loc.Files {
			if loc.Files[i].Path == projectRelPath {
				if hash != "" {
					loc.Files[i].XXH = hash
				}
				return
			}
		}
		loc.Files = append(loc.Files, domain.InstalledFile{Path: projectRelPath, XXH: hash})
	}

	// Walk the legacy .claude/ tree for this location (the only path the v1
	// install code wrote to directly).
	claudeBase := filepath.Join(locPath, ".claude")
	for _, skill := range lp.Files.Skills {
		skillDir := filepath.Join(claudeBase, "skills", skill)
		entries, err := os.ReadDir(skillDir)
		if err != nil {
			// Record the skill anyway with no hash so we don't lose tracking.
			add(domain.RuntimeTypeClaudeCode, filepath.ToSlash(filepath.Join(".claude/skills", skill, "SKILL.md")), "")
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			abs := filepath.Join(skillDir, e.Name())
			rel := filepath.ToSlash(filepath.Join(".claude/skills", skill, e.Name()))
			add(domain.RuntimeTypeClaudeCode, rel, hashFile(abs))
		}
	}
	for _, agent := range lp.Files.Agents {
		abs := filepath.Join(claudeBase, "agents", agent)
		rel := filepath.ToSlash(filepath.Join(".claude/agents", agent))
		add(domain.RuntimeTypeClaudeCode, rel, hashFile(abs))
	}

	// Walk other tool dot-folders we recognize, hashing any files that exist.
	// We don't know the exact transformer paths from data alone, so we only
	// scan well-known top-level dot-folders that the legacy ToolChecksums
	// map advertised.
	for tool, expectedHash := range lp.ToolChecksums {
		runtimeType := tool
		// Best-effort: look for any file under .<tool>/ in the location.
		dotDir := filepath.Join(locPath, "."+tool)
		_ = filepath.WalkDir(dotDir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, relErr := filepath.Rel(locPath, p)
			if relErr != nil {
				return nil
			}
			add(runtimeType, filepath.ToSlash(rel), hashFile(p))
			return nil
		})
		// If nothing was found on disk, still record the runtime type with
		// the legacy hash so drift detection has *something* to compare.
		if _, ok := byType[runtimeType]; !ok {
			byType[runtimeType] = &domain.InstalledLocation{
				Path: locPath,
				Type: runtimeType,
				Files: []domain.InstalledFile{
					{Path: "." + tool, XXH: expectedHash},
				},
			}
		}
	}

	if len(byType) == 0 {
		// Fallback: record an empty claude-code location.
		return []domain.InstalledLocation{{Path: locPath, Type: domain.RuntimeTypeClaudeCode}}
	}

	out := make([]domain.InstalledLocation, 0, len(byType))
	for _, loc := range byType {
		out = append(out, *loc)
	}
	return out
}

// hashFile returns the hex-encoded xxhash64 of the file's contents, or "" on
// any error.
func hashFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%016x", xxhash.Sum64(data))
}

func (a *InstallDBAdapter) Save(cacheDir string, db domain.InstallDatabase) error {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}
	data, err := a.Marshal(db)
	if err != nil {
		return err
	}
	return writeFileAtomic(a.Path(cacheDir), data, 0600)
}

// Marshal returns installed.json's on-disk byte representation.
func (a *InstallDBAdapter) Marshal(db domain.InstallDatabase) ([]byte, error) {
	if db.SchemaVersion == 0 {
		db.SchemaVersion = domain.InstallSchemaVersion
	}
	data, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal install database: %w", err)
	}
	return data, nil
}

// Path returns the absolute path of installed.json within cacheDir.
func (a *InstallDBAdapter) Path(cacheDir string) string {
	return filepath.Join(cacheDir, "installed.json")
}

// Lock blocks for up to five seconds and returns domain.ErrLockContention
// when another process (or another Lock on the same cache dir) holds it.
func (a *InstallDBAdapter) Lock(cacheDir string) error {
	return a.locks.lock(filepath.Join(cacheDir, "installed.lock"), a.lockTimeout, a.lockPoll)
}

func (a *InstallDBAdapter) Unlock(cacheDir string) error {
	return a.locks.unlock(filepath.Join(cacheDir, "installed.lock"))
}
