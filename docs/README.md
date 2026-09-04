# Retrom 文档索引

Retrom 的规划文档按“总览 + 统一验收 + 领域专题 + 可执行数据基线”维护。总览只保留跨领域决策；字段与流程在对应专题维护一次，全部验收 Case 只在统一验收文档维护。

## 实施就绪结论

当前数据库由 `001_identity.sql`–`011_emulationstation_import_liveness.sql` 的 clean migration lineage 建立最终模型；只支持向前升级，不提供降级或双读分支。运行时以 Provider Bundle 为唯一部署单元：EmulatorJS Provider 声明 35 个 Target，retrom-runtime Provider 声明 12 个 Target；Retrom 只保存已激活 Provider/Target、产品 Core binding 和不可变 contract digest，不保存或推导 Provider 私有 adapter/core 映射。所有 Product、Review Preview、RPG Runtime Validation、存档、多盘、沉浸与联机 Launch 都返回同一 `Launch Envelope V1`，Web 只通过共享 Provider dispatcher 装载 module 并取得 `PlayerRuntimeV1`。

全新数据库只 seed Platform/Core/关系等 reference catalog，PlatformInstance 初始为零；管理员在游戏目录页一键补齐推荐模板。RPG Maker 对用户仍是唯一 `rpgmaker` Core，服务端按项目证据绑定 `rpgmaker-2000` 至 `rpgmaker-mz` 七个 Provider Target；这些 Target 只用于不可变运行绑定和管理诊断，不进入用户 Core 选择器。FDS 归入 NES/FCEUmm，扩展名只由平台内容 profile 提供。Pegasus/EmulationStation、标签、收藏、Payload 生命周期与受限异地联机继续使用各自领域契约；八个联机 profile 绑定精确 Provider Target 和 `netplayCompatibilityLine`，不按单个 ROM 建产品白名单。

一期基线、账户隔离、Saturn/yabause 多盘系统、服务器 BIOS 导入、Pegasus ROM 目录导入与精确诊断、统一审核交接、审核运行预览、截图人工放行和快速审批都已落入代码、OpenAPI 和生成物。当前版本要求登录，区分 `ADMIN`/`USER`，每个账号拥有独立 Profile。部署者配置的只读 root 同时承载 BIOS 与 Pegasus 两种管理导入：前者按完整启用 catalog 逐项安装，后者按 `metadata.pegasus.txt` 扫描、显式 Collection 映射、复制与运行检查后生成普通审核事项。审核页可预览当前筛选范围，并把严格 `READY`、没有重复内容且没有活动补传的条目交给可恢复后台批次发布；截图人工放行、重复内容和其他需要判断的条目继续逐项处理。审核详情仍可在隔离子窗体中尽最大可能运行当前来源，核心真实启动后第 5 秒保存运行截图；进入发布、丢弃、跳过或不可恢复失败等终态后，审核历史只保留 ReviewEvent v2 的文字与结构化审计，工作流 CAS payload 由可恢复的 PayloadRelease 后台任务释放。管理员“永久删除游戏”保留 Game 与历史关系的文字墓碑，立即关闭运行能力并异步释放游戏内容、媒体、存档和运行时 payload；共享 Blob 继续受保护，独占 Blob 经 24 小时至 30 天的配置宽限期后由有界 GC 回收。详情页可在前台可见满两秒后静音播放当前 VIDEO，其他用户列表保持 cover-only。正式细节分别由数据、导入、HTTP、运维和 UI 专题维护。

上述审核 preview、第 5 秒截图和截图人工放行只适用于非 RPG Maker 条目。RPG Maker 项目使用正式 `RPG_RUNTIME_VALIDATION` Launch；管理员已主动发起并成功创建真实 Player 检查后即可“通过并发布”，不要求普通上传者逐项完成复杂校验。A→B 保存→C→不同 Launch 精确恢复到 B 与恢复后 `RESTORE_INPUT` 是可选的高级审核流程，同时仍是七核心自动化发布验收门禁；一旦选择执行，机器 gate 失败不能由截图 override。

