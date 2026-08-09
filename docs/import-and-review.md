# 游戏导入、元信息刮削与审核

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期实施基线 |
| 版本 | 1.3 |
| 日期 | 2026-08-08 |
| 元信息源 | Hasheous 公共 Hash Lookup |

## 1. 边界

导入链路将文件接收、运行依赖识别、展示元信息刮削和人工审核分开：

- Arcade Core DAT 回答“这组 ROM 在特定核心下如何运行”，只处理 machine、parent、BIOS 和 entry 校验。
- Hasheous 回答“这是什么游戏以及如何展示”，生成标题、平台证据、厂商、描述和图片候选；平台证据只用于告警，不改目标基础平台或目录。
- DAT 的 description/year/manufacturer 只保留在依赖诊断快照中，不能直接覆盖 Game 展示元信息。
- Hasheous 未命中不影响 DAT 依赖判断；DAT 缺失也不能被元信息命中掩盖。
- 一期不接入 ScreenScraper。

Arcade 依赖细节见 [BIOS 与 Arcade DAT](./bios-and-arcade.md)，文件存储见 [存储与数据库设计](./storage-and-database.md)。

## 2. 任务层级

- `UploadSession/UploadFile/UploadPart`：一次浏览器文件或目录上传及可恢复分块；分块齐备后由 `UPLOAD_FINALIZE` 异步 Job 流式组装、重算 hash 并发布 CAS。
- `ImportJob`：从一个已完成 UploadSession 创建的一次导入。
- `ImportItem`：识别出的单个游戏候选。
- `ImportItemCoreValidation/ImportItemValidationFile`：发布前针对目标平台目录默认核心完成的不可变验证结论和派生依赖文件；审核通过只复制这份 READY 证据，不在事务中重新扫描/打包。
- `MetadataScrapeRun`：一个 ImportItem（或已发布 Game 的重新刮削）的一次 provider/hash 证据批次；provider=NONE 只保留一次明确的 no-op Run/Job 和审核选择，不创建伪造的 ContentHashEvidence、QueryAttempt、ProviderResponse 或 Candidate。
- `ScrapeCandidate/ScrapeCandidateAsset`：一次 run 中 provider 返回的展示元信息候选和可独立下载/失败的媒体候选。
- `ReviewEvent`：追加式审核事件与前后快照。

所有时间点使用 `_at_ms INTEGER` Unix 毫秒，任务耗时使用 `_duration_ms INTEGER`。

## 3. 创建任务与配置快照

用户必须选择目标 PlatformInstance（UI：“平台目录”），不能直接选择 Platform。

创建 ImportJob 时冻结：

- `target_platform_instance_id`。
- `platform_id_snapshot`。
- `default_core_id_snapshot`。
- EmulatorJS 版本、Core artifact 文件名与 SHA-256。
- Arcade `dat_version_id_snapshot`。
- MetadataProvider 配置版本。
- `created_at_ms`。

选择器按基础平台分组，并同时展示目录名称、默认核心以及 Arcade 活动 DAT 状态。任务执行期间目录或 DAT 发生变化时，旧任务继续使用快照；审核前提示差异并要求重新验证，不能静默改用新配置。

## 4. 状态机与恢复

三个状态机不得混成一个字段：

- Upload：`CREATED | UPLOADING | FINALIZING | COMPLETE | FAILED | CANCELLED | EXPIRED`；
- ImportJob：`QUEUED | RUNNING | REVIEW_PENDING | PARTIAL_FAILURE | COMPLETED | CANCEL_REQUESTED | CANCELLED | FAILED`；
- ImportItem：`QUEUED | HASHING | IDENTIFYING | SCRAPING | REVIEW_PENDING | PUBLISHED | DISCARDED | FAILED_RETRYABLE | FAILED_FINAL | CANCELLED`。

~~~mermaid
stateDiagram-v2
    [*] --> QUEUED
    QUEUED --> HASHING
    HASHING --> IDENTIFYING
    IDENTIFYING --> DISCARDED : active game already uses identical content
    IDENTIFYING --> SCRAPING
    SCRAPING --> REVIEW_PENDING
    REVIEW_PENDING --> PUBLISHED
    REVIEW_PENDING --> DISCARDED
    HASHING --> FAILED_RETRYABLE
    IDENTIFYING --> FAILED_RETRYABLE
    SCRAPING --> FAILED_RETRYABLE
    HASHING --> FAILED_FINAL : deterministic failure
    IDENTIFYING --> FAILED_FINAL : deterministic failure
    SCRAPING --> FAILED_FINAL : deterministic failure
    FAILED_RETRYABLE --> QUEUED : retry
    FAILED_RETRYABLE --> FAILED_FINAL : attempts exhausted
    QUEUED --> CANCELLED
    HASHING --> CANCELLED
    IDENTIFYING --> CANCELLED
    SCRAPING --> CANCELLED
    FAILED_RETRYABLE --> CANCELLED
    REVIEW_PENDING --> CANCELLED
~~~

ImportJob 按下列优先级聚合，不能让同一计数组合得到两种状态：首次 Worker 尚未领取为 `QUEUED`；存在 queued/running pipeline 时为 `RUNNING`；无运行项但有失败 Item 或 REJECTED 文件为 `PARTIAL_FAILURE`；只有待审核且无失败/拒绝时为 `REVIEW_PENDING`；全部 Item 为 PUBLISHED/DISCARDED 且无 rejected file 时才为 `COMPLETED`；任务级不可恢复故障才为 `FAILED`。`PARTIAL_FAILURE` 是仍可重试、审核或显式取消收口的“需要处置”状态，不是完成终态。显式取消若能同步终止所有未决 Item，直接为 `CANCELLED`；仍有 Worker 需要在有界检查点停止时先为 `CANCEL_REQUESTED`，全部未决 Item 确认 CANCELLED 后才转 `CANCELLED`。取消只影响 QUEUED、运行中、FAILED_RETRYABLE 或 REVIEW_PENDING Item，已 PUBLISHED/DISCARDED/FAILED_FINAL 的结果和 REJECTED 文件证据不回滚；因此一个已有部分发布或拒绝记录的任务仍可最终显示 `CANCELLED` 并保留各计数，绝不能伪装成 `COMPLETED`。`completed_at_ms` 只在 `COMPLETED/CANCELLED/FAILED` 写入，进入或停留在 `PARTIAL_FAILURE` 时必须为空。

