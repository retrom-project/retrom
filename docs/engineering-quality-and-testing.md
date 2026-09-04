# 工程质量、Lint 与测试规范

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期实施基线 |
| 版本 | 2.0 |
| 日期 | 2026-08-25 |
| 适用范围 | Go 后端、Next.js 前端、SQLite/XML 集成、WebSocket rollback 联机与 EmulatorJS/RetromRpgRuntime 运行时验证 |
| 质量原则 | 零 lint warning、关键路径有测试、每个已发现 bug 有回归用例、不设覆盖率百分比门槛 |

## 1. 文档职责

本文是 Retrom 工程质量的唯一详细基线，供后续 Agent 实施 lint、测试和 CI 使用。根级 [`AGENTS.md`](../AGENTS.md) 只保留必须遵守的行为铁律；全部可执行验收 Case 由 [`project-acceptance.md`](./project-acceptance.md) 统一维护，本文不重复 Case 流程。

本文参考了 Fireman 项目中已经使用的严格 Go/Next.js 门禁、固定工具版本、统一 Makefile 入口和测试纪律，但已按 Retrom 的模块边界、SQLite/CAS、EmulatorJS 与本地二进制约束重新整理。后续实施不依赖外部仓库，也不得直接复制其中的业务规则或包路径。

本文不替代实际配置文件；质量基础设施落地时必须按第 10 节建立配置、命令、CI 和首批测试，并以仓库中的可执行结果证明规范已经实现。

## 2. 目标与非目标

### 2.1 目标

- Go 和 Next.js 从第一批代码开始执行 lint，不积累“以后再清”的基线债务。
- 本地与 CI 使用同一组命令和固定依赖，避免“本机通过、CI 失败”。
- 通过分层测试保护关键业务、数据库约束、安全边界和浏览器交互。
- 将开发自测、产品验收、代码评审和实际使用中发现的每个 bug 固化为可重复用例。
- 让检查失败能指向真实问题；不以宽泛排除、弱断言或覆盖率数字制造虚假的安全感。

### 2.2 非目标

- 不设置全仓或单包的行、分支、函数覆盖率百分比门槛。
- 不要求每个简单 DTO、纯展示组件或生成文件都有单测。
- 不让默认 CI 依赖真实 Hasheous、用户 ROM/BIOS、浮动 CDN 或开发机预装的系统 Chrome。
- 不用单元测试替代实际 EmulatorJS/core 兼容性验证，也不用人工截图替代可自动断言的逻辑测试。

### 2.3 一期工具链基线

脚手架必须从以下已审定基线开始，并以锁文件为最终事实源：Go `1.26.5`；`modernc.org/sqlite v1.52.0`；`github.com/google/uuid v1.6.0`；`github.com/coder/websocket v1.8.15`；`oapi-codegen v2.8.0`；`github.com/oapi-codegen/nethttp-middleware v1.2.0` 与其直接使用的 `github.com/getkin/kin-openapi v0.142.0`；`gofumpt v0.11.0`；`goimports` 来自 `golang.org/x/tools v0.48.0`；Node `24.18.0`（`.node-version`）；npm `11.16.0`（`packageManager`）；Next.js 与 `eslint-config-next` `16.3.0`；React/React DOM `19.2.7`；Tailwind CSS 与 `@tailwindcss/postcss` `4.3.0`；TypeScript `5.9.3`；ESLint `9.39.0`；Vitest `4.1.8` 与 Vite `8.2.0`；`@playwright/test 1.61.1`（它锁定同版本 `playwright` runtime 及 Chrome for Testing `149.0.7827.55`）；`openapi-typescript 7.13.0`；`openapi-fetch 0.17.0`；golangci-lint `v2.11.4`。

前端测试配套固定为 `@testing-library/react 16.3.2`、`@testing-library/dom 10.4.1`、`@testing-library/user-event 14.6.3`、`@testing-library/jest-dom 6.9.1`、`jsdom 29.1.1`、`@vitejs/plugin-react 6.0.2`、`axe-core 4.13.0`、`postcss 8.5.26`、`@types/node 24.13.3`、`@types/react 19.2.18`、`@types/react-dom 19.2.4`。Vite 必须作为直接 devDependency，不能只依赖 Vitest 的传递依赖；`@testing-library/dom` 同理是 React Testing Library 的必需 peer；`axe-core` 作为 `ACC-UI-009` 的浏览器无障碍扫描器也必须直接锁定，不能依赖 Playwright 的传递安装。`package.json` 的全部直接依赖/devDependency 使用精确版本而非 `^`/`~`，`package-lock.json` 和 `go.sum` 必须提交；若首次 `npm ci` 证明某组合存在 peer incompatibility，必须作为独立工具链修订更新本节与锁文件，不能在功能 PR 中静默漂移到 `latest`。TypeScript 固定 5.9.3 是因为 `openapi-typescript 7.13.0` 的正式 peer range 为 `^5.x`；升级到 TypeScript 6 前必须先升级/验证生成器，不能使用 `--force` 或 `legacy-peer-deps` 绕过。

`openapi-typescript 7.13.0` 的 Redocly 工具链间接依赖统一由 npm override 固定为 `js-yaml 4.3.1`，用于排除 4.3.0 的 `!!omap` 二次复杂度安全问题；`npm ci` 后完整 `npm audit` 必须为零。移除 override 前必须先证明上游依赖已采用不低于 4.3.1 的版本并更新锁文件，不能让同名的旧嵌套副本重新进入工具链。

这些版本是一期起点，不是永不升级的承诺。若安全修复要求升级，必须在独立变更中更新版本文件/锁文件、兼容说明与完整门禁；Agent 不得在功能实现中自行改用 `latest`。

## 3. 统一命令契约

仓库根目录的 `Makefile` 是本地和 CI 的唯一高层入口。脚本可以作为 Make target 的实现细节，但 CI 不应重新拼装另一套命令。

| 命令 | 必须执行的内容 | 是否修改文件 |
| --- | --- | --- |
| `make install-deps` | 项目初始化：物化固定 Go 工具与模块、Node/npm 包、EmulatorJS/core/DAT/许可和 Playwright 锁定的 Chrome for Testing | 会写 `bin/`、`web/node_modules/`、`data/` 的忽略 payload 与 `.cache/` |
| `make prepare-go` | 精确版本的宿主 Go 已存在时直接复用，否则按 `go.mod` 版本和仓库固定 SHA-256 原子物化 Linux x86-64 工具链；损坏缓存自动隔离并重建 | 可能写被忽略的 `.cache/tools/` |
| `make prepare-node` | 按 `.node-version` 与固定 npm 版本校验仓库 Node 工具链；缺失或损坏时按官方 SHA-256 原子重建 | 可能写被忽略的 `.cache/tools/` |
| `make fmt` | 对 Go 源码执行 `gofumpt` 与 `goimports` | 是 |
| `make fmt-check` | 检查 Go 格式并输出 diff；存在差异即失败 | 否 |
| `make install-go-formatters` | 将固定 `gofumpt v0.11.0` 与 `goimports@v0.48.0` 安装到仓库忽略的 `bin/` | 会写工具缓存与 `bin/` |
| `make install-golangci-lint` | 将固定版本 golangci-lint v2 安装到仓库忽略的 `bin/` | 会写工具缓存与 `bin/` |
| `make prepare-e2e-browser` | 通过锁定的 Playwright CLI 幂等下载并启动校验官方 Chrome for Testing；缓存到 `.cache/tools/ms-playwright/`，稳定入口为 `.cache/tools/retrom-chrome-for-testing` | 会写被忽略的 `.cache/tools/` |
| `make quality-structure-check` | 运行结构检查器自身测试，再检查全仓手写源码行数与 suppression 策略 | 否 |
| `make build` | 按需生成被 Git 忽略的 Go API 文件，再构建 `./cmd/retrom` | 会写被忽略的 Go 生成物 |
| `make test` | 按需生成被 Git 忽略的 Go API 文件，再运行常规 Go 单元测试；默认不含 `integration` build tag | 会写被忽略的 Go 生成物 |
| `make lint-go` | 按需生成被 Git 忽略的 Go API 文件，再使用仓库固定版本的 golangci-lint v2 扫描源码和测试 | 会写被忽略的 Go 生成物 |
| `make backend-check` | `fmt-check + build + test + lint-go` | 否 |
| `make web-install` | 在 `web/` 执行 `npm ci`，只接受 `package-lock.json` | 会重建依赖目录 |
| `make web-lint` | ESLint 扫描全部受控 TS/TSX/JS，warning 视为失败 | 否 |
| `make web-typecheck` | `tsc --noEmit` | 否 |
| `make web-test` | `vitest run` | 否 |
| `make web-build` | 干净执行 Next.js production build；运行中的本地开发服务需要保留 `.next/` 时可显式设置 `NEXT_DIST_DIR=.next-build` | 只允许重建 `.next/` 或被忽略的 `.next-build/` |
| `make web-check` | `web-install + web-lint + web-typecheck + web-test + web-build` | 仅依赖/构建产物 |
| `make integration-test` | 按需生成被 Git 忽略的 Go API 文件，再运行 Go `integration` build tag：migration、SQLite、HTTP 与跨模块流程 | 会写被忽略的 Go 生成物 |
| `make api-generate` | 先把 OpenAPI 领域文件确定性合并为统一 bundle、以锁文件安装前端依赖，再生成被忽略的 Go models/server/spec 与须提交的前端 TypeScript schema | 会重建依赖目录并修改两端 generated 文件 |
| `make api-check` | 在临时目录用固定生成器验证 OpenAPI 和两端生成结果，逐字节比较已提交的 TypeScript schema，并拒绝 Go 生成物被跟踪或未被 ignore | 仅依赖产物 |
| `make web-e2e` | 先执行 `prepare-e2e-browser`，再用缓存中固定 Chrome for Testing 运行关键 Playwright 场景，包括项目自有 GBA/NES/SNES/Arcade 单机与双浏览器联机产品链路 | 会写浏览器缓存并产生本地报告 |
| `make public-fixtures-check` | 从仓库内唯一生成源重建公开 ROM/metadata fixture 到临时目录，逐字节核对 bytes、SHA-256、许可、三个 GBA 来源身份及真实产品消费者；不得读取私有 source | 否 |
| `make data-check` | 离线校验 Makefile/GitHub Actions 的 clean-checkout 依赖顺序、`docs/design` 图片不跟踪/不引用边界，以及已提交的小型依赖 manifest/SHA-256/DAT/许可配方 schema；CPS fixture 只与已提交的 source commit/hash/count 元数据对齐，不读取生产 DAT，无 payload 也通过 | 否 |
| `make prepare-deps` | 按固定 manifest 物化 EmulatorJS/core/五份 DAT/许可文件并生成 notice；两个 FBA2012 DAT 从锁定源码确定性原生生成两次；正确缓存不联网；完成后执行 `deps-check` | 会写被忽略的依赖缓存 |
| `make deps-check` | 完全离线校验本地 allowlist、core、DAT、override、许可输入/notice、DAT 统计，以及 CPS fixture ROM 布局与已物化生产 DAT 的逐项一致性 | 否 |
| `RETROM_RUNTIME_DEV_ROOT=/abs/path make retrom-runtime-dev-link` | 构建并链接相邻 `retrom-runtime` checkout 的 library；默认保留正式 core/bridge bytes。联调 fork 核心时，先由对应 fork 生成候选目录，再设置 `RETROM_RUNTIME_DEV_RELEASE_OVERRIDES` 和 `RETROM_RUNTIME_DEV_INCLUDE_ASSETS=true`，且只允许 fresh dev DB | 会改被忽略的 `web/node_modules`，显式含 assets 时还会改 RPG runtime 开发缓存 |
| `make retrom-runtime-dev-unlink` | 移除本地 runtime override，以固定 manifest 重新物化 aggregate Release 并恢复锁文件声明的 Web package | 会重建被忽略的依赖目录 |
| `make release-input-digest` | 离线计算依赖专题规定的源码/依赖发布输入指纹，stdout 只输出 64 位小写 SHA-256 | 否 |
| `make ci` | `quality-structure-check + api-check + backend-check + web-check + integration-test + data-check` | 仅依赖/构建产物与被忽略的 Go 生成物 |
| `make dev` | 先生成被忽略的 Go API 文件并执行 `prepare-deps + web-install`；设置绝对路径 `RETROM_RUNTIME_DEV_ROOT` 时再应用显式本地 runtime link，随后在宿主机启动 Go/Next.js 并统一处理退出信号；不使用 Docker | 会写本地依赖/开发数据缓存与被忽略的 Go 生成物 |
| `make pfb-init/validate/status` | 确定性建立或只读检查 PFB ID、严格 spec、registry、worktree、工具链、Chrome、workspace 与 dev provider revision；不操作 Git、不启动容器 | `init` 写被忽略的 `.pfb/` 与 owner-only全局 registry，其余只读 |
| `make pfb-build` | 仅在工具链或 package/API 生成输入变化时准备开发镜像、Node/Go依赖与生成代码；不构建 core、Provider archive、candidate tar或生产镜像 | 写 `.pfb/workspace` 中的可复用开发cache；相同输入幂等复用 |
| `make pfb-up/use/restart/down/status/logs` | `up` 只执行 Compose `--no-build`，`restart` 只重启 app；管理共享 loopback网关、选择和状态，不运行 `npm ci` 或切换数据 | 写 PFB状态/日志并管理开发容器；workspace、旧卷、URL均保留 |
| `make pfb-core-build` | 只构建 `CORE=<id>` 精确指定的 core worktree；绝不由 init/build/up/restart 隐式调用 | 写该 PFB workspace 的 core build输出 |
| `make pfb-verify` | 执行 `ACC-PFB-*` 基础设施及受影响产品 Case，把网络、workspace、provider revision和结果写入当前 PFB证据目录 | 只写 `.pfb/evidence/` 与验收临时状态 |
| `make pfb-migrate-storage/data-reset/remove/destroy` | 只在停止态和 exact `CONFIRM=<pfb-id>` 下迁移旧卷、归档重置数据、移除注册或销毁 `.pfb/`；迁移保留源卷，remove保留workspace | 是，范围由spec/registry/确认值共同限制 |
| `make build-backend-image` | 只构建 `retrom:${IMAGE_TAG}`，前后复核并标记 release-input digest | 只写本地镜像缓存 |
| `make build-web-image` | 只构建 `retrom-web:${IMAGE_TAG}`，前后复核并标记同一 digest | 只写本地镜像缓存 |
| `make build-images` | 以同一 digest 依次构建上述两个镜像，最后 inspect label 一致性 | 只写本地镜像缓存 |
| `make acceptance-prepare` | 按统一验收文档创建隔离的临时验收环境、固定 seed 与本次 run ID；不得读取/删除用户运行数据 | 会写 `.artifacts/acceptance/` 与临时数据根 |
| `make acceptance-case CASE=ACC-…` | 只执行一个已登记 Case，应用该 Case 的硬超时并写机器结果/证据 | 只写本次验收证据与临时状态 |
| `make acceptance-report` | 只聚合本次已有 Case 结果并按统一规则判定，不补跑、不篡改结果 | 写最终报告 |

