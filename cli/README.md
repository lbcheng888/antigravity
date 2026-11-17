# Antigravity CLI (`agy`) 离线解包、深度逆向与源码还原工程

本目录是将本机 `/Users/mac/.local/bin/agy`（v`1.5.10`，`224,809,248` 字节）中的全部**可解包内嵌资源**、**Go 符号/反射类型/Protobuf 描述符**以及**核心模块 1:1 等价 Go 源码**无损提取并深度还原成的离线分析工程。原二进制未被修改。

> [!NOTE]
> 与仓库根目录中从 [Antigravity.app](file:///Applications/Antigravity.app) 解包还原的 Electron/TypeScript 桌面端工程不同，`/Users/mac/.local/bin/agy` 是一个由 Google 内部 Go 1.28 编译的 `Mach-O 64-bit executable arm64` 原生可执行文件（不含 `app.asar` / `NODE_SEA`），核心逻辑已被编译为 `__TEXT` 机器码。因此本工程不仅完成 L1/L2 静态解包与 `gopclntab` 符号提取，还进一步完成了 L3 全量 `.proto` 与 `__go_type` Go 类型表导出、L4-A ARM64 反编译工作台与跨包调用图构建，以及 L4-B 三大核心子系统的 1:1 Go 源码语义还原。

---

## 目录结构

| 路径 | 说明 |
| :--- | :--- |
| [bin/agy-info.json](file:///Users/mac/antigravity/cli/bin/agy-info.json) | 源二进制 SHA-256、Mach-O 格式、Go 构建版本与动态链接库快照（`v1.5.10`） |
| [docs/architecture.md](file:///Users/mac/antigravity/cli/docs/architecture.md) | `agy` 单体六层架构、与桌面端的同源差异及认证/运行模式分析 |
| [docs/auth-and-requests.md](file:///Users/mac/antigravity/cli/docs/auth-and-requests.md) | CLI (`agy`) 与桌面版 (`Antigravity.app`) 授权及请求链路同源对比分析 |
| [docs/cli-reference.md](file:///Users/mac/antigravity/cli/docs/cli-reference.md) | 实测抓取的 `agy` 主参数与所有子命令参数参考 |
| [docs/package-map.md](file:///Users/mac/antigravity/cli/docs/package-map.md) | Go 模块分类统计、19 个 Protobuf/ConnectRPC 服务与提取清单 |
| [docs/deep-reverse-plan.md](file:///Users/mac/antigravity/cli/docs/deep-reverse-plan.md) | L3/L4 深度向下逆向技术方案与实测验收报告 |
| [docs/reconstructed-subsystems.md](file:///Users/mac/antigravity/cli/docs/reconstructed-subsystems.md) | L4-B 核心子系统（`cli/backend/auth`、`native_swarm`、`cli/acp`）1:1 源码还原文档 |
| [metadata/](file:///Users/mac/antigravity/cli/metadata) | Go 文件/符号/包索引、`proto-services.json`、`recovered-protos.json`（400 Protos）、`recovered-gotypes.json`（95,644 Go Types）、`callgraph-summary.json`（55,834 节点 / 260,173 边） |
| [extracted/](file:///Users/mac/antigravity/cli/extracted) | 从二进制无损解包出的 `prompts/`、`assets/`、`metaschemas/`、`chroma-*`、`web-bundle/`、`tsserver/`、`python/`、`tzdata/`、`fonts/`、`proto-descriptors/` |
| [reconstructed/](file:///Users/mac/antigravity/cli/reconstructed) | 深度还原产物：`proto/`（400 个 `.proto`）、`go-types/`（396 个包 `types_recovered.go`）、`src/`（82 个 1:1 语义还原 `.go` 源文件，`code_assist_client` 14 个 + `cli/backend/auth` 11 个 + `native_swarm` 13 个 + `cli/acp` 44 个） |
| [go-sdk/](file:///Users/mac/antigravity/cli/go-sdk) | **L5~L7 独立可编译运行全栈 Go SDK、Cortex 本地 Agent 与 CLI**（`module antigravity.dev/agy-sdk`，Go `1.27.4` 纯标准库零第三方依赖：12 个 `pkg/*` 核心包 + `cmd/agy-sdk` -> `bin/agy-sdk`） |
| [src/](file:///Users/mac/antigravity/cli/src) | 严格类型 TypeScript 分析工具（[cliAnalyzer.ts](file:///Users/mac/antigravity/cli/src/cliAnalyzer.ts)、[deepAnalyzer.ts](file:///Users/mac/antigravity/cli/src/deepAnalyzer.ts)、[metadataLoader.ts](file:///Users/mac/antigravity/cli/src/metadataLoader.ts)、[types.ts](file:///Users/mac/antigravity/cli/src/types.ts)） |
| [scripts/](file:///Users/mac/antigravity/cli/scripts) | 提取/还原/反编译/校验脚本（[extract-agy.py](file:///Users/mac/antigravity/cli/scripts/extract-agy.py)、[recover-protos.py](file:///Users/mac/antigravity/cli/scripts/recover-protos.py)、[recover-gotypes.py](file:///Users/mac/antigravity/cli/scripts/recover-gotypes.py)、[decompile-workbench.py](file:///Users/mac/antigravity/cli/scripts/decompile-workbench.py)、[check-cli.js](file:///Users/mac/antigravity/cli/scripts/check-cli.js)） |

---

## 常用离线分析与逆向命令

所有命令均在 [cli/](file:///Users/mac/antigravity/cli) 目录下执行（或在根目录加 `--prefix cli`）：

```sh
# 1. 类型检查分析工具 TS 代码
npm --prefix cli run typecheck

# 2. 编译并生成全局二进制与架构摘要报告
npm --prefix cli run report

# 3. 统一检索已还原的 Proto / Go Struct / Callgraph / 符号，或联动反编译指定函数
npm --prefix cli run deep-query -- "nativeswarm"
node cli/build/deepAnalyzer.js --decompile NewCLITokenStorage --limit 1 --pseudo-only

# 4. 直接使用 ARM64 反编译工作台输出带源码行号、字符串与类型的伪代码视图
python3 cli/scripts/decompile-workbench.py --dump-func NewCLITokenStorage

# 5. 重新导出全量 .proto / Go 反射类型 / 全仓调用图
npm --prefix cli run recover:protos
npm --prefix cli run recover:gotypes
npm --prefix cli run callgraph

# 6. 校验全部 L1~L7 提取、还原与全栈 Go SDK / CLI 产物一致性
npm --prefix cli run check

# 7. Go SDK 静态检查、单元测试与编译（零第三方依赖）
npm --prefix cli run go:vet
npm --prefix cli run go:test
npm --prefix cli run go:build

# 8. 如需从原二进制重新完整解包（先清理 extracted/ 后重跑）
npm --prefix cli run extract
```

---

## L5 ~ L7 全栈 Go SDK、Cortex 本地 Agent 与 CLI（`cli/go-sdk`）

在 L4-B `82` 个 1:1 逆向还原 `.go` 源文件基础上，[cli/go-sdk/](file:///Users/mac/antigravity/cli/go-sdk)（`module antigravity.dev/agy-sdk`，Go `1.27.4` `/Users/mac/.local/bin/go`，**100% Go 标准库、零第三方依赖、免 CGO**）进一步落地为覆盖 **L5 核心通信与认证**、**L6 Cortex 本地 Agent 多轮工具闭环** 与 **L7 全栈子系统（MCP/插件/技能/会话/双阶段规划执行/Seatbelt 沙箱/追踪代理/本地 Swarm 编排/Headless 输出与 Schema 校验）** 的可运行 Go SDK 与独立 CLI 工具 `cli/go-sdk/bin/agy-sdk`（详见 [go-sdk/README.md](file:///Users/mac/antigravity/cli/go-sdk/README.md)）。

### 包结构与职责（12 个 `pkg/*` + `cmd/agy-sdk`）

| Go 包 | 对应原生逆向源 | 核心能力 |
| :--- | :--- | :--- |
| `pkg/auth` | `cli/backend/auth/*` + `code_assist_client/*token_storage*` | `ChainedAuth` 多级认证（`GEMINI_API_KEY` / `AGY_LLM_GATEWAY_URL` / ADC / Keychain + File 双层存储）、`SSH_*`/Docker/WSL 钥匙串旁路、1 小时故障降级标记、PKCE `S256` 本地回环 OAuth 2.0 登录与自动刷新 |
| `pkg/aicode` | `code_assist_client/{aicode,generation,http_helpers,stream_helpers,model_configs,user_management}.go` + `v1internal` / `businessaicode` Protos | CloudCode PA (`daily-cloudcode-pa.googleapis.com/v1internal:*`)、AICode (`aicode.googleapis.com/v1alpha`)、Gemini API、Gateway 四路请求与通用 SSE 空闲超时流解析 |
| `pkg/cortex` | `cortex/{engine,permissions,tools,transcript}` | **L6 Cortex 本地 Agent 引擎**：`Engine.RunTurn` 多轮 ReAct 函数调用循环（默认上限 `DefaultMaxSteps = 25`）、7 个内置工作区工具（`read_file`/`write_file`/`edit_file`/`list_dir`/`find_by_name`/`grep_search`/`run_command`）、`PermissionGuard` 工作区沙箱与 `agent`/`plan`/`ask` 只读模式守卫、`TrajectoryRecorder` JSONL 轨迹录制 |
| `pkg/swarm` | `cortex/core/native_swarm/*` (`filestore`, `mentions`, `channels`, `wake`, `quota_pause`) | 兼容 `board.jsonl`（追加写 + 最近 64 行 `RequestID` 去重）、`members.json` 原子写入、`@name`/`@all`/`<name>-N`/`@#channel` 解析、频道订阅与 `WakeEngine` 唤醒状态机 |
| `pkg/acp` | `cli/acp/{jsonrpc,schema,messages,session,sessionconfig,stepview,server}` | 自动识别 NDJSON 与 `Content-Length` 帧头的 JSON-RPC 2.0 双向流、10 类 `SessionUpdate` 序列化、`session.Turn/Fence` 状态机、模型档位归并、22 类工具 `stepview` 映射，以及默认挂载 `pkg/cortex` 多轮工具闭环的完整 `Server` |
| `pkg/mcp` | `cli/mcp/*` | **L7**：`mcp_config.json`（`0600`）持久化管理（`ConfigStore`）、Stdio NDJSON 与 Streamable HTTP（`application/json` + `text/event-stream`）MCP 客户端、`RegisterMCPTools` 桥接至 `cortex.Registry`（`mcp__<server>__<tool>`） |
| `pkg/customizations` | `cli/plugins/*` + `skills` + `rules` | **L7**：`SKILL.md` Frontmatter 递归发现与 `<skills>` 注入、`GEMINI.md`/`AGENTS.md`/`CLAUDE.md` 规则加载、`plugin.json` / `.gemini-plugin/plugin.json` 校验（`ValidatePlugin`）与已安装插件列举（`ListInstalledPlugins`） |
| `pkg/sessionstore` | `cli/sessions/*` | **L7**：会话 JSON 原子持久化（`NewSession`/`Save`/`Load`/`Latest`/`List`），支持 `-c`/`--continue` 与 `--conversation <id>` 断点续聊 |
| `pkg/planexec` | `cli/agent/planexec` | **L7**：双阶段（只读规划 `ModePlan` + `exit_plan_mode` -> 执行 `ModeAgent` + `enter_plan_mode`）多轮协同编排 `Runner` |
| `pkg/sandbox` | `cli/sandbox/*` + `trace_proxy` | **L7**：macOS `/usr/bin/sandbox-exec` Seatbelt 沙箱工具 `NewSandboxedRunCommandTool`、无缓冲 SSE 透传与 `auth.MaskSecret` 脱敏 JSONL 审计代理 `TraceProxy` |
| `pkg/swarmrun` | `cli/swarm/orchestrator` | **L7**：本地多 Agent `Orchestrator`：将 `swarm`/`post_message`/`read_board`/`swarm_wait` 注册为 `cortex.Tool`，基于 `WakeEngine.Doorbells()` 调度协调者与 Worker 协程 |
| `pkg/printmode` | `cli/print/*` | **L7**：Headless/CI 格式化输出（`text`/`json`/`stream-json` NDJSON）、`ReadStreamInput` 流式 JSON 输入解析、纯标准库 `ValidateJSONSchema` 输出校验 |
| `cmd/agy-sdk` | `cli/entrypoints` 核心子集 | 独立可执行二进制 `cli/go-sdk/bin/agy-sdk`，提供 `version`/`help`、`auth`、`models`、`quota`、`prompt`、`agent`、`swarm inspect|run`、`mcp`、`plugin`、`skills`、`sessions`、`trace-proxy` 与 `acp --stdio` |

### L6 Cortex 与 L7 全栈能力矩阵

- **7 个内置工具（`cortex.NewDefaultRegistry()`）**：`read_file`（按行区间读取，800 行 / 46,080 字节上限）、`write_file`（写入/覆盖/追加并自动建父目录）、`edit_file`（精确替换并返回 `IsDiff` 差异元数据）、`list_dir`、`find_by_name`、`grep_search`（纯 Go 搜索，50 条匹配上限）、`run_command`（`/bin/sh -c` 有界超时执行；传入 `--sandbox` 时由 `sandbox.NewSandboxedRunCommandTool` 接管）。
- **沙箱权限守卫（`cortex.PermissionGuard` + `pkg/sandbox`）**：基于 `WorkspaceRoot` + `AdditionalDirs` 解析符号链接并阻止 `..` 路径逃逸（`--yolo` 可显式放开）；在 `plan` 与 `ask` 模式下拦截所有非只读工具；`--sandbox` 启用 macOS Seatbelt Profile 隔离。
- **`agy-sdk agent` 单次 / 交互式 REPL / Headless 流水线**：
  - 传入 `-p "<task>"` 执行自主多轮工具闭环；省略 `-p` 进入交互式 REPL（`/help`、`/model <id>`、`/mode <agent|plan|ask>`、`/clear`、`/exit`）。
  - 支持 `-c`/`--continue`、`-conversation`/`--conversation <id>` 断点续聊，`--plan-model <model>` 双阶段规划执行，`--mcp` 挂载已启用 MCP 工具，`--skills` 注入 `SKILL.md` 与规则，`--input-format text|stream-json`、`--output-format text|json|stream-json` 与 `--json-schema <inline-or-path>` 自动化验收。
- **L7 独立子命令**：`mcp <list|add|remove|enable|disable|tools>`、`plugin <list|validate>`、`skills`、`sessions <list|show>`、`swarm run`、`trace-proxy`。
- **`agy-sdk acp` 多轮工具闭环**：`acp.Server` 默认挂载 `cortex.NewDefaultRegistry()`，通过 `ContentToolCallContent` / `DiffToolCallContent` 实时推送工具执行与文件 diff；传入 `--disable-tools` 可回退至纯单轮流式模式。

### CLI 用法示例（`cli/go-sdk/bin/agy-sdk`）

```sh
# 版本与帮助
cli/go-sdk/bin/agy-sdk --version
cli/go-sdk/bin/agy-sdk --help

# 认证链诊断（所有 Secret/Token 经 auth.MaskSecret 脱敏）、PKCE S256 OAuth 登录与登出
SSH_CONNECTION=1 cli/go-sdk/bin/agy-sdk auth status [--data-dir <dir>] [--app-data-dir <name>] [--file] [--json]
cli/go-sdk/bin/agy-sdk auth login [--enterprise] [--data-dir <dir>] [--app-data-dir <name>] [--file] [--no-browser]
cli/go-sdk/bin/agy-sdk auth logout [--data-dir <dir>] [--app-data-dir <name>] [--file]

# 模型目录与配额查询
cli/go-sdk/bin/agy-sdk models [--project <id>] [--json]
cli/go-sdk/bin/agy-sdk quota [--project <id>] [--json]

# 流式模型推理（支持实时打印思考块与回复）
cli/go-sdk/bin/agy-sdk prompt -p "用一句话解释 Go interface" [-m <model>] [--thoughts=true|false]

# L6/L7 本地 Cortex Agent（单次任务、交互式 REPL、断点续聊、规划执行、沙箱、MCP/Skills、Headless JSON）
cli/go-sdk/bin/agy-sdk agent [-p "<task>"] [-m <model>] [--cwd <dir>] [--mode agent|plan|ask] [--max-steps 25] [--yolo] [-c|--continue] [--conversation <id>] [--plan-model <model>] [--sandbox] [--mcp] [--skills] [--input-format text|stream-json] [--output-format text|json|stream-json] [--json-schema <schema>] [--transcript <file.jsonl>] [--thoughts] [--data-dir <dir>] [--app-data-dir <name>] [--file]

# L7 MCP 服务器管理与工具列举
cli/go-sdk/bin/agy-sdk mcp <list|add|remove|enable|disable|tools> [--type stdio|http] [--env KEY=VAL] [--header KEY:VAL] [--data-dir <dir>] [--json]

# L7 插件校验 / 列表 与 Skills 发现
cli/go-sdk/bin/agy-sdk plugin <list|validate> [path] [--strict] [--plugins-dir <dir>] [--json]
cli/go-sdk/bin/agy-sdk skills [--cwd <dir>] [--skills-dir <dir>] [--json]

# L7 会话历史查询
cli/go-sdk/bin/agy-sdk sessions <list|show> [id] [--cwd <dir>] [--data-dir <dir>] [--json]

# Swarm 看板只读检查与本地多 Agent 协同编排
cli/go-sdk/bin/agy-sdk swarm inspect ~/.gemini/antigravity/brain/<conv-id>/.system_generated/swarm [--tail N] [--json]
cli/go-sdk/bin/agy-sdk swarm run -p "<goal>" [-m <model>] [--cwd <dir>] [--swarm-dir <dir>] [--title <title>] [--max-steps 25] [--json]

# L7 SSE 透明审计代理（自动脱敏 Authorization / x-goog-api-key 等凭据）
cli/go-sdk/bin/agy-sdk trace-proxy [--listen 127.0.0.1:8899] [--upstream https://daily-cloudcode-pa.googleapis.com] [--out trace.jsonl]

# ACP JSON-RPC 2.0 --stdio 服务端（默认启用 Cortex 多轮工具闭环）
cli/go-sdk/bin/agy-sdk acp [--stdio] [-m <default-model>] [--cwd <dir>] [--disable-tools] [--yolo]
```
