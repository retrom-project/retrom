# Retrom Agent 实施规范

## 1. 适用范围与优先级

本文件适用于整个 Retrom 仓库。子目录如果以后增加更具体的 `AGENTS.md`，只能在其目录范围内补充或收紧规则，不能降低本文件的质量与安全要求。

执行优先级依次为：用户与系统指令、距离目标文件最近的 `AGENTS.md`、`docs/` 中的正式契约、仓库已有实现与惯例。发现正式文档彼此冲突、文档与实现冲突或需求会破坏既有契约时，不得静默选择一种解释；先核对事实，再修正文档或向用户报告需要决策的冲突。

本文件只规定实施纪律，不复制产品规格。开始工作前按改动范围阅读：

- `docs/README.md`：文档地图与事实源；
- `docs/retrom-product-architecture.md`：跨模块边界和稳定不变量；
- `docs/implementation-plan.md`：实现顺序、migration 边界和里程碑退出门禁；
- `docs/engineering-quality-and-testing.md`：lint、测试、CI 和回归规范；
- `docs/project-acceptance.md`：统一验收 Case、证据格式和最终通过规则；
- `docs/data-model.md`、`docs/http-api-contract.md`：数据库与 HTTP 的唯一细节契约；
- `docs/dependency-management.md`：EmulatorJS/core/DAT 的物化、校验与镜像边界；
- 目标功能对应的领域专题文档。

## 2. 仓库边界

- Go 后端位于仓库根目录，入口放在 `cmd/retrom/`，内部实现放在 `internal/`，数据库迁移放在 `migrations/`。
- Next.js、React、TypeScript 与 Tailwind CSS 前端全部位于 `web/`。
- 根目录 `Dockerfile` 只构建后端镜像 `retrom`，`web/Dockerfile` 只构建前端镜像 `retrom-web`；镜像构建和服务启动是两个独立动作。
- `docs/` 保存可长期维护的正式契约；设计决策、行为或验收标准变化时必须同步更新。
- `data/dat/` 的 Git 内容只保存受版本约束的真实来源 manifest、SHA、DAT/许可物化配方与说明；约 53 MiB DAT、runtime、许可原文和生成 notice 由 `make prepare-deps` 写入被忽略目录，不得提交、手工改写或用 mock 替换。
- 第三方浏览器核心 fork 的分支与 tag 不在本仓库临场管理：进入 `xxxsen/Player`、
  `xxxsen/mkxp-z-libretro-emscripten`、`xxxsen/OnscripterYuri` 或
  `xxxsen/kirikiroid2-web` 工作前必须先读取该仓库根 `AGENTS.md` 和
  `retrom-fork.json`。Player `master`、mkxp wrapper `main`、ONS `master` 与
  KiriKiri `web` 只允许作为上游 fast-forward 镜像；Retrom 修改只能进入各 fork 当前默认的
  `retrom/<baseline>` 维护分支并从该分支打 `rpg-runtime-<baseline>-rN` tag。
  禁止把固定版本补丁合入移动的上游镜像、从镜像直接发布、恢复
  `retrom-web-*` tag，或新建 `runtime-clean` 等平行长期分支。
- 自动化测试不得读取或下载操作者私有 ROM、BIOS 与来源归档。可提交 fixture 目前只有 `testdata/public-roms/gba-smoke/`、`testdata/public-roms/nes-smoke/`、`testdata/public-roms/snes-smoke/`、`testdata/public-roms/arcade-smoke/`（含 MAME、FBNeo、FBA2012）与 `testdata/public-roms/rpgmaker-smoke/`：它们都必须同时具备项目所有权或明确再分发许可、仓库内唯一且确定性的生成源、固定并受测试锁定的完整 bytes、真实 Retrom 产品消费者，且不得包含第三方游戏、BIOS、密钥或上游二进制片段。RPG Maker fixture 只允许 Retrom 自有的 LCF/RGSS/项目数据与锁定的 MIT MV CoreScript；不得提交厂商 RTP、运行时、专有游戏或 ignored 的 MZ 官方样例。Arcade 的测试 BIOS 角色归档不包含第三方 BIOS且不被目标驱动执行。新增其他可提交二进制必须满足相同条件，不能借此提交第三方游戏或 BIOS。`.dev-data/dev.mk`、`.dev-data/data` 与 `.dev-data/dev-state` 保存标准开发实例的配置、数据和启动状态，`.dev-data/bios` 与 `.dev-data/roms` 保存 `make dev` 暴露给服务器导入功能的操作者语料；整个 `.dev-data/` 都不是测试 fixture。核心是否已接入必须通过 Retrom 实际导入、Launch、内容端点与 Player 链路验证，不得再建立绕过产品代码的独立示例页或私有 fixture 根目录。
- 不得提交凭据、launch capability/cookie、本机 `launch-capability.key`、用户主机绝对路径、专有游戏内容或来源不明的二进制文件。非秘密 `launchId` 不得被误当成授权凭据。

