# 验收证据记录模板

- Evidence ID / Task ID / AT与REQ：
- 类型：static_review / unit / mock_upstream / real_database / multi_instance / real_upstream / production。
- 状态：not_run / not_collected / passed / failed / blocked / not_applicable。
- not_applicable理由和审核者：
- 源码、依赖、生成输入、工具链、命令、环境指纹或已有有效回执：
- 完整命令及执行位置：
- 脱敏输入来源、客户端/上游版本、模式/传输/轮次：
- 前置状态、触发动作、预期观察、实际观察：
- 账务/归属/副作用核验：
- 开始/结束与退出状态：
- 保留的精简证据路径、失败根因及重测范围：
- 临时输出与清理实测：

本模板不等于tools/change_delivery.py生成的验证receipt，也不能用手填记录伪造工具回执。执行与收尾继续遵守CHANGE_DELIVERY。
