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
- BIOS 与 DAT 按 Provider Target declaration管理，不按 PlatformInstance 复制；游戏目录只引用默认 Core，Core binding 决定当前 Target。
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

MelonDS 三项必须全部存在才能得到 READY。它们不进入根 BIOS bundle：Variant dependency snapshot 锁定 installation/version/blob/delivery/path，Launch 创建事务复制到 `launch_external_files`，配置只生成三个受 capability 保护的同源 URL。同一 Requirement 切换 active installation 是显式破坏性边界：事务撤销依赖旧 Installation 的 Launch/Play/Netplay，删除其运行 payload 与存档，再释放旧 Installation Blob；新 Launch 必须先以新 BIOS 生成 READY revision。外部文件不得在仍运行的 Launch 内静默漂移。

### 3.6 Arcade Core

不维护脱离 DAT 的手工固定表。当前 v4.2.3 FBNeo DAT 中实际存在 13 个 `isbios="yes"` machine：

`bubsys`、`cchip`、`decocass`、`isgsm`、`midssio`、`namcoc69`、`namcoc70`、`namcoc75`、`neogeo`、`nmk004`、`pgm`、`skns`、`ym2608`。

MAME 2003 和 MAME 2003-Plus 的旧 List XML 没有显式 `isbios` 属性；当前真实基线各有 17 个由 `romof != cloneof` 推导的 base dependency target。名称、entry 和 hash 必须从活动 DAT 解析，不能复制 FBNeo 列表。

数据库中的 BIOS Requirement 是 Provider Target 内的稳定逻辑安装槽，而不是把某份 DAT entry 复制成永不变化的手工表：静态固件 slot 的 `source_kind=STATIC`，condition/activation 按第 3.8 节；Arcade BIOS/base archive 的 slot 为 `DAT_MACHINE`，logical name 固定 `<machine>.zip`，`catalog_digest` 来自活动 DAT 的规范必需 entry 集，外层 ZIP 本身没有 DAT 规定的唯一 hash。切换 DAT 时按 logical slot upsert/disable 并递增发生变化的 requirement version，旧安装 Blob 不复制；随后针对新 catalog 重验证 active installation。

Provider Target 升级会建立新的 Requirement 槽，不把旧 Target 的 active installation 暗中复制成新安装；既有审计快照继续引用旧槽身份，新 Target 在 BIOS 页明确显示未安装。用户再次选择同一文件安装时 CAS 会按 SHA-256 去重，但会创建归属新 Requirement 的独立 Installation 并重新校验。这样不会把旧 Target 的“已匹配”结论冒充新 Target 的证据，也没有未建模的跨 Target 自动迁移。

### 3.7 dosbox_pure

普通 DOS 游戏没有统一固定 BIOS。可执行程序和目录内容属于 GameFiles，`dosbox.conf`/启动 ZIP 属于 GameVariant 的派生文件；ISO/CUE/IMG/VHD 等磁盘镜像一期不接收，不能因 Core 未来可能支持而展示成已支持 BIOS/内容类型。

### 3.8 条件与 core option 的精确规则

静态目录不能只保存文件名/hash：Gambatte 和 mGBA 的上游都要求 core option 开启后才会使用可选启动 BIOS。每个 Provider Target 的 seed 因此还要写入下表的稳定 `condition_code` 与 canonical `activation_options_json`；它们属于 Requirement/catalog digest，不允许前端按 core display name 特判：

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

primary content 指 GameFiles 中唯一 `CONTENT` 文件；host-console 原 ZIP 已在验证阶段物化成保留真实后缀的唯一可运行 member，因此不能用上传 archive 的 `.zip` 后缀判断。逻辑名按 ASCII lower-case 比较最后一个后缀，不读标题或刮削平台猜类型。FDS/GB/GBC/GBA 按表中后缀判定；一期对 BS-X/Sufami 没有足够可靠的独立 classifier，所以这两项“已安装则对全部 Snes9x Variant 装入并进入 digest、未安装则只在完整目录显示且不产生逐游戏 Warning”，由 core 决定是否实际读取。`GAME_GENIE_ADDON_MODE/MGBA_SGB_MODEL` 一期恒不适用。其余 STATIC requirement 的适用集合与 active installation/status/options 一并进入 `validation_input_digest/dependency_snapshot_json`；不适用项不装入本次 bundle，也不触发本游戏重校验。

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

