# Retrom 产品与架构总览

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期实施基线 |
| 版本 | 1.2 |
| 日期 | 2026-08-14 |
| 适用范围 | Retrom 一期 |
| 技术栈 | Go、Next.js、React、Tailwind CSS、SQLite、本地文件存储、版本锁定 EmulatorJS（4.2.3 基线 + 4.3.0-pre 定向覆盖）、OCI/Docker 镜像 |

## 1. 文档职责

本文只维护 Retrom 一期的产品边界、跨模块关系、关键决策和实施顺序。具体字段、页面状态、任务状态机、BIOS/DAT 规则、API 及可执行验收 Case 不再堆叠于总览，由对应专题或统一验收文档作为唯一实现基线。

### 1.1 文档地图

| 文档 | 唯一负责的内容 |
| --- | --- |
| [一期项目验收规范](./project-acceptance.md) | 全部可执行验收 Case、固定数据、证据格式、超时、缺陷回归和最终通过规则 |
| [UI 与交互规范](./ui-specification.md) | 信息架构、页面规格、320px 起的响应式与 4K 布局、直接启动交互和设计稿入口 |
| [游戏目录领域设计](./platform-instance.md) | PlatformInstance 字段、唯一归属、默认核心、变更和迁移规则 |
| [导入、刮削与审核](./import-and-review.md) | 文件/目录导入、Hasheous、任务状态机、审核与回溯 |
| [BIOS 与 Arcade DAT](./bios-and-arcade.md) | BIOS 要求、哈希校验、核心专属 DAT、依赖解析和管理 UI |
| [EmulatorJS 4.2.3 Arcade DAT 基线](./arcade-dat-baseline.md) | 真实 DAT 的来源、SHA-256、统计值、artifact 绑定与升级校验 |
| [运行时、启动与游玩数据](./runtime-and-play-data.md) | 一键启动、默认全屏、预检、EmulatorJS、DOS、存档与游玩时长 |
| [核心运行时验证基线](./core-runtime-validation.md) | 35 核真实夹具、Chrome 启动画面证据、可重复验证链路、PSP ISO/CSO 和兼容覆盖 |
| [存储与数据库](./storage-and-database.md) | SQLite 时间戳规则、表目录、CAS、归档安全、GC 和备份 |
| [一期数据库实体与不变量](./data-model.md) | 表字段、枚举、revision、外键、索引与数据库级保护 |
| [HTTP API、上传与启动凭据契约](./http-api-contract.md) | JSON/错误协议、认证/CSRF、上传分块、launch cookie、内容缓存和路由 |
| [第三方运行时与 DAT 依赖管理](./dependency-management.md) | 小型 manifest、构建前物化、完整性校验、镜像纳入与升级规则 |
| [后端、API 与运行维护](./backend-api-and-operations.md) | Go 模块、API、任务队列、文件端点、安全、日志和部署 |
| [工程质量、Lint 与测试规范](./engineering-quality-and-testing.md) | Go/Next.js lint、统一门禁、关键路径测试、bug 回归固化和 CI |

发生冲突时：专题文档负责其领域细节；本文负责产品边界和跨领域不变量。完整索引见 [docs/README.md](./README.md)。

## 2. 产品定位与一期范围

Retrom 是供用户与可信朋友共享的自托管复古游戏 Web 平台。用户从 Chrome 的手机、平板或桌面视口浏览已发布游戏、选择存档快速继续；同一站点还提供游戏导入、审核、游戏目录、BIOS/DAT 和游戏维护能力。普通页面在 `320px` 起提供完整响应式操作，移动 Player 仅在横屏稳定后才装载游戏运行时。

一期目标：

- 支持浏览、搜索、按基础平台及游戏目录筛选并启动已发布游戏。
- 支持手动状态存档；每个存档必须有截图，并能一键恢复到对应进度。
- 记录最近游玩、继续游玩和有效游玩时长。
- 支持文件和目录导入、SHA-256 去重、元信息刮削、人工审核、发布与审核回溯。
- 使用 Hasheous 的免登录哈希查询作为一期元信息候选源；不集成 ScreenScraper。
- 使用与具体 EmulatorJS/core artifact 绑定的 DAT 识别 Arcade machine、parent ROM 和 BIOS 依赖；DAT 不承担元信息刮削。
- 支持游戏元信息、文件 revision、游戏目录、BIOS 和用户 DAT 的管理。
- 支持安全初始化、邀请注册、账户密码轮换以及管理员维护账号角色与状态。
- 所有私有游玩、存档和启动数据按账号 Profile 隔离；管理员没有读取他人私有数据的旁路。
- 可选启用两人异地联机房间；首发开放 manifest 精确锁定的 EmulatorJS 4.2.3 FCEUmm/FBNeo core profile，覆盖其全部合格 READY 游戏，使用服务端中继输入的 rollback，不传输画面或音频。
- 正式支持 35 个逐一验证的 EmulatorJS core；完整平台映射、默认目录与核心清单见第 6 节，画面证据见核心运行时验证基线。

一期不包含：

- 多因素认证、WebAuthn、自助邮件找回密码和外部身份提供商。
- Chrome 之外的浏览器兼容性承诺；移动端正式支持范围仍限定为 Chrome。
- 公网匿名开放或无需登录的管理入口。
- 自动匹配、聊天、观战、语音、WebRTC 信令或 TURN；联机首发只有已登录用户通过房间链接加入的两人模式。
- Arcade Merged ROMset。
- 成就、评分、评论、推荐算法和社交关系。

