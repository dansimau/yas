package yas

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Stack is a GitHub stacked pull request stack, as returned by the Stacks REST
// API. PullRequests are ordered from the bottom of the stack (closest to the
// base branch) to the top.
type Stack struct {
	ID           int                `json:"id"`
	Number       int                `json:"number"`
	Base         StackRef           `json:"base"`
	Open         bool               `json:"open"`
	PullRequests []StackPullRequest `json:"pull_requests"`
}

// StackPullRequest is the minimal pull request representation the Stacks API
// embeds in a stack.
type StackPullRequest struct {
	Number   int      `json:"number"`
	State    string   `json:"state"`
	Draft    bool     `json:"draft"`
	MergedAt *string  `json:"merged_at"`
	Head     StackRef `json:"head"`
}

// StackRef is a git ref as reported by the Stacks API.
type StackRef struct {
	Ref string `json:"ref"`
	SHA string `json:"sha,omitempty"`
}

// IsMerged reports whether the pull request has been merged. Merged PRs stay in
// a stack forever, so they are ignored when comparing a stack to a lineage.
func (pr StackPullRequest) IsMerged() bool {
	return pr.MergedAt != nil && *pr.MergedAt != ""
}

// openPRNumbers returns the numbers of the stack's unmerged PRs, bottom to top.
func (s *Stack) openPRNumbers() []int {
	numbers := []int{}

	for _, pr := range s.PullRequests {
		if !pr.IsMerged() {
			numbers = append(numbers, pr.Number)
		}
	}

	return numbers
}

// mergedPRNumbers returns the numbers of the stack's merged PRs.
func (s *Stack) mergedPRNumbers() []int {
	numbers := []int{}

	for _, pr := range s.PullRequests {
		if pr.IsMerged() {
			numbers = append(numbers, pr.Number)
		}
	}

	return numbers
}

// errStacksUnavailable is returned when looking up a stack responds with 404,
// which GitHub uses to signal that stacked pull requests are not enabled for
// the repository.
var errStacksUnavailable = errors.New("stacked PRs are not available for this repository")

// ghHTTPError is an HTTP error reported by `gh api`.
type ghHTTPError struct {
	Status  int
	Message string
}

func (e *ghHTTPError) Error() string {
	return e.Message
}

// ghHTTPStatusPattern matches the status `gh api` appends to its error output,
// e.g. "gh: Not Found (HTTP 404)".
var ghHTTPStatusPattern = regexp.MustCompile(`\(HTTP (\d{3})\)`)

// ghAPI runs `gh api` inside the repository with the given arguments and
// returns the response body. HTTP failures are returned as *ghHTTPError.
// Nothing is written to the terminal.
func (yas *YAS) ghAPI(args ...string) ([]byte, error) {
	out, err := yas.gh(append([]string{"api"}, args...)...).
		WithStdout(nil).
		WithStderr(nil).
		Output()
	if err == nil {
		return out, nil
	}

	exitErr := &exec.ExitError{}
	if !errors.As(err, &exitErr) {
		return nil, err
	}

	stderr := strings.TrimSpace(string(exitErr.Stderr))

	if match := ghHTTPStatusPattern.FindStringSubmatch(stderr); match != nil {
		status, _ := strconv.Atoi(match[1])

		return nil, &ghHTTPError{Status: status, Message: stderr}
	}

	if stderr != "" {
		return nil, fmt.Errorf("%w: %s", err, stderr)
	}

	return nil, err
}

// stacksPath is the Stacks API path for the current repository. `gh api` fills
// in the {owner} and {repo} placeholders from the repository in the working
// directory.
const stacksPath = "repos/{owner}/{repo}/stacks"

// findStackForPR returns the stack containing the given PR, or nil if the PR is
// not part of a stack. A 404 here means stacked PRs are not enabled for the
// repository; the lookup always runs before any stack is changed, so this is
// the only place a 404 is read that way.
func (yas *YAS) findStackForPR(prNumber int) (*Stack, error) {
	out, err := yas.ghAPI(fmt.Sprintf("%s?pull_request=%d", stacksPath, prNumber))

	httpErr := &ghHTTPError{}
	if errors.As(err, &httpErr) && httpErr.Status == http.StatusNotFound {
		return nil, errStacksUnavailable
	}

	if err != nil {
		return nil, err
	}

	var stacks []Stack
	if err := json.Unmarshal(out, &stacks); err != nil {
		return nil, fmt.Errorf("failed to parse stacks response: %w", err)
	}

	if len(stacks) == 0 {
		return nil, nil
	}

	return &stacks[0], nil
}

