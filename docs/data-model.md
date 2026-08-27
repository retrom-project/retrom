# 一期数据库实体与不变量

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期实施基线 |
| 版本 | 1.5 |
| 日期 | 2026-08-25 |
| 数据库 | SQLite，单个 `retrom` 写进程 |

## 1. 标识、版本与删除

- `platforms.id` 和 `cores.id` 是代码种子字符串；账号版本不再 seed `profiles.id='local'`。User 与 Profile 均使用小写 UUIDv7 `TEXT`，一一绑定且创建后不可变；其他业务实体主键同样使用小写 UUIDv7 `TEXT`。
- 管理侧可修改资源至少包含 `version INTEGER NOT NULL DEFAULT 1`；每次成功修改在同一事务 `version = version + 1`。HTTP 使用 `ETag/If-Match`。
- 业务时刻统一为 `*_at_ms INTEGER` Unix 毫秒；除下表明确以 `fetched_at_ms` 等领域时刻表示首次落库时刻外，带独立业务实体 ID 的表有 `created_at_ms`，可修改实体另有 `updated_at_ms`。复合键关系/明细表是否带创建时刻由下表逐一规定，不能从本句推断额外列。禁止 TEXT 时间和 `CURRENT_TIMESTAMP`。
- PlatformInstance 等目录实体继续使用软删除。Game 使用保留墓碑的永久删除：Game、文字 metadata/content revision、ReviewEvent、AuditEvent 与游玩/收藏/联机关系保留，全部游戏内容、媒体、存档和运行 payload 异步释放；被保留的历史 DTO 显示原标题与 `DELETED`，不可再启动或读取内容。
- 枚举在数据库用大写 `TEXT CHECK`；布尔值用 `INTEGER CHECK (x IN (0,1))`。所有外键开启并默认 `ON DELETE RESTRICT`。

### 1.1 账号、凭据与实例状态

| 表 | 必需字段与约束 |
| --- | --- |
| `profiles` | `id PK`、`display_name`、`created_at_ms`；不再有代码 seed。每个 User（包括软删除 User）恰好引用一个不可变 Profile。 |
| `users` | `id/profile_id/username/display_name` 创建后不可变，`profile_id/username` 唯一；另有 `role ADMIN/USER`、`status ENABLED/DISABLED/DELETED`、`session_version/version`、登录与生命周期时刻。Profile `display_name` 是同一创建事务的显示名快照，本版本不提供修改入口。User 只软删除，username 永不复用；username 匹配 `^[a-z][a-z0-9._-]{2,31}$`，并保留 `local/root/system/retrom`。任何事务结束时至少保留一名 `ENABLED+ADMIN`。 |
| `user_credentials` | `user_id PK`、严格 `password_scheme=ARGON2ID_V1`、PHC hash、密码变更/创建时刻；DELETED User 没有 Credential。 |
| `auth_sessions` | 随机 session token 只在 cookie；数据库保存唯一 32-byte SHA-256、User session version、创建/last-seen、8h idle、24h absolute、可空撤销时刻及封闭原因 `LOGOUT/PASSWORD_CHANGED/PASSWORD_RESET/ROLE_CHANGED/USER_DISABLED/USER_DELETED/OFFLINE_RECOVERY/RESTORE/EXPIRED`。 |
| `account_links` | Invitation 与 PasswordReset 共表；只保存公开 ID、kind、邀请 role/目标 User、创建者、1h 到期、消费/撤销元数据和 version，绝不保存 capability secret、URL 或可离线验证的 token hash。消费与撤销互斥且终态不可变。 |
| `instance_state` | 单行 `PENDING/COMPLETED`、bootstrap kind、首位管理员、`test_default_password_active` 与 version/时刻。完成后不可重开；PENDING 必须零 User/零 Profile，COMPLETED 必须至少一名启用管理员。 |
| `auth_rate_limits` | `(scope,subject_hash)` 主键；scope 为 `LOGIN_ACCOUNT/LOGIN_IP/SETUP_IP/LINK_IP`，subject 仅保存实例 HMAC，不保存原用户名/IP/token；窗口、失败次数、阻断和更新时间全部用 Unix 毫秒。 |

User 状态变更、Credential/Session/Link 撤销、待用 Launch 终止与安全审计在同一短事务完成。当前管理员不能降权、停用或删除自己；最后一名启用管理员的降权、停用和删除由 service 与 trigger 双重阻断。`admin-reset` 只接受现有非删除 ADMIN，重置合规密码、重新启用、撤销全部会话并写 SYSTEM 审计。

## 2. 平台、核心与目录

| 表 | 必需字段与约束 |
| --- | --- |
| `profiles` | 账号 Profile，字段与生命周期见 1.1；本节不再定义 seed。 |
| `platforms` | `id PK`、`name`、`sort_order`、`enabled`、`created_at_ms/updated_at_ms`；代码 seed，运行期不创建/删除。 |
| `cores` | `id PK`、`name`、`enabled`、`created_at_ms/updated_at_ms`；既有 35 个 EmulatorJS core 加七个 RPG Maker 版本 core。线程能力属于构件而非产品 core，不再保存 `requires_threads`。 |
| `rpgmaker_core_generations` | `core_id PK/FK`、唯一 `generation RPG2000/RPG2003/RPGXP/RPGVX/RPGVXACE/RPGMV/RPGMZ`；只登记七个内部运行 core，数据库和编译时 registry 必须双向一一对应。用户目录默认 core 是独立的虚拟 `rpgmaker`。 |
| `core_artifacts` | 通用不可变运行时构件：`id PK`、`core_id FK`、非空 `route_key/runtime_family/runtime_adapter_kind/runtime_version/adapter_id/entry_path/size_bytes/manifest_sha256/artifact_set_sha256/sha256/provenance_json/compatibility_json`、`requires_threads`、`save_payload_kind`、`save_max_bytes`、`selected_for_new_bindings/available_for_launch`、`version`、`created_at_ms/updated_at_ms`、可空 `retired_at_ms`；既有 EJS 专用字段只作为 `runtime_family=EMULATORJS` 的 compatibility 投影，不再决定通用语义。family 只允许 `EMULATORJS|RPGMAKER`；kind 只允许 `EMULATORJS|EASYRPG_WEB|MKXP_LIBRETRO_WEB|NATIVE_WEB` 且必须与 family 相容。partial unique 保证 `(core_id,route_key)` 至多一个当前选择项、每个 RPG 版本 core 至多一个当前新绑定项；`selected_for_new_bindings=1` 强制 available 且未 retired。历史 artifact 可停止新绑定但只要仍受 Variant/Save 引用就必须 available，Launch 不得按“当前选择”替换它。 |
| `platform_cores` | `(platform_id, core_id) PK`、`enabled`；默认核心和 Variant 的 core 必须引用 enabled 关系。 |
| `platform_instances` | `id PK`、`platform_id`、`default_core_id`、`name`、规范化 `slug`、`description`、`sort_order`、`enabled`、`version`、`created_at_ms/updated_at_ms`、可空 `deleted_at_ms`、可空 `catalog_template_key`；`UNIQUE(platform_id, slug)`，另以 partial unique index 保证非空 template key 全局唯一。复合 FK 保证默认 core 存在于 PlatformCore，trigger/service 额外保证其 enabled。手动创建永远写 NULL；推荐补齐才写 `<platform_id>/<default_core_id>`，停用或软删除也保留该 key 作为不自动恢复的 tombstone。 |

`slug` 由服务端创建时从展示名生成小写 ASCII 标识，冲突时追加数字后缀，必须匹配 `^[a-z0-9]+(?:-[a-z0-9]+)*$` 且不超过 80 byte；创建后不可改，软删除也不释放。展示名可改。游戏目录的 `platform_id` 创建后不可改。

`core_artifacts.provenance_json` 固定为 `{"schemaVersion":1,"dependencyManifestSha256":"<64 lowercase hex>","manifestEntryPointer":"<manifest JSON pointer>","sourceAssociationStatus":"EXACT_COMMIT|EMBEDDED_VERSION|INFERRED_BUILD_TIME|RELEASE_ONLY","sourceUrl":null|string,"notes":[]}`；EmulatorJS 指针位于 `/emulatorjs/selected_core_artifacts/<index>`，RPG 指针位于 RPG artifact manifest 中对应条目。notes 只存简短证据说明，不存宿主路径。schema 不存在 `source_commit` 列；固定来源身份由不可变 `runtime_version` 与 `provenance_json` 共同表达，不得另增可漂移列。

`runtime_family=EMULATORJS` 的 `compatibility_json` 唯一当前格式为 schema V5：`runtimeCoreId`、`requestedArtifactBasename`、`canvasResizePolicy`、`defaultOptions`、`inputMode`、`startupActions`、`supportedContentKinds` 与可空 `multiDisc` 全部由 dependency manifest schema V7 明示；其他 schema 或未知字段直接拒绝。`inputMode` 只允许 `STANDARD|POINTER`。启动动作最多 4 条，只允许有界的 `GAME_START/PRESS_CONTROL` 整数字段，`delayMs` 上限 30,000、`durationMs` 上限 1,000。basename 是所属 EmulatorJS 版本 loader 实际请求的 key；线程产物也使用其真实 `*-thread-wasm.data` basename。v4.2.3 只有 `mame2003` 使用 `ON_GAME_START_TO_CSS_PIXELS`，NDS 三核心与 Azahar为 POINTER，PPSSPP 有 2 秒/5 秒两条 120ms 确认动作，Beetle VB 的四条确认动作延迟为 2/4/15/25 秒。前后端不得按 core ID 补默认值。RPG Maker artifact 使用第 25 节的 route/adapter/pack 封闭 profile，不伪造 EmulatorJS V5 字段。

## 3. Blob 与不可变游戏 revision

| 表 | 必需字段与约束 |
| --- | --- |
| `blobs` | `id UUIDv7 PK`、唯一小写 `sha256`、`size_bytes`、实际 bytes 的非空小写 `md5/sha1/crc32`、首次入库时检测到的 `media_type`、`created_at_ms`；物理路径只由 SHA-256 推导。CAS 按 bytes 去重，Blob 级 `media_type` 只是首次登记元数据，不能覆盖或否决 `game_assets`、导入媒体记录等引用边已经完成的 MIME/尺寸校验。 |
| `games` | `id PK`、非空 `platform_instance_id`、`status PUBLISHED/DELETED`、`payload_state RETAINED/RELEASING/RELEASED/FAILED`、可空唯一 `payload_release_job_id`、可空 `payload_released_at_ms/payload_last_error_code`、非空 `current_metadata_revision_id/current_content_revision_id/search_text`、`version`、`created_at_ms/updated_at_ms`、可空 `deleted_at_ms`；不得另存 `platform_id` 或 `default_core_id`。PUBLISHED 恰为 RETAINED；DELETED 恰为后三种状态并保留 current revision 作为文字审计。search_text 与 current metadata 必须同事务更新；改变目录默认 core 不改变 current content。 |
| `game_metadata_revisions` | `id PK`、`game_id`、非空 title、`title_initial TEXT NOT NULL`、非空但可为空串的 description/developer/publisher/genre、可空 players/release_year、`source_kind IMPORT_REVIEW/ADMIN_EDIT/RESCRAPE_APPLY/SERVER_PEGASUS_IMPORT/SERVER_EMULATIONSTATION_IMPORT`、可空 `source_ref_id`、`created_at_ms`；创建后 append-only。`title_initial` 严格为 `#`、`0..9` 或 `A..Z`：标题第一个 Unicode code point 是 ASCII 数字时原样保留、ASCII 字母时转大写、汉字时取其普通话拼音首字母并转大写，其他字符为 `#`；不得在查询时依赖 SQLite locale 临时推导。IMPORT_REVIEW 的 ref 必须是发布该 Game 的 ImportItem；两种 SERVER source 的 ref 必须是已交接到该 ImportItem、正处于审核发布边界的对应来源 Item；RESCRAPE_APPLY 必须是被应用且属于该 Game/current ContentRevision 有效 run 的 ScrapeCandidate；ADMIN_EDIT 的 ref 必须为 NULL，精确修改另由同事务 AuditEvent 关联 Game/revision ID。 |
| `game_assets` | `id PK`、`game_id`、`metadata_revision_id`、`blob_id`、`kind COVER/BACKGROUND/SCREENSHOT/VIDEO`、`ordinal`、`width_px/height_px/media_type`、`created_at_ms`；`UNIQUE(metadata_revision_id, kind, ordinal)`。图片尺寸为正且 MIME 限 `image/png|image/jpeg|image/webp`；VIDEO 只允许 ordinal 0、`video/mp4|video/webm` 且尺寸为 null。只有 Game 的 current MetadataRevision 持有完整媒体清单；切换 current 时先为未变媒体创建新 Asset，再删除全部旧 revision 的 Asset 叶子引用并交给统一 GC。MetadataRevision 的文字/来源审计继续保留。Asset ID 存续期间 bytes 不变，但内容端点只接受 current Asset。 |
| `game_content_revisions` | `id PK`、`game_id`、`content_kind SINGLE_FILE/DOS_BUNDLE/MULTI_DISC_M3U_V1/RPG_MAKER_PROJECT_V1/ONS_PROJECT_V1`、`source_kind IMPORT_REVIEW/ADMIN_REPLACE/SERVER_PEGASUS_IMPORT/SERVER_EMULATIONSTATION_IMPORT`、非空 `source_ref_id`、`source_manifest_json`、`source_manifest_digest`、`created_at_ms`；append-only。IMPORT_REVIEW ref 指向被 Approve 的 ImportItem，两种 SERVER source 的 ref 指向与该 ImportItem 一一关联的对应来源 Item，ADMIN_REPLACE ref 指向 `GAME_FILE_REVISION` Job。它只表示一次已接受的用户内容版本，不包含 core、DAT 或派生启动包；单 ROM bytes 相同，或多盘按盘序得到的全部 Disc hash 相同，必须以终态 `GAME_CONTENT_UNCHANGED` 拒绝，不能创建空审计 revision。 |
| `game_content_files` | `(game_content_revision_id, role, logical_name) PK`、`blob_id`、可空 `source_archive_blob_id/source_archive_entry_ordinal`、`sort_order`；两个 source archive 字段同时空或同时非空，并复合引用对应 ArchiveEntry，其 `materialized_blob_id` 必须等于 `blob_id`。role 只允许 `CONTENT/DOS_SOURCE/COMPANION/PROJECT_FILE`；RPG 项目逐文件使用 `PROJECT_FILE`，其 V2 fileset 不内联文件数组。逻辑名是安全规范相对路径；主机/掌机平台只允许一个 CONTENT，DOS 使用 DOS_SOURCE，Arcade CONTENT 是本机 ROMset ZIP。 |
| `game_variants` | `id PK`、`game_id`、`core_id`、可空 `current_revision_id`、`version`、`created_at_ms/updated_at_ms`；`UNIQUE(game_id, core_id)`，表示稳定逻辑槽，不承载可变文件。只有从未产生 READY 结果、仅保存失败验证证据的备用 core 槽允许 current 为空；发布所用默认 core 槽必须非空。 |
| `game_variant_revisions` | `id PK`、`game_variant_id`、非空 `game_content_revision_id/core_artifact_id/route_key`、可空 `dat_version_id`、`validation_input_digest`、可空 `emulator_game_id INTEGER UNIQUE`、`status READY/BLOCKED/INCOMPATIBLE`、`compatibility_code`、`dependency_snapshot_json`、可空 `default_dos_entry`、`created_at_ms`；`UNIQUE(game_variant_id,validation_input_digest)`，完成后 append-only。route 必须与 artifact 相等。READY EJS revision 必须有正 `emulator_game_id`；READY RPG revision 必须为 NULL 并有一对一 `rpgmaker_variant_profiles`。历史 READY 是否已替代只由 current pointer 推导。 |
| `variant_files` | `(game_variant_revision_id, role, logical_name) PK`、`blob_id`、`sort_order`；只保存 core-specific/派生文件，role 只允许 `PARENT/BIOS_BUNDLE/DOS_LAUNCH_BUNDLE/RPG_EASYRPG_INDEX/RPG_MAKER_LAUNCH_BUNDLE`；用户原始内容只能从 `game_content_files` 读取。 |
| `dos_entries` | `(game_content_revision_id, normalized_path) PK`、`original_relative_path`、`kind EXE/COM/BAT`、`rank`、`enabled/direct_launch_safe`；路径大小写比较采用 ASCII case-insensitive，冲突在导入时阻断。`direct_launch_safe=1` 表示安全扫描后可写入确定性 `dosbox.conf [autoexec]`，不将宿主命令或未验证字符串直接拼入。 |
| `archive_entries` | `(archive_blob_id, ordinal) PK`、`original_relative_path/normalized_path/ascii_casefold_path`、`archive_format ZIP/SEVEN_Z`、`compression_profile STORE/DEFLATE/SEVEN_Z_DECODER_VALIDATED`、`uncompressed_size_bytes`、`crc32/md5/sha1/sha256`、可空 `materialized_blob_id`、`created_at_ms`；`UNIQUE(archive_blob_id, normalized_path)` 与 `UNIQUE(archive_blob_id, ascii_casefold_path)`。路径使用存储专题 `SAFE_LOGICAL_PATH_V1`；casefold 只映射 ASCII A-Z。只记录经过安全扫描且非目录、非加密、非 symlink/device 的 regular-file entry；ZIP 只允许 Store/Deflate，7z 只接受隔离 worker 完整读取校验的 profile。扫描时对实际解压 bytes 计算四种 hash，但只在领域需要独立 member 时物化到 CAS。除 `materialized_blob_id` 可以在校验 Blob size/四种 hash 全部等于该 entry 后一次性从 NULL 提升为该 Blob ID 外，其他字段永不可更新。常规 CRUD 永不允许删除；唯一例外是存储专题定义的 owner-GC。 |

