package tui

import (
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

type countingMarkets struct {
	service.MarketCommands
	calls atomic.Int32
}

func (c *countingMarkets) ListMarkets() ([]domain.Market, error) {
	c.calls.Add(1)
	return []domain.Market{{Name: "flat", SkillsOnly: true}}, nil
}

type countingSearch struct {
	service.SearchQueries
	calls atomic.Int32
}

func (c *countingSearch) Search(string, service.SearchOpts) ([]service.SearchResult, error) {
	c.calls.Add(1)
	return nil, nil
}

func asModel(m tea.Model) AppModel {
	if p, ok := m.(*AppModel); ok {
		return *p
	}
	return m.(AppModel)
}

func readyModel(svc TUIServices) AppModel {
	m := NewAppModel(svc)
	m.loading = false
	m.mode = ModeNormal
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return asModel(next)
}

func runCmd(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if batch, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, c := range batch {
				out = append(out, runCmd(t, c)...)
			}
			return out
		}
		if msg == nil {
			return nil
		}
		return []tea.Msg{msg}
	case <-time.After(time.Second):
		return nil
	}
}

func drain(t *testing.T, m AppModel, cmds ...tea.Cmd) AppModel {
	t.Helper()
	for _, cmd := range cmds {
		for _, msg := range runCmd(t, cmd) {
			if !strings.HasSuffix(reflect.TypeOf(msg).PkgPath(), "/tui") {
				continue
			}
			next, c := m.Update(msg)
			m = drain(t, asModel(next), c)
		}
	}
	return m
}

func TestAppModel_UpdateDoesNotListMarketsSynchronously(t *testing.T) {
	markets := &countingMarkets{}
	svc := TUIServices{Markets: markets, Search: &countingSearch{}}

	for name, msg := range map[string]tea.Msg{
		"index ready":    IndexReadyMsg{},
		"market added":   MarketAddedMsg{Name: "x"},
		"market removed": MarketRemovedMsg{Name: "x"},
		"popup key":      tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}},
	} {
		t.Run(name, func(t *testing.T) {
			markets.calls.Store(0)
			m := readyModel(svc)
			_, _ = m.Update(msg)
			require.Zero(t, markets.calls.Load(), "ListMarkets must run in a tea.Cmd, not inside Update")
		})
	}
}

func TestAppModel_SkillsOnlyMarketsLoadedAsync(t *testing.T) {
	svc := TUIServices{Markets: &countingMarkets{}, Search: &countingSearch{}}
	m := readyModel(svc)
	m = drain(t, m, m.loadAllMarkets())
	require.True(t, m.skillsOnlyMarkets["flat"])
}

func TestAppModel_MarketPopupLoadedAsync(t *testing.T) {
	svc := TUIServices{Markets: &countingMarkets{}, Search: &countingSearch{}}
	m := readyModel(svc)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m = drain(t, asModel(next), cmd)
	require.Equal(t, ModeMarketPopup, m.mode)
	require.Len(t, m.marketPopup.markets, 1)
}

func TestAppModel_SearchIsDebounced(t *testing.T) {
	search := &countingSearch{}
	m := readyModel(TUIServices{Markets: &countingMarkets{}, Search: search})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = asModel(next)

	var cmds []tea.Cmd
	for _, r := range "abc" {
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = asModel(next)
		cmds = append(cmds, cmd)
	}
	m = drain(t, m, cmds...)

	require.Equal(t, "abc", m.searchQuery)
	require.Equal(t, int32(1), search.calls.Load())
	require.False(t, m.searching)
}

func TestRenderMarkdown_CacheIsBounded(t *testing.T) {
	mdRendererMu.Lock()
	mdRenderCache = newBoundedCache[mdCacheKey, string](mdRenderCacheMax)
	mdRendererMu.Unlock()

	for i := range 400 {
		_ = renderMarkdown(fmt.Sprintf("# doc %d", i), 40)
	}
	for w := 20; w < 80; w++ {
		_ = renderMarkdown("x", w)
	}

	mdRendererMu.Lock()
	defer mdRendererMu.Unlock()
	require.LessOrEqual(t, mdRenderCache.len(), 256)
	require.LessOrEqual(t, mdRenderers.len(), 16)
}
