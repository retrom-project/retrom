# 第三方运行时、DAT 与账户安全依赖管理

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期实施基线 |
| 版本 | 1.2 |
| 日期 | 2026-08-25 |
| 适用范围 | EmulatorJS、RPG Maker runtime、core artifact、兼容覆盖、预置 Arcade DAT 与密码 blocklist |

## 1. 结论

Git 只保存小型、可审查的来源清单、物化配方、大小、SHA-256、许可来源与解析统计，不保存 EmulatorJS 发布包、core、第三方或用户 ROM、BIOS、许可 payload、五份 Arcade DAT payload 或密码 blocklist payload。公开测试程序例外位于 `testdata/public-roms/`：`gba-smoke/` 产出两个内容身份不同的 mGBA 程序；`nes-smoke/` 产出分别供 FCEUmm/Nestopia 使用、内容身份不同且读取 P1/P2 的两个 iNES NROM 程序；`snes-smoke/` 产出供 SNES9x 使用的 32 KiB LoROM；`arcade-smoke/` 产出 MAME2003/Plus、FBNeo、FBA2012 CPS1/CPS2 的 Z80/68000 程序、生成图形/声音、测试依赖归档与五种小型 DAT。它们均由 Retrom 自有源码确定性生成、使用 MIT 许可，生成源与 bytes 同时提交并由 `data-check` 校验。测试 BIOS 与 CPS2 `spf2t` 父归档不含第三方 ROM/BIOS bytes，也不被目标驱动执行。其他真实 payload 在开发或镜像构建开始前按固定 URL/tag/commit 取得，写到被 Git 忽略的固定目录，并在使用前进行本地完整性校验。RPG Maker JS/Wasm 只从两个自有 fork（EasyRPG Player 和 mkxp-z-libretro-emscripten）的固定 release tag 下载预构建 asset，manifest 锁定 repo、tag、tag 对应的完整 commit、asset URL/文件名和 adapter ABI，但不保存或校验远端 expected SHA-256。同 tag 的同名重建 asset 被视为 ABI 兼容；下载后计算 observed size/SHA 用于本地缓存损坏检测和诊断，但它不参与 route identity 或远端准入。`prepare-deps`、`make dev` 和镜像构建不编译 RPG runtime，也不从 Pages、`latest`、浮动分支或本机其他 ignored 目录复制 bytes。固定 release asset 缺失、metadata 身份不符或 observed 本地完整性漂移时必须 fail closed。

应用进程启动期间禁止联网下载或自动升级依赖。依赖缺失、大小或 SHA-256 不符时，后端必须拒绝进入 ready 状态并输出 `make prepare-deps` 这一条可操作命令；不能回退到 CDN、最新版本或另一个 core。

## 2. 唯一事实源与目录

运行时机器事实源由按 SemVer 升序配置的 manifest 集合组成：[`4.2.3/manifest.json`](../data/dat/emulatorjs/4.2.3/manifest.json) 提供 33 个基础 artifact 与五份 DAT，[`4.3.0-pre/manifest.json`](../data/dat/emulatorjs/4.3.0-pre/manifest.json) 覆盖 DOSBox Pure、Genesis Plus GX Wide 与 Azahar；按“后声明为该 core 的新绑定候选”合并后共有 35 个 `selected_for_new_bindings=1` 且 `available_for_launch=1` 的 EmulatorJS artifact。账户密码拒绝列表由 [`password-blocklists/v1/manifest.json`](../data/auth/password-blocklists/v1/manifest.json) 独立锁定 SecLists tag、40 位 commit、10,000 行 payload、MIT 许可及各自 size/SHA-256。后出现的运行时 manifest 只选择它明确列出的 core 新 artifact；不能要求部分覆盖重复全部核心/DAT，也不能覆盖或改写旧 artifact。仓库自有的公开测试 ROM 只服务实际产品测试，不得反向覆盖依赖版本；自动化测试不读取操作者私有 ROM/BIOS。运行时 manifest 同时锁定：

- EmulatorJS release、tag、发布资产 URL/size/SHA-256；
- 允许进入镜像/由 Go 静态服务的 EmulatorJS 文件 allowlist、36 个跨版本 selected artifact 条目（合并为 35 个当前新绑定 artifact）以及 PPSSPP auxiliary asset 的路径、size/SHA-256；
- Player adapter ID、runtime base 与 loader 的精确 release 相对路径；前端机器 registry 以 `web/features/player/adapters/registry.json` 为单一可读索引；
- `mame2003` 的显式兼容覆盖；
- 每份 DAT 的 core source 证据、物化配方、最终 size/SHA-256 和解析统计。
- 两个 EmulatorJS 版本与所有 selected core 的许可文件路径、固定来源、size/SHA-256、二进制关联证据等级和 notice 顺序（两个 manifest 共 38 个 component）。