每阶段幂等，重试不能重复创建 Blob、Validation、候选、Game 或 ReviewEvent。lease 固定 60 秒、worker 每 15 秒 heartbeat；超过 lease 的运行任务可重新领取。Hasheous 超时/未命中不是 Item failure，仍进入审核并标记“需手动补全”。用户可重试 `FAILED_RETRYABLE`，attempt 用尽或确定性坏输入进入 `FAILED_FINAL`。

重复内容采用两个阶段约束。内容身份固定为“基础 `platform_id` + Item 当前全部 source file 的 `(role, Blob SHA-256, occurrence count)` 规范排序摘要”，不包含上传任务、文件名、逻辑名、ZIP/7z wrapper 名或目标 PlatformInstance；因此同一平台内改名或换 archive wrapper 不能绕过判断，不同基础平台不互相误判。识别阶段在 SourceFile 已闭合、CoreValidation/ReviewDraft/刮削尚未创建时，查询是否已有 `PUBLISHED`（即未软删除）Game 的 current ContentRevision 使用完全相同内容身份：命中则把 Item 原子转为 `DISCARDED`，记录全部已有 Game/当时 ContentRevision 的不可变匹配证据，并把任务计数投影为“已导入并跳过”；不会创建待审核条目或重复游戏。底层 ImportJobFile 仍为可追溯的 `SOURCE`，详情 API 仅将参与该匹配的上传文件投影为 `ALREADY_IMPORTED`。

审核阶段必须重新执行相同判断，以覆盖两个任务在任一任务发布前都完成识别的竞态。普通 Approve 若命中当前未删除 Game，返回 `409 DUPLICATE_GAME_CONFIRMATION_REQUIRED` 及当前完整已有游戏集合，不创建任何发布实体。用户只有在二次确认中提交 `duplicatePolicy=ALLOW_NEW` 和与服务端当前集合完全一致、无重复的 `acknowledgedGameIds`，才能继续发布；集合变化必须再次确认。内容身份 claim 与查询/发布位于同一 SQLite 写事务，单写者下并发首发不能双双越过检查。确认 policy、内容摘要和已确认 Game ID 写入最终 ReviewEvent diff；软删除的 Game 不阻止重新导入。

ImportItem 进入失败态时必须写 `failed_stage=HASHING|IDENTIFYING|SCRAPING`。前两类 Item retry 增加原 IMPORT_ITEM_PIPELINE Job execution并继续使用 ImportJob 创建时冻结的配置；SCRAPING retry 根据同一 provider/config 新建 MetadataScrapeRun/Job，绝不重开或污染旧 Run。领域输入后来变化时由审核过期/重新验证流程处理，不能用 retry 静默改目标目录、DAT、BIOS 或 provider 版本。

Job 交接只有一条实现路径：`IMPORT_ITEM_PIPELINE` 完成 hash、分组后识别、默认 CoreValidation 与派生文件，随后在一个短事务把 Item 转为 SCRAPING，并创建该 Item 的首个 MetadataScrapeRun/`METADATA_SCRAPE` Job；到此 pipeline Job 即 SUCCEEDED，不持有线程轮询另一个 Job。Metadata Job 对每条 eligible evidence 执行有界查询；HIT、MISS、INVALID_RESPONSE，以及用尽内部 attempt 后的 RATE_LIMITED/TIMEOUT/NETWORK_ERROR 都是该 evidence 的持久终态。所有 evidence 已终态（或本来为零）时，Job/Run 为 SUCCEEDED/COMPLETED，provider 错误作为 Warning 留在 Response/Attempt，不把 Item 错误地打成失败。只有数据库/CAS/领域不变量故障或整个 Job execution deadline 导致证据集合无法闭合时，Run/Job 才为 FAILED，并按 retryable 属性把 Item 转为 FAILED_RETRYABLE/FAILED_FINAL。候选创建时同时投递独立 MEDIA_FETCH Job；媒体仍在 PENDING/FETCHING 或单项失败都不阻止 Run 完成和 Item 进入 REVIEW_PENDING，只有 READY 媒体可被采用。

取消投影同样固定：整条 Import 已请求取消时，初始 Metadata Job 停止后 Item 跟随 Import 进入 CANCELLED，不能创建审核草稿；没有父级取消而用户单独取消初始 Metadata Job 时，Run/Job 为 CANCELLED，并以 basename fallback 初始化草稿、把 Item 转 REVIEW_PENDING，允许手工审核或从审核页新建另一次 Run。取消 REVIEW_PENDING Item 的后续重刮削只终止该 Run，不改变现有草稿/Item；取消 Game 重刮削只终止候选批次，不改变发布元信息。MEDIA_FETCH 取消只把对应 Asset/Job 置 CANCELLED。任何一种取消都不复活旧 Job，且已持久化的 Response/Candidate 仍作为证据保留。

