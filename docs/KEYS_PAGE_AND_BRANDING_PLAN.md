# /keys 页面优化 + 品牌定制 开发文档

> 基于 `holeen/main` @ `ef25d1f48` 的源码分析整理。行号为写作时的快照，落地前请以 `rg` 复核。
> 目标读者：负责实施的开发者。每个需求给出「现状 → 改动点 → 方案 → 测试 → 决策点」。
> 2026-09-05 审查更新：补齐设置兼容性、模型参数验证、配置下载依赖、纯重排测试及品牌盘点边界。本次更新仅修改方案，不代表功能已实现或测试已通过。
> 2026-09-05 需求确认：`review_model` 与最终选中的 `model` 始终相同，包括模型目录兜底后的结果；不再单独首选 Terra。此条覆盖本方案此前的双模型设计。

---

## 0. 总览

| # | 需求 | 范围 | 改动规模 | 后端 | 需要决策 |
|---|------|------|---------|------|---------|
| 1 | 移除「导入到 CCS」 | 前端（可选后端清理） | 小 | 可选 | 是否连带删除 `hide_ccs_import_button` 设置 |
| 2 | 「使用密钥」默认模型升级 | 前端 | 小 | 否 | Antigravity/Composite 是否同步；`gpt-6-astra` 参数 |
| 3 | 配置块增加「下载」 | 前端 | 小 | 否 | 下载覆盖哪些文件 |
| 4 | Codex 模型目录按分组顺序排列 | **后端** | 小 | 是 | 无 |
| 5 | 品牌 / 赞助商 盘点 | 全项目 | 盘点（本次不改码） | — | 白标策略 |

**共同约束**：本仓库长期跟踪 `upstream/main` 合并（见 git log 的 merge 记录）。所有方案按「最小 diff 足迹」设计，能不碰后端就不碰，能集中在一个函数改就不散改，以降低后续合并冲突。

**本文档的可提交性**：`.gitignore` L135 为 `docs/*` 加逐文件白名单，本文档原先被忽略。现已追加 `!docs/KEYS_PAGE_AND_BRANDING_PLAN.md`，只放行本文档，不开放整个目录。首次提交时同时包含白名单与文档；新增其他盘点文件也须逐文件检查忽略状态。`git status` 显示 `??` 只代表未跟踪且可见，不代表已暂存；可用 `git add --dry-run -- docs/KEYS_PAGE_AND_BRANDING_PLAN.md` 验证可加入，而不改变暂存区。

**实施边界**：本次功能实施范围为 §1–4；§5 仅交付盘点清单。实际品牌替换、广告移除、更新源切换、部署重命名均需另行确认。未核实的模型能力参数不得作为占位值发布；若发现必须新增后端能力或定价支持，单独立项并验证，不混入前端示例更新。

---

## 1. 移除「导入到 CCS」

### 1.1 现状

`/keys` 操作列有「导入到 CCS」按钮，点击生成 `ccswitch://v1/import?...` 深链拉起 CC-Switch 桌面端；Antigravity 分组会先弹「选择客户端」对话框。按钮受后台设置 `hide_ccs_import_button`（公开设置）控制显隐。

### 1.2 前端改动（必做）

| 文件 | 位置 | 改动 |
|------|------|------|
| `frontend/src/views/user/KeysView.vue` | L382–390 | 删除「导入到 CCS」按钮（`v-if="!publicSettings?.hide_ccs_import_button"` 那个 `<button>`） |
| 同上 | L1001–1045 | 删除 `<!-- CCS Client Selection Dialog for Antigravity -->` 整个 `BaseDialog` |
| 同上 | L1148–1151 | 删除 `import { buildCcSwitchImportDeeplink, type CcSwitchClientType } from '@/utils/ccswitchImport'` |
| 同上 | L1303, L1305 | 删除 `showCcsClientSelect`、`pendingCcsRow` 两个 ref |
| 同上 | L1872–1942 | 删除 `importToCcswitch`、`executeCcsImport`、`handleCcsClientSelect`、`closeCcsClientSelect` 四个函数 |
| `frontend/src/utils/ccswitchImport.ts` | 整文件 | **删除**（84 行，仅 KeysView 引用） |
| `frontend/src/utils/__tests__/ccswitchImport.spec.ts` | 整文件 | **删除**（96 行） |
| `frontend/src/i18n/locales/zh/dashboard.ts` | L98 `importToCcSwitch`；L241–247 `ccSwitchNotInstalled`、`ccsClientSelect.*` | 删除 |
| `frontend/src/i18n/locales/en/dashboard.ts` | L98；L237–243 | 删除（zh/en 必须对称，否则 `localesNoKeyCollision.spec` / 编译检查可能报错） |

删完后 `Icon name="upload"` 在 KeysView 无引用，但 Icon 组件里 `upload` 仍被其他 7 处使用，**不要**从 `Icon.vue` 删。

### 1.3 后台开关 `hide_ccs_import_button` 的处理（决策点）

按钮删除后这个开关就成了死配置。两个方案：

**方案 A（推荐，兼容保留）— 只删可见开关，保留字段往返，后端不动**

| 文件 | 位置 | 改动 |
|------|------|------|
| `frontend/src/views/admin/SettingsView.vue` | L6605–6618 | 删除「隐藏 CCS 导入按钮」Toggle 区块 |
| 同上 | L9582 | 保留 `form` 字段及加载服务器已有值的逻辑 |
| 同上 | L11214 | 保留保存 payload 中的字段，原值往返，不强制重置 |
| 同上 | L10784（`loadSettings()`） | **无需改动，但要知道为什么**：服务端值不是逐字段装载，而是 `for (const [key, value] of Object.entries(settings))` 通用循环写进 `form`。只要 L9582 的 `form` 默认字段不删，装载就自动保留；删了默认字段，循环仍会写入但类型丢失。这是「只删 Toggle 就够」的依据 |
| `frontend/src/i18n/locales/zh/admin/settings.ts` | L651–652 | 删除 `hideCcsImportButton` / `hideCcsImportButtonHint` |
| `frontend/src/i18n/locales/en/admin/settings.ts` | L656–657 | 同上 |
| `frontend/src/api/admin/settings.ts` | L494, L834 | 保留读写接口字段 |
| `frontend/src/types/index.ts` | L242 | 保留 `PublicSettings.hide_ccs_import_button` |
| `frontend/src/stores/app.ts` | L350 | 保留兼容默认值 |
| 测试 fixture | `stores/__tests__/app.spec.ts` L37, L458；`views/admin/__tests__/SettingsView.spec.ts` L391；`components/auth/__tests__/WechatOAuthSection.spec.ts` L73；`components/user/profile/__tests__/ProfileIdentityBindingsSection.spec.ts` L254 | 保留字段，补充已有值不变的回归用例 |

