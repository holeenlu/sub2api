# 前端测试用例索引

本索引与cases.json采用相同编号，详细前置条件、步骤、预期、清理和证据要求见该文件。目录采用冻结验收初始状态not_run；开发期间定向回归不等于完整验收已执行。

- 页面验收入口：93
- 重定向 / alias：17 / 2
- 设置面板：51
- 测试用例：762

## access-policy

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-ADMIN-RISK-CONTROL-ENTRY | P0 | local_e2e | /admin/risk-control：进入 |
| FE-PAGE-ADMIN-RISK-CONTROL-REFRESH | P0 | local_e2e | /admin/risk-control：刷新 |
| FE-PAGE-ADMIN-RISK-CONTROL-BACK | P0 | local_e2e | /admin/risk-control：返回和前进 |
| FE-PAGE-ADMIN-RISK-CONTROL-ACTION-1 | P0 | local_e2e | /admin/risk-control：查看状态和日志，筛选/分页并打开输入详情 |
| FE-PAGE-ADMIN-RISK-CONTROL-ACTION-2 | P0 | local_e2e | /admin/risk-control：编辑本地引擎配置/规则并保存，取消确认 |
| FE-PAGE-ADMIN-RISK-CONTROL-ACTION-3 | P0 | local_e2e | /admin/risk-control：解除 synthetic 用户封禁并清除选定本地 flagged hash |
| FE-PAGE-ADMIN-RISK-CONTROL-MANUAL-COMPLIANCE | P0 | manual | /admin/risk-control：以待确认合规 fixture 打开页面，确认对话框/文档/拒绝；真正接受由有权测试操作者明确执行。 |
| FE-PAGE-ADMIN-RISK-CONTROL-EXTERNAL-UPSTREAM | P0 | external_e2e | /admin/risk-control：执行页面实际提供的模型测试、探测、凭据刷新、图像任务或外部审核端点调用。 |

## accessibility

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-UI-001 | P1 | local_e2e | 键盘、焦点、语义与错误提示 |

## app/custom-page

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-CUSTOM-ID-ENTRY | P1 | local_e2e | /custom/:id：进入 |
| FE-PAGE-CUSTOM-ID-REFRESH | P1 | local_e2e | /custom/:id：刷新 |
| FE-PAGE-CUSTOM-ID-BACK | P1 | local_e2e | /custom/:id：返回和前进 |
| FE-PAGE-CUSTOM-ID-ACTION-1 | P1 | local_e2e | /custom/:id：打开本地 Markdown、iframe 与未配置/危险 URL 项 |
| FE-PAGE-CUSTOM-ID-ACTION-2 | P1 | local_e2e | /custom/:id：切换目录、拖动打开按钮、主题/语言并刷新返回 |
| FE-PAGE-CUSTOM-ID-EXTERNAL-NETWORK | P1 | external_e2e | /custom/:id：验证页面的真实代理出口/GeoJS 人工查询/外链下载或外部 iframe；仅执行本页实际出现的能力。 |

## app/dashboard

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-DASHBOARD-ENTRY | P1 | local_e2e | /dashboard：进入 |
| FE-PAGE-DASHBOARD-REFRESH | P1 | local_e2e | /dashboard：刷新 |
| FE-PAGE-DASHBOARD-BACK | P1 | local_e2e | /dashboard：返回和前进 |
| FE-PAGE-DASHBOARD-ACTION-1 | P1 | local_e2e | /dashboard：切换图表日期/粒度、手动刷新和快捷入口 |
| FE-PAGE-DASHBOARD-ACTION-2 | P1 | local_e2e | /dashboard：刷新、切换用户及空/加载失败状态 |
| FE-PAGE-ADMIN-DASHBOARD-ENTRY | P0 | local_e2e | /admin/dashboard：进入 |
| FE-PAGE-ADMIN-DASHBOARD-REFRESH | P0 | local_e2e | /admin/dashboard：刷新 |
| FE-PAGE-ADMIN-DASHBOARD-BACK | P0 | local_e2e | /admin/dashboard：返回和前进 |
| FE-PAGE-ADMIN-DASHBOARD-ACTION-1 | P0 | local_e2e | /admin/dashboard：修改时间和图表粒度并手动刷新 |
| FE-PAGE-ADMIN-DASHBOARD-ACTION-2 | P0 | local_e2e | /admin/dashboard：点击用户排名进入 /admin/usage，再返回 |
| FE-PAGE-ADMIN-DASHBOARD-MANUAL-COMPLIANCE | P0 | manual | /admin/dashboard：以待确认合规 fixture 打开页面，确认对话框/文档/拒绝；真正接受由有权测试操作者明确执行。 |

## app/group-editor

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-ADMIN-GROUPS-ENTRY | P0 | local_e2e | /admin/groups：进入 |
| FE-PAGE-ADMIN-GROUPS-REFRESH | P0 | local_e2e | /admin/groups：刷新 |
| FE-PAGE-ADMIN-GROUPS-BACK | P0 | local_e2e | /admin/groups：返回和前进 |
| FE-PAGE-ADMIN-GROUPS-ACTION-1 | P0 | local_e2e | /admin/groups：搜索/排序/分页，新建/编辑/复制 synthetic 组 |
| FE-PAGE-ADMIN-GROUPS-ACTION-2 | P0 | local_e2e | /admin/groups：调整复合路由、倍率、RPM、排序和删除确认 |
| FE-PAGE-ADMIN-GROUPS-MANUAL-COMPLIANCE | P0 | manual | /admin/groups：以待确认合规 fixture 打开页面，确认对话框/文档/拒绝；真正接受由有权测试操作者明确执行。 |

## app/resource-workspace

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-ADMIN-ACCOUNTS-ENTRY | P0 | local_e2e | /admin/accounts：进入 |
| FE-PAGE-ADMIN-ACCOUNTS-REFRESH | P0 | local_e2e | /admin/accounts：刷新 |
| FE-PAGE-ADMIN-ACCOUNTS-BACK | P0 | local_e2e | /admin/accounts：返回和前进 |
| FE-PAGE-ADMIN-ACCOUNTS-ACTION-1 | P0 | local_e2e | /admin/accounts：筛选分页、列设置、全选匹配结果并打开批量编辑 |
| FE-PAGE-ADMIN-ACCOUNTS-ACTION-2 | P0 | local_e2e | /admin/accounts：编辑/复制本地账号与 Native/BPS/Cloud 配置，处理409确认 |
| FE-PAGE-ADMIN-ACCOUNTS-ACTION-3 | P0 | local_e2e | /admin/accounts：导入/导出 v1/v2 本地数据并查看错误/TLS/诊断面板 |
| FE-PAGE-ADMIN-ACCOUNTS-MANUAL-COMPLIANCE | P0 | manual | /admin/accounts：以待确认合规 fixture 打开页面，确认对话框/文档/拒绝；真正接受由有权测试操作者明确执行。 |
| FE-PAGE-ADMIN-ACCOUNTS-EXTERNAL-OAUTH | P0 | external_e2e | /admin/accounts：完成页面对应的真实第三方登录、绑定、pending/adoption 或资源授权往返。 |
| FE-PAGE-ADMIN-ACCOUNTS-EXTERNAL-UPSTREAM | P0 | external_e2e | /admin/accounts：执行页面实际提供的模型测试、探测、凭据刷新、图像任务或外部审核端点调用。 |
| FE-PAGE-ADMIN-PROXIES-ENTRY | P0 | local_e2e | /admin/proxies：进入 |
| FE-PAGE-ADMIN-PROXIES-REFRESH | P0 | local_e2e | /admin/proxies：刷新 |
| FE-PAGE-ADMIN-PROXIES-BACK | P0 | local_e2e | /admin/proxies：返回和前进 |
| FE-PAGE-ADMIN-PROXIES-ACTION-1 | P0 | local_e2e | /admin/proxies：筛选协议/状态、分页，复制本地代理 URL |
| FE-PAGE-ADMIN-PROXIES-ACTION-2 | P0 | local_e2e | /admin/proxies：标准/批量新增编辑、导入导出及删除确认 |
| FE-PAGE-ADMIN-PROXIES-ACTION-3 | P0 | local_e2e | /admin/proxies：查看本地关联账号/质量报告和可控本机代理测试 |
| FE-PAGE-ADMIN-PROXIES-MANUAL-COMPLIANCE | P0 | manual | /admin/proxies：以待确认合规 fixture 打开页面，确认对话框/文档/拒绝；真正接受由有权测试操作者明确执行。 |
| FE-PAGE-ADMIN-PROXIES-EXTERNAL-NETWORK | P0 | external_e2e | /admin/proxies：验证页面的真实代理出口/GeoJS 人工查询/外链下载或外部 iframe；仅执行本页实际出现的能力。 |

## app/settings

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-ADMIN-SETTINGS-ENTRY | P0 | local_e2e | /admin/settings：进入 |
| FE-PAGE-ADMIN-SETTINGS-REFRESH | P0 | local_e2e | /admin/settings：刷新 |
| FE-PAGE-ADMIN-SETTINGS-BACK | P0 | local_e2e | /admin/settings：返回和前进 |
| FE-PAGE-ADMIN-SETTINGS-ACTION-1 | P0 | local_e2e | /admin/settings：逐一切换实际10个设置 tab。workspace draft 字段以 revision+sentFields 一次原子保存；标有 independent 的面板走自身领域命令。 |
| FE-PAGE-ADMIN-SETTINGS-ACTION-2 | P0 | local_e2e | /admin/settings：用两个本地编辑者制造409，再比较/审阅合并 |
| FE-PAGE-ADMIN-SETTINGS-ACTION-3 | P0 | local_e2e | /admin/settings：无写权限/无字段白名单管理员打开配置 |
| FE-PAGE-ADMIN-SETTINGS-MANUAL-COMPLIANCE | P0 | manual | /admin/settings：以待确认合规 fixture 打开页面，确认对话框/文档/拒绝；真正接受由有权测试操作者明确执行。 |
| FE-PAGE-ADMIN-SETTINGS-EXTERNAL-OAUTH | P0 | external_e2e | /admin/settings：完成页面对应的真实第三方登录、绑定、pending/adoption 或资源授权往返。 |
| FE-PAGE-ADMIN-SETTINGS-EXTERNAL-PAYMENT | P0 | external_e2e | /admin/settings：完成获准 sandbox 的真实支付/授权恢复/退款与 webhook 状态确认。 |
| FE-PAGE-ADMIN-SETTINGS-EXTERNAL-UPSTREAM | P0 | external_e2e | /admin/settings：执行页面实际提供的模型测试、探测、凭据刷新、图像任务或外部审核端点调用。 |
| FE-PAGE-ADMIN-SETTINGS-EXTERNAL-CAPTCHA | P0 | external_e2e | /admin/settings：验证已启用的真实 CAPTCHA 或外部邮件投递；本地邮件捕获/验证器只记录本地结果。 |
| FE-PAGE-ADMIN-SETTINGS-SETTINGS-ADMIN-KEYS | P0 | local_e2e | /admin/settings：在 security tab 分别操作两种管理 Key 的本地创建、查看、限权、撤销与取消 step-up。 |
| FE-PAGE-ADMIN-SETTINGS-SETTINGS-BACKUP | P0 | local_e2e | /admin/settings：backup tab 使用一次性本地库和本机对象存储测试配置、计划、备份、下载、恢复和删除。 |
| FE-PAGE-ADMIN-SETTINGS-SETTINGS-ROLES | P0 | local_e2e | /admin/settings：roles tab 设置权限、填写原因并保存 disposable 角色策略。 |
| FE-PAGE-ADMIN-SETTINGS-EXTERNAL-STORAGE | P0 | external_e2e | /admin/settings：验证实际获准 S3/R2/图像对象存储连接、备份及恢复。 |

## app/shell

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-PATHMATCH-ENTRY | P1 | local_e2e | /:pathMatch(.*)*：进入 |
| FE-PAGE-PATHMATCH-REFRESH | P1 | local_e2e | /:pathMatch(.*)*：刷新 |
| FE-PAGE-PATHMATCH-BACK | P1 | local_e2e | /:pathMatch(.*)*：返回和前进 |
| FE-PAGE-PATHMATCH-ACTION-1 | P1 | local_e2e | /:pathMatch(.*)*：用已登录本地用户进入不存在的路径，点击返回 |
| FE-PAGE-PATHMATCH-ACTION-2 | P1 | local_e2e | /:pathMatch(.*)*：匿名访问同一路径 |