标准手柄沉浸模式是独立于普通 PC/移动界面的电视交互面：普通首页既提供显式入口，也可由任意标准手柄
按键打开确认层。进入后先按“全部游戏、最近游玩、收藏游戏、我的存档”固定顺序展示 Profile 私有入口，
再列可见平台；用户可浏览收藏夹、从存档启动，并以 Y 快速切换默认收藏。浏览阶段循环播放内置 BGM，
Select 打开声音、全屏与退出系统菜单；单机 Player 中双击 Select+Start 打开“取消、创建存档、退出游戏”
菜单。该模式仍复用现有认证、Favorite/SaveState、Game/媒体、Launch 与 Core stage，不包含联机、搜索、
管理或普通全站手柄导航，也不让普通/联机 Player 继承其输入过滤。页面、API、adapter 与验收细节由 UI、
HTTP、运行时、依赖及统一验收专题维护。

| 检查面 | 状态 | 实施事实源 |
| --- | --- | --- |
| 一期范围、非目标与跨模块不变量 | 已锁定 | [`retrom-product-architecture.md`](./retrom-product-architecture.md) |
| 实体、状态机、migration 顺序与存储安全 | 已锁定 | [`data-model.md`](./data-model.md)、[`storage-and-database.md`](./storage-and-database.md)、[`implementation-plan.md`](./implementation-plan.md) |
| HTTP、上传、SSE、并发、凭据与诊断输出 | 已锁定；实施时按切片先写 OpenAPI | [`http-api-contract.md`](./http-api-contract.md) |
| EmulatorJS/core/DAT/BIOS 与真实兼容基线 | 已锁定；payload 按 manifest 物化 | [`dependency-management.md`](./dependency-management.md)、[`core-runtime-validation.md`](./core-runtime-validation.md) |
| Provider、RPG Maker 七世代 Target、项目导入、隔离运行时与通用检查点 | 已锁定；按 Provider/Target/contract digest 绑定 | [`import-and-review.md`](./import-and-review.md)、[`runtime-and-play-data.md`](./runtime-and-play-data.md)、[`dependency-management.md`](./dependency-management.md) |
| KiriKiri2 KAG 项目导入、审核试玩、运行与书签检查点 | 已接入；非 KAG 自定义 TJS 只保证形状识别，不声明存档兼容 | [`import-and-review.md`](./import-and-review.md)、[`runtime-and-play-data.md`](./runtime-and-play-data.md)、[`project-acceptance.md`](./project-acceptance.md) |
| GameMaker 项目导入与 Butterscotch Web runtime | 已接入基础闭环；`data.win` 只做候选识别，实际兼容由审核试玩与产品 Case 证明 | [`import-and-review.md`](./import-and-review.md)、[`runtime-and-play-data.md`](./runtime-and-play-data.md)、[`project-acceptance.md`](./project-acceptance.md) |
| TyranoScript 项目导入、隔离运行与语义检查点 | 已接入基础闭环；只在稳定等待标签开放存档，实际兼容由审核试玩与产品 Case 证明 | [`import-and-review.md`](./import-and-review.md)、[`runtime-and-play-data.md`](./runtime-and-play-data.md)、[`project-acceptance.md`](./project-acceptance.md) |
| 页面、直接启动、320px 起的响应式布局、横屏 Player、4K 与无障碍 | 已锁定 | [`ui-specification.md`](./ui-specification.md)、[`runtime-and-play-data.md`](./runtime-and-play-data.md) |
| 联机 allowlist、房间、SSE/WebSocket、rollback/lockstep 与 Player 差异 | 已锁定；八个精确 core profile | [`dependency-management.md`](./dependency-management.md)、[`data-model.md`](./data-model.md)、[`http-api-contract.md`](./http-api-contract.md)、[`runtime-and-play-data.md`](./runtime-and-play-data.md) |
| 测试、CI、镜像与最终通过规则 | 已锁定 | [`engineering-quality-and-testing.md`](./engineering-quality-and-testing.md)、[`project-acceptance.md`](./project-acceptance.md) |
| 本机 localhost 与 PFB 并行联调 | 已实施；共享网关只绑定回环地址 | [`backend-api-and-operations.md`](./backend-api-and-operations.md)、[`dependency-management.md`](./dependency-management.md)、[`project-acceptance.md`](./project-acceptance.md) |