后端继续接受/返回这个字段。**不能删除保存字段后声称无副作用**：`setting_handler_update.go` L164 的字段是非指针 `bool`，缺省为 `false`；`setting_update.go` L347 会无条件持久化，导致保存其他设置时也把已有 `true` 覆盖成 `false`，影响回滚或仍使用旧前端的客户端。方案 A 保留字段往返，仅去掉入口和可见开关。若未来需要省略字段，应先明确后端缺省更新语义并测试，而不是依赖 Go 零值。

**方案 B — 后端一并清理**（约 20 个改动点，全部是样板代码）

`backend/internal/service/domain_constants.go` L372 · `setting_public.go` L191/L332/L582/L671 · `setting_parse.go` L362 · `setting_update.go` L347 · `settings_view.go` L160/L354 · `handler/dto/settings.go` L159/L387 · `handler/setting_handler.go` L79 · `handler/admin/setting_handler.go` L257 · `handler/admin/setting_handler_update.go` L164/L1624/L2252 · `handler/admin/setting_handler_audit.go` L344–345 · 测试 `server/api_contract_test.go` L892/L1178。数据库里已存在的 `hide_ccs_import_button` 行会被忽略，不需要迁移。

方案 B 更干净但每个文件都是 upstream 高频改动区，合并成本高。**建议先做 A，B 放到下一次与 upstream 同步之后再评估。**

### 1.4 测试

- 删除 `ccswitchImport.spec.ts`。
- `KeysView.spec.ts` 目前没有引用 CCS，不需要改；跑一遍确认。
- 若做方案 A：保留现有 fixture 字段；已有值分别为 `true` / `false` 时，加载设置并保存无关字段，断言原值仍在请求中且不变。写法直接套用 `views/admin/__tests__/SettingsView.spec.ts` L723 `submits the compact home page toggle`（mock `getSettings` 返回 `hide_ccs_import_button: true` → 改动别的字段并保存 → 断言 `updateSettings` 收到的 payload 里该字段仍为 `true`）。
- 补充 `/keys` 不再渲染 CCS 入口、后台不再渲染可见开关的断言；保留 Antigravity「使用密钥」流程回归。
- `pnpm -C frontend test:run`、`pnpm -C frontend typecheck`。

---

## 2. 「使用密钥」默认模型升级

所有改动都在 `frontend/src/components/keys/UseKeyModal.vue`。

### 2.1 Anthropic 分组 → Codex CLI：`claude-sonnet-4-6` → `claude-sonnet-5`

- 位置：`generateRoutedCodexFiles()` L1213–1223 的 `preferredModels` 映射，`anthropic: 'claude-sonnet-4-6'`。
- macOS/Linux 与 Windows 两个 tab 共用同一个映射，改一处即可覆盖需求里提到的 Windows。
- `claude-sonnet-5` 在后端已被识别为 1M 上下文默认模型（`settings_view.go` L622–658），无需后端配合。

**决策点**：同一映射里 `antigravity: 'claude-sonnet-4-6'`（L1217）是否同步改？Antigravity 上游能否提供 sonnet-5 取决于账号配置，建议**暂不改**，等确认 Antigravity 上游模型列表后再动。

### 2.2 OpenAI 分组 → Codex CLI / Codex CLI (WebSocket)

现状（`generateOpenAIFiles()` L929–935、`generateOpenAIWsFiles()` L1274–1280）：

```ts
const model = selectCodexCatalogModel('gpt-5.5')
...
model = "${model}"
review_model = "${model}"     // 与 model 同值
```

改为：

```ts
const model = selectCodexCatalogModel('gpt-5.6-sol')
const reasoningEffortLine = codexReasoningEffortTomlLine(model)
...
model = "${model}"
review_model = "${model}"
```

普通 CLI 和 WebSocket 两条生成路径都只选择一次主模型，`review_model` 直接复用该结果，不独立选择 Terra。`selectCodexCatalogModel()`（L657）的语义是：用户点过「获取目录」且目录里没有首选模型时，退回目录第 1 个；未获取目录时原样返回首选值。改动后这个语义不变，兜底后两个配置键仍必须同值。Composite 和其他 routed Codex 配置同样遵守此约束。

**注意**：后端 `configuredCodexGPTReasoningLevels()`（`openai_codex_models_service.go` L562）为 `gpt-5.6-sol`/`gpt-5.6-terra` 额外声明了 `ultra` 档，目录获取后 `codexReasoningEffortTomlLine` 会按目录 `default_reasoning_level` 生成 `model_reasoning_effort`，不需要前端硬编码。

**决策点**：`preferredModels.composite: 'gpt-5.5'`（L1222）是否同步改为 `gpt-5.6-sol`？需求未提，建议同步改以保持一致，但需确认 Composite 分组的路由表里有该模型。

### 2.3 OpenCode 支持模型列表

位置：`generateOpenCodeConfig()` 的 `openaiModels` 对象 L1309–1473。

现有条目：`gpt-5.2`、`gpt-5.6`、`gpt-5.6-sol`、`gpt-5.6-terra`、`gpt-5.6-luna`、`gpt-5.5`、`gpt-5.4`、`gpt-5.4-mini`、`gpt-5.3-codex-spark`、`codex-mini-latest`。

目标列表（按需求顺序）：

| slug | name | context / output | variants | 备注 |
|------|------|------------------|----------|------|
| `gpt-5.5` | GPT-5.5 | 1050000 / 128000 | low medium high xhigh | 保留现有 |
| `gpt-5.6` | GPT-5.6 | 1050000 / 128000 | low medium high xhigh max | 现名 "GPT-5.6 (Sol)" 建议改回 "GPT-5.6" |
| `gpt-5.6-sol` | GPT-5.6 Sol | 1050000 / 128000 | low medium high xhigh max **ultra** | 后端目录声明 ultra |
| `gpt-5.6-terra` | GPT-5.6 Terra | 1050000 / 128000 | low medium high xhigh max **ultra** | 同上 |
| `gpt-5.6-luna` | GPT-5.6 Luna | 1050000 / 128000 | low medium high xhigh max | 保留 |
| `gpt-6-astra` | GPT-6 Astra | **待确认** | **待确认** | 见下 |

