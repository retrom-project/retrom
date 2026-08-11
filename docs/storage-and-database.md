# 存储与数据库设计

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期实施基线 |
| 版本 | 1.1 |
| 日期 | 2026-08-10 |
| 适用范围 | Retrom 一期 |

## 1. 文档边界

本文档定义 SQLite 类型约定、时间字段、核心表目录、本地 SHA-256 CAS、Archive 安全、垃圾回收以及备份恢复。

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
- 字段名统一使用 `_at_ms` 后缀，例如 `created_at_ms`、`updated_at_ms`、`started_at_ms`、`last_heartbeat_at_ms`、`expires_at_ms`。
- Go 类型使用 `int64`，写入值来自 `time.Now().UTC().UnixMilli()`。
- 浏览器值可直接与 `Date.now()` 对接；展示时才按用户时区格式化。
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
    game_variant_revision_id TEXT NOT NULL,
    started_at_ms INTEGER NOT NULL CHECK (started_at_ms >= 0),
    last_heartbeat_at_ms INTEGER NOT NULL CHECK (last_heartbeat_at_ms >= started_at_ms),
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

一期固定使用 pure-Go `modernc.org/sqlite`，避免后端镜像隐式依赖 CGO/系统 SQLite。精确 module 版本由 `go.mod/go.sum` 锁定；更换 driver 属于数据库基线变更，必须重跑全部 migration、并发、备份与恢复 Case。

每个数据库连接初始化：

~~~sql
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
~~~

规则：

- 所有写操作通过短事务完成；耗时哈希、网络请求和 DAT 解析不得占用写事务。
- SQLite 数据库和 WAL 必须位于本机磁盘；不支持把数据库放在 NFS/SMB/分布式文件系统。CAS 可单独挂载，但必须满足原子 rename 语义。
- 一期只允许一个 `retrom` 进程写同一数据库。写 handle 的 `MaxOpenConns=1`；只读 handle 最多 4 个连接。每个新连接都执行 `foreign_keys=ON` 和 `busy_timeout=5000`，不能只在首个连接设置。
- 外键删除策略默认 `RESTRICT`，业务软删除通过状态字段实现。
- 布尔值使用 `INTEGER NOT NULL CHECK (value IN (0, 1))`。
- 枚举使用 `TEXT` 加 `CHECK` 或稳定字典表，不使用依赖插入顺序的整数枚举。
- JSON 只用于不可变快照、低频配置和 provider raw payload；可查询关系必须规范化。
- 代码种子 ID 使用稳定 code；其他业务实体使用规范小写 UUIDv7 文本。EmulatorJS 要求 number 类型 `EJS_gameID` 的 GameVariantRevision 另存稳定唯一 `INTEGER` surrogate key，范围固定 `1..9007199254740991`，API 不把它字符串化。

### 3.1 账户版本的重建边界

Migration 020 是账户版本边界。`store.Open` 在取得数据根独占锁后先用 SQLite `mode=ro` 探测既有 schema；001–019 旧库必须在执行 PRAGMA 写操作、DDL、DML 或 WAL checkpoint 前以 `DATABASE_REBUILD_REQUIRED` 失败，错误只给出旧/所需版本和“使用新数据根”提示。不存在的数据库或没有 Retrom 业务表的真正空 schema 可从 001 顺序建立到最新版本；已经包含 020 的库继续追加 migration。版本空洞、checksum 漂移、无 migration 记录却已有业务表或 020 结构不完整统一 `DATABASE_SCHEMA_INVALID`，不得动态修补。

发布切换固定为：停止旧版本并归档整个旧数据根 → 保留归档用于旧版本回退 → 为新版本配置全新空数据根 → 以 release 启动并完成主机证明初始化。旧 `local` Profile、游戏或存档不迁移，也没有双写或“选择迁移数据”页面。仓库 `make dev` 的默认根为 `.cache/retrom/user-management-v1-data`；测试与每个验收 Case 使用独立临时 data root 并在结束时删除。

## 4. 表目录

### 4.1 平台、核心与固件

| 表 | 用途 |
| --- | --- |
| `profiles` | 每个账号独立且不可变的 Profile；无 `local` seed |
| `users` / `user_credentials` | 账号身份、角色/状态/version 与 Argon2id 凭据 |
| `auth_sessions` / `account_links` / `instance_state` / `auth_rate_limits` | 登录 session、一次性邀请/重置、初始化状态和 HMAC 限流桶 |
| `platforms` | 基础平台 |
| `cores` | EmulatorJS/Core 配置 |
| `core_artifacts` | 可执行 core artifact、实际版本/hash 和兼容配置 |
| `platform_cores` | 平台与核心多对多关联 |
| `platform_instances` | 用户维护的游戏目录及默认核心 |
| `bios_requirements` | 固件要求 |
| `bios_installations` | 已上传 BIOS |