审核发布在一个事务创建 GameContentRevision、默认核心的 READY VariantRevision，并闭合 Game/Variant 两个 current pointer。Archive member 物化先在事务外流式写 CAS，再以 `materialized_blob_id IS NULL` 为条件的短事务验证 size/CRC32/MD5/SHA-1/SHA-256 并提升；并发已提升时必须逐项等于同一 Blob，否则为完整性故障。崩溃留下的无引用物化 Blob 交给 GC，不在事务内重解压。管理侧替换游戏文件先创建 `GAME_FILE_REVISION` Job，并以 whole-session consumption 独占 Upload；Worker 在事务外完成归档/格式/依赖验证。单 ROM identity 按内容文件 role/hash 序列比较，多盘 identity 按规范盘序的 Disc hash 序列比较；与 current 完全相同时 Job 以不可重试的 `GAME_CONTENT_UNCHANGED` 结束并释放 consumption。只有不同的新内容对任务快照中的目录默认 core 验证 READY，且提交时 Game current content、目录/default core/version 仍等于快照，才在一个短事务创建新的 GameContentRevision/ContentFiles/VariantRevision、切换 `games.current_content_revision_id` 与目标 `game_variants.current_revision_id`，随后删除所有非 current ContentFile/VariantFile、旧 Launch payload 和绑定旧 VariantRevision 的 SaveState，并撤销仍运行的旧 Launch/Play/Netplay；旧 Content/Variant revision 行只保留文字、manifest/hash 和结构化审计。配置或内容基线变化使 Job 以 retryable conflict 失败，手动重试会显式刷新快照；其他失败同样不创建 revision、不改变 current、也不删除存档，其中仍可能重试的 Job/Upload consumption 保留为输入证据。其他 core 槽仍保留旧 revision 行，但因 content revision 不匹配而显示 `NEEDS_VALIDATION`。BLOCKED/INCOMPATIBLE VariantRevision 只用于既有 GameContentRevision 在备用 core 或新依赖配置下的幂等诊断，不能成为 current。存档、Launch、PlaySession 都引用精确 READY VariantRevision ID；它再唯一确定 GameContentRevision。术语中的“GameVariant revision”一律指该实体，不是可变行上的整数。

`game_content_revisions.source_manifest_json` 对全部 `content_kind` 统一使用 RFC 8785 canonical 紧凑 V2 对象 `{"schemaVersion":2,"contentKind":"<content_kind>","fileCount":<u32>,"totalBytes":<u64>,"filesDigest":"<64 lowercase hex>"}`；精确文件留在 `game_content_files`，不得内联文件数组。`source_manifest_digest` 是该 canonical JSON bytes 的 lowercase hex SHA-256，只标识清单；`filesDigest` 独立标识文件集。`RETROM_FILESET_V1` 算法固定为：将文件按 `(role UTF-8 bytes,logical_name UTF-8 bytes)` 升序，从 ASCII `RETROM_FILESET_V1` + `0x00` 开始，对每条连续写 `u32be(role byte length)+role+u32be(logical_name byte length)+logical_name+32-byte raw blob SHA-256+u64be(size)+u8(sourceArchivePresent)`；存在来源归档时再写 `32-byte raw source archive SHA-256+u64be(entry ordinal)`，最后对全部 bytes 做 SHA-256。生成器拒绝非 UTF-8、空逻辑名、重复 `(role,logical_name)`、非 32-byte 小写 hash、负 size/ordinal 和整数溢出；role 使用对应内容类型在 `game_content_files` 中允许的闭集，不含运行时生成的 BIOS/parent/launch bundle。`games.current_content_revision_id` 是普通启动唯一的 canonical source lineage；改变目录默认 core、活动 DAT 或 artifact 不能隐式改变它。core 的 READY 结果必须直接引用该 ContentRevision；引用其他内容版本的旧 current 不可用于普通启动。

`validation_input_digest` 是下列 RFC 8785 canonical object 的 lowercase hex SHA-256：GameContentRevision 的 id 与 `sourceManifestDigest`；CoreArtifact 的 id/version/SHA-256；可空 DatVersion 的 id/version/SHA-256；按 requirement ID UTF-8 byte 排序的“对该 content 适用或按 BIOS 专题规定已安装即装入”的 BIOS requirement id/version/logicalName/catalog digest/activation options，与当时 active installation 的可空 Blob SHA-256/status/version；按逻辑 archive 名排序的 companion/parent source Blob SHA-256；以及 `validatorVersion`。它不包含线程、全屏等客户端能力，也不把明确不适用的静态可选固件混入。同一内容 revision + 依赖输入的并发验证依靠唯一约束只保存一个结果；新的 ContentRevision（即使 bytes 相同）、适用 BIOS 安装、活动 DAT、artifact 或验证算法变化都会产生新 digest，允许重新验证而不覆盖旧证据。READY revision 的 `dependency_snapshot_json` 必须足以按这些值重建当时 bundle与合并后的 core options；活动配置后来变化不使旧 READY 自动失效。

`search_text` 的生成算法固定为：按字段指定顺序用一个 U+0020 连接，执行 Go `strings.ToLower`，把每段连续 `unicode.IsSpace` 折叠为单个 U+0020，再 trim；不执行语言相关 collation、拼音、分词或 SQLite `NOCASE` 猜测。Game 字段顺序为 current title/developer/publisher/genre；ImportItem 为候选 title 与 source relative paths。query `q` 用同一算法后以 `instr(search_text, :q) > 0` 匹配，空 query 不加条件。未来升级正规化算法必须新增 schema migration 重建两列和 cursor contract，不能只改 Go 函数。

`games ↔ game_metadata_revisions`、`games ↔ game_content_revisions` 的循环外键，以及 GameVariant 非空 current pointer 的同槽引用，必须声明 `DEFERRABLE INITIALLY DEFERRED`，让创建/切换事务能先插任一侧并在 COMMIT 前闭合；Game 不得把 current pointer 暂时设 NULL。GameVariant 仅允许在“尚无 READY 的备用 core 槽”持续为 NULL，不能在替换已有 READY 时先清空。`emulator_game_id` 只为 READY revision 分配 `1..9007199254740991` 的整数，在单写事务中取现有最大值 + 1；达到上限必须拒绝创建而非溢出。不可变 READY revision 不删除，因此不复用；API 以 JSON number 发送，使 `EJS_gameID` 保持 number 类型。

一期不支持 Arcade CHD/disk 内容：DAT 可以解析 `disk` 元素作为诊断证据，但导入发现运行所需 CHD 时返回 `UNSUPPORTED_CHD`，不创建假装可运行的 VariantRevision。Merged ROMset 同样返回 `UNSUPPORTED_MERGED_ROMSET`；只支持 Split 与 Full Non-Merged。

## 4. BIOS 与 DAT

| 表 | 必需字段与约束 |
| --- | --- |
| `bios_requirements` | `id PK`、`core_id/core_artifact_id`、`source_kind STATIC/DAT_MACHINE`、可空 `dat_machine_name`、`logical_name`、`requirement_mode REQUIRED/OPTIONAL/CONDITIONAL`、可空 `condition_code`、可空 `activation_options_json`、`delivery_kind BIOS_BUNDLE/EXTERNAL_FILE`、可空 `emulator_path`、`catalog_digest`、可空期望 `size_bytes/md5/sha1/sha256`、`source_url/source_version`、`enabled`、`version`、`created_at_ms/updated_at_ms`；`UNIQUE(core_artifact_id, logical_name)`，复合 FK 保证 artifact 属于同 core。BIOS_BUNDLE 的 path 必须为空，EXTERNAL_FILE 必须是规范绝对虚拟路径；交付方式和路径都进入 catalog/validation digest。STATIC 必须无 dat machine；DAT_MACHINE 固定为 bundle。release manifest 的内置 DAT 选择变化时按 slot upsert/disable，并在 digest 改变时递增 version，不删除历史 slot。 |
| `bios_installations` | `id PK`、`requirement_id`、可空 `blob_id`、`original_filename`、实际 hash、`validated_requirement_version`、`status MATCHED/HASH_WARNING/MISSING_ENTRY/INVALID`、`validation_details_json`、`is_active`、`version`、`created_at_ms/updated_at_ms`、可空 `payload_released_at_ms`；同 requirement 只允许一条 active，active 必须有 Blob，`blob_id IS NULL` 与已记录 release time 等价且不可恢复。错误静态 hash 可 active 为 HASH_WARNING；可读 Arcade archive 若必需 entry 名齐全但 size/hash 有差异，同样 active 为 HASH_WARNING 并允许装入；完全缺少任一必需 entry 才是可 active 但阻断启动的 MISSING_ENTRY；损坏/不安全 archive 为 INVALID 且不可 active。Requirement version 改变时由 release 引导在发布新 active DAT 前完成有界重验证，并更新 status/version。 |
| `dat_versions` | `id PK`、`core_id/core_artifact_id`、非空 `builtin_relative_path`、`sha256`、`parser_version`、`parse_status PENDING/PARSING/READY/FAILED/CANCELLED`、`is_active`、可空解析统计、`version`、`created_at_ms/updated_at_ms`、可空 `parsed_at_ms/activated_at_ms`；每个 core artifact 只允许一条 active，`UNIQUE(core_artifact_id, sha256, parser_version)`。记录只由已校验 release manifest 创建；只有 READY 可由启动引导激活。schema 不包含用户来源、上传 Blob、diff 或常量兼容状态。 |
| `dat_machines` | `(dat_version_id, machine_name) PK`、description/year/manufacturer、cloneof/romof、explicit BIOS flag、`classification NORMAL/EXPLICIT_BIOS/ROMOF_INFERENCE`。 |
| `dat_bios_sets` | `(dat_version_id, machine_name, bios_name) PK`、`description`、`is_default`；同 machine 至多一个 default。MAME 实际 BIOS option 在此保存；FBNeo 当前为零行。 |
| `dat_rom_entries` | `(dat_version_id, machine_name, ordinal) PK`、`name/size_bytes`、可空 `crc32/sha1/status/merge_name/bios_name`；bios_name 非空时必须命中同 machine BiosSet；索引 machine 与非空 hash。现实 FBNeo 条目没有 SHA-1，NODUMP 条目还可同时没有 CRC/SHA-1，不得用 NOT NULL 或伪哈希填充；除 NODUMP 外至少必须有一个 CRC32/SHA-1。未声明 status 的 FBNeo entry 规范为 GOOD，NODUMP 不进入必需闭包，BADDUMP 进入闭包但生成 Warning。 |
| `dat_disk_entries` | `(dat_version_id, machine_name, ordinal) PK`、name、可空 SHA1/status；非 NODUMP disk 必须有 SHA-1，NODUMP 可空。一期只诊断，不表示 CHD 可运行。 |
| `variant_dependencies` | `(game_variant_revision_id, kind, logical_archive) PK`、`dat_version_id`、`kind PARENT/BIOS_OR_BASE`、`source_machine_name`、`required_entries_json`、`state SATISFIED_BY_CONTENT/SATISFIED_EXTERNAL/HASH_WARNING/MISSING/MISMATCH/UNSUPPORTED`、`created_at_ms`；append-only。DatVersion 必须等于 VariantRevision 锁定值，logical archive 固定为 `<source_machine_name>.zip`。`HASH_WARNING` 只允许 `BIOS_OR_BASE` 且所有必需 entry 名存在、至少一项 size/hash 不匹配；它可出现在 READY revision 并装入 bundle。`MISMATCH` 用于 parent 等必须精确匹配的内容并阻断 READY。 |

DatVersion 只读取 dependency manifest 物化且逐字节校验的只读 payload，不进入 CAS；解析只使用通用 `jobs` 与 DatVersion 状态。release 切换 active DAT 只影响后续验证结果/新 VariantRevision，不改写既有依赖快照。

BIOS Installation 不跨 CoreArtifact 自动复制：新 artifact 的 Requirement 初始没有 active installation；用户可再次选择同一 UploadFile/bytes 安装，CAS Blob 复用但 Installation 行与校验证据独立。不同 Requirement 的旧 Installation 继续独立存在；同一 Requirement 的 active Installation 被真正替换时，旧行保留结构化审计但清空 Blob，依赖旧快照的存档、运行 payload 与 `BIOS_BUNDLE` VariantFile 载荷同事务失效。对应 VariantRevision 仍保留依赖快照审计；下一次 Launch 通过 validation digest 漂移触发新 BIOS 的异步重校验。

DAT parse 状态投影固定为：Job 首次领取时 `PENDING→PARSING`；Job SUCCEEDED 时同一最终事务 `PARSING→READY`；Job 最终 FAILED 时转 `FAILED`。内置 `DAT_PARSE` 不可取消；依赖准备或启动引导会为尚未 READY 的 manifest 版本幂等补建/恢复任务。Worker 解析时允许在 DatVersion 仍为 PARSING 的前提下，每批最多 1,000 个规范行以短事务幂等插入索引表；Requirement 构建一律只读取 READY 版本。相同 bytes/parser 的恢复遇到同主键时必须逐字段相等，否则以确定性 `DAT_NONDETERMINISTIC_PARSE` 失败。全部输入读完后先在事务外计算统计，再在最终短事务校验统计、发布 READY，并由引导逻辑把该 artifact 在 manifest 中选定的版本设为 active；不得把数十万索引行塞进一个长事务或先激活再解析。

`bios_requirements.catalog_digest` 是 lowercase SHA-256(RFC 8785 canonical JSON)。STATIC object 含 source kind、logical name、mode/condition、delivery kind、emulator path、canonical activation options、期望 size/hash 和 source version；DAT_MACHINE object 含 DatVersion SHA-256、machine name，以及按 `(entryName UTF-8 bytes,size,crc32,sha1)` 排序的必需非 NODUMP/default-bios entry。排序中可空 hash 以 JSON `null` 排在非空小写 hex 之前；同一 entry 的 canonical object 始终显式写 `crc32`/`sha1` 字段及其 `null`，不因上游缺少字段而改变 schema。`activation_options_json` 为可空 RFC 8785 object，最多 8 个 ASCII core-option key，每个值是最多 128 bytes 的 ASCII string；不能嵌套。适用的多个 requirement 按 ID 排序合并，重复 key 必须同值，否则 seed/parser 校验失败，不允许后写覆盖。`bios_installations.validation_details_json` 固定为 `{"schemaVersion":1,"missingEntries":[],"mismatchedEntries":[],"warnings":[]}`，entry 数组按逻辑名排序且只含 name/expected/actual hash/size，不含宿主路径。manifest 选择、DatVersion SHA-256、CoreArtifact version 与 requirement catalog digest 共同形成后续 validation 输入，避免 release 切换后复用旧结果。

`variant_dependencies.required_entries_json` 固定为 `{"schemaVersion":1,"entries":[...]}`，entries 按 `(name UTF-8 bytes,sizeBytes,crc32,sha1)` 升序，可空 hash 使用上述 `null` 排序规则。每项始终显式含 `name/sizeBytes/crc32/sha1/datStatus`（hash 可为 JSON `null`）、`resolution=CONTENT|EXTERNAL|MISSING|MISMATCH|UNSUPPORTED`、可空 `actualSizeBytes/actualCrc32/actualSha1/sourceBlobSha256`。entry 级 `MISMATCH` 在 `BIOS_OR_BASE` aggregate 中归并为 `HASH_WARNING`，在 `PARENT` aggregate 中仍归并为阻断性的 `MISMATCH`；缺名始终归并为 `MISSING`。不存宿主路径，不把 NODUMP 写成必需项；因此真正进入此数组的 entry 至少有 CRC32 或 SHA-1。

## 5. 上传、导入与审核

