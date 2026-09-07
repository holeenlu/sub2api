"""Print the local merge workflow used by upstream-watch issues."""
import argparse

BRANCHES = {
    "public": ("holeen/main", "origin/main"),
    "kdan": ("KDAN", "origin/KDAN"),
    "tapmodels": ("TapModels", "origin/TapModels, erwinlin/main"),
}


def render_instructions(branch):
    local, destinations = BRANCHES[branch]
    return "\n".join([
        "## 同步步驟（在維護者整合工作區執行）",
        "",
        "工作區須已安裝 sync-upstream 技能及本機入口，並配置 origin、upstream、erwinlin 遠端。",
        "以普通 Git Merge 保留原始提交；先審閱衝突與驗證結果，再依該次授權發布。",
        "",
        "```bash",
        f"./deploy/sync-upstream.sh prepare --branches {branch}",
        "./deploy/sync-upstream.sh status",
        "```",
        "",
        f"本機分支：{local}；發布目標：{destinations}。",
        "prepare 只完成本機合併與驗證；發布需使用該檢查點 ID 及明確的推送授權。",
        "此工具同步公開上游，公共層新增的客製化修正是否已納入品牌仍須另行驗收。",
        "",
    ])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("branch", choices=BRANCHES)
    args = parser.parse_args()
    print(render_instructions(args.branch))


if __name__ == "__main__":
    main()
