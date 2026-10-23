# TapModels · 客户端安装、示例与下载资源

## 需求与最终方案

保留来源 87fb1f2613667cb250525af8a4c7c0c12f03c2f6 的最终品牌行为；从已重建 main 继承同一份共享功能与规则。详见本归档 BRAND-TapModels.md。此组仅落以下文件的最终 mode/blob，源码、资源及配置不重新实现。

## 验收边界

完整品牌提交集应与原品牌树逐对象相同，额外仅有本次需求归档与两项共享规则；不能从单个品牌分类提交推断独立部署就绪。没有推送、发布或部署。

## 文件归属

- M frontend/public/downloads/SHA256SUMS.txt
- M frontend/public/downloads/gpt-image-flare.zip
- M frontend/public/downloads/gpt-image-sunburst.zip
- D frontend/public/downloads/kdan-claude-session-recovery.zip
- D frontend/public/downloads/kdan-claude-session-recovery/Find-ClaudeSessions.ps1
- D frontend/public/downloads/kdan-claude-session-recovery/README.md
- D frontend/public/downloads/kdan-claude-session-recovery/find-claude-sessions.sh
- D frontend/public/downloads/kdan-claude-session-recovery/find_claude_sessions.py
- D frontend/public/downloads/kdan-codex-session-repair.zip
- D frontend/public/downloads/kdan-codex-session-repair/README.md
- D frontend/public/downloads/kdan-codex-session-repair/RepairSessions.ps1
- D frontend/public/downloads/kdan-codex-session-repair/repair-sessions.sh
- D frontend/public/downloads/kdan-codex-session-repair/repair_sessions.py
- D frontend/public/downloads/kdan-codex-session-repair/resume_sessions.py
- D frontend/public/downloads/kdan-image-skills/gpt-image-flare/SKILL.md
- D frontend/public/downloads/kdan-image-skills/gpt-image-flare/agents/openai.yaml
- D frontend/public/downloads/kdan-image-skills/gpt-image-flare/requirements.txt
- D frontend/public/downloads/kdan-image-skills/gpt-image-flare/scripts/generate.py
- D frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/SKILL.md
- D frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/agents/openai.yaml
- D frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/requirements.txt
- D frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/scripts/generate.py
- A frontend/public/downloads/session-repair.zip
- A frontend/public/downloads/session-repair/README.md
- A frontend/public/downloads/session-repair/RepairSessions.ps1
- A frontend/public/downloads/session-repair/repair-sessions.sh
- A frontend/public/downloads/session-repair/repair_sessions.py
- A frontend/public/downloads/tapmodels-claude-session-recovery.zip
- A frontend/public/downloads/tapmodels-claude-session-recovery/Find-ClaudeSessions.ps1
- A frontend/public/downloads/tapmodels-claude-session-recovery/README.md
- A frontend/public/downloads/tapmodels-claude-session-recovery/find-claude-sessions.sh
- A frontend/public/downloads/tapmodels-claude-session-recovery/find_claude_sessions.py
- A frontend/public/downloads/tapmodels-codex-session-repair.zip
- A frontend/public/downloads/tapmodels-image-skills/gpt-image-flare/SKILL.md
- A frontend/public/downloads/tapmodels-image-skills/gpt-image-flare/agents/openai.yaml
- A frontend/public/downloads/tapmodels-image-skills/gpt-image-flare/requirements.txt
- A frontend/public/downloads/tapmodels-image-skills/gpt-image-flare/scripts/generate.py
- A frontend/public/downloads/tapmodels-image-skills/gpt-image-sunburst/SKILL.md
- A frontend/public/downloads/tapmodels-image-skills/gpt-image-sunburst/agents/openai.yaml
- A frontend/public/downloads/tapmodels-image-skills/gpt-image-sunburst/requirements.txt
- A frontend/public/downloads/tapmodels-image-skills/gpt-image-sunburst/scripts/generate.py
- A frontend/public/downloads/tapmodels-session-repair/README.md
- A frontend/public/downloads/tapmodels-session-repair/Repair-TapModelsSessions.ps1
- A frontend/public/downloads/tapmodels-session-repair/repair-sessions.sh
- A frontend/public/downloads/tapmodels-session-repair/repair_sessions.py
- A frontend/public/downloads/tapmodels-session-repair/resume_sessions.py
- M frontend/public/install/README.md
- M frontend/public/install/claude-code.sh
- M frontend/public/install/codex.sh
- M frontend/src/components/keys/UseKeyModal.vue
- M frontend/src/components/keys/__tests__/UseKeyModal.spec.ts
- M tools/build_docs_downloads.py
- M tools/tests/test_claude_session_recovery.py
- M tools/tests/test_docs_downloads.py
- M tools/tests/test_image_skills.py
- M tools/tests/test_session_recovery_commands.py
- M tools/tests/test_session_repair.py
- M tools/zh-tw/README.md
