# Retrom 文档索引

Retrom 的规划文档按“总览 + 统一验收 + 领域专题 + 可执行数据基线”维护。总览只保留跨领域决策；字段与流程在对应专题维护一次，全部验收 Case 只在统一验收文档维护。

## 实施就绪结论

Migration 035 已使 Pegasus 审核发布的内容来源校验跟随 ReviewDraft 当前有效来源快照：补传 Parent ROM 或多盘内容后仍保留 `SERVER_PEGASUS_IMPORT` 来源审计，但不会因 manifest 已从 Pegasus 初始快照演进而拒绝发布。Migration 034 实例级游戏标签已经进入当前代码、OpenAPI 和生成物：管理员先建立共享标签，再在普通导入、Pegasus Collection、审核或游戏维护中引用；活动标签参与游戏搜索和展示，但不进入内容、运行时或存档协议。Migration 032 受限异地联机也已进入当前代码、OpenAPI、机器清单和生成物：默认关闭，启用后开放 EmulatorJS 4.2.3 的 FCEUmm/FBNeo 两个精确 core profile、同源房间和服务端中继 rollback；每个 profile 覆盖使用该锁定 artifact 的全部合格 READY 游戏，不按单个 ROM 建产品白名单。

一期基线、账户隔离升级、Saturn/yabause 多盘系统、服务器 BIOS 导入、Migration 028 Pegasus ROM 目录导入、Migration 029 Pegasus 管理诊断、Migration 030 Pegasus 审核交接、Migration 031 审核运行预览、Migration 033 截图人工放行和 Migration 037 快速审批已经落入代码、OpenAPI 和生成物。当前版本要求登录，区分 `ADMIN`/`USER`，每个账号拥有独立 Profile；旧的共享 `local` Profile 数据根不原地升级。部署者配置的只读 root 同时承载 BIOS 与 Pegasus 两种管理导入：前者按完整启用 catalog 逐项安装，后者按 `metadata.pegasus.txt` 扫描、显式 Collection 映射、复制与运行检查后生成普通审核事项。审核页可预览当前筛选范围，并把严格 `READY`、没有重复内容且没有活动补传的条目交给可恢复后台批次发布；截图人工放行、重复内容和其他需要判断的条目继续逐项处理。审核详情仍可在隔离子窗体中尽最大可能运行当前来源，核心真实启动后第 5 秒保存运行截图。详情页可在前台可见满两秒后静音播放当前 VIDEO，其他用户列表保持 cover-only。正式细节分别由数据、导入、HTTP、运维和 UI 专题维护。

| 检查面 | 状态 | 实施事实源 |
| --- | --- | --- |
| 一期范围、非目标与跨模块不变量 | 已锁定 | [`retrom-product-architecture.md`](./retrom-product-architecture.md) |
| 实体、状态机、migration 顺序与存储安全 | 已锁定 | [`data-model.md`](./data-model.md)、[`storage-and-database.md`](./storage-and-database.md)、[`implementation-plan.md`](./implementation-plan.md) |
| HTTP、上传、SSE、并发、凭据与诊断输出 | 已锁定；实施时按切片先写 OpenAPI | [`http-api-contract.md`](./http-api-contract.md) |
| EmulatorJS/core/DAT/BIOS 与真实兼容基线 | 已锁定；payload 按 manifest 物化 | [`dependency-management.md`](./dependency-management.md)、[`core-runtime-validation.md`](./core-runtime-validation.md) |
| 页面、直接启动、320px 起的响应式布局、横屏 Player、4K 与无障碍 | 已锁定 | [`ui-specification.md`](./ui-specification.md)、[`runtime-and-play-data.md`](./runtime-and-play-data.md) |
| 联机 allowlist、房间、SSE/WebSocket、rollback 与 Player 差异 | 已锁定；仅 FCEUmm/FBNeo 两个 core profile | [`dependency-management.md`](./dependency-management.md)、[`data-model.md`](./data-model.md)、[`http-api-contract.md`](./http-api-contract.md)、[`runtime-and-play-data.md`](./runtime-and-play-data.md) |
| 测试、CI、镜像与最终通过规则 | 已锁定 | [`engineering-quality-and-testing.md`](./engineering-quality-and-testing.md)、[`project-acceptance.md`](./project-acceptance.md) |