当前两个 EmulatorJS manifest 的唯一有效格式为 schema V7，CoreArtifact compatibility 的唯一有效格式为 schema V5；校验器拒绝其他 schema，不读取已退役字段。每个 `selected_core_artifacts` 项都显式给出 `runtime_core_id`、loader 实际请求的 `requested_artifact_basename`、canvas policy、默认 option、input mode、启动动作、`supported_content_kinds` 与 report；校验器不根据 core ID 推导这些值。产品只在用户点击“创建存档”时创建 SaveState，不进行定时或退出自动保存。只有 `yabause` 同时声明 `MULTI_DISC_M3U_V1` 与 `multi_disc={max_discs:8,max_total_bytes:1073741824,delivery:EAGER_EXTERNAL_FILES}`，没有该对象或 kind 不匹配即不支持。`dosbox_pure`、`mednafen_psx_hw`、`ppsspp`、`azahar` 的 loader basename 分别为 `dosbox_pure-thread-wasm.data`、`mednafen_psx_hw-thread-wasm.data`、`ppsspp-thread-wasm.data`、`azahar-thread-wasm.data`，并与实际 THREAD_WASM 路径逐字匹配。`auxiliary_files` 当前只登记 `data/cores/ppsspp-assets.zip`，它和 36 份跨版本 core report 都必须在各自 runtime allowlist 中。4.2.3 联机适配器还精确发布 release 内的 `data/emulator.css` 与 loader 所列八个 `data/src/*` 文件；带指定 SaveState 的全部 4.2.3 单机 Launch 也使用这些受 size/SHA 约束的可审计源文件捕获原生 state-load 成败，并保持 EmulatorJS 自带的实验性 netplay transport 关闭。带指定 DOSBox Pure SaveState 的 4.3.0-pre Launch 同样只使用该 release 中按 size/SHA 固定的 `data/emulator.css`、ES module loader 及其完整静态 import closure；没有指定 SaveState 的普通单机模式继续使用 minified loader 资产。

4.2.3 的普通 Player adapter 固定为 `ejs-4.2.3-v3`，4.3.0-pre 固定为 `ejs-4.3.0-pre-v2`；registry 仅为联机 manifest 显式保留 legacy `ejs-4.2.3-v2`，不保留其他无法由当前普通或联机 manifest 解释的 fallback。4.2.3 与 4.3.0-pre 的 adapter 都对两份版本中逐字节相同的官方 `extract7z.js`、`extractzip.js` 执行运行时专题锁定的 CSP 兼容转换：4.2.3 保留既有 Worker Blob 转换，4.3.0-pre 因 compression 变为 module-private 而精确转换同源下载响应，再由 EmulatorJS 构造 Worker Blob。转换发生在浏览器内且任一源形状漂移即阻断，不能修改 runtime allowlist 中的官方 bytes、size/SHA 或许可关联证据。新版本不得仅因进入 registry 就自动继承转换，必须先证明其锁定 Worker 仍为已接受形状。dependency bootstrap 以 `(core_id,route_key='DEFAULT',artifact_set_sha256)` 复用 CoreArtifact ID，并逐字段验证不可变的 `runtime_version/adapter_id/entry_path/size_bytes/sha256/manifest_sha256/requires_threads/save_payload_kind/save_max_bytes/provenance_json/compatibility_json`；相同 identity 但任一字段不同必须 fail closed，不能原地更新 payload 或运行语义。只有 `selected_for_new_bindings/available_for_launch/version/updated_at_ms/retired_at_ms` 是生命周期字段；逐字节等价的重复 bootstrap 不改变 version 或 `updated_at_ms`。VariantRevision 与 SaveState 精确绑定原 artifact ID；未发布且输入漂移的 validation 必须重新验证后才能发布。

`4.3.0-pre` 的 DOSBox Pure 状态兼容只允许作用于 manifest 已校验的精确 thread artifact：浏览器实例化时要求唯一 DOS marker 与恰好两个 4 MiB stack high-watermark 等长编码，再把这两个运行副本位置提升到 64 MiB；官方 core 文件、manifest size/SHA、runtime allowlist 与 CAS bytes 均不改写。兼容层同时绕过上游 `save_state_info` helper 对 stack pointer 的错误释放，并为指定 SaveState 使用延迟 native load。marker、计数、WASM validation 或 state 导出任一不符合时必须 fail closed；其他 core、其他 EmulatorJS 版本和不相关 WASM 不得继承该转换。升级 DOSBox Pure artifact 时必须重新取得源码/二进制证据并完成实际产品存档恢复 smoke，不能沿用旧 marker。

联机使用独立的 [`data/netplay/v2/manifest.json`](../data/netplay/v2/manifest.json) 与 schema 作为普通兼容性之上的 core-profile allowlist。唯一当前 manifest schema 为 v4，精确 profile 为 `fceumm-423-v1`、`fbneo-423-v1`、`snes9x-423-v1`、`nestopia-423-v1`、`mame2003-423-override-v1`、`mame2003-plus-423-v1`、`fbalpha2012-cps1-423-v1` 与 `fbalpha2012-cps2-423-v1`。每项锁定 EmulatorJS 4.2.3、core artifact SHA-256、`maxPlayers=2` 与适用 `platformIds`；FCEUmm/Nestopia 只标记 `nes`，SNES9x 只标记 `snes`，五个 Arcade profile 只标记 `arcade`。FCEUmm 的核心级 prediction 上限为 8，其余七项为 0 帧严格 lockstep。协议另锁定 `SINGLE_FILE`、`retrom-netplay-v2`、`ejs-4.2.3-v2`、`ejs-netplay-4.2.3-v1`、24 controls、120-frame checkpoint、8 帧 prediction 协议上限、120 rollback、600 history 与 1 MiB state 上限。协议闭集不包含 suspend 消息；窗口失焦只清空本地输入。任意发布游戏只要所属基础平台在 profile `platformIds`、当前 READY VariantRevision 使用该精确 artifact、内容类型受协议允许且依赖快照仍有效，即可选择此 profile；ROM 逻辑名、大小、hash 与来源 archive 不进入产品准入。校验器必须与 runtime manifest 的 core artifact、supported content kind 和前端 registry 双向一致，未知 EmulatorJS/adapter/profile 不得 fallback。`ACC-NP-014`–`022` 使用项目自有 NES/SNES/Arcade bytes 经过真实 Retrom 导入、Launch、内容端点和两个 Chrome Player，建立八个 profile 的房间平台/游戏枚举、rollback/严格 lockstep、后台恢复和断线重连基线；该证据只适用于精确 profile/artifact，不能改写为任意 ROM 的兼容承诺。

