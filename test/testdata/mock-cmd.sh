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
                # Simulate successful push
                exit 0
                ;;
            pull)
                # Simulate successful pull
                exit 0
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
            head_branch=""
            for ((i = 1; i <= $#; i++)); do
                if [[ "${!i}" == "--head" ]]; then
                    j=$((i + 1))
                    if (( j <= $# )); then
                        head_branch="${!j}"
                    fi
                    break
                fi
            done

            branch_key=""
            if [ -n "$head_branch" ]; then
                branch_key=$(echo "$head_branch" | tr '[:lower:]' '[:upper:]')
                branch_key=${branch_key//[^A-Z0-9]/_}
            fi

            branch_id_var="YAS_TEST_EXISTING_PR_ID${branch_key:+_}$branch_key"
            branch_state_var="YAS_TEST_PR_STATE${branch_key:+_}$branch_key"
            branch_url_var="YAS_TEST_PR_URL${branch_key:+_}$branch_key"
            branch_is_draft_var="YAS_TEST_PR_IS_DRAFT${branch_key:+_}$branch_key"

            # Check if we should return an existing PR for this branch
            branch_id=""
            branch_state=""
            branch_url=""
            branch_is_draft=""

            if [ -n "$branch_key" ]; then
                branch_id=${!branch_id_var}
                branch_state=${!branch_state_var}
                branch_url=${!branch_url_var}
                branch_is_draft=${!branch_is_draft_var}
            fi

            id="${branch_id:-$YAS_TEST_EXISTING_PR_ID}"
            if [ -n "$id" ]; then
                state="${branch_state:-${YAS_TEST_PR_STATE:-OPEN}}"
                url="${branch_url:-${YAS_TEST_PR_URL:-https://github.com/test/test/pull/1}}"
                isDraft="${branch_is_draft:-${YAS_TEST_PR_IS_DRAFT:-false}}"
                if [ -z "$isDraft" ]; then
                    isDraft="false"
                fi
                echo "[{\"id\":\"$id\",\"state\":\"$state\",\"url\":\"$url\",\"isDraft\":$isDraft}]"
            else
                echo "[]"
            fi
            exit 0
        elif [[ "$1" == "pr" && "$2" == "create" ]]; then
            # Simulate successful PR creation
            echo "https://github.com/test/test/pull/1"
            exit 0
        fi
        ;;
esac

exit 0
