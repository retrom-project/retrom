# Retrom 数据模型

字段、CHECK、FK 和索引的事实源是 `migrations/001_identity.sql` 至 `migrations/015_shared_runtime_sessions.sql`；跨表与状态转换校验在 `internal/persistence/recordstore`，会话及存档联动在 `internal/persistence/sessionstore`，共享查询投影在 `internal/persistence/storequery`。本文描述稳定领域关系。HTTP 字段以 `api/openapi.yaml` 的统一 bundle 为准。

## 1. 基线

- 001–014 组成新的未发布建库基线，创建表、声明式约束和索引，不创建 trigger、view 或回填历史数据。此次基线与旧开发库不兼容，旧开发库必须停机重建，可在确认环境作用域后直接清除数据；不转换历史数据、不双写、不运行时修补 schema。校验和仍严格匹配，只允许当前基线的有序前缀续跑。
- 业务主键使用 UUIDv7，摘要使用 64 位小写 SHA-256，时刻使用 Unix 毫秒 `INTEGER`。
- 当前业务状态原位更新并推进 `version`；需要追踪的历史进入 audit、event、job input、来源快照和验证证据，不为 metadata、content、Variant 建平行业务版本树。
- 数据库不保存 Launch 明文 capability、Cookie、CSRF token、用户主机绝对路径或 Provider 私有实现映射。

### 共享运行会话

`015_shared_runtime_sessions.sql` 是对当前 001–014 基线的兼容追加，已有游戏、存档、账户和 Launch 原地保留。`runtime_sessions` 每个 `auth_session_id` 至多一行，保存独立运行 ID、凭据摘要、创建/续期/到期毫秒时间；`expires_at_ms=renewed_at_ms+86400000`。续期更新同一行，超过 12 小时才延长，凭据值不轮换；已过期凭据必须凭有效账户会话重新签发并更换运行 ID/摘要。运行认证检查父 AuthSession 的显式撤销、User 状态与 session version，不使用父会话的自然 idle/absolute 到期作为运行期限。普通 Launch/资源授权仍按 Profile 隔离，凭据不绑定某个存档。

### 应用写入与数据库职责

数据库保留 PK、UNIQUE、CHECK、FK、必要的关系级联和查询索引。文件所有权接收、交接和退休与领域写入同事务完成；其他跨表归属、快照冻结、版本推进、终态不可恢复、最后一个管理员、标签数量与批次丢弃围栏使用显式 SQL 校验。新增或修改这类写入必须经过对应的 `recordstore` 方法；只读查询或不涉及这些不变量的写入仍可直接使用参数化 SQL。

`recordstore.Update` 分离 SET 值和 WHERE 参数，在同一连接读取所选记录的旧值，再更新并校验新旧值；删除先校验所有目标记录，再执行删除。调用方不得把未经校验的客户端文本拼入 SET/WHERE。写入与校验共享保存点，任何错误会撤销该次操作，调用方继续外层事务也不能提交非法记录。乐观条件未命中仍返回零行。冻结字段校验以值是否发生改变为准，对原值赋值不产生新状态；不可变证据整行更新、凭据重复消费/撤销等一次性操作仍按各自契约拒绝。

Launch 创建、变更与终态处理，以及 Save 创建，必须使用 `sessionstore`，在相同保存点维护回收排期、存档数据版本绑定和隔离凭据撤销。批量更新逐个处理实际选中的会话，已撤销的凭据不改写原撤销时间，已释放排期不重新入队。

存档兼容性和批次丢弃的共享 SELECT 由 `storequery` 提供，消费者以子查询组合，不依赖数据库 view。当前驱动仍是 SQLite；JSON 函数、占位符、PRAGMA、索引、事务隔离/锁的其他数据库适配不属于这次触发器/view 移除。

## 2. Runtime Provider catalog

```text
RuntimeProvider
  └─ RuntimeTarget
       ├─ RuntimeTargetBinding ── Product Core / Platform / content kind
       ├─ BIOSRequirement / DatVersion
       ├─ ReviewCurrentFacts / GameVariant
       └─ LaunchSession
```

`runtime_providers` 每个 Provider 一行，保存当前 SemVer、Provider API、Bundle/manifest/module SHA-256、来源与激活时刻。安装器拒绝降级、同版本换字节、身份不一致和活动文件漂移。

