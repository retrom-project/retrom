# 一期实施编排与交付门禁

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期实施基线 |
| 版本 | 1.3 |
| 日期 | 2026-08-10 |
| 用途 | 规定实现顺序、依赖和里程碑退出条件，不复制领域规格 |

## 1. 使用方式

本文回答“按什么顺序实现才不会返工”。产品边界以[总体架构](./retrom-product-architecture.md)为准，数据库字段以[数据模型](./data-model.md)为准，HTTP 以[API 契约](./http-api-contract.md)和实现后的 `api/openapi.yaml` 为准，最终通过与否只以[统一验收](./project-acceptance.md)为准。本文不得成为第二套字段、路由或验收步骤。

实现 Agent 开始一个里程碑前必须：

1. 阅读根级 `AGENTS.md`、本文及该里程碑引用的专题；
2. 确认前序里程碑的退出门禁已通过，不能用临时 mock 绕过前序基础设施；
3. 先改事实源，再生成或实现依赖它的产物；
4. 把本次自测发现的 bug 先固化为失败用例，再修复；
5. 在交付中列出实际执行的 Case/命令和未执行原因。

## 2. 不可倒置的依赖链

```mermaid
flowchart LR
    D["版本化依赖 manifest"] --> F["工程脚手架与门禁"]
    F --> S["迁移、种子、CAS、任务"]
    S --> A0["账户、授权与 Profile 隔离"]
    A0 --> U["上传协议"]
    U --> I["识别、刮削、审核"]
    S --> B["BIOS / DAT"]
    I --> G["Game / Metadata / Variant"]
    B --> G
    G --> L["目录、详情与 Launch"]
    L --> R["Player / EmulatorJS"]
    R --> P["存档、时长、恢复"]
    P --> A["完整验收与镜像"]
```

以下顺序是强约束：

- 数据库迁移先于 repository/store；store 先于领域 service；领域 service 先于 HTTP handler。
- 新增或改变 HTTP 行为时先改 `api/openapi.yaml`，再运行生成器，最后实现 strict handler 和前端调用；禁止先手写 DTO/URL 再补 schema。
- 后端状态机和错误码完成并有测试后，前端才能实现对应成功、空、警告和阻断状态；不得以页面本地假状态代替服务端不变量。
- 上传 bytes 必须先安全落入临时区/CAS，随后任务只引用 Blob/ArchiveEntry；worker 不接收浏览器路径或内存中的大文件对象。
- Arcade 识别必须在目标 CoreArtifact 的 DAT 可用后进行；Hasheous 只生成展示候选，不能替代 DAT 或阻断无候选的审核。
- Launch 只能引用已提交的 READY VariantRevision 和依赖快照；Player 不自行选择 core、DAT、BIOS、ROM 或 URL。
- 依赖准备发生在 `make dev`/镜像 builder 前置阶段；同步启动只校验依赖字节并登记缺失的 DAT_PARSE Job，Worker 可从已校验的只读本地 payload 建索引，但任何进程都不能为方便实现而加入运行期下载。

## 3. Migration 落地顺序

Migration 文件一旦进入已发布分支不可改写。首版有序边界固定如下；可在同一边界内拆成多个小 migration，但不能把后置外键表提前到其依赖之前：

1. `profiles/platforms/cores/core_artifacts/platform_cores/platform_instances` 与代码 seed；
2. `blobs/jobs/job_input_snapshots/job_events/idempotency_records/audit_events/blob_gc_candidates`；
3. `upload_sessions/upload_files/upload_parts/upload_consumptions/archive_entries`；
4. `bios_requirements/bios_installations/dat_versions/dat_machines/dat_bios_sets/dat_rom_entries/dat_disk_entries/dat_import_jobs/dat_diff_snapshots/dat_diff_items`；
5. `import_jobs/import_job_files/import_items/import_item_source_files/import_item_dos_entries/import_item_core_validations/import_item_validation_files/content_identity_claims/import_item_duplicate_matches`；
6. `games/game_metadata_revisions/game_assets/game_content_revisions/game_content_files/game_variants/game_variant_revisions/variant_files/dos_entries/variant_dependencies`；这些循环 current FK 在同一 schema 边界声明 deferred；
7. `metadata_scrape_runs/content_hash_evidence/metadata_provider_cache/metadata_provider_responses/metadata_scrape_query_attempts/scrape_candidates/scrape_candidate_hits/scrape_candidate_assets/review_uploaded_assets/review_drafts/review_draft_screenshot_assets/review_events`；Game owner 的 scrape run 因而不会引用尚未建立的 Game/ContentRevision；
8. `launch_sessions/play_sessions/play_session_events/save_states/persistent_saves/persistent_save_revisions`；
9. 全部 partial unique index、不可变/归属 trigger、搜索索引和 schema checksum 回归。