## app/vendor-portal

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-VENDOR-ACCOUNTS-ENTRY | P0 | local_e2e | /vendor/accounts：进入 |
| FE-PAGE-VENDOR-ACCOUNTS-REFRESH | P0 | local_e2e | /vendor/accounts：刷新 |
| FE-PAGE-VENDOR-ACCOUNTS-BACK | P0 | local_e2e | /vendor/accounts：返回和前进 |
| FE-PAGE-VENDOR-ACCOUNTS-ACTION-1 | P0 | local_e2e | /vendor/accounts：筛选排序分页，查看/编辑自己供应商的 synthetic 账号 |
| FE-PAGE-VENDOR-ACCOUNTS-ACTION-2 | P0 | local_e2e | /vendor/accounts：本地导入/导出、批改与删除确认，并切换供应商 session |
| FE-PAGE-VENDOR-ACCOUNTS-EXTERNAL-OAUTH | P0 | external_e2e | /vendor/accounts：完成页面对应的真实第三方登录、绑定、pending/adoption 或资源授权往返。 |
| FE-PAGE-VENDOR-ACCOUNTS-EXTERNAL-UPSTREAM | P0 | external_e2e | /vendor/accounts：执行页面实际提供的模型测试、探测、凭据刷新、图像任务或外部审核端点调用。 |
| FE-PAGE-VENDOR-PROXIES-ENTRY | P0 | local_e2e | /vendor/proxies：进入 |
| FE-PAGE-VENDOR-PROXIES-REFRESH | P0 | local_e2e | /vendor/proxies：刷新 |
| FE-PAGE-VENDOR-PROXIES-BACK | P0 | local_e2e | /vendor/proxies：返回和前进 |
| FE-PAGE-VENDOR-PROXIES-ACTION-1 | P0 | local_e2e | /vendor/proxies：搜索协议/状态、分页、查看详情与复制本地代理地址 |
| FE-PAGE-VENDOR-PROXIES-ACTION-2 | P0 | local_e2e | /vendor/proxies：新增/编辑、导入/导出和批量删除本地代理 |
| FE-PAGE-VENDOR-PROXIES-EXTERNAL-NETWORK | P0 | external_e2e | /vendor/proxies：验证页面的真实代理出口/GeoJS 人工查询/外链下载或外部 iframe；仅执行本页实际出现的能力。 |
| FE-PAGE-VENDOR-USAGE-ENTRY | P0 | local_e2e | /vendor/usage：进入 |
| FE-PAGE-VENDOR-USAGE-REFRESH | P0 | local_e2e | /vendor/usage：刷新 |
| FE-PAGE-VENDOR-USAGE-BACK | P0 | local_e2e | /vendor/usage：返回和前进 |
| FE-PAGE-VENDOR-USAGE-ACTION-1 | P0 | local_e2e | /vendor/usage：筛选本供应商组/日期/账号并查看图表和记录 |
| FE-PAGE-VENDOR-USAGE-ACTION-2 | P0 | local_e2e | /vendor/usage：刷新、分页和导出 synthetic 记录 |
| FE-PAGE-VENDOR-SETTLEMENTS-ENTRY | P0 | local_e2e | /vendor/settlements：进入 |
| FE-PAGE-VENDOR-SETTLEMENTS-REFRESH | P0 | local_e2e | /vendor/settlements：刷新 |
| FE-PAGE-VENDOR-SETTLEMENTS-BACK | P0 | local_e2e | /vendor/settlements：返回和前进 |
| FE-PAGE-VENDOR-SETTLEMENTS-ACTION-1 | P0 | local_e2e | /vendor/settlements：筛选结算期间/状态、查看详情和分页 |
| FE-PAGE-VENDOR-SETTLEMENTS-ACTION-2 | P0 | local_e2e | /vendor/settlements：操作当前角色可见的本地账单草稿/确认/取消流程 |
| FE-PAGE-VENDOR-PROFILE-ENTRY | P0 | local_e2e | /vendor/profile：进入 |
| FE-PAGE-VENDOR-PROFILE-REFRESH | P0 | local_e2e | /vendor/profile：刷新 |
| FE-PAGE-VENDOR-PROFILE-BACK | P0 | local_e2e | /vendor/profile：返回和前进 |
| FE-PAGE-VENDOR-PROFILE-ACTION-1 | P0 | local_e2e | /vendor/profile：切换资料与安全 tab，查看供应商字段 |
| FE-PAGE-VENDOR-PROFILE-ACTION-2 | P0 | local_e2e | /vendor/profile：修改本地密码、TOTP/Passkey 管理并取消敏感挑战 |
| FE-PAGE-VENDOR-PROFILE-EXTERNAL-WEBAUTHN | P0 | external_e2e | /vendor/profile：用获准真实 authenticator 完成页面 Passkey 创建/登录/撤销中的适用动作。 |
| FE-PAGE-VENDOR-PLATFORM-ENTRY | P0 | local_e2e | /vendor/platform：进入 |
| FE-PAGE-VENDOR-PLATFORM-REFRESH | P0 | local_e2e | /vendor/platform：刷新 |
| FE-PAGE-VENDOR-PLATFORM-BACK | P0 | local_e2e | /vendor/platform：返回和前进 |
| FE-PAGE-VENDOR-PLATFORM-ACTION-1 | P0 | local_e2e | /vendor/platform：逐一访问 suppliers/accounts/proxies/usage/settlements/supply/audit 七个 query tab |
| FE-PAGE-VENDOR-PLATFORM-ACTION-2 | P0 | local_e2e | /vendor/platform：切换全体聚合和单一 vendor，查看下钻与服务端错误 |
| FE-PAGE-VENDOR-PLATFORM-ACTION-3 | P0 | local_e2e | /vendor/platform：编辑本地供应商/组供给或结算草稿，取消并检查权限 |
| FE-PAGE-VENDOR-PLATFORM-EXTERNAL-UPSTREAM | P0 | external_e2e | /vendor/platform：执行页面实际提供的模型测试、探测、凭据刷新、图像任务或外部审核端点调用。 |
| FE-PAGE-VENDOR-CHILDREN-ENTRY | P0 | local_e2e | /vendor/children：进入 |
| FE-PAGE-VENDOR-CHILDREN-REFRESH | P0 | local_e2e | /vendor/children：刷新 |
| FE-PAGE-VENDOR-CHILDREN-BACK | P0 | local_e2e | /vendor/children：返回和前进 |
| FE-PAGE-VENDOR-CHILDREN-ACTION-1 | P0 | local_e2e | /vendor/children：筛选/分页子供应商，创建/编辑 synthetic 子节点并打开子资源 |
| FE-PAGE-VENDOR-CHILDREN-ACTION-2 | P0 | local_e2e | /vendor/children：以 level2/普通用户尝试直达 |
| FE-PAGE-VENDOR-CHILD-ACCOUNTS-ENTRY | P0 | local_e2e | /vendor/child-accounts：进入 |
| FE-PAGE-VENDOR-CHILD-ACCOUNTS-REFRESH | P0 | local_e2e | /vendor/child-accounts：刷新 |
| FE-PAGE-VENDOR-CHILD-ACCOUNTS-BACK | P0 | local_e2e | /vendor/child-accounts：返回和前进 |
| FE-PAGE-VENDOR-CHILD-ACCOUNTS-ACTION-1 | P0 | local_e2e | /vendor/child-accounts：在 child 列表选择具体 vendor_id 或全部子节点并刷新 |
| FE-PAGE-VENDOR-CHILD-ACCOUNTS-ACTION-2 | P0 | local_e2e | /vendor/child-accounts：检查只读资源/用量下钻，尝试跨父 vendor_id 和 level2 角色 |
| FE-PAGE-VENDOR-CHILD-PROXIES-ENTRY | P0 | local_e2e | /vendor/child-proxies：进入 |
| FE-PAGE-VENDOR-CHILD-PROXIES-REFRESH | P0 | local_e2e | /vendor/child-proxies：刷新 |
| FE-PAGE-VENDOR-CHILD-PROXIES-BACK | P0 | local_e2e | /vendor/child-proxies：返回和前进 |
| FE-PAGE-VENDOR-CHILD-PROXIES-ACTION-1 | P0 | local_e2e | /vendor/child-proxies：在 child 列表选择具体 vendor_id 或全部子节点并刷新 |
| FE-PAGE-VENDOR-CHILD-PROXIES-ACTION-2 | P0 | local_e2e | /vendor/child-proxies：检查只读资源/用量下钻，尝试跨父 vendor_id 和 level2 角色 |
| FE-PAGE-VENDOR-CHILD-USAGE-ENTRY | P0 | local_e2e | /vendor/child-usage：进入 |
| FE-PAGE-VENDOR-CHILD-USAGE-REFRESH | P0 | local_e2e | /vendor/child-usage：刷新 |
| FE-PAGE-VENDOR-CHILD-USAGE-BACK | P0 | local_e2e | /vendor/child-usage：返回和前进 |
| FE-PAGE-VENDOR-CHILD-USAGE-ACTION-1 | P0 | local_e2e | /vendor/child-usage：在 child 列表选择具体 vendor_id 或全部子节点并刷新 |
| FE-PAGE-VENDOR-CHILD-USAGE-ACTION-2 | P0 | local_e2e | /vendor/child-usage：检查只读资源/用量下钻，尝试跨父 vendor_id 和 level2 角色 |
| FE-PAGE-VENDOR-PAYABLES-ENTRY | P0 | local_e2e | /vendor/payables：进入 |
| FE-PAGE-VENDOR-PAYABLES-REFRESH | P0 | local_e2e | /vendor/payables：刷新 |
| FE-PAGE-VENDOR-PAYABLES-BACK | P0 | local_e2e | /vendor/payables：返回和前进 |
| FE-PAGE-VENDOR-PAYABLES-ACTION-1 | P0 | local_e2e | /vendor/payables：筛选结算期间/状态、查看详情和分页 |
| FE-PAGE-VENDOR-PAYABLES-ACTION-2 | P0 | local_e2e | /vendor/payables：操作当前角色可见的本地账单草稿/确认/取消流程 |
| FE-PAGE-VENDOR-UNAVAILABLE-ENTRY | P0 | local_e2e | /vendor-unavailable：进入 |
| FE-PAGE-VENDOR-UNAVAILABLE-REFRESH | P0 | local_e2e | /vendor-unavailable：刷新 |
| FE-PAGE-VENDOR-UNAVAILABLE-BACK | P0 | local_e2e | /vendor-unavailable：返回和前进 |
| FE-PAGE-VENDOR-UNAVAILABLE-ACTION-1 | P0 | local_e2e | /vendor-unavailable：以错误 origin、关闭供应商控制台、普通角色进入 |
| FE-PAGE-VENDOR-UNAVAILABLE-ACTION-2 | P0 | local_e2e | /vendor-unavailable：点击重试入口和退出 |

## architecture

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-ARCH-001 | P0 | static | 领域依赖与退役路径门禁 |

## authorization

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-AUTH-004 | P0 | local_e2e | 权限撤销与精确页面/字段授权 |
| FE-AUTH-005 | P0 | local_e2e | step-up取消与失败的区分 |

## bootstrap

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-BOOT-001 | P0 | local_e2e | 首次加载与初始化失败后的真实恢复 |

## branding

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-BRAND-001 | P0 | local_e2e | 中性默认与运行时品牌切换 |
| FE-BRAND-002 | P0 | local_e2e | 特殊品牌文字和生成客户端配置 |
| FE-BRAND-003 | P0 | static | 下载包、安装器和历史恢复合同 |

## catalog

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-MODEL-PLAZA-ENTRY | P1 | local_e2e | /model-plaza：进入 |
| FE-PAGE-MODEL-PLAZA-REFRESH | P1 | local_e2e | /model-plaza：刷新 |
| FE-PAGE-MODEL-PLAZA-BACK | P1 | local_e2e | /model-plaza：返回和前进 |
| FE-PAGE-MODEL-PLAZA-ACTION-1 | P1 | local_e2e | /model-plaza：切换平台、组、倍率并搜索模型 |
| FE-PAGE-MODEL-PLAZA-ACTION-2 | P1 | local_e2e | /model-plaza：在公开、强制登录、禁用和 embedded=1 四种配置进入 |
| FE-PAGE-AVAILABLE-CHANNELS-ENTRY | P1 | local_e2e | /available-channels：进入 |
| FE-PAGE-AVAILABLE-CHANNELS-REFRESH | P1 | local_e2e | /available-channels：刷新 |
| FE-PAGE-AVAILABLE-CHANNELS-BACK | P1 | local_e2e | /available-channels：返回和前进 |
| FE-PAGE-AVAILABLE-CHANNELS-ACTION-1 | P1 | local_e2e | /available-channels：搜索/筛选可用渠道并手动刷新 |
| FE-PAGE-AVAILABLE-CHANNELS-ACTION-2 | P1 | local_e2e | /available-channels：覆盖空数据、功能关闭、加载失败和身份切换 |

## delivery

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-CLEAN-001 | P1 | local_e2e | 测试数据及制品收尾 |

## deployment

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-DEPLOY-001 | P0 | local_e2e | 冻结镜像、静态路由与构建来源 |
| FE-DEPLOY-002 | P0 | local_e2e | 空白安装与同制品多品牌 |

## i18n

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-I18N-001 | P1 | local_e2e | 四语言唯一owner与完整渲染 |
| FE-I18N-002 | P1 | local_e2e | 快速切语言与失败重试 |