删除：`gpt-5.2`、`gpt-5.4`、`gpt-5.4-mini`、`gpt-5.3-codex-spark`、`codex-mini-latest`。

**`gpt-6-astra` 发布前检查（未完成前不得发布占位参数）**：源码快照中未发现该 slug 的专用处理，但这不等于动态定价或通用转发必然不支持——

- `billing_service.go` 没有该模型的硬编码兜底价（`gpt-5.6-*` 有，L435–465）；需沿实际价格解析路径检查配置覆盖、运行时价格目录及未命中行为，不能只检查远端仓库是否收录。
- `openai_model_alias.go` / `openai_codex_transform.go` 没有别名归一化与 Codex 转换映射。
- `isOpenAIGPT56Model()` 为 false → Codex 目录不会给它 `max` 档。

列入正式 OpenCode 配置前，必须记录上下文、输出上限和推理档位的可验证来源及核验日期，确认目标上游可调用、运行时定价解析正确。**禁止先按 1050000/128000 填充并用「未核实」注释代替验证**，客户端仍会实际使用这些参数。表中其他模型沿用或新增的参数也须核验，后端声明档位不等同于已验证所有上游支持。

缺少硬编码兜底价不意味着必须新增兜底价。若实际解析正确，本任务不改计费后端；若发现缺价、转换或能力缺口，另立后端支持任务，使用已核实的数据及测试。参数未确认时保留为未完成项，不静默删除用户要求的模型，也不宣称需求已全部完成。

### 2.4 测试影响（`frontend/src/components/keys/__tests__/UseKeyModal.spec.ts`）

| 测试 | 行 | 处理 |
|------|----|------|
| `keeps legacy OpenAI Codex config as the default` | L344, 断言 L368–369 | `gpt-5.5` → `model = "gpt-5.6-sol"`、`review_model = "gpt-5.6-sol"` |
| `renders API Key Mode authorization in OpenAI Codex config` | L387 | 同上 |
| `keeps legacy OpenAI Codex WebSocket config as the default` | L438, 断言 L470–471 | 同上 |
| `preserves API Key Mode when switching to OpenAI Codex WebSocket config` | L486 | 检查是否断言模型 |
| `renders GPT-5.4 mini entry in OpenCode config` | L569 | **删除**。`gpt-6-astra` 的用例待 §2.3 参数核实后再补，参数未确认前不写断言（否则与 §2.3「禁止占位」矛盾） |
| `renders GPT-5.6 alias and max variants in OpenCode config` | L603 | 若加 `ultra` 需补断言 |
| `offers a downloadable Codex catalog for Composite API keys` | L688, L763 | `not.toContain('model = "gpt-5.5"')` 若 composite 改为 sol 需同步 |
| `keeps the preferred Composite default when it exists in the catalog` | L827, fixture L834 | 若 composite 改为 sol，fixture slug 与断言同步 |
| `derives OpenAI Codex reasoning effort from the selected catalog descriptor` | L873 | fixture 里的 slug 改为 `gpt-5.6-sol` |

新增：一个断言 Anthropic 分组 Codex tab（unix + windows）`model = "claude-sonnet-5"` 的用例；断言 `model` 与 `review_model` 始终同值。Composite 也须覆盖同时提供 Sol/Terra 和仅提供 Terra 时的结果，后者应由主模型目录兜底统一选中 Terra。

默认模型测试覆盖 Codex CLI / WebSocket、legacy / API Key Mode：Sol/Terra 同时存在、仅 Sol 存在但不在首位、Terra 存在而 Sol 缺失、两者均不存在、未获取目录、空目录、获取失败。先断言主模型的选择结果，再断言 `review_model === model`；不得单独优先选中 Terra。下载文件也须通过 TOML 解析验证此同值约束。按当前 helper 语义，空目录回到主模型首选值，这不代表上游可用性已验证。

---

## 3. 配置块增加「下载」按钮

### 3.1 现状

`UseKeyModal.vue` L138–173 为每个 `FileConfig` 渲染一个代码卡片，头部只有「复制」。`file-saver` 已引入（L260，用于目录下载），`Icon` 已有 `download` 图标（`Icon.vue` L88）。已有 `keys.useKeyModal.openai.configTomlHint` 提示，但提示本身不能替代「根级配置位于文件开头」的结构性验收。

生成器目前大体已把根级字段放在 table 之前，无需为此大规模重构。实施时须确认 `model_provider`、`model`、`review_model` 等根级键位于首个 `[table]` 之前，解析结果也确实位于根级；未使用的可选键不要求强行补齐。展示、复制和下载共用 `file.content`，不得单独拼接另一份下载模板。

### 3.2 方案

1. `FileConfig` 接口（L291–296）加字段 `downloadName?: string`。有值才渲染下载按钮，避免给 `Terminal` / `Command Prompt` / `PowerShell` 这类环境变量块生成无意义文件。
2. 模板 L152–167 「复制」按钮旁增加：

```vue
<button
  v-if="file.downloadName"
  type="button"
  @click="downloadFile(file)"
  class="...同复制按钮样式..."
>
  <Icon name="download" size="sm" />
  {{ t('keys.useKeyModal.download') }}
</button>
```

3. 新增函数：

```ts
function downloadFile(file: FileConfig) {
  if (!file.downloadName) return
  const mime = file.downloadName.endsWith('.json')
    ? 'application/json;charset=utf-8'
    : 'text/plain;charset=utf-8'
  saveAs(new Blob([file.content], { type: mime }), file.downloadName)
}
```

4. 在各生成函数里补 `downloadName`：

