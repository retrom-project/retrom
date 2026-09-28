# 存储与数据库设计

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期实施基线 |
| 版本 | 1.3 |
| 日期 | 2026-08-24 |
| 适用范围 | Retrom 一期 |

## 1. 文档边界

本文档定义 SQLite 类型约定、时间字段、核心表目录、独立文件存储、Archive 安全以及后台删除。

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

所有表示“某一时刻”的数据库字段统一使用 SQLite `INTEGER`，保存 UTC Unix epoch milliseconds：

- 单位固定为毫秒，不允许同库混用秒、微秒或纳秒。
- 字段名统一使用 `_at_ms` 后缀，例如 `created_at_ms`、`updated_at_ms`、`started_at_ms`、`last_reported_at_ms`、`expires_at_ms`。
- Go 类型使用 `int64`，写入值来自 `time.Now().UTC().UnixMilli()`。
- 浏览器值可直接与 `Date.now()` 对接；展示时才按用户时区格式化。会被 SSR 的 Client Component 固定以 UTC 生成 server/hydration snapshot，hydration 完成后再切换到浏览器解析出的 IANA 时区；不得让容器或 Node 的本地时区参与首轮客户端渲染，也不得通过统一部署时区代替逐用户展示。
- JSON 审核快照、任务事件和 LaunchSession 配置中的时间点也使用带 `Ms` 后缀的整数，避免同一概念在不同层采用不同单位。

禁止：

- 用 `TEXT` 保存 RFC 3339 时间作为业务表的主时间字段。
- 使用 SQLite `CURRENT_TIMESTAMP`，因为它生成文本且精度/格式与本约定不一致。
- 保存服务器本地时区时间。
- 使用无单位语义的字段名，例如 `timestamp`、`time` 或 `created`。

示例：

~~~sql
CREATE TABLE play_sessions (
    id TEXT PRIMARY KEY,
    profile_id TEXT NOT NULL,
    game_id TEXT NOT NULL,
    started_at_ms INTEGER NOT NULL CHECK (started_at_ms >= 0),
    last_reported_at_ms INTEGER NOT NULL CHECK (last_reported_at_ms >= started_at_ms),
    ended_at_ms INTEGER,
    active_duration_ms INTEGER NOT NULL DEFAULT 0 CHECK (active_duration_ms >= 0),
    CHECK (ended_at_ms IS NULL OR ended_at_ms >= started_at_ms)
);

CREATE INDEX idx_play_sessions_started_at_ms
    ON play_sessions(started_at_ms DESC);
~~~

### 2.2 时长、年份与日期

并非所有“与时间有关”的值都是时间戳：

- 时长使用 `INTEGER` 毫秒并以 `_duration_ms` 或 `_interval_ms` 结尾，例如 `active_duration_ms`、`heartbeat_interval_ms`。
- EmulatorJS 固定保存间隔等原生以毫秒定义的配置继续使用毫秒。
- 游戏发行年份使用 `INTEGER` 年份，例如 `release_year = 1996`，不能伪造为某年 1 月 1 日时间戳。
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

### 2.4 旧 TEXT 时间迁移

当前仓库尚无已发布数据库，一期首版 migration 直接创建整数时间列；不得为了兼容一个不存在的旧 schema 增加 TEXT 列、双写层或伪造旧版本 fixture。只有未来确实存在已交付的 TEXT 时间 schema 时，才在独立 migration 变更中按下列流程处理并把该真实旧版本加入支持清单：

1. 先确认所有旧值均为带时区的 RFC 3339/ISO 8601，无法解析的记录进入迁移错误表，不能取当前时间掩盖。
2. 新增 `_at_ms INTEGER` 列并由 Go 迁移程序解析为 UTC 后调用 `UnixMilli()`；不要依赖 SQLite 对各种时区字符串的宽松解析。
3. 比较记录数量、最小/最大值和抽样格式化结果。
4. 使用 SQLite 表重建移除旧 TEXT 列并补上 `NOT NULL`、`CHECK` 和索引。

