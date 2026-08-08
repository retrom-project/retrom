# Retrom 产品与架构总览

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期实施基线 |
| 版本 | 1.0 |
| 日期 | 2026-08-06 |
| 适用范围 | Retrom 一期 |
| 技术栈 | Go、Next.js、React、Tailwind CSS、SQLite、本地文件存储、EmulatorJS 4.2.3、OCI/Docker 镜像 |

## 1. 文档职责

本文只维护 Retrom 一期的产品边界、跨模块关系、关键决策和实施顺序。具体字段、页面状态、任务状态机、BIOS/DAT 规则、API 及可执行验收 Case 不再堆叠于总览，由对应专题或统一验收文档作为唯一实现基线。

### 1.1 文档地图

| 文档 | 唯一负责的内容 |
| --- | --- |
| [一期项目验收规范](./project-acceptance.md) | 全部可执行验收 Case、固定数据、证据格式、超时、缺陷回归和最终通过规则 |
| [UI 与交互规范](./ui-specification.md) | 信息架构、页面规格、4K 布局、直接启动交互和设计稿入口 |
| [平台目录领域设计](./platform-instance.md) | PlatformInstance 字段、唯一归属、默认核心、变更和迁移规则 |
| [导入、刮削与审核](./import-and-review.md) | 文件/目录导入、Hasheous、任务状态机、审核与回溯 |
| [BIOS 与 Arcade DAT](./bios-and-arcade.md) | BIOS 要求、哈希校验、核心专属 DAT、依赖解析和管理 UI |
| [EmulatorJS 4.2.3 Arcade DAT 基线](./arcade-dat-baseline.md) | 真实 DAT 的来源、SHA-256、统计值、artifact 绑定与升级校验 |
| [运行时、启动与游玩数据](./runtime-and-play-data.md) | 一键启动、默认全屏、预检、EmulatorJS、DOS、存档与游玩时长 |
| [核心运行时验证基线](./core-runtime-validation.md) | 8 核真实夹具、Chrome 启动画面证据、可重复验证链路和兼容覆盖 |
| [存储与数据库](./storage-and-database.md) | SQLite 时间戳规则、表目录、CAS、归档安全、GC 和备份 |
| [一期数据库实体与不变量](./data-model.md) | 表字段、枚举、revision、外键、索引与数据库级保护 |
| [HTTP API、上传与启动凭据契约](./http-api-contract.md) | JSON/错误协议、上传分块、受信内网写请求、launch cookie、内容缓存和路由 |
| [第三方运行时与 DAT 依赖管理](./dependency-management.md) | 小型 manifest、构建前物化、完整性校验、镜像纳入与升级规则 |
| [后端、API 与运行维护](./backend-api-and-operations.md) | Go 模块、API、任务队列、文件端点、安全、日志和部署 |
| [工程质量、Lint 与测试规范](./engineering-quality-and-testing.md) | Go/Next.js lint、统一门禁、关键路径测试、bug 回归固化和 CI |

发生冲突时：专题文档负责其领域细节；本文负责产品边界和跨领域不变量。完整索引见 [docs/README.md](./README.md)。

## 2. 产品定位与一期范围

Retrom 是供用户与可信朋友共享的自托管复古游戏 Web 平台。用户从桌面版 Chrome 浏览已发布游戏、选择存档快速继续；同一站点还提供游戏导入、审核、平台目录、BIOS/DAT 和游戏维护能力。

一期目标：

- 支持浏览、搜索、按基础平台及平台目录筛选并启动已发布游戏。
- 支持手动状态存档；每个存档必须有截图，并能一键恢复到对应进度。
- 记录最近游玩、继续游玩和有效游玩时长。
- 支持文件和目录导入、SHA-256 去重、元信息刮削、人工审核、发布与审核回溯。
- 使用 Hasheous 的免登录哈希查询作为一期元信息候选源；不集成 ScreenScraper。
- 使用与具体 EmulatorJS/core artifact 绑定的 DAT 识别 Arcade machine、parent ROM 和 BIOS 依赖；DAT 不承担元信息刮削。
- 支持游戏元信息、文件 revision、平台目录、BIOS 和用户 DAT 的管理。
- 支持 `fceumm`、`snes9x`、`gambatte`、`mgba`、`mame2003_plus`、`mame2003`、`fbneo`、`dosbox_pure`。

