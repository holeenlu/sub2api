"""Allocate an immutable, channel-scoped release from integrated official history."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
from urllib.parse import quote

if __package__:
    from .publish import api
else:
    from publish import api

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


def next_version(base, previous, exact_upstream, new_base, floor=None):
    """A formal upstream bump resets the suffix; subsequent pushes increment it."""
    # A brand moving repositories must not restart below its last published
    # version. The floor is metadata only; old assets are never reused.
    if floor:
        if parts(base)[:3] < parts(floor)[:3]:
            raise ValueError("Integrated upstream version is below the migration floor")
        previous = [*previous, floor]
    same_base = [v for v in previous if parts(v)[:3] == parts(base)[:3]]
    if same_base:
        return base + "." + str(max(parts(v)[3] for v in same_base) + 1)
    if exact_upstream and new_base:
        return base
    return base + ".1"


def reserved_versions(channel):
    """Tags remain reserved even when their draft Release is removed."""
    prefix = f"refs/tags/{channel}/v"
    versions = []
    for line in git("ls-remote", "--tags", "--refs", "origin", prefix + "*").splitlines():
        _, ref = line.split()
        if ref.startswith(prefix) and VERSION.fullmatch(ref[len(prefix):]):
            versions.append(ref[len(prefix):])
    return versions


def listed_versions(releases, channel):
    """Names reserve versions even when old notes have no release-plan marker."""
    prefix = channel + "/v"
    return [r["tag_name"][len(prefix):] for r in releases
            if r["tag_name"].startswith(prefix) and VERSION.fullmatch(r["tag_name"][len(prefix):])]


def release_for_tag(repository, tag):
    # List releases can omit records whose Git tags were removed. A missing
    # list entry is therefore not evidence that the release name is reusable.
    return api("GET", f"repos/{repository}/releases/tags/{quote(tag, safe='')}", missing_ok=True)


def tag_commit(tag):
    ref = "refs/tags/" + tag
    refs = {name: sha for sha, name in (line.split() for line in
            git("ls-remote", "--tags", "origin", ref, ref + "^{}").splitlines())}
    return refs.get(ref + "^{}", refs.get(ref))


def next_available_version(repository, channel, base, previous, exact_upstream, new_base, floor=None):
    occupied = list(previous)
    for _ in range(100):
        version = next_version(base, occupied, exact_upstream, new_base, floor)
        tag = channel + "/v" + version
        if release_for_tag(repository, tag) is None and tag_commit(tag) is None:
            return version
        # Read-only discovery: never delete a historical Release or move its tag.
        occupied.append(version)
    raise ValueError("No free release version found after 100 checks; inspect retained release history")


def resumable_release(repository, releases, history, head):
    candidates = sorted((item for item in history if item[0]["commit"] == head),
                        key=lambda item: (not item[1]["draft"], parts(item[0]["version"])), reverse=True)
    for plan, release in candidates:
        if any(r["tag_name"] == release["tag_name"] and r["id"] != release["id"] for r in releases):
            continue
        published = release_for_tag(repository, plan["tag"])
        if published and published["id"] != release["id"]:
            continue
        if not release["draft"] and published is None:
            continue
        if tag_commit(plan["tag"]) != head:
            continue
        if release["draft"] and plan.get("previous_commit") and tag_commit(plan["previous_tag"]) != plan["previous_commit"]:
            continue
        return plan, release
    return None


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


def notes_baseline(history, version, resolve_tag=None):
    """Freeze the previous published stable release of this channel, not a draft."""
    published = [(p, r) for p, r in history
                 if not r.get("draft") and not r.get("prerelease")
                 and parts(p["version"]) < parts(version)]
    published.sort(key=lambda item: parts(item[0]["version"]), reverse=True)
    previous = next((item for item in published
                     if resolve_tag is None or resolve_tag(item[1]["tag_name"]) == item[0]["commit"]), None)
    if not previous:
        return {}
    p, r = previous
    return dict(previous_version=p["version"], previous_tag=r["tag_name"],
                previous_commit=p["commit"])


def history_migrated_with_integrated_official_base(previous_plan, head):
    """Allow a known branch rebuild only when its recorded upstream base remains integrated."""
    upstream_commit = (previous_plan or {}).get("upstream_commit")
    return bool(upstream_commit) and ancestor(upstream_commit, head)


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
    # Keep the manual run pinned to its selected commit even if the channel advances.
    head = git("rev-parse", "HEAD")
    pages = json.loads(run("gh", "api", "--paginate", "--slurp",
                          f"repos/{cfg['repository']}/releases?per_page=100"))
    releases = [r for page in pages for r in page]
    history = records(releases, channel)
    previous = max(history, key=lambda r: parts(r[0]["version"]), default=None)
    # Old retries never move latest backwards or overwrite a published artifact.
    matching = resumable_release(cfg["repository"], releases, history, head)
    skip = bool(matching and not matching[1]["draft"])
    if previous and previous[0]["commit"] != head:
        if ancestor(head, previous[0]["commit"]):
            skip = True
        elif not ancestor(previous[0]["commit"], head) and not history_migrated_with_integrated_official_base(previous[0], head):
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
        version = next_available_version(cfg["repository"], channel, base,
                    listed_versions(releases, channel) + reserved_versions(channel),
                    release_only and not custom_since_release, new_base or pristine, floor=cfg.get("version_floor"))
        tag = channel + "/v" + version
        if any(r["tag_name"] == tag for r in releases):
            raise ValueError("Release tag already exists without matching provenance")
        plan = dict(channel=channel, version=version, base=base, upstream_commit=frontier,
                    commit=head, image=cfg["image"], tag=tag, zh_tw=cfg["zh_tw"],
                    date=git("show", "-s", "--format=%cI", head))
        if cfg.get("portal_image"):
            plan["portal_image"] = cfg["portal_image"]
        plan.update(notes_baseline(history, version, resolve_tag=tag_commit))
        Path("release-notes.md").write_text("<!-- release-plan:" + json.dumps(plan) + " -->\n")
        existing_tag = git("ls-remote", "--tags", "origin", "refs/tags/" + tag)
        if existing_tag:
            raise ValueError("Release tag became occupied during reservation; retry to allocate a new version")
        run("gh", "api", "-X", "POST", f"repos/{cfg['repository']}/git/refs",
            "-f", "ref=refs/tags/" + tag, "-f", "sha=" + head)
        if release_for_tag(cfg["repository"], tag) is not None:
            raise ValueError("Release name became occupied during reservation; refusing a duplicate draft")
        # Reserve once. Failed builds leave a draft which the same SHA resumes.
        # Use the creation response directly. The list endpoint can lag behind
        # a successful create and omit the draft, especially after a tag reset.
        Path("release-request.json").write_text(json.dumps(dict(tag_name=tag,
            target_commitish=head, name=f"{channel} {version}", draft=True,
            body=Path("release-notes.md").read_text())))
        release = json.loads(run("gh", "api", "--method", "POST",
            f"repos/{cfg['repository']}/releases", "--input", "release-request.json"))
        reserved_tag = git("ls-remote", "--tags", "origin", "refs/tags/" + tag)
        if not reserved_tag or reserved_tag.split()[0] != head:
            raise ValueError("Reserved tag does not match the build commit")
    # Old reserved drafts predate the notes boundary. Excluding this version
    # keeps retries stable even if the release list contains the current draft.
    if "previous_commit" not in plan:
        plan.update(notes_baseline(history, plan["version"], resolve_tag=tag_commit))
    Path("release-plan.json").write_text(json.dumps(plan, indent=2) + "\n")
    with Path(os.environ["GITHUB_OUTPUT"]).open("a") as output:
        for key, value in {**plan, "release_id": release["id"], "publish": True, "promote": True}.items():
            output.write(f"{key}={str(value).lower() if isinstance(value, bool) else value}\n")


if __name__ == "__main__":
    main()
