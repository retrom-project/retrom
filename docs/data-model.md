# Retrom 数据模型

字段、CHECK、FK、索引与 trigger 的逐字节事实源是 `migrations/001_identity.sql` 至 `migrations/010_cross_domain_invariants.sql`；本文只描述稳定领域关系。HTTP 字段以 `api/openapi.yaml` 的统一 bundle 为准。

## 1. 基线

- 项目尚未发布，001–010 直接创建最终 current-state 模型；不兼容的开发库必须停机归档并使用空数据根重建，不执行旧表转换、兼容回填或 DROP/ALTER 迁移。首次正式发布后才采用只追加的向前升级，不提供降级、回滚、双写或运行时 schema 修补。
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

`bios_requirements`、`dat_versions` 和服务器 BIOS 导入项引用稳定 Provider/Target。当前 active DAT 可以前移；已创建 Launch 只消费其冻结的依赖文件。BIOS 安装替换会撤销受影响的活动运行并把当前 Variant 置为待重验，但不会删除仍可由当前 Target 读取的 Game 存档。

依赖 snapshot 是规范 JSON，包含所选 BIOS、parent/base、多盘或 runtime pack 的实际闭包。Variant 保存当前 snapshot，Launch 创建时复制 snapshot 并锁定实际 Blob 边。

静态 BIOS/多盘和 Arcade 依赖均采用当前 `schemaVersion:1`，分别以 `kind:STATIC/ARCADE` 区分实际类型，不根据历史版本号选择解析器。

## 5. 导入、审核与刮削

Upload、Archive、ImportJob、ImportItem、来源快照、Validation、ReviewDraft/Event、ScrapeRun 与服务器导入维持各自 owner、版本、幂等和 payload release 边界。运行选择只保存稳定 `provider_id/target_id`；ReviewDraft 只选择与当前来源、目录 Core、Target、DAT、依赖和内容策略完全匹配的 Validation，写事务发现输入变化时直接创建或切换当前选择。历史校验不进入当前 HTTP 投影，Provider Bundle 单独升级不使审核结果失效。

来源快照是不可变的输入证据，不是业务版本树：不分配 revision 序号；每个 Item 最多一份 `created_by=IDENTIFICATION` 初始来源，当前来源只由 `ReviewDraft.effective_source_snapshot_id` 选择，不按创建时间或最大序号猜测。

Upload 的业务用途只区分 `GENERAL/PROJECT/RUNTIME_ASSET_PACK`，并独立记录文件/目录形态；项目引擎由归一化后的真实内容检测。审核不存储算法 generation；目录展示变化和不相关能力变化不参与有效性摘要。

检查摘要不设跨历史记录的唯一约束：依赖从缺失变为可用、再变回缺失，是新的检查结果，即使输入摘要与较早记录相同也必须能正常保存。未变化的重复检查复用当前结果，不新增记录。RPG 的导入、重新检查和发布共用现有 pack resolver；可用 RTP 的选择随校验冻结，发布事务重新核对真实依赖，不以是否打开过 Player 作为就绪条件。

发布事务将审核 metadata、媒体、内容文件与默认 Variant 一次写入 Game current state。重新刮削以稳定 `game_id` 为 owner 创建候选；显式应用候选才更新当前 metadata/assets，不能因为旧内容版本表已经删除而丢失 Game 关联。

RPG Maker profile 保存实际检测得到的项目 fingerprint、generation、Provider/Target 和依赖摘要，不保存运行 gate、位置证明或独立验证决定。所有审核通过 `review_preview_sessions` 试运行，来源文件与校验产物分开锁定；`RUNTIME_FILE` 只能引用该审核所选校验的产物或已选运行资源包，不能借试运行读取其他来源的 Blob。

审核临时 checkpoint 使用会话级存储，一份 preview 保留当前临时 payload，格式及 Blob 关系明确。恢复 preview 冻结自己的恢复输入，不跟随原 preview 后续覆盖。已关闭会话的临时 checkpoint 可在审核未结束且未到期时用于恢复；过期或审核 payload 释放时清理。临时存档不是审批/升级门槛，不引入原会话、恢复会话或人工确认的附加状态机。

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

Game 内容替换会立即移除旧 Game-owned 与 Game-runtime-owned 边；BIOS 替换只移除旧 BIOS 相关运行边；Game 删除移除内容、媒体、存档和运行边。共享 Blob 始终由剩余 owner 保护。

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
