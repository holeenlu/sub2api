# TapModels 文档与下载包验收

日期：2026-09-15。范围：本地 TapModels 文档、图片 Skill 与会话恢复下载包。线上发布另行记录。

原独立验收任务「TapModels 文档与下载包独立验收」提出 3 个 P1、1 个 P2；上一轮协调任务未读取并关闭报告就结束汇报，原有 17 条脚本测试不能证明这四项已修复。

| 编号 | 原始问题 | 修复负责人 | 状态 |
| --- | --- | --- | --- |
| P1-1 | 教程仅设置 TAPMODELS_API_KEY，但图片脚本因此跳过 Provider 地址并要求 BASE_URL | Sol 图片修复任务 | 修复中 |
| P1-2 | 三个 ZIP 缺命名顶层目录，教程命令路径不存在，两种 Skill 会相互覆盖 | 协调器 | 修复中 |
| P1-3 | 显式数据库的父目录链接可逃逸 Codex home | Sol 恢复修复任务 | 修复中 |
| P2-1 | 备份先于写锁，指纹只覆盖三个列，无法保证回滚快照包含并发更新 | Sol 恢复修复任务 | 修复中 |

## 验证命令

在仓库根目录执行，全部使用临时目录、模拟服务或静态包，不操作真实会话数据，不调用付费图片接口。

```bash
python3 tools/build_docs_downloads.py
python3 tools/build_docs_downloads.py --check
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest tools.tests.test_image_skills tools.tests.test_session_repair tools.tests.test_docs_downloads
```

打包必须在所有源文件完成后执行，源码改变会使 `--check` 和包一致性测试失败。ZIP SHA256 记录在 `frontend/public/downloads/SHA256SUMS.txt`。

## 独立复验

待修复完成后由原验收任务读取最终源码与 ZIP，逐项复验。复验完成前，不宣称四项已关闭或下载包已上线。
