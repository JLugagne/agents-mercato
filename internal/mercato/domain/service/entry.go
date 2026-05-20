package service

import "github.com/JLugagne/agents-mercato/internal/mercato/domain"

type EntryQueries interface {
	List(opts ListOpts) ([]domain.Entry, error)
	GetEntry(ref domain.MctRef) (domain.Entry, error)
	ReadEntryContent(market, relPath string) ([]byte, error)
	ListSkillDirFiles(market, dirPrefix string) ([]domain.SkillDirFile, error)
	Conflicts() ([]domain.Conflict, error)
}

// AddResult contains the outcome of an Add operation, including per-tool
// write results and any warnings (e.g. skipped tools).
type AddResult struct {
	ToolWrites map[string]string `json:"tool_writes,omitempty"` // tool -> output path
	Warnings   []string          `json:"warnings,omitempty"`
}

// RemoveResult contains the outcome of a Remove operation.
type RemoveResult struct {
	ToolsRemoved []string `json:"tools_removed,omitempty"`
}

type EntryCommands interface {
	EntryQueries
	Add(ref domain.MctRef, opts AddOpts) (AddResult, error)
	Remove(ref domain.MctRef, opts RemoveOpts) (RemoveResult, error)
	Prune(opts PruneOpts) ([]PruneResult, error)
	Init(opts InitOpts) error
}

type ListOpts struct {
	Market    string
	Type      domain.EntryType
	Installed bool
}

// ConfirmMarketFunc is called when an agent requires a skill from an
// unregistered market. It receives the market URL and returns true if the
// user agrees to register it.
type ConfirmMarketFunc func(marketURL string) bool

type AddOpts struct {
	Profile        string
	AcceptBreaking bool
	NoDeps         bool
	DryRun         bool
	ConfirmMarket  ConfirmMarketFunc
	Tools          map[string]bool // per-invocation tool override (e.g. {"opencode": true})
}

type RemoveOpts struct {
	AllLocations bool // remove from all locations, not just current project
}

type PruneOpts struct {
	Ref       domain.MctRef
	AllKeep   bool
	AllRemove bool
}

type PruneResult struct {
	Ref    domain.MctRef `json:"ref"`
	Action string        `json:"action"`
	Err    error         `json:"error,omitempty"`
}

type InitOpts struct {
	Markets   []string
	LocalPath string
	CI        bool
}