以 `api/openapi.yaml` 为入口的 OpenAPI 领域文件集、编译期 Go 生成结果、须提交的 TypeScript schema、migration、Makefile 和应用代码是按垂直切片产出的实施资产，不是允许临场改变上述契约的待定设计。OpenAPI 的 route 与领域 DTO 位于 `api/domains/`，跨领域组件位于 `api/components/`；所有生成器只消费经过本地引用校验的统一 bundle。剩余外部条件包括依赖首次物化需要公网、生产需要前置 NG，以及外部分发需要许可复核；公开 `make web-e2e` 使用仓库自有的确定性 GBA、NES、SNES 与 Arcade ROM，不属于外部前置条件。八个联机 profile 均有双浏览器产品链路基线，SNES9x、Nestopia、MAME2003 Plus 与 FBA2012 CPS1/CPS2 另有单浏览器真实核心基线；这些结果只覆盖 manifest 锁定 artifact 和项目自有测试程序，不能外推到未登记核心、其他 artifact 或任意游戏内容。阻塞/适用语义统一见实施计划第 6 节和验收规范，不构成产品决策缺口。

## 从这里开始

全新 checkout 先执行 `make install-deps`，再用 `make prepare-deps && make deps-check` 物化并离线校验生产 Provider lock、DAT 与许可输入。普通开发执行 `make dev` 并访问 `http://localhost:4000`；并行跨仓联调使用根工作区的命名 PFB 流程，每个 PFB 拥有独立 Provider candidate、数据代际和 `.localhost` origin，共享网关只绑定 `127.0.0.1:3000`。`make dev` 与全部 `make pfb-*` 必须由当前普通用户执行。正式镜像只接收 production active descriptor；PFB 镜像只接收本 PFB 的 candidate descriptor，candidate 不能进入 production lock、release input 或正式镜像。Provider Bundle 的 manifest/module/assets 均逐字节校验；任何身份、hash、Target binding 或 active descriptor 漂移都在启动前 fail closed。

- [`../AGENTS.md`](../AGENTS.md)：项目级 Agent 实施铁律；任何代码、测试、迁移或正式文档变更都必须先遵守。
- [`retrom-product-architecture.md`](./retrom-product-architecture.md)：一期范围、关键决策、系统关系、业务流程和阶段计划。
- [`implementation-plan.md`](./implementation-plan.md)：不可倒置的实现依赖、migration 顺序、里程碑与退出门禁；后续 Agent 的落地路线图。
- [`project-acceptance.md`](./project-acceptance.md)：一期唯一验收事实源；包含覆盖全部领域的可执行、可复现、短时 Case、证据格式和最终通过规则。
- [`ui-specification.md`](./ui-specification.md)：页面信息架构、移动/平板/桌面响应式布局、交互状态、4K 规则，以及“一次点击、自动启动、默认全屏”的 UI 契约。

## 领域与实现专题

