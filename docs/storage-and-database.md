# 存储与数据库设计

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期实施基线 |
| 版本 | 1.3 |
| 日期 | 2026-08-24 |
| 适用范围 | Retrom 一期 |

## 1. 文档边界

本文档定义 PostgreSQL 类型约定、时间字段、核心表目录、独立文件存储、Archive 安全以及后台删除。

关联文档：

- [游戏目录领域设计](./platform-instance.md)
- [导入、刮削与审核](./import-and-review.md)
- [EmulatorJS 运行时、快速启动与游玩数据](./runtime-and-play-data.md)
- [BIOS 与 Arcade DAT](./bios-and-arcade.md)
- [一期数据库实体与不变量](./data-model.md)
- [HTTP API、上传与启动凭据契约](./http-api-contract.md)
- [第三方运行时与 DAT 依赖管理](./dependency-management.md)

## 2. 时间字段统一规则

### 2.1 时间点

所有表示“某一时刻”的数据库字段统一使用 PostgreSQL `BIGINT`，保存 UTC Unix epoch milliseconds：

- 单位固定为毫秒，不允许同库混用秒、微秒或纳秒。
- 字段名统一使用 `_at_ms` 后缀，例如 `created_at_ms`、`updated_at_ms`、`started_at_ms`、`last_reported_at_ms`、`expires_at_ms`。
- Go 类型使用 `int64`，写入值来自 `time.Now().UTC().UnixMilli()`。
- 浏览器值可直接与 `Date.now()` 对接；展示时才按用户时区格式化。会被 SSR 的 Client Component 固定以 UTC 生成 server/hydration snapshot，hydration 完成后再切换到浏览器解析出的 IANA 时区；不得让容器或 Node 的本地时区参与首轮客户端渲染，也不得通过统一部署时区代替逐用户展示。
- JSON 审核快照、任务事件和 LaunchSession 配置中的时间点也使用带 `Ms` 后缀的整数，避免同一概念在不同层采用不同单位。

禁止：

- 用 `TEXT` 保存 RFC 3339 时间作为业务表的主时间字段。
- 使用 `CURRENT_TIMESTAMP` 作为毫秒整数主存储；其类型和单位不符合本约定。
- 保存服务器本地时区时间。
- 使用无单位语义的字段名，例如 `timestamp`、`time` 或 `created`。

示例：

~~~sql
CREATE TABLE play_sessions (
    id TEXT PRIMARY KEY,
    profile_id TEXT NOT NULL,
    game_id TEXT NOT NULL,
    started_at_ms BIGINT NOT NULL CHECK (started_at_ms >= 0),
    last_reported_at_ms BIGINT NOT NULL CHECK (last_reported_at_ms >= started_at_ms),
    ended_at_ms BIGINT,
    active_duration_ms BIGINT NOT NULL DEFAULT 0 CHECK (active_duration_ms >= 0),
    CHECK (ended_at_ms IS NULL OR ended_at_ms >= started_at_ms)
);

CREATE INDEX idx_play_sessions_started_at_ms
    ON play_sessions(started_at_ms DESC);
~~~

### 2.2 时长、年份与日期

并非所有“与时间有关”的值都是时间戳：

- 时长使用 `BIGINT` 毫秒并以 `_duration_ms` 或 `_interval_ms` 结尾，例如 `active_duration_ms`、`heartbeat_interval_ms`。
- EmulatorJS 固定保存间隔等原生以毫秒定义的配置继续使用毫秒。
- 游戏发行年份使用 `BIGINT` 年份，例如 `release_year = 1996`，不能伪造为某年 1 月 1 日时间戳。
- 只有年/月/日、没有精确时刻的历史发行日期应拆为 `release_date_precision` 与整数年/月/日字段；一期只需要 `release_year`。
- DAT 中的原始日期文本若需要审计，可保存在 raw payload，不作为排序和状态机时间字段。

### 2.3 API 映射

数据库时间字段映射到 JSON 时沿用毫秒单位并使用 camelCase：

~~~json
{
  "createdAtMs": 1785999600123,
  "activeDurationMs": 8642000
}
~~~

前端不得通过字段值位数猜测单位。OpenAPI schema 应声明 `type: integer`、`format: int64` 并在 description 中写明 `Unix epoch milliseconds (UTC)`。

### 2.4 建库与数据切换

PostgreSQL 首版直接创建 `BIGINT` 毫秒列。本次切换不保留业务数据，不提供旧库转换器、双写或双后端模式；数据库与领域文件必须作为同一环境整体重建。未来已发布的 PostgreSQL schema 只通过追加 migration 演进，不改写已应用 checksum。

## 3. PostgreSQL 基线

