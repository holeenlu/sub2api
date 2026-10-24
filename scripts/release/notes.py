"""Render complete, reviewed customization notes from the released source tree."""
import argparse
import json
import os
import posixpath
from pathlib import Path
import re
import subprocess
from urllib.parse import quote

FULL_NOTES_PATH = "docs/CUSTOMIZATION_RELEASE_NOTES.md"
FULL_NOTES_SECTIONS = (
    "新增功能", "优化改进", "Bug 修复", "删除与退役", "破坏性变更与兼容性",
    "配置与默认值", "升级指南", "品牌差异", "来源与验证", "已知限制与后续研究",
)
BRAND_NAMES = {"kdan": "KDAN", "tapmodels": "TapModels", "tokensavy": "Tokensavy"}


def git(*args):
    return subprocess.check_output(["git", *args], text=True).strip()


def git_ok(*args):
    result = subprocess.run(["git", *args], capture_output=True, text=True)
    if result.returncode not in (0, 1):
        raise RuntimeError(result.stderr.strip())
    return result.returncode == 0


def source_changes(base, head):
    """Compare actual trees, including branches with rewritten customization commits."""
    paths = git("diff", "--name-only", "--no-renames", base, head).splitlines()
    rewritten = not git_ok("merge-base", "--is-ancestor", base, head)
    return paths, rewritten


def requires_notes_update(path):
    if (path in ("backend/cmd/server/VERSION", "deploy/AUTOMATIC_RELEASE.md") or
            path.startswith(("docs/", "deploy/release-notes/"))):
        return False
    parts = path.split("/")
    if any(part in ("tests", "__tests__", "testdata") for part in parts):
        return False
    name = parts[-1]
    return not (name.startswith("test_") or name.endswith("_test.go") or
                re.search(r"\.(?:spec|test)\.[cm]?[jt]sx?$", name))


def complete_notes(plan, base, changed_paths):
    head = plan["commit"]
    try:
        content = git("show", f"{head}:{FULL_NOTES_PATH}")
    except subprocess.CalledProcessError as error:
        raise ValueError("Released source must contain complete CUSTOMIZATION_RELEASE_NOTES.md") from error
    for title in FULL_NOTES_SECTIONS:
        # A heading with no explanation cannot certify a complete reader-facing note.
        section = re.search(r"^## " + re.escape(title) + r"\s*\n(.*?)(?=^## |\Z)",
                            content, re.MULTILINE | re.DOTALL)
        if not section or not section[1].strip():
            raise ValueError(f"Complete release notes require a nonempty section: {title}")
    if any(requires_notes_update(path) for path in changed_paths):
        previous = subprocess.run(["git", "rev-parse", "--verify", f"{base}:{FULL_NOTES_PATH}"],
                                  capture_output=True, text=True)
        if previous.returncode == 0 and previous.stdout.strip() == git("rev-parse", f"{head}:{FULL_NOTES_PATH}"):
            raise ValueError("Source changed without updating complete release notes; review added, changed and removed functionality")
    return content


def source_links(content, repo, head):
    """Make repository-relative documentation links work inside a GitHub Release."""
    def replace(match):
        target = match[1]
        if target.startswith(("#", "/")) or re.match(r"[a-zA-Z][a-zA-Z0-9+.-]*:", target):
            return match[0]
        path, separator, anchor = target.partition("#")
        path = posixpath.normpath(posixpath.join("docs", path))
        if path == ".." or path.startswith("../"):
            raise ValueError("Release documentation link escapes the repository")
        return "](" + repo + "/blob/" + head + "/" + quote(path, safe="/") + (separator + anchor if separator else "") + ")"
    return re.sub(r"\]\(([^\s)]+)\)", replace, content)