`runtime_targets` 的主键是 `(provider_id,target_id)`，保存当前 Provider manifest 投影的展示名、闭合 options schema、能力、checkpoint declaration 和公开 fragment。稳定引用只使用 Provider/Target；Bundle digest 只在需要重现实际执行字节的 Launch 与 Preview 中冻结。

`runtime_target_bindings` 把产品 `core_id` 绑定到一个稳定 Target，并通过 platform/content-kind 关系收紧适用范围。数据库不保存 adapter、引擎 core、入口或资产映射。

平台、核心和内容分类的产品数据来自 `data/runtime-target-bindings/v1/catalog.json`，而非 migration seed。当前目录只有内容摘要；`schemaVersion` 描述序列化格式，不另设目录递增计数器。系统同步复用 `internal/runtime/catalog`，与 Provider/Target 和 binding 在同一事务发布；新增使用已有存储/交付策略的产品不修改 schema。稳定定义被用户引用时不可删除，目录名称、默认核心等用户选择不被声明同步覆盖。

## 3. Game current state

`games` 是用户可见游戏及其当前 metadata/content 根：它直接保存 PlatformInstance、标题字段、metadata 来源、content kind/来源类别、规范 manifest、状态、payload 生命周期、搜索文本和 `version`。

`game_assets` 与 `game_files` 直接归属 Game。`game_variants` 每个 `(game_id,core_id)` 一行，保存当前 Provider/Target、DAT、emulator game ID、兼容状态、依赖快照、DOS 入口、版本和可选的 `runtime_profile_json`。`games.content_profile_json` 保存内容类型专属的一对一扩展；`variant_dependencies` 与 `variant_files` 仍按稳定 Variant ID 独立存储多行关系。

Game 不保存指向审核 Item、导入 Job 或刮削 Candidate 的来源引用；来源类别保留为描述，具体操作证据进入 AuditEvent。产品启动、存档、媒体与删除只读取 Game、Variant 和各自运行对象，已完成的导入及审核记录可独立删除。

metadata 编辑和媒体替换原位推进 Game；内容替换在后台准备完成后执行一次事务切换，删除旧文件、派生物、运行资源和存档，再写入新当前态。永久删除保留 Game tombstone 与审计，异步释放 payload。

## 4. 依赖与 DAT

`bios_requirements` 与冻结的 `server_bios_import_items` 以 `archive_members_json` 保存源码派生的成员数组（name、sizeBytes、CRC32、SHA1、required）；该字段仅用于 STATIC archive。生成列 `file_kind` 在 DAT_MACHINE 或成员声明非 NULL 时为 ARCHIVE，其余为 FILE。成员不得成为独立 Requirement；服务器任务冻结成员声明并随 catalog digest 校验漂移。未发布的初始 schema 直接收口，不提供散文件槽到归档槽的历史转换。

`bios_requirements`、`dat_versions` 和服务器 BIOS 导入项引用稳定 Provider/Target。当前 active DAT 可以前移；已创建 Launch 只消费其冻结的依赖文件。BIOS 安装替换会撤销使用旧 BIOS 的 Launch/Play，并切换当前安装；新的启动按当前安装重校验；Game 存档保留。

BIOS 的“当前库所需”范围以当前已发布 Game 的 Provider/Target 判断成员资格，按 `game_variants_provider_target_game(provider_id,target_id,game_id)` 联合索引查找候选，再检查 Game 发布状态；不得按每条 BIOS 要求重复遍历整个已发布游戏库。多款游戏共享 Target 不重复增加 BIOS 项数，只有已删除游戏使用的 Target 不进入该范围。

依赖 snapshot 是规范 JSON，包含所选 BIOS、parent/base 和多盘的实际闭包。Variant 保存当前 snapshot，Launch 创建时复制 snapshot 并记录文件标识；Blob 引用仍由领域 owner 持有。

静态 BIOS/多盘和 Arcade 依赖均采用当前 `schemaVersion:1`，分别以 `kind:STATIC/ARCADE` 区分实际类型，不根据历史版本号选择解析器。

## 5. 导入、审核与刮削

