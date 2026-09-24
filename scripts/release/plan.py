"""Allocate an immutable, channel-scoped release from integrated official history."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess

NUMBER = r"(?:0|[1-9][0-9]*)"
VERSION = re.compile(rf"{NUMBER}\.{NUMBER}\.{NUMBER}(?:\.{NUMBER})?")
MARKER = re.compile(r"<!-- release-plan:(\{[^\n]+\}) -->")


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def git(*args):
    return run("git", *args)


def ancestor(a, b):
    return subprocess.run(["git", "merge-base", "--is-ancestor", a, b], check=False).returncode == 0


def parts(version):
    if not VERSION.fullmatch(version):
        raise ValueError(f"Invalid release version: {version}")
    return tuple(map(int, version.split("."))) + ((0,) if version.count(".") == 2 else ())


def next_version(base, previous, exact_upstream, new_base):
    """A formal upstream bump resets the suffix; subsequent pushes increment it."""
    same_base = [v for v in previous if parts(v)[:3] == parts(base)[:3]]
    if same_base:
        return base + "." + str(max(parts(v)[3] for v in same_base) + 1)
    if exact_upstream and new_base:
        return base
    return base + ".1"


def official_base(head):
    # Separate ref namespace: imported sub4api tags must NEVER influence the base.
    git("fetch", "--no-tags", "https://github.com/Wei-Shaw/sub2api.git",
        "+refs/heads/main:refs/remotes/release-upstream/main",
        "+refs/tags/v*:refs/upstream-release-tags/v*")
    frontier = git("merge-base", head, "refs/remotes/release-upstream/main")
    official_releases = json.loads(run("gh", "api", "--paginate", "--slurp",
        "repos/Wei-Shaw/sub2api/releases?per_page=100"))
    stable_tags = {r["tag_name"] for page in official_releases for r in page
                   if not r.get("draft") and not r.get("prerelease")}
    candidates = []
    for ref in git("for-each-ref", "--format=%(refname)", "refs/upstream-release-tags/").splitlines():
        version = ref.rsplit("/", 1)[1].removeprefix("v")
        if "v" + version in stable_tags and VERSION.fullmatch(version) and version.count(".") == 2 and ancestor(ref, head):
            candidates.append((parts(version), version, git("rev-parse", ref + "^{commit}")))
    if not candidates:
        raise ValueError("No integrated official stable release found; refusing a guessed version")
    _, base, sha = max(candidates)
    return base, sha, frontier


def records(releases, channel):
    result = []
    for release in releases:
        match = MARKER.search(release.get("body") or "")
        if match:
            plan = json.loads(match[1])
            if plan.get("channel") == channel and release["tag_name"] == channel + "/v" + plan["version"]:
                parts(plan["version"])
                result.append((plan, release))
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--select", action="store_true")
    args = parser.parse_args()
    config = json.loads(Path("scripts/release/channels.json").read_text())
    matches = [(channel, cfg) for channel, cfg in config.items()
               if cfg["repository"] == os.environ["GITHUB_REPOSITORY"]
               and "refs/heads/" + cfg["branch"] == os.environ["GITHUB_REF"]]
    if args.select:
        print("channel=" + (matches[0][0] if matches else ""))
        return
    if len(matches) != 1:
        raise ValueError("This repository/branch has no automatic release channel")
    channel, cfg = matches[0]
    head = git("rev-parse", "HEAD")
    pages = json.loads(run("gh", "api", "--paginate", "--slurp",
                          f"repos/{cfg['repository']}/releases?per_page=100"))
    releases = [r for page in pages for r in page]
    history = records(releases, channel)
    previous = max(history, key=lambda r: parts(r[0]["version"]), default=None)
    # Old retries never move latest backwards or overwrite a published artifact.
    matching = next(((p, r) for p, r in history if p["commit"] == head), None)
    skip = bool(matching and not matching[1]["draft"])
    if previous and previous[0]["commit"] != head:
        if ancestor(head, previous[0]["commit"]):
            skip = True
        elif not ancestor(previous[0]["commit"], head):
            raise ValueError("Release history diverged; refusing non-fast-forward publication")
    if skip:
        with Path(os.environ["GITHUB_OUTPUT"]).open("a") as output:
            output.write("publish=false\n")
            if matching and previous and matching[0]["version"] == previous[0]["version"]:
                for key, value in {"promote": "true", **matching[0]}.items():
                    output.write(f"{key}={value}\n")
        return
    if matching:
        plan, release = matching
    else:
        base, base_sha, frontier = official_base(head)
        if previous and parts(base)[:3] < parts(previous[0]["base"])[:3]:
            raise ValueError("Upstream version went backwards")
        new_base = bool(previous and previous[0]["base"] != base)
        # First run contains our existing custom work, hence .1. A pristine
        # upstream checkout can use the upstream version even on bootstrap.
        pristine = git("rev-parse", head + "^{tree}") == git("rev-parse", base_sha + "^{tree}")
        # Upstream's release bot writes VERSION after tagging; this mechanical
        # bookkeeping commit does not count as unreleased functional changes.
        release_only = subprocess.run(["git", "diff", "--quiet", base_sha, frontier, "--", ".",
            ":(exclude)backend/cmd/server/VERSION"], check=False).returncode == 0
        custom_since_release = bool(previous and git("rev-list", "--no-merges", head,
            "^" + previous[0]["commit"], "^" + frontier))
        version = next_version(base, [p["version"] for p, _ in history],
                               release_only and not custom_since_release, new_base or pristine)
        tag = channel + "/v" + version
        if any(r["tag_name"] == tag for r in releases):
            raise ValueError("Release tag already exists without matching provenance")
        plan = dict(channel=channel, version=version, base=base, upstream_commit=frontier,
                    commit=head, image=cfg["image"], tag=tag, zh_tw=cfg["zh_tw"],
                    date=git("show", "-s", "--format=%cI", head))
        Path("release-notes.md").write_text("<!-- release-plan:" + json.dumps(plan) + " -->\n")
        existing_tag = git("ls-remote", "--tags", "origin", "refs/tags/" + tag)
        if existing_tag and existing_tag.split()[0] != head:
            raise ValueError("Tag already points to another commit; refusing to reuse it")
        # Reserve once. Failed builds leave a draft which the same SHA resumes.
        run("gh", "release", "create", tag, "--draft", "--target", head,
            "--title", f"{channel} {version}", "--notes-file", "release-notes.md")
        reserved_tag = git("ls-remote", "--tags", "origin", "refs/tags/" + tag)
        if not reserved_tag or reserved_tag.split()[0] != head:
            raise ValueError("Reserved tag does not match the build commit")
        release = next(r for page in json.loads(run("gh", "api", "--paginate", "--slurp",
            f"repos/{cfg['repository']}/releases?per_page=100")) for r in page if r["tag_name"] == tag)
    Path("release-plan.json").write_text(json.dumps(plan, indent=2) + "\n")
    with Path(os.environ["GITHUB_OUTPUT"]).open("a") as output:
        for key, value in {**plan, "release_id": release["id"], "publish": True, "promote": True}.items():
            output.write(f"{key}={str(value).lower() if isinstance(value, bool) else value}\n")


if __name__ == "__main__":
    main()