PlatformInstance 的复合外键、游戏唯一归属和迁移规则见 [游戏目录领域设计](./platform-instance.md)。

### 4.2 文件、游戏与媒体

| 表 | 用途 |
| --- | --- |
| `blobs` | CAS 文件、大小及多种内容哈希 |
| `archive_entries` | 经安全扫描的 archive entry 路径、大小、内容 hash 与可选物化 Blob |
| `games` | 用户可见游戏；`platform_instance_id/current_content_revision_id` 必填，不保存 `platform_id` |
| `game_assets` | 封面、背景、截图等 |
| `game_content_revisions` | 一次已接受的不可变用户内容版本与 canonical manifest |
| `game_content_files` | ContentRevision 的 CONTENT/DOS_SOURCE/COMPANION Blob 与逻辑路径 |
| `dos_entries` | ContentRevision 中经过安全扫描的可执行程序候选 |
| `game_variants` | Game + Core 唯一的稳定逻辑槽及当前 revision 指针 |
| `game_variant_revisions` | 某 ContentRevision 在一个 CoreArtifact 上的不可变验证/运行结果 |
| `variant_files` | parent、BIOS bundle、DOS launch bundle 等 core-specific/派生文件 |
| `game_metadata_revisions` | 已发布元信息修订 |

`variant_files.role` 一期固定支持：

- `PARENT`
- `BIOS_BUNDLE`
- `DOS_LAUNCH_BUNDLE`

用户内容的 `CONTENT/DOS_SOURCE/COMPANION` role 只属于 `game_content_files`；不得再复制到 VariantRevision 形成第二条“当前文件”事实源。

### 4.3 DAT

| 表 | 用途 |
| --- | --- |
| `dat_versions` | Core artifact 专属 DAT、来源、SHA-256、解析器版本、兼容性及活动状态 |
| `dat_import_jobs` | 用户 DAT 上传、解析、差异、启用与重校验任务 |
| `dat_machines` | machine |
| `dat_bios_sets` | MAME machine 的 BIOS option/default |
| `dat_rom_entries` | ROM entry |
| `dat_disk_entries` | CHD/disk |
| `variant_dependencies` | GameVariantRevision 的 parent/BIOS 依赖快照 |

### 4.4 导入、审核与元信息

| 表 | 用途 |
| --- | --- |
| `import_jobs` | 一次导入任务及目标游戏目录快照 |
| `import_job_files` | UploadSession 每个文件的 SOURCE/IGNORED/REJECTED 分类与原因 |
| `import_items` | 单个游戏候选 |
| `import_item_source_files` | 候选的 CONTENT/DOS_SOURCE/COMPANION 发布前文件映射 |
| `import_item_dos_entries` | 发布前 DOS 程序候选 |
| `import_item_core_validations` / `import_item_validation_files` | 审核可选择的默认核心验证证据与派生文件 |
| `upload_sessions` / `upload_files` | 浏览器上传会话与相对路径 |
| `upload_parts` | 分块上传 |
| `upload_consumptions` | 已完成上传到 Import、游戏文件替换 Job、BIOS/DAT/Game Asset/Review Asset 的互斥审计归属 |
| `metadata_scrape_runs` | ImportItem 或 Game 的一次 hash/provider 证据批次 |
| `content_hash_evidence` | run 内的版本化 hash profile、来源 Blob/archive entry 与查询顺序 |
| `metadata_scrape_query_attempts` | run/evidence 到每次网络或缓存 response 的不可变关联 |
| `scrape_candidates` / `scrape_candidate_hits` | Hasheous 元信息候选及多 hash/entry 命中关系 |
| `scrape_candidate_assets` | 候选媒体的受控获取状态、Blob 与尺寸 |
| `review_uploaded_assets` | 审核期间人工上传的不可变封面资源及 Blob 归属 |
| `metadata_provider_cache` | provider + request digest 的可变缓存指针与过期时间 |
| `metadata_provider_responses` | 每次查询的不可变状态、原始响应 Blob 与有效期 |
| `review_drafts` | 待审核条目的可编辑草稿与 version |
| `review_draft_screenshot_assets` | 草稿截图选择的规范顺序与外键 |
| `review_events` | 追加式审核历史 |

