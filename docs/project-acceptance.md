# Retrom 一期项目验收规范

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期唯一验收基线 |
| 版本 | 1.6 |
| 日期 | 2026-08-13 |
| 执行者 | AI Agent，必要时由人工复核当前运行生成的画面证据 |
| 范围 | 工程质量、镜像、本地开发、账户认证与隔离、游戏目录、导入审核、BIOS/DAT、存储、安全、运行时、联机、35 核兼容性、PSP ISO/CSO 和 4K UI |

## 1. 文档职责

本文是 Retrom 一期所有项目验收 Case 的唯一事实源。领域文档负责解释为什么这样设计，本文负责说明如何执行、什么结果才算通过。专题文档不得另建一份验收清单；新增或修改稳定行为时，必须在本文新增或更新 Case ID，并从专题回链。

“验收通过”不是浏览页面后主观判断，也不是只运行一次 build。每个 Case 必须：

- 有固定 ID、明确前置条件、可直接执行的命令或有限 UI 步骤；
- 使用隔离数据和固定夹具，能够由另一 Agent 重复得到相同结论；
- 有明确的通过/失败边界和本次运行产生的证据；
- 有硬超时，不允许无限等待、soak、压力测试或依赖长时间观察；
- 发现 bug 后按工程质量规范先固化回归用例，再重新执行原 Case。

支持事实与设计依据仍由以下专题维护：

- [产品与架构总览](./retrom-product-architecture.md)
- [工程质量、Lint 与测试规范](./engineering-quality-and-testing.md)
- [游戏目录领域设计](./platform-instance.md)
- [导入、刮削与审核](./import-and-review.md)
- [BIOS 与 Arcade DAT](./bios-and-arcade.md)
- [核心运行时验证基线](./core-runtime-validation.md)
- [运行时、启动与游玩数据](./runtime-and-play-data.md)
- [存储与数据库](./storage-and-database.md)
- [后端、API 与运行维护](./backend-api-and-operations.md)
- [一期数据库实体与不变量](./data-model.md)
- [HTTP API、上传与启动凭据契约](./http-api-contract.md)
- [第三方运行时与 DAT 依赖管理](./dependency-management.md)
- [UI 与交互规范](./ui-specification.md)

## 2. 禁止的验收方式

一期验收不包含：

- soak、耐久、压力、峰值吞吐、容量规划或长时间资源泄漏观察；
- 依靠 `sleep` 等待 lease、过期、心跳或重试，相关 Case 必须使用 fake clock 或可控时钟；
- 依赖真实 Hasheous、浮动 CDN 或其他不可控公网响应作为通过条件；
- 扫描用户整个游戏目录、批量试玩 ROMset 或推断“一个样本通过等于核心全部兼容”；
- 只看 HTTP 200、EmulatorJS 外壳、静止第一帧或旧截图就判定游戏可运行；
- 在用户真实数据库/CAS 上做 destructive 测试；
- 为了通过验收而跳过用例、放宽 lint、更新错误快照或把失败标为 warning。

并发、恢复和 GC 仍需验收，但只使用有界输入：默认不超过 8 个任务、16 个 Blob 或 2 个并发执行者。单 Case 的最长硬超时见第 3.4 节。

## 3. 验收运行契约

### 3.1 必需环境

- Linux 开发/CI 环境，仓库根目录为当前目录；
- 仓库锁定的 Go、Node.js/npm、golangci-lint 和依赖；
- Chrome 与 Playwright；只验收 Chrome，不验收其他浏览器或移动端；
- 构建镜像 Case 需要 Docker daemon，但不授权启动容器；
- 可选的已部署代理 Case 需要一个已由 NG 暴露的 HTTPS 地址，通过 `RETROM_ACCEPTANCE_BASE_URL` 提供；没有部署环境时只有明确标注的条件 Case 可为 `NOT_APPLICABLE`；
- 用户有权使用的 ROM/BIOS 只保存在本地 `data/game/`，不进入 Git 或验收报告。

首次准备依赖和用户授权二进制夹具可以执行：

```bash
make prepare-deps
# 由操作者在 shell 中提供 RETROM_FIXTURE_HOST 与 RETROM_FIXTURE_ROOT；不要写入仓库
data/example/fetch-fixtures.sh
python3 data/example/verify-fixtures.py
```

依赖/夹具下载是验收前准备，不计入单 Case 时长；正式计时期间只运行离线 `make deps-check`。`verify-fixtures.py` 必须在核心 Case 前通过；无法取得用户授权夹具时，相关核心 Case 为 `BLOCKED`，不能把项目判为通过。fixture manifest 只保存来源相对路径与 hash，不保存主机名、远端绝对路径或凭据。

### 3.2 隔离数据与固定种子

后续实现必须提供统一入口：

```bash
make acceptance-prepare
make acceptance-case CASE=ACC-XXX-000
make acceptance-report
```

以上三个 target 是项目实现的一部分，也是验收 Agent 的稳定入口；任一 target 缺失或无法按约定工作时，对应 Case 直接 `FAIL`，不得临时改用一组未记录的手工命令绕过。除 Case 另列附加前置外，每个 Case 的公共前置都是：`make acceptance-prepare` 已通过、当前目录为目标 commit 的仓库根目录、验收环境文件已加载、固定 seed 未被改写。

- `acceptance-prepare` 先离线执行 `make deps-check`，再在明确的临时目录创建全新 SQLite、CAS、Hasheous stub 和安全负向夹具；它不联网、不读取或删除用户运行数据，输出本次 `run_id` 和环境文件。
- `acceptance-case` 每次只运行一个 Case，负责启动/停止本 Case 需要的本地进程、重置确定性种子并执行硬超时。
- `acceptance-report` 只聚合已有结果，不重新运行 Case。
- 实现可由 `scripts/acceptance/run.sh` 承载，但 Make target 和 Case ID 是稳定接口。

除 `ACC-QA-003`、条件 Case `ACC-DAT-006` 和最终报告这三个明确的聚合步骤外，所有 Case 都必须能在只运行自身的情况下复现，不得依赖前一个 Case 遗留的数据库状态或进程。

固定种子至少包含：

| 类型 | 固定值 |
| --- | --- |
| 账户/Profile | `test`：`ADMIN/ENABLED`；`alice`：`USER/ENABLED`；`disabled`：`USER/DISABLED`；三者各绑定不同固定 UUIDv7 Profile，不存在 `local` Profile |
| Fake clock | 初始 `now_ms = 1786000000000`，所有等待、过期、lease 与时长只由 runner 显式推进 |
| Arcade 游戏目录 | `acc-arcade-fbneo` → `fbneo`；`acc-arcade-mame` → `mame2003` |
| 普通游戏目录 | `acc-nes-fceumm`、`acc-snes-snes9x`、`acc-gb-gambatte`、`acc-gba-mgba`、`acc-dos-pure` |
| 已发布目录 | 使用本地合法夹具为上述游戏目录创建固定 Game、MetadataRevision、Asset、GameContentRevision/ContentFiles 和可运行 GameVariant/VariantRevision；标题、排序键及 ID 固定 |
| 游玩数据 | 一条已完成 PlaySession、一条最近记录和一份带固定 PNG 的兼容 SaveState，所有时间相对 fake clock 固定 |
| 入库数据 | 小规模的 queued/running/review-pending/failed Item 与 approve/discard ReviewEvent，供总览、任务、待审核和历史页面使用 |
| Hasheous stub | 命中、未命中、429、超时、500、401 和畸形响应七种固定路由，不要求凭证 |
| ROM/BIOS | `data/example/fixtures.json` 中的固定路径、大小和 SHA-256 |
| 联机 | `test` 与 `alice` 分别作为 P1/P2；`fceumm-423-f1race-v1` 与 `fbneo-423-ldrun-v1` 的内容、core、adapter 和依赖 digest 来自 `data/netplay/v1/manifest.json`；两个浏览器必须为独立 Chrome process |
| 游戏替换 revision | 基于 `fceumm` 真实本地夹具确定性重打包：ROM entry 字节不变，ZIP 时间固定且 comment 为 `retrom-acceptance-revision-2`；原始 Blob SHA-256 必须变化，提取内容 hash 必须不变 |
| 媒体 | Hasheous stub 提供一张固定字节的小型合法 PNG，SHA-256 写入 seed manifest |
| 用户 DAT 候选 | 将 `make prepare-deps` 物化的真实 `data/dat/emulatorjs/4.2.3/fbneo/fbneo-arcade.dat` 逐字节作为用户上传输入；允许 CAS 去重，但 DatVersion/安装记录必须独立 |
| 非法 BIOS | 临时目录内生成内容为 `retrom-invalid-bios\n`、逻辑文件名为 `gba_bios.bin` 的文件 |
| 数据库 | 空库及每个受支持 migration 起始版本各一份最小确定性 fixture |
| 安全夹具 | 小型 path traversal、绝对路径、symlink、超压缩比和 XXE 文件；不得真实展开超大内容 |

### 3.3 证据目录与状态

所有本次运行证据写到被 Git 忽略的：

```text
.artifacts/acceptance/<run_id>/
  run.json
  cases/<case_id>/result.json
  cases/<case_id>/stdout.log
  cases/<case_id>/screenshots/*.png
  cases/<case_id>/network.json
  defects.json
```

`result.json` 至少记录 `caseId`、`status`、`startedAtMs`、`finishedAtMs`、`durationMs`、实际命令、Git commit/dirty 状态、fixture manifest SHA-256、断言和证据相对路径。所有时刻使用 Unix 毫秒整数。

状态只有：

- `PASS`：本 Case 全部步骤与标准满足；
- `FAIL`：行为、命令、视觉或证据任一不满足；
- `BLOCKED`：缺少明确外部前置条件，例如用户 ROM/BIOS 或 NG 验收地址；
- `NOT_APPLICABLE`：仅允许标记带“条件 Case”的项目，并写明条件为什么不成立。

Required Case 出现 `BLOCKED`、缺失结果或超时都不能通过项目验收。失败后重跑成功不能抹掉首次失败；报告必须保留缺陷与回归测试映射。

### 3.4 短时执行规则

| Case 类型 | 单 Case 硬超时 |
| --- | --- |
| 单元/集成/API/数据 | 120 秒 |
| 三份真实 DAT 的冷库全量解析 | 300 秒 |
| 静态规则哨兵、备份或三项以内的恢复流程 | 300 秒 |
| UI/Chrome E2E | 180 秒 |
| 单个 EmulatorJS Core | 180 秒 |
| `make dev` 生命周期 | 180 秒 |
| `make ci` | 900 秒 |
| 单个镜像冷构建 | 900 秒 |

镜像首次下载基础层、npm/Go 依赖或 EmulatorJS 固定发布物属于环境准备；正式计时前允许完成一次受哈希约束的缓存预热。超时后 runner 必须终止自己的进程树并标记 `FAIL`，不得自动增加等待时间或无限重试。

### 3.5 AI Agent 执行纪律

1. 记录工作树状态和目标 commit，不回滚用户改动。
2. 执行 `acceptance-prepare`，确认临时数据根不指向用户目录。
3. 按第 4 节顺序逐 Case 执行；不要一次启动无人监管的长任务。
4. 视觉 Case 必须查看本次时间戳生成的截图；不得沿用设计稿或历史 PASS 图。
5. 每个 Case 结束立即记录状态和证据，运行中超过 60 秒应向用户提供简短进度。
6. 发现 bug 立即登记 defect，先增加能在旧实现失败的回归测试，再修复和重跑；未经授权不得通过改规范规避。
7. 全部完成后运行 `make acceptance-report`，按第 20 节判定项目结论。

## 4. 执行顺序

推荐顺序用于减少无效的 UI 调试：

1. `ACC-QA-*`：静态质量和回归纪律；
2. `ACC-PKG-*`、`ACC-DEV-*`、`ACC-NET-*`：构建、进程与代理边界；
3. `ACC-DB-*`、`ACC-CAS-*`、`ACC-SEC-*`、`ACC-API-*`、`ACC-OPS-*`：基础设施；
4. `ACC-AUTH-*`、`ACC-ISO-*`：账户生命周期、权限与私有数据隔离；
5. `ACC-PLAT-*`、`ACC-GAME-*`、`ACC-IMP-*`、`ACC-DAT-*`、`ACC-BIOS-*`、`ACC-PEG-*`、`ACC-MEDIA-*`：管理与入库；
6. `ACC-RUN-*`、`ACC-SAVE-*`、`ACC-PLAY-*`：产品主路径；
7. `ACC-CORE-*`：逐核心真实画面；
8. `ACC-MDISC-*`：多盘导入、运行、回归与隔离；
9. `ACC-NP-*`：联机房间、真实双端运行、恢复、安全与单机回归；
10. `ACC-UI-*`：信息架构、4K 和无障碍；
11. 缺陷回归审计与最终报告。

除明确写明直接命令的 Case 外，执行命令统一为：

```bash
make acceptance-case CASE=<case-id>
```

## 5. 工程质量、镜像、本地开发与网络边界

### ACC-QA-001：完整代码质量门禁

- 上限：900 秒。
- 执行：`make ci`。
- 流程：从依赖锁文件安装工具；先验证 OpenAPI 3.0.3 能由固定 `oapi-codegen v2.8.0` 与 TypeScript 生成器处理且两端生成物无漂移，再依次执行 Go format/build/test/lint、Web lint/typecheck/test/production build、integration test 和提交数据基线校验。
- 通过标准：命令退出码为 0；Go/ESLint 为零 warning；OpenAPI/Go/TS generated diff 为空；没有跳过 required suite；报告列出实际固定工具版本。
- 证据：完整 stdout/stderr 和各子 target 耗时。

### ACC-QA-002：质量规则哨兵

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-QA-002`。
- 流程：runner 在临时副本分别注入未处理 Go error、floating Promise、`*_at_ms TEXT` migration 和可穿越路径输入；逐一运行对应 lint/test，然后丢弃临时副本。
- 通过标准：四个错误均被预期门禁拒绝，主工作树 hash 前后相同；任一哨兵被放过即失败。
- 证据：四个预期失败命令、命中的规则/测试名和主工作树校验值。

### ACC-QA-003：验收缺陷回归映射

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-QA-003`。
- 流程：读取本次 `defects.json`；对每个已修复缺陷解析回归测试路径、测试名、修复前失败证据和修复后命令结果。
- 通过标准：没有缺陷时明确记录空数组；存在缺陷时每项都有永久自动化回归用例，且聚焦测试与受影响完整门禁通过。只写文字说明或手工步骤不能通过。
- 证据：defect → regression test → command 三方映射。

### ACC-PKG-001：后端镜像构建

- 上限：900 秒。
- 执行：`make build-backend-image`，随后 `docker image inspect retrom:latest`；用 `docker image save` 到验收临时目录检查最终 image layer 文件清单，不创建/启动容器。
- 流程：先记录 `make release-input-digest`，只构建根 Dockerfile；检查镜像名、非 root User、HTTP 入口、最终镜像配置/发布输入 label、`THIRD_PARTY_NOTICES`、两个运行时 manifest 共 38 个许可 component、依赖 manifest allowlist、五份 DAT 以及密码 blocklist/许可；从 manifest 在验收临时目录重建 notice 并逐字节比较。
- 通过标准：默认 target 为 `retrom:latest`，`io.retrom.release-input-sha256` 等于包含密码 manifest digest 的本次 helper 值；所有 runtime/DAT/license/blocklist artifact 命中固定 hash。最终文件包含 36 个跨版本 selected core/report 条目（合并为 35 个 enabled core）、PPSSPP assets、五份 DAT、38 个运行时许可 component、10,000 行密码 blocklist及 MIT 许可，但不包含下载 archive、非 allowlist core、用户数据、源码/缓存、TLS 私钥或开发启动命令；被忽略 payload 未被 Git 跟踪且构建不 push。
- 证据：build log、image ID、RepoTags、User、Entrypoint/Cmd、最终 layer 文件/size 清单、Git tracked-file size 检查和 artifact 校验摘要；不启动容器。

### ACC-PKG-002：前端镜像构建

- 上限：900 秒。
- 执行：`make acceptance-case CASE=ACC-PKG-002`。
- 流程：runner 记录 `make release-input-digest`，调用 `make build-web-image` 并 inspect `retrom-web:latest`；确认 target 在编译生产代码前执行 `data-check`，检查 manifest 的 `player_adapter` 与 `web/features/player/adapters/registry.json` 双向一致且每个登记项有实现，再检查 standalone production 产物、非 root User 和内部 HTTP 入口。最后在临时工作树副本把 manifest adapter ID 改为未知值，运行同一 `data-check` 并要求预期失败；不在主工作树留修改，也不对负向样本再构建镜像。
- 通过标准：默认目标 tag 为 `retrom-web:latest`，`io.retrom.release-input-sha256` 等于本次 helper 值；基线 `ejs-4.2.3-v2` 精确映射版本 `4.2.3`，runtime base/loader 路径命中 manifest allowlist，未知或无实现登记项使临时副本的校验失败；镜像没有开发依赖/缓存、内置后端地址、TLS 私钥或用户数据，Cmd 不是 `next dev`。
- 证据：build log、image inspect/digest 摘要与负向 `data-check` 错误；不启动容器。

### ACC-PKG-003：镜像 Target 只构建不运行

- 上限：900 秒。
- 执行：`make acceptance-case CASE=ACC-PKG-003`。
- 流程：先检查 `make -n build-images`；若出现 `run`、Compose、registry login/push 或部署命令则停止并失败。静态检查通过后记录 `make release-input-digest`、`docker ps -aq --no-trunc`、Docker network 和 volume ID；运行无参数 `make build-images`；再次记录并比较，inspect 两个 image label。
- 通过标准：新增/更新的只有默认 `retrom:latest`、`retrom-web:latest` 及 build cache；两个 `io.retrom.release-input-sha256` 完全相同且等于预先记录的 helper 值；容器、网络、volume 数量和 ID 完全不变，没有 registry login/push、Compose 或服务启动调用。
- 证据：前后快照、两个 image ID 和构建命令。