修改生成物前先找到唯一源文件，并从源文件重新生成。具体事实源以 `docs/README.md` 为准；禁止只改导出文件造成源稿、清单或快照漂移。

### 2.1 `td/` 临时设计文档目录

`td/` 只用于存放尚待实施的临时设计方案，不是正式契约、源码依赖或历史归档。其目录与生命周期必须满足以下规则：

- 每一份设计方案必须独占 `td/` 下的一个直接子目录，目录名固定为 `{3位数编号}-{设计文档描述}`，例如 `006-favorites-import`；编号必须恰为三位十进制数字，描述必须简短且能唯一识别方案。不得把设计文档以散落文件形式直接放在 `td/` 根目录，也不得让多个独立方案共用一个子目录。
- 每个方案子目录必须包含该方案的设计文档；存在 UI、交互或视觉设计时，同时在该子目录保存对应设计稿及其必要的本地评审资产。临时稿中的示例数据、图片和脚本不能因此成为生产事实源。
- `docs/`、源码、测试、配置、构建脚本、生成物和机器可读基线不得链接、导入、嵌入、读取或以路径依赖 `td/` 中的任何文档、设计稿或资产。正式内容也不得通过“详情见 `td/...`”规避自身完整性；仓库内任何可交付能力必须在不读取 `td/` 的情况下构建、运行、验证和维护。
- 实施某个临时方案前，先同时阅读该方案与适用的正式文档。若两者冲突，`docs/` 仍是现有契约事实源；不得静默用临时稿覆盖正式契约，必须在同一变更中明确解决冲突并更新对应唯一事实源。
- 实施完成时，必须按 `docs/README.md` 的文档职责，将稳定的领域边界、数据、HTTP、UI、测试和验收内容分别合并到现有正式文档。这里的“合并”是消除重复、解决冲突、保留有效决策并纳入既有结构，不是把临时 Markdown 原样复制或移动到 `docs/`，也不得在 `docs/` 中建立与现有专题平行的第二套事实源。
- 若方案包含设计稿，必须把其有效页面、组件、状态和交互合并到 `docs/design/` 的原始统一设计源，复用既有 App Shell、设计 token、通用组件与导出流程，并重新生成受影响的评审 HTML。图片快照只作为本地评审产物生成，由 `docs/design/.gitignore` 忽略，不得提交或被正式文档引用。不得把临时独立 HTML、CSS、图片集合或整套页面壳直接移动/复制进 `docs/design/` 充当正式设计源。
- 实施出的 UI 在视觉层级、布局、控件形态、图标、间距、弹层、状态、响应式与关键交互上必须与设计稿一致或接近一致，并在设计覆盖的 viewport 和交互状态下进行可重复验证。若正式契约、无障碍或技术安全要求迫使实现发生实质偏差，必须先同步修订统一设计源和 UI 正式契约、说明原因并补充验证；不得以“后续美化”交付明显偏离设计稿的界面。
- 当实现、测试、正式文档、统一设计源及其生成物全部闭环后，删除对应的 `td/{3位数编号}-{设计文档描述}/` 子目录。完成状态下不得继续保留临时稿作为隐藏参考，也不得留下任何指向该目录的仓库引用。

## 3. 默认工作方式

1. 先读适用指令、正式文档、现有代码和测试，再提出结论或修改。
2. 先检查工作树，保留用户已有及与当前任务无关的改动；不得顺手清理、回滚或格式化无关文件。
3. 明确受影响的契约、风险和验证范围，实施能完整解决问题的最小变更。
4. 业务行为变化时同时修改实现、测试和正式文档；不要把必要工作留成无归属的 TODO。
5. 新依赖必须有明确必要性，使用锁文件固定版本，并避免引入与现有能力重复的框架。
6. 不做未经请求的大规模重构、技术栈替换、数据迁移或破坏兼容性的接口改造。
7. 未获得命令结果、测试证据或可复现验证前，不得宣称完成。
8. 修改手写源码前先确认目标文件、函数、组件或 hook 是否满足正式质量文档的当前规模与复杂度门槛；预计超限时，在同一变更中按领域职责完成必要拆分。

