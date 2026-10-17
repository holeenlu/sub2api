# TapModels 文档与下载包验收

日期：2026-09-15。范围：本地 TapModels 文档、图片 Skill 与会话恢复下载包。线上发布另行记录。

原独立验收任务「TapModels 文档与下载包独立验收」提出 3 个 P1、1 个 P2；上一轮协调任务未读取并关闭报告就结束汇报，原有 17 条脚本测试不能证明这四项已修复。

| 编号 | 原始问题 | 修复负责人 | 状态 |
| --- | --- | --- | --- |
| P1-1 | 教程仅设置 TAPMODELS_API_KEY，但图片脚本因此跳过 Provider 地址并要求 BASE_URL | Sol 图片修复任务 | 原独立验收任务复验关闭 |
| P1-2 | 三个 ZIP 缺命名顶层目录，教程命令路径不存在，两种 Skill 会相互覆盖 | 协调器 | 原独立验收任务复验关闭 |
| P1-3 | 显式数据库的父目录链接可逃逸 Codex home | Sol 恢复修复任务 | 原独立验收任务复验关闭 |
| P2-1 | 备份先于写锁、摘要范围不足、旧备份可用于已替换数据库 | Sol 恢复修复任务 | 原独立验收任务及本轮 Sol 独立复验关闭 |
| P1-4 | 非 OpenAI/Composite 分组的 Codex 模型目录按钮必然 404，生成配置仍引用不存在的目录文件 | Sol API 修复任务 | 修复中 |
| P2-2 | Gemini countTokens 未说明 Antigravity OAuth 路径固定返回占位 0 | Astra | 文档已修正，待复验 |

## 验证命令

在仓库根目录执行，全部使用临时目录、模拟服务或静态包，不操作真实会话数据，不调用付费图片接口。

```bash
python3 tools/build_docs_downloads.py
python3 tools/build_docs_downloads.py --check
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest tools.tests.test_image_skills tools.tests.test_session_repair tools.tests.test_docs_downloads
```

打包必须在所有源文件完成后执行，源码改变会使 `--check` 和包一致性测试失败。ZIP SHA256 记录在 `frontend/public/downloads/SHA256SUMS.txt`。

## 独立复验

原独立验收任务 `01a0a32f-db90-7b32-9d80-b3dd8627afcc` 于本轮确认原三项 P1 和回滚 P2 关闭，并指出新 P1-4。本轮独立 Sol 复验另发现 P2-2。应用任务读取工具未返回报告正文，协调器从该任务本地会话记录的最终消息取得正式报告，未把空返回视为通过。

本轮本地验收已覆盖首页双入口、`/docs` 与 `/apps` 独立导航/搜索、旧链接重定向、390px 手机布局、三语言文章完整性和实际配置器截图。截图使用无效示例 Key 与保留演示域名，可打开原图；来源见 `frontend/public/docs-assets/README.md`。

下载包原始源码及 ZIP 一致性、安装目录结构、权限、SHA256 以及修复脚本边界合计 39 项测试通过。前端相关 111 项测试、类型检查与 Vite 构建通过。本地缺少可用业务后端，未以真实 Key 调用模型或验证线上服务；没有部署或数据库迁移。

会话恢复 ZIP SHA256：`a31fca1ac86521dbff5ee99a39e920a0866457bdb7fc66e4d76dc5170d826c93`。源文件不变时保持此冻结包，最终以 `SHA256SUMS.txt` 和 `--check` 为准。
