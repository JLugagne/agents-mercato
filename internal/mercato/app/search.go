package app

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
	"github.com/JLugagne/bm25"
	"github.com/JLugagne/snowball"
)

func (a *App) Search(query string, opts service.SearchOpts) ([]service.SearchResult, error) {
	entries, err := a.buildCorpus()
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}

	// Apply filters to all entries.
	filtered := filterEntries(entries, opts)
	if len(filtered) == 0 {
		return nil, nil
	}

	// Empty query: return all filtered entries with score 0.
	if strings.TrimSpace(query) == "" {
		results := make([]service.SearchResult, len(filtered))
		for i, e := range filtered {
			results[i] = service.SearchResult{Entry: e}
		}
		return results, nil
	}

	// Group entries by profile for BM25 indexing. One document per profile
	// ensures IDF works properly — identical terms shared within a profile
	// don't dilute relevance.
	type profileData struct {
		key     string // "market@category"
		entries []domain.Entry
	}
	profileOrder := make([]string, 0)
	profileMap := make(map[string]*profileData)
	for _, e := range filtered {
		key := e.Market + "@" + e.Category
		pd, ok := profileMap[key]
		if !ok {
			pd = &profileData{key: key}
			profileMap[key] = pd
			profileOrder = append(profileOrder, key)
		}
		pd.entries = append(pd.entries, e)
	}

	// Build one BM25 document per profile.
	docs := make([]string, len(profileOrder))
	for i, key := range profileOrder {
		docs[i] = buildProfileDoc(profileMap[key].entries)
	}

	vocab := buildVocab(docs)

	index, err := bm25.NewBM25Okapi(docs, tokenize, 1.5, 0.75, nil)
	if err != nil {
		return nil, err
	}

	queryTokens := tokenize(strings.ToLower(query))
	queryTokens = expandFuzzy(queryTokens, vocab)

	scores, err := index.GetScores(queryTokens)
	if err != nil {
		return nil, err
	}

	type hit struct {
		index int
		score float64
	}
	var hits []hit
	for i, s := range scores {
		if s > 0 {
			hits = append(hits, hit{i, s})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })

	limit := opts.Limit
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}

	// Expand profile hits back to entry-level results.
	var results []service.SearchResult
	for _, h := range hits {
		pd := profileMap[profileOrder[h.index]]
		for _, e := range pd.entries {
			results = append(results, service.SearchResult{
				Entry: e,
				Score: h.score,
			})
		}
	}

	return results, nil
}

// filterEntries applies SearchOpts filters to a list of entries.
func filterEntries(entries []domain.Entry, opts service.SearchOpts) []domain.Entry {
	result := make([]domain.Entry, 0, len(entries))
	for _, e := range entries {
		if !opts.IncludeDeleted && e.Deleted {
			continue
		}
		if opts.Type != "" && e.Type != opts.Type {
			continue
		}
		if opts.Market != "" && e.Market != opts.Market {
			continue
		}
		if opts.Category != "" && !strings.HasPrefix(e.Category, opts.Category) {
			continue
		}
		if opts.Installed && !e.Installed {
			continue
		}
		if opts.NotInstalled && e.Installed {
			continue
		}
		result = append(result, e)
	}
	return result
}

func (a *App) DumpIndex() ([]domain.Entry, error) {
	return a.buildCorpus()
}

func (a *App) BenchIndex() (service.BenchResult, error) {
	t0 := time.Now()

	entries, err := a.buildCorpus()
	if err != nil {
		return service.BenchResult{}, err
	}
	scanDone := time.Now()

	// Group by profile.
	profileMap := make(map[string][]domain.Entry)
	var profileOrder []string
	for _, e := range entries {
		key := e.Market + "@" + e.Category
		if _, ok := profileMap[key]; !ok {
			profileOrder = append(profileOrder, key)
		}
		profileMap[key] = append(profileMap[key], e)
	}

	docs := make([]string, len(profileOrder))
	for i, key := range profileOrder {
		docs[i] = buildProfileDoc(profileMap[key])
	}

	vocab := buildVocab(docs)

	if len(docs) == 0 {
		return service.BenchResult{
			Scan:  scanDone.Sub(t0),
			Total: time.Since(t0),
		}, nil
	}

	_, err = bm25.NewBM25Okapi(docs, tokenize, 1.5, 0.75, nil)
	indexDone := time.Now()
	if err != nil {
		return service.BenchResult{}, err
	}

	return service.BenchResult{
		Scan:    scanDone.Sub(t0),
		Index:   indexDone.Sub(scanDone),
		Total:   indexDone.Sub(t0),
		Entries: len(entries),
		Vocab:   len(vocab),
	}, nil
}