## 4. 实现边界

### 4.1 Go 后端

- HTTP handler 负责协议解析、校验和错误映射；业务规则进入对应应用模块；SQL 与持久化细节留在存储层。
- 后台任务只负责编排、租约和重试，不复制领域规则。耗时哈希、网络访问、归档扫描和 DAT 解析不得占用长数据库写事务。
- 依赖方向遵循 `httpapi/jobs -> 应用模块 -> store/blobstore`。底层包不得反向依赖 HTTP、任务编排或进程入口。
- 错误必须保留原因并在边界映射为稳定错误码；不得静默吞错、依赖错误字符串分支或输出临时调试日志。
- 已发布数据库只能通过有序 migration 演进；运行时代码不得动态修补 schema。每个迁移都要覆盖新建库和旧库升级路径。
- SQLite 中表示业务时刻的字段必须为 Unix 毫秒 `INTEGER`，命名为 `*_at_ms`；Go/API 使用 `int64`。详细规则见存储专题。

### 4.2 Web 前端

- 路由与页面壳放在 `web/app/`，能力模块放在 `web/features/`，通用无业务状态组件放在 `web/components/`，API client 与纯逻辑放在 `web/lib/`。
- 复杂状态转换和可独立验证的计算必须从 JSX 中抽离并测试；组件不直接解析 DAT、不访问宿主路径，也不复制后端授权与兼容性规则。
- Server/Client Component 边界应明确；仅在需要浏览器 API、交互状态或客户端数据时使用 `"use client"`。
- 可访问性、键盘操作、加载/空/错误状态和 4K 桌面布局属于功能契约，不是交付后的美化项。

### 4.3 构建与部署

- `make dev` 只启动宿主机上的 Go 与 Next.js 开发进程，不得调用 Docker、Compose、容器镜像或容器网络。
- `make dev` 可以且必须先调用幂等 `make prepare-deps`；依赖准备结束后仍只启动宿主机进程。应用启动期只校验依赖，不自行联网下载。
- 全新 checkout 使用 `make install-deps` 初始化固定 Go/Node/Web 工具、应用依赖及缓存中的 Chrome for Testing；`make web-e2e` 必须自行依赖浏览器准备 target，不得要求开发机预装系统 Chrome。
- `make build-backend-image`、`make build-web-image` 和 `make build-images` 只构建/检查镜像；不得隐式执行 `docker run`、Compose、push、部署或修改运行数据。两个镜像必须使用依赖专题的同一 `io.retrom.release-input-sha256`，不得用 tag 相同冒充可组合证据。
- 默认镜像名固定为后端 `retrom`、前端 `retrom-web`。改变默认名称属于构建契约变更，必须同步正式文档。
- 两个应用进程只监听明文 HTTP。TLS 证书、TLS 握手、HTTP 到 HTTPS 跳转和 HSTS 由前置 NG/反向代理负责；不得在 Go 或 Next.js 应用内加入 TLS 终结能力。
- `retrom-runtime` 的开发联调不得以“先发正式 Release”作为前置。先在相邻的独立 checkout 中按其 `AGENTS.md` 完成回归和基础门禁，再用 `RETROM_RUNTIME_DEV_ROOT=/absolute/path/to/retrom-runtime make dev` 显式链接本地 `dist`；本地 override 必须使用被忽略的独立 Next distDir 并把该包作为显式 transpile/watch 输入，不能复用正式依赖的持久 bundle 缓存。不得为此修改 Retrom 的正式 manifest、package lock、route identity 或发布镜像输入。`retrom-runtime` 不管理或编译第三方核心；修改核心时必须进入对应 fork，按该 fork 的 `AGENTS.md` 生成并校验本地候选资产，再用 `RETROM_RUNTIME_DEV_RELEASE_OVERRIDES` 把 runtime ID 映射到候选输出目录。只有可删除的 fresh dev DB 才可同时设置 `RETROM_RUNTIME_DEV_INCLUDE_ASSETS=true`；此时本地 checkout 的下一待发布 package version 可以与当前正式 tag 不同，候选 bytes 只装入被忽略的当前正式路径并由 dev marker 记录，不能让同一正式 artifact identity 在有引用的数据库中对应不同 bytes。
- 本地 runtime 必须经过受影响的真实 Retrom 导入、审核预览、Launch、输入、checkpoint/恢复产品链后，才允许合并 runtime PR、打不可移动的 `v*` tag 和发布 Release；随后 Retrom 再以独立提交固定该 tag/commit/assets。正式 `deps-check`、镜像或发布门禁前必须运行 `make retrom-runtime-dev-unlink` 恢复锁定 Release，不能把本地 override 的 observed digest 当成发布证据。