| 函数 | 位置 | 文件 | downloadName |
|------|------|------|--------------|
| `buildOpenAICodexFileConfigs` | L968–983 | `config.toml` / `auth.json` | `config.toml` / `auth.json` |
| `generateRoutedCodexFiles` | L1259–1267 | `config.toml` | `config.toml` |
| `generateGrokCodexFiles` | L1198–1202 | `config.toml` | `config.toml` |
| `generateGrokFiles` | L1129–1133 | `~/.grok/config.toml` | `config.toml` |
| `generateOpenCodeConfig` | L1851–1855 | `opencode.json` | 默认 `opencode.json`；Antigravity 双配置是否另命名待确认，不能仅为避免重名改成客户端未验证的文件名 |
| `generateAnthropicFiles` / `generateGrokClaudeFiles` | L816–820 / L869–876 | `~/.claude/settings.json` | `settings.json`（可选扩展，确认后再纳入） |

5. i18n：`zh/dashboard.ts` `useKeyModal` 下加 `download: '下载'`；`en/dashboard.ts` 加 `download: 'Download'`。不要复用 `codexModelCatalog.download`（那是「下载目录」）。

### 3.3 测试

- `UseKeyModal.spec.ts` 已 mock `saveAs`（L771 `saveAsMock`），新增：点击 config.toml 卡片下载按钮 → `saveAs` 被调用，文件名 `config.toml`，Blob 内容等于卡片文本；OpenCode tab 同理 `opencode.json`。
- 断言环境变量块（`Terminal`）不渲染下载按钮。
- 使用 TOML 解析器验证下载内容和根级字段归属，不仅做字符串包含断言。`frontend/package.json` 当前未直接声明 TOML 解析器；候选为 `smol-toml`（仅作为 devDependency）。实施前核验拟采用版本的依赖树、许可证、运行时兼容性及安全记录，选定版本并同步锁文件，不手写解析器。本次未安装，也未独立确认该候选版本是否零依赖。
- `.github/workflows/security-scan.yml` L50–58 执行 `pnpm audit --prod --audit-level=high`，随后由例外检查脚本处理结果，仅覆盖生产依赖。新增 devDependency 不在此审计范围内，不能据此认定安全或保证 CI 通过；引入时须另外执行包含开发依赖的审计并记录结果，本次文档修订不更改门禁。
- 用 `JSON.parse` 验证 `opencode.json` / `auth.json`，断言模型键、认证值和展示内容一致。
- 覆盖 Windows / Unix、legacy / API Key Mode、Codex CLI / WebSocket，以及 §2.4 的目录兜底状态；切换密钥或分组后不得下载旧上下文的配置或目录。
- 中英文及窄屏下检查复制/下载按钮不重叠，浏览器实际下载文件名、编码和内容正确。

### 3.4 模型目录与认证依赖（发布前必须确定）

当前 TOML 无条件包含 `model_catalog_json`，即使用户未获取或保存目录。仅增加下载按钮并不构成可用配置的完整流程：

- 建议保留现有目录配套方式，明确目录获取成功、下载并放置到目标路径后，再使用引用它的配置。获取失败、空目录或未获取时，不把带目录引用的下载描述为「直接可用」。是否禁用该状态下的配置下载，需在实现前确定；不能仅靠「已获取」推断文件已保存到本地。
- 若选择目录不可用时省略 `model_catalog_json`，须先验证对应客户端和平台可使用在线目录，不能静默移除而未经验证。无需强制打包，但配置与目录必须有可验收的配套流程。
- 在真实 Windows / Unix 客户端核验路径展开和目录加载；页面显示 `%userprofile%` 或 `~` 不等于客户端一定按预期解析。
- legacy 模式同时需要 `auth.json`；路由模式还依赖环境变量。下载单个 TOML 不代表认证配置完成，测试须覆盖配套凭据。
- 下载的是当前模板，不读取或合并用户原有配置；不得宣称可无损覆盖旧文件。含密钥的下载内容不得写入日志、埋点或使用真实凭据的测试快照。

---

## 4. Codex 模型目录按分组「自定义 /v1/models 列表」顺序排列

### 4.1 现状与根因

「获取目录」前端调用 `GET {baseUrl}/v1/models?client_version=0.147.0`（`frontend/src/api/codex.ts`），路由层 `routes/gateway.go` L68–77 检测到 `client_version` 后分发到 Codex 目录处理器；OpenAI 分组走 `OpenAIGatewayHandler.CodexModels`，其余分组走 `GatewayHandler.CodexModels`。

管理端「自定义 /v1/models 模型列表」（`GroupsView.vue` L778 起，含上下箭头排序）保存为 `group.ModelsListConfig.Models`，`normalizeGroupModelsListConfig()`（`group_models_list.go`）只去重不排序，**顺序已被正确持久化**。

三条产出目录的路径，顺序表现不一致：

| 路径 | 入口 | 排序结果 | 状态 |
|------|------|---------|------|
| A. OpenAI 分组、账号配置了 model_mapping | `BuildGroupConfiguredCodexModelsManifest` (`openai_codex_models_service.go` L109) → `openAIConfiguredCodexModelIDsForGroup` L271 | **`sort.Strings` 字母序**（L302；L267 亦同） | ❌ 需修 |
| B. OpenAI 分组、走上游 ChatGPT 目录 | `FetchCodexModelsManifest` → `MergeGroupConfiguredCodexModels` L161 → `mergeConfiguredCodexModelsManifest` L1141 | 上游原序 + 追加字母序别名，只做过滤不重排 | ❌ 需修 |
| C. 非 OpenAI / Composite 分组 | `GatewayHandler.CodexModels` (`gateway_handler.go` L1160) → `codexModelIDsForGroup` L1195 → `filterModelsByCustomList` L1387 | 按 `selectedModels` 迭代，**已是分组顺序** | ✅ 无需改 |

`/v1/models`（非 Codex）也走 `filterModelsByCustomList`，所以普通模型列表顺序一直是对的，只有 OpenAI 分组的 Codex 目录不对。

### 4.2 方案（单点修复）

路径 A 和 B 最终都经过 `mergeConfiguredCodexModelsManifest(body, configuredModels, selectedModels, filterBySelection)`，且 `filterBySelection == group.CustomModelsListEnabled()`。在该函数构建完 `merged` 之后、**`if !changed { return body, false, nil }` 提前返回判断之前**（源码快照 L1232），增加一步；仅放在序列化之前但落在提前返回之后，会漏掉「只改变顺序」的情况：

