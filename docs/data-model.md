# Retrom 数据模型

本文描述稳定领域关系和跨表不变量。字段、CHECK、FK、索引与 trigger 的逐字节事实源是
`migrations/001_identity.sql` 至 `migrations/011_emulationstation_import_liveness.sql`；本文不复制一份会漂移的 SQL 字典。
HTTP 字段以 `api/openapi.yaml` 的统一 bundle 为准。

## 1. 基线与身份

- 当前项目尚未发布，只支持 clean 001–011 lineage。非当前开发数据库必须更换为空数据根后重建；没有降级、旧表转换、双写或运行时 schema 修补。
- 业务主键使用 UUIDv7 字符串；SHA-256 使用 64 位小写十六进制；业务时刻使用 Unix 毫秒 `INTEGER`。
- 数据库只保存摘要、逻辑 ID 和相对存储键，不保存 Launch 明文凭据、Cookie、CSRF token、用户主机绝对路径或第三方内容明文。
- current pointer 只前移到同 owner 的不可变 revision。完成态 revision、事件、快照和证据禁止修改。

## 2. Runtime Provider catalog

运行时部署和选择只有以下公开层级：

```text
RuntimeProvider
  └─ RuntimeTarget
       ├─ RuntimeTargetBinding ── Product Core / Platform / content kind
       ├─ BIOSRequirement / DatVersion
       ├─ Import validation / VariantRevision
       └─ LaunchSession / SaveState / NetplaySession
```

### 2.1 `runtime_providers`

每个 Provider 恰好一行，冻结 `provider_id`、SemVer `provider_version`、正整数 `provider_api_version`、Bundle/manifest/client
module SHA-256、`source=candidate|production` 和单调激活时刻。production 还必须有不可移动 repository/tag/commit；
candidate 的三项 release 身份全为空。

数据库只保存已验证的结构事实；当前安装器和 active loader 仍只支持 Provider API 1，其他正整数不能被激活。

`provider_id + bundle_sha256` 唯一。同一版本换 bytes、降级、身份不一致或活动文件突变均在启动协调前拒绝。

### 2.2 `runtime_targets`

Target 由 Provider manifest 投影，主键是 `(provider_id,target_id)`。每行冻结展示名、游戏兼容线、可空联机兼容线、
闭合 `target_options_schema_json`、capabilities、checkpoint contract、公开 manifest fragment 和 canonical fragment 的
`target_contract_sha256`。

数据库不保存 Provider 私有 adapter、core 或 asset mapping。任何消费者只引用 Provider、Target 和 contract digest；
Target 公开字段只能来自已校验 Bundle manifest。

### 2.3 bindings 与 catalog state

`runtime_target_bindings` 把产品 `core_id` 绑定到一个 Target，并声明 detector、delivery、launch policy 和 review
policy；`runtime_binding_platforms`、`runtime_binding_content_kinds` 收紧适用平台与内容类型。内容类型通过
`content_kinds` reference catalog 外键约束，不在多张业务表复制固定 `CHECK IN (...)` 列表。一个 Target 只能有一条
Host binding。`runtime_catalog_state` 保存当前完整 catalog 的单调版本、canonical digest 和激活时刻。

EmulatorJS Provider 当前声明 35 个 Target；retrom-runtime Provider 当前声明 12 个 Target。RPG Maker 对用户只有
`rpgmaker` Core，七个世代是 retrom-runtime Target，不是七个用户可选 Core。

## 3. 平台、目录与游戏

- `platforms`、`cores`、`platform_cores` 是 reference catalog；`platform_instances` 是管理员维护的游戏目录。
- `games` 只归属一个 PlatformInstance。metadata、媒体、内容与运行兼容性分别使用不可变 revision 或关系表。
- `game_content_revisions` 与 `game_content_files` 冻结用户内容；原始 bytes 进入 CAS，逻辑路径必须通过安全路径规范。
- `game_variants` 是 `(game,core)` 稳定逻辑槽；`game_variant_revisions` 是 append-only 验证结果。

`game_variant_revisions` 冻结 `provider_id`、`target_id`、`target_contract_sha256`、游戏兼容线、可空 DAT、validation
input digest、依赖快照和状态。READY 结果必须引用当前内容 revision，且 Provider/Target/contract 与 binding 一致。
EmulatorJS 可额外保存正整数 `emulator_game_id`；其他 Target 不伪造该值。

## 4. 依赖、DAT 与运行包

- `bios_requirements`、`dat_versions` 和服务器 BIOS 导入项都引用 Provider Target 及其 contract digest。
- 同一 Target 只有一条 READY active DAT；DAT 更新创建新版本，不改写已发布 VariantRevision 的冻结引用。
- `bios_installations`、DAT machine/ROM/disk/BIOS set 表保存解析与安装证据；Blob 可去重，领域安装状态不可跨 Target 复用。
- RPG runtime asset pack definition/installation/file 和 Variant pack selection 保持独立领域模型。被 Variant 或可恢复 checkpoint 引用的安装不能删除或替换。

依赖 snapshot 必须由规范 JSON 计算摘要，包含 Target contract、DAT、BIOS、parent/base、多盘或 runtime pack 的实际闭包。
普通 Launch 只消费 VariantRevision 已冻结的闭包，不读取“当前最新”依赖重新拼装。