“和朋友一起玩”包括通过管理员邀请共享游戏目录，以及在 `RETROM_NETPLAY_ENABLED=true` 时通过同源房间链接进行受限异地联机。每个账号固定拥有一个不可复用的 Profile；游戏目录共享，普通最近游玩、Launch、存档和 PersistentSave 私有。联机参与身份仍按 Profile 授权，但本局从头开始且不读取或写入任何个人存档。

## 3. 关键产品与技术决策

### 3.1 游戏目录决定默认核心

领域关系不是“游戏直接属于平台”，而是：

~~~mermaid
flowchart LR
    P["Platform 基础平台"] -->|创建| PI["PlatformInstance 游戏目录"]
    P -->|关联 N 个| C["Core 核心"]
    PI -->|选择 1 个| DC["默认核心"]
    PI -->|唯一持有| G["Game 游戏"]
    G -->|指向当前| CR["GameContentRevision 用户内容版本"]
    G -->|每核心 1 个稳定槽| V["GameVariant"]
    V -->|指向当前| VR["GameVariantRevision 不可变"]
    CR -->|被验证为| VR
    C --> V
~~~

- Platform 表示 Arcade、NES、MS-DOS 等基础硬件/系统。
- Platform 可关联多个 Core，但自身不保存默认核心。
- PlatformInstance 在 UI 统一称为“游戏目录”；它从 Platform 创建，并从该平台的启用核心中选择一个默认核心。
- Game 只能且必须由一个 PlatformInstance 持有，基础平台由此间接推导。
- Game 另以非空 current pointer 持有一个不可变 GameContentRevision，唯一决定普通启动的当前用户文件；改变游戏目录默认核心不能改变内容版本。
- GameVariant 是 Game + Core 的稳定逻辑槽；某个 GameContentRevision 在具体 core artifact 上的兼容性、派生文件和依赖快照保存在不可变 GameVariantRevision。稳定槽有 READY 结果时只指向当前 READY revision；从未验证成功的备用核心槽允许 current 为空并保留失败诊断。
- 详情页在没有浏览器本地偏好时选择游戏目录的默认核心，同时保留该基础平台其他核心供用户显式切换；浏览器可按游戏记住非默认选择，选回推荐核心即清除。偏好不修改服务端游戏目录，也不做失败后的静默回退。

因此 Arcade 可创建“FBNeo 游戏列表 → FBNeo”“MAME 游戏列表 → MAME 2003”“FBNeo 飞行游戏 → FBNeo”。用户导入时选择游戏目录，而不是直接选择基础平台。

### 3.2 DAT 与刮削是两条独立证据链

- `fbneo`、`mame2003`、`mame2003_plus`、`fbalpha2012_cps1`、`fbalpha2012_cps2` 各自使用核心专属 DAT。
- DAT 只负责 ROM entry、machine、clone/parent、BIOS 依赖及核心兼容性诊断。
- Hasheous 只根据内容哈希提供标题、厂商、描述、封面等展示元信息候选。
- 两者独立保存原始证据、版本和审核结果；一方未命中不能覆盖另一方的结论。
- 系统预置并锁定真实 DAT；管理员也可针对某核心上传 DAT，解析并预览差异后显式启用或回滚。

### 3.3 时间统一为整数时间戳

数据库中表示“某个时刻”的字段一律使用 SQLite `INTEGER`，存 UTC Unix 毫秒时间戳，并命名为 `*_at_ms`；Go 使用 `int64`，API 使用 `createdAtMs` 等 camelCase `int64`。不得以 TEXT/RFC 3339 或 `CURRENT_TIMESTAMP` 作为业务时刻的主存储。

时长也使用 `INTEGER` 毫秒，命名为 `*_duration_ms` 或 `*_interval_ms`。发行年份等日历属性是普通 `INTEGER`，不伪装成时间戳。唯一完整规范及迁移规则见 [存储与数据库](./storage-and-database.md)。

### 3.4 点击一次即开始游戏

从游戏详情的“开始游戏”、详情中的存档“继续”、我的存档“从这里继续”和首页“继续游戏”进入时：

1. 原始点击事件立即请求浏览器全屏。
2. 同一 Player Shell 显示预检/加载状态，不出现第二个 Start/Play 按钮。
3. 后端完成启动预检；已 READY 时返回可记录的非秘密 `launchId` 并以限定路径的 HttpOnly cookie 下发短时 capability。若用户所选备用 Core 尚需物化依赖，同一加载壳先等待可观察验证 Job，完成后自动重调创建 Launch；不要求用户再点开始。秘密不进入 URL 或 JSON。
4. 前端导航到 `/play/:launchId`，用 cookie 读取该会话的运行配置，再以 `EJS_startOnLoaded = true` 配置 EmulatorJS，资源就绪后自动运行。
5. 若存在阻断项，退出全屏并在来源上下文展示可修复错误；普通警告不增加确认步骤。

存档快速启动锁定创建该存档时的 CoreArtifact 和 GameVariantRevision，不让目录默认核心覆盖它。同一浏览器携带 launch cookie 刷新深链时因缺少用户激活而无法自动进入全屏，允许显示一次“进入全屏”恢复控件但仍自动运行；把 URL 复制到没有 cookie 的 context 只能显示“启动会话不可用”。

### 3.5 原始内容不可变并用 SHA-256 去重