一期不包含：

- 注册、登录、权限隔离和独立用户账户。
- 移动端及 Chrome 之外的浏览器兼容性承诺。
- 公网匿名开放。
- 异地联机房间、匹配、聊天、信令或 TURN。
- Arcade Merged ROMset。
- 成就、评分、评论、推荐算法和社交关系。

“和朋友一起玩”在一期指共享站点，以及 EmulatorJS/core 本身支持的本地多人输入；不等于互联网实时联机。所有人暂时共享一个内置 `local` Profile、存档和游玩数据。

## 3. 关键产品与技术决策

### 3.1 平台目录决定默认核心

领域关系不是“游戏直接属于平台”，而是：

~~~mermaid
flowchart LR
    P["Platform 基础平台"] -->|创建| PI["PlatformInstance 平台目录"]
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
- PlatformInstance 在 UI 统一称为“平台目录”；它从 Platform 创建，并从该平台的启用核心中选择一个默认核心。
- Game 只能且必须由一个 PlatformInstance 持有，基础平台由此间接推导。
- Game 另以非空 current pointer 持有一个不可变 GameContentRevision，唯一决定普通启动的当前用户文件；改变平台目录默认核心不能改变内容版本。
- GameVariant 是 Game + Core 的稳定逻辑槽；某个 GameContentRevision 在具体 core artifact 上的兼容性、派生文件和依赖快照保存在不可变 GameVariantRevision。稳定槽有 READY 结果时只指向当前 READY revision；从未验证成功的备用核心槽允许 current 为空并保留失败诊断。
- 详情页在没有浏览器本地偏好时选择平台目录的默认核心，同时保留该基础平台其他核心供用户显式切换；浏览器可按游戏记住非默认选择，选回推荐核心即清除。偏好不修改服务端平台目录，也不做失败后的静默回退。

因此 Arcade 可创建“FBNeo 游戏列表 → FBNeo”“MAME 游戏列表 → MAME 2003”“FBNeo 飞行游戏 → FBNeo”。用户导入时选择平台目录，而不是直接选择基础平台。

### 3.2 DAT 与刮削是两条独立证据链

- `fbneo`、`mame2003`、`mame2003_plus` 各自使用核心专属 DAT。
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

## 4. 系统上下文

~~~mermaid
flowchart LR
    U["Chrome Desktop"] -->|HTTPS| N["前置 NG / TLS 终结"]
    N -->|HTTP：页面 / _next| W["retrom-web / Next.js + Player Shell"]
    N -->|HTTP：API / content / runtime| S["retrom / Go 模块化单体"]
    W --> R["浏览器内锁定版本 EmulatorJS（初始 4.2.3）"]
    S --> D["SQLite WAL"]
    S --> B["本地 SHA-256 CAS"]
    S --> J["SQLite 队列 + 进程内 Worker"]
    J --> A["Arcade DAT 解析器"]
    J --> H["Hasheous 哈希元信息查询"]
    R -->|同源启动资源 / 存档 / 心跳| N
~~~

部署与安全边界：

- 一期默认仅面向可信局域网；没有账户时，所有访问者都可使用管理功能。
- 两个应用只监听明文 HTTP；生产环境只向受信容器/主机网络开放，由前置 NG 终结 TLS 并提供 HTTPS。
- ROM、BIOS、存档等只通过短时 LaunchSession capability cookie 授权的同源端点提供；URL 只有非秘密 `launchId`，不暴露宿主路径或内容 hash。
- NG 必须让页面、EmulatorJS 与受控内容端点保持同源，并保留/设置正确的 COOP/COEP/CORP 响应头；DOSBox Pure 等线程模式依赖该安全上下文。
- EmulatorJS、core artifact 和 DAT 均锁定版本，不依赖浮动 CDN。

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
~~~

关键不变量：