- [`platform-instance.md`](./platform-instance.md)：游戏目录（PlatformInstance）的唯一归属、默认核心、导入快照、数据库约束和生命周期。
- [`import-and-review.md`](./import-and-review.md)：文件/目录导入、Hasheous 哈希刮削、任务状态机、人工审核、Arcade Parent 与多盘缺盘补充、历史回溯。
- [`bios-and-arcade.md`](./bios-and-arcade.md)：BIOS 文件、哈希提示、服务器目录批量导入、核心专属 Arcade DAT、完整 machine/parent/BIOS 依赖闭包和 Parent ZIP 内容校验。
- [`runtime-and-play-data.md`](./runtime-and-play-data.md)：直接启动、全屏 Player Shell、移动横屏方向门禁、预检、EmulatorJS、`RetromRpgRuntime`、DOS 启动程序、通用检查点与游玩时长。
- 受限联机不建立第二份专题；manifest 归依赖专题，持久状态归数据专题，REST/SSE/WebSocket 归 HTTP 专题，rollback/Player 归运行时专题，页面归 UI 专题。
- [`favorites-and-collections.md`](./favorites-and-collections.md)：Profile 私有收藏、可重复加入的收藏夹、跨页面接入与统一验收入口。
- [`game-tags.md`](./game-tags.md)：实例共享、管理员维护的游戏标签，覆盖生命周期、关系并发、导入继承、搜索投影与页面接入。
- [`core-runtime-validation.md`](./core-runtime-validation.md)：核心运行时的实际产品测试覆盖、未覆盖边界、内容格式约束和 MAME2003 兼容覆盖。
- [`storage-and-database.md`](./storage-and-database.md)：SQLite Unix 毫秒 `INTEGER` 时间规范、表目录、本地 SHA-256 CAS、GC 和备份。
- [`data-model.md`](./data-model.md)：一期表字段、ID、枚举、不可变 revision、外键、索引和数据库级不变量的唯一数据字典。
- [`http-api-contract.md`](./http-api-contract.md)：认证/授权、Origin/CSRF、JSON/错误、乐观并发、分块上传、SSE、launch cookie、内容缓存和 route 的唯一 HTTP 细节契约。
- [`dependency-management.md`](./dependency-management.md)：EmulatorJS/core/DAT 与 RPG Maker 开源运行时的小型 manifest、构建前物化、离线校验、镜像 allowlist、许可与升级规则。
- [`backend-api-and-operations.md`](./backend-api-and-operations.md)：Go 模块、HTTP/API、后台任务、文件端点、双镜像、`make dev`、NG/TLS 边界、安全和部署。
- [`engineering-quality-and-testing.md`](./engineering-quality-and-testing.md)：Go/Next.js lint、统一命令、镜像构建 targets、关键路径测试、bug 回归固化与 CI 落地规范。
- [`arcade-dat-baseline.md`](./arcade-dat-baseline.md)：EmulatorJS 4.2.3、实际 core artifact、真实 Arcade DAT、SHA-256 和升级流程的精确绑定基线。

## UI 评审

- [`design/retrom-ui-review.html`](./design/retrom-ui-review.html)：包含认证、账户、用户管理、收藏、其他用户侧页面、管理后台和移动 Player 的统一可交互响应式 UI 评审稿。
- [`design/retrom-ui-review.fragment.html`](./design/retrom-ui-review.fragment.html)：可交互评审稿的可维护源文件；修改页面结构时先更新此文件，再重新导出 HTML。
- `web/scripts/capture-ui-design.mjs` 仍可按固定 viewport 在本地生成评审图片；`docs/design/.gitignore` 忽略所有常见图片格式，图片不提交 Git，也不作为正式文档依赖。

## 维护规则

