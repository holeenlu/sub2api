# 分支结构与品牌交付模型

自 2026-10-22 起，三个交付分支各为“最新已核对的 Wei-Shaw/main + 一笔完整定制提交”。上游原始历史不改写，定制功能不再分散在上游 merge 节点和多个本地提交之间。

```text
Wei-Shaw/main（固定 SHA）
  ├── main：共享功能 + KDAN 完整定制（一笔）
  ├── TapModels：共享功能 + TapModels 完整定制（一笔）
  └── tokensavy：共享功能 + Tokensavy 完整定制（一笔）
```

| 分支 | 定位 | 推送目标 |
| --- | --- | --- |
| upstream-base | 已核对官方基线，仅本地记录 | 不推送 |
| main | 共享行为先审查、KDAN 主站 | holeenlu/sub2api 的 origin/main |
| TapModels | 共享行为与 TapModels 品牌完整结果 | 同仓库 origin/TapModels |
| tokensavy | 共享行为与 Tokensavy 品牌完整结果 | 同仓库 origin/tokensavy |

共享行为先在 main 完成，品牌专属配置不扩散。品牌默认值继续由 `backend/internal/service/brand.go`、`frontend/src/config/brand.ts` 和运行时设置承载。三个站点保留独立部署、资产、法律文案、安装包和默认值。

同步采用三方文件树整合，最终压缩为以上结构。转换后 main 不是品牌分支祖先，不能仅凭祖先关系断言品牌缺功能，也不能直接 merge 旧品牌历史来补齐。利用上次发布的 main/品牌树及记录计算共享增量、保留品牌差异，详见 [单一定制提交](CHANGE_DELIVERY.md#单一定制提交2026-10-22)。

非快进交付先备份并核验完整预期树，再使用精确旧 SHA 的 lease 和完整 refspec，优先原子更新三个目标。最后核验实时远端、三个正式本地分支和工作区；不改写有效版本标签、Release 或其他任务现场。

累计功能说明见 [定制 Release Note](CUSTOMIZATION_RELEASE_NOTES.md)，提交正文携带完整说明。发版仍由 `scripts/release/channels.json` 的 main/kdan、TapModels/tapmodels、tokensavy/tokensavy 渠道人工触发；提交和推送不发版、不部署、不执行数据库迁移。
