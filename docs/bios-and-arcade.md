# BIOS、固件与 Arcade DAT

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期实施基线 |
| 版本 | 1.1 |
| 日期 | 2026-08-08 |
| EmulatorJS 基线 | v4.2.3 |

## 1. 责任边界

- BIOS/Firmware Requirement 定义核心启动所需文件及期望 hash。
- Arcade DAT 用于特定核心下的 machine、ROM entry、clone/parent 和 BIOS/base archive 识别。
- DAT 不是展示元信息刮削源；Hasheous 的职责见 [导入、刮削与审核](./import-and-review.md)。
- BIOS 与 DAT 按 Core/Core artifact 管理，不按 PlatformInstance 复制；游戏目录只引用默认 Core。
- 浏览器和 EmulatorJS 不解析原始 DAT。后端预解析并持久化，页面查询物化结果，启动查询依赖快照。

精确来源、commit、artifact hash、DAT hash 和已知格式差异以 [EmulatorJS 4.2.3 Arcade DAT 基线](./arcade-dat-baseline.md)及 [`data/dat` manifest](../data/dat/emulatorjs/4.2.3/manifest.json) 为唯一事实源。

## 2. 哈希规则

本地 Blob 去重使用 SHA-256；外部规范的身份 hash 独立保存：

- `sha256`：CAS 地址和文件完整性。
- `md5`：常见 BIOS 文件身份。
- `sha1`：DAT ROM/disk entry。
- `crc32`：DAT 与 ZIP entry 快速匹配。

MD5/CRC 只用于身份识别，不作为安全机制。

上传 BIOS 时：

- 流式写入 CAS 并计算全部支持 hash。
- 保存原始文件名用于审计，挂载时使用 Requirement 的逻辑文件名。
- 文件存在但 hash 与期望不同：保存并显示 Warning，不强制拒绝。
- Archive 完全缺少必需 entry：属于 Blocker；entry 名存在但 size/hash 与 DAT 不同则归为 `HASH_WARNING`，仍保存、装入并允许启动，不能把“哈希不一致仅提醒”误实现成强制校验。

## 3. 静态 BIOS 目录

### 3.1 fceumm

| 文件 | 条件 | MD5 |
| --- | --- | --- |
| `disksys.rom` | Famicom Disk System 必需 | `ca30b50f880eb660a320674ed365ef7a` |
| `gamegenie.nes` | Game Genie 模式必需 | `7f98d77d7a094ad7d069b74bd553ec98` |