补充规则：

- `make ci` 包含全部可复现的仓库内单元、集成与数据检查；没有合法公开 fixture 的核心启动兼容性不在自动化测试中冒充已覆盖。
- 全新 checkout 的统一初始化入口是 `make install-deps`。它允许在测试或服务启动前联网下载锁定依赖；正确缓存后 `prepare-go`、`prepare-node`、`prepare-deps` 与 `prepare-e2e-browser` 均幂等复用。Go/Node 工具链、浏览器缓存和运行时 payload 不进入 Git 或镜像；固定版本的宿主 Go 可由 `auto` 模式直接复用，PFB 镜像中的固定工具链使用 `system` 模式。
- 自动化测试不得读取操作者私有 ROM/BIOS。可提交 ROM/项目必须由项目所有或有明确再分发许可、保留可审查的唯一生成源，并由 `data-check`、`public-fixtures-check` 和实际产品消费者共同逐字节校验；当前实例是 `testdata/public-roms/gba-smoke/`、`testdata/public-roms/nes-smoke/`、`testdata/public-roms/snes-smoke/`、`testdata/public-roms/arcade-smoke/` 与 `testdata/public-roms/rpgmaker-smoke/`。RPG Maker 目录只含 Retrom 自有生成内容和清单锁定的 MIT MV CoreScript；ignored MZ 官方样例不属于可提交 fixture。
- `make ci` 默认不构建容器镜像；Dockerfile、镜像内容或发布资产变化时，在 PR 验证中额外执行 `make build-images`。tag 发布流水线不重复运行 PR 的 quality job，只执行自身的双镜像构建、输入校验和推送。
- Go package 列表应显式覆盖 `./cmd/...`、`./internal/...` 和 `./migrations/...`，避免未来 `web/node_modules` 或本地数据目录中的意外 Go 文件污染 `./...`。根 `migrations` 是可导入的 Go embed package，SQL 与 `embed.go` 同目录，不能依赖运行容器中另有源码目录。
- OpenAPI 固定为以 `api/openapi.yaml` 为入口的领域文件集（项目协议基线为 OpenAPI 3.0.3；锁定的 `oapi-codegen v2.8.0` 虽支持 3.1，但不得在普通实现任务中变更规范方言）。`scripts/openapi-bundle` 只允许解析 `api/` 内的本地相对引用，保留入口声明顺序和内部 component identity，并生成被忽略的 `.cache/generated/openapi.bundle.yaml`；两端生成器和内嵌运行时规范必须消费该同一文件。Go 侧由该版本分别生成同一个 `generated` package 下的 `models.gen.go`、`server.gen.go` 与 `spec.gen.go`，分别承载 DTO、strict stdlib server/router 和内嵌规范；三者均被 Git 忽略且不得提交，由标准后端 build/test/lint/integration/dev target 和后端镜像构建在编译前按需生成。生成配置放在 `api/codegen/`。请求验证固定 `nethttp-middleware v1.2.0`，另加 HTTP 专题的重复 JSON key/未知 query lexical guard。前端 `web/package.json#scripts.api:generate` 固定从上述 bundle 生成单一 `lib/api/generated/schema.d.ts`，并用 `openapi-fetch 0.17.0` 封装同源 client；该 TypeScript schema 必须提交并由漂移检查逐字节比较。生成文件都不得手改；改用 OpenAPI 3.1 必须单独完成两端生成、validator 与 contract test 的契约迁移。
- `api-generate` 与 `api-check` 必须直接依赖 `web-install`，保证全新 checkout 在调用 `npx --no-install` 前已通过 `package-lock.json` 物化精确版本；不得依赖开发机残留的 `web/node_modules`，也不得允许 npx 临时下载缺失包。`api-check` 必须在临时目录生成 Go 文件，不能依赖或改写工作树中的被忽略副本；同时检查该路径仍被 ignore 且不在 Git index。`data-check` 的 Makefile 回归用例必须锁定这些依赖与跟踪边界。
- Makefile 固定 `GOFUMPT_VERSION=v0.11.0`、`GOIMPORTS_VERSION=v0.48.0` 与 `GOLANGCI_LINT_VERSION=v2.11.4`，都安装到仓库内忽略的 `bin/`；`fmt/fmt-check` 只调用本地 formatter，`lint-go` 只调用本地 golangci-lint，不得调用浮动的 `@latest` 或依赖开发机全局版本。安装命令精确为 `go install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)`、`go install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION)` 和 `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)`；版本 sentinel 必须核对二进制报告值，已有错误版本不能因文件存在而复用。
- Go 版本以 `go.mod` 为事实源，`.golangci.yml`、仓库 Go 归档 SHA-256 与 CI 必须一致。Node 版本以 `web/package.json#engines` 和仓库版本文件为事实源，CI 不得另选一个未记录版本。仓库工具链只能先下载到临时目录、完成版本与完整性校验后再原子发布；已存在但校验失败的缓存必须自动重建，不能要求操作者手工删除。
- Web 统一使用 npm，必须提交 `web/package-lock.json`；CI 使用 `npm ci`，不得用会改锁文件的 `npm install`。
- Makefile 固定 golangci-lint `v2.11.4`；升级必须显式修改变量和本节并运行完整门禁，不能在安装命令使用 `@latest`。
- Makefile 固定 `DOCKER ?= docker`、`BACKEND_IMAGE ?= retrom`、`WEB_IMAGE ?= retrom-web`、`IMAGE_TAG ?= latest`。默认输出必须是 `retrom:latest` 与 `retrom-web:latest`，同时允许调用者显式覆盖 tag 或完整镜像仓库前缀。
- 三个 image targets 只能调用镜像构建，不得依赖 `dev`，也不得执行 `docker run`、`docker compose`、push、登录 registry 或部署操作。
- `make dev` 必须先拒绝 real/effective UID 为 0 或带任一 sudo 调用标记的进程，再前置执行 `make prepare-go`、Go API 生成、`make prepare-deps` 与 `make web-install`，之后只能以当前普通用户运行宿主机的 `go run ./cmd/retrom` 与固定 `--webpack` 的 `npm run dev`（可以由 `scripts/dev.sh` 编排）。固定 bundler 是开发入口的可重复性要求：当前锁定的 Next/Tailwind 组合在 Turbopack PostCSS transform 中会生成无法解析的内部 `@vercel/turbopack/postcss` 引用，不能让标准开发入口因机器缓存不同而有时可用、有时 500。脚本必须正确转发 `SIGINT/SIGTERM` 并在任一子进程异常退出时结束另一进程；登记必须同时覆盖 supervisor 与两个独立 process group 的 PID/start ticks。启动前以仓库专用 PID/start ticks/工作目录/命令行身份安全停止并等待旧 dev supervisor；若 supervisor 被强制终止，则还要以登记的 process group/session 和子进程身份安全接管遗留 Go/Next.js。身份无法确认时只能失败，不能按端口或名称误杀其他进程；不得要求 Docker daemon。
- `make dev` 的默认网络基线是 `http://localhost:4000`、Next `127.0.0.1:4000`、Go `127.0.0.1:8080` 与 runtime `http://{launchId}.rpg.localhost:8080`。PFB 命令与 `make dev` 并列且不成为其依赖；共享 PFB 网关继续独占宿主 3000，普通开发独占 4000，两者必须能够同时运行。全部 `make pfb-*` 命令和直接 PFB CLI 同样拒绝 root/sudo，PFB 应用与共享网关容器都显式使用发起命令的普通用户 UID/GID。
- 本地自动化明确使用 `RETROM_MODE=test`，dev supervisor 将它转换为后端 CLI 的 `--mode=test` 后从 Go 子进程环境中移除，避免严格环境变量校验把前端编排变量误当作后端配置。测试模式只允许临时数据目录、固定 `test/test` 账号和显著 UI 警告；release 模式测试必须走 setup code，不得用测试账号旁路。