一期尚未产生业务数据，实施结论就是直接按新字段建表，不保留双写兼容层；本小节不是首版实施任务。

## 3. SQLite 基线

一期固定使用 pure-Go `modernc.org/sqlite`，避免后端镜像隐式依赖 CGO/系统 SQLite。精确 module 版本由 `go.mod/go.sum` 锁定；更换 driver 属于数据库基线变更，必须重跑全部 migration、并发与完整性 Case。

每个数据库连接初始化：

~~~sql
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
~~~

规则：

- 所有写操作通过短事务完成；耗时哈希、网络请求和 DAT 解析不得占用写事务。
- 事务统一通过 `database.DB.BeginTx` 开始。SQLite 接入层固定驱动参数 `_txlock=immediate`，非只读事务在开始时取得写保留，避免先读后写的锁升级冲突；只读事务必须显式传入 `ReadOnly: true`，由驱动使用普通 `BEGIN`，不预占写锁。提交、回滚、context 取消和连接归还统一由 `database/sql` 与驱动管理，不另行持有 `sql.Conn` 手写事务状态机。驱动故障注入连接也必须使用相同事务策略。
- SQLite 数据库和 WAL 必须位于本机磁盘；不支持把数据库放在 NFS/SMB/分布式文件系统。独立文件存储可单独挂载，但必须满足原子 rename 语义。
- 一期只允许一个 `retrom` 进程写同一数据库。写 handle 的 `MaxOpenConns=1`；独立只读 handle 使用 `mode=ro` 且最多 4 个连接，健康探测等只读控制面查询不能排在唯一写连接之后。每个新连接都执行 `foreign_keys=ON` 和 `busy_timeout=5000`，不能只在首个连接设置。
- 外键删除策略默认 `RESTRICT`，业务软删除通过状态字段实现。
- 布尔值使用 `INTEGER NOT NULL CHECK (value IN (0, 1))`。
- 枚举使用 `TEXT` 加 `CHECK` 或稳定字典表，不使用依赖插入顺序的整数枚举。
- JSON 只用于不可变快照、低频配置和 provider raw payload；可查询关系必须规范化。
- 代码种子 ID 使用稳定 code；其他业务实体使用规范小写 UUIDv7 文本。EmulatorJS 要求 number 类型 `EJS_gameID` 的 GameVariant 另存稳定唯一 `INTEGER` surrogate key，范围固定 `1..9007199254740991`，API 不把它字符串化。

### 3.1 clean migration lineage

当前未发布建库基线包含 `001_identity.sql` 至 `014_metadata_media_queue.sql`；`010_indexes.sql` 集中建立已存在 owner 表的索引。基线直接创建 current-state 表、PK/UNIQUE/CHECK/FK 和索引，不包含 trigger 或 view、旧数据回填或外键关闭窗口。每条 migration 与 checksum 记录在同一事务提交。

`store.Open` 在任何 schema 写入前只读检查 `schema_migrations`，只接受不存在/真正空的数据库、当前文件逐项同名同 checksum 的有序前缀，以及完整当前 lineage。此次改写与旧开发基线不兼容，旧 checksum 不会被覆盖；当前前缀只用于中断初始化的续跑，不能解释为支持旧开发库升级。

只读 schema 预检被取消或超过启动期限时保留对应的 context 错误，不将其误报为 `DATABASE_SCHEMA_INVALID`；超时本身不构成重建数据库的依据。