BIOS 页面默认只统计当前游戏库各 GameVariant 当前 GameVariant 实际引用的 Requirement；核心完整目录中的未使用项标记“未使用”，不进入红色缺失计数。

`MISSING_ENTRY` installation 可以作为用户已上传文件保留并维持 active，方便展示实际缺项和直接替换，但绝不能装入 READY Variant 或 Launch bundle；`INVALID`（损坏、不安全或不可读 archive）在上传时保留审计记录但不能成为 active。DAT_MACHINE 安装必须在数据库写事务外按统一 ZIP 安全限制扫描归档，并把条目 hash 目录持久化；校验范围只含普通条目与默认 BIOS set 的非 NODUMP 条目。文件名不同时，仅当 size 与 DAT SHA-1（缺失时为 CRC32）一致才视为历史别名并记录 Warning；同名但内容不同为 `HASH_WARNING`，内容不存在才是 `MISSING_ENTRY`。静态文件 hash 不匹配也统一为 `HASH_WARNING` 并可进入 Launch bundle。每次状态都记录 `validated_requirement_version`，页面若发现版本不一致显示“待重验证”，不能继续使用旧 MATCHED 标签。

生成初始待审核条目，以及草稿 PATCH、依赖附件或 DAT/BIOS 处理触发当前 Validation 切换时，都必须刷新两类 BIOS 快照：STATIC requirement 重新按当前安装集合求值；Arcade `DAT_MACHINE` 的 `BIOS_OR_BASE` 依赖按快照中的 machine 精确查找同 Provider Target 的 `<machine>.zip` active installation。命中 `MATCHED/HASH_WARNING` 后更新依赖状态、从 `missingEntries` 移除该 archive，并把实际 Blob 加入新的不可变 `BIOS_BUNDLE` ValidationFile；只在原阻断确为 `LAUNCH_BIOS_MISSING` 且全部缺项解除时转为 READY。已经在当前校验开始前安装的 BIOS 不得先误报为缺失；已安装无关 BIOS 不能解除阻断，也不能要求用户重新导入游戏。

## 5. 真实 DAT 基线

| Core | 绑定 source | 文件 | SHA-256 | 解析统计 |
| --- | --- | --- | --- | --- |
| fbneo | `c8c70ba…` | `fbneo/fbneo-arcade.dat` | `99605be7…d73637` | 7,980 machines；13 explicit BIOS |
| mame2003 | `0ee1c0e…` | `mame2003/mame2003.xml` | `dacf9d57…6da147` | 4,727 machines；17 base targets；4.2.1 运行覆盖的 DAT 字节相同 |
| mame2003_plus | `09e84fe…` | `mame2003_plus/mame2003-plus.xml` | `e1107634…d7303c` | 5,257 machines；17 base targets |
| fbalpha2012_cps1 | `d6f5267…` | `fbalpha2012_cps1/fbalpha2012-cps1.dat` | `54a95deb…15b789` | 227 machines；无 BIOS/base target |
| fbalpha2012_cps2 | `3fb5b89…` | `fbalpha2012_cps2/fbalpha2012-cps2.dat` | `f59166db…07f99` | 284 machines；无 BIOS/base target |

FBNeo、MAME2003-Plus 与两个 FBA2012 source commit 按 EmulatorJS v4.2.3 官方 build report 时间推定；MAME2003 commit 来自 core 运行日志内嵌 Git version。manifest 分别记录证据等级，不能把任何一种证据改写成 build report 明示。两个 FBA2012 DAT 从各自锁定源码的生产 driver registry 原生生成两次并要求逐字节相同；CPS-1 的真实 machine 数是 227，CPS-2 仅允许 manifest 明列的一个集合外 parent 规范化。

