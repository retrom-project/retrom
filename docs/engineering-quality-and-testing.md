# 工程质量、Lint 与测试规范

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期实施基线 |
| 版本 | 1.6 |
| 日期 | 2026-08-17 |
| 适用范围 | Go 后端、Next.js 前端、SQLite 集成、WebSocket rollback 联机与 EmulatorJS 运行时验证 |
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
| `make fmt` | 对 Go 源码执行 `gofumpt` 与 `goimports` | 是 |
| `make fmt-check` | 检查 Go 格式并输出 diff；存在差异即失败 | 否 |
| `make install-go-formatters` | 将固定 `gofumpt v0.11.0` 与 `goimports@v0.48.0` 安装到仓库忽略的 `bin/` | 会写工具缓存与 `bin/` |
| `make install-golangci-lint` | 将固定版本 golangci-lint v2 安装到仓库忽略的 `bin/` | 会写工具缓存与 `bin/` |
| `make prepare-e2e-browser` | 通过锁定的 Playwright CLI 幂等下载并启动校验官方 Chrome for Testing；缓存到 `.cache/tools/ms-playwright/`，稳定入口为 `.cache/tools/retrom-chrome-for-testing` | 会写被忽略的 `.cache/tools/` |
| `make build` | 构建 `./cmd/retrom` | 否 |
| `make test` | 运行 Go 常规单元测试，默认不含 `integration` build tag | 否 |
| `make lint-go` | 使用仓库固定版本的 golangci-lint v2 扫描源码和测试 | 否 |
| `make backend-check` | `fmt-check + build + test + lint-go` | 否 |
| `make web-install` | 在 `web/` 执行 `npm ci`，只接受 `package-lock.json` | 会重建依赖目录 |
| `make web-lint` | ESLint 扫描全部受控 TS/TSX/JS，warning 视为失败 | 否 |
| `make web-typecheck` | `tsc --noEmit` | 否 |
| `make web-test` | `vitest run` | 否 |
| `make web-build` | 干净执行 Next.js production build；运行中的本地开发服务需要保留 `.next/` 时可显式设置 `NEXT_DIST_DIR=.next-build` | 只允许重建 `.next/` 或被忽略的 `.next-build/` |
| `make web-check` | `web-install + web-lint + web-typecheck + web-test + web-build` | 仅依赖/构建产物 |
| `make integration-test` | Go `integration` build tag：migration、SQLite、HTTP 与跨模块流程；不含要求本地专有内容的 `localfixtures` 测试 | 否 |
| `make api-generate` | 先以锁文件安装前端依赖，再从 `api/openapi.yaml` 生成 Go strict stdlib server types 与前端 TypeScript schema | 会重建依赖目录并只修改两个 generated 文件 |
| `make api-check` | 先以锁文件安装前端依赖，再在临时目录用固定生成器重建并逐字节比较，OpenAPI 无效或 generated 漂移即失败 | 仅依赖产物 |
| `make web-e2e` | 先执行 `prepare-e2e-browser`，再用缓存中固定 Chrome for Testing 运行关键 Playwright 场景 | 会写浏览器缓存并产生本地报告 |
| `make data-check` | 离线校验 Makefile/GitHub Actions 的 clean-checkout 依赖顺序及已提交的小型依赖 manifest/SHA-256/DAT/许可配方 schema；无 payload 也通过 | 否 |
| `make prepare-deps` | 按固定 manifest 物化 EmulatorJS/core/五份 DAT/许可文件并生成 notice；两个 FBA2012 DAT 从锁定源码确定性原生生成两次；正确缓存不联网；完成后执行 `deps-check` | 会写被忽略的依赖缓存 |
| `make deps-check` | 完全离线校验本地 allowlist、core、DAT、override、许可输入/notice 及 DAT 统计 | 否 |
| `make release-input-digest` | 离线计算依赖专题规定的源码/依赖发布输入指纹，stdout 只输出 64 位小写 SHA-256 | 否 |
| `make ci` | `api-check + backend-check + web-check + integration-test + data-check` | 仅依赖/构建产物 |
| `make dev` | 先 `prepare-deps + web-install`，再在宿主机启动 Go/Next.js 本地进程并统一处理退出信号；不使用 Docker | 会写本地依赖/开发数据缓存 |
| `make build-backend-image` | 只构建 `retrom:${IMAGE_TAG}`，前后复核并标记 release-input digest | 只写本地镜像缓存 |
| `make build-web-image` | 只构建 `retrom-web:${IMAGE_TAG}`，前后复核并标记同一 digest | 只写本地镜像缓存 |
| `make build-images` | 以同一 digest 依次构建上述两个镜像，最后 inspect label 一致性 | 只写本地镜像缓存 |
| `make acceptance-prepare` | 按统一验收文档创建隔离的临时验收环境、固定 seed 与本次 run ID；不得读取/删除用户运行数据 | 会写 `.artifacts/acceptance/` 与临时数据根 |
| `make acceptance-case CASE=ACC-…` | 只执行一个已登记 Case，应用该 Case 的硬超时并写机器结果/证据 | 只写本次验收证据与临时状态 |
| `make acceptance-report` | 只聚合本次已有 Case 结果并按统一规则判定，不补跑、不篡改结果 | 写最终报告 |

