#!/bin/bash
# Mock script for git and gh commands
# Logs commands to a file specified by YAS_TEST_CMD_LOG env var

# Get the command name (git or gh)
CMD_NAME=$(basename "$0")

# Log the command and all arguments (one per line)
if [ -n "$YAS_TEST_CMD_LOG" ]; then
    echo "$CMD_NAME" >> "$YAS_TEST_CMD_LOG"
    for arg in "$@"; do
        echo "  $arg" >> "$YAS_TEST_CMD_LOG"
    done
    echo "" >> "$YAS_TEST_CMD_LOG"
fi

# Simulate specific command behaviors
case "$CMD_NAME" in
    git)
        # Handle git commands
        case "$1" in
            push)
                # Track which branch was pushed
                branch_name=""
                for arg in "$@"; do
                    if [[ "$arg" != -* && "$arg" != "origin" && "$arg" != "push" ]]; then
                        branch_name="$arg"
                        break
                    fi
                done
                # Mark this branch as pushed (so rev-parse can find it)
                if [ -n "$branch_name" ]; then
                    echo "pushed" > "/tmp/yas-test-pushed-$branch_name"
                fi
                # Simulate successful push
                exit 0
                ;;
            rev-parse)
                # Check if they're asking for origin/branch
                if [[ "$2" == origin/* ]]; then
                    branch_name="${2#origin/}"
                    # Check if this branch was pushed
                    if [ -f "/tmp/yas-test-pushed-$branch_name" ]; then
                        # Return the actual local branch hash (to simulate remote is in sync)
                        if [ -n "$YAS_TEST_REAL_GIT" ]; then
                            "$YAS_TEST_REAL_GIT" rev-parse "$branch_name" 2>/dev/null && exit 0
                        else
                            /usr/bin/git rev-parse "$branch_name" 2>/dev/null && exit 0
                        fi
                    fi
                fi
                # For other rev-parse commands, call real git
                if [ -n "$YAS_TEST_REAL_GIT" ]; then
                    exec "$YAS_TEST_REAL_GIT" "$@"
                else
                    exec /usr/bin/git "$@"
                fi
                ;;
            --version)
                echo "git version 2.40.0"
                exit 0
                ;;
            *)
                # For other git commands, call real git
                if [ -n "$YAS_TEST_REAL_GIT" ]; then
                    exec "$YAS_TEST_REAL_GIT" "$@"
                else
                    exec /usr/bin/git "$@"
                fi
                ;;
        esac
        ;;
    gh)
        # Handle gh commands
        if [[ "$1" == "pr" && "$2" == "list" ]]; then
            # Extract the branch from --head argument
            head_branch=""
            for ((i=1; i<=$#; i++)); do
                if [[ "${!i}" == "--head" ]]; then
                    ((i++))
                    head_branch="${!i}"
                    break
                fi
            done

            # Check if we should return an existing PR
            if [ -n "$YAS_TEST_EXISTING_PR_ID" ]; then
                state="${YAS_TEST_PR_STATE:-OPEN}"
                url="${YAS_TEST_PR_URL:-https://github.com/test/test/pull/1}"
                isDraft="${YAS_TEST_PR_IS_DRAFT:-false}"
                baseRefName="${YAS_TEST_PR_BASE_REF:-main}"
                reviewDecision="${YAS_TEST_PR_REVIEW_DECISION:-}"
                statusCheckRollup="${YAS_TEST_PR_STATUS_CHECK_ROLLUP:-}"

                # Build JSON with optional fields
                json="{\"id\":\"$YAS_TEST_EXISTING_PR_ID\",\"state\":\"$state\",\"url\":\"$url\",\"isDraft\":$isDraft,\"baseRefName\":\"$baseRefName\""
                if [ -n "$reviewDecision" ]; then
                    json="$json,\"reviewDecision\":\"$reviewDecision\""
                fi
                if [ -n "$statusCheckRollup" ]; then
                    json="$json,\"statusCheckRollup\":$statusCheckRollup"
                else
                    json="$json,\"statusCheckRollup\":[]"
                fi
                json="$json}"
                echo "[$json]"
            elif [ -f "/tmp/yas-test-pr-created-$head_branch" ]; then
                # PR was created in this test session
                pr_url=$(cat "/tmp/yas-test-pr-created-$head_branch")
                base_ref=$(cat "/tmp/yas-test-pr-base-$head_branch" 2>/dev/null || echo "main")
                echo "[{\"id\":\"PR_CREATED\",\"state\":\"OPEN\",\"url\":\"$pr_url\",\"isDraft\":true,\"baseRefName\":\"$base_ref\",\"statusCheckRollup\":[]}]"
            else
                echo "[]"
            fi
            exit 0
        elif [[ "$1" == "pr" && "$2" == "create" ]]; then
            # Extract the branch from --head and --base arguments
            head_branch=""
            base_branch="main"
            for ((i=1; i<=$#; i++)); do
                if [[ "${!i}" == "--head" ]]; then
                    ((i++))
                    head_branch="${!i}"
                elif [[ "${!i}" == "--base" ]]; then
                    ((i++))
                    base_branch="${!i}"
                fi
            done

            # Simulate successful PR creation
            pr_url="https://github.com/test/test/pull/1"
            echo "$pr_url"

            # Save that we created a PR for this branch
            if [ -n "$head_branch" ]; then
                echo "$pr_url" > "/tmp/yas-test-pr-created-$head_branch"
                echo "$base_branch" > "/tmp/yas-test-pr-base-$head_branch"
            fi
            exit 0
        elif [[ "$1" == "pr" && "$2" == "view" ]]; then
            # Check if they want JSON output with title and body
            has_json=false
            for arg in "$@"; do
                if [[ "$arg" == "--json" ]]; then
                    has_json=true
                    break
                fi
            done

            if $has_json; then
                # Extract the query to determine what fields to return
                query=""
                for ((i=1; i<=$#; i++)); do
                    if [[ "${!i}" == "-q" ]]; then
                        ((i++))
                        query="${!i}"
                        break
                    fi
                done

                # Mock PR title and body
                title="Mock PR Title"
                body="This is the original PR description."

                # If the query contains the separator, return title and body separated
                if [[ "$query" == *"---SEPARATOR---"* ]]; then
                    echo "$title"
                    echo "---SEPARATOR---"
                    echo "$body"
                else
                    # Otherwise just return the body (for backwards compatibility)
                    echo "$body"
                fi
            else
                # Return mock PR body (plain text)
                echo "This is the original PR description."
            fi
            exit 0
        elif [[ "$1" == "pr" && "$2" == "merge" ]]; then
            # Simulate successful merge
            echo "✓ Merged pull request"
            exit 0
        elif [[ "$1" == "pr" && "$2" == "edit" ]]; then
            # Extract PR number and base argument
            pr_number=""
            new_base=""
            for ((i=3; i<=$#; i++)); do
                arg="${!i}"
                if [[ "$arg" != --* ]]; then
                    pr_number="$arg"
                elif [[ "$arg" == "--base" ]]; then
                    ((i++))
                    new_base="${!i}"
                fi
            done

            # If updating base, save it for the head branch
            # We need to find which branch this PR is for
            # For now, we'll just simulate success
            # In a real test, you might track PR number to branch mapping
            exit 0
        fi
        ;;
esac

exit 0