- PlatformInstance 的默认核心必须是其基础平台已关联且启用的 Core。
- Game 不保存可为空的 `platform_id` 作为另一条归属路径，只保存非空 `platform_instance_id`。
- Game 的 `current_content_revision_id` 唯一决定普通启动内容；目录默认核心、CoreArtifact 或 DAT 变化不得反向改写它。
- 存档同时绑定 GameVariantRevision 与 CoreArtifact；不匹配时不得静默加载。
- BIOS 要求和 DAT 活动版本按 CoreArtifact 隔离；同一个 Blob 可以去重，但 Installation/校验状态不可跨 artifact 或核心串用。
- ImportJob/ImportItem 记录创建时的平台目录、默认核心、core artifact、DAT 和刮削证据快照；在途结果不因后续配置变化而漂移。
- 审核发布、平台目录移动、DAT 启用和文件 revision 切换必须可审计。

## 6. 平台、核心与初始平台目录

空库 bootstrap 必须在同一 migration/seed 版本中写入下表的基础平台、启用关系和初始平台目录；重复执行按稳定 code/slug 幂等，不生成重复目录。管理员之后可以创建、重命名或软删除空目录，但不得修改基础平台 code。

| 基础平台（稳定 code） | 启用核心 | 初始平台目录（slug）→ 默认核心 | 备注 |
| --- | --- | --- | --- |
| NES / Famicom (`nes`) | `fceumm` | NES 游戏（`nes-games`）→ `fceumm` | 普通卡带不要求 BIOS |
| Famicom Disk System (`fds`) | `fceumm` | FDS 游戏（`fds-games`）→ `fceumm` | 需要 `disksys.rom` |
| SNES (`snes`) | `snes9x` | SNES 游戏（`snes-games`）→ `snes9x` | 标准游戏通常不要求 BIOS |
| Game Boy / Color (`gbc`) | `gambatte`、`mgba` | Game Boy 游戏（`gbc-games`）→ `gambatte` | 两个 core 均可供本次启动切换 |
| Game Boy Advance (`gba`) | `mgba` | GBA 游戏（`gba-games`）→ `mgba` | BIOS 可选 |
| Arcade (`arcade`) | `fbneo`、`mame2003_plus`、`mame2003` | FBNeo 游戏（`fbneo-games`）→ `fbneo`；MAME 2003 Plus 游戏（`mame2003-plus-games`）→ `mame2003_plus`；MAME 2003 游戏（`mame2003-games`）→ `mame2003` | 每个核心使用独立 DAT |
| MS-DOS (`dos`) | `dosbox_pure` | DOS 经典游戏（`dos-games`）→ `dosbox_pure` | 启动前可选程序；需要线程模式 |

平台和核心是代码种子/版本化配置；平台目录是管理员可创建、重命名和调整默认核心的业务实体。平台目录不是标签或多对多收藏集。

## 7. 产品信息架构

用户侧左侧导航：

- 首页：有效游玩时长、继续游玩、最近游玩和最新添加游戏。
- 游戏库：搜索、平台/平台目录筛选和已发布游戏卡片。
- 我的存档：带截图的手动存档及快速继续。
- 管理后台：固定在底部，切换整套管理菜单。

管理后台左侧导航：

- 游戏入库
  - 导入游戏
  - 任务进度
  - 待审核
  - 审核历史
- 游戏管理
- 游戏目录
- BIOS 管理（包含“BIOS 文件”和“Arcade DAT 版本”视图）

“游戏入库”是可点击的父级总览；四个子项使用明确缩进并保持同级。进入子页时父项保留上下文高亮，当前子项使用强高亮。游戏详情不是左侧一级菜单。它只能从游戏库卡片、首页最近游戏或资源详情链接进入；进入时左侧仍保持“游戏库”上下文。存档的主按钮直接启动，标题/次要操作才进入游戏详情。

一期路由固定为：

| 页面 | 路由 |
| --- | --- |
| 首页 / 游戏库 / 存档 | `/`、`/library`、`/saves` |
| 游戏详情 | `/games/:gameId` |
| 持久 Player Shell | `/play/:launchId` |
| 游戏入库总览 | `/admin/imports` |
| 新建导入 / 任务进度 | `/admin/imports/new`、`/admin/imports/tasks` |
| 待审核 / 审核详情 / 历史 | `/admin/reviews`、`/admin/reviews/:itemId`、`/admin/reviews/history` |
| 游戏管理 / 详情 | `/admin/games`、`/admin/games/:gameId` |
| 游戏目录 | `/admin/platform-instances` |
| BIOS 与 DAT | `/admin/bios`、`/admin/bios/dats` |

