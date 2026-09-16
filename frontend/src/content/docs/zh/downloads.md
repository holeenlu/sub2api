## 客户端工具

下载前先阅读 [控制台配置器](/apps/console) 与对应的 [应用教程](/apps)，确认分组和客户端支持范围。

| 文件 | 用途 | 默认行为 |
| --- | --- | --- |
| [Codex 会话修复包](/downloads/session-repair.zip) | macOS、Linux、Windows Codex 会话诊断与修复 | 只读诊断，显式 Apply 才修改 |
| [Claude Code 会话恢复包](/downloads/claude-session-recovery.zip) | 查找本机 Claude 会话并生成精确恢复命令 | 只读扫描，不启动 Claude、不修改会话 |
| [GPT Image 2.5 Flare Skill](/downloads/gpt-image-flare.zip) | Codex 图片生成与编辑 | 固定 Flare 模型 |
| [GPT Image 2.5 Sunburst Skill](/downloads/gpt-image-sunburst.zip) | Codex 图片生成与编辑 | 固定 Sunburst 模型 |

## 下载后检查

压缩包属于当前网站的静态发布资源。安装前解压并检查顶层目录中的 `README.md` 或 `SKILL.md`；脚本不应包含你的 API Key。Codex 修复包仅操作本机 Codex 数据；Claude 恢复包只读扫描本机 Claude 会话并输出恢复命令；图片 Skill 会使用当前 Codex Provider 发送一次明确的计费请求。

解压前，请使用 [SHA256SUMS.txt](/downloads/SHA256SUMS.txt) 校验下载文件。

不要从聊天记录、网盘或非本站镜像安装同名脚本。升级包前保留原 Skill 目录与 `~/.codex` 备份。
