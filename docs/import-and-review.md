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
- `ImportItemCoreValidation/ImportItemValidationFile`：发布前针对目标游戏目录默认核心完成的不可变验证结论和派生依赖文件；普通审核通过复制 READY 证据，截图人工放行复制同一当前阻断证据中已实际具备的文件，两者都不在事务中重新扫描/打包。
- `MetadataScrapeRun`：一个 ImportItem（或已发布 Game 的重新刮削）的一次 provider/hash 证据批次；provider=NONE 只保留一次明确的 no-op Run/Job 和审核选择，不创建伪造的 ContentHashEvidence、QueryAttempt、ProviderResponse 或 Candidate。
- `ScrapeCandidate/ScrapeCandidateAsset`：一次 run 中 provider 返回的展示元信息候选和可独立下载/失败的媒体候选。
- `ReviewEvent`：追加式审核事件与前后快照。

所有时间点使用 `_at_ms INTEGER` Unix 毫秒，任务耗时使用 `_duration_ms INTEGER`。

## 3. 创建任务与配置快照

用户必须选择目标 PlatformInstance（UI：“游戏目录”），不能直接选择 Platform。

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
- 普通用户导入不接受服务器路径；管理员可从部署者 `RETROM_SERVER_IMPORT_ROOTS` allowlist 中选择规范相对目录，分别创建 BIOS 或 Pegasus 任务。浏览器永远不能提交或读取任意宿主绝对路径。拖放目录仍只是普通浏览器导入的 Chrome 增强能力。
- Arcade DAT 发现 machine 依赖 disk/CHD 或 Merged ROMset 时保留文件证据并进入带 `UNSUPPORTED_CHD` / `UNSUPPORTED_MERGED_ROMSET` 的待审核 Blocker；这条只约束 Arcade ROMset，不影响 PSX/Saturn/3DO/PC-FX 明确支持的单文件 CHD。

分组与扩展名规则从目标游戏目录的基础平台推导。默认核心是导入流水线唯一自动执行的兼容性目标；一期不得在导入后为其他核心自动投递后台验证。用户在详情页首次显式选择其他核心启动时，才按运行时专题的 `EnsureVariant` 流程按需验证。

可接收格式固定如下；这里列出 Retrom 已验证并承诺接收的产品子集，不直接照搬某个核心声明的全部 `valid_extensions`。扩展名比较使用 ASCII case-insensitive，ZIP/7z entry 先执行本节与存储文档的路径、数量、展开大小和压缩比检查。ZIP entry 名优先采用标准 UTF-8；仅当 ZIP 明确标记名称为非 UTF-8 且原始字节不是合法 UTF-8 时，允许按 GB18030 严格解码一次，解码结果仍必须通过相同的 UTF-8、路径穿越、控制字符、重复路径和 ASCII casefold 碰撞检查。表外格式、加密/损坏/不安全 archive，以及 RAR/TAR、SFX/分卷/加密 7z 在分组前标为 `REJECTED`。普通 ROM wrapper 的 nested archive 仍拒绝；DOS ZIP 内的 archive 只作为不透明游戏文件保留且绝不递归展开，因此允许存在。压缩比门禁对超过 16 MiB 的单成员生效，小型空白存档即使高度可压缩也仍受总展开量和成员数上限约束。7z 仅用于表中标为“ZIP/7z”的唯一 ROM wrapper；Arcade、DOS、CHD、ISO、CSO、3DS、CCI 均不接受 7z。

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
| Virtual Boy (`virtualboy`) | 原始 `.vb`；或一个 ZIP/7z | archive 必须恰有一个 `.vb` entry。 |
| WonderSwan / Color (`wonderswan`) | 原始 `.ws`、`.wsc`；或一个 ZIP/7z | archive 必须恰有一个支持 entry。 |
| Master System (`mastersystem`) | 原始 `.sms`；或一个 ZIP/7z | archive 必须恰有一个 `.sms` entry；本期不借用 SMS Plus 的其他平台格式扩展产品范围。 |
| PlayStation / 3DO / PC-FX | 对应目录中的单个原始 `.chd` | 不展开、不接受 archive wrapper；不支持 CUE/BIN、M3U、多盘或伴随音轨。 |
| Saturn | STANDARD 为单个原始 `.chd`；显式 `MULTI_DISC_M3U_V1` 为同目录一个 M3U 与其引用的 2–8 个 CHD | 多盘仅对 capability 返回支持的 yabause artifact 开放；不接受跨目录引用、archive wrapper、CUE/BIN 或非 CHD entry。 |
| PSP (`psp`) | 单个原始 `.iso` 或 `.cso` | 两者均为 `RAW_FILE_V1` CONTENT，直接交给 PPSSPP；服务端不转码，也不接受 `.iso.7z/.cso.7z`。 |
| Nintendo 3DS (`nintendo3ds`) | 单个原始 `.3ds` 或 `.cci` | 作为 `RAW_FILE_V1` CONTENT 直接交给 Azahar；本期不接受 archive wrapper 或其他 3DS 容器/可执行格式。 |
| Arcade (`arcade`) | 一个未加密 `.zip` ROMset archive | 顶层 ZIP 必须精确命中活动 DAT machine；ZIP 本身不是 Hasheous hash 来源。只有 NORMAL machine 是 primary 候选。相同 UploadSession 中经 DAT 闭包明确采用的其他顶层 ZIP 作为该 Item 的 COMPANION parent/BIOS/base；NORMAL parent 也可形成自己的 Item，而 EXPLICIT_BIOS/ROMOF_INFERENCE 只能作为依赖。不能把无关全局 Blob 猜成依赖。 |
| MS-DOS (`dos`) | 一个目录树，或一个未加密 `.zip` | 整棵目录/整个 ZIP 是一项，必须至少有一个 `.exe/.com/.bat` entry，全部候选均保留。`game/go/launch/play/run/start` 优先，setup/install/config/uninstall/readme/驱动/解包工具降权，再按扩展名、深度和路径稳定排序；这只决定审核默认值。目录输入会生成确定性 ZIP。ISO/CUE/IMG/VHD/M3U 和安装介质流程不在一期范围。 |

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
| `description` | `signature.game.description`，为空再取首个 `attributeName="AIDescription"`、`attributeType="LongString"`、`attributeRelationType="None"` 的字符串值 | 可空，最多 10,000 code point；CRLF/CR 统一为 LF，允许 LF 与制表符并拒绝其他控制字符；采用 fallback 时写 `FIELD_FALLBACK:description:AIDescription`，不从 Tags 推断。 |
| `releaseYear` | `signature.game.year` | 只有 ASCII 四位数字且在 `1950..(run.created_at_ms 的 UTC 年+1)` 时转 integer，否则为 null 并告警。 |
| `developer/genre/players` | 无 | 分别固定 `""/""/null`；一期不从异构或 AI-generated Tags 推断。 |
| 平台证据 | `platform.name` | 只写 evidence 并在与目标基础平台不一致时展示 warning；绝不改写 PlatformInstance、hash profile 或 Core 验证。 |
| provider game score | `signature.game.score` | 可空非负 integer，写为 `providerGameScore`，仅作诊断证据，不参与候选排序或自动选择。 |
| provider ROM score | `signature.rom.score` | 可空非负 integer，写为 `providerRomScore`，仅作诊断证据；缺少 `signature.rom` 时为 null。 |

