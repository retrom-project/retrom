# 一期数据库实体与不变量

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期实施基线 |
| 版本 | 1.2 |
| 日期 | 2026-08-10 |
| 数据库 | SQLite，单个 `retrom` 写进程 |

## 1. 标识、版本与删除

- `platforms.id` 和 `cores.id` 是代码种子字符串；账号版本不再 seed `profiles.id='local'`。User 与 Profile 均使用小写 UUIDv7 `TEXT`，一一绑定且创建后不可变；其他业务实体主键同样使用小写 UUIDv7 `TEXT`。
- 管理侧可修改资源至少包含 `version INTEGER NOT NULL DEFAULT 1`；每次成功修改在同一事务 `version = version + 1`。HTTP 使用 `ETag/If-Match`。
- 业务时刻统一为 `*_at_ms INTEGER` Unix 毫秒；除下表明确以 `fetched_at_ms` 等领域时刻表示首次落库时刻外，带独立业务实体 ID 的表有 `created_at_ms`，可修改实体另有 `updated_at_ms`。复合键关系/明细表是否带创建时刻由下表逐一规定，不能从本句推断额外列。禁止 TEXT 时间和 `CURRENT_TIMESTAMP`。
- Game、PlatformInstance 等有历史引用的实体使用状态和 `deleted_at_ms` 软删除。Blob、不可变 revision、ReviewEvent、AuditEvent 不因软删除级联删除。
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
| `cores` | `id PK`、`name`、`requires_threads`、`enabled`、`created_at_ms/updated_at_ms`；35 个稳定 code seed。`dosbox_pure`、`mednafen_psx_hw`、`ppsspp`、`azahar` 的 `requires_threads=1`。 |
| `core_artifacts` | `id PK`、`core_id FK`、`emulatorjs_version`、`bundle_version`、`flavor WASM/THREAD_WASM/OVERRIDE`、`relative_path`、`size_bytes`、`sha256`、可空 `source_commit`、`provenance_json`、`compatibility_config_json`、`enabled`、`version`、`created_at_ms/updated_at_ms`；`UNIQUE(core_id, emulatorjs_version, sha256)`、`UNIQUE(emulatorjs_version, relative_path)`，另以 partial unique index 保证每个 core 至多一条 `enabled=1`。`relative_path` 是相对 `<RETROM_DEPENDENCY_ROOT>/runtime/emulatorjs/<emulatorjs_version>` 的规范相对路径，不得含 `..`；不同版本可合法复用 `data/cores/mgba-wasm.data` 等路径。active 依赖 manifest 的 `selected_core_artifacts` 必须让每个 enabled Core 恰有一条 enabled artifact；保留版本的历史 artifact 为 disabled，但精确引用仍可通过对应版本静态路由启动。路径/hash 必须命中该版本 manifest，`mame2003` override 是该 core 当前唯一 enabled 行；上游未提供 commit 时必须在 provenance 保留证据等级，不能伪造值。active DAT 等 artifact 级配置切换会递增 version。 |
| `platform_cores` | `(platform_id, core_id) PK`、`enabled`；默认核心和 Variant 的 core 必须引用 enabled 关系。 |
| `platform_instances` | `id PK`、`platform_id`、`default_core_id`、`name`、规范化 `slug`、`description`、`sort_order`、`enabled`、`version`、`created_at_ms/updated_at_ms`、可空 `deleted_at_ms`；`UNIQUE(platform_id, slug)`。复合 FK 保证默认 core 存在于 PlatformCore，trigger/service 额外保证其 enabled。 |

`slug` 由服务端创建时从展示名生成小写 ASCII 标识，冲突时追加数字后缀，必须匹配 `^[a-z0-9]+(?:-[a-z0-9]+)*$` 且不超过 80 byte；创建后不可改，软删除也不释放。展示名可改。游戏目录的 `platform_id` 创建后不可改。

`core_artifacts.provenance_json` 固定为 `{"schemaVersion":1,"dependencyManifestSha256":"<64 lowercase hex>","manifestEntryPointer":"/emulatorjs/selected_core_artifacts/<index>","sourceAssociationStatus":"EXACT_COMMIT|EMBEDDED_VERSION|INFERRED_BUILD_TIME|RELEASE_ONLY","sourceUrl":null|string,"notes":[]}`；notes 只存简短证据说明，不存宿主路径。`source_commit` 只在证据能锁定 40 位 Git commit 时填写，RELEASE_ONLY 必须为空。

`compatibility_config_json` 固定为 schema V2：`runtimeCoreId`、`requestedArtifactBasename`、`canvasResizePolicy`、`defaultOptions`、`persistentSaveMode`、可空 `persistentSaveKind`、`inputMode` 与 `startupActions` 全部由依赖 manifest 明示。`persistentSaveMode` 只允许 `SINGLE_FILE|DOS_OVERLAY|NONE`，前两种的 kind 分别固定为 `CORE_SAVE|DOS_OVERLAY`，NONE 必须为 null；`inputMode` 只允许 `STANDARD|POINTER`。启动动作最多 4 条，只允许有界的 `GAME_START/PRESS_CONTROL` 整数字段，`delayMs` 上限 30,000、`durationMs` 上限 1,000。basename 是所属 EmulatorJS 版本 loader 实际请求的 key；线程产物也使用其真实 `*-thread-wasm.data` basename。v4.2.3 只有 `mame2003` 使用 `ON_GAME_START_TO_CSS_PIXELS`，NDS 三核心与 Azahar 为 POINTER，PPSSPP 有 2 秒/5 秒两条 120ms 确认动作，Beetle VB 的四条确认动作延迟为 2/4/15/25 秒。前后端不得按 core ID 补默认值。

## 3. Blob 与不可变游戏 revision

| 表 | 必需字段与约束 |
| --- | --- |
| `blobs` | `id UUIDv7 PK`、唯一小写 `sha256`、`size_bytes`、实际 bytes 的非空小写 `md5/sha1/crc32`、检测后的 `media_type`、`created_at_ms`；物理路径只由 SHA-256 推导。 |
| `games` | `id PK`、非空 `platform_instance_id`、`status PUBLISHED/DELETED`、非空 `current_metadata_revision_id`、非空 `current_content_revision_id`、非空 `search_text`、`version`、`created_at_ms/updated_at_ms`、可空 `deleted_at_ms`；不得另存 `platform_id` 或 `default_core_id`。search_text 与 current metadata 必须同事务更新；改变目录默认 core 不改变 current content。 |
| `game_metadata_revisions` | `id PK`、`game_id`、非空 title、非空但可为空串的 description/developer/publisher/genre、可空 players/release_year、`source_kind IMPORT_REVIEW/ADMIN_EDIT/RESCRAPE_APPLY/SERVER_PEGASUS_IMPORT`、可空 `source_ref_id`、`created_at_ms`；创建后 append-only。IMPORT_REVIEW 的 ref 必须是发布该 Game 的 ImportItem；SERVER_PEGASUS_IMPORT 的 ref 必须是已交接到该 ImportItem、正处于审核发布边界的 Pegasus Item；RESCRAPE_APPLY 必须是被应用且属于该 Game/current ContentRevision 有效 run 的 ScrapeCandidate；ADMIN_EDIT 的 ref 必须为 NULL，精确修改另由同事务 AuditEvent 关联 Game/revision ID。 |
| `game_assets` | `id PK`、`game_id`、`metadata_revision_id`、`blob_id`、`kind COVER/BACKGROUND/SCREENSHOT/VIDEO`、`ordinal`、`width_px/height_px/media_type`、`created_at_ms`；`UNIQUE(metadata_revision_id, kind, ordinal)`。图片尺寸为正且 MIME 限 `image/png|image/jpeg|image/webp`；VIDEO 只允许 ordinal 0、`video/mp4|video/webm` 且尺寸为 null。每个 MetadataRevision 拥有完整媒体清单，未改媒体时复制旧 Blob 引用为新 Asset；URL 使用不可变 asset ID。 |
| `game_content_revisions` | `id PK`、`game_id`、`source_kind IMPORT_REVIEW/ADMIN_REPLACE/SERVER_PEGASUS_IMPORT`、非空 `source_ref_id`、`source_manifest_json`、`source_manifest_digest`、`created_at_ms`；append-only。IMPORT_REVIEW ref 指向被 Approve 的 ImportItem，SERVER_PEGASUS_IMPORT ref 指向与该 ImportItem 一一关联的 Pegasus Item，ADMIN_REPLACE ref 指向 `GAME_FILE_REVISION` Job。它只表示一次已接受的用户内容版本，不包含 core、DAT 或派生启动包；同一 bytes 再次替换仍可形成新 revision，CAS Blob 仍去重。 |
| `game_content_files` | `(game_content_revision_id, role, logical_name) PK`、`blob_id`、可空 `source_archive_blob_id/source_archive_entry_ordinal`、`sort_order`；两个 source archive 字段同时空或同时非空，并复合引用对应 ArchiveEntry，其 `materialized_blob_id` 必须等于 `blob_id`。role 仅 `CONTENT/DOS_SOURCE/COMPANION`。逻辑名是安全规范相对路径；主机/掌机平台只允许一个 CONTENT，DOS 使用 DOS_SOURCE，Arcade CONTENT 是本机 ROMset ZIP，审核确认与其同属 bundle 的 parent/BIOS/base 源 archive 以 COMPANION 保留。 |
| `game_variants` | `id PK`、`game_id`、`core_id`、可空 `current_revision_id`、`version`、`created_at_ms/updated_at_ms`；`UNIQUE(game_id, core_id)`，表示稳定逻辑槽，不承载可变文件。只有从未产生 READY 结果、仅保存失败验证证据的备用 core 槽允许 current 为空；发布所用默认 core 槽必须非空。 |
| `game_variant_revisions` | `id PK`、`game_variant_id`、非空 `game_content_revision_id`、`core_artifact_id`、可空 `dat_version_id`、`validation_input_digest`、可空 `emulator_game_id INTEGER UNIQUE`、`status READY/BLOCKED/INCOMPATIBLE`、`compatibility_code`、`dependency_snapshot_json`、可空 `default_dos_entry`、`created_at_ms`；`UNIQUE(game_variant_id, validation_input_digest)`，完成后 append-only。content revision 必须属于同一 Game。只有 READY revision 可被 current 指向且必须有正 `emulator_game_id`；非 READY 必须没有该 ID，且永不成为 current。旧 READY 是否已被替代由 `game_variants.current_revision_id` 推导，不回写状态。 |
| `variant_files` | `(game_variant_revision_id, role, logical_name) PK`、`blob_id`、`sort_order`；只保存 core-specific/派生文件，role 仅 `PARENT/BIOS_BUNDLE/DOS_LAUNCH_BUNDLE`；用户原始内容只能从 `game_content_files` 读取。相同 ContentRevision 的 DOS artifact 重校验必须引用既有确定性 `DOS_LAUNCH_BUNDLE`，不得要求一个不存在的 CONTENT 行或重新物化 Blob。一期不预留未实现的 patch role。 |
| `dos_entries` | `(game_content_revision_id, normalized_path) PK`、`original_relative_path`、`kind EXE/COM/BAT`、`rank`、`enabled/direct_launch_safe`；路径大小写比较采用 ASCII case-insensitive，冲突在导入时阻断。`direct_launch_safe=1` 表示安全扫描后可写入确定性 `dosbox.conf [autoexec]`，不将宿主命令或未验证字符串直接拼入。 |
| `archive_entries` | `(archive_blob_id, ordinal) PK`、`original_relative_path/normalized_path/ascii_casefold_path`、`archive_format ZIP/SEVEN_Z`、`compression_profile STORE/DEFLATE/SEVEN_Z_DECODER_VALIDATED`、`uncompressed_size_bytes`、`crc32/md5/sha1/sha256`、可空 `materialized_blob_id`、`created_at_ms`；`UNIQUE(archive_blob_id, normalized_path)` 与 `UNIQUE(archive_blob_id, ascii_casefold_path)`。路径使用存储专题 `SAFE_LOGICAL_PATH_V1`；casefold 只映射 ASCII A-Z。只记录经过安全扫描且非目录、非加密、非 symlink/device 的 regular-file entry；ZIP 只允许 Store/Deflate，7z 只接受隔离 worker 完整读取校验的 profile。扫描时对实际解压 bytes 计算四种 hash，但只在领域需要独立 member 时物化到 CAS。除 `materialized_blob_id` 可以在校验 Blob size/四种 hash 全部等于该 entry 后一次性从 NULL 提升为该 Blob ID 外，其他字段永不可更新。常规 CRUD 永不允许删除；唯一例外是存储专题定义的 owner-GC。 |

