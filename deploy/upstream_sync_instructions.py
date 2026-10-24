"""Print the candidate review and single-customization workflow for upstream-watch issues."""
import argparse

BRANCHES = {
    "main": ("main", "origin/main"),
    "tapmodels": ("TapModels", "origin/TapModels"),
    "tokensavy": ("tokensavy", "origin/tokensavy"),
}


def render_instructions(branch):
    local, destinations = BRANCHES[branch]
    return "\n".join([
        "## 同步步驟（在維護者整合工作區執行）",
        "",
        "工作區須已安裝 sync-upstream 技能及本機入口，並配置 origin（holeenlu/sub2api）、upstream 遠端。",
        "先在隔離工作區整合候選樹；最終每個品牌為最新作者上游加一筆完整定制提交。",
        "",
        "```bash",
        f"./deploy/sync-upstream.sh prepare --branches {branch}",
        "./deploy/sync-upstream.sh status",
        "```",
        "",
        f"本機分支：{local}；發布目標：{destinations}。main 是 KDAN 品牌主整合分支；不維護獨立 KDAN 或公共分支。",
        "prepare/continue/status 僅用於臨時候選；舊檢查點 publish 已停用，不直接發布合併歷史。",
        "共享行為先在 main 驗證，再整合品牌預期樹；其他 Git 來源也歸入同一筆定制提交。",
        "依 docs/CHANGE_DELIVERY.md 的單一定制提交規則核對完整 Release Note、唯一官方父提交、預期樹和精確舊值租約後發布；僅交付本倉庫。",
        "",
    ])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("branch", choices=BRANCHES)
    args = parser.parse_args()
    print(render_instructions(args.branch))


if __name__ == "__main__":
    main()