## 5. 质量底线

未经用户明确批准，不得通过降低标准让检查通过，包括但不限于：

- 放宽、关闭或绕过 lint、TypeScript、测试、构建或安全规则；
- 添加宽泛的 `nolint`、`eslint-disable`、类型断言、`any` 或跳过测试来掩盖问题；
- 删除失败用例、弱化断言、更新快照以接受错误行为；
- 把当前变更引入的错误或 warning 描述成“已有问题”；
- 在主链路、迁移或数据安全仍未闭环时声明完成。
- 为新旧生产或测试源码建立存量 baseline、旧文件 allowlist 或“只禁止继续增长”的历史豁免；
- 使用结构性 `nolint`、前端 inline disable/ignore、伪造生成标记、压缩排版或把手写源码移入排除目录规避规模与复杂度门槛。

结构性 lint 不允许例外。安全或正确性规则确属工具误报、外部协议要求时，抑制必须精确到一个规则和最小代码范围，与机器可读中央 allowlist 的 symbol、理由、不变量和复审日期一致，并在交付说明中列出。生成代码和经确认的第三方产物可以使用集中、可审计的排除项；业务源码不能使用整目录兜底排除。详细阈值和例外 schema 只以 `docs/engineering-quality-and-testing.md` 与可执行配置为准。

## 6. 测试纪律

- 不设置单元测试覆盖率百分比门槛；覆盖率报告只用于发现风险，不代替测试设计。
- 关键路径中可分离的业务决策、状态转换、校验和计算必须有单元测试；集成/E2E/smoke 作为跨边界补充，不能替代这些单元测试。普通改动至少覆盖受影响的正常路径、错误路径和边界条件。
- 纯逻辑优先单元测试；SQLite、migration、HTTP 契约和跨模块事务使用集成测试；关键浏览器交互与核心运行兼容性使用经过 Retrom 导入、Launch、内容端点和 Player 的 Chrome E2E。
- 任意在开发自测、验收、评审或生产使用中发现的 bug，都必须留下能阻止同类问题再次出现的回归用例。修复前应先证明用例在旧行为上失败，修复后运行聚焦用例及受影响的完整测试集。
- 若浏览器权限、第三方运行时或不得提交的 ROM/BIOS 使普通自动化无法完整复现，仍须在最近的确定性边界增加自动化测试，并补充可执行 smoke 用例和机器可读验收记录；不得只留下文字说明。
- 测试必须可重复：常规测试不依赖真实外网、真实时间、随机执行顺序或用户本机状态；使用 fake clock、固定 seed、临时目录和独立 SQLite 数据库。
- 测试夹具只包含合法可分发内容。格式兼容性测试应同时覆盖小型确定性夹具与仓库中固定版本的真实 DAT 基线，不得用臆造数据冒充生产基线。
- 项目验收必须按 `docs/project-acceptance.md` 的 Case ID 和硬超时执行；不得临时合并 Case、复用历史截图，或加入 soak、压力、无限等待类验收。
- 测试源码不因属于 fixture、集成或 E2E 获得结构性豁免；拆分时使用稳定行为命名的 case、builder 和断言 helper，不得删除、skip、合并验收 Case 或弱化断言。

### 6.1 按影响面选择测试

测试范围由“本次修改改变了哪个生产边界及其实际消费者”决定，不由文件名、历史失败或“更保险”决定。先用
调用链、分支条件和契约确认影响面，再选择最近的单元/集成测试和精确验收 Case；不得无依据地把每次 UI 改动
扩大成全部 Core、联机或全仓 E2E，也不得用一次全量运行代替应补的聚焦回归。