```go
// 自定义列表开启时，目录顺序以管理端排好的顺序为准：
// 列表内的按其下标排，列表外的（理论上已被过滤掉）保持相对顺序排在最后。
if filterBySelection && len(selectedModels) > 0 {
    if reorderCodexModelsBySelection(merged, selectedModels) {
        changed = true
    }
}
```

`reorderCodexModelsBySelection` 用 `sort.SliceStable`，key 为 slug 在 `selectedModels` 中的下标（未命中 → `len(selectedModels)`）；返回是否发生了位置变化以正确驱动 `changed`。路径 B 在 `MergeGroupConfiguredCodexModels` L187–189 根据 `changed` 重算 ETag；路径 A 在 `BuildGroupConfiguredCodexModelsManifest` L145 从最终 body 计算 ETag，不依赖该条件。slug 已在循环里解析过一次，为避免二次 `json.Unmarshal`，建议把 `merged` 改为 `[]struct{ slug string; raw json.RawMessage }` 或并行维护一个 `slugs []string`。

不要动 `openAIConfiguredCodexModelIDsForGroup` 里的 `sort.Strings`：自定义列表关闭时它保证确定性输出，且被其他调用方依赖。

排序只调整原始条目的位置，保留现有模型描述及未知扩展字段，不为排序重建 descriptor。验收目标是下载 JSON 的 `models[]` 顺序；若后续还要求客户端模型选择器顺序一致，另行验证客户端是否按 `priority` 等字段重排，不未经验证就改能力元数据。

### 4.3 副作用（正向）

前端 `selectCodexCatalogModel()` 在首选模型不在目录中时退回目录第 1 项。修复后「第 1 项」= 管理员排在最前的模型，config.toml 的兜底 `model` 也会跟着管理员意图走。

### 4.4 测试（`backend/internal/service/openai_codex_models_service_test.go`）

- 新增：自定义列表 `["b","a","c"]`，账号映射 `a/b/c` → 路径 A 输出 slug 顺序 `b,a,c`。
- 新增：上游目录 `a,b,c` + 自定义列表 `c,a` → 路径 B 输出 `c,a`，且 ETag 与未排序时不同、`changed == true`。
- **必须新增纯重排**：模型集合始终为 `a,b,c`，只把自定义顺序改为 `c,b,a`；A/B 两条路径均输出新顺序。合并路径单测使用无需过滤、注入或 visibility 修改的输入，确保仅排序也产生 `changed == true` 和新 ETag。
- Handler 层验证：重排后携带旧 ETag 请求返回 200 和新内容；再次携带新 ETag 返回 304。不能仅在 helper 层比较哈希。模板：`backend/internal/handler/gateway_models_test.go` L231 `TestGatewayCodexModels_GeneratedManifestUsesFinalBodyETag`（非 OpenAI 路径）；OpenAI 的 `openai_codex_models_handler_test.go` L133 已有 `TestCodexModelsAppliesLocalFiltersBeforeClientETag`（含 200/304 断言），L203 已有 `TestCodexModelsAPIKeyCacheDoesNotLeakGroupFilters`。在这些模式上补充纯重排场景，不把过滤用例视为纯重排覆盖，也不声称目前仅有取消请求测试。ETag 比较用 `codexModelsManifestETagMatches`（L2309）。
- 两个分组共享上游账号/目录缓存但自定义顺序不同，连续和交错请求均保持各自顺序，不污染源目录。**隔离机制已经存在**：`fetchCachedAPIKeyCodexModelsManifest`（L1634）三个返回点都经 `codexModelsManifestForClient` → `cloneCodexModelsManifest`（L2285/L2306），`MergeGroupConfiguredCodexModels` 改的是克隆而非缓存条目。测试的目标是断言这个克隆语义没有被新代码破坏（例如有人为省一次拷贝把 reorder 挪到克隆之前），而不是重新推导隔离。路径 A 不经缓存，无此风险。
- 原始能力字段及未知字段保留；缺失模型、重复选择、过滤后的有效子集、媒体/auto 模型规则保持现有语义，不为补齐顺序引入本来不可见的条目。
- 新增：自定义列表关闭 → 顺序与当前行为一致（回归保护）。
- 现有 `TestBuildGroupConfiguredCodexModelsManifestUsesAdministratorConfiguration` (L1055)、`TestMergeGroupConfiguredCodexModelsInjectsCurrentGroupAliases` (L1004) 若断言了字母序需调整。
- 运行：`cd backend && go test -tags unit -run 'Codex' ./internal/service/ ./internal/handler/`。

前端无改动；「获取目录」原样保存服务端响应。

---

## 5. 品牌 / 赞助商 全项目盘点

统计口径：排除 `backend/ent/`（生成代码）、`backend/internal/web/dist/`（构建产物）、`node_modules`、测试文件、以及 Go import 路径 `github.com/Wei-Shaw/sub2api/...`（约 1500 行，属模块路径不是品牌）。

### 5.1 分级结论

| 级别 | 类别 | 处理建议 |
|------|------|---------|
| **P0** | 运营可见：站名兜底、i18n 文案、README 赞助商、广告位、外链 | 白标必改 |
| **P1** | 生成给终端用户的配置文本（env 变量名、provider 名、注释） | 改名即破坏已有用户配置，需版本说明 |
| **P2** | 元数据：网页标题、logo、TOTP 发行方、WebAuthn 显示名、支付商品名 | 建议改 |
| **P3** | 协议 / 基础设施标识（接口路径、Header、Redis 前缀、镜像名、模块路径） | **建议不改** |
| **法律** | 合规确认短语、法律文档、CLA | 改则前后端必须同步，且有法律语义 |

### 5.2 P0-a：站名兜底字面量 `'Sub2API'`

站名本身已是后台设置（`site_name`），但代码里散落了大量 `|| 'Sub2API'` 兜底。白标时应收敛到单一常量。

**前端**（建议新建 `frontend/src/config/brand.ts` 导出 `BRAND_NAME`）：