锁定 Nestopia core 的 `retro_serialize` 把 8 字节 libretro 输入跟踪块写在 Nestopia `NST\x1a` 根状态块之后，而 `retro_unserialize` 不会恢复该块；FDS 的内部 state 长度还会变化，所以它不能按固定 MEM 尾部定位。联机只对 `nestopia-423-v1` 解析根块长度，将紧随其后的精确 8 字节归零后形成传输与 checkpoint 摘要；authority 原生 load 前后、target 原生 load 后都必须在同一投影下逐字节一致，传输的 RASTATE 本身也携带归零后的 bytes，使服务端 raw full/core digest 校验保持成立。根块、根块外其他 padding、长度、容器形状或其他 profile 只要有一字节差异都以 `STATE_INVALID` fail closed。该投影不修改普通单机存档格式，也不允许忽略 ROM、FDS、BIOS 或模拟器机器状态。

仓库与本地缓存边界固定为：

```text
data/dat/emulatorjs/4.2.3/
  manifest.json                 # Git 跟踪
  SHA256SUMS                    # Git 跟踪
  fbneo/fbneo-arcade.dat        # prepare-deps 生成，Git 忽略
  mame2003/mame2003.xml         # prepare-deps 下载，Git 忽略
  mame2003_plus/mame2003-plus.xml # prepare-deps 下载，Git 忽略
  fbalpha2012_cps1/fbalpha2012-cps1.dat # prepare-deps 从锁定源码原生生成，Git 忽略
  fbalpha2012_cps2/fbalpha2012-cps2.dat # prepare-deps 从锁定源码原生生成，Git 忽略

data/runtime/emulatorjs/4.2.3/  # prepare-deps 解包，Git 忽略
  4.2.3.7z                      # 仅依赖缓存；不进入最终镜像
  data/
  overrides/
  licenses/<component>/...      # manifest 锁定的小型许可 payload
  THIRD_PARTY_NOTICES            # 从上列文件确定性生成

data/auth/password-blocklists/v1/
  manifest.json                  # Git 跟踪，账户安全依赖唯一事实源
  payload/10k-most-common.txt    # prepare-deps 下载，Git 忽略
  payload/LICENSE                # prepare-deps 下载，Git 忽略
```

不得在 Markdown、shell 默认值或 Dockerfile 中复制另一套 digest。脚本必须读取 manifest；升级时新增版本目录，不覆盖旧 manifest。默认配置为 `4.2.3,4.3.0-pre`，其中 `RETROM_ACTIVE_EMULATORJS_VERSION=4.2.3` 仍是基础/备份契约值；新验证对每个 core 选择配置顺序中最后一个声明它的 manifest，因此 `dosbox_pure`、`genesis_plus_gx_wide`、`azahar` 使用 4.3.0-pre，其余核心使用 4.2.3。

## 3. 命令契约

根 Makefile 必须实现：

| 命令 | 确切行为 |
| --- | --- |
| `make data-check` | 只校验已提交的小文件：EmulatorJS manifest schema V7、artifact compatibility V5、多盘 kind/limits、普通与联机 Player adapter registry 双向一致、netplay core-profile exact allowlist、公开 CPS fixture 布局绑定的 source commit/DAT hash/machine count、DAT/许可字段与 `SHA256SUMS`；RPG aggregate Release identity、七条 route/artifact、adapter registry 和文件 allowlist 双向一致；以及密码 blocklist manifest 的固定 tag/commit、URL、行数、size/SHA 和 MIT 许可。不得打开被忽略的生产 DAT；无 payload、无网络时也必须通过。 |
| `make prepare-deps` | 对 `RETROM_DEPENDENCY_VERSIONS` 中缺失/错误的 EmulatorJS runtime/core/DAT/许可、单一 `retrom-runtime` tag Release，以及账户密码 blocklist/许可执行固定来源下载、校验、必要的确定性转换、解包与原子发布，生成 notice；默认 EJS 版本为 `4.2.3,4.3.0-pre`，最后隐式执行 `deps-check`。已有正确缓存时不重复取得对应 payload。RPG runtime 只从 manifest 锁定的 aggregate release repo/tag/tag commit/asset URL 下载，不验证远端 expected SHA；下载后计算的 observed size/SHA 只用于同一本地缓存的损坏检测和诊断。本命令不调用 Docker、CMake、Meson、Emscripten 或任何 RPG runtime 编译器。任一必需 asset 缺失、URL/tag/commit/metadata 形状漂移、下载不完整或为符号链接都 fail closed，绝不从其他 ignored 路径复制或本地重建。 |
| `make deps-check` | 不联网，逐个校验 EJS/RPG 运行时 allowlist、当前 selected artifact、本机 observed digest、可选 DAT、override、许可输入、确定性 notice，以及密码 blocklist 的 size/SHA/10,000 行和许可；同时把公开 CPS fixture 的 ROM 布局逐项核对到已物化的生产 DAT。缺少、额外发布或不匹配均失败。 |
| `make release-input-digest` | 不联网、不写工作树，按本节算法校验并只向 stdout 输出 64 位小写 `releaseInputDigest`；镜像 target 调用同一 helper，不复制 shell 算法。 |
| `make dev` | 先依赖 `prepare-deps`，然后只启动宿主机 Go/Next.js 进程；依赖准备不改变“非 Docker”契约。 |