完整页面状态、4K 密度和响应式上限见 [UI 与交互规范](./ui-specification.md)。

## 8. 核心业务流程

### 8.1 导入、刮削与审核

~~~mermaid
flowchart LR
    A["选择平台目录"] --> B["上传文件 / 目录"]
    B --> C["SHA-256 / CAS / 分组"]
    C --> D["Arcade DAT 依赖识别"]
    C --> E["Hasheous 元信息候选"]
    D --> F["人工审核"]
    E --> F
    F -->|通过| G["发布到平台目录"]
    F -->|不通过| H["Discard + 保留历史"]
~~~

上传完成不等于发布。任务可以部分失败、重试和重启恢复；审核人可换元信息候选、手工编辑字段、调整同基础平台内的平台目录，并查看 DAT 与 Hasheous 两类独立证据。审核结果和当时快照永久可回溯。

### 8.2 BIOS 与 DAT

普通核心的 BIOS 需求来自版本化静态目录；Arcade 核心的 machine/parent/BIOS 依赖从该核心活动 DAT 解析。管理页按平台和核心展示逻辑文件名、期望哈希、已安装 Blob 的实际哈希、来源和受影响游戏。

哈希不一致只警告，不禁止用户保存 BIOS；启动是否阻断由该固件对当前 content 是否适用、Requirement 的必需/可选模式和 installation 状态共同决定。Gambatte/mGBA 的可选启动 BIOS 还会从版本化 Requirement 合并上游要求的 core option，不能只挂载文件却忘记启用。Arcade 默认只把当前游戏库实际引用的缺失依赖标为阻断，完整核心目录中未被任何游戏使用的 BIOS 不标红。

### 8.3 直接启动与存档

所有入口共用启动编排器。正常点击后立即进入全屏 Player Shell、显示加载状态并自动运行；详情页显式选择的核心由当前浏览器按游戏保留，并在后续普通启动中继续显式提交，不反向修改平台目录默认核心。DOS 详情多一个启动程序选择框。存档入口不重新询问核心或程序，也不采用该浏览器偏好，而是恢复存档绑定的运行环境。

手动状态存档必须包含截图。有效游玩时长通过 PlaySession 心跳累计；页面后台、模拟器暂停和长时间失联不计入有效时长。

## 9. 数据与版本基线

- EmulatorJS 一期前端运行时锁定 `4.2.3`；core 和 DAT 必须记录实际 artifact 标识与 SHA-256。`mame2003` 暂用已验证的官方 4.2.1 core bundle 覆盖，精确边界见[核心运行时验证基线](./core-runtime-validation.md)，不得概括成“所有 core 都来自 4.2.3”。
- 真实 Arcade DAT 在开发、验收和镜像构建前物化到 `data/dat/emulatorjs/4.2.3/`；Git 只保存机器可读 manifest、`SHA256SUMS` 与物化脚本，不提交 50+ MiB payload。同步启动阶段只校验本地依赖并登记解析任务，Worker 可建立数据库索引，但任何启动阶段都不联网下载。
- SQLite schema 中业务时刻全部为 Unix 毫秒 `INTEGER`；禁止后续 migration 引入 TEXT 时刻字段。
- 用户上传内容、下载媒体、存档和截图进入运行时 CAS，不提交到代码仓库。
- 预置 DAT 不可变；用户 DAT 作为新的非活动 DatVersion 保存，经过解析、差异预览和显式启用后才影响新诊断。
- DAT 更新不静默改写已发布 GameVariant 的历史兼容性快照；重校验产生新结果并可追踪来源。

## 10. 一期实施阶段

本节只给产品级阶段；migration 顺序、不可倒置依赖和每阶段退出 Case 以[一期实施编排](./implementation-plan.md)为准。

### Phase 0：兼容性闸门