Upload、ImportFile、Archive、ImportJob、ImportItem、来源快照与审核草稿维持各自 owner、版本、幂等和 payload release 边界。审核只有当前事实：内容观察属于 Item，当前来源、目录 Core、Provider/Target、活动 DAT、BIOS 要求与 active installation 在读取事务内求值，不保存或选择历史 Validation。BIOS 模块负责安装事实，审核模块通过只读端口查询它；安装不触碰审核草稿版本，也不向审核发送业务状态。Provider Bundle 单独升级不使审核结论失效。

`review_arcade_parent_attachments` 与 `review_multidisc_attachments` 只保存业务决定 `PENDING/ACCEPTED/REJECTED/CANCELLED`；排队、租约、尝试次数、execution deadline 与可重试失败均以关联 Job 为唯一事实源。`PENDING` 的部分唯一索引保留每类补传预约，手动 Retry 无需同步另一套业务执行状态。当前 clean schema 不兼容旧 Attachment 执行状态和裸业务输入快照；开发库通过精确 PFB ID 的 `pfb-data-reset` 归档重建，不回填旧数据。

来源快照是不可变的输入证据，不是业务版本树：不分配 revision 序号；每个 Item 最多一份 `created_by=IDENTIFICATION` 初始来源，当前来源只由 `ReviewDraft.effective_source_snapshot_id` 选择，不按创建时间或最大序号猜测。

Upload 的业务用途只区分 `GENERAL/PROJECT`，并独立记录文件/目录形态；项目引擎由归一化后的真实内容检测。审核不存储算法 generation；目录展示变化和不相关能力变化不参与有效性摘要。

审核详情、队列、来源结果、批量候选、预览创建和批准共用当前依赖求值。缺失 → 安装 → 移除在下一次读取中分别为 BLOCKED → READY → BLOCKED，不追加校验记录。RPG 的读取和发布共用项目资源策略；外部 RTP 声明默认阻断，管理员的显式自包含确认保存于当前 profile，发布事务重新核对。

运行包安装、选包与运行挂载已退出产品。当前建库基线不再创建 `runtime_asset_pack_definitions`、`runtime_asset_pack_installations`、`runtime_asset_pack_files`、`game_variant_runtime_packs` 和 `review_draft_runtime_pack_selections`，也不接受对应的上传用途、消费类型或后台任务类型。外部 RTP 声明继续由项目校验阻断，管理员只能按自包含确认规则继续发布。

发布事务将审核 metadata、媒体、内容文件与默认 Variant 一次写入 Game current state。当前有效的运行截图复制成 Game 自有 SCREENSHOT，标签及内容/运行 profile 写入 Game/Variant；临时预览 checkpoint 不转为产品存档。最终提交同时撤销所有关联 Preview 与隔离凭据、关闭审核媒体授权、清空审核 profile/标签并解除 Source 的工作关联。重新刮削以稳定 `game_id` 为 owner 创建候选；显式应用候选才更新当前 metadata/assets，不能因为旧内容版本表已经删除而丢失 Game 关联。

`import_items.review_profile_json` 保存审核阶段内容类型专属的一对一扩展。三个 profile 字段均为可空 JSON；数据库只校验 JSON 合法、`kind` 是字符串且 `data` 是对象，不把任何具体核心的字段结构写进 schema。当前 RPG Maker 使用 `{"kind":"RPG_MAKER_PROJECT","data":{...}}`；代码按 owner 与 `kind` 映射到对应的 model，并由对应核心校验业务字段。RPG Maker 审核 profile 保存检测代际、证据、项目文件统计与 fingerprint、要求摘要、分析结果、自包含确认、稳定 Provider/Target 和依赖摘要；发布时将内容证据复制到 Game 的 `content_profile_json`，将运行代际及依赖摘要写入 Variant 的 `runtime_profile_json`。`metadata_json` 与 `source_manifest_json` 继续承担各自通用职责；文件、Blob、校验和依赖等一对多实体保持独立。profile 不保存运行 gate、位置证明或独立验证决定。所有审核通过中立的 `runtime_preview_sessions` 试运行，`review_preview_bindings` 单向关联 Item/来源快照到 Preview；运行表没有审核或导入外键，使用不透明 scope/revision、冻结的返回路径、Provider/Target、Bundle 和文件授权。来源文件与校验产物分开锁定；`RUNTIME_FILE` 只能引用该审核 Item 的派生产物，不能借试运行读取其他来源的 Blob。

