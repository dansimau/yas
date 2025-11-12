#!/usr/bin/env python3
"""Mock git/gh wrapper used by tests without depending on jq."""

from __future__ import annotations

import json
import os
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import List, Sequence, NoReturn

CMD_NAME = Path(sys.argv[0]).name
TMP_DIR = Path("/tmp")


def real_git_path() -> str:
    return os.environ.get("YAS_TEST_REAL_GIT") or "/usr/bin/git"


def log_command(args: Sequence[str]) -> None:
    log_path = os.environ.get("YAS_TEST_CMD_LOG")
    if not log_path:
        return

    event = {
        "ts": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "pid": os.getpid(),
        "script": CMD_NAME,
        "cwd": os.getcwd(),
        "args": list(args),
    }

    line = json.dumps(event, separators=(",", ":"))
    with open(log_path, "a", encoding="utf-8") as handle:
        handle.write(line)
        handle.write("\n")


def exec_real_git(args: List[str]) -> NoReturn:
    real_git = real_git_path()
    os.execvp(real_git, [real_git, *args])


def run_git(args: List[str], **kwargs) -> subprocess.CompletedProcess:
    real_git = real_git_path()
    return subprocess.run([real_git, *args], **kwargs)


def pushed_marker(branch: str) -> Path:
    return TMP_DIR / f"yas-test-pushed-{branch}"


def pushed_hash_path(branch: str) -> Path:
    return TMP_DIR / f"yas-test-pushed-hash-{branch}"


def pr_created_path(branch: str) -> Path:
    return TMP_DIR / f"yas-test-pr-created-{branch}"


def pr_base_path(branch: str) -> Path:
    return TMP_DIR / f"yas-test-pr-base-{branch}"


def capture_branch_hash(branch: str) -> str:
    try:
        proc = run_git(["rev-parse", branch], capture_output=True, text=True)
    except FileNotFoundError:
        return ""
    if proc.returncode != 0:
        return ""
    return proc.stdout


