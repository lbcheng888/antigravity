# Antigravity

本工作区由 `/Applications/Antigravity.app`（`com.google.antigravity` `2.32.1`）中的 `Contents/Resources/app.asar` 与 `Contents/Resources/app.asar.unpacked` 完整解包，并将 `dist/` 下全部 `107` 个已编译 CommonJS JavaScript 模块还原为严格类型检查通过、可读可维护的 TypeScript 源码（`src/`）。原 `/Applications/Antigravity.app` 保持未修改状态。

> 解包校验：共提取 `1,068` 个文件（`721` 个来自 `app.asar`，`347` 个来自 `app.asar.unpacked`），全部通过 ASAR 头记录的 SHA-256 完整性校验；在此基础上完成全部 `107` 个模块的 TypeScript 源码还原、`tsc --noEmit` 类型检查（0 错误）与 `tsc` 编译输出及导出符号一致性校验。

---

## 目录结构

```text
/Users/mac/antigravity/
├── README.md                 # 本工程说明、架构概览与构建校验指南
├── tsconfig.json             # TypeScript 编译配置（strict: true, rootDir: src, outDir: build）
├── jsconfig.json             # JavaScript/dist 浏览辅助配置
├── package.json              # 应用清单及 typecheck / build / check 脚本
├── .gitignore                # Git 忽略规则（忽略 node_modules、build 等）
├── docs/
│   └── modules.md            # 全量 107 个模块按功能域分类的详细索引
├── scripts/
│   └── check-project.js      # 离线完整性、语法、模块覆盖与导出一致性校验脚本
├── icon.png                  # 应用图标
├── trayTemplate.png          # macOS 状态栏模板图标
├── trayTemplate@2x.png       # macOS Retina 状态栏模板图标
├── src/                      # 还原后的 TypeScript 源码（107 个模块 + types/vitest.d.ts）
│   ├── main.ts               # Electron 主进程入口
│   ├── preload.ts            # Hub 窗口 contextBridge 预加载脚本
│   ├── types/
│   │   └── vitest.d.ts       # 测试/Mock 模块使用的 Vitest 环境类型声明
│   ├── browserEngine/        # 独立 Chromium CDP Pipe 浏览器引擎
│   ├── ideInstall/           # IDE 下载与安装向导
│   ├── proto/                # HostBridgeService protobuf 描述
│   ├── remote/               # SSH 远程连接、SOCKS 传输与远端自举
│   ├── services/             # 设置持久化服务
│   ├── test/                 # Vitest 测试辅助
│   └── __mocks__/            # Vitest Mock（electron / electron-updater / electron-log）
├── build/                    # `npm run build` (`tsc`) 从 src/ 编译输出的 JS、.d.ts 与 .js.map
├── dist/                     # 从 app.asar 解包的原始 CommonJS 参考基线（保留未改）
└── node_modules/             # 解包合并的运行时依赖与 TypeScript / @types/node / Electron 44 类型声明
```

---

## 常用命令（开发、类型检查与编译）

```bash
# 1. 严格 TypeScript 类型检查（tsc --noEmit）
npm run typecheck

# 2. 编译 src/**/*.ts -> build/（生成 .js / .d.ts / .js.map，不覆盖参考基线 dist/）
npm run build

# 3. 离线全量结构校验（检查 dist/、src/ 覆盖率、build/ 语法、相对 require 与导出符号一致性）
npm run check
```

---

## 整体架构概览

1. **主进程启动与窗口管理**
   - 入口为 [`src/main.ts`](src/main.ts)（对应 [`dist/main.js`](dist/main.js)）。
   - 启动时处理单实例锁、命令行开关（如 `ELECTRON_OZONE_PLATFORM_HINT=headless`、`DEV_URL`、`--wsl-distro`、`--ssh-host=`）、自定义协议（`plugin://` 与 `agy-favicon://`，见 [`src/customScheme.ts`](src/customScheme.ts)）以及 `antigravity://` 深链。
   - 初始化 [`StorageManager`](src/storage.ts)、[`SettingsService`](src/services/settingsService.ts)、[`RemoteManager`](src/remote/remoteManager.ts)、[`registerIpcHandlers`](src/ipcHandlers.ts) 与 [`registerWcvIpcHandlers`](src/wcvIpcHandlers.ts)。
   - Hub 渲染进程通过 [`src/preload.ts`](src/preload.ts) 暴露的 `contextBridge` API 与主进程通信。