当前 RPG Maker `data` 字段按 owner 分层：审核字段为 `generation`、`evidenceFamily`、`evidenceGeneration`、`evidenceConfidence`、`engineVersion`、`entryHtmlPath`、`fileCount`、`totalBytes`、`projectFingerprint`、`requirementsSha256`、`analysis`（JSON 对象）、`selfContainedOverride`（0/1）、`providerId`、`targetId` 与 `dependencySnapshotSha256`；Game 只保留证据、文件统计、要求摘要及分析；Variant 只保留 `generation` 与 `dependencySnapshotSha256`。没有适用扩展时整个字段为 SQL `NULL`，不写空对象。新增类型由对应代码定义和校验 `data`，无需新增一对一表或修改这三个字段的数据库约束。

审核临时 checkpoint 使用会话级存储，一份 preview 保留当前临时 payload，格式及 Blob 关系明确。恢复 preview 在写事务前复制 checkpoint 到自己的 `previews/<Preview UUID>/restore/`，提交时重验来源 checkpoint 未变化；恢复输入不跟随原 preview 后续覆盖，也不依赖原 preview 到期后的文件寿命。已关闭会话的临时 checkpoint 可在审核未结束且未到期时用于恢复；审核完成立即撤销恢复权限；过期或终态清理退休预览自有目录。临时存档不是审批/升级门槛，不引入原会话、恢复会话或人工确认的附加状态机。