审核发布在一个事务创建 GameContentRevision、默认核心的 READY VariantRevision，并闭合 Game/Variant 两个 current pointer。Archive member 物化先在事务外流式写 CAS，再以 `materialized_blob_id IS NULL` 为条件的短事务验证 size/CRC32/MD5/SHA-1/SHA-256 并提升；并发已提升时必须逐项等于同一 Blob，否则为完整性故障。崩溃留下的无引用物化 Blob 交给 GC，不在事务内重解压。管理侧替换游戏文件先创建 `GAME_FILE_REVISION` Job，并以 whole-session consumption 独占 Upload；Worker 在事务外完成归档/格式/依赖验证。只有新内容对任务快照中的目录默认 core 验证 READY，且提交时 Game current content、目录/default core/version 仍等于快照，才在一个短事务创建新的 GameContentRevision/ContentFiles/VariantRevision，并切换 `games.current_content_revision_id` 与目标 `game_variants.current_revision_id`；配置或内容基线变化使 Job 以 retryable conflict 失败，手动重试会显式刷新快照。其他失败也不创建这些 revision、不改变 current，但 Job/Upload consumption 保留为审计和重试依据。其他 core 槽仍保留旧 revision，但因 content revision 不匹配而显示 `NEEDS_VALIDATION`。BLOCKED/INCOMPATIBLE VariantRevision 只用于既有 GameContentRevision 在备用 core 或新依赖配置下的幂等诊断，不能成为 current。存档、Launch、PlaySession 都引用精确 READY VariantRevision ID；它再唯一确定 GameContentRevision。术语中的“GameVariant revision”一律指该实体，不是可变行上的整数。

`game_content_revisions.source_manifest_json` 是按 `(role, logicalName UTF-8 bytes)` 排序的数组，仅含本次用户内容来源：每项固定为 `role`、规范 `logicalName`、实际 GameContentFile 的 `blobSha256/sizeBytes`，以及同时出现或同时缺省的 `sourceArchiveSha256/sourceArchiveEntryOrdinal`；不得只保存一个无 archive 标识的 ordinal。role 只允许 `CONTENT/DOS_SOURCE/COMPANION`，不含运行时生成的 BIOS/parent/DOS launch bundle。JSON 用 RFC 8785 canonical form编码，`source_manifest_digest` 为其 lowercase hex SHA-256。`games.current_content_revision_id` 是普通启动唯一的 canonical source lineage；改变目录默认 core、活动 DAT 或 artifact 不能隐式改变它。core 的 READY 结果必须直接引用该 ContentRevision；引用其他内容版本的旧 current 不可用于普通启动。

`validation_input_digest` 是下列 RFC 8785 canonical object 的 lowercase hex SHA-256：GameContentRevision 的 id 与 `sourceManifestDigest`；CoreArtifact 的 id/version/SHA-256；可空 DatVersion 的 id/version/SHA-256；按 requirement ID UTF-8 byte 排序的“对该 content 适用或按 BIOS 专题规定已安装即装入”的 BIOS requirement id/version/logicalName/catalog digest/activation options，与当时 active installation 的可空 Blob SHA-256/status/version；按逻辑 archive 名排序的 companion/parent source Blob SHA-256；以及 `validatorVersion`。它不包含线程、全屏等客户端能力，也不把明确不适用的静态可选固件混入。同一内容 revision + 依赖输入的并发验证依靠唯一约束只保存一个结果；新的 ContentRevision（即使 bytes 相同）、适用 BIOS 安装、活动 DAT、artifact 或验证算法变化都会产生新 digest，允许重新验证而不覆盖旧证据。READY revision 的 `dependency_snapshot_json` 必须足以按这些值重建当时 bundle与合并后的 core options；活动配置后来变化不使旧 READY 自动失效。

`search_text` 的生成算法固定为：按字段指定顺序用一个 U+0020 连接，执行 Go `strings.ToLower`，把每段连续 `unicode.IsSpace` 折叠为单个 U+0020，再 trim；不执行语言相关 collation、拼音、分词或 SQLite `NOCASE` 猜测。Game 字段顺序为 current title/developer/publisher/genre；ImportItem 为候选 title 与 source relative paths。query `q` 用同一算法后以 `instr(search_text, :q) > 0` 匹配，空 query 不加条件。未来升级正规化算法必须新增 schema migration 重建两列和 cursor contract，不能只改 Go 函数。

`games ↔ game_metadata_revisions`、`games ↔ game_content_revisions` 与 `persistent_saves ↔ persistent_save_revisions` 的循环外键，以及 GameVariant 非空 current pointer 的同槽引用，必须声明 `DEFERRABLE INITIALLY DEFERRED`，让创建/切换事务能先插任一侧并在 COMMIT 前闭合；Game 与 PersistentSave 不得把 current pointer 暂时设 NULL。GameVariant 仅允许在“尚无 READY 的备用 core 槽”持续为 NULL，不能在替换已有 READY 时先清空。`emulator_game_id` 只为 READY revision 分配 `1..9007199254740991` 的整数，在单写事务中取现有最大值 + 1；达到上限必须拒绝创建而非溢出。不可变 READY revision 不删除，因此不复用；API 以 JSON number 发送，使 `EJS_gameID` 保持 number 类型。

一期不支持 Arcade CHD/disk 内容：DAT 可以解析 `disk` 元素作为诊断证据，但导入发现运行所需 CHD 时返回 `UNSUPPORTED_CHD`，不创建假装可运行的 VariantRevision。Merged ROMset 同样返回 `UNSUPPORTED_MERGED_ROMSET`；只支持 Split 与 Full Non-Merged。

## 4. BIOS 与 DAT

| 表 | 必需字段与约束 |
| --- | --- |
| `bios_requirements` | `id PK`、`core_id/core_artifact_id`、`source_kind STATIC/DAT_MACHINE`、可空 `dat_machine_name`、`logical_name`、`requirement_mode REQUIRED/OPTIONAL/CONDITIONAL`、可空 `condition_code`、可空 `activation_options_json`、`delivery_kind BIOS_BUNDLE/EXTERNAL_FILE`、可空 `emulator_path`、`catalog_digest`、可空期望 `size_bytes/md5/sha1/sha256`、`source_url/source_version`、`enabled`、`version`、`created_at_ms/updated_at_ms`；`UNIQUE(core_artifact_id, logical_name)`，复合 FK 保证 artifact 属于同 core。BIOS_BUNDLE 的 path 必须为空，EXTERNAL_FILE 必须是规范绝对虚拟路径；交付方式和路径都进入 catalog/validation digest。STATIC 必须无 dat machine；DAT_MACHINE 固定为 bundle。DAT 切换按 slot upsert/disable 并在 digest 改变时递增 version，不删除历史 slot。 |
| `bios_installations` | `id PK`、`requirement_id/blob_id`、`original_filename`、实际 hash、`validated_requirement_version`、`status MATCHED/HASH_WARNING/MISSING_ENTRY/INVALID`、`validation_details_json`、`is_active`、`version`、`created_at_ms/updated_at_ms`；同 requirement 只允许一条 active。错误静态 hash 可 active 为 HASH_WARNING；可读 Arcade archive 若必需 entry 名齐全但 size/hash 有差异，同样 active 为 HASH_WARNING 并允许装入；完全缺少任一必需 entry 才是可 active 但阻断启动的 MISSING_ENTRY；损坏/不安全 archive 为 INVALID 且不可 active。Requirement version 改变时在 DAT 激活事务前完成有界重验证，并在激活事务更新 status/version。 |
| `dat_versions` | `id PK`、`core_id`、`core_artifact_id`、`source BUILTIN/USER`、可空 `builtin_relative_path`、可空 `blob_id`（两者恰一）、`sha256`、`parser_version`、`compatibility_status MATCHED/USER_CONFIRMED/UNKNOWN/INCOMPATIBLE`、`parse_status PENDING/PARSING/READY/FAILED/CANCELLED`、`is_active`、可空 `machine_count/rom_entry_count/disk_entry_count/bios_set_count/default_bios_set_count/explicit_bios_machine_count/base_dependency_target_count/unresolved_relation_count`、`version`、`created_at_ms/updated_at_ms`、可空 `parsed_at_ms/activated_at_ms`；每个 core artifact 只允许一条 active，另以 partial unique index 保证同一 `(core_artifact_id, sha256, parser_version)` 只有一条 BUILTIN 行，USER 即使 bytes 相同仍可保留独立来源记录。只有 `READY` 且兼容状态为 `MATCHED/USER_CONFIRMED` 可激活；只有 READY 时统计与 `parsed_at_ms` 非空，其他状态统计/时刻为空。内置版本必须为 `MATCHED`；用户版本若无法由已知 header/hash 证明，激活时需显式确认并审计后转为 `USER_CONFIRMED`，结构明确不符则永久 `INCOMPATIBLE`。 |
| `dat_machines` | `(dat_version_id, machine_name) PK`、description/year/manufacturer、cloneof/romof、explicit BIOS flag、`classification NORMAL/EXPLICIT_BIOS/ROMOF_INFERENCE`。 |
| `dat_bios_sets` | `(dat_version_id, machine_name, bios_name) PK`、`description`、`is_default`；同 machine 至多一个 default。MAME 实际 BIOS option 在此保存；FBNeo 当前为零行。 |
| `dat_rom_entries` | `(dat_version_id, machine_name, ordinal) PK`、`name/size_bytes`、可空 `crc32/sha1/status/merge_name/bios_name`；bios_name 非空时必须命中同 machine BiosSet；索引 machine 与非空 hash。现实 FBNeo 条目没有 SHA-1，NODUMP 条目还可同时没有 CRC/SHA-1，不得用 NOT NULL 或伪哈希填充；除 NODUMP 外至少必须有一个 CRC32/SHA-1。未声明 status 的 FBNeo entry 规范为 GOOD，NODUMP 不进入必需闭包，BADDUMP 进入闭包但生成 Warning。 |
| `dat_disk_entries` | `(dat_version_id, machine_name, ordinal) PK`、name、可空 SHA1/status；非 NODUMP disk 必须有 SHA-1，NODUMP 可空。一期只诊断，不表示 CHD 可运行。 |
| `variant_dependencies` | `(game_variant_revision_id, kind, logical_archive) PK`、`dat_version_id`、`kind PARENT/BIOS_OR_BASE`、`source_machine_name`、`required_entries_json`、`state SATISFIED_BY_CONTENT/SATISFIED_EXTERNAL/HASH_WARNING/MISSING/MISMATCH/UNSUPPORTED`、`created_at_ms`；append-only。DatVersion 必须等于 VariantRevision 锁定值，logical archive 固定为 `<source_machine_name>.zip`。`HASH_WARNING` 只允许 `BIOS_OR_BASE` 且所有必需 entry 名存在、至少一项 size/hash 不匹配；它可出现在 READY revision 并装入 bundle。`MISMATCH` 用于 parent 等必须精确匹配的内容并阻断 READY。 |
| `dat_import_jobs` | `job_id PK/FK jobs`、`dat_version_id UNIQUE`、可空 `base_dat_version_id`、可空 `diff_summary_json/diff_input_digest`、`created_at_ms/updated_at_ms`；Job 必须 kind=`DAT_PARSE`/scope=该 DatVersion。解析状态只使用通用 jobs 与 DatVersion `parse_status`，不复制 lease/attempt；`diff_*` 只保留最近一次成功物化的兼容摘要，不是差异查询事实源。激活/回滚可多次发生，只通过 AuditEvent 关联 DatVersion，不在本表伪存单一 activation ID。 |
| `dat_diff_snapshots` | `id PK`、`dat_version_id UNIQUE/FK`、可空 `base_dat_version_id`、`state PENDING/RUNNING/READY/STALE/FAILED`、`input_digest`、可空 `summary_json/impact_json/impact_digest/error_code`、`attempt_count/version`、`queued_at_ms/started_at_ms/completed_at_ms/created_at_ms/updated_at_ms`。input digest 锁定目标/base DAT、parser、CoreArtifact version、依赖平台和已发布 Variant 引用；只有 READY 同时拥有三项结果。进程重启把遗留 RUNNING 恢复为 PENDING，执行按单一 DAT worker 串行，扫描和 JSON 编码不占长写事务。 |
| `dat_diff_items` | `(snapshot_id, section, cursor_key) PK`、`change_kind`、`key_json/before_json/after_json`；section 固定为 `MACHINES/ROM_ENTRIES/BIOS_SETS/DEPENDENCY_TARGETS`，按 snapshot/section/change/cursor 建分页索引。后台比对按最多 1,000 行的短事务写入；HTTP GET 只查询这里，不重新扫描 DAT 索引。 |