### 4.5 通用任务、幂等与审计

| 表 | 用途 |
| --- | --- |
| `jobs` / `job_input_snapshots` / `job_events` | 带 scope、不可变 execution 输入、lease、attempt、SSE resume ID 的持久 work-unit 与事件 |
| `idempotency_records` | 按 USER/SYSTEM principal 隔离的写操作 24 小时请求/响应重放 |
| `audit_events` | 管理操作 append-only 审计 |
| `blob_gc_candidates` | 两阶段 Blob 回收与失败重试 |
| `schema_migrations` | migration version、name、checksum 与整数应用时刻 |

### 4.6 启动与游玩数据

| 表 | 用途 |
| --- | --- |
| `save_states` | 带截图的手动状态存档 |
| `persistent_saves` | SRAM/NVRAM/DOS overlay |
| `persistent_save_revisions` | 持久保存不可变 Blob revision |
| `play_sessions` | 有效游玩会话和 heartbeat |
| `play_session_events` | 连续 client sequence、服务端计时判定与幂等证据 |
| `launch_sessions` | 短期不可变启动配置、非秘密 launchId 与 capability hash |

所有表中的时间点和时长必须遵守第 2 节，不能由各模块自行选择类型或单位。表的必需字段、枚举、唯一索引、append-only revision 和 trigger 以 [一期数据库实体与不变量](./data-model.md) 为唯一数据字典；本节只做模块目录，不能据此省略该文档的约束。

## 5. 本地 CAS 文件存储

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

.cache/retrom/user-management-v1-data/ # 开发 RETROM_DATA_DIR，不进入版本控制；旧 data 目录不自动删除
  retrom.lock
  retrom.db
  blobs/sha256/ab/cd/<64-char-sha256>
  secrets/launch-capability.key
  tmp/uploads/<upload-id>/
  tmp/jobs/
~~~

项目 ignore 规则必须忽略 `.cache/`、`data/runtime/**` 和五个 DAT payload 目录，只允许 manifest、`SHA256SUMS`、文档和脚本进入 Git；许可原文与生成 notice 也属于 runtime payload，不能因体积小而提交成第二份事实源。生产 `RETROM_DATA_DIR` 使用独立持久卷；不得把只读依赖目录挂成业务数据根。`make prepare-deps` 在服务启动前物化并校验 payload；应用同步预检只校验、不下载，随后 Worker 可从已校验只读 DAT 建立数据库索引。用户上传 DAT 作为 Blob 写入 CAS，由 `dat_versions.blob_id` 引用；不得改写内置 DAT 基线。完整契约见 [第三方依赖管理](./dependency-management.md)。

### 5.2 Blob 写入

1. 在 CAS 同一文件系统的任务目录创建 `0600` 独占临时文件；不使用上传文件名。
2. 从实际 bytes 流式计算 SHA-256、MD5、SHA-1、CRC32 和 size，不信任客户端声明值。
3. 完整接收后 `fsync` 文件并关闭；校验传输 size/digest。
4. 创建 `sha256/ab/cd/`，通过独占 create/link 或同文件系统 atomic rename 发布；并发相同 hash 只有一方发布，另一方验证已有目标 size/hash 后复用。
5. `fsync` 目标父目录。数据库短事务写 Blob 和业务引用；事务失败允许留下无引用完整 Blob，由 GC 处理，但绝不留下指向半文件的引用。
6. 清理失败临时文件是可重试任务；进程崩溃后的陈旧临时文件按创建时刻回收，不把临时目录作为 CAS 内容。

禁止使用原文件名作为物理存储路径；原始文件名仅保留在业务元数据中。

### 5.3 内容服务

固定公开资源与启动受限内容使用不同缓存策略：

- `GET`
- `HEAD`
- Range 请求
- `ETag: "<sha256>"`
- 固定版本 EmulatorJS 与发布媒体：`Cache-Control: public, max-age=31536000, immutable`。
- ROM、parent、BIOS、状态存档：仅经 launch cookie 的逻辑路径访问，`Cache-Control: private, no-store`、`Vary: Cookie`，URL 不含 Blob ID/hash。

公开媒体以不可变 GameAsset ID 形成新 URL；固定运行时包含明确版本。受限内容由 LaunchSession 映射到不可变 VariantRevision。所有端点设置强 ETag、正确 MIME 与 `nosniff`；精确 Range 行为见 [HTTP API 契约](./http-api-contract.md)。

## 6. Archive 安全

