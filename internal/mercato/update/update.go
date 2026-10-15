package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	repoOwner     = "JLugagne"
	repoName      = "agents-mercato"
	checkInterval = 24 * time.Hour
	stateFile     = "update-check.json"
)

// State persists the last update check result.
type State struct {
	LastCheck     time.Time `json:"last_check"`
	LatestVersion string    `json:"latest_version,omitempty"`
}

// Result describes the outcome of an update check.
type Result struct {
	UpdateAvailable bool
	CurrentVersion  string
	LatestVersion   string
}

func statePath(cacheDir string) string {
	return filepath.Join(cacheDir, stateFile)
}

func loadState(cacheDir string) State {
	data, err := os.ReadFile(statePath(cacheDir))
	if err != nil {
		return State{}
	}
	var s State
	_ = json.Unmarshal(data, &s)
	return s
}

func saveState(cacheDir string, s State) {
	data, _ := json.Marshal(s)
	_ = os.MkdirAll(cacheDir, 0o755)
	_ = os.WriteFile(statePath(cacheDir), data, 0o644)
}

func ShouldCheck(cacheDir string) bool {
	s := loadState(cacheDir)
	return time.Since(s.LastCheck) >= checkInterval
}

func CheckLatestVersion(cacheDir, currentVersion string) Result {
	result := Result{CurrentVersion: currentVersion}

	latest, err := fetchLatestVersion()
	s := State{LastCheck: time.Now()}
	if err == nil {
		s.LatestVersion = latest
		result.LatestVersion = latest
		result.UpdateAvailable = currentVersion != "dev" && newerVersion(latest, currentVersion)
	}
	saveState(cacheDir, s)
	return result
}

func CachedResult(cacheDir, currentVersion string) Result {
	s := loadState(cacheDir)
	return Result{
		CurrentVersion:  currentVersion,
		LatestVersion:   s.LatestVersion,
		UpdateAvailable: currentVersion != "dev" && newerVersion(s.LatestVersion, currentVersion),
	}
}

func fetchLatestVersion() (string, error) {
	u, err := defaultUpgrader()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return u.latestTag(ctx)
}

// RunUpgrade replaces the running binary with the latest GitHub release when
// it is newer, after verifying the archive against the release checksums.
func RunUpgrade(currentVersion string) error {
	u, err := defaultUpgrader()
	if err != nil {
		return err
	}
	latest, updated, err := u.upgrade(context.Background(), currentVersion)
	if err != nil {
		return errors.Join(errors.New("upgrade failed"), err)
	}
	if updated {
		fmt.Printf("  ok  updated %s → %s\n", currentVersion, latest)
	} else {
		fmt.Println("  ok  already at latest version", latest)
	}
	return nil
}

func FormatUpdateNotice(r Result) string {
	return fmt.Sprintf("A new version of mct is available: %s → %s (run `mct upgrade` to update)", r.CurrentVersion, r.LatestVersion)
}
