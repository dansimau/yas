package yas

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/dansimau/yas/pkg/xexec"
)

// Merge merges the PR for the current branch using gh pr merge.
func (yas *YAS) Merge(branchName string, force bool) error {
	// Check if a restack is in progress
	if err := yas.errIfRestackInProgress(); err != nil {
		return err
	}

	// Get current branch if not specified
	if branchName == "" {
		currentBranch, err := yas.git.GetCurrentBranchName()
		if err != nil {
			return err
		}

		branchName = currentBranch
	}

	// Get branch metadata
	metadata := yas.data.Branches.Get(branchName)

	// Check that PR exists
	if metadata.GitHubPullRequest.ID == "" {
		return fmt.Errorf("branch '%s' does not have a PR", branchName)
	}

	// Check that branch is at top of stack (parent is trunk)
	if metadata.Parent != yas.cfg.TrunkBranch {
		return fmt.Errorf("branch must be at top of stack (parent must be %s, but is %s). Merge parent branches first", yas.cfg.TrunkBranch, metadata.Parent)
	}

	// Check that branch doesn't need restack
	needsRebase, err := yas.needsRebase(branchName, metadata.Parent)
	if err != nil {
		return fmt.Errorf("failed to check if branch needs restack: %w", err)
	}

	if needsRebase {
		return errors.New("branch needs restack. Run 'yas restack' first")
	}

	// Check that branch doesn't need submit
	needsSubmit, err := yas.needsSubmit(branchName)
	if err != nil {
		return fmt.Errorf("failed to check if branch needs submit: %w", err)
	}

	if needsSubmit {
		return errors.New("branch needs submit. Run 'yas submit' first")
	}

	branch, err := yas.git.WithBranchContext(branchName)
	if err != nil {
		return err
	}

	defer branch.RestoreOriginal()

	// Check CI and review status (unless --force)
	if !force {
		pr, err := yas.fetchPRStatusWithChecks(branchName)
		if err != nil {
			return fmt.Errorf("failed to fetch PR status: %w", err)
		}

		if pr == nil {
			return errors.New("failed to fetch PR status")
		}

		ciStatus := pr.GetOverallCIStatus()
		if ciStatus != "SUCCESS" {
			return fmt.Errorf("CI checks are not passing (status: %s). Use --force to override", ciStatus)
		}

		if pr.ReviewDecision != "APPROVED" {
			return fmt.Errorf("PR needs approval (review status: %s). Use --force to override", pr.ReviewDecision)
		}
	}

	// Get PR number
	prNumber := extractPRNumber(metadata.GitHubPullRequest.URL)

	// Fetch PR title and body
	title, body, err := yas.getPRTitleAndBody(prNumber)
	if err != nil {
		return fmt.Errorf("failed to get PR title and body: %w", err)
	}

	// Strip stack section from body
	body = removeStackSection(body)

	// Write merge message to file (use YAS config base to handle worktrees correctly)
	yasConfigBase, err := getYASConfigBase(yas.cfg.RepoDirectory)
	if err != nil {
		return fmt.Errorf("failed to get YAS config base: %w", err)
	}

	mergeFilePath := path.Join(yasConfigBase, ".yas", "yas-merge-msg")

	mergeMsg := title + "\n\n" + body
	if err := os.WriteFile(mergeFilePath, []byte(mergeMsg), 0o644); err != nil {
		return fmt.Errorf("failed to write merge message file: %w", err)
	}

	// Open editor
	if err := yas.openEditor(mergeFilePath); err != nil {
		return fmt.Errorf("editor failed: %w", err)
	}

	// Read back the merge message
	finalMsg, err := os.ReadFile(mergeFilePath)
	if err != nil {
		return fmt.Errorf("failed to read merge message file: %w", err)
	}

	// Parse title and body
	finalTitle, finalBody := yas.parseMergeMessage(string(finalMsg))

	// Check if merge message is empty
	if strings.TrimSpace(finalTitle) == "" {
		// Clean up merge message file
		if err := os.Remove(mergeFilePath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to delete merge message file: %v\n", err)
		}

		return errors.New("merge aborted: empty commit message")
	}

	// Execute gh pr merge
	//	if err := xexec.Command("gh", "pr", "merge", prNumber, "--squash", "--delete-branch", "--auto", "--subject", finalTitle, "--body", finalBody).Run(); err != nil {
	if err := xexec.Command("gh", "pr", "merge", prNumber, "--squash", "--auto", "--subject", finalTitle, "--body", finalBody).Run(); err != nil {
		return fmt.Errorf("failed to merge PR: %w", err)
	}

	// Delete merge message file
	if err := os.Remove(mergeFilePath); err != nil {
		// Don't fail if cleanup fails
		fmt.Fprintf(os.Stderr, "Warning: failed to delete merge message file: %v\n", err)
	}

	fmt.Printf("\nPR #%s merged successfully!\n", prNumber)
	fmt.Printf("\nRun 'yas sync' to restack locally.\n")

	return nil
}

// getPRTitleAndBody fetches the PR title and body using gh pr view.
func (yas *YAS) getPRTitleAndBody(prNumber string) (string, string, error) {
	output, err := xexec.Command("gh", "pr", "view", prNumber, "--json", "title,body", "-q", ".title + \"\n---SEPARATOR---\n\" + .body").WithStdout(nil).Output()
	if err != nil {
		return "", "", err
	}

	parts := strings.Split(string(output), "\n---SEPARATOR---\n")
	if len(parts) != 2 {
		return "", "", errors.New("unexpected gh pr view output format")
	}

	return parts[0], parts[1], nil
}

// openEditor opens the user's editor for the given file path.
func (yas *YAS) openEditor(filePath string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}

	cmd := xexec.Command(editor, filePath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// parseMergeMessage parses a merge message file into title and body.
// First line is the title, rest is the body (after skipping blank line).
func (yas *YAS) parseMergeMessage(msg string) (string, string) {
	lines := strings.Split(msg, "\n")
	if len(lines) == 0 {
		return "", ""
	}

	title := strings.TrimSpace(lines[0])

	// Find the start of the body (skip blank lines after title)
	bodyStart := 1
	for bodyStart < len(lines) && strings.TrimSpace(lines[bodyStart]) == "" {
		bodyStart++
	}

	if bodyStart >= len(lines) {
		return title, ""
	}

	body := strings.TrimSpace(strings.Join(lines[bodyStart:], "\n"))

	return title, body
}