## 6. DAT 生命周期

1. 仓库只保存 `data/dat/emulatorjs/<version>/manifest.json` 和 `SHA256SUMS`；真实 DAT payload 由 `make prepare-deps` 按固定配方预下载/生成到同一被 Git 忽略的版本目录。
2. 服务同步启动阶段只校验、不下载；按 release manifest 为每个 DAT-capable Provider Target 登记 DatVersion。`builtin_relative_path`、SHA-256 和 parser version 都是非空当前字段，不存在用户来源、上传 Blob 或兼容状态列。若 manifest 选择与数据库当前 active 不同，启动引导先停用旧选择并推进 Provider Target version，使服务保持 not ready，不能在后台索引期间继续使用旧 DAT。
3. 缺少索引时创建唯一、不可取消的 `DAT_PARSE` Job，并在事务外使用第 7.1 节的 streaming XML parser。当前 Arcade Provider Target 完成前服务 live 但以 `DEPENDENCY_INDEXING` not ready；确定性失败时以 `DEPENDENCY_DAT_PARSE_FAILED` not ready，不能回退到空目录、其他 Core 的 DAT 或旧 Target。
4. 只有 manifest 固定的 SHA-256、EmulatorJS version 与实际 Provider Target 均匹配，且解析统计与 manifest 一致的内置 DatVersion 才能在短事务内成为 active。激活同时同步 DAT_MACHINE requirements、写系统审计；已建立正确索引的重复启动只修复 active 指针，不重复解析。
5. 管理员和普通用户都不能上传、创建、比较、启用、回滚或删除 DAT。OpenAPI、HTTP router、数据库 schema 和 Web UI 均不存在用户 DAT、DAT diff 或 base-version 输入分支。
6. DatVersion 身份仍被 Import、GameVariant、ReviewEvent 和 Launch 精确引用；release manifest 升级产生新的内置 DatVersion，成功索引后由启动引导激活，受影响稳定 GameVariant 通过既有版本/输入漂移机制按需重校验。既有 current GameVariant 和 Launch 保留原 DatVersion 与依赖快照，不被静默改写。

同步启动 60 秒预算不包含后台解析，解析使用通用 DAT_PARSE execution deadline 与重启 lease 恢复规则。文件名相同但 SHA-256 不同仍是不同内置版本；同一 `(Provider Target, SHA-256, parser version)` 只能有一条记录。

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

- FBNeo 与两个 FBA2012 family：`isbios="yes"` 可直接识别 BIOS machine；当前两份 FBA2012 基线均没有此标记或 base target，但仍必须按独立 family 校验声明名，不能复用 FBNeo DAT。
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

scanner 与 XML decoder 必须从无文件系统/网络 callback 的 `io.LimitedReader` 工作；不得把 DOCTYPE 写到临时 DTD 文件，也不得用正则直接删除跨行内部 subset。小型项目自有 DAT 继续直接测试同一生产 parser 的正常与恶意输入边界，但不能通过 HTTP 进入运行配置。

## 8. ROM 匹配

