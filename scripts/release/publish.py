"""Publish only to the reserved Release ID; never resolve writes by tag name."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
from urllib.parse import quote


def api(method, path, *, payload=None, file=None, paginate=False, missing_ok=False):
    command = ["gh", "api", "--method", method, path]
    if paginate:
        command += ["--paginate", "--slurp"]
    if file is not None:
        command += ["--input", str(file), "-H", "Content-Type: application/octet-stream"]
    elif payload is not None:
        command += ["--input", "-"]
    result = subprocess.run(command, input=json.dumps(payload) if payload is not None else None,
                            text=True, capture_output=True)
    data = json.loads(result.stdout) if result.stdout.strip() else None
    if result.returncode:
        if missing_ok and isinstance(data, dict) and str(data.get("status")) == "404":
            return None
        raise RuntimeError(f"GitHub {method} {path} failed: {result.stderr.strip()}")
    return data


def verify_target(repository, release_id, plan):
    endpoint = f"repos/{repository}/releases/{release_id}"
    release = api("GET", endpoint)
    if release["id"] != release_id or release["tag_name"] != plan["tag"]:
        raise ValueError("Reserved Release ID no longer matches the planned tag")
    marker = re.search(r"<!-- release-plan:(\{[^\n]+\}) -->", release.get("body") or "")
    provenance = json.loads(marker[1]) if marker else {}
    for key in ("channel", "version", "tag", "commit", "image", "portal_image"):
        if provenance.get(key) != plan.get(key):
            raise ValueError(f"Reserved Release provenance differs: {key}")
    # The list may lag immediately after draft creation. The direct ID lookup
    # is authoritative, but any other draft/published ID for this tag is unsafe.
    pages = api("GET", f"repos/{repository}/releases?per_page=100", paginate=True)
    conflicts = {item["id"] for page in pages for item in page
                 if item["tag_name"] == plan["tag"] and item["id"] != release_id}
    published = api("GET", f"repos/{repository}/releases/tags/{quote(plan['tag'], safe='')}",
                    missing_ok=True)
    if published and published["id"] != release_id:
        conflicts.add(published["id"])
    if conflicts:
        raise ValueError(f"Release tag {plan['tag']} is also used by IDs {sorted(conflicts)}; "
                         "resolve the duplicate before building or publishing")
    ref = api("GET", f"repos/{repository}/git/ref/tags/{quote(plan['tag'], safe='')}")["object"]
    for _ in range(5):
        if ref["type"] != "tag":
            break
        ref = api("GET", f"repos/{repository}/git/tags/{ref['sha']}")["object"]
    if ref["type"] != "commit" or ref["sha"] != plan["commit"]:
        raise ValueError("Release tag no longer points to the planned source commit")
    return release


def artifact_hash(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def matches(asset, path):
    return (asset.get("state") == "uploaded" and asset.get("size") == path.stat().st_size
            and asset.get("digest") == "sha256:" + artifact_hash(path))


def publish(repository, release_id, plan, directory, notes_path):
    release = verify_target(repository, release_id, plan)
    archive = f"{plan['channel']}_{plan['version']}_linux_amd64.tar.gz"
    expected = {archive, "checksums.txt", "release-manifest.json", "compose.vendor-release.json"}
    files = {path.name: path for path in directory.iterdir() if path.is_file()}
    if set(files) != expected:
        raise ValueError("Release must contain exactly the four expected Linux amd64 artifacts")
    manifest = json.loads(files["release-manifest.json"].read_text())
    if any(manifest.get(key) != value for key, value in plan.items()):
        raise ValueError("Artifact manifest differs from the frozen release plan")
    for key in ("image_digest", "portal_image_digest"):
        if not re.fullmatch(r"sha256:[a-f0-9]{64}", manifest.get(key, "")):
            raise ValueError(f"Invalid {key}")
    override = json.loads(files["compose.vendor-release.json"].read_text())
    if override != {"services": {"vendor-portal": {"image": plan["portal_image"] + "@" + manifest["portal_image_digest"]}}}:
        raise ValueError("Supplier portal override differs from the manifest")
    if files["checksums.txt"].read_text().split() != [artifact_hash(files[archive]), archive]:
        raise ValueError("Archive checksum or filename does not match")
    notes = notes_path.read_text()
    marker = re.search(r"<!-- release-plan:(\{[^\n]+\}) -->", notes)
    if not marker or json.loads(marker[1]) != plan:
        raise ValueError("Release notes differ from the frozen release plan")
    assets = {asset["name"]: asset for asset in release["assets"]}
    if len(assets) != len(release["assets"]) or set(assets) - expected:
        raise ValueError("Reserved Release contains unexpected or duplicate assets")
    if not release["draft"]:
        if release["body"] != notes or set(assets) != expected or any(not matches(assets[name], path) for name, path in files.items()):
            raise ValueError("Published Release differs; refusing to overwrite existing artifacts")
        return  # Successful publication with a lost response is safe to retry.
    for name, path in sorted(files.items()):
        existing = assets.get(name)
        if existing and matches(existing, path):
            continue
        if existing:
            api("DELETE", f"repos/{repository}/releases/assets/{existing['id']}")
        uploaded = api("POST", f"https://uploads.github.com/repos/{repository}/releases/{release_id}/assets?name={quote(name, safe='')}", file=path)
        if not matches(uploaded, path):
            raise ValueError(f"Uploaded asset failed verification: {name}")
    release = verify_target(repository, release_id, plan)
    assets = {asset["name"]: asset for asset in release["assets"]}
    if set(assets) != expected or any(not matches(assets[name], path) for name, path in files.items()):
        raise ValueError("Reserved Release assets changed during upload")
    if not release["draft"]:
        if release["body"] != notes:
            raise ValueError("Release was published concurrently with different notes")
        return
    api("PATCH", f"repos/{repository}/releases/{release_id}",
        payload={"body": notes, "draft": False, "make_latest": "true"})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    plan = json.loads(Path("release-plan.json").read_text())
    repository = os.environ["GITHUB_REPOSITORY"]
    release_id = int(os.environ["RELEASE_ID"])
    config = json.loads(Path("scripts/release/channels.json").read_text())[plan["channel"]]
    if (release_id <= 0 or repository != config["repository"] or
            plan["tag"] != plan["channel"] + "/v" + plan["version"] or
            plan["image"] != config["image"] or plan["portal_image"] != config["portal_image"]):
        raise ValueError("Release target differs from the configured channel")
    for name, key in (("RELEASE_TAG", "tag"), ("RELEASE_SHA", "commit"),
                      ("RELEASE_VERSION", "version"), ("RELEASE_CHANNEL", "channel")):
        if name in os.environ and os.environ[name] != plan[key]:
            raise ValueError(f"{name} differs from the frozen release plan")
    if args.check:
        verify_target(repository, release_id, plan)
    else:
        publish(repository, release_id, plan, Path("dist/release"), Path("release-notes.md"))


if __name__ == "__main__":
    main()