来源：[Libretro FCEUmm](https://docs.libretro.com/library/fceumm/)。

### 3.2 snes9x

| 文件 | 条件 | MD5 |
| --- | --- | --- |
| `BS-X.bin` | 可选；仅相应 BS-X 内容会使用 | `fed4d8242cfbed61343d53d48432aced` |
| `STBIOS.bin` | 可选；仅相应 Sufami Turbo 内容会使用 | `d3a44ba7d42a74d3ac58cb9c14c6a5ca` |

来源：[Libretro Snes9x](https://docs.libretro.com/library/snes9x/)。

### 3.3 gambatte

| 文件 | 条件 | MD5 |
| --- | --- | --- |
| `gb_bios.bin` | 可选 GB Bootloader | `32fbbd84168d3482956eb3c5051637f5` |
| `gbc_bios.bin` | 可选 GBC Bootloader | `dbfce9db9deaa2567f6a84fde55f9680` |

来源：[Libretro Gambatte](https://docs.libretro.com/library/gambatte/)。

### 3.4 mgba

| 文件 | 条件 | MD5 |
| --- | --- | --- |
| `gba_bios.bin` | 可选 | `a860e8c0b6d573d191e4ec7db1b1e4f6` |
| `gb_bios.bin` | 可选 | `32fbbd84168d3482956eb3c5051637f5` |
| `gbc_bios.bin` | 可选 | `dbfce9db9deaa2567f6a84fde55f9680` |
| `sgb_bios.bin` | 可选 | `d574d4f9c12f305074798f54c091a8b4` |

来源：[Libretro mGBA](https://docs.libretro.com/library/mgba/)。

### 3.5 扩展核心

EmulatorJS 4.2.3 manifest 另声明下列 12 个静态 Requirement；精确 size、MD5、SHA-1/SHA-256 与来源版本以 manifest 为机器事实源，不能从本文反向生成 seed。

| core | logical name | mode / condition | delivery |
| --- | --- | --- | --- |
| `nestopia` | `disksys.rom` | `CONDITIONAL / FDS_CONTENT` | `BIOS_BUNDLE` |
| `melonds` | `bios7.bin` | `REQUIRED` | `EXTERNAL_FILE /retroarch/userdata/system/bios7.bin` |
| `melonds` | `bios9.bin` | `REQUIRED` | `EXTERNAL_FILE /retroarch/userdata/system/bios9.bin` |
| `melonds` | `firmware.bin` | `REQUIRED` | `EXTERNAL_FILE /retroarch/userdata/system/firmware.bin` |
| `a5200` | `5200.rom` | `REQUIRED` | `BIOS_BUNDLE` |
| `pcsx_rearmed` | `scph5500.bin` | `REQUIRED` | `BIOS_BUNDLE` |
| `mednafen_psx_hw` | `scph5500.bin` | `REQUIRED` | `BIOS_BUNDLE` |
| `handy` | `lynxboot.img` | `REQUIRED` | `BIOS_BUNDLE` |
| `yabause` | `saturn_bios.bin` | `REQUIRED` | `BIOS_BUNDLE` |
| `opera` | `panafz10.bin` | `REQUIRED` | `BIOS_BUNDLE` |
| `prosystem` | `7800 BIOS (U).rom` | `REQUIRED` | `BIOS_BUNDLE` |
| `mednafen_pcfx` | `pcfx.rom` | `REQUIRED` | `BIOS_BUNDLE` |

MelonDS 三项必须全部存在才能得到 READY。它们不进入根 BIOS bundle：Variant dependency snapshot 锁定 installation/version/blob/delivery/path，Launch 创建事务复制到 `launch_external_files`，配置只生成三个受 capability 保护的同源 URL。切换 active installation 后，旧 Launch 继续读旧 Blob，新 Launch 使用新 Blob；外部文件不得因当前 BIOS 状态变化而漂移。

### 3.6 Arcade Core

不维护脱离 DAT 的手工固定表。当前 v4.2.3 FBNeo DAT 中实际存在 13 个 `isbios="yes"` machine：

`bubsys`、`cchip`、`decocass`、`isgsm`、`midssio`、`namcoc69`、`namcoc70`、`namcoc75`、`neogeo`、`nmk004`、`pgm`、`skns`、`ym2608`。

MAME 2003 和 MAME 2003-Plus 的旧 List XML 没有显式 `isbios` 属性；当前真实基线各有 17 个由 `romof != cloneof` 推导的 base dependency target。名称、entry 和 hash 必须从活动 DAT 解析，不能复制 FBNeo 列表。

数据库中的 BIOS Requirement 是 CoreArtifact 内的稳定逻辑安装槽，而不是把某份 DAT entry 复制成永不变化的手工表：静态固件 slot 的 `source_kind=STATIC`，condition/activation 按第 3.8 节；Arcade BIOS/base archive 的 slot 为 `DAT_MACHINE`，logical name 固定 `<machine>.zip`，`catalog_digest` 来自活动 DAT 的规范必需 entry 集，外层 ZIP 本身没有 DAT 规定的唯一 hash。切换 DAT 时按 logical slot upsert/disable 并递增发生变化的 requirement version，旧安装 Blob 不复制；随后针对新 catalog 重验证 active installation。

CoreArtifact 升级会建立新的 Requirement 槽，不把旧 artifact 的 active installation 暗中复制成新安装；旧 VariantRevision/存档仍按其快照使用旧槽与 Blob，新 artifact 在 BIOS 页明确显示未安装。用户再次选择同一文件安装时 CAS 会按 SHA-256 去重，但会创建归属新 Requirement 的独立 Installation 并重新校验。这样不会把旧 core 的“已匹配”结论冒充新 core 的证据，也没有未建模的跨 artifact 自动迁移。

### 3.7 dosbox_pure

普通 DOS 游戏没有统一固定 BIOS。可执行程序和目录内容属于 GameContentRevision，`dosbox.conf`/启动 ZIP 属于 GameVariantRevision 的派生文件；ISO/CUE/IMG/VHD 等磁盘镜像一期不接收，不能因 Core 未来可能支持而展示成已支持 BIOS/内容类型。

### 3.8 条件与 core option 的精确规则

静态目录不能只保存文件名/hash：Gambatte 和 mGBA 的上游都要求 core option 开启后才会使用可选启动 BIOS。每个 CoreArtifact 的 seed 因此还要写入下表的稳定 `condition_code` 与 canonical `activation_options_json`；它们属于 Requirement/catalog digest，不允许前端按 core display name 特判：

| Core / Requirement | mode / condition_code | 一期适用条件 | activation_options_json |
| --- | --- | --- | --- |
| fceumm / `disksys.rom` | `CONDITIONAL / FDS_CONTENT` | primary content 后缀为 `.fds`；适用时是 Blocker | `null` |
| fceumm / `gamegenie.nes` | `CONDITIONAL / GAME_GENIE_ADDON_MODE` | 一期不提供该 add-on 模式，目录中可上传但不阻断普通 `.nes` | `null` |
| snes9x / `BS-X.bin` | `OPTIONAL / SNES_BSX_FIRMWARE` | active 时装入 bundle，由 core 仅对相应内容使用 | `null` |
| snes9x / `STBIOS.bin` | `OPTIONAL / SNES_SUFAMI_FIRMWARE` | active 时装入 bundle，由 core 仅对相应内容使用 | `null` |
| gambatte / `gb_bios.bin` | `OPTIONAL / GB_CONTENT` | primary content 后缀为 `.gb` 或 `.dmg` | `{"gambatte_gb_bootloader":"enabled"}` |
| gambatte / `gbc_bios.bin` | `OPTIONAL / GBC_CONTENT` | primary content 后缀为 `.gbc` | `{"gambatte_gb_bootloader":"enabled"}` |
| mgba / `gba_bios.bin` | `OPTIONAL / GBA_CONTENT` | primary content 后缀为 `.gba` | `{"mgba_use_bios":"ON"}` |
| mgba / `gb_bios.bin` | `OPTIONAL / GB_CONTENT` | primary content 后缀为 `.gb` 或 `.dmg` | `{"mgba_use_bios":"ON"}` |
| mgba / `gbc_bios.bin` | `OPTIONAL / GBC_CONTENT` | primary content 后缀为 `.gbc` | `{"mgba_use_bios":"ON"}` |
| mgba / `sgb_bios.bin` | `OPTIONAL / MGBA_SGB_MODEL` | 一期不提供 SGB model 选择，目录中可上传但标“未使用” | `{"mgba_use_bios":"ON"}` |

primary content 指 GameContentRevision 中唯一 `CONTENT` 文件；host-console 原 ZIP 已在验证阶段物化成保留真实后缀的唯一可运行 member，因此不能用上传 archive 的 `.zip` 后缀判断。逻辑名按 ASCII lower-case 比较最后一个后缀，不读标题或刮削平台猜类型。FDS/GB/GBC/GBA 按表中后缀判定；一期对 BS-X/Sufami 没有足够可靠的独立 classifier，所以这两项“已安装则对全部 Snes9x Variant 装入并进入 digest、未安装则只在完整目录显示且不产生逐游戏 Warning”，由 core 决定是否实际读取。`GAME_GENIE_ADDON_MODE/MGBA_SGB_MODEL` 一期恒不适用。其余 STATIC requirement 的适用集合与 active installation/status/options 一并进入 `validation_input_digest/dependency_snapshot_json`；不适用项不装入本次 bundle，也不触发本游戏重校验。

适用的 REQUIRED/CONDITIONAL 项缺失时阻断；适用 OPTIONAL 缺失时仅 Warning 且不加 activation option。存在 `MATCHED` 或 `HASH_WARNING` active installation 时按逻辑名装入 BIOS bundle并合并其 activation options；对 DAT_MACHINE，全部必需 entry 名存在但 size/hash 有差异也属于 `HASH_WARNING`。错误 hash 始终遵循“提示但允许”的产品要求；只有缺少必需 entry 的 `MISSING_ENTRY` 与不可读的 `INVALID` 不装入。每次 EmulatorJS 实例都是新配置，所以无需发送反向的 `disabled/OFF`，也不能让浏览器上一次设置成为事实源。上游依据分别是 [Gambatte BIOS/core option](https://docs.libretro.com/library/gambatte/) 与 [mGBA BIOS/core option](https://docs.libretro.com/library/mgba/)。

## 4. BIOS 状态

| 条件 | 状态 | 可启动 |
| --- | --- | --- |
| 必需逻辑文件不存在 | `MISSING` | 否 |
| 必需 archive entry 缺失 | `MISSING_ENTRY` | 否 |
| 文件/全部必需 entry 与期望 size/hash 匹配 | `MATCHED` | 是 |
| 文件或已存在的必需 entry 的 size/hash 不同 | `HASH_WARNING` | 是，带 Warning |
| 可选文件不存在 | `OPTIONAL_MISSING` | 是 |
| 文件不可读或 archive 损坏 | `INVALID` | 否 |
| parent/BIOS entry 已内含于游戏 archive | `SATISFIED_BY_CONTENT` | 是 |

BIOS 页面默认只统计当前游戏库各 GameVariant 当前 VariantRevision 实际引用的 Requirement；核心完整目录中的未使用项标记“未使用”，不进入红色缺失计数。

`MISSING_ENTRY` installation 可以作为用户已上传文件保留并维持 active，方便展示实际缺项和直接替换，但绝不能装入 READY Variant 或 Launch bundle；`INVALID`（损坏、不安全或不可读 archive）在上传时保留审计记录但不能成为 active。DAT_MACHINE 安装必须在数据库写事务外按统一 ZIP 安全限制扫描归档，并把条目 hash 目录持久化；校验范围只含普通条目与默认 BIOS set 的非 NODUMP 条目。文件名不同时，仅当 size 与 DAT SHA-1（缺失时为 CRC32）一致才视为历史别名并记录 Warning；同名但内容不同为 `HASH_WARNING`，内容不存在才是 `MISSING_ENTRY`。静态文件 hash 不匹配也统一为 `HASH_WARNING` 并可进入 Launch bundle。每次状态都记录 `validated_requirement_version`，页面若发现版本不一致显示“待重验证”，不能继续使用旧 MATCHED 标签。

待审核条目点击“重新运行检查”时必须刷新两类 BIOS 快照：STATIC requirement 重新按当前安装集合求值；Arcade `DAT_MACHINE` 的 `BIOS_OR_BASE` 依赖按快照中的 machine 精确查找同 CoreArtifact 的 `<machine>.zip` active installation。命中 `MATCHED/HASH_WARNING` 后更新依赖状态、从 `missingEntries` 移除该 archive，并把实际 Blob 加入新的不可变 `BIOS_BUNDLE` ValidationFile；只在原阻断确为 `LAUNCH_BIOS_MISSING` 且全部缺项解除时转为 READY。已安装无关 BIOS 不能解除阻断，也不能要求用户重新导入游戏。

## 5. 真实 DAT 基线

| Core | 绑定 source | 文件 | SHA-256 | 解析统计 |
| --- | --- | --- | --- | --- |
| fbneo | `c8c70ba…` | `fbneo/fbneo-arcade.dat` | `99605be7…d73637` | 7,980 machines；13 explicit BIOS |
| mame2003 | `0ee1c0e…` | `mame2003/mame2003.xml` | `dacf9d57…6da147` | 4,727 machines；17 base targets；4.2.1 运行覆盖的 DAT 字节相同 |
| mame2003_plus | `09e84fe…` | `mame2003_plus/mame2003-plus.xml` | `e1107634…d7303c` | 5,257 machines；17 base targets |

FBNeo 与 MAME2003-Plus source commit 按 EmulatorJS v4.2.3 官方 build report 时间推定；MAME2003 commit 来自 core 运行日志内嵌 Git version。manifest 分别记录证据等级，不能把任何一种证据改写成 build report 明示。

## 6. DAT 生命周期

1. 仓库只保存 `data/dat/emulatorjs/<version>/manifest.json` 和 `SHA256SUMS`；真实 DAT payload 由 `make prepare-deps` 按固定配方预下载/生成到同一被 Git 忽略的版本目录。
2. 服务同步启动阶段只校验、不下载；比较 manifest 与 `dat_versions`，并为缺少缓存的内置版本持久化唯一、不可取消的 `DAT_PARSE` Job。首次数据库或 parser version 变化时由 Worker 在事务外安全解析；当前 enabled Arcade artifact 完成前服务 live 但以 `DEPENDENCY_INDEXING` 不 ready，解析成功并短事务发布索引后才允许该内置版本成为 active。
3. 内置 DAT 只有 EmulatorJS version 和实际 Core artifact SHA-256 都匹配 manifest 时才可自动激活。
4. 用户上传时必须选择目标 Core；流式保存为非活动候选并计算 SHA-256。
5. 使用第 7.1 节的 streaming XML parser；用户 DAT 默认上限 64 MiB、500,000 元素、单字段 4 KiB，超限失败。
6. 候选解析成功后自动排队异步差异物化；页面显示排队/运行状态并禁用查看和启用，不让 HTTP GET 承担全量 DAT 扫描。后台以当时 active DAT 为 base，物化 machine/entry/依赖差异、影响摘要和 digest；失败或失效可直接重新生成，无需再次上传 DAT。
7. 用户显式启用前，diff GET 只分页读取 READY 物化结果。BIOS/parent archive 在上传验证时已安全扫描为 ArchiveEntry，因而 preview 和 commit 只比较索引的 entry hash/size，不重读大 Blob。启用请求校验同一 canonical input/impact、CoreArtifact version 与活动指针，短事务内切换 DatVersion、应用 requirement/version/installation 状态结果、写审计并为受影响的稳定 GameVariant 投递可观察 revalidation job。不回写不可变 VariantRevision，也不引入未定义的临时状态。任一启用或回滚会使同 CoreArtifact 其他候选/历史版本的已物化差异全部失效并删除明细；其页面恢复为“重新生成差异”。
8. 重校验完成前旧 current VariantRevision 与旧 DAT/依赖快照仍可启动；成功校验才创建新 VariantRevision 并原子切换 current，失败则旧 current 不变并显示 job blocker。
9. 支持回滚到内置版本或之前成功解析的用户版本。

启动自动激活只发生在目标 CoreArtifact 尚无 active DatVersion 时；已经激活的用户 DAT 不被内置版本覆盖。当前 enabled artifact 的 bootstrap parse 确定性失败时保持 live、以 `DEPENDENCY_DAT_PARSE_FAILED` 不 ready，不能回退到空目录、其他 core 的 DAT 或旧 artifact。同步启动 60 秒预算不包含后台解析，解析使用通用 DAT_PARSE execution deadline 与重启 lease 恢复规则。

文件名相同但 SHA-256 不同必须视为新版本。用户 DAT 能由内置版本 hash/已知 header 证明时为 `MATCHED`；无法证明但结构属于所选 core family 时为 `UNKNOWN`，不能自动替换内置基线。管理员在激活影响预览中勾选“确认未验证兼容性”后才可把它转为 `USER_CONFIRMED` 并激活，审计必须保存原状态、确认值和影响 digest。结构/root/family 明确不符为 `INCOMPATIBLE`，即使确认也不可激活。

DAT 与任务时间字段使用数据模型中唯一命名的 `created_at_ms`、`activated_at_ms`、`parsed_at_ms` 等 Unix 毫秒 INTEGER；不存在另一套 `imported_at_ms` 字段。

## 7. 解析模型

至少解析：

- machine name、description、year、manufacturer。
- `cloneof`、`romof`。
- 显式 BIOS 标记（若格式提供）。
- MAME `biosset` 的 name/description/default，以及 ROM 的可空 `bios` 归属。
- ROM name、size、CRC、SHA-1、status、merge。
- disk/CHD name 与 SHA-1（只作诊断；一期明确不提供 CHD 启动）。

格式差异：

- FBNeo：`isbios="yes"` 可直接识别 BIOS machine。
- MAME 2003 / Plus：`cloneof` 表示 parent；`romof != cloneof` 的目标表示 BIOS/base archive，依赖沿 parent 链传递。
- MAME 2003 / Plus 的真实基线分别有 1,427/3,462 个 `biosset`、213/268 个 default，以及 1,408/3,435 条带 `bios` 的 ROM。某 machine 声明 biosset 时，一期固定选择且只选择唯一 `default="yes"`：无 bios 归属的 ROM 与该 default bios 的 ROM 才进入必需闭包；非默认选项在完整目录显示为可选但不阻断启动。一期不提供 BIOS region/option 选择 UI。缺少 default、存在多个 default 或 ROM 引用未知 bios name 是 parse failure。
- FBNeo 未写 `status` 的 ROM 视为 `GOOD`；三个格式的 `nodump` 都不要求用户提供，`baddump` 按 DAT hash 校验并显示 Warning，而不是静默当成 GOOD。
- 悬空上游关系形成可审计 Warning，不补造 machine，也不使整份 DAT 失败。当前已知 `psarc95` 例外见 DAT 基线。

### 7.1 XML 安全解析算法

三份真实基线都含 DOCTYPE：FBNeo 是外部 PUBLIC 声明，两个 MAME 文件是约 4 KiB 的内部元素/属性声明。因此“拒绝所有 DOCTYPE”不可实施，“交给通用 DTD parser”又会引入 XXE 风险。解析器固定执行：

1. 只接受 UTF-8（可有 BOM）和 XML 1.0；在 64 MiB raw byte 上限内流式读取。除 XML declaration、注释和一个位于 root 前的 DOCTYPE 外，拒绝其他 processing instruction/directive。
2. 用带 quote 状态及内部 `[...]` bracket depth 的专用 scanner 定位完整 DOCTYPE，声明本身上限 64 KiB；忽略其语法内容而不解析 DTD。声明内 case-insensitive 出现 `<!ENTITY`、参数实体 `%`、第二个 DOCTYPE、未闭合 quote/bracket 或声明后还有 directive 均返回 `DAT_UNSAFE_DTD`。FBNeo 的 PUBLIC/SYSTEM URL 只作为被跳过的文本，绝不解析 DNS/发起 I/O。
3. 把 DOCTYPE 从 token stream 移除后交给 Go `encoding/xml.Decoder{Strict:true}`；不设置 `CharsetReader` 或自定义 `Entity`。只允许 XML 五个预定义 entity，未知 entity 失败。禁止 XInclude 语义；普通 namespace 当作不识别结构处理。
4. 同时限制 depth 32、每元素 attribute 64、machine 名/entry 名 1–255 UTF-8 bytes、description/manufacturer/year 4 KiB、总 XML element 500,000，并每次循环检查 context cancellation。根必须精确为 FBNeo `datafile` 或 MAME `mame`，且必须与用户选定 core family 一致。
5. 重复 machine key、重复 `(machine, ROM ordinal/name)`、非固定长度 ASCII hex 的 CRC32/SHA-1（有值时）、负 size，以及非 NODUMP ROM 同时缺 CRC/SHA-1，都是确定性 parse failure。合法 hash 接受 ASCII 大小写并在入表/canonical JSON 前规范为小写。NODUMP ROM 可同时缺两种 hash；FBNeo 的真实基线普遍没有 SHA-1，不得自行伪造。悬空关系为 Warning。clone/romof 图用显式 visited/visiting 集合迭代遍历，cycle 只阻断受影响 machine 并输出排序稳定诊断，不递归至栈溢出。

scanner 与 XML decoder 必须从无文件系统/网络 callback 的 `io.LimitedReader` 工作；不得把用户 DOCTYPE 写到临时 DTD 文件，也不得用正则直接删除跨行内部 subset。内置与用户 DAT 共用相同 parser，防止“预置数据特权路径”掩盖解析差异。

## 8. ROM 匹配

1. 顶层 ROMset ZIP 的 basename 去掉最后一个 `.zip` 后按 ASCII case-insensitive 与 machine name 精确匹配；不做前缀/模糊猜测。每个 Arcade archive 内只接受安全的 flat file entry；目录或 `machine/rom.bin` 结构按 Merged 证据处理而不是拍平成普通 ROMset。
2. 读取 ZIP central directory，不默认解压全部内容；entry logical name 按 ASCII case-insensitive 唯一，碰撞阻断。实际 bytes 的 size 必须匹配；DAT 有 CRC32 时校验 CRC32，有 SHA-1 时物化/流式读取该 entry 并校验 SHA-1，两者都有时两者都必须命中。非 NODUMP 条目至少有一个可校验 hash；不能仅凭 hash 接受错误逻辑文件名。
3. 从目标 machine 沿 `cloneof` 构造无环 parent 链，并把 `romof != cloneof` 的目标加入 BIOS/base archive；每个 archive 名必须来自该 GameContentRevision 的 CONTENT/COMPANION，不得扫描全局无归属 Blob。NODUMP entry 排除；有 bios name 时只保留机器 default bios option。
4. 对 machine 自有非 merge entry，要求在其逻辑 archive 中以 DAT `name` 存在；对 `merge` entry，Split 可由声明 parent archive 中的 `merge_name` 满足。Full Non-Merged 允许目标 CONTENT archive 自身满足整个 parent/BIOS 闭包。主 machine 与 parent 的 entry 必须 name/size/hash 匹配，否则阻断；BIOS/base archive 只要必需 entry 名齐全即可装入，size/hash 全部匹配记 `SATISFIED_BY_CONTENT`/`SATISFIED_EXTERNAL`，存在差异则记 `HASH_WARNING` 并允许 READY/Launch，同时把期望值与实际值带入 Warning。任何外部 archive 仍按其逻辑 machine 名核对。
5. 若 clone 特有 entry 只出现在 parent archive 的子目录/合并结构、所选 machine 没有独立 CONTENT archive，或必须依赖 DAT 闭包之外的同 ZIP 子目录，则确定为 `UNSUPPORTED_MERGED_ROMSET`。如果闭包存在但某个独立 archive/entry 缺失，则是可修复的 `MISSING`，不能误报为 Merged。
6. 为指定 Core 创建/复用稳定 GameVariant，并生成直接引用目标 GameContentRevision 的不可变 GameVariantRevision；保存 DAT version、逐 archive/entry 依赖与诊断快照，不写入 Game 展示字段。

一期 ROMset：

- 支持 Full Non-Merged。
- 支持 Split，包括 parent 链和 BIOS sets。
- 不支持 Merged。
- 不支持 CHD；DAT disk entry 只保留诊断，运行必需 CHD 返回 `UNSUPPORTED_CHD` Blocker。

Full Non-Merged 已包含 parent/BIOS entry 时显示“由游戏文件满足”，不要求重复上传；Split 才查找独立 archive。

### 8.1 V2 完整闭包与审核补充

Arcade 识别从 CONTENT machine 开始，沿每一级 `cloneof` 继续到根 parent，并在每一级把 `romof != cloneof` 的目标加入 `BIOS_OR_BASE`；闭包最大 64 个节点。每个节点显式记录 `kind/machine/requiredBy/depth/expectedLogicalName/state/requiredEntryCount/requiredEntries`，并按 depth、kind、machine 形成 canonical V2 dependency snapshot。自环、`a -> b -> a`、超限或关系目标缺失产生稳定的不兼容结果；历史 V1 snapshot 保持原 bytes，由读取层结合其锁定 DAT 投影为 V2，首次重验证才写 V2。

Full Non-Merged 可以由 CONTENT 满足闭包；Split 的独立 Parent 使用来源快照中的 COMPANION。审核补充只允许 V2 闭包中可修复的 Parent `MISSING/MISMATCH` 节点，BIOS/Base 仍由 BIOS 管理页安装，Merged/CHD/cycle/DAT stale 不生成 `canAttach`。补传 ZIP 必须是单个安全 flat archive：拒绝加密、损坏、路径穿越、绝对路径、控制字符、symlink、目录/子目录 entry、ASCII case-insensitive 名称碰撞和超出统一 ArchiveLimits 的展开量/压缩比。客户端文件名不用于识别；请求 machine 与锁定 DAT 唯一决定期望逻辑名。

Parent 必需 ROM 排除 NODUMP、保留 BADDUMP warning，按 ASCII case-insensitive entry name 精确匹配，size 必须相等；DAT 提供 CRC32/SHA-1 时全部校验。正确 bytes 即使名为 `anything.zip` 也在新快照中绑定为 `<machine>.zip`；同名错误、缺项或 hash 不符为 `REVIEW_PARENT_CONTENT_MISMATCH`。额外不冲突 entry 只进 diagnostics，不能替代缺项。每次接受后必须从 CONTENT 重建并重验完整闭包；补 b 后仍缺 c 时保持 BLOCKED，补齐且 BIOS 满足后才 READY。Launch 继续只使用 selected READY ValidationFiles 生成确定性根级 Parent bundle，补传不改变 Player bundle 协议。

发布后的首次启动可能因当前 BIOS 输入快照与审核期摘要不同而创建后继 VariantRevision。只有后继仍引用同一 GameContentRevision 和同一 DatVersion 时，重校验才继承 current revision 已验证的 `PARENT` VariantFiles 与 `variant_dependencies`，并重新生成 BIOS bundle；不得因摘要归一化丢失 Parent，也不得把旧 DAT 的 Parent 关联带入新 DAT。

## 9. 管理页面

### BIOS 文件

- 导航归入“运行依赖”，BIOS 文件与街机数据目录是两个独立子页。
- “当前游戏库需要”与“完整 BIOS 目录”在客户端切换，不做整页导航；URL 保留范围、关键字、Core 和状态，便于从启动阻断处返回。
- 页首摘要固定展示当前范围、缺失/阻断、需要核对和已就绪；可选文件未安装不计入阻断。
- 先按“需要处理”和“已就绪与可选项”分区，再展示逻辑文件/ROMset、Core、状态和可证明的使用语义。当前列表接口没有使用数量时，不显示虚构计数。
- 支持上传、替换；STATIC 文件 hash 不同明确警告但保留，期望/实际 MD5 直接展示。Arcade `DAT_MACHINE` 没有可信的整个 ZIP 期望 MD5，它以条目级 name/size/CRC/SHA-1 校验为准。
- 已安装的 `DAT_MACHINE` ZIP 文件名可点击；对比弹窗左右列出锁定 DAT 版本要求和安装时落库的实际归档条目 name/size/CRC，并区分 `MATCHED/ALIASED/MISMATCHED/MISSING/EXTRA`。`ALIASED` 只在 size 与 SHA-1（无 SHA-1 时 CRC32）同时命中时成立；弹窗读取持久化 ArchiveEntry，不重新打开或解压 Blob。

### Arcade DAT 版本

- 每个 Core 当前实际生效的版本先用独立卡片展示；候选与历史版本位于下方列表并支持即时搜索、来源、处理状态和稳定快速筛选。
- 上传入口放入右侧 Drawer；只生成候选，解析和 diff 完成后才能显式启用，未选择文件或 CoreArtifact 时不允许提交。
- 展示接口真实提供的 machine/ROM/BIOS 数、来源、兼容状态、EmulatorJS/bundle 版本和更新时间；SHA-256、影响目录和稳定 GameVariant 数只在对应 diff/API 实际返回时显示。
- 差异查看、启用和回滚共用宽对话框，显示当前/目标版本、四类增删改摘要、影响、警告和分页明细，不展示只含 ID 或原始 JSON 的技术详情。
- 解析状态逐字对应 `PENDING/PARSING/READY/FAILED/CANCELLED`；取消仅适用于用户 DAT，CANCELLED 候选不可重试，只能删除后重新上传，避免复活已经终止的 Job 证据。
- 支持启用、回滚及删除未活动且无引用的用户 DAT。

## 10. API

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/v1/admin/bios` | BIOS 状态与筛选 |
| POST | `/api/v1/admin/bios/{requirementId}/installations` | 上传新的 BIOS installation revision |
| GET | `/api/v1/admin/arcade-dats` | Core 独立 DAT 版本 |
| POST | `/api/v1/admin/arcade-dats` | 上传 DAT 候选 |
| GET | `/api/v1/admin/arcade-dats/{id}/diff` | 相对当前 active DAT 分页查看差异、影响和 activation digest |
| POST | `/api/v1/admin/arcade-dats/{id}/activate` | 启用并投递重校验 |
| POST | `/api/v1/admin/arcade-dats/{id}/rollback` | 回滚活动版本 |
| DELETE | `/api/v1/admin/arcade-dats/{id}` | 删除无引用用户版本 |

## 11. 统一验收入口

真实 DAT、core 隔离、用户 DAT 生命周期与升级证据统一执行 [一期项目验收规范](./project-acceptance.md) 的 `ACC-DAT-001`–`ACC-DAT-006`；BIOS 上传、哈希提示和依赖阻断执行 `ACC-BIOS-001`–`ACC-BIOS-002`。本文不再维护重复通过条件。
