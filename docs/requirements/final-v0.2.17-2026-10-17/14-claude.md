# 14 · Claude Linux 指纹、CCH 与请求身份一致性

归档日期：2026-10-17；官方基线 `060d0160417782cd962a0c7f460f6cd4565d4916`；重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

目标提交：`fix(claude): preserve verified Linux fingerprint and CCH wire format`。这是当前最终需求及其现有实现的整理，不按旧开发过程重放已被覆盖的版本。

## 最终需求

1. 网关生成的 Claude OAuth mimic 与账号探测使用已验证 2.1.301 / SDK 0.128.0 / node v26.3.0 / Linux arm64 基线。UA、billing、CCH 从同一版本生成，宿主机平台和更高账号版本不改变模板。
2. 自动版本发现、环境变量、手工配置和真实 CLI profile 继续用于原身份跟踪；未经重新采样不能只改版本串冒充新指纹。显式账号头覆写和非 mimic 转发沿用原策略。
3. TLS ClientHello 采用固定 Linux 样本稳定字段，包括 ML-KEM/X25519、ALPN 与扩展/密码顺序；直连、HTTP/HTTPS CONNECT 和 SOCKS5 使用同一内置模板。HTTPS 代理外层 TLS 验证及 context 取消保留。
4. 按原生 CCH 的 XXH64 seed、字段排除和原始字节规则生成签名；不是把完整重新序列化 JSON 直接哈希。保留已验证边界向量和模糊测试入口。
5. HTTP/1 头部/线格式、压缩声明与响应解压配套；prompt/session/request 身份一致，未知版本遗留 CCH 必须剥离，不能混用旧签名。
6. 服务器运行期不安装或调用 Claude CLI；样本来自隔离开发采集。固定版本和平台样本不代表未来版本均等效，也不证明真实服务反风控结果。

## 入口与配置归属

复用既有入口、配置和授权；具体落点见下方逐文件清单与关联功能组。

## 最终实现方案

1. pkg/claude 和 tlsfingerprint 管理单一已验证版本与握手模板；service 的 body、billing、prompt、identity 和探测统一消费它。
2. 原生 golden ClientHello、88 个 CCH edge vectors 和 Linux body 样本作为版本化测试输入，保留协议差异说明与来源 SHA-512。
3. HTTP client、proxy、解压与原上游连接池的共用文件在第 19/04/12 等组只提交一次，related_groups 标注指纹依赖。详细采样与限制见 ../../CLAUDE_REQUEST_FINGERPRINT.md。

## 边界、失败路径与依赖

本组文件只提交一次，共用文件通过 files-main.tsv 的 related_groups 关联。第 19 组统一收录跨功能配置、路由、DTO、依赖与装配；完整组合是验证与部署单位。新归档不新增业务接口、配置项或执行路径。历史设计和退役能力不能仅因保留说明或迁移记录而重新启用。

## 验收要求

CLI 版本漂移、CCH golden/edge/fuzz 种子、ClientHello 稳定字节、HTTP/1 顺序、HTTPS 代理证书及取消回归；本轮不重新采集真实 CLI 或向 Anthropic 发起付费调用。

本轮实际执行与限制见 [VALIDATION.md](VALIDATION.md)。来源文档中的早期通过记录不能代替本轮结果。

## 相对官方 v0.2.17 的累计差异

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| asset | 1 | 1 | 0 | 0 |
| documentation | 1 | 213 | 0 | 0 |
| source | 10 | 598 | 105 | 0 |
| test | 17 | 1380 | 99 | 0 |
| tooling | 2 | 1181 | 0 | 0 |

这是既有最终代码相对官方基线的累计差异；不等于此次新写代码。重命名按删除和新增分别列出，二进制不折算行数。本归档新增文档不计入表内。

## 来源与最终版本

本组涉及 13 个可达来源提交。完整作者、原提交正文及父子关系见 [source-commits.json](source-commits.json)，每个文件的来源及旧归档来源见 [files-main.tsv](files-main.tsv)。来源列表表示曾触及该文件，不据此声称逐行归属；最终 blob / mode 才是重建依据。

## 文件与方案落点