补充规则：

- `make ci` 不含依赖用户私有夹具的核心启动 smoke；该验证按第 8 节的影响范围单独执行。
- 全新 checkout 的统一初始化入口是 `make install-deps`。它允许在测试或服务启动前联网下载锁定依赖；正确缓存后 `prepare-deps` 与 `prepare-e2e-browser` 均幂等复用。浏览器缓存、Node 工具链和运行时 payload 不进入 Git 或镜像。
- 读取 `data/game/` 中私有 ROM/BIOS 的 Go 集成测试必须同时声明 `integration && localfixtures`，只在操作者已物化并验证授权夹具后以 `go test -tags='integration localfixtures' ...` 显式运行；默认 `make integration-test` 和 `make ci` 不得因缺少专有 ROM/BIOS 失败。`data-check` 必须回归检查这一标签边界，并拒绝本机 `data/game/` 中没有被 `Makefile`、`internal/`、`scripts/` 或 `web/e2e/` 实际测试入口按完整相对路径引用的文件；仅有文档说明、core 配置或相同 basename 不构成消费证据。
- `make ci` 默认不构建容器镜像；Dockerfile、镜像内容或发布资产变化时，在 PR 验证中额外执行 `make build-images`。tag 发布流水线不重复运行 PR 的 quality job，只执行自身的双镜像构建、输入校验和推送。
- Go package 列表应显式覆盖 `./cmd/...`、`./internal/...` 和 `./migrations/...`，避免未来 `web/node_modules` 或本地数据目录中的意外 Go 文件污染 `./...`。根 `migrations` 是可导入的 Go embed package，SQL 与 `embed.go` 同目录，不能依赖运行容器中另有源码目录。
- OpenAPI 固定为 `api/openapi.yaml`（项目协议基线为 OpenAPI 3.0.3；锁定的 `oapi-codegen v2.8.0` 虽支持 3.1，但不得在普通实现任务中变更规范方言）。Go 侧由该版本以 `models + std-http-server + strict-server` 生成 `internal/httpapi/generated/api.gen.go`；生成器作为 `go.mod` 的 tool 依赖锁定，配置放 `api/oapi-codegen.yaml`。请求验证固定 `nethttp-middleware v1.2.0`，另加 HTTP 专题的重复 JSON key/未知 query lexical guard。前端 `web/package.json#scripts.api:generate` 固定执行 `openapi-typescript ../api/openapi.yaml -o lib/api/generated/schema.d.ts`，并用 `openapi-fetch 0.17.0` 封装同源 client。两个生成文件不得手改；改用 OpenAPI 3.1 必须单独完成两端生成、validator 与 contract test 的契约迁移。
- `api-generate` 与 `api-check` 必须直接依赖 `web-install`，保证全新 checkout 在调用 `npx --no-install` 前已通过 `package-lock.json` 物化精确版本；不得依赖开发机残留的 `web/node_modules`，也不得允许 npx 临时下载缺失包。`data-check` 的 Makefile 回归用例必须锁定这一先后关系。
- Makefile 固定 `GOFUMPT_VERSION=v0.11.0`、`GOIMPORTS_VERSION=v0.48.0` 与 `GOLANGCI_LINT_VERSION=v2.11.4`，都安装到仓库内忽略的 `bin/`；`fmt/fmt-check` 只调用本地 formatter，`lint-go` 只调用本地 golangci-lint，不得调用浮动的 `@latest` 或依赖开发机全局版本。安装命令精确为 `go install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)`、`go install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION)` 和 `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)`；版本 sentinel 必须核对二进制报告值，已有错误版本不能因文件存在而复用。
- Go 版本以 `go.mod` 为事实源，`.golangci.yml` 与 CI 必须一致。Node 版本以 `web/package.json#engines` 和仓库版本文件为事实源，CI 不得另选一个未记录版本。
- Web 统一使用 npm，必须提交 `web/package-lock.json`；CI 使用 `npm ci`，不得用会改锁文件的 `npm install`。
- Makefile 固定 golangci-lint `v2.11.4`；升级必须显式修改变量和本节并运行完整门禁，不能在安装命令使用 `@latest`。
- Makefile 固定 `DOCKER ?= docker`、`BACKEND_IMAGE ?= retrom`、`WEB_IMAGE ?= retrom-web`、`IMAGE_TAG ?= latest`。默认输出必须是 `retrom:latest` 与 `retrom-web:latest`，同时允许调用者显式覆盖 tag 或完整镜像仓库前缀。
- 三个 image targets 只能调用镜像构建，不得依赖 `dev`，也不得执行 `docker run`、`docker compose`、push、登录 registry 或部署操作。
- `make dev` 前置执行 `make prepare-deps` 与 `make web-install`，之后只能运行宿主机的 `go run ./cmd/retrom` 与 `npm run dev`（可以由 `scripts/dev.sh` 编排），必须正确转发 `SIGINT/SIGTERM` 并在任一子进程异常退出时结束另一进程；登记必须同时覆盖 supervisor 与两个独立 process group 的 PID/start ticks。启动前以仓库专用 PID/start ticks/工作目录/命令行身份安全停止并等待旧 dev supervisor；若 supervisor 被强制终止，则还要以登记的 process group/session 和子进程身份安全接管遗留 Go/Next.js。身份无法确认时只能失败，不能按端口或名称误杀其他进程；不得要求 Docker daemon。
- 本地自动化明确使用 `RETROM_MODE=test`，dev supervisor 将它转换为后端 CLI 的 `--mode=test` 后从 Go 子进程环境中移除，避免严格环境变量校验把前端编排变量误当作后端配置。测试模式只允许临时数据目录、固定 `test/test` 账号和显著 UI 警告；release 模式测试必须走 setup code，不得用测试账号旁路。