账户版本追加的 migration 顺序固定为：020 建立 User/Credential/AuthSession/AccountLink/InstanceState/RateLimit 并移除 `local` Profile；021 修正 test bootstrap 从 PENDING 首次完成时的默认密码状态 trigger；022 将幂等记录主键绑定 `principal_id`；023 把 AuditEvent、ReviewEvent 和 ImportJobFileResolution 重建为真实 USER/SYSTEM actor。私有数据与 cursor 的 principal 隔离由同一版本的 service/query 实现和测试保证，不伪装成额外 schema migration。020 是破坏性重建边界：既有 001–019 数据库在任何写入前拒绝，只有不存在的数据库、真正空的 schema 或已经包含 020 的库可以继续。

测试环境允许直接重建数据库，当前 migration 集以新建库为交付基线；循环 current pointer 使用数据模型规定的 deferred FK，不能由运行时代码关闭 foreign keys、事后修补 NULL 或使用启动时 DDL。`archive_entries` 的 ZIP/7z 通用列在初始归档 migration 中建立；014 增加核心目录、BIOS delivery 与 Launch external-file 能力；015 收敛旧零 Item 导入；016 增加拒绝文件重新配置 lineage；017 增加重复内容识别证据；018 增加异步 DAT 差异快照与分页明细；019 增加审核来源快照、Validation/草稿快照归属、Arcade Parent Attachment、Job/ReviewEvent enum 和发布 digest 兜底。019 因 SQLite 无法原地改变 CHECK/非空列而使用带 `-- retrom: rebuild-with-foreign-keys-off` 的唯一受控重建：store 只识别 019，先复算旧 source manifest，事务外关闭外键，事务内重建/回填，提交前执行 `PRAGMA foreign_key_check`，随后无条件恢复外键；任何漂移或外键错误都使启动失败。正式发布后继续遵守只追加 migration 的升级纪律。

## 4. 里程碑

下列退出门禁只列“该里程碑结束时已经能完整执行”的 `ACC-*`。一个 Case 跨越多个模块时只能在所有前置都存在后首次列入，不能在较早里程碑记录“部分 PASS”；较早阶段以对应 package unit/integration/contract test 作为局部门禁，最终仍必须运行完整 Case。

### M0：可重复工程基线

范围：固定 Go/Node/npm 和工具版本；建立 Makefile、lint、测试、OpenAPI 生成、`web/` App Shell、CI；实现 manifest 小文件校验、依赖物化/离线校验和双镜像骨架。`make dev` 只编排宿主机进程。

退出门禁：`ACC-QA-001`–`003`、`ACC-PKG-001`–`003`、`ACC-DEV-001`。镜像 Case 可以在本机无 Docker 时明确留到具备 Docker 的 runner，但不得因此开始依赖镜像内部未定义路径的功能。

### M1：进程、数据与协议骨架

范围：配置一次性加载、launch key 安全生成、按第 3 节建立数据字典的完整首版 migration/checksum/trigger（领域 service 可后续实现）、SQLite PRAGMA、seed、CAS 原子发布/GC、任务租约、统一错误/日志、session/health/封闭诊断摘要 OpenAPI 与同源代理/CSP。

退出门禁：完整执行 `ACC-DB-001`–`002`、`ACC-CAS-001`–`002`、`ACC-SEC-003`、`ACC-OPS-001`、`ACC-NET-001`，以及条件满足时的 `ACC-NET-002`。此时不需要把硬编码游戏暴露给 UI；Case/集成测试直接在临时库建立最小领域 fixture。`ACC-SEC-001/002` 与 `ACC-API-001` 分别等待 DAT/Archive、Launch 和完整 route 集后执行。

### M1A：账户与数据隔离边界

范围：020–023 migration、release 初始化与显式 test 模式、Argon2id 密码和阻断列表、AuthSession/CSRF/Origin/限流、Invitation/PasswordReset、账号生命周期、principal-scoped 幂等/cursor、所有私有数据 SQL owner predicate、主机只读 `setup-code`、离线 `admin-reset` 与恢复安全围栏；前端完成 server-side 入口守卫、账户页和用户管理页。