### ACC-DEV-001：`make dev` 仅启动宿主机进程

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-DEV-001`。
- 前置：验收准备已完成，`make deps-check` 离线通过。
- 流程：把会记录调用并退出 99 的 `docker` 哨兵放在临时 `PATH` 首位，以显式 `RETROM_MODE=test` 启动未覆盖 `NEXT_DEV_HOST` 和 `RETROM_SERVER_IMPORT_ROOTS` 的 `make dev`，并把 public origin 设为确定性的开发域名；确认依赖离线命中、后端收到 `--mode=test` 且环境中不残留未知的 `RETROM_MODE`。等待两端 ready 后用 `test/test` 登录，通过前端 origin 请求 `/api/v1/home`，并读取本地扫描 roots 投影。再经前端端口发送字段完整但未认证的联机 WebSocket upgrade，并保持 HMR upgrade。随后执行 supervisor 正常接管、`SIGKILL` 后孤儿 process group 接管和伪造登记身份矩阵，最后安全停止。
- 通过标准：test 空库只创建一个 `test` ADMIN/Profile，登录页有测试警告，认证后的 rewrite 同源成功；联机 upgrade 到达 Go 并返回 `401 AUTHENTICATION_REQUIRED`，HMR 仍为 101；仓库 `.dev-data/bios` 与 `.dev-data/roms` 已幂等创建，API 分别只投影 `local-bios`/“本地 BIOS”和 `local-roms`/“本地 ROM”两个状态为 `AVAILABLE` 的 root，且不暴露绝对路径；其余进程接管、监听、Docker 哨兵、身份保护和退出约束全部满足。默认 release 不创建测试账号，由 `ACC-AUTH-002` 独立证明。
- 证据：进程树、健康/登录/首页/root HTTP 结果、HMR 与联机 upgrade status 和退出后的 PID 检查。

### ACC-NET-001：应用侧代理契约与同源隔离

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-NET-001`。
- 流程：以正常 `make dev` 的 `http://localhost:3000` 同源入口运行 Chrome，先登录，再连续两次完整 navigation 并采集页面、`/_next`、`/api/v1/home`、`/runtime/emulatorjs/4.2.3/data/loader.js` 和一个 seed Asset；同时执行既有 nonce、监听、可信/不可信转发头、production CSP 与 TLS 能力扫描。
- 通过标准：localhost 单一 origin 下 `window.isSecureContext === true`、`window.crossOriginIsolated === true`、`SharedArrayBuffer` 可用；每次 HTML response 的 nonce 均非空且彼此不同，CSP、转发 request nonce 和 Next framework script nonce 一致；开发 CSP 只额外允许 `unsafe-eval`，production CSP 不含它并只开放文档锁定的 self/blob/wasm 能力；页面没有共享静态 HTML/ISR/PPR，控制台没有 CSP 回退/CDN 请求；COOP/COEP/CORP/`nosniff` 覆盖页面、iframe 和 runtime。应用只提供内部明文 HTTP 且没有 TLS 管理能力；非受信转发头无效，受信代理值只在精确 allowlist/公开 origin 校验后生效；内部地址未进入 browser bundle。
- 证据：network trace、CSP/隔离头、浏览器断言、监听 socket、代理请求矩阵和应用配置摘要。

### ACC-NET-002：已部署 NG 的 HTTPS 责任边界（条件 Case）

- 上限：180 秒。
- 条件：提供了由实际 NG 暴露的 `RETROM_ACCEPTANCE_BASE_URL`；未提供时为 `NOT_APPLICABLE`，不阻塞未部署源码的一期验收。
- 执行：`make acceptance-case CASE=ACC-NET-002`。
- 流程：Chrome 打开该 HTTPS 地址并采集与 `ACC-NET-001` 相同的页面/API/runtime/asset；检查浏览器连接证书、HTTP→HTTPS/HSTS 的响应方，以及 NG 到两个应用上游的明文边界。
- 通过标准：浏览器只看到一个 HTTPS origin且 secure/cross-origin-isolated/SAB 断言通过；证书、重定向和 HSTS 由 NG 提供，Retrom 两个应用仍只监听内部 HTTP；NG 不删除或放宽 nonce CSP、COOP/COEP/CORP/`nosniff`，不把内部地址写入前端。
- 证据：外部 network trace、证书/响应头摘要和上游监听配置；不得复制私钥。

## 6. 数据库、CAS、安全、API 与运维

### ACC-DB-001：全新数据库与整数时间字段

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-DB-001`。
- 流程：在空临时目录运行真实 migration；枚举全部表、列、外键、索引和 trigger；在一个事务创建 Game/MetadataRevision/GameContentRevision 与 GameVariant/VariantRevision 的三个闭合 current pointer；尝试跨 owner content/metadata/variant pointer、VariantRevision 引用其他 Game 的 ContentRevision、修改/删除 append-only revision、重复 active BIOS/DAT、冲突 whole/file Upload consumption、无效平台/core 关系和负数 duration；读取一条 API JSON 与 SQLite `typeof()`。
- 通过标准：所有业务时刻以 `*_at_ms INTEGER` 存储并通过 API 输出 JSON integer；时长为有单位的整数；不存在业务时刻 TEXT、`CURRENT_TIMESTAMP` 主存储或单位不明字段。循环 current pointer 外键为 `DEFERRABLE INITIALLY DEFERRED` 且合法事务可提交；全部负向约束在数据库层拒绝；数据字典要求的 partial unique index、外键索引和 append-only trigger 均存在。
- 证据：完整 schema 摘要、合法事务、每个负向 SQL 结果和 API 响应。

### ACC-DB-002：顺序迁移与版本保护

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-DB-002`。
- 流程：读取 `migrations/testdata/supported_versions.json` 的有序整数版本清单，对每个仍受支持的真实旧版本 fixture 逐级迁移到当前版本；再次启动当前 schema 验证幂等；构造 migration 020 之前的匿名 `local` 数据库和比二进制更新的 schema version。账户版本明确使用全新数据根，前账户数据库只执行只读识别。
- 通过标准：清单与 fixture 一一对应、无未登记 fixture；受支持版本升级后 schema 与全新库同构且 `foreign_key_check` 无结果。前账户数据库以 `DATABASE_REBUILD_REQUIRED` 拒绝，文件 hash、WAL/SHM、schema version 和业务行逐字节/逐项不变；重复启动不重复变更，未来 schema 被快速拒绝且不会写库。
- 证据：支持版本清单、各实际起始/最终 schema 摘要、行数/hash、二次启动结果与未来版本拒绝日志。

### ACC-CAS-001：SHA-256 去重与原子写入

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-CAS-001`。
- 流程：对同一小型合法 fixture 进行两次顺序和两次并发上传，并故障注入一次中途写失败。
- 通过标准：物理 Blob 只有一个，逻辑引用数量正确；路径由 SHA-256 推导；失败写入不留下可见半文件或数据库孤儿引用。
- 证据：Blob/引用查询、CAS 路径和临时目录清单。

### ACC-CAS-002：引用保护与单轮 GC

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-CAS-002`。
- 流程：创建旧 GameContentRevision/VariantRevision、未删除 SaveState、失败但可重试的游戏文件替换 Job/Upload、审核证据与孤儿 Blob；另建两份小型 archive 并物化各自一个 entry，一份仍被 UploadFile 保护，一份已过期且无消费/复合 entry 引用。运行一轮 GC；软删除 SaveState 并移除其余引用，先在 7 天保留期内再运行；随后用 fake clock 推进超过配置保留期并再运行一轮，同时故障注入一个并发新引用。输入不超过 16 个 Blob。
- 通过标准：被 ContentRevision、VariantRevision、未到期软删除 SaveState、Import/游戏文件替换 Job 或审核历史引用的 Blob 均保留；有业务根的 archive、ArchiveEntry 及已物化内层 Blob 一起保留。保留期后 SaveState 行才可物理清除；无业务根的过期 archive 不被自身 ArchiveEntry 永久保活，GC 事务成组删除其 entry/外层 Blob，无其他引用的物化内层 Blob 在自己的后续保留期到期后删除；`blob_gc_candidates` 自身不阻止回收。解除全部引用后只有目标孤儿被删除；删除前新增的并发引用不会被误删。
- 证据：三轮前后带边分类的引用闭包、ArchiveEntry/文件清单、fake clock 推进值和 GC 决策日志。

### ACC-BKP-001：备份与空目录恢复

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-BKP-001`。
- 流程：在验收库写入三个 User/Profile、游戏、Blob、用户 DatVersion、私有存档、一条未完成 UploadPart、ACTIVE AuthSession/AccountLink/LaunchSession，以及一条 RUNNING `SERVER_BIOS_IMPORT` Job/ServerImport；另建 GC 宽限期 Blob、crash orphan 和受保护 archive。保持服务运行调用一次 `retrom backup` 验证拒绝，再正常停止服务，备份到不存在的临时输出路径。完成既有 manifest/依赖负向矩阵后恢复到第二个不存在的数据根，启动恢复服务，分别用旧认证 cookie、账号链接与 launch capability访问，再用原密码重新登录并核对三个 Profile 的私有数据。
- 通过标准：既有 bundle 结构、mode/hash、CAS/registry、依赖和负向恢复约束全部满足；外部 source bytes/root 不进入 bundle。User/Profile/credential 与私有数据的非围栏行数和摘要一致。restore 在开放 HTTP 前用单事务撤销全部非终态 AuthSession、未使用 AccountLink 和非终态 LaunchSession，并把外部 source Job/ServerImport 置为不可重试 `FAILED/SERVER_IMPORT_SOURCE_NOT_RESTORED`，写一条不含 ID/secret 的 `RESTORE_SECURITY_FENCE` 审计；旧 cookie/link/capability 全部失败，启用用户可用原密码重新登录并只能看到自己的原数据。清单/日志不含密码 hash、session/link/capability/key 明文、BIOS 内容或完整宿主路径。
- 证据：脱敏 canonical `backup.json`、bundle tree/mode、负向错误矩阵、恢复检查、key equality boolean、cookie 请求结果和前后摘要 hash。

### ACC-SEC-001：Archive 与 XML 输入安全

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-SEC-001`。
- 流程：分别提交固定的小型 `../`、编码 traversal、绝对路径、symlink、条目数/压缩比超限，以及外部实体、内部/参数实体、未闭合/超限/重复 DOCTYPE XML 夹具；另让三份真实基线经过同一 parser。对服务器 root 再覆盖相对路径 traversal、途中/末端 symlink、special file、目录/文件/候选/hash bytes/depth 门禁和最终复制前 source 替换。
- 通过标准：恶意输入在写出授权目录或创建 BIOS Installation 前以稳定 code 拒绝；服务器扫描命中任一门禁时零安装，API/日志不含绝对 root、basename、hash 或底层 `os.PathError`。没有外部实体访问、DNS、宿主文件读取或目标外文件，返回稳定 4xx 而非进程崩溃。真实 FBNeo PUBLIC DOCTYPE 和 MAME 内部 DTD 均被安全 scanner 跳过且统计命中 manifest；实现没有联网 DTD parser、正则删 DTD 或预置专用解析旁路。
- 证据：每个夹具的错误码、临时目录前后清单和无外连记录。

### ACC-SEC-002：Launch capability、内容范围与缓存

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-SEC-002`。
- 流程：用 `alice` 登录后，在临时数据根首次启动并检查 key 权限；以同一主体的 Idempotency-Key/body 并发重放，创建只授权其 Profile/VariantRevision 的 LaunchSession；执行既有 cookie、时钟、finish、路径、Range、新浏览器 context 和 key 负向矩阵。再由 `test` 管理员探测同一 launch/save logical path，并在停用 `alice` 与 restore 围栏后重试原 capability。
- 通过标准：既有 capability 生成、cookie、TTL、范围、缓存、Range 与脱敏约束全部满足；LaunchSession 不可变绑定 `alice` Profile，普通管理员身份不能替代 capability 或读取其私有内容。停用/删除创建者或 restore 围栏立即撤销未结束 Launch，原 capability 后续 config/heartbeat/save 全部失败且不新增私有数据。
- 证据：cookie/数据库摘要（capability 只记录不可逆 hash）、请求矩阵、缓存/Range 响应头、新 context trace 和脱敏扫描。

### ACC-SEC-003：认证写请求的同源与 CSRF 边界

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-SEC-003`。
- 流程：对同一受保护写路由发送匿名、只有 cookie、缺/错 Origin、`Sec-Fetch-Site: cross-site`、缺/错 `X-Retrom-CSRF`、完全合法六组请求；再从不可信来源伪造 `X-Forwarded-*`，从精确可信代理发送正确公开 origin。另覆盖无需 cookie 的 setup/link capability 写入与跨站负向请求。
- 通过标准：匿名为 401，已登录写入必须同时命中精确公开 Origin、非 cross-site Fetch Metadata、session 对应 CSRF header；失败均在读取业务 body/写状态前拒绝。可信代理仅按 allowlist 解析；setup/link capability 不要求 CSRF token但仍执行 Origin/Fetch 门禁。无宽松 CORS，登录和 secret response 均 `private, no-store`。
- 证据：请求矩阵、数据库结果和响应头。

### ACC-SEC-004：Hasheous 媒体 SSRF 与不可信展示数据

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-SEC-004`。
- 流程：使用本地 fake resolver/HTTP transport 模拟允许的 lookup 与 `/api/v1/images/<id>` PNG，以及 lookup 超过 4 MiB/20 candidates/每 candidate 媒体上限、媒体 run 累计超过 100 MiB、redirect 到 loopback/private/link-local/metadata 地址、非 443 端口、第四次 redirect、单项超过 10 MiB、40 MP、伪造 MIME、SVG/HTML；候选文本包含 HTML/script payload。
- 通过标准：lookup 在 15 秒/4 MiB 和候选数量门禁内解析；超限 response 记为 INVALID_RESPONSE 而非截断成功。每个 QueryAttempt/Response/Candidate/Hit/Asset 有同 run 的可验证归属，MISS/错误 response 也能经 attempt 回放；只有 READY candidate asset 可被 ReviewDraft/MetadataRevision 采用。图片只有在 HTTPS `hasheous.org` allowlist、最多 3 次 redirect、≤10 MiB、run 合计≤100 MiB、≤40 MP、声明为受支持图片且魔数/解码均为 PNG/JPEG/WebP 时写入 CAS；受支持图片子类型错标按解码出的真实格式保存，非图片声明、无效魔数和解码失败仍拒绝。每次 DNS/redirect 后都重检 IP，所有负向输入无外网/内网读取且不落 Blob。候选文本通过 JSON/React 作为纯文本呈现，无 `dangerouslySetInnerHTML`、脚本执行或 SVG 渲染。
- 证据：fake transport 请求图、每个拒绝码、CAS 前后清单、DOM 文本与无脚本执行断言。

### ACC-API-001：API 通用契约

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-API-001`。
- 流程：先用固定生成器/validator 加载 OpenAPI 3.0.3；覆盖正常读取、未知/重复 JSON 字段、无效 UTF-8、多个顶层 JSON/depth 65、未知/重复 query、未授权、不可见资源、不存在、缺少/错误 If-Match、上传过大、可修复业务校验、限流、Idempotency-Key 的同语义 JSON 不同 key 顺序、异 body/path/If-Match、两个并发相同请求及 SaveState/PersistentSave streaming 摘要，并单独验证 UploadPart 的 path/range/digest 永久幂等，以及 cursor 正常翻页、篡改、过期、超长和错 route/filter 复用；对待审队列另以两个 ImportJob 的交错 Item 验证 `importJobId` 精确筛选、cursor 绑定、封闭摘要字段和宿主路径脱敏。并发发送普通 JSON 与三种流式请求，确认 validator chain 选择互不污染；再让代表性成功/错误 response 通过 schema contract test。对 Import SSE，先在事务内注入两个 scope 交错的事件，无 `Last-Event-ID` 连接后记录 snapshot ID，再注入新事件并以该 ID 重连；另对一个通用 Job 注入与其他 Job 交错的事件，重复无 cursor snapshot、合法跨 Job 水位、重连和非法/超前水位矩阵。覆盖 Launch 的四个合法 `returnTo` 和 origin/query/fragment/percent-encoding/不同 game ID 负向值，以及 NEEDS_VALIDATION 的 202/no-cookie、旧 key 稳定重放 202 与 Job 完成后新 key 返回 201/cookie。最后让诊断摘要与其他代表性响应通过 OpenAPI schema 校验。
- 通过标准：固定 JSON object 全部禁止未知 property，lexical guard 在生成 binder 前拒绝重复 key/无效 UTF-8/尾随值，query guard 拒绝未声明名和标量多值；OpenAPI 恰有 `putAdminUploadPart`/`postRuntimeSaveState`/`putRuntimePersistentSave` 三个 `x-retrom-streaming-body=true`，它们生成 reader 而非 `[]byte`/`ParseMultipartForm`；启动时构建普通/流式两条不可变 validator chain，前置 router 按 extension 分派，只有流式链设置 `Options.Options.ExcludeRequestBody=true`，且不跳过 path/query/header。普通 JSON 与流式请求并发时不能使对方误跳过或误读取 body，不得动态修改共享 options、维护 URL skip 清单或使用全局 `Skipper`。错误 envelope 固定为 `error.code/message/details/requestId`；状态码按契约覆盖 400/401/403/404/409/413/416/422/428/429/503；ID 是 UUIDv7 字符串或稳定 seed code、时刻是 int64。语义相同请求返回原 status/body/白名单 header，并发只产生一个领域结果；body/path/precondition/stream digest 任一语义变化均冲突，记录与事务同成败且无敏感 header。Launch validation pending 符合 202 schema、不设 capability cookie/不建 LaunchSession，并发请求复用 Job；完成后只有新 key 的新请求产生 201/cookie。cursor 严格按契约签名/限长/24 小时过期，分页无重复漏项且不能跨 route/filter 复用，payload 不含 secret/宿主路径；Import 与通用 Job SSE 的首帧 snapshot ID 都等于同一快照事务看到的全局最大 JobEvent ID，后续/重连只发送更大且属于目标 aggregate/job 的持久事件，不丢失、不混 scope、不因断开取消 Job；其他 scope/job 的合法 ID 可作为全局水位，非法/超前值返回 `400 INVALID_EVENT_CURSOR`，15 秒 heartbeat 是无 ID comment。`returnTo` 只接受精确白名单。诊断与抽样 response、两条 health response 符合相同 OpenAPI schema。
- 证据：生成/validator 版本、负向请求矩阵和请求/响应 contract snapshot；动态 request ID 需规范化后比较。