### 3.1 全仓源码结构门禁

所有 Git 已跟踪及尚未提交但未被 ignore 的手写新旧源码执行同一规则；不建立存量 baseline、旧文件 allowlist、“只禁止继续增长”或按本次 diff 跳过的历史豁免。`make quality-structure-check` 在完整 lint 和测试前快速失败，并由 `make backend-check`、`make web-check`、`make ci` 及 CI quality job 调用同一实现。

文件以格式化后的物理行计数，包含空行与注释；无末尾换行的最后一行仍计一行。硬门槛为：Go 生产文件 1,000 行、Go `*_test.go` 1,200 行、前端生产 `.ts/.tsx/.js/.jsx/.mjs` 600 行、前端测试与 `web/e2e/**` 800 行、手写 CSS 800 行。非阻断设计目标依次为 600、800、400、500、500 行。不得通过压缩代码、删除必要注释、合并语句、缩短可读命名或把逻辑机械移动到无职责的 `part1/part2/helpers` 文件规避计数。

结构检查器枚举 Git index 与未忽略新文件，校验上述边界、严格生成标记和允许路径、Go suppression 中央清单双向一致性，以及前端 inline disable/ignore 为零。它必须一次报告全部 `path:line` 违规并对空文件、无末尾换行、Unicode、CRLF、新文件、重命名、ignored 文件、伪生成标记、非法/过期/未使用 allowlist 和前端 suppression 建立确定性自身测试。生成物只有同时命中精确允许路径与严格生成标记时才可排除；普通源码不能靠目录或注释伪装生成物。

## 4. Go Lint 基线

### 4.1 配置原则

使用根目录 `.golangci.yml`，配置 schema `version: "2"`，`linters.default: none` 后显式启用规则，`run.tests: true`，超时初始设为 5 分钟。显式列表让升级新增默认规则时不会产生不可控漂移。

初始启用集如下：

| 类别 | Linters |
| --- | --- |
| 正确性与资源释放 | `bodyclose`、`contextcheck`、`copyloopvar`、`errcheck`、`errorlint`、`gocritic`、`govet`、`ineffassign`、`nilerr`、`nilnil`、`noctx`、`rowserrcheck`、`sqlclosecheck`、`staticcheck`、`unparam`、`unused` |
| 安全与 API 约束 | `asasalint`、`canonicalheader`、`fatcontext` |
| 错误契约 | `err113`、`wrapcheck` |
| 可维护性 | `dogsled`、`dupl`、`exhaustive`、`funlen`、`gocognit`、`gocyclo`、`interfacebloat`、`lll`、`makezero`、`misspell`、`nakedret`、`nestif`、`nonamedreturns`、`prealloc`、`revive`、`unconvert`、`whitespace` |
| 工程纪律 | `depguard`、`forbidigo`、`gochecknoinits`、`nolintlint`、`predeclared` |
| 格式 | `gofumpt`、`goimports`（放在 `formatters.enable`） |

当前阈值对所有手写生产和测试 Go 代码一致采用：

- `dupl.threshold = 120`；
- `funlen.lines = 150`、`funlen.statements = 100`；
- `gocognit.min-complexity = 25`；
- `gocyclo.min-complexity = 15`；
- `interfacebloat.max = 5`；
- `lll.line-length = 120`；
- `nestif.min-complexity = 4`；
- `misspell.locale = US`。

这些阈值不是鼓励把逻辑拆成无意义的小函数。`funlen`、`gocyclo`、`gocognit`、`nestif`、`dupl` 和用于掩盖业务表达过长的 `lll` 属于结构性规则，任何生产或测试源码都不得用 inline suppression 规避；事务原子性通过命名步骤共享同一 `*sql.Tx` 保持，编排顺序和状态机边界通过短函数与显式阶段结果表达。

### 4.2 必须显式配置的规则

- `nolintlint` 要求具体 linter 名、非空原因、禁止未使用的 suppress。结构性规则不可抑制；`errcheck`、`staticcheck`、`wrapcheck` 等正确性规则只有确属工具误报或外部协议强制要求时，才允许精确到单一表达式和单一 linter 的 suppression。项目不启用 `gosec`，也不得保留对应 suppression 或中央例外。
- 每个获准的非结构性 suppression 必须登记在 `quality/go-suppressions.json`，字段固定为 `path/line/symbol/linter/reason/invariant/reviewAfter`；源码与清单双向一致，未知字段、重复键、路径漂移、过期或未使用条目都使结构门禁失败。同一模式重复出现时优先收口到可测试 helper，不复制 suppression。
- `forbidigo` 禁止 `fmt.Print*` 及内建 `print/println`；运行时代码使用结构化日志，测试使用测试框架输出。
- `errcheck` 检查类型断言；不得通过全局排除忽略文件关闭、事务 rollback、响应体关闭或序列化错误。
- `errorlint` 开启 `%w`、错误比较和断言检查；可分类错误使用 sentinel 或类型，不依赖字符串匹配。
- `revive` 至少启用空 import、context 参数、错误命名/返回、early return、内建标识符覆盖、无效控制流和未使用参数等规则。
- 生成文件使用严格生成标记集中排除；`web/node_modules`、构建产物和本地数据目录不参与 Go 扫描。

### 4.3 架构依赖检查

模块路径确定后必须用 `depguard` 把以下方向写入 `.golangci.yml`，不能只靠代码评审记忆：

1. `internal/**` 不得导入 `cmd/**`；
2. `internal/store/**` 与 `internal/blobstore/**` 不得导入 `httpapi`、`jobs` 或上层功能模块；
3. `internal/arcadedat/**` 是解析与依赖图底层，不得导入 `httpapi`、`jobs`、`metadata`、`bios` 或 `catalog`；
4. `internal/httpapi/**` 可以调用应用模块，但 handler 不得绕过模块直接依赖具体 SQL 实现；
5. `internal/jobs/**` 可以编排应用模块，但应用模块不得反向导入 `jobs`。

如果后续目录布局有经过评审的变化，应先更新架构专题和本节，再修改 `depguard`；不得为了修复循环依赖直接删掉规则。

### 4.4 测试文件例外

`*_test.go` 只集中豁免 `dupl`、`err113`、`lll` 与 `wrapcheck`，用于清晰的表驱动夹具和失败消息；不得豁免 `funlen`、`gocognit`、`gocyclo`、`nestif` 或 `noctx`。测试函数同样遵守 150 行、100 statements、圈复杂度 15、认知复杂度 25 和嵌套复杂度 4，较大场景进入稳定行为命名的 `t.Run`、fixture builder 和断言 helper。

测试中仍必须保留 `errcheck`、`govet`、`staticcheck`、`rowserrcheck` 和 `sqlclosecheck`。测试也是受治理源码，不能用整类正确性检查豁免隐藏资源泄漏、无效断言或不稳定边界，也不能通过删除、skip、合并 Case 或弱化断言缩短文件。

## 5. Next.js Lint、类型与单测基线

### 5.1 ESLint

`web/eslint.config.mjs` 使用 ESLint flat config，并组合：

- `eslint-config-next/core-web-vitals`；
- `eslint-config-next/typescript`；
- TypeScript type-aware 规则，使用项目服务读取 `tsconfig.json`。

`web/package.json` 固定以下脚本契约：

```json
{
  "scripts": {
    "api:generate": "openapi-typescript ../.cache/generated/openapi.bundle.yaml -o lib/api/generated/schema.d.ts",
    "lint": "eslint . --max-warnings=0",
    "typecheck": "tsc --noEmit",
    "test": "vitest",
    "test:ci": "vitest run",
    "build": "next build",
    "test:e2e": "playwright test"
  }
}
```

项目追加规则至少包含：

- `@typescript-eslint/no-floating-promises`：异步调用必须 await、返回或显式 `void` 并解释；
- `@typescript-eslint/no-misused-promises`：事件处理和布尔上下文不得误用 Promise；
- `@typescript-eslint/consistent-type-imports`：类型使用 `import type`；
- `@typescript-eslint/no-explicit-any`：边界数据先作为 `unknown` 并解析；
- `no-console`：禁止 `log/debug/info`，只允许经说明的 `warn/error`；
- `eqeqeq`、`curly`、`prefer-const`；
- Next.js 自带的 React Hooks、Core Web Vitals、Image、Link 与 JSX a11y 规则。
- 生产源码 `max-lines=600`、`max-lines-per-function=250`；测试/E2E 分别为 800 与 350；两类都执行 `complexity=15`、`max-depth=4`。函数、method、React 组件、hook 和事件处理器均独立检查，不能把复杂度转移到未命名 hook。

全局 ignore 只能覆盖 `node_modules/`、`.next/`、`out/`、`build/`、`coverage/`、Playwright 报告、测试结果及 `next-env.d.ts` 等生成物。不得排除 `web/app/`、`web/features/`、`web/lib/`、`web/components/` 或测试源码。

flat config 必须设置 `linterOptions.noInlineConfig=true` 且 unused disable 为 error。手写前端源码禁止 `eslint-disable`、`eslint-disable-next-line`、`eslint-disable-line`、`@ts-ignore` 和 `@ts-expect-error`；确有第三方类型或框架误报时，只能在 flat config 中按精确文件与规则登记，不能排除业务或测试目录。lint 以 `--max-warnings=0` 运行，任何 warning 都是门禁失败。

### 5.2 TypeScript 与边界数据

