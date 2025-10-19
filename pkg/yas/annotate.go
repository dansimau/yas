package yas

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dansimau/yas/pkg/progress"
	"github.com/dansimau/yas/pkg/xexec"
)

func (yas *YAS) Annotate() error {
	currentBranch, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return err
	}

	if currentBranch == "HEAD" {
		return errors.New("cannot annotate in detached HEAD state")
	}

	if err := yas.annotateBranch(currentBranch); err != nil {
		return err
	}

	// Print success message for single-branch annotation
	fmt.Printf("Updated PR for %s with stack information\n", currentBranch)
	return nil
}

// AnnotateAll annotates all branches that have PRs with stack information.
func (yas *YAS) AnnotateAll() error {
	branchesWithPRs := yas.data.Branches.ToSlice().WithPRs()

	if len(branchesWithPRs) == 0 {
		fmt.Println("No branches with PRs found")
		return nil
	}

	// Create progress runner
	runner := progress.New(5, fmt.Sprintf("Annotating %d branch(es):", len(branchesWithPRs)))

	// Add annotation tasks
	for _, branch := range branchesWithPRs {
		branchName := branch.Name // capture for closure
		runner.Add(branchName, func() error {
			return yas.annotateBranch(branchName)
		})
	}

	// Execute with progress display and print final results
	return runner.Start(true)
}

func (yas *YAS) annotateBranch(branchName string) error {
	// Get the branch metadata
	metadata := yas.data.Branches.Get(branchName)
	if metadata.GitHubPullRequest.ID == "" {
		return fmt.Errorf("branch '%s' does not have a PR", branchName)
	}

	// Count PRs in the stack
	prCount, err := yas.countPRsInStack(branchName)
	if err != nil {
		return fmt.Errorf("failed to count PRs in stack: %w", err)
	}

	// Get the current PR body
	prNumber := extractPRNumber(metadata.GitHubPullRequest.URL)

	currentBody, err := yas.getPRBody(prNumber)
	if err != nil {
		return fmt.Errorf("failed to get PR body: %w", err)
	}

	var newBody string
	if prCount <= 1 {
		// Remove stack section if it exists
		newBody = removeStackSection(currentBody)
	} else {
		// Build the stack visualization
		stackVisualization, err := yas.buildStackVisualization(branchName)
		if err != nil {
			return fmt.Errorf("failed to build stack visualization: %w", err)
		}
		// Update the body with the stack section
		newBody = updatePRBodyWithStack(currentBody, stackVisualization)
	}

	// Update the PR
	if err := yas.updatePRBody(prNumber, newBody); err != nil {
		return fmt.Errorf("failed to update PR: %w", err)
	}

	return nil
}

func (yas *YAS) countPRsInStack(currentBranch string) (int, error) {
	// Get the graph
	graph, err := yas.graph()
	if err != nil {
		return 0, err
	}

	count := 0

	// Count ancestors (walking up to trunk)
	branch := currentBranch
	for {
		metadata := yas.data.Branches.Get(branch)
		if metadata.GitHubPullRequest.ID != "" {
			count++
		}

		if metadata.Parent == "" || metadata.Parent == yas.cfg.TrunkBranch {
			break
		}

		branch = metadata.Parent
	}

	// Count descendants (walking down from current)
	descendants := []string{}
	if err := yas.collectDescendants(graph, currentBranch, &descendants); err != nil {
		return 0, err
	}

	for _, descendantBranch := range descendants {
		metadata := yas.data.Branches.Get(descendantBranch)
		if metadata.GitHubPullRequest.ID != "" {
			count++
		}
	}

	return count, nil
}

func (yas *YAS) buildStackVisualization(currentBranch string) (string, error) {
	// Get the graph
	graph, err := yas.graph()
	if err != nil {
		return "", err
	}

	// Get ancestors (walking up to trunk)
	ancestors := []string{}

	branch := currentBranch
	for {
		metadata := yas.data.Branches.Get(branch)
		if metadata.Parent == "" || metadata.Parent == yas.cfg.TrunkBranch {
			break
		}

		ancestors = append([]string{metadata.Parent}, ancestors...)
		branch = metadata.Parent
	}

	// Get descendants (walking down from current)
	descendants := []string{}
	if err := yas.collectDescendants(graph, currentBranch, &descendants); err != nil {
		return "", err
	}

	// Build the visualization
	var lines []string

	indent := 0

	// Add ancestors
	for _, ancestorBranch := range ancestors {
		lines = append(lines, yas.formatStackLine(ancestorBranch, indent, false))
		indent++
	}

	// Add current branch
	lines = append(lines, yas.formatStackLine(currentBranch, indent, true))
	indent++

	// Add descendants
	for _, descendantBranch := range descendants {
		lines = append(lines, yas.formatStackLine(descendantBranch, indent, false))
		indent++
	}

	return strings.Join(lines, "\n"), nil
}

func (yas *YAS) formatStackLine(branchName string, indent int, isCurrent bool) string {
	metadata := yas.data.Branches.Get(branchName)

	// Build the line
	line := strings.Repeat(" ", indent*2)
	line += "* "

	if metadata.GitHubPullRequest.ID != "" {
		// Just use the URL directly
		line += metadata.GitHubPullRequest.URL
	} else {
		// No PR, just show branch name
		line += branchName
	}

	if isCurrent {
		line += " 👈 (this PR)"
	}

	return line
}

func (yas *YAS) getPRBody(prNumber string) (string, error) {
	output, err := xexec.Command("gh", "pr", "view", prNumber, "--json", "body", "-q", ".body").WithStdout(nil).Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

func (yas *YAS) updatePRBody(prNumber, newBody string) error {
	return xexec.Command("gh", "pr", "edit", prNumber, "--body", newBody).WithStdout(nil).WithStderr(nil).Run()
}

func removeStackSection(currentBody string) string {
	stackMarkerAlt := "Stacked PRs:"

	// Check if there's already a stack section
	if idx := strings.Index(currentBody, stackMarkerAlt); idx != -1 {
		// Find the start of the stack section (look for --- before it)
		startIdx := idx
		if sepIdx := strings.LastIndex(currentBody[:idx], "---\n"); sepIdx != -1 {
			startIdx = sepIdx
		}
		// Remove the stack section
		currentBody = strings.TrimSpace(currentBody[:startIdx])
	}

	return currentBody
}

func updatePRBodyWithStack(currentBody, stackVisualization string) string {
	stackMarker := "---\n\nStacked PRs:\n\n"
	stackMarkerAlt := "Stacked PRs:"

	// Check if there's already a stack section
	if idx := strings.Index(currentBody, stackMarkerAlt); idx != -1 {
		// Find the start of the stack section (look for --- before it)
		startIdx := idx
		if sepIdx := strings.LastIndex(currentBody[:idx], "---\n"); sepIdx != -1 {
			startIdx = sepIdx
		}
		// Remove the old stack section
		currentBody = strings.TrimSpace(currentBody[:startIdx])
	}

	// Append the new stack section
	if currentBody != "" {
		currentBody += "\n\n"
	}

	currentBody += stackMarker + stackVisualization

	return currentBody
}