## 4. Go Lint 基线

### 4.1 配置原则

使用根目录 `.golangci.yml`，配置 schema `version: "2"`，`linters.default: none` 后显式启用规则，`run.tests: true`，超时初始设为 5 分钟。显式列表让升级新增默认规则时不会产生不可控漂移。

初始启用集如下：

| 类别 | Linters |
| --- | --- |
| 正确性与资源释放 | `bodyclose`、`contextcheck`、`copyloopvar`、`errcheck`、`errorlint`、`gocritic`、`govet`、`ineffassign`、`nilerr`、`nilnil`、`noctx`、`rowserrcheck`、`sqlclosecheck`、`staticcheck`、`unparam`、`unused` |
| 安全 | `gosec`、`asasalint`、`canonicalheader`、`fatcontext` |
| 错误契约 | `err113`、`wrapcheck` |
| 可维护性 | `dogsled`、`dupl`、`exhaustive`、`funlen`、`gocognit`、`gocyclo`、`interfacebloat`、`lll`、`makezero`、`misspell`、`nakedret`、`nestif`、`nonamedreturns`、`prealloc`、`revive`、`unconvert`、`whitespace` |
| 工程纪律 | `depguard`、`forbidigo`、`gochecknoinits`、`nolintlint`、`predeclared` |
| 格式 | `gofumpt`、`goimports`（放在 `formatters.enable`） |

第一版阈值采用：

- `dupl.threshold = 120`；
- `funlen.lines = 80`、`funlen.statements = 50`；
- `gocognit.min-complexity = 25`；
- `gocyclo.min-complexity = 15`；
- `interfacebloat.max = 5`；
- `lll.line-length = 120`；
- `nestif.min-complexity = 4`；
- `misspell.locale = US`。

这些阈值是重构提示，不是鼓励把逻辑拆成无意义的小函数。若一个有序校验器、状态机或事务边界确实需要保持连续，可以使用局部例外，但必须说明为什么拆分会损害正确性或审计性。

### 4.2 必须显式配置的规则

- `nolintlint` 要求具体 linter 名、非空原因、禁止未使用的 suppress。合法形式类似 `//nolint:gosec // 输入已经过 allowlist，拼接内容不是用户值。`
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

`*_test.go` 可以集中豁免 `dupl`、`err113`、`funlen`、`gocognit`、`gocyclo`、`lll`、`nestif`、`noctx` 和 `wrapcheck`，以允许清晰的表驱动夹具与失败消息。

测试中仍必须保留 `errcheck`、`govet`、`staticcheck`、`gosec`、`rowserrcheck` 和 `sqlclosecheck`。测试也是代码，不能用整类正确性检查豁免来隐藏资源泄漏或无效断言。

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
    "api:generate": "openapi-typescript ../api/openapi.yaml -o lib/api/generated/schema.d.ts",
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

全局 ignore 只能覆盖 `node_modules/`、`.next/`、`out/`、`build/`、`coverage/`、Playwright 报告、测试结果及 `next-env.d.ts` 等生成物。不得排除 `web/app/`、`web/features/`、`web/lib/`、`web/components/` 或测试源码。