`stores/app.ts` L29, L297 · `main.ts` L48（`!== 'Sub2API'` 判断） · `router/title.ts` L10 · `views/HomeView.vue` L511 · `views/KeyUsageView.vue` L434 · `views/auth/RegisterView.vue` L403, L538 · `views/auth/EmailVerifyView.vue` L273, L375 · `views/public/LegalDocumentView.vue` L123 · `components/layout/AuthLayout.vue` L72 · `components/modelPlaza/PlazaNavBar.vue` L55 · `views/admin/SettingsView.vue` L7777（placeholder）, L7799, L9573 · `i18n/*/admin/settings.ts` `siteNamePlaceholder`（zh L601 / en 对应）、`fromNamePlaceholder`（zh L907）。

**后端**（建议 `service` 包加 `const DefaultSiteName`）：

`setting_parse.go` L70, L354 · `setting_public.go` L324 · `setting_features.go` L294 · `auth_service.go` L336, L378, L1554 · `user_service.go` L1279 · `auth_oauth_email_flow.go` L49 · `auth_email_binding.go` L126 · `balance_notify_service.go` L25 · `content_moderation.go` L2070, L2074。邮件模板本身取 `siteName` 参数，不含硬编码。

### 5.2 P0-b：i18n 硬编码品牌文案

zh/en 各约 40 行，建议改为 `{siteName}` 插值（`t('...', { siteName })`），或在文案层面去品牌化。

| 文件 (zh / en) | 行数 | 内容 |
|---|---|---|
| `dashboard.ts` | 9 / 9 | Grok/Codex 配置说明（`SUB2API_API_KEY`、"Sub2API Grok 分组"） |
| `admin/settings.ts` | 8 / 8 | OAuth 描述、上游倍率描述、易支付说明、placeholder |
| `admin/accounts.ts` | 6 / 5 | 用量窗口提示、倍率信任警告、Grok 免费额度、TTS 测试文本 |
| `misc.ts` | 5 / 5 | 引导教程「欢迎使用 Sub2API」及 HTML 正文 |
| `admin/plugins.ts` | 5 / 5 | 插件宿主说明 |
| `admin/overview.ts` | 3 / 3 | 备份桶示例名、Live 说明 |
| `landing.ts` | 2 / 2 | 安装向导标题/描述 |

### 5.2 P0-c：README 赞助商 / 生态 / 外部徽章

| 文件 | 位置 | 内容 |
|------|------|------|
| `README.md` | L30–170 | `## ❤️ Sponsors`，25 家赞助商 `<tr>`，含 `mailto:support@sub2api.org` |
| `README_CN.md` | L31–173 | 同上（中文） |
| `README_JA.md` | L30–172 | 同上（日文） |
| 三份 README | L13 | trendshift 徽章（`Wei-Shaw/sub2api`） |
| 三份 README | `## Ecosystem` / `## 生态项目` | sub2api-mobile、Sub2ApiPay 链接 |
| 三份 README | 末尾 `## Star History` | star-history 图（`Wei-Shaw/sub2api`） |
| 三份 README | L3 | `assets/logo.svg` |
| `assets/partners/logos/` | 30 个文件 | 赞助商 logo（apikey-fun、cctk、etok、qiniu、veilx、RoxyBrowser…） |
| `CLA.md` | 全文 | "Sub2API Individual Contributor License Agreement" |

前端**没有**赞助商展示组件，赞助信息只在 README。

### 5.2 P0-d：广告位与外链

| 文件 | 位置 | 内容 | 建议 |
|------|------|------|------|
| `components/common/ProxyAdBanner.vue` | L4 | 指向 `https://sub2api.io/proxyip` 的横幅；被 `EditAccountModal`、`CreateAccountModal`、`ProxiesView` 引用 | 白标应移除或改为可配置 |
| `components/admin/TLSFingerprintProfilesModal.vue` | L148 | `https://tls.sub2api.org` | 改为可配置或移除 |
| `components/layout/AppHeader.vue` | L168 | GitHub `Wei-Shaw/sub2api` 链接 | 改为设置项或移除 |
| `views/HomeView.vue` L530、`views/KeyUsageView.vue` L437 | `githubUrl` | 同上 |
| `views/admin/SettingsView.vue` | L8860–8867 | 支付文档链接指向上游仓库 `docs/PAYMENT*.md` | 改为相对路径或自有文档 |
| `stores/adminCompliance.ts` L60–61、`components/admin/AdminComplianceDialog.vue` L130–132 | 合规文档 URL 兜底指向上游仓库 | 见「法律」 |
| `components/common/VersionBadge.vue` | L654–656 | `GITHUB_REPO = 'Wei-Shaw/sub2api'`、`DOCKER_IMAGE = 'weishaw/sub2api'` —— **版本更新检查源** | 白标后需指向自己的发布源，否则提示升级到上游版本 |
| `backend/internal/service/update_service.go` | L33 | `githubRepo = "Wei-Shaw/sub2api"`（自更新下载源） | 同上；`repository/github_release_service.go` L100 UA `Sub2API-Updater` |
| `deploy/install.sh` | L5, L34 | 安装脚本 curl 源与 `GITHUB_REPO` | 同上 |
| `backend/internal/config/config.go` | L2310–2311 | `pricing.remote_url` 指向 `Wei-Shaw/model-price-repo` | **数据源，不是品牌，保留** |

### 5.3 P1：生成给终端用户的配置文本

`frontend/src/components/keys/UseKeyModal.vue`（24 处）：

- 环境变量名 `SUB2API_API_KEY`（L1150/1155/1159/1239/1240/1252 及 i18n 说明 6 处）
- `model_provider = "sub2api"`、`[model_providers.sub2api]`（L1168/1179/1243/1249）
- `name = "Sub2API Grok"` / `"Sub2API ${label}"` / `'Grok via Sub2API'`（L1180/1250/1821）
- TOML 注释若干（L1021–1044、L1115、L1162、L1189、L1242）

`backend/internal/service/openai_codex_models_service.go` L308, L432, L446, L459, L470：Codex 目录 description "…routed through Sub2API."。

`backend/internal/service/account_test_service.go` L92 + i18n `accounts.ts` L779–780：TTS 连通性测试默认文本。

**风险**：改 `SUB2API_API_KEY` / `model_providers.sub2api` 会让所有已按旧模板配置的用户 Codex 失效。建议：品牌名可改，**变量名与 provider id 保留**，或提供过渡期双写说明。

### 5.4 P2：元数据