- `tsconfig.json` 启用 `strict`，不通过降低严格度修复类型错误。
- HTTP 响应、URL 参数、本地存储和 EmulatorJS 回调都视为不可信边界；先校验再转换为领域类型。
- API 类型应来自稳定 schema 的生成物或单一手写事实源，禁止页面各自复制接口类型。
- `next build` 与 `tsc --noEmit` 都必须运行。构建成功不能替代独立 typecheck，ESLint 也不能替代二者。

### 5.3 Vitest 与 React Testing Library

初始测试栈采用 Vitest、`jsdom`、React Testing Library 和 `@testing-library/jest-dom`。`web/vitest.config.ts` 至少约定：

- 环境为 `jsdom`；
- setup 文件集中安装 matcher 与必要的浏览器 API fake；
- 测试匹配 `**/*.test.{ts,tsx}`；
- `@` alias 与 Next.js 项目一致；
- React/ReactDOM 去重，避免重复 runtime；
- 常规 CI 使用单次 `vitest run`，不进入 watch。

测试优先通过角色、可访问名称和用户行为查询元素，不依赖 Tailwind class、DOM 深层结构或大面积 snapshot。浏览器 API fake 必须模拟与用例相关的语义，不能简单返回永远成功从而掩盖全屏、可见性或计时问题。

`web/playwright.config.ts` 只能登记 Chrome 项目：所有项目统一 `use.channel="chrome"`，不以 Playwright 的 Firefox/WebKit 或无品牌 Chromium 结果冒充一期浏览器兼容性。默认浏览器是 `@playwright/test` 精确版本绑定的官方 Chrome for Testing，由 `make prepare-e2e-browser` 下载到仓库忽略的 `.cache/tools/ms-playwright/`，校验 manifest 版本和实际启动后以 `.cache/tools/retrom-chrome-for-testing` 稳定路径提供给所有项目。桌面项目继续承载既有流程，移动项目只承载响应式与移动 Player Case，避免把全部桌面矩阵无意义复制到每个视口。runner 必须记录实际 Chrome 版本；可显式传入 `RETROM_CHROME_EXECUTABLE` 覆盖默认路径，但 runner 必须先验证其版本标识确为 `Google Chrome`，不得指向无品牌 Chromium。浏览器缺失时错误必须指向 `make install-deps`，不得静默改用系统浏览器或另一个 channel。`@playwright/test` 是直接 devDependency；不能只安装底层 `playwright` 包后假定 `playwright test` runner 已被正确锁定。

## 6. 测试分层与目录

关键路径中的状态转换、校验、依赖计算、时间累计和请求构造必须先在确定性边界建立单元测试；集成测试和产品 E2E 用于证明各边界能正确组合，不能作为不拆分、不测试核心规则的理由。只有无法脱离真实边界才有意义的能力（例如 migration 执行本身）才以集成测试作为最低层。

| 层级 | 适用问题 | 建议位置与工具 | 默认 CI |
| --- | --- | --- | --- |
| Go 单元测试 | 状态机、哈希、依赖闭包、校验、时间累计、纯领域逻辑 | 与源码同包或 `_test` 包，`*_test.go` | 是 |
| Go 集成测试 | SQLite migration、事务、CAS 文件系统、HTTP 契约、跨模块流程 | `*_integration_test.go` + `integration` build tag | 是 |
| Web 单元/组件测试 | 页面状态、表单、路由 payload、错误映射、用户交互 | 源文件旁 `*.test.ts(x)` + Vitest/RTL | 是 |
| Chrome E2E | 路由联动、用户激活/Fullscreen、移动方向门禁、响应式与 4K 关键布局 | `web/e2e/` + Playwright Chrome | 按影响范围/发布门禁 |
| 产品运行时 E2E | 真实 Retrom 导入/Launch/内容端点/Player 是否能驱动 EmulatorJS 核心 | `web/e2e/` + `testdata/public-roms/` 项目自有 ROM | 按影响范围/发布门禁 |
| RPG Maker 产品 E2E | 七版本项目导入、Provider/Target、三 adapter、unique-origin、A→B→C→不同 Launch 恢复到 B 与恢复后 `RESTORE_INPUT` | `web/e2e/` + 合法确定性 fixture/操作者 MZ deployment | 发布门禁 |
| 联机协议与回归 | 房间/协议边界、安全拒绝、feature flag、容量、单机路径，以及八个精确 profile 的双浏览器核心与生命周期 | 聚焦 Go/Web 测试 + `ACC-NP-010`–`022` | 按影响范围/发布门禁 |
| PFB 基础设施 | ID/spec/registry、严格 Host、共享网关、双PFB隔离、持久workspace、loose provider revision、无隐式build、显式core、旧卷迁移、release拒绝与销毁确认 | Python/Node/Go单测 + Docker/Chrome `ACC-PFB-001`–`012` | PFB实现、runtime开发层或存储边界变更 |

命名要求：

- 测试名描述用户可观察行为或稳定不变量，不描述私有函数实现。
- Go 回归测试可使用 `TestRegression_<Area>_<Behavior>`；Web 用例标题以 `regression:` 开头或放入 `*.regression.test.ts(x)`。
- 有 issue/验收编号时可在注释中关联，但编号不能代替清楚的前置条件和断言。
- 表驱动测试的 case 名必须能直接定位失败输入；不得用 `case1`、`test2`。

## 7. 一期关键路径测试矩阵

“关键路径必须有测试”按以下矩阵执行。每个能力落地时，其可分离的核心规则必须有单元测试，并至少包含正常、错误和边界用例；跨层不变量还需集成测试。

### 7.1 后端与数据

| 关键路径 | 最低自动化要求 |
| --- | --- |
| 上传、SHA-256 与 CAS 去重 | 流式哈希、相同内容只建一个 Blob、并发写入、临时文件失败清理、原子发布、大小/哈希不一致拒绝 |
| 目录与归档导入 | 多文件分组、单 ROM/多文件游戏、Unicode 文件名、路径穿越、绝对路径、symlink、条目/展开大小/压缩比上限 |
| 多盘解析、导入与审核 | M3U UTF-8/BOM/LF/CRLF、basename/case-fold/重复/歧义、2–8 盘和总量、坏 CHD、递归多组/局部失败、缺盘占位、精确补传、snapshot 不可变、generation 4 证据；parser 保留 `FuzzParse` seed 且 fuzz 不得越权 I/O、panic 或无界分配 |
| 游戏目录与默认核心 | 默认核心必须属于基础平台；Game 只能有一个非空游戏目录；导入快照不随配置变化；非法跨平台移动拒绝 |
| 游戏维护 | 元信息/媒体 revision、重新刮削候选不自动覆盖、乐观版本冲突、文件替换不可变、失败替换回滚、软删除与引用保护 |
| 导入任务与审核 | 状态转换、幂等重试、租约到期恢复、部分失败、取消竞争、重复发布防护、审核快照与历史不可变 |
| Hasheous 适配 | 哈希请求与响应映射、未命中、超时、429/Retry-After、畸形响应、缓存；常规测试不得访问真实外网 |
| Arcade DAT | 对三个核心分别解析 machine/clone/parent/BIOS、依赖闭包、循环/缺失引用、活动版本隔离，并对固定真实 DAT 运行集成校验 |
| BIOS 安装与诊断 | 文件名/哈希匹配；哈希不符保存并警告；必需项缺失阻断；可选项缺失不阻断；不同核心状态不串用 |
| 启动预检与 capability | 默认核心与单次覆盖、必需依赖、DOS 程序、静态 BIOS schema v1 与 Arcade DAT schema v2 分流、Arcade 冻结 BIOS bundle、cookie capability hash/过期/范围/一次启动绑定、复制 launchId 无 cookie 拒绝、未授权 Blob 与路径逃逸拒绝、日志脱敏 |
| 多盘发布、Launch 与存档 | canonical playlist/ordered identity、artifact V3 digest、config discSet、playlist/Disc GET/HEAD/单 Range、跨 Launch/原名拒绝、当前盘存档与先切盘后恢复、替换失败保留旧 revision |
| 账户初始化与认证 | 数据库 `PENDING/COMPLETED` 及 context 映射、release setup code、test bootstrap、Argon2 参数、密码 blocklist、通用登录错误、session 轮换/过期/撤销、Origin/Fetch Metadata/CSRF、限流与可信代理 |
| 用户管理 | 邀请/重置 secret 单次显示且数据库不保存 secret/hash、角色和状态转换、ETag、本人保护、最后管理员保护、停用/删除级联撤销、离线 admin-reset 与 restore 安全栅栏 |
| 私有数据隔离 | 所有 Profile 派生列表/详情/写入按认证主体限定；跨用户 ID、cursor、Idempotency-Key、SaveState 和 Launch 探测均不泄露也不串写 |
| 收藏与收藏夹 | 名称 NFC/空白/case-fold 边界、收藏状态机、Folder 上限/version、批量边界和原子失败；卡片 E2E 锁定收藏前后相同的按钮/图标几何、居中位置及红色实心状态；current-schema 复合 owner FK、隐藏投影；每条 route 的 strict JSON/query、CSRF、cursor、ETag、幂等与两个 Profile 隔离 |
| 联机控制面与实时协议 | Room/Member/Session 全状态与非法边、core profile 准入（同 artifact 的不同 ROM 名称/大小/hash 均可选，错版本/artifact/content kind/READY/dependency 均拒绝）、profile canonical digest、2/3/4 occupied mask、乱序贡献与 neutral seat、seq/frame/int16/大小校验、租约/history、前三次真实 resync/第四次终局、slow peer/backpressure、prepare/restart/restore 收口；Hub 必须跑 race test，SQLite 不保存实时 state/input bytes |
| NG/代理边界 | 只信任 allowlist 代理的转发头、公开 origin 校验、伪造 `X-Forwarded-*` 拒绝、应用仅绑定 HTTP 且没有证书配置路径 |
| 存档与恢复 | 非空 checkpoint payload 必需、PRODUCT 截图可选且缺失时 API/UI 明确返回空预览、存档绑定 Provider Target 与 GameVariantRevision、兼容恢复、不匹配拒绝、旧 revision 被引用时 GC 保护；RPG runtime validation 的恢复证据截图仍是发布 gate 必需项 |
| RPG Maker 项目与运行时 | selected-core×signature outcome（含 RPG2K family-only）、LCF/INI/HTML/JSON/parser fuzz、路径/gencache 冲突、V2 fileset、pack match/ref protection、route uniqueness、validation 状态机、bootstrap ticket 一次消费、native bundle codec、checkpoint compatibility；恢复必须断言 A→B 保存→C→不同 Launch 的 map/坐标/变量回到 B |
| 游玩时长 | 心跳幂等、页面不可见/暂停不累计、失联上限、重复 finish、异常时钟、整数毫秒持久化 |
| SQLite migration | 空库 001–010 建表、当前有序前缀续跑、名称/checksum/gap/unknown/future 拒绝、重复启动、事务回滚、外键/索引、所有业务时刻列为 `INTEGER` |
| Blob GC/备份恢复 | 引用扫描、竞态保护、孤儿回收、仍被存档/任务引用的 Blob 保留、恢复后数据库与内容引用一致 |
| 已登记 CAS 容量分析 | registry 每条 `PROTECTIVE` 边与容量语义双向覆盖；保护集与 GC 共用；Archive 用途单向传播；长期用途优先/跨长期用途共享；同大小不同 Blob 不误去重；九类含零值且总量恒等；int64 溢出失败；存档/候选引用视图不与分类相加；ADMIN/USER/匿名、未知 query、脱敏、空库与读库失败 |

