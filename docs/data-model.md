# Retrom 数据模型

字段、CHECK、FK、索引与 trigger 的逐字节事实源是 `migrations/001_identity.sql` 至 `migrations/013_bios_session_retirement.sql`；本文只描述稳定领域关系。HTTP 字段以 `api/openapi.yaml` 的统一 bundle 为准。

## 1. 基线

- 001–010 是冻结的 current-state bootstrap；011 起可追加兼容扩展，新增表和原子替换领域 trigger，不转换或删除现有 payload 表、不关闭外键。已有且 checksum 完全匹配的 001–010 数据库可原地升级。不兼容的开发库仍须停机归档并使用空数据根重建；不提供降级、回滚、双写或运行时 schema 修补。
- 业务主键使用 UUIDv7，摘要使用 64 位小写 SHA-256，时刻使用 Unix 毫秒 `INTEGER`。
- 当前业务状态原位更新并推进 `version`；需要追踪的历史进入 audit、event、job input、来源快照和验证证据，不为 metadata、content、Variant 建平行业务版本树。
- 数据库不保存 Launch 明文 capability、Cookie、CSRF token、用户主机绝对路径或 Provider 私有实现映射。

## 2. Runtime Provider catalog

```text
RuntimeProvider
  └─ RuntimeTarget
       ├─ RuntimeTargetBinding ── Product Core / Platform / content kind
       ├─ BIOSRequirement / DatVersion
       ├─ ImportValidation / GameVariant
       └─ LaunchSession / NetplaySession
```

`runtime_providers` 每个 Provider 一行，保存当前 SemVer、Provider API、Bundle/manifest/module SHA-256、来源与激活时刻。安装器拒绝降级、同版本换字节、身份不一致和活动文件漂移。

`runtime_targets` 的主键是 `(provider_id,target_id)`，保存当前 Provider manifest 投影的展示名、闭合 options schema、能力、checkpoint declaration 和公开 fragment。稳定引用只使用 Provider/Target；Bundle digest 只在需要重现实际执行字节的 Launch、Preview 与 Netplay session 中冻结。

`runtime_target_bindings` 把产品 `core_id` 绑定到一个稳定 Target，并通过 platform/content-kind 关系收紧适用范围。数据库不保存 adapter、引擎 core、入口或资产映射。

平台、核心、内容分类和内置资源包的产品数据来自 `data/runtime-target-bindings/v1/catalog.json`，而非 migration seed。当前目录只有内容摘要；`schemaVersion` 描述序列化格式，不另设目录递增计数器。系统同步复用 `internal/runtimecatalog`，与 Provider/Target 和 binding 在同一事务发布；新增使用已有存储/交付/布局策略的产品不修改 schema。稳定定义被用户引用时不可删除，目录名称、默认核心及已安装资源的用户选择不被声明同步覆盖。

## 3. Game current state

`games` 是用户可见游戏及其当前 metadata/content 根：它直接保存 PlatformInstance、标题字段、metadata 来源、content kind/来源、规范 manifest、状态、payload 生命周期、搜索文本和 `version`。

`game_assets` 与 `game_files` 直接归属 Game。`game_variants` 每个 `(game_id,core_id)` 一行，保存当前 Provider/Target、DAT、emulator game ID、兼容状态、依赖快照、DOS 入口和版本。`rpgmaker_game_profiles`、`rpgmaker_variant_profiles`、`variant_dependencies`、`variant_files` 和 runtime pack selection 都引用稳定 Game 或 Variant ID。

metadata 编辑和媒体替换原位推进 Game；内容替换在后台准备完成后执行一次事务切换，删除旧文件、派生物、运行资源和存档，再写入新当前态。永久删除保留 Game tombstone 与审计，异步释放 payload。

## 4. 依赖与 DAT

`bios_requirements` 与冻结的 `server_bios_import_items` 以 `archive_members_json` 保存源码派生的成员数组（name、sizeBytes、CRC32、SHA1、required）；该字段仅用于 STATIC archive。生成列 `file_kind` 在 DAT_MACHINE 或成员声明非 NULL 时为 ARCHIVE，其余为 FILE。成员不得成为独立 Requirement；服务器任务冻结成员声明并随 catalog digest 校验漂移。未发布的初始 schema 直接收口，不提供散文件槽到归档槽的历史转换。