`make install-deps` 是项目级初始化聚合入口，还会安装 Go/Node/Web 工具与 Playwright 锁定的 Chrome for Testing；其中浏览器位于 `.cache/tools/`，不属于本专题的应用 runtime allowlist，也不进入任何发布镜像。`make prepare-deps` 的职责和发布输入因此保持不变。

下载规则：连接、首字节和总请求分别有界；最多跟随 3 次 HTTPS redirect；拒绝 scheme 降级；先写同文件系统的 `0600` 临时文件，再校验 size/SHA，最后原子 rename。失败不得覆盖已有正确文件，也不得留下会被 `deps-check` 误认的目标文件。脚本不使用 `latest`、分支名、浮动 CDN 目录或未校验镜像。`PINNED_RAW_FILE` 只能命中 manifest 中与 40 位 commit 对应的 `raw.githubusercontent.com` URL；`RELEASE_ENTRY` 只能从已校验 release archive 的 allowlist 路径复制，不能再联网取“相似”文本。

FBNeo 的快速物化配方不是 mock：它下载固定 commit 的公开上游 DAT 快照，执行 manifest 中带 `expected_count` 的两项精确字节替换，并验证最终 SHA-256 与从绑定 EmulatorJS/FBNeo 源码直接运行 `fbneo -dat` 的结果完全一致。任一替换计数不符必须失败；直接源码构建仍是升级审计的独立复核路径。

FB Alpha 2012 CPS-1/CPS-2 没有可直接复用的绑定 DAT。`make prepare-deps` 必须下载各自 manifest 锁定的源码 archive，校验 archive size/SHA-256、安全展开，在全新临时目录构建原生静态枚举器，并分别执行两次干净生成；两次 Logiqx XML 必须逐字节相同且命中 manifest 的最终 size、SHA-256、声明名/版本与完整解析统计。CPS-2 仅允许 manifest 明列的 `mmancp2u -> megaman` 核心集合外 parent 规范化；任何新增、缺失或不同的外部关系都必须失败。生成器不接受浮动 branch/tag，也不能以手写游戏列表或 FBNeo DAT 替代。

### 3.1 发布输入指纹

前后端镜像的可组合性不依赖 tag 或人工记忆。共用 helper 先用 `git ls-files --cached --others --exclude-standard -z` 取得全部 Git 跟踪及未忽略文件；已在工作树删除但尚未暂存的旧索引路径不属于本次构建输入，必须排除，使删除和重命名在暂存前后得到相同工作树语义。其余路径拒绝非 UTF-8/非规范相对形式，并将每项规范为 `{"path":"...","mode":"100644|100755|120000","sha256":"..."}`；symlink 的 SHA-256 对 Git link target bytes 计算，其他只接受 regular file。按 path UTF-8 bytes 升序后对 RFC 8785 canonical array 计算 `sourceTreeSha256`，被 Git 忽略的 DAT/runtime/ROM/BIOS/缓存不进入此值。

再对以下 RFC 8785 object 计算 lowercase hex SHA-256，得到 `releaseInputDigest`：

```json
{
  "schemaVersion": 1,
  "sourceTreeSha256": "<64-lowercase-hex>",
  "dependencyVersions": [
    {
      "version": "4.2.3",
      "manifestSha256": "<sha256-of-exact-manifest-bytes>",
      "playerAdapterId": "ejs-4.2.3-v3"
    },
    {
      "version": "4.3.0-pre",
      "manifestSha256": "<sha256-of-exact-manifest-bytes>",
      "playerAdapterId": "ejs-4.3.0-pre-v2"
    }
  ],
  "activeEmulatorjsVersion": "4.2.3",
  "rpgMakerRuntimeManifestSha256": "<sha256-of-data/dat/rpgmaker/v1/manifest.json-bytes>",
  "passwordBlocklistManifestSha256": "<sha256-of-exact-password-blocklist-manifest-bytes>",
  "netplayManifestSha256": "<sha256-of-exact-netplay-manifest-bytes>"
}
```

dependencyVersions 必须等于规范化后的 `RETROM_DEPENDENCY_VERSIONS`、按 SemVer 升序，active 必须属于其中；各 manifest SHA 与 `rpgMakerRuntimeManifestSha256` 都对对应完整原始 bytes 计算，adapter ID 从运行时 manifest 读取。RPG manifest 缺失、非法或与 registry/物化 artifact 不一致时不能计算 digest。两个镜像都设置 OCI label `io.retrom.release-input-sha256=<releaseInputDigest>`。每个镜像 target 在 Docker build 前后重算并比较，工作树、选定版本或任一依赖 manifest 中途改变必须返回非零，该产物不得被宣称可部署。`make build-images` 只计算一份预期 digest，两个子 target 仍各自复核当前值，最后用 image inspect 确认两个 label 与预期值完全相同。直接绕过 Makefile 的 Docker build 不是受支持的发布路径；部署时 label 缺失或不同的两个镜像必须被编排/人工门禁拒绝。

