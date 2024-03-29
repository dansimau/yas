package yas

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/dansimau/yas/pkg/log"
	"github.com/dansimau/yas/pkg/xexec"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/hashicorp/go-version"
	"github.com/heimdalr/dag"
	"github.com/sourcegraph/conc/pool"
	"github.com/xlab/treeprint"
)

var minimumRequiredGitVersion = version.Must(version.NewVersion("2.38"))

const yasStateFile = ".git/.yasstate"

type YAS struct {
	cfg  Config
	data *yasDatabase
	git  *gitexec.Repo
	repo *git.Repository
}

func New(cfg Config) (*YAS, error) {
	repo, err := git.PlainOpen(cfg.RepoDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to open git repo: %w", err)
	}

	data, err := loadData(path.Join(cfg.RepoDirectory, yasStateFile))
	if err != nil {
		return nil, fmt.Errorf("failed to load YAS state: %w", err)
	}

	yas := &YAS{
		cfg:  cfg,
		data: data,
		git:  gitexec.WithRepo(cfg.RepoDirectory),
		repo: repo,
	}

	if err := yas.validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return yas, nil
}

func NewFromRepository(repoDirectory string) (*YAS, error) {
	cfg, err := ReadConfig(repoDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	return New(*cfg)
}

func (yas *YAS) cleanupBranch(name string) error {
	yas.data.Branches.Remove(name)
	return yas.data.Save()
}

func (yas *YAS) Config() Config {
	return yas.cfg
}

func (yas *YAS) DeleteBranch(name string) error {
	branchExists, err := yas.git.BranchExists(name)
	if err != nil {
		return err
	}

	if !branchExists {
		if err := yas.cleanupBranch(name); err != nil {
			return err
		}

		return nil
	}

	currentBranchName, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return err
	}

	// Can't delete the branch while we're on it; switch to trunk
	if currentBranchName == name {
		if err := yas.git.Checkout(yas.cfg.TrunkBranch); err != nil {
			return fmt.Errorf("can't delete branch while on it; failed to checkout trunk: %w", err)
		}
	}

	if err := yas.git.DeleteBranch(name); err != nil {
		return err
	}

	if err := yas.cleanupBranch(name); err != nil {
		return err
	}

	return nil
}

func (yas *YAS) fetchGitHubPullRequestStatus(branchName string) (*PullRequestMetadata, error) {
	log.Info("Fetching PRs for branch", branchName)

	b, err := xexec.Command("gh", "pr", "list", "--head", branchName, "--state", "all", "--json", "id,state").WithStdout(nil).Output()
	if err != nil {
		return nil, err
	}

	data := []PullRequestMetadata{}
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, nil
	}

	return &data[0], nil
}

func (yas *YAS) graph() (*dag.DAG, error) {
	graph := dag.NewDAG()

	trunkBranch := yas.data.Branches.Get(yas.cfg.TrunkBranch)
	graph.AddVertexByID(yas.cfg.TrunkBranch, trunkBranch)

	for _, branch := range yas.data.Branches.ToSlice().WithParents() {
		graph.AddVertexByID(branch.Name, branch) // TODO handle errors
	}

	for _, branch := range yas.data.Branches.ToSlice().WithParents() {
		graph.AddEdge(branch.Parent, branch.Name) // TODO handle errors
	}

	return graph, nil
}

func (yas *YAS) Restack() error {
	graph, err := yas.graph()
	if err != nil {
		return err
	}

	currentBranchName, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return err
	}

	vertex, err := graph.GetVertex(currentBranchName)
	if err != nil {
		return err
	}

	descendents, _, err := graph.GetDescendantsGraph(vertex.(BranchMetadata).Name)
	if err != nil {
		return err
	}

	for _, v := range descendents.GetLeaves() {
		if err := yas.git.Rebase(yas.cfg.TrunkBranch, v.(BranchMetadata).Name); err != nil {
			return err
		}
	}

	return nil
}

func (yas *YAS) toTree(graph *dag.DAG, rootNode string) (treeprint.Tree, error) {
	tree := treeprint.NewWithRoot(rootNode)

	if err := addNodesFromGraph(tree, graph, rootNode); err != nil {
		return nil, err
	}

	return tree, nil
}

