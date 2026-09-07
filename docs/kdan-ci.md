# KDAN CI／Release

本 fork（`holeenlu/sub2api`）自己的三個工作流程。上游 `Wei-Shaw/sub2api` 的
`.github/workflows/{backend-ci,release,security-scan,cla}.yml` 原封不動留在樹裡，
只是在本 repo 停用（見文末）。

## 分支模型：main 是 force-push 上來的

`holeenlu/sub2api` 的 `main` **不是**在 GitHub 上長出來的：維護者在本機維護一條線性
分支 `KDAN`（= `holeenlu/sub2api` 的 `holeen/main`，本身又是 `Wei-Shaw/sub2api`
的 `upstream/main` ＋ 本地 commit），每次同步上游都是本機 rebase，再
`git push --force-with-lease` 覆蓋 `origin/main`（TODO(brand)：KDAN 還沒有專屬 remote）。

由此推出兩件事：

- **不要直接 commit 到 `origin/main`**，也不要在 GitHub 上合併 PR 進 `main`——下一次
  force-push 會把它整個洗掉。要改東西請對 `holeenlu/sub2api` 的 `KDAN` 分支開 PR，
  或直接跟維護者協調，讓改動進到那條本機線性分支裡。
- 工作流程一律**不寫 repo 內容**。同步上游是本機的事（`deploy/sync.sh`），CI 只負責提醒與驗證。

## 工作流程

| 檔案 | 觸發時機 | 做什麼 |
| --- | --- | --- |
| `kdan-sync-upstream.yml` | 每天 09:00（台北）／手動 | **唯讀**盤點上游進度，寫進一張追蹤 issue（不合併、不開 PR、不推分支） |
| `kdan-docker-image.yml` | push `main`、push 版號 tag（`1.0.0`，不加 v）、手動 | 建映像推 GHCR；tag 另外開 GitHub Release |
| `kdan-ci.yml` | push `main`、對 `main` 的 PR／手動 | 後端 ent 產生碼檢查＋單元測試、前端 lint+typecheck+vitest+build、zh-TW 語言包同步檢查、後端 zh-TW 轉換器 |

### 上游更新監看

不再自動合併、也不開 PR（開了也會被 force-push 洗掉）。每天跑一次，算出 `main` 目前掛靠的
上游基準點（`git merge-base HEAD upstream/main`）到 `upstream/main` 之間差多少，把結果寫進
**一張**追蹤 issue（標題 `上游更新待同步：<tag> / upstream/main <sha>`，標籤 `upstream-sync`）：

- 落後幾個 commit、其中幾個是 PR 合併 commit、diff shortstat、上游最新 release
- 上游動到哪些頂層區域，以及 `backend/ent/schema`、`backend/migrations`、
  `frontend/src/i18n/locales/zh`、`tools/zh-tw` 這四個「動到就要多做一件事」的路徑
- 雙方都改過的檔案（rebase 的衝突熱點）
- 上游是否動到 `tools/zh-tw/convert-go.mjs` 的保護清單
- 上游新增的中文字串字面值中「疑似拿中文比對外部文字」的那些（這類字串必須加進
  `PROTECTED_LITERALS`，否則繁體化會改壞比對）
- 在**上游那棵樹**上跑 `convert-go.mjs backend --dry` 的結果（用 `git archive` 倒進 `/tmp`，
  不動工作目錄），順便確認 Go 詞法分析器仍能解析上游全部檔案

每天是**改同一張 issue 的標題與內文**，不留言、不重開，避免洗版；`main` 追上 `upstream/main`
之後自動關閉。權限只要 `contents: read` ＋ `issues: write`，用內建的 `GITHUB_TOKEN` 就夠。

### CI 的取捨

刻意**不含**整合測試、golangci-lint、macOS runner——私有 repo 每月 2000 分鐘，
macOS 以 10 倍計費。若要恢復完整檢查，啟用上游的 `backend-ci.yml` 即可。

前端繁體走提交進 git 的 `zh-TW` 語言包（`tools/zh-tw/gen-locale.mjs` 由 zh 產生），CI 用 `gen-locale.mjs --check` 確認沒忘了重產；後端訊息在 Docker 建置時由 `convert-go.mjs` 轉換。

**觸發時機**：除了對 `main` 的 PR，**push `main` 也跑**。`main` 上的 commit 是本機 force-push
上來的，從來不經過 PR，只掛 `pull_request` 等於 `main` 從沒被測過。

**路徑過濾**：`changes` job 先用三點 diff 算出 PR 改到哪些區域，其他 job 據此決定跑不跑：
`backend/**` → 後端測試與轉換器；`frontend/**`、`tools/zh-tw/**`、`docs/legal/**` → 前端；
改到 `Makefile` 或工作流程本身則全跑。被略過的 job 在 PR 上顯示 skipped，不擋合併。
push `main` 與手動觸發**不做路徑過濾一律全跑**——force-push 的前後基準不可比
（`event.before` 常常不是祖先），算出來的「改了哪些檔案」沒有意義。

`locale` job（`gen-locale.mjs --check`）刻意獨立出來，不掛 `needs` 也不掛路徑過濾：
zh-TW 語言包是產生物，忘了重產的代價很高，而這個檢查只要裝一個小套件、幾十秒就跑完，
所以每個 PR、每次 push、每次手動觸發都跑。

## 需要的 secret

| 名稱 | 放哪 | 權限 |
| --- | --- | --- |
| `UPDATE_GITHUB_TOKEN` | 跑 app 那台機器的 `deploy/.env` | fine-grained PAT，限 `holeenlu/sub2api`：Contents Read |