## identity

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-LOGIN-ENTRY | P1 | local_e2e | /login：进入 |
| FE-PAGE-LOGIN-REFRESH | P1 | local_e2e | /login：刷新 |
| FE-PAGE-LOGIN-BACK | P1 | local_e2e | /login：返回和前进 |
| FE-PAGE-LOGIN-ACTION-1 | P1 | local_e2e | /login：填写本地账号密码并提交，覆盖正确、错误和禁用账号 |
| FE-PAGE-LOGIN-ACTION-2 | P1 | local_e2e | /login：打开密码显隐、协议提示及取消 MFA |
| FE-PAGE-LOGIN-ACTION-3 | P1 | local_e2e | /login：刷新并在另一 tab 退出或以同用户新 session 登录 |
| FE-PAGE-LOGIN-EXTERNAL-OAUTH | P1 | external_e2e | /login：完成页面对应的真实第三方登录、绑定、pending/adoption 或资源授权往返。 |
| FE-PAGE-LOGIN-EXTERNAL-CAPTCHA | P1 | external_e2e | /login：验证已启用的真实 CAPTCHA 或外部邮件投递；本地邮件捕获/验证器只记录本地结果。 |
| FE-PAGE-LOGIN-EXTERNAL-WEBAUTHN | P1 | external_e2e | /login：用获准真实 authenticator 完成页面 Passkey 创建/登录/撤销中的适用动作。 |
| FE-PAGE-REGISTER-ENTRY | P1 | local_e2e | /register：进入 |
| FE-PAGE-REGISTER-REFRESH | P1 | local_e2e | /register：刷新 |
| FE-PAGE-REGISTER-BACK | P1 | local_e2e | /register：返回和前进 |
| FE-PAGE-REGISTER-ACTION-1 | P1 | local_e2e | /register：测试注册开关、邮箱后缀、密码确认和协议未同意状态 |
| FE-PAGE-REGISTER-ACTION-2 | P1 | local_e2e | /register：以本地未注册邮箱提交，切换已有邮箱与验证码流程 |
| FE-PAGE-REGISTER-EXTERNAL-OAUTH | P1 | external_e2e | /register：完成页面对应的真实第三方登录、绑定、pending/adoption 或资源授权往返。 |
| FE-PAGE-REGISTER-EXTERNAL-CAPTCHA | P1 | external_e2e | /register：验证已启用的真实 CAPTCHA 或外部邮件投递；本地邮件捕获/验证器只记录本地结果。 |
| FE-PAGE-EMAIL-VERIFY-ENTRY | P1 | local_e2e | /email-verify：进入 |
| FE-PAGE-EMAIL-VERIFY-REFRESH | P1 | local_e2e | /email-verify：刷新 |
| FE-PAGE-EMAIL-VERIFY-BACK | P1 | local_e2e | /email-verify：返回和前进 |
| FE-PAGE-EMAIL-VERIFY-ACTION-1 | P1 | local_e2e | /email-verify：输入有效、错误、过期验证码并提交 |
| FE-PAGE-EMAIL-VERIFY-ACTION-2 | P1 | local_e2e | /email-verify：重发验证码、返回上一页并刷新 |
| FE-PAGE-EMAIL-VERIFY-ACTION-3 | P1 | local_e2e | /email-verify：完成 pending OAuth 新建账号及资料 adoption 选择 |
| FE-PAGE-EMAIL-VERIFY-EXTERNAL-CAPTCHA | P1 | external_e2e | /email-verify：验证已启用的真实 CAPTCHA 或外部邮件投递；本地邮件捕获/验证器只记录本地结果。 |
| FE-PAGE-AUTH-CALLBACK-ENTRY | P0 | local_e2e | /auth/callback：进入 |
| FE-PAGE-AUTH-CALLBACK-REFRESH | P0 | local_e2e | /auth/callback：刷新 |
| FE-PAGE-AUTH-CALLBACK-BACK | P0 | local_e2e | /auth/callback：返回和前进 |
| FE-PAGE-AUTH-CALLBACK-ACTION-1 | P0 | local_e2e | /auth/callback：以缺失参数、provider 错误和过期 pending fixture 进入 |
| FE-PAGE-AUTH-CALLBACK-ACTION-2 | P0 | local_e2e | /auth/callback：测试 generic/email OAuth callback 的注册补全、完成、code/state/fullUrl 复制入口，以及 alias 对 backend provider callback 的转发。 |
| FE-PAGE-AUTH-CALLBACK-ACTION-3 | P0 | local_e2e | /auth/callback：刷新、后退并返回同一完成地址 |
| FE-PAGE-AUTH-CALLBACK-EXTERNAL-OAUTH | P0 | external_e2e | /auth/callback：完成页面对应的真实第三方登录、绑定、pending/adoption 或资源授权往返。 |
| FE-PAGE-AUTH-LINUXDO-CALLBACK-ENTRY | P0 | local_e2e | /auth/linuxdo/callback：进入 |
| FE-PAGE-AUTH-LINUXDO-CALLBACK-REFRESH | P0 | local_e2e | /auth/linuxdo/callback：刷新 |
| FE-PAGE-AUTH-LINUXDO-CALLBACK-BACK | P0 | local_e2e | /auth/linuxdo/callback：返回和前进 |
| FE-PAGE-AUTH-LINUXDO-CALLBACK-ACTION-1 | P0 | local_e2e | /auth/linuxdo/callback：以缺失参数、provider 错误和过期 pending fixture 进入 |
| FE-PAGE-AUTH-LINUXDO-CALLBACK-ACTION-2 | P0 | local_e2e | /auth/linuxdo/callback：操作当前 provider 显示的新建、绑定已有账号、继续登录和 MFA 分支 |
| FE-PAGE-AUTH-LINUXDO-CALLBACK-ACTION-3 | P0 | local_e2e | /auth/linuxdo/callback：刷新、后退并返回同一完成地址 |
| FE-PAGE-AUTH-LINUXDO-CALLBACK-EXTERNAL-OAUTH | P0 | external_e2e | /auth/linuxdo/callback：完成页面对应的真实第三方登录、绑定、pending/adoption 或资源授权往返。 |
| FE-PAGE-AUTH-WECHAT-CALLBACK-ENTRY | P0 | local_e2e | /auth/wechat/callback：进入 |
| FE-PAGE-AUTH-WECHAT-CALLBACK-REFRESH | P0 | local_e2e | /auth/wechat/callback：刷新 |
| FE-PAGE-AUTH-WECHAT-CALLBACK-BACK | P0 | local_e2e | /auth/wechat/callback：返回和前进 |
| FE-PAGE-AUTH-WECHAT-CALLBACK-ACTION-1 | P0 | local_e2e | /auth/wechat/callback：以缺失参数、provider 错误和过期 pending fixture 进入 |
| FE-PAGE-AUTH-WECHAT-CALLBACK-ACTION-2 | P0 | local_e2e | /auth/wechat/callback：操作当前 provider 显示的新建、绑定已有账号、继续登录和 MFA 分支 |
| FE-PAGE-AUTH-WECHAT-CALLBACK-ACTION-3 | P0 | local_e2e | /auth/wechat/callback：刷新、后退并返回同一完成地址 |
| FE-PAGE-AUTH-WECHAT-CALLBACK-EXTERNAL-OAUTH | P0 | external_e2e | /auth/wechat/callback：完成页面对应的真实第三方登录、绑定、pending/adoption 或资源授权往返。 |
| FE-PAGE-AUTH-DINGTALK-CALLBACK-ENTRY | P0 | local_e2e | /auth/dingtalk/callback：进入 |
| FE-PAGE-AUTH-DINGTALK-CALLBACK-REFRESH | P0 | local_e2e | /auth/dingtalk/callback：刷新 |
| FE-PAGE-AUTH-DINGTALK-CALLBACK-BACK | P0 | local_e2e | /auth/dingtalk/callback：返回和前进 |
| FE-PAGE-AUTH-DINGTALK-CALLBACK-ACTION-1 | P0 | local_e2e | /auth/dingtalk/callback：以缺失参数、provider 错误和过期 pending fixture 进入 |
| FE-PAGE-AUTH-DINGTALK-CALLBACK-ACTION-2 | P0 | local_e2e | /auth/dingtalk/callback：操作当前 provider 显示的新建、绑定已有账号、继续登录和 MFA 分支 |
| FE-PAGE-AUTH-DINGTALK-CALLBACK-ACTION-3 | P0 | local_e2e | /auth/dingtalk/callback：刷新、后退并返回同一完成地址 |
| FE-PAGE-AUTH-DINGTALK-CALLBACK-EXTERNAL-OAUTH | P0 | external_e2e | /auth/dingtalk/callback：完成页面对应的真实第三方登录、绑定、pending/adoption 或资源授权往返。 |
| FE-PAGE-AUTH-DINGTALK-EMAIL-COMPLETION-ENTRY | P0 | local_e2e | /auth/dingtalk/email-completion：进入 |
| FE-PAGE-AUTH-DINGTALK-EMAIL-COMPLETION-REFRESH | P0 | local_e2e | /auth/dingtalk/email-completion：刷新 |
| FE-PAGE-AUTH-DINGTALK-EMAIL-COMPLETION-BACK | P0 | local_e2e | /auth/dingtalk/email-completion：返回和前进 |
| FE-PAGE-AUTH-DINGTALK-EMAIL-COMPLETION-ACTION-1 | P0 | local_e2e | /auth/dingtalk/email-completion：在有效 pending 会话中填写邮箱资料并提交 |
| FE-PAGE-AUTH-DINGTALK-EMAIL-COMPLETION-ACTION-2 | P0 | local_e2e | /auth/dingtalk/email-completion：切换绑定已有账号及缺失/过期 pending 状态 |
| FE-PAGE-AUTH-DINGTALK-EMAIL-COMPLETION-EXTERNAL-OAUTH | P0 | external_e2e | /auth/dingtalk/email-completion：完成页面对应的真实第三方登录、绑定、pending/adoption 或资源授权往返。 |
| FE-PAGE-AUTH-OIDC-CALLBACK-ENTRY | P0 | local_e2e | /auth/oidc/callback：进入 |
| FE-PAGE-AUTH-OIDC-CALLBACK-REFRESH | P0 | local_e2e | /auth/oidc/callback：刷新 |
| FE-PAGE-AUTH-OIDC-CALLBACK-BACK | P0 | local_e2e | /auth/oidc/callback：返回和前进 |
| FE-PAGE-AUTH-OIDC-CALLBACK-ACTION-1 | P0 | local_e2e | /auth/oidc/callback：以缺失参数、provider 错误和过期 pending fixture 进入 |
| FE-PAGE-AUTH-OIDC-CALLBACK-ACTION-2 | P0 | local_e2e | /auth/oidc/callback：操作当前 provider 显示的新建、绑定已有账号、继续登录和 MFA 分支 |
| FE-PAGE-AUTH-OIDC-CALLBACK-ACTION-3 | P0 | local_e2e | /auth/oidc/callback：刷新、后退并返回同一完成地址 |
| FE-PAGE-AUTH-OIDC-CALLBACK-EXTERNAL-OAUTH | P0 | external_e2e | /auth/oidc/callback：完成页面对应的真实第三方登录、绑定、pending/adoption 或资源授权往返。 |
| FE-PAGE-FORGOT-PASSWORD-ENTRY | P1 | local_e2e | /forgot-password：进入 |
| FE-PAGE-FORGOT-PASSWORD-REFRESH | P1 | local_e2e | /forgot-password：刷新 |
| FE-PAGE-FORGOT-PASSWORD-BACK | P1 | local_e2e | /forgot-password：返回和前进 |
| FE-PAGE-FORGOT-PASSWORD-ACTION-1 | P1 | local_e2e | /forgot-password：提交空/非法邮箱及本地测试邮箱 |
| FE-PAGE-FORGOT-PASSWORD-ACTION-2 | P1 | local_e2e | /forgot-password：从本地邮件捕获器取得重置流程，重复请求和返回登录 |
| FE-PAGE-FORGOT-PASSWORD-EXTERNAL-CAPTCHA | P1 | external_e2e | /forgot-password：验证已启用的真实 CAPTCHA 或外部邮件投递；本地邮件捕获/验证器只记录本地结果。 |
| FE-PAGE-RESET-PASSWORD-ENTRY | P1 | local_e2e | /reset-password：进入 |
| FE-PAGE-RESET-PASSWORD-REFRESH | P1 | local_e2e | /reset-password：刷新 |
| FE-PAGE-RESET-PASSWORD-BACK | P1 | local_e2e | /reset-password：返回和前进 |
| FE-PAGE-RESET-PASSWORD-ACTION-1 | P1 | local_e2e | /reset-password：使用本地重置 token 引用，测试密码不一致、过期及重放 |
| FE-PAGE-RESET-PASSWORD-ACTION-2 | P1 | local_e2e | /reset-password：成功后返回登录并用新旧密码检查 |
| FE-PAGE-PROFILE-ENTRY | P0 | local_e2e | /profile：进入 |
| FE-PAGE-PROFILE-REFRESH | P0 | local_e2e | /profile：刷新 |
| FE-PAGE-PROFILE-BACK | P0 | local_e2e | /profile：返回和前进 |
| FE-PAGE-PROFILE-ACTION-1 | P0 | local_e2e | /profile：编辑用户名/允许资料及余额提醒配置，刷新 |
| FE-PAGE-PROFILE-ACTION-2 | P0 | local_e2e | /profile：修改密码、执行本地 TOTP 设置/关闭并取消 step-up |
| FE-PAGE-PROFILE-ACTION-3 | P0 | local_e2e | /profile：查看绑定/Passkey 列表及不支持浏览器状态 |
| FE-PAGE-PROFILE-EXTERNAL-OAUTH | P0 | external_e2e | /profile：完成页面对应的真实第三方登录、绑定、pending/adoption 或资源授权往返。 |
| FE-PAGE-PROFILE-EXTERNAL-WEBAUTHN | P0 | external_e2e | /profile：用获准真实 authenticator 完成页面 Passkey 创建/登录/撤销中的适用动作。 |
| FE-PAGE-ADMIN-USERS-ENTRY | P0 | local_e2e | /admin/users：进入 |
| FE-PAGE-ADMIN-USERS-REFRESH | P0 | local_e2e | /admin/users：刷新 |
| FE-PAGE-ADMIN-USERS-BACK | P0 | local_e2e | /admin/users：返回和前进 |
| FE-PAGE-ADMIN-USERS-ACTION-1 | P0 | local_e2e | /admin/users：筛选、排序、切页、列偏好、属性过滤并刷新 |
| FE-PAGE-ADMIN-USERS-ACTION-2 | P0 | local_e2e | /admin/users：新建、编辑、停用、批改与删除 disposable 用户，分别取消和确认 |
| FE-PAGE-ADMIN-USERS-ACTION-3 | P0 | local_e2e | /admin/users：打开组授权、Key、订阅、余额历史/调整及平台配额面板 |
| FE-PAGE-ADMIN-USERS-MANUAL-COMPLIANCE | P0 | manual | /admin/users：以待确认合规 fixture 打开页面，确认对话框/文档/拒绝；真正接受由有权测试操作者明确执行。 |
| FE-AUTH-001 | P0 | local_e2e | 登录、错误密码、禁用用户与安全跳转 |
| FE-AUTH-002 | P0 | local_e2e | 令牌刷新、并发请求与跨tab协调 |
| FE-AUTH-003 | P0 | local_e2e | 登出、同用户重新登录和A/B切换拒绝迟到数据 |
| FE-AUTH-006 | P0 | local_e2e | OAuth回调、pending和重复完成 |
| FE-DATA-002 | P0 | local_e2e | Bearer与缓存profile主体不一致 |
| FE-AUTH-LOGIN-REDIRECT | P0 | local_e2e | 已登录访问/login与backend-mode例外 |
| FE-AUTH-REGISTER-REDIRECT | P0 | local_e2e | 已登录访问/register与backend-mode例外 |

## jobs

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-BATCH-IMAGE-ENTRY | P1 | local_e2e | /batch-image：进入 |
| FE-PAGE-BATCH-IMAGE-REFRESH | P1 | local_e2e | /batch-image：刷新 |
| FE-PAGE-BATCH-IMAGE-BACK | P1 | local_e2e | /batch-image：返回和前进 |
| FE-PAGE-BATCH-IMAGE-ACTION-1 | P1 | local_e2e | /batch-image：筛选任务状态、分页、多选并展开父子任务/详情 |
| FE-PAGE-BATCH-IMAGE-ACTION-2 | P1 | local_e2e | /batch-image：打开创建表单/指南并检查输入与本地输出预览 |
| FE-PAGE-BATCH-IMAGE-ACTION-3 | P1 | local_e2e | /batch-image：下载已准备的本地结果文件，取消/确认删除 synthetic 任务 |
| FE-PAGE-BATCH-IMAGE-EXTERNAL-UPSTREAM | P1 | external_e2e | /batch-image：执行页面实际提供的模型测试、探测、凭据刷新、图像任务或外部审核端点调用。 |