退出门禁：`ACC-AUTH-001`–`006`、`ACC-ISO-001`–`003`、`ACC-DB-002`、`ACC-BKP-001` 和 `ACC-UI-009`。任一既有共享 `local`、匿名写入或恢复旧 cookie 的断言必须同时移除，不能保留两套运行模式。

### M2：上传与持久任务

范围：文件/目录 manifest、分块流式写入、resume/complete/cancel、`UPLOAD_FINALIZE` 异步组装与故障恢复、Archive 安全扫描、Upload consumption、通用 Job snapshot/SSE resume/retry/cancel。实现真实 CAS，不在前端或 handler 缓冲大 body，也不在 HTTP complete request 内组装最大 32 GiB 的 session。

退出门禁：`make test` 与 `make integration-test` 中 upload manifest/part/finalize/cancel、lease 恢复和三个 streaming operation 的聚焦 contract test 全通过；此时 ImportItem pipeline 尚未实现，所以不得把包含导入/审核断言的 `ACC-IMP-001/002/008` 或全路由 `ACC-API-001` 标为 PASS。

### M3：依赖识别、刮削与审核

范围：内置 DAT 安全解析/索引、BIOS catalog/installation、Arcade machine/多级 parent/逐级 romof V2 闭包与 Parent ZIP 审核补充；不可变 ImportItem 来源快照、Attachment Job/retry/cancel/cleanup；Hasheous adapter/cache/媒体安全；ImportItem 状态机、拒绝文件基于既有 CAS Blob 的重新配置导入与任务 lineage、ReviewDraft/Event、审核工作台与历史。普通测试只用 fake Hasheous，外部 smoke 有界且不决定测试结果。

退出门禁：完整执行 `ACC-IMP-001`、`ACC-IMP-003`、`ACC-IMP-005`、`ACC-IMP-008`、`ACC-DAT-001`、`ACC-DAT-003`、`ACC-DAT-005`、`ACC-BIOS-001`、`ACC-SEC-001` 与 `ACC-SEC-004`；Arcade Parent 补充分步链的纯逻辑、018→019 migration、service/HTTP 与前端聚焦测试必须通过。其余聚焦 unit/integration test 全通过。在 Approve 事务接入前先完成 M4 的 Game aggregate store；M3 与 M4 可以按垂直切片交替推进，但不得用另一套临时发布表。涉及补充后 Approve/Launch、Game 移动/启动、DAT 对 Game 重校验或 BIOS Launch options 的 Case 明确留到 M4/M5，不能用半实现记录 PASS。

### M4：目录与游戏聚合

范围：PlatformInstance 生命周期与默认 core 影响预览；Game/MetadataRevision/Asset、GameContentRevision/ContentFiles、Variant/VariantRevision；审核原子发布、异步游戏内容替换 Job/重试、游戏编辑/移动、带标题确认和幂等重放的软删除、搜索与 cursor。Game current content 是唯一普通启动来源；失败兼容验证按 `validation_input_digest` 去重，Variant current 只指 READY。

退出门禁：完整执行 `ACC-IMP-002`、`ACC-IMP-004`、`ACC-IMP-006`、`ACC-IMP-007`、`ACC-PLAT-001`、`ACC-PLAT-002`、`ACC-PLAT-005` 与 `ACC-GAME-001`；Game store/审核发布/搜索/不可变 revision 的聚焦测试全通过。依赖普通 Launch 的 `ACC-PLAT-003/004`、`ACC-GAME-002/003` 留到 M5。完成后禁止任何页面继续读取硬编码游戏卡片。

### M5：用户目录与一键启动

范围：首页、游戏库、详情、存档列表空壳的真实 API；LaunchSession/HMAC capability、全部受限内容端点、Core option 状态、`EnsureVariant` 的 202/SSE/自动二次 POST 协议、BIOS/parent bundle、DOS 锁定原 bundle 加 seekable 虚拟 ZIP 引导；Player Shell 通过显式 `playerAdapterId` 设置锁定版本 globals/callback/external files 与 artifact override，未知/错配 adapter 在加载 loader 前阻断。基础 adapter 对应 v4.2.3，DOS whole-archive adapter 对应 v4.3.0-pre。

退出门禁：完整执行 `ACC-API-001`、`ACC-SEC-002`、`ACC-PLAT-003/004`、`ACC-GAME-002/003`、`ACC-DAT-002/004`、`ACC-BIOS-002`、`ACC-RUN-001`–`005` 与 `ACC-CORE-001`–`028`；若依赖版本发生变化另执行 `ACC-DAT-006`。核心 Case 与核心的映射以验收文档表格为准，使用本地授权夹具；夹具不可用时可以阻塞这些 Case，但不能以 mock 或历史截图判为通过。PPSSPP 的一个 Case 内含 ISO、CSO 两个独立 run，两者必须同时通过。