Builtin DatVersion 读取只读 payload；用户 DAT 进入 CAS。切换 active DAT 只创建重校验结果/新 VariantRevision，不改写历史依赖快照。

BIOS Installation 不跨 CoreArtifact 自动复制：新 artifact 的 Requirement 初始没有 active installation；用户可再次选择同一 UploadFile/bytes 安装，CAS Blob 复用但 Installation 行与校验证据独立。旧 Requirement/Installation 在仍被历史依赖快照保护时保留。

DAT parse 状态投影固定为：Job 首次领取时 `PENDING→PARSING`；Job SUCCEEDED 时同一最终事务 `PARSING→READY`；Job 最终 FAILED/CANCELLED 时分别转 `FAILED/CANCELLED`。用户取消后的候选只能删除后重新上传，不能用通用 retry 复活 CANCELLED Job；FAILED 且 `error_retryable=true` 的通用人工 retry 在同一事务把 DatVersion 重置为 PENDING、清空失败统计/`parsed_at_ms` 并增加 Job execution。Worker 解析时允许在 DatVersion 仍为 PARSING 的前提下，每批最多 1,000 个规范行以短事务幂等插入索引表；API/DAT diff/Requirement 构建一律只读取 READY 版本。相同 bytes/parser 的恢复或 retry 遇到同主键时必须逐字段相等，否则以确定性 `DAT_NONDETERMINISTIC_PARSE` 失败；因此无需删除已提交的相同 partial rows。全部输入读完后先在事务外计算统计，再在最终短事务校验统计、发布 READY/active 指针与 Requirement。失败或取消留下的 partial rows不可见，删除未曾 active/引用的候选时与该 DatVersion 一并按 FK cascade 清除；不得把数十万索引行塞进一个长事务或先激活再解析。

`bios_requirements.catalog_digest` 是 lowercase SHA-256(RFC 8785 canonical JSON)。STATIC object 含 source kind、logical name、mode/condition、delivery kind、emulator path、canonical activation options、期望 size/hash 和 source version；DAT_MACHINE object 含 DatVersion SHA-256、machine name，以及按 `(entryName UTF-8 bytes,size,crc32,sha1)` 排序的必需非 NODUMP/default-bios entry。排序中可空 hash 以 JSON `null` 排在非空小写 hex 之前；同一 entry 的 canonical object 始终显式写 `crc32`/`sha1` 字段及其 `null`，不因上游缺少字段而改变 schema。`activation_options_json` 为可空 RFC 8785 object，最多 8 个 ASCII core-option key，每个值是最多 128 bytes 的 ASCII string；不能嵌套。适用的多个 requirement 按 ID 排序合并，重复 key 必须同值，否则 seed/parser 校验失败，不允许后写覆盖。`bios_installations.validation_details_json` 固定为 `{"schemaVersion":1,"missingEntries":[],"mismatchedEntries":[],"warnings":[]}`，entry 数组按逻辑名排序且只含 name/expected/actual hash/size，不含宿主路径。DAT activation 的影响 digest 覆盖 requirement enable/version/catalog/delivery/path 与 installation 新状态，防止预览后漂移。

`variant_dependencies.required_entries_json` 固定为 `{"schemaVersion":1,"entries":[...]}`，entries 按 `(name UTF-8 bytes,sizeBytes,crc32,sha1)` 升序，可空 hash 使用上述 `null` 排序规则。每项始终显式含 `name/sizeBytes/crc32/sha1/datStatus`（hash 可为 JSON `null`）、`resolution=CONTENT|EXTERNAL|MISSING|MISMATCH|UNSUPPORTED`、可空 `actualSizeBytes/actualCrc32/actualSha1/sourceBlobSha256`。entry 级 `MISMATCH` 在 `BIOS_OR_BASE` aggregate 中归并为 `HASH_WARNING`，在 `PARENT` aggregate 中仍归并为阻断性的 `MISMATCH`；缺名始终归并为 `MISSING`。不存宿主路径，不把 NODUMP 写成必需项；因此真正进入此数组的 entry 至少有 CRC32 或 SHA-1。`dat_diff_snapshots.summary_json` 固定为 `{"schemaVersion":1,"machines":{"added":0,"removed":0,"changed":0},"romEntries":{"added":0,"removed":0,"changed":0},"biosSets":{"added":0,"removed":0,"changed":0},"dependencyTargets":{"added":0,"removed":0,"changed":0},"warnings":0}`；`dat_import_jobs.diff_summary_json` 只镜像同一次 READY 结果。启用或回滚任一 DAT 时，同一 CoreArtifact 的全部 snapshot 原子转为 STALE、清空摘要和影响并删除 materialized items；下次必须显式或自动重新排队，旧 impact digest 不得复用。

## 5. 上传、导入与审核