| 表 | 必需字段与约束 |
| --- | --- |
| `upload_sessions` | `id PK`、`state CREATED/UPLOADING/FINALIZING/COMPLETE/FAILED/CANCELLED/EXPIRED`、`source_type FILES/DIRECTORY`、`total_files/total_bytes`、`manifest_digest`、`finalization_no`、可空 `finalize_job_id UNIQUE`、`version`、`expires_at_ms/created_at_ms/updated_at_ms`、可空 `unconsumed_pruned_at_ms/last_error_code`。`finalization_no` 初始为 0，每次接受 `POST complete` 的事务恰好加 1；manifest digest 是创建时对 source type 与按规范相对路径排序的 fileId/path/declared size 做 RFC 8785 SHA-256，之后不变。FINALIZING 必须引用 kind=`UPLOAD_FINALIZE`、同 scope 且 input `finalizationNo` 等于当前计数的 Job；COMPLETE 时所有 UploadFile 必须 COMPLETE。旧 finalization Job/事件永久保留，但 session 只指向当前一次。 |
| `upload_files` | `id PK`、`upload_session_id`、规范 `relative_path`、`declared_size_bytes/received_size_bytes`、可空 `final_blob_id`、`state PENDING/PARTIAL/FINALIZING/COMPLETE/FAILED/PURGED`、可空 `payload_released_at_ms/last_error_code`、`created_at_ms/updated_at_ms`；同 session 规范路径唯一。只有 COMPLETE 有 final Blob 且没有释放时刻；PURGED 必须清空 final Blob 并写释放时刻，路径和大小审计继续保留。 |
| `upload_parts` | `(upload_file_id, part_no) PK`、`offset_bytes/size_bytes/sha256`、相对于 `RETROM_DATA_DIR/tmp/uploads/` 的规范 `storage_key`、`created_at_ms`；key 满足 `SAFE_LOGICAL_PATH_V1`、不得包含 `tmp/uploads/` 前缀，且数据库唯一；partNo 从 0 连续，声明范围不重叠，完成后才要求无缺口。 |
| `upload_consumptions` | `id PK`、`upload_session_id`、可空 `upload_file_id`、`consumer_type IMPORT_JOB/GAME_FILE_REVISION_JOB/GAME_ASSET/REVIEW_ASSET/REVIEW_ARCADE_PARENT/REVIEW_MULTI_DISC/BIOS_INSTALLATION`、`consumer_id`、`version`、可空成对的 `released_at_ms/release_reason`、`created_at_ms`；`UNIQUE(consumer_type, consumer_id)`，并为活动的 whole-session consumption 建互斥索引。每个消费在所属流程终态时单向 released；同 UploadFile 的其他活动 consumption 继续保护 bytes，最后一个消费释放且不存在领域叶子时 UploadFile 才转 PURGED。BIOS 安装属于全局耐久引用，不由流程释放器越权回收。 |
| `import_jobs` | `id PK`、`upload_session_id UNIQUE`、`target_platform_instance_id/platform_instance_version/platform_id/default_core_id/core_artifact_id`、可空 `dat_version_id`、`metadata_provider HASHEOUS/NONE`、`config_snapshot_json/config_snapshot_digest`、`state`、全部分类计数、可空自引用 `reconfigured_from_import_job_id`、可空 `last_error_code/cancel_requested_at_ms/cancel_reason`、`version`、`created_at_ms/updated_at_ms/completed_at_ms`，以及 `payload_state RETAINED/RELEASING/RELEASED/FAILED`、可空唯一 `payload_release_job_id`、可空 `payload_released_at_ms/payload_last_error_code`。item 分类计数总和必须等于 total；`completed_at_ms` 仅在 `COMPLETED/CANCELLED/FAILED` 非空。只有这三个真终态是 payload 释放边界；`PARTIAL_FAILURE` 仍可重试且必须保持 RETAINED。进入终态与创建父 `PAYLOAD_RELEASE(IMPORT_JOB)` 同事务；父 Job 收敛所有终态子 Item，再释放 aggregate UploadConsumption。重新配置的新 UploadSession 继续单独保护复用 Blob。 |
| `import_job_files` | `(import_job_id, upload_file_id) PK`、`disposition PENDING/SOURCE/IGNORED/REJECTED`、可空 `reason_code`、`created_at_ms/updated_at_ms`。创建 Import 时为 session 每个 UploadFile 建 PENDING 行；分组后每个文件必须有唯一终态 disposition。SOURCE 必须至少被一条 ItemSourceFile 引用且 reason 为空；IGNORED/REJECTED 必须有稳定 reason，并在任务页可见，不允许静默丢文件。 |
| `import_job_file_resolutions` | `(import_job_id, upload_file_id) PK/FK ImportJobFile`、`action RECONFIGURED`、`replacement_import_job_id FK ImportJob`、`actor_kind USER/SYSTEM`、可空 `actor_user_id/actor_label`、`created_at_ms`，append-only。USER actor 必须引用真实用户，SYSTEM actor 只能使用封闭 label；它保留原 REJECTED disposition/reason，同时证明该文件已由哪个新任务接管。只有新 UploadSession、replacement ImportJob、全部 resolution 与 source 聚合计数在同一创建事务成功时才算已处理。 |
| `import_items` | `id PK`、`import_job_id/group_key/state/source_manifest_json/source_manifest_digest/search_text`、不可变 `review_handoff_kind DIRECT/EMULATIONSTATION`、可空 `failed_stage/last_error_code/completed_at_ms`、`version`、时刻，以及 `payload_state RETAINED/RELEASING/RELEASED/FAILED`、可空唯一 `payload_release_job_id`、可空 `payload_released_at_ms/payload_last_error_code`；同 job/group key 唯一。普通创建使用 `DIRECT`；EmulationStation 的幂等内部创建必须在创建 Item 的同一事务写入 `EMULATIONSTATION` 预留，不能在稍后 attach 时补写。`PUBLISHED/DISCARDED/FAILED_FINAL/CANCELLED` 是 Item 释放边界并与创建唯一 Job 同事务；`FAILED_RETRYABLE` 继续 RETAINED。文字 manifest、hash 摘要、状态、错误与审核事件保留，来源/快照/校验/预览/审核媒体叶子在 RELEASED 后不可访问。 |
| `import_item_source_files` | `(import_item_id, role, logical_name) PK`、`upload_file_id`、`blob_id`、可空 `source_archive_blob_id/source_archive_entry_ordinal`、`sort_order`、`created_at_ms`。role 仅 `CONTENT/DOS_SOURCE/COMPANION/PROJECT_FILE`；RPG 项目的每个规范文件都使用 `PROJECT_FILE`，不内联进 compact manifest。archive pair 同时空或非空，非空时必须指向该 UploadFile final Blob 的 ArchiveEntry 且其 materialized Blob 等于 `blob_id`；为空时 blob 必须等于 UploadFile final Blob。同一 UploadFile 可作为多个 Arcade Item 的 COMPANION，所以不对 upload_file_id 建唯一约束。Approve 逐行复制为 GameContentFile。 |
| `content_identity_claims` | `(platform_id, content_identity_digest) PK`、`created_at_ms`，append-only。digest 为基础平台加 source file `(role, blob SHA-256, occurrence count)` 规范集合的 lowercase SHA-256；不含文件名、UploadSession、archive wrapper 或 PlatformInstance。Approve 在写事务中先插入/命中 claim，再查询当前发布 Game 并决定冲突或发布，以序列化同一内容身份的并发决策。 |
| `import_item_duplicate_matches` | `(import_item_id, existing_game_id) PK`、`existing_game_content_revision_id`、`content_identity_digest`、`detected_stage=IDENTIFICATION`、`created_at_ms`，append-only。只记录识别阶段因当前未删除 Game 已使用完全相同内容而自动跳过的匹配，保留命中时 Game current ContentRevision；Item 必须转 `DISCARDED`，且不能创建 ReviewDraft。 |
| `import_item_dos_entries` | `(import_item_id, normalized_path) PK`、`original_relative_path`、`kind EXE/COM/BAT`、`rank`、`enabled/direct_launch_safe`、`created_at_ms`；ASCII case-insensitive 路径冲突阻断。Approve 时逐项复制到新 ContentRevision 的 `dos_entries`；ReviewDraft default 只能指向本 Item enabled entry。 |
| `import_item_core_validations` | `id PK`、`import_item_id/source_snapshot_id/target_platform_instance_id`、目录 `platform_instance_version`、`core_id/core_artifact_id`、可空 `dat_version_id/default_dos_entry`、`source_manifest_digest/prepublish_input_digest`、`status READY/BLOCKED/INCOMPATIBLE`、`compatibility_code/dependency_snapshot_json`、`created_at_ms`；`UNIQUE(import_item_id, prepublish_input_digest)`，append-only。source snapshot 必须属于同 Item，digest 必须与其 canonical manifest 一致；digest 使用 source manifest、目标目录/version、artifact/DAT、BIOS/companion、可空 default DOS entry 和 validator version 的 canonical SHA-256。它是发布前证据，不冒充最终 GameVariantRevision 的 `validation_input_digest`。 |
| `import_item_validation_files` | `(import_item_core_validation_id, role, logical_name) PK`、`blob_id/sort_order`、`created_at_ms`；role 仅 `PARENT/BIOS_BUNDLE/DOS_LAUNCH_BUNDLE`，保存审核前已完成的确定性派生/依赖 Blob。只有 READY validation 可被 ReviewDraft 选择；普通 Approve 复制所选 READY 引用，截图人工放行则复制当前阻断 Validation 中实际存在的引用，不在事务里重新打包。 |
| `metadata_scrape_runs` | `id PK`、恰一非空的 `import_item_id/game_id`、可空 `game_content_revision_id`、`job_id UNIQUE`、`provider HASHEOUS/NONE`、`provider_config_version`、`state RUNNING/COMPLETED/FAILED/CANCELLED`、`version`、`created_at_ms/updated_at_ms`、可空 `completed_at_ms/error_code`。`completed_at_ms` 在 `COMPLETED/FAILED/CANCELLED` 非空、在 `RUNNING` 为空；只有 FAILED 可有 `error_code`。Game owner 必须同时引用创建 run 时该 Game 的 ContentRevision，ImportItem owner 必须令 content revision 为空。每个 ImportItem 都创建独立 run；支持精确 hash 的内容创建零到多条 evidence，DOS、Arcade 无 eligible primary entry 或 provider=NONE 可合法为零。每个 run 都有 kind=`METADATA_SCRAPE` 的 Job；零 evidence 或 NONE 不发 provider 请求并在同一事务把 Job/Run 置为 SUCCEEDED/COMPLETED，NONE 也不得有 evidence/attempt/response/candidate。游戏重新刮削只允许 HASHEOUS；MISS 与完成了有界错误证据的 run 也是 COMPLETED，只有无法闭合领域证据的本地/Job 故障才为 FAILED。CANCELLED 的领域投影按导入专题区分父 Import 取消、初始 Run 单独取消和后续重刮削，不能把 ImportItem 留在 SCRAPING。 |
| `content_hash_evidence` | `id PK`、`scrape_run_id`、`profile RAW_FILE_V1/SINGLE_ARCHIVE_MEMBER_V1/ARCADE_DAT_ENTRIES_V1`、可释放的来源 `blob_id` 或 `(archive_blob_id, archive_entry_ordinal)`、`payload_released_at_ms`、可空 `crc32/md5/sha1/sha256`、`query_order`、`created_at_ms`；未释放时来源恰一，释放后来源三字段全空且完成时刻非空；hash 摘要始终至少一个非空并永久保留。 |
| `metadata_provider_cache` | `(provider, request_digest) PK`、`current_response_id`、`expires_at_ms/updated_at_ms`；只作可变缓存指针，current response 必须具有相同 provider/request digest。 |
| `metadata_provider_responses` | `id PK`、provider、request digest、可空 HTTP status、`outcome HIT/MISS/RATE_LIMITED/TIMEOUT/INVALID_RESPONSE/NETWORK_ERROR`、`raw_payload_state NONE/RETAINED/RELEASED`、可空 `raw_response_blob_id/raw_payload_released_at_ms`、`fetched_at_ms/expires_at_ms`。过期且没有 RUNNING ScrapeRun 使用时先删除 cache pointer、释放 raw Blob；状态、请求摘要、结果与时刻永久保留。 |
| `metadata_scrape_query_attempts` | `id PK`、`scrape_run_id/content_hash_evidence_id/provider_response_id`、`attempt_no`、`source NETWORK/CACHE`、`created_at_ms`；`UNIQUE(content_hash_evidence_id, attempt_no)`。Evidence 必须属于同 run，response provider/request digest 必须与 evidence 规范请求一致；CACHE 固定 attempt 1，NETWORK 从 1 连续。MISS/错误也必须留下 attempt，从而不能因没有 Candidate 丢失 run→response 证据。 |
| `scrape_candidates` | `id PK`、`scrape_run_id`、`primary_response_id`、`provider_game_id`、`normalized_metadata_json`、`evidence_json`、`created_at_ms`；不可变，`UNIQUE(scrape_run_id, provider_game_id)`。同一 provider game ID 多次命中时，在 run 查询收集完成后按 `(evidence.query_order, attempt.attempt_no, response.id)` 升序的第一个合法 HIT response 固定为 primary，不受 Worker 并发完成顺序影响；文本和媒体都只从 primary 归一化。game/ROM 两个上游 score 只按导入专题写在封闭的 evidence JSON，不再复制一个含义不明且可能漂移的 `provider_score` 列。JSON schema 与字段映射以导入专题 `BY_HASH_V1` 为准。 |
| `scrape_candidate_hits` | `(scrape_candidate_id, query_attempt_id) PK`、`matched_hashes_json`、`created_at_ms`；记录 Arcade 多 entry 或多个 hash 对同一候选的全部命中；attempt 必须是同 run 的有效 HIT，provider response 由 attempt 唯一推导，不能再复制一个可能不一致的 query_order。 |
| `scrape_candidate_assets` | `id PK`、`scrape_candidate_id`、`provider_response_id`、`provider_asset_id`、`kind_hint COVER/BACKGROUND/SCREENSHOT/UNKNOWN`、`ordinal`、`source_path`、`status PENDING/FETCHING/READY/FAILED/CANCELLED`、可空 `blob_id/width_px/height_px/media_type/error_code/fetched_at_ms`、`version`、`created_at_ms/updated_at_ms`；`UNIQUE(scrape_candidate_id, provider_asset_id)`。source_path 只能是已校验的 Hasheous 相对 image path；READY 必须有合法 Blob/dimensions/media type，FAILED/CANCELLED 必须有 error，其他状态两者均空。 |
| `review_uploaded_assets` | `id PK`、`import_item_id/upload_file_id UNIQUE/blob_id`、`kind COVER`、`width_px/height_px/media_type`、`created_at_ms`；不可变。每个 UploadFile 最多生成一份审核资源，并通过 `REVIEW_ASSET` consumption 留下上传归属。仅允许 COMPLETE 上传中的 ≤10 MiB、≤40 MP PNG/JPEG/WebP；资源在 Apply 前可以只作为对比窗体暂存，不暗中改变草稿。 |
| `review_drafts` | `id PK`、`import_item_id UNIQUE/target_platform_instance_id`、非空 `effective_source_snapshot_id`、可空 `selected_validation_id/selected_candidate_id/cover_candidate_asset_id/cover_uploaded_asset_id/background_candidate_asset_id/default_dos_entry`、完整 `metadata_json`、`version`、`created_at_ms/updated_at_ms`；有效来源快照必须属于同 Item。候选封面和人工封面互斥，人工封面必须属于本 Item。仅在 Item 未最终决策时可改。metadata_json 固定为 title/description/developer/publisher/genre/players/releaseYear 的完整 object；不保存含义不明的“只改字段”JSON。首个 Metadata Run 按导入专题的固定 candidate/basename 规则创建且只创建一次；selected validation 必须 READY、属于同 Item/有效来源快照且精确匹配目录当前默认 core/config，default DOS entry 必须属于 Item。Approve 另要求 title trim 后 1–200 Unicode code points且无控制字符。 |
| `review_draft_screenshot_assets` | `(review_draft_id, ordinal) PK`、`candidate_asset_id`、`created_at_ms`；`UNIQUE(review_draft_id, candidate_asset_id)`，ordinal `0..31` 连续。cover/background/screenshot 可来自同一 Item 任意 COMPLETED HASHEOUS run 的 READY asset，允许人工混合媒体来源；selected candidate 只说明文本元信息来源。 |
| `review_events` | `id PK`、`import_item_id`、封闭 `event_type`、`actor_kind USER/SYSTEM`、可空 `actor_user_id/actor_label`、`before_json/after_json/diff_json/config_snapshot_json/dat_evidence_json/provider_evidence_json`、可空 `reason`、`created_at_ms`；append-only。全部 JSON 固定 `schemaVersion=2`，只保存文字和结构化决策（标题/字段/标签、candidate/validation/run/Attachment 等审计 ID、结论和稳定错误码），数据库 trigger 拒绝 asset/blob/upload ID、URL/路径/hash/MIME/尺寸以及配置/依赖原文等 CAS payload 定位信息。审核历史不保存封面或视频，不提供历史媒体回退。 |

本节通用表中的“阻断 Validation + 第 5 秒截图”替代审批和 `REVIEW_SCREENSHOT_OVERRIDE` 仅适用于非 RPG Maker 条目。RPG Approve 必须按第 25 节在当前 `runtime_binding_revision` 找到已分配原始 `launch_id` 的 runtime validation 并逐字段重算绑定；通用截图句子不得用于 RPG trigger 或发布事务。详细机器 gate 与 PASSED 状态是可选高级验证和自动化验收证据，不是人工发布门槛。

状态枚举固定为：

- Upload：`CREATED | UPLOADING | FINALIZING | COMPLETE | FAILED | CANCELLED | EXPIRED`；
- ImportJob：`QUEUED | RUNNING | REVIEW_PENDING | PARTIAL_FAILURE | COMPLETED | CANCEL_REQUESTED | CANCELLED | FAILED`；
- ImportItem：`QUEUED | HASHING | IDENTIFYING | SCRAPING | REVIEW_PENDING | PUBLISHED | DISCARDED | FAILED_RETRYABLE | FAILED_FINAL | CANCELLED`。