首个自动 Run 完成时，负责它的 Metadata Job 在同一短事务创建唯一 ReviewDraft、把 Item 转为 REVIEW_PENDING：READY 的默认 CoreValidation 自动写入 `selected_validation_id`，BLOCKED/INCOMPATIBLE 时该字段为空并展示 blocker；候选按第 7 节固定顺序非空时自动选择第一项并复制其 normalized metadata，候选为空时用 primary content 的安全 basename 去掉最后扩展名作为初始 title，其他展示字段为空。DOS 目录使用 common root 最后一段，DOS ZIP 使用 ZIP basename；结果 trim 后若为空或含控制字符则 title 为空并禁用 Approve，不能写“未命名游戏”后误发布。自动初始化不选择尚未完成的媒体，也不写冒充人工操作的 ReviewEvent；候选/Run/Validation 本身已是不可变来源。审核者之后清除 `selected_candidate_id` 只表示改为人工来源，不自动回滚当前字段；字段变化必须由同一次 PATCH 明示并写 ReviewEvent。后续显式重刮削只新增 Run/Candidate，不自动改现有草稿。

## 5. 文件和目录分组

分组输入是 COMPLETE UploadSession 的全部 UploadFile，先按规范 relative path UTF-8 bytes 升序固定顺序。每个文件都必须落到 `SOURCE/IGNORED/REJECTED` 之一并在任务页可见；不能因扩展名不认识就静默丢弃。一期只对规范 basename 恰为 `.DS_Store`、`Thumbs.db` 或以 `._` 开头的已知系统边车文件使用 `IGNORED_SYSTEM_SIDECAR`；其他不属于下表输入的文件标为 `REJECTED/UNSUPPORTED_CONTENT_FORMAT`，使 ImportJob 进入可见的 PARTIAL_FAILURE，不阻止其他合法 Item 进审核。

- 单 ROM 主机/掌机：每个受支持的 raw ROM、ZIP 或 7z UploadFile 是一个 primary 分组，不按父目录把多个 ROM 误合为一个游戏。ZIP/7z 安全扫描后的唯一平台候选 entry 成为 Item `CONTENT`；raw ROM 自身成为 CONTENT。光盘类 CHD 与 PSP ISO/CSO 只作为 raw 单文件，不接受 archive wrapper。
- Arcade：先用目标 CoreArtifact 锁定的活动 DAT，将每个安全顶层 ZIP 的 basename 精确解析为 machine。只有 `classification=NORMAL` 的 archive 形成自己的 primary Item；`EXPLICIT_BIOS` 和 `ROMOF_INFERENCE` 是依赖 archive，绝不单独发布成 Game。再根据每个 primary Item 的 DAT 闭包，把同 UploadSession 中精确 basename/machine 命中的 parent/BIOS/base ZIP 以 COMPANION 关联。NORMAL parent 可作为多个 Item 的 companion，同时仍可作为自己的 machine Item；不得扫全局 CAS 补依赖。被闭包引用的依赖 archive 为 SOURCE；未被任何 Item 引用的依赖 archive 为 `REJECTED/ARCADE_UNUSED_DEPENDENCY_ARCHIVE`，任务页引导用户改由 BIOS 管理安装或与需要它的 ROMset 同批导入，不创建假游戏。
- MS-DOS：`sourceType=DIRECTORY` 时整个 session 的全部非 sidecar 文件是一个 Item，其 common root 从 relative path 派生；`sourceType=FILES` 时只允许恰一个 ZIP UploadFile。多个独立 ZIP/文件不猜测为一款 DOS 游戏，以 `AMBIGUOUS_DOS_BUNDLE` 拒绝并要求重新选择目录或单 ZIP。目录文件或 ZIP entry 逐项形成 DOS_SOURCE，后端生成确定性运行 bundle 但不改写原 bytes。
- 每个 Item 的 `ImportItemSourceFile` 是 source manifest 与 Approve 复制 GameContentFile 的唯一关系来源；`group_key` 使用数据模型的 canonical digest，重试不得因 worker 遍历顺序改变分组。
- Chrome 目录上传使用 `webkitdirectory` 并只传递 `File.webkitRelativePath`；浏览器不会也不得提交宿主绝对路径。
- 局域网开发允许通过非 localhost 的明文 HTTP 域名访问；该上下文可能只有 `crypto.getRandomValues`，没有 `crypto.randomUUID` 或 `crypto.subtle`。前端必须用 CSPRNG bytes 生成规范小写 UUIDv4，并以经过标准 SHA-256 向量验证的本地实现完成分块 digest fallback；不能降级为 `Math.random`、时间戳、跳过 `Content-Digest` 或把整个文件交给后端代算。
- 一期不提供服务器路径/共享目录导入 API。拖放目录只是 Chrome 增强能力，失败时回退到目录选择器。
- Arcade DAT 发现 machine 依赖 disk/CHD 或 Merged ROMset 时保留文件证据并进入带 `UNSUPPORTED_CHD` / `UNSUPPORTED_MERGED_ROMSET` 的待审核 Blocker；这条只约束 Arcade ROMset，不影响 PSX/Saturn/3DO/PC-FX 明确支持的单文件 CHD。

分组与扩展名规则从目标平台目录的基础平台推导。默认核心是导入流水线唯一自动执行的兼容性目标；一期不得在导入后为其他核心自动投递后台验证。用户在详情页首次显式选择其他核心启动时，才按运行时专题的 `EnsureVariant` 流程按需验证。

可接收格式固定如下；扩展名比较使用 ASCII case-insensitive，ZIP/7z entry 先执行本节与存储文档的路径、数量、展开大小和压缩比检查。ZIP entry 名优先采用标准 UTF-8；仅当 ZIP 明确标记名称为非 UTF-8 且原始字节不是合法 UTF-8 时，允许按 GB18030 严格解码一次，解码结果仍必须通过相同的 UTF-8、路径穿越、控制字符、重复路径和 ASCII casefold 碰撞检查。表外格式、加密/损坏/不安全 archive、nested archive，以及 RAR/TAR、SFX/分卷/加密 7z 在分组前把对应 ImportJobFile 标为 `REJECTED`（稳定 reason 为具体安全码或 `UNSUPPORTED_CONTENT_FORMAT`），不创建无 canonical source 的 ImportItem；Upload/CAS 与文件处置证据仍保留，Job 聚合为 `PARTIAL_FAILURE`。7z 仅用于表中标为“ZIP/7z”的唯一 ROM wrapper；Arcade、DOS、CHD、ISO、CSO 均不接受 7z。`FAILED_FINAL` 只用于已经形成规范 Item 后发现的确定性领域错误。

