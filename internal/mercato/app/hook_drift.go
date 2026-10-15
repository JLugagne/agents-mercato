package app

import (
	"os"
	"path/filepath"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

func (a *App) detectHookDrift(market string, pkg domain.InstalledPackage, location string) []string {
	if len(pkg.Files.Hooks) == 0 {
		return nil
	}
	settingsPath := filepath.Join(location, ".claude", "settings.json")

	content, err := os.ReadFile(settingsPath)
	if err != nil {
		return allHooksDrifted(pkg)
	}
	ownersContent, _, err := readOptional(os.ReadFile, hookOwnersPath(settingsPath))
	if err != nil {
		return allHooksDrifted(pkg)
	}
	st, err := parseHookState(content, ownersContent, true)
	if err != nil {
		return allHooksDrifted(pkg)
	}
	resolved := st.resolve()

	var drifted []string
	loc := findLocationByPathAndType(&pkg, location, domain.RuntimeTypeClaudeCode)
	for _, hookFile := range pkg.Files.Hooks {
		ref := domain.MctRef(market + "@" + hookFileRepoPathStandalone(pkg.Profile, hookFile))
		at, ok := resolved[mctIDForRef(ref)]
		if !ok {
			drifted = append(drifted, "hooks/"+hookFile)
			continue
		}
		matcher, handlers, _ := parseHookGroup(st.hooks[at.event][at.index])
		current, err := hookChecksum(matcher, handlers)
		if err != nil || current != lookupHookChecksum(loc, hookFile) {
			drifted = append(drifted, "hooks/"+hookFile)
		}
	}
	return drifted
}

func allHooksDrifted(pkg domain.InstalledPackage) []string {
	out := make([]string, 0, len(pkg.Files.Hooks))
	for _, h := range pkg.Files.Hooks {
		out = append(out, "hooks/"+h)
	}
	return out
}

func lookupHookChecksum(loc *domain.InstalledLocation, hookFile string) string {
	if loc == nil {
		return ""
	}
	suffix := "#hooks/" + hookFile
	for _, f := range loc.Files {
		if len(f.Path) >= len(suffix) && f.Path[len(f.Path)-len(suffix):] == suffix {
			return f.XXH
		}
	}
	return ""
}
