# 前端会话交接边界

日期：2026-10-25。前后端统一分支 codex/sub2api-rebuild；前端由用户另建会话在隔离工作树开发，通过审查后的普通增量提交接入。此文件定义交接事实，不向另一会话发指令，也不代表其工作已经交付。

## 写入和集成

- 前端会话独占 frontend/** 的领域目录、路由、共享组件、类型及品牌读取调整；本后端任务不修改它们。
- backend/**、根Dockerfile与deploy/api/**、根构建和共享合同由本任务整合。跨界变动先给出精确接口/构建产物影响，由各自owner落地。
- 两分支基线差异仅为已完成发布源修正。后续整合按实际文件及接口审查，不把一个分支整棵树覆盖另一工作树，不向 main 自动合入。
- 本分支后端可以单独构建、运行；完整站点由前端会话产物接入后验收。WP-21的external状态不意味着前端已经完成。

## 当前对外合同

| 边界 | 本轮合同 |
| --- | --- |
| 管理、用户、供应商API | 保持现有路由、JSON、鉴权及错误格式；尚未引入新的控制面revision API |
| 网关 | HTTP/SSE/WS已有请求、终态和取消合同保持；内部包迁移不改变URL或response字段 |
| 网站品牌 | 继续使用site_name、site_logo、site_subtitle、api_base_url、doc_url、contact_info及现有公开设置投影；后端空白安装fallback中性化 |
| API链接 | api_base_url是用户展示的网关地址，不是携带管理令牌的浏览器请求baseURL；浏览器管理API地址沿用已有配置 |
| 更新 | 独立开发制品不应下载维护品牌的二进制；前端应显示后端返回的更新状态，不从展示品牌推导可信更新源 |
| 构建交付 | 后端API制品独立于Node和frontend/dist；完整UI角色与最终资源路径由集成阶段确认，不能把缺少UI误报为全站可用 |

现有代码锚点：[公开设置DTO](../../backend/internal/handler/dto/settings.go)、[API客户端](../../frontend/src/api/client.ts)、[前端品牌入口](../../frontend/src/shared/config/project.ts)、[目标CT-11](CONTRACTS.md)。本轮没有新增面向前端的API字段或路由。

## 后续整合验收

核对公开设置、登录和刷新token、主站/供应商入口、网关地址展示及独立品牌首屏；确认API请求目标与品牌链接分离。若分离部署，先采用同源反向代理复用当前身份/跨域合同；不凭本文件推导任意跨域许可。前端真实构建/浏览器结果由对应会话提供，未提供时保持not_run。

## 2026-10-25 删除旧自更新后的接口变化

用户明确要求新项目不保留无用旧实现，因此旧自更新/回滚写链及专属缓存/socket/发布通道已移除。本节覆盖先前“所有管理路由不变”的第一波说明。前端合入前必须同步删除以下调用和无效按钮/状态/文案，不能仅隐藏界面留下旧调用：

| 接口 | 当前状态 |
| --- | --- |
| POST /api/v1/admin/system/update | 已删除路由、处理器、安装实现及权限目录项 |
| POST /api/v1/admin/system/rollback | 已删除路由、处理器、恢复实现及权限目录项 |
| GET /api/v1/admin/system/rollback-versions | 已删除路由、处理器、版本列表及权限目录项 |
| GET /api/v1/admin/system/version | 保留version、upstream_version、build_commit；升级器专属响应头已删除 |
| GET /api/v1/admin/system/check-updates | 已删除；前端已改为仅getVersion，无活跃消费者 |
| POST /api/v1/admin/system/restart | 保留独立运维用途、原授权、锁及幂等要求 |

更新方式是从本独立分支构建制品并重新部署。后端不提供隐藏的下载/安装开关，UPDATE_CHECK_ENABLED已删除。保留实际被Claude/Codex版本同步和价格数据读取使用的GitHub客户端及代理配置；它们不安装本项目二进制。前端代码未由本任务修改，相关删除验收由前端会话完成后再整合。

旧根assets/logo.svg、wordmark.svg及两张partners/logos推广图片已确认无消费者并删除；没有修改frontend自身Logo或资源。旧vendor-local品牌网络覆盖也退出，同机API和门户使用同一Compose项目default网络；新版门户UI和品牌资源仍由前端会话完成。

## 前端会话反馈（2026-10-25，外部提供）

前端会话告知：用户已要求双方统一在codex/sub2api-rebuild按普通增量提交协作；其实现暂存在隔离工作树/Users/luhonglin/.codex/worktrees/b889/sub2api的detached 6f48基线，不应自动收集其中未提交文件。该会话报告327项定向测试、类型检查、变更lint及主站/门户构建通过，浏览器仅验证合成设置首屏；本后端会话未独立重跑这些检查，尚未将该前端实现合入。

后端完成当前提交后在此登记可集成HEAD和写集边界；前端会话自行核对并仅集成frontend及其实施记录。后端清理已删除三个旧系统更新/回滚接口；门户旧updater代理块随同退休，前端需移除旧socket/updater调用与控件。网站公共设置、认证、网关协议合同本轮保持。维护三品牌分支继续独立。

独立项目启动默认改为/etc/sub2api配置目录、sub2api数据库名和WebAuthn显示名；显式配置优先，不变更RP ID或已保存数据，不再自动发现旧/etc/kdan。

## 已整合前端检查点

后端第一波代码提交：5aaf015b6d62864ee0fe3279967a579261cab51f。前端原始提交737fd177d30679a573995f6ad9b9314467ccb3f4已审查并cherry-pick为18853316238485e373a1113feee782549169bd03；frontend树与已验证d786178e800e85b0dd7e3d36540e25625bf17d89完全相同，没有收取隔离工作树的未提交文件。

当前前端仅消费GET /api/v1/admin/system/version。因此进一步删除无消费者GET /api/v1/admin/system/check-updates、安装状态字段及X-Portal-Backend-Version专属升级器响应头；BuildStatus仅保存三个当前版本值，不留旧更新占位接口。独立restart接口保留。前后端共同分支已有第一波实现，完整领域迁移与真实UI登录联调仍未完成。