单 ROM 主机/掌机的 ZIP/7z 在后端物化唯一 primary entry 到 CAS，发布后的 GameContentRevision 以一个 `CONTENT` GameContentFile 指向物化后的原始 entry bytes；原 archive Blob、ArchiveEntry、`archiveFormat` 与两者 hash 继续作为来源/审核证据。运行时不得再次把这类 wrapper archive 交给 EmulatorJS 猜 entry。Arcade ZIP 和 DOS bundle 是有意的多 entry 运行内容，不适用这一物化规则；Arcade 的 `CONTENT` 是 ROMset ZIP，DOS 的每个安全成员/目录文件是带规范相对逻辑名的 `DOS_SOURCE`。

| 基础平台 | 一期输入 | ImportItem 与 primary content 规则 |
| --- | --- | --- |
| NES (`nes`) | 原始 `.nes`、`.unf`、`.unif`；或一个 ZIP/7z | archive 必须恰有一个上述扩展的安全 entry，该 entry 是 `SINGLE_ARCHIVE_MEMBER_V1` hash 与运行内容来源。 |
| Famicom Disk System (`fds`) | 原始 `.fds`；或一个 ZIP/7z | 与 NES 相同，但唯一候选 entry 必须是 `.fds`；启动仍检查 `disksys.rom`。 |
| SNES (`snes`) | 原始 `.sfc`、`.smc`、`.swc`、`.fig`；或一个 ZIP/7z | archive 必须恰有一个支持 entry；不自动拼接多卷或补 copier header。 |
| Game Boy / Color (`gbc`) | 原始 `.gb`、`.gbc`、`.dmg`；或一个 ZIP/7z | archive 必须恰有一个支持 entry。 |
| Game Boy Advance (`gba`) | 原始 `.gba`；或一个 ZIP/7z | archive 必须恰有一个 `.gba` entry。 |
| Nintendo DS (`nds`) | 原始 `.nds`；或一个 ZIP/7z | archive 必须恰有一个 `.nds` entry。 |
| Atari 2600 / 5200 / 7800 | 对应 `.a26/.a52/.a78`；或一个 ZIP/7z | 各目录只接受自己的扩展，唯一成员物化为 raw CONTENT。 |
| Atari Lynx (`lynx`) | 原始 `.lnx`；或一个 ZIP/7z | archive 必须恰有一个 `.lnx` entry。 |
| Mega Drive (`megadrive`) | 原始 `.md`；或一个 ZIP/7z | archive 必须恰有一个 `.md` entry。 |
| PC Engine (`pce`) | 原始 `.pce`；或一个 ZIP/7z | archive 必须恰有一个 `.pce` entry。 |
| Neo Geo Pocket (`ngpc`) | 原始 `.ngp`；或一个 ZIP/7z | archive 必须恰有一个 `.ngp` entry。 |
| Nintendo 64 (`n64`) | 原始 `.z64`；或一个 ZIP/7z | archive 必须恰有一个 `.z64` entry。 |
| PlayStation / Saturn / 3DO / PC-FX | 对应目录中的单个原始 `.chd` | 不展开、不接受 archive wrapper；不支持 CUE/BIN、M3U、多盘或伴随音轨。 |
| PSP (`psp`) | 单个原始 `.iso` 或 `.cso` | 两者均为 `RAW_FILE_V1` CONTENT，直接交给 PPSSPP；服务端不转码，也不接受 `.iso.7z/.cso.7z`。 |
| Arcade (`arcade`) | 一个未加密 `.zip` ROMset archive | 顶层 ZIP 必须精确命中活动 DAT machine；ZIP 本身不是 Hasheous hash 来源。只有 NORMAL machine 是 primary 候选。相同 UploadSession 中经 DAT 闭包明确采用的其他顶层 ZIP 作为该 Item 的 COMPANION parent/BIOS/base；NORMAL parent 也可形成自己的 Item，而 EXPLICIT_BIOS/ROMOF_INFERENCE 只能作为依赖。不能把无关全局 Blob 猜成依赖。 |
| MS-DOS (`dos`) | 一个目录树，或一个未加密 `.zip` | 整棵目录/整个 ZIP 是一项，必须至少有一个安全 `.exe/.com/.bat` entry；目录输入会生成确定性 ZIP。ISO/CUE/IMG/VHD/M3U 和安装介质流程不在一期范围。 |

主机/掌机 ZIP 中零个 primary 候选是 `REJECTED/NO_SUPPORTED_CONTENT`，多个是 `REJECTED/AMBIGUOUS_PRIMARY_CONTENT`；两者都不创建 ImportItem，任务页列出文件和重打包/重新上传入口，不能用文件名打破平局，也不能宣称审核页支持一期不存在的“重新归组”。DOS 按上表是有意的多 entry bundle，不应用唯一 ROM entry 限制，但没有任何安全可执行候选时同样以 `REJECTED/NO_DOS_PROGRAM` 处理。Arcade ZIP 按 machine/DAT 规则识别，不应用主机唯一 entry 限制；未命中 DAT 的 archive 为 `REJECTED/ARCADE_MACHINE_NOT_FOUND`，命中但只是未使用依赖的 archive 使用上述独立 reason。

## 6. 哈希语义