可选字符串先 trim；标题、描述和厂商按 code point 边界截断并写 `FIELD_TRUNCATED:<field>`，控制字符使标题无效、使对应可选字段置空并告警。所有文本按纯文本渲染，不解析上游 Markdown/HTML。`normalized_metadata_json` 精确为 `{"schemaVersion":1,"title":"...","description":"...","developer":"","publisher":"...","genre":"","players":null,"releaseYear":null|1983}`。

`evidence_json` 精确字段为 `schemaVersion/normalizerVersion/normalizationYear/platformName/providerGameScore/providerRomScore/warnings`；其中 `normalizerVersion="HASHEOUS_BY_HASH_V1"`，`normalizationYear` 是 run `created_at_ms` 对应的 UTC 年整数（不是硬编码 2026，也不是重放时当前年份），两个 score 为 null 或非负整数。该固定输入保证同一 raw response 可重复归一化。候选排序仍只使用命中数、primary query order 和 provider ID，不使用上游 score。

图片属性只从 primary response 的 `attributes[]` 读取，并同时要求：`attributeType="ImageId"`、`attributeName` 是支持的槽位名、`attributeRelationType="None"`、`value` 是 1..128 字符的 ASCII opaque ID、`link` 精确等于 `/api/v1/images/` + `value` 且没有 query/fragment。Hasheous 的属性 `value` 是异构字段（例如 `EmbeddedList/Tags` 为 object）；适配器只对 `ImageId` 解码字符串，未知属性保留在 raw evidence，不能因其为非字符串而拒绝整份合法 response。`Logo` 映射 `COVER/ordinal=0`；`Screenshot1..Screenshot4` 映射 `SCREENSHOT/ordinal=0..3`；`BY_HASH_V1` 不生成 BACKGROUND。重复 `(kind,ordinal)` 取 attributes 原数组中第一项并告警，其他/未知 ImageId 仅留 raw evidence、不下载。于是一期单 candidate 最多 1 个 cover 和 4 个 screenshot。

若同一 run 的多个 HIT 使用相同 provider ID，先全部写 Hit，再按前述 primary 规则仅生成一份 Candidate/Asset；primary response 的 `id/name` 合法但可选字段异常时保留候选并告警，而不是用另一次响应暗中拼字段。`metadata[]`、`signatures`、`Tags` 和任何上游 source link 均不参与一期归一化；`AIDescription` 只允许按上表作为空 description 的显式 fallback。

