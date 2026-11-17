# Go 模块树、RPC 服务与解包资源映射

数据全部来源于 [extract-agy.py](file:///Users/mac/antigravity/cli/scripts/extract-agy.py) 对 `/Users/mac/.local/bin/agy` 的 `__lrodata_gopcln`、`__DATA_CONST` 与压缩段解析，并保存在 [metadata/](file:///Users/mac/antigravity/cli/metadata)。

---

## 1. CLI 前端层核心包（`third_party/jetski/cli/*`，共 64 个包）

| Go 包路径 | 文件数 | 函数数 | 机器码大小 | 职责说明 |
| :--- | ---: | ---: | ---: | :--- |
| `cli/model/model` | 130 | 2,605 | 1,901,600 B | TUI 主状态机、消息流、输入区、弹窗、补全与交互生命周期 |
| `cli/store/store` | 92 | 1,278 | 648,832 B | 会话、轨迹、项目、权限及本地缓存存储 |
| `cli/render/render` | 38 | 342 | 333,056 B | Markdown / 代码高亮 / Diff / 表格 / 流式终端渲染 |
| `cli/backend/backend` | 30 | 546 | 238,864 B | `ServerBackend`：直连进程内引擎、管理配置/Hooks/Keybindings/Effort/Search |
| `cli/acp/acp` | 18 | 515 | 229,632 B | Agent Client Protocol (ACP) stdio JSON-RPC 服务与 session worker |
| `cli/plugins/plugins` | 20 | 206 | 171,664 B | 插件安装、卸载、启用/禁用、校验（含 `claude` / `gemini` 导入） |
| `cli/swarm/tui/tui` | 13 | 113 | 84,528 B | 原生多代理 Swarm 终端看板与导航 |
| `cli/printmode/printmode` | 16 | 126 | 80,032 B | `-p` / `--print` 非交互与 `stream-json` NDJSON 流模式 |
| `cli/mermaid/mermaid` | 7 | 89 | 76,528 B | 终端 Mermaid 图渲染 |
| `cli/types/types` | 19 | 152 | 64,448 B | CLI 内部事件与视图数据结构 |
| `cli/acp/acpauth/acpauth` | 4 | 110 | 63,136 B | ACP 模式下配置解析、Gateway / Vertex / OAuth loopback |
| `cli/commands/commands` | 41 | 244 | 58,016 B | `/` 斜杠命令实现 |
| `cli/editing/editing` | 7 | 169 | 55,456 B | 终端多行输入编辑（含 `cli/editing/vim`、`undo`、`textedit`） |
| `cli/keybindings/keybindings` | 9 | 71 | 46,240 B | 自定义快捷键解析与绑定 |
| `cli/analytics/analytics` | 8 | 79 | 45,712 B | CLI 使用指标记录 |
| `cli/entrypoints/entrypoints` | 11 | 76 | 45,280 B | `launchCLI`、子命令路由、项目/模型覆写参数解析 |
| `cli/latex/latex` | 2 | 56 | 39,472 B | LaTeX 数学表达式终端渲染 |
| `cli/backend/auth/auth` | 11 | 91 | 37,488 B | `ChainedAuth`、API Key、ADC、Gateway、Keyring、浏览器 OAuth / WIF |
| `cli/remotecontrol/remotecontrol` | 4 | 28 | 12,976 B | macOS 远程控制守护进程管理（`daemon_darwin.go`） |
| `cli/audio/*` | — | — | — | `audio`、`micserve`、`miniaudio`（CGO 音频采集）、`micwire`、`micperm` |

完整列表见 [cli-files.txt](file:///Users/mac/antigravity/cli/metadata/cli-files.txt)（644 个文件）与 [jetski-functions.jsonl](file:///Users/mac/antigravity/cli/metadata/jetski-functions.jsonl)（71,955 个函数符号）。

---

## 2. 进程内引擎层核心包（`language_server/*` 与 `cortex/*`）

| Go 包路径 | 文件数 | 函数数 | 机器码大小 | 职责说明 |
| :--- | ---: | ---: | ---: | :--- |
| `language_server/language_server` | 113 | 1,422 | 753,536 B | 语言服务器核心服务实现与本地管理 |
| `language_server/code_assist_client/codeassistclient` | 48 | 967 | 410,736 B | Cloud Code Assist 请求客户端 |
| `language_server/backend/cloudproxy/cloudproxy` | 27 | 523 | 252,736 B | 远程代理与云端转发后端 |
| `language_server/remotecontrol/remotecontrol` | 9 | 337 | 175,344 B | WebRTC / 信令与远程会话通道 |
| `cortex/customizations/customizations` | 38 | 747 | 366,352 B | Skills、Hooks、Rules、Workflows、Plugins 加载器 |
| `cortex/policyguardian/vetting/vetting` | 29 | 401 | 291,792 B | 工具调用与命令安全审查 |
| `cortex/cortex` | 15 | 531 | 271,200 B | Cortex 执行编排与轨迹步进引擎 |
| `cortex/utils/utils` | 41 | 500 | 250,112 B | 上下文、媒体、文件工具集 |
| `cortex/ext/plugins/computer/computer` | 26 | 266 | 197,408 B | Computer-Use 桌面插件（共享库模块） |
| `cortex/handlers/handlers` | 27 | 225 | 169,568 B | 模型/工具处理器 |
| `cortex/ext/plugins/browser/browser` | 9 | 213 | 159,376 B | 浏览器控制插件 |
| `cortex/policyguardian/policyguardian` | 13 | 140 | 146,320 B | 权限与策略判定 |
| `cortex/core/native_swarm/nativeswarm` | 17 | 191 | 135,632 B | 原生 Swarm 消息板与协调原语 |
| `cortex/command/sandboxproxy/sandboxproxy` | 16 | 263 | 133,040 B | `--sandbox` 终端命令沙箱代理 |
| `cortex/permissions/permissions` | 9 | 124 | 129,408 B | 对话级权限解析 |
| `cortex/utils/mcp/mcp` | 12 | 282 | 116,144 B | MCP 客户端与传输层 |

---

## 3. 从符号表恢复的 19 个 Protobuf / ConnectRPC / gRPC 服务

完整方法名清单保存在 [proto-services.json](file:///Users/mac/antigravity/cli/metadata/proto-services.json)：

| 服务全称 | 方法数 | 用途 |
| :--- | ---: | :--- |
| `exa.language_server_pb.LanguageServerService` | 377 | 本地引擎核心 API（会话/Cascade、轨迹、权限、Git/Worktree、插件/MCP、终端、审批等） |
| `exa.api_server_pb.ApiServerService` | 114 | 云端 API 服务（模型配置、MQuery、Embedding、Web 搜索、遥测/组织策略等） |
| `exa.seat_management_pb.SeatManagementService` | 96 | 座席许可、团队域名、审批与配额管理 |
| `exa.extension_server_pb.ExtensionServerService` | 53 | 宿主 IDE 扩展反向回调（编辑 Notebook、打开设置、执行命令、音频录制等） |
| `exa.opensearch_clients_pb.KnowledgeBaseService` | 20 | 知识库索引与 GitHub/Slack 等数据源集成 |
| `exa.index_pb.IndexManagementService` | 19 | 仓库索引生命周期管理 |
| `exa.analytics_pb.AnalyticsService` | 7 | 轨迹与补全指标上报 |
| `exa.remoting.RemotingService` | 7 | 远程主机连接管理（`ConnectRemote`、`ListRemotes`、`RespondToPrompt` 等） |
| `exa.computer_viewer_pb.ComputerViewerService` | 5 | 桌面远程视图管理 |
| `jetski.product.v1.ConversationService` | 5 | 对话 CRUD 服务 |
| `exa.actuator_pb.ActuatorService` | 4 | 桌面/浏览器执行器指令流 |
| `exa.index_pb.IndexService` | 4 | 语义检索与向量查询 |
| `exa.opensearch_clients_pb.CodeIndexService` | 4 | 图检索与混合代码检索 |
| `exa.cascade_plugins_pb.CascadePluginsService` | 3 | Cascade 插件市场查询与安装 |
| `exa.host_bridge_pb.HostBridgeService` | 3 | `GetOAuthToken`、`ApplyUpdate`、`GetUpdateStatus`（与 Electron 端 `HostBridgeServer` 对应） |
| `exa.plugin_context_pb.ContextProviderService` | 3 | StdioRPC 插件上下文探测与解析 |
| `exa.dev_pb.DevService` | 1 | 开发调试服务 |
| `google3/third_party/jetski/api_server_pb.EvalApiServerService` | 1 | 评测 API 服务 |
| `google3/third_party/jetski/chat_client_server_pb.ChatClientServerService` | 1 | 聊天客户端流服务 |

---

## 4. 解包出的全部内嵌静态资源清单

详见 [embedded-manifest.json](file:///Users/mac/antigravity/cli/metadata/embedded-manifest.json)：

| 目标目录 | 来源类型 | 文件数 | 大小 | 内容说明 |
| :--- | :--- | ---: | ---: | :--- |
| [prompts](file:///Users/mac/antigravity/cli/extracted/prompts) | `embed.FS` | 16 | 28.1 KB | `identity.tmpl`、`guidelines.tmpl`、`skills.tmpl`、`slash_commands.tmpl` 等 Prompt 模板 |
| [metaschemas](file:///Users/mac/antigravity/cli/extracted/metaschemas) | `embed.FS` | 35 | 43.7 KB | JSON Schema 2019-09 / 2020-12 元模式定义 |
| [assets](file:///Users/mac/antigravity/cli/extracted/assets) | `embed.FS` | 42 | 543.1 KB | 内置 Skills 文档、`swarm` 插件与完整可读未压缩的 `swarm_board` Sidecar (`main.mjs` / `channels.mjs` / `app.js` / `hive.js`) |
| [chroma-styles](file:///Users/mac/antigravity/cli/extracted/chroma-styles) | `embed.FS` | 74 | 155.8 KB | 语法高亮配色主题 XML |
| [chroma-lexers](file:///Users/mac/antigravity/cli/extracted/chroma-lexers) | `embed.FS` | 279 | 1.88 MB | 语法高亮词法规则 XML |
| [web-bundle](file:///Users/mac/antigravity/cli/extracted/web-bundle) | ZIP | 540 | 18.88 MB | `Jetski Web` 前端包（`index.html`、`main.js`、`page_agent_bundle.js`、`page_facade_bundle.js`、`diff_worker.js`、`audio_processor.js` 等） |
| [tzdata](file:///Users/mac/antigravity/cli/extracted/tzdata) | ZIP | 598 | 337.9 KB | Go `time/tzdata` 嵌入式时区库 |
| [tsserver](file:///Users/mac/antigravity/cli/extracted/tsserver) | `.tar.gz` | 113 | 12.37 MB | `lib/_tsserver.js` 与 TypeScript 标准库 `.d.ts` |
| [python](file:///Users/mac/antigravity/cli/extracted/python) | `.tar.gz` | 1,366 (+8 symlinks) | 55.39 MB | 嵌入式独立 Python 3.13 运行时 |
| [fonts](file:///Users/mac/antigravity/cli/extracted/fonts) | Gzip | 5 | 3.61 MB | 内嵌 TTF 字体文件 |
| [proto-descriptors](file:///Users/mac/antigravity/cli/extracted/proto-descriptors) | Gzip | 17 | 20.1 KB | `google/protobuf/*.pb` 与 `errorspb/*.pb` 描述符 |