// pullRequestFields renders PR numbers as `gh api` typed array fields, which
// are sent as a JSON array of integers.
func pullRequestFields(prNumbers []int) []string {
	fields := []string{}
	for _, n := range prNumbers {
		fields = append(fields, "-F", fmt.Sprintf("pull_requests[]=%d", n))
	}

	return fields
}

// createStack creates a stack from PR numbers ordered bottom to top.
func (yas *YAS) createStack(prNumbers []int) (*Stack, error) {
	args := append([]string{"--method", "POST", stacksPath}, pullRequestFields(prNumbers)...)

	out, err := yas.ghAPI(args...)
	if err != nil {
		return nil, err
	}

	var stack Stack
	if err := json.Unmarshal(out, &stack); err != nil {
		return nil, fmt.Errorf("failed to parse stack response: %w", err)
	}

	return &stack, nil
}

// addToStack appends PRs to the top of an existing stack.
func (yas *YAS) addToStack(stackNumber int, prNumbers []int) error {
	args := append([]string{"--method", "POST", fmt.Sprintf("%s/%d/add", stacksPath, stackNumber)}, pullRequestFields(prNumbers)...)

	_, err := yas.ghAPI(args...)

	return err
}

// unstack removes the unmerged PRs from a stack. It returns the remaining stack,
// or nil when the stack was dissolved because nothing remained.
func (yas *YAS) unstack(stackNumber int) (*Stack, error) {
	out, err := yas.ghAPI("--method", "POST", fmt.Sprintf("%s/%d/unstack", stacksPath, stackNumber))
	if err != nil {
		return nil, err
	}

	if len(strings.TrimSpace(string(out))) == 0 {
		return nil, nil
	}

	var stack Stack
	if err := json.Unmarshal(out, &stack); err != nil {
		return nil, fmt.Errorf("failed to parse stack response: %w", err)
	}

	return &stack, nil
}

// prNumber returns the number of the branch's PR, or false if it has none.
func prNumber(metadata BranchMetadata) (int, bool) {
	if metadata.GitHubPullRequest.ID == "" {
		return 0, false
	}

	n, err := strconv.Atoi(extractPRNumber(metadata.GitHubPullRequest.URL))
	if err != nil || n <= 0 {
		return 0, false
	}

	return n, true
}

// lineagePRNumbers returns the PR numbers of the branches from trunk up to and
// including branch, bottom to top. A stack needs every layer to be a PR, so if
// any branch in the lineage has none its name is returned as missing.
func (yas *YAS) lineagePRNumbers(branch string) (numbers []int, missing string, err error) {
	lineage, err := yas.lineage(branch)
	if err != nil {
		return nil, "", err
	}

	for _, name := range lineage {
		n, ok := prNumber(yas.data.Branches.Get(name))
		if !ok {
			return nil, name, nil
		}

		numbers = append(numbers, n)
	}

	return numbers, "", nil
}

// linkOutcome describes the result of linking one lineage.
type linkOutcome struct {
	// Message describes what happened, for the user.
	Message string
	// Attempted is false when the lineage was never a candidate for a stack
	// (fewer than two PRs, or a branch without a PR), so GitHub was not asked.
	Attempted bool
}

// lineageStacks returns the distinct stacks that the given PRs belong to,
// bottom to top. Every PR is looked up unless a stack already found contains
// it, so a lineage whose bottom PR is not stacked yet still finds the stack its
// higher PRs are in.
func (yas *YAS) lineageStacks(prNumbers []int) ([]*Stack, error) {
	stacks := []*Stack{}
	covered := map[int]bool{}

	for _, n := range prNumbers {
		if covered[n] {
			continue
		}

		stack, err := yas.findStackForPR(n)
		if err != nil {
			return nil, err
		}

		if stack == nil {
			continue
		}

		stacks = append(stacks, stack)

		for _, pr := range stack.PullRequests {
			covered[pr.Number] = true
		}
	}

	return stacks, nil
}