| 修改场景 | 默认必须验证 | 何时扩大 |
| --- | --- | --- |
| Go 纯领域逻辑、parser、状态机 | 目标包单元测试 + 后端基础门禁 | 进入 SQLite/HTTP/后台任务事务时追加 integration |
| migration、SQLite 约束、事务、HTTP route/DTO/error | 聚焦集成测试 + `make integration-test`；API 变化再跑 generate/check | 跨多个应用模块或影响面无法证明时跑 `make ci` |
| 普通 Web 组件、排版、焦点、响应式 | 组件测试 + 前端基础门禁；有真实浏览器行为时跑对应 `ACC-UI/MOB/TAG/FAV/...` 精确 Case | 修改共享 App Shell、认证或全局样式时扩大到全部直接消费页面，不自动进入 Core/联机矩阵 |
| 沉浸模式入口、平台/游戏浏览、资料库、收藏、存档、音频、焦点、全屏恢复、菜单或返回导航 | `ACC-IMM-001`–`012` 中与改动直接对应的精确 Case；涉及真实启动/返回时覆盖对应 Player Case，并保留普通 UI 隔离 | 只有改到共享 adapter、Core 帧执行、runtime config/content 或联机分支时才追加 `ACC-RUN/SAVE/NP` |
| 普通 Player 外围 UI（工具栏、全屏、方向门禁、退出导航） | 对应 `ACC-RUN-002`–`004`、`ACC-MOB-*` 或领域精确 Case；共享分支需补普通/沉浸隔离 | 进入 iframe 装载、帧步进、state、输入 adapter 或内容装配时扩大到受影响 Core 产品 Case |
| Core adapter、EmulatorJS 版本、运行配置、ROM/BIOS/Parent、存档/多盘恢复 | 对应 `ACC-RUN-*`、`ACC-SAVE-*`、`ACC-MDISC-*` 与受影响 Core 的真实产品链 | 修改所有 Core 共享 adapter/loader、manifest 或无法枚举消费者时运行完整 `make web-e2e` |
| 联机房间/协议、canonical input、rollback/state transfer、联机 adapter | 对应 `ACC-NP-*` 聚焦测试；共享协议/controller 改动覆盖全部登记联机 profile | 普通或沉浸单机 UI 未进入联机分支时不得仅因复用 Player 文件而跑联机矩阵 |
| 导入、审核、媒体、删除、容量/GC | 对应格式和链路的 `ACC-PEG/ES/GAME/MEDIA/STOR-*`，以及相关 HTTP/SQLite/CAS 集成测试 | 只有修改共享导入/审核/ownership/release 基础设施时扩大到所有直接消费者 |
| 依赖 manifest、DAT、许可、镜像 | `data-check/prepare-deps/deps-check` 或 `build-images` | 物化结果进入 Player/Core 时再追加对应运行产品 Case |

Case 的步骤、硬超时和通过标准只以 `docs/project-acceptance.md` 为准；上表只负责选用场景，不复制验收规范。
执行聚焦浏览器验收使用 `make acceptance-case CASE=<Case ID>` 或文档登记的等价命令，并在交付中列出选择理由。
完整 `make web-e2e`/`make ci` 只用于发布门禁、CI、共享测试夹具/全局 setup 变化、跨多个上述领域的改动、
共享 adapter/loader/协议变化或无法可靠界定影响面。若运行中确认范围过大，应停止并清理该运行，改跑精确 Case；
人工停止的 Case 只能报告为 interrupted，不能记作产品失败或 PASS。

## 7. 必跑门禁

项目脚手架应实现 `docs/engineering-quality-and-testing.md` 规定的 Makefile 命令。涉及代码修改时按范围执行：

```bash
make quality-structure-check
```

该门禁适用于所有手写生产、单元、集成和 E2E 源码；失败不得以存量问题或不在当前 diff 为由跳过。

后端：

```bash
make fmt-check
make build
make test
make lint-go
```

前端：

```bash
make web-install
make web-lint
make web-typecheck
make web-test
make web-build
```

涉及 migration、SQLite 事务、HTTP 契约或跨模块主链路时：

```bash
make integration-test
```

