package gitadapter

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/repositories/gitrepo"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/utils/merkletrie"
)

var _ gitrepo.GitRepo = (*Adapter)(nil)

type Adapter struct {
	sshEnabled bool
	depth      int
	progress   io.Writer
	cloneLocks sync.Map
}

func New(opts ...Option) *Adapter {
	a := &Adapter{depth: 1, progress: io.Discard}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

type Option func(*Adapter)

func WithSSHEnabled(enabled bool) Option {
	return func(a *Adapter) {
		a.sshEnabled = enabled
	}
}

func WithDepth(depth int) Option {
	return func(a *Adapter) {
		a.depth = depth
	}
}

func (a *Adapter) cloneLock(clonePath string) *sync.RWMutex {
	key := filepath.Clean(clonePath)
	if abs, err := filepath.Abs(key); err == nil {
		key = abs
	}
	l, _ := a.cloneLocks.LoadOrStore(key, &sync.RWMutex{})
	return l.(*sync.RWMutex)
}

func (a *Adapter) lock(clonePath string) func() {
	l := a.cloneLock(clonePath)
	l.Lock()
	return l.Unlock
}

func (a *Adapter) rlock(clonePath string) func() {
	l := a.cloneLock(clonePath)
	l.RLock()
	return l.RUnlock
}

// WithProgress sends go-git transfer progress to w. Progress is discarded by
// default so it never pollutes machine-readable stdout (e.g. --json output).
func WithProgress(w io.Writer) Option {
	return func(a *Adapter) {
		if w == nil {
			w = io.Discard
		}
		a.progress = w
	}
}

func (a *Adapter) Clone(url, clonePath string) error {
	defer a.lock(clonePath)()
	if isSSHURL(url) && !a.sshEnabled {
		return domain.ErrSSHDisabled
	}
	_, err := git.PlainClone(clonePath, false, &git.CloneOptions{
		URL:      url,
		Auth:     resolveAuth(url),
		Depth:    a.depth,
		Progress: a.progress,
	})
	return err
}

// resolveBranchRef resolves a branch reference, trying the remote tracking ref
// first (refs/remotes/origin/<branch>) and falling back to the local branch ref
// (refs/heads/<branch>). go-git shallow clones may not always create remote
// tracking refs.
func resolveBranchRef(repo *git.Repository, branch string) (*plumbing.Reference, error) {
	ref, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", branch), true)
	if err != nil {
		ref, err = repo.Reference(plumbing.NewBranchReferenceName(branch), true)
		if err != nil {
			return nil, fmt.Errorf("resolve ref for branch %q: %w", branch, err)
		}
	}
	return ref, nil
}

// DefaultBranch reports the branch the clone tracks. HEAD is detached after
// Fetch, so it falls back to origin/HEAD, then to the only remote or local
// branch, then to probing "main" and "master".
func (a *Adapter) DefaultBranch(clonePath string) (string, error) {
	defer a.rlock(clonePath)()
	repo, err := git.PlainOpen(clonePath)
	if err != nil {
		return "", fmt.Errorf("open repo: %w", err)
	}

	ref, err := repo.Reference(plumbing.HEAD, false)
	if err != nil {
		return "", fmt.Errorf("resolve HEAD: %w", err)
	}
	if ref.Type() == plumbing.SymbolicReference {
		return ref.Target().Short(), nil
	}

	originHead, err := repo.Reference(plumbing.NewRemoteHEADReferenceName("origin"), false)
	if err == nil && originHead.Type() == plumbing.SymbolicReference {
		return strings.TrimPrefix(originHead.Target().Short(), "origin/"), nil
	}

	remote, local := branchNames(repo)
	if len(remote) == 1 {
		return remote[0], nil
	}
	if len(local) == 1 {
		return local[0], nil
	}
	for _, candidate := range []string{"main", "master"} {
		if _, err := resolveBranchRef(repo, candidate); err == nil {
			return candidate, nil
		}
	}
	return "", errors.New("cannot determine default branch of " + clonePath)
}

func branchNames(repo *git.Repository) (remote, local []string) {
	refs, err := repo.References()
	if err != nil {
		return nil, nil
	}
	_ = refs.ForEach(func(r *plumbing.Reference) error {
		name := r.Name()
		switch {
		case name.IsRemote() && strings.HasPrefix(name.String(), "refs/remotes/origin/") && name.Short() != "origin/HEAD":
			remote = append(remote, strings.TrimPrefix(name.Short(), "origin/"))
		case name.IsBranch():
			local = append(local, name.Short())
		}
		return nil
	})
	return remote, local
}

func (a *Adapter) Fetch(clonePath, branch string) (string, error) {
	defer a.lock(clonePath)()
	repo, err := git.PlainOpen(clonePath)
	if err != nil {
		return "", fmt.Errorf("open repo: %w", err)
	}

	if !a.sshEnabled {
		if remote, rErr := repo.Remote("origin"); rErr == nil && remote != nil {
			if urls := remote.Config().URLs; len(urls) > 0 && isSSHURL(urls[0]) {
				return "", domain.ErrSSHDisabled
			}
		}
	}

	err = repo.Fetch(&git.FetchOptions{
		Auth:     resolveAuthFromRepo(repo),
		Depth:    a.depth,
		Prune:    true,
		Force:    false,
		Progress: a.progress,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return "", fmt.Errorf("fetch: %w", err)
	}

	ref, err := resolveBranchRef(repo, branch)
	if err != nil {
		return "", err
	}

	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("worktree: %w", err)
	}

	newHash := ref.Hash()

	err = wt.Checkout(&git.CheckoutOptions{
		Hash:  newHash,
		Force: true,
	})
	if err != nil {
		return "", fmt.Errorf("checkout: %w", err)
	}

	// Explicitly advance refs/heads/<branch> to the fetched SHA. A detached-HEAD
	// checkout does not update the local branch ref, so RemoteHEAD() would fall
	// back to the stale ref and report no update available.
	_ = repo.Storer.SetReference(
		plumbing.NewHashReference(plumbing.NewBranchReferenceName(branch), newHash),
	)

	return newHash.String(), nil
}

func (a *Adapter) DiffSinceCommit(clonePath, branch, oldSHA string) ([]domain.FileDiff, error) {
	defer a.rlock(clonePath)()
	repo, err := git.PlainOpen(clonePath)
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}

	oldHash := plumbing.NewHash(oldSHA)
	oldCommit, err := repo.CommitObject(oldHash)
	if err != nil {
		return nil, fmt.Errorf("resolve old commit: %w", err)
	}

	remoteRef, err := resolveBranchRef(repo, branch)
	if err != nil {
		return nil, err
	}

	newCommit, err := repo.CommitObject(remoteRef.Hash())
	if err != nil {
		return nil, fmt.Errorf("resolve new commit: %w", err)
	}

	oldTree, err := oldCommit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get old tree: %w", err)
	}

	newTree, err := newCommit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get new tree: %w", err)
	}

	changes, err := oldTree.Diff(newTree)
	if err != nil {
		return nil, fmt.Errorf("diff trees: %w", err)
	}

	var diffs []domain.FileDiff
	for _, change := range changes {
		action, err := change.Action()
		if err != nil {
			return nil, fmt.Errorf("get change action: %w", err)
		}

		var domainAction domain.DiffAction
		switch action {
		case merkletrie.Insert:
			domainAction = domain.DiffInsert
		case merkletrie.Modify:
			domainAction = domain.DiffModify
		case merkletrie.Delete:
			domainAction = domain.DiffDelete
		default:
			continue
		}

		path := change.To.Name
		if path == "" {
			path = change.From.Name
		}

		if !isAgentOrSkillPath(path) {
			continue
		}

		diffs = append(diffs, domain.FileDiff{
			Action: domainAction,
			From:   change.From.Name,
			To:     change.To.Name,
		})
	}

	return diffs, nil
}