func (a *App) buildCorpus() ([]domain.Entry, error) {
	cfg, err := a.cfg.Load(a.configPath)
	if err != nil {
		return nil, err
	}

	db, err := a.idb.Load(a.cacheDir)
	if err != nil {
		return nil, err
	}

	projectPath := projectPath(cfg.LocalPath)

	// Build a set of installed refs from installdb for quick lookup.
	installedRefs := make(map[domain.MctRef]bool)
	for _, im := range db.Markets {
		for _, pkg := range im.Packages {
			atLocation := false
			for _, loc := range pkg.Locations {
				if loc.Path == projectPath {
					atLocation = true
					break
				}
			}
			if !atLocation {
				continue
			}
			for _, ref := range a.packageFileRefs(im.Market, pkg) {
				installedRefs[ref] = true
			}
		}
	}

	var entries []domain.Entry
	for _, mc := range cfg.Markets {
		clonePath := a.clonePath(mc.Name)
		mfiles, err := a.git.ReadMarketFiles(clonePath, mc.Branch)
		if err != nil {
			continue
		}

		readmeByProfile := make(map[string]readmeInfo)
		for _, mf := range mfiles {
			if !isReadme(mf.Path) {
				continue
			}
			ri := readmeInfo{content: string(mf.Content)}
			if rfm, err := domain.ParseReadmeFrontmatter(mf.Content); err == nil {
				ri.tags = rfm.MctTags
				ri.description = rfm.Description
			}
			readmeByProfile[profilePrefix(mf.Path)] = ri
		}

		for _, mf := range mfiles {
			if isReadme(mf.Path) {
				continue
			}
			if mc.SkillsOnly && !isSkillPath(mf.Path, mc.SkillsPath) {
				continue
			}
			ref := domain.MctRef(mc.Name + "@" + mf.Path)
			entryType := inferEntryType(mf.Path)

			// Hooks are JSON snippets, not markdown. They have no
			// frontmatter; parse with the snippet validator instead so
			// they appear in the search index alongside agents/skills.
			if entryType == domain.EntryTypeHook {
				snip, err := domain.ParseHookSnippet(mf.Content)
				if err != nil {
					continue
				}
				desc := snip.Event
				if snip.Matcher != "" {
					desc = snip.Event + " " + snip.Matcher
				}
				entry := domain.Entry{
					Ref:         ref,
					Market:      mc.Name,
					RelPath:     mf.Path,
					Filename:    filepath.Base(mf.Path),
					Category:    inferCategory(mf.Path),
					Type:        entryType,
					Description: desc,
					Version:     mf.Version,
					Installed:   installedRefs[ref],
				}
				if ri, ok := readmeByProfile[profilePrefix(mf.Path)]; ok {
					entry.ReadmeContext = ri.content
					entry.MctTags = ri.tags
					entry.ProfileDescription = ri.description
				} else {
					entry.ReadmeContext = string(mf.Content)
				}
				entries = append(entries, entry)
				continue
			}

			fm, err := domain.ParseFrontmatter(mf.Content)
			if err != nil {
				continue
			}

			entry := domain.Entry{
				Ref:            ref,
				Market:         mc.Name,
				RelPath:        mf.Path,
				Filename:       filepath.Base(mf.Path),
				Category:       inferCategory(mf.Path),
				Type:           entryType,
				Description:    fm.Description,
				Author:         fm.Author,
				Version:        mf.Version,
				Installed:      installedRefs[ref],
				BreakingChange: fm.BreakingChange,
				Deprecated:     fm.Deprecated,
				RequiresSkills: fm.RequiresSkills,
			}

			if ri, ok := readmeByProfile[profilePrefix(mf.Path)]; ok {
				entry.ReadmeContext = ri.content
				entry.MctTags = ri.tags
				entry.ProfileDescription = ri.description
			} else {
				// Skills repos often lack a profile README. Fall back to
				// the entry's own content and description so the TUI can
				// display something meaningful.
				entry.ReadmeContext = string(mf.Content)
				if entry.ProfileDescription == "" {
					entry.ProfileDescription = fm.Description
				}
			}

			entries = append(entries, entry)
		}
	}
	return entries, nil
}

type readmeInfo struct {
	content     string
	tags        []string
	description string
}