### ACC-OPS-001：健康检查、快速失败与诊断脱敏

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-OPS-001`。
- 流程：用明确临时数据根和完整依赖启动服务并检查 live/ready、进程数及前端写入；另在独立空库中用可控 DatParser gate 阻塞 bootstrap parse，检查 health 与业务路由，再释放 gate；在第三个空库注入确定性 parse failure。该 test double 只验证状态编排，真实 bytes/解析统计由 `ACC-DAT-001` 验证。随后分别使用不可写/越界数据目录、未来 schema、缺失 manifest 和 hash 不匹配 payload 启动；再分别注入拼错的 `RETROM_DATA_DI` 和具有固定假值的 `RETROM_ACCEPTANCE_BASE_URL/RETROM_FIXTURE_ROOT/RETROM_EJS_DEP_ZIP_V1`；以 fake reader/clock 触发同步 `RETROM_STARTUP_CHECK_TIMEOUT`；触发一次带 capability 的启动失败，再调用 `GET /api/v1/admin/diagnostics` 导出诊断摘要并按封闭 schema、header 和敏感模式扫描。
- 通过标准：后端只有一个 Go 进程并只写配置的数据根，Next.js 不保存业务状态。阻塞时 live=200、ready=`503 DEPENDENCY_INDEXING`，任一非 health 路由（包括 diagnostics）在读取 body/写状态前返回标准 envelope `503 SERVICE_NOT_READY`；释放后 DatVersion 先 READY/active 再 ready=200。确定性失败保持 live=200、ready=`503 DEPENDENCY_DAT_PARSE_FAILED`，重启不清空失败证据或误激活。多个动态故障按 `DATABASE_UNAVAILABLE→CAS_UNAVAILABLE→DEPENDENCY_INVALID→DEPENDENCY_DAT_PARSE_FAILED→DEPENDENCY_INDEXING` 选择首个 reason。可静态发现的坏配置在 10 秒内非零退出且从未开放 HTTP，并给出稳定可操作错误；未知非工具变量以 `CONFIG_UNKNOWN_VARIABLE` 失败；六类已声明工具前缀可继承但不改变服务配置且值不进日志。慢同步校验在配置的 60 秒 fake deadline 退出，后台 DAT_PARSE 不受这 60 秒误杀；启动不联网下载或 fallback。ready 后诊断响应为 schemaVersion 1 的严格 JSON、字段/计数/版本排序与数据库快照一致，带 `private, no-store`、固定 attachment filename 和 `nosniff`，且不创建 Blob/归档。结构化日志按契约关联 `requestId`、非秘密 `launchId` 和必要的类型化资源 ID，但没有内容 hash、capability/cookie/key、ROM/BIOS bytes、完整宿主路径、工具变量值或上游敏感响应；诊断摘要在此基础上还不得包含任何资源 ID。
- 证据：健康响应、退出码、耗时和脱敏扫描结果。

## 7. 账户认证、用户管理与私有数据隔离

### ACC-AUTH-001：release 空实例安全初始化

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-AUTH-001`。
- 流程：以 release 和全新数据根启动到 PENDING，读取匿名 auth context；运行主机只读 `retrom setup-code`，分别提交错误 code、两个并发正确 initialize 和初始化后的重复请求；扫描命令/API/日志与数据库。
- 通过标准：启动时为 `PENDING` 且无 User/Profile；错误 code 零写入；并发最多一个 `201`，同事务只创建一名 `ADMIN/ENABLED` User、Profile、Argon2id credential、AuthSession 与初始化审计，另一个和重复请求为稳定冲突。setup code 不进数据库/日志，初始化响应只在安全 cookie/封闭 DTO 中返回会话材料。
- 证据：context/HTTP 记录、User/Profile/credential 行数、审计摘要和敏感模式扫描。

### ACC-AUTH-002：release/test 模式隔离与旧库拒绝

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-AUTH-002`。
- 流程：用三个独立数据根分别默认 release、显式 `--mode=test` 和 release 含弱默认账号启动；并发启动两次 test 空库，再尝试 `test/test` 登录；另以 pre-account 数据库启动。
- 通过标准：默认 release 不创建或接受 `test/test`；test 空库无论并发只创建一个 `test` ADMIN/Profile，密码仅存 Argon2id hash且页面/context 标记 test；release 遇测试默认凭据 fail-fast。旧库以 `DATABASE_REBUILD_REQUIRED` 零写入拒绝。
- 证据：三个数据根的启动/登录结果、行数、文件 hash 和模式 UI 截图。

### ACC-AUTH-003：登录、会话、密码与请求防护

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-AUTH-003`。
- 流程：覆盖正确/错误/停用登录、logout、idle 8h、absolute 24h、并发改密、当前/其他 session rotation、Argon2 参数、10,000 项 blocklist、Origin/Fetch Metadata/CSRF 和 username+IP 双维限流；用 fake clock 和可信/不可信代理矩阵，不真实等待。
- 通过标准：登录错误通用且等时路径不泄露账号状态；session cookie/CSRF/缓存属性符合契约，过期和撤销立即生效；改密要求当前密码并只保留轮换后的当前会话。release 密码策略与物化 blocklist fail-closed，限流只信任 allowlist 代理并返回稳定 `429/Retry-After`。
- 证据：cookie 属性、受控时钟、密码校验与请求负向矩阵、blocklist hash。

### ACC-AUTH-004：邀请与密码重置 capability

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-AUTH-004`。
- 流程：管理员创建 USER/ADMIN invitation 和 password reset，分别在创建后 1 小时边界、撤销后、使用后和两个并发消费者下校验；完成邀请注册和停用用户重置；扫描 URL fragment 处理、数据库、日志和浏览器存储。
- 通过标准：只在创建或同幂等键重放响应显示 fragment URL；数据库只保存公开 link ID 与非秘密元数据，不保存 secret/hash，响应时由实例 key 重新派生。`now >= expiresAtMs`、撤销、已使用和错误 kind 均统一 unavailable，并发最多一次成功且事务不留半个账号。重置撤销全部旧认证会话但不启用停用账号；secret 被页面立即从地址清除且不进入 Referer/日志/DOM 终态或任何浏览器持久存储。
- 证据：link 状态、并发结果、浏览器 history/storage/network 和敏感数据扫描。

### ACC-AUTH-005：管理员用户生命周期与离线恢复

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-AUTH-005`。
- 流程：管理员以当前 ETag 修改角色/状态、停用/启用/软删除目标用户，提交陈旧 ETag，并证明 username/displayName/Profile/密码没有管理修改入口；再尝试停用/降级/删除自己及最后一名 enabled ADMIN，对另一副本运行离线 `retrom admin-reset`。
- 通过标准：合法变化原子更新 User、session version、AuthSession、未使用 AccountLink、必要的 Launch 与审计；陈旧 ETag 为 412 且不写入。self/last-admin 全部拒绝；DELETED 不恢复、username 不复用且私有 Profile/历史保留。`admin-reset` 只接受离线服务，从 `/dev/tty` 隐藏读取两次合规新密码，重新启用现有 ADMIN并撤销旧安全状态；密码不进入参数、环境、输出或日志，只留下无 secret 审计。
- 证据：API/UI 记录、会话/link/launch/User/Profile/审计前后摘要及 CLI 脱敏输出。

