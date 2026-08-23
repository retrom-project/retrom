# 游戏目录（PlatformInstance）领域设计

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期实施基线 |
| 版本 | 1.2 |
| 日期 | 2026-08-22 |
| 适用范围 | Retrom 一期 |

## 1. 结论与命名

在 <code>Platform</code> 与 <code>Game</code> 之间增加 <code>PlatformInstance</code>：

- 领域模型、数据库和 API 使用 <code>PlatformInstance</code>。
- 领域说明、导入目标和跨页面归属文案仍称为“游戏目录”；管理后台一级菜单及该管理页标题使用更直接的“游戏目录”。
- 每个游戏目录从一个基础平台创建，并选择该平台已关联的一个核心作为默认核心。
- 游戏不再直接持有平台外键，只能且必须由一个游戏目录持有；基础平台通过游戏目录推导。
- “合集 / Collection”和“标签 / Tag”不承担此职责。实例级 Tag 已作为允许同一游戏重复加入的管理员多对多分类落地，但每个 Game 仍只有一个非空 `platform_instance_id`；标签筛选与导入默认值不能推断、移动或复制 Game 的目录。完整边界见 [`game-tags.md`](./game-tags.md)。

“游戏目录”表达的是一层可浏览的游戏目录，而不是服务器文件系统目录。涉及上传时，界面应使用“选择文件目录”或“上传目录”与其区分。

Pegasus source Collection 与游戏目录没有名称绑定关系。管理员必须逐 Collection 显式选择一个当前 enabled PlatformInstance 或明确跳过；`shortname`、Collection 名称、`extensions` 与 `launch` 只能作为非绑定展示/忽略字段，不能选择 Platform、Core 或 artifact。映射时冻结 PlatformInstance version、platform、默认 Core、enabled CoreArtifact version 与 active DAT；start 后这些字段不可改，后续配置漂移不能偷换本次运行目标。

## 2. 解决的问题

基础平台可能关联多个核心，但不同 ROMset 并不天然兼容所有核心。Arcade 尤其容易出现“用户不知道该选 FBNeo 还是 MAME 2003”的问题。

游戏目录同时承担两项职责：

1. 唯一归属：决定游戏放在哪个目录中，支持“FBNeo 游戏列表”“MAME 游戏列表”“FBNeo 飞行游戏”等组织方式。
2. 默认运行策略：决定没有存档上下文时优先使用哪个核心。

游戏目录不会缩小基础平台的可选核心集合。游戏详情仍展示基础平台关联的其他核心，并明确其兼容性状态；切换核心只影响本次启动，不修改游戏目录默认值。

## 3. 关系模型