- `td/` 是临时工作目录，不是正式事实源；`docs/` 不得链接或依赖其中任何文件。临时方案合入时必须按领域职责拆入正式文档，并把视觉内容合并到统一设计源，而不是保留平行稿。
- 跨领域产品决策先更新总览，再更新受影响专题。
- 字段、状态机、API 和页面细节只在负责该领域的专题维护，总览仅链接和摘要。
- 所有项目验收流程和通过标准只在 `project-acceptance.md` 维护；专题文档只按 Case ID 回链，不得复制验收清单。
- `design/retrom-ui-review.fragment.html` 是 UI 源稿；`design/retrom-ui-review.html` 只从该源稿重新导出，禁止只改导出文件造成评审稿漂移。本地生成的图片只用于即时评审，由 `docs/design/.gitignore` 忽略且不得被正式文档引用。
- 根目录 `README.md` 的项目展示图是上述本地评审快照规则之外的文档资产：只允许由同一 UI 源稿先运行 `node web/scripts/export-ui-review.mjs`，再运行 `node web/scripts/capture-ui-design.mjs --readme` 生成到 `docs/readme-assets/`，固定使用 `2560×1440` CSS viewport 与 `deviceScaleFactor=1.5` 输出物理 `3840×2160` PNG。图中标题、封面、数量和状态必须明确属于演示样例，不得作为生产 seed、测试 fixture、正式验收证据或产品数据事实源；`scripts/test_design_assets.py` 校验允许清单、README 引用和像素尺寸。
- active `emulatorjs` Provider Bundle 的 declaration 是 35 个 EJS Target、运行文件、能力和 checkpoint contract 的唯一机器事实源；`data/dat/emulatorjs/<version>/manifest.json` 只维护对应真实 DAT 的来源、目标 Target contract、SHA-256 和统计。前端不维护 adapter registry；所有运行入口只经 Launch Envelope V1 与共享 dispatcher。五份 DAT 与许可 payload/notice 由 `make prepare-deps` 物化并被 Git 忽略。
- `data/auth/password-blocklists/v1/manifest.json` 是 release 密码阻断列表及许可的机器事实源；10,000 行 payload 与许可原文由 `make prepare-deps` 校验物化并被 Git 忽略。
- `make install-deps` 是全仓初始化入口；Playwright 精确版本绑定的 Chrome for Testing 由 `make prepare-e2e-browser` 物化到 `.cache/tools/ms-playwright/`，稳定可执行入口为 `.cache/tools/retrom-chrome-for-testing`。这些测试工具不属于应用发布依赖，不进入镜像。
- `testdata/public-roms/gba-smoke/`、`testdata/public-roms/nes-smoke/`、`testdata/public-roms/snes-smoke/` 与 `testdata/public-roms/arcade-smoke/` 保存 Retrom 自有、MIT 许可且由同目录生成源确定性生成的产品 E2E 程序。生成二进制随仓库提交，`make public-fixtures-check` 与实际 HTTP/E2E 消费者共同锁定 bytes。NES 的两个独立内容身份分别覆盖 FCEUmm 与 Nestopia；SNES 夹具覆盖 SNES9x；Arcade 小型 DAT 由 acceptance-only 装置登记为 test-only `BUILTIN`，覆盖 MAME2003/Plus、FBNeo 与 FBA2012 CPS1/CPS2 的实际装配、核心运行和联机，但不替代 `ACC-DAT-004` 的 production DAT 验证。CPS2 锁定 core loader 要求单独提供 `spf2t.zip` 父归档；该父归档只有项目自有 marker，不含第三方 ROM，也不被驱动执行。自动化测试不读取操作者私有 ROM/BIOS，也不存在绕过产品链路的独立 example 或私有 fixture 根目录。`.dev-data/dev.mk`、`.dev-data/data` 与 `.dev-data/dev-state` 保存标准开发实例的配置、数据和启动状态，`.dev-data/bios` 与 `.dev-data/roms` 保存服务器导入语料；整个目录都不属于测试 fixture。
- `data/netplay/v2/manifest.json` 与 schema 是联机 profile 的唯一 Host 事实源；它只引用八个已激活 EmulatorJS Target 的 `netplayCompatibilityLine` 与 contract，锁定适用平台、内容类型、协议和帧上限，不复制 Provider 内部实现。`ACC-NP-014`–`022` 是八个锁定 profile 的真实双浏览器产品基线，不扩大 allowlist。
- active `retrom-runtime` Provider Bundle declaration 是 RPG Maker、ONS、KiriKiri、Butterscotch、TyranoScript 与 WASM-4 共 12 个 Target 的唯一机器事实源；Retrom 的 `data/runtime-target-bindings/v1/catalog.json` 只把产品 Core 绑定到精确 Target，不声明内部实现。Provider 源码、项目自有 bridge 与聚合发布 workflow 位于独立 `retrom-runtime`，第三方核心源码、构建与 Release workflow 位于各维护 fork。公开 RPG fixture 只有在来源、许可、确定性生成和真实产品消费者全部满足仓库夹具规则时才可进入 `testdata/public-roms/rpgmaker-smoke/`；不可分发输入仍只由对应当次 smoke 证明。
- 任何表示时刻的 SQLite 字段必须为 Unix 毫秒 `INTEGER` 并以 `*_at_ms` 命名。
- 根级 [`AGENTS.md`](../AGENTS.md) 是 Agent 实施铁律；详细质量规则只在 [`engineering-quality-and-testing.md`](./engineering-quality-and-testing.md) 维护。