## keys

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-KEYS-ENTRY | P0 | local_e2e | /keys：进入 |
| FE-PAGE-KEYS-REFRESH | P0 | local_e2e | /keys：刷新 |
| FE-PAGE-KEYS-BACK | P0 | local_e2e | /keys：返回和前进 |
| FE-PAGE-KEYS-ACTION-1 | P0 | local_e2e | /keys：搜索、筛选、排序、选择、列偏好和分页 |
| FE-PAGE-KEYS-ACTION-2 | P0 | local_e2e | /keys：创建/编辑/停用/删除及批量修改 disposable Key |
| FE-PAGE-KEYS-ACTION-3 | P0 | local_e2e | /keys：导入/导出本地 Key 数据，打开使用指南与复制入口 |

## lifecycle

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PERF-002 | P1 | local_e2e | 路由循环与后台资源收尾 |

## operations

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-ADMIN-OPS-ENTRY | P0 | local_e2e | /admin/ops：进入 |
| FE-PAGE-ADMIN-OPS-REFRESH | P0 | local_e2e | /admin/ops：刷新 |
| FE-PAGE-ADMIN-OPS-BACK | P0 | local_e2e | /admin/ops：返回和前进 |
| FE-PAGE-ADMIN-OPS-ACTION-1 | P0 | local_e2e | /admin/ops：切换时间、平台、组、查询模式并刷新/全屏 |
| FE-PAGE-ADMIN-OPS-ACTION-2 | P0 | local_e2e | /admin/ops：打开请求/上游错误详情及返回列表 |
| FE-PAGE-ADMIN-OPS-ACTION-3 | P0 | local_e2e | /admin/ops：打开告警规则/设置并检查只读管理员 |
| FE-PAGE-ADMIN-OPS-MANUAL-COMPLIANCE | P0 | manual | /admin/ops：以待确认合规 fixture 打开页面，确认对话框/文档/拒绝；真正接受由有权测试操作者明确执行。 |
| FE-PAGE-ADMIN-OPS-EXTERNAL-UPSTREAM | P0 | external_e2e | /admin/ops：执行页面实际提供的模型测试、探测、凭据刷新、图像任务或外部审核端点调用。 |
| FE-PAGE-ADMIN-OPS-EXTERNAL-NETWORK | P0 | external_e2e | /admin/ops：验证页面的真实代理出口/GeoJS 人工查询/外链下载或外部 iframe；仅执行本页实际出现的能力。 |
| FE-PAGE-ADMIN-AUDIT-LOGS-ENTRY | P0 | local_e2e | /admin/audit-logs：进入 |
| FE-PAGE-ADMIN-AUDIT-LOGS-REFRESH | P0 | local_e2e | /admin/audit-logs：刷新 |
| FE-PAGE-ADMIN-AUDIT-LOGS-BACK | P0 | local_e2e | /admin/audit-logs：返回和前进 |
| FE-PAGE-ADMIN-AUDIT-LOGS-ACTION-1 | P0 | local_e2e | /admin/audit-logs：按时间、操作者、动作筛选并打开详情 |
| FE-PAGE-ADMIN-AUDIT-LOGS-ACTION-2 | P0 | local_e2e | /admin/audit-logs：打开清理确认和 TOTP 对话框，取消/错误挑战 |
| FE-PAGE-ADMIN-AUDIT-LOGS-MANUAL-COMPLIANCE | P0 | manual | /admin/audit-logs：以待确认合规 fixture 打开页面，确认对话框/文档/拒绝；真正接受由有权测试操作者明确执行。 |
| FE-PAGE-ADMIN-PROMPT-AUDIT-ENTRY | P0 | local_e2e | /admin/prompt-audit：进入 |
| FE-PAGE-ADMIN-PROMPT-AUDIT-REFRESH | P0 | local_e2e | /admin/prompt-audit：刷新 |
| FE-PAGE-ADMIN-PROMPT-AUDIT-BACK | P0 | local_e2e | /admin/prompt-audit：返回和前进 |
| FE-PAGE-ADMIN-PROMPT-AUDIT-ACTION-1 | P0 | local_e2e | /admin/prompt-audit：查看 runtime、配置和事件，筛选分页/详情 |
| FE-PAGE-ADMIN-PROMPT-AUDIT-ACTION-2 | P0 | local_e2e | /admin/prompt-audit：编辑端点/规则/启用或阻断设置并保存，取消阻断确认 |
| FE-PAGE-ADMIN-PROMPT-AUDIT-ACTION-3 | P0 | local_e2e | /admin/prompt-audit：选择单条/多条/筛选预览删除本地事件 |
| FE-PAGE-ADMIN-PROMPT-AUDIT-MANUAL-COMPLIANCE | P0 | manual | /admin/prompt-audit：以待确认合规 fixture 打开页面，确认对话框/文档/拒绝；真正接受由有权测试操作者明确执行。 |
| FE-PAGE-ADMIN-PROMPT-AUDIT-EXTERNAL-UPSTREAM | P0 | external_e2e | /admin/prompt-audit：执行页面实际提供的模型测试、探测、凭据刷新、图像任务或外部审核端点调用。 |
| FE-PAGE-ADMIN-CHANNELS-MONITOR-ENTRY | P0 | local_e2e | /admin/channels/monitor：进入 |
| FE-PAGE-ADMIN-CHANNELS-MONITOR-REFRESH | P0 | local_e2e | /admin/channels/monitor：刷新 |
| FE-PAGE-ADMIN-CHANNELS-MONITOR-BACK | P0 | local_e2e | /admin/channels/monitor：返回和前进 |
| FE-PAGE-ADMIN-CHANNELS-MONITOR-ACTION-1 | P0 | local_e2e | /admin/channels/monitor：切换页面实际支持的监控模式、查询与分页 |
| FE-PAGE-ADMIN-CHANNELS-MONITOR-ACTION-2 | P0 | local_e2e | /admin/channels/monitor：创建/编辑/复制本地监控项和模板，启停/删除 |
| FE-PAGE-ADMIN-CHANNELS-MONITOR-MANUAL-COMPLIANCE | P0 | manual | /admin/channels/monitor：以待确认合规 fixture 打开页面，确认对话框/文档/拒绝；真正接受由有权测试操作者明确执行。 |
| FE-PAGE-ADMIN-CHANNELS-MONITOR-EXTERNAL-UPSTREAM | P0 | external_e2e | /admin/channels/monitor：执行页面实际提供的模型测试、探测、凭据刷新、图像任务或外部审核端点调用。 |
| FE-PAGE-MONITOR-ENTRY | P1 | local_e2e | /monitor：进入 |
| FE-PAGE-MONITOR-REFRESH | P1 | local_e2e | /monitor：刷新 |
| FE-PAGE-MONITOR-BACK | P1 | local_e2e | /monitor：返回和前进 |
| FE-PAGE-MONITOR-ACTION-1 | P1 | local_e2e | /monitor：按配置进入当前 channel monitor 模式，操作实际筛选/详情 |
| FE-PAGE-MONITOR-ACTION-2 | P1 | local_e2e | /monitor：刷新和返回后再进入并切换身份 |

## payments

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-PURCHASE-ENTRY | P0 | local_e2e | /purchase：进入 |
| FE-PAGE-PURCHASE-REFRESH | P0 | local_e2e | /purchase：刷新 |
| FE-PAGE-PURCHASE-BACK | P0 | local_e2e | /purchase：返回和前进 |
| FE-PAGE-PURCHASE-ACTION-1 | P0 | local_e2e | /purchase：切换充值/订阅，选择支付方式、金额、套餐与续费组 |
| FE-PAGE-PURCHASE-ACTION-2 | P0 | local_e2e | /purchase：使用本地可控响应测试订单创建失败与 unknown 结果 |
| FE-PAGE-PURCHASE-ACTION-3 | P0 | local_e2e | /purchase：刷新并恢复本地 pending 订单或微信恢复状态 |
| FE-PAGE-PURCHASE-EXTERNAL-PAYMENT | P0 | external_e2e | /purchase：完成获准 sandbox 的真实支付/授权恢复/退款与 webhook 状态确认。 |
| FE-PAGE-AUTH-WECHAT-PAYMENT-CALLBACK-ENTRY | P0 | local_e2e | /auth/wechat/payment/callback：进入 |
| FE-PAGE-AUTH-WECHAT-PAYMENT-CALLBACK-REFRESH | P0 | local_e2e | /auth/wechat/payment/callback：刷新 |
| FE-PAGE-AUTH-WECHAT-PAYMENT-CALLBACK-BACK | P0 | local_e2e | /auth/wechat/payment/callback：返回和前进 |
| FE-PAGE-AUTH-WECHAT-PAYMENT-CALLBACK-ACTION-1 | P0 | local_e2e | /auth/wechat/payment/callback：用缺失、错误及本地微信恢复 fixture 打开回调 |
| FE-PAGE-AUTH-WECHAT-PAYMENT-CALLBACK-ACTION-2 | P0 | local_e2e | /auth/wechat/payment/callback：检查 fragment/query 优先级及返回支付页 |
| FE-PAGE-AUTH-WECHAT-PAYMENT-CALLBACK-EXTERNAL-PAYMENT | P0 | external_e2e | /auth/wechat/payment/callback：完成获准 sandbox 的真实支付/授权恢复/退款与 webhook 状态确认。 |
| FE-PAGE-ORDERS-ENTRY | P1 | local_e2e | /orders：进入 |
| FE-PAGE-ORDERS-REFRESH | P1 | local_e2e | /orders：刷新 |
| FE-PAGE-ORDERS-BACK | P1 | local_e2e | /orders：返回和前进 |
| FE-PAGE-ORDERS-ACTION-1 | P1 | local_e2e | /orders：按状态筛选、分页和刷新订单 |
| FE-PAGE-ORDERS-ACTION-2 | P1 | local_e2e | /orders：打开取消/退款确认并取消；本地 confirmed/unknown 响应分支 |
| FE-PAGE-ORDERS-EXTERNAL-PAYMENT | P1 | external_e2e | /orders：完成获准 sandbox 的真实支付/授权恢复/退款与 webhook 状态确认。 |
| FE-PAGE-PAYMENT-QRCODE-ENTRY | P0 | local_e2e | /payment/qrcode：进入 |
| FE-PAGE-PAYMENT-QRCODE-REFRESH | P0 | local_e2e | /payment/qrcode：刷新 |
| FE-PAGE-PAYMENT-QRCODE-BACK | P0 | local_e2e | /payment/qrcode：返回和前进 |
| FE-PAGE-PAYMENT-QRCODE-ACTION-1 | P0 | local_e2e | /payment/qrcode：带本地订单 ID 打开二维码页及别人的订单 ID |
| FE-PAGE-PAYMENT-QRCODE-ACTION-2 | P0 | local_e2e | /payment/qrcode：观察本地 pending/expired/paid 状态并返回购买/结果页 |
| FE-PAGE-PAYMENT-QRCODE-EXTERNAL-PAYMENT | P0 | external_e2e | /payment/qrcode：完成获准 sandbox 的真实支付/授权恢复/退款与 webhook 状态确认。 |
| FE-PAGE-PAYMENT-RESULT-ENTRY | P0 | local_e2e | /payment/result：进入 |
| FE-PAGE-PAYMENT-RESULT-REFRESH | P0 | local_e2e | /payment/result：刷新 |
| FE-PAGE-PAYMENT-RESULT-BACK | P0 | local_e2e | /payment/result：返回和前进 |
| FE-PAGE-PAYMENT-RESULT-ACTION-1 | P0 | local_e2e | /payment/result：覆盖 resume token、本人 order_id 与 legacy out_trade_no/trade_status 输入 |
| FE-PAGE-PAYMENT-RESULT-ACTION-2 | P0 | local_e2e | /payment/result：查看成功/失败/待确认/未知并返回购买和订单列表 |
| FE-PAGE-PAYMENT-RESULT-EXTERNAL-PAYMENT | P0 | external_e2e | /payment/result：完成获准 sandbox 的真实支付/授权恢复/退款与 webhook 状态确认。 |
| FE-PAGE-PAYMENT-STRIPE-ENTRY | P0 | local_e2e | /payment/stripe：进入 |
| FE-PAGE-PAYMENT-STRIPE-REFRESH | P0 | local_e2e | /payment/stripe：刷新 |
| FE-PAGE-PAYMENT-STRIPE-BACK | P0 | local_e2e | /payment/stripe：返回和前进 |
| FE-PAGE-PAYMENT-STRIPE-ACTION-1 | P0 | local_e2e | /payment/stripe：用缺失参数、错误、过期和已支付的本地订单 fixture 进入 |
| FE-PAGE-PAYMENT-STRIPE-ACTION-2 | P0 | local_e2e | /payment/stripe：刷新、返回购买及切换 session/订单 |
| FE-PAGE-PAYMENT-STRIPE-EXTERNAL-PAYMENT | P0 | external_e2e | /payment/stripe：完成获准 sandbox 的真实支付/授权恢复/退款与 webhook 状态确认。 |
| FE-PAGE-PAYMENT-AIRWALLEX-ENTRY | P0 | local_e2e | /payment/airwallex：进入 |
| FE-PAGE-PAYMENT-AIRWALLEX-REFRESH | P0 | local_e2e | /payment/airwallex：刷新 |
| FE-PAGE-PAYMENT-AIRWALLEX-BACK | P0 | local_e2e | /payment/airwallex：返回和前进 |
| FE-PAGE-PAYMENT-AIRWALLEX-ACTION-1 | P0 | local_e2e | /payment/airwallex：用缺失参数、错误、过期和已支付的本地订单 fixture 进入 |
| FE-PAGE-PAYMENT-AIRWALLEX-ACTION-2 | P0 | local_e2e | /payment/airwallex：刷新、返回购买及切换 session/订单 |
| FE-PAGE-PAYMENT-AIRWALLEX-EXTERNAL-PAYMENT | P0 | external_e2e | /payment/airwallex：完成获准 sandbox 的真实支付/授权恢复/退款与 webhook 状态确认。 |
| FE-PAGE-PAYMENT-STRIPE-POPUP-ENTRY | P0 | local_e2e | /payment/stripe-popup：进入 |
| FE-PAGE-PAYMENT-STRIPE-POPUP-REFRESH | P0 | local_e2e | /payment/stripe-popup：刷新 |
| FE-PAGE-PAYMENT-STRIPE-POPUP-BACK | P0 | local_e2e | /payment/stripe-popup：返回和前进 |
| FE-PAGE-PAYMENT-STRIPE-POPUP-ACTION-1 | P0 | local_e2e | /payment/stripe-popup：从本地受控父窗口以 order_id/method 打开，覆盖缺失/错误状态 |
| FE-PAGE-PAYMENT-STRIPE-POPUP-ACTION-2 | P0 | local_e2e | /payment/stripe-popup：关闭/重开并切换订单、模拟迟到结果 |
| FE-PAGE-PAYMENT-STRIPE-POPUP-EXTERNAL-PAYMENT | P0 | external_e2e | /payment/stripe-popup：完成获准 sandbox 的真实支付/授权恢复/退款与 webhook 状态确认。 |
| FE-PAGE-ADMIN-ORDERS-DASHBOARD-ENTRY | P0 | local_e2e | /admin/orders/dashboard：进入 |
| FE-PAGE-ADMIN-ORDERS-DASHBOARD-REFRESH | P0 | local_e2e | /admin/orders/dashboard：刷新 |
| FE-PAGE-ADMIN-ORDERS-DASHBOARD-BACK | P0 | local_e2e | /admin/orders/dashboard：返回和前进 |
| FE-PAGE-ADMIN-ORDERS-DASHBOARD-ACTION-1 | P0 | local_e2e | /admin/orders/dashboard：切换 7/30 等页面可见日范围并刷新 |
| FE-PAGE-ADMIN-ORDERS-DASHBOARD-ACTION-2 | P0 | local_e2e | /admin/orders/dashboard：切换/退出管理员 session |
| FE-PAGE-ADMIN-ORDERS-DASHBOARD-MANUAL-COMPLIANCE | P0 | manual | /admin/orders/dashboard：以待确认合规 fixture 打开页面，确认对话框/文档/拒绝；真正接受由有权测试操作者明确执行。 |
| FE-PAGE-ADMIN-ORDERS-ENTRY | P0 | local_e2e | /admin/orders：进入 |
| FE-PAGE-ADMIN-ORDERS-REFRESH | P0 | local_e2e | /admin/orders：刷新 |
| FE-PAGE-ADMIN-ORDERS-BACK | P0 | local_e2e | /admin/orders：返回和前进 |
| FE-PAGE-ADMIN-ORDERS-ACTION-1 | P0 | local_e2e | /admin/orders：搜索状态/类型、分页并打开订单详情 |
| FE-PAGE-ADMIN-ORDERS-ACTION-2 | P0 | local_e2e | /admin/orders：打开取消/重试/退款及查询结果，覆盖本地成功/失败/unknown |
| FE-PAGE-ADMIN-ORDERS-MANUAL-COMPLIANCE | P0 | manual | /admin/orders：以待确认合规 fixture 打开页面，确认对话框/文档/拒绝；真正接受由有权测试操作者明确执行。 |
| FE-PAGE-ADMIN-ORDERS-EXTERNAL-PAYMENT | P0 | external_e2e | /admin/orders：完成获准 sandbox 的真实支付/授权恢复/退款与 webhook 状态确认。 |
| FE-PAGE-ADMIN-ORDERS-PLANS-ENTRY | P0 | local_e2e | /admin/orders/plans：进入 |
| FE-PAGE-ADMIN-ORDERS-PLANS-REFRESH | P0 | local_e2e | /admin/orders/plans：刷新 |
| FE-PAGE-ADMIN-ORDERS-PLANS-BACK | P0 | local_e2e | /admin/orders/plans：返回和前进 |
| FE-PAGE-ADMIN-ORDERS-PLANS-ACTION-1 | P0 | local_e2e | /admin/orders/plans：新建/编辑本地套餐、校验必填和价格，保存刷新 |
| FE-PAGE-ADMIN-ORDERS-PLANS-ACTION-2 | P0 | local_e2e | /admin/orders/plans：上架/下架、删除确认与取消 |
| FE-PAGE-ADMIN-ORDERS-PLANS-MANUAL-COMPLIANCE | P0 | manual | /admin/orders/plans：以待确认合规 fixture 打开页面，确认对话框/文档/拒绝；真正接受由有权测试操作者明确执行。 |
| FE-BILL-002 | P0 | local_e2e | 订阅与余额两种支付模式边界 |
| FE-PAY-001 | P0 | local_e2e | 支付弹窗、回调和订单归属 |
| FE-PAY-002 | P0 | external_e2e | 获准支付sandbox完整闭环 |