ImportJob 聚合规则按固定优先级执行：首次领取前为 `QUEUED`；有 queued/running Item 为 `RUNNING`；无运行项但 `failed_item_count>0` 或 `rejected_file_count-resolved_rejected_file_count>0` 为 `PARTIAL_FAILURE`；仅有待审核时为 `REVIEW_PENDING`；全部 Item 为 PUBLISHED/DISCARDED/CANCELLED 且没有未处理 rejected file 时才为 `COMPLETED`；任务级不可恢复故障才是 `FAILED`。显式取消的短事务写 cancel 字段，把 QUEUED/FAILED_RETRYABLE/REVIEW_PENDING Item 转 CANCELLED，并向运行中的通用 Job 请求取消：没有运行项时直接 `CANCELLED`，否则先 `CANCEL_REQUESTED`，最后一个 Worker 确认停止后才为 `CANCELLED`。一旦 cancel 字段存在，后续聚合不得落到 COMPLETED/PARTIAL_FAILURE；已 PUBLISHED/DISCARDED/FAILED_FINAL 的 Item 与 REJECTED 文件证据保持不变。只有 ignored sidecar 不会单独使 Job 失败；若没有任何 Item 且存在未处理 rejected file，Job 为 PARTIAL_FAILURE；全部拒绝文件已转入 replacement ImportJob 后原任务可 COMPLETED，但详情仍展示原 reason 与 replacement 链接。Approve 接受与目录当前 version/default CoreArtifact/DAT/BIOS input 完全匹配的 READY ImportItemCoreValidation，或同一当前来源/目标/CoreArtifact/generation 的阻断 Validation 第 5 秒截图；事务创建 Game/metadata/content/default-core READY VariantRevision、复制实际已有的 ValidationFiles/全部引用和 ReviewEvent，人工放行另记录 `REVIEW_SCREENSHOT_OVERRIDE` 与 screenshot ID。审批事务不能做 archive/ZIP/DAT 计算。Discard 不删除证据。

`POST complete` 只在短事务内递增 `finalization_no`、冻结该次 part 输入、创建新的 `UPLOAD_FINALIZE` Job、更新 `finalize_job_id` 并转 FINALIZING；Worker 在事务外按 file/partNo 顺序流式校验无缺口与重叠、组装、从实际 bytes 计算四种 hash 并原子发布 CAS。每个文件成功后在短事务转 COMPLETE，再删除该文件的临时 part bytes/行；全部成功才使 session COMPLETE。同一次 finalization 的可重试 I/O 错误由该 Job 自动/人工 retry（增加 execution）并跳过已 COMPLETE 文件；确定性缺失/损坏 part 使当前 Job、文件和 session FAILED。客户端只可重传错误明细列出的 part；重传使 session 回到 UPLOADING，之后以新 Idempotency-Key 再次 complete 会递增 `finalization_no` 并创建另一 Job，即使修复后的 part hash 与旧声明相同也不能复活或改写旧 Job。已 COMPLETE 文件不重组装。取消 FINALIZING 先使当前 Job 进入 CANCEL_REQUESTED，session 仍为 FINALIZING 且 API 暴露 `cancelRequested=true`；Worker 至少每处理 8 MiB 检查一次，在停止并完成 scratch 清理的短事务内才把 Job/session 置 CANCELLED。已发布但未被引用的 Blob 由 GC 回收，不能把“已请求取消”谎报成文件已清除。

`import_jobs.config_snapshot_json` 固定为 `{"schemaVersion":1,"platformInstance":{"id":"...","version":1,"platformId":"...","defaultCoreId":"..."},"coreArtifact":{"id":"...","version":1,"sha256":"..."},"datVersion":null|{"id":"...","version":1,"sha256":"..."},"metadataProvider":{"code":"HASHEOUS|NONE","configVersion":1},"biosCatalogVersion":1,"biosInputs":[]}`。Job 创建时把目标 artifact 的全部 enabled Requirement 拍入 `biosInputs`，按 requirement ID 排序，含 id/version/logicalName/mode/condition/catalog digest/activation options 与 active installation 的可空 id/version/blob SHA/status；它是完整冻结输入，不是假称每项都适用。每个 `import_item_core_validations.prepublish_input_digest/dependency_snapshot_json` 再按实际 primary content 使用与 GameVariant 相同的规则筛选该快照；任务期间 BIOS 改变不会让旧 Job 漂移，审核前的配置过期检查会要求新验证。`config_snapshot_digest` 是 Job-level RFC 8785 object 的 lowercase SHA-256；`biosCatalogVersion` 是静态 condition 算法/seed 的单调整数，任何规则变化必须升级并使在途 Job 显式过期。`import_items.group_key` 是 lowercase SHA-256(RFC 8785 canonical `{"schemaVersion":1,"platformId":"...","primaryUploadFileIds":[...],"logicalRoot":"..."}`)；ID 按 UTF-8 byte 排序，logicalRoot 为规范最小共同目录，单文件为其父目录或空串。同 Job retry 必须生成同一 key。

`POST /import-items/{id}/retry` 由 `failed_stage` 唯一分派：HASHING/IDENTIFYING 对既有 IMPORT_ITEM_PIPELINE Job 增加 execution，Item 回 QUEUED；SCRAPING 不重试不可变的旧 MetadataScrapeRun/Job，而是按原 ImportJob 的 provider/config snapshot 创建新 Run/Job并把 Item 置 SCRAPING。两种路径都在同一事务清空 failed 字段/`completed_at_ms`、递增 version 和追加事件；FAILED_FINAL 不可重试。不能仅凭“最近一个 Job”猜失败阶段。

Upload 清理规则固定为：未完成 session 在 24 小时后进入 EXPIRED；清理器不与持有有效 lease 的 FINALIZING Job 竞态，过期时先取消 Job 再清理 part。COMPLETE 且无 consumption 的 session 保留 7 天供创建消费者，随后进入 EXPIRED 并移除 UploadFile 对 Blob 的引用。whole-session consumption 保留该 session 的全部 UploadFile 作为 Import/替换 Job 证据；只有 file-level consumption 时，后台在 24 小时后删除未被任何 consumption 引用的 UploadFile 行/Blob 引用并设置 `unconsumed_pruned_at_ms`，但保留 session、被消费文件和审计关系。物理 Blob 是否删除仍统一交给引用扫描 GC。

### 5.1 审核来源快照与 Arcade Parent Attachment

| 表 | 必需字段与约束 |
| --- | --- |
| `import_item_source_snapshots` | `id PK`、`import_item_id/revision_no`、`content_kind SINGLE_FILE/DOS_BUNDLE/MULTI_DISC_M3U_V1/RPG_MAKER_PROJECT_V1/ONS_PROJECT_V1`、`source_manifest_json/source_manifest_digest`、`created_by IDENTIFICATION/ARCADE_PARENT_ATTACHMENT/MULTI_DISC_ATTACHMENT`、`created_at_ms`；同 Item revision 唯一，append-only。首个 snapshot 由当前识别路径直接创建。 |
| `import_item_source_snapshot_files` | `(source_snapshot_id,role,logical_name) PK`、`upload_file_id/blob_id`、可空 archive pair、连续 `sort_order`、`created_at_ms`；归属与 Blob/ArchiveEntry 约束等同初始 SourceFile，append-only。Blob registry 把这些行计作永久引用。 |
| `review_arcade_parent_attachments` | `id PK`、Item/Draft/base snapshot/machine/expected logical name/requiredBy/depth/CoreArtifact/DatVersion/UploadFile/原文件名/Job、状态、可空 accepted Blob/result snapshot/observed hash-size/error/finished time、diagnostics、version/time；状态仅 `QUEUED/RUNNING/ACCEPTED/REJECTED/FAILED_RETRYABLE/CANCELLED`，终态字段与转移由 trigger 约束。每 Item 对 `QUEUED/RUNNING` 建 partial unique；ACCEPTED 必须同时有 Blob、后继快照、observed 值和 `REVIEW_ARCADE_PARENT` consumption，其他终态不能伪造后继快照。 |

`import_item_core_validations.source_snapshot_id` 与 `review_drafts.effective_source_snapshot_id` 均非空；`REVIEW_ARCADE_PARENT_VALIDATE→IMPORT_ITEM` 属于当前 Job kind/scope 闭集。Validation/SourceSnapshot/SourceSnapshotFile 均有 update/delete 阻断；GameContentRevision 从审核来源发布时，其 source digest 必须等于草稿有效快照 digest，不能忽略补充 Parent 后发布 child-only 内容。

## 6. Launch、存档与游玩时长

| 表 | 必需字段与约束 |
| --- | --- |
| `launch_sessions` | `id PK`、非空 `profile_id`、`purpose PRODUCT/RPG_RUNTIME_VALIDATION`、可空 `game_id/game_content_revision_id/game_variant_revision_id/rpgmaker_runtime_validation_id/effective_source_snapshot_id`、非空 `core_artifact_id/route_key/return_to`、可空 `save_state_id/dos_entry_path`、`initial_disc_index`、32-byte `credential_sha256 BLOB`、`state CREATED/ACTIVE/FINISHED/EXPIRED/REVOKED` 与既有全部时刻/version；绝不保存明文 capability。PRODUCT 要求 game/content revision/variant 且 validation 为空；RPG validation 要求 validation/effective snapshot 且 game/content/variant/save 为空。两者使用同一 config/content/Player/checkpoint 链。 |
| `launch_content_files` | `(launch_session_id,logical_name) PK`、`blob_id`、`format_version SOURCE_V1/RETROM_DOS_DIRECT_ZIP_V1/RETROM_MULTIDISC_M3U_V1/RPG_MAKER_PROJECT_V1`、`created_at_ms`，保存每个 Launch 的不可变运行内容锁定。单文件、DOS 与多盘虚拟包各恰一条；EasyRPG/mkxp RPG Launch 锁定适配器所需项目与派生文件，Native Web Launch 则从完整 source snapshot 只锁定 `index.html` 和固定 Web MIME allowlist，根 `package.json` 与 native executable 后缀仍留在 source snapshot/filesDigest、但不进入运行内容；`logical_name` 始终保持规范项目相对路径。DOS 的程序菜单/直接启动都锁定已有规范 bundle Blob 并使用 `RETROM_DOS_DIRECT_ZIP_V1`，entry 只在 `launch_sessions.dos_entry_path` 锁定。内容端点按该 entry 生成带受控 `AUTOBOOT.DBP`（直接启动）或 `DOSBOX.BAT`（程序菜单）的 seekable ZIP 虚拟视图；高位 byte 路径只在该视图中把所选入口的对应路径组件确定性映射为无碰撞 ASCII 名称，并同步改写受影响的 local/central name 与后续 local offset，不创建或改写 Blob、临时文件和数据库记录。源包的受控引导保留名不能覆盖生成文件；当前 V1 虚拟视图不能降为 SOURCE。后续 Variant cache 或内容 current 变化不能令本次 URL 漂移。 |
| `launch_external_files` | `(launch_session_id, virtual_path) PK`、`logical_name`、`blob_id`、`created_at_ms`，并对 `(launch_session_id, logical_name)` 唯一；append-only。只保存 Variant 依赖快照中 `EXTERNAL_FILE` 的已验证 Blob，当前用于 MelonDS 三个 BIOS；活动安装切换不能改变已创建 Launch。 |
| `play_sessions` | `id PK`、`launch_session_id UNIQUE`、profile/game/variant revision、`started_at_ms/last_heartbeat_at_ms`、可空 `ended_at_ms`、`active_duration_ms`、`last_client_sequence`、`state ACTIVE/FINISHED/ABANDONED`、`version`、`created_at_ms/updated_at_ms`。 |
| `play_session_events` | `(play_session_id, client_sequence) PK`、`event_kind START/HEARTBEAT/FINISH`、`client_observed_at_ms/server_received_at_ms`、`running/visible/paused` 布尔、`accepted_duration_ms`、`created_at_ms`；append-only，用于幂等与审计。START 固定 sequence 0/accepted 0；其余必须连续。 |
| `save_states` | `id PK`、`profile_id/game_id/game_content_revision_id/game_variant_revision_id/core_artifact_id/adapter_abi`、可空 `dat_version_id/dos_entry_path/disc_index`、`payload_blob_id/payload_sha256/payload_size_bytes`、`payload_kind RUNTIME_STATE/NATIVE_SAVE_BUNDLE_V1/ONS_SAVE_BUNDLE_V1`、可空 `native_profile EASYRPG_V1/RPGMV_V1/RPGMZ_V1` 与 `resume_slot`、非空 `dependency_snapshot_sha256/source_launch_session_id`、可空 `screenshot_blob_id`、`name/active_duration_ms/version` 与时刻/软删除字段；payload 与记录在同一事务创建，截图若提交也在该事务绑定，但 PRODUCT 保存不强制截图。`RUNTIME_STATE` 与 `ONS_SAVE_BUNDLE_V1` 禁止 native 字段；native bundle 要求 profile 和正 resume slot。trigger 校验 owner、Game/ContentRevision/VariantRevision/Artifact/DAT/DOS/Disc、adapter ABI 与 dependency snapshot 全部等于来源 Launch；恢复必须精确匹配，不得 fallback。 |

heartbeat 带单调 `clientSequence` 和上一个区间的 `running/visible/paused` 状态；服务端只接受连续新序号，重复序号返回原结果，跳号返回冲突。单次可计入 delta 上限 45 秒；页面隐藏、模拟器暂停/未启动、超出 45 秒失联段计 0。异常关闭由最后一次已接受 heartbeat 截断。

所有私有表（LaunchSession、PlaySession、SaveState 及其派生读取）都以认证 Principal 的 `profile_id` 参与 SQL predicate；管理员没有 owner bypass。创建 Launch 时把 Profile 固化到不可变 LaunchSession，后续 runtime play/save 只能从该 Launch 派生，客户端不得提交或覆盖 owner。私有 cursor 与 Idempotency-Key 同样绑定当前 User；跨账号复用必须返回不可用/404，而不能命中另一账号的数据或重放结果。

## 7. 通用任务、事件与审计

| 表 | 必需字段与约束 |
| --- | --- |
| `jobs` | `id PK`、`scope_type/scope_id`、`kind/dedupe_key`、`execution_no`、`payload_json`、`cancellable`、`state QUEUED/RUNNING/CANCEL_REQUESTED/SUCCEEDED/FAILED/CANCELLED`、`attempt_count/max_attempts`、`version`、`available_at_ms`、可空 `execution_started_at_ms/execution_deadline_at_ms/leased_until_ms/heartbeat_at_ms/finished_at_ms/worker_id/error_code/error_retryable/cancel_requested_at_ms/cancel_reason`、`created_at_ms/updated_at_ms`；`error_retryable` 可空或布尔。`UNIQUE(kind, dedupe_key)`，可领取索引 `(state, available_at_ms)`。第一次领取某 execution 时原子写 started/deadline；自动 attempt/退避共享该 deadline，人工 retry 增加 execution 并重新置空二者。CANCEL_REQUESTED 不是终态：保留当前 lease，不能被普通 worker 重新领取，原 worker 在有界检查点停止后才转 CANCELLED；若 lease 到期，恢复器只能执行取消确认/清理，不能继续领域计算。只有终态可有 finished；只有 CANCEL_REQUESTED/CANCELLED 可有 cancel request 字段。kind/scope 映射严格固定为 `UPLOAD_FINALIZE→UPLOAD_SESSION`、`IMPORT_GROUP→IMPORT_JOB`、`IMPORT_ITEM_PIPELINE→IMPORT_ITEM`、`REVIEW_ARCADE_PARENT_VALIDATE→IMPORT_ITEM`、`DAT_PARSE→DAT_VERSION`、`VARIANT_REVALIDATE→GAME_VARIANT`、`METADATA_SCRAPE→SCRAPE_RUN`、`MEDIA_FETCH→CANDIDATE_ASSET`、`GAME_FILE_REVISION→GAME`、`BLOB_GC→BLOB`、`UPLOAD_CLEANUP→UPLOAD_SESSION`、`REVIEW_BULK_APPROVE→REVIEW_BULK_APPROVAL`、`RUNTIME_ASSET_PACK_VALIDATE→RUNTIME_ASSET_PACK_INSTALLATION`、`SERVER_EMULATIONSTATION_SCAN|SERVER_EMULATIONSTATION_IMPORT→EMULATIONSTATION_IMPORT`。IMPORT_GROUP 负责安全扫描/分组，并在每个 Item 创建事务中同时创建其 IMPORT_ITEM_PIPELINE Job。新的领域动作生成不透明 execution ID 作为新 Job dedupe key 的一部分；通用人工 retry 不改已存 Job/dedupe key，而是 `execution_no+1`、新建 InputSnapshot、重置 attempt 并追加事件。自动 retry 只增 attempt，不换 input snapshot。`METADATA_SCRAPE` 不允许通用 Job retry：一个 run 是不可混入新证据的用户批次，人工重试必须通过 review/game 领域端点创建新 Run/Job；Worker 只能在该 Job 最终失败前做有界自动 attempt。`REVIEW_ARCADE_PARENT_VALIDATE` 的 FAILED_RETRYABLE 允许通用 retry 复用同一 UploadFile，不能要求重新上传 bytes；`REVIEW_BULK_APPROVE` 只允许快速审批领域按冻结 aggregate retry。 |
| `job_input_snapshots` | `(job_id, execution_no) PK`、`input_json/input_digest`、`created_at_ms`；append-only。execution 从 1 连续，`jobs.execution_no` 必须指向最新行。input 是下文固定的 canonical envelope，digest 为 lowercase SHA-256；自动 attempt 不新建行。 |
| `job_events` | 递增 `id INTEGER PK`、`job_id`、冗余且校验一致的 `scope_type/scope_id`、`event_type QUEUED/STARTED/PROGRESS/RETRY_SCHEDULED/CANCEL_REQUESTED/MANUAL_RETRY/SUCCEEDED/FAILED/CANCELLED`、`data_json`、`created_at_ms`；SSE 按 scope 过滤并用该全局稳定 event ID resume。 |
| `idempotency_records` | `(principal_id,operation_id,key) PK`、`request_digest`、`http_status`、`response_headers_json`、`response_body BLOB`、`created_at_ms/expires_at_ms`；登录写入的 `principal_id` 为 User ID，系统维护命名空间为 `SYSTEM`。response body 是最大 1 MiB 的 SQLite BLOB，24h 后可清理。摘要绑定 principal User ID 但不得包含密码、session/CSRF 或 account-link capability；不得保存 Set-Cookie，Launch cookie 由本机 key 按 launchId 重派生。 |
| `audit_events` | `id UUIDv7 PK`、`actor_kind USER/SYSTEM`、可空 `actor_user_id/actor_label`、封闭 `action`、`resource_type/resource_id`、可空 `before_json/after_json/diff_json/request_id`、`created_at_ms`；append-only。USER actor 以外键引用永不硬删除的 User，SYSTEM actor 只能使用 `release-setup/offline-recovery/startup-test-bootstrap/restore-security-fence`；二者恰一成立。账户动作包括初始化、邀请、密码重置创建/消费/撤销、角色/状态/删除、改密、离线恢复与恢复围栏；普通登录/退出不写审计事件。JSON 禁止宿主绝对路径、IP、hash、cookie/capability 或 key material。 |
| `blob_gc_candidates` | `blob_id PK`、`first_unreferenced_at_ms/scheduled_at_ms`、可空 `deleted_at_ms/last_failed_at_ms/error_code`、`attempt_count`。每次扫描若恢复引用则删除 candidate 行但不删 Blob；仅连续无引用超过宽限后才删物理 bytes 和 Blob 行。 |
| `schema_migrations` | migration version PK、name、checksum、applied_at_ms；checksum 改变即启动失败。 |

