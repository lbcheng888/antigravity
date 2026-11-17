# `agy` 命令行参数与子命令参考

以下内容直接抓取自本机 `/Users/mac/.local/bin/agy`（v`1.5.9`）实测 `--help` 输出，完整原始 JSON 快照保存在 [cli-commands.json](file:///Users/mac/antigravity/cli/metadata/cli-commands.json)。

---

## 1. 主命令全局参数（`agy --help`）

### 会话与项目控制
- `--add-dir`：向工作区追加目录（可重复传参）
- `--project`：指定会话所属 Project ID 或名称
- `--new-project`：为当前会话新建项目
- `-c` / `--continue`：继续最近一次会话
- `--conversation`：按会话 ID 恢复历史对话
- `--agent`：指定本次会话使用的自定义 Agent
- `--model`：指定本次会话模型
- `--effort`：设置推理深度（`low|medium|high|xhigh|max`）
- `--mode`：设置执行模式（`accept-edits`、`auto`、`plan`）
- `--settings`：仅本次会话生效、不落盘的 JSON 配置（可传内联 JSON 或路径）
- `--log-file`：覆盖 CLI 日志路径

### 权限与安全边界
- `--sandbox`：启用终端限制的沙箱模式
- `--dangerously-skip-permissions`：跳过所有工具权限确认弹窗并自动放行

### 非交互 / 脚本模式（Print Mode）
- `-p` / `--print` / `--prompt`：运行单次提示并输出结果后退出
- `-i` / `--prompt-interactive`：带初始提示词进入交互会话
- `--input-format`：`text`（默认）或 `stream-json`（从 stdin 按行读取 NDJSON 消息并逐轮执行，需配合 `--output-format stream-json`）
- `--output-format`：`text`（默认）、`json` 或 `stream-json`
- `--json-schema`：为最终输出强制校验 JSON Schema（字符串或文件路径）
- `--print-timeout`：print 模式超时限制（默认 `0s` 等待回合结束）
- `--disable-slash-commands`：在 print 模式下禁用斜杠命令和 Skill 展开

### 远程代理与远程控制
- `--remote-agent`：使用远端 Agent 运行会话（可配合 `--project` 指定远端项目）
- `--remote-control`：启动 CLI 时同步建立远程控制连接

---

## 2. 子命令全览

| 子命令 | 用途 | 关键参数 / 次级子命令 |
| :--- | :--- | :--- |
| `acp` | 启动 Agent Client Protocol (ACP) stdio JSON-RPC 服务 | `--stdio`、`--login`、`--process-per-session`、`--ide-mcp-port`、`--app-data-dir`、`--cloud_code_endpoint`、`--sandbox`、`--dangerously-skip-permissions`、`--use_stubby_auth`、`--worker` |
| `agent` / `agents` | 列出可用 Agent | `-h` / `--help` |
| `changelog` | 查看版本更新日志 | `-h` / `--help` |
| `install` | 配置环境变量与 Shell PATH/Alias | `--dir`、`--skip-aliases`、`--skip-path` |
| `mcp` | 管理 MCP 服务器配置 | `add`（`--type stdio\|http`、`--env`、`--header`）、`remove`、`list`、`enable`、`disable` |
| `mic-serve` | 将本机麦克风音频流共享给另一台主机上的 CLI | `--addr`（默认 `127.0.0.1:4713`） |
| `models` | 列出可用模型 | `-h` / `--help` |
| `plugin` / `plugins` | 管理 Antigravity 插件 | `list [--json]`、`import [source]`、`install <target>`、`uninstall <name>`、`enable <name>`、`disable <name>`、`validate [path] [--strict] [--json]`、`link <mp> <target>` |
| `remote-control` | 管理本地远程控制后台守护进程 | `start [--name] [--session]`、`status`、`stop` |
| `update` | 升级 `agy` CLI | （无额外导出 help flags） |
| `help` | 显示各子命令帮助 | `agy help <subcommand>` |

---

## 3. 常用子命令语法示例

```sh
# 1) Print 模式流式 JSON（适合 CI / 脚本集成）
agy -p --input-format stream-json --output-format stream-json --json-schema schema.json

# 2) 注册 HTTP 或 Stdio MCP 服务器
agy mcp add fs npx -y @modelcontextprotocol/server-filesystem /work
agy mcp add --header "Authorization: Bearer TOKEN" api https://example.com/mcp
agy mcp add --env GITHUB_TOKEN=xxx gh -- docker run -i ghcr.io/github/mcp

# 3) 以 ACP stdio 模式接入外部编辑器，每个会话隔离 worker
agy acp --stdio --process-per-session

# 4) 校验本地插件清单是否符合加载器规范
agy plugin validate ./my-plugin --strict --json
```
