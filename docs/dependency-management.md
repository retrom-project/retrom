# 第三方运行时、DAT 与账户安全依赖管理

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期实施基线 |
| 版本 | 1.1 |
| 日期 | 2026-08-11 |
| 适用范围 | EmulatorJS、core artifact、兼容覆盖、预置 Arcade DAT 与密码 blocklist |

## 1. 结论

Git 只保存小型、可审查的来源清单、物化配方、大小、SHA-256、许可来源与解析统计，不保存 EmulatorJS 发布包、core、ROM、BIOS、许可 payload、五份 Arcade DAT payload 或密码 blocklist payload。真实 payload 在开发或镜像构建开始前按固定 URL/commit 取得或从锁定源码确定性生成，写到被 Git 忽略的固定目录，并在使用前逐字节校验。

应用进程启动期间禁止联网下载或自动升级依赖。依赖缺失、大小或 SHA-256 不符时，后端必须拒绝进入 ready 状态并输出 `make prepare-deps` 这一条可操作命令；不能回退到 CDN、最新版本或另一个 core。

## 2. 唯一事实源与目录

运行时机器事实源由按 SemVer 升序配置的 manifest 集合组成：[`4.2.3/manifest.json`](../data/dat/emulatorjs/4.2.3/manifest.json) 提供 33 个基础 artifact 与五份 DAT，[`4.3.0-pre/manifest.json`](../data/dat/emulatorjs/4.3.0-pre/manifest.json) 覆盖 DOSBox Pure、Genesis Plus GX Wide 与 Azahar；按“后声明覆盖同 core”合并后共有 35 个 enabled core artifact。账户密码拒绝列表由 [`password-blocklists/v1/manifest.json`](../data/auth/password-blocklists/v1/manifest.json) 独立锁定 SecLists tag、40 位 commit、10,000 行 payload、MIT 许可及各自 size/SHA-256。后出现的运行时 manifest 只替换它明确列出的 core；不能要求部分覆盖重复全部核心/DAT。`data/example/fixtures.json` 只描述本地兼容夹具，不得反向覆盖依赖版本。运行时 manifest 同时锁定：

- EmulatorJS release、tag、发布资产 URL/size/SHA-256；
- 允许进入镜像/由 Go 静态服务的 EmulatorJS 文件 allowlist、36 个跨版本 selected artifact 条目（合并为 35 个 enabled core artifact）以及 PPSSPP auxiliary asset 的路径、size/SHA-256；
- Player adapter ID、runtime base 与 loader 的精确 release 相对路径；前端机器 registry 以 `web/features/player/adapters/registry.json` 为单一可读索引；
- `mame2003` 的显式兼容覆盖；
- 每份 DAT 的 core source 证据、物化配方、最终 size/SHA-256 和解析统计。
- 两个 EmulatorJS 版本与所有 selected core 的许可文件路径、固定来源、size/SHA-256、二进制关联证据等级和 notice 顺序（两个 manifest 共 38 个 component）。

当前 4.2.3 manifest 使用 schema V5，CoreArtifact compatibility 使用 schema V3。每个 `selected_core_artifacts` 项都显式给出 `runtime_core_id`、loader 实际请求的 `requested_artifact_basename`、canvas policy、默认 option、PersistentSave mode/kind、input mode、启动动作、`supported_content_kinds` 与 report；校验器不再根据 core ID 推导这些值。只有 `yabause` 同时声明 `MULTI_DISC_M3U_V1` 与 `multi_disc={max_discs:8,max_total_bytes:1073741824,delivery:EAGER_EXTERNAL_FILES}`，没有该对象、来自旧 schema 或 kind 不匹配都等价于不支持。`dosbox_pure`、`mednafen_psx_hw`、`ppsspp`、`azahar` 的 loader basename 分别为 `dosbox_pure-thread-wasm.data`、`mednafen_psx_hw-thread-wasm.data`、`ppsspp-thread-wasm.data`、`azahar-thread-wasm.data`，并与实际 THREAD_WASM 路径逐字匹配。`auxiliary_files` 当前只登记 `data/cores/ppsspp-assets.zip`，它和 36 份跨版本 core report 都必须在各自 runtime allowlist 中。4.2.3 联机适配器还精确发布 release 内的 `data/emulator.css` 与 loader 所列八个 `data/src/*` 文件；联机模式令 loader 使用这些受 size/SHA 约束的可审计源文件，以接收 RetroArch 原生 state-load 完成日志，同时保持 EmulatorJS 自带的实验性 netplay transport 关闭。普通单人模式继续使用 minified loader 资产。

