# 前端重构实施与冻结记录

更新：2026-10-26。共同分支codex/sub2api-rebuild；HEAD仍为独立未验收根快照673c53c13。本记录替换第一波历史快照；当前结构以[frontend/README](../../frontend/README.md)为源码入口，规范仍由FRONTEND_ARCHITECTURE和CODING_STANDARDS维护。

## 当前范围

前端owner已完成十四个领域、app/shared装配及所有已领取实现切片，并交接production/test冻结源。MAIN逐个复核1948份文件的SHA256，全部匹配。这里的code_ready不等于752项页面操作或整个重构accepted；后端仍在开发，完整验收尚未开始。

- 前端源集合SHA256：d4d7bfae71c4ccbca8d36302cf9d709c1bd06a1544e8ff95579d6315be7809f5
- 精确源清单：frontend/.local/rebuild-source-freeze.json；验证回执：frontend/.local/rebuild-validation.json。
- 根api/stores/types/components/views/utils/composables/constants/router均为0文件；不再保留第一波“这些根桶仍有真实消费者”的当前状态描述。
- identity、keys、resources、access-policy、catalog、pricing、subscriptions、payments、usage、operations、jobs、supply、site、system各自拥有具名公开出口、业务API/DTO、查询/命令、状态、页面及locale。
- app只保留装配、跨域组合、导航、布局和只读投影；Group原混合draft/panel/payload已下沉所属域，既有后端原子保存边界保留。
- 共享请求/身份机制保持单一owner；late response、取消、step-up卸载、设置scope、支付终态及多目标操作均有对应回归。取消等待不宣称服务器撤销写入。

## 开发验证证据

以下前端测试数据来自owner回执，MAIN核对冻结文件而未重复整套前端执行。

| 项目 | 实际结果 | 范围限制 |
| --- | --- | --- |
| 全量Vitest首轮 | 559文件，4966/4970通过、4失败 | 原失败日志保留 |
| 唯一测试文件修复 | pricingSnapshot旧locale路径改为site公开locale贡献；6/6定向通过 | 其中4项替换首轮失败，不重复累加已经通过的2项 |
| 有效单元结果 | 4970项、559文件；生产源码与其余测试保持原输入 | 不称全量一次全绿 |
| production typecheck | exit 0 | 不证明实际浏览器行为 |
| lint | 1769个唯一文件，0错误/0警告 | 未关闭规则掩盖问题 |
| web/vendor Vite | 两角色exit 0 | 原产物hash已保留，临时dist清理 |
| Chrome键盘复核 | 隔离BaseDialog/Select含radio及不同form行为通过 | 属组件环境，不等于业务页面验收 |

默认Vite大chunk提示保留，AccountsPage约703 kB、vendor-ui约510 kB；未上调阈值。实际加载性能仍须在完整浏览器验收采集。

## Docker制品

MAIN使用冻结清单创建仅18,397,437字节的必要源快照，未复制node_modules或依赖缓存；复用现有Dockerfile和pnpm缓存，构建中来源未漂移。frontend/.local已加入UI Dockerignore，避免任务证据/私有状态进入构建上下文。

- UI完整构建上下文SHA256：90c927992b545676e5080c936125a3c474598493999877f2615c3e282c10e539（还包含Docker/NGINX、LICENSE、legal及两个既有后端JSON输入）。
- 主站：sub2api-web:rebuild-90c927992b54；镜像ID sha256:67ee43c31f2850c8a45cadbb47a60e56fbda2be609d1f7412c29ae7783f8843e；107328212 bytes。
- 供应商：sub2api-vendor:rebuild-90c927992b54；镜像ID sha256:ef077d26578c7a417eb7c4e7ae86bda86094efb795ad27bfa4ad2f20e203348f；107323766 bytes。
- 构建耗时：主站54.983s，供应商复用构建层2.234s；两个exit均0。
- 回执：.local/rebuild-integration/ui-build/receipt.json。源快照已经实测删除；镜像保留供本机联调和最终验收。

本节仅确认两个实际镜像已构建，尚未将其当作Docker全栈或逐页通过。运行API镜像仍属于既有阶段，不能用其来源SHA代替最新后端工作区。

## 变更与清理统计

相对当前HEAD的frontend统计包含路径移动（删除+新增），不是新增功能量；未跟踪交付文件已纳入。

| 类别 | 新增行 | 删除行 | 净变化 |
| --- | ---: | ---: | ---: |
| business_and_configuration | 244327 | 233489 | 10838 |
| tests | 128854 | 73682 | 55172 |
| documentation | 6638 | 6802 | -164 |

资产变化为6个二进制文件；前端新增后端接口、部署配置及路由数量均为0。原作者广告/赞助等无关资产按所属域清理，必要许可、有效历史数据读取和来源声明保留。

## 未验收边界

- [前端完整用例](../../frontend/tests/acceptance/README.md)：752项定义，覆盖92页、17 redirect、2 alias、52设置面板及24类fixtures。当前完整执行not_run。
- 后端/系统79场景和13项API子集见[TEST_CASES](TEST_CASES.md)。双方源码及镜像最终冻结后再统一执行。
- 供应商HTTPS须完成受控测试信任验证；不得使用证书忽略开关、修改系统信任或放松生产Origin。
- 真实OAuth、支付、上游、邮件/存储/认证器等外部条件依各用例明确授权及证据，不将替身当真实验收。
- 只在联合Docker与逐页验收达到用户首版条件后，统一整理为一次独立根提交并推送；当前没有暂存、提交、推送或发版。
