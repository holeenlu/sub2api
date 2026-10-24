# 协议链路修复与增量处理（2026-10-23）

共享能力，交付 main、TapModels、tokensavy。保持交付时已发布的官方基线和其他任务修改，不附带新的四源同步；不发版、部署或执行迁移。

## 安全与正确性

- Bedrock EventStream 在分配内存前检查帧总长、头部上限及长度关系（32 MiB 帧、128 KiB 头）；CRC 正确也不能绕过边界。
- Bedrock 解码后复用 Anthropic 生命周期检查。缺失 message_stop、坏 chunk、异常事件和读取错误不能报告成功；关闭 Body 并等待读泵退出。保留已观测 usage，经原 Messages 结算链提交一次。
- 图片、媒体及 Embeddings 的用户/账号槽位保留到实际调用退出，客户端取消不再提前虚报空闲；准入等待仍可取消，不启动无人接收的新尝试。
- 这些非流式后台完成共享既有异步图片的 30 分钟执行上限，并保留更早的调用方 deadline；关闭时取消上游 context。不把计费用的后台完成一律缩短为普通 SSE 的 1.2 秒排空。
- Gemini 原生 SSE 使用已有有界读泵，应用 gateway.stream_data_interval_timeout 和 gateway.stream_keepalive_interval。原始字节进度与下游心跳分别计时，心跳不延长上游空闲预算；结束时 join，再读取 terminal 状态。
- Grok Realtime 下游逐帧写使用已有 gateway.openai_ws.write_timeout_seconds；失败取消双方读泵并等待退出，不重放音频。
- Anthropic 工具 schema 展平 allOf 的同名字段继续使用交集约束，保留原属性顺序。

## 性能和失效状态

- Grok TTS 的 audio/* 返回使用 32 KiB 有界缓冲增量写回和 Flush，不再等整段音频。沿用输入字符计费；失败后已观测的音频结果不丢结算，也不重新生成。STT 和音色资源 JSON 仍走原有读取/归属持久化。
- OAuth 非流式图片复用现有完整 SSE 帧解析器，逐事件收集图片、usage 和最多 600 字节文本诊断；完整终态后不再等待连接 EOF，不保留整份中间事件日志。账号探测复用同一图片收集器。
- Gemini Chat 删除内部包装和为了再次取得已知 model/stream 而进行的完整 JSON 解析及重复模型映射。工具、thinking、媒体和格式转换仍复用既有转换链，没有新增第二个转换器。
- 异步图片过了执行期限仍 processing 时，查询返回明确的执行超时/上游结果可能未知，不再一直 processing 到 Redis TTL 消失。只做结果投影，不覆盖并发完成、不延长 TTL、不自动重放；已知完成结果继续可查询。
- SSE 完整帧复用和 zstd 正文字节复用已经由先前 CPR 适配落实，本次沿用而非重复实现。调度的按需堆候选仍需证实真实热点；本轮不重写完整业务层级排序，避免引入未经实测的行为风险。

## 变更边界

新增公共接口、用户配置、数据库字段/迁移、常驻 Worker、独立网关路径均为 0。现有 TTS 分支从整包缓冲改为增量交付；异步任务 Get 增加过期状态投影。现有读泵支持在原始字节计时内接入 Gemini 规范化，内部槽位 helper 显式区分调用结束释放与客户端取消释放。

无需前端设置。已有 Gemini 空闲/心跳和 OpenAI WS 写超时配置沿用。后台完成新增统一有限期限是可见保护：极长非流式请求不再无限运行；没有修改历史迁移或业务数据。JSON 图片心跳原有的 HTTP 200 提交取舍保持不变。

验证采用受影响协议/handler/转换测试、本地 HTTP/WS/管道故障夹具和定向 race，不跑无关前端、全仓 build/vet 或生产测试。实际结果及旧新树、精确 lease、恢复 bundle 见本轮交付记录；测试中的首音早于上游 EOF 只证明增量交付，不等同于生产 TTFT 改善比例。

实现基于本地端到端审计。Wei-Shaw/ranxi 同类缺口不是优先选择上游的理由；New API AWS SDK 和 CPR 写超时仅作方案比较，没有复制新外部代码或升级依赖。