## performance

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PERF-001 | P1 | local_e2e | 两角色首屏与按需加载 |

## pricing

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-ADMIN-CHANNELS-PRICING-ENTRY | P0 | local_e2e | /admin/channels/pricing：进入 |
| FE-PAGE-ADMIN-CHANNELS-PRICING-REFRESH | P0 | local_e2e | /admin/channels/pricing：刷新 |
| FE-PAGE-ADMIN-CHANNELS-PRICING-BACK | P0 | local_e2e | /admin/channels/pricing：返回和前进 |
| FE-PAGE-ADMIN-CHANNELS-PRICING-ACTION-1 | P0 | local_e2e | /admin/channels/pricing：搜索排序分页，创建/编辑 synthetic 渠道价格 |
| FE-PAGE-ADMIN-CHANNELS-PRICING-ACTION-2 | P0 | local_e2e | /admin/channels/pricing：切换渠道状态并取消/确认删除 |
| FE-PAGE-ADMIN-CHANNELS-PRICING-MANUAL-COMPLIANCE | P0 | manual | /admin/channels/pricing：以待确认合规 fixture 打开页面，确认对话框/文档/拒绝；真正接受由有权测试操作者明确执行。 |

## resources

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-DATA-001 | P0 | local_e2e | 复制的未知结果与幂等恢复 |
| FE-DATA-004 | P0 | local_e2e | 导入、导出与重复提交 |
| FE-RES-001 | P0 | local_e2e | 创建/编辑真实409混合渠道确认 |
| FE-RES-002 | P0 | local_e2e | 资源凭据可见性与部分保存 |
| FE-RES-BPS-EXPLICIT-MODELS | P0 | local_e2e | BPS只匹配显式选择的最终映射模型 |
| FE-RES-BPS-PUBLIC-RECOVERY | P0 | local_e2e | BPS安全恢复投影与作用范围展示 |
| FE-RES-BPS-STALE-EDIT | P0 | local_e2e | BPS自动停用与陈旧编辑不能互相覆盖 |

## responsive

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-UI-002 | P1 | local_e2e | 响应式与明暗主题 |

## routing

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-REDIRECT-ADMIN-CHANNELS | P0 | local_e2e | /admin/channels → /admin/channels/pricing |
| FE-REDIRECT-APPS-SESSION-RECOVERY | P1 | local_e2e | /apps/session-recovery → /apps/session-recovery-codex |
| FE-REDIRECT-DOCS-APPS-SESSION-RECOVERY | P1 | local_e2e | /docs/apps/session-recovery → /apps/session-recovery-codex |
| FE-REDIRECT-DOCS-APPS | P1 | local_e2e | /docs/apps → /apps |
| FE-REDIRECT-DOCS-APPS-CODEX | P1 | local_e2e | /docs/apps/codex → /apps/codex |
| FE-REDIRECT-DOCS-APPS-CLAUDE-CODE | P1 | local_e2e | /docs/apps/claude-code → /apps/claude-code |
| FE-REDIRECT-DOCS-APPS-CLAUDE-DESKTOP | P1 | local_e2e | /docs/apps/claude-desktop → /apps/claude-desktop |
| FE-REDIRECT-DOCS-APPS-SESSION-RECOVERY-CODEX | P1 | local_e2e | /docs/apps/session-recovery-codex → /apps/session-recovery-codex |
| FE-REDIRECT-DOCS-APPS-SESSION-RECOVERY-CLAUDE | P1 | local_e2e | /docs/apps/session-recovery-claude → /apps/session-recovery-claude |
| FE-REDIRECT-DOCS-APPS-IMAGE-SKILLS | P1 | local_e2e | /docs/apps/image-skills → /apps/image-skills |
| FE-REDIRECT-DOCS-DOWNLOADS | P1 | local_e2e | /docs/downloads → /apps/downloads |
| FE-REDIRECT-VENDOR | P0 | local_e2e | /vendor → /vendor/accounts |
| FE-REDIRECT-VENDOR-SECURITY | P0 | local_e2e | /vendor/security → /vendor/profile |
| FE-REDIRECT-VENDOR-AUDIT | P0 | local_e2e | /vendor/audit → /vendor/accounts |
| FE-REDIRECT-ADMIN-VENDORS | P0 | local_e2e | /admin/vendors → /admin/dashboard |
| FE-REDIRECT- | P1 | local_e2e | / → /home |
| FE-REDIRECT-ADMIN | P0 | local_e2e | /admin → /admin/dashboard |
| FE-ALIAS-AUTH-OAUTH-CALLBACK | P0 | local_e2e | /auth/oauth/callback → /auth/callback |
| FE-ALIAS-DOCS-BATCH-IMAGE | P1 | local_e2e | /docs/batch-image → /batch-image |
| FE-ROUTE-001 | P0 | static | 导航和验收目录持续覆盖实际路由 |

## security

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-SEC-001 | P0 | local_e2e | 管理API与公开品牌API地址隔离 |
| FE-SEC-002 | P0 | local_e2e | 匿名请求不携带会话信息 |
| FE-SEC-003 | P0 | local_e2e | XSS、Markdown、URL与下载链接 |
| FE-SEC-004 | P0 | local_e2e | 错误响应与日志不泄露秘密 |