`metadata_media_runs` 每个 ScrapeRun 一行，保存媒体顺序冻结时刻、累计收费 bytes、版本与时刻，不复制 Job 状态，也不增加 Blob owner。`scrape_candidate_assets.media_fetch_job_id` 唯一关联下载 Job，`media_fetch_order` 保存冻结顺序，`media_charged_bytes/media_reserved_bytes` 保存资源累计收费和当前预留。可空 Job/顺序字段用于未冻结或手工证据状态，所有新下载必须原子绑定 Job 与输入。进程重启保留排序、预算与 Job 原始 execution 期限；payload 释放删除资产引用后，预算记录不阻碍实际释放。预算与恢复策略见[导入与刮削](./import-and-review.md#7-hasheous-适配器)。

### ScummVM 检测与选择

`SCUMMVM_PROJECT` 沿用项目来源快照、`PROJECT_FILE`、审核草稿与当前 Variant。`review_profile_json` 的 SCUMMVM_PROJECT 扩展保存检测来源摘要、固定检测器上游 commit、完整候选与根目录，以及当前 selectedCandidateId。候选 ID 由来源摘要和全部有界检测字段计算；客户端不能复用另一份来源的 ID。

ScummVM 选择在带草稿版本检查的事务中更新当前 profile，不生成验证记录。发布复制当前选择到 Game Variant，并复查来源摘要；完整项目树保留全部根目录，所选 root 只是启动输入。ScummVM 不进入单 ROM BIOS 解析。

原生存档格式与槽位属于 Provider payload，数据库只记录公共 checkpoint format/大小/摘要；预览存档继续冻结自己的恢复输入；正式用户存档由 SaveState 持有 Blob，Launch 只绑定存档版本。

### 归一化接收文件与当前结果

`import_files` 是所有传输来源共用的接收表。每行包含 `id/upload_session_id/relative_path/file_record/size_bytes/created_at_ms/released_at_ms`，没有格式或来源类型字段；ID 与 transport UploadFile 对应，路径在会话内唯一。接收与上传文件记录原子提交；幂等重放不能改变路径、大小或内容。准备阶段复制成 Item 独占文件后，校验和审核读取 Item 自己的文件记录。输入释放时清空接收行的文件记录并记录释放时间；同一 UploadSession 的全部输入释放后才删除上传目录。

不创建 `review_events`。审核版本仅用于并发控制；批准/丢弃保留当前 Item 状态和发布 Game，API 不返回 reviewEventId。`metadata_scrape_runs` 分别以 `import_item_id/game_id` 建唯一约束；重新抓取替换旧 run，级联删除其候选、evidence、query attempts 与 media budget，并取消旧活动 Job。当前 GameAsset 独立保留；共享 provider cache 由 TTL 管理。

### 服务器 metadata 扫描证据

`source_imports` 的 `format=BASIC|PEGASUS|GAMELIST` 只选择扫描器；`extension_filter` 保存 BASIC 的规范小写扩展名筛选，使用分号分隔、去重并排序，其他格式必须为空。BASIC 不生成 metadata evidence；所选目录形成一个 Collection（`metadata_relative_path=.`），每个符合筛选的文件形成一个 Item（该位置字段保存文件相对路径），文件 facts 与大小进入既有 source 文件快照。Collection、Item、file、asset、metadata evidence 共用 `source_import_*` 表。通用 jobs 使用 `IMPORT_SCAN/IMPORT_RECEIVE`，不再创建格式专属任务表。

`source_import_metadata_files` 保存来源相对路径、实际大小、文件特征、解析状态与错误。大小不超过 8 MiB 的记录必须保存内容摘要；仅超过该上限且状态为 `INVALID/PEGASUS_METADATA_TOO_LARGE` 或 `INVALID/EMULATIONSTATION_GAMELIST_TOO_LARGE` 时允许摘要为 NULL，此时扫描和启动重验都不得读取超限内容。无摘要不能表示正常 metadata 或其他解析错误，相关组合由表级 CHECK 保证。

### 批次丢弃与服务器上传归属

`import_batch_discards` 对 `(kind,import_id)` 只保留一个当前处置，kind 仅为 `IMPORT/SOURCE`。`REQUESTED → COMPLETED|FAILED`，失败可回到 REQUESTED；记录请求管理员、错误码和毫秒时间，不增加试玩 revision 或按运行次数累积记录。来源批次由服务校验；请求落库后，发布/重试事务通过 `storequery.DiscardedImportJobs` 查询与 `recordstore` 状态校验共同阻止批次再次发布、重试导入。

`server_import_upload_owners` 将内部 UploadSession 唯一关联到一个来源 Item，与内部上传同事务创建，覆盖“创建内部导入后、尚未交接审核前”的中断和不支持格式分支。UploadSession 删除级联移除此归属；该表仅记录身份，不增加 Blob 引用。不保留旧格式或历史数据库的归属推断/修复路径。

批量处置中的真实待审核 Item 通过正常 Discard 事务生成审核决定。未产生审核的失败来源也可进入 `REVIEW_DISCARDED`，由批次处置作为证据，保留原错误码和详情，不生成审核历史。PUBLISHED/SKIPPED_EXISTING 不进入该转换；普通导入被取消的执行项与拒绝文件保留原终态及失败证据。引用释放仍以现有 payload state 和 release job 为唯一事实源。

### 文件与媒体的交接所有权

SourceItem 复制文件、来源归档和 COVER/VIDEO 后先持有自己的引用；Arcade 伴随文件在 `source_import_item_companions` 中按 `(item_id,candidate_item_id)` 唯一保留，直到交接或终态清理。它不依赖伴随来源项是否同时执行。

普通 ImportItem 持有完整来源快照及校验文件。来源媒体归 `import_item_assets`，主键为 `(import_item_id,kind)`，`kind` 只允许 `COVER/VIDEO`，保存 Blob、media type、可空宽高和创建时刻。Source 进入 `REVIEW_PENDING` 的交接事务同时复制媒体引用、完成 metadata/warning 与活动工作关联、更新聚合并登记独立 Source release Job；任一步失败全部回滚。仅转移引用，不复制独立文件存储字节。

审核与发布读取 ImportItem 的文件和媒体，不读取 Source payload。Source 可在审核期间达到 `RELEASED`；来源摘要与结果仍保留，绑定只维持到审核决定或 Source 终态。发布冻结目标 Game UUID 后移动 Item 的 payload 目录；ImportItem 终态清理剩余 staging 目录和审核读取关系。三者的 `payload_release_job_id` 不共用，删除 Game 不回溯清理 Source 或 ImportItem。

payload 物理状态只有 `RETAINED/RELEASING/RELEASED`，没有 owner 失败列；任务失败与原因只保存在 Job。API 的 `FAILED` 是 `RELEASING` owner 与关联失败 Job 的读投影，已释放 owner 不会因后续任务错误退回失败。

## 6. Launch 与资源冻结

`launch_sessions` 保存 Game/Core、稳定 Provider/Target、冻结 `bundle_sha256`、内容类型、依赖 snapshot、兼容状态、可选 save owner、凭据摘要和生命周期。`launch_content_files` 与 `launch_external_files` 锁定本次内容、BIOS、parent 和 disc 的领域文件记录；这些授权投影不取得文件所有权，文件随所属领域目录释放；Game 内容或 BIOS 变化会撤销受影响的 Launch。Provider Bundle 身份仍按创建时冻结。

Review Preview 使用相同冻结原则和 Player，但保留审核来源 owner，不创建假 Game。config 激活和幂等结束推进会话授权状态，不写入已发布游戏的游玩统计。Provider 静态资源由 Provider/Bundle/path 三元组读取并逐请求校验 allowlist 与摘要。

## 7. SaveState

`save_states` 保存 Profile、Game、checkpoint format、payload Blob/SHA-256/size、可选截图、DOS 路径/disc index 和来源 Launch。它不复制 Provider、Target、Bundle 或 Variant 身份。

写入必须来自同一 Profile/Game 的有效 PRODUCT Launch，且格式等于 Target 当前 `writeFormat`、大小不超过 `maxBytes`。恢复使用显式 Core 的当前 READY Variant；省略 Core 时通过来源 Launch 选择原 Core，而非目录当前默认 Core。当前 Target 还须声明可读该 checkpoint format，来源 Launch 不锁定恢复时的 Provider 版本或 Variant。不可读存档保留为 BLOCKED 投影，不加载旧 Provider、不 fallback，也不阻止无存档启动。

Provider 激活前按来源 Launch 的 Core 关联其当前 Variant/Target，保证该核心现有未删除持久存档格式仍在 `readFormats` 中；同一 Game 的其他备用核心不继承这项格式要求。审核临时 checkpoint 不参与升级门槛，也不以 `maxBytes` 减少阻塞升级。审核结束的最终事务撤销 Preview 与隔离凭据；异步清理删除 checkpoint/restore 读取关系，并排队退休 `previews/<Preview UUID>/` 自有目录。过期 Preview 使用同一目录回收机制。后台删除队列只处理已经退休的文件。

## 8. Play 与隔离

PRODUCT 的 `play_sessions` 保存客户端可见、未暂停运行时间的累计最大值；首次成功上报才创建记录；`last_reported_at_ms` 保存最近一次成功上报时刻，不保存事件序号或逐次事件表。统计写入不改变 Launch 授权或内容回收时间。`isolated_runtime_bootstrap_tickets` 和 `isolated_runtime_capabilities` 为每个 Launch/Preview 提供一次性、exact-origin 授权。


## 9. 领域文件与目录清理

Game、ImportItem、SaveState、BIOSInstallation、上传和抓取记录直接保存文件值：相对路径、size、四种 hash 与 MIME。`file_record` 及其用途前缀字段保存该 JSON 值；不存在全局文件登记、引用计数、owner 转移或逐文件删除表。摘要用于完整性和内容识别，不决定物理路径。不同游戏的同内容文件分别存储。

Game 独占 `files/<UUID 后两位>/<Game UUID>/` 下的内容和媒体。ImportItem 独占 `staging/items/<Item UUID>/` 的 payload 和 scratch；发布移动整个 payload，审核截图等临时材料随 Item 终态清理；预览 checkpoint 由独立 `previews/<Preview UUID>/checkpoints/` 目录持有。存档、BIOS、抓取响应等仍由各自业务对象持有独立目录；Game 和 Launch 只能读取被授权的 BIOS 安装。

`import_items` 的 `publication_game_id`、`publication_json` 和可选 `publication_bulk_id` 保存一次发布决定。审批先冻结当前输入和目标 Game UUID，转为 `PUBLISHING`；目录 rename 后再提交 Game/Variant 和 `PUBLISHED`。`PUBLISHING` 必须存在完整决定，其他状态没有未完成决定。启动和后台恢复继续同一决定，重复批准返回同一个 Game UUID。`PUBLISHING` 仍属于未完成审核；只有最终事务提交产物及授权关闭后才是 `PUBLISHED`。终态清理移除来源快照、派生文件、刮削候选及草稿关系，Item 仅保留完成结果和恢复父任务所需的最小原始内容类型/manifest 摘要；这些摘要不提供产品运行权。

`archive_entries` 保存归档扫描事实；文件复制时复制所需事实，目录退休时删除对应事实。已发布文件的读取不依赖原上传归档仍然存在。

业务删除或替换在自己的事务中提交目录清理意图，复用 jobs 的 `PATH_DELETE`，按目录去重。物理删除在事务外执行，失败可以重试。路径只允许不可复用的领域目录或内容/媒体代次，绝不枚举跨领域引用决定文件寿命。`OWNER_CLEANUP` 只负责有界清除业务读取关系并排队目录删除。布局和崩溃恢复见 [存储与数据库](./storage-and-database.md)。

### BIOS 与 Launch 延迟清理

BIOS 替换在安装事务切换当前安装、撤销旧 Launch/Play，保留 Game 存档；领域后台分批移除旧 Variant BIOS 关系，退休旧安装文件，保留名称/hash/来源审计。不同安装文件独立，无“另一个活动安装保护相同 Blob”规则。

`launch_payload_retirements` 保存 launch_session_id、due_at_ms、released_at_ms。创建、续期、结束、撤销与排期同事务更新；每次清理重验会话版本、状态、时限，每批至多 200 条，只在文件标识全部清空后记录完成。Launch 不拥有物理文件，清理读取关系不改变 Game/BIOS/Save 的所有权。

## 10. 数据库不变量

`recordstore`、`sessionstore` 与声明式数据库约束共同保证：

- 活动工作和已落地运行产物的 Provider/Target 引用命中当前 catalog，Launch 的 Bundle 命中创建时的当前 Provider；完成的导入、Source 和审核历史只保留身份字符串，不阻止未被产物引用的 Target 退出；
- Game、Variant 的稳定 owner 和逐次 `version` 更新；
- Launch、Preview、Save、运行依赖 与资源 owner 一致；
- checkpoint format 位于 Target 的可读格式集合；
- 隔离 capability 的 owner/origin/expiry 一致；
- payload release 不产生悬空 Blob 引用；
- 来源快照、审核事件和其他证据保持不可变。

新增运行时引用时必须复用稳定 Provider/Target 和既有 Bundle 冻结规则，禁止新增第二套运行选择字段或从 Target ID 推导 Provider 私有实现。

### 原生游戏数据存档

`game_save_versions` 按 `save_state_id` 关联 `save_states`，其 `data_version` 只随原生数据更新递增，与包含重命名的通用 `version` 分开；`last_synced_at_ms`
为空表示此前未提交原生数据，`last_writer_launch_session_id` 关联最近实际写入的 Launch。
`source_launch_session_id`、ID、名称与创建时间在确认覆盖中保持不变。显示/分页按 `COALESCE(last_synced_at_ms,created_at_ms)`。

`launch_game_save_bindings` 只绑定声明 `GAME_SAVE` 的 Product Launch，记录目标、预期数据版本与初始累计时长，不保存恢复 Blob。
无存档启动的绑定目标为空且预期版本为 0，首次同步创建并绑定；目标删除后保留非零版本，以禁止错误重建。
同一事务比较数据版本并替换完整 payload/截图，其他会话先写入则冲突。恢复时读取当前 SaveState payload；版本不匹配则拒绝该 Launch 的恢复，请重新启动。Blob 只由 SaveState 自身持有。

浏览器的 GAME_SAVE 草稿不新增服务端数据表。IndexedDB 按账号与 Launch 隔离，保存完整 checkpoint、截图、标题、来源恢复标记、
更新时间和固定幂等请求；它不参与 Launch 恢复输入。用户确认提交时才通过既有 launch_game_save_bindings 原子更新正式存档。

### 用户游戏活动读模型

`profile_game_activity` 以 `(profile_id,game_id)` 为主键，记录 `last_played_at_ms/active_duration_ms/session_count`。首次有效游玩上报增加会话数，后续上报只增加已接受累计时长与前值的差额，和 `play_sessions` 在同一事务写入；重复或乱序样本不重复计数。删除游戏保留文字墓碑与汇总，停用目录只改变用户可见性。迁移 016 从当前标准会话账本一次生成该读模型，运行时不再以全表聚合充当缺失读模型的回退。

最近时间、时长、次数排序分别由 Profile 开头的覆盖索引支持；会话账本具有 Profile/时间及 Profile/Game/时间索引。后台游戏列表为标题、创建时间、更新时间提供与游标排序一致的索引，首页最新游戏和主封面查询分别使用 `games_latest`、`game_assets_primary`。统计与筛选选项仅在首屏或筛选改变时计算，不随翻页重复传输。