// describeStacks renders the open PRs of each stack, e.g.
// "stack #7 contains #41, #42; stack #8 contains #43".
func describeStacks(stacks []*Stack) string {
	parts := make([]string, 0, len(stacks))
	for _, stack := range stacks {
		parts = append(parts, fmt.Sprintf("stack #%d contains %s", stack.Number, formatPRNumbers(stack.openPRNumbers())))
	}

	return strings.Join(parts, "; ")
}

// linkLineage makes sure the lineage trunk→branch is linked as a stack on
// GitHub. Merged PRs are ignored on both sides: yas deletes merged branches, so
// the local lineage starts above them, while GitHub keeps them in the stack.
// The stack is created if none of the lineage's PRs is stacked yet and extended
// if the lineage grew; any other difference is reported without changing
// anything.
func (yas *YAS) linkLineage(branch string, dryRun bool) (linkOutcome, error) {
	numbers, missing, err := yas.lineagePRNumbers(branch)
	if err != nil {
		return linkOutcome{}, err
	}

	if missing != "" {
		return linkOutcome{Message: missing + " has no PR; nothing to link"}, nil
	}

	if len(numbers) < 2 {
		return linkOutcome{Message: "nothing to link (a stack needs at least two PRs)"}, nil
	}

	stacks, err := yas.lineageStacks(numbers)
	if err != nil {
		return linkOutcome{}, err
	}

	if len(stacks) == 0 {
		if dryRun {
			return attempted("would create stack with %s", formatPRNumbers(numbers)), nil
		}

		created, err := yas.createStack(numbers)
		if err != nil {
			return linkOutcome{}, fmt.Errorf("failed to create stack: %w", err)
		}

		return attempted("linked %s as stack #%d", formatPRNumbers(numbers), created.Number), nil
	}

	merged := []int{}
	for _, stack := range stacks {
		merged = append(merged, stack.mergedPRNumbers()...)
	}

	desired := slices.DeleteFunc(slices.Clone(numbers), func(n int) bool {
		return slices.Contains(merged, n)
	})

	mismatch := attempted("this lineage (%s) does not match GitHub: %s; run 'yas link --unlink' and then 'yas link' to relink it",
		formatPRNumbers(desired), describeStacks(stacks))

	if len(stacks) > 1 {
		return mismatch, nil
	}

	stack := stacks[0]
	open := stack.openPRNumbers()

	switch {
	case isPrefix(desired, open):
		return attempted("already linked (stack #%d)", stack.Number), nil

	case isPrefix(open, desired):
		delta := desired[len(open):]

		if dryRun {
			return attempted("would add %s to stack #%d", formatPRNumbers(delta), stack.Number), nil
		}

		if err := yas.addToStack(stack.Number, delta); err != nil {
			return linkOutcome{}, fmt.Errorf("failed to add to stack #%d: %w", stack.Number, err)
		}

		return attempted("added %s to stack #%d", formatPRNumbers(delta), stack.Number), nil

	default:
		return mismatch, nil
	}
}

func attempted(format string, args ...any) linkOutcome {
	return linkOutcome{Message: fmt.Sprintf(format, args...), Attempted: true}
}

// isPrefix reports whether a is a (possibly equal) prefix of b.
func isPrefix(a, b []int) bool {
	return len(a) <= len(b) && slices.Equal(a, b[:len(a)])
}

func formatPRNumbers(numbers []int) string {
	parts := make([]string, 0, len(numbers))
	for _, n := range numbers {
		parts = append(parts, fmt.Sprintf("#%d", n))
	}

	return strings.Join(parts, ", ")
}

// Link links the lineage trunk→current branch as a stack on GitHub.
func (yas *YAS) Link(dryRun bool) error {
	currentBranch, err := yas.currentBranchForLinking()
	if err != nil {
		return err
	}

	outcome, err := yas.linkLineage(currentBranch, dryRun)
	if errors.Is(err, errStacksUnavailable) {
		fmt.Println(err.Error())

		return nil
	}

	if err != nil {
		return err
	}

	fmt.Println(outcome.Message)

	return nil
}

