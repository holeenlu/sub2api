# 三品牌共享工具对齐

`main` 是 KDAN 品牌主分支。共享能力先在 main 落地，再普通 merge 到
TapModels 和 tokensavy；品牌名称、环境变量和下载目录保留各自配置。

2026-10-03 对齐范围：

| 能力 | 统一行为 | 来源 |
| --- | --- | --- |
| 图片 Skill 下载 | 构建 Flare、Sunburst ZIP 和 SHA256 清单；按本品牌环境变量或客户端配置读取 Provider | TapModels `74fda25d6` 的下载源码与测试，main 适配为 KDAN |
| 繁体文档 | `gen-locale.mjs` 同时生成语言包、教程 Markdown 和法律文件；`--check` 检查是否过期 | TapModels `74fda25d6` |
| Docker 构建 | pnpm 固定为 9.15.9，Corepack 使用传入的 npm registry；包含运行时 Markdown，排除本地凭据与交付资料 | tokensavy `83c4d1c91`，去除冗余的品牌专属排除项 |
| 会话回滚 | 预览及执行时均核对备份记录的数据库身份，事务内再次核对，拒绝数据库替换 | main `05ae508ac` 的 `resolve_backup` / `apply_rollback`，补充预览拒绝测试 |

会话工具的品牌目录和备份前缀保留兼容性，不能因统一实现而更换已有备份前缀。
各分支下载包从本分支源码重新生成，ZIP 校验值因品牌内容不同而不同。

验证命令（各分支工作目录）：

```sh
python3 tools/build_docs_downloads.py --check
python3 -m unittest tools.tests.test_image_skills tools.tests.test_session_repair tools.tests.test_docs_downloads tools.tests.test_session_recovery_commands tools.tests.test_claude_session_recovery
node tools/zh-tw/gen-locale.mjs --check
python3 -m unittest deploy.tests.test_auto_release_workflow
```

图片测试只访问本机 HTTP 夹具，不请求收费模型服务。Docker 前端构建另行验证
`pnpm run build`、类型检查及实际 Markdown 导入；构建上下文使用合成凭据夹具检查排除规则。
本次同步不改变人工发版入口，也不执行发版、部署或数据库迁移。