`eslint-disable` 必须精确到规则和最小行，并附原因。因为 lint 以 `--max-warnings=0` 运行，任何 warning 都是门禁失败。

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
| 产品运行时 E2E | 真实 Retrom 导入/Launch/内容端点/Player 是否能驱动 EmulatorJS 核心 | `web/e2e/` + `testdata/public-roms/` 项目自有 ROM；特定兼容性验收另用 `data/game/` 本机授权资源 | 按影响范围/发布门禁 |
| 多进程联机验收 | 真实双端初始 state、rollback、checkpoint 收敛、断线/resync 与终局 | 两个或更多独立 Chrome process + `ACC-NP-*` runner | 发布门禁 |

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
| 启动预检与 capability | 默认核心与单次覆盖、必需依赖、DOS 程序、cookie capability hash/过期/范围/一次启动绑定、复制 launchId 无 cookie 拒绝、未授权 Blob 与路径逃逸拒绝、日志脱敏 |
| 多盘发布、Launch 与存档 | canonical playlist/ordered identity、artifact V3 digest、config discSet、playlist/Disc GET/HEAD/单 Range、跨 Launch/原名拒绝、当前盘存档与先切盘后恢复、替换失败保留旧 revision |
| 账户初始化与认证 | 数据库 `PENDING/COMPLETED` 及 context 映射、release setup code、test bootstrap、Argon2 参数、密码 blocklist、通用登录错误、session 轮换/过期/撤销、Origin/Fetch Metadata/CSRF、限流与可信代理 |
| 用户管理 | 邀请/重置 secret 单次显示且数据库不保存 secret/hash、角色和状态转换、ETag、本人保护、最后管理员保护、停用/删除级联撤销、离线 admin-reset 与 restore 安全栅栏 |
| 私有数据隔离 | 所有 Profile 派生列表/详情/写入按认证主体限定；跨用户 ID、cursor、Idempotency-Key、SaveState、PersistentSave 和 Launch 探测均不泄露也不串写 |
| 收藏与收藏夹 | 名称 NFC/空白/case-fold 边界、收藏状态机、Folder 上限/version、批量边界和原子失败；023/024→025、复合 owner FK、隐藏投影；每条 route 的 strict JSON/query、CSRF、cursor、ETag、幂等与两个 Profile 隔离 |
| 联机控制面与实时协议 | Room/Member/Session 全状态与非法边、core profile 准入（同 artifact 的不同 ROM 名称/大小/hash 均可选，错版本/artifact/content kind/READY/dependency 均拒绝）、profile canonical digest、2/3/4 occupied mask、乱序贡献与 neutral seat、seq/frame/int16/大小校验、租约/history、前三次真实 resync/第四次终局、slow peer/backpressure、prepare/restart/restore 收口；Hub 必须跑 race test，SQLite 不保存实时 state/input bytes |
| NG/代理边界 | 只信任 allowlist 代理的转发头、公开 origin 校验、伪造 `X-Forwarded-*` 拒绝、应用仅绑定 HTTP 且没有证书配置路径 |
| 存档与恢复 | 截图必需、存档绑定 CoreArtifact 与 GameVariantRevision、兼容恢复、不匹配拒绝、旧 revision 被引用时 GC 保护 |
| 游玩时长 | 心跳幂等、页面不可见/暂停不累计、失联上限、重复 finish、异常时钟、整数毫秒持久化 |
| SQLite migration | 空库建表、每个受支持旧版本逐级升级、重复启动、事务回滚、外键/索引、所有业务时刻列为 `INTEGER` |
| Blob GC/备份恢复 | 引用扫描、竞态保护、孤儿回收、仍被存档/任务引用的 Blob 保留、恢复后数据库与内容引用一致 |

### 7.2 前端与浏览器

