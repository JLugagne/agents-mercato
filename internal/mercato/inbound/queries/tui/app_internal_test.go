package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

func TestAppModel_TinyWindowDoesNotPanic(t *testing.T) {
	content := strings.Repeat("line\n", 40)
	for w := 0; w <= 130; w += 3 {
		for h := 0; h <= 12; h++ {
			t.Run(fmt.Sprintf("%dx%d", w, h), func(t *testing.T) {
				m := NewAppModel(TUIServices{})
				m.loading = false
				m.mode = ModeNormal
				next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
				m = next.(AppModel)
				m.contentView.SetContent(content)
				m.contentView.YOffset = 10
				next, _ = m.Update(EntryContentMsg{Content: "short"})
				m = next.(AppModel)
				require.GreaterOrEqual(t, m.contentView.Height, 0)
				require.GreaterOrEqual(t, m.entriesList.Height(), 0)
				_ = m.View()
			})
		}
	}
}

func TestAppModel_StaleSearchResultIgnored(t *testing.T) {
	m := NewAppModel(TUIServices{})
	m.loading = false
	m.mode = ModeNormal
	m.searchQuery = "current"
	m.filteredEntries = []EntryItem{{}}

	next, _ := m.Update(SearchResultMsg{Query: "stale", Results: []service.SearchResult{{}, {}, {}}})
	got := next.(AppModel)

	require.Len(t, got.filteredEntries, 1)
}