`bios_requirements`、`dat_versions` 和服务器 BIOS 导入项引用稳定 Provider/Target。当前 active DAT 可以前移；已创建 Launch 只消费其冻结的依赖文件。BIOS 安装替换只切换当前安装，已有运行保持冻结的旧文件，新的启动按需重校验；Game 存档保留。

依赖 snapshot 是规范 JSON，包含所选 BIOS、parent/base、多盘或 runtime pack 的实际闭包。Variant 保存当前 snapshot，Launch 创建时复制 snapshot 并锁定实际 Blob 边。

静态 BIOS/多盘和 Arcade 依赖均采用当前 `schemaVersion:1`，分别以 `kind:STATIC/ARCADE` 区分实际类型，不根据历史版本号选择解析器。

## 5. 导入、审核与刮削

Upload、Archive、ImportJob、ImportItem、来源快照、Validation、ReviewDraft/Event、ScrapeRun 与服务器导入维持各自 owner、版本、幂等和 payload release 边界。运行选择只保存稳定 `provider_id/target_id`；ReviewDraft 只选择与当前来源、目录 Core、Target、DAT、依赖和内容策略完全匹配的 Validation，写事务发现输入变化时直接创建或切换当前选择。历史校验不进入当前 HTTP 投影，Provider Bundle 单独升级不使审核结果失效。

来源快照是不可变的输入证据，不是业务版本树：不分配 revision 序号；每个 Item 最多一份 `created_by=IDENTIFICATION` 初始来源，当前来源只由 `ReviewDraft.effective_source_snapshot_id` 选择，不按创建时间或最大序号猜测。

Upload 的业务用途只区分 `GENERAL/PROJECT`，并独立记录文件/目录形态；项目引擎由归一化后的真实内容检测。审核不存储算法 generation；目录展示变化和不相关能力变化不参与有效性摘要。

检查摘要不设跨历史记录的唯一约束：依赖从缺失变为可用、再变回缺失，是新的检查结果，即使输入摘要与较早记录相同也必须能正常保存。未变化的重复检查复用当前结果，不新增记录。RPG 的导入、重新检查和发布共用项目资源策略；外部 RTP 声明默认阻断，管理员的显式自包含确认与声明一起写入依赖快照并参与摘要，发布事务重新核对，不以是否打开过 Player 作为就绪条件。

运行包安装、选包与运行挂载已退出产品。冻结的历史 migrations 及既存 Blob 引用保护仍保留，避免改写 checksum 或误回收已有 payload；它们不再接受应用创建新的安装或绑定。该调整不重建开发库，也不转换、删除已有游戏及存档。

发布事务将审核 metadata、媒体、内容文件与默认 Variant 一次写入 Game current state。重新刮削以稳定 `game_id` 为 owner 创建候选；显式应用候选才更新当前 metadata/assets，不能因为旧内容版本表已经删除而丢失 Game 关联。

RPG Maker profile 保存实际检测得到的项目 fingerprint、generation、Provider/Target 和依赖摘要，不保存运行 gate、位置证明或独立验证决定。所有审核通过 `review_preview_sessions` 试运行，来源文件与校验产物分开锁定；`RUNTIME_FILE` 只能引用该审核所选校验的产物或已选运行资源包，不能借试运行读取其他来源的 Blob。

审核临时 checkpoint 使用会话级存储，一份 preview 保留当前临时 payload，格式及 Blob 关系明确。恢复 preview 冻结自己的恢复输入，不跟随原 preview 后续覆盖。已关闭会话的临时 checkpoint 可在审核未结束且未到期时用于恢复；过期或审核 payload 释放时清理。临时存档不是审批/升级门槛，不引入原会话、恢复会话或人工确认的附加状态机。

### ScummVM 检测与选择

`SCUMMVM_PROJECT` 沿用项目来源快照、`PROJECT_FILE`、Validation、ReviewDraft 与当前 Variant，不新增游戏特征库或第二套候选关系表。Validation 的 dependency snapshot 保存 `schemaVersion=1/kind=SCUMMVM`、来源内容摘要、固定检测器上游 commit、完整候选与根目录集合，以及当前 `selectedCandidateId`。候选 ID 由来源摘要和全部有界检测字段计算；客户端不能自行生成或复用另一份来源的 ID。