def curated_notes(plan, directory):
    path = directory / (plan["version"] + ".md")
    result = subprocess.run(["git", "show", f"{plan['commit']}:{path.as_posix()}"],
                            capture_output=True, text=True)
    if result.returncode:
        return ""
    content = result.stdout.strip()
    marker = re.match(r"<!-- previous-version: ([0-9.]+|none) -->\n", content)
    if not marker:
        raise ValueError(f"{path}: missing previous-version boundary")
    # Channels can skip releases. Never use a two-fix patch note to describe a
    # larger upgrade from a different baseline.
    if marker[1] != plan.get("previous_version", "none"):
        return ""
    return content[marker.end():].strip()


def release_content(plan):
    head = plan["commit"]
    base = plan.get("previous_commit") or plan["upstream_commit"]
    # Historical tags survive branch rewrites. Fetch only the recorded tag if
    # checkout did not include the old commit; never guess an earlier boundary.
    if not git_ok("rev-parse", "--verify", "--quiet", base + "^{commit}"):
        if "previous_tag" not in plan:
            raise ValueError("Release baseline commit is unavailable")
        git("fetch", "--no-tags", "origin", "refs/tags/" + plan["previous_tag"])
        if git("rev-parse", "FETCH_HEAD^{commit}") != base:
            raise ValueError("Previous release tag no longer matches its recorded commit")
    paths, rewritten = source_changes(base, head)
    full = complete_notes(plan, base, paths)
    return base, paths, rewritten, full