`api/openapi.yaml`、两端生成物、migration、Makefile 和应用代码是按垂直切片产出的实施资产，不是允许临场改变上述契约的待定设计。剩余外部条件包括依赖首次物化需要公网、部分产品 E2E 需要 `data/game/` 下的用户授权资源、生产需要前置 NG，以及外部分发需要许可复核；它们的阻塞/适用语义统一见实施计划第 6 节和验收规范，不构成产品决策缺口。

## 从这里开始

全新 checkout 先在仓库根目录执行 `make install-deps`。该入口统一安装固定 Go/Node/Web 工具链、物化 EmulatorJS/core/DAT/许可，并把 Playwright 锁定的官方 Chrome for Testing 缓存到被 Git 忽略的 `.cache/tools/`；之后 `make dev` 与 `make web-e2e` 也会各自确保所需子集存在，不依赖开发机预装 Chrome。

- [`../AGENTS.md`](../AGENTS.md)：项目级 Agent 实施铁律；任何代码、测试、迁移或正式文档变更都必须先遵守。
- [`retrom-product-architecture.md`](./retrom-product-architecture.md)：一期范围、关键决策、系统关系、业务流程和阶段计划。
- [`implementation-plan.md`](./implementation-plan.md)：不可倒置的实现依赖、migration 顺序、里程碑与退出门禁；后续 Agent 的落地路线图。
- [`project-acceptance.md`](./project-acceptance.md)：一期唯一验收事实源；包含覆盖全部领域的可执行、可复现、短时 Case、证据格式和最终通过规则。
- [`ui-specification.md`](./ui-specification.md)：页面信息架构、移动/平板/桌面响应式布局、交互状态、4K 规则，以及“一次点击、自动启动、默认全屏”的 UI 契约。

## 领域与实现专题

- [`platform-instance.md`](./platform-instance.md)：游戏目录（PlatformInstance）的唯一归属、默认核心、导入快照、数据库约束和生命周期。
- [`import-and-review.md`](./import-and-review.md)：文件/目录导入、Hasheous 哈希刮削、任务状态机、人工审核、Arcade Parent 与多盘缺盘补充、历史回溯。
- [`bios-and-arcade.md`](./bios-and-arcade.md)：BIOS 文件、哈希提示、服务器目录批量导入、核心专属 Arcade DAT、完整 machine/parent/BIOS 依赖闭包和 Parent ZIP 内容校验。
- [`runtime-and-play-data.md`](./runtime-and-play-data.md)：直接启动、全屏 Player Shell、移动横屏方向门禁、预检、EmulatorJS、DOS 启动程序、存档与游玩时长。
- 受限联机不建立第二份专题；manifest 归依赖专题，持久状态归数据专题，REST/SSE/WebSocket 归 HTTP 专题，rollback/Player 归运行时专题，页面归 UI 专题。
- [`favorites-and-collections.md`](./favorites-and-collections.md)：Profile 私有收藏、可重复加入的收藏夹、跨页面接入与统一验收入口。
- [`game-tags.md`](./game-tags.md)：实例共享、管理员维护的游戏标签，覆盖生命周期、关系并发、导入继承、搜索投影与页面接入。
- [`core-runtime-validation.md`](./core-runtime-validation.md)：核心运行时的实际产品测试覆盖、未覆盖边界、内容格式约束和 MAME2003 兼容覆盖。
- [`storage-and-database.md`](./storage-and-database.md)：SQLite Unix 毫秒 `INTEGER` 时间规范、表目录、本地 SHA-256 CAS、GC 和备份。
- [`data-model.md`](./data-model.md)：一期表字段、ID、枚举、不可变 revision、外键、索引和数据库级不变量的唯一数据字典。
- [`http-api-contract.md`](./http-api-contract.md)：认证/授权、Origin/CSRF、JSON/错误、乐观并发、分块上传、SSE、launch cookie、内容缓存和 route 的唯一 HTTP 细节契约。
- [`dependency-management.md`](./dependency-management.md)：EmulatorJS/core/DAT 的小型 manifest、构建前物化、离线校验、镜像 allowlist、许可与升级规则。
- [`backend-api-and-operations.md`](./backend-api-and-operations.md)：Go 模块、HTTP/API、后台任务、文件端点、双镜像、`make dev`、NG/TLS 边界、安全和部署。
- [`engineering-quality-and-testing.md`](./engineering-quality-and-testing.md)：Go/Next.js lint、统一命令、镜像构建 targets、关键路径测试、bug 回归固化与 CI 落地规范。
- [`arcade-dat-baseline.md`](./arcade-dat-baseline.md)：EmulatorJS 4.2.3、实际 core artifact、真实 Arcade DAT、SHA-256 和升级流程的精确绑定基线。

