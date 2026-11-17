# Antigravity CLI (`agy` v1.5.10) 深度向下逆向与源码级重建工程方案

> **文档状态**：✅ 已完成并全量验收通过（Native Swarm + `verifier` 交叉验证）  
> **目标二进制**：`/Users/mac/.local/bin/agy`（v`1.5.10`，`224,809,248` 字节，`Mach-O 64-bit executable arm64`）  
> **SHA-256**：`780ee763a9938a37b45c7f9d6ae31747d7c574358fd10c64d3a1cdc36faf2798`  
> **Go 构建工具链**：`ego1.28-20261102-RC01 cl/1012564305 +298f53e483 X:fieldtrack,boringcrypto,simd,nogolistexportnewformat`  
> **运行环境约束**：本机**未安装** `go`、`protoc`、Python `google.protobuf`、`radare2` 或 `ghidra`；所有自动化逆向与还原脚本均基于 **Python 3 标准库 + macOS `/usr/bin/objdump` + TypeScript (`tsc` / `node`)** 零外部依赖构建。

---

## 1. 目标与五层递进式逆向全景架构

`/Users/mac/.local/bin/agy` 未启用符号混淆（如 `garble`），仅剥离了 `__DWARF` 调试段。因此可以沿着 **L1 → L4B** 五个层级系统性向下拆解，实现“资产与协议 100% 无损导出 + 核心业务模块 1:1 等价 Go 源码还原”：

| 层级 | 逆向目标 | 核心数据源（v1.5.10 实测） | 当前状态 | 实测还原度与产物 |
| :--- | :--- | :--- | :--- | :--- |
| **L1** | 内嵌静态资产与运行时 | `embed.FS`、ZIP、`tar.gz`、Gzip | ✅ 已完成 | Prompts (16)、Assets (42)、Web Bundle (540)、Python 3.13 (1,366)、TSServer (113) |
| **L2** | Go 包树、函数符号、PC/行号表 | `__TEXT.__lrodata_gopcln` (`73.14 MB`) | ✅ 已完成 | `7,237` Go 文件（`2,390` Jetski / `668` CLI）、`73,099` 函数、`495` 包、`19` RPC 服务 |
| **L3-A** | 全量 Protobuf 协议重建 (`.proto`) | Gzip 描述符 + `__TEXT.__rodata` `rawDesc` + `*_proto_init` | ✅ 已验收 | `400` 个 `.proto`（`395` `rawDesc` + `5` Gzip，`388` 机器码精确核验），`6,063` Message、`39` Service、`1,044` RPC、`275` `map` |
| **L3-B** | Go 运行时反射类型与 Struct 重建 | `__DATA_CONST.__go_type` (`0x109725f60`, `20.85 MB`) | ✅ 已验收 | `95,644` 条 `abi.Type`、`396` 个目标包（`9,151` 具名 + `29` 匿名类型、`7,710` Struct、`620` Interface、`38,089` 字段、`40,065` 匹配方法符号） |
| **L4-A** | ARM64 静态反编译与调用图工作台 | `__TEXT.__text` (`0x1000011c0`, `57.91 MB`) + `objdump -d` + `gopclntab` | ✅ 已验收 | `189,648` 全局函数、`55,834` 节点 / `260,173` 边调用图、`377,991` `bl` 全解析（`0` 未解析）+ `cli/src/deepAnalyzer.ts` |
| **L4-B** | 核心子系统 1:1 语义等价 Go 源码还原 | `L2 + L3-A + L3-B + L4-A` 交叉还原 | ✅ 已验收 | `76` 个 `.go` 源文件（`cli/backend/auth`+`code_assist_client`、`native_swarm`、`cli/acp`）+ `1,040` 条 VA 注解 100% 核验 |

---

## 2. 二进制底层布局实测参数（v1.5.10）