### 7.2 前端与浏览器

| 关键路径 | 最低自动化要求 |
| --- | --- |
| 游戏库到详情 | 卡片进入 `/games/:id`；详情不是一级菜单；筛选/搜索可恢复；加载、空、错误状态 |
| 收藏跨页面闭环 | 游戏库/详情/收藏页状态一致；全部/未分类/Folder URL 恢复；创建/重命名/删除、精确分类、批量与两秒 undo；两个账号隔离；1280/2560/物理 4K 150%、键盘/focus/ARIA/axe/reduced-motion |
| 详情启动 | 默认选中游戏目录核心；用户可作单次切换；一次点击创建 launch 并自动运行；正常路径无第二个 Start 按钮 |
| 默认全屏 | Fullscreen 请求发生在原始用户激活链；拒绝/刷新深链有恢复入口；阻断失败退出全屏并返回可修复错误 |
| 存档快速启动 | 首页、存档页和详情存档都直接启动；使用存档绑定环境，不重新询问核心或 DOS 程序 |
| 多盘导入与审核 | capability 隐藏/自动 mode/退回 STANDARD、递归目录预检、完整/缺盘/非法/ignored 计数、精确缺盘上传、Job resume/retry、审核刷新、管理详情和完整目录替换 |
| 容量分析 | BigInt IEC 格式化、精确 byte 可访问文本、loading/empty/initial error/refresh success/refresh failure 保留快照、九类固定顺序、范围说明、导航顺序，以及 320/768/1280/2560/物理 4K 的 overflow/axe |
| DOS 启动 | 程序列表、默认项、缺失选择校验和 launch payload；不能在浏览器端猜测可执行文件；4.3 thread core 的 7z/ZIP Worker 在生产 CSP 下完成无 `eval` 精确转换，源形状漂移 fail closed |
| 管理侧信息架构 | “游戏入库”为父级总览；导入、任务、待审核、历史同级缩进；父/子高亮和直接路由一致 |
| 认证与路由守卫 | 初始化、登录、邀请注册、重置、账户设置；匿名 returnTo、已登录认证页重定向、USER 后台 403、401 清除内存状态；secret fragment 立即清除且不进任何浏览器存储 |
| 用户管理 | 1280/2560/物理 4K 150% 表格、筛选、Drawer/焦点、本人/最后管理员禁用态、ETag 冲突、邀请/重置一次性 secret 对话框和确认流程 |
| 账户切换与 Player | 同一 Chrome profile 中 A 的平台图钉、DOS 偏好、查询缓存和 EJS IDBFS bytes 不得被 B 读取；无服务器保存时清除旧 IDBFS 路径 |
| Player 画面模式 | 单测锁定模式到 shader/CSS 合成策略的映射、默认“锐利像素”、未知偏好回退和用户命名空间，并证明 750ms 暂停期限不会丢弃随后在 5 秒窗口内完成的截图；真实 Chrome E2E 在 mGBA、MAME 2003、FBNeo 上确认默认 shader 关闭、`image-rendering: pixelated`、动画帧与零页面异常，并在 mGBA 回归“清晰增强”等模式切换及 Core 设置切换到显示设置后的 shader 入口；物理 4K 150% 生成 3840×2160 当次截图，并完成 core framebuffer 优先的状态存档截图、服务端解码与继续游戏 |
| Player 换盘 | loader 前盘组/大小校验、真实 diskCount 不匹配阻断、初始盘/当前盘回读、no-op/失败保持、busy/live region、菜单键盘与焦点、光盘 2 SaveState 恢复、两个账号保存隔离 |
| 导入与审核 | 必须选择游戏目录；上传进度、失败重试、候选切换、人工编辑、approve/discard 与历史回放 |
| BIOS/DAT 管理 | 按平台/core 展示状态；哈希 warning 与缺失 blocking 视觉语义不同；DAT 上传、差异预览和启用确认 |
| NG 同源部署 | 通过测试 NG 访问时页面、API、content、runtime 均为同一公开 origin；内部地址不进入 bundle；`isSecureContext` 与 `crossOriginIsolated` 为真 |
| PFB 本机网关 | 裸 localhost只安全重定向；两个规范 `.localhost` app Host、两个 unique runtime Host、Cookie/storage/DB/CAS/cache互不串用；非法Host、未知alias、跨PFB capability、外部监听与转发头欺骗全部失败关闭 |
| RPG Maker 浏览器运行 | 七版本 core 选择无底层实现名；EasyRPG engine 与 mkxp RGSS profile 强制生效；MV/MZ exact unique origin、bootstrap/CSP/MessageChannel/恶意隔离；每版真实 marker、输入/音频/帧、A→B→C→新 Launch 恢复 B 和恢复后 `RESTORE_INPUT` |
| 联机房间与 Player | feature flag 导航、SUPPORTED/ALL 与全部筛选/URL、分享/选座/ready/start gate、loading/空/error/blocker、确认弹层和焦点；Player 只暴露联机允许控件，启动前安装 v4.2.3 frame/state hook，rollback 输出抑制必须 finally 恢复，页面隐藏/断线全局暂停并在 lease 内原座恢复 |
| 响应式应用壳与页面 | `320×568`、`360×800`、`390×844`、`412×915` 手机与 `768×1024`、`1024×768` 平板；路由上下文、底栏/Drawer/Sheet、草稿应用/取消、焦点归还、44px target、safe area、卡片列数和 document 零横向溢出 |
| 移动 Player 方向门禁 | reducer/clock 单测覆盖首次竖屏、250ms 抖动、单机门禁拥有的暂停、用户暂停不误恢复、P1/P2 职责和 hidden 优先级；Chrome E2E 覆盖 config-first、竖屏零 iframe/core/game/PlaySession 请求、旋转后单次启动，以及 `568×320`、`667×375`、`844×390`、`932×430` HUD/Sheet |
| 4K 与桌面体验 | 1280×800 最小桌面、2560×1440，以及物理 3840×2160、150% scale（CSS 2560×1440、DPR 1.5）的关键页面无失控拉伸、遮挡和不可达操作；4K viewport 截图实际为 3840×2160 像素，Player 保持正确比例 |

响应式与 4K 视觉回归不能只依赖像素快照：E2E 还应断言内容最大宽度、关键控件可见、页面无横向溢出、Player canvas/阻断层在视口内、关键 target 尺寸以及导航层级可达。手机普通页面至少覆盖全部四个固定手机视口，平板覆盖两个固定横/竖视口；移动 Player 横屏至少覆盖四个固定视口。截图用于评审证据，不取代语义断言。

共享 Player 方向/暂停实现进入所有 EmulatorJS core 的执行路径，因此修改其状态机、adapter pause/resume 或 iframe 装载门禁后必须运行 `make web-e2e` 及所有受影响的产品 E2E。当前没有产品 E2E 的核心必须在交付说明中列为未覆盖，不能用直接装载 EmulatorJS 的独立页面补齐。纯移动 CSS、HUD 排版或外围 Sheet 若有调用链证据证明不进入装载、帧执行、配置翻译和存档协议，则不因文件位于 Player 目录自动扩大核心覆盖范围。

影响多盘 parser、Launch resource、Provider `discSwitch` 实现或换盘时，除受影响单元/集成/Web 测试外还必须执行 `make web-e2e` 与 `ACC-MDISC-001`–`008` 的受影响产品测试。当前没有真实 Saturn ROM 的浏览器产品 E2E；交付时必须明确这一边界，不能用伪 CHD、独立 EmulatorJS 页面或历史截图替代。