文件链接指向 main 的对应路径；精确归档版本以 TSV 中的 blob 和固定来源 SHA 为准。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | source | 6 | 7 | 14 | [backend/internal/pkg/claude/cli_version.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/claude/cli_version.go) |
| M | source | 3 | 3 | 14 | [backend/internal/pkg/claude/cli_version_runtime.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/claude/cli_version_runtime.go) |
| M | test | 17 | 3 | 14 | [backend/internal/pkg/claude/cli_version_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/claude/cli_version_test.go) |
| M | source | 25 | 30 | 14 | [backend/internal/pkg/claude/constants.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/claude/constants.go) |
| M | source | 50 | 21 | 12,14 | [backend/internal/pkg/tlsfingerprint/dialer.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/tlsfingerprint/dialer.go) |
| M | test | 4 | 4 | 14 | [backend/internal/pkg/tlsfingerprint/dialer_capture_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/tlsfingerprint/dialer_capture_test.go) |
| M | test | 5 | 5 | 14 | [backend/internal/pkg/tlsfingerprint/dialer_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/tlsfingerprint/dialer_integration_test.go) |
| M | test | 98 | 14 | 14 | [backend/internal/pkg/tlsfingerprint/dialer_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/tlsfingerprint/dialer_test.go) |
| A | source | 237 | 0 | 14 | [backend/internal/pkg/tlsfingerprint/http1_wire.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/tlsfingerprint/http1_wire.go) |
| A | test | 579 | 0 | 14 | [backend/internal/pkg/tlsfingerprint/http1_wire_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/tlsfingerprint/http1_wire_test.go) |
| A | asset | 1 | 0 | 14 | [backend/internal/pkg/tlsfingerprint/testdata/claude_code_2_1_301_linux_hello.hex](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/tlsfingerprint/testdata/claude_code_2_1_301_linux_hello.hex) |
| M | test | 9 | 0 | 14 | [backend/internal/repository/decompress_response_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/decompress_response_test.go) |
| A | test | 184 | 0 | 14 | [backend/internal/repository/http_upstream_claude_wire_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/http_upstream_claude_wire_test.go) |
| M | test | 3 | 7 | 14 | [backend/internal/repository/http_upstream_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/http_upstream_test.go) |
| A | test | 43 | 0 | 14 | [backend/internal/service/account_test_service_claude_fingerprint_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_test_service_claude_fingerprint_test.go) |
| M | test | 43 | 43 | 14 | [backend/internal/service/claude_code_version_sync_service_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/claude_code_version_sync_service_test.go) |
| M | test | 1 | 1 | 14 | [backend/internal/service/gateway_accept_encoding_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_accept_encoding_test.go) |
| M | source | 10 | 9 | 14 | [backend/internal/service/gateway_billing_block.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_billing_block.go) |
| M | source | 204 | 2 | 14 | [backend/internal/service/gateway_billing_header.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_billing_header.go) |
| M | test | 177 | 3 | 14 | [backend/internal/service/gateway_billing_header_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_billing_header_test.go) |
| M | source | 4 | 5 | 14 | [backend/internal/service/gateway_claude_oauth_body.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_claude_oauth_body.go) |
| M | test | 147 | 5 | 14 | [backend/internal/service/gateway_cli_version_runtime_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_cli_version_runtime_test.go) |
| M | test | 1 | 1 | 14 | [backend/internal/service/gateway_prompt_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_prompt_test.go) |
| M | source | 58 | 27 | 14 | [backend/internal/service/identity_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/identity_service.go) |
| M | test | 34 | 1 | 14 | [backend/internal/service/identity_service_user_agent_validation_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/identity_service_user_agent_validation_test.go) |
| M | test | 25 | 2 | 14 | [backend/internal/service/identity_service_version_floor_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/identity_service_version_floor_test.go) |
| M | test | 10 | 10 | 14 | [backend/internal/service/setting_claude_code_version_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/setting_claude_code_version_test.go) |
| A | tooling | 1146 | 0 | 14 | [backend/internal/service/testdata/claude_code_2_1_301_cch_edges.json](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/testdata/claude_code_2_1_301_cch_edges.json) |
| A | tooling | 35 | 0 | 14 | [backend/internal/service/testdata/claude_code_2_1_301_linux_cch.json](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/testdata/claude_code_2_1_301_linux_cch.json) |
| M | source | 1 | 1 | 14 | [backend/internal/service/tls_fingerprint_profile_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/tls_fingerprint_profile_service.go) |
| A | documentation | 213 | 0 | 14 | [docs/CLAUDE_REQUEST_FINGERPRINT.md](https://github.com/holeenlu/sub2api/blob/main/docs/CLAUDE_REQUEST_FINGERPRINT.md) |