func (yas *YAS) List() error {
	graph, err := yas.graph()
	if err != nil {
		return fmt.Errorf("failed to get graph: %w", err)
	}

	tree, err := yas.toTree(graph, yas.cfg.TrunkBranch)
	if err != nil {
		return err
	}

	fmt.Print(tree.String())

	return nil
}

func (yas *YAS) SetParent(branchName, parentBranchName string) error {
	if branchName == "" {
		currentBranch, err := yas.git.GetCurrentBranchName()
		if err != nil {
			return err
		}

		branchName = currentBranch
	}

	if parentBranchName == "" {
		forkPoint, err := yas.git.GetForkPoint(branchName)
		if err != nil {
			return err // TODO return typed err
		}

		if forkPoint == "" {
			return errors.New("failed to autodetect parent branch (specify --parent)") // TODO type err
		}

		branchName, err := yas.git.GetLocalBranchNameForCommit(forkPoint + "^")
		if err != nil {
			return err // TODO return typed err
		}

		if branchName == "" {
			return errors.New("failed to autodetect parent branch (specify --parent)") // TODO type err
		}

		parentBranchName = branchName
	}

	branchMetdata := yas.data.Branches.Get(branchName)
	branchMetdata.Parent = parentBranchName
	yas.data.Branches.Set(branchName, branchMetdata)
	yas.data.Save()

	fmt.Printf("Set '%s' as parent of '%s'\n", parentBranchName, branchName)

	return nil
}

func (yas *YAS) Submit() error {
	currentBranch, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return err
	}

	if currentBranch == "HEAD" {
		return errors.New("cannot submit in detached HEAD state")
	}

	if err := yas.refreshRemoteStatus(currentBranch); err != nil {
		return err
	}

	if err := yas.git.Push(); err != nil {
		return fmt.Errorf("failed to push: %w", err)
	}

	if err := xexec.Command("gh", "pr", "create", "--draft", "--fill-first").Run(); err != nil {
		return err
	}

	return nil
}

func (yas *YAS) TrackedBranches() Branches {
	return yas.data.Branches.ToSlice()
}

// UpdateConfig sets the new config and writes it to the configuration file.
func (yas *YAS) UpdateConfig(cfg Config) (string, error) {
	yas.cfg = cfg
	return WriteConfig(cfg)
}

func (yas *YAS) UntrackedBranches() ([]string, error) {
	iter, err := yas.repo.Branches()
	if err != nil {
		return nil, err
	}

	branches := []string{}
	iter.ForEach(func(r *plumbing.Reference) error {
		name := string(r.Name().Short())
		if !yas.data.Branches.Exists(name) {
			branches = append(branches, name)
		}
		return nil
	})

	return branches, nil
}

func (yas *YAS) refreshRemoteStatus(name string) error {
	if strings.TrimSpace(name) == "" {
		panic("branch name cannot be empty")
	}

	pullRequestMetadata, err := yas.fetchGitHubPullRequestStatus(name)
	if err != nil {
		return err
	}

	if pullRequestMetadata == nil {
		pullRequestMetadata = &PullRequestMetadata{}
	}

	branchMetadata := yas.data.Branches.Get(name)

	branchMetadata.GitHubPullRequest = *pullRequestMetadata

	yas.data.Branches.Set(name, branchMetadata)

	if err := yas.data.Save(); err != nil {
		return err
	}

	return nil
}

func (yas *YAS) RefreshRemoteStatus(branchNames ...string) error {
	p := pool.New().WithMaxGoroutines(5).WithErrors().WithFirstError()
	for _, name := range branchNames {
		p.Go(func() error {
			return yas.refreshRemoteStatus(name)
		})
	}

	if err := p.Wait(); err != nil {
		return err
	}

	return nil
}

func (yas *YAS) UpdateTrunk() error {
	if err := yas.git.Checkout(yas.cfg.TrunkBranch); err != nil {
		return err
	}

	// Switch back to original branch
	defer yas.git.Checkout("-")

	return yas.git.Pull()
}

func (yas *YAS) validate() error {
	gitVersion, err := yas.git.GitVersion()
	if err != nil {
		return fmt.Errorf("unable to determine git version: %w", err)
	}

	if gitVersion.LessThan(minimumRequiredGitVersion) {
		path, _ := yas.git.GitPath()
		return fmt.Errorf("git version %s (%s) is less than the required version %s", gitVersion.String(), path, minimumRequiredGitVersion.String())
	}

	return nil
}