- 上传内容流式计算 SHA-256，写入本地内容寻址存储；相同内容只保存一个 Blob。
- Blob 发布后不原地修改。替换游戏文件创建新的 GameContentRevision，并在默认核心验证 READY 后创建对应 GameVariantRevision、原子切换两个 current pointer；相同 Blob 仍可出现在不同内容修订的审计链中。
- 存档继续引用其创建时的 revision；只要仍有引用，旧 Blob 不得垃圾回收。
- 数据库保存逻辑关系、哈希、大小、MIME 和引用，不保存宿主机任意路径供浏览器使用。

### 3.6 模块化后端、双镜像与单一数据目录

一期的后端仍是单个 Go 模块化单体，负责 API、进程内持久任务队列、EmulatorJS 运行时、受控内容端点、SQLite 与本地 CAS；前端作为独立 Next.js 进程提供 UI 与 Player Shell。构建分别产出后端镜像 `retrom` 和前端镜像 `retrom-web`，前后端分镜像不等于把后端领域拆成微服务。

生产环境由已有 NG（Nginx/网关/反向代理）对外暴露同一个 HTTPS origin，再通过明文 HTTP 路由至两个应用。Retrom 不加载证书、不监听 HTTPS，也不负责 TLS 跳转或 HSTS。开发环境的 `make dev` 直接启动宿主机 Go 与 Next.js 进程，不使用 Docker。

SQLite 使用 WAL；所有用户文件写入一个明确的数据目录。Next.js + React + Tailwind CSS 位于仓库根目录 `web/`。

### 3.7 账户边界与破坏性数据版本

- 默认 `release` 模式的空实例进入 `PENDING`，只有持有主机侧 `retrom setup-code` 输出的人能创建首位启用管理员；初始化完成后不可重开。
- `--mode=test` 只供明确的开发/验收数据根使用，会在空库创建 `test/test` 并显示警告；除此之外不放宽认证、授权、Origin、CSRF、cookie 或数据隔离。
- 已初始化实例的普通 API 要求有效 AuthSession，`/api/v1/admin/**` 另要求 `ADMIN`。普通管理员只管理账号和共享内容，不能查看其他用户的存档名称、截图、游玩记录或保存内容。
- 账户版本以 migration 020 为边界。任何 001–019 旧数据库在执行 DDL/DML 前以 `DATABASE_REBUILD_REQUIRED` 拒绝；发布时归档旧数据根并使用全新空根，不迁移旧 `local` 数据。
- Session、Invitation、PasswordReset 和 Launch 都是服务端可撤销能力。停用/删除账号和恢复安全围栏会同步撤销相应凭据；密码变化撤销 AuthSession，但不扩大 Launch 权限。

### 3.8 多盘内容是一个不可拆分 revision

Saturn/yabause 的 `MULTI_DISC_M3U_V1` 内容由同一物理目录中的一个来源 M3U 与按其顺序引用的 2–8 个 CHD 组成。ImportItem、SourceSnapshot、GameContentRevision、GameVariantRevision、Launch 和 SaveState 都以整组盘序为边界；缺盘只形成审核依赖，不创建占位 Blob。发布后运行时仅暴露服务端规范化的 `playlist.m3u` 与 `disc-NNN.chd`，不暴露原始路径。该能力由 feature flag、Platform content profile 与当前 enabled CoreArtifact compatibility 三者取交集，首发只有 Saturn/yabause 可用；关闭新导入能力不破坏已发布多盘内容的运行和存档。

### 3.9 收藏是 Profile 私有的独立多对多能力

收藏不改变 Game 对 PlatformInstance 的唯一归属。Favorite 绑定认证 Profile 与共享 Game；FavoriteFolder 只组织已收藏游戏，一款 Game 可进入多个 Folder。加入 Folder 自动收藏，移除或删除 Folder 保留 Favorite，取消 Favorite 才原子删除全部 Membership。管理员没有跨 Profile 查看或维护入口，游戏不可见只隐藏投影而不删除关系。完整边界见 [收藏与收藏夹](./favorites-and-collections.md)。

### 3.10 联机是版本锁定的非串流 rollback 能力

联机不是所有核心自动获得的通用能力。`data/netplay/v1/manifest.json` 同时锁定 EmulatorJS、普通 Player adapter、联机 adapter、FCEUmm/FBNeo core artifact SHA-256、允许的内容类型、24 个控制值和 prediction/rollback/state 上限；不按 ROM 名称、大小或 hash 决定资格。游戏仍须有引用该 artifact 与当前 ContentRevision 的 READY VariantRevision，依赖快照必须有效；房间和 Session 再锁定该不可变 revision，确保每位参与者运行同一内容。Go `internal/netplay` 只持久化房间控制面并在有界内存中排序输入、比较 checkpoint hash、转发不超过 1 MiB 的 savestate 和保留 10 秒断线租约。WebSocket 凭据使用独立 netplay key 与 HttpOnly room cookie，不复用 Launch capability。未知版本、profile 漂移、state 不一致或协议越界全部 fail closed；进程重启结束活动联机，不尝试跨进程恢复实时帧。

### 3.11 标签是实例共享、管理员维护的分类

Tag 必须先由管理员建立，再以稳定 ID 关联 Game、导入 ReviewDraft 或 Pegasus Collection；普通用户只能看到可见游戏已关联的活动标签。它与 Profile 私有 FavoriteFolder、单归属 PlatformInstance、metadata genre 和外部来源 tags 都是不同概念。重命名通过动态关系投影立即生效；删除还会推进受影响 owner version，使旧写入稳定冲突。两者都不改写游戏元信息/内容 revision，也不进入 Launch、Player 或存档。完整边界见 [游戏标签](./game-tags.md)。

## 4. 系统上下文