当前 clean schema 在上述通用表上进一步固定 `PAYLOAD_RELEASE→IMPORT_ITEM|IMPORT_JOB|PEGASUS_IMPORT_ITEM|EMULATIONSTATION_IMPORT_ITEM|UPLOAD_CONSUMPTION|GAME`，以及 `BLOB_GC→BLOB`；二者均 `cancellable=0/max_attempts=4`。`PAYLOAD_RELEASE` 的 dedupe 输入只含 scope type/id，`BLOB_GC` 绑定 Blob ID、SHA-256 与首次失去引用时刻。release/GC 的 SYSTEM 审计 actor 固定为 `payload-release-worker`，不是可配置自由文本。

`jobs.payload_json` 只是可领取队列指针，固定为 `{"schemaVersion":1,"inputExecutionNo":<positive integer>}`；不存宿主路径、Blob bytes、cookie 或可变业务副本。`job_input_snapshots.input_json` 通用外形为 `{"schemaVersion":1,"kind":"<JobKind>","scope":{"type":"<ScopeType>","id":"..."},"executionId":"<UUIDv7>","inputs":{...}}`。人工 retry 总是创建新的 execution/InputSnapshot 供审计，但只有 GAME_FILE_REVISION 按其领域规则刷新 Game/目录/依赖配置快照；UPLOAD_FINALIZE、IMPORT_GROUP、IMPORT_ITEM_PIPELINE、DAT_PARSE、VARIANT_REVALIDATE、MEDIA_FETCH、BLOB_GC 和 UPLOAD_CLEANUP 都保留原领域输入/digest，只更新 envelope executionId 与该 kind 明定的资源 version。依赖输入已变化而应重新验证时必须创建语义上的新 Job/dedupe key，不能借 retry 偷换。inputs 仅允许以下键，不在表内堆大清单：

- `UPLOAD_FINALIZE`：`uploadVersion/manifestDigest/finalizationNo/finalizationInputDigest`，最后一项对按 fileId/partNo 排序的 offset/size/part SHA-256 做 canonical digest；
- `IMPORT_GROUP`：`uploadSessionId/uploadVersion/manifestDigest/importConfigSnapshotDigest`；`IMPORT_ITEM_PIPELINE`：`importItemVersion/sourceManifestDigest/importConfigSnapshotDigest`；
- `DAT_PARSE`：`datVersion/version/datSha256/parserVersion/baseDatVersionId`；`datSha256` 对 BUILTIN 是已校验只读 payload；`baseDatVersionId` 仅用于兼容旧快照，新任务固定为空；
- `VARIANT_REVALIDATE`：`gameVariantId/gameContentRevisionId/coreArtifactId/datVersionId/validationInputDigest`；`GAME_FILE_REVISION`：`gameVersion/baseContentRevisionId/uploadSessionId/platformInstanceId/platformInstanceVersion/coreArtifactId/datVersionId/configSnapshotDigest`；
- `METADATA_SCRAPE`：`scrapeRunId/provider/providerConfigVersion/ownerVersion/evidenceSetDigest`；`MEDIA_FETCH`：`candidateAssetId/assetVersion/providerResponseId/sourcePathDigest`；
- `BLOB_GC`：`blobId/blobSha256/firstUnreferencedAtMs`；`UPLOAD_CLEANUP`：`uploadId/uploadVersion/expiresAtMs`。
- `PAYLOAD_RELEASE`：`scopeVersion/reason`；scope 本身只在 envelope 的 `scope.type/id` 出现，reason 只允许已发布、丢弃、最终失败、取消、父 Import 真终态、Pegasus/EmulationStation 终态、上传消费完成或 Game 删除对应的封闭值。

字段不适用时使用 JSON `null`，不允许同 kind 因实现分支漂移成两种 shape。大型 manifest/依赖集仍由快照 digest 定位领域表；Worker 开始时同时校验引用和 digest，不信任队列 JSON 里的重复值。JobEvent `data_json` 固定含 `schemaVersion/executionNo/attempt`，PROGRESS 另含 `phase/completedUnits/totalUnits/unit`，error event 另含稳定 `errorCode/errorRetryable`；不存堆栈或秘密。

`dedupe_key` 统一存为 lowercase hex SHA-256(`"retrom-job-dedupe-v1\0" || kind || "\0" || RFC 8785 canonical dedupe input`)。dedupe input 固定为：UPLOAD_FINALIZE 用 uploadId+finalizationNo+finalizationInputDigest；IMPORT_GROUP 用 importJobId；IMPORT_ITEM_PIPELINE 用 importItemId；DAT_PARSE 用 datVersionId+parserVersion；VARIANT_REVALIDATE 用 gameVariantId+validationInputDigest；METADATA_SCRAPE 用 scrapeRunId；MEDIA_FETCH 用 candidateAssetId；GAME_FILE_REVISION 用 gameId+input `executionId`；BLOB_GC 用 blobId+firstUnreferencedAtMs；UPLOAD_CLEANUP 用 uploadId+expiresAtMs。因此同一次上传终结的并发重放只命中一条 Job，part 修复后的新 `finalizationNo` 必然保留为另一条 Job；需语义收敛的 Variant 并发请求复用同一 Job，而独立的游戏文件替换动作使用不同 execution ID。不允许每个 repository 自行拼字符串。

`cancellable` 的默认值也是契约：UPLOAD_FINALIZE、IMPORT_GROUP、IMPORT_ITEM_PIPELINE、METADATA_SCRAPE、MEDIA_FETCH 与 GAME_FILE_REVISION 为 true；内置 DAT 的 DAT_PARSE、VARIANT_REVALIDATE、RUNTIME_ASSET_PACK_VALIDATE、PAYLOAD_RELEASE、BLOB_GC、UPLOAD_CLEANUP 为 false。运行包校验最多一次 attempt，失败保留 installation 诊断并要求重新安装，不允许通用 retry 改写不可变输入。VARIANT_REVALIDATE 会被多个并发 Launch 按 digest 共用，某个 Player 退出等待只能取消自己的订阅/全屏 overlay，不能取消共享 Job。未来要改变此表必须先定义引用者计数与最后订阅者取消语义，不能只把按钮接到通用 cancel route。

Worker 默认并发：hash/copy 2、archive 1、DAT 1、metadata lookup 2、media 2、PayloadRelease 最多 4、GC 1。lease 60 秒、heartbeat 15 秒；最多 4 次 attempt，基础退避 1s/5s/30s/120s，外部 `Retry-After` 可覆盖但上限 15 分钟。时间逻辑必须注入 clock，测试不真实等待。

每个 execution 的 wall deadline 从第一次成功领取计算，固定为：UPLOAD_FINALIZE/IMPORT_GROUP/IMPORT_ITEM_PIPELINE/GAME_FILE_REVISION 6 小时，RUNTIME_ASSET_PACK_VALIDATE 10 分钟，DAT_PARSE/VARIANT_REVALIDATE 30 分钟，METADATA_SCRAPE 1 小时，MEDIA_FETCH/PAYLOAD_RELEASE/BLOB_GC/UPLOAD_CLEANUP 30 分钟。到期后 context 中止，短事务将 Job 置 FAILED；运行包校验固定 `error_retryable=false`，其余既有 kind 沿用各自重试契约。VARIANT_REVALIDATE 映射 `LAUNCH_CORE_VALIDATION_TIMEOUT`，PayloadRelease 使用固定 `PAYLOAD_RELEASE_EXECUTION_TIMEOUT`，其他 kind 使用 `<KIND>_EXECUTION_TIMEOUT`。超时前已发布的独立幂等子结果可在人工 retry 时复用，但不得发布半成品领域 current。deadline 和退避都用注入 clock；验收不真实等待这些时长。

取消事务必须以当前 Job `If-Match` 和领域资源版本为条件：QUEUED 任务可直接转 CANCELLED；RUNNING 任务只转 CANCEL_REQUESTED 并追加同事务事件。Worker 的每次最终提交都必须再次验证 state=RUNNING、lease owner/token 和未请求取消，因而旧 worker 不能在取消或 lease 转移后发布结果。hash/copy 每 8 MiB、XML 每不超过 1,024 token、archive 每 entry、网络 read loop 每 1 MiB 或一次阻塞 I/O 返回后检查 context；这些是取消检查上界，不要求测试真实处理大文件。

## 8. 数据库级保护

Migration 必须建立并测试：

- partial unique indexes：每个 BIOS requirement 的 active installation、每个 core artifact 的 active DatVersion、每个 `(core_id,route_key)` 的 `selected_for_new_bindings` artifact，以及每个 `(core_artifact_id, sha256, parser_version)` 至多一条 BUILTIN DatVersion；启动校验另保证每个 enabled Core 恰有一条可供新绑定且可启动的适用 artifact；
- trigger：MetadataRevision、GameAsset、GameContentRevision、GameContentFile、VariantRevision、VariantFile、DosEntry、VariantDependency、ImportItemSourceFile、ImportItemCoreValidation/ValidationFile、ContentHashEvidence、ProviderResponse、ScrapeQueryAttempt、ScrapeCandidate/Hit、ReviewEvent、AuditEvent、JobInputSnapshot、JobEvent 和 PlaySessionEvent 创建后禁止 UPDATE/DELETE；Game/Variant 只通过 current pointer 前移；
- trigger：ArchiveEntry 只允许 `materialized_blob_id` 从 NULL 一次性设为实际 size/CRC32/MD5/SHA-1/SHA-256 全部相等的 Blob；已非空、设回 NULL、改为另一 Blob 或修改任一其他字段都拒绝。DELETE 只在 owner-GC 事务中允许：同一 `archive_blob_id` 已无业务保护边，且不存在指向其任一 `(archive_blob_id, ordinal)` 的外部复合外键；服务必须按 archive_blob_id 成组删除并立即删除 owning Blob，普通 repository 不暴露 entry delete；
- trigger：DatVersion 只能由 release manifest 引导创建和选择；曾激活或已被 VariantRevision 引用的 DatVersion 及其 machine/entry 禁止删除；
- trigger：PlatformInstance 默认 core、GameVariant core 与间接平台关系有效；被目录/Variant 使用的 PlatformCore 不可禁用；
- trigger：BIOS Requirement 的 STATIC/DAT_MACHINE XOR、logical name/catalog 字段成立；active Installation 的 requirement/version/status 关系有效，INVALID 永不 active，MISSING_ENTRY 永不用于 READY Variant dependency bundle；BIOS_OR_BASE 的 HASH_WARNING 可用于 READY，PARENT 的 MISMATCH 不可用于 READY；
- trigger：Job kind/scope 只允许数据字典映射，`execution_no` 指向同 Job 最新连续 InputSnapshot，payload 只指向该 execution；JobEvent 的 scope/execution/attempt 与其 Job 一致；ProviderCache current response 与 cache key 的 provider/request digest 一致；
- trigger：ImportJobFile 覆盖当前 UploadSession 每个 UploadFile 且 SOURCE 与 ItemSourceFile 引用一致；Item 的 failed stage/error 与 FAILED_RETRYABLE/FAILED_FINAL 状态同时出现或同时为空；ItemSourceFile 的 UploadFile/Blob/archive entry 归属与 Item source manifest 一致；ImportItemCoreValidation 的 target/core/artifact/DAT/source manifest 归属一致，ValidationFile 只能属于同一 READY validation；ReviewDraft selected validation 必须属于自身 Item、READY 且匹配 target，人工封面必须属于自身 Item并与候选封面互斥，default DOS entry 属于自身 Item；
- trigger：MetadataScrapeRun owner XOR、Game owner/content ownership 与 Import owner/content-null 约束成立，且 Job scope/provider 匹配；provider=NONE 的 run 不得通过 evidence/attempt/hit 关联全局 ProviderResponse，也不得有 candidate/asset；ScrapeQueryAttempt 的 evidence/response/request digest/run/provider 关系一致；Candidate、Hit、CandidateAsset 的 attempt/response/run/provider/owner 关系一致；ReviewDraft 只能选择自己 Item 的 COMPLETED run/candidate 和 READY asset；
- trigger：Game 的当前 metadata/content revision 必须属于自身；GameVariant 的当前 revision 必须属于自身、状态为 READY，且其 content revision 必须属于同一个 Game；
- trigger：MetadataRevision 的 source kind/ref nullability、ImportItem/Game ownership 和 ScrapeCandidate/run/current ContentRevision 必须符合上表；ADMIN_EDIT 的领域 service 另必须在同一事务写入指向该 Game 与新 MetadataRevision ID 的 AuditEvent，该跨表“同次操作必须存在事件”用 service 集成测试保护，不伪称 SQLite 有 deferred assertion；
- trigger：GameContentRevision 的 source kind/ref 类型匹配；VariantRevision 的非空 default DOS entry 必须存在于其 ContentRevision 的 `dos_entries`；
- trigger：GameContentFile 的可空 source archive pair 必须命中同一 ArchiveEntry，且 materialized Blob 与 `blob_id` 一致；
- trigger：UploadSession `finalization_no >= 0`；FINALIZING 必须指向同 scope、同 `finalizationNo` 的当前 UPLOAD_FINALIZE Job，旧 Job 不可改写，COMPLETE 时所有文件 COMPLETE；插入 whole-session Upload consumption 时该 session 必须 COMPLETE 且没有任何既有 consumption，且 consumer ID 必须指向匹配的 ImportJob 或 `GAME_FILE_REVISION` Job；插入 file-level consumption 时该 session/file 必须 COMPLETE、该 session 没有 whole-session consumption，且 UploadFile 必须属于该 session；两方向都在同一写事务防竞态；
- CHECK：所有 size/duration/version 非负，结束时刻不早于开始，XOR 路径/blob 约束成立，MetadataRevision
  `title_initial` 恰为单个 `#`、ASCII 数字或 ASCII 大写字母；
- 索引：游戏搜索/目录/状态、存档 profile+game+created_at_ms、任务领取与 scope event、审核队列、DAT machine/hash、全部外键列。

业务服务仍需在事务前返回可理解错误，trigger 是并发和遗漏的最后防线，不能以 trigger 错误字符串作为 HTTP 契约。

## 9. 多盘证据与运行锁定

`content_kind`/`content_mode` 增加 `MULTI_DISC_M3U_V1`；Import source 与 GameContent file role 增加 `PLAYLIST_SOURCE`/`DISC`，VariantFile role 增加 `MULTI_DISC_PLAYLIST`。`import_item_multidisc_entries` 以 `(source_snapshot_id, ordinal)` 保存连续盘序、source reference、canonical name、PRESENT/MISSING 与可空 Blob；`review_multidisc_attachments` 关联 ImportItem、base/effective snapshot、Upload、Job、真实 User actor、状态、错误和诊断，并以局部唯一索引保证一个 active attachment。