1. 顶层 ROMset ZIP 的 basename 去掉最后一个 `.zip` 后按 ASCII case-insensitive 与 machine name 精确匹配；不做前缀/模糊猜测。每个 Arcade archive 内只接受安全的 flat file entry；目录或 `machine/rom.bin` 结构按 Merged 证据处理而不是拍平成普通 ROMset。
2. 读取 ZIP central directory，不默认解压全部内容；entry logical name 按 ASCII case-insensitive 唯一，碰撞阻断。实际 bytes 的 size 必须匹配；DAT 有 CRC32 时校验 CRC32，有 SHA-1 时物化/流式读取该 entry 并校验 SHA-1，两者都有时两者都必须命中。非 NODUMP 条目至少有一个可校验 hash；不能仅凭 hash 接受错误逻辑文件名。
3. 从目标 machine 沿 `cloneof` 构造无环 parent 链，并把 `romof != cloneof` 的目标加入 BIOS/base archive；每个 archive 名必须来自该 GameFiles 的 CONTENT/COMPANION，不得扫描全局无归属 Blob。NODUMP entry 排除；有 bios name 时只保留机器 default bios option。
4. 对 machine 自有非 merge entry，要求在其逻辑 archive 中以 DAT `name` 存在；对 `merge` entry，Split 可由声明 parent archive 中的 `merge_name` 满足。Full Non-Merged 允许目标 CONTENT archive 自身满足整个 parent/BIOS 闭包。主 machine 与 parent 的 entry 必须 name/size/hash 匹配，否则阻断；BIOS/base archive 只要必需 entry 名齐全即可装入，size/hash 全部匹配记 `SATISFIED_BY_CONTENT`/`SATISFIED_EXTERNAL`，存在差异则记 `HASH_WARNING` 并允许 READY/Launch，同时把期望值与实际值带入 Warning。任何外部 archive 仍按其逻辑 machine 名核对。
5. 若 clone 特有 entry 只出现在 parent archive 的子目录/合并结构、所选 machine 没有独立 CONTENT archive，或必须依赖 DAT 闭包之外的同 ZIP 子目录，则确定为 `UNSUPPORTED_MERGED_ROMSET`。如果闭包存在但某个独立 archive/entry 缺失，则是可修复的 `MISSING`，不能误报为 Merged。
6. 为指定 Core 创建/复用稳定 GameVariant，并生成直接引用目标 GameFiles 的不可变 GameVariant；保存 DAT version、逐 archive/entry 依赖与诊断快照，不写入 Game 展示字段。

一期 ROMset：

- 支持 Full Non-Merged。
- 支持 Split，包括 parent 链和 BIOS sets。
- 不支持 Merged。
- 不支持 CHD；DAT disk entry 只保留诊断，运行必需 CHD 返回 `UNSUPPORTED_CHD` Blocker。

Full Non-Merged 已包含 parent/BIOS entry 时显示“由游戏文件满足”，不要求重复上传；Split 才查找独立 archive。

### 8.1 V2 完整闭包与审核补充

Arcade 识别从 CONTENT machine 开始，沿每一级 `cloneof` 继续到根 parent，并在每一级把 `romof != cloneof` 的目标加入 `BIOS_OR_BASE`；闭包最大 64 个节点。每个节点显式记录 `kind/machine/requiredBy/depth/expectedLogicalName/state/requiredEntryCount/requiredEntries`，并按 depth、kind、machine 形成 canonical V2 dependency snapshot。自环、`a -> b -> a`、超限或关系目标缺失产生稳定的不兼容结果；所有 Arcade writer、reader、审核、Launch 与 Netplay 只接受 V2。

Full Non-Merged 可以由 CONTENT 满足闭包；Split 的独立 Parent 使用来源快照中的 COMPANION。审核补充只允许 V2 闭包中可修复的 Parent `MISSING/MISMATCH` 节点，BIOS/Base 仍由 BIOS 管理页安装，Merged/CHD/cycle/DAT stale 不生成 `canAttach`。补传 ZIP 必须是单个安全 archive：拒绝加密、损坏、路径穿越、绝对路径、控制字符、symlink、ASCII case-insensitive 路径碰撞、真正嵌套的 archive 和超出统一 ArchiveLimits 的展开量/压缩比。Parent DAT 只匹配根级 regular-file entry；像 `1944.zip` 这样同时携带根级 parent ROM 与安全 clone 子目录的归档可以保留子目录 bytes 作为原始证据，但子目录 entry 只作为 diagnostics 中的 ignored extra，不能满足缺失的根 entry、参与 Parent 判定或放开 Merged 主 ROMset。客户端文件名不用于识别；请求 machine 与锁定 DAT 唯一决定期望逻辑名。