## 4. 服务启动与健康检查

本地运行时通过三个只读配置确定依赖集合：`RETROM_DEPENDENCY_ROOT` 是包含 `dat/emulatorjs/<version>/` 与 `runtime/emulatorjs/<version>/` 的绝对根；`RETROM_DEPENDENCY_VERSIONS` 是无空白、无重复、按 SemVer（含 prerelease）升序的逗号列表，默认 `4.2.3,4.3.0-pre`；`RETROM_ACTIVE_EMULATORJS_VERSION` 必须属于该列表，当前为 `4.2.3`。开发值由 `make dev` 显式传入仓库 `data/` 的绝对路径，镜像值指向只读依赖层。

对每个配置版本 `v`，manifest 固定解析为 `<root>/dat/emulatorjs/<v>/manifest.json`，内置 DAT 根为同目录，release 根为 `<root>/runtime/emulatorjs/<v>`。数据库 CoreArtifact 的 `runtime_version=v`，`entry_path` 相对该 release 根解析，静态 URL 固定为 `/runtime/emulatorjs/<runtime_version>/<entry_path>`；`compatibility_json` 保存 manifest 的 schema V5 投影。当前合并后的 35 个 manifest artifact 被设为 `selected_for_new_bindings=1,available_for_launch=1`；保留版本为 `selected_for_new_bindings=0`，只要受 SaveState 或 READY VariantRevision 引用就必须继续 `available_for_launch=1` 并可精确启动。普通新验证只选择当前 selected artifact，绝不能因旧文件仍在磁盘而自动选中它。

后端启动顺序固定为：

1. 规范化版本列表，并逐版本读取约定位置的 manifest，验证 schema、目录名、manifest 版本、Player adapter 描述和 runtime base/loader allowlist 一致；同时读取密码 blocklist manifest 与 payload，逐字节校验并构建密码拒绝集合；前后端 adapter 对齐已在构建期 `data-check` 完成，Go 进程不假装读取另一镜像的代码；
2. 逐版本校验 allowlist、selected core、override、DAT、许可输入与 notice，并校验 blocklist 许可 payload；
3. 校验部署的 core artifact，包括 `mame2003` override，并建立仅含已验证版本/路径的静态路由表；
4. 打开数据库并执行 migration；
5. 在一个短事务中插入或严格复用全部已配置版本的不可变 CoreArtifact 与版本化静态 BIOS Requirement；逐 core 将 `selected_for_new_bindings` 切到配置顺序中最后一个声明它的 manifest，保留仍受引用 artifact 的 `available_for_launch`，再按 `core_artifact_id + dat_sha256 + parser_version` 创建或复用实际存在的内置 DatVersion。部分 runtime overlay 可以不含 DAT/其他 core，不能因此删除基础版本 seed；
6. 建立 allowlist 静态路由，启动 HTTP 与 worker。`/health/live` 此时可返回 200；只要当前 selected Arcade CoreArtifact 还没有 `READY` 的 active DatVersion，`/health/ready` 就返回 `503 DEPENDENCY_INDEXING`，除 health 外的全部路由统一返回错误 envelope `503 SERVICE_NOT_READY`；
7. Worker 在事务外通过受限 streaming parser 读取 DAT，以数据模型规定的短事务批次写入“尚未发布”的索引行；成功后以一个短事务把 DatVersion 转为 `READY`、发布这些行的可见性。启动引导随后校验 manifest 为该 CoreArtifact 选定的精确 DatVersion，停用同 artifact 的其他 BUILTIN 版本、激活目标、物化 Requirement 并写 AuditEvent；选版变化同时递增 CoreArtifact version。所有当前 selected Arcade artifact 都有 manifest 指定的 `READY` active DatVersion 后，ready 才转为 200；非 Arcade 静态 BIOS seed 不依赖 DAT 解析。

步骤 1–5 属于同步启动校验，受 `RETROM_STARTUP_CHECK_TIMEOUT` 约束（默认 60 秒）；后台 DAT_PARSE 的 30 分钟 execution deadline 由通用 Job 契约单独约束，不能塞进这 60 秒，也不能持有长数据库事务。进程在解析期间保持 live，重启后按 lease/Job 规则恢复同一任务。当前 selected artifact 所需的内置解析若确定性失败，则 DatVersion/Job 为 `FAILED`，进程保持 live、ready 固定为 `503 DEPENDENCY_DAT_PARSE_FAILED`，其他路由继续由 readiness gate 阻断；相同 DAT/parser 的重启不得自动抹掉失败证据，修复必须发布新的 parser version（产生新 DatVersion/Job）或按通用规则对 retryable execution 显式重试。未选定版本的预解析失败可记录诊断但不阻断当前 ready。

