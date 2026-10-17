"""Render reader-facing release notes from the reserved channel/version boundary."""
import argparse
import html
import json
import os
from pathlib import Path
import re
import subprocess
from urllib.parse import quote

SECTIONS = {
    "feat": "新增功能", "perf": "优化改进", "refactor": "优化改进",
    "fix": "问题修复", "revert": "问题修复", "security": "安全修复",
    "docs": "文档更新", "build": "构建与发布", "ci": "构建与发布",
    "chore": "维护更新", "test": "测试完善",
}
CONVENTIONAL = re.compile(r"(?P<kind>\w+)(?:\((?P<scope>[^)]+)\))?(?P<breaking>!)?:\s*(?P<text>.+)")


def git(*args):
    return subprocess.check_output(["git", *args], text=True).strip()


def git_ok(*args):
    result = subprocess.run(["git", *args], capture_output=True, text=True)
    if result.returncode not in (0, 1):
        raise RuntimeError(result.stderr.strip())
    return result.returncode == 0


def inline(text):
    """Treat commit subjects as text, not executable Markdown/HTML."""
    return re.sub(r"([\\`*_[\]])", r"\\\1", html.escape(text, quote=False))


def changes_between(base, head):
    if git_ok("diff", "--quiet", base, head):
        return [], False, ""
    rewritten = not git_ok("merge-base", "--is-ancestor", base, head)
    # Include feature commits reached through brand merges, and exclude patches
    # already released under another SHA after an ordinary history rewrite.
    raw = git("log", "--reverse", "--right-only", "--cherry-pick", "--no-merges",
              "--format=%H%x00%s%x00%b%x00", f"{base}...{head}")
    fields = raw.split("\0")
    changes = [tuple(fields[i + n].strip() for n in range(3))
               for i in range(0, len(fields) - 1, 3)]
    stat = git("diff", "--stat", base, head)
    return changes, rewritten, stat


def release_descriptions(body):
    return re.findall(r"^Release-Note:[ \t]*(.+)$", body, re.MULTILINE)


def breaking_descriptions(body):
    return re.findall(r"^BREAKING[ -]CHANGE:[ \t]*(.+)$", body, re.MULTILINE)


def grouped_changes(changes, rewritten, released_notes=()):
    groups = {}
    maintenance = []
    seen = set(released_notes)
    for sha, subject, body in changes:
        subject = re.sub(r"^(?:(?:fixup|squash)!\s*)+", "", subject)
        parsed = CONVENTIONAL.fullmatch(subject)
        kind = parsed["kind"] if parsed else "other"
        # A Release-Note is an explicit, reviewed user-facing description.
        # A rewritten aggregate commit's old title must not masquerade as new work.
        editorial = release_descriptions(body)
        breaking = breaking_descriptions(body)
        for description in breaking:
            if description not in seen:
                groups.setdefault("破坏性变更", []).append((sha, description))
                seen.add(description)
        if not editorial and rewritten:
            continue
        title = SECTIONS.get(kind, "其他变更")
        texts = editorial or [(f"{parsed['scope']}：" if parsed and parsed["scope"] else "")
                              + (parsed["text"] if parsed else subject)]
        for description in texts:
            if description in seen:
                continue
            seen.add(description)
            if not editorial and kind in ("docs", "test", "ci", "chore", "build"):
                maintenance.append((sha, title + "：" + description))
            else:
                groups.setdefault(title, []).append((sha, description))
        if not breaking and parsed and parsed["breaking"] and texts[0] not in released_notes:
            groups.setdefault("破坏性变更", []).append((sha, texts[0]))
    return groups, maintenance


def curated_notes(plan, directory):
    path = directory / (plan["version"] + ".md")
    if not path.is_file():
        return ""
    content = path.read_text(encoding="utf-8").strip()
    marker = re.match(r"<!-- previous-version: ([0-9.]+|none) -->\n", content)
    if not marker:
        raise ValueError(f"{path}: missing previous-version boundary")
    # Channels can skip releases. Never use a two-fix patch note to describe a
    # larger upgrade from a different baseline.
    if marker[1] != plan.get("previous_version", "none"):
        return ""
    return content[marker.end():].strip()


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
    base = plan.get("previous_commit") or plan["upstream_commit"]
    # Historical tags survive branch rewrites. Fetch only the recorded tag if
    # checkout did not include the old commit; never guess an earlier boundary.
    if not git_ok("rev-parse", "--verify", "--quiet", base + "^{commit}"):
        if "previous_tag" not in plan:
            raise ValueError("Release baseline commit is unavailable")
        git("fetch", "--no-tags", "origin", "refs/tags/" + plan["previous_tag"])
        if git("rev-parse", "FETCH_HEAD^{commit}") != base:
            raise ValueError("Previous release tag no longer matches its recorded commit")
    changes, rewritten, stat = changes_between(base, head)
    released_body = git("log", "--format=%b", "--extended-regexp",
                        "--grep=^(Release-Note:|BREAKING[ -]CHANGE:)", base) if rewritten else ""
    released = release_descriptions(released_body) + breaking_descriptions(released_body)
    groups, maintenance = grouped_changes(changes, rewritten, released)
    reviewed = curated_notes(plan, notes_directory)
    lines = ["<!-- release-plan:" + json.dumps(plan) + " -->", "",
             f"基于官方 Sub2API **{plan['base']}**，发布渠道 **{plan['channel']}**。", ""]
    if "previous_version" in plan:
        previous_url = repo + "/releases/tag/" + quote(plan["previous_tag"], safe="")
        lines += [f"本次更新：[{plan['previous_version']}]({previous_url}) → **{plan['version']}**。", ""]
    else:
        lines += ["这是本渠道的首次发布，以下列出相对已集成上游基线的定制变化。", ""]
    if reviewed and stat:
        lines += [reviewed, ""]
    else:
        if rewritten and stat:
            lines += ["发布历史经过整理；以下仅列出明确编写的发布说明，实际变化以两版文件对比为准。", ""]
        if not stat:
            lines += ["两版源码内容一致，本次未新增功能或修复。", ""]
        elif not groups:
            lines += ["本次存在源码变化；具体范围见下方变更明细与两版文件对比。", ""]
        for title, entries in groups.items():
            lines += ["## " + title, ""]
            lines += [f"- {inline(text)} ([`{sha[:9]}`]({repo}/commit/{sha}))" for sha, text in entries]
            lines += [""]
    if maintenance and not reviewed:
        lines += ["<details>", "<summary>文档、测试与构建变更</summary>", ""]
        lines += [f"- {inline(text)} ([`{sha[:9]}`]({repo}/commit/{sha}))" for sha, text in maintenance]
        lines += ["", "</details>", ""]
    if rewritten and stat:
        lines += ["<details>", "<summary>两版文件变更统计</summary>", "", "```text", stat, "```", "", "</details>", ""]
    doc = f"{repo}/blob/{head}/deploy/AUTOMATIC_RELEASE.md"
    tag_url = f"{repo}/releases/tag/{quote(plan['tag'], safe='')}"
    lines += ["## 安装与升级", "", "**Docker 镜像（仅 Linux amd64）：**", "", "```sh",
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
                  "- 门户镜像只包含静态页面和 API 代理，不连接 PostgreSQL/Redis。后端在线更新不会自动更新其他服务器的门户容器。", ""]
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
    args.manifest.write_text(json.dumps(manifest, indent=2) + "\n")


if __name__ == "__main__":
    main()