Parent 必需 ROM 排除 NODUMP、保留 BADDUMP warning，按 ASCII case-insensitive entry name 精确匹配，size 必须相等；DAT 提供 CRC32/SHA-1 时全部校验。正确 bytes 即使名为 `anything.zip` 也在新快照中绑定为 `<machine>.zip`；同名错误、缺项或 hash 不符为 `REVIEW_PARENT_CONTENT_MISMATCH`。额外不冲突 entry 只进 diagnostics，不能替代缺项。每次接受后必须从 CONTENT 重建并重验完整闭包；补 b 后仍缺 c 时保持 BLOCKED，补齐且 BIOS 满足后才 READY。Launch 继续只使用 selected READY ValidationFiles 生成确定性根级 Parent bundle，补传不改变 Player bundle 协议。

发布后的首次启动可能因当前 BIOS 输入快照与审核期摘要不同而更新 GameVariant。只有 GameFiles 和 DatVersion 均未变化时，重校验才保留当前态已验证的 `PARENT` VariantFiles 与 `variant_dependencies`，并重新生成 BIOS bundle；不得因摘要归一化丢失 Parent，也不得把旧 DAT 的 Parent 关联带入新 DAT。

### 8.2 公开自动化回归夹具

[`testdata/public-roms/arcade-smoke/`](../testdata/public-roms/arcade-smoke/) 保存项目自有、MIT 许可且可确定性重建的 MAME2003、MAME2003 Plus、FBNeo 与 FBA2012 CPS1/CPS2 测试程序、小型 DAT 和测试依赖归档。Pac-Man Z80 程序读取 P1/P2 active-low 输入并更新可见 tile；CPS 68000 程序初始化工作 RAM/显示、读取两路输入并写入可验证 marker/palette，音频 CPU 运行确定性静音循环。生成器按锁定 driver layout 固定 archive/entry/name/size/CRC32，并用 SHA-1/SHA-256 锁定完整项目自有 bytes；CRC patch 只位于不执行、不显示的生成器自有窗口。

CPS1 测试 DAT 把 `1941` 表示为无 parent/BIOS 的完整根集合。锁定的 FBA2012 CPS2 core loader 在载入 Phoenix `spf2xjd` 时会按 driver 的 zip-name 链强制打开 `spf2t.zip`，因此测试 DAT 保留真实 `cloneof/romof=spf2t`，并提供只有 `retrom-parent.marker` 的项目自有父归档。该 marker 只让 Retrom 的 Parent 识别、依赖快照、bundle 和核心 loader 开包路径闭环，不冒充或复制 `spf2t` ROM，也不被目标 driver 执行；Launch 必须有 `parentUrl`、不得有 `biosUrl`。

真实 release DAT 的来源、物化、parser stats 与 manifest 精确 active 选择由 `ACC-DAT-001/002/004` 证明。`ACC-RUN-006/007/010/011/012` 与 `ACC-NP-015/016/019/020/021/022` 为了合法执行自制 ROM，由 acceptance-only 装置在临时数据库中把对应小型 DAT 直接登记为 test-only `BUILTIN`；该装置只接受代码内固定的 fixture ID/path/hash/machine allowlist，没有 HTTP/UI 入口，不能在生产构建中调用，也不构成用户 DAT 功能。Case 仍经过真实产品导入、审核 schema v2、发布、受限内容和 Chrome Player；启动前后必须保持同一 DatVersion 和 schema v2。测试 BIOS 与 CPS2 marker parent 不被目标驱动执行，因此只证明 Retrom 的解析、装配、冻结与交付，不证明核心内部 BIOS/parent 程序执行语义；双浏览器 Case 只证明精确 profile 的 lockstep 与 digest 收敛。

## 9. 管理页面

### BIOS 文件

