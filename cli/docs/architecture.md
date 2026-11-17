# Antigravity CLI (`agy`) 离线二进制架构分析

本文基于对 `/Users/mac/.local/bin/agy`（版本 `1.5.9`，`223,424,816` 字节，`Mach-O 64-bit executable arm64`）的无损结构解析与 `gopclntab` 符号表还原，结合桌面端（[docs/反重力授权请求.md](file:///Users/mac/antigravity/docs/%E5%8F%8D%E9%87%8D%E5%8A%9B%E6%8E%88%E6%9D%83%E8%AF%B7%E6%B1%82.md)）做对照说明。

---

## 1. 二进制总体解剖

| 属性 | 实测值 | 说明 |
| :--- | :--- | :--- |
| 文件路径 | `/Users/mac/.local/bin/agy` | 独立终端二进制 |
| 版本 | `1.5.9` | `agy --version` 输出 |
| SHA-256 | `fad3628ad91fe8613a72f074a1c5fde7cd816517788a5b9be96703f7d256741a` | 快照记录于 [agy-info.json](file:///Users/mac/antigravity/cli/bin/agy-info.json) |
| Go 构建信息 | `ego1.28-20261102-RC00 cl/1012564305 +298f53e483 X:fieldtrack,boringcrypto,simd,nogolistexportnewformat` | Google 内部 Go 1.28 工具链，启用 BoringCrypto 与 FIPS 信息 |
| Mach-O 段布局 | `__PAGEZERO` + `__TEXT` (`144.88 MB`) + `__DATA_CONST` (`24.94 MB`) + `__DATA` (`41.78 MB`) + `__LINKEDIT` (`1.48 MB`) | 4 段字节总和刚好等于文件总长 `223,424,816`，无 `app.asar` 或 `NODE_SEA` |
| macOS 动态库 | `libresolv.9.dylib`、`CoreFoundation`、`Security`、`AVFoundation`、`AudioToolbox`、`CoreAudio`、`IOKit`、`CoreServices`、`libc++.1.dylib` | 用于钥匙串鉴权、语音采集/播放、本地证书与系统接口 |

---

## 2. 六层合体架构：`agy` 如何把后端引擎编进单一可执行文件

桌面版 `Antigravity.app` 采用 **Electron 主进程/渲染进程（TS/JS） + 外部独立子进程 `language_server`（Go）** 的进程隔离架构；而 CLI (`agy`) 则是**纯 Go 单体编译产物**，直接把 `language_server` 与 `cortex` 核心引擎静态链接在同一进程中。

从 [jetski-packages.json](file:///Users/mac/antigravity/cli/metadata/jetski-packages.json) 与 [cliAnalyzer.ts](file:///Users/mac/antigravity/cli/src/cliAnalyzer.ts) 按代码段统计结果如下：

| 架构层级 | Go 包数量 | 函数数量 | 机器码体积（`__text`） | 核心职责 |
| :--- | ---: | ---: | ---: | :--- |
| `cortex-engine` | 190 | 11,368 | `5.32 MB` | 核心 Agent 执行循环、Tools、Customizations（Skills/Hooks/Plugins）、PolicyGuardian、SandboxProxy、Native Swarm |
| `cli-frontend` | 64 | 8,000 | `4.49 MB` | TUI 终端模型与渲染（`cli/model`、`cli/render`）、Swarm TUI、ACP stdio 服务器、本地状态库、认证链与 CLI 入口 |
| `protobuf-rpc` | 65 | 33,032 | `4.46 MB` | `LanguageServerService`、`ApiServerService`、`SeatManagementService`、`ExtensionServerService`、`HostBridgeService`、`RemotingService` 等 19 个服务的 Protobuf/gRPC/Connect 定义 |
| `language-server` | 74 | 6,504 | `3.13 MB` | `codeassistclient`、`cloudproxy`、`remotecontrol`、会话检索、语音服务与鉴权客户端 |
| `other` | 81 | 12,852 | `3.06 MB` | 公共基础库、Prompt 模板系统（`prompt/template_provider`）等辅助模块 |
| `telemetry-devtools` | 5 | 199 | `70.2 KB` | `devtools/jetski/telemetry` 脱敏与上报客户端 |

注意一个关键符号细节：`third_party/jetski/cli/backend/language_server.go` 中仅包含一个函数 `backend.unsupported`（`208 B`），而同目录下的 `server.go` 定义了 `NewServerBackend`（`4,512 B`）与 `ServerBackend` 方法族——说明 `agy` 通过 `ServerBackend` **进程内直连** `language_server` / `cortex`，而非像桌面端那样通过 HTTPS/CSRF 子进程通信。

---

## 3. CLI 认证与配置链（`cli/backend/auth` 与 `cli/acp/acpauth`）

从 `gopclntab` 还原的 `third_party/jetski/cli/backend/auth/`（11 个文件，91 个函数）揭示了 CLI 的 `ChainedAuth` 多级认证链：

1. **环境变量直连（`gemini_api_key.go`）**：`(*geminiAPIKeyAuth)` 支持无头环境直接使用 `GEMINI_API_KEY`；
2. **Gateway / 企业代理（`gateway.go`）**：`(*gatewayAuth)` 与 `acpauth` 中的 `ResolveAdminGatewayDisposition`、`loadMDMAdminSettings` 支持企业网关及 Vertex 配置（`ResolveVertexConfig`）；
3. **Google ADC（`adc_auth.go`）**：`(*adcAuth)` 支持本地 Application Default Credentials 探测与用户信息拉取（`fetchAdcUserInfo`）；
4. **系统钥匙串缓存（`keyring.go` / `token_storage.go`）**：`(*keyringAuth)`、`defaultTokenStorage` 与 `refreshAndSaveStoredToken` 管理持久化 OAuth Token；
5. **浏览器 OAuth 回调与 WIF（`browser.go` / `wif_browser.go`）**：`(*oauthMethod)` 启动本地回环监听并拉起浏览器完成 OAuth 授权（`OpenBrowser`、`completeOAuth`、`savingTokenSource`）；在内部代码中保留的 `corplogin_external.go`（`externalCorpLoginAuth`）均为 `16 B` 外部构建占位空实现；
6. **ACP 终端登录与授权（`cli/acp/acpauth/`）**：当作为 IDE/编辑器的 ACP（Agent Client Protocol）stdio 后端启动（`agy acp`）时，通过 `oauth_loopback.go` 与 `login.go` 处理交互认证。

配置文件路径（见 [extracted/assets](file:///Users/mac/antigravity/cli/extracted/assets) 与 `strings` 分析）：
- CLI 专属设置与缓存：`~/.gemini/antigravity-cli/settings.json`、`~/.gemini/antigravity-cli/hooks.json`、`~/.gemini/antigravity-cli/cache/projects.json`；
- 与桌面端共用的扩展生态：`~/.gemini/antigravity/` 下的插件、Skills、Rules 与 MCP 配置（对应 `cli/plugins/plugins`、`cli/mcp/mcp`）。

> [!NOTE]
> CLI 与桌面端在 OAuth 客户端、上游 `CodeAssistClient` 请求以及本地进程通信/认证链上的完整逐项对比，请参阅 [auth-and-requests.md](file:///Users/mac/antigravity/cli/docs/auth-and-requests.md)。

---

## 4. 四种运行模式

从入口包 `third_party/jetski/cli/entrypoints/` 与 `cli/acp/`、`cli/printmode/`、`cli/remotecontrol/` 可见 `agy` 支持四类主要运行形态：

1. **交互式 TUI 模式（默认 `agy`）**：由 `cli/model`（130 个 Go 文件、`1.90 MB` 机器码）、`cli/render`（38 个文件）、`cli/editing` / `cli/editing/vim`、`cli/swarm/tui` 驱动终端全屏交互界面、斜杠命令与多代理监控看板。
2. **脚本 / 无头打印模式（`agy -p` / `--print`）**：由 `cli/printmode` 实现，支持 `--input-format text|stream-json` 与 `--output-format text|json|stream-json`、结构化 `--json-schema` 校验及 `--print-timeout`。
3. **Agent Client Protocol（`agy acp`）**：通过 `--stdio` JSON-RPC 暴露标准 Agent 服务，支持 `--process-per-session`（按会话隔离 `--worker` 子进程）及 `--ide-mcp-port` 桥接宿主 IDE 工具。
4. **远程控制与音频桥接（`agy remote-control` / `agy mic-serve`）**：
   - `remotecontrol/daemon_darwin.go` 在 macOS 注册后台守护进程，配合 `exa.remoting.RemotingService` 与 WebRTC/Signaling 实现远程会话接入；
   - `audio/miniaudio` + `audio/micserve` 在本地环回地址（默认 `127.0.0.1:4713`）向远端 CLI 转发麦克风音频流。

---

## 5. 为什么 `agy` 也内嵌了 `Jetski Web` 与语言服务压缩包？

因为 `agy` 静态编译进了 `language_server` 与 `cortex` 代码包，所以二进制中同时带入了它们的 Go `//go:embed` 静态资源（已全部无损解包到 [extracted](file:///Users/mac/antigravity/cli/extracted)）：

- **`web-bundle`（540 个文件，解压 `18.88 MB`）**：[index.html](file:///Users/mac/antigravity/cli/extracted/web-bundle/index.html) 标题为 `Jetski Web`，包含桌面/Web 界面所需的 `main.js`（`12.3 MB`）、浏览器 DOM 代理 `page_agent_bundle.js`、`page_facade_bundle.js`、`diff_worker.js`、`audio_processor.js` 等；
- **`prompts`（16 个 `.tmpl` 文件）**：`google3/third_party/jetski/prompt/template_provider/templates/` 下的系统提示词（`identity.tmpl`、`guidelines.tmpl`、`slash_commands.tmpl`、`skills.tmpl`、`artifacts.tmpl` 等）与步骤输出模板；
- **`assets`（42 个文件）**：内置 Skills（`antigravity_guide`、`agy-customizations`、`native_swarm`、`swarm_monitor`、`swarm_narrator` 等）以及完整的 **Swarm Board Sidecar**（[main.mjs](file:///Users/mac/antigravity/cli/extracted/assets/assets/shared/plugins/swarm/sidecars/swarm_board/main.mjs)、[channels.mjs](file:///Users/mac/antigravity/cli/extracted/assets/assets/shared/plugins/swarm/sidecars/swarm_board/channels.mjs)、[app.js](file:///Users/mac/antigravity/cli/extracted/assets/assets/shared/plugins/swarm/sidecars/swarm_board/app.js)）；
- **语言运行时压缩包**：`lib/_tsserver.js` + `.d.ts`（`12.37 MB`）与独立 `Python 3.13` 发行版（`55.39 MB`，用于部分插件/沙箱工具环境）。