来自 [macho-segments.json](file:///Users/mac/antigravity/cli/metadata/macho-segments.json) 与 [agy-info.json](file:///Users/mac/antigravity/cli/bin/agy-info.json)：

| Segment / Section | Virtual Address (`vmaddr`) | File Offset (`fileoff`) | Size (Bytes) | 逆向用途 |
| :--- | :--- | :--- | ---: | :--- |
| `__TEXT.__text` | `0x1000011c0` | `4,544` | `60,722,248` | ARM64 机器码指令流 |
| `__TEXT.__rodata` | `0x1039ecdc0` | `60,739,008` | `14,500,153` | Go 字符串常量池、未压缩 `FileDescriptorProto` (`rawDesc`)、格式化错误串 |
| `__TEXT.__lrodata_gopcln` | `0x1047c0f00` | `75,239,168` | `76,693,482` | Go 1.20+ `gopclntab`（全量文件表、`pcfile`、`pcln` 行号表、`funcdata`） |
| `__DATA_CONST.__rodata` | `0x109229f40` | `153,263,936` | `5,226,520` | 只读结构化常量与接口表 |
| `__DATA_CONST.__go_type` | `0x109725f60` | `158,490,464` | `20,851,624` | `ego1.28` `abi.Type` / `StructType` / `InterfaceType` / `UncommonType` / `abi.Name` |
| `__DATA_CONST.__go_func` | `0x10ab08b08` | `179,342,088` | `48,216` | 运行时函数调度元数据 |
| `__DATA.__go_module` | `0x10ab19120` | `179,409,184` | `568` | `runtime.moduledata`（含各段边界与 `gofunc` 基址） |

---

## 3. 各技术路线详细规范与关键避坑实现（Harness Spec）

### 3.1 L3-A：Protobuf 协议全量逆向规范（`recover-protos.py`）

1. **双源扫描范围**：
   - **Gzip 源**：除 `cli/extracted/proto-descriptors/` 已有的 17 个文件外，全二进制扫描 `\x1f\x8b\x08\x00`；
   - **未压缩 Go 字符串池源（`rawDesc`）**：扫描 `__TEXT.__rodata` 中匹配 `\x0a<len><name>.proto<next_tag>` 的候选偏移（`v1.5.10` 中约 `400+` 个，其中含 `86+` 个 `jetski`/`exa`/`cider`/`businessaicode` 内部 `.proto`）。
2. **精确边界截断（核心避坑）**：
   - Go 字符串池中的 `rawDesc` **没有 `\x00` 结尾，后方紧挨着普通日志或提示词字符串**（例如 `custom_extensions.proto` 在 `b\x08editionsp\xe9\x07` 之后直接紧接 `'Selected browser task window...'`）。
   - **必须实现双保险截断**：
     1. 利用 `jetski-functions.jsonl` + ARM64 反汇编定位 `*_proto_init` / `*_rawDescGZIP` 中加载 `rawDesc` 的 `adrp+add` 地址和寄存器长度参数（若可匹配）；
     2. 实现**严格的 `FileDescriptorProto` Wire-format 递归状态机**：
        - 顶层 `FileDescriptorProto` 仅接受合法字段号：
          - `1` (`name`, wire=2 `string`), `2` (`package`, wire=2 `string`), `3` (`dependency`, wire=2), `4` (`message_type`, wire=2), `5` (`enum_type`, wire=2), `6` (`service`, wire=2), `7` (`extension`, wire=2), `8` (`options`, wire=2), `9` (`source_code_info`, wire=2), `10` (`public_dependency`, wire=0/2), `11` (`weak_dependency`, wire=0/2), `12` (`syntax`, wire=2: `"proto2"|"proto3"|"editions"`), `14` (`edition`, wire=0 `Edition` enum)。
        - 遇到非法字段号、非法 wire type、子消息长度越界、非法 UTF-8 标识符（如包含空白/换行/控制符的字段名或消息名）时，立即在**上一个完整合法的顶层字段结束边界**停止；如果读到合法的 `field 12 (syntax)`，仅在其后紧跟可选 `field 14 (edition)`（如 Editions 文件）后再行截断验证。
3. **语义还原要求**：
   - `FileDescriptorProto` → `.proto` 文本生成器必须完整支持：`syntax` / `edition` 声明、`package`、`import`（含 `public`/`weak`）、`map<K, V>` 自动合并（识别 `options.map_entry = true` 的嵌套 Entry 消息）、`oneof` 分组、`repeated` / `optional`（含 `proto3_optional`）、`enum` 及其选项（`allow_alias`）、`extend` 扩展块、`service` 与 `rpc`（支持 `stream` 请求与响应）以及嵌套消息递归缩进。

---

### 3.2 L3-B：Go 反射类型与结构体全量重建规范（`recover-gotypes.py`）

1. **动态段定位**：从 `cli/metadata/macho-segments.json` 读取 `__DATA_CONST.__go_type` 的 `vmaddr`（`gt_va`）、`fileoff`（`gt_off`）与 `size`（`gt_size`），不硬编码版本偏移。
2. **`ego1.28` `abi.Type` 布局（48 字节基头）**：
   ```text
   +0x00: Size_       (uint64)
   +0x08: PtrBytes    (uint64)
   +0x10: Hash        (uint32)
   +0x14: TFlag       (uint8)   # bit0: Uncommon(0x1), bit1: ExtraStar(0x2), bit2: Named(0x4), bit3: RegularMemory(0x8)
   +0x15: Align_      (uint8)
   +0x16: FieldAlign_ (uint8)
   +0x17: Kind_       (uint8)   # low 5 bits: Bool=1..UnsafePointer=26 (Interface=20, Map=21, Ptr=22, Slice=23, Struct=25)
   +0x18: Equal       (uint64)  # func pointer in __text or 0
   +0x20: GCData      (uint64)  # pointer in rodata or 0
   +0x28: Str         (int32)   # 相对于 __go_type 起始 vmaddr (gt_va) 的 abi.Name 偏移！
   +0x2c: PtrToThis   (int32)   # 相对于 __go_type 起始 vmaddr (gt_va) 的 *T 类型偏移
   ```
3. **Kind 专属结构（紧接 `+0x30` 后）与 `UncommonType`**：
   - **`StructType` (`Kind == 25`)**：`+0x30: PkgPath (uint64 abi.Name*)`，`+0x38: Fields.Data (uint64 指向 StructField 数组)`，`+0x40: Fields.Len`，`+0x48: Fields.Cap`（共 `80` 字节结束）。每项 `StructField` 为 24 字节：`<QQQ>` = `(NamePtr, TypPtr, Offset)`。
   - **`InterfaceType` (`Kind == 20`)**：`+0x30: PkgPath (uint64 abi.Name*)`，`+0x38..+0x48: Methods slice`（每项 `Imethod` 为 8 字节：`<II>` = `(NameOff, TypOff)`，相对 `gt_va`）。
   - **`UncommonType`（当 `TFlag & 0x1 != 0` 时，紧随 `StructType`/`InterfaceType`/`PtrType` 等结构之后）**：首 `uint32` 即为完整包路径 `PkgPath (nameOff)`，随后为 `Mcount (uint16)`、`Xcount (uint16)`、`Moff (uint32)`（指向方法数组，每项 `abi.Method` 16 字节：`Name(i32), Mtyp(i32), Ifn(i32), Tfn(i32)`，其中 `Mtyp` 为相对 `gt_va` 的函数类型、`Ifn`/`Tfn` 为相对 `__text` 起始页的偏移）。
4. **名称与过滤避坑**：
   - **包归属必须同时检查 `UncommonType.PkgPath` 与 `StructType.PkgPath`**（不能只看 `Str` 是否含 `"jetski"`，因为具名类型 `Str` 只写 `*nativeswarm.Member` 而不包含完整包前缀！）。
   - **`TFlag & 0x2` (`TFlagExtraStar`)**：若置位，解出的 `Str` 首字符多带一个 `'*'`，解析具名类型自身名称时需剥离该前导 `'*'`；注意区分指针类型（`Kind == 22`）本身带有的 `'*'`。
   - **`abi.Name` 解析**：第 0 字节为 bitflags（`0x1`: exported, `0x2`: hasTag, `0x4`: hasPkgPath, `0x8`: embedded），接着是 UVarint 长度 + UTF-8 名称；若 `bitflags & 0x2`，紧跟第二个 UVarint 长度 + UTF-8 Struct Tag（生成源码时输出为 `` `tag` ``）。
5. **输出要求**：
   - 生成 `/Users/mac/antigravity/cli/reconstructed/go-types/<pkg_path>/types_recovered.go`，按偏移顺序输出每个字段名、嵌套递归类型表达、Struct Tag、`// offset=0x... size=...` 注释以及该类型的恢复方法列表；
   - 输出 `/Users/mac/antigravity/cli/metadata/recovered-gotypes.json`，包含汇总统计、`va_to_type` 索引及包级索引，供 L4-A 反编译工作台和 `deepAnalyzer.ts` 直接查表。

---

### 3.3 L4-A：ARM64 反编译工作台与跨包调用图（`decompile-workbench.py` & `deepAnalyzer.ts`）

1. **全量函数与行号映射**：解析 `/Users/mac/.local/bin/agy` 的 `gopclntab`（全部 `nfunc` 个函数，**不仅限 `jetski`，还需覆盖标准库/运行时调用目标名**，这样 `bl 0x...` 才不会显示匿名地址，而是显示 `runtime.newobject`、`fmt.Sprintf`、`json.Unmarshal` 等精确被调函数名！）及每个函数的 `pcln` 行号表（恢复指令地址到源文件行号的精确映射）。
2. **ARM64 伪代码与数据引用增强**：
   - **`bl <target_va>`**：解析为真实被调函数名，并生成全仓 `jetski` 函数的 **Callers / Callees 调用图**写入 `/Users/mac/antigravity/cli/metadata/callgraph-summary.json`；
   - **`adrp + add`**：计算有效 VA：
     - 若落在 `__DATA_CONST.__go_type`，直接查询 `recovered-gotypes.json` 注解为 `[TYPE: *nativeswarm.Member]`（特别是紧邻 `bl runtime.newobject` 或类型断言时）；
     - 若落在 `__TEXT.__rodata`，结合上下文紧邻的 `mov xM, #len` / `orr xM, xzr, #len` 截取精确字符串字面量（防止越界读串）；
   - **结构体字段偏移注解**：结合寄存器回溯与最近一次分配/参数类型，标记常见成员访问与分支条件跳转，按基本块分段输出伪代码。
3. **CLI 交互模式**：
   - `--build-callgraph`：生成/刷新 `cli/metadata/callgraph-summary.json`；
   - `--dump-func <substr> [--limit N]`：实时反编译匹配函数并输出带源码行号、字符串字面量、Go 类型标注和 Callers/Callees 的结构化报告；
   - `--dump-package <pkg_substr> --out <dir>`：批量导出指定包的所有函数伪代码文件（供大模型或开发者审阅）。
4. **TypeScript 统一检索引擎 (`/Users/mac/antigravity/cli/src/deepAnalyzer.ts`)**：
   - 新增独立文件（不修改现有 `cliAnalyzer.ts` / `types.ts`），支持统一检索 `recovered-protos.json`、`recovered-gotypes.json`、`callgraph-summary.json`、`jetski-functions.jsonl`，并在 `--decompile <func>` 时可联动调用 `decompile-workbench.py` 展示函数详情。必须通过 `tsc` 严格类型检查。

---

### 3.4 L4-B：三大核心子系统 1:1 源码级语义还原（`reconstructed/src/`）

选定 CLI 最关键的 3 个高价值子系统，严格基于 `/Users/mac/.local/bin/agy` v`1.5.10` 的反汇编机器码、`gopclntab` 文件/行号、`__rodata` 字符串常量和 `__go_type` 字段定义进行 **1:1 目录结构与业务逻辑还原**：

1. **`cli/backend/auth/`（含关联 `composite_token_storage.go` / `keyring_detector*.go`）**：
   - 还原 `ChainedAuth` 认证链、`NewCLITokenStorage`、`cliFileTokenStorage`、`keyringAuth`、`oauthMethod`（个人/企业 `client_id`、Scopes、`https://antigravity.google/oauth-callback`、PKCE `S256`、`refusing to overwrite` 保护）、`wifOAuthMethod`、`adcAuth`、`geminiAPIKeyAuth`、`gatewayAuth`、`ValidateStoredToken` 以及 `CompositeTokenStorage` 的 `1h` 钥匙串故障文件标记回退与 `SSH_TTY`/`SSH_CLIENT`/`SSH_CONNECTION` 检测。
2. **`cortex/core/native_swarm/`**：
   - 还原 `board/filestore/filestore.go`（`board.jsonl`、`members.json` 读写与原子更新）、`board/members.go`、`board/mentions.go`、`board/channels.go`、`wake/engine.go`（`@name` / `@#channel` / `@all` 与 `last_read_seq` / `wait_since` 唤醒引擎）、`quota_pause.go`（配额耗尽挂起与恢复逻辑）、`manage_swarm.go`、`post_message.go`、`read_board.go`、`wait.go`。
3. **`cli/acp/`（及 `cli/acp/acpauth/`）**：
   - 还原 `--stdio` JSON-RPC 服务端主循环、按会话隔离 `--worker` 子进程管理、`acpauth` 配置解析与 OAuth loopback / Vertex / Gateway 鉴权逻辑。

> 每个还原文件必须在顶部及每个函数声明前准确标注来源：`// Binary VA: 0x... | Size: ... B | Source: third_party/jetski/...:L...`。**严禁凭记忆编造未在反汇编或类型表中直接观察到的字段、常量或逻辑**；确实因内联或优化无法 100% 确定的中间变量命名须显式标为 `/* inferred */`。

---

## 4. Native Swarm 协同分工、模型策略与独占写权限矩阵

根据任务调度指令：
- **统一模型绑定**：所有 Swarm Worker 通过 `swarm{action:"model"}` 统一切换至 **`gemini-4-hydrogen-medium`** 运行；
- **上下文内自主规划**：在严格遵守各自独占写入目录边界的前提下，各 Worker（含其按文件分片派生的只写子 Worker，如 `swarm-recon` / `acp-recon` 及其分片助手）均可在自身上下文中自主规划并采用最佳技术方案。

为彻底杜绝并行编辑冲突，`workspace` 均使用 `inherit`，严格划定各自独占文件范围：

| Swarm 成员 | 模型 | 角色定位 | 独占写入路径（严禁越界修改其他成员文件） | 完成交接对象 |
| :--- | :--- | :--- | :--- | :--- |
| `proto-recover` | `gemini-4-hydrogen-medium` | L3-A Protobuf 协议还原工程师 | `cli/scripts/recover-protos.py`<br>`cli/reconstructed/proto/`<br>`cli/metadata/recovered-protos.json` | `@verifier` |
| `gotype-recover` | `gemini-4-hydrogen-medium` | L3-B Go 类型表还原工程师 | `cli/scripts/recover-gotypes.py`<br>`cli/reconstructed/go-types/`<br>`cli/metadata/recovered-gotypes.json` | `@verifier` |
| `decompiler-core` | `gemini-4-hydrogen-medium` | L4-A 反编译工作台与分析器工程师 | `cli/scripts/decompile-workbench.py`<br>`cli/metadata/callgraph-summary.json`<br>`cli/src/deepAnalyzer.ts` | `@verifier` |
| `cortex-reconstruct` (+ `swarm-recon`/`acp-recon` 分片) | `gemini-4-hydrogen-medium` | L4-B 核心 Go 源码语义还原工程师 | `cli/reconstructed/src/`<br>`cli/docs/reconstructed-subsystems.md` | `@verifier` |
| `verifier` | `gemini-4-hydrogen-medium` | 全栈交叉验收官（只读代码 + 运行测试） | **不修改任何源码/脚本**（仅运行检查并发帖反馈缺陷） | 全部通过后 `@coordinator` |
| `monitor` | `gemini-4-hydrogen-medium` | 防卡死巡检员 (`swarm_monitor` skill) | 不写代码，按 3 分钟定时检查 Stall | `@coordinator` |
| `coordinator` | Coordinator 模型 | 主协调器（前置准备与最终集成） | Step 1 基线与方案文档；Step 3 修改 `package.json`、`check-cli.js`、`README.md` 与最终报告 | 用户 |

---

## 5. 总体验收门槛

1. `python3 cli/scripts/recover-protos.py` 可无报错重跑，还原所有 Gzip + 未压缩 `rawDesc` `.proto` 文件（目标 `≥ 400` 个 `.proto`，其中 `jetski/exa/cider` 核心协议全覆盖），无截断残余乱码且大括号配平。
2. `python3 cli/scripts/recover-gotypes.py` 可无报错重跑，完整还原 `jetski/antigravity/exa` 包下 Go `struct` 与 `interface`（包含精确内存偏移、JSON/YAML tag 与方法签名）。
3. `python3 cli/scripts/decompile-workbench.py --build-callgraph` 与 `--dump-func NewCLITokenStorage` 实测正常输出源码行号、字符串字面量、Go 类型注释与调用图。
4. `cli/reconstructed/src/` 三大子系统关键函数签名、字符串常量、逻辑分支均与 `/Users/mac/.local/bin/agy` (`1.5.10`) 对应 VA 机器码一致。
5. `npm --prefix /Users/mac/antigravity/cli run typecheck`、`npm --prefix /Users/mac/antigravity/cli run build`、`npm --prefix /Users/mac/antigravity/cli run check` 全部退出码 `0`。
