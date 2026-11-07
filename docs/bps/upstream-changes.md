# BPS 来源记录入口

本路径由原始 NOTICE 引用，保留稳定导航。现行来源范围与适配边界见 [来源说明](../SOURCE_ADAPTATIONS.md)，当前需求见 [R09](../ENGINEERING.md#req-bps)。

旧逐轮核查正文已并入历史；原文件 blob、来源 SHA 和引用在 [来源索引](../customization/documentation-sources.tsv)，授权及源码归档不改写。integration-state.json/source-inventory.json 保留原核查时点，不据其早期默认值推断当前设置。

## 2026-11-06 五源同步

本批从 NewAPI `54f179cde11fef387ff42fcd85a06bc9aa2eb0fa` 构建隔离候选，官方父基线更新为 `0eee80ba87aa4f3aff7282bf1695aa6dc0471c36`。本轮实际来源、行为比较及延期主题见 [现行来源适配](../SOURCE_ADAPTATIONS.md)，完整累计说明见 [Release Note](../CUSTOMIZATION_RELEASE_NOTES.md)。未重新引入 full_access、原值 turn ID、Mihomo、自动运维、票据、VM/Node 或平行网关。

本地细目与核验回执保存在主目录 `.release/upstream-sync/sync-five-20261106/`；该忽略目录是本机证据，不作为远端文件链接。旧 integration-state.json 继续表示原归档专题，不改写为整仓完成水位。本轮未执行已退役的三品牌 checkpoint 脚本。
