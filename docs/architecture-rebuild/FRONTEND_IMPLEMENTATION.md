# 前端重构第一波实施记录

日期：2026-10-25。统一集成目标：codex/sub2api-rebuild。前端源码基于6f48d6ab6fcc29763af84410690530aeaa957403，规划/编码细则参考72dc16b8659f24319ac86e816c7900aa159261b4的开发资料。开发在隔离工作树进行，后端未提交工作及维护main/TapModels/tokensavy不属于本轮修改范围。

本记录描述已实现的前端基础切片，不表示完整WP-21/28/29或全部前端重构完成。整体领域迁移继续按规划推进；共享任务状态由集成者在work-items.json合并时登记。

## 实际切片

| 切片 | 唯一实现与接入 | 清理 |
| --- | --- | --- |
| WP-28-TRANSPORT | shared/http承载管理URL与旋转令牌协调；shared/session承载既有存储键、逻辑session及只读快照；API/auth/step-up调用方已接入 | 删除api/tokenRefresh.ts、api/url.ts，原刷新测试迁移并保留原行为；无转发空壳 |
| WP-28-PUBLIC-CONFIG | shared/config/publicSettings拥有公共设置状态、注入、single-flight与代际；app store只负责业务DTO/副作用注入 | 删除旧加载/缓存实现和不可能状态下拼造的大型fallback对象；清除后旧请求不能回写 |
| WP-28-APP | app/bootstrap和app/App拥有启动、路由挂载及跨域会话副作用；登录变化、换session和卸载取消公告timer/订阅/路由hook | 删除旧根App.vue实现；main保留唯一浏览器入口，不提供旧路径兼容导出 |
| WP-28-BRANDING | project.ts中性默认sub2api；显式后台设置优先；四语言名称/首屏、Logo、帮助链接和更新显示接入 | 删除config/brand.ts、默认商业域名、维护镜像/安装/回滚命令及无消费者推广入口/文案；后端退休接口的完整前端清理见下节 |
| WP-21-CLEANUP-01 | 按实际调用核对源码、测试、动态文章与截图路径 | 删除无消费者pricingSnapshot.ts及其专属格式化测试、四张旧截图；其余有效断言保留 |

这里的切片ID用于实现/交接定位；父工作包不能因本轮通过而整体accepted。现有通用api/client、root stores、路由和其他页面仍有真实消费者，未被伪装为全部迁移完成。

## 保留的行为

- 管理API地址与公开api_base_url分离；供应商角色构建仍强制同源，网站展示链接不能承接Authorization。
- token轮换、跨tab竞争、同用户新登录隔离、刷新暂时性错误与step-up/合规确认保持原合同。
- 公共设置强制刷新仍与当前有效请求single-flight；清除只失效旧结果，不冒充取消服务器工作。失效期间保留上次品牌展示值，避免闪回其他品牌。
- 后端已退休在线安装/回滚功能，VersionBadge收敛为只读当前构建信息，门户分别读取其静态构建信息与后端版本，不再使用updater或远端release状态。管理员确认对话框、明确短语和服务端确认流程继续保留。
- 有效动态语言截图、96篇导航文章、四个下载工具包和其源码有真实消费者；历史备份前缀有回滚读取合同。未按品牌词或目录名称删除它们。后续工具品牌迁移需连同生成器、文档、ZIP和校验清单一并处理。

## 删除与修正明细

删除的旧资产：public/docs-assets/client-codex.png、client-claude.png、client-codex-apikey.png、client-claude-windows.png，共240336字节，资产说明同步更新。create-api-key等动态语言选择锚点保留。

教程修正：日文安装地址使用既有API_ROOT模板，恢复命令使用实际RepairSessions.ps1，英/简/繁控制台教程使用实际GEMINI_API_KEY。未改客户端原生认证字段、真实用户配置或历史数据。

## 已执行验证

| 范围 | 实际结果/边界 |
| --- | --- |
| 依赖 | Node 24.22.1、项目锁定pnpm 9.15.9；frozen安装复用442个缓存包，依赖清单和锁文件未改 |
| 定向行为 | 最终365/365通过（含后端退休接口同步后的完整受影响范围）：共享基础、client/auth/step-up、app状态与生命周期、供应商scope/guard、版本/合规、路由、i18n、保留价格文档断言 |
| 类型与静态 | vue-tsc生产类型检查通过；变更TS/Vue文件lint无错误/警告；实际范围与工具回执保存在本轮证据目录 |
| 功能角色构建 | 主站与vendor两种Vite构建通过，输出隔离；现有Accounts/vendor-ui大chunk警告仍存在，不宣称性能优化已完成 |
| 浏览器 | 真实浏览器访问本地Vite与合成公共设置服务：配置名称Rebuild Brand与空名称中性sub2api均正确，Logo/标题/登录表单可见；请求仍走同源管理API |
| 未执行 | 真实后端登录联调、真实上游生成、生产部署、数据库迁移、完整领域重构验收 |

首次检查发现四处翻译linked key不一致和一个泛型ShallowRef类型约束问题，修正后复测通过。后端接口退休同步后，类型、80个实际变更文件lint、365项测试与两种角色构建再次验证；最终检查的实际文件数以回执为准。浏览器检查使用合成公开配置，没有真实用户账号/密钥，不代替真实前后端或生产验收。

## 后端退休接口的同步清理

后端FRONTEND_HANDOFF在本轮期间明确删除system/update、system/rollback、system/rollback-versions和portal-updater链。前端同步删除这些请求封装、15分钟安装超时、更新/回滚/容器轮询、相应状态/文案和专属测试；系统API只读getVersion，门户不再探测或调用旧updater。原restart的前端包装只供退休安装流程调用，已随其删除；后端独立restart API继续由后端保留。本变化不新增替代开关或隐藏旧功能。

## 下一阶段与交付边界

接下来仍需完成HTTP客户端与领域副作用的完整解耦、identity/keys的完整纵向切片及其余领域迁移；Account/Group/Settings巨型编辑器尚未重建。每次切换同步处理旧消费者、专属文案/测试与确无用途的资产，不保留长期双实现。

仅按普通增量提交合入codex/sub2api-rebuild；不向维护三品牌传播，不发版或部署。实际提交与资源收尾由本轮最终回执记录，后端并发文件不得混入前端提交。