唯一数据库为 PostgreSQL 18，Go 使用 `github.com/jackc/pgx/v5` 的 `database/sql` 适配器，版本由 `go.mod/go.sum` 固定。PFB、CI 与部署示例使用同一固定 PostgreSQL 镜像。服务必须提供 `RETROM_DATABASE_URL`（本地 `make dev` 可自动启动 PostgreSQL 并注入；开发生命周期见[运维专题](./backend-api-and-operations.md#73-make-dev-只运行本地进程)），连接 URL 必须包含服务器和数据库；凭据不能写入日志、诊断或版本库。应用文件仍位于 `RETROM_DATA_DIR`，数据库由独立 PostgreSQL 服务持久化。

- 每个物理连接设置 UTC、`application_name=retrom`、`lock_timeout=5s`。生产保持 PostgreSQL 默认持久化保证，不关闭 fsync、full_page_writes 或 synchronous_commit。
- `database.DB.BeginTx` 默认使用 `SERIALIZABLE` 写事务；显式 `ReadOnly` 使用 `REPEATABLE READ` 快照。数据库允许不同事务并发，不再依赖进程级单写者。涉及授权、版本、租约和输入快照的最终检查与写入必须同事务；需要阻止同一记录变化时使用条件更新或行锁。
- PostgreSQL 可能返回序列化失败 `40001` 或死锁 `40P01`。只有经过审查的纯数据库事务边界使用 `database.RetryTransaction`，最多八次、指数退避且受 context 约束。回调每次重读事实、覆盖返回结果，不累积外部状态；文件复制/发布、哈希、网络、Provider 解析和任务派发必须在可重试范围外。事务错误仍保留原始原因。
- 并发审核预览请求命中同一用户与幂等键的唯一约束时，预览持久化边界允许在回滚后重新开启一次事务，沿既有幂等检查返回已提交的会话；其他唯一约束错误不得重试。
- 依赖目录写入使用单独的数据库级 advisory transaction lock 与 `READ COMMITTED`，等待上限 60 秒且服从 context。目录发布者依次提交，等待后重读最新状态；普通游戏、账户和任务写入不获取该锁。大型 DAT 物化不参与全库 SSI 谓词冲突，版本、索引、活动选择和发布收据仍原子提交；测试 DAT 发布也必须取得同一目录锁。
- 写连接池上限 16、独立只读池上限 8，各保留至多 4 个空闲连接。组合层显式注入 reader/writer，受保护列表与账户鉴权使用 reader，写事务的事实重验与收据不能跨池拆开。
- 有效会话续期最多等待写事务 100ms；超时后重读当前已提交的权限和到期事实，不伪造续期。
- 连接池与事务统计分别记录等待、BEGIN、SQL、行消费、COMMIT 和持有时间。慢调用 500ms、慢写事务 100ms 输出无 SQL/参数/凭据的结构化日志。驱动取消必须释放连接；自动回滚与提交竞争时，错误仍须保留 context 取消原因。故障注入连接必须透传驱动的连接有效性与重置语义。
- 标志列使用 `BIGINT CHECK(value IN (0,1))`，适配器绑定 Go bool 为 0/1；SQL `EXISTS` 等布尔表达式直接扫描到 Go bool。二进制摘要和响应体使用 `BYTEA`。枚举使用 `TEXT` 加 `CHECK` 或字典表。
- JSON 快照保持经过 `IS JSON` 校验的 `TEXT`，保留生成时的字节与摘要；查询使用原生 JSON/JSONB 操作，嵌套快照提取使用 `json` 保留文本表示。可查询关系仍规范化，不以 JSON 替代表和外键。
- 参数由适配器将组合完成的匿名 `?` 编号为 PostgreSQL `$n`；表达式、目录查询、upsert、聚合均为 PostgreSQL SQL。字节序比较显式使用 `COLLATE "C"`；RPG 路径回退使用 ASCII `translate`，不扩大为 Unicode 模糊匹配。
- 游标比较与 `ORDER BY` 必须使用相同的 collation。可空排序列显式声明空值位置；游戏库最近游玩使用 `DESC NULLS LAST`，游标把未玩时间映射为 `-1`，保证已玩和未玩游戏之间不漏项、不重复。
- UUIDv7 业务主键保持规范小写文本；稳定字典使用 code。EmulatorJS 的 `emulator_game_id` 为 `BIGINT`，范围为 `1..9007199254740991`。
- 不创建业务 VIEW/TRIGGER。外键和 CHECK 在数据库中执行；跨表所有权、状态转换与上限通过 `recordstore` 参数化 SQL 及保存点校验。会话和回收排期由 `sessionstore` 在同一事务更新。

### 3.1 migration lineage

`001_schema.sql` 直接创建当前最终表、约束和索引；循环外键在所有表建立后以 `ALTER TABLE` 声明，保持全程事务性。建库只包含实例初始化状态，不包含业务游戏、账户或目录。Provider/Platform/Core 由正常启动同步。

`store.Open` 使用 `READ COMMITTED` 迁移事务与 advisory transaction lock 串行化 schema 初始化，确保锁等待结束后能看到前一初始化事务的提交；检查 `schema_migrations` 的版本、名称和 checksum；只接受空库、当前精确有序前缀或完整当前 lineage。未知/未来版本、缺失历史、名称或 checksum 不匹配会拒绝启动。迁移和历史记录同事务提交；失败回滚。取消或期限错误保留原始 context 原因。

PFB 不兼容切换必须先停止应用与 PostgreSQL，再执行 exact-ID `pfb-data-reset`；`data/` 与 `postgres/` 一起归档到 `reset-backups/`，保留 Provider、依赖和构建缓存及稳定 URL。新 PFB 直接初始化空库。不得把旧数据库与另一份领域文件混用。

### 3.2 测试、备份与恢复

Go 测试通过 `RETROM_TEST_DATABASE_URL` 连接专用测试服务器，角色需要建库权限；每个测试创建独立 `retrom_test_*` 数据库并注册删除，不复用产品数据库。浏览器夹具创建 `retrom_acceptance_*` 库，与临时文件根绑定，结束后同时清理。`make prepare-postgres-tools` 准备锁定的 psycopg 工具；普通应用不依赖 Python 数据库驱动。

备份先停止 Retrom 写入（含后台任务），使用与服务器主版本匹配的 `pg_dump --format=custom` 导出，并复制同一时刻的整个 `RETROM_DATA_DIR` 和 Provider 活动配置。恢复到空 PostgreSQL 数据库与配套文件根，再用实际 `store.Open` 路径校验迁移历史、约束/索引目录和领域文件所有权；单独的存活探测不等于业务完整性证明。正常备份不直接复制正在运行的 PGDATA。

## 4. 表目录

### 4.1 平台、核心与固件

| 表 | 用途 |
| --- | --- |
| `profiles` | 每个账号独立且不可变的 Profile；无 `local` seed |
| `users` / `user_credentials` | 账号身份、角色/状态/version 与 Argon2id 凭据 |
| `auth_sessions` / `account_links` / `instance_state` / `auth_rate_limits` | 登录 session、一次性邀请/重置、初始化状态和 HMAC 限流桶 |
| `platforms` | 基础平台 |
| `cores` | EmulatorJS/Core 配置 |
| `runtime_providers`、`runtime_targets`、`runtime_target_bindings` | 已激活 Provider Bundle、公开 Target declaration 与 Product Core binding |
| `content_kinds` | Host 持久内容类型 reference catalog；业务表通过外键引用，不复制固定枚举列表 |
| `platform_cores` | 平台与核心多对多关联 |
| `platform_instances` | 用户维护的游戏目录及默认核心 |
| `bios_requirements` | 固件要求 |
| `bios_installations` | 已上传 BIOS |

PlatformInstance 的复合外键、游戏唯一归属和迁移规则见 [游戏目录领域设计](./platform-instance.md)。

### 4.2 文件、游戏与媒体

| 表 | 用途 |
| --- | --- |
| `archive_entries` | 经安全扫描的 archive entry 路径、大小及内容 hash |
| `games` | 用户可见游戏及当前 metadata/content 来源；可选 `content_profile_json` 保存内容类型专属一对一扩展；`platform_instance_id` 必填，不保存 `platform_id` |
| `game_assets` | 封面、背景、截图等 |
| `game_files` | Game 当前 CONTENT/DOS_SOURCE/COMPANION/PROJECT_FILE 等 Blob 与逻辑路径 |
| `game_variants` | Game + Core 唯一的稳定当前验证/运行状态，含 Provider/Target、DAT、依赖快照和可选 `runtime_profile_json` |
| `dos_entries` | GameFiles中经过安全扫描的可执行程序候选 |
| `variant_files` | parent、BIOS bundle、DOS launch bundle 等 core-specific/派生文件 |
| `tags` / `game_tags` | 实例级 Tag tombstone 与 Game 多对多关系；不含 Blob 或宿主路径 |

`variant_files.role` 一期固定支持：

- `PARENT`
- `BIOS_BUNDLE`
- `DOS_LAUNCH_BUNDLE`

用户内容的 `CONTENT/DOS_SOURCE/COMPANION` role 只属于 `game_files`；不得再复制到 GameVariant 形成第二条“当前文件”事实源。

### 4.3 DAT

| 表 | 用途 |
| --- | --- |
| `dat_versions` | Provider Target 专属 release-managed DAT、非空内置相对路径、SHA-256、解析器版本、解析及活动状态 |
| `dat_machines` | machine |
| `dat_bios_sets` | MAME machine 的 BIOS option/default |
| `dat_rom_entries` | ROM entry |
| `dat_disk_entries` | CHD/disk |
| `variant_dependencies` | GameVariant 的 parent/BIOS 依赖快照 |

### 4.4 导入、审核与元信息

所有接收文件写入 `import_files`，与上传完成事务一同提交，由 Upload 持有独立文件。格式仅保存在统一的 `source_imports` 扫描计划；Collection、Item、metadata evidence、file、asset 使用同一 `source_import_*` 表。审核不保存历史；抓取按 subject 只保存当前 run、候选及证据，替换时原子取消旧任务并级联清理。详细约束见数据模型第 5 节。

| 表 | 用途 |
| --- | --- |
| `import_files` | 所有来源的已接收文件、规范路径、大小与当前独立文件记录 |
| `source_imports` / `source_import_collections` / `source_import_items` | 格式适配共享的扫描计划、显式映射与来源条目 |
| `source_import_metadata_files` / `source_import_item_files` / `source_import_item_assets` | 有界扫描证据、待接收来源及独立媒体 |
| `import_jobs` | 一次导入任务及目标游戏目录快照 |
| `import_job_files` | UploadSession 每个文件的 SOURCE/IGNORED/REJECTED 分类与原因 |
| `import_items` | 单个游戏候选，含可选的内容类型专属 `review_profile_json` |
| `import_item_source_files` | 候选的 CONTENT/DOS_SOURCE/COMPANION 发布前文件映射 |
| `import_item_dos_entries` | 发布前 DOS 程序候选 |
| `import_items.content_analysis_json` / `import_item_runtime_files` | 当前来源的内容观察与 Item 拥有的派生产物；安装 BIOS 和 source COMPANION 不复制到派生产物表 |
| `upload_sessions` / `upload_files` | 浏览器上传会话与相对路径 |
| `upload_parts` | 分块上传 |
| `upload_consumptions` | 已完成上传到 Import、游戏文件替换 Job、BIOS/Game Asset/Review Asset 的互斥审计归属 |
| `metadata_scrape_runs` | ImportItem 或 Game 唯一的当前 hash/provider 证据批次 |
| `content_hash_evidence` | run 内的版本化 hash profile、来源 Blob/archive entry 与查询顺序 |
| `metadata_scrape_query_attempts` | run/evidence 到每次网络或缓存 response 的不可变关联 |
| `scrape_candidates` / `scrape_candidate_hits` | Hasheous 元信息候选及多 hash/entry 命中关系 |
| `scrape_candidate_assets` | 候选媒体的受控获取状态、Blob、尺寸、任务绑定、冻结顺序与资源收费 |
| `metadata_media_runs` | 每个刮削 Run 的媒体顺序冻结、累计收费和版本 |
| `review_arcade_parent_attachments` / `review_multidisc_attachments` | 补传业务决定与冻结来源/结果引用；`PENDING` 保留预约，执行状态由关联 Job 投影 |
| `review_uploaded_assets` | 审核期间人工上传的不可变封面资源及 Blob 归属 |
| `metadata_provider_cache` | provider + request digest 的可变缓存指针与过期时间 |
| `metadata_provider_responses` | 每次查询的不可变状态、原始响应 Blob 与有效期 |
| `import_items` | 导入 Item 与当前审核字段共用一行；`review_version` 独立于 Item `version`，用于审核乐观并发，不保存编辑历史 |
| `review_draft_screenshot_assets` | 草稿截图选择的规范顺序与外键 |
| `runtime_preview_sessions` / `runtime_preview_files` | 中立的短时运行快照、授权和冻结资源；不引用审核/导入表 |
| `review_preview_bindings` | 审核到运行 Preview 的工作关联，只由审核创建、恢复和清理使用 |
| `review_runtime_screenshots` | 按 import_item_id 唯一的当前审核截图与人工放行证据 |
| `review_draft_tags` | 待审核草稿的活动标签选择；最终决定复制到 Game 后删除 |
| `source_collection_tags` | 统一 Collection 的管理员标签映射；名称证据另冻结在 Collection snapshot |

### 4.5 通用任务、幂等与审计

| 表 | 用途 |
| --- | --- |
| `jobs` / `job_input_snapshots` / `job_events` | 带 scope、不可变 execution 输入、lease、attempt、SSE resume ID 的持久 work-unit 与事件 |
| `idempotency_records` | 按 USER/SYSTEM principal 隔离的写操作 24 小时请求/响应重放 |
| `audit_events` | 管理操作 append-only 审计 |
| `schema_migrations` | migration version、name、checksum 与整数应用时刻 |

补传 Job 的 InputSnapshot 使用统一的 `schemaVersion/kind/scope/executionId/inputs` 信封，payload 只引用当前 execution。`jobs_recovery` 索引支持按 kind/state/lease/deadline 的有界恢复；每批至多 64 条，取消业务收口与 Job/Event 更新同事务。当前破坏性 schema 收口直接修改建库基线，不提供旧 Attachment 状态或裸输入的兼容迁移。

### 4.6 启动与游玩数据

| 表 | 用途 |
| --- | --- |
| `save_states` | 带截图的手动状态存档 |
| `play_sessions` | 有效游玩会话和累计 progress 统计 |
| `launch_sessions` | 短期不可变启动配置、非秘密 launchId 与 capability hash |

所有表中的时间点和时长必须遵守第 2 节，不能由各模块自行选择类型或单位。表的必需字段、枚举、唯一索引、append-only evidence 和应用写入校验 以 [一期数据库实体与不变量](./data-model.md) 为唯一数据字典；本节只做模块目录，不能据此省略该文档的约束。

## 5. 本地独立文件存储

### 5.1 目录

~~~text
data/
  dat/                     # manifest 入 Git，真实 DAT 由 prepare-deps 物化并忽略
    emulatorjs/
      4.2.3/
        manifest.json
        SHA256SUMS
        fbneo/fbneo-arcade.dat
        mame2003/mame2003.xml
        mame2003_plus/mame2003-plus.xml
        fbalpha2012_cps1/fbalpha2012-cps1.dat
        fbalpha2012_cps2/fbalpha2012-cps2.dat
  runtime/                 # EmulatorJS 依赖缓存，不进入版本控制，不保存业务数据
    emulatorjs/4.2.3/
      data/
      licenses/             # manifest 锁定许可原文；不写入 Git
      THIRD_PARTY_NOTICES   # 确定性生成；不写入 Git

.dev-data/data/              # 开发 RETROM_DATA_DIR，不进入版本控制
  retrom.lock
  files/<game-id-last2>/<game-uuid>/{content,media}/
  secrets/launch-capability.key
  tmp/uploads/<upload-id>/
  staging/items/<item-uuid>/{payload,scratch}/
  previews/<preview-uuid>/checkpoints/<checkpoint-uuid>/
  previews/<preview-uuid>/restore/
  staging/uploads/<upload-uuid>/
  staging/sources/<source-item-uuid>/
  staging/writes/
  saves/<save-uuid>/<write-uuid>/
  bios/<installation-uuid>/
  scrapes/<run-uuid>/
  responses/<response-uuid>/
.dev-data/dev-state/         # make dev PID 登记与接管锁，不进入版本控制
.dev-data/dev.mk             # make dev 本地启动配置，不进入版本控制
~~~

项目 ignore 规则必须忽略 `.cache/`、`data/runtime/**` 和五个 DAT payload 目录，只允许 manifest、`SHA256SUMS`、文档和脚本进入 Git；许可原文与生成 notice 也属于 runtime payload，不能因体积小而提交成第二份事实源。生产 `RETROM_DATA_DIR` 使用独立持久卷；不得把只读依赖目录挂成业务数据根。`make prepare-deps` 在服务启动前物化并校验 payload；应用同步预检只校验、不下载，随后 Worker 可从已校验只读 DAT 建立数据库索引。Arcade DAT 不接受上传且不进入独立文件存储。完整契约见 [第三方依赖管理](./dependency-management.md)。

### 5.2 领域目录与独立文件

每个游戏独占 `files/<游戏 UUID 最后两位>/<游戏 UUID>/`。`content/<写入 UUID>/` 保存 ROM、目录项目与校验产物，`media/<资源 UUID>/` 保存封面、视频和截图。相同内容的两个游戏仍有独立目录、文件与 inode；摘要只用于完整性、DAT、内容识别和重复游戏提示。

导入条目独占 `staging/items/<Item UUID>/`，其中 `payload/` 是准备发布的目录，`scratch/` 保存审核截图等临时材料；预览 checkpoint 存放于 `previews/<Preview UUID>/checkpoints/`，由 Preview 独占并通过 PATH_DELETE 回收。浏览器上传与服务器来源分别使用 `staging/uploads/<Upload UUID>/`、`staging/sources/<SourceItem UUID>/`。来源只是输入；准备完成后 Item 的 ROM 和媒体都能独立于来源存活。

未登记为导入条目、来源项或上传会话的准备目录超过 24 小时后按目录清理；数据库查询失败时保留。长期待审条目不按目录年龄过期。

文件先在同一数据根的 `staging/writes/` 完整写入并计算 SHA-256、MD5、SHA-1、CRC32、size，fsync 后才进入领域目录。业务记录直接保存受约束的文件记录值，包含相对路径、摘要、大小和 MIME，不再建立全局文件登记、公共引用计数或所有权交接表。

审批先在 Item 上持久化发布决定、固定 Game UUID 和选中的校验/媒体，再把 `payload/` 原子 rename 到游戏目录，最后在短事务中写入 Game/Variant 并完成 Item。中断后按原决定继续，不分配第二个游戏；批量审批复用单条流程。数据库发布失败时保留目录和决定供恢复。

替换 ROM 或媒体写入全新子目录，数据库提交后清理旧目录；目录标识永不复用，延迟任务不能删除新内容。存档写入 `saves/<Save UUID>/<写入 UUID>/`，BIOS 安装写入 `bios/<Installation UUID>/`，抓取候选和响应分别属于 `scrapes/<Run UUID>/`、`responses/<Response UUID>/`。这些领域各自决定保留和清理，不向 Game 交接全局文件所有权。

### 5.3 内容服务

固定公开资源与启动受限内容使用不同缓存策略：

- `GET`
- `HEAD`
- Range 请求
- `ETag: "<sha256>"`
- 固定版本 EmulatorJS 与发布媒体：`Cache-Control: public, max-age=31536000, immutable`；媒体替换创建新
  GameAsset ID 与 URL，current 切换后旧 URL 失效。
- ROM、parent、BIOS、多盘外部文件和目录型 runtime 项目：仅经 `/runtime/content/` 的 Launch content grant 访问，URL 携带由
  实际 bytes 与必要输出选项带领域分隔派生的内容身份而不暴露 内部文件记录或摘要；`Cache-Control: private,
  max-age=31536000, immutable`。ROM 或 bundle 任一文件替换必须改变 URL，授权校验仍逐请求重算并匹配身份。
- 状态存档与截图继续是 Profile 私有数据，使用 Launch/SaveState 逻辑 ID、`Cache-Control: private,
  no-store` 与限定路径 cookie；不得因 ROM/BIOS 改为内容寻址而把存档设为 immutable 或跨用户复用。

公开媒体以不可变 GameAsset ID 形成新 URL；固定运行时包含明确版本。受限运行内容由 LaunchSession grant
授权并以不可变内容身份形成 URL；替换只前移 identity，不原地改变同 URL bytes。所有端点设置强 ETag、
正确 MIME 与 `nosniff`；精确授权、身份和 Range 行为见 [HTTP API 契约](./http-api-contract.md)。
`private immutable` 只在同一浏览器缓存内复用；网络请求始终重新校验 grant、ACTIVE 状态与精确内容身份。
硬删除/撤销后新的或强制网络请求立即失败；浏览器已经合法取得的私有缓存副本与已经下载到内存的 ROM 一样
无法被服务器追溯擦除，因此 SaveState state/screenshot 等用户私有数据始终使用 `no-store`，不采用该策略。

## 6. Archive 安全

- ZIP 在服务进程内使用受限 reader；7z 必须由同一后端二进制的隐藏 worker 子进程读取，父进程只传只读 fd，不传用户路径，也不调用宿主 `7z/7zz`。Linux worker fail-closed 设置 Go 512 MiB memory limit、2 GiB `RLIMIT_AS`、8 GiB `RLIMIT_FSIZE`、64 个 fd、0 core dump、120 秒 CPU，上层 wall timeout 125 秒且 IPC JSON 最多 64 MiB；OS 无法建立限制时返回 `ARCHIVE_SANDBOX_UNAVAILABLE`。worker crash/signal/timeout/resource/超长 IPC 统一为 `ARCHIVE_RESOURCE_LIMIT`。
- 7z 只接受首字节 magic `37 7a bc af 27 1c` 的未加密、单卷、非 SFX archive，并用 `NewReader(readerAt,size)` 禁止邻接分卷发现。最多 20,000 个 regular-file entry、单 entry 8 GiB、总展开 32 GiB、展开/原包比 200；扫描按自然 ordinal 顺序完整读取、校验 CRC/声明大小并计算四种 hash。父进程先 SCAN 再按唯一候选 ordinal MATERIALIZE，输出以 `expectedSize+1` 限流进入独立文件存储；任何半成品都不能成为有效文件记录。
- 上传 manifest 与 ZIP entry 共用 `SAFE_LOGICAL_PATH_V1`：输入必须是有效 UTF-8，使用 `/` 分隔，整体 1–1,024 UTF-8 bytes、每段 1–255 bytes；拒绝开头/结尾 `/`、空段、`.`/`..` 段、反斜杠、NUL、U+0001..U+001F、U+007F、Windows drive 前缀和 UNC/绝对路径。字符串不做 percent decode、Unicode NFC/NFD 或平台文件系统 canonicalization；存储的 `normalized_path` 只是把已验证段以单个 `/` 连接，因此相同原始 bytes 必须得到相同结果。UI 展示时仍按纯文本转义。
- ZIP central directory 的每个 name 都先执行该算法。显式目录 entry 必须且只能以单个 `/` 结尾：分类为 directory 后先去掉这个终止符，再对剩余非空 path 执行 `SAFE_LOGICAL_PATH_V1`，通过后忽略该 entry；不能把“目录例外”用于接受 `//`、根目录、`.`/`..` 或反斜杠。任何 symlink、hardlink/device/FIFO/socket、加密 entry 或路径不安全都会阻断整个 archive。无 Unix mode 的非目录 entry 可按 regular file 处理；存在 mode 时只接受 regular file/directory。只支持 ZIP method 0（Store）和 8（Deflate）；ZIP64 只有在同一大小门禁内才允许，不注册额外 decompressor。
- `archive_entries.original_relative_path` 保留安全原名，`normalized_path` 保留其大小写，另保存 `ascii_casefold_path`（只把 ASCII `A..Z` 映射为 `a..z`）。同一 archive 对 normalized path 和 ASCII-casefold path 都唯一；因此 `ROM.BIN/rom.bin` 稳定阻断而不会在 Arcade/DOS 虚拟文件系统中互相覆盖。DAT entry lookup、BIOS 重验证和依赖预览查询该已索引 key，不重读 archive。
- 安全扫描在同一次有界解压流中计算每个 regular entry 的 size/CRC32/MD5/SHA-1/SHA-256，并在完整 archive 通过所有门禁后才原子提交 ArchiveEntry 集；不因 central-directory 声明值相同而跳过实际 bytes。只有主机唯一 ROM member、DOS_SOURCE 或其他明确领域引用需要独立 bytes 时才物化到独立文件存储；Arcade ROMset 验证可使用已保存 hash，不默认复制全部内层 entry。需要 member bytes 时按当前 owner 物化独立文件；ArchiveEntry 不保存跨 owner 的物化文件指针。
ArchiveEntry 只保存所属归档的不可变扫描事实。归档文件退休后，删除任务在短事务中移除它的派生索引；已物化的游戏文件具有独立 ID 和 owner。来源归档 ID 与 ordinal 仅作历史溯源，不保护原归档，也不反向决定内层文件寿命。
- 限制 entry 数、单 entry 展开大小、总展开大小和压缩比；压缩大小为 0 而展开大小非 0 直接视为超限。路径/entry 门禁先于物化。默认情况下，任一 regular-file entry 的规范扩展名或文件魔数只要表明它仍是 ZIP/7z/RAR/TAR/gzip 等归档，就以 `NESTED_ARCHIVE_UNSUPPORTED` 阻断整个外层 archive；一期不递归展开，也不能靠改扩展名绕过魔数检查。只有 DOS 与 RPG Maker 可请求扫描器“标记但不展开”：二者都将项目根内层 archive 自身的原始 bytes 物化到不可变源快照，绝不打开内层目录、使用内层 marker 或执行内层内容。某次 Launch 是否锁定、打包或提供该不透明文件，继续由所选运行适配器的固定运行投影决定；源快照保留不等于 Native Web 子域可以读取任意后缀。普通 ROM、Arcade、BIOS 与其他消费者继续使用默认拒绝策略。
- XML DAT 解析只允许 BIOS/DAT 专题定义的一个有界 DOCTYPE 声明并在 token stream 前安全移除；绝不解释 DTD/实体，也不允许外部实体或网络访问。不能把这条简写实现成“拒绝所有真实 DAT 的 DOCTYPE”。
- 不信任扩展名、ZIP 声明 MIME 或 archive 内路径。
- 读取 Arcade ZIP central directory 时不默认展开全部内容到磁盘。
- Arcade DAT 遇到运行必需 CHD 仍直接产生 `UNSUPPORTED_CHD` 审核 Blocker；PSX、Saturn、3DO、PC-FX 的 STANDARD profile 接受单个 raw CHD，Saturn 另可在 capability 明确允许时使用 `MULTI_DISC`，这些规则不能与 Arcade CHD 混用。PSP 的 raw ISO/CSO 不作为 archive 扫描。

## 7. 目录清理与失败恢复

清理调度由所属领域在业务事务中决定：Game 校验删除版本，Import 校验条目终态和批次子项，SourceImport 校验交接与可重试状态，Upload 校验消费释放与上传状态。领域处理器按自己的顺序清理关系，并在同一事务中保存有界进度和释放状态；`cleanupjobs` 只负责冻结输入校验、执行权限、租约和重试。公共执行器不持有业务终态分支、字符串引用组或可解释的通用清理计划。

Game 删除立即撤销可见性与运行授权，在同一业务事务中安排领域清理。领域关系处理完成后，以游戏目录为单位执行 `PATH_DELETE`；Item 发布或丢弃后清理自己的 staging 目录。Game 已移动到持久目录，因此清理旧 Item 不会影响已发布数据。批次丢弃只清理其未发布条目和输入，外部服务器来源文件不删除。

删除任务保存受校验的相对目录，执行前复核任务租约；文件系统删除在 SQL 事务外进行。目录已经不存在视为成功，删除后结算失败可幂等重试。不存在逐文件登记表、候选表、引用计数或全库文件 GC；封面、ROM 和存档替换只清理其已被替换的不可变子目录。

容量分析页面、统计 API、容量分类与“立即清理”功能均已移除。任务失败沿用任务中心的重试能力。清理完成与业务删除是两个阶段，以任务状态及目录实际存在性验证。

## 8. 数据根进程锁

`retrom` 从启动到退出持有 `RETROM_DATA_DIR/retrom.lock` 的 Linux advisory exclusive lock，阻止多个进程同时写同一数据根。lock 文件不是 PID 或秘密，崩溃后由内核释放；数据根必须位于支持该锁语义的本地文件系统。

## 9. 多盘存储边界

当前 clean schema 直接创建 `import_item_multidisc_entries` 与 `review_multidisc_attachments`，并在 source/content/variant/launch/save 表中建立数据模型专题规定的受约束 enum 与列，并由应用存储方法验证跨表归属和状态转换；不执行重建或回填。User/Profile owner、USER/SYSTEM actor 和 principal-scoped idempotency 由当前 schema 原生约束，完成后 `foreign_key_check` 为零。

多盘 Item 独占 DISC、来源 playlist 与派生 canonical playlist，发布时交给 Game；Launch 只冻结文件标识。补盘来自独立上传并交给该 Item。缺盘不创建物理文件，拒绝补盘不推进快照；删除与替换由所属领域退休文件。执行 ACC-DB-001–002、ACC-CAS-001–002、ACC-MDISC-002–004。

## 10. 收藏数据

当前 clean schema 直接创建三张无 Blob 引用的关系表：Favorite、FavoriteFolder 和 FolderMembership；不回填或推断收藏。隐藏游戏或目录只影响投影，不删除收藏关系。

完整字段和索引见 [`data-model.md`](./data-model.md)，关系不变量由 `ACC-FAV-001` 验证。

## 11. 外部服务器 source 边界

服务器导入 root 是 Retrom 数据根之外的只读 source，不属于独立文件存储或依赖物化目录。目录浏览、递归扫描和最终复制都逐段使用 Linux `openat`/`O_NOFOLLOW` 与 `fstat`；只接受规范 UTF-8 相对路径，跳过 special file，并防止 symlink/rename 逃逸。发现完成前不创建 Installation；选中候选进入独立文件存储前重新打开、重新哈希并重验 archive，变化的 source 以 `SOURCE_CHANGED` 收口。

EmulationStation 递归发现只匹配精确小写 `gamelist.xml`；每个 XML 与其中游戏/媒体路径都保留相对于服务器根目录 `/` 的规范路径和 no-follow facts，不保存绝对路径。XML、目录 facts、M3U 与媒体/CHD 头在扫描期受独立字节/数量上限约束，完整 ROM 只在 start 后按冻结 manifest 流式复制进独立文件存储。一个所选目录内的多个子目录清单各自形成 Collection；单目录的一份清单和多文件形成一个 Collection，二者使用同一存储边界。

每个 BIOS 安装与审核条目独占自己的输入文件，同 hash 不去重。交接完成后审核条目保留独立文件，Source 可以独立清理，不能删除已交给审核条目的文件。

## 12. 审核运行预览的存储边界

当前 clean schema 在运行领域创建 runtime_preview_sessions/runtime_preview_files，在审核领域创建 review_preview_bindings/review_runtime_screenshots。审核关联只从 binding 指向中立 Preview，运行表没有导入、审核或 Source 外键。Preview 冻结不透明 scope/revision、返回路径、Provider/Target、Bundle 字节身份和实际资源，不引用 validation_id。运行内容引用已有独立文件存储，不复制成假 Game。

重复试玩仅创建会话，不生成验证记录。审核截图以 import_item_id 唯一，保存成功在同一事务覆盖该条目的当前截图；保存失败保留原结果。旧图片文件由 Item 保留到终态清理。

Preview 对输入文件只持有冻结读取授权；截图属于 Item，临时 checkpoint 属于 Preview 自有目录。最终发布或丢弃在决定事务中撤销 Preview、bootstrap ticket 和隔离 capability；此后运行与存档模块不读取 Item 状态。发布将当前有效截图复制到 Game 自有资产，终态审核媒体返回 404。异步清理负责剩余工作记录及目录删除。恢复资源在事务外复制到恢复 Preview 的自有 restore 目录，提交时重验来源 checkpoint 未变化；复制或提交失败清理未提交目录。恢复资源不跟随后续 checkpoint 覆盖，也不依赖原 Preview 的到期清理。逐文件回收只选择对应 checkpoint/restore workspace，终态才删除整个 Preview 目录。当前截图按 Item 关联，投影时核对来源快照、目标目录与 Provider/Target；BIOS 安装变化不使截图失效。READY 或允许人工放行的阻断状态均可显示截图。HTTP 不暴露内部文件 ID。

## 14. 标签数据边界

Tag、Game/Review/Pegasus/EmulationStation 关系和 tombstone 全部只存在 PostgreSQL，不新增独立文件存储 payload、Blob reference、外部 taxonomy 或运行期下载。软删除保留 DELETED tombstone、历史关系、mapping 名称 snapshot 与审计；同名新 Tag 不继承旧关系。领域文件所有权、物理文件枚举和依赖物化均不因标签改变。

Tag 删除是业务软删除，不是存储清理：不得以减小数据库为由硬删 tombstone/关系。lineage 不匹配的应用不能写库。字段与当前应用写入约束见 [`data-model.md`](./data-model.md)，生命周期见 [`game-tags.md`](./game-tags.md)。

## 15. Provider 激活与数据库协调

服务在开放业务路由前先逐字节校验 active descriptor、已安装 Bundle、manifest、module 和所有声明资产，再把两个
Provider 及当前声明的全部 Target 投影为一个 canonical catalog。协调事务只能整体写入 Provider、Target、binding 和 catalog
state；任一 Target、Host binding、DAT、BIOS、checkpoint reference 不闭合时不得部分激活。

升级只允许 SemVer 增长。事务必须证明所有被当前 Variant、活动运行会话与未完成导入/审核引用的 Target 仍存在；完成的过程记录不构成引用，且每个存档的
checkpoint format 仍可由来源 Launch 的 Core 所对应当前 Variant/Target 的 `readFormats` 读取；其他备用 Core 不代替来源 Core 满足升级门槛。同版换 bytes、降级、移除受引用 Target、catalog digest 不一致或 active
文件在协调后变化均使 readiness 失败。没有数据库降级或恢复旧 Provider 的路径。

PFB loose provider 与 production active descriptor 的 source 和目录必须严格分离。启动时由当前部署的 production descriptor 协调，数据库记录不能授权一个未安装或摘要不符的 Bundle。

## 16. 统一验收入口

PostgreSQL、migration、独立文件存储、后台删除统一执行 [一期项目验收规范](./project-acceptance.md) 的 `ACC-DB-001`–`ACC-DB-002`、`ACC-CAS-001`–`ACC-CAS-002`、`ACC-AUTH-001`–`002`、`ACC-ISO-*`、`ACC-TAG-001` 与 `ACC-ES-002/004`；归档/XML 与内容访问安全执行 `ACC-SEC-001`–`ACC-SEC-002`、`ACC-ES-001`。本文不再维护重复通过条件。

### 请求与 PostgreSQL 分段观测

共享 `telemetry` 只携带请求关联 ID、固定操作名和累计时间；accounts 不依赖数据库包，仍通过业务 repository 获取事务。PostgreSQL adapter 的 `Rows` 边界统计 Next/Scan/Close，分别记录连接池等待、BEGIN、SQL 调用、行消费、COMMIT、写事务持有及扣除 SQL/行消费/提交后的事务内时间。独立查询及取消中的事务必须释放连接；所有连接执行相同 PostgreSQL 初始化设置，生产持久化强度不降低。

上传完成准入与异步文件组装各自拥有独立计时范围。上传后台任务使用新的不透明关联 ID 和固定操作名 `BACKGROUND uploads.finalize`，不能把 HTTP context 中继承的计时对象用于后台执行，避免将后台 writer 等待累计到已返回的请求。

HTTP 请求和登录分段日志使用同一 request_id。长写事务额外记录拥有它的 Go 函数名，绝不记录 SQL/绑定值、用户标识、游戏内容、宿主路径、Cookie 或密码。阶段数值为请求内累计耗时，存在并发操作时不能简单相加充当墙钟时间；事务持有时间包含 SQL 和提交时间。混合负载必须同时报告登录、控制操作和只读页面的完整样本，不能把空载单测或短时采样当作性能验收。验收见 `ACC-DB-003`。