~~~mermaid
flowchart LR
    U["Chrome · Phone / Tablet / Desktop"] -->|HTTPS| N["前置 NG / TLS 终结"]
    N -->|HTTP：页面 / _next| W["retrom-web / Next.js + Player Shell"]
    N -->|HTTP：API / content / runtime| S["retrom / Go 模块化单体"]
    W --> R["浏览器内锁定版本 EmulatorJS（初始 4.2.3）"]
    S --> D["SQLite WAL"]
    S --> B["本地 SHA-256 CAS"]
    S --> J["SQLite 队列 + 进程内 Worker"]
    J --> A["Arcade DAT 解析器"]
    J --> H["Hasheous 哈希元信息查询"]
    R -->|同源启动资源 / 存档 / 心跳| N
    R -->|同源 WSS：输入 / hash / state| S
~~~

部署与安全边界：

- 所有页面经同源认证入口；匿名用户只能访问初始化、登录、邀请注册和密码重置页面，普通用户不能访问管理 API。
- 两个应用只监听明文 HTTP；生产环境只向受信容器/主机网络开放，由前置 NG 终结 TLS 并提供 HTTPS。
- ROM、BIOS、存档等只通过短时 LaunchSession capability cookie 授权的同源端点提供；URL 只有非秘密 `launchId`，不暴露宿主路径或内容 hash。
- NG 必须让页面、EmulatorJS 与受控内容端点保持同源，并保留/设置正确的 COOP/COEP/CORP 响应头；DOSBox Pure 等线程模式依赖该安全上下文。
- 所有浏览器写入校验精确公开 `Origin`；已登录写入另校验内存中的 CSRF token。可信代理 CIDR 只用于规范化限流客户端 IP，不构成授权。
- EmulatorJS、core artifact 和 DAT 均锁定版本，不依赖浮动 CDN。
- 联机只在同一 Go 进程内中继；NG 必须保留 WebSocket upgrade，同一数据根仍禁止多个后端写进程。

## 5. 核心领域关系