- CAS 去重始终使用原始上传 Blob SHA-256。
- 一期刮削 hash profile 只有三个稳定 code：`RAW_FILE_V1` 对实际文件 bytes 计算 CRC32/MD5/SHA-1/SHA-256；`SINGLE_ARCHIVE_MEMBER_V1` 对安全扫描后唯一被平台规则选中的 ROM member 原始 bytes 计算四种 hash；`ARCADE_DAT_ENTRIES_V1` 使用下述 DAT entry 规则，只保存上游 DAT 真实提供的 CRC32/SHA-1，缺失值为 NULL 而不伪造。`provider=HASHEOUS` 时，profile code、来源 Blob/entry、该 profile 适用的 hash 和 query_order 作为本次 MetadataScrapeRun 的不可变 ContentHashEvidence 持久化；没有 eligible hash 时 evidence 可以为零。`provider=NONE` 只创建明确的 no-op Run/Job，不创建 ContentHashEvidence、QueryAttempt、ProviderResponse 或 Candidate；内容来源与全部实际 hash 仍由 ImportItem source manifest、Blob 和 ArchiveEntry 保留，不会因没有 provider evidence 而丢失。
- 一期不剥 iNES/FDS/SNES copier header、不改 padding/endian、不应用 patch，也不把重新打包后的 ZIP hash 冒充内容 hash。未来增加规范化算法必须使用新 profile code、固定测试向量并重新刮削，不能改变 V1 结果。
- 非 Arcade archive 若存在零个或多个候选 ROM member，则不猜 primary member，按第 5 节生成 Blocker；DOS 目录/bundle 不做 Hasheous 精确 hash 命中声明。
- Arcade 不查询 ZIP 整体 hash，也不拿 parent/BIOS/base archive 的 entry 冒充本游戏。先按活动 DAT 验证 machine，再只从“DAT 直接声明在该 machine、非 `nodump`、且已在 primary CONTENT archive 中以逻辑名和 DAT 提供的全部 hash 精确匹配”的 ROM entry 生成 evidence；来源必须指向该真实 ArchiveEntry，hash 值取实际 bytes 的校验结果，但只发送 DAT 对该 entry 明示的 CRC32/SHA-1 字段。按“有 SHA-1 优先、size 降序、entry name UTF-8 byte 升序”排序，对 `(CRC32,SHA1)` 去重后最多取前 8 项逐条查询。没有符合项时不查询并进入可手工补全审核；不能改查 parent、BIOS 或 ZIP hash。候选以 provider game ID 聚合，记录命中 entry 列表与次数；并列只并列展示，不凭文件名打破。
- DOS 目录和自制压缩包不保证 hash 命中；不得用文件名模糊匹配伪装成精确 hash 命中。

## 7. Hasheous 适配器

~~~go
type ContentHashes struct {
    MD5    string
    SHA1   string
    SHA256 string
    CRC32  string
}

type ProviderOutcome string

type LookupResult struct {
    Outcome   ProviderOutcome // HIT/MISS/RATE_LIMITED/TIMEOUT/INVALID_RESPONSE/NETWORK_ERROR
    Candidate *Candidate      // 仅 HIT 非空；一次 ByHash 最多一个
}

type MetadataProvider interface {
    LookupByHash(ctx context.Context, hashes ContentHashes) (LookupResult, error)
    FetchAsset(ctx context.Context, asset AssetRef) (io.ReadCloser, error)
}
~~~

一期边界：

- 固定调用 `POST https://hasheous.org/api/v1/Lookup/ByHash`；body 使用上游精确字段 `mD5/shA1/shA256/crc`，至少一个非空。Lookup 无需 Retrom 用户注册、登录或 API Key。
- 上游没有一期可依赖的批量 contract；适配器逐 ContentHashes 查询，用内部并发 2、缓存和限流控制吞吐。
- 单次 lookup 总 deadline 15 秒，成功 body 最多 4 MiB；一个 run 最多接受 20 个不同 provider game ID。`200` body 是一个 JSON object 而不是 array，所以单次 QueryAttempt 最多产生一个 provider game ID；`404` 是 MISS，即使 body 是 `text/plain` 也不尝试按 JSON 解析；`429` 是 RATE_LIMITED；请求 deadline 是 TIMEOUT；DNS/TLS/连接/响应读取失败及最终 `500..599` 是可重试 NETWORK_ERROR。最终 `400..499`（除 404/429）以及 redirect 策略结束后仍出现的 `1xx/3xx/其他状态` 是非重试 INVALID_RESPONSE，避免把确定性的请求/协议错误伪装成网络波动；`200` 的 JSON/必需字段/结构不合法同样是 INVALID_RESPONSE。超过 body/结构/candidate 上限把该 response 记为 INVALID_RESPONSE，不截断成看似完整的数据。
- 不调用需要 App API Key 的 MetadataProxy，不使用需要用户 Key 的 Submissions。
- 只发送内容 hash，不上传 ROM、路径、本地文件名或自造的 platform hint。
- 保存独立 scrape run、provider ID、每次原始 response Blob、`fetched_at_ms`、缓存状态、候选聚合命中和采用关系；Arcade 多 entry 命中同一 provider game ID 时保留全部 hit。所有查询收集完成后才按 `(query_order, attempt_no, response.id)` 决定 primary，候选文本和媒体只从该 primary response 归一化；不能由最先返回的并发请求抢占 primary。
- 每个 evidence 的网络重试或缓存复用都创建 MetadataScrapeQueryAttempt；MISS/timeout/429 因没有候选也不能丢失 run→response 关联。请求 body 只含非空 hash，值规范为 lowercase hex（CRC32 恰 8 位，MD5/SHA-1/SHA-256 长度分别 32/40/64）。`request_digest` 固定为 lowercase SHA-256(RFC 8785 canonical `{"provider":"HASHEOUS","endpointContract":"BY_HASH_V1","body":<实际上游 JSON>}`)，因此 cache key 不受 Go map 顺序影响。
- 只接受 lookup attributes 返回的同一 `hasheous.org` `/api/v1/images/<opaque-id>` 图片；每个引用先建立带稳定 ID 的 ScrapeCandidateAsset，再由后端按 HTTP 契约执行 DNS/redirect SSRF 校验、10 MiB/40 MP/图片格式限制后写入 CAS。响应声明必须是受支持的图片类型，实际格式以魔数与完整解码结果为准；上游把 JPEG 错标成 PNG 等受支持图片子类型时允许按真实格式保存，声明为 HTML/SVG/其他非图片或内容无法解码时仍拒绝。单个媒体失败只把该 asset 标为 FAILED，不阻断候选文本或人工审核；只有 READY asset 可被草稿选择和发布。
- run 内按“命中数降序、primary query_order 升序、provider game ID UTF-8 byte 升序”，再按 asset kind/ordinal/ID 排序抓取媒体；所有 candidate asset 的实际响应 bytes 合计上限 100 MiB，触顶后的剩余项标为 `ASSET_RUN_BUDGET_EXCEEDED`。这一上限只控制不可信媒体，不截断已保存的文本候选/raw response。
- 使用查询缓存、并发限制、超时和指数退避。
- 重新刮削针对创建时的 Game current ContentRevision 建立带精确 content FK 的 MetadataScrapeRun、evidence、候选与媒体，不直接覆盖已发布元信息；“最新批次”只在仍等于 Game current content 的 COMPLETED run 中按 `created_at_ms,id` 稳定排序确定，只有显式 apply 才生成 MetadataRevision。