影响 `internal/netplay`、联机 manifest、WebSocket、Player netplay adapter 或房间 UI 时，必须运行聚焦 Go/Web 测试、`go test -race ./internal/netplay`、migration/HTTP integration、`make web-e2e`，并按 [`ACC-NP-010`–`022`](./project-acceptance.md#19-联机游玩) 生成当次协议、安全、feature flag、单机回归与双浏览器核心证据。`ACC-NP-014`–`022` 只证明 manifest 锁定的八个 profile/artifact 与项目自有 fixture；其他 ROM/core 版本仍必须明确列为未覆盖。

## 8. Bug 回归固化流程

任何在 Agent 自测、人工验收、代码评审或实际使用中发现的 bug，都执行同一流程：

1. **记录最小复现**：输入、初始状态、触发步骤、实际结果和期望结果必须清楚；先排除环境或数据版本漂移。
2. **选择最低可靠层级**：纯函数用单测，数据库/HTTP/事务用集成测试，用户激活、全屏和真实 core 运行用经过 Retrom 产品链路的 Chrome E2E。
3. **先建立失败证据**：新增用例在修复前应稳定失败，并且失败原因正是该 bug；不能只证明“某处报错”。不要求提交一个单独的红色 commit，但修复说明要能说明 red/green 过程。
4. **实施最小修复**：修复根因，不只调整测试数据、等待时间或错误提示来绕过症状。
5. **运行回归**：先跑聚焦用例，再跑对应包/页面完整测试、lint、typecheck/build 和受影响的集成/E2E/smoke。
6. **同步契约**：如果 bug 暴露了文档、API、migration 或机器基线缺口，在同一变更更新负责该契约的文件。

回归用例必须永久保留，除非被更高层、同等或更强断言明确覆盖。删除或合并时，PR/交付说明必须指出替代用例。

### 8.1 无法提交完整运行数据时

第三方或用户 ROM、BIOS 和截图可能因授权不能进 Git；Fullscreen 和某些 EmulatorJS 行为也不能由 jsdom 真实复现。这不构成“无需回归用例”的例外。项目自有、许可清晰且从可审查源码确定性生成的公开测试程序可以作为真实 core E2E 输入，但不能外推为其他游戏/core 的兼容性证据。应组合：

- 对故障最近的确定性逻辑增加普通自动化测试；
- 在实际产品 E2E 中增加或收紧资源 hash、Launch/config、请求、帧/canvas 和控制台断言；
- 需要人工画面判断时，将结果写入当次验收证据，而不是聊天或历史样例记录；
- 在交付说明中列出本地夹具要求和未能完全自动化的边界。

影响 EmulatorJS、core artifact、DAT、BIOS 装配、Player Shell 或存档恢复时，至少执行：

```bash
make data-check
make deps-check
make web-e2e
```

另运行所有受影响的产品集成测试。现有 E2E 未覆盖到的核心按 [`core-runtime-validation.md`](./core-runtime-validation.md) 明确报告，不能把 manifest 校验或相邻核心成功外推成运行兼容。

## 9. 覆盖率、夹具与测试可靠性

### 9.1 覆盖率策略

- CI 不配置全仓最低 coverage 百分比，也不因数字下降单独阻断。
- 可按需生成 `go test -coverprofile=coverage.out ./internal/...` 和 Vitest coverage 报告，用于发现未执行分支、死代码和关键路径缺口。
- 评审以风险和不变量为准：关键错误分支未测试，即使总覆盖率很高也不合格；简单映射未覆盖，不为追数字强制补无价值测试。
- 覆盖率工具版本与配置必须固定，报告目录不提交。

### 9.2 确定性要求

- 时间相关逻辑注入 clock；不得用真实 sleep 验证租约、心跳和过期。
- 随机或抖动逻辑使用固定 seed，并断言范围与状态，不依赖执行顺序。
- 文件测试使用测试框架临时目录，不写仓库 `data/` 或用户数据目录。
- SQLite 测试每个 case 使用独立数据库；需要共享内存库时必须证明连接语义，事务/锁竞争优先使用临时文件库。
- 网络适配器使用本地 `httptest`/fake server，覆盖超时、断连、非 JSON、限流和重试；默认测试不访问 Hasheous 或 CDN。
- 并发测试应可重复运行；涉及租约、CAS、发布或 GC 竞争的包额外运行 `go test -race`，但 race 不必成为所有普通改动的默认全仓门禁。

### 9.3 数据夹具

- 解析器可以使用小型、可读、带来源说明的确定性片段覆盖边界和畸形输入。
- Arcade 兼容性结论必须另有针对 `make prepare-deps` 物化到 `data/dat/` 的完整、真实、版本锁定 DAT 的集成校验；小片段不能替代真实基线，payload 也不能因此提交 Git。
- 负向安全测试可以构造恶意 ZIP/XML/路径，因为它们用于验证拒绝行为，不能被描述为真实游戏数据。
- 自动化测试不得读取或下载用户 ROM/BIOS。仓库内公开 ROM 只允许使用项目自有、许可清晰、生成源可审查且由 `data-check/public-fixtures-check` 逐字节验证的夹具；GBA 的三个独立身份分别覆盖普通上传、Pegasus 与 EmulationStation，其中 `emulationstation-smoke.gba` 随最小严格 `gamelist.xml` 使用且不能复用已发布的另一个身份；NES 的两个内容身份分别覆盖 FCEUmm/Nestopia，SNES 夹具覆盖 SNES9x，Arcade 夹具覆盖 MAME 2003/Plus、FBNeo 与 FBA2012 CPS1/CPS2 的依赖装配、单机帧执行和双浏览器联机。真实 release DAT 的物化、解析和精确 active 选择由 `ACC-DAT-004` 使用 production manifest 独立证明；Arcade 产品 Case 的项目自有小型 DAT 由 acceptance-only 装置直接登记为 test-only `BUILTIN`，不得经过 DAT 上传 API，也不得冒充 production baseline。Case 必须显式核对 schema v2 的 `PARENT` 与 `BIOS_OR_BASE`、同一 DatVersion 及冻结内容 bytes。测试 BIOS 不被目标驱动执行；CPS2 的 `spf2t` 父归档只含项目自有 marker 且不被驱动执行；双浏览器结果只证明锁定 profile/artifact 与项目自有 fixture。

RPG Maker fixture 必须遵守同一再分发规则：生成源、许可、固定 bytes 与真实 Retrom 产品消费者同时存在，不得包含厂商 RTP、商业 runtime、官方 executable 或来源不明脚本。MZ 没有可提交商业 runtime 时，自动化只覆盖自有 shape/isolation harness，最终兼容性必须由操作者合法 Web deployment 的 `ACC-RPG-008` 证明，harness 不得冒充真实 MZ 运行。

## 10. 后续实施清单

以下顺序用于真正落地质量基础设施，避免先写大量代码再追补规则。

### Phase Q0：工具与命令

1. 创建 Go module、锁定 Go 版本并建立 `cmd/retrom` 最小可构建入口。
2. 在 `web/` 创建 Next.js TypeScript 项目，锁定 Node/npm 约束并提交 `package-lock.json`。
3. 新增 `.golangci.yml`，按第 4 节启用规则、阈值、formatters 与 depguard；新增结构检查器、中央 suppression allowlist 及其边界测试，不建立存量 baseline。
4. 新增 `web/eslint.config.mjs`、严格 `tsconfig.json`、Vitest config/setup、package scripts、`web/next.config.ts` 和 `web/proxy.ts`。`next.config.ts` 负责 standalone、开发 rewrite 与固定隔离头；`proxy.ts` 按 HTTP 契约为动态 HTML 生成逐响应 nonce CSP 并把同一 header 传入 App Router，不得改用静态 nonce 或旧 `middleware.ts`。
5. 新增以 `api/openapi.yaml` 为入口的 OpenAPI 领域文件、bundle 与两端生成配置，先覆盖通用 envelope、session/health 与一条代表性 CRUD；Go 生成物在后端编译前按需生成且不提交，TypeScript schema 提交并检查漂移；实现 `api-generate/api-check`，后续每个 route 必须先扩对应领域 schema 和入口闭集再写 handler/UI。
6. 新增根 Makefile，实现第 3 节所有命令；golangci-lint 安装到仓库本地并固定版本。
7. 更新 `.gitignore`：忽略 `bin/`、`.cache/`、`internal/httpapi/generated/*.gen.go`、`web/node_modules/`、`web/.next/`、coverage/E2E 报告和五份 DAT payload；继续跟踪 TypeScript schema、真实来源 manifest、SHA256SUMS、物化配方与可提交验证清单。

### Phase Q1：基础测试

1. 为 SQLite migration、时间字段类型、CAS 原子去重和路径安全建立首批 Go 测试。
2. 为 API 错误 envelope、健康检查和最小前后端契约建立集成测试。
3. 为 App Shell、导航层级和 API client 错误映射建立首批 Vitest/RTL 测试。
4. 将每个后续领域能力按第 7 节矩阵逐行补齐，不允许先发布关键路径再追测试。

### Phase Q2：CI 与浏览器门禁

1. `.github/workflows/ci.yml` 在所有 pull request 上设置 Go、Node/npm、仓库固定 Node 工具链及物化 runtime 缓存；随后先执行幂等且逐字节校验的 `make prepare-deps`，再运行 `make ci`。固定 golangci-lint 由 Makefile 依赖自动安装，同一 PR 的旧运行由 concurrency 取消。
2. CI 使用锁文件和固定 manifest 安装依赖；runtime/core/DAT/许可 payload 可以由 `prepare-deps` 从锁定来源物化并按 hash 校验，测试阶段不下载第三方 ROM/BIOS、不访问真实 Hasheous，也不依赖开发机浏览器；仓库自有公开测试 ROM 直接从 checkout 读取并验证生成一致性。
3. 建立 `web/e2e/` 的 Chrome 配置和关键路径；按改动范围或发布流程运行 `make web-e2e`。
4. 真实核心覆盖只能加入 Retrom 产品 E2E；不得建立绕过导入、Launch、内容端点或 Player 的独立示例门禁。

### Phase Q3：镜像构建门禁

1. 新增根 `Dockerfile`，用多阶段构建生成后端镜像 `retrom`；最终镜像只包含 Go 可执行文件、固定 DAT/必要 EmulatorJS runtime 资产和运行所需证书库，不包含编译工具、ROM、BIOS、测试截图或本地数据库。
2. 新增 `web/Dockerfile`，用多阶段构建生成前端镜像 `retrom-web`；采用 Next.js production/standalone 产物，最终镜像不包含开发依赖和构建缓存。
3. 新增 `.dockerignore` 与 `web/.dockerignore`，排除 `.git`、缓存、`node_modules`、`.next`、coverage、E2E 报告、公开测试 ROM、本地 runtime 结果和运行数据；构建阶段只通过版本化脚本下载并校验允许进入镜像的固定 runtime artifact。
4. 在 Makefile 实现三个 image targets 和共用 `release-input-digest` helper；两镜像都写入 `io.retrom.release-input-sha256`，组合 target 以 inspect 确认一致。构建完成后立即返回，不创建容器、不建立网络、不挂载卷、不 push registry。
5. PR 的 required quality check 统一执行 `make ci`；涉及 Dockerfile、依赖锁文件、静态/runtime 资产或发布脚本时还必须在合并前验证 `make build-images`。tag 发布不重复执行 quality check。
6. `.github/workflows/docker-image.yml` 在 tag push 时直接执行 `make build-images`；该命令通过镜像内的确定性依赖物化、`data-check`、release-input digest 和双镜像 label 复核完成发布输入校验。两个镜像校验完成后才允许登录 Docker Hub 并推送，流程不等待 Environment 人工批准，也不能用 Action 重新拼装或绕过 Makefile 的发布输入校验。

### 10.1 预期文件

| 文件 | 责任 |
| --- | --- |
| `/AGENTS.md` | Agent 实施铁律 |
| `/api/openapi.yaml`、`/api/domains/`、`/api/components/` | HTTP 事实源入口、领域 route/DTO 与跨领域组件 |
| `/api/codegen/`、`/scripts/openapi-bundle/` | Go 分层生成配置与只接受本地引用的确定性 bundle 工具 |
| `/internal/httpapi/generated/{models,server,spec}.gen.go` | 后端编译前由统一 bundle 按需生成的 Go 类型、strict server/router 与内嵌规范；禁止手改、被 Git 忽略且不得提交 |
| `/migrations/embed.go`、`/migrations/*.sql` | 编译进后端的顺序 migration 与 checksum 输入 |
| `/.golangci.yml` | Go lint、formatter、排除与 depguard |
| `/quality/go-suppressions.json`、`/scripts/quality_structure.py` | 非结构性 Go suppression 中央清单与全仓源码结构门禁 |
| `/Makefile` | 本地与 CI 的统一命令入口 |
| `/.github/workflows/ci.yml` | 调用 `make ci` 的 required check |
| `/.github/workflows/docker-image.yml` | tag 的双镜像构建校验与 Docker Hub 发布门禁；不重复 PR quality job |
| `/web/eslint.config.mjs` | Next.js/TypeScript lint 基线 |
| `/web/next.config.ts` | standalone 输出、本地后端 rewrite 与固定 COOP/COEP/CORP/`nosniff` 头 |
| `/web/proxy.ts` | Next.js 16 动态 HTML 的逐响应 nonce CSP；开发模式唯一受控的 `unsafe-eval` 例外 |
| `/web/tsconfig.json` | TypeScript strict 与 alias |
| `/web/vitest.config.ts` | jsdom、setup、alias 与测试匹配 |
| `/web/vitest.setup.ts` | matcher 和最小浏览器 API fake |
| `/web/package.json`、`/web/package-lock.json` | 固定脚本、Node 约束和依赖图 |
| `/web/lib/api/generated/schema.d.ts` | `web/package.json#scripts.api:generate` 生成的 TS schema，禁止手改 |
| `/web/e2e/` | Chrome 关键路径、响应式与 4K 验收 |
| `/Dockerfile`、`/.dockerignore` | 后端 `retrom` 多阶段镜像与构建上下文 |
| `/web/Dockerfile`、`/web/.dockerignore` | 前端 `retrom-web` 多阶段镜像与构建上下文 |
| `/scripts/dev.sh` | 仅编排宿主机 Go/Next.js 开发进程，不接触 Docker |
| `/scripts/release-input-digest` | 以依赖专题的唯一算法计算两镜像共用指纹，不联网/不写工作树 |

### 10.2 统一验收入口

质量门禁、规则哨兵和缺陷回归映射统一执行 [一期项目验收规范](./project-acceptance.md) 的 `ACC-QA-001`–`ACC-QA-003`；镜像与本地开发分别执行 `ACC-PKG-*` 和 `ACC-DEV-001`。本文第 7 节仍负责说明哪些关键路径必须有哪些测试层级，但不构成另一份验收 Case 清单。

## 11. 服务器 BIOS 导入测试矩阵

- 配置/路径：封闭 JSON、数量/字符/重叠/受保护根、root 不可用、逐段 no-follow、symlink/special/traversal/cursor 绑定和零绝对路径泄漏。
- 领域/存储：当前 clean schema 与 lineage 拒绝；STATIC/DAT exact、fallback、同名/重命名、多 Requirement CAS 去重；overwrite off/on、同分/更差、同 bytes、版本漂移与并发安装均证明不降级。
- Worker：完整发现前零安装、扫描门禁、2 hash/1 archive 并发、8 MiB cancel、lease/heartbeat/deadline、崩溃后不重复 revision、瞬时 root 退避与 attempt 耗尽、restore fence。
- HTTP：ADMIN/USER/匿名与 CSRF 矩阵，严格 body/Idempotency/ETag/active conflict，root/directory/list/item/candidate cursor 和 allowlist 投影；BIOS 286 fixture 为 100/100/86，无重复遗漏且全集汇总恒为 286。
- React/Chrome：无配置/不可用/空历史、Drawer 键盘、SSE/cancel/retry、完成/部分失败/候选解释；FULL_CATALOG abort/乱序/重复触发/追加失败/键盘 fallback。分别验证 1280×800、2560×1440、物理 4K 150% scale、无页面横向溢出及零 serious/critical axe 结果。

该切片除聚焦用例外必须运行 `make api-check`、后端四门禁、`make integration-test`、前端五门禁、`make web-e2e`、fixture 校验、无 core 参数全量 smoke、`ACC-BIOS-003`–`007` 与 `make ci`。测试 source 使用临时目录或操作者授权且 Git 忽略的本地文件，绝不提交 BIOS bytes。

## 12. Pegasus 目录导入与视频测试矩阵

- parser/scanner：UTF-8 BOM、LF/CRLF、续行与 flowing text、字段别名、同一 metadata 多 game、目录内多个 metadata、大小/条目/深度门禁、非法命令值、路径穿越、symlink/special file、来源中途变化和稳定 `sourceKey`。
- 映射/持久化：当前 clean schema 只包含 review handoff、精确诊断与受当前来源/目标/Provider Target/generation 约束的 preview/screenshot Blob 保护边；Collection 显式映射、ETag、版本冻结；最大 64 文件的投影、全部声明文件参与确定性 key、M3U+CHD 有序分组、Arcade 当前 ZIP 与冻结 DAT 依赖闭包内的同目标显式 companion 集。
- 审核/发布/重复：单文件和多盘沿用既有 library import/validation/review/publish 事务；Worker 完成后只产生 `REVIEW_PENDING` 且零 Game，READY 与 blocker 都可在统一队列处理；初始 Arcade Validation 会采用导入前已经安装且匹配当前 Provider Target 的 DAT BIOS，生成 `SATISFIED_EXTERNAL` 依赖与 `BIOS_BUNDLE` 文件，真正仍缺 Parent/内容的条目继续阻断。Approve/Discard 原子推进普通与 Pegasus 两组状态/计数，来源 COVER/VIDEO 正确保留，用户封面选择优先。快速审批覆盖完整筛选枚举、preview/create digest 漂移、严格 READY 与截图 override 分界、duplicate/Attachment 排除、逐项原子记账、取消竞争、重启恢复、restore fence、worker-only retry、10,000/10,001 上限和两个并发创建；另以真实 Arcade dependency snapshot schema v2 覆盖预览 candidate 与最终发布，证明它走 Arcade DAT closure/required-entry/ValidationFile 校验而不是 BIOS schema v1 解析失败分支。交接崩溃恢复复用已有内部 ImportItem 且不重复系统草稿事件；未完成交接的 Item 不出现在队列/详情且不能发布。同一来源重扫和内容重复列出全部已有游戏并返回稳定结果；失败/取消不删除审核事项或回滚已经提交的游戏，重试不重复 Game/Revision/Blob。
- Worker/存储：BIOS、Pegasus 与 EmulationStation 共用 2-reader limiter；lease/heartbeat/deadline/attempt 耗尽、重启恢复、restore fence、外部 root 变更、媒体告警、保护边 GC 和 backup/restore 均有确定性测试。
- HTTP/UI：ADMIN/USER/匿名/CSRF、strict body、Idempotency、ETag、cursor/filter/SSE；`pegasusImportId` 精确队列筛选、来源媒体 GET/HEAD 与 COVER/VIDEO kind；审核 best-effort preview 锁定现有依赖，READY/阻断均在 `EJS_onGameStart + 5000ms` 优先读取核心最后一帧并上传 PNG，核心截图有界失败时回退 canvas，使静态 ROM/BIOS 错误画面不退化成黑帧；当前阻断截图启用人工发布 override，过期 Validation 拒绝、弹窗失败提示和四个等宽决策按钮。快速审批 UI 覆盖当前筛选的服务端影响预览、零候选/active/stale、进度恢复、取消/retry、终态缓存清理、结果链接与 390/1280/物理 4K 150% scale 的键盘/reduced-motion。三张服务器导入能力卡中 Pegasus 的三步 Drawer、无默认映射、关闭恢复、同计划轮询重渲染不重置映射/焦点/滚动、详情审核行动区和逐行审核入口保持不变。
- 产品运行：独立的项目自有 `pegasus-smoke.gba` 必须从临时服务器 root 经 Chrome 完成目录选择、真实扫描、显式 GBA 映射、Worker、待审核、逐项发布、Game 详情、Launch config、受限内容端点与 mGBA 帧推进；不得复用普通上传已经发布的相同内容、直接写库、mock Pegasus API 或只检查 canvas 元素。
- 总览聚合：一个包含多个游戏的 PegasusImport 只能贡献一个最近任务和一个顶层批次；其逐游戏内部 ImportJob 不进入普通任务分页。进行中/完成/异常批次、处理中条目、异常条目和实际待审核 Item 分别按正式口径断言，主动取消不误报为异常，最近三条不能反向决定流水线数字。
- VIDEO：MP4/WebM magic 与限额、nullable dimensions、Range/HEAD/MIME、不可变 revision、元信息编辑保留、删除保留历史；详情 2 秒累计可见自动播放、后台页不计时、5 秒/拒绝/错误回退、用户暂停和 reduced-motion 手动模式，以及列表零视频请求。

该切片除聚焦用例外必须运行 `make api-check`、后端四门禁、`make integration-test`、前端五门禁、`make web-e2e`、`ACC-PEG-001`–`006`、`ACC-MEDIA-001` 与 `make ci`。使用操作者授权的真实 Pegasus 目录时只记录相对统计和结果，不把 ROM、完整宿主路径或媒体内容写入报告。

## 12.1 EmulationStation 服务器目录导入测试矩阵

- parser：严格 UTF-8/BOM、无 namespace `gameList`、game/folder、字段缺省/重复/尺寸边界、title fallback、players/date、hidden/adult/kidgame；DTD/实体/PI/namespace/非 UTF-8/深度/attribute/token/总 token 全部 fail closed。`command/emulator/core/provider` 值必须有负向存储/API/日志泄漏断言，warning 只能包含封闭结构。
- 路径与扫描：`./`、普通路径、Windows 分隔符规范化；空白/control、`..`、absolute/tilde/drive/UNC/URI、大小写错误的清单名、symlink/special、rename/source/root 漂移、64/250k/2m/1000/8MiB/64MiB/100k/2TiB 上限和确定性 source key/snapshot。分别建立“所选父目录含多个子目录且每个子目录有 `gamelist.xml`”与“无子目录、只有一份 `gamelist.xml` 和多个游戏文件”集成 fixture，断言 Collection 边界、独立映射与错误隔离。
- mapping/HTTP/store：ADMIN/USER/匿名、strict JSON/query、CSRF/Origin、Idempotency、If-Match/ETag、cursor/filter、create/get/list/delete/start/cancel/retry；无默认 mapping、IMPORT/SKIP/Tag union、目标/Tag 删除漂移、来源重验。fresh/前缀/非法 lineage、Job kind/scope、不可变发现 snapshot、Pegasus/EmulationStation 普通 ImportItem owner XOR、source revision/ref 与 Blob registry 均有非法 SQL 测试。
- library handoff：单 ROM、M3U 同目录 2–8 CHD、Arcade 同 execution/target/DAT companion、COVER/VIDEO 优先级与媒体 warning；普通内容身份、CoreValidation/DAT/BIOS/重复、Parent/多盘后继快照。Worker 只能形成 `REVIEW_PENDING`，Approve 前零 Game；逐项 Approve/Discard 与严格 READY 快速审批原子推进两组聚合，hidden/adult 固定进入 `sourceFlagged` 且只允许逐项发布。崩溃恢复复用既有内部 ImportItem，未交接项不可见。
- Worker/恢复/释放：共享 2-reader、单 active execution、20 waiting/7 天过期、lease60/heartbeat15、1/5/30/120 退避、4 attempts、8 小时 deadline、每 8 MiB cancel 检查；cancel/retry/delete/restore fence。发布、丢弃、已存在、确定性阻断、取消和不可重试失败分别验证 PayloadRelease；Game 发布后先证明 Launch/Player，再永久删除并以 fake clock 推进宽限 GC，断言流程/Game payload 释放、共享 Blob 保留、新引用撤销候选及墓碑不可启动。
- React/Chrome：三张等权卡、EmulationStation 卡文案、760px 三步 Drawer、关闭/恢复/焦点/滚动、每 Gamelist 一行的 mapping、folder/flag/扩展/issues、批量与逐项 Tag、确认警告、详情 action、`emulationStationImportId` 固定审核筛选、source media/flag、快速审批排除桶、RELEASED 状态。固定 390×844、1280×800、2560×1440 和物理 4K 150% scale，键盘、reduced-motion、document 零横向溢出与 axe serious/critical 为零。
- 产品运行：独立项目自有 `emulationstation-smoke.gba` 从临时 server root 经真实扫描、显式 GBA mapping、普通审核/Approve、Game 详情、Launch config、受限内容端点与 mGBA 核心帧推进；不得直接写库、mock source API、复用普通/Pegasus 已发布内容或只检查 canvas 元素。操作者授权的 Batocera 目录只可用于隔离开发实例人工 smoke，不进入 CI、Git 或自动证据。

本切片必须完整执行 `ACC-ES-001`–`006`，并回归 `ACC-PEG-001`–`006`、`ACC-IMP-001/003/007/008/009`、`ACC-MDISC-001/004`、`ACC-BIOS-003/006`、`ACC-CAS-002`、`ACC-BKP-001`、`ACC-GAME-001/003`。命令门禁固定为 `make quality-structure-check`、`make fmt-check`、`make build`、`make test`、`make lint-go`、`make integration-test`、`make web-install`、`make web-lint`、`make web-typecheck`、`make web-test`、`make web-build`、`make api-generate`、`make api-check`、`make public-fixtures-check`、`make web-e2e` 与 `make ci`；任一未运行或失败都不能宣称切片完成。

## 13. 游戏标签测试矩阵

- migration/store：034 新库与 033 升级、表/列/partial unique/index/trigger/INTEGER 时刻/FK、DELETED 不可恢复、同名新 ID、20/21 owner 上限，以及 backup/restore 对 tombstone、关系和审计的保真。
- Tagging/HTTP：NFC、Unicode whitespace/case-fold/control、40/41 code point、160/161 byte、1,000/1,001 实例上限；CRUD/usage/cursor/filter/sort、ADMIN/USER、strict JSON/CSRF/If-Match/Idempotency、同名并发、关系 no-op、delete 与 assignment 两种提交顺序、版本联动和审计。
- 搜索/投影：Game/Admin/Review 的 `q/tagId` 在 SQL 分页前取交集，cursor 不跨筛选复用；Favorite/Recent/Save/Netplay 与 detail 的数组始终非 null、名称稳定排序、删除立即隐藏且列表批量读取无 N+1。
- Import/Review/Pegasus/EmulationStation：批次默认标签、多 Item 继承、reconfigure、逐项 autosave、删除后的旧 ETag、Approve 原子复制、Discard snapshot；逐 Collection 集合、SKIP 空值、mapping 恢复/start 漂移/retry/handoff 幂等和外部 metadata tags 不自动关联。
- React/Chrome：TagPicker 键盘/20 上限/空 taxonomy，管理页 loading/empty/error/conflict/Drawer/Dialog 焦点，导入/Pegasus/EmulationStation/审核/管理员游戏写入，Library/Admin/Favorite/Recent/Save/Netplay 的名称搜索和精确 URL 恢复；390/1280/2560/物理 4K 150% scale 无页面横向溢出且 axe serious/critical 为零。

该切片运行 `make api-check`、后端四门禁、`make integration-test`、前端五门禁、`make web-e2e`、`ACC-TAG-001`–`005` 与 `make ci`。标签不进入 EmulatorJS、内容字节、Variant 或存档协议，因此不因本切片运行 core smoke、fixture 或依赖基线；若实际调用链改变则重新判定。

## 13.1 沉浸模式测试矩阵

沉浸模式必须同时覆盖纯输入状态机、独立 UI、后端只读投影、Provider `setInputFilter` 契约与真实产品链路，不能用鼠标点击、直接调用 React handler 或独立 EmulatorJS 页面替代手柄路径：

- 纯逻辑测试覆盖 standard mapping、轴阈值/回滞、中立门禁、A/B/方向沿触发、重复节流、断开/隐藏清零，以及 Select+Start 的 `100/60/650ms` 双组合键状态机；
- 标题首字符纯逻辑测试覆盖 ASCII 数字、大小写字母、常用与多音汉字的固定拼音首字母、符号/emoji/空白
  回退 `#`；所有 MetadataRevision 生产 writer 和改名事务都断言同一字段与数据库值域，不能只测沉浸查询；
- HTTP 集成测试覆盖 destinations 固定四卡顺序、Profile 隔离、平台可见性、收藏夹 ownership、存档投影、
  `titleInitial/title/gameId` 分页与 recent 时序例外、媒体/描述 current revision、签名 cursor 的范围绑定、
  未知 query、非法 folderId、隐藏平台 404 和媒体 revision 退役；
- React 测试使用可控 Gamepad source 覆盖首页显式/手柄两种入口、焦点保持、同帧方向+A、destination 左右
  环绕和定向动画、资料库/平台游戏浏览、收藏夹、Y 默认收藏及红色右侧爱心、存档横向大/小框选择与非黑屏截图、长简介无滚动条自动滚动及 reduced-motion 静止、媒体等高、700ms
  视频延迟、路由/隐藏时手柄认领保持、真实断连去抖、SSR 稳定时钟、错误/空状态和 B 返回；普通 PC/移动
  导航与壳不得出现沉浸控件；
- BGM/系统菜单测试使用可控 media、visibility、Fullscreen 和 localStorage，覆盖内置 OGG、自动播放阻断
  提示、共享 layout 内跨入口/平台/资料库导航的同节点与播放位置连续性、隐藏/静音/零音量/Player/卸载暂停、可见恢复、两组音量严格 v1 payload/default/failure、Select
  菜单顺序、上下/左右/A/B 与全屏拒绝；测试不得真实播放或写全局用户状态；
- Provider 测试必须证明过滤器先于私有 loader 安装、仅过滤活动手柄 Select/Start、第一次 chord 不泄漏、
  菜单期间所有本地手柄归零、取消只恢复本菜单拥有的暂停、创建存档只走手动非空 state+可选截图链路且 payload 失败可重试、
  退出完成并撤销 Launch、teardown 恢复原 `getGamepads`；4.2.3 与 4.3.0-pre 都覆盖，联机 legacy adapter
  不能继承过滤；
- 内容端点集成测试覆盖 Asset/ROM/多盘外部文件/BIOS/parent 相同 bytes URL 稳定、任一替换 URL 改变、旧
  identity 授权失败或退役、immutable private/public cache header，以及 SaveState/state screenshot 始终
  Profile 私有 no-store；
- 产品 E2E 使用项目自有 GBA 与 Arcade fixture，经过真实登录、首页手柄入口、四类资料库/平台页、收藏与
  存档、Launch/config/content、Player、创建存档、EmulatorJS Core 和退出返回。至少一条 Arcade 路径覆盖
  多个手柄快照并证明只有活动手柄控制沉浸 UI；媒体存在时须走受权 COVER/VIDEO 端点。

`ACC-IMM-001`–`012` 是唯一验收步骤事实源。修改输入过滤、普通 adapter manifest/registry、Player Shell、
沉浸 API 或内容身份缓存时，除聚焦测试外必须运行 `make data-check`、`make deps-check`、`make web-e2e` 及
普通/联机既有回归；不得通过复用联机 adapter 或降低输入断言取得通过。

## 13.2 RPG Maker 测试矩阵

- 纯逻辑：一个用户虚拟 core、七条 `generation→内部 core→route/adapter` 双向 registry、七世代自动检测与 42 个跨世代 core mismatch、LCF varint/chunk、INI UTF-8/CP932、RGSS marker、MV/MZ HTML/JSON、SAFE_LOGICAL_PATH、V2 fileset、gencache、deterministic mkxpz、pack matcher、native bundle、runtime reducer；parser/codec 必须有固定 seed fuzz 且无 I/O/panic/无界分配。
- SQLite/HTTP：fresh clean migration、profile/artifact/pack/save/Launch 跨表 trigger、历史 artifact protection、ticket one-time consume/expiry/replay、review ETag/runtime binding revision、validation/restore Launch/gate sequence、270 MiB multipart、Range/ETag/MIME/Host/Origin 和错误码；非 RPG preview 与 RPG validation 相互隔离。
- Web：导入 selected version/evidence、pack 与验证 gate、七 core 选择器、loading/disabled/error、checkpoint availability、factory config decoder、adapter cleanup、移动/桌面/4K/focus/axe；普通 EJS、沉浸与联机分支必须回归无变化。
- Chrome 产品链：七个世代都必须经过真实浏览器上传、审核、Launch、受授权内容、Player、marker、输入/音频/帧、创建 checkpoint、结束和不同 restore Launch。每个 Case 记录 A、B、C 与恢复值，逐字段证明 restore=B 且与 A/C 可区分，并保留恢复后截图；随后继续真实输入并持久化与 B 不同的 `RESTORE_INPUT`。HTTP 201、load success、Blob/hash 相等、同进程 load 或单张截图均不合格。
- 安全/供应链：七核心最小产品闭环完成后再恢复 MV/MZ malicious harness 与额外矩阵；当前固定 manifest identity、`retrom-runtime` repo/tag/tag commit/asset 形状、checkpoint format、route/registry 和两个镜像 release digest 双向一致。RPG release asset 不保存 expected SHA；observed SHA 只用于本地缓存损坏和诊断回归。

本切片最终必须运行：

```bash
make quality-structure-check
make fmt-check
make build
make test
make lint-go
make web-install
make web-lint
make web-typecheck
make web-test
make web-build
make integration-test
make api-generate
make api-check
make data-check
make prepare-deps
make deps-check
make public-fixtures-check
make web-e2e
make build-images
make ci
make acceptance-case CASE=ACC-RPG-001
# 继续逐项执行 ACC-RPG-002 至 ACC-RPG-012，不得合并、省略或用 web-e2e 替代
```

`ACC-RPG-008` 还要求 `RPG_MZ_SMOKE_ROOT=<licensed-web-deployment-directory>`。缺少合法物料时必须报告该 Case 未满足，不能把其余测试绿色写成七世代完成。

## 14. 维护规则

- 升级 Go、Next.js、ESLint、TypeScript、Vitest 或 golangci-lint 时，单独提交配置变化，阅读迁移说明并运行完整 `make ci`；不能把工具升级与大功能混在一起掩盖行为变化。
- 新 linter 先证明信噪比和修复现有问题，再加入显式 enable；禁止用长期 `new-from-rev` 只检查新增代码形成双重标准。
- 如果一条规则确实不适合 Retrom，应记录具体误报、替代保护和评审结论；不得因修复成本高直接关闭。
- 测试章节只记录必须覆盖的行为和命令，不写某次执行的测试数量或 PASS 日志。
- 镜像名、Docker build context、`make dev` 进程模型或 TLS 终结边界发生变化时，必须同步更新本文和部署专题；不得把部署环境差异硬编码进前端构建产物。
- 当目录、命令或技术栈发生变化时，同步更新根 `AGENTS.md`、本文和实际配置，保证三者一致。