## settings

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-SETTING-ADMINROLEPERMISSIONS-FIELDS | P0 | local_e2e | AdminRolePermissions：字段、权限和边界值 |
| FE-SETTING-ADMINROLEPERMISSIONS-WRITE | P0 | local_e2e | AdminRolePermissions：提交、失败与结果归属 |
| FE-SETTING-CONSOLESETTINGSPANEL-FIELDS | P0 | local_e2e | ConsoleSettingsPanel：字段、权限和边界值 |
| FE-SETTING-CONSOLESETTINGSPANEL-WRITE | P0 | local_e2e | ConsoleSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-APPEARANCESETTINGSPANEL-FIELDS | P0 | local_e2e | AppearanceSettingsPanel：字段、权限和边界值 |
| FE-SETTING-APPEARANCESETTINGSPANEL-WRITE | P0 | local_e2e | AppearanceSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-NAVIGATIONSETTINGSPANEL-FIELDS | P0 | local_e2e | NavigationSettingsPanel：字段、权限和边界值 |
| FE-SETTING-NAVIGATIONSETTINGSPANEL-WRITE | P0 | local_e2e | NavigationSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-AGREEMENTSSETTINGSPANEL-FIELDS | P0 | local_e2e | AgreementsSettingsPanel：字段、权限和边界值 |
| FE-SETTING-AGREEMENTSSETTINGSPANEL-WRITE | P0 | local_e2e | AgreementsSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-ADMINKEYSSETTINGSPANEL-FIELDS | P0 | local_e2e | AdminKeysSettingsPanel：字段、权限和边界值 |
| FE-SETTING-ADMINKEYSSETTINGSPANEL-WRITE | P0 | local_e2e | AdminKeysSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-REGISTRATIONSETTINGSPANEL-FIELDS | P0 | local_e2e | RegistrationSettingsPanel：字段、权限和边界值 |
| FE-SETTING-REGISTRATIONSETTINGSPANEL-WRITE | P0 | local_e2e | RegistrationSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-CLIENTIPSETTINGSPANEL-FIELDS | P0 | local_e2e | ClientIpSettingsPanel：字段、权限和边界值 |
| FE-SETTING-CLIENTIPSETTINGSPANEL-WRITE | P0 | local_e2e | ClientIpSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-PANELRATELIMITSETTINGSPANEL-FIELDS | P0 | local_e2e | PanelRateLimitSettingsPanel：字段、权限和边界值 |
| FE-SETTING-PANELRATELIMITSETTINGSPANEL-WRITE | P0 | local_e2e | PanelRateLimitSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-CAPTCHASETTINGSPANEL-FIELDS | P0 | local_e2e | CaptchaSettingsPanel：字段、权限和边界值 |
| FE-SETTING-CAPTCHASETTINGSPANEL-WRITE | P0 | local_e2e | CaptchaSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-LINUXDOSETTINGSPANEL-FIELDS | P0 | local_e2e | LinuxDoSettingsPanel：字段、权限和边界值 |
| FE-SETTING-LINUXDOSETTINGSPANEL-WRITE | P0 | local_e2e | LinuxDoSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-EMAILOAUTHSETTINGSPANEL-FIELDS | P0 | local_e2e | EmailOAuthSettingsPanel：字段、权限和边界值 |
| FE-SETTING-EMAILOAUTHSETTINGSPANEL-WRITE | P0 | local_e2e | EmailOAuthSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-WECHATSETTINGSPANEL-FIELDS | P0 | local_e2e | WeChatSettingsPanel：字段、权限和边界值 |
| FE-SETTING-WECHATSETTINGSPANEL-WRITE | P0 | local_e2e | WeChatSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-DINGTALKSETTINGSPANEL-FIELDS | P0 | local_e2e | DingTalkSettingsPanel：字段、权限和边界值 |
| FE-SETTING-DINGTALKSETTINGSPANEL-WRITE | P0 | local_e2e | DingTalkSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-OIDCSETTINGSPANEL-FIELDS | P0 | local_e2e | OidcSettingsPanel：字段、权限和边界值 |
| FE-SETTING-OIDCSETTINGSPANEL-WRITE | P0 | local_e2e | OidcSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-ADMINACCESSKEYSSETTINGS-FIELDS | P0 | local_e2e | AdminAccessKeysSettings：字段、权限和边界值 |
| FE-SETTING-ADMINACCESSKEYSSETTINGS-WRITE | P0 | local_e2e | AdminAccessKeysSettings：提交、失败与结果归属 |
| FE-SETTING-USERDEFAULTSSETTINGSPANEL-FIELDS | P0 | local_e2e | UserDefaultsSettingsPanel：字段、权限和边界值 |
| FE-SETTING-USERDEFAULTSSETTINGSPANEL-WRITE | P0 | local_e2e | UserDefaultsSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-DEFAULTQUOTASETTINGSPANEL-FIELDS | P0 | local_e2e | DefaultQuotaSettingsPanel：字段、权限和边界值 |
| FE-SETTING-DEFAULTQUOTASETTINGSPANEL-WRITE | P0 | local_e2e | DefaultQuotaSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-DEFAULTSUBSCRIPTIONSSETTINGSPANEL-FIELDS | P0 | local_e2e | DefaultSubscriptionsSettingsPanel：字段、权限和边界值 |
| FE-SETTING-DEFAULTSUBSCRIPTIONSSETTINGSPANEL-WRITE | P0 | local_e2e | DefaultSubscriptionsSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-AUTHDEFAULTSSETTINGSPANEL-FIELDS | P0 | local_e2e | AuthDefaultsSettingsPanel：字段、权限和边界值 |
| FE-SETTING-AUTHDEFAULTSSETTINGSPANEL-WRITE | P0 | local_e2e | AuthDefaultsSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-OVERLOADCOOLDOWNSETTINGSPANEL-FIELDS | P0 | local_e2e | OverloadCooldownSettingsPanel：字段、权限和边界值 |
| FE-SETTING-OVERLOADCOOLDOWNSETTINGSPANEL-WRITE | P0 | local_e2e | OverloadCooldownSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-RATELIMITCOOLDOWNSETTINGSPANEL-FIELDS | P0 | local_e2e | RateLimitCooldownSettingsPanel：字段、权限和边界值 |
| FE-SETTING-RATELIMITCOOLDOWNSETTINGSPANEL-WRITE | P0 | local_e2e | RateLimitCooldownSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-STREAMTIMEOUTSETTINGSPANEL-FIELDS | P0 | local_e2e | StreamTimeoutSettingsPanel：字段、权限和边界值 |
| FE-SETTING-STREAMTIMEOUTSETTINGSPANEL-WRITE | P0 | local_e2e | StreamTimeoutSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-RECTIFIERSETTINGSPANEL-FIELDS | P0 | local_e2e | RectifierSettingsPanel：字段、权限和边界值 |
| FE-SETTING-RECTIFIERSETTINGSPANEL-WRITE | P0 | local_e2e | RectifierSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-BETAPOLICYSETTINGSPANEL-FIELDS | P0 | local_e2e | BetaPolicySettingsPanel：字段、权限和边界值 |
| FE-SETTING-BETAPOLICYSETTINGSPANEL-WRITE | P0 | local_e2e | BetaPolicySettingsPanel：提交、失败与结果归属 |
| FE-SETTING-FASTPOLICYSETTINGSPANEL-FIELDS | P0 | local_e2e | FastPolicySettingsPanel：字段、权限和边界值 |
| FE-SETTING-FASTPOLICYSETTINGSPANEL-WRITE | P0 | local_e2e | FastPolicySettingsPanel：提交、失败与结果归属 |
| FE-SETTING-CLAUDEVERSIONSETTINGSPANEL-FIELDS | P0 | local_e2e | ClaudeVersionSettingsPanel：字段、权限和边界值 |
| FE-SETTING-CLAUDEVERSIONSETTINGSPANEL-WRITE | P0 | local_e2e | ClaudeVersionSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-CODEXCLIENTSSETTINGSPANEL-FIELDS | P0 | local_e2e | CodexClientsSettingsPanel：字段、权限和边界值 |
| FE-SETTING-CODEXCLIENTSSETTINGSPANEL-WRITE | P0 | local_e2e | CodexClientsSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-BILLINGPROBESETTINGSPANEL-FIELDS | P0 | local_e2e | BillingProbeSettingsPanel：字段、权限和边界值 |
| FE-SETTING-BILLINGPROBESETTINGSPANEL-WRITE | P0 | local_e2e | BillingProbeSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-OLLAMAUSAGESETTINGSPANEL-FIELDS | P0 | local_e2e | OllamaUsageSettingsPanel：字段、权限和边界值 |
| FE-SETTING-OLLAMAUSAGESETTINGSPANEL-WRITE | P0 | local_e2e | OllamaUsageSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-OPENCODEUSAGESETTINGSPANEL-FIELDS | P0 | local_e2e | OpenCodeUsageSettingsPanel：字段、权限和边界值 |
| FE-SETTING-OPENCODEUSAGESETTINGSPANEL-WRITE | P0 | local_e2e | OpenCodeUsageSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-SCHEDULINGSETTINGSPANEL-FIELDS | P0 | local_e2e | SchedulingSettingsPanel：字段、权限和边界值 |
| FE-SETTING-SCHEDULINGSETTINGSPANEL-WRITE | P0 | local_e2e | SchedulingSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-WEBSEARCHSETTINGSPANEL-FIELDS | P0 | local_e2e | WebSearchSettingsPanel：字段、权限和边界值 |
| FE-SETTING-WEBSEARCHSETTINGSPANEL-WRITE | P0 | local_e2e | WebSearchSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-ERRORVISIBILITYSETTINGSPANEL-FIELDS | P0 | local_e2e | ErrorVisibilitySettingsPanel：字段、权限和边界值 |
| FE-SETTING-ERRORVISIBILITYSETTINGSPANEL-WRITE | P0 | local_e2e | ErrorVisibilitySettingsPanel：提交、失败与结果归属 |
| FE-SETTING-BPSIMAGESSETTINGSPANEL-FIELDS | P0 | local_e2e | BpsImagesSettingsPanel：字段、权限和边界值 |
| FE-SETTING-BPSIMAGESSETTINGSPANEL-WRITE | P0 | local_e2e | BpsImagesSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-CHANNELMONITORSETTINGSPANEL-FIELDS | P0 | local_e2e | ChannelMonitorSettingsPanel：字段、权限和边界值 |
| FE-SETTING-CHANNELMONITORSETTINGSPANEL-WRITE | P0 | local_e2e | ChannelMonitorSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-AVAILABLECHANNELSSETTINGSPANEL-FIELDS | P0 | local_e2e | AvailableChannelsSettingsPanel：字段、权限和边界值 |
| FE-SETTING-AVAILABLECHANNELSSETTINGSPANEL-WRITE | P0 | local_e2e | AvailableChannelsSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-MODELPLAZASETTINGSPANEL-FIELDS | P0 | local_e2e | ModelPlazaSettingsPanel：字段、权限和边界值 |
| FE-SETTING-MODELPLAZASETTINGSPANEL-WRITE | P0 | local_e2e | ModelPlazaSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-BILLINGMODESETTINGSPANEL-FIELDS | P0 | local_e2e | BillingModeSettingsPanel：字段、权限和边界值 |
| FE-SETTING-BILLINGMODESETTINGSPANEL-WRITE | P0 | local_e2e | BillingModeSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-PLUGINFEATURESETTINGSPANEL-FIELDS | P0 | local_e2e | PluginFeatureSettingsPanel：字段、权限和边界值 |
| FE-SETTING-PLUGINFEATURESETTINGSPANEL-WRITE | P0 | local_e2e | PluginFeatureSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-RISKCONTROLSETTINGSPANEL-FIELDS | P0 | local_e2e | RiskControlSettingsPanel：字段、权限和边界值 |
| FE-SETTING-RISKCONTROLSETTINGSPANEL-WRITE | P0 | local_e2e | RiskControlSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-PAYMENTSETTINGSPANEL-FIELDS | P0 | local_e2e | PaymentSettingsPanel：字段、权限和边界值 |
| FE-SETTING-PAYMENTSETTINGSPANEL-WRITE | P0 | local_e2e | PaymentSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-RECHARGEBONUSSETTINGSPANEL-FIELDS | P0 | local_e2e | RechargeBonusSettingsPanel：字段、权限和边界值 |
| FE-SETTING-RECHARGEBONUSSETTINGSPANEL-WRITE | P0 | local_e2e | RechargeBonusSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-PROVIDERSSETTINGSPANEL-FIELDS | P0 | local_e2e | ProvidersSettingsPanel：字段、权限和边界值 |
| FE-SETTING-PROVIDERSSETTINGSPANEL-WRITE | P0 | local_e2e | ProvidersSettingsPanel：聚合草稿字段保存（独立provider命令另测） |
| FE-SETTING-SMTPSETTINGSPANEL-FIELDS | P0 | local_e2e | SmtpSettingsPanel：字段、权限和边界值 |
| FE-SETTING-SMTPSETTINGSPANEL-WRITE | P0 | local_e2e | SmtpSettingsPanel：SMTP草稿保存（连接与投递另测） |
| FE-SETTING-EXPIRYNOTIFICATIONSETTINGSPANEL-FIELDS | P0 | local_e2e | ExpiryNotificationSettingsPanel：字段、权限和边界值 |
| FE-SETTING-EXPIRYNOTIFICATIONSETTINGSPANEL-WRITE | P0 | local_e2e | ExpiryNotificationSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-BALANCENOTIFICATIONSETTINGSPANEL-FIELDS | P0 | local_e2e | BalanceNotificationSettingsPanel：字段、权限和边界值 |
| FE-SETTING-BALANCENOTIFICATIONSETTINGSPANEL-WRITE | P0 | local_e2e | BalanceNotificationSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-QUOTANOTIFICATIONSETTINGSPANEL-FIELDS | P0 | local_e2e | QuotaNotificationSettingsPanel：字段、权限和边界值 |
| FE-SETTING-QUOTANOTIFICATIONSETTINGSPANEL-WRITE | P0 | local_e2e | QuotaNotificationSettingsPanel：提交、失败与结果归属 |
| FE-SETTING-EMAILTEMPLATEEDITOR-FIELDS | P0 | local_e2e | EmailTemplateEditor：字段、权限和边界值 |
| FE-SETTING-EMAILTEMPLATEEDITOR-WRITE | P0 | local_e2e | EmailTemplateEditor：提交、失败与结果归属 |
| FE-SETTING-BACKUPPAGE-FIELDS | P0 | local_e2e | BackupPage：字段、权限和边界值 |
| FE-SETTING-BACKUPPAGE-WRITE | P0 | local_e2e | BackupPage：提交、失败与结果归属 |
| FE-SETTING-DELEGATEDSETTINGS-FIELDS | P0 | local_e2e | DelegatedSettings：字段、权限和边界值 |
| FE-SETTING-DELEGATEDSETTINGS-WRITE | P0 | local_e2e | DelegatedSettings：提交、失败与结果归属 |
| FE-SET-001 | P0 | local_e2e | 设置双编辑者CAS冲突 |
| FE-SET-002 | P0 | local_e2e | 省略、false、0、空和null的原子语义 |
| FE-SET-003 | P0 | local_e2e | 设置提交结果未知与离页 |
| FE-PROVIDER-CREATE | P0 | local_e2e | 支付provider独立命令：创建 |
| FE-PROVIDER-EDIT | P0 | local_e2e | 支付provider独立命令：编辑 |
| FE-PROVIDER-TOGGLE | P0 | local_e2e | 支付provider独立命令：开关及支持类型 |
| FE-PROVIDER-DELETE | P0 | local_e2e | 支付provider独立命令：删除 |
| FE-PROVIDER-ORDER | P0 | local_e2e | 支付provider独立命令：排序与部分失败 |
| FE-SMTP-CONNECTION | P0 | local_e2e | SMTP独立连接测试 |
| FE-SMTP-DELIVERY | P0 | local_e2e | SMTP独立测试邮件投递 |
| FE-ADMIN-KEY-ONCE | P0 | local_e2e | 管理访问密钥的一次性明文与后续遮罩 |
| FE-LEGACY-KEY-PRESENCE | P0 | local_e2e | 历史管理Key存在与缺失分支 |