缓存语义固定为：初次 Import 自动查询可以复用尚未过期的合法 `HIT`（7 天）或 `MISS`（24 小时），并写 `source=CACHE` attempt；`RATE_LIMITED/TIMEOUT/INVALID_RESPONSE/NETWORK_ERROR` 只保留证据，不成为可复用 cache current。审核者或已发布 Game 显式点击“重新刮削”始终绕过 cache 发起有界网络请求，成功后更新 cache；这样“重新刮削”不会在 7 天内假装执行却只返回旧结果。测试仍使用 fake transport。

### 7.1 `BY_HASH_V1` 响应映射

实现不得把上游 DTO 直接暴露为 Retrom API。`200` object 的稳定归一化规则如下；未知字段仅留在 raw response Blob，不能自动变成 Game 字段：

| Retrom 字段 | 上游读取顺序 | 规则 |
| --- | --- | --- |
| `provider_game_id` | 顶层 `id` | 必须是正 JSON integer，转十进制字符串；缺失/类型错误使 response 为 INVALID_RESPONSE。 |
| `title` | 顶层 `name`，为空再取 `signature.game.name` | trim 后必须非空；按 Unicode code point 截至 200 并写 normalization warning。 |
| `publisher` | `publisher.name`，为空再取 `signature.game.publisher` | 可空，最多 200 code point。 |
| `description` | `signature.game.description` | 可空，最多 10,000 code point；不自动采用 `attributes.AIDescription`。 |
| `releaseYear` | `signature.game.year` | 只有 ASCII 四位数字且在 `1950..(run.created_at_ms 的 UTC 年+1)` 时转 integer，否则为 null 并告警。 |
| `developer/genre/players` | 无 | 分别固定 `""/""/null`；一期不从异构或 AI-generated Tags 推断。 |
| 平台证据 | `platform.name` | 只写 evidence 并在与目标基础平台不一致时展示 warning；绝不改写 PlatformInstance、hash profile 或 Core 验证。 |
| provider game score | `signature.game.score` | 可空非负 integer，写为 `providerGameScore`，仅作诊断证据，不参与候选排序或自动选择。 |
| provider ROM score | `signature.rom.score` | 可空非负 integer，写为 `providerRomScore`，仅作诊断证据；缺少 `signature.rom` 时为 null。 |

可选字符串先 trim；标题、描述和厂商按 code point 边界截断并写 `FIELD_TRUNCATED:<field>`，控制字符使标题无效、使对应可选字段置空并告警。所有文本按纯文本渲染，不解析上游 Markdown/HTML。`normalized_metadata_json` 精确为 `{"schemaVersion":1,"title":"...","description":"...","developer":"","publisher":"...","genre":"","players":null,"releaseYear":null|1983}`。

`evidence_json` 精确字段为 `schemaVersion/normalizerVersion/normalizationYear/platformName/providerGameScore/providerRomScore/warnings`；其中 `normalizerVersion="HASHEOUS_BY_HASH_V1"`，`normalizationYear` 是 run `created_at_ms` 对应的 UTC 年整数（不是硬编码 2026，也不是重放时当前年份），两个 score 为 null 或非负整数。该固定输入保证同一 raw response 可重复归一化。候选排序仍只使用命中数、primary query order 和 provider ID，不使用上游 score。

图片属性只从 primary response 的 `attributes[]` 读取，并同时要求：`attributeType="ImageId"`、`attributeName` 是支持的槽位名、`attributeRelationType="None"`、`value` 是 1..128 字符的 ASCII opaque ID、`link` 精确等于 `/api/v1/images/` + `value` 且没有 query/fragment。Hasheous 的属性 `value` 是异构字段（例如 `EmbeddedList/Tags` 为 object）；适配器只对 `ImageId` 解码字符串，未知属性保留在 raw evidence，不能因其为非字符串而拒绝整份合法 response。`Logo` 映射 `COVER/ordinal=0`；`Screenshot1..Screenshot4` 映射 `SCREENSHOT/ordinal=0..3`；`BY_HASH_V1` 不生成 BACKGROUND。重复 `(kind,ordinal)` 取 attributes 原数组中第一项并告警，其他/未知 ImageId 仅留 raw evidence、不下载。于是一期单 candidate 最多 1 个 cover 和 4 个 screenshot。

若同一 run 的多个 HIT 使用相同 provider ID，先全部写 Hit，再按前述 primary 规则仅生成一份 Candidate/Asset；primary response 的 `id/name` 合法但可选字段异常时保留候选并告警，而不是用另一次响应暗中拼字段。`metadata[]`、`signatures`、`AIDescription`、`Tags` 和任何上游 source link 均不参与一期归一化。