// Unlink is the reverse of Link: it removes the unmerged PRs from every stack
// that any PR in the lineage trunk→current branch belongs to, so that the
// lineage can be linked again from scratch.
func (yas *YAS) Unlink(dryRun bool) error {
	currentBranch, err := yas.currentBranchForLinking()
	if err != nil {
		return err
	}

	lineage, err := yas.lineage(currentBranch)
	if err != nil {
		return err
	}

	numbers := []int{}

	for _, name := range lineage {
		if n, ok := prNumber(yas.data.Branches.Get(name)); ok {
			numbers = append(numbers, n)
		}
	}

	if len(numbers) == 0 {
		return fmt.Errorf("no branch in the lineage of '%s' has a PR", currentBranch)
	}

	stacks, err := yas.lineageStacks(numbers)
	if errors.Is(err, errStacksUnavailable) {
		fmt.Println(err.Error())

		return nil
	}

	if err != nil {
		return err
	}

	if len(stacks) == 0 {
		fmt.Printf("nothing to unlink: no PR in the lineage of %s is part of a stack\n", currentBranch)

		return nil
	}

	for _, stack := range stacks {
		if dryRun {
			fmt.Printf("would unstack stack #%d (%s)\n", stack.Number, formatPRNumbers(stack.openPRNumbers()))

			continue
		}

		remaining, err := yas.unstack(stack.Number)
		if err != nil {
			return fmt.Errorf("failed to unstack stack #%d: %w", stack.Number, err)
		}

		if remaining == nil {
			fmt.Printf("dissolved stack #%d\n", stack.Number)

			continue
		}

		fmt.Printf("removed open PRs from stack #%d; merged PRs remain (%s)\n", stack.Number, formatPRNumbers(remaining.mergedPRNumbers()))
	}

	return nil
}

func (yas *YAS) currentBranchForLinking() (string, error) {
	currentBranch, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return "", err
	}

	if currentBranch == "HEAD" {
		return "", errors.New("cannot link in detached HEAD state")
	}

	return currentBranch, nil
}

// forkPoint returns a branch with more than one child among the given branches,
// and those children, or "" if the branches form a single line. The parent need
// not be one of the branches itself: submit --outdated leaves out parents that
// are up to date, but two siblings still cannot share one stack. Trunk is not a
// fork point, since stacks growing from trunk are independent of each other.
func (yas *YAS) forkPoint(branches []string) (parent string, children []string) {
	childrenOf := map[string][]string{}

	for _, name := range branches {
		p := yas.parentBranchName(yas.data.Branches.Get(name))
		if p == "" || p == yas.cfg.TrunkBranch {
			continue
		}

		childrenOf[p] = append(childrenOf[p], name)
	}

	for _, p := range slices.Sorted(maps.Keys(childrenOf)) {
		if len(childrenOf[p]) > 1 {
			// Branches arrive in submission (completion) order; keep the
			// report stable.
			slices.Sort(childrenOf[p])

			return p, childrenOf[p]
		}
	}

	return "", nil
}

// linkSubmittedBranches links the lineage of every submitted branch, one after
// another: a later branch's lookup has to see the stack an earlier one created.
// Linking never fails a submit; problems are reported and skipped.
//
// A GitHub stack is a single line of PRs, so when the submitted branches fork
// only one side could be linked and which one would depend on iteration order.
// Nothing is linked in that case; the user picks a side with `yas link`.
func (yas *YAS) linkSubmittedBranches(branches []string) {
	if parent, children := yas.forkPoint(branches); parent != "" {
		fmt.Printf("\nstack forks at %s (%s); GitHub stacks are linear, so nothing was linked. Run 'yas link' from the branch whose lineage should be linked.\n",
			parent, strings.Join(children, ", "))

		return
	}

	headerShown := false

	for _, branchName := range branches {
		outcome, err := yas.linkLineage(branchName, false)
		if errors.Is(err, errStacksUnavailable) {
			fmt.Printf("\n%s; skipping stack linking\n", err)

			return
		}

		if err != nil {
			fmt.Printf("\nWarning: failed to link stack for %s: %v\n", branchName, err)

			continue
		}

		if !outcome.Attempted {
			continue
		}

		if !headerShown {
			fmt.Println("\nLinking stacks:")

			headerShown = true
		}

		fmt.Printf("  %s: %s\n", branchName, outcome.Message)
	}
}
