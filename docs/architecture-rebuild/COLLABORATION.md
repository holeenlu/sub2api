# 多人和多子智能体协作协议

本文件仅约束本次重构的工作分配与交接，不改变AGENTS或发布政策。主代理可以独立完成任务；允许按需要委派子智能体。2026-10-25 用户已授权多子智能体开发，领取状态写入work-items.json。根AGENTS的独立分支范围优先；前端由用户另建会话负责，其他worker不得修改frontend。

## 角色与单一写入者

| 角色 | 职责 | 不能自行执行 |
| --- | --- | --- |
| Integrator | 冻结合同/基线，分派任务，处理共享旧文件与装配，整合、核验和独立重构分支交付 | 把未验收worker改动直接推上正式分支 |
| Contract owner | CT-01至11类型/接口语义及版本，处理所有消费者的变更 | 与多个worker同时改共享类型；创建平行v2合同 |
| Domain worker | 领取一个工作包，修改分配路径，更新相应证据与删除清单 | 越界修改公共装配、他人文件或正式分支；自行提交推送正式品牌 |
| Reviewer | 检查独立风险、接口消费、状态/安全/账务不变量 | 将编译或mock结果声明成真实上游验收 |
| Release integrator | 依现有交付规范备份、树核验、精确lease、远端/本地收尾 | 发版、生产部署或生产数据迁移 |

一个人可以承担多个角色，但共享文件始终一个实际写者。人类团队与子智能体适用同一合同，模型选择不写死。按用户最新交付规则，开发期间worker及集成者均不创建阶段提交或推送；使用已分配写集交接本地差异与证据。全部开发和联合验收完成后，只有集成者统一替换临时根并交付，维护分支历史不变。

## 领取流程

1. 读README、REQUIREMENTS、ARCHITECTURE、CONTRACTS、CODING_STANDARDS、BRANDING、当前AGENTS及与任务相关的协议/数据/验收文档。
2. 主集成者核对依赖已验收、基线仍有效，把work-items.json该任务的assignee、status、claimed_base及合同版本填实；未分派时assignee保持null。
3. 明确允许写集、只读旧代码锚点、请求集成者修改的共享文件以及删除目标。未列文件先提影响，不通过通配符扩大授权。
4. 默认一个worker一个隔离worktree/分支；沿用已有合适的任务工作树，不复制SDK或依赖。确需同一工作树时只有不相交写集可并行，Git索引操作仍单一写者。
5. 实现后只提交本任务路径/补丁和证据，进入review；集成者结合实际diff验证，不直接相信文字结论。
6. 集成后更新任务和验收证据，关闭不再需要的子智能体。保护必要恢复资料，按现行规则清理任务临时输出。

work-items.json是任务依赖、写集和状态的唯一来源；WORK_PACKAGES.md仅是阅读视图。该JSON是协作记录，不是自动运行器或产品配置。claim/release由集成者串行更新，不另建锁服务或调度平台。

## 受保护的共享文件

以下变化只能由集成者或明确指定的单一合同owner落地；worker提交具体修改建议与影响，不在并行工作中直接改：

- CONTRACTS.md、REQUIREMENTS.md、work-items.json，以及gateway/model等共享契约声明。
- backend/cmd/server/wire.go、生成装配、service/wire.go、handler/wire.go和server注册入口。
- backend/internal/server/routes/共享路由及middleware顺序；根go.mod/go.sum、前端锁文件、构建/部署/工作流。
- backend/internal/domain/platforms.go、现有共享Account/Group/APIKey/账务类型，以及多模式共享的openai_gateway_*、first_output、failover、quota helper。
- SQL迁移、Ent schema/生成文件和跨模式Redis字段；生产迁移需独立授权。
- frontend共享router、types/api根出口、brand配置；业务页切片由其worker管理。

新包的公共父目录文件也受此限制；不能在providers/openai根层各写一份common.go。优先复用已有纯工具；确有共同接口变化由owner发布一次。

## 并行与串行

WP-00冻结本轮实施输入、WP-01形成实际可编译合同后，纯协议、access、session、billing、catalog、host准备可并行；routing等待access接口可用。execution依赖其端口验收，native adapter可以基于固定合同并行实现。

原生完整切片验收后，BPS、Cloud、Anthropic、Gemini/Antigravity、其他兼容provider、媒体作业和实时模块可在分离目录并行开发。旧handler/service删除与注册、Wire装配由WP-18/19/23串行整合。前端由外部会话推进，控制面合同变更通过[前端交接](FRONTEND_HANDOFF.md)说明。

共享接口、真实数据变更、统一路由装配、同一垂直切片切换、独立分支提交和推送属于串行边界。不同worker的局部成功不等于该边界可以省略。

## 契约变更流程

worker发现合同缺口时记录：任务ID、CT版本、失败场景/现有源码证据、最小变化、受影响消费者与验证。主集成者区分工程细节和明确产品行为：前者在授权范围直接处理，后者按现行需求规则确定是否需要用户决定。确认后合同owner一次更新合同与类型，依赖任务再前进。

未解决的合同冲突只阻断依赖任务，其他已明确任务继续。禁止以临时字段、第二套DTO、任意map或新增开关静默绕过。

## Worker任务提示模板

~~~text
任务：<WP-ID / title>。源码基线：<实际SHA>。合同：0.2或已更新版本。编码规范：<work-items清单中的版本>。
读取：AGENTS、README、CONTRACTS、CODING_STANDARDS、BRANDING及该包相关协议/数据/验收条目。
目标：<outcome>。依赖：<已经验收的ID与证据>。
允许写：<精确paths>。只读参考：<legacy_read_paths>。
共享修改/删除申请：<integration_paths / retirement_targets>，由集成者合入。
保留：<REQ-ID>。验收：<CT / AT场景及最小命令>。
不修改其他任务、不升级来源依赖、不推送正式分支、不部署或迁移。
输出：实际文件diff、合同符合性、旧路径处理、测试命令/环境/结果、未解决项和资源清单。
~~~

## Handoff完成格式

使用templates/HANDOFF.md。编码规范版本、适用CS编号、实际检查证据及必要局部例外同时交接；不要求机械运行全部检查。至少记录源SHA/合同版本、实际而非预估写集、业务/测试增删、新接口/配置/执行路径数量、验收和未运行项、旧执行路径删除证据、依赖消费者、资源保留及清理。没有验证的内容标not_run，不补造回执。

## 冲突与故障恢复

发现源SHA、共享文件或他人写集漂移时保留当前diff，核对归属后整合；不reset他人分支，不覆盖未提交文件。失败任务保留可复现证据和最小现场，停止其已失效的任务进程；不得按进程名杀其他任务。

子智能体中断后，由集成者检查实际文件和测试结果再重领，不能从最后一条聊天状态推断完成。新worker接收同一任务ID、当前diff和剩余验收，不另起一份平行实现。

## 部署品牌写集补充

WP-27是统一项目/部署参数的唯一实现工作包，集成者可向其指定唯一部署写者，限定修改任务清单中的现有Compose/环境模板；源码公共身份、前端构建、更新渠道和发布工具仍由单一集成者处理。WP-23等待WP-27汇入，WP-26仅交付已验证结果，不在验收后临时改部署代码。重构分支独立交付，维护refs不被传播或改写。后端部署切片可先完成；完整品牌/UI验收仍等待其真实依赖。