## site

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-HOME-ENTRY | P1 | local_e2e | /home：进入 |
| FE-PAGE-HOME-REFRESH | P1 | local_e2e | /home：刷新 |
| FE-PAGE-HOME-BACK | P1 | local_e2e | /home：返回和前进 |
| FE-PAGE-HOME-ACTION-1 | P1 | local_e2e | /home：切换主题/语言，点击目录、应用、模型区和登录入口 |
| FE-PAGE-HOME-ACTION-2 | P1 | local_e2e | /home：检查空品牌/禁用功能/目录错误配置 |
| FE-PAGE-LEGAL-DOCUMENTID-ENTRY | P1 | local_e2e | /legal/:documentId：进入 |
| FE-PAGE-LEGAL-DOCUMENTID-REFRESH | P1 | local_e2e | /legal/:documentId：刷新 |
| FE-PAGE-LEGAL-DOCUMENTID-BACK | P1 | local_e2e | /legal/:documentId：返回和前进 |
| FE-PAGE-LEGAL-DOCUMENTID-ACTION-1 | P1 | local_e2e | /legal/:documentId：分别打开 fixture-terms、admin-compliance 和不存在 ID |
| FE-PAGE-LEGAL-DOCUMENTID-ACTION-2 | P1 | local_e2e | /legal/:documentId：切换语言并点击文档内安全链接、返回 |
| FE-PAGE-ADMIN-ANNOUNCEMENTS-ENTRY | P0 | local_e2e | /admin/announcements：进入 |
| FE-PAGE-ADMIN-ANNOUNCEMENTS-REFRESH | P0 | local_e2e | /admin/announcements：刷新 |
| FE-PAGE-ADMIN-ANNOUNCEMENTS-BACK | P0 | local_e2e | /admin/announcements：返回和前进 |
| FE-PAGE-ADMIN-ANNOUNCEMENTS-ACTION-1 | P0 | local_e2e | /admin/announcements：搜索状态、分页、预览和查看已读状态 |
| FE-PAGE-ADMIN-ANNOUNCEMENTS-ACTION-2 | P0 | local_e2e | /admin/announcements：创建/编辑/发布本地公告并取消/确认删除 |
| FE-PAGE-ADMIN-ANNOUNCEMENTS-MANUAL-COMPLIANCE | P0 | manual | /admin/announcements：以待确认合规 fixture 打开页面，确认对话框/文档/拒绝；真正接受由有权测试操作者明确执行。 |
| FE-PAGE-DOCS-ENTRY | P1 | local_e2e | /docs：进入 |
| FE-PAGE-DOCS-REFRESH | P1 | local_e2e | /docs：刷新 |
| FE-PAGE-DOCS-BACK | P1 | local_e2e | /docs：返回和前进 |
| FE-PAGE-DOCS-ACTION-1 | P1 | local_e2e | /docs：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-DOCS-ACTION-2 | P1 | local_e2e | /docs：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-DOCS-QUICKSTART-ENTRY | P1 | local_e2e | /docs/quickstart：进入 |
| FE-PAGE-DOCS-QUICKSTART-REFRESH | P1 | local_e2e | /docs/quickstart：刷新 |
| FE-PAGE-DOCS-QUICKSTART-BACK | P1 | local_e2e | /docs/quickstart：返回和前进 |
| FE-PAGE-DOCS-QUICKSTART-ACTION-1 | P1 | local_e2e | /docs/quickstart：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-DOCS-QUICKSTART-ACTION-2 | P1 | local_e2e | /docs/quickstart：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-DOCS-QUICKSTART-ACTION-3 | P1 | local_e2e | /docs/quickstart：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-DOCS-AUTHENTICATION-ENTRY | P1 | local_e2e | /docs/authentication：进入 |
| FE-PAGE-DOCS-AUTHENTICATION-REFRESH | P1 | local_e2e | /docs/authentication：刷新 |
| FE-PAGE-DOCS-AUTHENTICATION-BACK | P1 | local_e2e | /docs/authentication：返回和前进 |
| FE-PAGE-DOCS-AUTHENTICATION-ACTION-1 | P1 | local_e2e | /docs/authentication：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-DOCS-AUTHENTICATION-ACTION-2 | P1 | local_e2e | /docs/authentication：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-DOCS-AUTHENTICATION-ACTION-3 | P1 | local_e2e | /docs/authentication：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-DOCS-API-PROTOCOLS-ENTRY | P1 | local_e2e | /docs/api/protocols：进入 |
| FE-PAGE-DOCS-API-PROTOCOLS-REFRESH | P1 | local_e2e | /docs/api/protocols：刷新 |
| FE-PAGE-DOCS-API-PROTOCOLS-BACK | P1 | local_e2e | /docs/api/protocols：返回和前进 |
| FE-PAGE-DOCS-API-PROTOCOLS-ACTION-1 | P1 | local_e2e | /docs/api/protocols：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-DOCS-API-PROTOCOLS-ACTION-2 | P1 | local_e2e | /docs/api/protocols：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-DOCS-API-PROTOCOLS-ACTION-3 | P1 | local_e2e | /docs/api/protocols：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-DOCS-PRICING-ENTRY | P1 | local_e2e | /docs/pricing：进入 |
| FE-PAGE-DOCS-PRICING-REFRESH | P1 | local_e2e | /docs/pricing：刷新 |
| FE-PAGE-DOCS-PRICING-BACK | P1 | local_e2e | /docs/pricing：返回和前进 |
| FE-PAGE-DOCS-PRICING-ACTION-1 | P1 | local_e2e | /docs/pricing：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-DOCS-PRICING-ACTION-2 | P1 | local_e2e | /docs/pricing：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-DOCS-PRICING-ACTION-3 | P1 | local_e2e | /docs/pricing：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-DOCS-ERRORS-ENTRY | P1 | local_e2e | /docs/errors：进入 |
| FE-PAGE-DOCS-ERRORS-REFRESH | P1 | local_e2e | /docs/errors：刷新 |
| FE-PAGE-DOCS-ERRORS-BACK | P1 | local_e2e | /docs/errors：返回和前进 |
| FE-PAGE-DOCS-ERRORS-ACTION-1 | P1 | local_e2e | /docs/errors：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-DOCS-ERRORS-ACTION-2 | P1 | local_e2e | /docs/errors：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-DOCS-ERRORS-ACTION-3 | P1 | local_e2e | /docs/errors：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-DOCS-LIMITS-ENTRY | P1 | local_e2e | /docs/limits：进入 |
| FE-PAGE-DOCS-LIMITS-REFRESH | P1 | local_e2e | /docs/limits：刷新 |
| FE-PAGE-DOCS-LIMITS-BACK | P1 | local_e2e | /docs/limits：返回和前进 |
| FE-PAGE-DOCS-LIMITS-ACTION-1 | P1 | local_e2e | /docs/limits：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-DOCS-LIMITS-ACTION-2 | P1 | local_e2e | /docs/limits：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-DOCS-LIMITS-ACTION-3 | P1 | local_e2e | /docs/limits：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-DOCS-API-CHAT-OPENAI-CHAT-ENTRY | P1 | local_e2e | /docs/api/chat/openai-chat：进入 |
| FE-PAGE-DOCS-API-CHAT-OPENAI-CHAT-REFRESH | P1 | local_e2e | /docs/api/chat/openai-chat：刷新 |
| FE-PAGE-DOCS-API-CHAT-OPENAI-CHAT-BACK | P1 | local_e2e | /docs/api/chat/openai-chat：返回和前进 |
| FE-PAGE-DOCS-API-CHAT-OPENAI-CHAT-ACTION-1 | P1 | local_e2e | /docs/api/chat/openai-chat：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-DOCS-API-CHAT-OPENAI-CHAT-ACTION-2 | P1 | local_e2e | /docs/api/chat/openai-chat：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-DOCS-API-CHAT-OPENAI-CHAT-ACTION-3 | P1 | local_e2e | /docs/api/chat/openai-chat：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-DOCS-API-CHAT-OPENAI-RESPONSES-ENTRY | P1 | local_e2e | /docs/api/chat/openai-responses：进入 |
| FE-PAGE-DOCS-API-CHAT-OPENAI-RESPONSES-REFRESH | P1 | local_e2e | /docs/api/chat/openai-responses：刷新 |
| FE-PAGE-DOCS-API-CHAT-OPENAI-RESPONSES-BACK | P1 | local_e2e | /docs/api/chat/openai-responses：返回和前进 |
| FE-PAGE-DOCS-API-CHAT-OPENAI-RESPONSES-ACTION-1 | P1 | local_e2e | /docs/api/chat/openai-responses：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-DOCS-API-CHAT-OPENAI-RESPONSES-ACTION-2 | P1 | local_e2e | /docs/api/chat/openai-responses：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-DOCS-API-CHAT-OPENAI-RESPONSES-ACTION-3 | P1 | local_e2e | /docs/api/chat/openai-responses：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-DOCS-API-CHAT-ANTHROPIC-MESSAGES-ENTRY | P1 | local_e2e | /docs/api/chat/anthropic-messages：进入 |
| FE-PAGE-DOCS-API-CHAT-ANTHROPIC-MESSAGES-REFRESH | P1 | local_e2e | /docs/api/chat/anthropic-messages：刷新 |
| FE-PAGE-DOCS-API-CHAT-ANTHROPIC-MESSAGES-BACK | P1 | local_e2e | /docs/api/chat/anthropic-messages：返回和前进 |
| FE-PAGE-DOCS-API-CHAT-ANTHROPIC-MESSAGES-ACTION-1 | P1 | local_e2e | /docs/api/chat/anthropic-messages：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-DOCS-API-CHAT-ANTHROPIC-MESSAGES-ACTION-2 | P1 | local_e2e | /docs/api/chat/anthropic-messages：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-DOCS-API-CHAT-ANTHROPIC-MESSAGES-ACTION-3 | P1 | local_e2e | /docs/api/chat/anthropic-messages：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-DOCS-API-CHAT-GEMINI-NATIVE-ENTRY | P1 | local_e2e | /docs/api/chat/gemini-native：进入 |
| FE-PAGE-DOCS-API-CHAT-GEMINI-NATIVE-REFRESH | P1 | local_e2e | /docs/api/chat/gemini-native：刷新 |
| FE-PAGE-DOCS-API-CHAT-GEMINI-NATIVE-BACK | P1 | local_e2e | /docs/api/chat/gemini-native：返回和前进 |
| FE-PAGE-DOCS-API-CHAT-GEMINI-NATIVE-ACTION-1 | P1 | local_e2e | /docs/api/chat/gemini-native：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-DOCS-API-CHAT-GEMINI-NATIVE-ACTION-2 | P1 | local_e2e | /docs/api/chat/gemini-native：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-DOCS-API-CHAT-GEMINI-NATIVE-ACTION-3 | P1 | local_e2e | /docs/api/chat/gemini-native：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-DOCS-API-CHAT-GROK-NATIVE-ENTRY | P1 | local_e2e | /docs/api/chat/grok-native：进入 |
| FE-PAGE-DOCS-API-CHAT-GROK-NATIVE-REFRESH | P1 | local_e2e | /docs/api/chat/grok-native：刷新 |
| FE-PAGE-DOCS-API-CHAT-GROK-NATIVE-BACK | P1 | local_e2e | /docs/api/chat/grok-native：返回和前进 |
| FE-PAGE-DOCS-API-CHAT-GROK-NATIVE-ACTION-1 | P1 | local_e2e | /docs/api/chat/grok-native：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-DOCS-API-CHAT-GROK-NATIVE-ACTION-2 | P1 | local_e2e | /docs/api/chat/grok-native：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-DOCS-API-CHAT-GROK-NATIVE-ACTION-3 | P1 | local_e2e | /docs/api/chat/grok-native：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-DOCS-API-IMAGE-OPENAI-IMAGE-ENTRY | P1 | local_e2e | /docs/api/image/openai-image：进入 |
| FE-PAGE-DOCS-API-IMAGE-OPENAI-IMAGE-REFRESH | P1 | local_e2e | /docs/api/image/openai-image：刷新 |
| FE-PAGE-DOCS-API-IMAGE-OPENAI-IMAGE-BACK | P1 | local_e2e | /docs/api/image/openai-image：返回和前进 |
| FE-PAGE-DOCS-API-IMAGE-OPENAI-IMAGE-ACTION-1 | P1 | local_e2e | /docs/api/image/openai-image：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-DOCS-API-IMAGE-OPENAI-IMAGE-ACTION-2 | P1 | local_e2e | /docs/api/image/openai-image：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-DOCS-API-IMAGE-OPENAI-IMAGE-ACTION-3 | P1 | local_e2e | /docs/api/image/openai-image：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-DOCS-MODELS-ENTRY | P1 | local_e2e | /docs/models：进入 |
| FE-PAGE-DOCS-MODELS-REFRESH | P1 | local_e2e | /docs/models：刷新 |
| FE-PAGE-DOCS-MODELS-BACK | P1 | local_e2e | /docs/models：返回和前进 |
| FE-PAGE-DOCS-MODELS-ACTION-1 | P1 | local_e2e | /docs/models：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-DOCS-MODELS-ACTION-2 | P1 | local_e2e | /docs/models：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-DOCS-MODELS-ACTION-3 | P1 | local_e2e | /docs/models：按组浏览模型并打开一个详情。 |
| FE-PAGE-DOCS-API-QUERY-MODELS-ENTRY | P1 | local_e2e | /docs/api/query/models：进入 |
| FE-PAGE-DOCS-API-QUERY-MODELS-REFRESH | P1 | local_e2e | /docs/api/query/models：刷新 |
| FE-PAGE-DOCS-API-QUERY-MODELS-BACK | P1 | local_e2e | /docs/api/query/models：返回和前进 |
| FE-PAGE-DOCS-API-QUERY-MODELS-ACTION-1 | P1 | local_e2e | /docs/api/query/models：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-DOCS-API-QUERY-MODELS-ACTION-2 | P1 | local_e2e | /docs/api/query/models：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-DOCS-API-QUERY-MODELS-ACTION-3 | P1 | local_e2e | /docs/api/query/models：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-DOCS-API-QUERY-TOKEN-COUNT-ENTRY | P1 | local_e2e | /docs/api/query/token-count：进入 |
| FE-PAGE-DOCS-API-QUERY-TOKEN-COUNT-REFRESH | P1 | local_e2e | /docs/api/query/token-count：刷新 |
| FE-PAGE-DOCS-API-QUERY-TOKEN-COUNT-BACK | P1 | local_e2e | /docs/api/query/token-count：返回和前进 |
| FE-PAGE-DOCS-API-QUERY-TOKEN-COUNT-ACTION-1 | P1 | local_e2e | /docs/api/query/token-count：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-DOCS-API-QUERY-TOKEN-COUNT-ACTION-2 | P1 | local_e2e | /docs/api/query/token-count：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-DOCS-API-QUERY-TOKEN-COUNT-ACTION-3 | P1 | local_e2e | /docs/api/query/token-count：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-DOCS-API-QUERY-EMBEDDINGS-ENTRY | P1 | local_e2e | /docs/api/query/embeddings：进入 |
| FE-PAGE-DOCS-API-QUERY-EMBEDDINGS-REFRESH | P1 | local_e2e | /docs/api/query/embeddings：刷新 |
| FE-PAGE-DOCS-API-QUERY-EMBEDDINGS-BACK | P1 | local_e2e | /docs/api/query/embeddings：返回和前进 |
| FE-PAGE-DOCS-API-QUERY-EMBEDDINGS-ACTION-1 | P1 | local_e2e | /docs/api/query/embeddings：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-DOCS-API-QUERY-EMBEDDINGS-ACTION-2 | P1 | local_e2e | /docs/api/query/embeddings：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-DOCS-API-QUERY-EMBEDDINGS-ACTION-3 | P1 | local_e2e | /docs/api/query/embeddings：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-APPS-ENTRY | P1 | local_e2e | /apps：进入 |
| FE-PAGE-APPS-REFRESH | P1 | local_e2e | /apps：刷新 |
| FE-PAGE-APPS-BACK | P1 | local_e2e | /apps：返回和前进 |
| FE-PAGE-APPS-ACTION-1 | P1 | local_e2e | /apps：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-APPS-ACTION-2 | P1 | local_e2e | /apps：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-APPS-ACTION-3 | P1 | local_e2e | /apps：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-APPS-CONSOLE-ENTRY | P1 | local_e2e | /apps/console：进入 |
| FE-PAGE-APPS-CONSOLE-REFRESH | P1 | local_e2e | /apps/console：刷新 |
| FE-PAGE-APPS-CONSOLE-BACK | P1 | local_e2e | /apps/console：返回和前进 |
| FE-PAGE-APPS-CONSOLE-ACTION-1 | P1 | local_e2e | /apps/console：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-APPS-CONSOLE-ACTION-2 | P1 | local_e2e | /apps/console：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-APPS-CONSOLE-ACTION-3 | P1 | local_e2e | /apps/console：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-APPS-CODEX-ENTRY | P1 | local_e2e | /apps/codex：进入 |
| FE-PAGE-APPS-CODEX-REFRESH | P1 | local_e2e | /apps/codex：刷新 |
| FE-PAGE-APPS-CODEX-BACK | P1 | local_e2e | /apps/codex：返回和前进 |
| FE-PAGE-APPS-CODEX-ACTION-1 | P1 | local_e2e | /apps/codex：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-APPS-CODEX-ACTION-2 | P1 | local_e2e | /apps/codex：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-APPS-CODEX-ACTION-3 | P1 | local_e2e | /apps/codex：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-APPS-CLAUDE-CODE-ENTRY | P1 | local_e2e | /apps/claude-code：进入 |
| FE-PAGE-APPS-CLAUDE-CODE-REFRESH | P1 | local_e2e | /apps/claude-code：刷新 |
| FE-PAGE-APPS-CLAUDE-CODE-BACK | P1 | local_e2e | /apps/claude-code：返回和前进 |
| FE-PAGE-APPS-CLAUDE-CODE-ACTION-1 | P1 | local_e2e | /apps/claude-code：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-APPS-CLAUDE-CODE-ACTION-2 | P1 | local_e2e | /apps/claude-code：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-APPS-CLAUDE-CODE-ACTION-3 | P1 | local_e2e | /apps/claude-code：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-APPS-CLAUDE-DESKTOP-ENTRY | P1 | local_e2e | /apps/claude-desktop：进入 |
| FE-PAGE-APPS-CLAUDE-DESKTOP-REFRESH | P1 | local_e2e | /apps/claude-desktop：刷新 |
| FE-PAGE-APPS-CLAUDE-DESKTOP-BACK | P1 | local_e2e | /apps/claude-desktop：返回和前进 |
| FE-PAGE-APPS-CLAUDE-DESKTOP-ACTION-1 | P1 | local_e2e | /apps/claude-desktop：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-APPS-CLAUDE-DESKTOP-ACTION-2 | P1 | local_e2e | /apps/claude-desktop：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-APPS-CLAUDE-DESKTOP-ACTION-3 | P1 | local_e2e | /apps/claude-desktop：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-APPS-SESSION-RECOVERY-CODEX-ENTRY | P1 | local_e2e | /apps/session-recovery-codex：进入 |
| FE-PAGE-APPS-SESSION-RECOVERY-CODEX-REFRESH | P1 | local_e2e | /apps/session-recovery-codex：刷新 |
| FE-PAGE-APPS-SESSION-RECOVERY-CODEX-BACK | P1 | local_e2e | /apps/session-recovery-codex：返回和前进 |
| FE-PAGE-APPS-SESSION-RECOVERY-CODEX-ACTION-1 | P1 | local_e2e | /apps/session-recovery-codex：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-APPS-SESSION-RECOVERY-CODEX-ACTION-2 | P1 | local_e2e | /apps/session-recovery-codex：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-APPS-SESSION-RECOVERY-CODEX-ACTION-3 | P1 | local_e2e | /apps/session-recovery-codex：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-APPS-SESSION-RECOVERY-CLAUDE-ENTRY | P1 | local_e2e | /apps/session-recovery-claude：进入 |
| FE-PAGE-APPS-SESSION-RECOVERY-CLAUDE-REFRESH | P1 | local_e2e | /apps/session-recovery-claude：刷新 |
| FE-PAGE-APPS-SESSION-RECOVERY-CLAUDE-BACK | P1 | local_e2e | /apps/session-recovery-claude：返回和前进 |
| FE-PAGE-APPS-SESSION-RECOVERY-CLAUDE-ACTION-1 | P1 | local_e2e | /apps/session-recovery-claude：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-APPS-SESSION-RECOVERY-CLAUDE-ACTION-2 | P1 | local_e2e | /apps/session-recovery-claude：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-APPS-SESSION-RECOVERY-CLAUDE-ACTION-3 | P1 | local_e2e | /apps/session-recovery-claude：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-APPS-IMAGE-SKILLS-ENTRY | P1 | local_e2e | /apps/image-skills：进入 |
| FE-PAGE-APPS-IMAGE-SKILLS-REFRESH | P1 | local_e2e | /apps/image-skills：刷新 |
| FE-PAGE-APPS-IMAGE-SKILLS-BACK | P1 | local_e2e | /apps/image-skills：返回和前进 |
| FE-PAGE-APPS-IMAGE-SKILLS-ACTION-1 | P1 | local_e2e | /apps/image-skills：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-APPS-IMAGE-SKILLS-ACTION-2 | P1 | local_e2e | /apps/image-skills：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-APPS-IMAGE-SKILLS-ACTION-3 | P1 | local_e2e | /apps/image-skills：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-APPS-DOWNLOADS-ENTRY | P1 | local_e2e | /apps/downloads：进入 |
| FE-PAGE-APPS-DOWNLOADS-REFRESH | P1 | local_e2e | /apps/downloads：刷新 |
| FE-PAGE-APPS-DOWNLOADS-BACK | P1 | local_e2e | /apps/downloads：返回和前进 |
| FE-PAGE-APPS-DOWNLOADS-ACTION-1 | P1 | local_e2e | /apps/downloads：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-APPS-DOWNLOADS-ACTION-2 | P1 | local_e2e | /apps/downloads：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-APPS-DOWNLOADS-ACTION-3 | P1 | local_e2e | /apps/downloads：阅读当前文章、操作可见代码复制/价格表/安全链接。 |
| FE-PAGE-APPS-DOWNLOADS-EXTERNAL-NETWORK | P1 | external_e2e | /apps/downloads：验证页面的真实代理出口/GeoJS 人工查询/外链下载或外部 iframe；仅执行本页实际出现的能力。 |
| FE-PAGE-DOCS-MODELS-MODELID-ENTRY | P1 | local_e2e | /docs/models/:modelId：进入 |
| FE-PAGE-DOCS-MODELS-MODELID-REFRESH | P1 | local_e2e | /docs/models/:modelId：刷新 |
| FE-PAGE-DOCS-MODELS-MODELID-BACK | P1 | local_e2e | /docs/models/:modelId：返回和前进 |
| FE-PAGE-DOCS-MODELS-MODELID-ACTION-1 | P1 | local_e2e | /docs/models/:modelId：操作文档搜索（按钮/快捷键）、API/Apps 顶栏、侧栏与移动抽屉，使用当前实际路径导航。 |
| FE-PAGE-DOCS-MODELS-MODELID-ACTION-2 | P1 | local_e2e | /docs/models/:modelId：切换组/语言，点击目录锚点并刷新。 |
| FE-PAGE-DOCS-MODELS-MODELID-ACTION-3 | P1 | local_e2e | /docs/models/:modelId：打开本地已知目录模型、synthetic未知模型和不存在模型。 |

