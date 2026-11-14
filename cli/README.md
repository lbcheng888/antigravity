# Antigravity CLI (`agy`) 离线解包与分析工程

本目录是将本机 `/Users/mac/.local/bin/agy`（v`1.5.9`，`223,424,816` 字节）中的全部**可解包内嵌资源**与 **Go 符号/包元数据**无损提取并整理成的离线分析工程。原二进制未被修改。

> [!NOTE]
> 与仓库根目录中从 [Antigravity.app](file:///Applications/Antigravity.app) 解包还原的 Electron/TypeScript 桌面端工程不同，`/Users/mac/.local/bin/agy` 是一个由 Google 内部 Go 1.28 编译的 `Mach-O 64-bit executable arm64` 原生可执行文件（不含 `app.asar` / `NODE_SEA`），核心逻辑已被编译为 `__TEXT` 机器码。因此本工程对可提取的静态资源/前端 JS/运行时压缩包进行全量无损解包，并从 `__lrodata_gopcln` 恢复完整的 Go 包树、函数地址、RPC 服务与 CLI 结构，同时提供严格类型的 TypeScript 元数据查询工具。

---

## 目录结构

| 路径 | 说明 |
| :--- | :--- |
| [bin/agy-info.json](file:///Users/mac/antigravity/cli/bin/agy-info.json) | 源二进制 SHA-256、Mach-O 格式、Go 构建版本与动态链接库快照 |
| [docs/architecture.md](file:///Users/mac/antigravity/cli/docs/architecture.md) | `agy` 单体六层架构、与桌面端的同源差异及认证/运行模式分析 |
| [docs/cli-reference.md](file:///Users/mac/antigravity/cli/docs/cli-reference.md) | 实测抓取的 `agy` 主参数与所有子命令参数参考 |
| [docs/package-map.md](file:///Users/mac/antigravity/cli/docs/package-map.md) | Go 模块分类统计、19 个 Protobuf/ConnectRPC 服务与提取清单 |
| [metadata/](file:///Users/mac/antigravity/cli/metadata) | 还原出的 `go-files.txt`、`jetski-files.txt`、`cli-files.txt`、`jetski-packages.json`、`jetski-functions.jsonl`、`proto-services.json`、`cli-commands.json`、`embedded-manifest.json` |
| [extracted/](file:///Users/mac/antigravity/cli/extracted) | 从二进制无损解包出的 `prompts/`、`assets/`、`metaschemas/`、`chroma-*`、`web-bundle/`、`tsserver/`、`python/`、`tzdata/`、`fonts/`、`proto-descriptors/` |
| [src/](file:///Users/mac/antigravity/cli/src) | 严格类型 TypeScript 分析工具（[types.ts](file:///Users/mac/antigravity/cli/src/types.ts)、[metadataLoader.ts](file:///Users/mac/antigravity/cli/src/metadataLoader.ts)、[cliAnalyzer.ts](file:///Users/mac/antigravity/cli/src/cliAnalyzer.ts)） |
| [scripts/](file:///Users/mac/antigravity/cli/scripts) | 可复现解包脚本 [extract-agy.py](file:///Users/mac/antigravity/cli/scripts/extract-agy.py) 与一致性校验脚本 [check-cli.js](file:///Users/mac/antigravity/cli/scripts/check-cli.js) |

---

## 常用离线分析命令

所有命令均在 [cli/](file:///Users/mac/antigravity/cli) 目录下执行（或在根目录加 `--prefix cli`）：

```sh
# 1. 类型检查分析工具 TS 代码
npm --prefix cli run typecheck

# 2. 编译并生成全局二进制与架构摘要报告
npm --prefix cli run report

# 3. 按包名 / 文件名 / 函数名检索 Go 符号表（支持子串）
node cli/build/cliAnalyzer.js "cli/backend/auth"
node cli/build/cliAnalyzer.js "acp/server.go"

# 4. 校验所有提取目录文件数、SHA-256 与元数据一致性
npm --prefix cli run check

# 5. 如需从原二进制重新完整解包（先清理 extracted/ 后重跑）
npm --prefix cli run extract
```
