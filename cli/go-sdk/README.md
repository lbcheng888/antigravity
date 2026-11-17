# `agy-sdk` —— 可独立编译运行的 Antigravity 核心 Go SDK 与 CLI

本目录将从 `/Users/mac/.local/bin/agy`（`v1.5.10`）逆向还原出的 **核心子系统**，整理为一个 **100% 仅依赖 Go 标准库（零第三方依赖、免 CGO）**、可直接 `go vet` / `go test` / `go build` 编译运行的独立工程：

| Go 包 | 对应原生逆向源 | 核心能力 |
| :--- | :--- | :--- |
| `pkg/auth` | `cli/backend/auth/*` + `code_assist_client/*token_storage*` | `ChainedAuth` 多级认证（`GEMINI_API_KEY` / `AGY_LLM_GATEWAY_URL` / ADC / Keychain + File 双层存储）、`SSH_*`/Docker/WSL 钥匙串旁路、1 小时故障降级标记、PKCE `S256` 本地回环 OAuth 2.0 登录与自动刷新 |
| `pkg/aicode` | `code_assist_client/{aicode,generation,http_helpers,stream_helpers,model_configs,user_management}.go` + `v1internal` / `businessaicode` Protos | CloudCode PA (`daily-cloudcode-pa.googleapis.com/v1internal:*`)、AICode (`aicode.googleapis.com/v1alpha`)、Gemini API、Gateway 四路请求与通用 SSE 空闲超时流解析 |
| `pkg/cortex` | `cortex/{engine,permissions,tools,transcript}` | 多轮 ReAct `Engine.RunTurn` 工具循环、7 个工作区内置工具（`read_file`/`write_file`/`edit_file`/`list_dir`/`find_by_name`/`grep_search`/`run_command`）、`PermissionGuard` 沙箱边界与只读模式守卫、`TrajectoryRecorder` JSONL 轨迹录制 |
| `pkg/swarm` | `cortex/core/native_swarm/*` (`filestore`, `mentions`, `channels`, `wake`, `quota_pause`) | 兼容 `board.jsonl`（追加写 + 最近 64 行 `RequestID` 去重）、`members.json` 原子写入（含旧字段兼容）、`@name`/`@all`/`<name>-N`/`@#channel` 解析、频道订阅与 `WakeEngine` 唤醒状态机 |
| `pkg/acp` | `cli/acp/{jsonrpc,schema,messages,session,sessionconfig,stepview,server}` | 自动识别 NDJSON 与 `Content-Length` 帧头的 JSON-RPC 2.0 双向流、10 类 `SessionUpdate` 序列化、`session.Turn/Fence` 状态机、模型档位归并、22 类工具 `stepview` 映射，以及集成 `pkg/cortex` 的多轮工具闭环 `Server` |
| `pkg/mcp` | `cli/mcp/*` | `mcp_config.json`（`0600`）持久化管理、Stdio NDJSON 与 Streamable HTTP（JSON + SSE）MCP 客户端、`RegisterMCPTools` 桥接至 `cortex.Registry`（`mcp__<server>__<tool>`） |
| `pkg/customizations` | `cli/plugins/*` + `skills` + `rules` | `SKILL.md` Frontmatter 递归发现与 `<skills>` 注入、`GEMINI.md`/`AGENTS.md`/`CLAUDE.md` 规则加载、`plugin.json` / `.gemini-plugin/plugin.json` 校验与已安装插件列表 |
| `pkg/sessionstore` | `cli/sessions/*` | 会话 JSON 原子持久化（`NewSession`/`Save`/`Load`/`Latest`/`List`），支持 `-c`/`--continue` 与 `--conversation <id>` 断点续聊 |
| `pkg/planexec` | `cli/agent/planexec` | 双阶段（只读规划 `ModePlan` + `exit_plan_mode` -> 执行 `ModeAgent` + `enter_plan_mode`）多轮协同编排 `Runner` |
| `pkg/sandbox` | `cli/sandbox/*` + `trace_proxy` | macOS `/usr/bin/sandbox-exec` Seatbelt 沙箱工具 `NewSandboxedRunCommandTool`、无缓冲 SSE 透传与 `auth.MaskSecret` 脱敏 JSONL 审计代理 `TraceProxy` |
| `pkg/swarmrun` | `cli/swarm/orchestrator` | 本地多 Agent `Orchestrator`：将 `swarm`/`post_message`/`read_board`/`swarm_wait` 注册为 `cortex.Tool`，基于 `WakeEngine.Doorbells()` 调度协调者与 Worker 协程 |
| `pkg/printmode` | `cli/print/*` | Headless/CI 格式化输出（`text`/`json`/`stream-json` NDJSON）、`ReadStreamInput` 流式 JSON 输入解析、纯标准库 `ValidateJSONSchema` 输出校验 |
| `cmd/agy-sdk` | `cli/entrypoints` 核心子集 | 独立可执行二进制 `bin/agy-sdk`，支持 `version`/`help`、`auth`、`models`、`quota`、`prompt`、`agent`（含 `-c`/`--plan-model`/`--sandbox`/`--mcp`/`--skills`/`--output-format`/`--json-schema`）、`swarm inspect|run`、`mcp`、`plugin`、`skills`、`sessions`、`trace-proxy` 与 `acp --stdio` |

---

## 快速编译与验证

```sh
cd /Users/mac/antigravity/cli/go-sdk

# 静态检查、全量单元测试与编译
go vet ./...
go test -v ./...
go build -o bin/agy-sdk ./cmd/agy-sdk
```