工作流程這邊**不需要任何 repo secret**：監看只寫 issue，內建的 `GITHUB_TOKEN`
（`issues: write`）就夠；映像推 GHCR 也是用內建 token。原本給同步流程開 PR、推分支用的
`SYNC_UPSTREAM_TOKEN` 已經沒有工作流程會讀，可以直接從 repo secrets 刪掉。

`UPDATE_GITHUB_TOKEN` 是給後端查 release 用的；因為 repo 是私有的，沒有 token 版本徽章
就讀不到最新版本。`deploy/docker-compose.wsl.yml` 已經把它透傳進容器。

## 版號規則

KDAN 用**自己的**版號 `1.x.y`，tag **不加 `v` 前綴**，不鏡像上游號碼。原因是後端的 `compareVersions()`
會把 `-` 之後的字串整段截掉（`1.0.0-main.abc` 等於 `1.0.0`），跟著上游跳號只會讓比較結果
難以預期。上游版本記在 Release 內文（推導方式見下節）。

`main` 分支建出來的映像版本字串是 `<最近 tag>-main.<sha>`，只由「本 repo 最近的版號 tag」
與 HEAD 的 short sha 決定，跟上游同步方式無關（force-push 之後照樣算得出來）。挑 tag 用的
`--match '[0-9]*'` 只認我們自己不帶 `v` 的 tag，所以工作流程為了推導上游版本而
`git fetch upstream --tags` 帶進來的 `v*` tag 不會混進版號；版本字串那一步也排在 fetch 之前。

發版：

```bash
git tag -a 1.0.0 -m "KDAN v1.0.0"
git push origin 1.0.0
```

### 上游基準版本

版本徽章除了自己的版號，還會顯示一行「基於上游 Sub2API vX.Y.Z」，說明這份建置掛靠在哪個
上游版本上。

這個值**在建置時烘進二進位檔**（ldflags `-X main.UpstreamVersion=`），來源是
**本分支掛靠的那個上游 commit 最近的 `v*` tag**：

```bash
git remote add upstream https://github.com/Wei-Shaw/sub2api.git   # 沒有才加
git fetch upstream --tags
git describe --tags --match 'v*' --abbrev=0 "$(git merge-base HEAD upstream/main)"
```

線性模型下樹裡沒有「合併上游」的 merge commit 可以掃（舊版是掃 commit 標題
`chore(sync): 合併上游 vX.Y.Z`，rebase 之後這種 commit 根本不存在），所以改成直接問 git
「我是站在哪個上游版本上」。也**不是**讀 `backend/cmd/server/VERSION`：上游是在發版流程跑到
一半才回寫那個檔（`chore: sync VERSION to X [skip ci]`），所以在 `vX.Y.Z` 這個 tag 上檔案內容
永遠還是上一版（`v0.2.1` 上寫著 `0.2.0`）——那個檔只在 `merge-base`／`describe` 都推導失敗時
當退路（會同時發一則 `::warning::`）。

推導邏輯在 `kdan-docker-image.yml` 的 `build` 與 `release` 兩個 job 各有一份，
以 `>>> 上游版本推導 <<<` 註解標記，**兩份必須逐字一致**。ldflags 沒帶值時，後端
（`backend/cmd/server/main.go` 的 `initUpstreamVersion()`）退回 embedded VERSION 檔，
並統一補上 `v` 前綴。

本機建置時由 `deploy/docker-compose.wsl.yml` 的 build arg 帶入：

```bash
KDAN_UPSTREAM_VERSION="$(git describe --tags --match 'v*' --abbrev=0 "$(git merge-base HEAD upstream/main)")" \
  docker compose -f docker-compose.dev.yml -f docker-compose.wsl.yml build
```

WSL 的複製建置腳本自己算好並 export 這個變數，改了推導方式記得一起改。

## 伺服器怎麼拉映像

```bash
docker login ghcr.io -u holeenlu     # PAT 需要 read:packages
# compose 裡把 image 指到 ghcr.io/holeenlu/sub2api:latest
docker compose pull && docker compose up -d
```

## 上游工作流程為什麼停用

用 GitHub API 停用（不改檔案，減少同步衝突）：

- `release.yml` — 監聽 `v*` tag，會用 goreleaser 建上游規格的映像與 release。它從未在本 repo 執行過，
  所以 GitHub 沒登記它、也無法用 API 停用；解法是我們的 tag 一律不加 `v`（`1.0.0`），它就永遠不會被觸發。
- `backend-ci.yml` — 用 macOS runner 加 golangci-lint，私有 repo 太貴。
- `security-scan.yml` — 每次 push 都跑 govulncheck／pnpm audit；目前 Dependabot 還有一批未處理的依賴警示，先停用以免每個 PR 都紅燈，處理完再開。

要恢復其中一個：

```bash
gh workflow enable backend-ci.yml --repo holeenlu/sub2api
```

## 為什麼 app 內的「更新」按鈕不能用

版本徽章只是**資訊性**的：它比對本機版本與我們 repo 的最新 release，提示有新版。
按下去的「更新」流程會去下載 release asset 就地替換執行檔——對這個 fork 不成立，因為

1. 我們的 Release 不放 asset（部署方式是 Docker 映像）；
2. 容器內 `/app` 由 root 擁有、程式以非 root 的 `sub2api` 使用者執行，建不了暫存目錄，也換不掉執行檔。

升級請走 `docker compose pull && docker compose up -d`。