4.2.3 的 Player adapter 固定为 `ejs-4.2.3-v2`，registry 不保留无法由当前 manifest 解释的 v1 fallback。该 adapter 对官方 `extract7z.js` 与 `extractzip.js` Worker Blob 执行运行时专题锁定的 CSP 兼容转换；转换发生在浏览器内且任一源形状漂移即阻断，不能修改 runtime allowlist 中的官方 bytes、size/SHA 或许可关联证据。dependency bootstrap 对同一 `(core_id, emulatorjs_version, sha256)` 保留 CoreArtifact ID；bundle/flavor/path/size/compatibility/enabled 等运行语义变化时原子递增 artifact version，逐字节等价的重复 bootstrap 连 `updated_at_ms` 也不改。历史 VariantRevision、SaveState 与 PersistentSave 继续绑定原 artifact ID，但未发布的 generation 3 validation 全部 stale，必须由 compatibility V3 重新验证后才能发布。

联机使用独立的 [`data/netplay/v1/manifest.json`](../data/netplay/v1/manifest.json) 与 schema 作为普通兼容性之上的 core-profile allowlist。schema v3 的首发 profile 为 `fceumm-423-v1` 与 `fbneo-423-v1`：每项锁定 EmulatorJS 4.2.3、core artifact SHA-256、`maxPlayers=2` 和核心级 prediction 上限；FCEUmm 为 8 帧，FBNeo 为 0 帧严格 lockstep。协议另锁定 `SINGLE_FILE`、`retrom-netplay-v1`、`ejs-4.2.3-v2`、`ejs-netplay-4.2.3-v1`、24 controls、120-frame checkpoint、8 帧 prediction 协议上限、120 rollback、600 history 与 1 MiB state 上限。任意发布游戏只要当前 READY VariantRevision 使用该精确 artifact、内容类型受协议允许且依赖快照仍有效，即可选择此 profile；ROM 逻辑名、大小、hash 与来源 archive 不进入产品准入。校验器必须与 runtime manifest 的 core artifact、supported content kind 和前端 registry 双向一致，未知 EmulatorJS/adapter/profile 不得 fallback。F-1 Race 与 Lode Runner 只作为两个 core profile 的代表性双端回归夹具，由操作者物化到被忽略的 `data/example/local-fixtures/netplay/`，不属于 dependency payload、镜像内容或逐游戏白名单。

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
| `make data-check` | 只校验已提交的小文件：4.2.3 manifest schema V5、artifact compatibility V3、多盘 kind/limits、普通与联机 Player adapter registry 双向一致、netplay core-profile exact allowlist、DAT/许可字段与 `SHA256SUMS`，以及密码 blocklist manifest 的固定 tag/commit、URL、行数、size/SHA 和 MIT 许可。无 payload、无网络时也必须通过。 |
| `make prepare-deps` | 对 `RETROM_DEPENDENCY_VERSIONS` 中缺失/错误的 runtime、core、DAT、许可 payload 以及账户密码 blocklist/许可执行固定来源下载、确定性转换、解包与原子发布，生成 notice；默认 `4.2.3,4.3.0-pre`，最后隐式执行 `deps-check`。已有正确缓存时不访问网络。 |
| `make deps-check` | 不联网，逐个校验运行时 allowlist、选定 core、可选 DAT、override、许可输入、确定性 notice，以及密码 blocklist 的 size/SHA/10,000 行和许可。缺少、额外发布或不匹配均失败。 |
| `make release-input-digest` | 不联网、不写工作树，按本节算法校验并只向 stdout 输出 64 位小写 `releaseInputDigest`；镜像 target 调用同一 helper，不复制 shell 算法。 |
| `make dev` | 先依赖 `prepare-deps`，然后只启动宿主机 Go/Next.js 进程；依赖准备不改变“非 Docker”契约。 |

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
      "playerAdapterId": "ejs-4.2.3-v2"
    },
    {
      "version": "4.3.0-pre",
      "manifestSha256": "<sha256-of-exact-manifest-bytes>",
      "playerAdapterId": "ejs-4.3.0-pre-v1"
    }
  ],
  "activeEmulatorjsVersion": "4.2.3",
  "passwordBlocklistManifestSha256": "<sha256-of-exact-password-blocklist-manifest-bytes>",
  "netplayManifestSha256": "<sha256-of-exact-netplay-manifest-bytes>"
}
```

dependencyVersions 必须等于规范化后的 `RETROM_DEPENDENCY_VERSIONS`、按 SemVer 升序，active 必须属于其中；各 manifest SHA 对完整原始 bytes 计算，adapter ID 从运行时 manifest 读取。两个镜像都设置 OCI label `io.retrom.release-input-sha256=<releaseInputDigest>`。每个镜像 target 在 Docker build 前后重算并比较，工作树、选定版本或密码 blocklist manifest 中途改变必须返回非零，该产物不得被宣称可部署。`make build-images` 只计算一份预期 digest，两个子 target 仍各自复核当前值，最后用 image inspect 确认两个 label 与预期值完全相同。直接绕过 Makefile 的 Docker build 不是受支持的发布路径；部署时 label 缺失或不同的两个镜像必须被编排/人工门禁拒绝。

## 4. 服务启动与健康检查

本地运行时通过三个只读配置确定依赖集合：`RETROM_DEPENDENCY_ROOT` 是包含 `dat/emulatorjs/<version>/` 与 `runtime/emulatorjs/<version>/` 的绝对根；`RETROM_DEPENDENCY_VERSIONS` 是无空白、无重复、按 SemVer（含 prerelease）升序的逗号列表，默认 `4.2.3,4.3.0-pre`；`RETROM_ACTIVE_EMULATORJS_VERSION` 必须属于该列表，当前为 `4.2.3`。开发值由 `make dev` 显式传入仓库 `data/` 的绝对路径，镜像值指向只读依赖层。

对每个配置版本 `v`，manifest 固定解析为 `<root>/dat/emulatorjs/<v>/manifest.json`，内置 DAT 根为同目录，release 根为 `<root>/runtime/emulatorjs/<v>`。数据库中 CoreArtifact 的 `relative_path` 相对它自己的 release 根解析，静态 URL 固定为 `/runtime/emulatorjs/<v>/<relative_path>`。当前合并后的 35 个 manifest artifact 被设为 enabled；保留版本的 artifact 以 disabled 历史行存在，但被 SaveState/PersistentSave/历史 READY VariantRevision 精确引用时仍可启动。普通新验证只选择当前 enabled artifact，绝不能因旧文件仍在磁盘而自动选中它。

后端启动顺序固定为：

1. 规范化版本列表，并逐版本读取约定位置的 manifest，验证 schema、目录名、manifest 版本、Player adapter 描述和 runtime base/loader allowlist 一致；同时读取密码 blocklist manifest 与 payload，逐字节校验并构建密码拒绝集合；前后端 adapter 对齐已在构建期 `data-check` 完成，Go 进程不假装读取另一镜像的代码；
2. 逐版本校验 allowlist、selected core、override、DAT、许可输入与 notice，并校验 blocklist 许可 payload；
3. 校验部署的 core artifact，包括 `mame2003` override，并建立仅含已验证版本/路径的静态路由表；
4. 打开数据库并执行 migration；
5. 在一个短事务中 upsert 全部已配置版本的 CoreArtifact 与版本化静态 BIOS Requirement；逐 core 将 enabled artifact 切到配置顺序中最后一个声明它的 manifest，再按 `core_artifact_id + dat_sha256 + parser_version` 创建或复用实际存在的内置 DatVersion。部分 runtime overlay 可以不含 DAT/其他 core，不能因此删除基础版本 seed；
6. 建立 allowlist 静态路由，启动 HTTP 与 worker。`/health/live` 此时可返回 200；只要当前 enabled Arcade CoreArtifact 还没有 `READY` 的 active DatVersion，`/health/ready` 就返回 `503 DEPENDENCY_INDEXING`，除 health 外的全部路由统一返回错误 envelope `503 SERVICE_NOT_READY`；
7. Worker 在事务外通过受限 streaming parser 读取 DAT，以数据模型规定的短事务批次写入“尚未发布”的索引行；成功后以一个短事务把 DatVersion 转为 `READY`、发布这些行的可见性，并且仅在该 CoreArtifact 此刻仍无 active DatVersion 时激活此内置版本、物化 Requirement 和写 AuditEvent。管理员已经激活的用户 DAT 永远不会被启动任务覆盖。所有当前 enabled Arcade artifact 都有 `READY` active DatVersion 后，ready 才转为 200；非 Arcade 静态 BIOS seed 不依赖 DAT 解析。

步骤 1–5 属于同步启动校验，受 `RETROM_STARTUP_CHECK_TIMEOUT` 约束（默认 60 秒）；后台 DAT_PARSE 的 30 分钟 execution deadline 由通用 Job 契约单独约束，不能塞进这 60 秒，也不能持有长数据库事务。进程在解析期间保持 live，重启后按 lease/Job 规则恢复同一任务。当前 enabled artifact 所需的内置解析若确定性失败，则 DatVersion/Job 为 `FAILED`，进程保持 live、ready 固定为 `503 DEPENDENCY_DAT_PARSE_FAILED`，其他路由继续由 readiness gate 阻断；相同 DAT/parser 的重启不得自动抹掉失败证据，修复必须发布新的 parser version（产生新 DatVersion/Job）或按通用规则对 retryable execution 显式重试。未启用版本的预解析失败可记录诊断但不阻断当前 ready。

任一已配置 manifest 无法解析、Player adapter 描述与其 allowlist 自相矛盾、payload 缺失/不匹配、core 与 DAT 绑定不成立，或 active 版本不在版本列表时，Go 进程在步骤 1–5 内非零退出，不先开放 HTTP。数据库引用了未配置版本时进程仍可 ready，但对应存档/历史 revision 的启动必须以 `LAUNCH_CORE_ARTIFACT_UNAVAILABLE` 阻断并指出所需版本；不得回退到当前 core。前端遇到部署错配造成的未知 adapter 按运行时专题安全阻断；发布验收必须在部署前阻止这种错配。发布升级镜像的验收要求把所有仍受保护引用的版本纳入列表，因此正常升级路径不会产生该 blocker。Hasheous 暂时不可用不影响进程 ready；它只影响单次刮削并走有界降级。

## 5. 镜像构建

根 Dockerfile 的 dependency builder stage 必须读取同一组 manifest，当前默认列表为 `4.2.3,4.3.0-pre`；该 stage 包含 FBA2012 锁定源码生成器及临时原生编译工具链，完整 release 只作为校验与选择输入。生成结束后必须从 manifest 确定性导出一个全新的只读目录，只把校验通过且被声明的依赖 payload 复制到最终镜像；不得直接复制解包目录，也不得在最终 stage 对整个依赖树执行会产生 copy-up layer 的递归改权限。生成器、源码、完整 release 和编译工具不得进入最终层。升级镜像必须同时保留数据库中受 SaveState/PersistentSave/READY VariantRevision 保护的旧版本；两个镜像都使用第 3.1 节的发布输入 label。最终 `retrom` 镜像只复制：

- 后端二进制；
- 每个配置 manifest `runtime_allowlist` 中的固定浏览器文件与 `selected_core_artifacts` 中的 core；
- 每个配置版本声明的兼容 override；
- 每个配置版本的 manifest、物化 DAT、逐字节校验的许可文件与确定性 `THIRD_PARTY_NOTICES`；
- 密码 blocklist manifest、逐字节校验的 10,000 行 payload 与 MIT 许可；
- netplay schema/manifest；绝不复制本地联机 ROM fixture；
- CA 根证书和运行所需的最小系统文件。

镜像不得复制 `data/game/`、`data/example/results/`、本地 SQLite/CAS、上传缓存、下载缓存、源码树或整个 7z 发布包。构建完成只产生镜像，不启动容器。运行镜像时依赖已在镜像内，启动不需要 GitHub/CDN 网络。

## 6. 升级与许可门禁

升级 EmulatorJS/core/DAT 必须作为独立变更：新增 manifest 版本目录，记录上游证据和 digest，重新物化与统计，并先新增/登记该精确版本的 Player adapter（即使代码与旧 adapter 相同也不能走默认分支）；再把新版本追加到 `RETROM_DEPENDENCY_VERSIONS` 并逐核心执行兼容、存档和 Arcade 依赖验收，最后切换 `RETROM_ACTIVE_EMULATORJS_VERSION`。`data-check` 必须在任何一个镜像 build 前证明 manifest/registry 对齐，两个镜像作为同一个项目版本部署。进程在 migration 后用短事务切换每个 core 的 enabled artifact；新 artifact 的内置 DAT 仍按第 4 节先解析为 READY，只有该 artifact 没有 active DAT 时才能激活，切换期间 readiness gate 阻止业务请求。旧 artifact 自己的 active DAT 和全部历史引用不变。回滚只切回旧 enabled artifact 并复用该 artifact 原有 READY active DAT，不修改历史 revision。旧版本只有在数据库已无 SaveState、PersistentSave、GameVariantRevision 或其他保护引用且有审计记录后，才可从后续镜像、adapter registry 和版本列表一并移除。不得只改版本字符串、覆盖目录、激活 PENDING DAT 或沿用旧 DAT。

EmulatorJS 与各 libretro core 的许可证不同。manifest schema V5 的 `license_materialization.entries` 是唯一许可输入清单：entry 以 `component_id` 显式关联 artifact，不能依赖数组位置；EmulatorJS 的文本来自已校验 release entry，各 core 使用官方 `cores.json` 声明的 license path，并从记录的固定 commit 下载。PPSSPP auxiliary asset 归属同一个 `ppsspp` component，不另造许可 component。`binary_association_status` 必须如实区分 `EXACT_RELEASE`、运行时日志给出的 `EMBEDDED_GIT_VERSION` 和仅按官方 build timestamp 推断的 `INFERRED_FROM_OFFICIAL_BUILD_TIMESTAMP`；后两者绝不能在 notice 中统一写成“已证明可复现源码”。

`THIRD_PARTY_NOTICES` 的生成算法固定为 `notice_format_version=1`：按 `notice_order`，为每项写 ASCII 分隔头、component ID、repository 的 `/tree/<source_commit>` URL、association status、declared license path，然后逐字节附加已校验许可文件；若源 bytes 最后不是 LF，只在分隔项之间追加一个 LF。禁止写生成时间、宿主路径或浮动 URL，因而相同 manifest/payload 必须产生相同 notice bytes。`deps-check` 重新生成到临时文件并逐字节比较；最终镜像同时保留 notice 和各原始许可文件。

默认镜像构建只面向本项目的私有自托管使用，不等于授予镜像再分发权。manifest 已把 Snes9x、FBNeo、MAME 2003 与 MAME 2003 Plus 标记为受限制组件；构建 target 可以完成本地镜像，但任何 registry push、公开发布、商业分发或第三方镜像交付都必须经过独立人工许可审查，并补足适用的源码提供/通知义务。Retrom 的 Make targets 本来就不执行 push；tag 发布 workflow 不设置二次人工审批，也不重复 PR quality，维护者创建并推送 tag 即确认质量门禁与上述义务已在发布前满足；随后流程只在双镜像构建及 release-input label 校验完成后使用 secret 自动登录并 push。ROM、BIOS 和用户 DAT 永不随镜像分发。

## 7. 统一验收入口

小型 manifest 结构由 `ACC-QA-001` 的 `make data-check` 覆盖；联机 exact manifest 与真实 fixture 由 `ACC-NP-001` 覆盖；完整 payload 准备与校验由 `ACC-DAT-001` 覆盖；密码 blocklist 的启动期校验与拒绝行为由 `ACC-AUTH-003` 覆盖；镜像内依赖、无启动期下载和许可文件由 `ACC-PKG-001` 覆盖；版本变化执行 `ACC-DAT-006`。