- ZIP 在服务进程内使用受限 reader；7z 必须由同一后端二进制的隐藏 worker 子进程读取，父进程只传只读 fd，不传用户路径，也不调用宿主 `7z/7zz`。Linux worker fail-closed 设置 Go 512 MiB memory limit、2 GiB `RLIMIT_AS`、8 GiB `RLIMIT_FSIZE`、64 个 fd、0 core dump、120 秒 CPU，上层 wall timeout 125 秒且 IPC JSON 最多 64 MiB；OS 无法建立限制时返回 `ARCHIVE_SANDBOX_UNAVAILABLE`。worker crash/signal/timeout/resource/超长 IPC 统一为 `ARCHIVE_RESOURCE_LIMIT`。
- 7z 只接受首字节 magic `37 7a bc af 27 1c` 的未加密、单卷、非 SFX archive，并用 `NewReader(readerAt,size)` 禁止邻接分卷发现。最多 20,000 个 regular-file entry、单 entry 8 GiB、总展开 32 GiB、展开/原包比 200；扫描按自然 ordinal 顺序完整读取、校验 CRC/声明大小并计算四种 hash。父进程先 SCAN 再按唯一候选 ordinal MATERIALIZE，输出以 `expectedSize+1` 限流进入 CAS；任何半成品都不能成为 Blob 引用。
- 上传 manifest 与 ZIP entry 共用 `SAFE_LOGICAL_PATH_V1`：输入必须是有效 UTF-8，使用 `/` 分隔，整体 1–1,024 UTF-8 bytes、每段 1–255 bytes；拒绝开头/结尾 `/`、空段、`.`/`..` 段、反斜杠、NUL、U+0001..U+001F、U+007F、Windows drive 前缀和 UNC/绝对路径。字符串不做 percent decode、Unicode NFC/NFD 或平台文件系统 canonicalization；存储的 `normalized_path` 只是把已验证段以单个 `/` 连接，因此相同原始 bytes 必须得到相同结果。UI 展示时仍按纯文本转义。
- ZIP central directory 的每个 name 都先执行该算法。显式目录 entry 必须且只能以单个 `/` 结尾：分类为 directory 后先去掉这个终止符，再对剩余非空 path 执行 `SAFE_LOGICAL_PATH_V1`，通过后忽略该 entry；不能把“目录例外”用于接受 `//`、根目录、`.`/`..` 或反斜杠。任何 symlink、hardlink/device/FIFO/socket、加密 entry 或路径不安全都会阻断整个 archive。无 Unix mode 的非目录 entry 可按 regular file 处理；存在 mode 时只接受 regular file/directory。只支持 ZIP method 0（Store）和 8（Deflate）；ZIP64 只有在同一大小门禁内才允许，不注册额外 decompressor。
- `archive_entries.original_relative_path` 保留安全原名，`normalized_path` 保留其大小写，另保存 `ascii_casefold_path`（只把 ASCII `A..Z` 映射为 `a..z`）。同一 archive 对 normalized path 和 ASCII-casefold path 都唯一；因此 `ROM.BIN/rom.bin` 稳定阻断而不会在 Arcade/DOS 虚拟文件系统中互相覆盖。DAT entry lookup、BIOS 重验证和依赖预览查询该已索引 key，不重读 archive。
- 安全扫描在同一次有界解压流中计算每个 regular entry 的 size/CRC32/MD5/SHA-1/SHA-256，并在完整 archive 通过所有门禁后才原子提交 ArchiveEntry 集；不因 central-directory 声明值相同而跳过实际 bytes。只有主机唯一 ROM member、DOS_SOURCE 或其他明确领域引用需要独立 bytes 时才物化到 CAS；Arcade ROMset 验证可使用已保存 hash，不默认复制全部内层 entry。同一 Archive Blob 后续在另一合法领域流程需要 member 时，允许经数据模型规定的一次性校验提升填写 `materialized_blob_id`；不得为规避不可变字段而重复造 ArchiveEntry 或盲目重读所有 entry。
- ArchiveEntry 是所属 archive Blob 的不可变派生索引，不是让 owning Blob 永久免于 GC 的业务引用。普通 repository 不提供删除 entry 的方法；只有当 owning Blob 无业务保护引用、所有 entry 无 GameContentFile/ImportItemSourceFile/ContentHashEvidence 等复合外键引用时，GC 才能在删除 owning Blob 的同一短事务先按 `archive_blob_id` 成组删除 entry。若 entry 物化的内层 Blob 不再有其他引用，它在后续 GC 轮次独立进入保留期；不在同一事务级联删物理文件。
- 限制 entry 数、单 entry 展开大小、总展开大小和压缩比；压缩大小为 0 而展开大小非 0 直接视为超限。路径/entry 门禁先于物化。任一 regular-file entry 的规范扩展名或文件魔数只要表明它仍是 ZIP/7z/RAR/TAR/gzip 等归档，就以 `NESTED_ARCHIVE_UNSUPPORTED` 阻断整个外层 archive；一期不递归展开，也不能靠改扩展名绕过魔数检查。
- XML DAT 解析只允许 BIOS/DAT 专题定义的一个有界 DOCTYPE 声明并在 token stream 前安全移除；绝不解释 DTD/实体，也不允许外部实体或网络访问。不能把这条简写实现成“拒绝所有真实 DAT 的 DOCTYPE”。
- 不信任扩展名、ZIP 声明 MIME 或 archive 内路径。
- 读取 Arcade ZIP central directory 时不默认展开全部内容到磁盘。
- Arcade DAT 遇到运行必需 CHD 仍直接产生 `UNSUPPORTED_CHD` 审核 Blocker；PSX、Saturn、3DO、PC-FX 的 STANDARD profile 接受单个 raw CHD，Saturn 另可在 capability 明确允许时使用 `MULTI_DISC_M3U_V1`，这些规则不能与 Arcade CHD 混用。PSP 的 raw ISO/CSO 不作为 archive 扫描。