任一已配置 manifest 无法解析、Player adapter 描述与其 allowlist 自相矛盾、payload 缺失/不匹配、core 与 DAT 绑定不成立，或 active 版本不在版本列表时，Go 进程在步骤 1–5 内非零退出，不先开放 HTTP。数据库引用了未配置版本时进程仍可 ready，但对应存档/历史 revision 的启动必须以 `LAUNCH_CORE_ARTIFACT_UNAVAILABLE` 阻断并指出所需版本；不得回退到当前 core。前端遇到部署错配造成的未知 adapter 按运行时专题安全阻断；发布验收必须在部署前阻止这种错配。发布升级镜像的验收要求把所有仍受保护引用的版本纳入列表，因此正常升级路径不会产生该 blocker。Hasheous 暂时不可用不影响进程 ready；它只影响单次刮削并走有界降级。

## 5. 镜像构建

根 Dockerfile 的 dependency builder stage 必须读取同一组 manifest，当前默认列表为 `4.2.3,4.3.0-pre`；该 stage 包含 FBA2012 锁定源码生成器及临时原生编译工具链，完整 release 只作为校验与选择输入。RPG runtime 不在该 stage 编译，只复制已下载并校验的 `retrom-runtime` aggregate Release assets。生成结束后必须从 manifest 确定性导出一个全新的只读目录，只把校验通过且被声明的依赖 payload 复制到最终镜像；不得直接复制解包目录，也不得在最终 stage 对整个依赖树执行会产生 copy-up layer 的递归改权限。生成器、源码、完整 release 和编译工具不得进入最终层。首版只携带一个当前 RPG runtime tag；第二个实际 release 出现后再设计受引用历史 artifact 的部署保留策略。两个镜像都使用第 3.1 节的发布输入 label。fresh dependency builder 在 aggregate Release 尚未发布、缺失或超出边界时必须返回非零；不得以已有宿主缓存、额外 Docker context、本地编译或跳过 `prepare` 绕过。固定 tag asset 可用后，最终 `retrom` 镜像只复制：

- 后端二进制；
- 每个配置 manifest `runtime_allowlist` 中的固定浏览器文件与 `selected_core_artifacts` 中的 core；
- 每个配置版本声明的兼容 override；
- 每个配置版本的 manifest、物化 DAT、逐字节校验的许可文件与确定性 `THIRD_PARTY_NOTICES`；
- `data/dat/rpgmaker/v1/manifest.json` 声明的单一 `retrom-runtime` tag、runtime allowlist，以及该 tag Release 内的许可和 notice；
- 密码 blocklist manifest、逐字节校验的 10,000 行 payload 与 MIT 许可；
- netplay schema/manifest；绝不复制任何测试 ROM；
- CA 根证书和运行所需的最小系统文件。

镜像不得复制 `testdata/public-roms/`、RTP、用户 RPG 项目/MV/MZ runtime、本地 SQLite/CAS、上传缓存、下载缓存、上游源码树或整个下载发布包。构建完成只产生镜像，不启动容器。运行镜像时依赖已在镜像内，启动不需要 GitHub/CDN 网络。

## 6. 升级与许可门禁

升级 EmulatorJS/core/DAT 必须作为独立变更：新增 manifest 版本目录，记录上游证据和 digest，重新物化与统计，并先新增/登记该精确版本的 Player adapter（即使代码与既有 adapter 相同也不能走默认分支）；再把新版本追加到 `RETROM_DEPENDENCY_VERSIONS` 并逐核心执行兼容、存档和 Arcade 依赖验收，最后切换 `RETROM_ACTIVE_EMULATORJS_VERSION`。`data-check` 必须在任何一个镜像 build 前证明 manifest/registry 对齐，两个镜像作为同一个项目版本部署。进程在 migration 后插入新 artifact 并以短事务切换每个 core 的 `selected_for_new_bindings`；新 artifact 的内置 DAT 仍按第 4 节先解析为 READY，再由启动引导把 manifest 指定版本设为 active，切换期间 readiness gate 阻止业务请求。artifact 自己的 DatVersion 和全部精确引用不变。release 回退只把新绑定选择切回另一个 `available_for_launch` artifact，并由同一引导恢复该 artifact manifest 指定的 READY active DAT，不修改 revision。版本只有在数据库已无 SaveState、GameVariantRevision 或其他保护引用且有审计记录后，才可将 `available_for_launch` 置零，并从后续镜像、adapter registry 和版本列表一并移除。不得只改版本字符串、覆盖目录、激活 PENDING DAT 或沿用错误 DAT。

EmulatorJS 与各 libretro core 的许可证不同。manifest schema V7 的 `license_materialization.entries` 是唯一许可输入清单：entry 以 `component_id` 显式关联 artifact，不能依赖数组位置；EmulatorJS 的文本来自已校验 release entry，各 core 使用官方 `cores.json` 声明的 license path，并从记录的固定 commit 下载。PPSSPP auxiliary asset 归属同一个 `ppsspp` component，不另造许可 component。`binary_association_status` 必须如实区分 `EXACT_RELEASE`、运行时日志给出的 `EMBEDDED_GIT_VERSION` 和仅按官方 build timestamp 推断的 `INFERRED_FROM_OFFICIAL_BUILD_TIMESTAMP`；后两者绝不能在 notice 中统一写成“已证明可复现源码”。