| 表 | 必需字段与约束 |
| --- | --- |
| `upload_sessions` | `id PK`、`state CREATED/UPLOADING/FINALIZING/COMPLETE/FAILED/CANCELLED/EXPIRED`、`source_type FILES/DIRECTORY`、`total_files/total_bytes`、`manifest_digest`、`finalization_no`、可空 `finalize_job_id UNIQUE`、`version`、`expires_at_ms/created_at_ms/updated_at_ms`、可空 `unconsumed_pruned_at_ms/last_error_code`。`finalization_no` 初始为 0，每次接受 `POST complete` 的事务恰好加 1；manifest digest 是创建时对 source type 与按规范相对路径排序的 fileId/path/declared size 做 RFC 8785 SHA-256，之后不变。FINALIZING 必须引用 kind=`UPLOAD_FINALIZE`、同 scope 且 input `finalizationNo` 等于当前计数的 Job；COMPLETE 时所有 UploadFile 必须 COMPLETE。旧 finalization Job/事件永久保留，但 session 只指向当前一次。 |
| `upload_files` | `id PK`、`upload_session_id`、规范 `relative_path`、`declared_size_bytes/received_size_bytes`、可空 `final_blob_id`、`state PENDING/PARTIAL/FINALIZING/COMPLETE/FAILED`、可空 `last_error_code`、`created_at_ms/updated_at_ms`；同 session 规范路径唯一，只有 COMPLETE 可有 final Blob。 |
| `upload_parts` | `(upload_file_id, part_no) PK`、`offset_bytes/size_bytes/sha256`、相对于 `RETROM_DATA_DIR/tmp/uploads/` 的规范 `storage_key`、`created_at_ms`；key 满足 `SAFE_LOGICAL_PATH_V1`、不得包含 `tmp/uploads/` 前缀，且数据库唯一；partNo 从 0 连续，声明范围不重叠，完成后才要求无缺口。 |
| `upload_consumptions` | `id PK`、`upload_session_id`、可空 `upload_file_id`、`consumer_type IMPORT_JOB/GAME_FILE_REVISION_JOB/GAME_ASSET/REVIEW_ASSET/REVIEW_ARCADE_PARENT/BIOS_INSTALLATION/DAT_VERSION`、`consumer_id`、`created_at_ms`；`UNIQUE(consumer_type, consumer_id)`，并为 `upload_file_id IS NULL` 建 `UNIQUE(upload_session_id)` partial index。ImportJob 与 kind=`GAME_FILE_REVISION` 的通用 Job 必须令 file ID 为空并独占消费整个 session；其余类型必须令 file ID 非空且属于该 session。`REVIEW_ARCADE_PARENT` 只在 Attachment ACCEPTED 的提交事务写入并以 Attachment ID 为 consumer；REJECTED/FAILED 不消费，Upload cleanup 可按常规期限回收 bytes。整 session consumption 与任何其他 consumption 互斥；file-level consumption 之间可共存，一个 UploadFile 可被多个不同领域资源显式复用，但每次都留独立记录。有任何 consumption 的 session 不可取消；清理规则见下文，不能用“保留 session”误保留所有未消费 Blob。 |
| `import_jobs` | `id PK`、`upload_session_id UNIQUE`、`target_platform_instance_id/platform_instance_version/platform_id/default_core_id/core_artifact_id`、可空 `dat_version_id`、`metadata_provider HASHEOUS/NONE`、`config_snapshot_json/config_snapshot_digest`、`state`、`total_item_count/queued_item_count/running_item_count/review_pending_item_count/published_item_count/discarded_item_count/failed_item_count/cancelled_item_count/ignored_file_count/rejected_file_count/resolved_rejected_file_count/already_imported_item_count/already_imported_file_count`、可空自引用 `reconfigured_from_import_job_id`、可空 `last_error_code/cancel_requested_at_ms/cancel_reason`、`version`、`created_at_ms/updated_at_ms`、可空 `completed_at_ms`。`rejected_file_count` 是不可丢失的原始拒绝证据计数，`resolved_rejected_file_count` 只统计已经转入新配置任务的拒绝文件且不得超过前者，二者之差才是当前待处理文件数；`already_imported_item_count` 是 `discarded_item_count` 的可解释子集，`already_imported_file_count` 按只被这些跳过 Item 使用、未同时服务于其他 Item 的不同 UploadFile 计数。item 分类计数总和必须等于 total，与 Item 状态在同一事务更新；仅 CANCEL_REQUESTED/CANCELLED 可有 cancel 字段。`completed_at_ms` 仅在 `COMPLETED/CANCELLED/FAILED` 非空，其余状态（包括仍可重试、审核或取消收口的 `PARTIAL_FAILURE`）必须为空。一个完成的 UploadSession 只能消费为一个 ImportJob；重新配置时由服务端创建新的 COMPLETE 逻辑 UploadSession/UploadFile 并复用相同 CAS Blob，不能让两个 ImportJob 消费同一 session。 |
| `import_job_files` | `(import_job_id, upload_file_id) PK`、`disposition PENDING/SOURCE/IGNORED/REJECTED`、可空 `reason_code`、`created_at_ms/updated_at_ms`。创建 Import 时为 session 每个 UploadFile 建 PENDING 行；分组后每个文件必须有唯一终态 disposition。SOURCE 必须至少被一条 ItemSourceFile 引用且 reason 为空；IGNORED/REJECTED 必须有稳定 reason，并在任务页可见，不允许静默丢文件。 |
| `import_job_file_resolutions` | `(import_job_id, upload_file_id) PK/FK ImportJobFile`、`action RECONFIGURED`、`replacement_import_job_id FK ImportJob`、`actor_kind USER/SYSTEM`、可空 `actor_user_id/actor_label`、`created_at_ms`，append-only。USER actor 必须引用真实用户，SYSTEM actor 只能使用封闭 label；它保留原 REJECTED disposition/reason，同时证明该文件已由哪个新任务接管。只有新 UploadSession、replacement ImportJob、全部 resolution 与 source 聚合计数在同一创建事务成功时才算已处理。 |
| `import_items` | `id PK`、`import_job_id`、`group_key`、`state`、`source_manifest_json/source_manifest_digest`、非空 `search_text`、可空 `failed_stage HASHING/IDENTIFYING/SCRAPING`、可空 `last_error_code`、`version`、`created_at_ms/updated_at_ms`、可空 `completed_at_ms`；同 job/group key 唯一。`search_text` 按统一 Unicode 折叠算法包含 Item ID、规范来源 basename/common root 和当前 ReviewDraft title；首建 Item 时用前两项生成，创建或 PATCH ReviewDraft 时在同一事务刷新 title 投影，使待审队列搜索不依赖内存过滤。`failed_stage/last_error_code` 只在 FAILED_RETRYABLE/FAILED_FINAL 非空，其他状态都为空；`completed_at_ms` 仅在 `PUBLISHED/DISCARDED/FAILED_FINAL/CANCELLED` 非空，重试时不得沿用旧完成时刻。source manifest 使用 GameContentRevision 相同的 canonical 字段/排序但尚无 ContentRevision ID，digest 为 lowercase SHA-256。ReviewDraft 通过自身 unique item FK 查找，不在 Item 保存反向指针。实际 lease/attempt 只在通用 `jobs` work-unit 表保存。 |
| `import_item_source_files` | `(import_item_id, role, logical_name) PK`、`upload_file_id`、`blob_id`、可空 `source_archive_blob_id/source_archive_entry_ordinal`、`sort_order`、`created_at_ms`。role 仅 `CONTENT/DOS_SOURCE/COMPANION`；archive pair 同时空或非空，非空时必须指向该 UploadFile final Blob 的 ArchiveEntry 且其 materialized Blob 等于 `blob_id`；为空时 blob 必须等于 UploadFile final Blob。同一 UploadFile 可作为多个 Arcade Item 的 COMPANION，所以不对 upload_file_id 建唯一约束。Approve 逐行复制为 GameContentFile。 |
| `content_identity_claims` | `(platform_id, content_identity_digest) PK`、`created_at_ms`，append-only。digest 为基础平台加 source file `(role, blob SHA-256, occurrence count)` 规范集合的 lowercase SHA-256；不含文件名、UploadSession、archive wrapper 或 PlatformInstance。Approve 在写事务中先插入/命中 claim，再查询当前发布 Game 并决定冲突或发布，以序列化同一内容身份的并发决策。 |
| `import_item_duplicate_matches` | `(import_item_id, existing_game_id) PK`、`existing_game_content_revision_id`、`content_identity_digest`、`detected_stage=IDENTIFICATION`、`created_at_ms`，append-only。只记录识别阶段因当前未删除 Game 已使用完全相同内容而自动跳过的匹配，保留命中时 Game current ContentRevision；Item 必须转 `DISCARDED`，且不能创建 ReviewDraft。 |
| `import_item_dos_entries` | `(import_item_id, normalized_path) PK`、`original_relative_path`、`kind EXE/COM/BAT`、`rank`、`enabled/direct_launch_safe`、`created_at_ms`；ASCII case-insensitive 路径冲突阻断。Approve 时逐项复制到新 ContentRevision 的 `dos_entries`；ReviewDraft default 只能指向本 Item enabled entry。 |
| `import_item_core_validations` | `id PK`、`import_item_id/source_snapshot_id/target_platform_instance_id`、目录 `platform_instance_version`、`core_id/core_artifact_id`、可空 `dat_version_id/default_dos_entry`、`source_manifest_digest/prepublish_input_digest`、`status READY/BLOCKED/INCOMPATIBLE`、`compatibility_code/dependency_snapshot_json`、`created_at_ms`；`UNIQUE(import_item_id, prepublish_input_digest)`，append-only。source snapshot 必须属于同 Item，digest 必须与其 canonical manifest 一致；digest 使用 source manifest、目标目录/version、artifact/DAT、BIOS/companion、可空 default DOS entry 和 validator version 的 canonical SHA-256。它是发布前证据，不冒充最终 GameVariantRevision 的 `validation_input_digest`。 |
| `import_item_validation_files` | `(import_item_core_validation_id, role, logical_name) PK`、`blob_id/sort_order`、`created_at_ms`；role 仅 `PARENT/BIOS_BUNDLE/DOS_LAUNCH_BUNDLE`，保存审核前已完成的确定性派生/依赖 Blob。只有 READY validation 可被 ReviewDraft 选择；普通 Approve 复制所选 READY 引用，截图人工放行则复制当前阻断 Validation 中实际存在的引用，不在事务里重新打包。 |
| `metadata_scrape_runs` | `id PK`、恰一非空的 `import_item_id/game_id`、可空 `game_content_revision_id`、`job_id UNIQUE`、`provider HASHEOUS/NONE`、`provider_config_version`、`state RUNNING/COMPLETED/FAILED/CANCELLED`、`version`、`created_at_ms/updated_at_ms`、可空 `completed_at_ms/error_code`。`completed_at_ms` 在 `COMPLETED/FAILED/CANCELLED` 非空、在 `RUNNING` 为空；只有 FAILED 可有 `error_code`。Game owner 必须同时引用创建 run 时该 Game 的 ContentRevision，ImportItem owner 必须令 content revision 为空。每个 ImportItem 都创建独立 run；支持精确 hash 的内容创建零到多条 evidence，DOS、Arcade 无 eligible primary entry 或 provider=NONE 可合法为零。每个 run 都有 kind=`METADATA_SCRAPE` 的 Job；零 evidence 或 NONE 不发 provider 请求并在同一事务把 Job/Run 置为 SUCCEEDED/COMPLETED，NONE 也不得有 evidence/attempt/response/candidate。游戏重新刮削只允许 HASHEOUS；MISS 与完成了有界错误证据的 run 也是 COMPLETED，只有无法闭合领域证据的本地/Job 故障才为 FAILED。CANCELLED 的领域投影按导入专题区分父 Import 取消、初始 Run 单独取消和后续重刮削，不能把 ImportItem 留在 SCRAPING。 |
| `content_hash_evidence` | `id PK`、`scrape_run_id`、`profile RAW_FILE_V1/SINGLE_ARCHIVE_MEMBER_V1/ARCADE_DAT_ENTRIES_V1`、来源 `blob_id` 或 `(archive_blob_id, archive_entry_ordinal)`、可空 `crc32/md5/sha1/sha256`、`query_order`、`created_at_ms`；来源恰一、至少一个 hash 非空，`UNIQUE(scrape_run_id, profile, query_order)`。Game 重新刮削只从其 current ContentRevision 生成新 run/evidence，不复用旧 run 的“当前”语义。 |
| `metadata_provider_cache` | `(provider, request_digest) PK`、`current_response_id`、`expires_at_ms/updated_at_ms`；只作可变缓存指针，current response 必须具有相同 provider/request digest。 |
| `metadata_provider_responses` | `id PK`、provider、request digest、可空 HTTP status、`outcome HIT/MISS/RATE_LIMITED/TIMEOUT/INVALID_RESPONSE/NETWORK_ERROR`、可空 raw response Blob、`fetched_at_ms/expires_at_ms`；append-only，刷新写新行并切换 cache pointer。 |
| `metadata_scrape_query_attempts` | `id PK`、`scrape_run_id/content_hash_evidence_id/provider_response_id`、`attempt_no`、`source NETWORK/CACHE`、`created_at_ms`；`UNIQUE(content_hash_evidence_id, attempt_no)`。Evidence 必须属于同 run，response provider/request digest 必须与 evidence 规范请求一致；CACHE 固定 attempt 1，NETWORK 从 1 连续。MISS/错误也必须留下 attempt，从而不能因没有 Candidate 丢失 run→response 证据。 |
| `scrape_candidates` | `id PK`、`scrape_run_id`、`primary_response_id`、`provider_game_id`、`normalized_metadata_json`、`evidence_json`、`created_at_ms`；不可变，`UNIQUE(scrape_run_id, provider_game_id)`。同一 provider game ID 多次命中时，在 run 查询收集完成后按 `(evidence.query_order, attempt.attempt_no, response.id)` 升序的第一个合法 HIT response 固定为 primary，不受 Worker 并发完成顺序影响；文本和媒体都只从 primary 归一化。game/ROM 两个上游 score 只按导入专题写在封闭的 evidence JSON，不再复制一个含义不明且可能漂移的 `provider_score` 列。JSON schema 与字段映射以导入专题 `BY_HASH_V1` 为准。 |
| `scrape_candidate_hits` | `(scrape_candidate_id, query_attempt_id) PK`、`matched_hashes_json`、`created_at_ms`；记录 Arcade 多 entry 或多个 hash 对同一候选的全部命中；attempt 必须是同 run 的有效 HIT，provider response 由 attempt 唯一推导，不能再复制一个可能不一致的 query_order。 |
| `scrape_candidate_assets` | `id PK`、`scrape_candidate_id`、`provider_response_id`、`provider_asset_id`、`kind_hint COVER/BACKGROUND/SCREENSHOT/UNKNOWN`、`ordinal`、`source_path`、`status PENDING/FETCHING/READY/FAILED/CANCELLED`、可空 `blob_id/width_px/height_px/media_type/error_code/fetched_at_ms`、`version`、`created_at_ms/updated_at_ms`；`UNIQUE(scrape_candidate_id, provider_asset_id)`。source_path 只能是已校验的 Hasheous 相对 image path；READY 必须有合法 Blob/dimensions/media type，FAILED/CANCELLED 必须有 error，其他状态两者均空。 |
| `review_uploaded_assets` | `id PK`、`import_item_id/upload_file_id UNIQUE/blob_id`、`kind COVER`、`width_px/height_px/media_type`、`created_at_ms`；不可变。每个 UploadFile 最多生成一份审核资源，并通过 `REVIEW_ASSET` consumption 留下上传归属。仅允许 COMPLETE 上传中的 ≤10 MiB、≤40 MP PNG/JPEG/WebP；资源在 Apply 前可以只作为对比窗体暂存，不暗中改变草稿。 |
| `review_drafts` | `id PK`、`import_item_id UNIQUE/target_platform_instance_id`、非空 `effective_source_snapshot_id`、可空 `selected_validation_id/selected_candidate_id/cover_candidate_asset_id/cover_uploaded_asset_id/background_candidate_asset_id/default_dos_entry`、完整 `metadata_json`、`version`、`created_at_ms/updated_at_ms`；有效来源快照必须属于同 Item。候选封面和人工封面互斥，人工封面必须属于本 Item。仅在 Item 未最终决策时可改。metadata_json 固定为 title/description/developer/publisher/genre/players/releaseYear 的完整 object；不保存含义不明的“只改字段”JSON。首个 Metadata Run 按导入专题的固定 candidate/basename 规则创建且只创建一次；selected validation 必须 READY、属于同 Item/有效来源快照且精确匹配目录当前默认 core/config，default DOS entry 必须属于 Item。Approve 另要求 title trim 后 1–200 Unicode code points且无控制字符。 |
| `review_draft_screenshot_assets` | `(review_draft_id, ordinal) PK`、`candidate_asset_id`、`created_at_ms`；`UNIQUE(review_draft_id, candidate_asset_id)`，ordinal `0..31` 连续。cover/background/screenshot 可来自同一 Item 任意 COMPLETED HASHEOUS run 的 READY asset，允许人工混合媒体来源；selected candidate 只说明文本元信息来源。 |
| `review_events` | `id PK`、`import_item_id`、`event_type DRAFT_SAVED/TARGET_CHANGED/SCRAPE_REQUESTED/CANDIDATE_APPLIED/CANDIDATE_REMOVED/PARENT_UPLOAD_REQUESTED/PARENT_ATTACHMENT_ACCEPTED/PARENT_ATTACHMENT_REJECTED/APPROVED/DISCARDED`、`actor_kind USER/SYSTEM`、可空 `actor_user_id/actor_label`、`before_json/after_json/diff_json`、配置/DAT/provider evidence JSON、可空 `reason`、`created_at_ms`；append-only。每个 JSON 都使用带 schemaVersion 的 canonical object，完整记录 selected validation/run/candidate/asset/Attachment/source snapshot ID；Parent 事件只含 ID、machine、原文件名、observed hash/size、状态和稳定错误码，不含 ROM bytes 或宿主路径。 |

状态枚举固定为：