- “运行依赖”是直接进入 `/admin/bios` 的单一导航项，不提供 Arcade DAT 子页。
- “当前游戏库需要”与“完整 BIOS 目录”在客户端切换，不做整页导航；URL 保留范围、关键字、Core 和状态，便于从启动阻断处返回。
- 页首摘要固定展示当前范围、缺失/阻断、需要核对和已就绪；可选文件未安装不计入阻断。
- 先按“需要处理”和“已就绪与可选项”分区，再展示逻辑文件/ROMset、Core、状态和可证明的使用语义。当前列表接口没有使用数量时，不显示虚构计数。
- 支持上传、替换；STATIC 文件 hash 不同明确警告但保留，期望/实际 MD5 直接展示。Arcade `DAT_MACHINE` 没有可信的整个 ZIP 期望 MD5，它以条目级 name/size/CRC/SHA-1 校验为准。
- 已安装的 `DAT_MACHINE` ZIP 文件名可点击；对比弹窗左右列出锁定 DAT 版本要求和安装时落库的实际归档条目 name/size/CRC，并区分 `MATCHED/ALIASED/MISMATCHED/MISSING/EXTRA`。`ALIASED` 只在 size 与 SHA-1（无 SHA-1 时 CRC32）同时命中时成立；弹窗读取持久化 ArchiveEntry，不重新打开或解压 Blob。

- 页面明确说明 Arcade DAT 由 release/core manifest 自动准备。`ARCADE_DAT_UNAVAILABLE` 是部署依赖/Ready 故障，界面提示检查 `make prepare-deps`、服务日志和 `/health/ready`，不引导用户上传另一份目录。

## 10. API

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/v1/admin/bios` | BIOS 状态与筛选 |
| POST | `/api/v1/admin/bios/{requirementId}/installations` | 上传新的 BIOS installation revision |

Arcade DAT 没有管理员 HTTP API；运行时只通过审核、GameVariant、Launch 和 BIOS 内容接口投影锁定的 DatVersion。

## 11. 统一验收入口

真实 DAT、core 隔离、内置-only 迁移/选择和升级证据统一执行 [一期项目验收规范](./project-acceptance.md) 的 `ACC-DAT-001`–`ACC-DAT-006`；BIOS 上传、哈希提示和依赖阻断执行 `ACC-BIOS-001`–`ACC-BIOS-002`，服务器导入执行 `ACC-BIOS-003`–`ACC-BIOS-007`。本文不再维护重复通过条件。

## 12. 服务器目录批量导入

任务创建时直接从数据库冻结当前 Provider catalog 中、被产品 Core binding 引用的全部 enabled Requirement：STATIC 与活动 DAT 的 DAT_MACHINE 均包含，REQUIRED/OPTIONAL/CONDITIONAL 均包含；不在当前 catalog/binding 闭包内的旧 Target、历史 DAT slot 和当前游戏库范围不参与。一个 Blob 可分别满足多个 Requirement，但 Installation 不跨 Requirement 共享。

STATIC 的可信 exact 要求全部已声明 size/hash 同时一致；否则依次按期望 size、精确 basename、较大 size 作低置信度选择，结果保持 `HASH_WARNING`。DAT_MACHINE 只把逻辑 `.zip` 交给全局串行 archive scanner，并优先安全、可启动、matched/aliased 更多且 mismatched/missing 更少的候选；最后以规范相对路径和确定性 ID 稳定排序。只以质量证据比较是否覆盖，身份、文件名或新扫描本身不增加质量。

`replaceIfBetter=false` 保留任何 active Installation；开启后也只允许严格更优，禁止同分、证据不完整或降级替换。相同 bytes 且 Requirement/catalog 未变时保持当前态；Requirement 改变时相同 bytes 仍重新校验。提交前重新检查完整 catalog digest、Requirement/稳定 Provider Target、DAT 和 source bytes；漂移分别以稳定条目结果收口。真正替换时，旧 Installation payload 单向释放并只保留来源审计；依赖它的已物化运行资源和旧 `BIOS_BUNDLE` VariantFile 被清理，活动 Launch/Play/Netplay 终止。SaveState 仍按 Game 保留，其可恢复性由当前 READY Target 的 `readFormats` 决定；受影响 GameVariant 转为需要重新校验，下一次 Launch 先复用或创建异步重校验 Job，并原子更新当前依赖与 VariantFiles。