## 7. 垃圾回收

- GC、备份完整性检查和存储审计共用一份机器可读 `blob reference registry`，每个 schema 中的 Blob FK/JSON Blob 引用必须恰好登记为以下一类：`PROTECTIVE`（业务根引用）、`ARCHIVE_OWNERSHIP`（`archive_entries.archive_blob_id/materialized_blob_id` 的派生所有权边）或 `BOOKKEEPING`（`blob_gc_candidates.blob_id` 等不阻止删除的记账边）。未登记、重复登记或分类错误都使 CI 失败；不把可变 `ref_count` 作为事实源。
- GC 保护集先取所有 `PROTECTIVE` Blob，再对其中的 archive Blob 加入该 ArchiveEntry 已物化的内层 Blob；一期禁止 nested archive，因此一层闭包即完整。`ARCHIVE_OWNERSHIP` 不会反向把一个无业务根的 owning archive 变成永久受保护；`BOOKKEEPING` 从不进入保护集。备份不能直接采用这个 GC 保护集：它逐字节复制未裁剪的 SQLite 快照，所以必须复制快照中每一条 `blobs` 行对应的物理文件，包括尚在 GC 宽限期的无业务引用行；registry 用于证明所有引用边都命中这些 Blob 行。只有“物理文件存在但数据库没有 Blob 行”的 crash orphan 才不进入备份。
- GameContentRevision、ImportItem/Upload/Job、Review snapshot、SaveState、媒体、旧 GameVariantRevision 和 DAT 均可能引用 Blob。
- Discard、软删除或替换文件不立即删除 Blob。
- 先进入默认 7 天回收保留期（配置只允许 1–30 天），之后在删除事务前后两次确认无引用才删除。
- 过期的无消费 Upload archive 在失去最后 `PROTECTIVE` 边后可正常进入 GC，不能被自身 ArchiveEntry 永久保活。删除事务再次计算保护集并检查所有 entry 复合外键；有新引用即撤销 candidate。无引用 archive 先成组删索引再删 Blob 行，事务提交后才无跟随删除物理文件；失败可幂等重试。删除任务记录 `scheduled_at_ms`、`deleted_at_ms` 或失败时间。

## 8. 备份与恢复

一期备份/恢复是显式离线维护命令，不伪装成不存在的 HTTP 管理 API：

```bash
# 先正常停止 retrom 服务；输出路径必须是绝对且尚不存在，并位于数据根之外
retrom backup --output /backup-volume/retrom-20260806

# 恢复目标也必须是绝对且尚不存在；依赖已由部署方预物化，不会联网或覆盖现有数据根
RETROM_DEPENDENCY_ROOT=/opt/retrom/dependencies \
RETROM_DEPENDENCY_VERSIONS=4.2.3,4.3.0-pre \
RETROM_ACTIVE_EMULATORJS_VERSION=4.2.3 \
retrom restore --input /backup-volume/retrom-20260806 \
  --output-data-dir /srv/retrom-restored
```