~~~mermaid
erDiagram
    PLATFORM ||--o{ PLATFORM_INSTANCE : creates
    PLATFORM ||--o{ PLATFORM_CORE : supports
    CORE ||--o{ PLATFORM_CORE : belongs
    PLATFORM_INSTANCE }o--|| CORE : defaults_to
    PLATFORM_INSTANCE ||--o{ GAME : owns
    GAME ||--o{ GAME_CONTENT_REVISION : content_versions
    GAME ||--o{ GAME_VARIANT : has
    CORE ||--o{ GAME_VARIANT : runs_with
    GAME_VARIANT ||--o{ GAME_VARIANT_REVISION : revisions
    GAME_CONTENT_REVISION ||--o{ GAME_VARIANT_REVISION : validated_by
    GAME_CONTENT_REVISION ||--o{ GAME_CONTENT_FILE : contains
    CORE_ARTIFACT ||--o{ GAME_VARIANT_REVISION : executes
    GAME_VARIANT_REVISION ||--o{ VARIANT_FILE : contains
    BLOB ||--o{ GAME_CONTENT_FILE : stores
    BLOB ||--o{ VARIANT_FILE : stores
    CORE_ARTIFACT ||--o{ BIOS_REQUIREMENT : declares
    BIOS_REQUIREMENT ||--o{ BIOS_INSTALLATION : fulfilled_by
    CORE_ARTIFACT ||--o{ DAT_VERSION : validates_with
    PROFILE ||--o{ SAVE_STATE : owns
    GAME_VARIANT_REVISION ||--o{ SAVE_STATE : creates
    PROFILE ||--o{ PLAY_SESSION : owns
    GAME_VARIANT_REVISION ||--o{ PLAY_SESSION : runs
    PROFILE ||--o{ FAVORITE_GAME : owns
    GAME ||--o{ FAVORITE_GAME : is_saved_as
    PROFILE ||--o{ FAVORITE_FOLDER : owns
    FAVORITE_GAME ||--o{ FAVORITE_FOLDER_GAME : is_grouped_by
    FAVORITE_FOLDER ||--o{ FAVORITE_FOLDER_GAME : contains
    TAG ||--o{ GAME_TAG : classifies
    GAME ||--o{ GAME_TAG : has
~~~

关键不变量：

- PlatformInstance 的默认核心必须是其基础平台已关联且启用的 Core。
- Game 不保存可为空的 `platform_id` 作为另一条归属路径，只保存非空 `platform_instance_id`。
- Game 的 `current_content_revision_id` 唯一决定普通启动内容；目录默认核心、CoreArtifact 或 DAT 变化不得反向改写它。
- 存档同时绑定 GameVariantRevision 与 CoreArtifact；不匹配时不得静默加载。
- BIOS 要求和 DAT 活动版本按 CoreArtifact 隔离；同一个 Blob 可以去重，但 Installation/校验状态不可跨 artifact 或核心串用。
- ImportJob/ImportItem 记录创建时的游戏目录、默认核心、core artifact、DAT 和刮削证据快照；在途结果不因后续配置变化而漂移。
- 审核发布、游戏目录移动、DAT 启用和文件 revision 切换必须可审计。
- Favorite 与 FavoriteFolder 都由认证 Profile 私有拥有；FolderMembership 必须同时引用同一 Profile 的 Favorite 与 Folder。收藏关系不改变 Game 的 PlatformInstance 唯一归属，管理员也没有跨 Profile 查询旁路。
- Tag 是实例级共享实体，只有管理员维护；GameTag 不属于 Profile，不改变目录归属，也不进入游戏运行 revision。

## 6. 平台、核心与初始游戏目录

空库 bootstrap 必须在同一 migration/seed 版本中写入下表的基础平台、启用关系和初始游戏目录；重复执行按稳定 code/slug 幂等，不生成重复目录。管理员之后可以创建、重命名或软删除空目录，但不得修改基础平台 code。

| 基础平台（稳定 code） | 启用核心 | 初始游戏目录（slug）→ 默认核心 | 备注 |
| --- | --- | --- | --- |
| NES / Famicom (`nes`) | `fceumm` | NES 游戏（`nes-games`）→ `fceumm` | 普通卡带不要求 BIOS |
| Famicom Disk System (`fds`) | `fceumm` | FDS 游戏（`fds-games`）→ `fceumm` | 需要 `disksys.rom` |
| SNES (`snes`) | `snes9x` | SNES 游戏（`snes-games`）→ `snes9x` | 标准游戏通常不要求 BIOS |
| Game Boy / Color (`gbc`) | `gambatte`、`mgba` | Game Boy 游戏（`gbc-games`）→ `gambatte` | 两个 core 均可供本次启动切换 |
| Game Boy Advance (`gba`) | `mgba` | GBA 游戏（`gba-games`）→ `mgba` | BIOS 可选 |
| Arcade (`arcade`) | `fbneo`、`mame2003_plus`、`mame2003`、`fbalpha2012_cps1`、`fbalpha2012_cps2` | FBNeo 游戏（`fbneo-games`）→ `fbneo`；MAME 2003 Plus 游戏（`mame2003-plus-games`）→ `mame2003_plus`；MAME 2003 游戏（`mame2003-games`）→ `mame2003`；FB Alpha 2012 CPS-1/2 游戏（`fbalpha2012-cps1-games` / `fbalpha2012-cps2-games`）→ 对应核心 | 每个核心使用独立 DAT |
| MS-DOS (`dos`) | `dosbox_pure` | DOS 经典游戏（`dos-games`）→ `dosbox_pure` | 启动前可选程序；需要线程模式 |
| Nintendo DS (`nds`) | `melonds`、`desmume2015`、`desmume` | Nintendo DS 游戏（`nds-games`）→ `desmume2015` | 指针输入；MelonDS 需要三个外部 BIOS 文件 |
| Atari 2600 (`atari2600`) | `stella2014` | Atari 2600 游戏（`atari-2600-games`）→ `stella2014` | `.a26`；允许 ZIP/7z 单成员来源 |
| Atari 5200 (`atari5200`) | `a5200` | Atari 5200 游戏（`atari-5200-games`）→ `a5200` | `.a52`；需要 `5200.rom` |
| Atari 7800 (`atari7800`) | `prosystem` | Atari 7800 游戏（`atari-7800-games`）→ `prosystem` | `.a78`；需要 `7800 BIOS (U).rom` |
| Atari Lynx (`lynx`) | `handy` | Atari Lynx 游戏（`atari-lynx-games`）→ `handy` | `.lnx`；需要 `lynxboot.img` |
| Mega Drive / Genesis (`megadrive`) | `genesis_plus_gx`、`picodrive`、`genesis_plus_gx_wide` | Mega Drive 游戏（`mega-drive-games`）→ `genesis_plus_gx` | `.md`；Wide 为可选核心，不另建目录 |
| PC Engine (`pce`) | `mednafen_pce` | PC Engine 游戏（`pc-engine-games`）→ `mednafen_pce` | `.pce` |
| Neo Geo Pocket / Color (`ngpc`) | `mednafen_ngp` | Neo Geo Pocket 游戏（`neo-geo-pocket-games`）→ `mednafen_ngp` | `.ngp` |
| Nintendo 64 (`n64`) | `mupen64plus_next`、`parallel_n64` | Nintendo 64 游戏（`nintendo-64-games`）→ `mupen64plus_next` | `.z64`；产品 ID 只使用 `parallel_n64` |
| PlayStation (`psx`) | `pcsx_rearmed`、`mednafen_psx_hw` | PlayStation 游戏（`playstation-games`）→ `pcsx_rearmed` | 单文件 CHD；后者需要线程且固定 software renderer |
| Sega Saturn (`saturn`) | `yabause` | Sega Saturn 游戏（`sega-saturn-games`）→ `yabause` | 单文件 CHD |
| PC-FX (`pcfx`) | `mednafen_pcfx` | PC-FX 游戏（`pc-fx-games`）→ `mednafen_pcfx` | 单文件 CHD |
| 3DO (`3do`) | `opera` | 3DO 游戏（`3do-games`）→ `opera` | 单文件 CHD |
| PlayStation Portable (`psp`) | `ppsspp` | PSP 游戏（`psp-games`）→ `ppsspp` | raw ISO/CSO；需要线程与固定辅助资产 |
| Virtual Boy (`virtualboy`) | `beetle_vb` | Virtual Boy 游戏（`virtual-boy-games`）→ `beetle_vb` | `.vb`；四条锁定启动动作，最大延迟 25 秒 |
| WonderSwan / Color (`wonderswan`) | `mednafen_wswan` | WonderSwan 游戏（`wonderswan-games`）→ `mednafen_wswan` | `.ws` / `.wsc` 单文件 |
| Master System (`mastersystem`) | `smsplus` | Master System 游戏（`master-system-games`）→ `smsplus` | `.sms`；本期只建立 Master System 映射 |
| Nintendo 3DS (`nintendo3ds`) | `azahar` | Nintendo 3DS 游戏（`nintendo-3ds-games`）→ `azahar` | `.3ds` / `.cci`；4.3.0-pre thread、pointer、WebGL2 |

平台和核心是代码种子/版本化配置；游戏目录是管理员可创建、重命名和调整默认核心的业务实体。游戏目录不是标签或多对多收藏集。

## 7. 产品信息架构

用户侧左侧导航：

- 首页：有效游玩时长、继续游玩、最近游玩和最新添加游戏。
- 游戏库：搜索、平台/游戏目录筛选和已发布游戏卡片。
- 我的存档：带截图的手动存档及快速继续。
- 最近游玩：只展示当前账号的启动历史。
- 联机游玩：feature flag 开启时位于“最近游玩”之后，展示当前房间、最近终局和创建入口。
- 账户设置：只读账号资料和密码轮换。
- 管理后台：固定在底部，切换整套管理菜单。

管理后台左侧导航：

- 游戏入库
  - 导入游戏
  - 本地扫描
  - 任务进度
  - 待审核
  - 审核历史
- 游戏管理
- 游戏目录
- 用户管理
- BIOS 管理（包含“BIOS 文件”和“Arcade DAT 版本”视图）

“游戏入库”是可点击的父级总览；五个子项使用明确缩进并保持同级，其中“本地扫描”位于“导入游戏”之后、“任务进度”之前并进入服务器 BIOS 导入能力。进入子页时父项保留上下文高亮，当前子项使用强高亮。游戏详情不是左侧一级菜单。它只能从游戏库卡片、首页最近游戏或资源详情链接进入；进入时左侧仍保持“游戏库”上下文。存档的主按钮直接启动，标题/次要操作才进入游戏详情。

一期路由固定为：

| 页面 | 路由 |
| --- | --- |
| 首次设置 / 登录 / 邀请注册 / 密码重置 | `/setup`、`/login`、`/register`、`/reset-password` |
| 当前账户 | `/account` |
| 首页 / 游戏库 / 存档 | `/`、`/library`、`/saves` |
| 游戏详情 | `/games/:gameId` |
| 持久 Player Shell | `/play/:launchId` |
| 联机首页 / 房间 | `/netplay`、`/netplay/rooms/:roomId` |
| 游戏入库总览 | `/admin/imports` |
| 新建导入 / 本地扫描 / 任务进度 | `/admin/imports/new`、`/admin/imports/server`、`/admin/imports/tasks` |
| 待审核 / 审核详情 / 历史 | `/admin/reviews`、`/admin/reviews/:itemId`、`/admin/reviews/history` |
| 游戏管理 / 详情 | `/admin/games`、`/admin/games/:gameId` |
| 游戏目录 | `/admin/platform-instances` |
| 用户管理 | `/admin/users` |
| BIOS 与 DAT | `/admin/bios`、`/admin/bios/dats` |

完整页面状态、4K 密度和响应式上限见 [UI 与交互规范](./ui-specification.md)。

## 8. 核心业务流程

### 8.1 导入、刮削与审核

~~~mermaid
flowchart LR
    A["选择游戏目录"] --> B["上传文件 / 目录"]
    B --> C["SHA-256 / CAS / 分组"]
    C --> D["Arcade DAT 依赖识别"]
    C --> E["Hasheous 元信息候选"]
    D --> F["人工审核"]
    E --> F
    F -->|通过| G["发布到游戏目录"]
    F -->|不通过| H["Discard + 保留历史"]
~~~

上传完成不等于发布。任务可以部分失败、重试和重启恢复；审核人可换元信息候选、手工编辑字段、调整同基础平台内的游戏目录，并查看 DAT 与 Hasheous 两类独立证据。审核结果和当时快照永久可回溯。

### 8.2 BIOS 与 DAT

服务器导入是一期管理能力：部署者用 `RETROM_SERVER_IMPORT_ROOTS` 建立只读宿主目录信任边界，浏览器只提交 root ID 与规范相对目录。BIOS 任务冻结全部 enabled CoreArtifact 的完整 catalog，先完整发现和评估，再逐 Requirement 短事务安装；Pegasus 任务分为 metadata/facts 扫描、管理员逐 Collection 显式映射、逐游戏复制/运行检查/审核交接三阶段，不执行 `launch` 命令，也不按名称或扩展名猜测目标游戏目录。Pegasus Worker 只生成普通 `REVIEW_PENDING` 事项，不创建 Game；管理员必须在统一审核工作台逐项修复、通过或丢弃，一期没有批量通过。管理员可在审核详情用独立子窗体尽最大可能运行当前来源：现有 Parent/BIOS 会被锁定交付，缺失依赖被省略；READY 与阻断 Validation 都在核心真实启动后第 5 秒写入截图。当前阻断截图与来源、目标、CoreArtifact 和 validation generation 一致时，可作为管理员逐项放行证据；发布的单机 Variant 保留 override 标记并继续最佳努力交付，Netplay 仍执行严格依赖门禁。外部 source 与原始 metadata 不属于 Retrom 数据根、CAS 或 backup；交接审核后的 ROM、封面和 VIDEO 已进入 CAS/backup，恢复时所有仍依赖外部 source 的任务必须失败收口。历史 Launch/VariantRevision 继续引用既有不可变快照。详细领域、协议和页面契约分别见 [`bios-and-arcade.md`](./bios-and-arcade.md)、[`import-and-review.md`](./import-and-review.md)、[`http-api-contract.md`](./http-api-contract.md) 与 [`ui-specification.md`](./ui-specification.md)。

游戏详情是唯一允许请求 VIDEO 的用户页面。详情先用 COVER 保持稳定的 3:4 识别位，媒体区在前台与 viewport 内累计可见满两秒后才尝试 `muted + playsInline + loop`；收到 `playing` 前不隐藏封面，播放拒绝、解码/停滞、隐藏标签页与减少动态效果均有确定性封面回退或手动入口。首页、游戏库、收藏、最近、存档和搜索的 DTO/查询保持 cover-only。

普通核心的 BIOS 需求来自版本化静态目录；Arcade 核心的 machine/parent/BIOS 依赖从该核心活动 DAT 解析。管理页按平台和核心展示逻辑文件名、期望哈希、已安装 Blob 的实际哈希、来源和受影响游戏。

哈希不一致只警告，不禁止用户保存 BIOS；启动是否阻断由该固件对当前 content 是否适用、Requirement 的必需/可选模式和 installation 状态共同决定。Gambatte/mGBA 的可选启动 BIOS 还会从版本化 Requirement 合并上游要求的 core option，不能只挂载文件却忘记启用。Arcade 默认只把当前游戏库实际引用的缺失依赖标为阻断，完整核心目录中未被任何游戏使用的 BIOS 不标红。

### 8.3 直接启动与存档

所有入口共用启动编排器。正常点击后立即进入全屏 Player Shell、显示加载状态并自动运行；详情页显式选择的核心由当前浏览器按游戏保留。DOS 详情多一个启动程序选择框，入口或程序菜单只在 Launch 成功签发后记住，失败不写；存档入口不重新询问、不采用或改写这些偏好，而是恢复存档绑定的运行环境。

手动状态存档必须包含截图。有效游玩时长通过 PlaySession 心跳累计；页面后台、模拟器暂停和长时间失联不计入有效时长。

### 8.4 联机房间与 rollback

房主创建 DRAFT 房间并原子占 P1，选择服务端 eligibility 返回的精确 profile 后进入 WAITING；访客通过站内房间 URL 登录、占 P2，双方 ready 后房主锁定 Session。每位参与者只创建自己的 Launch 与两类路径受限 cookie，进入普通无侧栏 `/play/:launchId`。浏览器先在帧边界暂停，P1 提供初始 RASTATE，服务端校验 full/core digest 后转发给 P2，再开始统一 epoch。运行期服务端只在收到全部已占座输入后发布 canonical frame；客户端最多预测 8 帧、最多回滚 120 帧，每 120 帧比较 core digest。隐藏或失焦只清空本地按键，不改变在线状态或停止会话推进；房主全局暂停或断线才停止推进，断线 10 秒内重连通过 history + 新 state transfer 开启新 epoch。访客退出/超时结束当前 Session、释放其座位并让房间回到 WAITING；房主丢失、服务重启、恢复或 8 小时硬到期才关闭房间。所有联机 save route 固定返回 `409 NETPLAY_SAVE_UNSUPPORTED`。

## 9. 数据与版本基线

- EmulatorJS 基础运行时锁定 `4.2.3`，`dosbox_pure`、`genesis_plus_gx_wide`、`azahar` 定向使用 `4.3.0-pre`；core 和 DAT 必须记录实际 artifact 标识与 SHA-256。`mame2003` 暂用已验证的官方 4.2.1 core bundle 覆盖，精确边界见[核心运行时验证基线](./core-runtime-validation.md)，不得概括成“所有 core 都来自同一版本”。
- 真实 Arcade DAT 在开发、验收和镜像构建前物化到 `data/dat/emulatorjs/4.2.3/`；Git 只保存机器可读 manifest、`SHA256SUMS` 与物化脚本，不提交 50+ MiB payload。同步启动阶段只校验本地依赖并登记解析任务，Worker 可建立数据库索引，但任何启动阶段都不联网下载。
- SQLite schema 中业务时刻全部为 Unix 毫秒 `INTEGER`；禁止后续 migration 引入 TEXT 时刻字段。
- 用户上传内容、下载媒体、存档和截图进入运行时 CAS，不提交到代码仓库。
- 预置 DAT 不可变；用户 DAT 作为新的非活动 DatVersion 保存，经过解析、差异预览和显式启用后才影响新诊断。
- DAT 更新不静默改写已发布 GameVariant 的历史兼容性快照；重校验产生新结果并可追踪来源。
- 联机 allowlist 是独立于普通兼容性的收紧层；只有 READY revision 使用 exact manifest core profile 的游戏可进入房间选择，但同一 profile 不再逐 ROM 限制名称、大小或 hash。

## 10. 一期实施阶段

本节只给产品级阶段；migration 顺序、不可倒置依赖和每阶段退出 Case 以[一期实施编排](./implementation-plan.md)为准。

### Phase 0：兼容性闸门

- 锁定基础 EmulatorJS 4.2.3、定向 4.3.0-pre 与三十五个实际 enabled core artifact（包括版本化覆盖），每个核心启动至少一个用户合法提供的测试游戏；固定兼容基线、线程产物、辅助资产与格式矩阵见[核心运行时验证基线](./core-runtime-validation.md)。
- 验证直接启动、默认全屏、状态存档/截图、持久存档、有效时长心跳。
- 验证 FBNeo/MAME/FBA2012 Split 与 Full Non-Merged 的 parent/BIOS 加载，及五个独立 DAT。
- 已确认 Hasheous 的 `POST /api/v1/Lookup/ByHash` 无凭证契约；自动测试使用 fake，上线前只做一次有界 smoke，不能依赖实时命中内容或把限流阈值写死。
- 已有三十五核历史 smoke 确认固定运行时在 Chrome 中 `crossOriginIsolated`、核心帧推进并进入可辨识画面；它不是产品启动编排的替代。DOSBox Pure 另验证 4.3 whole-archive 启动、虚拟 ZIP 引导、程序菜单、原 bundle 不复制及不安全路径阻断，必须执行 `ACC-RUN-005`。

Phase 0 未通过时，不进入大规模业务实现。

### Phase 1：基础设施

- Go 模块化单体、SQLite migrations、统一错误、OpenAPI。
- Platform/Core 种子、PlatformInstance 约束与初始目录。
- 本地 CAS、受控内容端点、任务队列和 Next.js App Shell。
- 按[工程质量、Lint 与测试规范](./engineering-quality-and-testing.md)建立固定版本 lint、统一 Makefile、关键路径测试和 CI；补齐 `make dev` 本地进程编排，以及 `retrom`/`retrom-web` 的只构建镜像 targets；拒绝 SQLite TEXT 业务时刻字段。

### Phase 2：导入与管理

- 文件/目录导入、任务恢复、Hasheous 适配器、DAT 解析和审核历史。
- 游戏目录、游戏 revision、BIOS 和用户 DAT 管理。
- DAT 差异预览、启用、回滚和批量重校验。

### Phase 3：用户侧与运行时

- 首页、游戏库、游戏详情和我的存档。
- 共用的一键启动编排器、全屏 Player Shell、启动预检和 EmulatorJS 集成。
- DOS 启动程序、错误恢复和核心临时切换。

### Phase 4：存档与稳定性

- 状态存档、截图、持久存档和 PlaySession。
- Blob GC、备份恢复、诊断导出和 Chrome E2E。
- `320px` 起的手机、平板、1280×800 最小桌面、2560×1440 与 4K 视觉回归；移动 Player 另覆盖竖屏阻断、横屏恢复与 P1/P2 暂停职责。

### Phase 5：收藏与收藏夹垂直切片

- Migration 025、Profile 私有 Favorite/Folder/Membership、owner-scoped API 与签名 cursor。
- 游戏库、详情和 `/favorites` 的收藏、分类、批量整理、两秒撤销、键盘与多尺寸闭环。
- 以 `ACC-FAV-001`–`004` 和 `make ci` 为退出门禁。

### Phase 6：受限异地联机垂直切片

- Migration 032、netplay manifest、房间/Session/Participant 控制面、独立 credential key 与备份恢复围栏。
- `/netplay`、房间 UI、SSE、同源 WebSocket hub、4.2.3 帧 adapter、rollback/state/hash/reconnect/end 全链路。
- 以 `ACC-NP-001`–`013`、全量 Player E2E、真实 FCEUmm/FBNeo 夹具 smoke 和双镜像构建为退出门禁。

### Phase 7：游戏标签垂直切片

- Migration 034、实例级 Tag 与 Game/Review/Pegasus 多对多关系、软删除 tombstone、版本联动和审计。
- 管理 CRUD、普通导入/Pegasus 默认值、审核原子发布、游戏维护、动态 `q` 与精确 `tagId` 搜索、全站活动标签投影。
- 以 `ACC-TAG-001`–`005`、API/后端/前端/集成/Chrome E2E 和 `make ci` 为退出门禁；不进入 core smoke。

## 11. 统一验收入口

一期所有验收流程、标准、固定夹具、证据要求和短时执行上限统一由 [一期项目验收规范](./project-acceptance.md) 维护。该文档中的 `ACC-*` Case 覆盖本文的全部一期范围；专题文档只解释设计和实现约束，不再维护另一份通过条件。

Agent 不得根据本总览自行省略或合并 Case，尤其不得把三十五个核心合成一个长时间运行任务，也不得用 soak、压力测试或无限等待代替统一规范中的确定性短时流程。

## 12. 已锁定边界与后续议题

以下决定均已进入一期基线，不再作为实施中的自由选择：使用 Hasheous 且不使用 ScreenScraper；DAT 只用于 Arcade 识别/依赖；Game 唯一属于游戏目录；详情页不是一级导航；正常启动一步完成并默认全屏；数据库时刻统一 Unix 毫秒 `INTEGER`；必须登录且账号 Profile 私有；一期只支持 Arcade Split / Full Non-Merged ROMset，不支持必需 CHD 和 Merged ROMset；联机是可关闭、按精确 EmulatorJS/core artifact allowlist、单进程服务端中继的非串流 rollback 且不支持存档；当前设计稿的现代复古、深色侧栏和紫色主操作色是视觉基线；前后端分别构建 `retrom`/`retrom-web` 镜像但构建不启动服务；`make dev` 只运行本地进程；TLS 只由前置 NG 终结。

FCEUmm/FBNeo 之外的核心联机、自动匹配/聊天/观战/WebRTC、MFA/外部身份、必需 CHD 和 Merged ROMset 只能作为未来版本提案，必须新增设计、威胁模型、迁移和验收 Case；一期 agent 不得把两个已验证 core profile 扩大为任意核心都自动支持联机。

## 13. 评审入口与参考

- [打开可交互 UI 设计稿](./design/retrom-ui-review.html)
- [EmulatorJS Options](https://emulatorjs.org/docs/options/)
- [EmulatorJS Cores](https://emulatorjs.org/docs4devs/cores/)
- [EmulatorJS v4.2.3](https://github.com/EmulatorJS/EmulatorJS/releases/tag/v4.2.3)
- [Hasheous Applications and API](https://github.com/gaseous-project/hasheous/wiki/Applications-and-API)
- [EmulatorJS 4.2.3 Arcade DAT 基线](./arcade-dat-baseline.md)