### M6：存档、时长与完整 UI

范围：状态存档+截图、PersistentSave revision/恢复竞态、PlaySession 连续 heartbeat、退出保存；完成全部用户/管理页面状态、1280 最小桌面/2560/4K 视觉与键盘可访问性。首页和“我的存档”的快速入口直接创建 Launch，不跳详情。

退出门禁：`ACC-SAVE-001`–`003`、`ACC-PLAY-001`、`ACC-UI-001`–`009`。

### M7：恢复、打包与最终验收

范围：离线 `retrom backup/restore`、数据根进程锁、统一 Blob reference registry、诊断脱敏全量复核、镜像最终 allowlist/许可、全量迁移升级、正式验收 runner/report；清除所有仅开发使用的 bypass、假数据和未归属 TODO。

退出门禁：`ACC-BKP-001`、全部适用 `ACC-*`、`make ci` 与两个镜像 build target。`ACC-DAT-006` 仅在版本基线相对上一接受版本变化时执行；`NOT_APPLICABLE` 必须有版本比较证据。

## 5. 垂直切片提交规则

每个可合并切片必须闭合以下链条，不允许把长期不工作的半成品推给后续 Agent：

1. migration/seed（若需要）；
2. store 与领域不变量测试；
3. OpenAPI、两端生成物与 contract test；
4. handler/worker 与错误映射；
5. 前端真实 API 状态和交互；
6. 对应聚焦测试、回归用例和正式文档同步。

尚未接通 UI 的后台能力可以先合并，但必须有可执行 API/集成测试且不暴露虚假菜单；尚未实现的路由不得返回伪成功。实验只能在测试或开发专用包内，不得写入生产 schema、seed、API 或设计稿。

多个 Agent 并行时以事实源拆分所有权：同一时刻只能有一个 Agent 修改 migration 序列、`api/openapi.yaml`、依赖 manifest 或 UI 源稿中的同一区域。合并前重新生成、运行漂移检查并处理语义冲突，不能只解决文本 merge conflict。

## 6. 可实施状态与剩余外部条件

一期产品决策、实体边界、协议、依赖版本、UI、质量与验收均已锁定，不存在需要实现 Agent 自行选择的产品阻塞项。以下是外部运行条件，不是许可 Agent 改规格的理由：

- 第一次 `make prepare-deps` 和镜像 dependency builder 需要访问 manifest 固定的公开 HTTPS runtime、DAT 与许可来源；正确缓存后校验与服务启动离线。
- 默认构建契约只产生私有自托管镜像；若未来要 push、公开或商业分发，必须先完成 manifest 标记的受限制 core 人工许可审查。这是分发授权边界，不允许实现 Agent通过删 notice、换浮动 core 或绕过检查来处理。
- 二十八个 core 的 ROM/BIOS 夹具由用户授权后保存在 Git 忽略目录；缺失只使对应 `ACC-CORE-*` 为 `BLOCKED`，不会授权提交 ROM/BIOS 或改用假游戏。夹具获取只允许读取操作者明确给出的主机和根目录。
- 生产需要前置 NG 提供同源 HTTPS、保留 nonce CSP/隔离头并挂载持久数据卷；Retrom 本身不实现 TLS。
- Hasheous 可临时不可用；导入仍进入人工审核，自动测试不依赖实时命中。

如果实施发现上游固定 bytes、浏览器行为或生成器能力与清单不符，应停在受影响里程碑，以可复现证据更新 manifest/正式契约和验收；不得加入静默 fallback、浮动版本或第二套实现。

## 7. 完成定义

“代码已写完”不是完成。项目一期只有同时满足下列条件才可判定可交付：

- 当前 schema、OpenAPI、生成物、实现、UI 与正式文档没有已知冲突；
- 所有适用验收 Case 为 PASS，条件 Case 为有证据的 PASS/NOT_APPLICABLE，不存在 FAIL；外部夹具导致的 BLOCKED 被明确报告且不能冒充通过；
- `make ci` 和两个仅构建镜像 target 通过，镜像不含 ROM、BIOS、下载 archive、数据库、缓存或开发工具；
- 自测、验收和评审发现的 bug 都有回归用例；
- 第三方 payload 仍由 manifest 物化且未进入 Git，运行数据只写配置的数据根；
- 交付报告包含目标 commit、环境、命令/Case、证据位置、未执行项与剩余风险。