`retrom` serve 进程从启动到退出持有 `RETROM_DATA_DIR/retrom.lock` 的 Linux advisory exclusive lock；`backup` 使用同一非阻塞锁，服务仍运行时以 `BACKUP_REQUIRES_OFFLINE` 失败，不尝试在线复制。lock 文件不是 PID/秘密，崩溃后由内核释放。数据根已被限定为本地文件系统，这一约束也适用于 lock。`restore` 只创建新目标，无需接管正在运行的数据根。默认无参数仍启动服务，维护子命令不得隐式启动 HTTP/worker。

完整 backup bundle 的 v1 目录固定如下；目录模式均为 `0700`、普通文件均为 `0600`，不保留源文件的 group/other permission、owner、mtime、xattr 或 ACL：

```text
<bundle>/
  backup.json
  retrom.db
  blobs/sha256/ab/cd/<64-lowercase-hex>
  tmp/uploads/<upload_parts.storage_key>
  secrets/launch-capability.key
  dependencies/emulatorjs/<version>/manifest.json
  dependencies/emulatorjs/<version>/SHA256SUMS
```

- `retrom.db` 是持锁后以普通 SQLite PRAGMA 完成一致性检查和 WAL checkpoint、关闭全部数据库 handle 后逐字节复制的单文件快照；不依赖 driver 私有 Backup API，也不依赖源 WAL/SHM。
- `blobs/sha256/...` 精确复制 staging 数据库快照中每一条 `blobs` 行对应的 CAS 文件，包括用户 DAT、审核/provider 证据、游戏、媒体、存档，以及仍在 GC 宽限期但暂时没有业务保护边的 Blob。因为 `retrom.db` 是未裁剪的原样快照，少复制其中任一 Blob 行都会制造不可恢复数据库；反之，只有物理 CAS 文件存在而数据库没有 Blob 行的 crash orphan 不复制。
- `tmp/uploads/...` 只复制快照中 `upload_parts.storage_key` 仍引用的未完成分块，并逐项验证 size/SHA-256；完成上传的 part 已按清理契约不存在。`storage_key` 是相对于 `RETROM_DATA_DIR/tmp/uploads/` 的 `SAFE_LOGICAL_PATH_V1` 路径，本身不得再带 `tmp/uploads/` 前缀；备份路径只拼接一次该前缀。key 必须数据库唯一并在复制前重新校验，不能借备份复制任意宿主文件。
- `secrets/launch-capability.key` 是原始 32 bytes；manifest 可记录其 SHA-256 用于完整性，但日志/报告只能给出校验布尔值。
- 每个配置版本只复制小型 dependency manifest 和对应 `SHA256SUMS` 作为恢复证据。内置 runtime/DAT/许可大 payload 不进入 bundle，由部署方按固定 manifest 预先物化到只读依赖根；用户 DAT 已作为 CAS Blob 备份。不存在另一个含糊的“运行配置快照”文件，active 与版本列表只在 `backup.json` 表达。

`backup.json` 必须是下列封闭 schema 的 RFC 8785 canonical JSON；字段名、类型与枚举不得由实现自行扩展。`files` 覆盖除 `backup.json` 自身外的全部普通文件，按 `path` 的原始 UTF-8 bytes 升序；路径使用 `/`、非空相对路径、无 `.`/`..`/反斜杠/NUL/控制字符，且不能重复或 ASCII case-fold 冲突。`dependencyManifests` 按 SemVer 升序且与 `dependencyVersions` 一一对应：

```json
{
  "schemaVersion": 1,
  "createdAtMs": 1785945600000,
  "databaseSchemaVersion": 1,
  "databaseSha256": "<64 lowercase hex>",
  "activeEmulatorjsVersion": "4.2.3",
  "dependencyVersions": ["4.2.3", "4.3.0-pre"],
  "dependencyManifests": [
    {
      "version": "4.2.3",
      "manifestPath": "dependencies/emulatorjs/4.2.3/manifest.json",
      "manifestSha256": "<64 lowercase hex>",
      "sha256sumsPath": "dependencies/emulatorjs/4.2.3/SHA256SUMS",
      "sha256sumsSha256": "<64 lowercase hex>"
    },
    {
      "version": "4.3.0-pre",
      "manifestPath": "dependencies/emulatorjs/4.3.0-pre/manifest.json",
      "manifestSha256": "<64 lowercase hex>",
      "sha256sumsPath": "dependencies/emulatorjs/4.3.0-pre/SHA256SUMS",
      "sha256sumsSha256": "<64 lowercase hex>"
    }
  ],
  "files": [
    {
      "path": "retrom.db",
      "kind": "DATABASE",
      "sizeBytes": 4096,
      "sha256": "<64 lowercase hex>",
      "mode": "0600"
    }
  ],
  "counts": {
    "fileCount": 1,
    "blobCount": 0,
    "uploadPartCount": 0,
    "dependencyVersionCount": 1
  }
}
```