- 锁定 EmulatorJS 4.2.3 前端运行时与八个实际 core artifact（包括版本化覆盖），每个核心启动至少一个用户合法提供的测试游戏；当前 8/8 最小启动门禁已通过，证据见[核心运行时验证基线](./core-runtime-validation.md)。
- 验证直接启动、默认全屏、状态存档/截图、持久存档、有效时长心跳。
- 验证 FBNeo/MAME Split 与 Full Non-Merged 的 parent/BIOS 加载，及三个独立 DAT。
- 已确认 Hasheous 的 `POST /api/v1/Lookup/ByHash` 无凭证契约；自动测试使用 fake，上线前只做一次有界 smoke，不能依赖实时命中内容或把限流阈值写死。
- 现有 8/8 历史 smoke 已确认固定运行时在 Chrome 中 `crossOriginIsolated`、核心帧推进并进入可辨识画面；它不是产品启动编排的替代。DOSBox Pure 的同名旁置 `.conf [autoexec]`、`dosbox_pure_conf=outside` 与 EmulatorJS `EJS_externalFiles` 能力来自官方契约，产品生成的精确模板、直接进入所选程序、原 bundle 不复制及不安全路径阻断仍必须执行 `ACC-RUN-005`。

Phase 0 未通过时，不进入大规模业务实现。

### Phase 1：基础设施

- Go 模块化单体、SQLite migrations、统一错误、OpenAPI。
- Platform/Core 种子、PlatformInstance 约束与初始目录。
- 本地 CAS、受控内容端点、任务队列和 Next.js App Shell。
- 按[工程质量、Lint 与测试规范](./engineering-quality-and-testing.md)建立固定版本 lint、统一 Makefile、关键路径测试和 CI；补齐 `make dev` 本地进程编排，以及 `retrom`/`retrom-web` 的只构建镜像 targets；拒绝 SQLite TEXT 业务时刻字段。

### Phase 2：导入与管理

- 文件/目录导入、任务恢复、Hasheous 适配器、DAT 解析和审核历史。
- 平台目录、游戏 revision、BIOS 和用户 DAT 管理。
- DAT 差异预览、启用、回滚和批量重校验。

### Phase 3：用户侧与运行时

- 首页、游戏库、游戏详情和我的存档。
- 共用的一键启动编排器、全屏 Player Shell、启动预检和 EmulatorJS 集成。
- DOS 启动程序、错误恢复和核心临时切换。

### Phase 4：存档与稳定性

- 状态存档、截图、持久存档和 PlaySession。
- Blob GC、备份恢复、诊断导出和 Chrome E2E。
- 1280×800 最小桌面、2560×1440 与 4K 视觉回归。

## 11. 统一验收入口

一期所有验收流程、标准、固定夹具、证据要求和短时执行上限统一由 [一期项目验收规范](./project-acceptance.md) 维护。该文档中的 `ACC-*` Case 覆盖本文的全部一期范围；专题文档只解释设计和实现约束，不再维护另一份通过条件。

Agent 不得根据本总览自行省略或合并 Case，尤其不得把八个核心合成一个长时间运行任务，也不得用 soak、压力测试或无限等待代替统一规范中的确定性短时流程。

## 12. 已锁定边界与后续议题

以下决定均已进入一期基线，不再作为实施中的自由选择：使用 Hasheous 且不使用 ScreenScraper；DAT 只用于 Arcade 识别/依赖；Game 唯一属于平台目录；详情页不是一级导航；正常启动一步完成并默认全屏；数据库时刻统一 Unix 毫秒 `INTEGER`；所有访问者共享 `local` Profile 和管理权限；一期只支持 Arcade Split / Full Non-Merged ROMset，不支持必需 CHD 和 Merged ROMset；当前设计稿的现代复古、深色侧栏和紫色主操作色是视觉基线；前后端分别构建 `retrom`/`retrom-web` 镜像但构建不启动服务；`make dev` 只运行本地进程；TLS 只由前置 NG 终结。

异地网络联机、账户/权限、CHD 和 Merged ROMset 只能作为未来版本提案，必须新增设计、威胁模型、迁移和验收 Case；一期 agent 不得预留半实现入口或用占位逻辑宣称支持。

## 13. 评审入口与参考

- [打开可交互 UI 设计稿](./design/retrom-ui-review.html)
- [EmulatorJS Options](https://emulatorjs.org/docs/options/)
- [EmulatorJS Cores](https://emulatorjs.org/docs4devs/cores/)
- [EmulatorJS v4.2.3](https://github.com/EmulatorJS/EmulatorJS/releases/tag/v4.2.3)
- [Hasheous Applications and API](https://github.com/gaseous-project/hasheous/wiki/Applications-and-API)
- [EmulatorJS 4.2.3 Arcade DAT 基线](./arcade-dat-baseline.md)