func (a *Adapter) ReadFileAtRef(clonePath, branch, filePath, commitSHA string) ([]byte, error) {
	defer a.rlock(clonePath)()
	repo, err := git.PlainOpen(clonePath)
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}

	var hash plumbing.Hash
	if commitSHA == "HEAD" || commitSHA == "" {
		ref, err := resolveBranchRef(repo, branch)
		if err != nil {
			return nil, err
		}
		hash = ref.Hash()
	} else {
		hash = plumbing.NewHash(commitSHA)
	}

	commit, err := repo.CommitObject(hash)
	if err != nil {
		return nil, fmt.Errorf("resolve commit: %w", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get tree: %w", err)
	}

	file, err := tree.File(filePath)
	if err != nil {
		return nil, fmt.Errorf("get file: %w", err)
	}

	contents, err := file.Contents()
	if err != nil {
		return nil, fmt.Errorf("read file contents: %w", err)
	}

	return []byte(contents), nil
}

func (a *Adapter) FileVersion(clonePath, filePath string) (domain.MctVersion, error) {
	defer a.rlock(clonePath)()
	repo, err := git.PlainOpen(clonePath)
	if err != nil {
		return "", errors.Join(errors.New("open repo"), err)
	}

	head, err := repo.Head()
	if err != nil {
		return "", errors.Join(errors.New("resolve HEAD"), err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return "", errors.Join(errors.New("resolve HEAD commit"), err)
	}

	file, err := commit.File(filePath)
	if errors.Is(err, object.ErrFileNotFound) {
		return "", gitrepo.ErrFileNotFound
	}
	if err != nil {
		return "", errors.Join(errors.New("read file at HEAD"), err)
	}

	for commit.NumParents() > 0 {
		parent, err := commit.Parent(0)
		if err != nil {
			break
		}
		pf, err := parent.File(filePath)
		if err != nil || pf.Hash != file.Hash {
			break
		}
		commit = parent
	}

	return commitVersion(commit), nil
}

func commitVersion(c *object.Commit) domain.MctVersion {
	return domain.MctVersion(c.Hash.String()[:7] + "\u00b7" + c.Author.When.Format("2006-01-02"))
}

func (a *Adapter) RemoteHEAD(clonePath, branch string) (string, error) {
	defer a.rlock(clonePath)()
	repo, err := git.PlainOpen(clonePath)
	if err != nil {
		return "", fmt.Errorf("open repo: %w", err)
	}

	ref, err := resolveBranchRef(repo, branch)
	if err != nil {
		return "", err
	}

	return ref.Hash().String(), nil
}

func (a *Adapter) ListFiles(clonePath, branch string) ([]string, error) {
	defer a.rlock(clonePath)()
	repo, err := git.PlainOpen(clonePath)
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}

	ref, err := resolveBranchRef(repo, branch)
	if err != nil {
		return nil, err
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("resolve commit: %w", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get tree: %w", err)
	}

	var paths []string
	err = tree.Files().ForEach(func(f *object.File) error {
		if strings.HasSuffix(f.Name, ".md") && (isAgentOrSkillPath(f.Name) || isReadmePath(f.Name)) {
			paths = append(paths, f.Name)
		} else if isHookPath(f.Name) {
			paths = append(paths, f.Name)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk files: %w", err)
	}

	return paths, nil
}

func (a *Adapter) ReadMarketFiles(clonePath, branch string) ([]gitrepo.MarketFile, error) {
	defer a.rlock(clonePath)()
	repo, err := git.PlainOpen(clonePath)
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}

	ref, err := resolveBranchRef(repo, branch)
	if err != nil {
		return nil, err
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("resolve commit: %w", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get tree: %w", err)
	}

	// Collect all matching files and read their content in one tree walk.
	var paths []string
	contentByPath := make(map[string][]byte)
	err = tree.Files().ForEach(func(f *object.File) error {
		md := strings.HasSuffix(f.Name, ".md")
		hook := isHookPath(f.Name)
		if !md && !hook {
			return nil
		}
		if md && !isAgentOrSkillPath(f.Name) && !isReadmePath(f.Name) {
			return nil
		}
		paths = append(paths, f.Name)
		c, err := f.Contents()
		if err != nil {
			return nil
		}
		contentByPath[f.Name] = []byte(c)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk files: %w", err)
	}

	versionByPath := batchFileVersions(commit, paths)

	result := make([]gitrepo.MarketFile, 0, len(paths))
	for _, p := range paths {
		result = append(result, gitrepo.MarketFile{
			Path:    p,
			Content: contentByPath[p],
			Version: versionByPath[p],
		})
	}
	return result, nil
}

func (a *Adapter) ListDirFiles(clonePath, branch, dirPrefix string) ([]string, error) {
	defer a.rlock(clonePath)()
	repo, err := git.PlainOpen(clonePath)
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}

	ref, err := resolveBranchRef(repo, branch)
	if err != nil {
		return nil, err
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("resolve commit: %w", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get tree: %w", err)
	}

	prefix := dirPrefix
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	var paths []string
	err = tree.Files().ForEach(func(f *object.File) error {
		if strings.HasPrefix(f.Name, prefix) {
			paths = append(paths, f.Name)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk files: %w", err)
	}
	return paths, nil
}

// batchFileVersions walks the first-parent history from tip once and returns
// the last-modified version for each path. Paths unchanged down to the root or
// the shallow boundary are attributed to the oldest reachable commit.
func batchFileVersions(tip *object.Commit, paths []string) map[string]domain.MctVersion {
	versions := make(map[string]domain.MctVersion, len(paths))
	remaining := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		remaining[p] = struct{}{}
	}

	commit := tip
	for len(remaining) > 0 && commit.NumParents() > 0 {
		parent, err := commit.Parent(0)
		if err != nil {
			break
		}
		curTree, err := commit.Tree()
		if err != nil {
			break
		}
		parentTree, err := parent.Tree()
		if err != nil {
			break
		}
		changes, err := parentTree.Diff(curTree)
		if err != nil {
			break
		}
		for _, change := range changes {
			for _, name := range []string{change.From.Name, change.To.Name} {
				if _, need := remaining[name]; need {
					versions[name] = commitVersion(commit)
					delete(remaining, name)
				}
			}
		}
		commit = parent
	}

	for p := range remaining {
		versions[p] = commitVersion(commit)
	}
	return versions
}

func (a *Adapter) IsValidRepo(clonePath string) bool {
	defer a.rlock(clonePath)()
	_, err := git.PlainOpen(clonePath)
	return err == nil
}

func (a *Adapter) ValidateRemote(url string) error {
	if isSSHURL(url) && !a.sshEnabled {
		return domain.ErrSSHDisabled
	}
	auth := resolveAuth(url)
	ep, err := transport.NewEndpoint(url)
	if err != nil {
		return err
	}
	cli, err := client.NewClient(ep)
	if err != nil {
		return err
	}
	sess, err := cli.NewUploadPackSession(ep, auth)
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()
	_, err = sess.AdvertisedReferences()
	return err
}