`launch_external_files.kind=DISC` 锁定每张盘的规范虚拟路径，`launch_sessions.initial_disc_index` 锁定普通启动或 SaveState 恢复盘号。完整多盘 revision 的依赖快照必须包含 content kind、parser/delivery 版本、盘数、ordered disc SHA-256 与 canonical playlist SHA-256；多盘 Variant validation 使用独立 V3 canonical digest，包含 Variant/Content/Artifact ID、artifact version、compatibility digest、DAT、BIOS、盘序和 playlist。SINGLE/DOS 使用 V2；只有 generation 4 当前 prepublish evidence 可发布。

## 10. 收藏与收藏夹

### 10.1 表、主键与外键

| 表 | 字段与约束 |
| --- | --- |
| `favorite_games` | `profile_id TEXT NOT NULL REFERENCES profiles(id)`、`game_id TEXT NOT NULL REFERENCES games(id)`、`created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0)`；主键为 `(profile_id,game_id)`，关系行不可 UPDATE。 |
| `favorite_folders` | `id TEXT PRIMARY KEY`、`profile_id TEXT NOT NULL REFERENCES profiles(id)`、`name TEXT NOT NULL`、`name_key TEXT NOT NULL`、`version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1)`、`created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0)`、`updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms)`；候选键 `(profile_id,id)`，唯一键 `(profile_id,name_key)`。 |
| `favorite_folder_games` | `profile_id/folder_id/game_id TEXT NOT NULL`、`created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0)`；主键为三列，复合外键 `(profile_id,folder_id)` 指向 Folder、`(profile_id,game_id)` 指向 Favorite，关系行不可 UPDATE。 |

三个 ID 列沿用全库规范 UUID 的 36 字符、小写十六进制与连字符 CHECK。所有外键使用限制语义，不使用隐藏级联，因此数据库直接拒绝跨 Profile Folder、未收藏 Membership、先删 Favorite/Folder 后删 Membership，以及上层资源删除造成的私有关系丢失。

### 10.2 Folder 名称与版本

创建和重命名必须调用同一个服务端规范化函数：

1. 按 Unicode whitespace 去除首尾空白；
2. 转为 Unicode NFC；
3. 将内部每段连续 Unicode whitespace 折叠为一个 U+0020；
4. 拒绝任意 Unicode control character；
5. 规范展示名限制为 1–40 code point，UTF-8 不超过 160 bytes；
6. `name_key = cases.Fold().String(normalizedName)`；
7. 由 `(profile_id,name_key)` 唯一约束阻止同一 Profile 的大小写和等价 Unicode 重名。

API 返回规范后的 `name`。例如输入 `"  双人　 游戏  "` 保存为 `"双人 游戏"`；同一 Profile 随后创建等价名称必须冲突，不同 Profile 可以使用相同名称。每个 Profile 最多 100 个 Folder，该业务上限由同一写事务校验。

Folder 只允许修改 `name/name_key/version/updated_at_ms`，且 `version=OLD.version+1`、`updated_at_ms>=OLD.updated_at_ms`；`id/profile_id/created_at_ms` 不可变，空操作和版本跳号由 trigger 拒绝。删除释放规范名称，Folder ID 不复用。

### 10.3 索引、删除与可见性

索引固定为：

```sql
CREATE INDEX favorite_games_profile_created
ON favorite_games(profile_id,created_at_ms DESC,game_id DESC);
CREATE INDEX favorite_games_game
ON favorite_games(game_id,profile_id);
CREATE INDEX favorite_folders_profile_created
ON favorite_folders(profile_id,created_at_ms,id);
CREATE INDEX favorite_folder_games_folder
ON favorite_folder_games(profile_id,folder_id,created_at_ms,game_id);
CREATE INDEX favorite_folder_games_game
ON favorite_folder_games(profile_id,game_id,folder_id);
```

`favorite_games` 与 `favorite_folder_games` 的 UPDATE trigger 无条件拒绝关系改写；Folder guarded-update trigger 实施上节版本与不可变字段规则。业务取消收藏按 Membership → Favorite、删除 Folder 按 Membership → Folder 的顺序在同一事务显式完成；删除 Folder 保留 Favorite，精确替换空 `folderIds` 也不删除 Favorite。

Game `DELETED`、PlatformInstance 停用或 User 停用不删除三张表中的关系。所有用户列表、搜索、计数和 Folder visible count 必须在 SQL 中限定 PUBLISHED Game 与 enabled PlatformInstance；未来恢复可见后，原 Favorite 与 Membership 自动重新进入投影。管理 API 和 diagnostics 不提供逐用户收藏投影。

### 10.4 Schema 与验证

当前 clean schema 直接创建收藏表、索引和 trigger，不回填或推断收藏，也不改变 Blob reference registry。所有结构、非法 SQL 和两个 Profile 隔离统一由 `ACC-FAV-001` 验证。

## 11. 服务器 BIOS 导入

`server_imports` 是一次 `BIOS_DIRECTORY` 导入聚合，保存 root ID/label 快照、不可逆 root 配置 digest、规范相对目录、完整 catalog digest、覆盖授权、阶段、计数、Job/创建者、乐观版本和 Unix 毫秒时刻。partial unique index 保证全实例同一时刻至多一个非终态 BIOS ServerImport；终态行的各 Item 分类计数必须恰好覆盖冻结 catalog。

`server_bios_import_items` 以 `(server_import_id, requirement_id)` 为主键，冻结 Requirement/CoreArtifact/DAT/期望 hash、激活选项、交付位置和创建时 active Installation 证据。trigger 阻止修改冻结输入；状态只记录该 Requirement 的选择和提交结论，previous/new Installation 必须属于同一 Requirement。

`server_bios_import_candidates` 保存 root-relative path、关联原因、内容 hash、archive 评估、rank 和未选原因；同一 Item 的 path/rank 唯一且至多一条 `SELECTED`。绝对 root、CAS path、root digest 不进入候选投影。

`jobs.kind` 增加 `SERVER_BIOS_IMPORT`，且强制 `scope_type=SERVER_IMPORT`。`bios_installations` 增加不可变的 `source_kind=BROWSER_UPLOAD|SERVER_DIRECTORY` 和可空候选外键；服务器来源必须引用所属 Requirement 的 selected Candidate。每个 Requirement 的 Installation、Item 终态、聚合计数与 `PROGRESS` 事件在同一短事务提交，进程恢复以终态 Item 为幂等边界。

## 12. Pegasus 目录导入与 VIDEO

`pegasus_imports` 保存 root ID/label 与不可逆配置 digest、规范相对目录、metadata snapshot digest、scan/import Job、聚合状态/phase、映射版本、计数、7 天到期和创建者；partial unique index 保证全实例至多一个 `QUEUED|RUNNING|CANCEL_REQUESTED` execution。`pegasus_import_metadata_files` 只保存相对路径、大小、内容/facts digest 和解析结论，不保存原始 metadata bytes。

`pegasus_import_collections` 以 `(import_id,metadata_relative_path,segment_ordinal)` 唯一，冻结展示投影；`IMPORT` 映射必须同时冻结 PlatformInstance/version、Platform、默认 Core、CoreArtifact/version 和可空 active DAT，`SKIP` 不得携带目标。`pegasus_import_items` 以确定性 source key 唯一，冻结标题、允许的 metadata、声明文件引用和 discovery 结论，并关联后续内部 ImportItem、发布 Game 或所有 existing match。`pegasus_import_item_files/assets` 保存 no-follow source facts、CAS 复制结果和媒体 warning；它们的 Blob 边全部登记为 protective reference。

`jobs.kind` 增加 `SERVER_PEGASUS_SCAN|SERVER_PEGASUS_IMPORT` 并强制 `scope_type=PEGASUS_IMPORT`。scan 与 import 是不同 Job；retry 在原 import Job 增加 execution/input snapshot，不复活旧事件。发布来源扩展为 `game_metadata_revisions.source_kind` 与 `game_content_revisions.source_kind` 的 `SERVER_PEGASUS_IMPORT`，且 source_ref 必须指向处于审核发布边界的 Pegasus Item。

`game_assets.kind` 的 ordinal 0 `VIDEO` 只允许 `video/mp4|video/webm` 且 `width_px/height_px` 必须为 null；图片仍必须具有正尺寸和受限图片 MIME。每次管理替换/移除媒体或修改文字都先为新 current MetadataRevision 建立完整媒体清单；旧 Asset 永不原地修改，也不作为历史 payload 长期保留，而是在 current pointer 成功切换后于同一事务删除。其 Blob 失去最后保护引用时立即进入 GC 候选，物理删除仍遵守统一宽限期。

## 13. Pegasus 管理员失败诊断

`pegasus_import_items.error_details_json` 是可空、最大 8 KiB 的 JSON object，只保存管理员排障所需的封闭证据：schema version、失败阶段、内部操作、稳定 cause code、受限技术详情、来源相对路径、观察值/上限，以及已创建时的内部 ImportJob/ImportItem ID。不得写入宿主绝对路径、Blob ID/hash、凭据或未截断上游 payload；retry 把该字段与旧 `error_code` 一起清空，新 execution 重新生成证据。

当前 worker 在产生失败结果时直接记录精确 stage/operation/cause、实际数量和内部上限；无法从当前执行证据证明根因时不得猜测或伪造技术详情。

## 14. Pegasus 审核交接

`pegasus_imports` 增加 `review_pending_item_count/review_discarded_item_count`，phase 增加 `PREPARING_REVIEWS`；总量约束把待审核、已发布和审核丢弃计入互斥结果。`pegasus_import_items.execution_state` 增加 `REVIEW_PENDING/REVIEW_DISCARDED`，`library_import_item_id` 建立非空唯一索引，使一个普通 ImportItem 至多归属一个 Pegasus Item。`REVIEW_PENDING` 必须引用同一内部 ImportJob 中仍为 `REVIEW_PENDING` 的 ImportItem；`REVIEW_DISCARDED` 必须对应内部 Item 的 `DISCARDED`。

Pegasus Worker 在复制、普通 content pipeline 与 CoreValidation 后只冻结 metadata、COVER/VIDEO 和内部 ImportItem 关联，再把 Pegasus Item 交接为 `REVIEW_PENDING`；此时普通审核队列才允许展示。交接未完成的关联 Item 不得出现在队列/详情，也不得被 Approve/Discard。崩溃恢复复用既有 `library_import_job_id/library_import_item_id` 并幂等补齐 metadata，不得创建第二个内部 ImportItem 或重复系统草稿事件。

管理员 Approve 在普通审核发布事务内创建 `SERVER_PEGASUS_IMPORT` metadata/content revision、复制未被人工封面覆盖的来源 COVER 与来源 VIDEO，并把 Pegasus Item 原子转为 `PUBLISHED`；Discard 在普通审核事务内原子转为 `REVIEW_DISCARDED`。`source_ref_id` 始终保留 Pegasus Item 作为来源审计，content manifest 必须匹配与其一一关联的 ReviewDraft 当前有效来源快照；Parent ROM 或多盘补传产生后继快照时不得退回校验 Pegasus Item 的初始 manifest。两种决策都同步重算 Pegasus 聚合计数。没有审核决策时不得创建 Game；快速审批只复用严格 READY Approve，不改变这个发布边界。

Pegasus 表从 fresh schema 直接包含当前 review-handoff 状态、诊断、映射、媒体和聚合计数，不创建自动发布或通用阻断兼容状态。

## 15. 审核运行预览与第 5 秒截图

`review_preview_sessions` 只保存非 RPG Maker 管理员从待审核条目创建的短时、不可变运行快照：锁定 ImportItem、有效来源快照、目标目录、默认 CoreArtifact、当次 Validation、主内容 Blob、依赖摘要、capability hash、启动/硬过期时间和是否允许截图。它不是 `launch_sessions` 或 `play_sessions`，不创建 Game、不累计游玩时长、不读写状态存档或持久存档。只有 `REVIEW_PENDING` 非 RPG Item、当前有效来源和 enabled 管理员能创建；单文件内容始终必需，当前 Validation 中实际存在的 Parent、BIOS 和 external file 才复制为 `review_preview_files`，缺失依赖不会伪造占位。ONS 则把当次 source snapshot 的脚本 marker 作为主内容，并逐项锁定其余 `PROJECT_FILE`，由虚拟 `index.json` 暴露冻结列表；不能在请求时回读已变化的 ReviewDraft。RPG core 请求该旧 preview 必须返回 `RPG_RUNTIME_VALIDATION_REQUIRED`。

`review_preview_files` 只允许 `PARENT/BIOS_BUNDLE/EXTERNAL_FILE/DISC/PROJECT_FILE`，行创建后不可更新或删除；Blob、逻辑名和可空虚拟路径必须属于创建时锁定的来源/Validation。`PROJECT_FILE` 只用于 ONS preview，必须是对应 source snapshot 中同名同 Blob 的规范相对路径且不带虚拟路径；其他 role 继续使用单文件名/既有虚拟路径规则。`review_runtime_screenshots` 以 `(import_item_id,validation_id)` 唯一保存 PNG、CoreArtifact、来源快照、尺寸和固定 `captured_after_ms=5000`；重新运行会以新不可变 Blob 替换该 Validation 的当前截图引用。当前 trigger 允许 READY/阻断 Validation，并收紧到最新一次当前证据。

三张表的 Blob 外键全部进入唯一 reference registry；fresh schema 直接建立最终表、索引和 trigger。

## 16. 受限联机控制面

| 表/变更 | 唯一职责与稳定不变量 |
| --- | --- |
| `netplay_rooms` | 房间聚合；状态 `DRAFT/WAITING/STARTING/RUNNING/ENDED/EXPIRED`。host、创建时刻不可变；DRAFT 没有 game/profile snapshot，WAITING 以后五个 snapshot 字段全有；STARTING/RUNNING 恰有 current session；每个 Profile 最多主持一个非终态房间。DRAFT 15 分钟、WAITING 30 分钟空闲过期，STARTING 120 秒、运行 8 小时硬终止。 |
| `netplay_room_members` | `(room_id,profile_id)` 唯一，active `(room_id,player_no)` 唯一；HOST 固定 P1 且每房恰一 active host，GUEST 为 P2–P4；ready 只在 active WAITING 成员上成立，离开必须清 ready 并记录封闭 reason。 |
| `netplay_sessions` | 每次 Start 的不可变 game/variant/core/profile canonical snapshot；状态 `PREPARING/LOADING/SYNCHRONIZING/RUNNING/PAUSED_RECONNECT/RESYNCHRONIZING/FINISHED/FAILED`，每房最多一个 active session。`profile_json` 是 core-profile canonical object，`profile_digest` 是 lowercase SHA-256；它包含不可变 GameVariantRevision ID，从而在 core profile 覆盖多个游戏时仍锁定本局唯一内容 revision，而不是把 ROM hash 放回准入 manifest。P1 是唯一 state authority，occupied mask 必含 P1，resync 只递增。 |
| `netplay_session_participants` | 锁定 Session 中每个 Profile/seat；状态 `LOCKED/LAUNCH_READY/RUNTIME_READY/SYNCHRONIZED/CONNECTED/DISCONNECTED/LEFT`。LOCKED 时 launch/credential 均空；LAUNCH_READY 起二者全有，credential generation 从 1 单调递增，数据库只存 SHA-256；seat/member/session/launch 绑定不可改。断线只存 10 秒 lease 时刻，不存输入或 state bytes。 |
| `netplay_events` | 房间级 append-only 小事件；只允许 schema 中封闭 event type 和低基数 `data_json`，禁止 UPDATE/DELETE。不得记录显示名、输入、ROM/BIOS 名称或 hash、state、cookie、IP、宿主路径。每帧 input/canonical/hash 不入库。 |
| `launch_sessions` | 新增可空 `netplay_session_id/netplay_player_no` 与非空 `save_access NORMAL/NETPLAY_DISABLED`。普通 Launch 必为 `NULL,NULL,NORMAL`；联机 Launch 三者同时锁定并与 Participant/Session snapshot 完全相同，每 Participant 最多一个 Launch。 |

每个 Session 终态都会撤销关联 Launch 并把其 Participant 标为 LEFT。运行中任一 Participant 离开等价全局 `USER_EXIT`：访客释放自己的 RoomMember、房间回到 WAITING且全员 ready 清零；房主主动结束本局时保留成员并回 WAITING。房主丢失/关闭、profile 撤销、服务重启、restore 与硬到期把 Room 标为 ENDED，活动 RoomMember 以 ROOM_ENDED 收口。服务启动 recovery 把遗留 STARTING/RUNNING Session 标为 `FAILED/SERVER_RESTARTED` 并撤销 Launch；restore 使用 `RESTORE`。实时 input/history/hash/state transfer 只存在 `internal/netplay.Hub` 的有界内存，不新增 Job/Blob/CAS 表，也不允许由运行时 DDL 修补。

## 17. 阻断截图人工放行

当前 schema 的审核 preview/screenshot 校验 trigger 要求新建 preview 及截图插入/替换都绑定该 Item、有效来源快照、草稿目标平台、默认 CoreArtifact 和同一组合下按 `created_at_ms,id` 选出的最新 Validation；Validation 必须使用当前 `prepublish_generation=4`，但状态可以是 READY 或阻断。重新检查产生更新 Validation 后，旧 preview 即使稍后上传截图也会被 trigger 拒绝。