~~~mermaid
erDiagram
    PLATFORM ||--o{ PLATFORM_INSTANCE : creates
    PLATFORM ||--o{ PLATFORM_CORE : supports
    CORE ||--o{ PLATFORM_CORE : belongs
    PLATFORM_INSTANCE }o--|| CORE : defaults_to
    PLATFORM_INSTANCE ||--o{ GAME : owns
    GAME ||--o{ GAME_CONTENT_REVISION : versions_content
    GAME ||--o{ GAME_VARIANT : has
    GAME_VARIANT ||--o{ GAME_VARIANT_REVISION : versions
    GAME_CONTENT_REVISION ||--o{ GAME_VARIANT_REVISION : validated_by
    CORE ||--o{ GAME_VARIANT : validates_with
    GAME_VARIANT_REVISION ||--o{ VARIANT_FILE : contains
    GAME_VARIANT_REVISION ||--o{ SAVE_STATE : creates
~~~

关键不变量：

- <code>Game.platform_instance_id</code> 必填；<code>Game</code> 不再保存 <code>platform_id</code>。
- <code>Game.current_content_revision_id</code> 必填并唯一决定普通启动使用的用户内容；改变目录默认 core 不改变该指针。
- 一个游戏同一时刻只能属于一个游戏目录。
- <code>PlatformInstance.default_core_id</code> 必须出现在其基础平台的 <code>platform_cores</code> 中。
- <code>GameVariant.core_id</code> 也必须属于游戏基础平台的启用核心集合；可执行文件和依赖属于不可变 <code>GameVariantRevision</code>。
- 游戏目录不持有 BIOS 或 DAT 副本。BIOS/DAT 仍按核心及核心 artifact 版本管理，多个游戏目录共享解析结果。
- 核心若仍是任一游戏目录的默认核心，不允许从该平台解除关联或被禁用。

## 4. 字段与数据库约束

<code>platform_instances</code> 必需字段：

| 字段 | 说明 |
| --- | --- |
| id | 主键 |
| platform_id | 基础平台，不允许在创建后直接修改 |
| default_core_id | 默认核心，必须属于该平台 |
| name | 展示名称，例如“FBNeo 飞行游戏” |
| slug | 稳定路由标识；同一基础平台内唯一 |
| description | 可选说明 |
| sort_order | 游戏库筛选和管理页排序 |
| enabled | 是否允许继续导入和出现在普通筛选项中 |
| version | 乐观并发版本，从 1 开始 |
| created_at_ms / updated_at_ms | UTC Unix 毫秒时间戳，SQLite INTEGER |
| deleted_at_ms | 软删除时刻；正常记录为空 |
| catalog_template_key | 可空的 release 推荐模板 key；只由一键补全写入，管理员手动创建始终为空 |

SQLite 关键约束示意：

~~~sql
CREATE TABLE platform_cores (
    platform_id TEXT NOT NULL,
    core_id TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (platform_id, core_id),
    FOREIGN KEY (platform_id) REFERENCES platforms(id) ON DELETE RESTRICT,
    FOREIGN KEY (core_id) REFERENCES cores(id) ON DELETE RESTRICT
);

CREATE TABLE platform_instances (
    id TEXT PRIMARY KEY,
    platform_id TEXT NOT NULL,
    default_core_id TEXT NOT NULL,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at_ms INTEGER NOT NULL CHECK (created_at_ms >= 0),
    updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= created_at_ms),
    deleted_at_ms INTEGER,
    catalog_template_key TEXT,
    UNIQUE (platform_id, slug),
    FOREIGN KEY (platform_id, default_core_id)
        REFERENCES platform_cores(platform_id, core_id)
        ON DELETE RESTRICT
);

CREATE UNIQUE INDEX platform_instances_catalog_template_key_unique
ON platform_instances(catalog_template_key)
WHERE catalog_template_key IS NOT NULL;

CREATE TABLE games (
    id TEXT PRIMARY KEY,
    platform_instance_id TEXT NOT NULL,
    -- 其余展示与状态字段省略
    FOREIGN KEY (platform_instance_id)
        REFERENCES platform_instances(id)
        ON DELETE RESTRICT
);
~~~

SQLite 无法仅靠上述外键验证 `platform_cores.enabled = 1` 或“GameVariant 核心属于 Game 间接关联的平台”。两条规则必须同时由服务层事务和数据库触发器保护，并有 migration/集成测试覆盖，不能只依赖前端下拉框。`slug` 由服务端从展示名称生成小写 ASCII 标识；名称无法产生 ASCII 单词时回退为 `<platform_id>-library`，同一基础平台发生冲突时追加从 `-2` 开始的最小可用序号。最终值匹配 `^[a-z0-9]+(?:-[a-z0-9]+)*$`、最长 80 byte，创建后不可修改。

所有其他时间点与时长字段遵循 [存储与数据库设计](./storage-and-database.md) 的 Unix 毫秒规则，不使用 TEXT 时间。

## 5. 默认核心与启动选择

启动核心选择优先级固定为：

1. 用户从某个存档继续：锁定该存档的 <code>GameVariantRevision</code> 与 CoreArtifact，游戏目录默认核心不参与覆盖。
2. 普通进入游戏详情：当前浏览器若为该游戏保存了显式核心偏好则优先选择它，否则选择当前游戏目录的 <code>default_core_id</code>。
3. 默认核心不可运行：不静默回退到其他核心；说明 ROM、parent、BIOS、DAT 或线程环境中的具体阻断原因，并让用户主动选择其他核心。

核心下拉框由基础平台的启用核心全集生成，而不是只由现有 <code>GameVariant</code> 生成。每项显示以下状态之一：

- “目录默认”：游戏目录的默认核心，且已通过当前游戏验证。
- “兼容”：已有有效 GameVariant，可直接选择。
- “需验证”：尚未针对当前文件 revision 建立兼容性结果；选择后执行预检并创建 GameVariant 或新的不可变 GameVariantRevision。
- “缺少依赖”：变体可识别但 BIOS/parent/base 之一未满足；保留可见，禁止启动，并用 reason 显示精确类型/逻辑文件名。
- “不兼容”：核心 DAT 或文件规则明确不匹配；保留可见并禁用，附原因。

用户在详情页应用核心选择后，由当前浏览器按 Game ID 保存显式偏好；后续普通启动继续显式提交该 Core，选回推荐核心即清除偏好。该偏好不写入服务端、不修改游戏目录，也不影响存档锁定的运行环境；它不是跨浏览器或全局的“最近核心”。

### 5.1 “需验证”核心的确定性物化

普通启动的 canonical source 是 `Game.current_content_revision_id` 指向的不可变 GameContentRevision，不从目录默认核心或任一 Variant current 反向猜测。详情 API 对每个启用核心查找直接引用该 ContentRevision 且可部署的 READY VariantRevision：没有时返回 `NEEDS_VALIDATION`，不能仅因该 core 曾对旧内容或相同 bytes 的另一 ContentRevision 通过就显示兼容。已有 READY 结果即锁定并继续使用它自己的 DAT/依赖快照；活动 DAT 后来变化不会把它降成不可运行，而是另以 `revalidationStatus=PENDING|FAILED` 提示后台重校验。新 DAT 校验成功并原子切换该 core 槽后才成为新的 current，但不会改变 Game current content。

详情 API 只返回只读兼容状态，不提供另一个“预热”写接口。用户选择“需验证”核心并点击开始时，`POST /launches` 使用同一个幂等 `EnsureVariant` 领域流程；这不增加第二个开始按钮，但允许需要生成大依赖 bundle 的验证通过可观察 Worker 完成，而不占住 HTTP/SQLite：

1. 只读取已持久化的 Game current ContentRevision/source manifest、Blob/hash、ArchiveEntry、活动 CoreArtifact/DAT 和 BIOS 状态；禁止调用 Hasheous、下载 payload、重新扫大文件或试跑另一个 core。
2. 已有直接引用该 ContentRevision 的 READY revision 时直接复用其锁定快照；同一 input 已有 BLOCKED/INCOMPATIBLE 结果时直接返回稳定 Blocker。
3. 否则仅从已入库证据计算 `validation_input_digest`，在一个短事务创建/复用 dedupe key=`gameVariantId + validationInputDigest` 的 `VARIANT_REVALIDATE` Job，返回 `202 VALIDATION_PENDING`，不创建 LaunchSession 或 credential。并发请求都得到同一 jobId。
4. Worker 复用 `CONTENT` GameContentFile；Arcade 只以该 ContentRevision 的 CONTENT/COMPANION 和目标 core 自己的活动 DAT 匹配 machine/entry/parent/BIOS（不得扫描无归属全局 Blob），并在事务外流式生成确定性 bundle；Host console 无依赖时只做索引判定。完成后短事务创建直接引用 ContentRevision 的 VariantRevision；READY 才切换 current，BLOCKED/INCOMPATIBLE 不成为 current。
5. Player overlay 订阅 Job；SUCCEEDED 时以相同 body 和新 Idempotency-Key 自动再调用 `POST /launches`，此次取得 `201`/cookie 并继续同一次点击流程。没有确认页或人工第二次开始。FAILED/CANCELLED 则退出全屏并显示 Job 的稳定 Blocker。证据缺失使用 `LAUNCH_CORE_VALIDATION_UNAVAILABLE`；有界 Job 超时使用 `LAUNCH_CORE_VALIDATION_TIMEOUT`，不写半成品、不静默回退。

按需创建的 revision 与导入预验证创建的 revision 使用同一领域服务、同一依赖快照和测试向量。管理侧替换游戏文件只有在新 GameContentRevision 对目录当前默认 core 验证 READY 时才原子提升 Game current content；其他 core 自动回到 `NEEDS_VALIDATION`。成功切换会保留旧 ContentRevision/VariantRevision 的文字和结构化审计，但删除其 ContentFile/VariantFile、运行快照及绑定存档；失败不改变 current 或存档。单 ROM bytes 相同，或多盘的规范盘序与全部 Disc hash 相同，必须以 `GAME_CONTENT_UNCHANGED` 拒绝而不创建 revision。

## 6. 导入、识别与审核

### 6.1 创建任务

用户上传文件或文件目录时必须选择游戏目录，不再选择基础平台。选择器按基础平台分组，并展示：

- 游戏目录名称。
- 推导出的基础平台。
- 默认核心。
- Arcade 默认核心当前活动 DAT 版本和状态。

创建 ImportJob 时保存以下不可变快照：

- <code>target_platform_instance_id</code>。
- <code>platform_id_snapshot</code>。
- <code>default_core_id_snapshot</code>。
- 核心 artifact / EmulatorJS 版本快照。
- Arcade 使用的 <code>dat_version_id_snapshot</code>。

这样可以避免任务执行期间游戏目录或活动 DAT 发生变化而静默改变识别结果。

### 6.2 处理策略

- 文件分组、扩展名检查和 hash profile 使用推导出的基础平台及导入专题中的固定 V1 规则。
- 默认核心是首个且必须执行的兼容性识别目标；Arcade 使用该核心自己的 DAT 解析 machine、parent 和 BIOS。
- 一期导入流水线不自动预检其他平台核心；它们初始显示为 `NEEDS_VALIDATION`，仅在用户从详情页首次显式选择该核心启动时通过同一个 `EnsureVariant` 流程按需检查，不阻塞默认核心的审核流程。
- Hasheous 仍只负责展示元信息候选，不使用 DAT 代替刮削。

### 6.3 审核

审核页展示并允许调整“目标游戏目录”，但不提供独立的“游戏默认核心”字段；默认核心始终由目录推导。

更改目标目录后的重算规则：

- 同平台、同默认核心：保留平台识别结果，只更新归属快照。
- 同平台、不同默认核心：重新执行核心及 DAT 兼容性检查。
- 不同平台：单个 ReviewDraft 不允许直接改归属，返回 `REIMPORT_REQUIRED_FOR_PLATFORM_CHANGE`；审核者 Discard 后以正确目录重新上传/导入，从而重做分组、hash profile 与识别。一期不在单 Item 内实现可能一拆多/多合一的跨平台重分组。

若任务快照与审核时游戏目录的当前配置不一致，必须提示“配置已变化”并完成重新验证后才能发布。

## 7. 生命周期与变更规则

### 创建

- 先选择基础平台，再选择该平台已启用的默认核心。
- 名称在站点内可以重复；创建请求不接收 slug，服务端在事务内生成唯一的 <code>(platform_id, slug)</code>，选择器同时显示基础平台消除歧义。
- 全新数据库不预置任何 PlatformInstance。管理员可点击“一键创建推荐目录”按 release catalog 一次性创建仍缺少的模板，也可继续手动创建自定义目录。

### Release 推荐目录 catalog

推荐模板的唯一机器事实源是 `internal/platformcatalog`，每项只定义稳定 `templateKey=<platform_id>/<default_core_id>`、平台、默认核心、名称、说明和 catalog 顺序。模板不定义 slug 或扩展名：slug 仍由创建服务生成，支持的 payload 扩展名只从 `internal/contentprofile` 按基础平台读取，数据库、模板与前端不得复制该集合。

每次读取推荐状态时，服务按全部未硬删除目录投影：

- `ACTIVE`：同 template key 的目录仍保持模板 platform/core/name/description；
- `CUSTOMIZED`：同 template key 已存在，但管理员修改过模板字段；
- `COVERED_BY_EQUIVALENT`：没有同 key 行，但存在未删除且启用的同 platform/core 手动目录；
- `SUPPRESSED`：同 key 行已停用或软删除，表示管理员明确不希望再次补齐；
- `MISSING`：以上均不成立，可以创建。

“一键创建推荐目录”只创建 `MISSING` 项。它在一个 `BEGIN IMMEDIATE` 短事务内重新读取状态，把新目录按 catalog 顺序追加到当前最大 `sort_order` 之后，并把目录、逐项 AuditEvent 和 domain idempotency response 一起提交；任一项失败即整体回滚。它不会覆盖自定义名称/核心、恢复停用或已删除目录、重排已有目录，也不会因同一基础平台已有别的核心目录而跳过。并发调用和相同 Idempotency-Key 重放只产生一组结果。

当前 catalog 含 27 项。FDS 不再是独立目录，`.fds` 由 NES/FCEUmm 模板所属平台规则接收；MAME 2003 不再是独立模板，Arcade 保留 FBNeo、MAME 2003 Plus 与 FBA CPS1/CPS2 四个推荐目录。启动时必须验证每个模板引用的 Platform、Core、启用 PlatformCore 和已登记 CoreArtifact；release catalog 与依赖不一致时快速失败，不能在补齐时静默跳过。

### 修改默认核心

1. 前端以 `{"coreId":"...","cursor":null,"limit":100}` 请求首页影响预览；服务端以当前目录 `version`、目标 core 和目录内全部 Game/current revision 集合计算一份 `impactDigest`，返回全量兼容/需验证/Blocker 计数、本页 items、`nextCursor` 和同一 digest。后续页使用相同 core/`If-Match` 并传回 cursor；任一影响输入变化就返回 `409 IMPACT_PREVIEW_STALE`，不拼接新旧快照。精确 schema、排序与签名见 HTTP 契约。
2. 提交修改必须携带 `If-Match`、同一个 `impactDigest` 和 `confirmBlocked`。目录版本或影响集合变化返回 `409 IMPACT_PREVIEW_STALE`，要求重新预览。
3. 有 Blocker 且 `confirmBlocked=false` 时拒绝；显式 `true` 才允许改变。变更后这些游戏显示“目录默认核心受阻”，绝不静默回退。
4. 变更只影响以后普通启动，不重写存档、GameVariantRevision 或历史 LaunchSession；操作与影响摘要写 AuditEvent。

### 移动游戏

- 同一基础平台内允许移动到另一游戏目录。
- 若目标目录默认核心不同，移动 preview 必须针对 Game current ContentRevision 和目标目录当前 CoreArtifact/DAT/BIOS 输入查询兼容结果。缺少结果时创建/复用共享 `VARIANT_REVALIDATE` Job 并返回 `202`；客户端等待任务终态后用新 Idempotency-Key 重新 preview，不能在后台校验尚未完成时先移动。
- 完成的 preview 返回目标目录/default core、READY 或 blocker 诊断、Game/目录版本和 `impactDigest`。提交重新计算全部输入；漂移返回 `IMPACT_PREVIEW_STALE`。有 blocker 时必须显式 `confirmBlocked=true`，移动后普通启动明确阻断，不能回退到旧目录默认 core。
- 移动只改变 Game 的 `platform_instance_id/version` 并写 AuditEvent；GameVariant、GameVariantRevision、GameContentRevision、存档和文件 revision 不被删除或改写。目标默认 core 已有与当前内容匹配的 READY revision 时复用该结果。
- 不允许用简单移动跨基础平台。跨平台需要重新走识别与审核流程，避免错误复用 hash profile 和平台规则。

### 停用与删除

- `enabled` 是用户侧可见性开关，不是目录是否为空的证明。非空目录也可停用；停用后 Game、存档、历史与管理记录继续保留，管理端仍可查看和重新启用，但用户侧首页、游戏库、游戏详情、存档列表及新启动统一排除该目录游戏。
- “删除”仍要求目录没有任何 Game；删除始终软删除并写审计，不提供硬删除 API。slug 不因删除自动释放，避免旧链接指向新语义。

### 显示排序

- 管理页面不暴露 `sort_order` 数字输入，使用拖拽手柄并为键盘提供上下方向键等价操作。
- 排序提交必须带全部未删除目录的 `id/version`，服务端在同一短事务验证集合恰好相等、版本均未漂移后按数组顺序写入间隔为 100 的 `sort_order` 并逐项递增版本/写审计。目录集合或任一版本变化时整体拒绝，不能部分成功。

## 8. 页面影响

### 用户侧

- 左侧导航不增加游戏目录入口，仍只有“游戏库”。
- 游戏库先按基础平台、再按游戏目录筛选；游戏卡片展示目录名称。
- 停用目录后，其游戏不进入首页统计/最近记录、游戏库、详情、存档列表或新 Launch；重新启用后使用原有 Game/存档/revision 恢复展示，不复制或改写业务记录。
- 游戏详情面包屑/元信息显示“基础平台 / 游戏目录”；没有浏览器偏好时运行方式对话框默认标记“目录默认”，存在偏好时明确显示红色“未采用默认核心”。
- 存档的“继续”不进入详情，直接使用存档锁定的 CoreArtifact 和 GameVariantRevision 启动；只有“游戏详情/兼容性”等次要入口进入详情。

### 管理后台

管理后台新增“游戏目录”页面，用于维护 PlatformInstance，包含：

- 目录名称、基础平台、默认核心、游戏数量、联机核心能力和兼容性摘要。联机能力按平台、默认核心及当前启用 CoreArtifact 精确匹配联机 manifest，不按核心名称硬编码，也不代表目录内每款游戏已经通过联机资格检查。
- 名称与用户说明通过各自铅笔在行内编辑；说明编辑器保持单行紧凑高度；默认核心以“推荐运行方式”下拉框呈现，启用状态以 checkbox 呈现。
- 拖拽/键盘排序和启停不显示会推动表格的成功提示条，失败才显示错误；非空目录可启停，但红色删除 X 禁用并显示原因，空目录才可删除。
- 下拉切换默认核心后自动进入影响预览，确认前不提交。
- 查看目录下游戏，并进入单个游戏的影响预览与移动流程。一期没有批量移动 API，不在 UI 暗示尚未定义的批量事务语义。

游戏导入、任务进度、审核历史和游戏管理均增加游戏目录列或筛选项。BIOS/DAT 页面仍按核心展示，可附加“被多少个游戏目录设为默认核心”作为影响提示，但不能按目录复制 BIOS/DAT 数据。

## 9. 示例

| 基础平台 | 游戏目录 | 默认核心 | 说明 |
| --- | --- | --- | --- |
| Arcade | FBNeo 游戏列表 | fbneo | 以 FBNeo ROMset 为主要来源 |
| Arcade | MAME 游戏列表 | mame2003 | 以 MAME 2003 ROMset 为主要来源 |
| Arcade | FBNeo 飞行游戏 | fbneo | 同一运行策略下的主题目录 |
| MS-DOS | DOS 经典游戏 | dosbox_pure | 详情页仍需选择启动程序 |

“FBNeo 游戏列表”和“FBNeo 飞行游戏”可以使用相同默认核心，但它们是两个独立的唯一归属目录。若未来需要让同一游戏同时出现在“飞行”“双人”“通关中”等视图，应另建 Tag/Collection，而不是让 Game 同时属于多个 PlatformInstance。

## 10. 当前 schema 边界

当前项目尚无已发布数据库，clean schema 直接以非空 <code>games.platform_instance_id</code> 建模，不存在 <code>games.platform_id</code>、过渡列、迁移目录或推断归属的回填逻辑。fresh 数据库不 seed PlatformInstance；管理员通过“应用推荐目录”创建当前 catalog，Game 只能由当前导入、审核或管理路径进入明确目录。