`files.kind` 只允许 `DATABASE | CAS_BLOB | UPLOAD_PART | LAUNCH_KEY | DEPENDENCY_MANIFEST | DEPENDENCY_SHA256SUMS`，且路径与 kind 必须符合上面的唯一目录槽；恰有一个 DATABASE、一个 LAUNCH_KEY、每版本一对依赖证据。`databaseSha256` 必须等于 DATABASE 行，四个 count 必须与数组/路径实际计数相等，所有 `sizeBytes` 是非负 int64。schema v1 的 object 全部拒绝未知字段；`backup.json` 不自包含 hash，最终 bundle 的外部签名不在一期范围。schema v1 有意不放入自由形式的应用版本字符串：恢复兼容性只由 `schemaVersion/databaseSchemaVersion` 与固定依赖证据决定，交付 commit 和双镜像 release-input label 由验收报告记录。清单不得含源/目标绝对路径、cookie、capability/key 明文或 Blob 业务名称。

`tmp/jobs` 永远只是可丢弃 scratch：Job 的唯一可恢复输入必须是数据库、CAS、ArchiveEntry 或 UploadFile 引用，不能只存在该目录，所以它不进入备份。新增任何 Blob FK/JSON blob 引用时必须同时更新第 7 节唯一且带边分类的 `blob reference registry`；GC 从它计算保护闭包，备份/完整性检查从它验证每条引用边都命中 `blobs` 行，CI 以 schema introspection 证明没有遗漏，禁止三个模块各维护一份手写引用清单。备份的物理 CAS 枚举则始终直接来自 staging DB 的全部 `blobs` 行，不能把 GC 保护闭包误当作备份集合。

备份算法固定为：取得离线 lock → 打开源库并执行 `integrity_check/foreign_key_check` → 执行 `PRAGMA wal_checkpoint(TRUNCATE)` 并要求返回 `busy=0` → 关闭全部数据库 handle → 无跟随打开源 `retrom.db` 并复制到 staging → 打开 staging DB 再执行相同检查，以 registry 校验全部引用边并直接枚举全部 `blobs` 行、未完成 UploadPart 与配置版本 → 逐个无跟随读取并验证 CAS/part/key → 写 canonical manifest → fsync 文件与目录 → 将输出父目录下的 owner-only sibling staging 目录原子 rename 为请求的最终路径。checkpoint 或 close 后若源 `-wal` 仍含未合并 frame，操作失败；`-shm` 是否存在不影响 bundle但不复制。exclusive data-root lock 保证关闭连接到复制完成之间没有另一个 Retrom writer。输出已存在、位于源数据根内、任一 Blob 行对应文件缺失/漂移、引用 registry 不闭合或空间不足都会失败；单纯存在“物理文件有而数据库无 Blob 行”的 crash orphan 只记录计数诊断，不进入 bundle，也不阻止对一致数据库的备份。staging 失败时绝不以最终名发布，名称带本次 UUID，可由操作者明确清理，不覆盖任何旧备份。

恢复先离线严格解析/验证 `backup.json`、目录槽、模式和每个文件，再确认当前二进制支持 backup/database schema。`restore` 必须显式读取 `RETROM_DEPENDENCY_ROOT`、`RETROM_DEPENDENCY_VERSIONS` 和 `RETROM_ACTIVE_EMULATORJS_VERSION`：环境中的版本列表与 active 必须逐字等于 bundle 记录，依赖根中的这些版本逐份 manifest/SHA256SUMS hash 必须等于 bundle 证据并通过完整 `deps-check`；根中额外物理版本目录可以存在但不扫描、不加入配置。命令不得编辑 shell、compose 或部署环境，也不能从网络补下载；不匹配以 `RESTORE_DEPENDENCY_CONFIG_MISMATCH` 失败。

验证完成后，在目标父目录创建 owner-only sibling staging 数据根，只把 DB/CAS/part/key 恢复到其数据根槽并统一权限；bundle 内 dependency 证据不复制进数据根。运行 `PRAGMA integrity_check`、`foreign_key_check`、Blob/UploadPart 引用和 DAT/依赖校验，全部通过才原子 rename 为 `--output-data-dir`。目标存在（即使为空）也拒绝，避免误覆盖；恢复失败不发布目标。成功输出只给出目标、`requiredDependencyVersions` 与 `requiredActiveEmulatorjsVersion`，不输出 key/hash/绝对源路径；操作者必须显式以该目标数据根和同一依赖配置启动服务。这样“额外已安装版本可忽略”和“服务使用 bundle 版本列表”之间没有隐式状态。