def handle_git(args: List[str]) -> int:
    if not args:
        exec_real_git(args)

    subcommand = args[0]

    if subcommand == "push":
        branch_name = None
        for arg in args:
            if arg in {"push", "origin"} or arg.startswith("-"):
                continue
            branch_name = arg
            break

        if branch_name:
            marker = pushed_marker(branch_name)
            marker.write_text("pushed\n", encoding="utf-8")
            pushed_hash_path(branch_name).write_text(capture_branch_hash(branch_name), encoding="utf-8")
        return 0

    if subcommand == "show-ref" and len(args) > 1:
        ref = args[1]
        if ref.startswith("refs/remotes/origin/"):
            branch_name = ref.split("refs/remotes/origin/", 1)[1]
            return 0 if pushed_marker(branch_name).exists() else 1
        exec_real_git(args)

    if subcommand == "rev-parse" and len(args) > 1:
        ref = args[1]
        if ref.startswith("origin/"):
            branch_name = ref.split("origin/", 1)[1]
            if pushed_marker(branch_name).exists():
                hash_path = pushed_hash_path(branch_name)
                if hash_path.exists():
                    sys.stdout.write(hash_path.read_text(encoding="utf-8"))
                    return 0
                local_hash = capture_branch_hash(branch_name)
                if local_hash:
                    sys.stdout.write(local_hash)
                    return 0
        exec_real_git(args)

    if subcommand == "checkout" and len(args) > 1 and not args[1].startswith("-"):
        branch_name = args[1]
        result = run_git(
            ["show-ref", f"refs/heads/{branch_name}"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        if result.returncode != 0 and pushed_marker(branch_name).exists():
            hash_path = pushed_hash_path(branch_name)
            if hash_path.exists():
                pushed_hash = hash_path.read_text(encoding="utf-8").strip()
                checkout = run_git(["checkout", "-b", branch_name, pushed_hash])
                return checkout.returncode
        exec_real_git(args)

    if subcommand == "--version":
        print("git version 2.40.0")
        return 0

    exec_real_git(args)


def bool_env(name: str, default: bool) -> bool:
    value = os.environ.get(name)
    if value is None or value == "":
        return default
    try:
        parsed = json.loads(value)
    except json.JSONDecodeError:
        parsed = value
    if isinstance(parsed, bool):
        return parsed
    return str(value).lower() in {"1", "true", "yes", "on"}


def json_env(name: str, default):
    value = os.environ.get(name)
    if not value:
        return default
    try:
        return json.loads(value)
    except json.JSONDecodeError:
        return default


def handle_gh(args: List[str]) -> int:
    if len(args) >= 2 and args[0] == "pr" and args[1] == "list":
        head_branch = ""
        idx = 0
        while idx < len(args):
            if args[idx] == "--head" and idx + 1 < len(args):
                head_branch = args[idx + 1]
                break
            idx += 1

        existing_pr = os.environ.get("YAS_TEST_EXISTING_PR_ID")
        if existing_pr:
            response = {
                "id": existing_pr,
                "state": os.environ.get("YAS_TEST_PR_STATE", "OPEN"),
                "url": os.environ.get("YAS_TEST_PR_URL", "https://github.com/test/test/pull/1"),
                "isDraft": bool_env("YAS_TEST_PR_IS_DRAFT", False),
                "baseRefName": os.environ.get("YAS_TEST_PR_BASE_REF", "main"),
            }
            review_decision = os.environ.get("YAS_TEST_PR_REVIEW_DECISION")
            if review_decision:
                response["reviewDecision"] = review_decision
            response["statusCheckRollup"] = json_env("YAS_TEST_PR_STATUS_CHECK_ROLLUP", [])
            print(json.dumps([response]))
            return 0

        if head_branch:
            created = pr_created_path(head_branch)
            if created.exists():
                pr_url = created.read_text(encoding="utf-8").strip()
                base_file = pr_base_path(head_branch)
                base = base_file.read_text(encoding="utf-8").strip() if base_file.exists() else "main"
                payload = {
                    "id": "PR_CREATED",
                    "state": "OPEN",
                    "url": pr_url,
                    "isDraft": True,
                    "baseRefName": base,
                    "statusCheckRollup": [],
                }
                print(json.dumps([payload]))
                return 0

        print("[]")
        return 0

    if len(args) >= 2 and args[0] == "pr" and args[1] == "create":
        head_branch = ""
        base_branch = "main"
        idx = 0
        while idx < len(args):
            if args[idx] == "--head" and idx + 1 < len(args):
                head_branch = args[idx + 1]
                idx += 1
            elif args[idx] == "--base" and idx + 1 < len(args):
                base_branch = args[idx + 1]
                idx += 1
            idx += 1

        pr_url = "https://github.com/test/test/pull/1"
        print(pr_url)
        if head_branch:
            pr_created_path(head_branch).write_text(f"{pr_url}\n", encoding="utf-8")
            pr_base_path(head_branch).write_text(f"{base_branch}\n", encoding="utf-8")
        return 0

    if len(args) >= 2 and args[0] == "pr" and args[1] == "view":
        has_json = any(arg == "--json" for arg in args)
        if has_json:
            query = ""
            idx = 0
            while idx < len(args):
                if args[idx] == "-q" and idx + 1 < len(args):
                    query = args[idx + 1]
                    break
                idx += 1
            title = "Mock PR Title"
            body = "This is the original PR description."
            if "---SEPARATOR---" in query:
                print(title)
                print("---SEPARATOR---")
                print(body)
            else:
                print(body)
        else:
            print("This is the original PR description.")
        return 0

    if len(args) >= 2 and args[0] == "pr" and args[1] == "merge":
        print("✓ Merged pull request")
        return 0

    if len(args) >= 2 and args[0] == "pr" and args[1] == "edit":
        return 0

    return 0


def main() -> int:
    args = sys.argv[1:]
    log_command(args)

    if CMD_NAME == "git":
        return handle_git(args)
    if CMD_NAME == "gh":
        return handle_gh(args)
    return 0


if __name__ == "__main__":
    sys.exit(main())