- Upload：`CREATED | UPLOADING | FINALIZING | COMPLETE | FAILED | CANCELLED | EXPIRED`；
- ImportJob：`QUEUED | RUNNING | REVIEW_PENDING | PARTIAL_FAILURE | COMPLETED | CANCEL_REQUESTED | CANCELLED | FAILED`；
- ImportItem：`QUEUED | HASHING | IDENTIFYING | SCRAPING | REVIEW_PENDING | PUBLISHED | DISCARDED | FAILED_RETRYABLE | FAILED_FINAL | CANCELLED`。

ImportJob 聚合规则按固定优先级执行：首次领取前为 `QUEUED`；有 queued/running Item 为 `RUNNING`；无运行项但 `failed_item_count>0` 或 `rejected_file_count-resolved_rejected_file_count>0` 为 `PARTIAL_FAILURE`；仅有待审核时为 `REVIEW_PENDING`；全部 Item 为 PUBLISHED/DISCARDED/CANCELLED 且没有未处理 rejected file 时才为 `COMPLETED`；任务级不可恢复故障才是 `FAILED`。显式取消的短事务写 cancel 字段，把 QUEUED/FAILED_RETRYABLE/REVIEW_PENDING Item 转 CANCELLED，并向运行中的通用 Job 请求取消：没有运行项时直接 `CANCELLED`，否则先 `CANCEL_REQUESTED`，最后一个 Worker 确认停止后才为 `CANCELLED`。一旦 cancel 字段存在，后续聚合不得落到 COMPLETED/PARTIAL_FAILURE；已 PUBLISHED/DISCARDED/FAILED_FINAL 的 Item 与 REJECTED 文件证据保持不变。只有 ignored sidecar 不会单独使 Job 失败；若没有任何 Item 且存在未处理 rejected file，Job 为 PARTIAL_FAILURE；全部拒绝文件已转入 replacement ImportJob 后原任务可 COMPLETED，但详情仍展示原 reason 与 replacement 链接。Approve 接受与目录当前 version/default CoreArtifact/DAT/BIOS input 完全匹配的 READY ImportItemCoreValidation，或同一当前来源/目标/CoreArtifact/generation 的阻断 Validation 第 5 秒截图；事务创建 Game/metadata/content/default-core READY VariantRevision、复制实际已有的 ValidationFiles/全部引用和 ReviewEvent，人工放行另记录 `REVIEW_SCREENSHOT_OVERRIDE` 与 screenshot ID。审批事务不能做 archive/ZIP/DAT 计算。Discard 不删除证据。

`POST complete` 只在短事务内递增 `finalization_no`、冻结该次 part 输入、创建新的 `UPLOAD_FINALIZE` Job、更新 `finalize_job_id` 并转 FINALIZING；Worker 在事务外按 file/partNo 顺序流式校验无缺口与重叠、组装、从实际 bytes 计算四种 hash 并原子发布 CAS。每个文件成功后在短事务转 COMPLETE，再删除该文件的临时 part bytes/行；全部成功才使 session COMPLETE。同一次 finalization 的可重试 I/O 错误由该 Job 自动/人工 retry（增加 execution）并跳过已 COMPLETE 文件；确定性缺失/损坏 part 使当前 Job、文件和 session FAILED。客户端只可重传错误明细列出的 part；重传使 session 回到 UPLOADING，之后以新 Idempotency-Key 再次 complete 会递增 `finalization_no` 并创建另一 Job，即使修复后的 part hash 与旧声明相同也不能复活或改写旧 Job。已 COMPLETE 文件不重组装。取消 FINALIZING 先使当前 Job 进入 CANCEL_REQUESTED，session 仍为 FINALIZING 且 API 暴露 `cancelRequested=true`；Worker 至少每处理 8 MiB 检查一次，在停止并完成 scratch 清理的短事务内才把 Job/session 置 CANCELLED。已发布但未被引用的 Blob 由 GC 回收，不能把“已请求取消”谎报成文件已清除。

`import_jobs.config_snapshot_json` 固定为 `{"schemaVersion":1,"platformInstance":{"id":"...","version":1,"platformId":"...","defaultCoreId":"..."},"coreArtifact":{"id":"...","version":1,"sha256":"..."},"datVersion":null|{"id":"...","version":1,"sha256":"..."},"metadataProvider":{"code":"HASHEOUS|NONE","configVersion":1},"biosCatalogVersion":1,"biosInputs":[]}`。Job 创建时把目标 artifact 的全部 enabled Requirement 拍入 `biosInputs`，按 requirement ID 排序，含 id/version/logicalName/mode/condition/catalog digest/activation options 与 active installation 的可空 id/version/blob SHA/status；它是完整冻结输入，不是假称每项都适用。每个 `import_item_core_validations.prepublish_input_digest/dependency_snapshot_json` 再按实际 primary content 使用与 GameVariant 相同的规则筛选该快照；任务期间 BIOS 改变不会让旧 Job 漂移，审核前的配置过期检查会要求新验证。`config_snapshot_digest` 是 Job-level RFC 8785 object 的 lowercase SHA-256；`biosCatalogVersion` 是静态 condition 算法/seed 的单调整数，任何规则变化必须升级并使在途 Job 显式过期。`import_items.group_key` 是 lowercase SHA-256(RFC 8785 canonical `{"schemaVersion":1,"platformId":"...","primaryUploadFileIds":[...],"logicalRoot":"..."}`)；ID 按 UTF-8 byte 排序，logicalRoot 为规范最小共同目录，单文件为其父目录或空串。同 Job retry 必须生成同一 key。

`POST /import-items/{id}/retry` 由 `failed_stage` 唯一分派：HASHING/IDENTIFYING 对既有 IMPORT_ITEM_PIPELINE Job 增加 execution，Item 回 QUEUED；SCRAPING 不重试不可变的旧 MetadataScrapeRun/Job，而是按原 ImportJob 的 provider/config snapshot 创建新 Run/Job并把 Item 置 SCRAPING。两种路径都在同一事务清空 failed 字段/`completed_at_ms`、递增 version 和追加事件；FAILED_FINAL 不可重试。不能仅凭“最近一个 Job”猜失败阶段。

Upload 清理规则固定为：未完成 session 在 24 小时后进入 EXPIRED；清理器不与持有有效 lease 的 FINALIZING Job 竞态，过期时先取消 Job 再清理 part。COMPLETE 且无 consumption 的 session 保留 7 天供创建消费者，随后进入 EXPIRED 并移除 UploadFile 对 Blob 的引用。whole-session consumption 保留该 session 的全部 UploadFile 作为 Import/替换 Job 证据；只有 file-level consumption 时，后台在 24 小时后删除未被任何 consumption 引用的 UploadFile 行/Blob 引用并设置 `unconsumed_pruned_at_ms`，但保留 session、被消费文件和审计关系。物理 Blob 是否删除仍统一交给引用扫描 GC。

### 5.1 Migration 019：审核来源快照与 Arcade Parent Attachment

| 表 | 必需字段与约束 |
| --- | --- |
| `import_item_source_snapshots` | `id PK`、`import_item_id/revision_no`、`source_manifest_json/source_manifest_digest`、`created_by IDENTIFICATION/ARCADE_PARENT_ATTACHMENT`、`created_at_ms`；同 Item revision 唯一，append-only。019 为所有旧 Item 以既有 canonical manifest 回填 revision 1；升级前必须复算并精确核对 manifest JSON/digest，漂移则 migration 失败。 |
| `import_item_source_snapshot_files` | `(source_snapshot_id,role,logical_name) PK`、`upload_file_id/blob_id`、可空 archive pair、连续 `sort_order`、`created_at_ms`；归属与 Blob/ArchiveEntry 约束等同初始 SourceFile，append-only。Blob registry 把这些行计作永久引用。 |
| `review_arcade_parent_attachments` | `id PK`、Item/Draft/base snapshot/machine/expected logical name/requiredBy/depth/CoreArtifact/DatVersion/UploadFile/原文件名/Job、状态、可空 accepted Blob/result snapshot/observed hash-size/error/finished time、diagnostics、version/time；状态仅 `QUEUED/RUNNING/ACCEPTED/REJECTED/FAILED_RETRYABLE/CANCELLED`，终态字段与转移由 trigger 约束。每 Item 对 `QUEUED/RUNNING` 建 partial unique；ACCEPTED 必须同时有 Blob、后继快照、observed 值和 `REVIEW_ARCADE_PARENT` consumption，其他终态不能伪造后继快照。 |

019 同时给 `import_item_core_validations` 增加非空 `source_snapshot_id`，给 `review_drafts` 增加非空 `effective_source_snapshot_id`，并把 `REVIEW_ARCADE_PARENT_VALIDATE→IMPORT_ITEM` 加入 Job kind/scope enum，把 Attachment 进度事件加入 JobEvent enum。新库直接建立约束；018 升级使用唯一受控的 foreign-keys-off rebuild，事务提交前必须执行 `foreign_key_check`，运行时代码不得提供通用关闭外键能力。Validation/SourceSnapshot/SourceSnapshotFile 均有 update/delete 阻断；GameContentRevision 从审核来源发布时，其 source digest 必须等于草稿有效快照 digest，旧应用不能忽略补充 Parent 后发布 child-only 内容。

## 6. Launch、存档与游玩时长

| 表 | 必需字段与约束 |
| --- | --- |
| `launch_sessions` | `id PK`、`profile_id/game_id/game_variant_revision_id/core_artifact_id`、可空 `save_state_id/dos_entry_path/persistent_save_base_revision_id`、非空 `return_to`、32-byte `credential_sha256 BLOB`、`state CREATED/ACTIVE/FINISHED/EXPIRED/REVOKED`、`bootstrap_expires_at_ms`、可空 `idle_expires_at_ms/activated_at_ms/finished_at_ms`、`hard_expires_at_ms/created_at_ms/updated_at_ms`、`version`；绝不保存明文 capability。persistent base 是创建 Launch 时该 Profile/Variant/kind 的 current revision 快照，可为空且之后不可改。ACTIVE 时 activated 必须非空；在 PlaySession 创建前 idle 必须为空，创建后必须非空。terminal 时 finished 必须非空。 |
| `launch_content_files` | 每个 LaunchSession 恰一条不可变运行内容锁定：`launch_session_id PK`、站内内容 URL 使用的 `logical_name`、`blob_id`、`format_version=SOURCE_V1|RETROM_DOS_DIRECT_ZIP_V1`、`created_at_ms`。普通内容锁定 SOURCE；DOS 的程序菜单/直接启动都锁定已有规范 bundle Blob 并使用 `RETROM_DOS_DIRECT_ZIP_V1`，entry 只在 `launch_sessions.dos_entry_path` 锁定。内容端点按该 entry 生成带受控 `AUTOBOOT.DBP`（直接启动）或 `DOSBOX.BAT`（程序菜单）的 seekable ZIP 虚拟视图，不创建 Blob、临时文件或额外数据库记录；两个保留名都不能由源包覆盖。migration 011 只负责清除旧实现曾物化的派生 DOS Blob；不能据此把当前 V1 虚拟视图降为 SOURCE。后续 Variant cache 或内容 current 变化不能令本次 URL 漂移。 |
| `launch_external_files` | `(launch_session_id, virtual_path) PK`、`logical_name`、`blob_id`、`created_at_ms`，并对 `(launch_session_id, logical_name)` 唯一；append-only。只保存 Variant 依赖快照中 `EXTERNAL_FILE` 的已验证 Blob，当前用于 MelonDS 三个 BIOS；活动安装切换不能改变已创建 Launch。 |
| `play_sessions` | `id PK`、`launch_session_id UNIQUE`、profile/game/variant revision、`started_at_ms/last_heartbeat_at_ms`、可空 `ended_at_ms`、`active_duration_ms`、`last_client_sequence`、`state ACTIVE/FINISHED/ABANDONED`、`version`、`created_at_ms/updated_at_ms`。 |
| `play_session_events` | `(play_session_id, client_sequence) PK`、`event_kind START/HEARTBEAT/FINISH`、`client_observed_at_ms/server_received_at_ms`、`running/visible/paused` 布尔、`accepted_duration_ms`、`created_at_ms`；append-only，用于幂等与审计。START 固定 sequence 0/accepted 0；其余必须连续。 |
| `save_states` | `id PK`、`profile_id/game_id/game_variant_revision_id/core_artifact_id`、可空 `dat_version_id/dos_entry_path/disc_index`、`state_blob_id`、非空 `screenshot_blob_id`、可空 `source_launch_session_id`、`name`、`active_duration_ms`、`version`、`created_at_ms/updated_at_ms`、可空 `deleted_at_ms`；状态与截图同事务创建。新建手动存档必须记录来源 LaunchSession，且 trigger 校验 Profile/Game/VariantRevision/CoreArtifact/DAT/DOS entry/盘号与来源启动一致；多盘必须记录范围内盘号，SINGLE/DOS 必须为空。历史迁移前存档允许来源为空，不能据此推断它属于某次游玩。软删除后保留引用 7 天，随后 GC 可物理删除该行并按引用规则回收 Blob；审核历史不依赖 SaveState 行。DOS entry 必须属于该 revision，DatVersion 必须等于 revision 快照。 |
| `persistent_saves` | `id PK`、`profile_id/game_variant_revision_id`、`kind CORE_SAVE/DOS_OVERLAY`、非空 `current_revision_id`、`version`、`created_at_ms/updated_at_ms`；`UNIQUE(profile_id, game_variant_revision_id, kind)`。kind 从锁定 artifact compatibility 推导；`dosbox_pure` 为 DOS_OVERLAY，`handy/prosystem/stella2014/ppsspp` 的模式为 NONE，不能创建 PersistentSave，其余当前核心为 CORE_SAVE。 |
| `persistent_save_revisions` | `id PK`、`persistent_save_id/blob_id/source_launch_session_id`、`client_sequence`、`source_event AUTO_INTERVAL/MANUAL_EXPORT/EXIT`、`created_at_ms`；`UNIQUE(source_launch_session_id, client_sequence)`，append-only，成功写新 Blob 后才切换 current。launch 必须与 PersistentSave 指向相同 Profile/VariantRevision；每个 launch 的 sequence 从 1 连续递增，重复 sequence 只能命中相同 Blob/event。首个 revision 只在 PersistentSave current 仍等于 Launch 的可空 base 时提升；后续 revision 只在 current 仍等于该 launch 上一 sequence revision 时提升，防止并发会话丢失更新。 |