## 5. 上传、导入与审核

上传、归档、ImportJob、ImportItem、来源快照、validation、ReviewDraft/Event、ScrapeRun 与服务器导入都保持原有
owner、版本、幂等和 payload release 边界。与运行时有关的冻结字段统一为：

```text
provider_id
target_id
target_contract_sha256
game_compatibility_line（适用时）
```

`import_jobs` 与 `import_item_core_validations` 在创建时保存 Target identity；Review 的运行输入变化递增
`runtime_binding_revision`。标题、标签或媒体变化不能偷换 Target。发布事务必须重新比较来源快照、目标目录、Core、
Target contract、依赖 snapshot 和所需 runtime validation；任一漂移返回稳定的 validation-stale 错误。

RPG Maker 内容证据保存在 `rpgmaker_content_profiles`；审核和 validation 保存 generation、Provider Target、project
fingerprint 与依赖摘要。`rpgmaker_runtime_validations` 的原 Launch 和 restore Launch 必须不同，gate event 连续、
append-only，临时 checkpoint 在终态后进入 payload release，不进入用户存档列表。

## 6. Launch、资源与游玩

`launch_sessions` 对 PRODUCT 与 RPG runtime validation 使用同一模型，并冻结 Provider、Target、Target contract、
游戏兼容线、Bundle digest、内容/Variant/validation owner、capability 摘要、用途、return path、生命周期和可选 netplay
信息。

明文 capability 只在浏览器 Cookie 中存在。`launch_content_files`、`launch_external_files` 与 Provider resource builder
把当次授权内容投影为 Launch Envelope resources；创建后 current pointer、DAT 或安装变化不能改变本次资源。

`play_sessions` 与事件通过连续 client sequence 计算有效时长。页面不可见、运行暂停或失联区间不计时；重放同一
sequence 幂等，跳号冲突。

## 7. Opaque checkpoint 与存档

`save_states` 不解释 Provider payload。每行冻结 Profile、Game、ContentRevision、VariantRevision、Provider、Target、
Target contract、游戏兼容线、`checkpoint_format`、依赖 snapshot、可空 DAT/DOS/disc、payload Blob/SHA-256/size、
来源 Launch 和可选截图。

来源 Launch、VariantRevision 和 SaveState 的运行身份必须逐字段一致。写入格式必须等于 Target 的
`checkpoint.writeFormat`；恢复时格式必须在当前 Target 的 `readFormats` 中且 payload 不超过 `maxBytes`。Host 只校验
外层格式、大小和摘要，不解析 EmulatorJS state、RPG bundle、KiriKiri/ONS/Tyrano snapshot 等 Provider 私有内容。

普通游戏启动使用当前已激活的兼容 Target；旧 SaveState 是否可恢复由同一游戏兼容线和 `readFormats` 决定。不可读
存档保留为 BLOCKED 投影，不自动加载旧 Provider、不 fallback，也不阻止无存档启动。

## 8. 隔离运行时

`isolated_runtime_bootstrap_tickets` 与 `isolated_runtime_capabilities` 为每个 Launch 或 Preview 提供一次性 ticket 和
host-only HttpOnly capability。数据库只存摘要、期望 origin、owner 和 expiry。ticket 只能消费一次；Launch 结束、
过期或清理会撤销 capability。用户项目 JavaScript 只能在该 Launch 的 unique origin 执行，不能退回应用 origin。

## 9. Netplay

房间、成员、Session、参与者和事件保持单进程控制面。`netplay_sessions` 冻结 Provider Target、Target contract、
联机兼容线、内容和依赖 identity；参与者 Launch 必须完全相同。Provider 只通过标准 `RuntimeNetplayPort` 暴露帧、输入、
state 与 hash；联机 Launch 禁止创建或恢复普通存档。

## 10. Blob ownership、GC 与删除

所有 CAS Blob 必须至少有一个领域 owner 或流程 consumption。流程进入真终态后由持久 PayloadRelease Job 单向释放
consumption；最后一个引用消失后才建立 GC candidate，并等待配置宽限期。Game 永久删除保留文字墓碑与审计，运行
能力立即关闭，独占 payload 异步释放；共享 Blob 继续受其他 owner 保护。

## 11. 跨域 trigger 最低集合

`010_cross_domain_invariants.sql` 必须保证：

- 所有 Provider/Target/contract 引用命中同一已激活 catalog；
- Variant、Launch、Save、Validation、DAT、BIOS 和 Netplay 的复合身份一致；
- current pointer、不可变 revision/event/snapshot 不被回写；
- Save 与来源 Launch owner、依赖、格式一致；
- 隔离 capability owner/origin 一致；
- payload release 与 Blob owner 不会产生悬空引用。

任何领域新增运行时引用时，都必须使用同一三元组并在 010 或后续已发布 migration 中补复合约束；禁止新增第二套
运行选择字段或从 Target ID 推导 Provider 私有实现。

`011_emulationstation_import_liveness.sql` 保留上述约束，并允许 EmulationStation 条目在 library import 未产生可交接的 review item 时从 `COPYING` 直接进入 `BLOCKED_CONTENT`。任何条目处理返回后都不得仍处于 `PENDING | COPYING | VALIDATING`；否则 worker 终止当前执行并按有界退避重试，不得立即重复处理同一条目。