上游依据：[Lookup ByHash](https://github.com/gaseous-project/hasheous/wiki/API%3A-Lookup-ByHash) 与 [Applications and API](https://github.com/gaseous-project/hasheous/wiki/Applications-and-API)。精确内部映射、网络安全和降级行为见 [HTTP API 契约](./http-api-contract.md)；外部临时字段不能直接成为 Retrom API。

## 8. Arcade 识别

1. 由目标平台目录解析默认 Arcade Core 和锁定 DAT。
2. 按 ZIP basename 和 entry hash 匹配 machine。
3. 解析 clone/parent、BIOS/base archive 和必需 entry。
4. 为审核草稿生成规范 source manifest，并创建默认核心的不可变 ImportItemCoreValidation/ValidationFiles；此时仍不创建已发布 Game、GameContentRevision 或 GameVariant。`ImportItemDosEntry` 只由第 5 节的 MS-DOS 分组/扫描流程生成，Arcade 流程不得创建伪 DOS 程序记录。
5. 独立调用 Hasheous 生成展示候选。

审核页必须分别显示“Core DAT 依赖检查”和“Hasheous 元信息候选”，不能合并为一个置信度或来源。

## 9. 审核

审核字段包括：

- 原文件/目录、相对路径、大小和各类 hash。
- 目标平台目录、基础平台和默认核心快照。
- Arcade Core/DAT、machine、parent、BIOS 和 entry 状态。
- Hasheous 候选、图片和原始响应引用。
- 标题、年份、厂商、类型、玩家数、简介和媒体。
- DOS 可执行程序及默认启动程序。

游戏没有独立默认核心字段；审核者通过调整平台目录改变默认核心语义。

更改目标目录后的处理：

- 同平台、同默认核心且目录 version/config input 未变：可复用同一 READY Validation，更新归属草稿。
- 同平台、不同默认核心或配置 input 已变：后台创建新的 CoreValidation/ValidationFiles，完成前 Approve 禁用。
- 不同基础平台：当前 Item 不允许直接改归属，返回 `422 REIMPORT_REQUIRED_FOR_PLATFORM_CHANGE`。因为这可能改变分组数、source manifest 和 hash profile，一期要求 Discard 后使用正确平台目录创建新 UploadSession/ImportJob，不在单 Item PATCH 中伪装已重新识别。

每次保存草稿都要把所选 Validation 的静态 BIOS snapshot 与当前 Requirement/active Installation 重新比较。完全相同时复用原不可变记录；BIOS 安装、版本、Blob、状态或交付映射变化时，以当前快照创建新的 CoreValidation，并只替换其中的 `BIOS_BUNDLE` 文件引用，旧 Validation/ValidationFiles 保留作历史证据。新结果为 READY 才能写入 `selected_validation_id`；若必需 BIOS 当前缺失，则保存其他草稿字段但清空选择并禁用 Approve。这样后续安装 BIOS 后再次保存即可恢复 READY，不会继续选择已过期证据，也不会为了元信息编辑复制无变化记录。

发布前校验平台目录仍启用且当前 version/default CoreArtifact/DAT/BIOS input 与 ReviewDraft.selectedValidation 完全一致。Approve 事务用已审核的 source manifest 创建 Game、GameContentRevision/GameContentFiles、默认核心 GameVariant/READY VariantRevision、复制 ValidationFiles、MetadataRevision/Asset 和 ReviewEvent，并同时闭合 Game 的 metadata/content current 与 Variant current；任一步失败全部回滚。事务不得读取大 archive、生成 ZIP 或访问网络；Validation 非 READY/过期时返回可修复冲突并投递新验证，不能发布。

审核页允许调整元信息源：`HASHEOUS` 会显式 bypass cache 新建 MetadataScrapeRun/Job，`NONE` 建立无网络的已完成 run；两者都写 `SCRAPE_REQUESTED` ReviewEvent，服务端不会自动覆盖持久化草稿。首次自动刮削已有候选且草稿尚未选择来源时，前端把首个候选基础信息与 READY 封面填入客户端状态，并通过当前 ETag 防抖、串行实时保存。之后显式查询原位等待 Job 终态，并以单个“当前信息 / 最新信息”对比对话框呈现结果：右侧可编辑且可上传人工封面，取消不采用，应用更新客户端状态并触发实时 PATCH；不得把历次候选卡不断追加到页面正文。来源 run/candidate/候选 asset 或人工上传 asset ID 必须完整进入草稿和最终审核事件。

## 10. 审核历史

ReviewEvent 只追加不覆盖，至少包含：

- ImportItem ID、事件类型和 Actor（一期固定 `local`）。
- 输入、输出与字段 diff 快照。
- 目标平台目录和配置快照。
- DAT 依赖证据。
- 采用的 Hasheous candidate/provider/raw response 引用。
- `decision`、可空兼容原因和 `created_at_ms`；当前 UI 不采集发布说明或丢弃原因。

草稿实时保存、改变目标目录、应用/撤销候选、Approve 和 Discard 都写一条事件；纯自动 Worker 进度写 JobEvent，不混入 ReviewEvent。历史页默认按最终 `APPROVED/DISCARDED` 决策列出记录，详情可回放此前草稿事件。

Discard 不立即删除 Blob，历史页可完整还原当时的文件、候选、修改和决策。最终审核事件所属 ImportItem 即使已进入 `PUBLISHED/DISCARDED`，其 READY 候选媒体端点仍允许历史快照读取；前端在物理媒体确实缺失时显示占位，不留下裂图。

### 10.1 管理后台页面职责

游戏入库使用“父级总览 + 四个同级子页”，不能再用一个页面内的 Tab 同时承载导入、任务和历史：

| 页面 | 路由 | 主要问题 | 允许的主要操作 |
| --- | --- | --- | --- |
| 游戏入库总览 | `/admin/imports` | 当前流水线是否健康、哪里需要处理 | 新建导入、跳转任务/待审核/历史 |
| 导入游戏 | `/admin/imports/new` | 本次导入什么、归属哪个平台目录、冻结什么配置 | 选择内容、配置、预检、创建 ImportJob |
| 任务进度 | `/admin/imports/tasks` | ImportJob/ImportItem 运行到哪里、为何失败 | 查看事件、取消任务、重试失败条目 |
| 待审核 | `/admin/reviews`、`/admin/reviews/:itemId` | 候选是否正确、最终发布内容是什么 | 实时编辑、替换封面、Discard、通过并发布 |
| 审核历史 | `/admin/reviews/history` | 当时依据什么作出什么决策 | 筛选、查看不可变快照与字段 diff |

总览的数据是聚合摘要，不复制完整任务管理能力。页首先展示待审核与异常任务两类优先事项，随后用运行中、等待审核、异常和历史完成四项 KPI，以及“上传与校验—识别—运行检查—游戏信息—人工审核—发布”六阶段流水线和最近任务摘要解释当前状态。界面只能组合现有聚合与 ImportJob 计数，不能为不可观测阶段伪造精确进度。

“导入游戏”固定为选择内容、确认配置、上传并验证三步；目标游戏目录没有默认值，用户选择后才显示基础平台和推荐运行方式。上传、终结校验与创建 ImportJob 按顺序执行，成功后进入任务进度，不直接跳入尚未生成内容的待审核队列。任务进度用可筛选的卡片行展示阶段、条目分布、异常和下一步；异常数必须同时包含失败 Item 与尚未解决的 REJECTED 文件，并分别说明“条目失败”和“文件未被接受”，不能把仅含拒绝文件的任务显示为 0 异常。展开区以普通语言说明六阶段和处理路径；REJECTED 文件提供“重新配置并导入”入口，不暴露内部 UUID。已导入并跳过不是异常：完成任务明确展示被跳过文件、`ALREADY_IMPORTED` 原因和已有游戏链接。

“重新配置并导入”只处理原任务中尚未解决的 REJECTED UploadFile。页面读取原任务详情，展示只读文件清单与原平台/元信息源，允许重新选择平台目录后提交；浏览器不恢复或伪造 file input。服务端为这些文件创建新的 COMPLETE UploadSession/UploadFile，逐项引用原 final Blob，并以新配置创建 replacement ImportJob，所以网络不重新上传 bytes、原 session 也不会被二次消费。新任务创建、source file resolution、source 聚合计数和双向任务 lineage 在同一 Import 创建事务提交；失败时 source 仍保持待处理。原 REJECTED reason 永久保留，任务页改显示 replacement 链接且不再把已接管文件计入异常。重新处理仍执行当前归档安全和平台 profile 规则，绝不把 `ARCHIVE_UNSAFE` 当作用户可绕过的门禁。

审核详情页首集中展示条目摘要和审核决定，丢弃/发布与实时保存状态在同一决策卡片；不再展示重复的“可以发布”信息。下方左栏回答“能不能发布”并展示来源文件，右栏回答“发布成什么”并承载实时保存的元信息与封面；窄桌面折叠为单栏。运行检查只呈现文件、兼容和依赖结论，不暴露 DAT/version 等内部技术 ID。若当前内容已关联到未删除游戏，页面常驻展示已有游戏链接；点击发布后用危险确认框明确说明会创建重复游戏，用户选择“仍然发布为新游戏”后才提交确认集合。服务端在点击间出现的新重复项必须返回新集合并再次显示同一确认框。

任务进度只展示 Worker/阶段运行态；待审核只展示未决条目；审核历史只读且按 ReviewEvent 回放。这三个边界可避免“失败任务”“待业务决策”和“已决审计记录”在同一列表中混淆。

待审核不是隐式的“下一条”游标。`/admin/reviews` 展示跨 ImportJob 的分页未决队列，并可按 `importJobId` 收窄到同一批导入；任务页进入审核时必须携带该筛选。用户可以查看各条目的来源、草稿标题、目录、Validation/Blocker、候选和更新时间后任意选择，详情路由保持队列上下文。Approve/Discard 仍是逐 ImportItem、逐 ETag 和逐 Idempotency-Key 的原子决策；一期没有批量决策 endpoint。

## 11. API

上传 manifest、8 MiB 分块、完成/取消、Import 创建/重新配置、SSE resume、If-Match 和 Idempotency-Key 的精确 contract 统一见 [HTTP API、上传与启动凭据契约](./http-api-contract.md)。本领域只增加以下业务约束：ImportJob 只能引用状态为 `COMPLETE` 且不存在任何 `upload_consumptions` 的 UploadSession，创建事务同时写唯一 ImportJob consumption；重新配置只复用 source ImportJob 中尚未 resolution 的 REJECTED files，并携带 source ImportJob 当前 version；Approve/Discard/审核显式重刮削必须携带当前 review version；历史端点只读。

## 12. Worker

默认并发固定为 Hash/Copy 2、Archive 1、DAT 1、Hasheous 2、图片 2、GC 1；配置可调低，调高上限分别为 4/2/1/4/4/1。最多 4 次 attempt，退避 1s/5s/30s/120s；上游 `Retry-After` 可覆盖但最长 15 分钟。任务必须有 lease、heartbeat、可观测阶段、进度、取消、重试和重启恢复；时间由可注入 clock 驱动，测试不 sleep。后台任务不得在哈希、网络或解析期间持有 SQLite 写事务。

## 13. 统一验收入口

本专题统一执行 [一期项目验收规范](./project-acceptance.md) 的 `ACC-IMP-001`–`ACC-IMP-008`；平台目录唯一归属由 `ACC-PLAT-*`、时间与 CAS 约束由 `ACC-DB-*` 和 `ACC-CAS-*` 联合覆盖。流程、通过标准和证据只在统一文档维护。