heartbeat 带单调 `clientSequence` 和上一个区间的 `running/visible/paused` 状态；服务端只接受连续新序号，重复序号返回原结果，跳号返回冲突。单次可计入 delta 上限 45 秒；页面隐藏、模拟器暂停/未启动、超出 45 秒失联段计 0。异常关闭由最后一次已接受 heartbeat 截断。

所有私有表（LaunchSession、PlaySession、SaveState、PersistentSave 及其派生读取）都以认证 Principal 的 `profile_id` 参与 SQL predicate；管理员没有 owner bypass。创建 Launch 时把 Profile 固化到不可变 LaunchSession，后续 runtime play/save/persistent-save 只能从该 Launch 派生，客户端不得提交或覆盖 owner。私有 cursor 与 Idempotency-Key 同样绑定当前 User；跨账号复用必须返回不可用/404，而不能命中另一账号的数据或重放结果。

## 7. 通用任务、事件与审计

| 表 | 必需字段与约束 |
| --- | --- |
| `jobs` | `id PK`、`scope_type/scope_id`、`kind/dedupe_key`、`execution_no`、`payload_json`、`cancellable`、`state QUEUED/RUNNING/CANCEL_REQUESTED/SUCCEEDED/FAILED/CANCELLED`、`attempt_count/max_attempts`、`version`、`available_at_ms`、可空 `execution_started_at_ms/execution_deadline_at_ms/leased_until_ms/heartbeat_at_ms/finished_at_ms/worker_id/error_code/error_retryable/cancel_requested_at_ms/cancel_reason`、`created_at_ms/updated_at_ms`；`error_retryable` 可空或布尔。`UNIQUE(kind, dedupe_key)`，可领取索引 `(state, available_at_ms)`。第一次领取某 execution 时原子写 started/deadline；自动 attempt/退避共享该 deadline，人工 retry 增加 execution 并重新置空二者。CANCEL_REQUESTED 不是终态：保留当前 lease，不能被普通 worker 重新领取，原 worker 在有界检查点停止后才转 CANCELLED；若 lease 到期，恢复器只能执行取消确认/清理，不能继续领域计算。只有终态可有 finished；只有 CANCEL_REQUESTED/CANCELLED 可有 cancel request 字段。kind/scope 映射严格固定为 `UPLOAD_FINALIZE→UPLOAD_SESSION`、`IMPORT_GROUP→IMPORT_JOB`、`IMPORT_ITEM_PIPELINE→IMPORT_ITEM`、`REVIEW_ARCADE_PARENT_VALIDATE→IMPORT_ITEM`、`DAT_PARSE→DAT_VERSION`、`VARIANT_REVALIDATE→GAME_VARIANT`、`METADATA_SCRAPE→SCRAPE_RUN`、`MEDIA_FETCH→CANDIDATE_ASSET`、`GAME_FILE_REVISION→GAME`、`BLOB_GC→BLOB`、`UPLOAD_CLEANUP→UPLOAD_SESSION`。IMPORT_GROUP 负责安全扫描/分组，并在每个 Item 创建事务中同时创建其 IMPORT_ITEM_PIPELINE Job。新的领域动作生成不透明 execution ID 作为新 Job dedupe key 的一部分；通用人工 retry 不改已存 Job/dedupe key，而是 `execution_no+1`、新建 InputSnapshot、重置 attempt 并追加事件。自动 retry 只增 attempt，不换 input snapshot。`METADATA_SCRAPE` 不允许通用 Job retry：一个 run 是不可混入新证据的用户批次，人工重试必须通过 review/game 领域端点创建新 Run/Job；Worker 只能在该 Job 最终失败前做有界自动 attempt。`REVIEW_ARCADE_PARENT_VALIDATE` 的 FAILED_RETRYABLE 允许通用 retry 复用同一 UploadFile，不能要求重新上传 bytes。 |
| `job_input_snapshots` | `(job_id, execution_no) PK`、`input_json/input_digest`、`created_at_ms`；append-only。execution 从 1 连续，`jobs.execution_no` 必须指向最新行。input 是下文固定的 canonical envelope，digest 为 lowercase SHA-256；自动 attempt 不新建行。 |
| `job_events` | 递增 `id INTEGER PK`、`job_id`、冗余且校验一致的 `scope_type/scope_id`、`event_type QUEUED/STARTED/PROGRESS/RETRY_SCHEDULED/CANCEL_REQUESTED/MANUAL_RETRY/SUCCEEDED/FAILED/CANCELLED`、`data_json`、`created_at_ms`；SSE 按 scope 过滤并用该全局稳定 event ID resume。 |
| `idempotency_records` | `(principal_id,operation_id,key) PK`、`request_digest`、`http_status`、`response_headers_json`、`response_body BLOB`、`created_at_ms/expires_at_ms`；登录写入的 `principal_id` 为 User ID，系统历史/维护命名空间为 `SYSTEM`。response body 是最大 1 MiB 的 SQLite BLOB，24h 后可清理。摘要绑定 principal User ID 但不得包含密码、session/CSRF 或 account-link capability；不得保存 Set-Cookie，Launch cookie 由本机 key 按 launchId 重派生。 |
| `audit_events` | `id UUIDv7 PK`、`actor_kind USER/SYSTEM`、可空 `actor_user_id/actor_label`、封闭 `action`、`resource_type/resource_id`、可空 `before_json/after_json/diff_json/request_id`、`created_at_ms`；append-only。USER actor 以外键引用永不硬删除的 User，SYSTEM actor 只能使用 `release-setup/offline-recovery/startup-test-bootstrap/restore-security-fence`；二者恰一成立。账户动作包括初始化、邀请、密码重置创建/消费/撤销、角色/状态/删除、改密、离线恢复与恢复围栏；普通登录/退出不写审计事件。JSON 禁止宿主绝对路径、IP、hash、cookie/capability 或 key material。 |
| `blob_gc_candidates` | `blob_id PK`、`first_unreferenced_at_ms/scheduled_at_ms`、可空 `deleted_at_ms/last_failed_at_ms/error_code`、`attempt_count`。每次扫描若恢复引用则删除 candidate 行但不删 Blob；仅连续无引用超过宽限后才删物理 bytes 和 Blob 行。 |
| `schema_migrations` | migration version PK、name、checksum、applied_at_ms；checksum 改变即启动失败。 |

`jobs.payload_json` 只是可领取队列指针，固定为 `{"schemaVersion":1,"inputExecutionNo":<positive integer>}`；不存宿主路径、Blob bytes、cookie 或可变业务副本。`job_input_snapshots.input_json` 通用外形为 `{"schemaVersion":1,"kind":"<JobKind>","scope":{"type":"<ScopeType>","id":"..."},"executionId":"<UUIDv7>","inputs":{...}}`。人工 retry 总是创建新的 execution/InputSnapshot 供审计，但只有 GAME_FILE_REVISION 按其领域规则刷新 Game/目录/依赖配置快照；UPLOAD_FINALIZE、IMPORT_GROUP、IMPORT_ITEM_PIPELINE、DAT_PARSE、VARIANT_REVALIDATE、MEDIA_FETCH、BLOB_GC 和 UPLOAD_CLEANUP 都保留原领域输入/digest，只更新 envelope executionId 与该 kind 明定的资源 version。依赖输入已变化而应重新验证时必须创建语义上的新 Job/dedupe key，不能借 retry 偷换。inputs 仅允许以下键，不在表内堆大清单：

- `UPLOAD_FINALIZE`：`uploadVersion/manifestDigest/finalizationNo/finalizationInputDigest`，最后一项对按 fileId/partNo 排序的 offset/size/part SHA-256 做 canonical digest；
- `IMPORT_GROUP`：`uploadSessionId/uploadVersion/manifestDigest/importConfigSnapshotDigest`；`IMPORT_ITEM_PIPELINE`：`importItemVersion/sourceManifestDigest/importConfigSnapshotDigest`；
- `DAT_PARSE`：`datVersion/version/datSha256/parserVersion/baseDatVersionId`；`datSha256` 对 BUILTIN 是已校验只读 payload、对 USER 是 CAS Blob，不能假设两者都有 `blob_id`；
- `VARIANT_REVALIDATE`：`gameVariantId/gameContentRevisionId/coreArtifactId/datVersionId/validationInputDigest`；`GAME_FILE_REVISION`：`gameVersion/baseContentRevisionId/uploadSessionId/platformInstanceId/platformInstanceVersion/coreArtifactId/datVersionId/configSnapshotDigest`；
- `METADATA_SCRAPE`：`scrapeRunId/provider/providerConfigVersion/ownerVersion/evidenceSetDigest`；`MEDIA_FETCH`：`candidateAssetId/assetVersion/providerResponseId/sourcePathDigest`；
- `BLOB_GC`：`blobId/blobSha256/firstUnreferencedAtMs`；`UPLOAD_CLEANUP`：`uploadId/uploadVersion/expiresAtMs`。

字段不适用时使用 JSON `null`，不允许同 kind 因实现分支漂移成两种 shape。大型 manifest/依赖集仍由快照 digest 定位领域表；Worker 开始时同时校验引用和 digest，不信任队列 JSON 里的重复值。JobEvent `data_json` 固定含 `schemaVersion/executionNo/attempt`，PROGRESS 另含 `phase/completedUnits/totalUnits/unit`，error event 另含稳定 `errorCode/errorRetryable`；不存堆栈或秘密。

`dedupe_key` 统一存为 lowercase hex SHA-256(`"retrom-job-dedupe-v1\0" || kind || "\0" || RFC 8785 canonical dedupe input`)。dedupe input 固定为：UPLOAD_FINALIZE 用 uploadId+finalizationNo+finalizationInputDigest；IMPORT_GROUP 用 importJobId；IMPORT_ITEM_PIPELINE 用 importItemId；DAT_PARSE 用 datVersionId+parserVersion；VARIANT_REVALIDATE 用 gameVariantId+validationInputDigest；METADATA_SCRAPE 用 scrapeRunId；MEDIA_FETCH 用 candidateAssetId；GAME_FILE_REVISION 用 gameId+input `executionId`；BLOB_GC 用 blobId+firstUnreferencedAtMs；UPLOAD_CLEANUP 用 uploadId+expiresAtMs。因此同一次上传终结的并发重放只命中一条 Job，part 修复后的新 `finalizationNo` 必然保留为另一条 Job；需语义收敛的 Variant 并发请求复用同一 Job，而独立的游戏文件替换动作使用不同 execution ID。不允许每个 repository 自行拼字符串。