| 项 | 位置 |
|----|------|
| 网页 `<title>` | `frontend/index.html` L7 `Sub2API - AI API Gateway` |
| favicon / logo | `frontend/public/logo.svg`、`assets/logo.svg` |
| TOTP 发行方 | `backend/internal/service/totp_service.go` L96 `totpIssuer = "Sub2API"`（改后用户验证器里显示名变化，旧条目不受影响） |
| WebAuthn RP 显示名 | `config.go` L2061 `webauthn.rp_display_name` |
| 支付商品名 | `payment_order.go` L538, L550 `"Sub2API Subscription …"`（`SettingsView` L7799 已支持 `payment_product_name_prefix` 覆盖） |
| CLI 向导横幅 | `setup/cli.go` L54；`cmd/server/main.go` L65, L115 |
| 邮箱域名默认值 | `admin@sub2api.local`（`setup.go` L596、`.env.example` L200、compose） |

### 5.5 P3：协议与基础设施标识（建议保留）

改这些不会改善品牌观感，却会破坏兼容性或造成大范围 diff：

| 类别 | 项 | 原因 |
|------|----|------|
| 实例间协议 | `/v1/sub2api/billing`、`object: "sub2api.key_billing"`（`api_key_auth.go` L168、`upstream_billing_probe.go`、`types/index.ts` L1038） | 上下游 sub2api 实例互探计费用，改名即互不识别 |
| 插件协议 | manifest `requires.sub2api`、`recommended_sub2api_version`、`tested_sub2api_versions`（`pluginapi/v1/manifest.schema.json`，`api/admin/plugins.ts`） | 第三方插件包格式 |
| 客户端协商 | WS 子协议 `sub2api-admin`（`ops_ws_handler.go` L53）；Header `X-Sub2API-Grok-Client-Tool-Cache`（`openai_gateway_grok_cache.go` L19）；插件 UI 桥 `sub2api-plugin-host/ui`、`sub2api.plugin.ready`（`PluginsView.vue`） | 前后端/插件握手常量 |
| 数据格式 | 导入导出类型 `sub2api-data` / `sub2api-bundle`（`ImportDataModal.vue` L227）；导出文件名前缀 | 影响旧备份文件可导入性 |
| 存储键 | Redis `dashboard_cache.key_prefix = "sub2api:"`；localStorage `sub2api_locale`、`sub2api_login_agreement_consent`、`sub2api:ip-geo-cache:v1`；IndexedDB `sub2api-batch-image-preview-cache`；Web Lock `sub2api-auth-token-refresh` | 改则用户本地状态丢失一次 |
| 稳定 ID 派生盐 | `openai_codex_fingerprint.go` L239–257、`openai_codex_transform.go` L120 `"sub2api:codex-…"` | **绝不能改**：改了所有账号的 Codex 设备指纹全部变化 |
| 部署名 | 镜像 `weishaw/sub2api`、容器/网络 `sub2api*`、DB 名/用户 `sub2api`、`/opt/sub2api`、`/etc/sub2api`、systemd `sub2api`、日志 `sub2api.log`、socket `/tmp/sub2api-*.sock`（deploy/ 7 个脚本共 143 处、`.goreleaser*.yaml`、`config.go`、`logger/options.go`） | 可改但需整套迁移文档；建议作为独立「部署重命名」任务 |
| Go 模块路径 | `github.com/Wei-Shaw/sub2api`（约 1500 import 行 + `go.mod`） | 一次性 `sed` 可完成，但之后与 upstream 每次合并都冲突；**强烈建议不改** |

### 5.6 法律 / 合规

| 项 | 位置 | 注意 |
|----|------|------|
| 项目许可证 | 根目录 `LICENSE` | 与运营品牌分开分类；默认保留原文，不纳入全局改名或删除。发布前单独复核适用要求 |
| 原作者及第三方声明 | 源码头部、文档、资源和依赖中的 copyright / license / notice | 建立保留清单；赞助展示与授权/署名声明不能混为一类，不因品牌替换自动删除 |
| 管理员合规确认短语 | `backend/internal/service/admin_compliance.go` L22–23；前端兜底 `stores/adminCompliance.ts` L6–7 | 用户须逐字输入；改品牌名后两处**必须同步**，且已确认过的管理员不会被要求重新确认（按现有逻辑核实） |
| 合规文档 | `docs/legal/admin-compliance.zh.md` / `.en.md`（各 4 处品牌） | 文档 URL 兜底见 5.2-d |
| README 重要提醒 | 三份 README L21–28 | 免责声明，白标后仍应保留同等声明 |
| CLA | `CLA.md` | 属贡献协议，不是营销文案；保留/替换/移除需单独确认，不因 fork 或停止接收贡献自动删除 |

### 5.7 白标实施建议（分三步，按合并冲突风险递增）

以下仅供未来单独批准的白标任务参考，本次不执行：

1. **低代码耦合层**：README 三份重写（按确认范围去赞助商/徽章/生态）、清理无引用的 `assets/partners/`、移除 `ProxyAdBanner` 引用、`index.html` 标题、logo 替换。仍需检查资源引用、许可证/署名保留及页面回归，不能称为零风险。
2. **常量收敛层**：新增 `frontend/src/config/brand.ts` 与后端 `DefaultSiteName`，把 5.2-a 的兜底字面量替换为常量引用。GitHub 展示链接与可执行更新源分别处理：`VersionBadge`、`update_service.go`、`install.sh` 的发布源切换须验证版本接口、产物命名、下载/升级和回滚流程，不能当作普通文案替换。
3. **文案层**：5.2-b 的 i18n 改插值；P1 的配置模板注释与 provider 显示名去品牌（保留变量名）；P2 元数据。

**不做**：P3 全部、Go 模块路径、稳定 ID 盐。

### 5.8 运行时品牌值与存量实例

仅修改代码默认值不会替换数据库中已经保存的设置。盘点需区分「源码兜底」「新安装默认值」「存量实例配置」，至少覆盖站名、Logo、首页自定义内容、邮件发件人、支付商品前缀及可配置外链，记录实际字段、读取入口和默认值优先级。

存量值只在获得授权后按明确字段只读核查，不导出整个设置表或凭据。未来迁移需给出值级变更范围、备份及回滚方案，不能批量覆盖用户自定义内容；新安装和已有实例分别验收。

