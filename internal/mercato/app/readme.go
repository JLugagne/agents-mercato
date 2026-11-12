package app

import (
	"path/filepath"
	"strings"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

func (a *App) Readme(market, path string) (domain.ReadmeEntry, error) {
	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return domain.ReadmeEntry{}, err
	}

	mc := findMarketConfig(cfg, market)
	if mc == nil {
		return domain.ReadmeEntry{}, domain.ErrMarketNotFound
	}

	clonePath := a.clonePath(mc.Name)
	content, err := a.git.ReadFileAtRef(clonePath, mc.Branch, path, "HEAD")
	if err != nil {
		return domain.ReadmeEntry{}, &domain.DomainError{
			Code:    "README_NOT_FOUND",
			Message: "README not found at " + path + " in market " + market,
			Err:     err,
		}
	}

	re := domain.ReadmeEntry{
		Market:  market,
		Path:    path,
		Content: string(content),
	}
	if rfm, err := domain.ParseReadmeFrontmatter(content); err == nil {
		re.MctTags = rfm.MctTags
	}
	return re, nil
}

func (a *App) ListReadmes(market string) ([]domain.ReadmeEntry, error) {
	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return nil, err
	}

	mc := findMarketConfig(cfg, market)
	if mc == nil {
		return nil, domain.ErrMarketNotFound
	}

	clonePath := a.clonePath(mc.Name)
	files, err := a.git.ListFiles(clonePath, mc.Branch)
	if err != nil {
		return nil, err
	}

	var readmes []domain.ReadmeEntry
	for _, f := range files {
		if !isReadme(f) {
			continue
		}
		content, err := a.git.ReadFileAtRef(clonePath, mc.Branch, f, "HEAD")
		if err != nil {
			continue
		}
		re := domain.ReadmeEntry{
			Market:  market,
			Path:    f,
			Content: string(content),
		}
		if rfm, err := domain.ParseReadmeFrontmatter(content); err == nil {
			re.MctTags = rfm.MctTags
		}
		readmes = append(readmes, re)
	}

	return readmes, nil
}

func isReadme(path string) bool {
	if !strings.EqualFold(filepath.Base(path), "README.md") {
		return false
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	return len(parts) == 3 // exactly */*/README.md
}
