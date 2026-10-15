package domain

import (
	"errors"
	"fmt"
)

type DomainError struct {
	Code    string
	Message string
	Err     error
}

func (e *DomainError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *DomainError) Unwrap() error {
	return e.Err
}

func IsDomainError(err error) bool {
	var de *DomainError
	return errors.As(err, &de)
}

func (e *DomainError) Wrap(err error) *DomainError {
	return &DomainError{Code: e.Code, Message: e.Message, Err: err}
}

var (
	ErrMarketNotFound      = &DomainError{Code: "MARKET_NOT_FOUND", Message: "market not found in configuration"}
	ErrMarketAlreadyExists = &DomainError{Code: "MARKET_ALREADY_EXISTS", Message: "market already exists in configuration"}
	ErrMarketURLExists     = &DomainError{Code: "MARKET_URL_EXISTS", Message: "a market with this URL is already registered"}
	ErrMarketUnreachable   = &DomainError{Code: "MARKET_UNREACHABLE", Message: "could not reach market repository"}
)

var (
	ErrEntryNotFound         = &DomainError{Code: "ENTRY_NOT_FOUND", Message: "entry not found"}
	ErrEntryAlreadyInstalled = &DomainError{Code: "ENTRY_ALREADY_INSTALLED", Message: "entry is already installed"}
	ErrEntryNotInstalled     = &DomainError{Code: "ENTRY_NOT_INSTALLED", Message: "entry is not installed"}
	ErrEntryOrphaned         = &DomainError{Code: "ENTRY_ORPHANED", Message: "entry's market has been removed"}
)

var (
	ErrInvalidFrontmatter = &DomainError{Code: "INVALID_FRONTMATTER", Message: "frontmatter missing required fields"}
	ErrInvalidEntryType   = &DomainError{Code: "INVALID_ENTRY_TYPE", Message: "entry type does not match parent directory"}
	ErrMctFieldsInRepo    = &DomainError{Code: "MCT_FIELDS_IN_REPO", Message: "mct_ref/version/market/installed_at must not be in market repo files"}
)

var (
	ErrSkillNotFound     = &DomainError{Code: "SKILL_NOT_FOUND", Message: "required skill not found in market"}
	ErrSkillTypeMismatch = &DomainError{Code: "SKILL_TYPE_MISMATCH", Message: "file exists but type is not 'skill'"}
	ErrPinMismatch       = &DomainError{Code: "PIN_MISMATCH", Message: "pinned SHA does not match current version"}
)

var (
	ErrSyncDirty   = &DomainError{Code: "SYNC_DIRTY", Message: "previous sync did not complete cleanly"}
	ErrCacheStale  = &DomainError{Code: "CACHE_STALE", Message: "market cache is older than stale_after threshold"}
	ErrOfflineMode = &DomainError{Code: "OFFLINE_MODE", Message: "network operation requested in offline mode"}
)

var (
	ErrConflictRefCollision     = &DomainError{Code: "CONFLICT_REF_COLLISION", Message: "same filename in two markets resolves to same local path"}
	ErrConflictDepVersion       = &DomainError{Code: "CONFLICT_DEP_VERSION", Message: "two agents require same skill at incompatible versions"}
	ErrConflictDepDeleted       = &DomainError{Code: "CONFLICT_DEP_DELETED", Message: "a required skill was deleted from its market"}
	ErrConflictHookEventMatcher = &DomainError{Code: "CONFLICT_HOOK_EVENT_MATCHER", Message: "two markets provide hooks for the same (event, matcher) pair"}
)

// ErrUnknownTool is returned when a tool name given in config or --tools has
// no registered transformer.
var ErrUnknownTool = &DomainError{Code: "UNKNOWN_TOOL", Message: "unknown tool target"}

var (
	ErrInvalidHookSnippet     = &DomainError{Code: "INVALID_HOOK_SNIPPET", Message: "hook snippet must contain a non-empty 'event' string and a non-empty 'hooks' array"}
	ErrSettingsHooksMalformed = &DomainError{Code: "SETTINGS_HOOKS_MALFORMED", Message: "settings.json 'hooks' key has unexpected shape; refusing to install"}
)

var (
	ErrCloneExists        = &DomainError{Code: "CLONE_EXISTS", Message: "clone directory exists but market is not registered"}
	ErrSSHDisabled        = &DomainError{Code: "SSH_DISABLED", Message: "SSH is disabled. To enable:\n  mct config set ssh_enabled true\nor set MCT_SSH_ENABLED=true"}
	ErrMarketIncompatible = &DomainError{Code: "MARKET_INCOMPATIBLE", Message: "repository has no profiles, agents, or skills — not a compatible market"}
	ErrAlreadyInitialized = &DomainError{Code: "ALREADY_INITIALIZED", Message: "mct is already initialized"}
)

var (
	ErrLockContention  = &DomainError{Code: "LOCK_CONTENTION", Message: "another mct process holds the lock"}
	ErrStaleLock       = &DomainError{Code: "STALE_LOCK", Message: "lock file is stale (owning process no longer exists)"}
	ErrDriftDetected   = &DomainError{Code: "DRIFT_DETECTED", Message: "local file has been modified since last install"}
	ErrDependencyCycle = &DomainError{Code: "DEPENDENCY_CYCLE", Message: "circular dependency detected in requires_skills"}
	ErrPathTraversal   = &DomainError{Code: "PATH_TRAVERSAL", Message: "path contains traversal or is outside allowed directory"}
)