## CLI 命令与使用示例

### 支持的子命令与参数

- `version` / `--version` / `-v`：输出 CLI 与 SDK 版本（`agy-sdk v1.5.10`）。
- `help` / `--help` / `-h`：输出帮助信息。
- `auth status [--data-dir <dir>] [--app-data-dir <name>] [--file] [--json]`：只读诊断认证链，所有 Token / API Key 均通过 `auth.MaskSecret` 脱敏。
- `auth login [--enterprise] [--data-dir <dir>] [--app-data-dir <name>] [--file] [--no-browser]`：启动本地回环 HTTP 回调服务，执行 PKCE `S256` OAuth 2.0 登录并持久化 Token。
- `auth logout [--data-dir <dir>] [--app-data-dir <name>] [--file]`：清除已存储的 OAuth 凭据。
- `models [--project <id>] [--json]`：调用 `aicode.Client.FetchAvailableModels` 查询可用模型及配额。
- `quota [--project <id>] [--json]`：调用 `aicode.Client.RetrieveUserQuotaSummary` 查询汇总配额桶。
- `prompt -p "<text>" [-m <model>] [--thoughts=true|false]`：调用 `aicode.Client.StreamGenerateContent` 实时流式打印思考块与文本回复。
- `agent [-p "<task>"] [-m <model>] [--cwd <dir>] [--mode agent|plan|ask] [--max-steps 25] [--yolo] [-c|--continue] [--conversation <id>] [--plan-model <model>] [--sandbox] [--mcp] [--skills] [--input-format text|stream-json] [--output-format text|json|stream-json] [--json-schema <schema>] [--transcript <file.jsonl>] [--thoughts]`：运行本地 Cortex 多轮工具调用 Agent；支持单次任务、交互式 REPL（`/help`、`/model`、`/mode`、`/clear`、`/exit`）、会话持久化续聊、双阶段规划执行、Seatbelt 沙箱、MCP/Skills 注入与 Headless NDJSON 输出/Schema 校验。
- `swarm inspect <swarm-dir> [--tail N] [--json]`：调用 `swarm.InspectDir` 解析并查看 `board.jsonl` + `members.json` 看板状态。
- `swarm run -p "<goal>" [-m <model>] [--cwd <dir>] [--swarm-dir <dir>] [--title <title>] [--max-steps 25] [--json]`：启动本地 `swarmrun.Orchestrator` 多 Agent 协同编排。
- `mcp <list|add|remove|enable|disable|tools> [--type stdio|http] [--env KEY=VAL] [--header KEY:VAL] [--data-dir <dir>] [--json]`：管理 `mcp_config.json` 服务器配置并列举远程 MCP 工具。
- `plugin <list|validate> [path] [--strict] [--plugins-dir <dir>] [--json]`：列举已安装插件或校验插件清单、`SKILL.md` 与 `mcp.json`。
- `skills [--cwd <dir>] [--skills-dir <dir>] [--json]`：扫描 `~/.gemini/antigravity/builtin/skills`、`~/.gemini/antigravity/skills` 与 `<cwd>/.gemini/skills` 中的 `SKILL.md`。
- `sessions <list|show> [id] [--cwd <dir>] [--data-dir <dir>] [--json]`：列举或查看已持久化的对话会话历史。
- `trace-proxy [--listen 127.0.0.1:8899] [--upstream https://daily-cloudcode-pa.googleapis.com] [--out trace.jsonl]`：启动支持 SSE 无缓冲透传与凭据脱敏的 HTTP/JSONL 审计代理。
- `acp [--stdio] [-m <default-model>] [--cwd <dir>] [--disable-tools] [--yolo]`：以 JSON-RPC 2.0 `--stdio` 模式启动 `acp.Server`（默认挂载 `cortex.NewDefaultRegistry()` 多轮工具闭环，`--disable-tools` 可回退至单轮流式模式）。

### 常用命令示例

```sh
# 1. 只读诊断本机认证链（自动对所有 Secret/Token 做脱敏显示，SSH_CONNECTION=1 可跳过钥匙串）
SSH_CONNECTION=1 ./bin/agy-sdk auth status

# 2. PKCE S256 OAuth 登录 / 登出（支持 --enterprise 与 --no-browser）
./bin/agy-sdk auth login --no-browser
./bin/agy-sdk auth logout

# 3. 解析并查看任意真实 Antigravity Swarm 看板（board.jsonl + members.json）
./bin/agy-sdk swarm inspect ~/.gemini/antigravity/brain/<conv-id>/.system_generated/swarm --tail 3

# 4. 查询当前账号可用模型目录与配额桶
./bin/agy-sdk models --json
./bin/agy-sdk quota

# 5. 发起一次流式模型推理（支持实时打印 [thought] 思考块与回复）
./bin/agy-sdk prompt -p "用一句话解释 Go interface" -m gemini-2.5-flash

# 6. 运行本地 Cortex Agent（单次自主任务或交互式 REPL）
./bin/agy-sdk agent -p "检查当前目录的 go.mod 并总结依赖" --cwd . --transcript /tmp/traj.jsonl
./bin/agy-sdk agent --cwd . --mode plan

# 7. 以 ACP JSON-RPC 2.0 --stdio 模式启动（默认启用 Cortex 多轮工具闭环）
./bin/agy-sdk acp --stdio -m gemini-4-hydrogen-medium --cwd .
```