恢复发布前还必须在 staging 数据库执行单一安全围栏事务：全部未撤销 AuthSession 以 `RESTORE` 撤销、全部 ACTIVE AccountLink 由 SYSTEM 撤销、全部 `CREATED/ACTIVE` LaunchSession 转为 `REVOKED`，并追加 `RESTORE_SECURITY_FENCE` SYSTEM 审计。备份内 User/Profile/私有数据保持逐项一致，但旧 session cookie、邀请/重置 capability 和 launch cookie 在恢复目标上全部失效；用户只能用现有密码重新登录。围栏失败则恢复不发布目标。

## 9. 多盘存储与升级边界

`024_multidisc.sql` 只支持 fresh schema 与 023 账户库顺序升级；001–019 仍在任何写入前返回 `DATABASE_REBUILD_REQUIRED`。migration rebuild 必须逐列保留 User/Profile owner、USER/SYSTEM actor 和 principal-scoped idempotency，完成后 `foreign_key_check` 为零。新表为 `import_item_multidisc_entries` 与 `review_multidisc_attachments`；既有 source/content/variant/launch/save 表按数据模型专题增加受约束 enum/列与 trigger。

GC 把初始和 effective SourceSnapshot、accepted/retryable Attachment、GameContent DISC/playlist、Variant canonical playlist、Launch 锁定 DISC、SaveState 锁定 Variant 视为 Blob 引用根。缺盘 entry 没有 Blob，拒绝补传不推进 effective snapshot；未引用上传文件只受既有 Upload/Job 保留期保护，不能因 entry 占位永久保活。统一执行 `ACC-DB-001`–`002`、`ACC-CAS-001`–`002` 与 `ACC-MDISC-002`–`004`。

## 10. 收藏数据、升级与恢复

Migration 025 新增三张无 Blob 引用的关系表：Favorite、FavoriteFolder 和 FolderMembership。它不重建旧表、不关闭 foreign keys、不回填推断收藏；`migrations/testdata/supported_versions.json` 同时列出 23、24，并分别验证顺序升级。三张表随 SQLite 离线 backup 自然进入快照；restore 的 session/link/launch 安全围栏不得删除收藏关系。

回滚旧应用必须停止服务并恢复部署前完整数据根，不允许删除 025 表、手工降低 `schema_migrations` 或让旧二进制继续写新 schema。Blob reference registry 保持不变。完整字段和索引见 [`data-model.md`](./data-model.md)，恢复证据由 `ACC-FAV-001` 维护。

## 11. 外部服务器 source 与恢复边界

服务器导入 root 是 Retrom 数据根之外的只读 source，不进入 backup、CAS 引用根或依赖物化目录。目录浏览、递归扫描和最终复制都逐段使用 Linux `openat`/`O_NOFOLLOW` 与 `fstat`；只接受规范 UTF-8 相对路径，跳过 special file，并防止 symlink/rename 逃逸。发现完成前不创建 Installation；选中候选进入 CAS 前重新打开、重新哈希并重验 archive，变化的 source 以 `SOURCE_CHANGED` 收口。

候选 bytes 可由 SHA-256 CAS 去重；只有 Installation 等业务引用保护 Blob，无引用候选由统一 GC 回收。backup 保留 ServerImport/Item/Candidate 审计和已经导入的 CAS bytes，但不打包外部目录。restore 在开放 HTTP 前把所有非终态 `SERVER_BIOS_IMPORT` Job 与 ServerImport 置为不可重试 `FAILED/SERVER_IMPORT_SOURCE_NOT_RESTORED`，即使恢复主机存在同名 root 也不得自动继续。

## 12. 统一验收入口

SQLite、migration、CAS、GC 与备份统一执行 [一期项目验收规范](./project-acceptance.md) 的 `ACC-DB-001`–`ACC-DB-002`、`ACC-CAS-001`–`ACC-CAS-002`、`ACC-BKP-001`、`ACC-AUTH-001`–`002` 与 `ACC-ISO-*`；归档/XML 与内容访问安全执行 `ACC-SEC-001`–`ACC-SEC-002`。本文不再维护重复通过条件。
