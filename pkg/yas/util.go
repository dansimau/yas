package yas

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
)

func formatBranchName(branchName string) string {
	lastSlash := strings.LastIndex(branchName, "/")
	if lastSlash == -1 {
		return branchName
	}

	prefix := branchName[:lastSlash]
	suffix := branchName[lastSlash+1:]
	if suffix == "" {
		return branchName
	}

	darkGray := color.New(color.FgHiBlack).SprintFunc()
	return fmt.Sprintf("%s%s", darkGray(prefix+"/"), suffix)
}

func getReviewStatusIcon(reviewDecision string) string {
	switch reviewDecision {
	case "APPROVED":
		return "✅"
	case "CHANGES_REQUESTED":
		return "❌"
	case "REVIEW_REQUIRED":
		return "❌"
	default:
		return "❌" // Default to cross for any other state
	}
}

func getCIStatusIcon(ciStatus string) string {
	switch ciStatus {
	case "SUCCESS":
		return "✅"
	case "FAILURE":
		return "❌"
	case "PENDING":
		return "⏳"
	case "": // No checks configured
		return "—" // Em dash to indicate no checks
	default:
		return "❌" // Default to cross for any other state
	}
}