影响 Player Shell 的浏览器交互、EmulatorJS 运行链路、core artifact、DAT、BIOS/Parent 装配或存档恢复时，还须按
第 6.1 节运行受影响的实际产品 Chrome E2E。默认先运行精确 Case，例如：

```bash
make acceptance-case CASE=ACC-IMM-006
```

只有第 6.1 节列出的全量条件成立时才运行 `make web-e2e`。该命令只代表其中已登记的真实产品场景；修改只影响
特定 core、平台或内容类型时，应运行该分支已有的精确产品集成测试。若受影响核心没有产品路径 E2E，不得用
独立 EmulatorJS 示例页冒充覆盖；必须在交付说明中明确未覆盖范围，并在最近的确定性产品边界补测试。

跨端改动或影响范围不确定时运行：

```bash
make ci
```

新增/修改 HTTP route、DTO、错误码或 client 调用时，必须先改 `api/openapi.yaml` 并运行 `make api-generate`。Go 生成物 `internal/httpapi/generated/api.gen.go` 由后端 build/test/lint/integration/dev 与镜像构建按需生成，必须被 Git 忽略且不得提交；TypeScript 生成物 `web/lib/api/generated/schema.d.ts` 必须提交。随后运行不会写工作树的 `make api-check`；禁止手改 generated 文件。

修改 Dockerfile、镜像内容、构建参数或发布资产时还必须运行：

```bash
make build-images
```

该命令成功只证明两个镜像可以构建，不得在验证过程中启动容器来改变服务状态，除非任务另有明确授权。

质量基础设施尚未落地时，引入首批业务代码的任务必须先补齐对应命令和 CI，不能以“命令不存在”作为跳过理由。纯文档改动不要求运行代码门禁，但必须检查链接、结构、事实源一致性和 diff。

## 8. 文档与数据维护

- 正式文档描述稳定行为、接口、数据约束、验证不变量和可重复命令，不记录普通开发运行产生的临时 PASS 输出、任务 ID 或本机路径。版本化依赖/核心兼容基线可以记录固定浏览器、artifact hash、解析统计和机器结果引用，但必须同时说明它只是该版本的历史证据，正式验收仍生成当次证据。
- 字段、状态机、API 或页面细节只在其负责的专题中维护一次；总览只保留跨领域摘要和链接。
- 可执行验收流程、标准、证据和 Case ID 只在 `docs/project-acceptance.md` 维护；领域文档只链接对应 ID，不得形成第二份清单。
- 修改机器可读 manifest、DAT、迁移或 API schema 时，同一变更中必须更新校验、测试和相应文档。
- 第三方 payload 不进入 Git。修改依赖 manifest、Player adapter registry、DAT/许可物化配方或 notice 规则时必须运行 `make data-check`、`make prepare-deps` 与 `make deps-check`，并验证 manifest/adapter ID/版本/实现双向一致，最终镜像只包含 allowlist、逐字节校验的许可原文和确定性 notice，而非下载 archive/缓存目录。不得让未知 EmulatorJS 版本回退到默认 adapter，不得把推断的 core source association 写成已证明精确构建源码，也不得把本地 image build 解释为外部分发授权。
- 第三方版本、core/DAT 哈希和兼容覆盖不得凭记忆填写；以仓库机器可读清单和可复现实验为准。

## 9. 安全与破坏性操作

- 文件上传、归档展开、CAS、运行时内容端点和路径处理必须防止目录穿越、符号链接逃逸、压缩炸弹和越权 Blob 访问，并有负向测试。
- 不记录 ROM/BIOS 内容、launch capability/cookie、完整宿主路径或上游敏感响应；可记录非秘密 `launchId` 用于关联诊断。
- 删除、覆盖、批量迁移或垃圾回收前必须精确解析目标并证明仍受引用保护；优先使用可恢复方案。
- 不得对用户工作树执行 `git reset --hard`、无范围删除或其他难以恢复的操作，除非用户明确授权。

## 10. 交付说明

每次交付必须说明：

- 改了什么以及为什么；
- 新增或更新了哪些测试；
- 实际运行了哪些检查及结果；
- 哪些检查未运行、原因和剩余风险；
- 是否同步了文档、migration、API 或机器可读基线。

只要必需检查失败、关键回归用例缺失或文档与实现仍冲突，就不得把任务描述为完成。若失败来自当前授权范围之外，应提供可复现证据并明确报告阻塞项。
