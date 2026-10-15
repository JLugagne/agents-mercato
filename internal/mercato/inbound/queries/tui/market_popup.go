package tui

import (
	"fmt"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type MarketAction int

const (
	MarketActionNone MarketAction = iota
	MarketActionAdd
	MarketActionDelete
)

type MarketPopup struct {
	markets  []domain.Market
	selected map[string]bool
	cursor   int
	action   MarketAction
	input    textinput.Model // for add (URL)
	errMsg   string
}

func newMarketPopup() MarketPopup {
	ti := textinput.New()
	ti.CharLimit = 256
	return MarketPopup{input: ti, selected: make(map[string]bool)}
}

func (p *MarketPopup) load(markets []domain.Market) {
	p.action = MarketActionNone
	p.errMsg = ""
	p.setMarkets(markets)
}

func (p *MarketPopup) setMarkets(markets []domain.Market) {
	p.markets = markets
	if p.cursor >= len(markets) {
		p.cursor = max(0, len(markets)-1)
	}
	if p.selected == nil {
		p.selected = make(map[string]bool)
	}
	existing := p.selected
	p.selected = make(map[string]bool, len(markets))
	for _, mk := range markets {
		if was, seen := existing[mk.Name]; seen {
			p.selected[mk.Name] = was
		} else {
			p.selected[mk.Name] = true
		}
	}
}

func (p *MarketPopup) selectedMarket() (domain.Market, bool) {
	if len(p.markets) == 0 {
		return domain.Market{}, false
	}
	return p.markets[p.cursor], true
}

func (m *AppModel) handleMarketPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.marketPopup

	// Handle sub-action input modes first
	if p.action == MarketActionAdd {
		switch msg.String() {
		case "esc":
			p.action = MarketActionNone
			p.input.Blur()
			p.input.Reset()
			p.errMsg = ""
			return m, nil
		case "enter":
			val := p.input.Value()
			if val == "" {
				return m, nil
			}
			p.input.Blur()
			p.input.Reset()
			p.action = MarketActionNone
			return m, m.addMarketCmd(val)
		}
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "esc", "m":
		m.mode = ModeNormal
		p.errMsg = ""
		return m, nil
	case "j", "down":
		if p.cursor < len(p.markets)-1 {
			p.cursor++
		}
		return m, nil
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
		}
		return m, nil
	case " ":
		mk, ok := p.selectedMarket()
		if !ok {
			return m, nil
		}
		p.selected[mk.Name] = !p.selected[mk.Name]
		return m, m.applyMarketFilterCmd()
	case "A":
		for _, mk := range p.markets {
			p.selected[mk.Name] = true
		}
		return m, m.applyMarketFilterCmd()
	case "N":
		for _, mk := range p.markets {
			p.selected[mk.Name] = false
		}
		return m, m.applyMarketFilterCmd()
	case "a":
		p.action = MarketActionAdd
		p.input.Placeholder = "git URL (e.g. git@github.com:user/repo.git)"
		p.input.Focus()
		p.errMsg = ""
		return m, textinput.Blink
	case "d":
		mk, ok := p.selectedMarket()
		if !ok {
			return m, nil
		}
		return m, m.removeMarketCmd(mk.Name)
	case "R":
		return m, m.refreshAllCmd()
	}

	return m, nil
}

func (m *AppModel) addMarketCmd(url string) tea.Cmd {
	return func() tea.Msg {
		_, err := m.svc.Markets.AddMarket(url, service.AddMarketOpts{})
		return MarketAddedMsg{Name: url, Err: err}
	}
}

func (m *AppModel) removeMarketCmd(name string) tea.Cmd {
	return func() tea.Msg {
		err := m.svc.Markets.RemoveMarket(name, service.RemoveMarketOpts{})
		return MarketRemovedMsg{Name: name, Err: err}
	}
}

type marketStats struct {
	profiles int
	agents   int
	skills   int
	commands int
	hooks    int
}

func (m *AppModel) recomputeMarketStats() {
	stats := make(map[string]marketStats)
	profileSeen := make(map[string]bool)
	for _, ei := range m.allEntries {
		e := ei.Entry
		ms := stats[e.Market]
		profKey := e.Market + "/" + e.Category
		if !profileSeen[profKey] {
			profileSeen[profKey] = true
			ms.profiles++
		}
		switch e.Type {
		case domain.EntryTypeAgent:
			ms.agents++
		case domain.EntryTypeSkill:
			ms.skills++
		case domain.EntryTypeCommand:
			ms.commands++
		case domain.EntryTypeHook:
			ms.hooks++
		}
		stats[e.Market] = ms
	}
	m.cachedMktStats = stats
}

func (m AppModel) viewMarketPopup() string {
	p := m.marketPopup

	width := 60
	if m.width < 70 {
		width = m.width - 10
	}

	bold := lipgloss.NewStyle().Bold(true)
	muted := lipgloss.NewStyle().Foreground(ColorMuted)
	selected := lipgloss.NewStyle().Foreground(ColorSelected)

	stats := m.cachedMktStats

	var s string
	s += bold.Render("Markets") + "\n\n"

	if len(p.markets) == 0 {
		s += muted.Render("No markets configured") + "\n"
	} else {
		for i, mk := range p.markets {
			cursor := "  "
			nameStyle := lipgloss.NewStyle()
			if i == p.cursor {
				cursor = "> "
				nameStyle = selected
			}
			check := "[ ]"
			if p.selected[mk.Name] {
				check = "[x]"
			}
			s += cursor + check + " " + nameStyle.Render(mk.Name) + "\n"
			s += "      " + muted.Render(mk.URL) + "\n"
			if ms, ok := stats[mk.Name]; ok {
				s += "      " + muted.Render(fmt.Sprintf("%d profiles  %d agents  %d skills  %d commands  %d hooks", ms.profiles, ms.agents, ms.skills, ms.commands, ms.hooks)) + "\n"
			}
		}
	}

	s += "\n"

	if p.action == MarketActionAdd {
		s += "Add market URL: " + p.input.View() + "\n"
	} else {
		s += muted.Render("space toggle  A select all  N select none  a add  d delete  R refresh  esc close") + "\n"
	}

	if p.errMsg != "" {
		s += "\n" + lipgloss.NewStyle().Foreground(ColorDanger).Render(p.errMsg) + "\n"
	}

	popup := StyleBorder.
		Width(width).
		Padding(1, 2).
		Render(s)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, popup)
}
