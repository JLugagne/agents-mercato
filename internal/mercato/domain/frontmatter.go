package domain

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Frontmatter struct {
	Description    string     `yaml:"description"`
	Author         string     `yaml:"author"`
	BreakingChange bool       `yaml:"breaking_change"`
	Deprecated     bool       `yaml:"deprecated"`
	RequiresSkills []SkillDep `yaml:"requires_skills"`
	MctRef         MctRef     `yaml:"mct_ref"`
	MctVersion     MctVersion `yaml:"mct_version"`
	MctMarket      string     `yaml:"mct_market"`
	MctProfile     string     `yaml:"mct_profile"`
	MctInstalledAt time.Time  `yaml:"mct_installed_at"`
	MctChecksum    string     `yaml:"mct_checksum"`
}

type ReadmeFrontmatter struct {
	MctTags     []string `yaml:"tags"`
	Description string   `yaml:"description"`
}

func ParseReadmeFrontmatter(content []byte) (ReadmeFrontmatter, error) {
	var fm ReadmeFrontmatter
	fmBytes, err := ExtractFrontmatterBytes(content)
	if err != nil {
		return fm, err
	}
	if err := yaml.Unmarshal(fmBytes, &fm); err != nil {
		return fm, fmt.Errorf("parse readme frontmatter: %w", err)
	}
	return fm, nil
}

func ParseFrontmatter(content []byte) (Frontmatter, error) {
	var fm Frontmatter
	fmBytes, err := ExtractFrontmatterBytes(content)
	if err != nil {
		return fm, err
	}
	if err := yaml.Unmarshal(fmBytes, &fm); err != nil {
		return fm, fmt.Errorf("parse frontmatter: %w", err)
	}
	for _, dep := range fm.RequiresSkills {
		if err := validateSkillDepPath(dep.File); err != nil {
			return Frontmatter{}, err
		}
	}
	return fm, nil
}

// safePathRe allows only alphanumerics, hyphens, underscores, dots, and forward slashes.
var safePathRe = regexp.MustCompile(`^[a-zA-Z0-9/_.\-]+$`)

// validateSkillDepPath ensures a skill dependency path is safe:
// no path traversal, no absolute paths, no shell metacharacters.
func validateSkillDepPath(path string) error {
	if path == "" {
		return fmt.Errorf("requires_skills: empty file path")
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("requires_skills: absolute path not allowed: %s", path)
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("requires_skills: path traversal not allowed: %s", path)
	}
	if !safePathRe.MatchString(path) {
		return fmt.Errorf("requires_skills: invalid characters in path: %s", path)
	}
	return nil
}

func ExtractFrontmatterBytes(content []byte) ([]byte, error) {
	s := string(content)
	if !strings.HasPrefix(s, "---") {
		return nil, fmt.Errorf("content does not start with ---")
	}
	end := strings.Index(s[3:], "\n---")
	if end == -1 {
		return nil, fmt.Errorf("closing --- not found")
	}
	raw := s[3 : end+3]
	raw = strings.TrimPrefix(raw, "\n")
	return []byte(raw), nil
}

// StripRequiresSkills removes the requires_skills block from the frontmatter,
// including its list items, leaving every other key untouched. Content without
// parseable frontmatter is returned unchanged.
func StripRequiresSkills(content []byte) []byte {
	fmBytes, err := ExtractFrontmatterBytes(content)
	if err != nil {
		return content
	}

	lines := bytes.Split(fmBytes, []byte("\n"))
	kept := make([][]byte, 0, len(lines))
	inBlock := false
	for _, line := range lines {
		if inBlock {
			if len(bytes.TrimSpace(line)) == 0 || line[0] == ' ' || line[0] == '\t' || line[0] == '-' {
				continue
			}
			inBlock = false
		}
		if bytes.HasPrefix(line, []byte("requires_skills:")) {
			inBlock = true
			continue
		}
		kept = append(kept, line)
	}
	newFM := bytes.TrimSpace(bytes.Join(kept, []byte("\n")))

	return bytes.Replace(content, fmBytes, newFM, 1)
}

func InjectMctFields(content []byte, ref MctRef, version MctVersion, market string, checksum string, profile string) ([]byte, error) {
	fmBytes, err := ExtractFrontmatterBytes(content)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	mctBlock := fmt.Sprintf("mct_ref: %s\nmct_version: %s\nmct_market: %s\nmct_profile: %s\nmct_installed_at: %s\nmct_checksum: %s",
		string(ref), string(version), market, profile, now, checksum)
	oldFM := string(fmBytes)
	newFM := mctBlock + "\n\n" + oldFM
	result := bytes.Replace(content, fmBytes, []byte(newFM), 1)
	return result, nil
}

var reMctVersion = regexp.MustCompile(`(?m)^mct_version:.*$`)

func PatchMctVersion(content []byte, newVersion MctVersion) ([]byte, error) {
	if !reMctVersion.Match(content) {
		return nil, fmt.Errorf("mct_version field not found in content")
	}
	return reMctVersion.ReplaceAll(content, []byte("mct_version: "+string(newVersion))), nil
}

var reMctChecksum = regexp.MustCompile(`(?m)^mct_checksum:.*$`)

func PatchMctChecksum(content []byte, newChecksum string) ([]byte, error) {
	if !reMctChecksum.Match(content) {
		return nil, fmt.Errorf("mct_checksum field not found in content")
	}
	return reMctChecksum.ReplaceAll(content, []byte("mct_checksum: "+newChecksum)), nil
}