2. **Language Server 与 HostBridge**
   - [`src/languageServer.ts`](src/languageServer.ts) 定位并拉起外部原生二进制 `language_server`（打包态位于 `process.resourcesPath/bin/language_server`，未打包态可通过 `CODEIUM_LANGUAGE_SERVER_BIN` 指定或回退到 `../bin/language_server`），同时通过环境变量 `CHROME_DEVTOOLS_MCP_JS` 指向 `node_modules/chrome-devtools-mcp/build/src/bin/chrome-devtools-mcp.js`。
   - [`src/hostBridgeServer.ts`](src/hostBridgeServer.ts) 在本地回环 `127.0.0.1` 启动 Connect HTTP 服务，基于 [`src/proto/host_bridge_pb.ts`](src/proto/host_bridge_pb.ts) 定义的 `HostBridgeService`（`GetUpdateStatus`、`ApplyUpdate`）供 Language Server 查询和触发宿主应用更新。

3. **内置浏览器、WebContentsView 与安全策略**
   - `src/wcv*.ts`、`src/webview*.ts` 与 `src/guest*.ts` 负责多标签 `WebContentsView` / `<webview>` 生命周期、CDP 调试连接、下载与文件上传校验、对话框/提示窗拦截、网络事件脱敏以及跨站凭据隔离。
   - [`src/scriptJail.ts`](src/scriptJail.ts) 提供临时、无外网出口的脚本隔离执行环境；[`src/browserEngine/`](src/browserEngine/browserEngine.ts) 通过 CDP Pipe 对接外部 Chrome。
   - [`src/cablev2.ts`](src/cablev2.ts)、[`src/webauthn.ts`](src/webauthn.ts)、[`src/qrEncoder.ts`](src/qrEncoder.ts) 与 [`src/deviceChooser.ts`](src/deviceChooser.ts) 实现 WebAuthn / caBLEv2 认证与设备选择器。

4. **远程 SSH 与 WSL**
   - [`src/remote/`](src/remote/remoteManager.ts) 使用本机 `ssh` 与 `SSH_ASKPASS` 中继在远端主机部署/连接持久化的 Language Server，并通过 SOCKS 代理与侧车路由接入 Electron 窗口。
   - [`src/wsl.ts`](src/wsl.ts) 与 [`src/provisionSplash.ts`](src/provisionSplash.ts) 负责 Windows WSL 环境下的服务端部署与引导。

详细模块列表见 [`docs/modules.md`](docs/modules.md)（其所列模块路径均同时对应 `src/` 下的同名 `.ts` 文件与 `dist/` 下的同名 `.js` 文件）；关于账号 OAuth 授权、进程间鉴权与网络请求代理的详细分析见 [`docs/auth-and-requests.md`](docs/auth-and-requests.md)。

---

## 关于运行的边界说明

- **编译产物输出在 `build/`**：[`tsconfig.json`](tsconfig.json) 将编译输出设为 `build/`（同时保留 [`package.json`](package.json) 中 `"main": "dist/main.js"` 指向原始解包产物），以便保留原始 `dist/` 作为参考基线不被覆盖。
- **实际运行需 Electron 与外部原生二进制**：
  - 本目录内只包含 Electron `44` 的类型声明（`node_modules/electron/electron.d.ts`），未下载 Electron 桌面二进制，因此纯 `node` 无法直接运行含 `electron` 模块的主进程代码；
  - `language_server`、`webm_encoder`、`cable_ble_scanner` 等原生可执行文件位于原 `/Applications/Antigravity.app/Contents/Resources/bin/`；若使用本机 Electron 二进制试运行编译后的 `build/main.js`，需传入对应的原生二进制路径（如 `CODEIUM_LANGUAGE_SERVER_BIN` 等）。
