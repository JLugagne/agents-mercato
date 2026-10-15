package gitrepo

import (
	"errors"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

// ErrFileNotFound is returned when a path does not exist in the tree at the
// requested revision. Callers rely on it to tell an upstream removal apart
// from a transient git failure before deleting anything locally.
var ErrFileNotFound = errors.New("file not found in repository tree")

// MarketFile holds a file's content and version, as read from a market repo in a single pass.
type MarketFile struct {
	Path    string
	Content []byte
	Version domain.MctVersion
}

type GitRepo interface {
	Clone(url, clonePath string) error
	DefaultBranch(clonePath string) (string, error)
	Fetch(clonePath, branch string) (newHeadSHA string, err error)
	DiffSinceCommit(clonePath, branch, oldSHA string) ([]domain.FileDiff, error)
	ReadFileAtRef(clonePath, branch, filePath, commitSHA string) ([]byte, error)
	// FileVersion returns the version of the last commit reachable from HEAD
	// that touched filePath. It returns ErrFileNotFound when the file is absent
	// from HEAD. On shallow clones the walk stops at the shallow boundary.
	FileVersion(clonePath, filePath string) (domain.MctVersion, error)
	RemoteHEAD(clonePath, branch string) (string, error)
	ListFiles(clonePath, branch string) ([]string, error)
	IsValidRepo(clonePath string) bool
	ValidateRemote(url string) error

	// ReadMarketFiles opens the repo once and returns all matching files with
	// their content and last-modified version in a single pass.
	ReadMarketFiles(clonePath, branch string) ([]MarketFile, error)

	// ListDirFiles lists all files under a directory prefix in the git tree.
	// Unlike ListFiles, this includes non-.md files and does not filter by
	// agent/skill path patterns.
	ListDirFiles(clonePath, branch, dirPrefix string) ([]string, error)
}