## subscriptions

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-SUBSCRIPTIONS-ENTRY | P1 | local_e2e | /subscriptions：进入 |
| FE-PAGE-SUBSCRIPTIONS-REFRESH | P1 | local_e2e | /subscriptions：刷新 |
| FE-PAGE-SUBSCRIPTIONS-BACK | P1 | local_e2e | /subscriptions：返回和前进 |
| FE-PAGE-SUBSCRIPTIONS-ACTION-1 | P1 | local_e2e | /subscriptions：查看 active/expired/revoked 订阅与额度，刷新 |
| FE-PAGE-SUBSCRIPTIONS-ACTION-2 | P1 | local_e2e | /subscriptions：点击续费/购买入口并检查目标组 |
| FE-PAGE-ADMIN-SUBSCRIPTIONS-ENTRY | P0 | local_e2e | /admin/subscriptions：进入 |
| FE-PAGE-ADMIN-SUBSCRIPTIONS-REFRESH | P0 | local_e2e | /admin/subscriptions：刷新 |
| FE-PAGE-ADMIN-SUBSCRIPTIONS-BACK | P0 | local_e2e | /admin/subscriptions：返回和前进 |
| FE-PAGE-ADMIN-SUBSCRIPTIONS-ACTION-1 | P0 | local_e2e | /admin/subscriptions：筛选用户、组和状态，排序分页与列切换 |
| FE-PAGE-ADMIN-SUBSCRIPTIONS-ACTION-2 | P0 | local_e2e | /admin/subscriptions：分配、延期、额度重置、撤销/恢复及批操作 |
| FE-PAGE-ADMIN-SUBSCRIPTIONS-MANUAL-COMPLIANCE | P0 | manual | /admin/subscriptions：以待确认合规 fixture 打开页面，确认对话框/文档/拒绝；真正接受由有权测试操作者明确执行。 |

## supply

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-SEC-005 | P0 | local_e2e | 供应商父子、同级和空scope隔离 |

## system

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-SETUP-ENTRY | P1 | local_e2e | /setup：进入 |
| FE-PAGE-SETUP-REFRESH | P1 | local_e2e | /setup：刷新 |
| FE-PAGE-SETUP-BACK | P1 | local_e2e | /setup：返回和前进 |
| FE-PAGE-SETUP-ACTION-1 | P1 | local_e2e | /setup：以 needs_setup=true 的一次性本地环境进入，测试步骤必填校验 |
| FE-PAGE-SETUP-ACTION-2 | P1 | local_e2e | /setup：随后由主代理在一次性环境测试本地数据库/Redis并安装 |
| FE-PAGE-SETUP-ACTION-3 | P1 | local_e2e | /setup：以 needs_setup=false 分别匿名/user/admin 进入 |
| FE-PAGE-ADMIN-PLUGINS-ENTRY | P0 | local_e2e | /admin/plugins：进入 |
| FE-PAGE-ADMIN-PLUGINS-REFRESH | P0 | local_e2e | /admin/plugins：刷新 |
| FE-PAGE-ADMIN-PLUGINS-BACK | P0 | local_e2e | /admin/plugins：返回和前进 |
| FE-PAGE-ADMIN-PLUGINS-ACTION-1 | P0 | local_e2e | /admin/plugins：上传合法/非法 synthetic .s2plugin，查看安装列表与配置页 |
| FE-PAGE-ADMIN-PLUGINS-ACTION-2 | P0 | local_e2e | /admin/plugins：启用/停用、灰度、测试无副作用本地插件并卸载 |
| FE-PAGE-ADMIN-PLUGINS-MANUAL-COMPLIANCE | P0 | manual | /admin/plugins：以待确认合规 fixture 打开页面，确认对话框/文档/拒绝；真正接受由有权测试操作者明确执行。 |

## tables

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-DATA-003 | P0 | local_e2e | 列表排序、分页、筛选和稳定选择 |

## transport

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-HTTP-001 | P0 | local_e2e | 统一失败与取消矩阵 |
| FE-HTTP-002 | P0 | local_e2e | 流式测试、取消和终态 |

## usage

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-USAGE-ENTRY | P1 | local_e2e | /usage：进入 |
| FE-PAGE-USAGE-REFRESH | P1 | local_e2e | /usage：刷新 |
| FE-PAGE-USAGE-BACK | P1 | local_e2e | /usage：返回和前进 |
| FE-PAGE-USAGE-ACTION-1 | P1 | local_e2e | /usage：切换日期、图表粒度、请求/错误 tab，筛选排序分页与重置 |
| FE-PAGE-USAGE-ACTION-2 | P1 | local_e2e | /usage：打开错误/用户/Key 下钻与导出本地 CSV/Excel |
| FE-PAGE-USAGE-ACTION-3 | P1 | local_e2e | /usage：查看 unknown/lower_bound/失败记录并切换身份 |
| FE-PAGE-USAGE-EXTERNAL-NETWORK | P1 | external_e2e | /usage：验证页面的真实代理出口/GeoJS 人工查询/外链下载或外部 iframe；仅执行本页实际出现的能力。 |
| FE-PAGE-ADMIN-USAGE-ENTRY | P0 | local_e2e | /admin/usage：进入 |
| FE-PAGE-ADMIN-USAGE-REFRESH | P0 | local_e2e | /admin/usage：刷新 |
| FE-PAGE-ADMIN-USAGE-BACK | P0 | local_e2e | /admin/usage：返回和前进 |
| FE-PAGE-ADMIN-USAGE-ACTION-1 | P0 | local_e2e | /admin/usage：切换日期、图表粒度、请求/错误 tab，筛选排序分页与重置 |
| FE-PAGE-ADMIN-USAGE-ACTION-2 | P0 | local_e2e | /admin/usage：打开错误/用户/Key 下钻与导出本地 CSV/Excel |
| FE-PAGE-ADMIN-USAGE-ACTION-3 | P0 | local_e2e | /admin/usage：查看 unknown/lower_bound/失败记录并切换身份 |
| FE-PAGE-ADMIN-USAGE-MANUAL-COMPLIANCE | P0 | manual | /admin/usage：以待确认合规 fixture 打开页面，确认对话框/文档/拒绝；真正接受由有权测试操作者明确执行。 |
| FE-PAGE-ADMIN-USAGE-EXTERNAL-NETWORK | P0 | external_e2e | /admin/usage：验证页面的真实代理出口/GeoJS 人工查询/外链下载或外部 iframe；仅执行本页实际出现的能力。 |
| FE-PAGE-KEY-USAGE-ENTRY | P1 | local_e2e | /key-usage：进入 |
| FE-PAGE-KEY-USAGE-REFRESH | P1 | local_e2e | /key-usage：刷新 |
| FE-PAGE-KEY-USAGE-BACK | P1 | local_e2e | /key-usage：返回和前进 |
| FE-PAGE-KEY-USAGE-ACTION-1 | P1 | local_e2e | /key-usage：输入本地 disposable Key 查询，测试空/错误 Key |
| FE-PAGE-KEY-USAGE-ACTION-2 | P1 | local_e2e | /key-usage：切换日期和每日窗口，显隐 Key 与切换主题 |
| FE-BILL-001 | P0 | local_e2e | 金额、配额、用量和延迟展示保真 |

## 统一模型管理增量

| 编号 | 优先级 | 层次 | 用例 |
| --- | --- | --- | --- |
| FE-PAGE-ADMIN-MODELS-ENTRY | P0 | local_e2e | 进入统一模型管理 |
| FE-PAGE-ADMIN-MODELS-REFRESH | P0 | local_e2e | 刷新统一模型管理 |
| FE-PAGE-ADMIN-MODELS-BACK | P0 | local_e2e | 统一模型管理返回和前进 |
| FE-MODELS-SYNC-DISABLED | P0 | local_e2e | 同步目录保留取消勾选状态 |
| FE-MODELS-ACCOUNT-ENABLED | P0 | local_e2e | 账号模型引用排除不可用项 |
| FE-MODELS-POLICY-CONFLICT | P0 | local_e2e | 模型策略并发版本冲突 |
| FE-MODELS-PRICE-CAS | P0 | local_e2e | 模型价格覆盖与账务读取 |
| FE-MODELS-AUTHZ-REVOCATION | P0 | local_e2e | 模型管理权限与退出隔离 |
| FE-MODELS-GROUP-CATALOG | P0 | local_e2e | 分组限制消费统一目录 |