def render(plan, digest, config, notes_directory=Path("deploy/release-notes"), portal_digest=""):
    if not re.fullmatch(r"\d+\.\d+\.\d+(?:\.\d+)?", plan["version"]):
        raise ValueError("Invalid release version")
    if not re.fullmatch(r"sha256:[a-f0-9]{64}", digest):
        raise ValueError("Invalid image digest")
    channel = config[plan["channel"]]
    if plan.get("portal_image"):
        if plan["portal_image"] != channel.get("portal_image") or not re.fullmatch(r"sha256:[a-f0-9]{64}", portal_digest):
            raise ValueError("Invalid supplier portal image or digest")
    repo = "https://github.com/" + channel["repository"]
    head = plan["commit"]
    base, paths, rewritten, full = release_content(plan)
    stat = git("diff", "--stat", base, head) if paths else ""
    reviewed = curated_notes(plan, notes_directory)
    lines = ["<!-- release-plan:" + json.dumps(plan) + " -->", "",
             f"## {BRAND_NAMES[plan['channel']]} v{plan['version']}", "",
             f"基于官方 Sub2API **{plan['base']}**，发布渠道 **{plan['channel']}**。",
             f"源码：[`{head[:9]}`]({repo}/commit/{head})。本版本提供当前全部有效定制说明。", ""]
    if plan.get("portal_image"):
        lines += ["**使用独立供应商门户时，后端和门户必须一并升级到本次版本。仅升级后端可能导致新增供应商等操作报参数错误。**",
                  "同一 Compose 项目的新版宿主机更新器会联动更新与恢复；分机或不同项目请按下方门户步骤单独更新。已有服务器须先重新安装本版本宿主机更新器。", ""]
    if "previous_version" in plan:
        previous_url = repo + "/releases/tag/" + quote(plan["previous_tag"], safe="")
        lines += [f"本次更新：[{plan['previous_version']}]({previous_url}) → **{plan['version']}**。", ""]
    else:
        lines += ["这是本渠道的首次发布，以下列出相对已集成上游基线的定制变化。", ""]
    lines += [source_links(full, repo, head), ""]
    if reviewed and stat:
        lines += ["<details>", "<summary>本版本相对上一版的补充说明</summary>", "", reviewed, "", "</details>", ""]
    if not stat:
        lines += ["两版源码内容一致；以上仍提供本版本的完整有效功能说明。", ""]
    if rewritten and stat:
        lines += ["<details>", "<summary>两版文件变更统计</summary>", "", "```text", stat, "```", "", "</details>", ""]
    doc = f"{repo}/blob/{head}/deploy/AUTOMATIC_RELEASE.md"
    tag_url = f"{repo}/releases/tag/{quote(plan['tag'], safe='')}"
    lines += ["## 本版本安装与升级", "", "**Docker 镜像（仅 Linux amd64）：**", "", "```sh",
              f"docker pull {plan['image']}:{plan['version']}", "```", "",
              f"固定摘要：`{plan['image']}@{digest}`", "",
              f"- 二进制安装：从[本次发布附件]({tag_url})下载 Linux amd64 安装包，并用 `checksums.txt` 校验。",
              f"- Docker Compose：已配置宿主机更新服务的实例可从管理后台在线更新，详见[升级指南]({doc})。",
              "- 升级前备份数据库。启动时会执行本版本自带的迁移；镜像回退不会回滚数据库。", "",
              "## 版本与变更范围", "",
              f"- 构建源码：[`{head[:9]}`]({repo}/commit/{head})。",
              f"- [两版文件对比]({repo}/compare/{base}..{head})。",
              f"- [官方 {plan['base']} 发布说明](https://github.com/Wei-Shaw/sub2api/releases/tag/v{plan['base']})。",
              "- 附件 `release-manifest.json` 记录渠道、源码提交、版本与镜像摘要，供在线更新校验。", ""]
    if plan.get("portal_image"):
        lines += ["## 独立供应商门户", "",
                  f"- 配套镜像：`{plan['portal_image']}:{plan['version']}`。",
                  f"- 固定摘要：`{plan['portal_image']}@{portal_digest}`。",
                  f"- [同机 / 分机部署]({repo}/blob/{head}/docs/vendor-console/PORTAL-ROUTING.md#独立门户镜像2026-10-19)。",
                  "- 门户镜像只包含静态页面和 API 代理，不连接 PostgreSQL/Redis。",
                  "- 同机同项目：重新安装本版本宿主机更新器，保留包含门户的完整 Compose 文件列表；默认自动识别 vendor-portal，其他名称通过 --portal-service 指定。",
                  "- 分机或不同项目：从本次发布附件下载 compose.vendor-release.json；在门户服务器使用原来的项目名及完整 -f 文件列表，最后追加 -f compose.vendor-release.json，再执行 pull vendor-portal 和 up -d --no-deps vendor-portal。该文件仅固定门户镜像，不修改后端或数据库。",
                  "- 自定义门户服务名时，将附件中的 vendor-portal 键改为真实服务名。后续维护也必须保留此 override；确认门户恢复后再开放使用。", ""]
    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--plan", type=Path, default=Path("release-plan.json"))
    parser.add_argument("--output", type=Path, default=Path("release-notes.md"))
    parser.add_argument("--manifest", type=Path, default=Path("dist/release/release-manifest.json"))
    args = parser.parse_args()
    plan = json.loads(args.plan.read_text())
    digest = os.environ["IMAGE_DIGEST"]
    config = json.loads(Path("scripts/release/channels.json").read_text())
    portal_digest = os.environ.get("PORTAL_IMAGE_DIGEST", "")
    notes = render(plan, digest, config, portal_digest=portal_digest)
    args.output.write_text(notes, encoding="utf-8")
    args.manifest.parent.mkdir(parents=True, exist_ok=True)
    manifest = {**plan, "image_digest": digest}
    if plan.get("portal_image"):
        manifest["portal_image_digest"] = portal_digest
        # A portable Compose override works for both colocated and remote portals;
        # it contains no host paths, credentials, backend or storage configuration.
        portal_override = {"services": {"vendor-portal": {"image": plan["portal_image"] + "@" + portal_digest}}}
        (args.manifest.parent / "compose.vendor-release.json").write_text(json.dumps(portal_override, indent=2) + "\n")
    args.manifest.write_text(json.dumps(manifest, indent=2) + "\n")


if __name__ == "__main__":
    main()