### 5.9 可复核的盘点交付

本次交付明细至少包含：文件/符号、原始文案或资源、展示入口、分类、建议保留或替换、兼容性风险、是否等待确认。与 §5.8 的运行时清单分开记录。数量和行号是快照，不作为全量覆盖的唯一证明。

从仓库根目录复跑检索，逐项分类命中；排除生成物，但保留测试及模块路径命中作为兼容性复核，不直接计入营销文案数量：

```bash
rg -n -i --hidden \
  -g '!.git/**' -g '!node_modules/**' -g '!frontend/node_modules/**' \
  -g '!backend/ent/**' -g '!backend/internal/web/dist/**' -g '!frontend/dist/**' \
  -g '!pnpm-lock.yaml' -g '!frontend/pnpm-lock.yaml' \
  'sub2api|wei-shaw|weishaw|赞助|sponsor|partners|copyright|license|notice' .

rg --files --hidden -g '!.git/**' -g '!**/node_modules/**' \
  | rg -i '(logo|favicon|partners|sponsor|license|copying|notice|readme|cla\.md)'
# docs/* 大部分仍被忽略（本文档已单独放行），文档限定范围单独复核。
rg --no-ignore -n -i -g '*.md' \
  'sub2api|wei-shaw|weishaw|赞助|sponsor|copyright|license|notice' docs
```

检索仅用于发现候选项，不自动替换。图片内嵌文字和运行时自定义内容需另行目视/只读核查；本次盘点文档自身的命中标为说明资料，不循环作为待改品牌项。

未安装 `rg` 时可使用以下 Git/grep 替代流程，不依赖特定机器上的 shell 包装。先扫描受版本控制的文件，再显式补查文档，避免递归扫描整个仓库时意外输出被忽略的本地凭据。范围与上面的 `rg` 并非逐文件完全等价：新增未跟踪源码/资源需另外检查。

```bash
git grep -n -i -I -E \
  'sub2api|wei-shaw|weishaw|赞助|sponsor|partners|copyright|license|notice' \
  -- . ':!backend/ent/**' ':!backend/internal/web/dist/**' ':!frontend/dist/**' \
  ':!frontend/pnpm-lock.yaml' ':!pnpm-lock.yaml'
grep -rnIE --include='*.md' \
  'sub2api|wei-shaw|weishaw|赞助|sponsor|copyright|license|notice' docs
git ls-files | grep -iE '(logo|favicon|partners|sponsor|license|copying|notice|readme|cla\.md)'
```

---

## 6. 建议的提交拆分

| 提交 | 内容 | 依赖 |
|------|------|------|
| 1 | `feat(keys): remove CC-Switch import`（§1 前端 + 方案 A） | — |
| 2 | `feat(keys): bump default models for Codex/OpenCode setup`（§2） | — |
| 3 | `feat(keys): add per-file download to setup snippets`（§3） | 建议在 2 后，复用更新后的生成器与测试 |
| 4 | `fix(codex): order group catalog by custom models list`（§4，纯后端） | — |
| 5 | 品牌盘点明细及复核方法（仅文档） | 包含 §5.8–5.9；首次提交同时包含白名单（见 §0）。`git status` 核对可见性，实际暂存后再用 `git diff --cached --name-only` 核对提交范围 |
| 后续独立任务 | 品牌重塑按 §5.7 分步实施 | 需另行批准品牌名与策略，不属于本次功能实施 |

2、3 同时修改 `UseKeyModal.vue` 及其测试，建议顺序实施，不视为无冲突并行任务。采用 CCS 方案 A 且未新增模型后端支持时，4 是唯一后端改动，可独立验证；上线仍须另行批准，不由本计划自动触发。

## 7. 验证清单

```bash
# 前端
pnpm -C frontend typecheck
pnpm -C frontend test:run UseKeyModal KeysView SettingsView app.spec localesNoKeyCollision
pnpm -C frontend build

# 后端
cd backend && go build ./... && go vet -tags unit ./internal/...
go test -tags unit -run 'Codex' ./internal/service/ ./internal/handler/

# 手工
# 1. /keys 操作列不再出现「导入到 CCS」；Antigravity 分组点「使用密钥」正常
# 2. OpenAI 分组 → Codex CLI：默认 model=review_model=gpt-5.6-sol；目录兜底后两者仍同值；
#    Anthropic 分组 → Codex CLI（Windows tab）：model=claude-sonnet-5
# 3. 每个 config.toml / opencode.json 卡片有「下载」，文件名与内容正确
#    TOML 根级字段在首个 table 之前，解析后属于根级；JSON 可解析
#    检查目录未获取/失败/空目录与配套 auth.json、环境变量流程
# 4. 管理端调整某 OpenAI 分组自定义模型列表顺序 → /keys「获取目录」→ 下载的 codex-models.json
#    models[] 顺序与管理端一致；关闭自定义列表后顺序回到原行为
#    仅重排不改变模型集合时同样生效，旧/新 ETag 与分组隔离正确
# 5. 中英文、Windows/Unix、窄屏界面及实际文件下载回归
# 6. CCS 原值 true/false 保存无关设置后均不变；品牌盘点不执行实际替换
```

验收记录应包含命令结果、浏览器/客户端版本和未验证项。当前计划修订未执行上述功能测试；不得把测试清单当作通过记录。新增解析器测试或国际化用例必须实际被测试命令选中。

## 8. 待用户决策汇总

1. §1.3：推荐方案 A（移除入口和可见开关，保留字段往返）；若选择 B，另行确认兼容性清理范围。
2. §2.1：Antigravity 的 Codex 默认模型是否同步改为 `claude-sonnet-5`。
3. §2.2：Composite 的 Codex 默认模型是否改为 `gpt-5.6-sol`。
4. §2.3：核实 `gpt-6-astra` 上下文/输出/variants 来源及实际定价；参数不靠用户选择或猜测确定。若存在后端支持缺口，单独立项，不默认补兜底价。
5. §3.2–3.4：下载是否扩展到 `settings.json`、Antigravity 双 OpenCode 配置的命名及加载方式；目录未就绪时下载行为和配套文件流程。
6. §5：本次仅完成品牌/赞助/运行时/许可证分类盘点；未来实际品牌替换、广告和更新源调整、部署迁移须单独批准。