选择只产生新的不可变 Validation，并在带版本检查的事务中切换 ReviewDraft 当前校验；原检测结果与历史 Validation 不被覆盖。发布复制所选校验到当前 Game Variant，重新验证保留来源匹配的准确选择，不能以通用 BIOS 空结果覆盖 ScummVM 快照。完整项目树保留全部根目录，所选 root 只是启动输入。ScummVM 没有必需的单 `CONTENT` 文件，不进入单 ROM 的 BIOS 哈希解析。

原生存档格式与槽位属于 Provider payload，数据库只记录公共 checkpoint format/大小/摘要；预览存档与正式用户存档继续使用既有 owner、冻结恢复输入和释放规则。

### 批次丢弃与服务器上传归属

`import_batch_discards` 对 `(kind,import_id)` 只保留一个当前处置，kind 为普通导入、Pegasus 或 EmulationStation。`REQUESTED → COMPLETED|FAILED`，失败可回到 REQUESTED；记录请求管理员、错误码和毫秒时间，不增加试玩 revision 或按运行次数累积记录。来源批次由服务校验；请求落库即通过 `discarded_import_jobs` 视图及 trigger 阻止该批次再次发布、重试导入。

`server_import_upload_owners` 将内部 UploadSession 唯一关联到一个来源 Item，与内部上传同事务创建，覆盖“创建内部导入后、尚未交接审核前”的中断和不支持格式分支。UploadSession 删除级联移除此归属；该表仅记录身份，不增加 Blob 引用。旧来源按确定性上传 ID 恢复；旧 Pegasus 随机 ID 仅在完整文件集合、目标和执行时间以及内部 manifest 摘要唯一匹配时恢复，歧义保留数据并报错。

批量处置中的真实待审核 Item 通过正常 Discard 事务生成审核决定。未产生审核的失败来源也可进入 `REVIEW_DISCARDED`，由批次处置作为证据，保留原错误码和详情，不伪造 ReviewEvent。PUBLISHED/SKIPPED_EXISTING 不进入该转换；普通导入被取消的执行项与拒绝文件保留原终态及失败证据。引用释放仍以现有 payload state 和 release job 为唯一事实源。

## 6. Launch 与资源冻结

`launch_sessions` 保存 Game/Core、稳定 Provider/Target、冻结 `bundle_sha256`、内容类型、依赖 snapshot、兼容状态、可选 save/netplay owner、凭据摘要和生命周期。`launch_content_files` 与 `launch_external_files` 锁定本次内容、BIOS、parent 和 disc Blob；创建后 Game、Variant、DAT、BIOS 或 Provider 当前态变化都不能改写既有 Launch。

Review Preview 使用相同冻结原则和 Player，但保留审核来源 owner，不创建假 Game。启动、心跳和退出只推进会话授权状态，不写入已发布游戏的游玩统计。Provider 静态资源由 Provider/Bundle/path 三元组读取并逐请求校验 allowlist 与摘要。

## 7. SaveState

`save_states` 保存 Profile、Game、checkpoint format、payload Blob/SHA-256/size、可选截图、DOS 路径/disc index 和来源 Launch。它不复制 Provider、Target、Bundle 或 Variant 身份。

写入必须来自同一 Profile/Game 的有效 PRODUCT Launch，且格式等于 Target 当前 `writeFormat`、大小不超过 `maxBytes`。恢复使用当前 READY Variant；只要当前 Target 声明可读该 checkpoint format 即可。不可读存档保留为 BLOCKED 投影，不加载旧 Provider、不 fallback，也不阻止无存档启动。

Provider 激活前必须保证现有未删除的持久用户存档格式仍在 `readFormats` 中；审核临时 checkpoint 不参与升级门槛，也不以 `maxBytes` 减少阻塞升级。审核结束由既有 payload release 清除临时引用；普通 GC 周期释放过期 preview 的 checkpoint/restore 引用。实际 CAS 删除仍按剩余 owner 与宽限期执行。

## 8. Play、隔离与联机

`play_sessions` 与事件使用连续 client sequence 计算有效游玩时长。`isolated_runtime_bootstrap_tickets` 和 `isolated_runtime_capabilities` 为每个 Launch/Preview 提供一次性、exact-origin 授权。

Netplay room、session、participant 与 event 保存当前选择和会话冻结态。Netplay session 冻结 Provider/Target、Bundle、内容/依赖摘要和 profile；参与者 Launch 必须一致。联机兼容由标准 Target 能力和 profile 精确匹配决定，不使用平行稳定 Target字段。