当前阻断 Validation 的第 5 秒截图可作为管理员放行证据；Approve 仍需在同一事务复核来源、目标、默认 CoreArtifact、active DAT 和配置版本，并把截图 ID 与 `REVIEW_SCREENSHOT_OVERRIDE` 写入不可变审核证据。fresh schema 必须通过 foreign-key/integrity 检查。

本节仅适用于非 RPG Maker 内容。RPG Maker 的 preview/截图 override 一律不能满足发布条件；它必须使用第 25 节 runtime validation 的不同 restore Launch 和位置恢复 gate。

## 18. 实例级游戏标签

| 表/变更 | 唯一职责与稳定不变量 |
| --- | --- |
| `tags` | `id PK`、规范 `name/name_key/search_text`、`status ACTIVE/DELETED`、`version`、创建/更新 actor 与 `*_at_ms`、可空 `deleted_at_ms`。活动 `name_key` partial unique；DELETED 必须有删除时刻且不能恢复、改名或硬删；稳定创建字段不可改。 |
| `game_tags` | `(game_id,tag_id) PK`、分配 actor/time；反向索引 `(tag_id,game_id)`。关系无顺序、不可 update，只能引用 ACTIVE Tag，每 Game 最多 20 个活动关系。历史 DELETED 关系保留且不计上限。 |
| `review_draft_tags` | `(review_draft_id,tag_id) PK`、分配 actor/time；反向索引。只在所属 ImportItem 为 `REVIEW_PENDING` 时改变活动关系，每 Draft 最多 20；决定后关系冻结保留。 |
| `pegasus_collection_tags` | `(collection_id,tag_id) PK`、分配 actor/time；反向索引。只在所属 PegasusImport 为 `AWAITING_MAPPING` 时改变活动关系，每 Collection 最多 20。 |
| `pegasus_import_collections.tag_snapshot_json` | 非空合法 JSON array，默认 `[]`；mapping PUT 保存按 Tag 名称稳定排序的 `{tagId,name}` 证据，start 后与其余映射字段共同冻结。 |
| `emulationstation_collection_tags` | `(collection_id,tag_id) PK`、分配 actor/time；反向索引。只在所属 EmulationStationImport 为 `AWAITING_MAPPING` 时改变活动关系，每 Collection 最多 20。 |
| `emulationstation_import_collections.tag_snapshot_json` | 非空合法 JSON array，默认 `[]`；保存与 Pegasus 相同的稳定名称 snapshot，start 后冻结。 |

Tag 名称的 NFC、Unicode 空白折叠、case-fold、control 拒绝、1–40 code point/160 byte 和实例 1,000 上限在唯一 Tagging service 执行；数据库对长度、活动唯一、owner 状态和关系上限再 fail closed。关系集合变化推进 touched Tag 与 owner version。Tag DELETE 在短事务内写 tombstone、推进全部受影响 Game、待审核 ReviewDraft 和未完成 Pegasus/EmulationStation aggregate/mapping version，并保留关系和 AuditEvent。Tag 不进入 metadata/content/variant/runtime 表；完整领域语义见 [`game-tags.md`](./game-tags.md)。

当前 clean schema 直接创建上述表、索引/trigger 和非空 `tag_snapshot_json`（默认 `[]`）；数据库必须通过 `foreign_key_check` 和 `integrity_check`。备份直接包含这些 SQLite 行，不新增 Blob reference。

## 19. Pegasus 审核内容后继快照

`game_content_revisions_pegasus_source_insert` trigger 只接受 `REVIEW_PENDING` 当前流程：它必须通过 `library_import_item_id` 一一关联 ReviewDraft，并让待写入 ContentRevision 的 digest/content kind 与 Draft 的 `effective_source_snapshot_id` 精确一致。这样 metadata/content revision 仍以 Pegasus Item 作为 `SERVER_PEGASUS_IMPORT` 来源引用，同时 Parent ROM 或多盘补传形成的新 manifest 由不可变审核来源快照证明。

发布服务集成回归必须覆盖 Pegasus Arcade 条目先因缺 Parent 阻断、补传生成后继快照和 READY Validation、再以当前 Review version 成功发布的完整事务；不得放宽到任意其他快照或只信任客户端 digest。

## 20. 严格 READY 快速审批

| 表/变更 | 唯一职责与稳定不变量 |
| --- | --- |
| `review_bulk_approvals` | 一次实例级快速审批 aggregate；保存创建者、规范筛选 `scope_json/scope_digest`、候选 manifest digest、初始分类计数、进度计数、`QUEUED/RUNNING/CANCEL_REQUESTED/COMPLETED/PARTIAL_FAILURE/CANCELLED/FAILED` 状态、错误、乐观版本与 Unix 毫秒时刻。partial unique 保证全实例至多一个 active batch；`processed_count` 必须等于各结果计数之和，正常完成、部分失败与取消必须覆盖全部 candidate；worker 级 `FAILED` 可保留 PENDING 项供领域 retry。 |
| `review_bulk_approval_items` | 批次候选的冻结 manifest；以 `(bulk_approval_id,import_item_id)` 为主键，ordinal 唯一，保存预览时 Review version、Validation ID、effective source snapshot、标题与目标目录名称快照。结果为 `PENDING/RUNNING/PUBLISHED/SKIPPED_DUPLICATE/SKIPPED_CHANGED/SKIPPED_NOT_READY/FAILED_FINAL/CANCELLED`；只有 PUBLISHED 可引用本 Item 的 Game 与 APPROVED ReviewEvent。冻结输入不可更新。 |
| `jobs/job_events` | kind/scope 增加 `REVIEW_BULK_APPROVE→REVIEW_BULK_APPROVAL`。Job 可取消；通用 Job retry 返回 `RETRY_VIA_DOMAIN_ACTION`，只有 aggregate 以 `REVIEW_BULK_WORKER_UNAVAILABLE` 失败且 Job 标记 retryable 时才允许领域 retry。 |

预览在同一个只读快照中枚举整个规范筛选范围，不依赖浏览器当前加载页。候选必须为当前 `REVIEW_PENDING`、严格 generation 4 `READY`、目录/CoreArtifact/active DAT/DOS entry/dependency snapshot/来源/标题都未漂移、无 active Parent/多盘补传且无当前重复 Game。阻断截图只能支持逐项人工放行，不能进入自动候选。创建批次时服务端重新计算 scope 与 candidate manifest；任一 digest 漂移都拒绝创建。

Worker 顺序处理冻结 Item；每项在普通 Approve 短事务内再次验证冻结输入和严格 READY，创建 Game/Revision/ReviewEvent、推进普通与对应服务器来源聚合并写 `PUBLISHED` 批次结果，任何一步失败都整体回滚。EmulationStation `hidden/adult` 来源项在预览阶段计入 `sourceFlagged` 并排除快速审批；它们仍可逐项审核。重复、输入漂移和不再 READY 分别收口为 skip；意外项错误进入 `FAILED_FINAL`，不阻止剩余候选。取消只停止未提交项并保留已发布项；进程重启把未提交 RUNNING Item 退回 PENDING 后恢复，restore fence 则把非终态 Item 取消、aggregate/Job 置不可重试 `FAILED/RESTORE_INTERRUPTED`。当前 clean schema 直接创建最终 job/event 约束并通过 foreign-key/integrity 检查。

## 21. 推荐目录代码 catalog

`platform_instances.catalog_template_key` 可空；非空值有 partial unique index。fresh DB 拥有完整 Platform/Core reference seed 但零 PlatformInstance。推荐模板由 `internal/platformcatalog` 管理，扩展名由 `internal/contentprofile` 管理；“应用推荐目录”以 UUIDv7 创建当前 29 个模板并保持幂等，其中 RPG Maker 只有 `rpgmaker/rpgmaker` 一个虚拟核心目录，另有一个 `ons/onscripter_yuri`，clean schema 不插入实例目录。七个 RPG Maker 世代 core 仅供内部 route/artifact 绑定，不进入 `platform_cores`，也不保留七目录兼容分支。NES profile 稳定返回 `.nes/.unf/.unif/.fds`，Arcade 共同 `.zip` 只投影一次；扩展名稳定有序且无重复。

## 22. Payload 生命周期、Game 墓碑与回收 Job

`PAYLOAD_RELEASE` 是不可取消、最多四次 attempt、单次 execution 30 分钟的通用 Job。scope 只允许 `IMPORT_ITEM/IMPORT_JOB/PEGASUS_IMPORT_ITEM/EMULATIONSTATION_IMPORT_ITEM/UPLOAD_CONSUMPTION/GAME`；输入冻结 scope version 与释放原因，`UNIQUE(kind,dedupe_key)` 保证每个 scope 唯一。Worker 只解除 ownership registry 指定的业务引用并建立 `BLOB_GC` 候选，不在领域事务中直接删除 CAS 文件。进程重启把中断的 release/GC Job 恢复为 QUEUED；重复执行以剩余叶子和最终引用断言收敛。稳定错误码固定为 `PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL/SCOPE_VERSION_MISMATCH/SOURCE_NOT_TERMINAL/DEPENDENCY_PENDING/REFERENCE_REMAINS/UPLOAD_STILL_CONSUMED/DATABASE_FAILED/EXECUTION_TIMEOUT`。

每条 Blob FK 同时在机器可读 blob reference registry 和代码 ownership registry 中恰好出现一次。ownership 分类为 `GAME_OWNED/GAME_RUNTIME_OWNED/IMPORT_ITEM_OWNED/PEGASUS_ITEM_OWNED/EMULATIONSTATION_ITEM_OWNED/SCRAPE_RUN_OWNED/UPLOAD_OWNED/GLOBAL_TTL/GLOBAL_DURABLE/ARCHIVE_OWNERSHIP/BOOKKEEPING`。新增或删除 Blob FK 但没有同步两份 registry 会使 schema/CI 失败。BIOS 等 GLOBAL_DURABLE 引用不由流程清理；provider raw response 只按 TTL 维护；archive member 继续按存储专题的一层 closure 保护。

`pegasus_import_items` 含单调 `version` 与 `payload_state/payload_release_job_id/payload_released_at_ms/payload_last_error_code`。已交接公共 ImportItem 的 Pegasus Item 共用 ImportItem 的 release Job；未交接且处于 `PUBLISHED/REVIEW_DISCARDED/SKIPPED_*`、阻断、取消或不可重试失败终态的 Item 才创建 `PEGASUS_IMPORT_ITEM` scope。`pegasus_import_item_files/assets` 的 `PAYLOAD_RELEASED` 形态必须清空 Blob/source archive 字段并保留相对路径、大小、facts digest、角色、解析结果与 warning/error。

`emulationstation_import_items` 使用对称的 payload/version 字段与释放门禁。已交接普通 ImportItem 的条目共用 ImportItem release；未交接终态条目创建 `EMULATIONSTATION_IMPORT_ITEM` scope。File/Asset 进入 `PAYLOAD_RELEASED` 后必须清空 Blob/archive 字段但保留相对路径、大小、facts digest、角色、解析结论与 warning/error。

Game 永久删除事务验证标题、version 和 impact digest，将 Game 原子转为 `DELETED+RELEASING`，撤销 Launch/Play、以 `GAME_DELETED` 结束联机、请求取消 mutation Job、创建唯一 release Job 并写 `GAME_PERMANENT_DELETE_REQUESTED` 审计。Game executor 遍历全部历史 Content/MetadataRevision 的固定 sourceRef，释放本地上传、Pegasus、EmulationStation 和后续管理上传来源链的已终态流程 payload；删除所有 GameAsset/GameContentFile/VariantFile、SaveState、Launch payload 与 scrape payload，但保留 revision 标题/manifest/hash 摘要、ReviewEvent/AuditEvent、Launch/Play/Netplay 历史、Favorite 与 Tag 关系。`DELETED` 不可恢复，trigger 阻断新 revision、Launch、Save 和内容读取。

`audit_events.actor_label` 的 SYSTEM 枚举包含 `payload-release-worker`；release 完成/失败分别追加 `PAYLOAD_RELEASE_COMPLETED/FAILED`，用户删除请求只记录 Game ID、影响计数、独占/共享字节、来源类型和 Job ID，不记录确认标题、Blob/hash 或路径。`blob_gc_candidates` 固定关联 `gc_job_id`；BLOB_GC 在 24 小时至 30 天的配置宽限后重新计算完整保护集，新引用会撤销候选。无引用 archive 先删 entry 与 Blob 行，事务提交后才删物理文件；物理删除失败可用同一 Job 输入幂等重试。

## 23. EmulationStation 服务器目录导入

当前未发布版本直接修改 clean `003/007/008/010` lineage，不新增迁移序号，也不保留旧开发库转换路径。新增业务 ID 使用 UUIDv7，时刻使用 Unix 毫秒 `INTEGER`；枚举全部以 `CHECK` 封闭。JSON 除 `json_valid` 外必须校验顶层类型、`schemaVersion`、允许 key、字段类型和尺寸上限，并由 service 再解码。

| 表 | 必需字段与约束 |
| --- | --- |
| `emulationstation_imports` | `id/root_id/root_label_snapshot/source_relative_path/root_config_digest/release_year_max` 与可空 `source_snapshot_digest`；`state/phase`、唯一 `scan_job_id`、可空唯一 `import_job_id`、`mapping_version/version/created_by_user_id`；gamelist/invalid/collection/folder/game/estimated bytes、mapping/skip/processable/blocked/review/published/discarded/existing/failed/cancelled/media/cover/video 全部非负计数；可空错误/取消/完成字段与 `retryable`、7 天 `expires_at_ms`。partial unique 保证全实例最多一个 `QUEUED|RUNNING|CANCEL_REQUESTED` execution；历史按 `(created_at_ms DESC,id DESC)`。 |
| `emulationstation_import_gamelists` | `(import_id,relative_path) PK`、观测 `size_bytes`、仅在超限隔离场景可空的 `content_digest`、非空 `source_facts_digest`、`parse_state VALID/INVALID`、可空 `error_code`、game/folder 计数、`provider_present`、有界 ignored fields/count 与创建时刻。正常读取的文件必须保存 content digest；超过 8 MiB 的文件只保存 stat/facts、`INVALID/EMULATIONSTATION_GAMELIST_TOO_LARGE` 与空 content digest，并与其他清单隔离。进入 `AWAITING_MAPPING` 后不可更新；不保存 XML bytes、provider 值或绝对路径。 |
| `emulationstation_import_collections` | `id/import_id`，唯一 `(import_id,gamelist_relative_path)`；冻结 `relative_directory/display_name/game_count/issue_count/folder_entry_count/hidden_game_count/adult_game_count` 与最多 32 个 `{extension,count}` 的 `extension_summary_json`/other count。mapping 为 `IMPORT|SKIP`；IMPORT 必须完整锁定 PlatformInstance/version、Platform、default Core、CoreArtifact/version、可空 active DAT 和 `tag_snapshot_json`，SKIP 不得带 target/tag。根清单展示名固定“根目录”，其他为清单父目录 basename；它不参与平台推断。 |
| `emulationstation_collection_tags` | `(collection_id,tag_id) PK`、分配 User 与时刻；只在 aggregate `AWAITING_MAPPING` 可变，每 Collection 至多 20 个活动 Tag。 |
| `emulationstation_import_items` | `id/import_id/collection_id/gamelist_relative_path/game_ordinal/source_key`；唯一 `(import_id,source_key)` 与 `(import_id,gamelist_relative_path,game_ordinal)`。冻结 title、`source_flags_json/discovery_state/content_kind/metadata_json/warnings_json/source_manifest_json/source_manifest_digest/discovery_code`；执行字段含 `execution_state/error_code/retryable/version`、可空唯一 `library_import_item_id`、内部 job、published/existing 与全部 match、最大 8 KiB 失败详情；payload 状态/release Job/时刻/error 与生命周期时刻。metadata/sourceFlags/warnings/sourceManifest/existingMatches 上限依次 32/4/16/32/32 KiB；warning 只保存封闭 code/field/pathKind/omitted/length 结构，不保存 XML 值或底层错误文本。 |
| `emulationstation_import_item_files` | `(item_id,ordinal) PK`，ordinal `<64`，同 Item relative path 唯一；`declared_kind FILE/PLAYLIST/DISC`、规范相对路径、size/facts、可空 Blob/archive entry、role/logical name、`state DISCOVERED/COPIED/SOURCE_CHANGED/READ_FAILED/UNSUPPORTED/PAYLOAD_RELEASED` 与时刻。发现 snapshot 不可修改，执行只填复制结果或转 released。 |
| `emulationstation_import_item_assets` | `(item_id,kind) PK`，kind `COVER/VIDEO`；`resolution_method` 只允许 `EXPLICIT_IMAGE/EXPLICIT_BOXART/EXPLICIT_MIX/EXPLICIT_THUMBNAIL/EXPLICIT_THUMBNAIL_ALIAS/EXPLICIT_VIDEO`；保存相对路径、size/facts、可空 Blob、媒体类型/图片尺寸、`DISCOVERED/COPIED/MISSING/INVALID/TOO_LARGE/SOURCE_CHANGED/READ_FAILED/PAYLOAD_RELEASED`、warning 与时刻。MIME、图片尺寸和 VIDEO 约束与 Pegasus/Game media 相同。 |