## UI 评审

- [`design/retrom-ui-review.html`](./design/retrom-ui-review.html)：包含认证、账户、用户管理、收藏、其他用户侧页面、管理后台和移动 Player 的统一可交互响应式 UI 评审稿。
- [`design/retrom-ui-review.fragment.html`](./design/retrom-ui-review.fragment.html)：可交互评审稿的可维护源文件；修改页面结构时先更新此文件，再重新导出 HTML 与快照。
- [`design/retrom-ui-library-4k.png`](./design/retrom-ui-library-4k.png)：4K 游戏库。
- [`design/retrom-ui-game-detail.png`](./design/retrom-ui-game-detail.png)：从游戏库卡片进入的游戏详情。
- [`design/retrom-ui-saves.png`](./design/retrom-ui-saves.png)：存档列表与直接继续入口。
- [`design/retrom-ui-favorites.png`](./design/retrom-ui-favorites.png)：收藏页、左侧 Rail、筛选与卡片布局。
- [`design/retrom-ui-favorites-folder-manager.png`](./design/retrom-ui-favorites-folder-manager.png)：单游戏 Folder 管理的稳定宽版弹层。
- [`design/retrom-ui-favorites-unfavorite-dialog.png`](./design/retrom-ui-favorites-unfavorite-dialog.png)：取消收藏的宽版影响确认框。
- [`design/retrom-ui-recent-4k.png`](./design/retrom-ui-recent-4k.png)：4K 最近游玩列表。
- [`design/retrom-ui-setup.png`](./design/retrom-ui-setup.png)、[`design/retrom-ui-login.png`](./design/retrom-ui-login.png)、[`design/retrom-ui-register.png`](./design/retrom-ui-register.png)、[`design/retrom-ui-reset-password.png`](./design/retrom-ui-reset-password.png)：账户公开入口的 1280×800 最小桌面快照。
- [`design/retrom-ui-account.png`](./design/retrom-ui-account.png)：账户设置。
- [`design/retrom-ui-admin-users-4k.png`](./design/retrom-ui-admin-users-4k.png)、[`design/retrom-ui-admin-user-drawer.png`](./design/retrom-ui-admin-user-drawer.png)、[`design/retrom-ui-admin-invitation-result.png`](./design/retrom-ui-admin-invitation-result.png)：用户管理、管理 Drawer 与一次性链接结果。
- [`design/retrom-ui-play.png`](./design/retrom-ui-play.png)：点击后自动启动的全屏 Player Shell。
- [`design/retrom-ui-play-portrait.png`](./design/retrom-ui-play-portrait.png)：竖屏内容按可用高度铺满的 Player Shell。
- [`design/retrom-ui-play-4k.png`](./design/retrom-ui-play-4k.png)：4K 下按视口高度放大的 Player Shell。
- [`design/retrom-ui-play-emulator-controls.png`](./design/retrom-ui-play-emulator-controls.png)：点击“模拟器设置”后出现的 Retrom 自绘工具栏。
- [`design/retrom-ui-play-debug.png`](./design/retrom-ui-play-debug.png)：Player 右侧实时调试信息面板。
- [`design/retrom-ui-home-mobile.png`](./design/retrom-ui-home-mobile.png)、[`design/retrom-ui-library-mobile.png`](./design/retrom-ui-library-mobile.png)、[`design/retrom-ui-game-detail-mobile.png`](./design/retrom-ui-game-detail-mobile.png)、[`design/retrom-ui-saves-mobile.png`](./design/retrom-ui-saves-mobile.png)、[`design/retrom-ui-favorites-mobile.png`](./design/retrom-ui-favorites-mobile.png)、[`design/retrom-ui-netplay-room-mobile.png`](./design/retrom-ui-netplay-room-mobile.png)：390×844 用户侧响应式基线。
- [`design/retrom-ui-admin-review-mobile.png`](./design/retrom-ui-admin-review-mobile.png)、[`design/retrom-ui-admin-review-detail-mobile.png`](./design/retrom-ui-admin-review-detail-mobile.png)：390×844 管理审核列表与四步详情基线。
- [`design/retrom-ui-play-portrait-mobile.png`](./design/retrom-ui-play-portrait-mobile.png)、[`design/retrom-ui-play-landscape-mobile.png`](./design/retrom-ui-play-landscape-mobile.png)：390×844 竖屏方向门禁与 844×390 横屏 Player HUD 基线。
- [`design/retrom-ui-admin-import-overview-4k.png`](./design/retrom-ui-admin-import-overview-4k.png)：4K 游戏入库父级总览。
- [`design/retrom-ui-admin-import.png`](./design/retrom-ui-admin-import.png)：2560×1440 文件/目录导入与配置快照。
- [`design/retrom-ui-admin-import-new-4k.png`](./design/retrom-ui-admin-import-new-4k.png)：4K 文件/目录导入与配置快照。
- [`design/retrom-ui-admin-import-tasks-4k.png`](./design/retrom-ui-admin-import-tasks-4k.png)：4K ImportJob 运行态与异常处置。
- [`design/retrom-ui-admin-review-4k.png`](./design/retrom-ui-admin-review-4k.png)：4K 待审核列表及封面、文件摘要。
- [`design/retrom-ui-admin-review-bulk-approval-4k.png`](./design/retrom-ui-admin-review-bulk-approval-4k.png)、[`design/retrom-ui-admin-review-bulk-progress-4k.png`](./design/retrom-ui-admin-review-bulk-progress-4k.png)：4K 快速审批影响预览与可恢复进度。
- [`design/retrom-ui-admin-review-detail-4k.png`](./design/retrom-ui-admin-review-detail-4k.png)：4K 审核详情合并工作台。
- [`design/retrom-ui-admin-review-attachment-4k.png`](./design/retrom-ui-admin-review-attachment-4k.png)：4K 缺失光盘完整集合上传 Drawer。
- [`design/retrom-ui-admin-review-validating-4k.png`](./design/retrom-ui-admin-review-validating-4k.png)：4K 补盘上传后的服务端校验中状态。
- [`design/retrom-ui-admin-review-ready-4k.png`](./design/retrom-ui-admin-review-ready-4k.png)：4K 多盘内容补齐并恢复发布能力的状态。
- [`design/retrom-ui-admin-review-compare-4k.png`](./design/retrom-ui-admin-review-compare-4k.png)：4K 最新刮削结果对比窗。
- [`design/retrom-ui-admin-review-history-4k.png`](./design/retrom-ui-admin-review-history-4k.png)：4K 不可变审核历史列表。
- [`design/retrom-ui-admin-review-history-detail-4k.png`](./design/retrom-ui-admin-review-history-detail-4k.png)：4K 审核完成瞬间的元信息快照。
- [`design/retrom-ui-admin-games-4k.png`](./design/retrom-ui-admin-games-4k.png)：4K 游戏管理列表。
- [`design/retrom-ui-admin-game-detail-4k.png`](./design/retrom-ui-admin-game-detail-4k.png)：4K 游戏管理详情的四区版本化工作台。
- [`design/retrom-ui-admin-tags-4k.png`](./design/retrom-ui-admin-tags-4k.png)、[`design/retrom-ui-admin-tags-mobile.png`](./design/retrom-ui-admin-tags-mobile.png)、[`design/retrom-ui-admin-tag-drawer.png`](./design/retrom-ui-admin-tag-drawer.png)：标签管理桌面/移动基线与创建 Drawer。
- [`design/retrom-ui-platform-directories.png`](./design/retrom-ui-platform-directories.png)：4K 游戏目录管理列表。
- [`design/retrom-ui-platform-directory-create.png`](./design/retrom-ui-platform-directory-create.png)：新建游戏目录 Drawer。
- [`design/retrom-ui-bios-files.png`](./design/retrom-ui-bios-files.png)：BIOS 文件管理。
- [`design/retrom-ui-bios-entry-compare.png`](./design/retrom-ui-bios-entry-compare.png)：Arcade BIOS 的 DAT/ZIP 条目对比。
- [`design/retrom-ui-server-import.png`](./design/retrom-ui-server-import.png)、[`design/retrom-ui-server-import-drawer.png`](./design/retrom-ui-server-import-drawer.png)、[`design/retrom-ui-server-import-detail-4k.png`](./design/retrom-ui-server-import-detail-4k.png)：服务器 BIOS 导入首页、目录选择 Drawer 与 4K 结果解释。
- [`design/retrom-ui-pegasus-import.png`](./design/retrom-ui-pegasus-import.png)、[`design/retrom-ui-pegasus-import-drawer.png`](./design/retrom-ui-pegasus-import-drawer.png)、[`design/retrom-ui-pegasus-import-detail-4k.png`](./design/retrom-ui-pegasus-import-detail-4k.png)：BIOS/Pegasus 双能力总览、Pegasus 三步 Drawer 与任务详情。
- [`design/retrom-ui-dat-versions.png`](./design/retrom-ui-dat-versions.png)：Arcade DAT 版本管理。