`cancellable` 的默认值也是契约：UPLOAD_FINALIZE、IMPORT_GROUP、IMPORT_ITEM_PIPELINE、用户 DAT 的 DAT_PARSE、METADATA_SCRAPE、MEDIA_FETCH 与 GAME_FILE_REVISION 为 true；内置 DAT 的 DAT_PARSE、VARIANT_REVALIDATE、BLOB_GC、UPLOAD_CLEANUP 为 false。VARIANT_REVALIDATE 会被多个并发 Launch 按 digest 共用，某个 Player 退出等待只能取消自己的订阅/全屏 overlay，不能取消共享 Job。未来要改变此表必须先定义引用者计数与最后订阅者取消语义，不能只把按钮接到通用 cancel route。

Worker 默认并发：hash/copy 2、archive 1、DAT 1、metadata lookup 2、media 2、GC 1。lease 60 秒、heartbeat 15 秒；最多 4 次 attempt，基础退避 1s/5s/30s/120s，外部 `Retry-After` 可覆盖但上限 15 分钟。时间逻辑必须注入 clock，测试不真实等待。

每个 execution 的 wall deadline 从第一次成功领取计算，固定为：UPLOAD_FINALIZE/IMPORT_GROUP/IMPORT_ITEM_PIPELINE/GAME_FILE_REVISION 6 小时，DAT_PARSE/VARIANT_REVALIDATE 30 分钟，METADATA_SCRAPE 1 小时，MEDIA_FETCH/BLOB_GC/UPLOAD_CLEANUP 30 分钟。到期后 context 中止，短事务将 Job 置 FAILED 且 `error_retryable=true`；VARIANT_REVALIDATE 映射 `LAUNCH_CORE_VALIDATION_TIMEOUT`，其他 kind 使用 `<KIND>_EXECUTION_TIMEOUT`。超时前已发布的独立幂等子结果可在人工 retry 时复用，但不得发布半成品领域 current。deadline 和退避都用注入 clock；验收不真实等待这些时长。

取消事务必须以当前 Job `If-Match` 和领域资源版本为条件：QUEUED 任务可直接转 CANCELLED；RUNNING 任务只转 CANCEL_REQUESTED 并追加同事务事件。Worker 的每次最终提交都必须再次验证 state=RUNNING、lease owner/token 和未请求取消，因而旧 worker 不能在取消或 lease 转移后发布结果。hash/copy 每 8 MiB、XML 每不超过 1,024 token、archive 每 entry、网络 read loop 每 1 MiB 或一次阻塞 I/O 返回后检查 context；这些是取消检查上界，不要求测试真实处理大文件。

## 8. 数据库级保护

Migration 必须建立并测试：

- partial unique indexes：每个 BIOS requirement 的 active installation、每个 core artifact 的 active DatVersion、每个 Core 的 enabled CoreArtifact 唯一，以及每个 `(core_artifact_id, sha256, parser_version)` 至多一条 BUILTIN DatVersion；启动校验另保证每个 enabled Core 恰有一条 enabled artifact；
- trigger：MetadataRevision、GameAsset、GameContentRevision、GameContentFile、VariantRevision、VariantFile、DosEntry、VariantDependency、ImportItemSourceFile、ImportItemCoreValidation/ValidationFile、ContentHashEvidence、ProviderResponse、ScrapeQueryAttempt、ScrapeCandidate/Hit、ReviewEvent、AuditEvent、JobInputSnapshot、JobEvent、PlaySessionEvent 和 PersistentSaveRevision 创建后禁止 UPDATE/DELETE；Game/Variant/PersistentSave 只通过 current pointer 前移；
- trigger：ArchiveEntry 只允许 `materialized_blob_id` 从 NULL 一次性设为实际 size/CRC32/MD5/SHA-1/SHA-256 全部相等的 Blob；已非空、设回 NULL、改为另一 Blob 或修改任一其他字段都拒绝。DELETE 只在 owner-GC 事务中允许：同一 `archive_blob_id` 已无业务保护边，且不存在指向其任一 `(archive_blob_id, ordinal)` 的外部复合外键；服务必须按 archive_blob_id 成组删除并立即删除 owning Blob，普通 repository 不暴露 entry delete；
- trigger：曾激活（`activated_at_ms IS NOT NULL`）或已被 VariantRevision 引用的 DatVersion 及其 machine/entry 禁止 UPDATE/DELETE；从未激活且无引用的用户候选才允许整组删除；
- trigger：PlatformInstance 默认 core、GameVariant core 与间接平台关系有效；被目录/Variant 使用的 PlatformCore 不可禁用；
- trigger：BIOS Requirement 的 STATIC/DAT_MACHINE XOR、logical name/catalog 字段成立；active Installation 的 requirement/version/status 关系有效，INVALID 永不 active，MISSING_ENTRY 永不用于 READY Variant dependency bundle；BIOS_OR_BASE 的 HASH_WARNING 可用于 READY，PARENT 的 MISMATCH 不可用于 READY；
- trigger：Job kind/scope 只允许数据字典映射，`execution_no` 指向同 Job 最新连续 InputSnapshot，payload 只指向该 execution；JobEvent 的 scope/execution/attempt 与其 Job 一致；ProviderCache current response 与 cache key 的 provider/request digest 一致；
- trigger：ImportJobFile 覆盖当前 UploadSession 每个 UploadFile 且 SOURCE 与 ItemSourceFile 引用一致；Item 的 failed stage/error 与 FAILED_RETRYABLE/FAILED_FINAL 状态同时出现或同时为空；ItemSourceFile 的 UploadFile/Blob/archive entry 归属与 Item source manifest 一致；ImportItemCoreValidation 的 target/core/artifact/DAT/source manifest 归属一致，ValidationFile 只能属于同一 READY validation；ReviewDraft selected validation 必须属于自身 Item、READY 且匹配 target，人工封面必须属于自身 Item并与候选封面互斥，default DOS entry 属于自身 Item；
- trigger：MetadataScrapeRun owner XOR、Game owner/content ownership 与 Import owner/content-null 约束成立，且 Job scope/provider 匹配；provider=NONE 的 run 不得通过 evidence/attempt/hit 关联全局 ProviderResponse，也不得有 candidate/asset；ScrapeQueryAttempt 的 evidence/response/request digest/run/provider 关系一致；Candidate、Hit、CandidateAsset 的 attempt/response/run/provider/owner 关系一致；ReviewDraft 只能选择自己 Item 的 COMPLETED run/candidate 和 READY asset；
- trigger：Game 的当前 metadata/content revision 必须属于自身；GameVariant 的当前 revision 必须属于自身、状态为 READY，且其 content revision 必须属于同一个 Game；
- trigger：MetadataRevision 的 source kind/ref nullability、ImportItem/Game ownership 和 ScrapeCandidate/run/current ContentRevision 必须符合上表；ADMIN_EDIT 的领域 service 另必须在同一事务写入指向该 Game 与新 MetadataRevision ID 的 AuditEvent，该跨表“同次操作必须存在事件”用 service 集成测试保护，不伪称 SQLite 有 deferred assertion；
- trigger：GameContentRevision 的 source kind/ref 类型匹配；VariantRevision 的非空 default DOS entry 必须存在于其 ContentRevision 的 `dos_entries`；
- trigger：GameContentFile 的可空 source archive pair 必须命中同一 ArchiveEntry，且 materialized Blob 与 `blob_id` 一致；
- trigger：PersistentSaveRevision 的 Launch/Profile/VariantRevision 归属一致；同 launch 新 sequence 必须恰为既有最大值 + 1，重复请求由唯一键命中原 revision，不能插入空洞或改写既有 event/blob；提升 current 时首项必须匹配 Launch base、后续项必须匹配该 launch 前一项，竞态返回领域冲突而非覆盖；
- trigger：UploadSession `finalization_no >= 0`；FINALIZING 必须指向同 scope、同 `finalizationNo` 的当前 UPLOAD_FINALIZE Job，旧 Job 不可改写，COMPLETE 时所有文件 COMPLETE；插入 whole-session Upload consumption 时该 session 必须 COMPLETE 且没有任何既有 consumption，且 consumer ID 必须指向匹配的 ImportJob 或 `GAME_FILE_REVISION` Job；插入 file-level consumption 时该 session/file 必须 COMPLETE、该 session 没有 whole-session consumption，且 UploadFile 必须属于该 session；两方向都在同一写事务防竞态；
- CHECK：所有 size/duration/version 非负，结束时刻不早于开始，XOR 路径/blob 约束成立；
- 索引：游戏搜索/目录/状态、存档 profile+game+created_at_ms、任务领取与 scope event、审核队列、DAT machine/hash、全部外键列。

业务服务仍需在事务前返回可理解错误，trigger 是并发和遗漏的最后防线，不能以 trigger 错误字符串作为 HTTP 契约。

## 9. Migration 024：多盘证据与运行锁定

`content_kind`/`content_mode` 增加 `MULTI_DISC_M3U_V1`；Import source 与 GameContent file role 增加 `PLAYLIST_SOURCE`/`DISC`，VariantFile role 增加 `MULTI_DISC_PLAYLIST`。`import_item_multidisc_entries` 以 `(source_snapshot_id, ordinal)` 保存连续盘序、source reference、canonical name、PRESENT/MISSING 与可空 Blob；`review_multidisc_attachments` 关联 ImportItem、base/effective snapshot、Upload、Job、真实 User actor、状态、错误和诊断，并以局部唯一索引保证一个 active attachment。

`launch_external_files.kind=DISC` 锁定每张盘的规范虚拟路径，`launch_sessions.initial_disc_index` 锁定普通启动或 SaveState 恢复盘号。完整多盘 revision 的依赖快照必须包含 content kind、parser/delivery 版本、盘数、ordered disc SHA-256 与 canonical playlist SHA-256；多盘 Variant validation 使用独立 V3 canonical digest，包含 Variant/Content/Artifact ID、artifact version、compatibility digest、DAT、BIOS、盘序和 playlist。SINGLE/DOS 保留 V2，不改写历史 identity。历史 prepublish evidence 回填 generation 3 并强制 stale；只有 generation 4 当前证据可发布。

## 10. Migration 025：收藏与收藏夹

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

### 10.4 升级与验证

`025_favorites.sql` 是纯增量 migration：不重建旧表、不关闭 foreign keys、不回填推断收藏，也不改变 Blob reference registry。空库执行 001→025；023 fixture 执行 024→025；024 fixture 直接执行 025。checksum 发布后不可改写，所有表、索引、trigger、升级路径、非法 SQL 和两个 Profile 隔离统一由 `ACC-FAV-001` 验证。

## 11. Migration 026：服务器 BIOS 导入

`server_imports` 是一次 `BIOS_DIRECTORY` 导入聚合，保存 root ID/label 快照、不可逆 root 配置 digest、规范相对目录、完整 catalog digest、覆盖授权、阶段、计数、Job/创建者、乐观版本和 Unix 毫秒时刻。partial unique index 保证全实例同一时刻至多一个非终态 BIOS ServerImport；终态行的各 Item 分类计数必须恰好覆盖冻结 catalog。

`server_bios_import_items` 以 `(server_import_id, requirement_id)` 为主键，冻结 Requirement/CoreArtifact/DAT/期望 hash、激活选项、交付位置和创建时 active Installation 证据。trigger 阻止修改冻结输入；状态只记录该 Requirement 的选择和提交结论，previous/new Installation 必须属于同一 Requirement。

`server_bios_import_candidates` 保存 root-relative path、关联原因、内容 hash、archive 评估、rank 和未选原因；同一 Item 的 path/rank 唯一且至多一条 `SELECTED`。绝对 root、CAS path、root digest 不进入候选投影。