EmulationStation aggregate 状态为 `SCANNING/AWAITING_MAPPING/QUEUED/RUNNING/COMPLETED/PARTIAL_FAILURE/EXPIRED/CANCEL_REQUESTED/CANCELLED/FAILED`；phase 封闭为 `DISCOVERING_GAMELISTS/PARSING_GAMELISTS/RESOLVING_SOURCES/COPYING_CONTENT/VALIDATING/PREPARING_REVIEWS`，Item discovery 封闭为 `READY/BLOCKED_SOURCE/BLOCKED_CONTENT`，execution 封闭为 `PENDING/COPYING/VALIDATING/REVIEW_PENDING/PUBLISHED/REVIEW_DISCARDED/SKIPPED_EXISTING/SKIPPED_MAPPING/BLOCKED_SOURCE/BLOCKED_CONTENT/SOURCE_CHANGED/READ_FAILED/COMMIT_FAILED/CANCELLED`；gamelist parse、file/asset 与 payload 状态也由代码和数据库共同维护为上表闭集。`SERVER_EMULATIONSTATION_SCAN|SERVER_EMULATIONSTATION_IMPORT` 强制 `scope_type=EMULATIONSTATION_IMPORT`，scan/import Job ID 写入后不可替换；`PAYLOAD_RELEASE` 允许 `EMULATIONSTATION_IMPORT_ITEM`。

数据库 trigger 必须保证 mapping 只在 `AWAITING_MAPPING` 修改；发现 snapshot 在离开 SCANNING 后不可更新；之后只开放白名单内 mapping、执行状态、Blob、关联、error、payload 与 version 单调转换。`library_import_item_id` 非空时全局唯一，同一普通 ImportItem 不得同时归属 Pegasus 与 EmulationStation。`REVIEW_PENDING/REVIEW_DISCARDED/PUBLISHED` 必须分别与普通 ImportItem、ReviewEvent、Game revision 的 `SERVER_EMULATIONSTATION_IMPORT` source/ref 一致；ContentRevision 允许由 Parent/多盘补传形成的当前有效审核 source snapshot，规则与 Pegasus 对称。File/Asset、审核预览与发布 Game 的全部 Blob 边进入唯一 reference registry，released 行清空 Blob 字段；fresh schema 必须通过 `foreign_key_check` 和 `integrity_check`。

EmulationStation 交接使用普通 ImportItem 上不可变的 `review_handoff_kind=EMULATIONSTATION` 作为耐久预留。`CreateServerSourceOnce` 必须把该预留与 ImportJob/ImportItem 原子创建，重试则按同一幂等键复用原 Item 和预留；在某个 EmulationStation Item 以 `library_import_item_id` 关联它且 source execution state 进入 `REVIEW_PENDING` 前，普通 Item 即使已经是 `REVIEW_PENDING`，也不得进入审核列表、详情、批量预览/创建、Approve、Discard 或待审核 KPI。attach 成功后这些入口同时开放，不能依赖短暂的内存标记或先暴露后补偿。

## 24. 沉浸模式复用既有持久化模型

沉浸模式不新增专用数据库表、状态机、服务端偏好或“当前入口/平台/游戏”持久化字段。它从现有 Profile、
Favorite/Folder、SaveState、游戏平台、当前元数据/媒体与游玩会话形成独立投影：入口和平台计数只统计当前
可见且已发布、可运行的 Game/Variant；上次游玩时间来自当前 Profile 的 `play_sessions.started_at_ms`；收藏
与收藏夹只读写当前 Profile 的既有关系；存档只读取当前 Profile 的未删除 SaveState；封面、视频和描述只读
当前 MetadataRevision。进入、退出、当前焦点和活动手柄索引只存在于浏览器内存。

`game_metadata_revisions.title_initial` 是所有生产写入路径在创建不可变 revision 时计算并冻结的通用排序键，
不是沉浸会话状态。改名必须在同一事务创建含新 `title_initial` 的 MetadataRevision、复制未变字段/媒体、推进
current pointer 和搜索投影；旧 revision 不回填或原地修改。沉浸的全部/收藏/存档/平台范围按
`title_initial ASC,title COLLATE NOCASE ASC,game_id ASC` 稳定分页，最近范围例外地按本 Profile 最近游玩时间
倒序，不能用标题键覆盖时序语义。

沉浸 Player 仍创建普通单机 Launch 与 PlaySession，并沿用现有完成、撤销、有效游玩时长和永久删除规则。
从存档启动只把所选 SaveState 交给既有 Launch 预检；Player 中“创建存档”显式复用普通手动 SaveState 的
状态 payload 与可选截图的上传事务。菜单暂停归属、双组合键、输入过滤、BGM 与两组音量偏好仍是前端运行态；偏好仅保存为
严格版本化 localStorage payload，不进入数据库。关闭菜单或退出不会自动创建 SaveState。

## 25. RPG Maker 内容、运行包、验证与隔离凭据

RPG Maker 数据严格分成“用户选择虚拟核心”和“服务端检测出的内部运行绑定”两层。`platform_instances.default_core_id` 固定为用户可见的 `rpgmaker`；`rpgmaker_content_profiles` 保存 bytes 能证明的 generation，`rpgmaker_variant_profiles` 冻结由该 generation 唯一选择的内部 core、route 与 artifact。无法唯一裁决的内容在导入边界拒绝，不能通过用户选择或 fallback 改写内容事实。

RPG 项目使用第 3 节的通用 V2 清单与 `RETROM_FILESET_V1` 算法，`contentKind` 固定为 `RPG_MAKER_PROJECT_V1`、所有用户项目文件的 role 固定为 `PROJECT_FILE`，最多 10,000 个文件；`rpgmaker_content_profiles.project_fingerprint` 必须逐字节等于该清单的 `filesDigest`。`source_manifest_digest` 与 `filesDigest` 不得互相代替。

ONS 项目复用同一通用 V2 清单与 `RETROM_FILESET_V1` 算法，`contentKind` 固定为 `ONS_PROJECT_V1`，全部项目文件 role 固定为 `PROJECT_FILE`。脚本 marker、字体路径和脚本编码只作为 core-validation dependency snapshot 的结构化运行参数，不进入内容身份，也不能替代实际审核试运行。

| 表/变更 | 必需字段与约束 |
| --- | --- |
| `rpgmaker_content_profiles` | `content_revision_id PK/FK`、`evidence_family RPG2K/RGSS/MV/MZ`、可空 `evidence_generation`、`evidence_confidence MATCHED/FAMILY_ONLY`、可空 `engine_version/entry_html_path`、`file_count/total_bytes/project_fingerprint/requirements_sha256/analysis_json/created_at_ms`；一对一、不可修改。只有 RPG2K 可为 `FAMILY_ONLY + evidence_generation NULL`；`project_fingerprint` 等于 V2 filesDigest。 |
| `rpgmaker_variant_profiles` | `game_variant_revision_id PK/FK`、`generation/route_key/adapter_id/adapter_abi/artifact_set_sha256/dependency_snapshot_sha256/runtime_validation_id`；一对一、不可修改。generation 必须等于 Variant core 的固定映射；exact evidence 必须相等，family-only 只允许 2000/2003。 |
| `review_drafts` RPG 字段 | 增加 `runtime_binding_revision INTEGER NOT NULL DEFAULT 1`。只有 effective source、目标目录/default core、所选 core、pack/self-contained、route/artifact 等运行输入变化才递增 binding revision；标题、描述、标签和媒体只递增既有 version。 |
| `rpgmaker_review_profiles` | `review_draft_id PK/FK`、`selected_core_id/generation`、`evidence_family/evidence_generation/evidence_confidence`、可空 `engine_version/entry_html_path`、`file_count/total_bytes/project_fingerprint/requirements_sha256/analysis_json`、`self_contained_override`、`route_key/artifact_id/artifact_set_sha256/adapter_id/adapter_abi/dependency_snapshot_sha256` 与时刻；一对一保存当前审核的内容证据投影和运行绑定，core/generation/route/artifact 必须逐项一致。 |
| `review_draft_runtime_pack_selections` | `(review_draft_id,slot) PK`、声明名/规范名、definition/installation 与创建时刻；PATCH 总是完整替换，EasyRPG 只允许 slot 0，RGSS 只允许 RTP1..3 的 slot 1..3。 |
| `runtime_asset_pack_definitions` | `id/kind/generation/declared_name/normalized_declared_name/display_name/required_layout_version/origin BUILTIN|CUSTOM/enabled`、可空 creator、时刻；定义不可修改，`UNIQUE(generation,normalized_declared_name)`。kind 闭集为 2000/2003 RTP、RGSS1/2/3 标准包和 RGSS custom。 |
| `runtime_asset_pack_installations` | `id/definition_id/files_digest/file_count/total_bytes`、可空 `bundle_blob_id/bundle_sha256`、`status VALIDATING/READY/FAILED/DELETE_PENDING/DELETED`、`diagnostic_json/source_note/creator`、`version` 与验证/删除时刻；内容身份不可修改，同一定义允许多个 READY 版本，状态变更必须使用当前 version。 |
| `runtime_asset_pack_files` | `(installation_id,path) PK`、连续唯一 ordinal、Blob/size/SHA；READY installation 必须拥有与 filesDigest 一致的完整集合。 |
| `game_variant_revision_runtime_packs` | `(game_variant_revision_id,slot) PK`、声明名/规范名、definition/installation；EasyRPG 仅 slot 0，RGSS 仅 RTP1..3 的 1..3。被任何 Variant 或可恢复 checkpoint 引用的 installation 不得删除或替换。 |
| `rpgmaker_runtime_validations` | `id/import_item_id/review_version_at_create/runtime_binding_revision/effective_source_snapshot_id/project_fingerprint/core_id/generation`、可空 `evidence_generation`、`evidence_confidence/route_key/artifact_id/artifact_set_sha256/adapter_id/adapter_abi/dependency_snapshot_sha256`、可空 `launch_id`、可空且必须不同的 `restore_launch_id`、`state/last_gate_sequence/machine_gates_json`、可空 screenshot/failure/decision actor/note 与时刻/15 分钟 expiry。`launch_id` 只允许在尚未签发 Launch 的 CREATED，或签发前收口的 FAILED 中为空；对外 FAILED 前置失败投影为 `launchId:null`。冻结绑定字段不可修改。状态只允许 `CREATED→STARTING→RUNNING→CHECKPOINTED→RESTORED→AWAITING_DECISION→PASSED|FAILED`，任意未终态可到 FAILED/EXPIRED。partial unique 保证每个 `(import_item_id,runtime_binding_revision)` 至多一个 PASSED。 |
| `rpgmaker_runtime_validation_gate_events` | `(validation_id,sequence) PK`、全局唯一 `event_id`、`launch_id`、`gate/phase/observed_at_ms/evidence_json/created_at_ms`；append-only。sequence 从 1 连续递增，14 个 gate 按 HTTP 专题顺序逐项 BEGIN→PASS/FAIL；原 Launch 与 restore Launch 的 gate 范围、同 gate 终态唯一性和 evidence 上限由 trigger/service 双重校验。位置证据包含初始 A、保存 B、继续状态 C、恢复 B 和恢复后输入位置。 |
| `rpgmaker_runtime_validation_checkpoints` | `validation_id PK/FK`、`payload_blob_id/payload_kind/native_profile/resume_slot/payload_sha256/size_bytes/created_at_ms`；每次验证至多一个，只能供同 validation 的 restore Launch 读取，绝不进入用户 `/saves`。验证终态事务删除该行，payload 进入宽限 GC；机器 gate 和恢复截图保留。 |
| `isolated_runtime_bootstrap_tickets` | `ticket_sha256 UNIQUE/launch_id/profile_id/expected_origin/expires_at_ms/consumed_at_ms`；原值至少 256 bit 且只在 LaunchConfig 返回，数据库只存摘要，一次消费、60 秒到期。 |
| `isolated_runtime_capabilities` | `credential_sha256 PK/launch_id UNIQUE/profile_id/expected_origin/issued_at_ms/expires_at_ms/revoked_at_ms`；只对应 runtime host-only HttpOnly cookie，origin/profile/expiry 与 Launch/ticket 必须一致，Launch 终止时撤销。同一 Launch 页面刷新只能复用未撤销且未过期的现有 capability；不创建第二张 ticket、不改 issued/expiry，也不让服务端仅凭行存在冒充浏览器实际持有 cookie。 |

pack kind 只允许 `RPG2000_RTP|RPG2003_RTP|RGSS1_RTP_STANDARD|RGSS2_RTP_RPGVX|RGSS3_RTP_RPGVXAce|RGSS_CUSTOM_RTP`。内置 definition ID/声明名一一固定为 `rpg2000_rtp/RPG2000_RTP`、`rpg2003_rtp/RPG2003_RTP`、`rgss1_standard/Standard`、`rgss2_rpgvx/RPGVX`、`rgss3_rpgvxace/RPGVXAce`；前两个声明名只作内部键而不与项目 INI 匹配。`RGSS_CUSTOM_RTP` 只允许 XP/VX/VX Ace，必须带 generation 和非空 declared name；以 `(generation,NFKC-casefold(name))` 复用已有 custom definition，与 builtin/另一名称冲突时 409。同一 definition 可有多个 READY installation：零个是 missing，一个由服务冻结，多个在管理员选定前是 ambiguous；新上传总是新 installation，不覆盖旧 bytes。

EasyRPG pack 最多 10,000 文件/512 MiB，必须至少有一个锁定 `easy-rtp-layout-v1` 登记资源，全部路径和扩展均落在该 category/Player 可解码集合内。pack 和 RGSS 游戏 `.mkxpz` 使用同一确定性 Store ZIP：按 UTF-8 path bytes 升序、DOS 时间 `1980-01-01 00:00:00`、Unix mode `0644`、无 archive/entry comment、extra field 或 symlink。RGSS RTP 单独挂载为 `mkxp-z/RTP/<declaredName>.mkxpz`，游戏 archive 不内联 RTP，`Game.ini` 不重写。

`NATIVE_SAVE_BUNDLE_V1` 固定为 8-byte ASCII `RTRPGSV1` magic、4-byte unsigned big-endian canonical manifest 长度、RFC 8785 manifest 和连续 entry payload。manifest 只含 `schemaVersion=1`、`engine RPG2000|RPG2003|RPGMV|RPGMZ`、正 `resumeSlot` 和 `entries[]`；每项严格为 `{store,key,mediaType,offset,sizeBytes,sha256}`，store 只允许 `FILESYSTEM|LOCAL_STORAGE|LOCALFORAGE|RETROM_NATIVE`。entries 按 `(store UTF-8 bytes,key UTF-8 bytes)` 严格升序且唯一；offset 必须从 0 连续覆盖全部 payload，无空洞/重叠/尾随 bytes，逐 entry SHA-256 必须复算。`FILESYSTEM` key 使用 SAFE_LOGICAL_PATH；其他 key 必须是无 NUL 的 NFC UTF-8 且至多 1 KiB；mediaType 必须命中对应 native profile registry。EasyRPG resume slot 恰为 100；MV/MZ 恰为保存时冻结且不与已有 slot 冲突的 `DataManager.maxSavefiles()+1`。最多 512 entries、manifest 256 KiB、native payload 64 MiB；mkxp `RUNTIME_STATE` 上限 256 MiB，既有 EJS 为 64 MiB。服务端在写入和每次下载前都重验 bundle 结构、entry 摘要与外层 Blob 摘要。

runtime validation 的位置 gate 固定比较 `{mapId,playerX,playerY,fixtureState}`：原 Launch 持久化初始 A，移动/改变变量后冻结与 A 不同的保存点 B，创建 checkpoint 后继续到与 B 不同的 C，再结束；不同 `restore_launch_id` 恢复后全部字段必须等于 B 且不同于 A/C。恢复截图关联并 PASS 后还必须继续真实输入，持久化与恢复位置不同的 RESTORE_INPUT；只有该 gate PASS 才可进入 `AWAITING_DECISION`。完整只读投影同时返回 initial/restore-input 位置、restoreInputVerified 以及原/恢复 Launch ID。Blob 摘要相等、load 返回成功、同一进程恢复或只看一张近似截图均不能满足高级 gate。发布事务逐字段比较当前 binding revision/source fingerprint/core/route/artifact/ABI/pack 与已分配原始 `launch_id` 的记录，任何漂移都返回验证必需；是否继续完成 PASSED 由管理员选择，七核心自动化验收仍必须完成全部 gate。

## 26. 统一验收入口

schema 与整数时间由 `ACC-DB-*` 覆盖；唯一归属由 `ACC-PLAT-*`；不可变 revision 与删除由 `ACC-GAME-*`、`ACC-SAVE-*`；Pegasus/VIDEO 由 `ACC-PEG-*` 与 `ACC-MEDIA-001`；EmulationStation schema、状态、来源互斥、handoff 与释放由 `ACC-ES-002`–`004`；标签由 `ACC-TAG-001`–`005`；状态机、lease 与快速审批由 `ACC-IMP-*`；凭据 hash 与内容授权由 `ACC-SEC-002`；沉浸模式对既有 Favorite/SaveState/Launch/PlaySession 的复用、`title_initial` 写入与排序、显式存档及无自动存档副作用由 `ACC-IMM-001`–`012` 覆盖；RPG Maker 七版本 core、内容/绑定分层、pack、validation、unique-origin capability 和通用 checkpoint 由 `ACC-RPG-001`–`012` 覆盖。