## 维护规则

- `td/` 是临时工作目录，不是正式事实源；`docs/` 不得链接或依赖其中任何文件。临时方案合入时必须按领域职责拆入正式文档，并把视觉内容合并到统一设计源，而不是保留平行稿。
- 跨领域产品决策先更新总览，再更新受影响专题。
- 字段、状态机、API 和页面细节只在负责该领域的专题维护，总览仅链接和摘要。
- 所有项目验收流程和通过标准只在 `project-acceptance.md` 维护；专题文档只按 Case ID 回链，不得复制验收清单。
- `design/retrom-ui-review.fragment.html` 是 UI 源稿；`design/retrom-ui-review.html` 与 PNG 只从该源稿重新导出，禁止只改导出文件造成评审稿漂移。
- `data/dat/emulatorjs/<version>/manifest.json` 与 `SHA256SUMS` 是 EmulatorJS/runtime、Player adapter 描述、可选真实 DAT 和许可输入的机器事实源；当前 `4.2.3` 是 33-artifact 基础集合，`4.3.0-pre` 覆盖 DOSBox Pure、Genesis Plus GX Wide 与 Azahar，合并为 35 个 enabled core。前端 adapter registry 由 `make data-check` 双向核对；runtime、五份 DAT 与许可 payload/notice 由 `make prepare-deps` 物化并被 Git 忽略。
- `data/auth/password-blocklists/v1/manifest.json` 是 release 密码阻断列表及许可的机器事实源；10,000 行 payload 与许可原文由 `make prepare-deps` 校验物化并被 Git 忽略。
- `make install-deps` 是全仓初始化入口；Playwright 精确版本绑定的 Chrome for Testing 由 `make prepare-e2e-browser` 物化到 `.cache/tools/ms-playwright/`，稳定可执行入口为 `.cache/tools/retrom-chrome-for-testing`。这些测试工具不属于应用发布依赖，不进入镜像。
- `data/game/` 是自动化测试所需本机授权 ROM、BIOS 和源归档的统一根目录，整体被 Git 与镜像构建忽略。需要私有资源的实际产品测试在自己的入口中锁定相对路径、大小或 SHA-256；不存在独立 example manifest 覆盖依赖或替代产品链路验证。`.dev-data/` 只作为 `make dev` 的服务器导入 root，不属于测试 fixture。
- `data/netplay/v1/manifest.json` 与 schema 是联机 core-profile allowlist 的唯一机器事实源；schema v2 锁定两个 profile 的 EmulatorJS 版本、core artifact SHA-256、协议/adapter/内容类型与帧上限，不包含单个 ROM 身份。它必须与依赖 manifest、前端 adapter registry 双向校验；代表性双端回归 ROM 位于被忽略的 `data/game/netplay/`，由 `scripts/acceptance/seed-netplay.py` 逐字节校验。
- 任何表示时刻的 SQLite 字段必须为 Unix 毫秒 `INTEGER` 并以 `*_at_ms` 命名。
- 根级 [`AGENTS.md`](../AGENTS.md) 是 Agent 实施铁律；详细质量规则只在 [`engineering-quality-and-testing.md`](./engineering-quality-and-testing.md) 维护。