### ACC-AUTH-006：管理员授权与响应最小化

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-AUTH-006`。
- 流程：匿名、USER、ADMIN 遍历全部 `/api/v1/admin/**` 和 `/admin/*`；读取用户列表/详情、diagnostics 与影响摘要并按 allowlist 校验字段，同时从数据库抽样账户 AuditEvent 的 actor。
- 通过标准：匿名 401，USER 的每个 admin API 为 403且页面为应用级 403，只有 ADMIN 成功；新增路由若未显式分类必须 fail-closed。用户管理响应不含 Profile ID、游戏/存档/时长、密码参数、hash、session/link/capability、IP 或完整宿主路径；审计 actor 使用真实 User ID 或封闭 SYSTEM label，软删除不破坏 actor 外键。
- 证据：route 矩阵、页面截图、OpenAPI/response allowlist 与敏感字段扫描。

### ACC-ISO-001：两个账号共享目录但隔离游玩数据

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-ISO-001`。
- 流程：`test` 与 `alice` 对同一已发布游戏分别创建 PlaySession、SaveState 和 PersistentSave，再读取 home、library/detail、recent、saves、launch config；在同一 Chrome profile 依次登录两个账号。
- 通过标准：两人看到相同公共游戏目录/元信息，只看到各自 Profile 的首页聚合、最近游玩、时长、存档和 persistent current；账户切换清理前一用户的查询缓存、平台图钉、DOS 偏好和内存状态。
- 证据：双账号固定 fixture、API 响应、浏览器存储 namespace 与切换前后 DOM。

### ACC-ISO-002：跨账号 ID、cursor 与幂等探测

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-ISO-002`。
- 流程：由另一账号直接使用 SaveState、截图、私有 Asset、Launch、PersistentSave、cursor 和 Idempotency-Key；对相同 key/body 在两主体下并发提交，并尝试从别人的存档启动。
- 通过标准：跨账号资源统一按契约 404/401，不泄露存在、字段或 bytes；cursor 绑定 route/filter/principal，幂等记录按主体分区，同 key 不串响应；客户端提交 owner/Profile ID 无法扩大授权。
- 证据：每类交叉 ID 的状态/body/timing 摘要、数据库 owner 与幂等记录。

### ACC-ISO-003：停用、删除与 Player 残留隔离

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-ISO-003`。
- 流程：两个并发 Chrome context 让目标用户保持页面与 Player 活跃，管理员分别停用、重新启用和软删除；在每个边界继续请求 context、heartbeat、状态/持久保存。使用同一 Chrome profile 令 A 留下 EJS IDBFS bytes，再以无服务器保存的 B 启动同一游戏。
- 通过标准：停用/删除立即阻止新认证请求并撤销未结束 Launch，Player 写入不新增数据；重新启用仅恢复原 Profile 私有数据并需重新登录，删除不可恢复。B 启动前删除相同 EJS 路径残留并调用 `loadSaveFiles()`，失败则阻断，绝不运行 A 的 bytes。
- 证据：并发浏览器 trace、heartbeat/save 响应、IDBFS 操作序列和数据行前后摘要。

## 8. 游戏目录

### ACC-PLAT-001：创建游戏目录与默认核心约束

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-PLAT-001`。
- 流程：为 Arcade 创建名为 `acc-arcade-fbneo` 的目录并选择 `fbneo`；使用相同名称再次创建，随后软删除第一项并第三次使用相同名称；另尝试选择未关联/停用核心。
- 通过标准：创建请求不包含 slug，合法目录创建成功；服务端依次生成 `acc-arcade-fbneo`、`acc-arcade-fbneo-2`、`acc-arcade-fbneo-3`，不会复用软删除标识；非法默认核心被 422 与数据库约束拒绝；时刻字段为整数。
- 证据：API 响应和数据库行。

### ACC-PLAT-002：Game 唯一归属

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-PLAT-002`。
- 流程：创建导入时只选择 PlatformInstance；发布 Game；尝试空归属、直接写 `platform_id` 和同时归属两个目录。
- 通过标准：发布 Game 只有一个非空 `platform_instance_id`，基础平台间接推导；其余三种写入均失败，UI 不提供直接选择 Platform 的替代路径。
- 证据：创建 payload、schema/触发器结果和 UI selector 截图。

### ACC-PLAT-003：修改默认核心的影响与启动优先级

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-PLAT-003`。
- 流程：目录内放置 3 个已发布游戏和旧核心存档；以 `limit=1` 分页预览从 `fbneo` 改为 `mame2003` 的影响，检查全量 counts/相同 digest/无重复 items。在一次后续页前改变任一 current revision 验证旧 cursor 失效，恢复固定输入后重新完整预览并确认；分别普通启动和从旧存档启动。
- 通过标准：每页 `counts/impactDigest/platformInstanceVersion` 一致，3 项按 Game ID 稳定遍历，漂移后返回 `409 IMPACT_PREVIEW_STALE` 而不拼接快照。变更前列出受影响/不兼容游戏；普通启动改用新默认核心；旧存档仍锁定原 CoreArtifact/VariantRevision；不兼容时明确阻断而非静默回退。
- 证据：影响预览、两次 launch payload 和存档绑定。

### ACC-PLAT-004：游戏移动边界

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-PLAT-004`。
- 流程：准备一个只对当前目录默认 core 有 READY 结果的 Arcade 游戏；并发两次使用不同 key 预览移动到同平台、另一默认 core 的目录，等待返回的兼容任务完成；先用原 key 重放，再用新 key 重新预览并携带 digest 提交。另准备 blocker 结果分别以 `confirmBlocked=false/true` 提交，最后尝试直接移动到 NES 目录。
- 通过标准：首次并发 preview 均为 `202 VALIDATION_PENDING` 且复用一个不可取消 `VARIANT_REVALIDATE` Job，Game 归属未变化且无 digest；任务完成后旧 key 仍稳定重放原 202，新 key 返回当前 READY/blocker 影响和 digest。READY 提交只改变 Game 目录/version并保留 GameVariant/VariantRevision/ContentRevision/save；blocker 未确认时以 `MOVE_TARGET_CORE_BLOCKED` 拒绝，确认后允许移动但普通启动明确阻断且不回退；preview 后任一 Game/目录/依赖版本漂移使提交返回 `IMPACT_PREVIEW_STALE`。跨基础平台直接移动被拒绝并要求重新识别/审核。
- 证据：两次 preview 的幂等响应、唯一 Job ID、移动前后领域行/digest、审计事件及跨平台拒绝错误码。

### ACC-PLAT-005：停用、删除与审计

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-PLAT-005`。
- 流程：分别对非空和空游戏目录执行停用/重新启用/删除；停用期间查询用户首页、游戏库、详情、存档并尝试新启动，同时查询管理端游戏。
- 通过标准：非空目录可停用且 Game/存档/revision 不改写；停用后关联游戏从用户首页统计与最近记录、游戏库、详情和存档列表消失，新启动被拒绝，管理端仍可见，重新启用后原记录恢复展示。非空目录仍不可软删除；空目录可软删除，但没有硬删除 API 且旧 slug 不释放；操作写入不可变审计记录；作为默认核心的关联不可被直接禁用。
- 证据：启停/删除响应、用户与管理端查询、Launch 拒绝、目录状态和审计事件。

## 9. 游戏管理

### ACC-GAME-001：元信息、媒体与重新刮削

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-GAME-001`。
- 流程：在游戏管理按关键字、基础平台和游戏目录找到固定游戏；检查发布信息/媒体/内容与运行版本/管理操作四区；在未修改时检查保存按钮，记录当前 version，编辑标题、简介、年份与类型并保存，再次检查按钮和版本；替换固定 PNG；针对 current ContentRevision 触发 Hasheous stub 重新刮削，先不采用候选，再选择部分字段和 READY media 应用；构造一个旧 ContentRevision run 做负向 apply，最后用旧 ETag 提交一次并发编辑。
- 通过标准：搜索/筛选结果正确；没有字段变化和保存成功后“保存新版本”都禁用，不会创建空修订；每次确认修改创建可追溯 MetadataRevision/Asset，ADMIN_EDIT revision 的 source ref 为 NULL 且同事务 AuditEvent 指向新 revision，RESCRAPE_APPLY ref 精确指向被采用 Candidate；游戏库、详情和管理页读取同一当前值。运行区分开显示当前/历史 ContentRevision、各 Core VariantRevision、CoreArtifact/DAT，而不暴露宿主路径/Blob 编辑；显式重新刮削绕过旧 cache，创建独立 MetadataScrapeRun/QueryAttempt/Candidate/Asset 且不自动覆盖，旧 content run 不能 apply；采用范围与字段 diff 一致；旧 ETag 写入以 409 拒绝；Game ID、current content 和游戏目录不变。
- 证据：查询参数、修改前后 API、revision/diff、Blob SHA-256、冲突响应及三处当前 UI 截图。

### ACC-GAME-002：重新上传与不可变文件 revision

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-GAME-002`。
- 流程：为当前 `fceumm` revision 创建一份带截图存档；创建 COMPLETE UploadSession 后调用内容 revision endpoint。先用损坏输入等待 Job 失败，再用固定 seed 的确定性重打包文件创建 Job，并在一次运行中于验证与提交之间改变目录默认 core/version，确认 retryable conflict 后从同一 Job 显式重试；比较 Upload consumption、GameContentRevision/ContentFiles、各 Core VariantRevision、两个 current pointer 和旧存档。最后用包含相同 ROM entry 的 ZIP 再完成一次替换，验证审计修订与 CAS 去重。
- 通过标准：创建 Job 与 whole-session consumption 原子且同一 Upload 不能再被 Import/Asset 使用；损坏输入和快照竞态不改变任何 current、不创建 Content/Variant revision，失败 Job/Upload 有引用而非孤儿。只有 READY 且最新快照一致时才原子创建新的 GameContentRevision 和默认 core VariantRevision、切换 Game content 与该 Variant current；相同 ROM bytes 可 CAS 去重但再次接受仍有独立 ContentRevision；其他 core 对新内容显示 `NEEDS_VALIDATION`，新普通启动使用新 revision，旧存档仍锁定旧 revision。人工重试递增 Job version、追加事件且不重复 consumption。
- 证据：三次上传/Job 结果、原始/派生 hash、Content/Variant revision 链、Upload/Blob 引用、快照冲突与重试事件、兼容诊断和两次 launch payload。

### ACC-GAME-003：软删除、版本保护与引用保留

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-GAME-003`。
- 流程：对带 GameVariant/VariantRevision、媒体、存档、活动 Launch 和审核历史的固定游戏读取 `deleteImpact`，先使用旧 version 删除，再用当前 version 但错误标题删除，最后用当前 version、精确标题和新 Idempotency-Key 软删除；以同 key 重放一次，再用不同 key 再删。查询游戏库、详情、普通启动、存档和历史，并运行一轮有界 GC。
- 通过标准：影响摘要的存档/历史/活动 Launch 计数正确；旧 version 以 409 拒绝，错误标题以 `422 GAME_DELETE_CONFIRMATION_MISMATCH` 拒绝且不改变状态。成功删除原子递增 Game version、设置整数 `deletedAtMs`、撤销活动 Launch 并写 AuditEvent；同 key 稳定重放原 204，不同 key 再删以 `409 GAME_ALREADY_DELETED` 拒绝。游戏不再出现在已发布游戏库/搜索中，普通启动被拒绝；管理详情明确显示已删除状态；存档、审核历史、metadata/file revisions 和审计事件仍可查看，存档操作明确标为不可用；仍被引用的 Blob 不被 GC，且没有物理级联删除。
- 证据：影响摘要、四类 DELETE 响应/幂等记录、Launch 撤销、各入口查询、审计事件、GC 决策和当前 UI 截图。

## 10. 导入、刮削与审核

### ACC-IMP-001：单文件导入与发布前隔离

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-001`。
- 流程：通过 upload manifest 选择 `acc-nes-fceumm` 和 `fixtures.json` 指向的单文件夹具；按服务端 fileId 上传 8 MiB parts（该小文件为单尾块），带正确 Content-Range/Content-Digest，重放同 part，再使用当前 ETag/Idempotency-Key complete；断言 `202/FINALIZING` 后等待 UPLOAD_FINALIZE Job 到 SUCCEEDED/session COMPLETE，再创建 ImportJob 并观察至 `REVIEW_PENDING`。另以错误 digest、`../` relativePath 和缺失 part 做负向请求；对缺失 part 只重传服务端列出的 part，以新 key 再 complete。再创建两个小文件的 COMPLETE session，仅把一个文件消费为媒体，用 fake clock 推进 24 小时并运行一次 upload cleanup；另将一个无消费 COMPLETE session 推进 7 天。
- 通过标准：必须选择游戏目录且只接受浏览器相对路径；同 part/同 digest 幂等，异 digest/非法路径拒绝；每次接受 complete 都在短事务递增 `finalizationNo`、创建该编号唯一 Job 并转 FINALIZING，不在请求内组装大文件。Worker 从 bytes 重算 hash/CAS、按已完成文件可恢复并删除其临时 part，全部成功才 COMPLETE；同一轮 I/O retry 复用当前 Job，缺失/损坏 part 修复后的 complete 创建递增编号的新 Job，旧失败 Job/事件保持不变且已 COMPLETE 文件不重组装。只有 COMPLETE session 可创建 ImportJob。上传终结、HASHING/IDENTIFYING/SCRAPING 阶段可见；生成一个 ImportItem、规范 source manifest 和匹配目录默认核心的 READY ImportItemCoreValidation，但审核前不创建 Game/ContentRevision/VariantRevision，游戏库不可见。缺少 `Content-Length` 的合法流式 part 仍成功，越过声明 range/8 MiB 上限的 chunked body 在超限处拒绝且不留下 part/Blob 引用。file-level 消费只保留被消费文件，24 小时后同 session 未消费文件引用被裁剪；无消费 COMPLETE session 在 7 天后 EXPIRED；whole-session Import 证据不被裁剪。
- 证据：UploadSession/File/Part 状态、UPLOAD_FINALIZE/Import 任务事件、Blob hash、part/UploadFile 清理前后清单、fake clock、Item 和游戏库查询。

### ACC-IMP-002：目录分组与 GBA 确定性派生

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-002`。
- 流程：先将 `gba-smoke.zip` 上传到 Arcade 目录，令它完成安全扫描但以 `ARCADE_MACHINE_NOT_FOUND` 拒绝；再以新 UploadSession 把相同 bytes 导入 GBA 目录，验证复用同一 Archive Blob/Entry 并将 Unicode `.gba` member 一次性物化。另导入固定 DOS 目录到审核；检查两份 READY ImportItemCoreValidation，再分别 approve 并读取发布实体。
- 通过标准：第一批次不物化无需的 member；第二批次不重复 ArchiveEntry，`materialized_blob_id` 只从 NULL 提升一次，物化 Blob 的 size/四种 hash 等于 entry/fixtures manifest，尝试改回 NULL、替换 Blob 或修改 entry hash 均被数据库拒绝。审核前 DOS 目录形成可追溯 source manifest/程序候选和确定性 ValidationFile，GBA 原 ZIP Blob/ArchiveEntry 保留，且没有提前创建 GameContentRevision。Approve 后 ContentRevision 的 DOS_SOURCE/CONTENT 与来源 pair 正确，VariantRevision 直接引用 ContentRevision并复制已验证派生文件；浏览器启动不临时猜 ZIP 入口，审批事务不读 archive/重新打包。
- 证据：审核前 Item/Validation/ValidationFile，发布后的 GameContentRevision 文件表、来源 archive/entry 与物化 Blob hash、实际 VariantRevision 对 ContentRevision 的引用。

### ACC-IMP-003：三种 Hash profile 不混用

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-003`。
- 流程：导入一个 raw ROM、内容相同的唯一 ROM member ZIP/7z 与 `ldrun.zip`；读取 Blob/archive entry、`archiveFormat` 和 `content_hash_evidence`，执行 stub lookup，并构造有两个同优先级 ROM member 的 archive。另以 Arcade 目标导入 NORMAL/parent/BIOS 依赖批次。
- 通过标准：CAS 始终使用原始 Blob SHA-256；raw 使用 `RAW_FILE_V1`，ZIP/7z member 都使用 `SINGLE_ARCHIVE_MEMBER_V1` 且物化 bytes/hash 相同，但分别保留 `ZIP/SEVEN_Z` 来源；GameContentRevision 只指向物化 member，来源 archive 不作为运行 CONTENT。Arcade 仍使用 `ARCADE_DAT_ENTRIES_V1` 且不查询 ZIP 整体 hash。零/多 primary 不猜测；RAR/TAR、nested、加密/分卷/SFX 7z 以稳定 file-level reason 拒绝。每次上游 body 只含精确 `mD5/shA1/shA256/crc`。
- 证据：数据库字段和发往 stub 的脱敏请求。

### ACC-IMP-004：ImportJob 配置快照不漂移

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-004`。
- 流程：创建任务后暂停 worker；修改游戏目录默认核心和活动 DAT；恢复任务并进入审核；先用旧 selectedValidation 尝试 Approve，再等待新 validation 完成并批准。另把同一 ReviewDraft 的目标目录 PATCH 为不同基础游戏目录，并记录 Job/Validation 数量。
- 通过标准：处理继续使用创建时的 Platform/Core/artifact/DAT/provider 快照；审核明确提示当前配置已变化，旧 Validation 因目录 version/default artifact/DAT 不匹配而不能发布；新 validation 独立留痕且 READY 后才可 Approve，旧证据不被覆盖。ReviewDraft 只允许同基础平台内换目录；跨平台 PATCH 返回 `422 REIMPORT_REQUIRED_FOR_PLATFORM_CHANGE`，不改 draft、不创建跨平台 Validation，用户只能 Discard 后按目标平台重新导入识别。
- 证据：创建快照、两份 Validation/input digest、当前配置、旧审批冲突、处理日志与最终发布引用。

### ACC-IMP-005：Hasheous 无凭证命中与降级

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-005`。
- 流程：依次让本地 stub 返回贴合上游形状的单 object 命中、`404 text/plain` 未命中、429 + `Retry-After`、超时、`500`、`401` 和 `200` 畸形 JSON；命中 object 同时含 Logo、Screenshot1..4、AIDescription、AI Tags、未知字段及平台名不一致证据，另以乱序完成的两个 hash 命中同一 provider ID。不配置 API Key。重复一次自动 Import 验证有效 HIT/MISS cache，再从审核页显式选择 HASHEOUS 重刮削和 NONE。
- 通过标准：单 object 命中只产生一份按确定 primary response 归一化的候选；首个自动 Run 按固定排序选中首候选并初始化一次 ReviewDraft/metadata，未完成媒体不被自动选中，后续重刮削不覆盖草稿。Logo/四张 Screenshot 映射正确；标准 description 为空时只允许首个合法 AIDescription 作为显式 fallback，AI Tags/未知字段只留 raw，平台名只产生 warning，均不能改变目录/Core；title/publisher/description/year 和两个 provider score 严格符合 `HASHEOUS_BY_HASH_V1`，normalizationYear 来自 run 时刻而非常量。降级输入均进入可人工补全审核而不丢文件；404 是 MISS，429 是 RATE_LIMITED，超时是 TIMEOUT，500 是可重试 NETWORK_ERROR，401 与畸形 200 是非重试 INVALID_RESPONSE；429/超时/500 只做有界重试，各有持久终态后 Run/Job 仍 COMPLETED/SUCCEEDED 并显示 warning。每个 evidence 的 NETWORK/CACHE attempt 都关联 ProviderResponse，MISS/错误也可从 run 回放。请求 digest 对 JSON map 顺序稳定；自动 Import 只复用 7 天 HIT/24 小时 MISS，不缓存错误；审核显式重刮削绕过 cache，NONE 创建无 evidence/attempt/response/candidate 的 no-op SUCCEEDED Job/COMPLETED Run 且无网络。请求固定为 `POST /api/v1/Lookup/ByHash`，不带 API key，不含 ROM、路径、本地文件名/platform hint，也没有 ScreenScraper/MetadataProxy/Submission 调用。
- 证据：各 Run/Attempt/Response/Job、stub request log、cache 时刻和候选记录。

### ACC-IMP-006：DAT 与元信息证据隔离

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-006`。
- 流程：导入 Arcade fixture，让 DAT 命中且 Hasheous stub 返回不同标题/年份；在审核中选择元信息候选。
- 通过标准：DAT 只决定 machine/parent/BIOS/entry；展示字段只来自选中的 Hasheous 候选或人工编辑；UI、数据库和 ReviewEvent 分开显示两条来源，DAT description 不覆盖标题。
- 证据：审核截图、依赖快照、候选与发布字段。

### ACC-IMP-007：Approve、重复内容决策、Discard 与不可变历史

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-007`。
- 流程：先用同一 GBA bytes、不同文件名创建两个 ImportJob，使二者在任一发布前都进入审核；发布第一项，普通 approve 第二项，再用响应给出的当前已有 Game 集合二次确认并发布。随后第三次以另一文件名上传相同 bytes。另对两个 Item 分别编辑字段，选择 READY Validation、文本 Candidate 和来自同 Item 两个已完成 run 的 READY media 后 approve/discard；对 READY 与缺 Parent/BIOS 的 blocker 分别打开审核预览，在核心报告 start 前后检查 5 秒 timer、截图上传与草稿投影，并从 blocker 重新运行到 READY；故障注入证明审批事务没有 archive/ZIP/网络调用；之后尝试修改旧 ReviewEvent，并从历史页回放。再使用固定 `a.zip -> b.zip -> c.zip` Arcade 向量：child-only 进入审核，以不同本地名补 b、错误 c、正确 c，metadata PATCH 与 Attachment Job 并行；最后 approve，读取 Content/Variant 文件和 Parent ReviewEvent，并触发一次同 ContentRevision/DatVersion 的首次启动重校验后读取 Player config 与 Parent bundle。另制造补传后的 effective content identity 命中，执行一次拒绝确认和一次精确确认。
- 通过标准：只有匹配当前目录/config 和 effective source snapshot 的 READY Validation 可 Approve；READY 审核预览锁定 source/Validation/CoreArtifact，真实 `EJS_onGameStart` 前不计时，第 4,999ms 没有截图、第 5,000ms 才保存非空 PNG 并在 Review GET 投影；重新运行生成新 READY 后替换当前截图。Blocker 预览只交付主 ROM 与实际存在的依赖、不写截图、不创建 Game/LaunchSession/PlaySession/Save/PersistentSave，试玩成功也不能启用 Approve。第二项首次 approve 返回 `409 DUPLICATE_GAME_CONFIRMATION_REQUIRED` 且不产生 Game/ReviewEvent，错误/过期/重复 acknowledged ID 不能越过；精确 `ALLOW_NEW` 确认后才发布，并在最终事件保留当前有效内容摘要、policy 和已有 Game IDs。第三次识别直接将 Item 置 `DISCARDED`、任务 `COMPLETED`，计数/文件投影为已导入跳过，保留指向前两个 Game/current ContentRevision 的不可变匹配，不创建 ReviewDraft、Validation、刮削或第三个 Game；改文件名/UploadSession 不影响身份，不同基础平台和已软删除 Game 不误阻断。Arcade 接受 b 只追加 revision 2 并仍 BLOCKED，错误 c REJECTED 且快照/digest 不变，正确 c 追加 revision 3/READY；metadata PATCH 不被覆盖。Approve 的 GameContentFiles 从 revision 3 包含 CONTENT a 与 COMPANION b/c，VariantFiles 含 PARENT b/c，ReviewEvent 保存前后快照/Validation/hash 且无 ROM bytes/宿主路径；同内容、同 DAT 的后继启动重校验仍保留这两项 PARENT 与依赖索引，config `parentUrl` 非空且 bundle 根级包含 `b.zip/c.zip`。补传后的重复检查使用 revision 3 digest，不沿用 child-only digest。审批只复制 effective source/ValidationFile refs 并原子发布到唯一游戏目录，不做耗时计算。Discard 会取消 active Attachment 且不发布、不立即删受保护证据；历史可还原输入、validation、scrape run/candidate/media/Attachment 混合来源、字段 diff、目录/DAT 快照；旧事件不可更新。
- 证据：三次普通任务与 Arcade 分步任务的 Item/文件/快照/Validation/consumption 计数、两次发布响应、409 details、确认和 Parent ReviewEvent、Content/Variant 文件、游戏库结果、历史 API/页面截图和更新拒绝。

### ACC-IMP-008：有界失败、取消、重试和重启恢复

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-008`。
- 7z 子流程：覆盖 magic/SFX、加密、nested、路径与 casefold 冲突、CRC/size、entry/总量/压缩比、worker crash/signal/timeout 与 IPC 上限；只接受未加密单卷非 SFX 的唯一 ROM wrapper，子进程无可用资源隔离时 fail-closed，所有故障不留下半成品 Blob。
- 流程：创建含 3 个 Item 的任务，对其中一个故障注入；让一个 Item 已发布、另一个进入 RUNNING、第三个进入 REVIEW_PENDING 后取消整个 Import，观察 ImportJob/运行 Job 的 CANCEL_REQUESTED，再由注入式 reader 的下一个检查点确认 CANCELLED；另建独立失败任务，分别在 IDENTIFYING 和 SCRAPING 注入 retryable 本地故障后调用 Item retry，记录 Job/Run/配置快照；并在另一个阶段终止 worker，使用 fake clock 令 lease 到期后重启。再让 CANCEL_REQUESTED worker lease 过期，验证恢复器只清理/确认取消；对确定性坏输入验证直接进入 FAILED_FINAL 而非虚假的 FAILED_RETRYABLE。另创建“GBA 中一个合法 ZIP、一个误选平台的 raw PSP ISO、一个 sidecar”的任务，先发布合法 Item，再把尚未解决的 ISO 以 source 当前 ETag 重新配置到 PSP 目录。
- 通过标准：已发布 Item 不回滚，REVIEW_PENDING Item 在取消事务转 CANCELLED；RUNNING cancel 返回 202，ImportJob 在停止前保持 CANCEL_REQUESTED，最后一个 Worker 确认后才为 CANCELLED，且绝不因已有发布/取消混合计数聚合成 COMPLETED/PARTIAL_FAILURE。取消检查不超过规定 reader/token 边界并且不会发布；旧 worker 在取消/lease 转移后提交被 state+lease token 拒绝；取消中 lease 恢复不继续领域计算。IDENTIFYING retry 复用 pipeline Job并增加 execution，SCRAPING retry 新建 Run/Job且旧证据不变；两者都由 persisted failedStage 分派、保留原 Import 配置，不重复创建 Blob/候选/ReviewEvent。重新配置不上传或复制 bytes，新 UploadFile 与旧文件引用相同 SHA-256 Blob，replacement 生成 raw ISO Item并回指 source；source 原 REJECTED reason 保留、resolution 指向 replacement、未解决计数归零并收口，陈旧 ETag/重复接管整体拒绝。JobEvent 仍按每次真实转换追加；普通过期任务被重新领取并完成；确定性错误直接 FAILED_FINAL，attempt 用尽才从 FAILED_RETRYABLE 进入 FAILED_FINAL；没有长事务或真实等待，任务/审核时刻均为 INTEGER。
- 证据：完整状态转换、引用计数、lease/attempt 和事务时长摘要。

## 11. BIOS 与 Arcade DAT

### ACC-DAT-001：真实 DAT 基线完整性

- 上限：300 秒。
- 前置：计时前已执行一次 `make prepare-deps`，本 Case 期间断网。
- 执行：`make acceptance-case CASE=ACC-DAT-001`。
- 流程：runner 先执行 `make data-check` 与 `make deps-check`，验证运行时 manifest/adapter/allowlist、36 个跨版本 selected core/report 条目、PPSSPP assets、mame2003 override、38 个许可 component、五份 DAT，以及密码 blocklist manifest、10,000 行 payload 和 MIT 许可；离线重建 notice。再用全新临时 SQLite 和真实五份 DAT 断网启动服务，等待 ready并重启复用；最后运行 seed/约束负向与 Git payload 边界检查。
- 通过标准：离线命令成功，所有值与机器基线一致；两份 manifest 的 adapter 精确为 `ejs-4.2.3-v2 → 4.2.3`、`ejs-4.3.0-pre-v1 → 4.3.0-pre`，base/loader 命中 allowlist，缺失、未知、版本错配或无实现 adapter 均使 `data-check` 失败；35 个 enabled Core 各恰有一条 enabled CoreArtifact 且逐项等于合并后的 manifest，线程 basename 与实际 artifact 一致，未知版本不回退默认 adapter。冷库先 live/`DEPENDENCY_INDEXING`，五个不可取消 bootstrap Job 在事务外解析，最终五个 Arcade core 各有独立 READY active DAT；重启不重跑 parser。两个 FBA2012 DAT 必须从锁定源码分别完成双生成且 bytes 相同。许可输入逐项命中 size/hash，notice 可重复生成；DAT、EJS archive/runtime、license/notice payload 均未被 Git 跟踪。整个 Case 断网且启动/解析不尝试 CDN；部署前由 `ACC-PKG-001`–`003` 比较两镜像 release-input digest。
- 证据：逐文件校验/统计、DatVersion/Job 状态序列与 parser 调用计数、事务批次摘要、Git 跟踪边界和断网 network log。

### ACC-DAT-002：Core 隔离与依赖闭包

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-DAT-002`。
- 流程：用三个活动 DAT 分别解析 `ldrun` 及固定 parent/BIOS 关系样本；从真实 MAME 数据选一个含多个 biosset 的 machine，验证 default 与非 default ROM；用确定性 DAT 向量建立 `a -> b -> c`，并让 a/b/c 各自声明 romof，验证 V2 `requiredBy/depth`、canonical bytes 和 64 节点上限；分别构造 flat Full Non-Merged、Split child-only、补 b、错误 c、正确 c、根级 Parent 完整且另含安全 clone 子目录 extra 的 ZIP、必需 ROM 只在 parent 子目录的 Merged；重放幂等请求、并发两个 Attachment、stale config/source、Discard/cancel、retryable retry，以及 traversal/case collision/encrypted/bomb/真正嵌套 archive/corrupt ZIP。交换 core/DAT 组合，再提交一个 DAT 声明必需 disk/CHD 的 machine。最后用授权的 `fbneo/mineswpr4 -> mineswpr`、`mame2003/canyonp -> canyon`、`mame2003_plus/geebeeg -> geebee` 三组真实样本分别执行 child-only、补 Parent、发布、启动 smoke。
- 通过标准：`ldrun` 在各自 DAT 中为 20/20 必需 entry；machine、多级 clone/parent、逐级 BIOS/base archive 闭包来自当前 core/content companions，V2 顺序/digest 对遍历顺序稳定，cycle/自环/超限阻断；只要求唯一 default bios option，NODUMP 排除、BADDUMP Warning；Requirement 不跨 core 串用，也不扫描无归属全局 Blob。Full Non-Merged 由 CONTENT 满足闭包，Split child-only 为 b/c MISSING，正确 b 即使本地名不同也 ACCEPTED 但仍缺 c，错误 c REJECTED 不改 effective snapshot，正确 c 后 READY。Parent 根级必需 entry 完整时，安全 clone 子目录 extra 被保留为原始归档证据、计入 ignored diagnostics 但不参与 DAT 匹配；缺少任何根级必需 entry 时，子目录同名文件不能补足。相同幂等键返回同 Attachment，异 body 冲突；并发只有一个 active；stale/cancel/retry 各自收口且无半快照，恶意/不支持归档全部稳定拒绝。只有必需 ROM 无法由根级 entry 满足的真实 Merged 结构报 `UNSUPPORTED_MERGED_ROMSET`；错误 core/DAT 组合标记未知/不兼容。依赖外层 ZIP 两次生成 hash 相同、只含根级 Store entry，main/BIOS/parent 名称冲突在 Launch 前拒绝，并在三个 Arcade smoke 中证明 v4.2.3 解一层后 core 可见内层 archive且游戏帧推进。disk 元素可入诊断但必需 CHD 返回 `UNSUPPORTED_CHD`，负向输入都不创建 READY VariantRevision。
- 证据：三份解析摘要、V2 依赖图/快照 digest、Attachment/Job/SSE 状态和错误矩阵、三组真实样本的 source/Parent bundle hash、Player config `parentUrl`、iframe 文件清单、画面与帧推进证据。

### ACC-BIOS-001：正确与错误 Hash 上传

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-BIOS-001`。
- 流程：上传 fixtures 中正确 `disksys.rom`，再上传临时生成的错误内容 `gba_bios.bin`，最后用 fixtures 中正确 `gba_bios.bin` 替换当前安装。对一个固定 Arcade Requirement 再分别上传“必需 entry 名齐全但一项 bytes/hash 不同”和“完全缺少一个必需 entry”的两个小型 ZIP，再为 ZIP 添加一个 DAT 未要求的文件。每次安装后点击 Arcade BIOS 文件名打开条目对比。
- 通过标准：正确文件显示 installed/matched；错误 hash 文件允许保存并明确显示期望/实际 hash Warning，不伪装成 matched，也不因 hash 不同强制拒绝上传；正确替换后活动安装变为 matched，旧 Blob/安装按引用规则保留而非原地改写。Arcade entry 名齐全但 size/hash 不同的 installation 为 active/HASH_WARNING，可装入 Launch bundle且不阻断；完全缺必需 entry 的 installation 可保留为 active/MISSING_ENTRY 供修复但 Launch 阻断；损坏/不安全 ZIP 为 INVALID 且不能 active。弹窗仅使用左右两栏面板，两侧各自为文件列表；列表顶部横向表头精确为 `name`、`size`、`crc`，每个文件在下方占一个仅略高于字体行高的紧凑行并包含同序三个值，字段名不在文件行左侧重复。行内没有状态徽标或状态文案，内容别名、不匹配、缺失和额外文件由不同背景色表达，鼠标悬停 tooltip 和辅助技术提供完整状态说明。各值和安装时校验一致，不把非默认 BIOS set 误列为必需项。
- 证据：三次上传响应、实际/期望 hash、安装 revision、BIOS 状态与 UI 截图。

### ACC-BIOS-002：必需、可选与 Full Non-Merged

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-BIOS-002`。
- 流程：移除 FDS 必需 BIOS 做预检；分别以 `.gb/.gbc/.gba` 小型真实 fixture 检查 Gambatte/mGBA 可选 BIOS 不存在、仅安装另一内容类型 BIOS，以及安装匹配内容类型的正确/`HASH_WARNING` BIOS；读取 Launch config/bundle。为 MelonDS 安装 `bios7.bin/bios9.bin/firmware.bin` 后创建 Launch，切换其中一个 active installation，再创建第二个 Launch并读取两个会话的 external files。最后以 entry 名齐全但 hash 不同的 Arcade BIOS/base archive 启动，并检查包含自身依赖的 Full Non-Merged Arcade fixture。
- 通过标准：适用必需文件/entry 完全缺失阻断；不适用 requirement 不进入 digest/bundle，可选文件缺失只提示且不增加 activation option。匹配内容类型的 active `MATCHED/HASH_WARNING` BIOS 以 Requirement 逻辑名装入，Gambatte config 精确增加 `gambatte_gb_bootloader=enabled`、mGBA 增加 `mgba_use_bios=ON`；MelonDS 的三个 BIOS 不进入根 bundle，而是精确映射到三个固定虚拟路径，旧 Launch 锁定旧 Blob、新 Launch 使用新 Blob，跨 Launch capability 访问失败。Arcade entry 名齐全但 size/hash 不同也形成 `HASH_WARNING` 依赖、进入 bundle 并允许启动。另一内容类型 BIOS 不误启用，冲突 option seed 被校验拒绝，浏览器不按 core 名补写。Full Non-Merged 已内含依赖时不要求重复上传；页面按平台/core 聚合而不按游戏目录复制，`gamegenie.nes/sgb_bios.bin` 按一期条件明确标“未使用”而非缺失。
- 证据：预检/digest、两份 Launch config、BIOS bundle/external file 清单、跨 Launch 负向响应和 BIOS 页面截图。

### ACC-BIOS-003：服务器 root、目录浏览与授权边界

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-BIOS-003`。
- 流程：配置两个只读 root，覆盖封闭 JSON、上限、ID/label、受保护目录与 root 重叠负向；ADMIN 浏览根和多页直接子目录，并提交绝对、`..`、反斜杠、跨目录 cursor、途中/末端 symlink、special file 和暂时卸载 root。匿名与 USER 执行相同 route 矩阵；create 覆盖严格 body、幂等重放/异 body、同时活动冲突和 queued cancel。
- 通过标准：浏览器和 API 只见 root ID/label/status 与规范相对路径；未知/不可用/越界均不泄漏宿主存在性。非法配置启动失败且只记录变量名；匿名 401、USER 403、ADMIN 成功，目录 cursor 绑定 root/path，幂等与 ETag 语义稳定。
- 证据：配置矩阵、HTTP 响应、cursor 负向和 API/log 脱敏扫描。

### ACC-BIOS-004：STATIC、DAT 候选与隔离排序

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-BIOS-004`。
- 流程：固定 source 覆盖 STATIC exact、大小相同重命名、同名错误 hash、大文件 fallback，以及两个 Arcade Core 的同名 ZIP、完整/不一致/缺 entry；同一 bytes 同时关联两个 Requirement。执行完整发现、排序、最终复验和安装。
- 通过标准：完整 hash 永远优先于 size/name，fallback 固定为 warning；DAT 安全且 launchable、matched/aliased 更优者获胜，非逻辑 ZIP 不被展开。CAS 按 SHA-256 去重，但 Installation、Candidate、ArchiveEntry 与结果按 Requirement/CoreArtifact 隔离；选择和未选原因稳定。
- 证据：候选 rank、hash/DAT count、CAS/Installation 查询和结果投影。

### ACC-BIOS-005：覆盖防降级、漂移与原子审计

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-BIOS-005`。
- 流程：依次覆盖 overwrite 关闭/开启、已有 MATCHED、相同 bytes、同分、较差候选、Requirement 版本变化、catalog/source 漂移和崩溃点恢复；与单文件安装并发竞争同一 Requirement。
- 通过标准：关闭时不替换，开启也只接受严格更优；同 bytes/同版本不创建 revision，版本变化重新验证可创建 revision，任何降级都保留旧 active。最终 source 或 catalog 变化以条目错误收口；Installation、Item 终态、聚合计数和 PROGRESS 事件同事务，崩溃恢复不重复 revision。
- 证据：前后 active ID/status/version、revision 数、竞态结果、JobEvent 与恢复查询。

### ACC-BIOS-006：异步恢复、取消、详情与多尺寸访问

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-BIOS-006`。
- 流程：用确定性门禁 fixture 验证大目录发现、2 个 hash worker、全局 1 个 archive scanner、进度、lease/heartbeat/deadline、瞬时 root 退避、cancel、崩溃恢复和 restore fence；再由 Chrome 在 1280×800、2560×1440、3840×2160 创建空候选任务并查看终态详情、筛选和候选入口。
- 通过标准：完整发现前零安装，cancel 保留已完成 Item 并终止其余项；零终态 Item 的瞬时错误按固定有界退避，恢复不重复结果，restore 不自动继续外部 source。Drawer 的 radio/目录/checkbox、Escape/focus trap/焦点返回可用；详情无页面横向溢出、状态不只靠颜色，axe 无 serious/critical 结果。
- 证据：Worker 计数/事件/事务、恢复前后摘要、三尺寸截图、键盘与 axe 结果。

### ACC-BIOS-007：FULL_CATALOG 286 条 cursor 分页

- 上限：240 秒。
- 执行：`make acceptance-case CASE=ACC-BIOS-007`。
- 流程：以固定 286 条 catalog 依次请求首页与 cursor 后续页；Chrome 首屏只加载 100 条，故障注入第一次续页 500，保留旧行并以同 cursor 重试，再通过“加载更多”到终点。
- 通过标准：API 页长精确为 100/100/86，ID 无重复遗漏且末页 cursor 为 null；每页 `summary/filteredCount` 恒为 286。UI 依次显示 100/286、200/286、全部 286；失败不清空旧页、重试 cursor 不变、终点不再请求，纯键盘可完成且页面无横向溢出。
- 证据：三页 request/response 摘要、唯一 ID 数、失败/重试 URL、最终 DOM 行数和截图。

### ACC-DAT-003：用户 DAT 候选不自动生效

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-DAT-003`。
- 流程：把固定 seed 中真实的 `fbneo-arcade.dat` 作为用户文件上传给 `fbneo`；观察解析完成后自动进入异步差异队列，在任务完成前尝试 GET/启用，完成后查看 diff 但不点击启用；随后删除这个未活动、无业务引用的用户候选。
- 通过标准：即使底层 Blob 因相同 SHA-256 去重，也创建来源、上传时刻和状态独立的 DatVersion；解析完成自动显示排队/比对状态，按钮禁用，GET 返回 `DAT_DIFF_NOT_READY` 且请求不执行 DAT 全量扫描；READY 后兼容状态和空 diff 可见。当前活动 DAT、已有 VariantRevision 诊断与启动结果完全不变；候选可删除但共享 Blob 和预置 DatVersion 不受影响。
- 证据：上传前后 active ID、diff、删除响应、Blob 引用和旧快照 hash。

### ACC-DAT-004：启用、重校验与回滚

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-DAT-004`。
- 流程：在本 Case 内按 `ACC-DAT-003` 的固定输入重新创建独立候选，等待差异 READY 后显式启用并等待有界重校验任务完成；查看同一 CoreArtifact 的其他历史/候选差异状态；为目标重新生成差异后回滚到预置 installation。
- 通过标准：不依赖 `ACC-DAT-003` 遗留状态；启用有影响预览和审计；同 artifact 的其他 materialized diff 原子转为 STALE 且明细删除，页面只提供异步“重新生成差异”，旧 impact digest 不能提交；相同内容允许生成 no-op 重校验，但不得静默改写历史快照；回滚恢复活动 DatVersion，新旧版本均可追溯且被引用版本不可删除。
- 证据：两个 active version 事件、重校验结果和快照引用。

### ACC-DAT-005：恶意/错误 DAT 拒绝

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-DAT-005`。
- 流程：提交 XXE/参数实体、畸形或超限 DOCTYPE/XML、声明 core 不匹配和关系循环夹具；夹具全部小于 1 MiB。
- 通过标准：XXE/超限/畸形输入在安全解析阶段拒绝且无外连；结构/core family 明确不匹配为 `INCOMPATIBLE` 且不可通过确认启用；结构可解析但无法证明 artifact 兼容的版本为 `UNKNOWN`，只有带影响 digest 和 `confirmUnknownCompatibility=true` 的显式激活才能转为 `USER_CONFIRMED`，否则拒绝；循环输出稳定诊断，不造成递归崩溃或污染活动版本。
- 证据：错误码、无外连日志和 active version 不变证明。

### ACC-DAT-006：版本升级证据审计（条件 Case）

- 上限：180 秒，不在本 Case 内重跑全部核心。
- 条件：EmulatorJS、任一 core artifact 或预置 DAT 相比上一已接受版本发生变化；否则为 `NOT_APPLICABLE`。
- 执行：`make acceptance-case CASE=ACC-DAT-006`。
- 流程：检查新版本目录、发布物 digest、core source 证据、DAT 同提交/生成证据、parser stats、关系完整性、manifest Player adapter 描述/前端 registry/实现一一对应，以及本次 `ACC-CORE-*` 和存档兼容 Case 的独立结果；先放入未登记 adapter 的小型 manifest 夹具验证 `data-check` 和 Player config guard 失败，再登记并把新版本追加到配置列表但不切 active，创建一份锁定旧 artifact 的存档，再切 active 并分别普通启动、从旧存档启动，最后切回旧 active。
- 通过标准：不覆盖旧版本；`UNIQUE(emulatorjs_version, relative_path)` 允许版本间同路径而不碰撞，静态路由只暴露每份 manifest allowlist。未登记/版本不符 adapter 使 `data-check` 失败，浏览器 guard 以 `PLAYER_ADAPTER_UNSUPPORTED` 在 loader 前拒绝且不套用 v4.2.3 默认；全部证据和适用 Case 已通过后才能启用。config 中 `emulatorjsVersion/playerAdapterId/runtimeBaseUrl/loaderUrl/path override` 始终来自锁定 artifact 的精确 manifest；切换后普通启动使用新 enabled artifact，旧存档仍从旧版本 URL 和对应 adapter 加载锁定 artifact，回滚恢复旧 enabled artifact/DAT 且不改历史 revision。仍有保护引用的旧版本不可从配置列表、adapter registry 或镜像移除；缺任一证据即失败。
- 证据：升级 manifest、Case 引用和切换/回滚记录。

## 12. Pegasus 服务器目录导入与视频媒体

### ACC-PEG-001：Pegasus parser、扫描投影与确定性边界

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-PEG-001`。
- 流程：对 BOM、LF/CRLF、续行、flowing text、alias、多 metadata/multi-game、缺失/非法字段、大小/条目/深度门禁和大于 64 个声明文件的固定夹具解析两次；扫描目录后改变无关 UUID 再扫描。
- 通过标准：两次 Collection/game/media 投影与 `sourceKey` 完全相同；全部声明文件参与 key，最多 64 条文件行进入 UI 投影且给出稳定 blocker；命令型值、非法 UTF-8、越界输入和 ASCII casefold 冲突被拒绝，不把 partial metadata 当成功。
- 证据：聚焦单元测试输出和确定性摘要。

### ACC-PEG-002：外部 root、HTTP 授权与来源漂移

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-PEG-002`。
- 流程：从配置 root 浏览直接子目录并创建扫描；尝试绝对路径、`..`、symlink、special file、未知 root、USER/匿名、无 CSRF、未知 JSON 字段和错误 ETag；在映射前替换已扫描来源。
- 通过标准：客户端只看到 root label/相对路径；逐段 no-follow 阻止越界且响应/日志不泄露完整宿主路径；权限和严格协议按 OpenAPI 拒绝；来源漂移终止计划且不会创建 Game、Revision 或 Blob 引用。
- 证据：路径和 HTTP 聚焦测试输出、稳定错误码摘要。

### ACC-PEG-003：映射、审核交接、重复、多盘与 Arcade

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-PEG-003`。
- 流程：扫描含两个 Collection 的固定目录，不设置默认映射并按 ETag 提交显式映射；准备普通单文件、M3U+CHD 和 Arcade ZIP+同目标 companion，并在 Arcade Collection 中放入超过 64 个无关 ZIP；在审核前查询 Game，再对 READY 条目逐项 Approve、对另一条目 Discard、对 blocker 条目修复依赖后 Approve；再次导入相同来源与相同内容的另一来源；在审核交接中点模拟进程退出并恢复。
- 通过标准：未映射时不能开始；计划冻结游戏平台目录/核心版本；三种内容均复用既有验证与普通审核管线，M3U 顺序与 Arcade primary source 正确；Arcade 只装配冻结 DAT parent/romof 闭包中的显式 ZIP，无关 ZIP 不进入单 Item 来源且不会触发 64 文件上限。导入前已安装且匹配冻结 CoreArtifact 的 DAT BIOS 在初始 Validation 中即为 `SATISFIED_EXTERNAL` 并进入 `BIOS_BUNDLE`，不得先误报缺失；同时仍缺 Parent 或主内容不匹配的条目继续按真实原因阻断。Worker 完成后 READY 与 blocker 都为普通 `REVIEW_PENDING`，Game 数仍为零且没有批量通过入口；Approve 才原子创建带 `SERVER_PEGASUS_IMPORT` 来源的 Game/Revision/媒体并同步两组计数，Discard 保留 ReviewEvent 并同步为 `REVIEW_DISCARDED`。交接中点恢复复用同一个内部 ImportItem，不重复草稿事件，未交接条目不可见且不可发布。library validation 未通过时原样保留 status、compatibility code、Core 与封闭依赖证据，不得统一覆盖为 `PEGASUS_RUNTIME_BLOCKED`；library import 内部错误收口为可重试失败，并持久化 stage/operation/cause/受限技术详情/数量上限和可用关联 ID；重复结果列出全部既有匹配，不生成审核事项或重复创建 Game/Revision/Blob，条目仍有稳定结果和链接。
- 证据：Migration/服务集成测试、发布与重复摘要。

### ACC-PEG-004：取消、重试、恢复、GC 与 restore fence

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-PEG-004`。
- 流程：在扫描和导入阶段分别取消；注入 retryable 失败、过期 lease、deadline/attempt 耗尽和进程重启；备份恢复含 Pegasus 历史的数据库，并在恢复后运行单轮 Blob GC。
- 通过标准：取消/失败不删除已生成审核事项或回滚已提交游戏；retry/recovery 不重复内部 ImportItem 或 revision；耗尽任务收敛到稳定 FAILED；BIOS/Pegasus 总内容读取并发不超过 2；restore 终止外部 source 工作且历史、已交接审核事项可读，不可恢复执行；受保护 Blob 不被 GC，终态可删除计划不会保留悬空保护边。
- 证据：worker、maintenance、blob registry/GC 聚焦测试输出。

### ACC-PEG-005：三步 UI、详情恢复与桌面布局

- 上限：240 秒。
- 执行：`make acceptance-case CASE=ACC-PEG-005`。
- 流程：在 1280×800、2560×1440、3840×2160 打开服务器导入页，只用键盘完成 root/目录选择与扫描；扫描后关闭 Drawer，直接进入该计划详情并从“继续映射”恢复全部 Collection 显式映射、确认审核计划和启动；另覆盖已完整保存映射后关闭并恢复第三步。任务准备完成后从批次行动区进入限定审核队列，打开 READY 与 blocker 各一项并返回；检查来源 COVER/VIDEO 和无批量入口。在详情注入 BIOS 缺失、parent 缺失、内容 entry 缺失、merged set 不支持、结构化 library import 内部失败和历史通用 runtime blocker，展开诊断、触发原计划重检，再使用 URL 筛选、分页、取消/retry 并模拟 SSE 断线。
- 通过标准：两张能力卡等权且共用 root 说明，Pegasus 卡明确不会自动发布并显示待审核总数；760px Drawer 三步可达、无默认映射，第三步明确“全部进入待审核”；`AWAITING_MAPPING` 详情能恢复指定计划且不重新选目录/扫描，未保存映射重新选择、已完整保存映射直接进入第三步。Drawer 打开时背景不可滚动，扫描转换与同计划摘要轮询不得造成布局跳动、焦点转移或本地映射丢失。详情以扫描范围/待审核/已发布·丢弃·已有/阻断·失败分组，显示 media READY/MISSING/WARNING、逐项审核入口与已有/新游戏链接；批次入口保留 `pegasusImportId`，清除其他筛选不丢批次，Pegasus metadata 不计作“未找到信息”；审核媒体中 VIDEO 等比居中且不自动播放，页面无任何批量通过按钮。阻断行展示具体原因，展开后可见稳定 code、Core/machine、缺失条目、依赖和处理建议；内部失败展开后可见 stage、operation、cause code、Pegasus Item ID、相对路径、观察数量/上限、可用内部关联 ID 与受限技术详情，不得只显示 `PEGASUS_LIBRARY_IMPORT_FAILED`；历史 `PEGASUS_RUNTIME_BLOCKED` 可在原任务重检且重检后不再保留通用原因；断线不清空内容；三个 viewport 无页面级横向溢出，焦点、Escape、reduced-motion 和状态文本符合 UI 契约。
- 证据：Playwright DOM/网络/布局断言和三尺寸当前截图。

### ACC-MEDIA-001：VIDEO 上传、服务与详情播放策略

- 上限：240 秒。
- 执行：`make acceptance-case CASE=ACC-MEDIA-001`。
- 流程：上传合法 MP4/WebM 与伪装/超限媒体；读取 GET/HEAD/single Range；检查管理员媒体区只显示封面和视频、视频槽布局与等比适配；编辑元信息、替换并删除 VIDEO；在详情模拟累计可见、切换后台、播放拒绝、5 秒无 `playing`、用户暂停和 reduced-motion，并监控游戏列表请求。
- 通过标准：VIDEO 尺寸允许 NULL，magic/MIME/大小严格校验；Range/HEAD 和私有缓存契约正确；管理员媒体区不展示背景图/游戏截图，VIDEO 槽占满封面外的剩余宽高并以 `contain` 等比完整适配、以 `50% 50%` 在槽内水平和垂直居中，1920px 及以上双栏高度由左侧发布信息决定且媒体面板不得反向撑高；元信息编辑保留视频，替换/删除产生不可变 revision 且历史引用保留。详情只有前台可见累计 2 秒才 muted/inline/loop 自动播放，`playing` 后 200ms 淡入；失败保持封面和手动播放，用户暂停不再自动恢复，reduced-motion 完全手动；列表无 VIDEO 请求或 autoplay。
- 证据：媒体/HTTP/React/Playwright 聚焦结果和请求断言。

## 13. 启动、存档与游玩数据

### ACC-RUN-001：默认核心与单次核心切换

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-RUN-001`。
- 流程：准备一个只有默认 core READY、另一个平台 core 为 `NEEDS_VALIDATION` 的固定游戏；从详情先用默认核心启动，再选择另一核心并只点一次开始。记录首个 launch 请求的 202、并发相同请求、Variant Job/SSE、成功后前端以新 key 自动发出的第二个 launch 请求和最终 201，然后重新打开详情。记录 Worker 的 DB/文件读取和网络；另用 fake clock 推进一条注入阻塞 reader 的 Variant execution 到 30 分钟 deadline。
- 通过标准：下拉默认标记目录核心且覆盖平台全部 enabled core；第二种 core 的首请求在短事务内返回 `202 VALIDATION_PENDING`、没有 LaunchSession/cookie，并发相同 input digest 只复用一个不可由单个 Player 取消的 Job；一个 browser 退出等待只断开自身 overlay/subscription，另一个仍能等到结果。Worker 只使用 `Game.current_content_revision_id` 已入库的 source/hash/ArchiveEntry/DAT 证据，无 Hasheous/外部网络或全局 CAS 猜测，事务外流式物化必要 bundle；最终只产出一个直接引用 ContentRevision/input digest 的 READY revision/current。同一加载壳自动以新 key 取得 201 并开始，没有人工第二次 Start、确认页或静默回退；旧 key 仍重放原 202。注入超时以 `LAUNCH_CORE_VALIDATION_TIMEOUT` FAILED 且不留 current/半成品，未真实等待。另用缺 BIOS 输入证明 BLOCKED revision 被去重且不成为 current，安装 BIOS 后 input digest 改变并可创建新 Job 验证。切换只覆盖最终 LaunchSession，不修改 Game current content、游戏目录或形成“最近核心”隐式默认；重新打开后该 core 显示 READY。
- 证据：三个 launch HTTP 结果/Set-Cookie 有无、Job/InputSnapshot/SSE、coreOptions、ContentRevision/source manifest digest、SQL/网络摘要、并发结果、目录记录和重新打开后的选择值。

### ACC-RUN-002：一次点击、自动开始与默认全屏

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-RUN-002`。
- 流程：在详情点击一次“开始游戏”，记录原始点击、Fullscreen 调用、launch/config 请求、iframe 配置、EmulatorJS network 和 start 事件；用普通 core 与 `mame2003` override 各执行一次短流程。
- 通过标准：对始终存在的 `document.documentElement` 的 Fullscreen 请求仍在用户激活链且发生于第一个 await 前；同一 Player Shell 显示加载并自动开始；没有 Retrom 第二个 Start 或 EmulatorJS `Play Now`；进入有效帧画面。进入游玩页与退出返回均替换当前浏览器历史项，退出后浏览器后退不得重新进入 Player Shell。config 严格符合 HTTP 契约且不含 secret/Blob/宿主路径；`emulatorGameId` 为 `1..9007199254740991` 的 JSON number、`gameName` 为其稳定十进制派生，Arcade `gameUrl` basename 精确为 DAT machine 的 `<machine>.zip`。iframe 先设置 `player/pathtodata/gameName/gameID/paths` 再加载固定 loader，`typeof EJS_gameID === "number"`。EJS 配置固定 `language=zh-CN`、`disableAutoLang=false`（按 v4.2.3 的反向 sentinel 语义），网络只请求 manifest 中的 `zh-CN.json`，不得按系统 locale 或 CDN fallback；普通 core artifact 来自 config 的 basename 映射，`mame2003-wasm.data` 精确请求固定 4.2.1 override，未请求 4.2.3 同名 artifact 或外部 CDN。
- 证据：Playwright trace、两份 config/network 摘要、事件顺序、截图和按钮断言。

### ACC-RUN-003：全屏拒绝与深链接恢复

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-RUN-003`。
- 流程：模拟 Fullscreen 拒绝后从正常入口启动；保留 cookie 刷新 `/play/{launchId}`；再把同一 URL 复制到没有 launch cookie 的新 browser context。
- 通过标准：正常入口拒绝全屏时游戏仍自动运行并显示可恢复“进入全屏”控件；同 context 刷新因无用户激活不伪造全屏，但仍自动加载且只有全屏恢复控件、没有第二个游戏 Start；新 context 显示“启动会话不可用”且不能取得 config/content。
- 证据：两条 trace、错误/恢复 UI 和运行帧状态。

### ACC-RUN-004：Blocker 与 Warning 分流

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-RUN-004`。
- 流程：分别以缺少必需 BIOS、静态 BIOS hash mismatch、Arcade BIOS/base entry 名齐全但 hash mismatch，以及可选 BIOS 缺失启动。
- 通过标准：Blocker 不创建可用 launch、退出全屏并回来源上下文显示修复入口；Warning 不增加确认步骤且继续自动启动；状态文案不只靠颜色。
- 证据：两次启动状态、UI 截图和 launch 记录。

### ACC-RUN-005：DOS 启动程序

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-RUN-005`。
- 流程：先用旧 DOS artifact 发布一个只有 `DOS_SOURCE` 的游戏，再启用 4.3 artifact 触发首次启动重校验；导入一个数据/图片排在程序之前且同时含安装器、实际入口、需缩短的长路径和两个会产生同一初始 8.3 名称的程序的 DOS ZIP；确认完整候选排序，选择非默认程序并启动；检查锁定内容与 game.zip 的 HEAD/Range/完整 GET；再选择“显示 DOSBox Pure 程序菜单”，重新进入详情并验证记忆选择，最后构造选中程序已不存在的 revision。
- 通过标准：重校验不要求不存在的 CONTENT 行，保留相同 ContentRevision 的 bundle/default entry 并在有界时间进入终态；安装/配置工具只降权不消失；直接启动锁定原 bundle Blob、Blob 数不增加，响应 ZIP 的首项是受控 `AUTOBOOT.DBP`，程序菜单首项是受控 `DOSBOX.BAT`，其余成员 bytes/顺序不变，源包的两个同名保留文件都无法覆盖或劫持选择；config 的 `externalFiles/defaultCoreOptions` 不含 DOS 启动补丁，4.3 adapter 在 start 前把完整 ZIP 交给 core，安全路径进入所选程序画面。程序菜单通过 `Z:\PUREMENU` 进入 core 菜单。只有成功创建 Launch 后才按游戏记住入口或菜单，失败不改偏好，存档恢复不改偏好；缺失/不安全 entry 仍分别稳定阻断且不猜替代程序。
- 证据：完整程序列表与排序、launch/config payload、原 Blob/引用计数、三种 game 响应、虚拟 ZIP central directory/引导 bytes、运行画面、浏览器偏好和错误响应；另以 `RETROM_DOS_CORPUS=<合法本地目录> go test ./internal/libraryimport -run TestLocalDOSCorpusCompatibility -count=1 -v` 验证多游戏结构矩阵。

### ACC-SAVE-001：手动状态存档与截图

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-SAVE-001`。
- 流程：启动游戏后打开退出确认，从操作区最左侧点击“创建存档”；等待成功提示，确认弹窗仍保持打开后取消退出；读取存档记录与“我的存档”卡片。另注入一次创建失败并从同一弹窗重试。
- 通过标准：退出确认内“创建存档、取消、退出游戏”的视觉与 DOM 顺序一致；创建时暂停并锁定弹窗内的离开动作，成功后弹窗不自动退出、显示不可重复点击的“已创建存档”，失败明确说明未创建不完整记录并显示“重试创建存档”。状态 Blob 与非空截图 Blob 同时存在且在同一事务引用；截图在暂停前从仍运行的帧取得、可解码且具有非零亮度分布，已暂停时复用进入暂停瞬间缓存的最后一帧，不能生成全黑 canvas 截图；记录 Profile、Game、CoreArtifact、GameVariantRevision、名称、整数时间和累计时长；缺截图或空 state 的创建请求被拒绝。
- 证据：退出确认三个状态、存档 API/数据库、CAS hash 和当前截图。

### ACC-SAVE-002：三个入口快速恢复与不兼容拒绝

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-SAVE-002`。
- 流程：先在本 Case 的 seed 中按 `ACC-SAVE-001` 规则创建一份带截图的有效存档；分别从详情存档、我的存档和首页继续入口恢复；再用不同 Core/revision 尝试加载。
- 通过标准：三个入口均一次点击直达 Player Shell，不经过详情或二次 Start，且使用存档锁定环境；不匹配时明确拒绝，不静默迁移或改用目录默认核心。
- 证据：三条 route/launch trace 和负向错误。

### ACC-SAVE-003：PersistentSave 更新

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-SAVE-003`。
- 流程：seed 一份服务端 PersistentSave；创建两个锁定相同 base 的 Launch，在第一条加载 loader 前预取，记录 `EJS_ready → saveDatabaseLoaded → start` 顺序并验证恢复；让第一条触发两次并发 `saveSaveFiles`、一次手动 export 和 `exit`，记录每个 request 的 sequence/event/hash，再让第二条用旧 base 保存。重放相同 sequence/body，并分别复用 sequence 改 event/bytes、制造跳号。另以无服务端保存但浏览器存在旧 IDBFS 文件、上传失败，以及 fake reader 报告超过 64 MiB 做负向测试；最后对 `persistentSaveMode=NONE` 的核心调用 PersistentSave GET/PUT。
- 通过标准：保存按 `Profile + GameVariantRevision + kind` 隔离；Launch GET 始终返回其创建时锁定的 revision。最多 64 MiB 的服务端 bytes 在 loader 前形成一个有界 `Uint8Array`，超限不分配完整 body 并以 `LAUNCH_PERSISTENT_SAVE_TOO_LARGE` 阻断；在真实 v4.2.3 且 `EJS_disableDatabases=true` 时 `saveDatabaseLoaded` 仍于 start 前触发，同步覆盖/清除 IDBFS 目标并调用 `loadSaveFiles()`，不会复活旧浏览器数据；在 `saveState()` 前已注册至少一个 saveState listener，v4.2.3 以 listener 数而非 callback 返回值阻止 fallback 写独立 state DB，自动更新来自真实 `saveSaveFiles` event。revision 保存正确 LaunchSession、从 1 连续的 sequence 和 `AUTO_INTERVAL/MANUAL_EXPORT/EXIT`，并发 callback 被串行/合并到后续 sequence；相同重放返回原结果，改 event/bytes 与跳号返回稳定冲突。第一条按 base/上一项 CAS 连续提升；第二条以 `PERSISTENT_SAVE_CONFLICT` 拒绝且不创建 revision/不覆盖 current，页面保留 bytes、提供本地下载并不自动死循环。不同 CoreArtifact/VariantRevision 不串用；其他失败明确报告且不覆盖最后有效 Blob；NONE 模式的 GET/PUT 均返回 `PERSISTENT_SAVE_UNSUPPORTED` 且不创建 revision/current 行，Player 不请求该端点；实现没有不存在的 `EJS_onExit/EJS_onSaveUpdate`。
- 证据：事件顺序、前后持久 Blob hash/revision、IDBFS 负向结果、网络请求和故障注入结果。

### ACC-PLAY-001：有效游玩时长

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-PLAY-001`。
- 流程：使用 fake clock 先驱动一次 config 后/start 前超过 2 分钟的加载和 pre-start finish，再用新 Launch 驱动 start、两次 heartbeat、页面隐藏、暂停、失联和重复 finish；另提交越界 `clientObservedAtMs`。
- 通过标准：加载阶段没有 PlaySession/idle 误过期，pre-start finish 撤销且不创建游玩记录；真实 start 后才启用 2 分钟 idle。三个事件端点都位于 `/runtime/launches/{launchId}/` 且校验 launch cookie，只有公开 launchId 没有 cookie 时为 401。只累计实际运行区间；隐藏/暂停/超出失联上限不累计；heartbeat/finish 幂等、跳号冲突，client time 只审计且越界拒绝；数据库全为整数毫秒，首页/详情汇总一致。
- 证据：事件时间线、期望/实际 duration 和 API 汇总。

## 14. 三十五个核心的真实运行画面

### 13.1 每个核心的统一执行流程

每个 `ACC-CORE-*` 都是独立 Case，不允许把三十五核合成一个可能超时的长 Case。执行前先运行：

```bash
python3 data/example/verify-fixtures.py
```

随后执行表中的单核心命令，并只读取本次生成结果：

1. `data/example/results/latest.json` 中目标 core 的 `status` 为 `passed`、`failure` 为 `null`；
2. `smoke.phase` 为 `frames-advancing`，`frameDelta >= 120`；
3. canvas backing/CSS 尺寸均大于 0；
4. `colorBuckets >= 3`、`luminanceStdDev >= 5`、`nonBlackRatio >= 0.01`；
5. `crossOriginIsolated === true`，实际请求 core artifact 的路径/hash 与 fixtures/manifest 一致；
6. AI Agent 使用图像查看能力检查本次 `<core>.png`，必须是表中画面，而不是 EmulatorJS 等待页、`Failed to start game`、RetroArch `Load Content`、纯黑或静止第一帧；
7. 人工视觉判断写入本 Case 的 `result.json`，不能复用旧 `manual-review.json` 的时间戳作为本次证据。
8. 在运行下一个核心前，把本次 `latest.json` 的目标记录和 `<core>.png` 复制到当前 Case 证据目录，避免被后续单核心命令覆盖。

| Case | 上限 | 命令 | 必需依赖 | 当前画面标准 |
| --- | --- | --- | --- | --- |
| `ACC-CORE-001` | 180 秒 | `node data/example/smoke-test.mjs fceumm` | `disksys.rom` hash 命中 | Family Computer/FDS 启动画面 |
| `ACC-CORE-002` | 180 秒 | `node data/example/smoke-test.mjs snes9x` | 无 | Dr. Mario 标题/玩家选择菜单 |
| `ACC-CORE-003` | 180 秒 | `node data/example/smoke-test.mjs gambatte` | 无 | Tetris 版权启动画面 |
| `ACC-CORE-004` | 180 秒 | `node data/example/smoke-test.mjs mgba` | `gba_bios.bin` 可用 | 数独 Advance 标题/菜单；必须请求提取后的 `.gba` |
| `ACC-CORE-005` | 180 秒 | `node data/example/smoke-test.mjs fbneo` | 本 core DAT 对 `ldrun` 为 20/20 | Lode Runner 标题/投币画面 |
| `ACC-CORE-006` | 180 秒 | `node data/example/smoke-test.mjs mame2003` | MAME 0.78 DAT；固定 4.2.1 bundle override hash | Lode Runner 标题/attract；不得请求 4.2.3 坏 bundle 或 mame2003_plus |
| `ACC-CORE-007` | 180 秒 | `node data/example/smoke-test.mjs mame2003_plus` | 本 core DAT 对 `ldrun` 为 20/20 | Lode Runner 标题/投币画面 |
| `ACC-CORE-008` | 180 秒 | `node data/example/smoke-test.mjs dosbox_pure` | thread core、COOP/COEP/CORP | DOOM II 标题画面；已知非阻断 ErrnoError 仅在帧继续且被记录时允许 |
| `ACC-CORE-009` | 180 秒 | `node data/example/smoke-test.mjs nestopia` | FDS 条件 BIOS | Family Computer/FDS 启动画面 |
| `ACC-CORE-010` | 180 秒 | `node data/example/smoke-test.mjs melonds` | 三个逐文件外部 BIOS、pointer | Zoo Keeper 标题菜单而非 FreeBIOS 空白双屏 |
| `ACC-CORE-011` | 180 秒 | `node data/example/smoke-test.mjs desmume2015` | pointer、无 BIOS | Zoo Keeper 标题菜单 |
| `ACC-CORE-012` | 180 秒 | `node data/example/smoke-test.mjs desmume` | pointer、无 BIOS | Zoo Keeper 标题菜单 |
| `ACC-CORE-013` | 180 秒 | `node data/example/smoke-test.mjs a5200` | 7z 来源、`.a52` 物化、5200 BIOS | Super Breakout 游戏画面 |
| `ACC-CORE-014` | 240 秒 | `node data/example/smoke-test.mjs pcsx_rearmed` | 单文件 CHD、PSX BIOS | PlayStation/游戏启动画面 |
| `ACC-CORE-015` | 240 秒 | `node data/example/smoke-test.mjs mednafen_psx_hw` | thread、software renderer、PSX BIOS | PlayStation/游戏启动画面 |
| `ACC-CORE-016` | 180 秒 | `node data/example/smoke-test.mjs handy` | Lynx BIOS、PersistentSave NONE | Lode Runner 标题/游戏画面 |
| `ACC-CORE-017` | 240 秒 | `node data/example/smoke-test.mjs yabause` | 单文件 CHD、Saturn BIOS | Sega Saturn 游戏画面 |
| `ACC-CORE-018` | 180 秒 | `node data/example/smoke-test.mjs genesis_plus_gx` | ZIP 单成员 `.md` | Felix the Cat 标题画面 |
| `ACC-CORE-019` | 240 秒 | `node data/example/smoke-test.mjs mupen64plus_next` | raw `.z64` | Dr. Mario 64 标题画面 |
| `ACC-CORE-020` | 240 秒 | `node data/example/smoke-test.mjs parallel_n64` | 产品/runtime ID 均为 `parallel_n64` | Dr. Mario 64 标题画面 |
| `ACC-CORE-021` | 240 秒 | `node data/example/smoke-test.mjs opera` | 单文件 CHD、3DO BIOS | Total Eclipse 游戏画面 |
| `ACC-CORE-022` | 180 秒 | `node data/example/smoke-test.mjs prosystem` | 7z 来源、`.a78` 物化、BIOS、PersistentSave NONE | Asteroids 标题画面 |
| `ACC-CORE-023` | 180 秒 | `node data/example/smoke-test.mjs stella2014` | 7z 来源、`.a26` 物化、PersistentSave NONE | Freeway 游戏画面 |
| `ACC-CORE-024` | 180 秒 | `node data/example/smoke-test.mjs picodrive` | ZIP 单成员 `.md` | Felix the Cat 标题画面 |
| `ACC-CORE-025` | 180 秒 | `node data/example/smoke-test.mjs mednafen_pce` | raw `.pce` | Adventure Island 游戏画面 |
| `ACC-CORE-026` | 240 秒 | `node data/example/smoke-test.mjs mednafen_pcfx` | 单文件 CHD、PC-FX BIOS | 光盘内游戏菜单 |
| `ACC-CORE-027` | 180 秒 | `node data/example/smoke-test.mjs mednafen_ngp` | raw `.ngp` | Pac-Man 游戏画面 |
| `ACC-CORE-028` | 480 秒 | `node data/example/smoke-test.mjs ppsspp` | raw CSO 与 ISO 两个独立 run、thread、assets、启动动作、PersistentSave NONE | 两种格式均到达 Sheep Defense 标题画面 |
| `ACC-CORE-029` | 180 秒 | `node data/example/smoke-test.mjs beetle_vb` | 无 BIOS；4 条固定启动动作；artifact hash 命中 | Panic Bomber 动画开场 |
| `ACC-CORE-030` | 180 秒 | `node data/example/smoke-test.mjs mednafen_wswan` | 解包后的 raw `.ws`；无 BIOS | Mingle Magnet 标题画面 |
| `ACC-CORE-031` | 180 秒 | `node data/example/smoke-test.mjs smsplus` | raw `.sms`；无 BIOS | Bank Panic 标题画面 |
| `ACC-CORE-032` | 180 秒 | `node data/example/smoke-test.mjs fbalpha2012_cps1` | 专属 DAT 命中 `1941`；无 DAT_MACHINE 缺项 | 1941 attract/游戏画面 |
| `ACC-CORE-033` | 180 秒 | `node data/example/smoke-test.mjs fbalpha2012_cps2` | 专属 DAT 命中 `sgemf`；无 DAT_MACHINE 缺项 | Pocket Fighter 动画开场 |
| `ACC-CORE-034` | 180 秒 | `node data/example/smoke-test.mjs genesis_plus_gx_wide` | `4.3.0-pre` artifact；raw `.md` | Fix-It Felix Jr. attract 画面 |
| `ACC-CORE-035` | 240 秒 | `node data/example/smoke-test.mjs azahar` | thread、COOP/COEP/CORP、WebGL2、raw `.cci` | Cave Story 2D 中文标题/菜单 |

`ACC-CORE-028` 必须同时生成 `ppsspp-cso` 与 `ppsspp-iso` 两条机器结果和两张截图；任一格式失败即为整个 Case 失败。`ACC-CORE-032` 与 `033` 还必须通过专属 DAT 的产品导入、READY Variant、Launch 和跨核 DAT 拒绝集成结果。任一核心失败只重跑该 Case 进行诊断；共享 loader/runtime 变化时仍逐个运行三十五个 Case，不使用一个无界全量 Case 代替。

## 15. UI、4K 与无障碍

### ACC-UI-001：认证入口、用户导航与权限入口

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-UI-001`。
- 流程：从全新浏览器 context 分别访问 PENDING 实例、READY 实例的首页、带 query 的游戏库和管理后台；依次以 USER、ADMIN 登录并从游戏卡片进入详情，再访问认证页和退出。
- 通过标准：PENDING 只进入 `/setup`；READY 匿名重定向 `/login?returnTo=...` 且登录后恢复站内 path/query；已登录访问认证页回首页。用户侧仅首页、游戏库、我的存档、最近游玩四个主菜单并有账户菜单；只有 ADMIN 显示底部管理入口，USER 直达后台显示 403。游戏详情不出现在侧栏且保持游戏库上下文；退出清除会话并回登录，无移动端验收分支。
- 证据：导航可访问名称、route 序列和截图。

### ACC-UI-002：游戏入库父子导航

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-UI-002`。
- 流程：依次访问 `/admin/imports`、`/admin/imports/new`、`/admin/imports/tasks`、`/admin/reviews` 和 `/admin/reviews/history`，使用浏览器前进/后退。
- 通过标准：“游戏入库”可点击进入独立总览；导入、任务、待审核、历史是同级缩进子菜单；父级上下文与当前子项同时高亮；页面不是通过页内 Tab 伪装路由，浏览器历史正确。
- 证据：每条 URL、导航状态和五张当前截图。

### ACC-UI-003：首页、游戏库、详情与存档流程

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-UI-003`。
- 流程：检查首页时长、最近游玩和按添加时间倒序的最新 10 款游戏；在游戏库搜索并按平台/目录筛选；从卡片进入详情；查看封面、元信息、时长、最近 4 份存档、全量存档 Drawer、截图预览、核心和 DOS 程序；从存档次要入口进入详情。
- 通过标准：首页五层顺序为最近玩的游戏/快速开始、最近游玩、最新添加、平台、资料库摘要；最新添加只含启用目录中的已发布游戏，最多 10 款且以创建时间和 Game ID 稳定倒序，入口进入游戏详情，“查看游戏库”恢复最近加入排序。筛选进入 URL 且刷新可恢复；卡片只显示已发布游戏；详情信息完整，默认核心状态准确；存在简介时全文可见、不行数截断，在 3840px viewport 中简介占满 Hero 中栏可用宽度而不留固定空白；详情只内联最近 4 份存档且 Drawer 包含当前游戏全部存档；取消运行方式对话框不修改偏好，应用后才生效；存档主操作直接启动、标题/次要操作才进详情。
- 证据：URL/query、可访问 DOM 断言和关键截图。

### ACC-UI-004：加载、空、错误、Warning 与 Blocker 状态

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-UI-004`。
- 流程：用 stub 依次返回加载、空列表、可重试错误、BIOS hash Warning 和必需依赖 Blocker。
- 通过标准：每种状态有明确文本和下一步；Warning/Blocker 不只靠颜色；错误不残留旧数据；loading 不造成布局跳变或二次 Start。
- 证据：五种状态的语义断言和截图。

### ACC-UI-005：用户侧最小桌面、1440p 与 4K

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-UI-005`。
- 流程：在 `1280×800`、`2560×1440` CSS viewport，以及 3840×2160、100% scale viewport 分别打开首页、游戏库、详情、存档、收藏、最近游玩、联机大厅、账户和 Player Shell；首页另在物理 4K 高缩放的代表性 `1920×950` CSS viewport 复测。
- 通过标准：无页面级横向溢出、遮挡、过小控件或跨屏长文本；同一 viewport 下首页、游戏库、详情、存档、收藏、最近游玩、联机大厅与账户页的主容器相对应用内容区的左、右间距分别一致，测量误差均不超过 1px。3840×2160 与 `1920×950` 首页五层均完整落在首屏且 `documentElement.scrollHeight <= clientHeight`，不出现纵向滚动条，紧凑态仍保持正文和卡片信息清晰可读。3840×2160 首页宽度至少占应用内容区 65%，最末层底边距离 viewport 底部不超过 48px，避免内容聚集在屏幕上半部。三个基准 viewport 的游戏库分别为 4/6/8 列，共享页面有效内容宽度不超过 2320px。详情页在 `2560×1440` 与 `3840×2160` 下 Hero、信息条和最近 4 份存档均完整落在首屏，截图保持比例，Drawer/对话框不推动页面布局；`1280×800` 下关键启动操作和存档区仍在首屏可达。Player stage 为无边距的 100vw×100dvh；运行后 58px toolbar 自动移出画面，只有指针进入顶部 32px、键盘操作或工具栏获焦才恢复，画面中央 pointermove 不改变可见性，标题/Core/平台和同步状态不挤压主操作。点击顶部 toolbar 的标题空白或任一操作都先暂停且保持暂停，只有点击游戏画面恢复；点击模拟器设置控件不能误恢复。EmulatorJS 原生底部工具栏启动后及靠近底边时始终隐藏；Retrom 的“模拟器设置”首次点击直接显示包含控制、显示、Core 设置、音量、静音和收起的自绘工具栏，桥接出来的原生设置面板与自绘栏均不存在 EmulatorJS 退出按钮。canvas rect 完全在 viewport 内，CSS/drawing-buffer 宽高比误差 ≤0.01，宽或高至少一边与 viewport 对应边误差 ≤2px，另一边按 contain 公式在水平和垂直方向居中，未被裁切或拉伸。
- 证据：三个 viewport 的布局测量、overflow 断言和页面截图。

### ACC-UI-006：管理侧 4K

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-UI-006`。
- 流程：在 `1280×800`、`2560×1440` 与 `3840×2160` 三个 viewport 打开入库总览、新建导入、任务、待审核、历史、游戏管理列表与详情、游戏目录、用户管理、`/admin/bios` 和 `/admin/bios/dats`。
- 通过标准：表格/卡片密度可读，筛选和主操作可达；子菜单缩进清晰；所有列出的管理页面在同一 viewport 下相对应用内容区的左、右间距分别一致，测量误差均不超过 1px。2560/3840 下历史 diff、任务阶段、BIOS hash 和 DAT 版本不被截断或横向藏在视口外，Arcade BIOS 条目对比左右栏可读。游戏目录表按“游戏目录—游戏平台—扩展名—游戏数”排列，扩展名与平台级已验证 payload 规则一致，名称列收窄后仍可读。1280 下没有页面级横向溢出；确需横向滚动的宽表只在带可见提示的局部容器中滚动，行首标识与行末主操作 sticky、键盘可达。游戏管理详情的发布信息/媒体/运行版本/管理操作四区在三个 viewport 均可达；封面容器保持 3:4 并在 3840px 双栏布局中等比延伸到媒体内容底边，发布信息与媒体面板同高且媒体不能撑出左侧空白。
- 证据：布局断言和每类页面当前截图。

### ACC-UI-007：键盘、标签与减少动画

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-UI-007`。
- 流程：只用键盘遍历主路径；检查图标按钮 accessible name、焦点、下拉状态文案；启用 `prefers-reduced-motion`；从 Player Shell 退出。
- 通过标准：Tab 顺序与视觉顺序一致，焦点清晰且无陷阱；状态包含“目录默认/缺 BIOS”等文本；颜色不是唯一载体；减少动画生效；保留 Escape 全屏语义并有明确退出游戏按钮。
- 证据：axe/语义断言、键盘 trace 和减少动画截图。

### ACC-UI-008：大量条目的待审队列与详情上下文

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-UI-008`。
- 流程：创建两个 ImportJob，其中一个含 60 个 REVIEW_PENDING Item、另一个含 3 个；从任务页进入前者的待审核，加载第二页后选择第 57 项，修改标题并等待实时保存，再切换到第 3 项并返回。修改第 3 项草稿并等待实时保存，选择第 58 项后用浏览器前进/后退；最后 Approve 第 3 项并 Discard 第 58 项。分别在 1280×800 和 3840×2160 执行，并用键盘完成一次筛选和非顺序选中。
- 通过标准：任务入口带精确 `importJobId`，队列只显示该批 60 项且可清除筛选查看 63 项；每行可辨认来源、草稿标题、批次、目录、Validation/Blocker、候选数量和更新时间，cursor 分页无重复/漏项。3840 下详情的“发布成什么”与左侧两容器堆叠总高一致，元信息位于中间、当前封面位于最右；简介标签与文本域间距不超过 8px，剩余栏高扩展文本域而不是标签空白；封面等比占满栏内剩余高度且底边对齐内容底边，不受固定最大高度限制，也不出现重复的候选摘要或信息来源卡片。审核决定中的“重新运行检查 / 运行游戏、丢弃条目 / 通过并发布”按两行两列显示，四个按钮计算宽度与高度一致，Tab 顺序同视觉顺序；页首截图槽只显示当前 READY Validation 的第 5 秒截图。1280 下列表/详情路由明确分离且详情顺序折叠为单栏。选择任意项都会更新 `/admin/reviews/:itemId` 并保留筛选、已加载页和滚动位置，前进/后退可恢复。字段和来源修改经防抖串行实时保存，离页前已成功冲刷且没有额外“保存草稿”按钮；决策后只移除对应行并聚焦相邻项。页面没有批量 Approve/Discard，所有最终决策各自使用当前 ETag/Idempotency-Key/ReviewEvent，两个批次不会串项。
- 证据：API query/cursor、route 序列、键盘 trace、决策前后队列 DOM 及两个 viewport 的当前截图。

### ACC-UI-009：账户与用户管理全流程

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-UI-009`。
- 流程：在 `1280×800`、`2560×1440` 和 `3840×2160` viewport 完成 setup、test login、邀请复制/注册、logout/login、管理员创建密码重置链接、重置、账户改密及管理员用户筛选/Drawer/角色/状态/删除；确认登录页不提供自助找回密码，账户资料只读且管理员不能代改 displayName/密码。覆盖空、loading、通用错误、429、ETag 冲突、本人和最后管理员状态；只用键盘重复邀请与 Drawer 流程并运行 axe。
- 通过标准：路由和表单符合 `ACC-AUTH-*`；secret 只在一次性对话框出现并从 fragment/状态及时清除；表格无页面级横向溢出，身份/操作列 sticky，Drawer/对话框焦点受控且关闭后返回触发器。危险确认包含用户名和影响，自身/最后管理员控件禁用并解释原因，错误/空/loading 不泄露旧数据或改变布局；测试模式有文本警告，密码/secret 不被辅助技术意外回读。
- 证据：三 viewport 当前截图、route/network/storage trace、axe/键盘结果与后端生命周期摘要。

## 16. 多盘系统

### ACC-MDISC-001：递归目录中的完整多盘导入

- 上限：600 秒。
- 执行：`make acceptance-case CASE=ACC-MDISC-001`。
- 流程：以启用多盘的 Saturn/yabause 目录选择一棵包含两个合法 M3U 子目录和未引用文件的 DIRECTORY；确认 Web 自动识别后显式提交 `MULTI_DISC_M3U_V1`，等待两个 Item 进入审核并发布。
- 通过标准：两个不同 M3U 父目录形成两个 Item；盘序、PRESENT 状态、canonical name 与来源引用正确，未引用文件为 `IGNORED/NOT_REFERENCED_BY_PLAYLIST`；完整组 generation 4 validation READY、可审核发布，STANDARD 缺省语义未改变。
- 证据：capability/预检截图、Upload/Import 配置快照、Item/entry/file outcome、validation 与发布实体。

### ACC-MDISC-002：三盘缺盘与精确补传

- 上限：600 秒。
- 执行：`make acceptance-case CASE=ACC-MDISC-002`。
- 流程：导入缺 Disc 3 的三盘目录；记录 BLOCKED Review 后分别提交错误集合与精确缺失集合，等待 Attachment Job，重新读取 Review 并发布。
- 通过标准：缺盘 entry 不引用假 Blob且 Approve 禁用；错误/意外 basename 不推进 effective snapshot；精确补传创建新不可变 snapshot 与 generation 4 READY validation，旧 snapshot/entry 保持不变，随后才可发布。网络重放不重复 Attachment，retryable failure 只能重试同一 Job。
- 证据：前后 Review/ETag、Attachment/Job/event、两份 snapshot/entry、Blob 引用与发布结果。

### ACC-MDISC-003：解析、安全与容量负向

- 上限：600 秒。
- 执行：`make acceptance-case CASE=ACC-MDISC-003`。
- 流程：依次覆盖整树零 M3U、同目录多个 M3U、非法 UTF-8/entry、traversal、绝对/跨目录/URI 引用、重复/case-fold 歧义、坏 CHD、1/9 盘和总量超过 1 GiB；另在同一目录树保留一个合法分组。
- 通过标准：每项在规定 create/worker 边界以稳定 code 拒绝，无宿主路径访问、越权 Blob、无界读取或半成品发布；局部非法 dirname 只产生 rejected outcome，其他合法目录仍形成可处理 Item。parser fuzz seed 无 panic、越权 I/O 或无界分配。
- 证据：逐向量请求/任务结果、file outcomes、资源上限与数据库/CAS 无副作用断言。

### ACC-MDISC-004：发布、Launch 与内容锁定

- 上限：600 秒。
- 执行：`make acceptance-case CASE=ACC-MDISC-004`。
- 流程：发布固定多盘组并创建 Launch；读取 config、playlist 以及每张盘的 HEAD、完整 GET 和单 Range，再尝试原始 basename、未锁定 index、复制 launchId 无 cookie、另一 Launch cookie 和过期 cookie。
- 通过标准：content identity、canonical playlist、ordered Disc hashes、Variant V3 digest 和 Launch 锁定值一致；`gameUrl/externalFiles/discSet` 完整且连续；合法内容的 ETag/长度/Range 正确，所有跨范围读取失败且不泄露 Blob ID、原始路径或 capability。
- 证据：发布 revision/validation snapshot、Launch/config、内容响应摘要、授权负向和 CAS hash 对照。

### ACC-MDISC-005：Saturn 双盘真实运行

- 上限：600 秒。
- 执行：`make acceptance-case CASE=ACC-MDISC-005`。
- 画面复核：机器断言通过后执行 `scripts/acceptance/run.sh review-multidisc ACC-MDISC-005 passed|failed '<本次画面观察>'`；未复核保持 BLOCKED。
- 流程：使用 `fixtures.json` 锁定的受控 Saturn 双盘 fixture 走真实发布/Launch/Player，等待有效画面后执行 `0 → 1 → 0`。
- 通过标准：进入游戏而非运行时菜单；真实 `diskCount=2`，每次切盘都回读正确且换盘后帧继续推进；Player 当前盘、busy、成功/失败状态与 runtime 一致。
- 证据：fixture/hash、artifact/adapter、config、external file 非零长度、事件/帧 delta、换盘前游戏画面、换盘后当前截图和机器断言 JSON；不保存 ROM bytes 或绝对来源路径。内容在换盘后自身处于黑场时仍保留原图，不用该单帧替代盘号回读与帧推进断言。

### ACC-MDISC-006：Saturn 三盘与跨盘存档

- 上限：900 秒。
- 执行：`make acceptance-case CASE=ACC-MDISC-006`。
- 画面复核：机器断言通过后执行 `scripts/acceptance/run.sh review-multidisc ACC-MDISC-006 passed|failed '<本次画面观察>'`；未复核保持 BLOCKED。
- 流程：使用受控三盘 fixture 启动，往返全部 index；在光盘 2 创建手动状态存档，退出后从该存档重新启动并记录 adapter 事件顺序，同时验证 PersistentSave。
- 通过标准：真实 `diskCount=3` 且全部 index 可切；SaveState `discIndex=1`，重启严格先完成 PersistentSave 注入、切到光盘 2 并回读，再显式 load state，之后才恢复 main loop/start；单盘和 PersistentSave 行为无回归。
- 证据：fixture/hash、external file 非零长度、盘切换/帧事件、SaveState/Launch 锁定、adapter 调用顺序、PersistentSave 前后 hash、换盘前游戏画面与换盘后当前截图。

### ACC-MDISC-007：能力、替换与共享 adapter 回归

- 上限：600 秒，不在本 Case 内重跑三十五核心。
- 执行：`make acceptance-case CASE=ACC-MDISC-007`。
- 流程：检查 Saturn/yabause 与 PSX/3DO/PC-FX capability；验证省略 `contentMode`、默认核心影响、完整目录替换成功/失败、V2→V3→V3 bootstrap、关闭/重开 flag 对新建与既有任务/内容的影响；最后读取同一 run ID、commit 的 `ACC-CORE-001`–`035` 独立结果。
- 通过标准：只有能力交集暴露 MULTI；缺省始终 STANDARD；替换创建新不可变 revision且失败保留旧 current；artifact ID 不变、version 只递增一次、重放不改 updated time，既有 Variant/PersistentSave 绑定不变；flag 关闭只阻止新建/替换，不偷换冻结任务且已发布内容仍可运行。三十五核心结果必须全部为本次 PASS，缺失、旧 commit 或合并结果均失败。
- 证据：capability/flag 矩阵、替换前后 revision、bootstrap 行、在途/已发布行为和三十五份 Case 引用。

### ACC-MDISC-008：授权、审计与私有数据隔离

- 上限：600 秒。
- 执行：`make acceptance-case CASE=ACC-MDISC-008`。
- 流程：匿名、普通 USER 和两个 ADMIN 探测全部新增管理 route；两个管理员复用同一幂等键提交不同主体请求。再让两个普通账号运行同一个多盘 Game，分别创建 SaveState/PersistentSave并尝试交叉 ID、cursor、幂等和 Launch 访问，最后停用其中一个账号。
- 通过标准：匿名为 401、USER 为 `ADMIN_REQUIRED`，ADMIN 写入保存真实 User actor，同 key 不跨 principal 串响应；两个 Profile 的盘号存档和持久保存互不可见/不可写，跨账号探测不泄露存在性；停用只撤销目标账号 Launch，不影响另一账号。结果同时满足本次 `ACC-AUTH-006` 与 `ACC-ISO-001`–`003` route/owner inventory。
- 证据：非秘密 User/username、route 状态矩阵、actor/idempotency/owner 断言、Launch 撤销与通用 Case 引用；截图/API DTO 不暴露 Profile ID 或内容秘密。

## 17. 收藏与收藏夹

### ACC-FAV-001：Migration、关系不变量与备份保留

- 上限：120 秒。执行：`make acceptance-case CASE=ACC-FAV-001`。
- 流程：空库、023 fixture、024 fixture 升级到 025；两个 Profile 对同 Game 建独立关系；执行跨 owner、未收藏 Membership、重名、非法 UPDATE/version 负向 SQL；隐藏 Game/目录后备份恢复。
- 通过：schema/checksum/FK/index/trigger 正确，负向 SQL 全部拒绝，隐藏关系保留而投影为零，恢复逐项一致且认证安全围栏仍生效。
- 证据：起止版本、schema 摘要、负向矩阵、owner 行数与恢复前后 hash。

### ACC-FAV-002：API、幂等、并发与隔离

- 上限：120 秒。执行：`make acceptance-case CASE=ACC-FAV-002`。
- 流程：覆盖全部收藏 route、四种分页排序、非法 query/body、CSRF/Origin/If-Match、同 key 重放/异 body、两个账号跨 Folder/cursor/key 探测及并发收藏/Folder 修改。
- 通过：OpenAPI 响应与稳定错误一致，owner 不泄漏，幂等按 principal 隔离，并发最终状态唯一，失败零部分写入。
- 证据：请求矩阵、contract snapshot、cursor/ETag/idempotency 摘要与 owner SQL。

### ACC-FAV-003：用户主流程与跨页面一致性

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-FAV-003`。
- 流程：从游戏库收藏，详情加入两个 Folder，收藏页切换/筛选/排序，创建/重命名/删除和批量操作，取消/undo、刷新与前进后退，再切换另一账号。
- 通过：URL、计数、标签和三页状态一致；自动收藏、未分类、删除保留、取消清成员、undo 跳过已删 Folder 语义正确；另一账号为空。
- 证据：route/network/storage trace、关键 DOM、两个账号 API 摘要和截图。

### ACC-FAV-004：状态、键盘与多尺寸布局

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-FAV-004`。
- 流程：1280×800、2560×1440、3840×2160 覆盖 loading/全空/Folder 空/筛选空/错误/冲突、50 项批量和 100 Folder；键盘创建/管理/取消，reduced motion 与 axe。
- 通过：无横向溢出/遮挡，卡宽 270–320px，Rail 头部和新建入口固定且只有中间列表自滚动，批量栏不盖末行，4K 字号/控件达标，dialog 焦点与 Escape 正确，axe 无 serious/critical。
- 证据：三 viewport 测量/截图、键盘 trace、焦点/ARIA/axe/reduced-motion 结果。

## 18. 联机游玩

`ACC-NP-001`–`011` 在启动浏览器或服务前必须先运行 `python3 data/example/verify-fixtures.py`，并确认两个 netplay selector 已逐字节物化；缺失或 hash 不符直接 `FAIL`，不得换 ROM、跳过或使用 mock。真实双端 Case 使用两个或所需数量的独立 Chrome process，不得用同一 browser 的多个 context 代替。机器证据除第 3.3 节通用字段外，固定记录 browser/runtime/core/content/profile digest、参与者数、confirmed frame、rollback/resync/reconnect 计数、双端最终 core digest 和终因；敏感 cookie、能力值与宿主绝对路径不得进入证据。

### ACC-NP-001：导航、搜索、分享与权限

- 上限：120 秒。执行：`make acceptance-case CASE=ACC-NP-001`。
- 流程：房主建房；默认 SUPPORTED 下分别用标题 `F-1`、平台 `Arcade`、目录 `FBNeo 游戏` 和 core `FCEUmm` 搜索并验证平台/集合联动；切 ALL 后检查不可用 blocker 与 URL 恢复；分享无 token 的房间链接。P2 claim/ready，第三账号并发抢 P2，ADMIN 尝试旁路，全流程使用键盘重复。
- 通过：导航按正式顺序出现；筛选、排序、URL 与 blocker 正确；匿名先登录；抢座稳定冲突；ADMIN 无额外权限；链接不含 capability/token；键盘焦点与弹层闭环。
- 证据：两账号 route/DOM/network、冲突错误码、URL、焦点 trace 和 1280 截图。

### ACC-NP-002：Start barrier 与准备失败回滚

- 上限：120 秒。执行：`make acceptance-case CASE=ACC-NP-002`。
- 流程：P1/P2 ready 后人为令 P2 Launch 预检失败，再修复输入并重开。
- 通过：失败局无人进入 RUNNING，Session=`PREPARE_FAILED`，已创建 Launch 全部 REVOKED，Room 回 WAITING 且 ready 清零；新局两端各自仅得到自己的 launch/cookie。
- 证据：Room/Session/Participant/Launch 状态、两端响应与无部分启动断言。

### ACC-NP-003：FCEUmm 双端基线

- 上限：240 秒。执行：`make acceptance-case CASE=ACC-NP-003`。
- 流程：`fceumm-423-f1race-v1` 两端先形成不同初始 state，再由 P1 同步；双方均贡献输入并运行至少 3000 个 confirmed frame，不注入网络故障。
- 通过：native load 改变 P2 core；最终连续三个 checkpoint core digest 双端一致且不同于 neutral baseline；无非预期 resync/终局。
- 证据：双进程 PID、初始/加载后 digest、frame/input/checkpoint 计数、最终截图和机器 JSON。

### ACC-NP-004：FBNeo 双端基线

- 上限：240 秒。执行：`make acceptance-case CASE=ACC-NP-004`。
- 流程与标准：对 `fbneo-423-ldrun-v1` 重复 `ACC-NP-003` 全部流程；运行 logical basename 必须仍为 `ldrun.zip`。
- 证据：除 `ACC-NP-003` 字段外记录 basename 与 content digest。

### ACC-NP-005：100ms rollback 与最终收敛

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-NP-005`。
- 流程：两个首发 profile 分别施加确定性 100ms RTT、±20ms jitter，并每 300 帧延后一次 INPUT，但不破坏 WebSocket。
- 通过：各 profile 至少一次 rollback；预测不超过 8 帧，单次回滚不超过 120 帧；3000 confirmed frame 后连续三个 checkpoint 收敛。
- 证据：确定性延迟 seed、rollback 深度直方、最大 prediction、三次最终 digest。

### ACC-NP-006：断线原座恢复

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-NP-006`。
- 流程：confirmed frame≥600 后断开 P2 socket 5 秒，再以原 AuthSession/cookie 恢复。
- 通过：两端在边界暂停；P2 保留原座；history replay、authority state 与新 epoch 完成后继续；Participant/Launch/PlaySession 各只有一套，暂停区间计时为零。
- 证据：断线/恢复时 frame、epoch、history range、行数和 PlaySession 时长断言。

### ACC-NP-007：访客超时与房主丢失

- 上限：120 秒。执行：`make acceptance-case CASE=ACC-NP-007`。
- 流程：用 fake clock 令 P2 租约超过 10 秒；随后新局令 P1 租约超过 10 秒。
- 通过：访客局以 `PEER_TIMEOUT` 收口、P2 释放、Room WAITING；房主局以 `HOST_LOST` 使 Room ENDED；旧 cookie/socket 均不可复用。
- 证据：fake clock advance、终因、Room/成员/credential 状态和重放拒绝。

### ACC-NP-008：desync 修复上限

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-NP-008`。
- 流程：测试构建连续破坏 P2 `MEM ` digest；每次记录 state transfer 和 recapture。
- 通过：60 秒窗口内前三次 mismatch 均执行真实 native load、recapture 并恢复一致；第四次以 `NETPLAY_UNSTABLE` 终局，之后不再推进也不生成存档。
- 证据：四次 mismatch/resync 序列、native completion、recapture digest、终局与零存档断言。

### ACC-NP-009：存档与内容授权隔离

- 上限：120 秒。执行：`make acceptance-case CASE=ACC-NP-009`。
- 流程：两账号预置不同 PersistentSave 后联机；探测全部 save route，并交叉组合 room/launch/cookie/profile 访问运行内容。
- 通过：两端从干净状态开始且结束后原 save bytes/version 不变；save route 全为 `409 NETPLAY_SAVE_UNSUPPORTED`；任何交叉组合不能读取内容。
- 证据：前后 save digest/version、route 矩阵、跨主体状态与零联机 SaveState。

### ACC-NP-010：协议与安全负向

- 上限：120 秒。执行：`make acceptance-case CASE=ACC-NP-010`。
- 流程：覆盖 foreign/null/missing Origin、错 AuthSession/player/generation、future/replayed input、2MiB+1 message、1MiB+1 state、坏 binary header/RASTATE/digest；同时保持另一独立房间运行。
- 通过：每项按封闭错误/终因拒绝，不泄密、不崩溃、不污染另一房间；严格 JSON 拒绝未知、重复、错型与过深字段。
- 证据：负向矩阵、close code/reason、独立房间前后 checkpoint。

### ACC-NP-011：重启、feature flag 与容量

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-NP-011`。
- 流程：运行中终止并重启后端；再关闭 flag；最后建立 17 个 active room。
- 通过：旧局以 `SERVER_RESTARTED` 收口且 Room/Launch 无假恢复；flag off 隐藏导航并拒绝新 API；第 17 房稳定 `429 NETPLAY_CAPACITY_REACHED`，前 16 房不被驱逐。
- 证据：重启前后 DB、route/navigation、容量响应和前 16 房抽样。

### ACC-NP-012：2–4 人协议边界

- 上限：120 秒。执行：`make acceptance-case CASE=ACC-NP-012`。
- 流程：fake core/transport 分别锁定 2/3/4 occupied mask，注入乱序贡献并计算 4 人 hash；再打开真实首发房间。
- 通过：空座始终 neutral，canonical 只在全部占用座贡献齐全时原子产生；4 人 digest 一致；真实 2P profile 的 P3/P4 显示不支持且不发 claim 请求。
- 证据：三个 mask 的 frame/input/digest、network 零 claim 与房间截图。

### ACC-NP-013：普通单机回归与生产产物

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-NP-013`。
- 流程：普通受支持游戏恢复/写 PersistentSave、创建 SaveState、使用 Player controls、退出并结算时长；检查 production web bundle。
- 通过：普通能力与联机改动前契约一致；联机模式专属禁用不泄漏到 single mode；production 产物不存在测试故障注入或可写 telemetry hook。
- 证据：普通 launch/save/play 断言、Player network/DOM 与产物扫描。

## 19. 缺陷处理与重验

任一 Case 出现非预期行为即登记 defect，不能在原结果上直接改成 PASS：

1. 保存首次失败的 `result.json`、日志、trace 和截图；
2. 在最近可靠层新增回归测试，并证明旧实现失败；
3. 实施修复后运行聚焦回归测试；
4. 重跑原 Case；
5. 重跑受影响类别，最后重跑 `ACC-QA-001`；
6. 在 `defects.json` 记录 root cause、测试路径/名称、修复 commit 和两次 Case result。

若错误只能在真实 EmulatorJS/Chrome 中出现，仍必须在最近确定性边界加自动化测试，并收紧对应 `ACC-CORE-*` 或 UI runner 断言。不得用“只能人工复现”免除固化。

## 20. 最终通过标准

一期项目只有同时满足以下条件才可标记 `PASS`：

- 第 5–18 节所有 Required Case 为 PASS；
- 条件 Case 要么 PASS，要么有可核实的 `NOT_APPLICABLE` 原因；
- 没有 `FAIL`、`BLOCKED`、超时、缺失 Case 或未经解释的重跑；
- 本次生成的三十五核机器结果与画面复核全部通过；PPSSPP 的 CSO、ISO 两个格式 run 均通过；Saturn 双盘、三盘各自的机器结果与画面复核通过；
- `ACC-NP-001`–`013` 全部通过，两个首发 profile 均生成当次双 Chrome process 的 3000 confirmed frame 和连续三个 checkpoint 收敛证据；
- 本次发现的每个 bug 均有回归测试和 red/green 证据；
- `make ci` 和两个镜像 build target 通过，且镜像构建没有启动服务；
- 最终报告记录 commit/dirty 状态、环境、Case 结果、缺陷、未执行项和残余风险；
- 报告不包含 ROM/BIOS、游戏截图内容以外的专有二进制、TLS 私钥、launch capability/cookie 或完整宿主路径；非秘密 launchId 只能用于关联 Case。

AI Agent 的最终交付摘要必须列出：总结果、失败/阻塞 Case ID、实际执行命令、证据目录、本次新增回归测试，以及任何 `NOT_APPLICABLE` 原因。不得仅回复“验收通过”。

## 21. 专题覆盖映射

| 专题 | 统一 Case |
| --- | --- |
| 工程质量与回归 | `ACC-QA-001`–`003` |
| 镜像、本地开发、NG/TLS | `ACC-PKG-001`–`003`、`ACC-DEV-001`、`ACC-NET-001`–`002`（`002` 为部署条件 Case） |
| SQLite、CAS、备份、安全、API、运维 | `ACC-DB-001`–`002`、`ACC-CAS-001`–`002`、`ACC-BKP-001`、`ACC-SEC-001`–`004`、`ACC-API-001`、`ACC-OPS-001` |
| 游戏目录 | `ACC-PLAT-001`–`005` |
| 游戏管理 | `ACC-GAME-001`–`003` |
| 导入、Hasheous、审核、任务恢复 | `ACC-IMP-001`–`008` |
| 多盘导入、运行、回归与隔离 | `ACC-MDISC-001`–`008` |
| BIOS、服务器导入与 Arcade DAT | `ACC-DAT-001`–`006`、`ACC-BIOS-001`–`007` |
| Pegasus 目录导入与游戏视频 | `ACC-PEG-001`–`005`、`ACC-MEDIA-001` |
| 启动、存档与游玩时长 | `ACC-RUN-001`–`005`、`ACC-SAVE-001`–`003`、`ACC-PLAY-001` |
| EmulatorJS 三十五核心 | `ACC-CORE-001`–`035` |
| 账户认证、用户管理与隔离 | `ACC-AUTH-001`–`006`、`ACC-ISO-001`–`003` |
| UI、4K、待审队列与无障碍 | `ACC-UI-001`–`009` |
| 收藏与收藏夹 | `ACC-FAV-001`–`004` |
| 联机房间、双端运行、恢复与隔离 | `ACC-NP-001`–`013` |

本文列出的范围不包含 soak、压力或性能基准；未来若需要性能专项，应另建不阻塞一期功能验收的测试计划，不能把长时间运行 Case 混入本文。