`THIRD_PARTY_NOTICES` 的生成算法固定为 `notice_format_version=1`：按 `notice_order`，为每项写 ASCII 分隔头、component ID、repository 的 `/tree/<source_commit>` URL、association status、declared license path，然后逐字节附加已校验许可文件；若源 bytes 最后不是 LF，只在分隔项之间追加一个 LF。禁止写生成时间、宿主路径或浮动 URL，因而相同 manifest/payload 必须产生相同 notice bytes。`deps-check` 重新生成到临时文件并逐字节比较；最终镜像同时保留 notice 和各原始许可文件。

默认镜像构建只面向本项目的私有自托管使用，不等于授予镜像再分发权。manifest 已把 Snes9x、FBNeo、MAME 2003 与 MAME 2003 Plus 标记为受限制组件；构建 target 可以完成本地镜像，但任何 registry push、公开发布、商业分发或第三方镜像交付都必须经过独立人工许可审查，并补足适用的源码提供/通知义务。Retrom 的 Make targets 本来就不执行 push；tag 发布 workflow 不设置二次人工审批，也不重复 PR quality，维护者创建并推送 tag 即确认质量门禁与上述义务已在发布前满足；随后流程只在双镜像构建及 release-input label 校验完成后使用 secret 自动登录并 push。ROM、BIOS 永不随镜像分发；Arcade DAT 只有在其 manifest 许可允许且通过发布审查时才进入镜像 allowlist。

## 7. retrom-runtime manifest、artifact 与许可

浏览器运行时由独立仓库 <https://github.com/xxxsen/retrom-runtime> 维护。它拥有 EasyRPG、mkxp、Native Web、ONScripter Yuri 四类 adapter、checkpoint codec、bridge、单元测试和 tag Release；不得导入 Retrom 的上传、审核、数据库、HTTP 或权限逻辑。Retrom 只消费一个固定的 `retrom-runtime` tag，并在 `data/dat/rpgmaker/v1/manifest.json` 记录 repository、tag、精确 tag commit、aggregate bundle/metadata asset URL 和八条宿主 route。源码树、Release 和本机物化目录在同一 tag 内每类 runtime 只有一份，不保留 V3/V4/V5/V6/V7 目录、adapter alias 或平行兼容包。当前唯一 pin 是 `v0.4.1`/`f7d46948a1a62fd204d543668851da065b6ba5ef`。

`make prepare-deps` 中的 `build.py prepare` 不是本地编译器：它先验证已有缓存是否与当前 tag/commit/metadata 和逐文件观测摘要一致；缓存不存在或损坏时，只下载该 tag 的 aggregate Release bundle 和 metadata，验证身份与文件 allowlist，再原子替换 `data/runtime/rpgmaker/v1/`。它不调用 Docker，不下载构建源码，也不比较远端 expected SHA。observed size/SHA 只用于本机缓存损坏检测、内容响应 ETag 和 artifact-set 冻结；同 tag 的准入身份是 repository/tag/tag commit/asset filename/adapter ABI。应用进程启动时只执行 `deps-check`，不联网下载。

开发联调不经过临时 tag 或 Release。维护者先在独立 `retrom-runtime` 功能分支完成单元回归，然后从 Retrom 执行 `RETROM_RUNTIME_DEV_ROOT=/absolute/path/to/retrom-runtime make dev`。该显式 override 在 `web-install` 后构建并链接 checkout 的 library，默认完整保留固定 Release 的 core/bridge bytes；Next 将该包作为显式 transpile/watch 输入，并使用被忽略的 `.next-runtime-dev`，避免正式 `.next` 的旧 bundle 缓存掩盖候选 adapter。它不修改 Git 中的 manifest、package lock、route/runtime version、artifact identity 或镜像输入；marker 绑定 checkout 的绝对路径和 commit，路径不匹配时依赖检查拒绝复用。adapter-only 修改不得隐式调用耗时 core build。只有确实修改 ONS core、host patch 或本地 bridge 时，维护者才先在独立仓库显式执行相应 build，并在可删除的 fresh dev DB 上额外设置 `RETROM_RUNTIME_DEV_INCLUDE_ASSETS=true`；这会原子覆盖被忽略的开发 runtime 缓存，禁止在已有 artifact 引用的数据库中让同一正式 identity 对应不同 bytes。

候选 runtime 必须先通过 Retrom 的真实审核预览以及 Product Launch、输入、checkpoint、不同 Launch 恢复链。通过后才合并 runtime PR、打不可移动 `v*` tag 并让 GitHub Actions 发布资产；Retrom 随后执行 `make retrom-runtime-dev-unlink` 恢复正式缓存，再用独立提交更新唯一 pin 并重跑正式依赖门禁和同一产品 Case。CI、镜像、release-input digest 与正式验收都不得把本地 override 的 observed size/SHA 当 Release 证据。

route registry 与 manifest 必须双向一一对应：

