"""Print the local merge workflow used by upstream-watch issues."""
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
        "以普通 Git Merge 保留原始提交；先審閱衝突與驗證結果，再依該次授權發布。",
        "",
        "```bash",
        f"./deploy/sync-upstream.sh prepare --branches {branch}",
        "./deploy/sync-upstream.sh status",
        "```",
        "",
        f"本機分支：{local}；發布目標：{destinations}。main 是 KDAN 品牌主整合分支；不維護獨立 KDAN 或公共分支。",
        "prepare 只完成本機合併與驗證；發布需使用該檢查點 ID 及明確的推送授權。",
        "官方上游先在 main 完成普通 merge 和驗證，再傳播到 TapModels、tokensavy；僅推送本倉庫，不向其他倉庫發布。",
        "",
    ])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("branch", choices=BRANCHES)
    args = parser.parse_args()
    print(render_instructions(args.branch))


if __name__ == "__main__":
    main()