跨表与新旧状态校验由 `recordstore` 的参数化 SQL 执行；会话、存档与回收排期的联动由 `sessionstore` 在同一事务完成。保存点保证校验失败时撤销该次写入，不能依赖调用方最终选择 rollback 来维持不变量。共享查询在 `storequery` 中维护；完整职责及空操作语义见[数据模型](./data-model.md#应用写入与数据库职责)。

不兼容开发数据库必须停机归档旧数据并使用全新空数据根；PFB 使用 exact ID 的 `pfb-data-reset`，归档整个旧 `data/`，保留 Provider/依赖/构建缓存、ID 和 URL。新建且未启动过的 PFB 直接初始化空库。程序不提供转换器、双写或隐式导入，也不得把旧数据库与文件目录拆开混入新库。默认开发数据根为 `.dev-data/data`，测试和验收使用独立临时根。未来发布后的兼容演进仍须追加 migration 并验证明确支持的升级路径，不能改写已发布 checksum。

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
| `import_item_core_validations` / `import_item_validation_files` | 审核可选择的默认核心验证证据与派生文件 |
| `upload_sessions` / `upload_files` | 浏览器上传会话与相对路径 |
| `upload_parts` | 分块上传 |
| `upload_consumptions` | 已完成上传到 Import、游戏文件替换 Job、BIOS/Game Asset/Review Asset 的互斥审计归属 |
| `metadata_scrape_runs` | ImportItem 或 Game 唯一的当前 hash/provider 证据批次 |
| `content_hash_evidence` | run 内的版本化 hash profile、来源 Blob/archive entry 与查询顺序 |
| `metadata_scrape_query_attempts` | run/evidence 到每次网络或缓存 response 的不可变关联 |
| `scrape_candidates` / `scrape_candidate_hits` | Hasheous 元信息候选及多 hash/entry 命中关系 |
| `scrape_candidate_assets` | 候选媒体的受控获取状态、Blob、尺寸、任务绑定、冻结顺序与资源收费 |
| `metadata_media_runs` | 每个刮削 Run 的媒体顺序冻结、累计收费和版本 |
| `review_uploaded_assets` | 审核期间人工上传的不可变封面资源及 Blob 归属 |
| `metadata_provider_cache` | provider + request digest 的可变缓存指针与过期时间 |
| `metadata_provider_responses` | 每次查询的不可变状态、原始响应 Blob 与有效期 |
| `import_items` | 导入 Item 与当前审核字段共用一行；`review_version` 独立于 Item `version`，用于审核乐观并发，不保存编辑历史 |
| `review_draft_screenshot_assets` | 草稿截图选择的规范顺序与外键 |
| `review_preview_sessions` / `review_preview_files` | 审核子窗体的短时不可变运行快照与实际可交付依赖 |
| `review_runtime_screenshots` | 当前 READY 或阻断 Validation 在普通 Player 中按需生成的审核截图与人工放行证据 |
| `review_draft_tags` | 待审核草稿的当前活动标签选择；决定后保留历史关系 |
| `source_collection_tags` | 统一 Collection 的管理员标签映射；名称证据另冻结在 Collection snapshot |

### 4.5 通用任务、幂等与审计

| 表 | 用途 |
| --- | --- |
| `jobs` / `job_input_snapshots` / `job_events` | 带 scope、不可变 execution 输入、lease、attempt、SSE resume ID 的持久 work-unit 与事件 |
| `idempotency_records` | 按 USER/SYSTEM principal 隔离的写操作 24 小时请求/响应重放 |
| `audit_events` | 管理操作 append-only 审计 |
| `schema_migrations` | migration version、name、checksum 与整数应用时刻 |

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
  retrom.db
  files/<game-id-last2>/<game-uuid>/{content,media}/
  secrets/launch-capability.key
  tmp/uploads/<upload-id>/
  staging/items/<item-uuid>/{payload,scratch}/
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

导入条目独占 `staging/items/<Item UUID>/`，其中 `payload/` 是准备发布的目录，`scratch/` 保存审核截图、预览检查点等临时材料。浏览器上传与服务器来源分别使用 `staging/uploads/<Upload UUID>/`、`staging/sources/<SourceItem UUID>/`。来源只是输入；准备完成后 Item 的 ROM 和媒体都能独立于来源存活。

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

当前 clean schema 直接创建 review_preview_sessions、review_preview_files 与 review_runtime_screenshots。Preview 冻结来源、当前 Validation、Provider/Target 与实际 Bundle 字节身份；运行内容引用既有独立文件存储，不复制成假 Game 或用户游玩历史。普通 Player 事件使状态从 CREATED 到 ACTIVE，再到 FINISHED/EXPIRED/REVOKED；终态撤销内容授权。checkpoint 仅有最新 payload/format/time 以及新会话冻结的 restore payload；没有独立 proof 表。bootstrap 有 5 分钟期限，运行授权最长 2 小时；有界 后台删除 和审核终态 OwnerCleanup 清除临时引用。

重复试玩相同输入必须复用已有当前 Validation，包括需要人工试玩的 BLOCKED 结果，不因新建运行窗口追加校验记录。审核截图只维护条目的当前结果：成功保存时，在同一事务中清除该条目其他 Validation 的旧截图并覆盖当前截图；新截图校验或保存失败时保留原结果。截图不是不可变历史记录；旧图片文件由 Item 保留到终态清理。

Preview 是 ImportItem 文件的读取会话，截图与临时 checkpoint 同属 Item，不形成独立保护引用。冻结恢复不跟随当前 checkpoint 覆盖；原文件保留到 Item 终态清理。当前截图只向匹配当前来源、目录、Provider Target 与 input digest 的 Validation 投影；READY 或阻断状态均可使用，阻断截图允许管理员人工放行。HTTP 不暴露内部文件 ID。

## 14. 标签数据边界

Tag、Game/Review/Pegasus/EmulationStation 关系和 tombstone 全部只存在 SQLite，不新增独立文件存储 payload、Blob reference、外部 taxonomy 或运行期下载。软删除保留 DELETED tombstone、历史关系、mapping 名称 snapshot 与审计；同名新 Tag 不继承旧关系。领域文件所有权、物理文件枚举和依赖物化均不因标签改变。

Tag 删除是业务软删除，不是存储清理：不得以减小数据库为由硬删 tombstone/关系。lineage 不匹配的应用不能写库。字段与当前应用写入约束见 [`data-model.md`](./data-model.md)，生命周期见 [`game-tags.md`](./game-tags.md)。

## 15. Provider 激活与数据库协调

服务在开放业务路由前先逐字节校验 active descriptor、已安装 Bundle、manifest、module 和所有声明资产，再把两个
Provider 及 61 个 Target 投影为一个 canonical catalog。协调事务只能整体写入 Provider、Target、binding 和 catalog
state；任一 Target、Host binding、DAT、BIOS、checkpoint reference 不闭合时不得部分激活。

升级只允许 SemVer 增长。事务必须证明所有被 Variant 和 Validation 引用的 Target 仍存在，且每个存档的
checkpoint format 仍可由至少一个当前 READY GameVariant 的 Target `readFormats` 读取。同版换 bytes、降级、移除受引用 Target、catalog digest 不一致或 active
文件在协调后变化均使 readiness 失败。没有数据库降级或恢复旧 Provider 的路径。

PFB loose provider 与 production active descriptor 的 source 和目录必须严格分离。启动时由当前部署的 production descriptor 协调，数据库记录不能授权一个未安装或摘要不符的 Bundle。

## 16. 统一验收入口

SQLite、migration、独立文件存储、后台删除统一执行 [一期项目验收规范](./project-acceptance.md) 的 `ACC-DB-001`–`ACC-DB-002`、`ACC-CAS-001`–`ACC-CAS-002`、`ACC-AUTH-001`–`002`、`ACC-ISO-*`、`ACC-TAG-001` 与 `ACC-ES-002/004`；归档/XML 与内容访问安全执行 `ACC-SEC-001`–`ACC-SEC-002`、`ACC-ES-001`。本文不再维护重复通过条件。
