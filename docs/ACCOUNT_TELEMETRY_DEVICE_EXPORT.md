# 无需 Node.js 或登录，导出真实设备安装资料

独立程序导出本机官方 Claude Code 的安装 ID、CLI 版本和设备环境。网关绑定账号身份、授权和会话，继续按现有规则生成遥测。

## 直接运行

选择本机系统和架构的工具包，解压后运行。Windows、Linux、macOS 均提供 x64 / ARM64；无需安装 Node.js、Go、Python 或 npm 依赖，无需 `claude auth login`。

Windows PowerShell：

```powershell
./telemetry-export.exe --shell powershell
```

Linux：

```bash
chmod +x ./telemetry-export
./telemetry-export
```

macOS：

```bash
chmod +x ./telemetry-export
./telemetry-export
```

## 导出文件在哪里

未指定 `--output` 时，文件默认保存在**工具所在目录**，名称为 `account-telemetry.json`，与 `telemetry-export` / `telemetry-export.exe` 放在一起。从其他目录调用工具也使用此默认位置。例如，工具放在 `Downloads/telemetry-export/`，默认 JSON 也生成在该目录。

显式传入 `--output` 时使用指定路径：绝对路径直接定位目标文件；`--output ./account-telemetry-macos.json` 等相对路径按终端当前目录计算。Windows PowerShell 用 `Get-Location` 查看当前目录，Linux / macOS 用 `pwd`。

导出成功后，工具会在 `Saved` 后打印文件的**完整绝对路径**；导入网关时选择该路径下的 JSON。也可以用 `--output` 明确指定位置，路径含空格时保留引号。例如：

Windows PowerShell，保存到当前用户的桌面（包括重定向后的桌面）：

```powershell
./telemetry-export.exe --shell powershell --output (Join-Path ([Environment]::GetFolderPath('Desktop')) 'account-telemetry-windows.json')
```

macOS，保存到桌面：

```bash
./telemetry-export --output "$HOME/Desktop/account-telemetry-macos.json"
```

Linux，保存到当前用户的主目录：

```bash
./telemetry-export --output "$HOME/account-telemetry-linux.json"
```

目标目录必须已经存在且可写；工具不会覆盖同名文件，重复导出请指定新的文件名。如果工具安装目录不可写，请通过 `--output` 指定可写位置。

## 自动安装官方 Claude Code

工具优先使用 PATH 或用户目录 `~/.local/bin` 下已有的官方原生 Claude Code，保留本机实际版本。未找到时自动执行：

1. 从官方 `https://downloads.claude.ai/claude-code-releases/latest` 读取最新版本。
2. 获取该版本 manifest，下载对应系统/架构的原生程序，校验 SHA-256 与大小。Linux 区分 glibc / musl。
3. 调用校验后的官方程序完成用户目录安装，再验证安装版本，继续导出。

每次新安装实时读取 latest，不把版本固定为本次测试值。自动安装需要联网、可写用户目录和系统证书，可使用现有 HTTPS 代理；不安装 Node.js，不调用 sudo，不升级已有 CLI。官方 CLI 自身的系统及运行库要求仍须满足，依赖缺失或下载失败时明确报错。

使用 `--claude /usr/local/bin/claude` 可指定已有 CLI，Windows 路径含空格时加引号。显式路径无效时直接报错，避免把路径错误解释成新安装。Windows 使用原生 `.exe`，不调用 npm `.cmd` / `.bat` 包装器。自动安装后直接使用绝对路径，不要求重开终端或手工调整 PATH。

官方安装依据：[Claude Code 原生安装说明](https://code.claude.com/docs/en/setup)。自动安装仅访问官方分发服务；遥测身份初始化与安装下载使用独立的进程环境。

## 安装身份及资料内容

版本来自本机官方可执行文件的 `--version`。安装身份优先从当前 `CLAUDE_CONFIG_DIR` 或默认 `~/.claude.json` / `~/.claude/.claude.json` 读取，只提取官方 `userID`、`machineID`、有效安装时间，不复制账号资料。

缺少安装身份时，在 `<Claude 配置目录>/telemetry-export` 的独立持久目录中启动本机官方 CLI。该进程不继承账号凭据，禁用非必要流量、工具、MCP 和会话保存，API 及代理指向 loopback。即使未授权退出，也必须实际写出有效 ID 才允许导出；无法取得真实身份或版本时失败，不随机补值。

专用目录保留供后续复用，不覆盖常用 Claude 配置。使用原生架构工具；平台和架构反映工具运行环境，WSL 应使用 Linux 版本。生成的单个 JSON 就是资料包，无需 ZIP。已有输出文件不会覆盖，重复导出请换一个 `--output` 文件名。

| 数据 | 来源 |
| --- | --- |
| `device_id` | 本机官方配置的 `userID`，64 位十六进制 |
| `machine_id` | 本机官方配置的 `machineID`，保存在安装资料中 |
| CLI 版本、平台、架构、环境 | 导出设备真实观测；未知硬件省略 |
| 安装时间 | 仅在官方配置存在有效 `firstStartTime` 时加入 |
| 账号 UUID、组织、邮箱、订阅 | 目标网关账号；包内不包含，未知项省略 |
| 运行会话、事件 ID、时间和 process | 网关现有生成器 |
| 授权凭据、出口、TLS 和周期 | 目标网关账号及现有配置 |

包格式保持 `sub2api-account-telemetry` / `version: 2`，不包含令牌、账号、会话、聊天历史或原始配置文件。MAC、硬件序号仍属于设备资料，请只传递给自己的网关。格式校验不是官方签名或远程设备认证。

## 导入及验证

本工具包只在本地设备运行，网关网站不分发工具。超级管理员进入「账号管理 → 编辑目标账号 → 账号遥测 → 导入设备资料」，选择 JSON，最大 1 MiB。目标为可用的 Anthropic OAuth / Setup Token 账号。已支持 version 2 的网关无需修改接口即可接收该工具的输出。

同一设备包可用于多个账号，表示同一设备安装；账号属性和会话分别绑定。网关按需构造完整事件；替换账号身份时保留设备安装 ID、CLI 版本和环境，重新绑定会话。没有服务器 CLI 初始化或手工生成入口。发送仍由默认关闭的原开关控制，开启后按十分钟活动窗口、约十秒事件、六小时 GrowthBook 周期执行。

旧 Node.js 导出器和专属测试已移除；没有双实现、新网关接口、配置开关或数据库迁移。已经导出的合法 version 2 包仍然有效，version 1 仍不接收。客户端采集、OTLP 和样本库继续保持移除。

独立工具是未公证的本地构建产物；macOS 若阻止运行，可按系统“隐私与安全性”提示允许可信来源程序，不要求关闭 Gatekeeper。

## 维护者构建

实现位于 `backend/cmd/telemetry-export`，仅使用 Go 标准库。构建机需要仓库指定 Go 版本，使用者只需可执行文件。

```bash
cd backend
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o telemetry-export ./cmd/telemetry-export
```

交叉构建使用 `GOOS=windows|linux|darwin`、`GOARCH=amd64|arm64`，Windows 文件名为 `telemetry-export.exe`。分发时同时提供 SHA-256 校验和。完整生成和发送规则见 [账号遥测](ENGINEERING.md#req-identity)。