| 用户 core | 当前 route | adapter | 固定构件 |
| --- | --- | --- | --- |
| `rpgmaker_2000` | `RPG2000_EASYRPG` | `easyrpg-web` | 当前 tag 的 EasyRPG assets；强制 rpg2k |
| `rpgmaker_2003` | `RPG2003_EASYRPG` | `easyrpg-web` | 同一 EasyRPG assets；强制 rpg2k3 |
| `rpgmaker_xp` | `RPGXP_MKXP` | `mkxp-libretro-web` | 当前 tag 的 threaded mkxp assets 与 `position_bridge.rb`；RGSS1 |
| `rpgmaker_vx` | `RPGVX_MKXP` | `mkxp-libretro-web` | 相同 release assets、独立 artifact row，RGSS2 |
| `rpgmaker_vx_ace` | `RPGVXACE_MKXP` | `mkxp-libretro-web` | 相同 release assets、独立 artifact row，RGSS3 |
| `rpgmaker_mv` | `RPGMV_NATIVE` | `native-web` | 当前 tag 的 `native/bridge.js`，游戏自带 runtime，profile=`RPGMV` |
| `rpgmaker_mz` | `RPGMZ_NATIVE` | `native-web` | 同一 bridge，profile=`RPGMZ` |
| `onscripter_yuri` | `ONS_YURI` | `ons-yuri-web` | 当前 tag 的 `onsyuri.js/.wasm`；checkpoint slot 999，ABI=`ons-save` |

`retrom-runtime` 自身固定两个上游 fork：<https://github.com/xxxsen/Player> 与 <https://github.com/xxxsen/mkxp-z-libretro-emscripten>。上游 patch、构建和 Release workflow 留在各自仓库；Retrom 不再保存其 patch、Docker builder、source offer 或本地复现脚本。Player 的 `master` 与 mkxp wrapper 的 `main` 只做原始上游的 fast-forward 镜像，不包含 Retrom 补丁；各 fork 只保留一个当前 `retrom/<baseline>` 维护分支，并把它设为默认分支。上游有 tag 时 baseline 同时固定 tag 与解引用后的完整 commit；没有 tag 时固定完整 commit。短期 `fix/*`、`feat/*`、`build/*`、`sync/upstream-*` 从该维护分支创建并合回，Release 只从维护分支使用不可移动的 `rpg-runtime-<baseline>-rN` tag 产生；不得把补丁合入移动镜像、恢复 `retrom-web-*` tag 或保留 `runtime-clean` 等平行长期分支。新增或修改核心时先用上述本地 override 完成产品联调，通过后才发布新的稳定 `retrom-runtime` tag 并一次性替换 Retrom 的唯一 pin；不得为联调创建临时 prerelease、临时修改开发 manifest 或预建 inactive 候选。首个稳定版本不预建“旧/新 route”或 inactive 候选。

每个 artifact 项必须包含 `coreId/routeKey/runtimeFamily/runtimeAdapterKind/runtimeVersion/adapterId/entryPath/requiresThreads/savePayloadKind/saveMaxBytes/compatibility/selectedForNewBindings/availableForLaunch`。八个 artifact 的 `runtimeVersion` 都等于当前 `retrom-runtime` tag，且全部为唯一 selected/available 行。未知 route、adapter、额外 artifact 或 manifest 漂移必须阻断 readiness；不存在默认 route、`latest` 查找或跨 core fallback。未来真实 tag 升级的数据保留策略必须在出现第二个已验证 release 时再按实际引用需求设计，不能在首版中携带虚构的历史包。

EasyRPG Player 为 GPLv3、liblcf 为 MIT、mkxp-z 为 GPLv2+、RetroArch 为 GPLv3、Nostalgist 为 MIT，OnscripterYuri 为 GPL-2.0-or-later。`retrom-runtime` 的 tag manifest 必须固定上游 repository、tag、commit、Release metadata 或固定源码构建声明与 adapter ABI；aggregate Release 同时携带 runtime、许可与 notice。Retrom 不再镜像上游源码、patch 或构建脚本。MV/MZ runtime 和插件是用户内容，Retrom 不宣称或取得其再分发权。manifest/registry/adapter/许可任一改变必须运行 `make data-check`、`make prepare-deps`、`make deps-check`，并让两个镜像的 `io.retrom.release-input-sha256` 完全相同。

许可证、notice 与上游源码定位信息属于 tag Release 和部署/分发物，不通过应用 HTTP API 或浏览器页面公开原文。管理员“运行依赖”页显示的 `coreId + routeKey + artifactId` 是追溯键：它必须唯一命中 RPG manifest 的 artifact 项，再定位 `retrom-runtime` tag metadata、`runtime-manifest.json`、`THIRD_PARTY_NOTICES.md` 和两个上游 fork/tag。镜像保留 aggregate notice 与许可文件；禁止新增返回许可 payload、宿主路径或源码 archive 的应用端点。

## 8. 统一验收入口

小型 manifest 结构（包括联机 exact manifest 与 RPG route/artifact manifest）由 `ACC-QA-001` 的 `make data-check` 覆盖；RPG 固定 release repo/tag/tag commit/asset 形状/adapter ABI、registry 对齐、七核心产品闭环与 tag 源码定位由 `ACC-RPG-002`–`008` 覆盖，`ACC-RPG-009`–`012` 在七核心完成前保持暂停；联机协议、安全、feature flag 与单机回归由 `ACC-NP-010`–`013` 覆盖，真实双端核心运行与生命周期由 `ACC-NP-014`–`022` 覆盖；完整 payload 准备与本地 observed digest 校验由 `ACC-DAT-001` 覆盖；密码 blocklist 的启动期校验与拒绝行为由 `ACC-AUTH-003` 覆盖；镜像内依赖、无启动期下载和许可文件由 `ACC-PKG-001` 覆盖；版本变化执行 `ACC-DAT-006`。