## 9. Blob ownership 与释放

每个 CAS Blob 必须存在于 `internal/blobregistry/registry.json` 并由 payload release ownership registry 分类。流程进入终态后由持久 Job 单向释放 consumption；最后一个保护引用消失后才建立 GC candidate，并等待配置宽限期。

Game 内容替换会立即移除旧 Game-owned 与 Game-runtime-owned 边；BIOS 替换只切换当前安装，后台分批释放旧安装与过时 Variant BIOS 边，已创建 Launch 的冻结边保留至结束或过期；Game 删除移除内容、媒体、存档和运行边。共享 Blob 始终由剩余 owner 保护。

### BIOS 与 Launch 延迟回收

`013_bios_session_retirement.sql` 为未释放的非活动 BIOS 安装、按 Blob 定位的 `BIOS_BUNDLE` VariantFile 和当前活动 BIOS Blob 建立索引。替换事务不遍历依赖 JSON，也不更新 GameVariant、Launch、Play、Netplay 或 SaveState。旧安装仍持有 Blob，后台每个事务最多移除 200 条旧 Variant BIOS 边；同一 Blob 仍被其他活动安装采用时保留这些边。释放安装的 Blob 引用后仍保留名称/hash/来源审计。

`launch_payload_retirements` 是 Launch 的回收排期，包含 `launch_session_id`、`due_at_ms`、`released_at_ms`。迁移为现有会话建立排期，插入/状态/心跳更新 trigger 同事务维护截止时间：CREATED 取 bootstrap/hard 最早值，ACTIVE 取 idle/hard 最早值，终态取 finished 时间；已释放行不重新入队。后台按未释放截止时间的部分索引逐会话处理，每个短事务分别最多释放 200 条内容文件和 200 条外部文件引用。超时会话标为 EXPIRED，并按 Launch ID 结束对应 Play；大项目跨批次继续，全部文件引用释放后才记录释放时间。存档和会话来源记录保留，物理文件仍受其他 owner 与 GC 宽限期保护。普通启动与每小时 GC 对账重试未完成工作；单次替换无需等待对账，服务重启可续做。

## 10. 数据库不变量

`010_cross_domain_invariants.sql` 至少保证：

- Provider/Target 引用命中当前 catalog，Launch/Netplay 的 Bundle 命中创建时的当前 Provider；
- Game、Variant 的稳定 owner 和逐次 `version` 更新；
- Launch、Preview、Save、Validation 与资源 owner 一致；
- checkpoint format 位于 Target 的可读格式集合；
- 隔离 capability 的 owner/origin/expiry 一致；
- payload release 不产生悬空 Blob 引用；
- 来源快照、gate event 和其他证据保持不可变。

新增运行时引用时必须复用稳定 Provider/Target 和既有 Bundle 冻结规则，禁止新增第二套运行选择字段或从 Target ID 推导 Provider 私有实现。

### 原生游戏数据存档

`game_save_versions` 按 `save_state_id` 关联 `save_states`，其 `data_version` 只随原生数据更新递增，与包含重命名的通用 `version` 分开；`last_synced_at_ms`
为空表示此前未提交原生数据，`last_writer_launch_session_id` 关联最近实际写入的 Launch。
`source_launch_session_id`、ID、名称与创建时间在确认覆盖中保持不变。显示/分页按 `COALESCE(last_synced_at_ms,created_at_ms)`。

`launch_game_save_bindings` 只绑定声明 `GAME_SAVE` 的 Product Launch，记录目标、预期数据版本、初始累计时长与冻结恢复 Blob。
无存档启动的绑定目标为空且预期版本为 0，首次同步创建并绑定；目标删除后保留非零版本，以禁止错误重建。
同一事务比较数据版本并替换完整 payload/截图，其他会话先写入则冲突。冻结恢复 Blob 是保护性引用，终态清除；统一 Blob registry、容量统计与 GC 保护该引用。

浏览器的 GAME_SAVE 草稿不新增服务端数据表。IndexedDB 按账号与 Launch 隔离，保存完整 checkpoint、截图、标题、来源恢复标记、
更新时间和固定幂等请求；它不参与 Launch 恢复输入。用户确认提交时才通过既有 launch_game_save_bindings 原子更新正式存档。
