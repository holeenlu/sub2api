#!/usr/bin/env python3
"""Read-only BPS upstream review; never fetches, checks out, merges, or writes."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]


def git(repo, *args):
    result = subprocess.run(["git", "-C", str(repo), *args], capture_output=True, check=False)
    if result.returncode:
        raise ValueError(result.stderr.decode(errors="replace").strip())
    return result.stdout


def resolve(repo, ref):
    return git(repo, "rev-parse", "--verify", "--end-of-options", ref + "^{commit}").decode().strip()


def review(repo, ref):
    state = json.loads((ROOT / "docs/bps/integration-state.json").read_text())
    inventory = json.loads((ROOT / "docs/bps/source-inventory.json").read_text())
    baseline = resolve(repo, state["source_commit"])
    target = resolve(repo, ref or state["source_branch"])
    changed = git(repo, "diff", "--name-only", "-z", baseline, target).decode().split("\0")
    imported = {item["path"] for item in state["files"]}
    # Inventory is only a discovery index; its presence never authorizes import.
    candidates = {item["path"] for item in inventory["files"]}
    focused, adjacent, other = [], [], []
    for path in filter(None, changed):
        if path in imported:
            focused.append(path)
        elif path in candidates or re.search(r"bps|basispoints|excel.*oauth", path, re.I):
            adjacent.append(path)
        else:
            # Detect new shared files mentioning BPS even when their names do not.
            diff = git(repo, "diff", "--unified=0", baseline, target, "--", path)
            if re.search(rb"bps|basispoints|excel[_ -]?oauth", diff, re.I):
                adjacent.append(path)
            else:
                other.append(path)
    drift = []
    for item in state["files"]:
        local = ROOT / item["path"]
        if not local.is_file() or hashlib.sha256(local.read_bytes()).hexdigest() != item["local_sha256"]:
            drift.append(item["path"])
    commits = git(repo, "log", "--format=%H %s", baseline + ".." + target,
                  "--", *sorted(set(focused + adjacent))).decode().splitlines() if focused or adjacent else []
    return {
        "source_repository": state["source_repository"],
        "baseline": baseline, "target": target,
        "imported_paths_changed": focused,
        "adjacent_candidates_requiring_semantic_review": adjacent,
        "unrelated_paths_changed": other,
        "candidate_commits": commits,
        "local_changes_since_integration_snapshot": drift,
        "action": "Review upstream-changes.md and both behaviors before selecting any patch. This report makes no changes.",
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source_repo", type=Path, help="local ranxi2001/sub2api checkout")
    parser.add_argument("--ref", help="source revision to compare; defaults to production")
    args = parser.parse_args()
    try:
        result = review(args.source_repo, args.ref)
    except (ValueError, OSError, KeyError) as exc:
        parser.exit(2, f"BPS upstream review failed: {exc}\n")
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