`jobs.kind` 增加 `SERVER_BIOS_IMPORT`，且强制 `scope_type=SERVER_IMPORT`。`bios_installations` 增加不可变的 `source_kind=BROWSER_UPLOAD|SERVER_DIRECTORY` 和可空候选外键；服务器来源必须引用所属 Requirement 的 selected Candidate。每个 Requirement 的 Installation、Item 终态、聚合计数与 `PROGRESS` 事件在同一短事务提交，进程恢复以终态 Item 为幂等边界。

## 12. Migration 028：Pegasus 目录导入与 VIDEO

`pegasus_imports` 保存 root ID/label 与不可逆配置 digest、规范相对目录、metadata snapshot digest、scan/import Job、聚合状态/phase、映射版本、计数、7 天到期和创建者；partial unique index 保证全实例至多一个 `QUEUED|RUNNING|CANCEL_REQUESTED` execution。`pegasus_import_metadata_files` 只保存相对路径、大小、内容/facts digest 和解析结论，不保存原始 metadata bytes。

`pegasus_import_collections` 以 `(import_id,metadata_relative_path,segment_ordinal)` 唯一，冻结展示投影；`IMPORT` 映射必须同时冻结 PlatformInstance/version、Platform、默认 Core、CoreArtifact/version 和可空 active DAT，`SKIP` 不得携带目标。`pegasus_import_items` 以确定性 source key 唯一，冻结标题、允许的 metadata、声明文件引用和 discovery 结论，并关联后续内部 ImportItem、发布 Game 或所有 existing match。`pegasus_import_item_files/assets` 保存 no-follow source facts、CAS 复制结果和媒体 warning；它们的 Blob 边全部登记为 protective reference。

`jobs.kind` 增加 `SERVER_PEGASUS_SCAN|SERVER_PEGASUS_IMPORT` 并强制 `scope_type=PEGASUS_IMPORT`。scan 与 import 是不同 Job；retry 在原 import Job 增加 execution/input snapshot，不复活旧事件。发布来源扩展为 `game_metadata_revisions.source_kind` 与 `game_content_revisions.source_kind` 的 `SERVER_PEGASUS_IMPORT`，且 source_ref 必须指向处于审核发布边界的 Pegasus Item。

`game_assets.kind` 增加 ordinal 0 的 `VIDEO`：只允许 `video/mp4|video/webm` 且 `width_px/height_px` 必须为 null；图片仍必须具有正尺寸和受限图片 MIME。每个 MetadataRevision 仍拥有完整媒体清单，管理替换/移除 VIDEO 或修改文字时复制未修改的 Asset 引用，历史 Asset 永不原地修改。Migration 028 重建受 enum 影响的表与触发器，026/027→028 与 fresh 001→028 必须同构并通过 foreign-key/integrity 检查。

## 13. Migration 029：Pegasus 管理员失败诊断

`pegasus_import_items.error_details_json` 是可空、最大 8 KiB 的 JSON object，只保存管理员排障所需的封闭证据：schema version、失败阶段、内部操作、稳定 cause code、受限技术详情、来源相对路径、观察值/上限，以及已创建时的内部 ImportJob/ImportItem ID。不得写入宿主绝对路径、Blob ID/hash、凭据或未截断上游 payload；retry 把该字段与旧 `error_code` 一起清空，新 execution 重新生成证据。

029 对 028 已产生且可确定为 Arcade companion 文件数超过 64 的 `PEGASUS_LIBRARY_IMPORT_FAILED` 进行一次性回填，记录实际组装数量和内部上限。其他无法从持久状态证明根因的历史失败不得猜测或伪造技术详情。

## 14. Migration 030：Pegasus 审核交接

`pegasus_imports` 增加 `review_pending_item_count/review_discarded_item_count`，phase 增加 `PREPARING_REVIEWS`；总量约束把待审核、已发布和审核丢弃计入互斥结果。`pegasus_import_items.execution_state` 增加 `REVIEW_PENDING/REVIEW_DISCARDED`，`library_import_item_id` 建立非空唯一索引，使一个普通 ImportItem 至多归属一个 Pegasus Item。`REVIEW_PENDING` 必须引用同一内部 ImportJob 中仍为 `REVIEW_PENDING` 的 ImportItem；`REVIEW_DISCARDED` 必须对应内部 Item 的 `DISCARDED`。

Pegasus Worker 在复制、普通 content pipeline 与 CoreValidation 后只冻结 metadata、COVER/VIDEO 和内部 ImportItem 关联，再把 Pegasus Item 交接为 `REVIEW_PENDING`；此时普通审核队列才允许展示。交接未完成的关联 Item 不得出现在队列/详情，也不得被 Approve/Discard。崩溃恢复复用既有 `library_import_job_id/library_import_item_id` 并幂等补齐 metadata，不得创建第二个内部 ImportItem 或重复系统草稿事件。

管理员 Approve 在普通审核发布事务内创建 `SERVER_PEGASUS_IMPORT` metadata/content revision、复制未被人工封面覆盖的来源 COVER 与来源 VIDEO，并把 Pegasus Item 原子转为 `PUBLISHED`；Discard 在普通审核事务内原子转为 `REVIEW_DISCARDED`。两种决策都同步重算 Pegasus 聚合计数。没有审核决策时不得创建 Game；一期没有批量决策状态或表。

Migration 030 受控重建两个 Pegasus 主表和受影响 trigger，保留 029 的状态、诊断、映射、媒体与历史发布行，并把新增计数初始化为零。029→030 与 fresh 001→030 必须同构并通过 foreign-key/integrity 检查。

## 15. Migration 031：审核运行预览与第 5 秒截图

`review_preview_sessions` 保存管理员从待审核条目创建的短时、不可变运行快照：锁定 ImportItem、有效来源快照、目标目录、默认 CoreArtifact、当次 Validation、主内容 Blob、依赖摘要、capability hash、启动/硬过期时间和是否允许截图。它不是 `launch_sessions` 或 `play_sessions`，不创建 Game、不累计游玩时长、不读写状态存档或持久存档。只有 `REVIEW_PENDING` Item、当前有效来源和 enabled 管理员能创建；主 ROM 始终必需，当前 Validation 中实际存在的 Parent、BIOS 和 external file 才复制为 `review_preview_files`，缺失依赖不会伪造占位。

`review_preview_files` 只允许 `PARENT/BIOS_BUNDLE/EXTERNAL_FILE/DISC`，行创建后不可更新或删除；Blob、逻辑名和可空虚拟路径必须属于创建时锁定的来源/Validation。`review_runtime_screenshots` 以 `(import_item_id,validation_id)` 唯一保存 PNG、CoreArtifact、来源快照、尺寸和固定 `captured_after_ms=5000`；重新运行会以新不可变 Blob 替换该 Validation 的当前截图引用。031 初始 trigger 只允许草稿所选 READY Validation 写入；当前 schema 由 Migration 033 扩展为 READY/阻断 Validation，并收紧到最新一次当前证据。

三张表的 Blob 外键全部进入唯一 reference registry。Migration 031 只追加表、索引和 trigger，不重建旧表；030→031 与 fresh 001→031 必须同构并通过 foreign-key/integrity 检查。

## 16. Migration 032：受限联机控制面

| 表/变更 | 唯一职责与稳定不变量 |
| --- | --- |
| `netplay_rooms` | 房间聚合；状态 `DRAFT/WAITING/STARTING/RUNNING/ENDED/EXPIRED`。host、创建时刻不可变；DRAFT 没有 game/profile snapshot，WAITING 以后五个 snapshot 字段全有；STARTING/RUNNING 恰有 current session；每个 Profile 最多主持一个非终态房间。DRAFT 15 分钟、WAITING 30 分钟空闲过期，STARTING 120 秒、运行 8 小时硬终止。 |
| `netplay_room_members` | `(room_id,profile_id)` 唯一，active `(room_id,player_no)` 唯一；HOST 固定 P1 且每房恰一 active host，GUEST 为 P2–P4；ready 只在 active WAITING 成员上成立，离开必须清 ready 并记录封闭 reason。 |
| `netplay_sessions` | 每次 Start 的不可变 game/variant/core/profile canonical snapshot；状态 `PREPARING/LOADING/SYNCHRONIZING/RUNNING/PAUSED_RECONNECT/RESYNCHRONIZING/FINISHED/FAILED`，每房最多一个 active session。`profile_json` 是 core-profile canonical object，`profile_digest` 是 lowercase SHA-256；它包含不可变 GameVariantRevision ID，从而在 core profile 覆盖多个游戏时仍锁定本局唯一内容 revision，而不是把 ROM hash 放回准入 manifest。P1 是唯一 state authority，occupied mask 必含 P1，resync 只递增。 |
| `netplay_session_participants` | 锁定 Session 中每个 Profile/seat；状态 `LOCKED/LAUNCH_READY/RUNTIME_READY/SYNCHRONIZED/CONNECTED/DISCONNECTED/LEFT`。LOCKED 时 launch/credential 均空；LAUNCH_READY 起二者全有，credential generation 从 1 单调递增，数据库只存 SHA-256；seat/member/session/launch 绑定不可改。断线只存 10 秒 lease 时刻，不存输入或 state bytes。 |
| `netplay_events` | 房间级 append-only 小事件；只允许 migration 中封闭 event type 和低基数 `data_json`，禁止 UPDATE/DELETE。不得记录显示名、输入、ROM/BIOS 名称或 hash、state、cookie、IP、宿主路径。每帧 input/canonical/hash 不入库。 |
| `launch_sessions` | 新增可空 `netplay_session_id/netplay_player_no` 与非空 `save_access NORMAL/NETPLAY_DISABLED`。普通 Launch 必为 `NULL,NULL,NORMAL`；联机 Launch 三者同时锁定并与 Participant/Session snapshot 完全相同，每 Participant 最多一个 Launch。 |

每个 Session 终态都会撤销关联 Launch 并把其 Participant 标为 LEFT。运行中任一 Participant 离开等价全局 `USER_EXIT`：访客释放自己的 RoomMember、房间回到 WAITING且全员 ready 清零；房主主动结束本局时保留成员并回 WAITING。房主丢失/关闭、profile 撤销、服务重启、restore 与硬到期把 Room 标为 ENDED，活动 RoomMember 以 ROOM_ENDED 收口。服务启动 recovery 把遗留 STARTING/RUNNING Session 标为 `FAILED/SERVER_RESTARTED` 并撤销 Launch；restore 使用 `RESTORE`。实时 input/history/hash/state transfer 只存在 `internal/netplay.Hub` 的有界内存，不新增 Job/Blob/CAS 表，也不允许由运行时 DDL 修补。

## 17. Migration 033：阻断截图人工放行

033 不增加表或列，只替换审核 preview/screenshot 的三个校验 trigger，并把升级瞬间仍存在的短时 preview session 统一设为 `capture_allowed=1`。新建 preview 及截图插入/替换都必须绑定该 Item、有效来源快照、草稿目标平台、默认 CoreArtifact 和同一组合下按 `created_at_ms,id` 选出的最新 Validation；Validation 必须使用当前 `prepublish_generation=4`，但状态可以是 READY 或阻断。重新检查产生更新 Validation 后，旧 preview 即使稍后上传截图也会被 trigger 拒绝。

当前阻断 Validation 的第 5 秒截图可作为管理员放行证据；Approve 仍需在同一事务复核来源、目标、默认 CoreArtifact、active DAT 和配置版本，并把截图 ID 与 `REVIEW_SCREENSHOT_OVERRIDE` 写入不可变审核证据。032→033 与 fresh 001→033 必须同构并通过 foreign-key/integrity 检查。

## 18. 统一验收入口

schema 与整数时间由 `ACC-DB-*` 覆盖；唯一归属由 `ACC-PLAT-*`；不可变 revision 与删除由 `ACC-GAME-*`、`ACC-SAVE-*`；Pegasus/VIDEO 由 `ACC-PEG-*` 与 `ACC-MEDIA-001`；状态机与 lease 由 `ACC-IMP-*`；凭据 hash 与内容授权由 `ACC-SEC-002`。