| 关键路径 | 最低自动化要求 |
| --- | --- |
| 游戏库到详情 | 卡片进入 `/games/:id`；详情不是一级菜单；筛选/搜索可恢复；加载、空、错误状态 |
| 收藏跨页面闭环 | 游戏库/详情/收藏页状态一致；全部/未分类/Folder URL 恢复；创建/重命名/删除、精确分类、批量与两秒 undo；两个账号隔离；1280/2560/3840、键盘/focus/ARIA/axe/reduced-motion |
| 详情启动 | 默认选中游戏目录核心；用户可作单次切换；一次点击创建 launch 并自动运行；正常路径无第二个 Start 按钮 |
| 默认全屏 | Fullscreen 请求发生在原始用户激活链；拒绝/刷新深链有恢复入口；阻断失败退出全屏并返回可修复错误 |
| 存档快速启动 | 首页、存档页和详情存档都直接启动；使用存档绑定环境，不重新询问核心或 DOS 程序 |
| 多盘导入与审核 | capability 隐藏/自动 mode/退回 STANDARD、递归目录预检、完整/缺盘/非法/ignored 计数、精确缺盘上传、Job resume/retry、审核刷新、管理详情和完整目录替换 |
| DOS 启动 | 程序列表、默认项、缺失选择校验和 launch payload；不能在浏览器端猜测可执行文件；4.3 thread core 的 7z/ZIP Worker 在生产 CSP 下完成无 `eval` 精确转换，源形状漂移 fail closed |
| 管理侧信息架构 | “游戏入库”为父级总览；导入、任务、待审核、历史同级缩进；父/子高亮和直接路由一致 |
| 认证与路由守卫 | 初始化、登录、邀请注册、重置、账户设置；匿名 returnTo、已登录认证页重定向、USER 后台 403、401 清除内存状态；secret fragment 立即清除且不进任何浏览器存储 |
| 用户管理 | 1280/2560/3840 表格、筛选、Drawer/焦点、本人/最后管理员禁用态、ETag 冲突、邀请/重置一次性 secret 对话框和确认流程 |
| 账户切换与 Player | 同一 Chrome profile 中 A 的平台图钉、DOS 偏好、查询缓存和 EJS IDBFS bytes 不得被 B 读取；无服务器保存时清除旧 IDBFS 路径 |
| Player 换盘 | loader 前盘组/大小校验、真实 diskCount 不匹配阻断、初始盘/当前盘回读、no-op/失败保持、busy/live region、菜单键盘与焦点、光盘 2 SaveState 恢复、两个账号保存隔离 |
| 导入与审核 | 必须选择游戏目录；上传进度、失败重试、候选切换、人工编辑、approve/discard 与历史回放 |
| BIOS/DAT 管理 | 按平台/core 展示状态；哈希 warning 与缺失 blocking 视觉语义不同；DAT 上传、差异预览和启用确认 |
| NG 同源部署 | 通过测试 NG 访问时页面、API、content、runtime 均为同一公开 origin；内部地址不进入 bundle；`isSecureContext` 与 `crossOriginIsolated` 为真 |
| 联机房间与 Player | feature flag 导航、SUPPORTED/ALL 与全部筛选/URL、分享/选座/ready/start gate、loading/空/error/blocker、确认弹层和焦点；Player 只暴露联机允许控件，启动前安装 v4.2.3 frame/state hook，rollback 输出抑制必须 finally 恢复，页面隐藏/断线全局暂停并在 lease 内原座恢复 |
| 响应式应用壳与页面 | `320×568`、`360×800`、`390×844`、`412×915` 手机与 `768×1024`、`1024×768` 平板；路由上下文、底栏/Drawer/Sheet、草稿应用/取消、焦点归还、44px target、safe area、卡片列数和 document 零横向溢出 |
| 移动 Player 方向门禁 | reducer/clock 单测覆盖首次竖屏、250ms 抖动、单机门禁拥有的暂停、用户暂停不误恢复、P1/P2 职责和 hidden 优先级；Chrome E2E 覆盖 config-first、竖屏零 iframe/core/game/PlaySession 请求、旋转后单次启动，以及 `568×320`、`667×375`、`844×390`、`932×430` HUD/Sheet |
| 4K 与桌面体验 | 1280×800 最小桌面、2560×1440 与 3840×2160 viewport 的关键页面无失控拉伸、遮挡和不可达操作；Player 保持正确比例 |

响应式与 4K 视觉回归不能只依赖像素快照：E2E 还应断言内容最大宽度、关键控件可见、页面无横向溢出、Player canvas/阻断层在视口内、关键 target 尺寸以及导航层级可达。手机普通页面至少覆盖全部四个固定手机视口，平板覆盖两个固定横/竖视口；移动 Player 横屏至少覆盖四个固定视口。截图用于评审证据，不取代语义断言。

共享 Player 方向/暂停实现进入所有 EmulatorJS core 的执行路径，因此修改其状态机、adapter pause/resume 或 iframe 装载门禁后必须运行 `make web-e2e` 及所有受影响的产品 E2E。当前没有产品 E2E 的核心必须在交付说明中列为未覆盖，不能用直接装载 EmulatorJS 的独立页面补齐。纯移动 CSS、HUD 排版或外围 Sheet 若有调用链证据证明不进入装载、帧执行、配置翻译和存档协议，则不因文件位于 Player 目录自动扩大核心覆盖范围。

