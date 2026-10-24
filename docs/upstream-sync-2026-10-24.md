# 五源同步行为对照（2026-10-24）

固定来源及本地基点见 state.json。共享改动交付 main/TapModels/tokensavy。官方/ranxi/New API 无新增，复用前轮有界审查，不声称来源全历史已审。CPR从76291f44比较至e033e219，vm2api按六项能力33e1f0a6与遥测819ee2a7分别比较至3c45c685；参考点不等于全仓水位。vm2api作者书面商用授权范围由用户明确确认，原LICENSE/NOTICE保留，不修改独立vm2api项目。

## 实施前决定
- CPR2818edb9：现有i18n已按需加载并有zh-TW→zh→en回退，组件防连点已限制普通UI并发；不能声称所有用户快速点击必然有竞态。来源的pending合并/最后选择生效和失败反馈可融入既有加载器；保留本地回退、运行时站名与标题、整页刷新，不采用缺文案显示空白。加载失败保持旧语言并恢复按钮可重试，不新增独立状态服务。
- vm2api98d2357f：本地Antigravity isWebSearchTool按名称误判调用方带schema的函数，导致丢schema及切换内置搜索模型。融合来源语义：显式内置type优先；同名客户端工具有真实schema时保留函数（custom读取其已有Custom spec），不以是否有description拒绝合法空描述。直接修正唯一分类器，模型选择与buildTools同时受益，不新增工具配置。原生Anthropic/Responses转换按type区分已有覆盖。
- vm2api45a6bf33：本地fullClaudeTelemetryEnv复制设备硬件字段并复用同一字段清单校验导入。用户已明确选择只移除遥测发送字段，保留设备包存储/导入和推理指纹。不删除共享校验字段，不清除历史设备数据；从发送投影剔除11项CPU/内存/MAC/DMI字段，同时保留事实环境和合成process现有合同。
- vm2api a38680c0/7be30d16：用户明确选择本轮采用可配置上传间隔。现有OpsAdvancedSettings为唯一配置owner，增加account_telemetry_upload_interval_seconds，60–86400秒、默认600秒。旧配置缺失该新字段采用600；已保存合法值优先；热更新以既有LastBatch计算，不重置发送时钟、不补发积压，不改变10分钟活动窗口、首次立即发送、6小时eval和默认发送关闭。仅在既有高级设置增加输入，不能引入来源worker.json/节点控制。
待验证：相关前端i18n/切换/高级设置测试、类型检查、受影响Antigravity转换和遥测/配置生命周期回归；热更新并发按定向race验证。无SQL/schema变化，不启动数据库，不执行真实上游/生产设置。

## 已覆盖、排除与保留研究
- 官方Wei-Shaw/main完整增量为空；ranxi/production与上轮7c6851cae相同，复用review-20261023-3的owner原子写入覆盖、质量运营/多应用fixture排除。New API/main与33c61a24相同，复用网关范围G01–G11及调度/恢复证据；不把所有未审历史推进为已核查。
- CPR1d1f34ba/e033e219为来源专用文案、图标、布局与Tooltip微调，不移植@codex-proxy/ui页面或覆盖本地品牌；2818edb9只吸收加载生命周期，不取消zh-TW回退。
- vm2api143abdb0：来源由逐data行commit改为延迟到真实输出。本地共享SSE读泵、openai_forward_timing.go中的语义输出判定及提交屏障已覆盖created/queued/metadata、空delta、托管工具未知执行及partial usage，不能降级为来源逐行拼帧和宽泛done判断。保留本地单一路径；无重放契约不调整。
- vm2api8185f455/8e5f463e：本地gateway_anthropic_passthrough、openai_gateway_messages_anthropic_native在出站最终应用账号HeaderOverrides，gateway_beta集中处理多值beta及context-1m政策，gateway_request按最终beta净化body。原生/APIKey路径与OAuth采样指纹的合同不同，来源的全体anthropic-/x-stainless-*透传不能直接放宽本地白名单；保留已验证本地策略。Responses→Anthropic函数按type分支，客户端web_search名称无误改写，仅Antigravity补缺。
- vm2api official_303/缓存断点/cli-node CCH补丁：来源频繁切换207/296/303协议档和预设、移除自定义system分支并恢复双user缓存标记；本地已验证Linux指纹、管理员保存blocks与原生CCH签名为当前合同。未取得相同平台整套新采样及完整工具/缓存结果，不仅凭版本号替换。保留现有自定义模板和签名/线程beta适配；新303档/提示词布局仍待独立验证，不标成等效覆盖。
- vm2api thinking summarized默认与eager_input_streaming改默认：是来源模型/产品选择，本地保留客户端显式thinking与现有beta对称约束，不无证据改写显式值；来源服务器CLI payload、hotfix/二进制钉版、Node/VM/guest/SeatPlanner、共享RT文件、登录终端和集群部署/CI/Release不引入。源模型目录/Images页面映射属于独立目录产品；本地已有图片入口/计费和映射，不照搬其经营配置。
- vm2api a54c12d1：来源异步worker health探测之后重新取now，避免过期cooldown重新排队；本地原生调度无该worker-hop等待队列，已有资格复核/模型健康隔离，未发现同一陈旧时间缺口。来源JEV将system+历史扁平文本改为标注来源；本地语义审核严格只取规定用户内容、distillation单独含system/developer，规则owner不同，不扩大送审范围。JEV其他历史用例未评估不等于排除。
- vm2api3c45c685/01c61b2a/b22ea293：区分VM worker envelope与CLI最终出站日志、普通日志默认normal；本地没有该双层worker，已有出站脱敏/有界诊断，不导入原始完整headers或放宽token保护。来源租户/账号属主修复59f94a4f/45d6b308以VM/managed-key为边界，本地采用原生用户/供应商树和response owner校验，不引入平行属主模型；来源Images新增端点和费率不整表复制。
- vm2api遥测字段与间隔为用户明确本轮选择。当前Node telemetry-env中的EVENT_BATCH_INTERVAL_MS仍10秒，而来源Go worker/runtime已采用可配置间隔；本地仅以原有Go遥测worker为执行owner，不照搬失效常量。
- 保留先前暂缓的第三方生图白名单、轮次ID原值透传；本轮未调整。长请求租约、manifest失败短退避、观测SQL容量仍按先前研究记录，不接管其他工作树未完成内容。Cloud按最新已发布实现保留，不恢复SIWC。

## 来源、许可与验证准备
vm2api固定版本LICENSE要求商用书面授权；用户明确确认覆盖本仓库适配、三品牌分发与收费服务。现有LICENSE.vm2api/NOTICE保持，新增适配来源在本记录与累计说明署名，不留合同私密资料。CPR固定版本为Apache-2.0，作者Zyy；vm2api搜索修复ted、硬件修复Niutopia及上传周期dofastted/贡献者。
Oracle由固定3c45c685 telemetry-env模块实际执行生成（只替换独立模块版本fallback导入为显式2.1.296；全部fixture显式给定版本），保留原输入硬件事实，只更新输出期望。没有声称新采样版本或真实Anthropic接收。
SDK沿用Node22.20.0/Go1.27.3；前端使用既有pnpm9.15.9离线缓存安装442项，zh-TW工具使用npm离线安装opencc-js。环境诊断修正：默认PATH无node、corepack环境指向不存在的12.14.0，已显式使用既有SDK和9.15.9，不修改依赖声明。