上游依据：[Lookup ByHash](https://github.com/gaseous-project/hasheous/wiki/API%3A-Lookup-ByHash) 与 [Applications and API](https://github.com/gaseous-project/hasheous/wiki/Applications-and-API)。精确内部映射、网络安全和降级行为见 [HTTP API 契约](./http-api-contract.md)；外部临时字段不能直接成为 Retrom API。

## 8. Arcade 识别

1. 由目标游戏目录解析默认 Arcade Core 和锁定 DAT。
2. 按 ZIP basename 和 entry hash 匹配 machine。
3. 解析 clone/parent、BIOS/base archive 和必需 entry。
4. 为审核草稿生成规范 source manifest，并创建默认核心的不可变 ImportItemCoreValidation/ValidationFiles；此时仍不创建已发布 Game、GameContentRevision 或 GameVariant。`ImportItemDosEntry` 只由第 5 节的 MS-DOS 分组/扫描流程生成，Arcade 流程不得创建伪 DOS 程序记录。
5. 独立调用 Hasheous 生成展示候选。

审核页必须分别显示“Core DAT 依赖检查”和“Hasheous 元信息候选”，不能合并为一个置信度或来源。

## 9. 审核

审核字段包括：

- 原文件/目录、相对路径、大小和各类 hash。
- 目标游戏目录、基础平台和默认核心快照。
- Arcade Core/DAT、machine、parent、BIOS 和 entry 状态。
- Hasheous 候选、图片和原始响应引用。
- 标题、年份、厂商、类型、玩家数、简介和媒体。
- DOS 可执行程序及默认启动程序。

游戏没有独立默认核心字段；审核者通过调整游戏目录改变默认核心语义。

更改目标目录后的处理：

- 同平台、同默认核心且目录 version/config input 未变：可复用同一 READY Validation，更新归属草稿。
- 同平台、不同默认核心或配置 input 已变：后台创建新的 CoreValidation/ValidationFiles，完成前 Approve 禁用。
- 不同基础平台：当前 Item 不允许直接改归属，返回 `422 REIMPORT_REQUIRED_FOR_PLATFORM_CHANGE`。因为这可能改变分组数、source manifest 和 hash profile，一期要求 Discard 后使用正确游戏目录创建新 UploadSession/ImportJob，不在单 Item PATCH 中伪装已重新识别。

每次保存草稿都要把所选 Validation 的静态 BIOS snapshot 与当前 Requirement/active Installation 重新比较。完全相同时复用原不可变记录；BIOS 安装、版本、Blob、状态或交付映射变化时，以当前快照创建新的 CoreValidation，并只替换其中的 `BIOS_BUNDLE` 文件引用，旧 Validation/ValidationFiles 保留作历史证据。新结果为 READY 才能写入 `selected_validation_id`；若必需 BIOS 当前缺失，则保存其他草稿字段但清空选择并禁用 Approve。这样后续安装 BIOS 后再次保存即可恢复 READY，不会继续选择已过期证据，也不会为了元信息编辑复制无变化记录。

发布前校验游戏目录仍启用且当前 version/default CoreArtifact/DAT/BIOS input 与 ReviewDraft.selectedValidation 完全一致。Approve 事务用已审核的 source manifest 创建 Game、GameContentRevision/GameContentFiles、默认核心 GameVariant/READY VariantRevision、复制 ValidationFiles、MetadataRevision/Asset 和 ReviewEvent，并同时闭合 Game 的 metadata/content current 与 Variant current；任一步失败全部回滚。事务不得读取大 archive、生成 ZIP 或访问网络；Validation 非 READY/过期时返回可修复冲突并投递新验证，不能发布。

### 9.1 Arcade Parent 补充与来源快照

每个 ImportItem 的初始来源在识别完成时固化为 revision 1 的 `ImportItemSourceSnapshot`；旧 `ImportItemSourceFile` 只作为初始导入证据，不再被审核补传原地改写。ReviewDraft 的 `effective_source_snapshot_id` 指向当前有效快照，CoreValidation 同时绑定 `source_snapshot_id`。补充 Parent、保存草稿、重复检查和 Approve 必须使用这一有效快照；READY Validation 只有与草稿同 Item、同有效快照时才能被选择。

Arcade 审核允许对当前 V2 依赖闭包中状态为 `MISSING/MISMATCH` 的 `PARENT` 节点补充一个 ZIP。浏览器先走通用单文件分块上传并等待 UploadFile COMPLETE，再创建 `REVIEW_ARCADE_PARENT_VALIDATE` Job。Attachment 状态机固定为：

```text
QUEUED -> RUNNING -> ACCEPTED
                  -> REJECTED
                  -> FAILED_RETRYABLE -> RUNNING
QUEUED/RUNNING/FAILED_RETRYABLE -> CANCELLED
```

同一 Item 同时最多一个 `QUEUED/RUNNING` Attachment。Worker 在事务外执行安全 ZIP 扫描，只以根级 regular-file entry 对锁定 DAT 做 name/size/CRC/SHA-1 严格匹配并完整重跑 Arcade validator；浏览器文件名不参与 machine 判定。ZIP 可以同时保留不冲突的安全子目录 clone 文件作为原始归档证据，但这些子目录 entry 只计入 diagnostics，不能满足 Parent DAT、替代缺失根 entry 或让 Merged 主 ROMset 获得支持；真正的嵌套 archive、路径穿越、碰撞和统一 ArchiveLimits 超限仍拒绝。接受时在一个短事务内追加后继来源快照、Validation/ValidationFiles、UploadConsumption、ReviewEvent/JobEvent，更新草稿有效快照并递增版本；闭包仍缺其他 Parent 时 Attachment 仍为 ACCEPTED，但 Validation 保持 BLOCKED 且草稿不选择 Validation。拒绝或可重试失败不产生后继快照，旧有效快照不变。

审核者可按 `a -> b -> c` 分步补齐：接受 b 后重新投影完整闭包并继续展示 c；全部依赖与 BIOS 满足后才自动选择 READY Validation。Merged、CHD、关系环、DAT/config/source 漂移不允许通过补传降级放行。Discard 会请求取消 active Job；离开页面不会取消，返回时由 Review GET 与 Job SSE 恢复。补传推进 Review version 是来源快照切换的并发保护，不能因此永久阻断发布：客户端在 Attachment 终态重新读取当前 Review；若发布仍因遗漏的系统版本推进返回 stale，只能在完整发布草稿等价、最新 Validation 可发布且没有 active Attachment 时用新 ETag 有界重试一次，任何人工字段并发变化仍必须停止。补传审计事件只保存 Attachment/Job/Validation/快照 ID、machine、原文件名、observed hash/size、状态和稳定错误码，不保存 ROM bytes 或宿主绝对路径。

Parent 改变有效 source manifest 和 content identity。每次接受后重新计算重复内容证据；进入人工审核的 Item 即使命中已发布游戏也不自动丢弃，Approve 继续以新 digest 做事务内最终重复检查并要求显式确认。发布时 ContentFiles 来自有效来源快照，VariantFiles 的 PARENT/BIOS 来自同快照 READY Validation，不得沿用 child-only 证据。若 Item 由 Pegasus 目录交接，最终 metadata/content 的来源仍记录为 `SERVER_PEGASUS_IMPORT` 并引用 Pegasus Item，但数据库必须沿一一关联的 `library_import_item_id -> ReviewDraft.effective_source_snapshot_id` 校验最终 content manifest；Pegasus 初始 manifest 只作为原始来源证据，不能阻断合法的 Parent 或多盘后继快照。

审核页允许调整元信息源：`HASHEOUS` 会显式 bypass cache 新建 MetadataScrapeRun/Job，`NONE` 建立无网络的已完成 run；两者都写 `SCRAPE_REQUESTED` ReviewEvent，服务端不会自动覆盖持久化草稿。首次自动刮削已有候选且草稿尚未选择来源时，前端把首个候选基础信息与 READY 封面填入客户端状态，并通过当前 ETag 防抖、串行实时保存。之后显式查询原位等待 Job 终态，并以单个“当前信息 / 最新信息”左右两栏对比对话框呈现结果；每栏内部上方为短元信息与 3:4 封面，下方为完整简介。右栏可编辑且可上传人工封面，取消不采用，应用更新客户端状态并触发实时 PATCH；不得把历次候选卡不断追加到页面正文。来源 run/candidate/候选 asset 或人工上传 asset ID 必须完整进入草稿和最终审核事件。

## 10. 审核历史

ReviewEvent 只追加不覆盖，至少包含：

- ImportItem ID、事件类型和真实 `USER` actor；后台/离线系统事件使用封闭 `SYSTEM` actor label。
- 输入、输出与字段 diff 快照。
- 目标游戏目录和配置快照。
- DAT 依赖证据。
- 采用的 Hasheous candidate/provider/raw response 引用。
- `decision`、可空兼容原因和 `created_at_ms`；当前 UI 不采集发布说明或丢弃原因。

草稿实时保存、改变目标目录、应用/撤销候选、Approve 和 Discard 都写一条事件；纯自动 Worker 进度写 JobEvent，不混入 ReviewEvent。历史页默认按最终 `APPROVED/DISCARDED` 决策列出记录，详情可回放此前草稿事件。

Discard 不立即删除 Blob，历史页可完整还原当时的文件、候选、修改和决策。最终审核事件所属 ImportItem 即使已进入 `PUBLISHED/DISCARDED`，其 READY 候选媒体端点仍允许历史快照读取；前端在物理媒体确实缺失时显示占位，不留下裂图。

### 10.1 管理后台页面职责

游戏入库使用“父级总览 + 五个同级子页”，不能再用一个页面内的 Tab 同时承载导入、任务和历史：

| 页面 | 路由 | 主要问题 | 允许的主要操作 |
| --- | --- | --- | --- |
| 游戏入库总览 | `/admin/imports` | 当前流水线是否健康、哪里需要处理 | 新建导入、跳转任务/待审核/历史 |
| 导入游戏 | `/admin/imports/new` | 本次导入什么、归属哪个游戏目录、冻结什么配置 | 选择内容、配置、预检、创建 ImportJob |
| 本地扫描 | `/admin/imports/server` | 配置的只读服务端目录中有哪些 BIOS 或 Pegasus 游戏 | 浏览允许目录、创建服务器 BIOS/Pegasus 导入、映射与查看结果 |
| 任务进度 | `/admin/imports/tasks` | ImportJob/ImportItem 运行到哪里、为何失败 | 查看事件、取消任务、重试失败条目 |
| 待审核 | `/admin/reviews`、`/admin/reviews/:itemId` | 候选是否正确、最终发布内容是什么 | 预览/启动严格 READY 快速审批；实时编辑、替换封面、Discard、逐项通过并发布 |
| 审核历史 | `/admin/reviews/history` | 当时依据什么作出什么决策 | 筛选、查看不可变快照与字段 diff |

总览的数据是聚合摘要，不复制完整任务管理能力。页首先展示待审核与异常任务两类优先事项，随后用进行中批次、等待审核、异常条目和历史完成批次四项 KPI，以及“上传与校验—识别—运行检查—游戏信息—人工审核—发布”六阶段流水线和最近任务摘要解释当前状态。所有批次数按用户发起的顶层操作统计：浏览器上传/重新配置产生的 ImportJob 各计一次，PegasusImport 各计一次；Pegasus 为复用普通审核管线而逐游戏创建的 ImportJob 是内部交接记录，不得出现在普通任务列表、最近任务或批次 KPI 中。处理中条目使用非终态顶层批次当前已知的普通 `total_item_count` 或 Pegasus `game_count`，异常条目使用普通失败 Item 加未解决 REJECTED 文件、或 Pegasus blocked/failed Item；不得从“最近三条任务”反推任何流水线统计。“待审核条目/等待审核/人工审核”三处必须共用实际 `state=REVIEW_PENDING` 的 ImportItem 总数，“发布”使用实际 `state=PUBLISHED` 的 ImportItem 总数，不能把含待审核内容的任务数、任务缓存计数或已经 DISCARDED 的 Item 计入。界面不能为不可观测阶段伪造精确进度。

“导入游戏”固定为选择内容、确认配置、上传并验证三步；目标游戏目录没有默认值，用户选择后才显示基础平台和推荐运行方式。上传、终结校验与创建 ImportJob 按顺序执行，成功后进入任务进度，不直接跳入尚未生成内容的待审核队列。任务进度只列浏览器上传/重新配置产生的普通 ImportJob；Pegasus 批次在“本地扫描”按一个顶层计划展示，其内部逐游戏 ImportJob 仅供详情诊断。普通任务进度按更新时间游标分页，每页最多 20 条；滚动到已加载列表末尾再取下一页。只对已加载且仍为 `QUEUED/RUNNING/CANCEL_REQUESTED` 的任务每秒读取一次详情，同页多批任务并行更新，全部进入终态后停止；尚未加载的运行任务不轮询。任务卡展示阶段、条目分布、异常和下一步；异常数必须同时包含失败 Item 与尚未解决的 REJECTED 文件，并分别说明“条目失败”和“文件未被接受”，不能把仅含拒绝文件的任务显示为 0 异常。任务同时存在待审核条目和拒绝文件时，“审核 N 个条目”与进度条下可点击的“N 异常”必须同时可见；异常数为零时只显示文本，不提供无效操作。展开区以普通语言说明六阶段和处理路径；每个稳定 reason code 可聚焦并以 tooltip 展示具体含义，REJECTED 文件提供“重新配置并导入”入口，不暴露内部 UUID。已导入并跳过不是异常：完成任务明确展示被跳过文件、`ALREADY_IMPORTED` 原因和已有游戏链接。

“重新配置并导入”只处理 STANDARD 原任务中尚未解决的 REJECTED UploadFile。页面读取原任务详情，展示只读文件清单与原平台/元信息源，允许重新选择游戏目录后提交；浏览器不恢复或伪造 file input。服务端为这些文件创建新的 COMPLETE UploadSession/UploadFile，逐项引用原 final Blob，并以新配置创建 replacement ImportJob，所以网络不重新上传 bytes、原 session 也不会被二次消费。新任务创建、source file resolution、source 聚合计数和双向任务 lineage 在同一 Import 创建事务提交；失败时 source 仍保持待处理。原 REJECTED reason 永久保留，任务页改显示 replacement 链接且不再把已接管文件计入异常。重新处理仍执行当前归档安全和平台 profile 规则，绝不把 `ARCHIVE_UNSAFE` 当作用户可绕过的门禁。MULTI 的拒绝目录必须重新选择完整 DIRECTORY 并重新预检，不能把 M3U 或 CHD 子集送入此复用流程。

审核详情页首集中展示条目摘要和审核决定，四个等宽操作按“重新运行检查 / 运行游戏、丢弃条目 / 通过并发布”两行排列，实时保存状态位于同一决策卡片；不再展示重复的“可以发布”信息。下方左栏回答“能不能发布”并展示来源文件，右栏回答“发布成什么”并承载实时保存的元信息与封面；窄桌面折叠为单栏。运行检查必须把稳定 compatibility code 映射为可操作说明：缺必需 BIOS 时列出逻辑文件名并链接到完整 BIOS 目录，DAT 缺失时链接到街机数据目录；修复后可点击“重新运行检查”，该操作只冲刷当前草稿、刷新服务端 Validation 和页面发布状态，不得打开游戏子窗体或创建审核 Preview。READY 结果必须在原页面立即启用发布，不得要求刷新页面、只显示“需要检查”或要求重新导入。依赖计数必须包含 BIOS，不能在存在缺失 BIOS 时显示“没有发现异常”。未知 code 至少展示原 code，不能吞掉原因。若当前内容已关联到未删除游戏，页面常驻展示已有游戏链接；点击发布后用危险确认框明确说明会创建重复游戏，用户选择“仍然发布为新游戏”后才提交确认集合。服务端在点击间出现的新重复项必须返回新集合并再次显示同一确认框。

“运行游戏”在同步冲刷草稿后立即打开独立子窗体，并由服务端冻结审核专用运行快照。冲刷只提交尚未保存的客户端变更；客户端草稿与已读取的服务端快照完全相同时，不得为创建预览强制发送无变化 PATCH。主 ROM 始终交付；当前 Validation 已具备的 Parent、BIOS 和 external file 一并交付，缺失依赖直接省略，尽最大可能让核心启动。DAT 驱动的 Arcade Validation 只从其不可变 ValidationFiles 装配 Parent/BIOS，不得把 V2 DAT dependency snapshot 交给普通平台的 V1 BIOS snapshot parser；非 DAT Validation 才按 V1 snapshot 加载外部 BIOS 文件。该预览不创建 Game/正式 Launch/PlaySession，也不提供状态或持久存档。无论 Validation 为 READY 还是阻断，子窗体收到当前平台默认核心的真实 `EJS_onGameStart` 后都启动 5,000ms 计时，随后保存 PNG 并通知审核页刷新；截图展示在页首条目摘要最右侧。当前截图是管理员已观察到核心真实启动的人工放行证据，可令 `canApprove=true`；发布事务必须再次证明截图、Validation、来源快照、目标平台和 CoreArtifact 一致，并将 `REVIEW_SCREENSHOT_OVERRIDE` 与截图 ID 写入 Variant/审核事件。正式单机启动沿用审核时的最佳努力依赖集合，不能发布后又因同一缺失依赖硬阻断；Netplay 不继承该放行。点击“重新运行检查”得到新 Validation 后，旧截图随之失效；如需人工放行必须再次点击“运行游戏”取得新截图。浏览器阻止弹窗、核心未触发游戏开始、自动播放失败或截图上传失败时不能伪造证据，页面必须明确提示管理员重试。

任务进度只展示 Worker/阶段运行态；待审核只展示未决条目；审核历史只读且按 ReviewEvent 回放。这三个边界可避免“失败任务”“待业务决策”和“已决审计记录”在同一列表中混淆。

待审核不是隐式的“下一条”游标。`/admin/reviews` 展示跨 ImportJob 的分页未决队列，每页最多 20 条并在滚动到底部后继续取页；可按 `importJobId` 收窄到同一批导入，任务页进入审核时必须携带该筛选。“当前已加载 / 可以发布 / 运行异常 / 未找到信息”是对已加载集合的真实即时筛选按钮，数量与筛选结果同步更新而不是装饰统计。用户可以查看各条目的来源、草稿标题、目录、Validation/Blocker、候选和更新时间后任意选择，详情路由保持队列上下文。普通 Approve/Discard 仍是逐 ImportItem、逐 ETag 和逐 Idempotency-Key 的原子决策；快速审批只在服务端枚举当前 URL 中的 `q/tagId/importJobId/pegasusImportId/platformInstanceId/blockerCode` 全范围，不使用 sort、cursor 或浏览器已加载集合。

“快速审批”先打开影响预览，明确 matched、严格 READY candidate，以及阻断截图放行、重复内容、活动 Parent/多盘补传和其他不 READY/已过期排除数。只有严格 `READY`、当前 generation/来源/目录/CoreArtifact/active DAT/BIOS/DOS entry/dependency snapshot 均一致、标题合法、没有重复内容和活动 Attachment 的 Item 才能进入 candidate；仅靠第 5 秒阻断截图启用逐项按钮的条目永不自动发布。dependency snapshot 必须按内容类型进入同一普通发布校验分支：静态 BIOS/多盘使用 `corevalidation` schema v1，Arcade 使用含 machine/DAT/closure/dependencies 的 schema v2，并重新核对当前 active DAT 的闭包、required entries 与冻结 ValidationFile；不得把合法 Arcade v2 送入 BIOS v1 解析器后误计为 stale。预览返回 scope digest 与候选 manifest digest；确认创建时服务端在一个事务重新枚举，任何筛选或 Item 输入漂移都以 `REVIEW_BULK_PREVIEW_STALE` 要求重新确认。空范围拒绝启动，单批上限 10,000，全实例同时只允许一个 active batch。

后台按冻结顺序逐项复用普通 Approve 事务。成功 Item 的 Game/Revision/ReviewEvent、普通/Pegasus 聚合与批次 `PUBLISHED` 结果必须同事务提交；ReviewEvent diff 增加 `approvalMode=QUICK_STRICT_READY` 和 `bulkApprovalId`，不建立第二套发布规则。处理前重复变为 `SKIPPED_DUPLICATE`，版本/Validation/来源漂移为 `SKIPPED_CHANGED`，严格门禁不再满足为 `SKIPPED_NOT_READY`，意外项故障为 `FAILED_FINAL` 并继续剩余项。取消只收口尚未提交的 Item；进程重启恢复未提交项，restore 使遗留批次以 `RESTORE_INTERRUPTED` 失败且不回滚已发布 Game。终态页面清除相关审核队列缓存、刷新列表，并提供逐项结果链接。

## 11. API

上传 manifest、8 MiB 分块、完成/取消、Import 创建/重新配置、Parent Attachment、SSE resume、If-Match 和 Idempotency-Key 的精确 contract 统一见 [HTTP API、上传与启动凭据契约](./http-api-contract.md)。本领域只增加以下业务约束：ImportJob 只能引用状态为 `COMPLETE` 且不存在任何 `upload_consumptions` 的 UploadSession，创建事务同时写唯一 ImportJob consumption；重新配置只复用 source ImportJob 中尚未 resolution 的 REJECTED files，并携带 source ImportJob 当前 version；Parent Attachment 只可引用单个 COMPLETE `.zip` UploadFile 且不消费整个 UploadSession；Approve/Discard/审核显式重刮削必须携带当前 review version；历史端点只读。

## 12. Worker

默认并发固定为 Hash/Copy 2、Archive 1、DAT 1、Hasheous 2、图片 2、GC 1；配置可调低，调高上限分别为 4/2/1/4/4/1。最多 4 次 attempt，退避 1s/5s/30s/120s；上游 `Retry-After` 可覆盖但最长 15 分钟。任务必须有 lease、heartbeat、可观测阶段、进度、取消、重试和重启恢复；时间由可注入 clock 驱动，测试不 sleep。后台任务不得在哈希、网络或解析期间持有 SQLite 写事务。

## 13. 多盘目录、缺盘与补传

Import create 的 `contentMode` 缺省严格等价于 `STANDARD`；新 Web 对两种模式都显式发送。MULTI 只接受 `sourceType=DIRECTORY`，以每个 M3U 的直接父目录分组：同目录必须恰有一个 M3U，引用只允许安全 CHD basename，按精确 UTF-8 后唯一 ASCII case-fold 匹配，整组限制 2–8 盘和 1 GiB。不同子目录可在同一 UploadSession 形成多个 Item；未引用文件记录为 `IGNORED/NOT_REFERENCED_BY_PLAYLIST`。局部非法目录产生稳定 rejected outcome，不阻断其他合法目录。

导入任务列表必须标明冻结的 MULTI 模式；详情按 Item 显示 playlist、总盘数、已找到/缺失盘数和最多 20 个未引用 basename，并保留未截断计数。即使任务已经进入待审核且没有异常，详情入口仍可用。任务详情不得返回 Blob ID 或宿主路径。

合法缺盘组仍创建 `REVIEW_PENDING/BLOCKED` Item；缺失 entry 没有 Blob。管理员必须通过 `/api/v1/admin/reviews/{id}/multi-disc-attachments` 一次上传当前全部缺盘，Attachment 以请求 User 为 actor、冻结 base snapshot/limits/capability 并异步校验精确 basename 集合、CHD 头和总量。审核页常驻展示 playlist 证据、规范盘序、来源/规范文件名、大小/hash 与冻结上限；缺盘时明确阻止发布，并通过“上传全部缺失光盘”抽屉逐项核对。抽屉只在集合无遗漏、无多余、无 ASCII case-fold 重名且总量未超限时允许提交，支持 Esc、焦点圈定与关闭后焦点恢复；版本冲突时刷新审核证据但保留本地选择。进入异步校验后抽屉关闭，卡片展示 Job 和进度；REJECTED 允许重新选文件，FAILED_RETRYABLE 只走通用 Job retry。接受后追加不可变 SourceSnapshot 与 generation 4 validation，旧快照不改，并以“缺失光盘已补齐，正在更新审核结果”通知；拒绝或 retryable failure 不推进 effective snapshot。reconfigure 继续只处理 STANDARD rejected-only 文件，不能用它拼多盘目录。

发布要求完整盘组、当前 generation 4 READY validation、artifact version/compatibility/content capability 未漂移。发布保留来源 M3U 作为证据，但 GameContentRevision 的 DISC 使用规范顺序，VariantFile 另锁定服务端 canonical playlist。统一验收见 `ACC-MDISC-001`–`003`、`007`–`008`。

## 14. Pegasus 服务器目录导入

Pegasus source 以每个 `metadata.pegasus.txt` 中的 segment 为独立 Collection；解析器只保存允许的纯文本字段和相对文件引用，忽略 `launch`、`command`、`logo` 与未知规则，不执行或持久化命令 payload。扫描只读取 metadata、目录项 facts、大小和受限媒体头，不读取完整 ROM、不写业务 Blob、不创建 Game；结果冻结 metadata digest、确定性 source key、可处理/阻断计数、媒体候选和 `estimatedSourceBytes` 上限。

每个 Collection 必须由管理员明确 `IMPORT + enabled PlatformInstance` 或 `SKIP`，没有默认映射。start 在创建 import execution 前重新验证全部 metadata digest；映射冻结后，Worker 在数据库事务外经 no-follow fd 读取源文件并写 CAS，再复用普通导入的 content profile、archive、M3U、DAT/Arcade companion、BIOS、CoreValidation、duplicate 和审核管线。单文件、单 archive/DOS ZIP、以及一个 M3U 加按序 2–8 个 CHD 走既有能力；其他多个可启动文件稳定阻断，不取第一项。同一任务、同一目标游戏目录下其他游戏显式声明的 ZIP 只有在冻结 DAT 的 parent/romof 闭包中被当前 machine 直接或间接依赖时才可成为 Arcade companion；不得把 Collection 中全部 ZIP 作为候选传给单个游戏。

内容管线产出的普通 ImportItem 无论 CoreValidation 为 READY 还是 BLOCKED/INCOMPATIBLE，都会带冻结的 Pegasus metadata、COVER/VIDEO 来源和一一关联关系进入统一 `REVIEW_PENDING` 队列；Worker 在此停止，不创建 Game。Pegasus 原始 metadata 仍作为不可变来源证据，交接到普通 ReviewDraft 前必须按通用审核字段契约归一化：description 在 code point 边界截断到 10,000，developer/publisher/genre 截断到 200，不在 `1950..当前 UTC 年+1` 的 releaseYear 置空；每个调整以 `{code:"FIELD_TRUNCATED"|"FIELD_VALUE_INVALID",field}` 追加到 Pegasus Item warning，不得把超出 Review PATCH 契约的值直接写入草稿。生成初始 Validation 时，Arcade `BIOS_OR_BASE` 必须先按冻结 CoreArtifact 精确合并当前 active 的匹配 DAT BIOS，并把 Blob 写入 `BIOS_BUNDLE` ValidationFile；不能只在管理员稍后点击“重新运行检查”时才发现导入前已经安装的 BIOS。队列可按 `pegasusImportId` 精确收窄，Pegasus 来源 metadata 不计作“未找到信息”，详情显示来源 Collection、封面和不自动播放的等比居中 VIDEO。管理员逐项处理运行依赖、编辑草稿和 Discard；严格 READY 的无重复条目也可通过同一筛选范围的快速审批发布。Approve 才在普通审核事务内形成 `SERVER_PEGASUS_IMPORT` MetadataRevision/ContentRevision 并复制来源媒体，人工候选/上传封面优先于 Pegasus COVER；Discard 保留普通 ReviewEvent 并把 Pegasus Item 收口为 `REVIEW_DISCARDED`。审核前必须为零 Game，逐项或快速审批的每次成功决策都同时更新普通 ImportJob 与 Pegasus 聚合。

相同规范内容不生成审核事项或第二个 Game，而是保存所有匹配证据并以 `SKIPPED_EXISTING` 收口。运行检查未通过时必须保留 library validation 的原始 `status/compatibilityCode/core` 和经过封闭投影的依赖快照，包括 machine、缺失/不匹配条目、parent/BIOS 逻辑归档、必需 entry 与多盘缺失引用；不得统一覆盖成无解释的 `PEGASUS_RUNTIME_BLOCKED`。library import 自身发生内部失败时收口为可重试 `PEGASUS_LIBRARY_IMPORT_FAILED`，并持久化失败 stage、operation、稳定 cause、受限技术文本、相对路径、输入数量/上限和可用内部关联 ID，不得只返回聚合错误码，也不得误报为内容不兼容。COVER/VIDEO 独立按 game 显式、Collection 显式、title 目录、file basename 目录的顺序选择；媒体读取或格式失败只留下 warning，不使可运行 ROM 失败。取消只停止尚未交接的工作，已经生成的审核事项继续保留；retry 只重开服务端标记 retryable 的失败 Item，复用冻结映射与 snapshot，不重做成功、待审核、已发布、审核丢弃、已存在或确定性阻断项，并清空旧失败详情。交接阶段崩溃时复用已关联的内部 ImportItem 并幂等补齐 metadata，不能制造不可见的第二个审核条目。历史版本已经写成通用 `PEGASUS_RUNTIME_BLOCKED` 且丢失诊断的条目可在原计划重新运行检查一次，以恢复真实结论和详细证据，无需重新扫描或映射。

聚合状态为 `SCANNING → AWAITING_MAPPING → QUEUED → RUNNING → COMPLETED|PARTIAL_FAILURE`，另有 `CANCEL_REQUESTED/CANCELLED/FAILED/EXPIRED`；等待映射计划 7 天过期，全实例至多 20 个未开始计划和一个执行中的 Pegasus import。统一验收见 `ACC-PEG-001`–`005` 与 `ACC-MEDIA-001`。

## 15. 标签默认值与发布传播

普通 Import 创建与 rejected-only reconfigure 接受既有活动 `tagIds`。服务在同一写事务先验证 0–20 个唯一 ID，再冻结按名称排序的 `{tagId,name}` 到配置 snapshot，并将集合复制到本次创建的每个 ReviewDraft；任一 Tag 不存在或已删除时不创建 ImportJob/Item，也不消费 Upload。reconfigure 页面只预填旧 snapshot 中仍活动的 Tag；标签不进入 source manifest、content identity 或重复内容判定。

ReviewDraft PATCH 必须携带当前完整 `tagIds`，并与元信息、Validation、媒体等共用 Review version 与自动保存序列。Tag 删除会推进待审核 Draft version；旧 PATCH/Approve 返回 `VERSION_CONFLICT`。Approve 在原发布事务重新验证并把当前活动 ReviewDraftTag 复制到 GameTag，Game、ReviewEvent 和 Tag 关系要么同时提交、要么全部回滚。Discard 不发布 GameTag，但保留 Draft 关系和最终 ReviewEvent 的 `{tagId,name}` snapshot。

Pegasus Collection 的 `IMPORT` mapping 必须显式提供 `tagIds`，`SKIP` 必须为空；mapping 事务同时保存关系与名称 snapshot。start 后冻结，retry、lease recovery 和 handoff 复用同一 Collection 选择，尚未 handoff 的 Item 只继承仍 ACTIVE 的 ID；已经待审/发布/丢弃/跳过的 Item 不重复分配。Pegasus metadata 的 `tags:`、genre 及其他外部标签绝不自动创建或按名称关联 Retrom Tag；`SKIPPED_EXISTING` 不改变既有 GameTag。完整生命周期见 [`game-tags.md`](./game-tags.md)。

## 16. 统一验收入口

本专题统一执行 [一期项目验收规范](./project-acceptance.md) 的 `ACC-IMP-001`–`ACC-IMP-008` 与 `ACC-PEG-001`–`005`；详情媒体执行 `ACC-MEDIA-001`，标签默认值、删除并发和原子发布执行 `ACC-TAG-003`–`004`。游戏目录唯一归属由 `ACC-PLAT-*`、时间与 CAS 约束由 `ACC-DB-*` 和 `ACC-CAS-*` 联合覆盖。流程、通过标准和证据只在统一文档维护。