影响多盘 parser、Launch content、Player adapter 或换盘时，除受影响单元/集成/Web 测试外还必须执行 `make web-e2e` 与 `ACC-MDISC-001`–`008` 的受影响产品测试。当前没有真实 Saturn ROM 的浏览器产品 E2E；交付时必须明确这一边界，不能用伪 CHD、独立 EmulatorJS 页面或历史截图替代。

影响 `internal/netplay`、联机 manifest、WebSocket、Player netplay adapter 或房间 UI 时，必须运行聚焦 Go/Web 测试、`go test -race ./internal/netplay`、migration/HTTP integration、`make web-e2e`，并按 [`ACC-NP-001`–`013`](./project-acceptance.md#18-联机游玩) 生成当次证据。真实 Case 由 `scripts/acceptance/seed-netplay.py` 校验 `data/game/netplay/` 的本机资源，并使用独立 Chrome process；iframe、同 browser 多 context、静止首帧、mock relay 或历史截图都不能替代双端 confirmed frame 与 digest 收敛。

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
- 用户 ROM/BIOS 统一保存在 Git 忽略的 `data/game/`，只由明确的产品集成/E2E 消费并校验，永不成为默认单元测试或 CI 下载前提。仓库内公开 ROM 只允许使用项目自有、许可清晰、生成源可审查且由 `data-check` 逐字节验证的夹具；当前唯一实例是 `testdata/public-roms/gba-smoke/`，只证明 mGBA 产品链路。

## 10. 后续实施清单

以下顺序用于真正落地质量基础设施，避免先写大量代码再追补规则。

### Phase Q0：工具与命令

1. 创建 Go module、锁定 Go 版本并建立 `cmd/retrom` 最小可构建入口。
2. 在 `web/` 创建 Next.js TypeScript 项目，锁定 Node/npm 约束并提交 `package-lock.json`。
3. 新增 `.golangci.yml`，按第 4 节启用规则、阈值、formatters 与 depguard。
4. 新增 `web/eslint.config.mjs`、严格 `tsconfig.json`、Vitest config/setup、package scripts、`web/next.config.ts` 和 `web/proxy.ts`。`next.config.ts` 负责 standalone、开发 rewrite 与固定隔离头；`proxy.ts` 按 HTTP 契约为动态 HTML 生成逐响应 nonce CSP 并把同一 header 传入 App Router，不得改用静态 nonce 或旧 `middleware.ts`。
5. 新增 `api/openapi.yaml` 与两端生成配置/产物，先覆盖通用 envelope、session/health 与一条代表性 CRUD；实现 `api-generate/api-check`，后续每个 route 必须先扩 schema 再写 handler/UI。
6. 新增根 Makefile，实现第 3 节所有命令；golangci-lint 安装到仓库本地并固定版本。
7. 更新 `.gitignore`：忽略 `bin/`、`.cache/`、`web/node_modules/`、`web/.next/`、coverage/E2E 报告和五份 DAT payload；继续跟踪真实来源 manifest、SHA256SUMS、物化配方与可提交验证清单。

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
3. 新增 `.dockerignore` 与 `web/.dockerignore`，排除 `.git`、缓存、`node_modules`、`.next`、coverage、E2E 报告、`data/game`、本地 runtime 结果和运行数据；构建阶段只通过版本化脚本下载并校验允许进入镜像的固定 runtime artifact。
4. 在 Makefile 实现三个 image targets 和共用 `release-input-digest` helper；两镜像都写入 `io.retrom.release-input-sha256`，组合 target 以 inspect 确认一致。构建完成后立即返回，不创建容器、不建立网络、不挂载卷、不 push registry。
5. PR 的 required quality check 统一执行 `make ci`；涉及 Dockerfile、依赖锁文件、静态/runtime 资产或发布脚本时还必须在合并前验证 `make build-images`。tag 发布不重复执行 quality check。
6. `.github/workflows/docker-image.yml` 在 tag push 时直接执行 `make build-images`；该命令通过镜像内的确定性依赖物化、`data-check`、release-input digest 和双镜像 label 复核完成发布输入校验。两个镜像校验完成后才允许登录 Docker Hub 并推送，流程不等待 Environment 人工批准，也不能用 Action 重新拼装或绕过 Makefile 的发布输入校验。

### 10.1 预期文件

| 文件 | 责任 |
| --- | --- |
| `/AGENTS.md` | Agent 实施铁律 |
| `/api/openapi.yaml`、`/api/oapi-codegen.yaml` | HTTP 事实源与 Go strict stdlib 生成配置 |
| `/internal/httpapi/generated/api.gen.go` | 由 OpenAPI 生成的 Go 类型/strict server 接口，禁止手改 |
| `/migrations/embed.go`、`/migrations/*.sql` | 编译进后端的顺序 migration 与 checksum 输入 |
| `/.golangci.yml` | Go lint、formatter、排除与 depguard |
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
- 领域/存储：026 全新库与受支持旧库升级；STATIC/DAT exact、fallback、同名/重命名、多 Requirement CAS 去重；overwrite off/on、同分/更差、同 bytes、版本漂移与并发安装均证明不降级。
- Worker：完整发现前零安装、扫描门禁、2 hash/1 archive 并发、8 MiB cancel、lease/heartbeat/deadline、崩溃后不重复 revision、瞬时 root 退避与 attempt 耗尽、restore fence。
- HTTP：ADMIN/USER/匿名与 CSRF 矩阵，严格 body/Idempotency/ETag/active conflict，root/directory/list/item/candidate cursor 和 allowlist 投影；BIOS 286 fixture 为 100/100/86，无重复遗漏且全集汇总恒为 286。
- React/Chrome：无配置/不可用/空历史、Drawer 键盘、SSE/cancel/retry、完成/部分失败/候选解释；FULL_CATALOG abort/乱序/重复触发/追加失败/键盘 fallback。分别验证 1280×800、2560×1440、3840×2160、无页面横向溢出及零 serious/critical axe 结果。

该切片除聚焦用例外必须运行 `make api-check`、后端四门禁、`make integration-test`、前端五门禁、`make web-e2e`、fixture 校验、无 core 参数全量 smoke、`ACC-BIOS-003`–`007` 与 `make ci`。测试 source 使用临时目录或操作者授权且 Git 忽略的本地文件，绝不提交 BIOS bytes。

## 12. Pegasus 目录导入与视频测试矩阵

- parser/scanner：UTF-8 BOM、LF/CRLF、续行与 flowing text、字段别名、同一 metadata 多 game、目录内多个 metadata、大小/条目/深度门禁、非法命令值、路径穿越、symlink/special file、来源中途变化和稳定 `sourceKey`。
- 映射/持久化：Migration 030 的新建库与 029 升级同构并保留旧状态/诊断，Migration 031 的新建库与 030 升级同构并保护审核 preview/screenshot Blob 边，Migration 033 的新建库与 032 升级同构并允许受当前来源/目标/CoreArtifact/generation 约束的阻断截图；Collection 显式映射、ETag、版本冻结；最大 64 文件的投影、全部声明文件参与确定性 key、M3U+CHD 有序分组、Arcade 当前 ZIP 与冻结 DAT 依赖闭包内的同目标显式 companion 集。
- 审核/发布/重复：单文件和多盘沿用既有 library import/validation/review/publish 事务；Worker 完成后只产生 `REVIEW_PENDING` 且零 Game，READY 与 blocker 都可在统一队列处理；初始 Arcade Validation 会采用导入前已经安装且匹配当前 CoreArtifact 的 DAT BIOS，生成 `SATISFIED_EXTERNAL` 依赖与 `BIOS_BUNDLE` 文件，真正仍缺 Parent/内容的条目继续阻断。Approve/Discard 原子推进普通与 Pegasus 两组状态/计数，来源 COVER/VIDEO 正确保留，用户封面选择优先。快速审批覆盖完整筛选枚举、preview/create digest 漂移、严格 READY 与截图 override 分界、duplicate/Attachment 排除、逐项原子记账、取消竞争、重启恢复、restore fence、worker-only retry、10,000/10,001 上限和两个并发创建；另以真实 Arcade dependency snapshot schema v2 覆盖预览 candidate 与最终发布，证明它走 Arcade DAT closure/required-entry/ValidationFile 校验而不是 BIOS schema v1 解析失败分支。交接崩溃恢复复用已有内部 ImportItem 且不重复系统草稿事件；未完成交接的 Item 不出现在队列/详情且不能发布。同一来源重扫和内容重复列出全部已有游戏并返回稳定结果；失败/取消不删除审核事项或回滚已经提交的游戏，重试不重复 Game/Revision/Blob。
- Worker/存储：BIOS 与 Pegasus 共用 2-reader limiter；lease/heartbeat/deadline/attempt 耗尽、重启恢复、restore fence、外部 root 变更、媒体告警、保护边 GC 和 backup/restore 均有确定性测试。
- HTTP/UI：ADMIN/USER/匿名/CSRF、strict body、Idempotency、ETag、cursor/filter/SSE；`pegasusImportId` 精确队列筛选、来源媒体 GET/HEAD 与 COVER/VIDEO kind；审核 best-effort preview 锁定现有依赖，READY/阻断均在 `EJS_onGameStart + 5000ms` 优先读取核心最后一帧并上传 PNG，核心截图有界失败时回退 canvas，使静态 ROM/BIOS 错误画面不退化成黑帧；当前阻断截图启用人工发布 override，过期 Validation 拒绝、弹窗失败提示和四个等宽决策按钮。快速审批 UI 覆盖当前筛选的服务端影响预览、零候选/active/stale、进度恢复、取消/retry、终态缓存清理、结果链接与 390/1280/3840 键盘/reduced-motion。Pegasus 双能力卡、三步 Drawer、无默认映射、关闭恢复、同计划轮询重渲染不重置映射/焦点/滚动、详情审核行动区和逐行审核入口保持不变。
- 总览聚合：一个包含多个游戏的 PegasusImport 只能贡献一个最近任务和一个顶层批次；其逐游戏内部 ImportJob 不进入普通任务分页。进行中/完成/异常批次、处理中条目、异常条目和实际待审核 Item 分别按正式口径断言，主动取消不误报为异常，最近三条不能反向决定流水线数字。
- VIDEO：MP4/WebM magic 与限额、nullable dimensions、Range/HEAD/MIME、不可变 revision、元信息编辑保留、删除保留历史；详情 2 秒累计可见自动播放、后台页不计时、5 秒/拒绝/错误回退、用户暂停和 reduced-motion 手动模式，以及列表零视频请求。

该切片除聚焦用例外必须运行 `make api-check`、后端四门禁、`make integration-test`、前端五门禁、`make web-e2e`、`ACC-PEG-001`–`005`、`ACC-MEDIA-001` 与 `make ci`。使用操作者授权的真实 Pegasus 目录时只记录相对统计和结果，不把 ROM、完整宿主路径或媒体内容写入报告。

## 13. 游戏标签测试矩阵

- migration/store：034 新库与 033 升级、表/列/partial unique/index/trigger/INTEGER 时刻/FK、DELETED 不可恢复、同名新 ID、20/21 owner 上限，以及 backup/restore 对 tombstone、关系和审计的保真。
- Tagging/HTTP：NFC、Unicode whitespace/case-fold/control、40/41 code point、160/161 byte、1,000/1,001 实例上限；CRUD/usage/cursor/filter/sort、ADMIN/USER、strict JSON/CSRF/If-Match/Idempotency、同名并发、关系 no-op、delete 与 assignment 两种提交顺序、版本联动和审计。
- 搜索/投影：Game/Admin/Review 的 `q/tagId` 在 SQL 分页前取交集，cursor 不跨筛选复用；Favorite/Recent/Save/Netplay 与 detail 的数组始终非 null、名称稳定排序、删除立即隐藏且列表批量读取无 N+1。
- Import/Review/Pegasus：批次默认标签、多 Item 继承、reconfigure、逐项 autosave、删除后的旧 ETag、Approve 原子复制、Discard snapshot；逐 Collection 集合、SKIP 空值、mapping 恢复/start 漂移/retry/handoff 幂等和外部 metadata tags 不自动关联。
- React/Chrome：TagPicker 键盘/20 上限/空 taxonomy，管理页 loading/empty/error/conflict/Drawer/Dialog 焦点，导入/Pegasus/审核/管理员游戏写入，Library/Admin/Favorite/Recent/Save/Netplay 的名称搜索和精确 URL 恢复；390/1280/2560/3840 无页面横向溢出且 axe serious/critical 为零。

该切片运行 `make api-check`、后端四门禁、`make integration-test`、前端五门禁、`make web-e2e`、`ACC-TAG-001`–`005` 与 `make ci`。标签不进入 EmulatorJS、内容字节、Variant 或存档协议，因此不因本切片运行 core smoke、fixture 或依赖基线；若实际调用链改变则重新判定。

## 14. 维护规则

- 升级 Go、Next.js、ESLint、TypeScript、Vitest 或 golangci-lint 时，单独提交配置变化，阅读迁移说明并运行完整 `make ci`；不能把工具升级与大功能混在一起掩盖行为变化。
- 新 linter 先证明信噪比和修复现有问题，再加入显式 enable；禁止用长期 `new-from-rev` 只检查新增代码形成双重标准。
- 如果一条规则确实不适合 Retrom，应记录具体误报、替代保护和评审结论；不得因修复成本高直接关闭。
- 测试章节只记录必须覆盖的行为和命令，不写某次执行的测试数量或 PASS 日志。
- 镜像名、Docker build context、`make dev` 进程模型或 TLS 终结边界发生变化时，必须同步更新本文和部署专题；不得把部署环境差异硬编码进前端构建产物。
- 当目录、命令或技术栈发生变化时，同步更新根 `AGENTS.md`、本文和实际配置，保证三者一致。