// profilePrefix returns the first two path segments (the profile root).
// e.g. "dev/go/skills/foo.md" -> "dev/go", "dev/go/README.md" -> "dev/go"
func profilePrefix(path string) string {
	parts := strings.SplitN(filepath.ToSlash(path), "/", 3)
	if len(parts) < 2 {
		return path
	}
	return parts[0] + "/" + parts[1]
}

// buildProfileDoc builds a single searchable document for a profile by
// combining the profile path, README content, profile description, tags,
// and all entry descriptions. This gives BM25 one doc per profile so
// IDF works across profiles rather than across individual entries.
//
// Descriptions and tags are repeated to boost their TF weight in BM25,
// since they are the most relevant signals for search ranking.
func buildProfileDoc(entries []domain.Entry) string {
	const descBoost = 3 // repeat descriptions/tags N times for TF boost

	var b strings.Builder

	// Profile path tokens (from the first entry's category).
	if len(entries) > 0 {
		cat := strings.ReplaceAll(entries[0].Category, "/", " ")
		cat = strings.ReplaceAll(cat, "-", " ")
		cat = strings.ReplaceAll(cat, "_", " ")
		b.WriteString(cat)
	}

	// Profile-level README content and description (same for all entries in profile).
	if len(entries) > 0 {
		e := entries[0]
		if e.ProfileDescription != "" {
			for range descBoost {
				b.WriteByte(' ')
				b.WriteString(e.ProfileDescription)
			}
		}
		if e.ReadmeContext != "" {
			b.WriteByte(' ')
			b.WriteString(e.ReadmeContext)
		}
		if len(e.MctTags) > 0 {
			tags := strings.Join(e.MctTags, " ")
			for range descBoost {
				b.WriteByte(' ')
				b.WriteString(tags)
			}
		}
	}

	// Each entry's own description and filename.
	for _, e := range entries {
		name := strings.TrimSuffix(e.Filename, ".md")
		name = strings.ReplaceAll(name, "-", " ")
		name = strings.ReplaceAll(name, "_", " ")
		b.WriteByte(' ')
		b.WriteString(name)
		if e.Description != "" {
			for range descBoost {
				b.WriteByte(' ')
				b.WriteString(e.Description)
			}
		}
	}

	return b.String()
}

func inferCategory(relPath string) string {
	return profilePrefix(relPath)
}

var stemmers = sync.Pool{New: func() any {
	s, _ := snowball.NewStemmer("english",
		snowball.WithoutToLower(),
		snowball.WithoutTrimSpace(),
	)
	return s
}}

func tokenize(text string) []string {
	lower := strings.ToLower(text)
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	stemmer := stemmers.Get().(*snowball.Stemmer)
	defer stemmers.Put(stemmer)
	tokens := make([]string, 0, len(words))
	for _, w := range words {
		if w == "" {
			continue
		}
		runes, stemmed := stemmer.StemRunes(w, false)
		if stemmed {
			tokens = append(tokens, string(runes))
		} else {
			tokens = append(tokens, w)
		}
	}
	return tokens
}

func buildVocab(docs []string) []string {
	seen := make(map[string]struct{})
	for _, doc := range docs {
		for _, tok := range tokenize(doc) {
			seen[tok] = struct{}{}
		}
	}
	vocab := make([]string, 0, len(seen))
	for term := range seen {
		vocab = append(vocab, term)
	}
	return vocab
}

// expandFuzzy adds fuzzy matches for query tokens that have no exact match in
// the vocabulary. For each unknown token, it finds the closest term within
// edit distance 2 and appends it to the query.
func expandFuzzy(queryTokens []string, vocab []string) []string {
	vocabSet := make(map[string]struct{}, len(vocab))
	for _, v := range vocab {
		vocabSet[v] = struct{}{}
	}

	expanded := make([]string, 0, len(queryTokens)*2)
	for _, qt := range queryTokens {
		expanded = append(expanded, qt)
		if _, exists := vocabSet[qt]; exists {
			continue // exact match exists
		}
		bestTerm := ""
		bestDist := 3
		for _, v := range vocab {
			d := levenshtein(qt, v)
			if d < bestDist {
				bestDist = d
				bestTerm = v
			}
		}
		if bestTerm != "" {
			expanded = append(expanded, bestTerm)
		}
	}
	return expanded
}

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	// Early exit: if length difference > 2, skip expensive computation
	if la-lb > 2 || lb-la > 2 {
		return 3
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}
