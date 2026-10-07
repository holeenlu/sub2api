## 可下载 Skills

TapModels 提供两个独立 Skill：

| Skill | 固定模型 | 生成 | 编辑 |
| --- | --- | --- | --- |
| `gpt-image-flare` | `gpt-image-2.5-flare` | `/v1/images/generations` | `/v1/images/edits` |
| `gpt-image-sunburst` | `gpt-image-2.5-sunburst` | `/v1/images/generations` | `/v1/images/edits` |

下载 [Flare Skill](/downloads/gpt-image-flare.zip) 或 [Sunburst Skill](/downloads/gpt-image-sunburst.zip)。每个包包含 `SKILL.md`、Codex 展示元数据与 Python 调用脚本。

## Codex 桌面端安装

1. 完全退出 Codex。
2. ZIP 自带 `gpt-image-flare/` 或 `gpt-image-sunburst/` 顶层目录；该目录内直接包含 `SKILL.md`。
3. 把整个目录放入 `~/.agents/skills/`，最终路径应为 `~/.agents/skills/gpt-image-flare/SKILL.md` 或 `~/.agents/skills/gpt-image-sunburst/SKILL.md`。
4. 重启 Codex，在任务中输入 `$gpt-image-flare` 或 `$gpt-image-sunburst`。

## Codex CLI 安装

macOS / Linux 示例：

```bash
mkdir -p "$HOME/.agents/skills"
unzip -q -o ./gpt-image-flare.zip -d "$HOME/.agents/skills"
test -f "$HOME/.agents/skills/gpt-image-flare/SKILL.md"
python3 -m venv "$HOME/.agents/skills/gpt-image-flare/.venv"
"$HOME/.agents/skills/gpt-image-flare/.venv/bin/python" -m pip install -r "$HOME/.agents/skills/gpt-image-flare/requirements.txt"
"$HOME/.agents/skills/gpt-image-flare/.venv/bin/python" "$HOME/.agents/skills/gpt-image-flare/scripts/generate.py" --check-config
```

Windows PowerShell 示例：

```powershell
New-Item -ItemType Directory -Force "$env:USERPROFILE\.agents\skills" | Out-Null
Expand-Archive -Force .\gpt-image-flare.zip "$env:USERPROFILE\.agents\skills"
if (-not (Test-Path "$env:USERPROFILE\.agents\skills\gpt-image-flare\SKILL.md")) { throw "压缩包目录结构无效" }
py -3 -m venv "$env:USERPROFILE\.agents\skills\gpt-image-flare\.venv"
& "$env:USERPROFILE\.agents\skills\gpt-image-flare\.venv\Scripts\python.exe" -m pip install -r "$env:USERPROFILE\.agents\skills\gpt-image-flare\requirements.txt"
& "$env:USERPROFILE\.agents\skills\gpt-image-flare\.venv\Scripts\python.exe" "$env:USERPROFILE\.agents\skills\gpt-image-flare\scripts\generate.py" --check-config
```

脚本读取顶层 Codex `model_provider` / `model_providers` 的 `base_url` 和 `env_key`，不会把 Key 写入生成文件；不支持通过 Codex `profile` 选择 Provider。仅设置 `TAPMODELS_API_KEY` 时，URL 和环境变量名仍由 Provider 提供；只有明确设置 `TAPMODELS_BASE_URL` 才启用完整环境覆盖，此时也必须设置 `TAPMODELS_API_KEY`。Provider 地址必须是 HTTPS（仅回环测试允许 HTTP），请求重定向会被拒绝。

## 依赖与凭据

图片脚本的凭据读取规则与 Codex 登录文件不是同一回事：

| 当前 Codex 配置 | 图片脚本行为 |
| --- | --- |
| Provider 设置了 `env_key` | 使用指定变量；变量缺失即停止 |
| Provider 使用 `experimental_bearer_token` | 使用该 Provider 内的令牌 |
| Legacy 只在 `auth.json` 保存 Key | 脚本不读取该文件；使用下面的显式环境覆盖 |

Legacy 用户或图片与聊天使用不同分组时，在运行 Skill 的进程环境中同时设置以下两项。这里的 Key 必须属于开放对应图像模型的分组；仅设置 Key 不会替换现有 Provider 的认证模式。

```bash
export TAPMODELS_BASE_URL="{{API_ROOT}}"
export TAPMODELS_API_KEY="你的图片分组 API Key"
```

PowerShell 使用 `$env:TAPMODELS_BASE_URL="{{API_ROOT}}"` 和 `$env:TAPMODELS_API_KEY="你的图片分组 API Key"`。桌面端需要从该终端启动，详见 [Codex 配置教程](/apps/codex)。

要求 Python 3.11+ 和 Pillow。在 Skill 目录建立虚拟环境并安装随包列明的依赖：

```bash
python3 -m venv "$HOME/.agents/skills/gpt-image-flare/.venv"
"$HOME/.agents/skills/gpt-image-flare/.venv/bin/python" -m pip install -r "$HOME/.agents/skills/gpt-image-flare/requirements.txt"
```

Windows 用 `py -3 -m venv "$env:USERPROFILE\.agents\skills\gpt-image-flare\.venv"`，再使用 `.venv\Scripts\python.exe` 执行相同的 `-m pip install -r` 与脚本命令。Sunburst 将路径中的目录名对应替换。

明确设置 `TAPMODELS_BASE_URL` 时启用完整环境覆盖，并要求同时设置 `TAPMODELS_API_KEY`；没有该 URL 时读取当前 Codex Provider 的地址及上述凭据字段。缺少指定环境变量会报错，不回退到其他账号 Key。`--check-config` 只检查配置结构；`--dry-run` 还会校验请求参数和输入图片但不联网。实际调用还需要凭据、网络和模型权限。输出文件必须使用绝对路径，已有文件需显式 `--force` 覆盖。

新版官方推荐 `~/.agents/skills`。使用旧版或TapModels既有 `~/.codex/skills` 的客户端，可保留其实际发现路径；同名 Skill 不要装两份。安装后在任务输入框输入 `$gpt-image-flare` 检查是否出现，未出现则重启并检查目录层级。

## 使用

生成图片：

```text
$gpt-image-flare 生成一张白色背景的精密产品图，1024x1024，高质量。
```

编辑图片时附上本地图片，并明确写出需要修改及必须保留的内容。Skill 会在有输入图时使用编辑接口。每次执行会产生一次计费请求，不会自动重试；结果会验证实际文件格式与尺寸。

安装依据：[Codex 官方 Skills](https://developers.openai.com/codex/skills/)。下载脚本仅做本地和模拟请求测试；实际模型调用取决于 Key 分组。
