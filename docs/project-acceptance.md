# Retrom 一期项目验收规范

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期唯一验收基线 |
| 版本 | 1.3 |
| 日期 | 2026-08-06 |
| 执行者 | AI Agent，必要时由人工复核当前运行生成的画面证据 |
| 范围 | 工程质量、镜像、本地开发、平台目录、导入审核、BIOS/DAT、存储、安全、运行时、8 核兼容性和 4K UI |

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
- [平台目录领域设计](./platform-instance.md)
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
| Profile | `local` |
| Fake clock | 初始 `now_ms = 1786000000000`，所有等待、过期、lease 与时长只由 runner 显式推进 |
| Arcade 平台目录 | `acc-arcade-fbneo` → `fbneo`；`acc-arcade-mame` → `mame2003` |
| 普通平台目录 | `acc-nes-fceumm`、`acc-snes-snes9x`、`acc-gb-gambatte`、`acc-gba-mgba`、`acc-dos-pure` |
| 已发布目录 | 使用本地合法夹具为上述平台目录创建固定 Game、MetadataRevision、Asset、GameContentRevision/ContentFiles 和可运行 GameVariant/VariantRevision；标题、排序键及 ID 固定 |
| 游玩数据 | 一条已完成 PlaySession、一条最近记录和一份带固定 PNG 的兼容 SaveState，所有时间相对 fake clock 固定 |
| 入库数据 | 小规模的 queued/running/review-pending/failed Item 与 approve/discard ReviewEvent，供总览、任务、待审核和历史页面使用 |
| Hasheous stub | 命中、未命中、429、超时、500、401 和畸形响应七种固定路由，不要求凭证 |
| ROM/BIOS | `data/example/fixtures.json` 中的固定路径、大小和 SHA-256 |
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
7. 全部完成后运行 `make acceptance-report`，按第 15 节判定项目结论。

## 4. 执行顺序

推荐顺序用于减少无效的 UI 调试：

1. `ACC-QA-*`：静态质量和回归纪律；
2. `ACC-PKG-*`、`ACC-DEV-*`、`ACC-NET-*`：构建、进程与代理边界；
3. `ACC-DB-*`、`ACC-CAS-*`、`ACC-SEC-*`、`ACC-API-*`、`ACC-OPS-*`：基础设施；
4. `ACC-PLAT-*`、`ACC-GAME-*`、`ACC-IMP-*`、`ACC-DAT-*`、`ACC-BIOS-*`：管理与入库；
5. `ACC-RUN-*`、`ACC-SAVE-*`、`ACC-PLAY-*`：产品主路径；
6. `ACC-CORE-*`：逐核心真实画面；
7. `ACC-UI-*`：信息架构、4K 和无障碍；
8. 缺陷回归审计与最终报告。

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
- 流程：先记录 `make release-input-digest`，只构建根 Dockerfile；检查镜像名、非 root User、HTTP 入口、最终镜像配置/发布输入 label、`THIRD_PARTY_NOTICES`、九份许可原文、依赖 manifest allowlist 和三份 DAT；从 manifest 在验收临时目录重建 notice 并逐字节比较。
- 通过标准：默认 target 为 `retrom:latest`，`io.retrom.release-input-sha256` 等于本次 helper 值；构建中所有 runtime/DAT/license artifact 命中固定 hash；notice 顺序、source commit URL、association status 与原始许可 bytes 符合 format v1，且受限制 core 没有被描述为可任意分发。最终文件包含八个 selected core、三份 DAT、九份许可原文和 notice，但不包含 303 MiB 下载 archive、非 allowlist core、`data/game`、本地 SQLite/CAS、source checkout/缓存、TLS 私钥或开发启动命令；Git index 没有被忽略的 DAT/runtime/license payload，也没有大于 5 MiB 的第三方数据文件；构建过程没有 registry push。
- 证据：build log、image ID、RepoTags、User、Entrypoint/Cmd、最终 layer 文件/size 清单、Git tracked-file size 检查和 artifact 校验摘要；不启动容器。

### ACC-PKG-002：前端镜像构建

- 上限：900 秒。
- 执行：`make acceptance-case CASE=ACC-PKG-002`。
- 流程：runner 记录 `make release-input-digest`，调用 `make build-web-image` 并 inspect `retrom-web:latest`；确认 target 在编译生产代码前执行 `data-check`，检查 manifest 的 `player_adapter` 与 `web/features/player/adapters/registry.json` 双向一致且每个登记项有实现，再检查 standalone production 产物、非 root User 和内部 HTTP 入口。最后在临时工作树副本把 manifest adapter ID 改为未知值，运行同一 `data-check` 并要求预期失败；不在主工作树留修改，也不对负向样本再构建镜像。
- 通过标准：默认目标 tag 为 `retrom-web:latest`，`io.retrom.release-input-sha256` 等于本次 helper 值；基线 `ejs-4.2.3-v1` 精确映射版本 `4.2.3`，runtime base/loader 路径命中 manifest allowlist，未知或无实现登记项使临时副本的校验失败；镜像没有开发依赖/缓存、内置后端地址、TLS 私钥或用户数据，Cmd 不是 `next dev`。
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
- 流程：把会记录调用并退出 99 的 `docker` 哨兵放在临时 `PATH` 首位，同时将 `DOCKER` 指向该哨兵后启动未覆盖 `NEXT_DEV_HOST` 的 `make dev`，并把 public origin 设为确定性的开发域名；确认它的 `prepare-deps` 命中本地缓存且不联网，然后依次等待 `127.0.0.1:8080/health/live`、`/health/ready` 与 `localhost:3000` 可访问。保持首个实例运行并再次执行相同 `make dev`，确认新 supervisor 主动识别、停止并等待旧 supervisor 后成功接管。随后读取已登记的 supervisor/Go/Next.js PID 与 start ticks，只向 supervisor 发送 `SIGKILL`，确认两个独立 process group 和数据根 lock 均仍存在；第三次启动必须用登记身份识别并停止两个孤儿 process group，等待 lock 释放后完成接管。通过前端 origin 请求 `/api/v1/home`，再携带该开发域名 Origin 对 `/_next/hmr` 完成 WebSocket upgrade；记录监听 socket 并向已验证的 supervisor 发送 `SIGTERM`。最后把当前验收 shell 的真实 PID/start ticks 写成伪造 dev 登记，确认 stop helper 只清理登记而不终止该进程。
- 通过标准：正常接管和 supervisor `SIGKILL` 后的孤儿接管都不因数据锁、旧 Next lock 或端口失败；每次旧 supervisor 及其 Go/Next 子进程都已退出且只剩新实例；伪造、陈旧或非 `scripts/dev.sh` 身份的 PID 不被终止；Go 与 Next.js 均为宿主机子进程，Docker 哨兵无调用；Go 默认只监听 `127.0.0.1`，Next.js 默认监听 `0.0.0.0:3000`，没有把后端直接绑定到外部接口；前端 rewrite 同源成功，HMR 返回 `101 Switching Protocols` 而非跨源拒绝；退出码与信号处理正确，5 秒内不残留两个子进程。
- 证据：进程树、三个 HTTP 结果和退出后的 PID 检查。

### ACC-NET-001：应用侧代理契约与同源隔离

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-NET-001`。
- 流程：以正常 `make dev` 的 `http://localhost:3000` 同源入口运行 Chrome，连续两次完整 navigation 并采集页面、`/_next`、`/api/v1/home`、`/runtime/emulatorjs/4.2.3/data/loader.js` 和一个 seed Asset；比较两次 CSP/DOM nonce；检查 Next/Go 只监听 HTTP；从非受信来源伪造转发头，再从测试 allowlist 中的受信代理地址提交固定 `X-Forwarded-Proto/Host`。另以 production build 启动短时前端，采集一次 HTML CSP。扫描后端/前端配置、CLI help 与镜像 metadata 是否出现 HTTPS listener、证书或私钥参数。
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
- 流程：读取 `migrations/testdata/supported_versions.json` 的有序整数版本清单，对每个登记的真实旧版本 fixture 逐级迁移到当前版本；再次启动当前 schema 验证幂等；构造比二进制更新的 schema version。绿地首版的清单固定为空数组 `[]`，此时不能伪造旧 schema，仍必须执行当前 schema 二次启动与未来版本拒绝两部分；以后每发布一个新 migration，必须在同一变更把仍受支持的真实旧版本及 fixture 加入清单。
- 通过标准：清单与 fixture 一一对应、无未登记 fixture；有旧版本时数据/外键保留且最终 schema 与全新库同构，所有路径 `PRAGMA foreign_key_check` 无结果。空清单只表示没有历史兼容输入，不跳过幂等和未来版本负向断言；重复启动不重复变更，未来 schema 被快速拒绝且不会写库。
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
- 流程：在验收库写入游戏、Blob、用户 DatVersion、存档、一条未完成 UploadPart 和一条 ACTIVE LaunchSession；另建一条仍在 GC 宽限期、无业务保护边但仍有 `blobs` 行/candidate 的 Blob，以及一个只有物理文件而没有数据库行的 crash orphan；包含一份受保护 archive 及其已物化 entry Blob。保持服务运行调用一次 `retrom backup` 验证拒绝，再正常停止服务，备份到不存在的临时输出路径。检查 v1 目录与 `backup.json` 封闭 schema，注入未知字段、case-fold 冲突路径、错误 mode/count/hash 各做一次负向 restore；尝试向既有路径备份/恢复均验证拒绝。分别用错误 active/list、缺依赖 payload 和根中仅多一个未配置物理版本验证依赖边界，最后用精确 bundle 配置恢复到第二个不存在的数据根，显式启动恢复服务并用原 cookie 访问同一 launch logical path。输入不超过 16 个 Blob/2 个 part。
- 通过标准：在线备份以 `BACKUP_REQUIRES_OFFLINE` 失败且不发布目录；离线 bundle 目录只含规范槽，普通文件 `0600`/目录 `0700`，`files`（不含 manifest 自身）排序且逐项 size/hash/kind/mode 正确，DATABASE/LAUNCH_KEY 唯一，database hash/count/版本证据互相一致。它包含 staging DB 每一条 `blobs` 行对应的 CAS，包括 GC 宽限期 Blob、受保护 archive 与已物化 entry Blob；同时包含 UploadPart、owner-only key、active/有序 dependency version 列表及每份小型 manifest/SHA256SUMS。只有无数据库行的物理 crash orphan被忽略；bundle 不包含 job scratch、内置 DAT/runtime/许可 payload、绝对路径或额外配置快照。registry 校验证明所有保护/ownership/bookkeeping 边都命中 Blob 行；未知/冲突/错误清单、缺少任一 Blob 行对应文件和已存在目标均拒绝，半成品不以最终名可见。错误 active/list 或缺 payload 以 `RESTORE_DEPENDENCY_CONFIG_MISMATCH` 拒绝；依赖根仅多未配置物理版本不影响恢复且不会加入输出配置。恢复命令不联网、不编辑部署配置，只输出要求的版本/active；使用这些精确值启动后 `integrity_check`、`foreign_key_check`、每条 Blob 行的物理文件、part 引用和逐版本依赖校验全通过，记录数、关键 SHA-256 与原环境一致。launch key byte equality 只报告布尔结果，原 cookie 仍通过 hash/session 校验；清单/日志不含 key/capability 明文且不依赖原 WAL/SHM。
- 证据：脱敏 canonical `backup.json`、bundle tree/mode、负向错误矩阵、恢复检查、key equality boolean、cookie 请求结果和前后摘要 hash。

### ACC-SEC-001：Archive 与 XML 输入安全

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-SEC-001`。
- 流程：分别提交固定的小型 `../`、编码 traversal、绝对路径、symlink、条目数/压缩比超限，以及外部实体、内部/参数实体、未闭合/超限/重复 DOCTYPE XML 夹具；另让三份真实基线经过同一 parser。
- 通过标准：恶意输入在写出授权目录前以稳定 code 拒绝；没有外部实体访问、DNS、宿主文件读取或目标外文件，返回稳定 4xx 而非进程崩溃。真实 FBNeo PUBLIC DOCTYPE 和 MAME 内部 DTD 均被安全 scanner 跳过且统计命中 manifest；实现没有联网 DTD parser、正则删 DTD 或预置专用解析旁路。
- 证据：每个夹具的错误码、临时目录前后清单和无外连记录。

### ACC-SEC-002：Launch capability、内容范围与缓存

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-SEC-002`。
- 流程：在临时数据根首次启动并检查 key 权限；以同一 Idempotency-Key/body 并发重放，创建只授权一个 VariantRevision 的 LaunchSession；记录 response/body/Set-Cookie、idempotency record 和数据库行；分别用正确 cookie、无 cookie、错误 cookie、fake clock bootstrap/hard/已 start idle 过期、pre-start finish 与正常 finish 后撤销、其他游戏 logical path、编码 traversal 和 Range 请求访问 game/BIOS/parent/state；在全新 browser context 只复制 `/play/{launchId}`。另读取固定 runtime 与已发布 Asset，并以 symlink/错误长度 key 做启动负向测试。
- 通过标准：key 为原子生成的 owner-only 32 bytes，symlink/错误长度使启动失败；body/URL 只有非秘密 UUIDv7 `launchId`。并发幂等响应的 launchId/capability 相同且 cookie 都有效；32-byte capability 仅以无 padding base64url 出现在 `retrom_launch_<launchId>` HttpOnly/SameSite=Strict/`Max-Age=86400`/限定 Path/无 Domain cookie（生产另有 Secure），数据库 LaunchSession 只有对原始 bytes 的 SHA-256，idempotency record 无 Set-Cookie/secret；finish 尝试清除同 name/path cookie。config/start/heartbeat/finish/save 写入都在限定 Path 内并要求 cookie；无 cookie 但有公开 launchId 也不能更新 PlaySession。config 前 5 分钟 bootstrap TTL、config 后/start 前仅 hard expiry、start 后 2 分钟 idle 均由 fake clock 精确生效，耗时加载不会被未发生的 heartbeat 误杀；pre-start finish 不创建 PlaySession。正确 cookie 只取得清单内内容；缺失/错误/过期/撤销 credential 为稳定 `401`，越界 logical path 为不泄露存在性的 `404`；复制 URL 无 cookie 不能取得 config。受限内容为 `private, no-store` + `Vary: Cookie`，runtime/Asset 为版本化 `public immutable`；单 Range/ETag/MIME/`nosniff` 正确。日志、Referer、诊断、JSON 和路由均无 capability、ROM/BIOS bytes、key 或宿主绝对路径。
- 证据：cookie/数据库摘要（capability 只记录不可逆 hash）、请求矩阵、缓存/Range 响应头、新 context trace 和脱敏扫描。

### ACC-SEC-003：受信内网写请求

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-SEC-003`。
- 流程：分别发送不带 `Origin`/session/cookie/header 的写请求和带跨站 `Origin`、`Sec-Fetch-Site: cross-site` 的写请求。
- 通过标准：两种请求都进入相同的 schema、幂等和领域校验并可成功写入；API 不读取或要求 CSRF token，也不返回宽松 CORS 响应。测试明确记录这是受信内网部署决策，不把 CORS 误作写请求授权。
- 证据：请求矩阵、数据库结果和响应头。

### ACC-SEC-004：Hasheous 媒体 SSRF 与不可信展示数据

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-SEC-004`。
- 流程：使用本地 fake resolver/HTTP transport 模拟允许的 lookup 与 `/api/v1/images/<id>` PNG，以及 lookup 超过 4 MiB/20 candidates/每 candidate 媒体上限、媒体 run 累计超过 100 MiB、redirect 到 loopback/private/link-local/metadata 地址、非 443 端口、第四次 redirect、单项超过 10 MiB、40 MP、伪造 MIME、SVG/HTML；候选文本包含 HTML/script payload。
- 通过标准：lookup 在 15 秒/4 MiB 和候选数量门禁内解析；超限 response 记为 INVALID_RESPONSE 而非截断成功。每个 QueryAttempt/Response/Candidate/Hit/Asset 有同 run 的可验证归属，MISS/错误 response 也能经 attempt 回放；只有 READY candidate asset 可被 ReviewDraft/MetadataRevision 采用。图片只有在 HTTPS `hasheous.org` allowlist、最多 3 次 redirect、≤10 MiB、run 合计≤100 MiB、≤40 MP 且魔数/解码均为 PNG/JPEG/WebP 时写入 CAS；每次 DNS/redirect 后都重检 IP，所有负向输入无外网/内网读取且不落 Blob。候选文本通过 JSON/React 作为纯文本呈现，无 `dangerouslySetInnerHTML`、脚本执行或 SVG 渲染。
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

## 7. 平台目录

### ACC-PLAT-001：创建平台目录与默认核心约束

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-PLAT-001`。
- 流程：为 Arcade 创建 `acc-arcade-fbneo` 并选择 `fbneo`；再尝试选择未关联/停用核心和重复 slug。
- 通过标准：合法目录创建成功；非法默认核心和重复 `(platform_id, slug)` 被 409/422 与数据库约束拒绝；时刻字段为整数。
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
- 流程：分别对非空和空平台目录执行停用/重新启用/删除；停用期间查询用户首页、游戏库、详情、存档并尝试新启动，同时查询管理端游戏。
- 通过标准：非空目录可停用且 Game/存档/revision 不改写；停用后关联游戏从用户首页统计与最近记录、游戏库、详情和存档列表消失，新启动被拒绝，管理端仍可见，重新启用后原记录恢复展示。非空目录仍不可软删除；空目录可软删除，但没有硬删除 API 且旧 slug 不释放；操作写入不可变审计记录；作为默认核心的关联不可被直接禁用。
- 证据：启停/删除响应、用户与管理端查询、Launch 拒绝、目录状态和审计事件。

## 8. 游戏管理

### ACC-GAME-001：元信息、媒体与重新刮削

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-GAME-001`。
- 流程：在游戏管理按关键字、基础平台和平台目录找到固定游戏；检查发布信息/媒体/内容与运行版本/管理操作四区；记录当前 version，编辑标题、简介、年份与类型并替换固定 PNG；针对 current ContentRevision 触发 Hasheous stub 重新刮削，先不采用候选，再选择部分字段和 READY media 应用；构造一个旧 ContentRevision run 做负向 apply，最后用旧 ETag 提交一次并发编辑。
- 通过标准：搜索/筛选结果正确；每次确认修改创建可追溯 MetadataRevision/Asset，ADMIN_EDIT revision 的 source ref 为 NULL 且同事务 AuditEvent 指向新 revision，RESCRAPE_APPLY ref 精确指向被采用 Candidate；游戏库、详情和管理页读取同一当前值。运行区分开显示当前/历史 ContentRevision、各 Core VariantRevision、CoreArtifact/DAT，而不暴露宿主路径/Blob 编辑；显式重新刮削绕过旧 cache，创建独立 MetadataScrapeRun/QueryAttempt/Candidate/Asset 且不自动覆盖，旧 content run 不能 apply；采用范围与字段 diff 一致；旧 ETag 写入以 409 拒绝；Game ID、current content 和平台目录不变。
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

## 9. 导入、刮削与审核

### ACC-IMP-001：单文件导入与发布前隔离

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-001`。
- 流程：通过 upload manifest 选择 `acc-nes-fceumm` 和 `fixtures.json` 指向的单文件夹具；按服务端 fileId 上传 8 MiB parts（该小文件为单尾块），带正确 Content-Range/Content-Digest，重放同 part，再使用当前 ETag/Idempotency-Key complete；断言 `202/FINALIZING` 后等待 UPLOAD_FINALIZE Job 到 SUCCEEDED/session COMPLETE，再创建 ImportJob 并观察至 `REVIEW_PENDING`。另以错误 digest、`../` relativePath 和缺失 part 做负向请求；对缺失 part 只重传服务端列出的 part，以新 key 再 complete。再创建两个小文件的 COMPLETE session，仅把一个文件消费为媒体，用 fake clock 推进 24 小时并运行一次 upload cleanup；另将一个无消费 COMPLETE session 推进 7 天。
- 通过标准：必须选择平台目录且只接受浏览器相对路径；同 part/同 digest 幂等，异 digest/非法路径拒绝；每次接受 complete 都在短事务递增 `finalizationNo`、创建该编号唯一 Job 并转 FINALIZING，不在请求内组装大文件。Worker 从 bytes 重算 hash/CAS、按已完成文件可恢复并删除其临时 part，全部成功才 COMPLETE；同一轮 I/O retry 复用当前 Job，缺失/损坏 part 修复后的 complete 创建递增编号的新 Job，旧失败 Job/事件保持不变且已 COMPLETE 文件不重组装。只有 COMPLETE session 可创建 ImportJob。上传终结、HASHING/IDENTIFYING/SCRAPING 阶段可见；生成一个 ImportItem、规范 source manifest 和匹配目录默认核心的 READY ImportItemCoreValidation，但审核前不创建 Game/ContentRevision/VariantRevision，游戏库不可见。缺少 `Content-Length` 的合法流式 part 仍成功，越过声明 range/8 MiB 上限的 chunked body 在超限处拒绝且不留下 part/Blob 引用。file-level 消费只保留被消费文件，24 小时后同 session 未消费文件引用被裁剪；无消费 COMPLETE session 在 7 天后 EXPIRED；whole-session Import 证据不被裁剪。
- 证据：UploadSession/File/Part 状态、UPLOAD_FINALIZE/Import 任务事件、Blob hash、part/UploadFile 清理前后清单、fake clock、Item 和游戏库查询。

### ACC-IMP-002：目录分组与 GBA 确定性派生

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-002`。
- 流程：先将 `gba-smoke.zip` 上传到 Arcade 目录，令它完成安全扫描但以 `ARCADE_MACHINE_NOT_FOUND` 拒绝；再以新 UploadSession 把相同 bytes 导入 GBA 目录，验证复用同一 Archive Blob/Entry 并将 Unicode `.gba` member 一次性物化。另导入固定 DOS 目录到审核；检查两份 READY ImportItemCoreValidation，再分别 approve 并读取发布实体。
- 通过标准：第一批次不物化无需的 member；第二批次不重复 ArchiveEntry，`materialized_blob_id` 只从 NULL 提升一次，物化 Blob 的 size/四种 hash 等于 entry/fixtures manifest，尝试改回 NULL、替换 Blob 或修改 entry hash 均被数据库拒绝。审核前 DOS 目录形成可追溯 source manifest/程序候选和确定性 ValidationFile，GBA 原 ZIP Blob/ArchiveEntry 保留，且没有提前创建 GameContentRevision。Approve 后 ContentRevision 的 DOS_SOURCE/CONTENT 与来源 pair 正确，VariantRevision 直接引用 ContentRevision并复制已验证派生文件；浏览器启动不临时猜 ZIP 入口，审批事务不读 archive/重新打包。
- 证据：审核前 Item/Validation/ValidationFile，发布后的 GameContentRevision 文件表、来源 archive/entry 与物化 Blob hash、实际 VariantRevision 对 ContentRevision 的引用。

### ACC-IMP-003：三种 Hash profile 不混用

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-003`。
- 流程：导入一个 raw ROM、唯一 ROM member 的 GBA ZIP 与 `ldrun.zip`；读取 Blob/archive entry 和 `content_hash_evidence`，执行 stub lookup，并构造有两个同优先级 ROM member 的非 Arcade ZIP。另以 Arcade 目标分别导入“NORMAL clone + NORMAL parent + 已引用 BIOS/base”和“单独未引用 BIOS/base”两个小批次。
- 通过标准：CAS 始终使用原始 Blob SHA-256；raw 使用 `RAW_FILE_V1`，GBA member 使用 `SINGLE_ARCHIVE_MEMBER_V1`，且均对原始内容 bytes 计算四种 hash、不剥 header；主机 ZIP 发布后的 GameContentRevision 以唯一 `CONTENT` GameContentFile 指向物化 member，原 ZIP 只作为来源证据，VariantRevision 不复制第二份 CONTENT；Arcade 使用 `ARCADE_DAT_ENTRIES_V1`，不查询 ZIP hash，只选择 DAT 直接声明且在 primary CONTENT 中逐名/逐 hash 匹配的 entry，evidence 指向实际 ArchiveEntry，并按规定排序/去重且最多 8 次；零个符合项时无 provider 请求且不回退 parent/BIOS hash。NORMAL parent 同时是自身 Item 与 clone COMPANION；EXPLICIT_BIOS/ROMOF_INFERENCE 只作为 COMPANION，不产生可发布 Game，单独未引用时以 `REJECTED/ARCADE_UNUSED_DEPENDENCY_ARCHIVE` 可见拒绝。多 primary archive 不猜测，文件以 `REJECTED/AMBIGUOUS_PRIMARY_CONTENT` 出现在任务页且不创建无 canonical source 的 ImportItem；7z/RAR/TAR/nested archive 同样以稳定 file-level reason 拒绝；每次上游 body 只含精确 `mD5/shA1/shA256/crc`。
- 证据：数据库字段和发往 stub 的脱敏请求。

### ACC-IMP-004：ImportJob 配置快照不漂移

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-004`。
- 流程：创建任务后暂停 worker；修改平台目录默认核心和活动 DAT；恢复任务并进入审核；先用旧 selectedValidation 尝试 Approve，再等待新 validation 完成并批准。另把同一 ReviewDraft 的目标目录 PATCH 为不同基础平台目录，并记录 Job/Validation 数量。
- 通过标准：处理继续使用创建时的 Platform/Core/artifact/DAT/provider 快照；审核明确提示当前配置已变化，旧 Validation 因目录 version/default artifact/DAT 不匹配而不能发布；新 validation 独立留痕且 READY 后才可 Approve，旧证据不被覆盖。ReviewDraft 只允许同基础平台内换目录；跨平台 PATCH 返回 `422 REIMPORT_REQUIRED_FOR_PLATFORM_CHANGE`，不改 draft、不创建跨平台 Validation，用户只能 Discard 后按目标平台重新导入识别。
- 证据：创建快照、两份 Validation/input digest、当前配置、旧审批冲突、处理日志与最终发布引用。

### ACC-IMP-005：Hasheous 无凭证命中与降级

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-005`。
- 流程：依次让本地 stub 返回贴合上游形状的单 object 命中、`404 text/plain` 未命中、429 + `Retry-After`、超时、`500`、`401` 和 `200` 畸形 JSON；命中 object 同时含 Logo、Screenshot1..4、AIDescription、AI Tags、未知字段及平台名不一致证据，另以乱序完成的两个 hash 命中同一 provider ID。不配置 API Key。重复一次自动 Import 验证有效 HIT/MISS cache，再从审核页显式选择 HASHEOUS 重刮削和 NONE。
- 通过标准：单 object 命中只产生一份按确定 primary response 归一化的候选；首个自动 Run 按固定排序选中首候选并初始化一次 ReviewDraft/metadata，未完成媒体不被自动选中，后续重刮削不覆盖草稿。Logo/四张 Screenshot 映射正确，AIDescription/AI Tags/未知字段只留 raw、平台名只产生 warning，不能改变目录/Core；title/publisher/description/year 和两个 provider score 严格符合 `HASHEOUS_BY_HASH_V1`，normalizationYear 来自 run 时刻而非常量。降级输入均进入可人工补全审核而不丢文件；404 是 MISS，429 是 RATE_LIMITED，超时是 TIMEOUT，500 是可重试 NETWORK_ERROR，401 与畸形 200 是非重试 INVALID_RESPONSE；429/超时/500 只做有界重试，各有持久终态后 Run/Job 仍 COMPLETED/SUCCEEDED 并显示 warning。每个 evidence 的 NETWORK/CACHE attempt 都关联 ProviderResponse，MISS/错误也可从 run 回放。请求 digest 对 JSON map 顺序稳定；自动 Import 只复用 7 天 HIT/24 小时 MISS，不缓存错误；审核显式重刮削绕过 cache，NONE 创建无 evidence/attempt/response/candidate 的 no-op SUCCEEDED Job/COMPLETED Run 且无网络。请求固定为 `POST /api/v1/Lookup/ByHash`，不带 API key，不含 ROM、路径、本地文件名/platform hint，也没有 ScreenScraper/MetadataProxy/Submission 调用。
- 证据：各 Run/Attempt/Response/Job、stub request log、cache 时刻和候选记录。

### ACC-IMP-006：DAT 与元信息证据隔离

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-006`。
- 流程：导入 Arcade fixture，让 DAT 命中且 Hasheous stub 返回不同标题/年份；在审核中选择元信息候选。
- 通过标准：DAT 只决定 machine/parent/BIOS/entry；展示字段只来自选中的 Hasheous 候选或人工编辑；UI、数据库和 ReviewEvent 分开显示两条来源，DAT description 不覆盖标题。
- 证据：审核截图、依赖快照、候选与发布字段。

### ACC-IMP-007：Approve、Discard 与不可变历史

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-007`。
- 流程：对两个 Item 分别编辑字段，选择 READY Validation、文本 Candidate 和来自同 Item 两个已完成 run 的 READY media 后 approve/discard；故障注入证明审批事务没有 archive/ZIP/网络调用；之后尝试修改旧 ReviewEvent，并从历史页回放。
- 通过标准：只有匹配当前目录/config 的 READY Validation 可 Approve；审批只复制 source/ValidationFile refs 并原子发布到唯一平台目录，不做耗时计算。Discard 不发布且不立即删证据 Blob；历史可还原输入、validation、scrape run/candidate/media 混合来源、字段 diff、目录/DAT 快照和理由；旧事件不可更新。
- 证据：游戏库结果、历史 API/页面截图和更新拒绝。

### ACC-IMP-008：有界失败、取消、重试和重启恢复

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-008`。
- 流程：创建含 3 个 Item 的任务，对其中一个故障注入；让一个 Item 已发布、另一个进入 RUNNING、第三个进入 REVIEW_PENDING 后取消整个 Import，观察 ImportJob/运行 Job 的 CANCEL_REQUESTED，再由注入式 reader 的下一个检查点确认 CANCELLED；另建独立失败任务，分别在 IDENTIFYING 和 SCRAPING 注入 retryable 本地故障后调用 Item retry，记录 Job/Run/配置快照；并在另一个阶段终止 worker，使用 fake clock 令 lease 到期后重启。再让 CANCEL_REQUESTED worker lease 过期，验证恢复器只清理/确认取消；对确定性坏输入验证直接进入 FAILED_FINAL 而非虚假的 FAILED_RETRYABLE。
- 通过标准：已发布 Item 不回滚，REVIEW_PENDING Item 在取消事务转 CANCELLED；RUNNING cancel 返回 202，ImportJob 在停止前保持 CANCEL_REQUESTED，最后一个 Worker 确认后才为 CANCELLED，且绝不因已有发布/取消混合计数聚合成 COMPLETED/PARTIAL_FAILURE。取消检查不超过规定 reader/token 边界并且不会发布；旧 worker 在取消/lease 转移后提交被 state+lease token 拒绝；取消中 lease 恢复不继续领域计算。IDENTIFYING retry 复用 pipeline Job并增加 execution，SCRAPING retry 新建 Run/Job且旧证据不变；两者都由 persisted failedStage 分派、保留原 Import 配置，不重复创建 Blob/候选/ReviewEvent。JobEvent 仍按每次真实转换追加；普通过期任务被重新领取并完成；确定性错误直接 FAILED_FINAL，attempt 用尽才从 FAILED_RETRYABLE 进入 FAILED_FINAL；没有长事务或真实等待，任务/审核时刻均为 INTEGER。
- 证据：完整状态转换、引用计数、lease/attempt 和事务时长摘要。

## 10. BIOS 与 Arcade DAT

### ACC-DAT-001：真实 DAT 基线完整性

- 上限：300 秒。
- 前置：计时前已执行一次 `make prepare-deps`，本 Case 期间断网。
- 执行：`make acceptance-case CASE=ACC-DAT-001`。
- 流程：runner 先执行 `make data-check` 与 `make deps-check`，验证小型 manifest schema v3、`player_adapter` ID/runtime base/loader、前端 registry 与实现双向对应、runtime allowlist、八个 selected core、mame2003 override、九份许可来源/JSON Pointer、`SHA256SUMS`、三个已物化 DAT 的 size/hash 及 machine/ROM/merge/biosset/default/bios/nodump/baddump/缺失 hash/关系统计和 core artifact 绑定；离线重建 notice。再用全新临时 SQLite 和真实三份 DAT 断网启动服务，等待 ready，记录 DatVersion/Job/索引统计；重启同一数据根，确认复用结果而不重跑 parser。最后运行 seed/约束负向检查，并用 `git ls-files`/`git check-ignore` 检查 payload 边界。
- 通过标准：离线命令成功，所有值与机器基线一致；基线 manifest 的 adapter 精确为 `ejs-4.2.3-v1 → 4.2.3`，base/loader 命中 allowlist，缺失、未知、版本错配或无实现 adapter 均使 `data-check` 失败；八个 enabled Core 各恰有一条 enabled CoreArtifact 且逐项等于 manifest `selected_core_artifacts`，尝试为同 core 启用第二条被数据库约束拒绝；历史 artifact 可 disabled 保留。冷库先 live/`DEPENDENCY_INDEXING`，三个不可取消 bootstrap Job 在事务外解析、每批至多 1,000 行，索引只在 READY 后可查询，最终三个 Arcade core 各有独立 READY active DAT；重启的 Job/DatVersion ID 与 parsedAtMs 不变且 parser 调用数为 0。可空 hash 精确保留为 NULL/NODUMP 而不伪造，未命中 manifest/未 enabled 的 artifact 不自动使用。许可输入逐项命中 size/hash，notice 可重复生成且 association status 不被升级为虚假精确证据；DAT、EJS archive/runtime、license/notice payload 均未被 Git 跟踪，manifest/SHA/配方被跟踪。整个 Case 断网且启动/解析不尝试 CDN；部署前由 `ACC-PKG-001`–`003` 对两镜像 `io.retrom.release-input-sha256` 的精确比较消除错配。
- 证据：逐文件校验/统计、DatVersion/Job 状态序列与 parser 调用计数、事务批次摘要、Git 跟踪边界和断网 network log。

### ACC-DAT-002：Core 隔离与依赖闭包

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-DAT-002`。
- 流程：用三个活动 DAT 分别解析 `ldrun` 及固定 parent/BIOS 关系样本；从真实 MAME 数据选一个含多个 biosset 的 machine，验证 default 与非 default ROM；分别构造 flat Full Non-Merged、Split 缺 parent/补齐 parent、clone 特有 ROM 只在 parent 子目录的 Merged；交换 core/DAT 组合；再提交一个 DAT 声明必需 disk/CHD 的 machine。
- 通过标准：`ldrun` 在各自 DAT 中为 20/20 必需 entry；machine、clone/parent、BIOS/base archive 闭包来自当前 core/content companions，且只要求唯一 default bios option，NODUMP 排除、BADDUMP Warning；Requirement 不跨 core 串用，也不扫描无归属全局 Blob。Full Non-Merged 由 CONTENT 满足闭包，Split 缺依赖报 MISSING 且补齐后 READY，只有真实合并子目录结构报 `UNSUPPORTED_MERGED_ROMSET`；错误 core/DAT 组合标记未知/不兼容。依赖外层 ZIP 两次生成 hash 相同、只含根级 Store entry，main/BIOS/parent 名称冲突在 Launch 前拒绝，并在对应 Arcade smoke 中证明 v4.2.3 解一层后 core 可见内层 archive。disk 元素可入诊断但必需 CHD 返回 `UNSUPPORTED_CHD`，负向输入都不创建 READY VariantRevision。
- 证据：三份解析摘要、依赖图和负向结果。

### ACC-BIOS-001：正确与错误 Hash 上传

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-BIOS-001`。
- 流程：上传 fixtures 中正确 `disksys.rom`，再上传临时生成的错误内容 `gba_bios.bin`，最后用 fixtures 中正确 `gba_bios.bin` 替换当前安装。对一个固定 Arcade Requirement 再分别上传“必需 entry 名齐全但一项 bytes/hash 不同”和“完全缺少一个必需 entry”的两个小型 ZIP。
- 通过标准：正确文件显示 installed/matched；错误 hash 文件允许保存并明确显示期望/实际 hash Warning，不伪装成 matched，也不因 hash 不同强制拒绝上传；正确替换后活动安装变为 matched，旧 Blob/安装按引用规则保留而非原地改写。Arcade entry 名齐全但 size/hash 不同的 installation 为 active/HASH_WARNING，可装入 Launch bundle且不阻断；完全缺必需 entry 的 installation 可保留为 active/MISSING_ENTRY 供修复但 Launch 阻断；损坏/不安全 ZIP 为 INVALID 且不能 active。
- 证据：三次上传响应、实际/期望 hash、安装 revision、BIOS 状态与 UI 截图。

### ACC-BIOS-002：必需、可选与 Full Non-Merged

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-BIOS-002`。
- 流程：移除 FDS 必需 BIOS 做预检；分别以 `.gb/.gbc/.gba` 小型真实 fixture 检查 Gambatte/mGBA 可选 BIOS 不存在、仅安装另一内容类型 BIOS，以及安装匹配内容类型的正确/`HASH_WARNING` BIOS；读取 Launch config/bundle。再以 entry 名齐全但 hash 不同的 Arcade BIOS/base archive 启动，并检查包含自身依赖的 Full Non-Merged Arcade fixture。
- 通过标准：适用必需文件/entry 完全缺失阻断；不适用 requirement 不进入 digest/bundle，可选文件缺失只提示且不增加 activation option。匹配内容类型的 active `MATCHED/HASH_WARNING` BIOS 以 Requirement 逻辑名装入，Gambatte config 精确增加 `gambatte_gb_bootloader=enabled`、mGBA 增加 `mgba_use_bios=ON`；Arcade entry 名齐全但 size/hash 不同也形成 `HASH_WARNING` 依赖、进入 bundle 并允许启动。另一内容类型 BIOS 不误启用，冲突 option seed 被校验拒绝，浏览器不按 core 名补写。Full Non-Merged 已内含依赖时不要求重复上传；页面按平台/core 聚合而不按平台目录复制，`gamegenie.nes/sgb_bios.bin` 按一期条件明确标“未使用”而非缺失。
- 证据：预检/digest、Launch config、BIOS bundle entry 清单和 BIOS 页面截图。

### ACC-DAT-003：用户 DAT 候选不自动生效

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-DAT-003`。
- 流程：把固定 seed 中真实的 `fbneo-arcade.dat` 作为用户文件上传给 `fbneo`；查看解析与 diff，但不点击启用；随后删除这个未活动、无业务引用的用户候选。
- 通过标准：即使底层 Blob 因相同 SHA-256 去重，也创建来源、上传时刻和状态独立的 DatVersion；兼容状态和空 diff 可见；当前活动 DAT、已有 VariantRevision 诊断与启动结果完全不变；候选可删除但共享 Blob 和预置 DatVersion 不受影响。
- 证据：上传前后 active ID、diff、删除响应、Blob 引用和旧快照 hash。

### ACC-DAT-004：启用、重校验与回滚

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-DAT-004`。
- 流程：在本 Case 内按 `ACC-DAT-003` 的固定输入重新创建独立候选，显式启用并等待有界重校验任务完成；查看旧 GameVariant；回滚到预置 installation。
- 通过标准：不依赖 `ACC-DAT-003` 遗留状态；启用有影响预览和审计；相同内容允许生成 no-op 重校验，但不得静默改写历史快照；回滚恢复活动 DatVersion，新旧版本均可追溯且被引用版本不可删除。
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

## 11. 启动、存档与游玩数据

### ACC-RUN-001：默认核心与单次核心切换

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-RUN-001`。
- 流程：准备一个只有默认 core READY、另一个平台 core 为 `NEEDS_VALIDATION` 的固定游戏；从详情先用默认核心启动，再选择另一核心并只点一次开始。记录首个 launch 请求的 202、并发相同请求、Variant Job/SSE、成功后前端以新 key 自动发出的第二个 launch 请求和最终 201，然后重新打开详情。记录 Worker 的 DB/文件读取和网络；另用 fake clock 推进一条注入阻塞 reader 的 Variant execution 到 30 分钟 deadline。
- 通过标准：下拉默认标记目录核心且覆盖平台全部 enabled core；第二种 core 的首请求在短事务内返回 `202 VALIDATION_PENDING`、没有 LaunchSession/cookie，并发相同 input digest 只复用一个不可由单个 Player 取消的 Job；一个 browser 退出等待只断开自身 overlay/subscription，另一个仍能等到结果。Worker 只使用 `Game.current_content_revision_id` 已入库的 source/hash/ArchiveEntry/DAT 证据，无 Hasheous/外部网络或全局 CAS 猜测，事务外流式物化必要 bundle；最终只产出一个直接引用 ContentRevision/input digest 的 READY revision/current。同一加载壳自动以新 key 取得 201 并开始，没有人工第二次 Start、确认页或静默回退；旧 key 仍重放原 202。注入超时以 `LAUNCH_CORE_VALIDATION_TIMEOUT` FAILED 且不留 current/半成品，未真实等待。另用缺 BIOS 输入证明 BLOCKED revision 被去重且不成为 current，安装 BIOS 后 input digest 改变并可创建新 Job 验证。切换只覆盖最终 LaunchSession，不修改 Game current content、平台目录或形成“最近核心”隐式默认；重新打开后该 core 显示 READY。
- 证据：三个 launch HTTP 结果/Set-Cookie 有无、Job/InputSnapshot/SSE、coreOptions、ContentRevision/source manifest digest、SQL/网络摘要、并发结果、目录记录和重新打开后的选择值。

### ACC-RUN-002：一次点击、自动开始与默认全屏

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-RUN-002`。
- 流程：在详情点击一次“开始游戏”，记录原始点击、Fullscreen 调用、launch/config 请求、iframe 配置、EmulatorJS network 和 start 事件；用普通 core 与 `mame2003` override 各执行一次短流程。
- 通过标准：对始终存在的 `document.documentElement` 的 Fullscreen 请求仍在用户激活链且发生于第一个 await 前；同一 Player Shell 显示加载并自动开始；没有 Retrom 第二个 Start 或 EmulatorJS `Play Now`；进入有效帧画面。config 严格符合 HTTP 契约且不含 secret/Blob/宿主路径；`emulatorGameId` 为 `1..9007199254740991` 的 JSON number、`gameName` 为其稳定十进制派生，Arcade `gameUrl` basename 精确为 DAT machine 的 `<machine>.zip`。iframe 先设置 `player/pathtodata/gameName/gameID/paths` 再加载固定 loader，`typeof EJS_gameID === "number"`。EJS 配置固定 `language=zh-CN`、`disableAutoLang=false`（按 v4.2.3 的反向 sentinel 语义），网络只请求 manifest 中的 `zh-CN.json`，不得按系统 locale 或 CDN fallback；普通 core artifact 来自 config 的 basename 映射，`mame2003-wasm.data` 精确请求固定 4.2.1 override，未请求 4.2.3 同名 artifact 或外部 CDN。
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
- 流程：在 DOS 详情加载可执行程序列表，选择非默认程序并启动；检查锁定内容、config 与按需 `game.conf`；再选择“显示 DOSBox Pure 程序菜单”，最后构造选中程序已不存在的 revision。
- 通过标准：直接启动仍以原 `game.zip`/Blob 作为 `EJS_gameUrl`，选择入口前后 Blob 数不增加；`EJS_externalFiles` 只把 `/game.conf` 映射到本次 Launch 的受限 config URL，`EJS_defaultOptions.dosbox_pure_conf == "outside"`，端点逐字节返回运行时专题规定的 `[autoexec]` 且不落磁盘/数据库，安全路径可进入所选程序画面，没有伪造 `DOSBOX.BAT`，运行页不二次询问。程序菜单选项使用原 bundle 且无 external config；缺失 entry 以 `LAUNCH_DOS_ENTRY_MISSING`、含 `%`/引号/控制字符/尾随空格或点等不安全 entry 以 `LAUNCH_DOS_ENTRY_UNSAFE` 阻断直接启动，且都不猜替代程序。
- 证据：程序列表、launch/config payload、原 Blob/引用计数、按需 config bytes、core option、运行画面与错误响应。

### ACC-SAVE-001：手动状态存档与截图

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-SAVE-001`。
- 流程：启动游戏后创建手动状态存档；读取存档记录与“我的存档”卡片。
- 通过标准：状态 Blob 与非空截图 Blob 同时存在且在同一事务引用；记录 Profile、Game、CoreArtifact、GameVariantRevision、名称、整数时间和累计时长；缺截图或空 state 的创建请求被拒绝。
- 证据：存档 API/数据库、CAS hash 和当前截图。

### ACC-SAVE-002：三个入口快速恢复与不兼容拒绝

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-SAVE-002`。
- 流程：先在本 Case 的 seed 中按 `ACC-SAVE-001` 规则创建一份带截图的有效存档；分别从详情存档、我的存档和首页继续入口恢复；再用不同 Core/revision 尝试加载。
- 通过标准：三个入口均一次点击直达 Player Shell，不经过详情或二次 Start，且使用存档锁定环境；不匹配时明确拒绝，不静默迁移或改用目录默认核心。
- 证据：三条 route/launch trace 和负向错误。

### ACC-SAVE-003：PersistentSave 更新

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-SAVE-003`。
- 流程：seed 一份服务端 PersistentSave；创建两个锁定相同 base 的 Launch，在第一条加载 loader 前预取，记录 `EJS_ready → saveDatabaseLoaded → start` 顺序并验证恢复；让第一条触发两次并发 `saveSaveFiles`、一次手动 export 和 `exit`，记录每个 request 的 sequence/event/hash，再让第二条用旧 base 保存。重放相同 sequence/body，并分别复用 sequence 改 event/bytes、制造跳号。另以无服务端保存但浏览器存在旧 IDBFS 文件、上传失败，以及 fake reader 报告超过 64 MiB 做负向测试。
- 通过标准：保存按 `Profile + GameVariantRevision + kind` 隔离；Launch GET 始终返回其创建时锁定的 revision。最多 64 MiB 的服务端 bytes 在 loader 前形成一个有界 `Uint8Array`，超限不分配完整 body 并以 `LAUNCH_PERSISTENT_SAVE_TOO_LARGE` 阻断；在真实 v4.2.3 且 `EJS_disableDatabases=true` 时 `saveDatabaseLoaded` 仍于 start 前触发，同步覆盖/清除 IDBFS 目标并调用 `loadSaveFiles()`，不会复活旧浏览器数据；在 `saveState()` 前已注册至少一个 saveState listener，v4.2.3 以 listener 数而非 callback 返回值阻止 fallback 写独立 state DB，自动更新来自真实 `saveSaveFiles` event。revision 保存正确 LaunchSession、从 1 连续的 sequence 和 `AUTO_INTERVAL/MANUAL_EXPORT/EXIT`，并发 callback 被串行/合并到后续 sequence；相同重放返回原结果，改 event/bytes 与跳号返回稳定冲突。第一条按 base/上一项 CAS 连续提升；第二条以 `PERSISTENT_SAVE_CONFLICT` 拒绝且不创建 revision/不覆盖 current，页面保留 bytes、提供本地下载并不自动死循环。不同 CoreArtifact/VariantRevision 不串用；其他失败明确报告且不覆盖最后有效 Blob；实现没有不存在的 `EJS_onExit/EJS_onSaveUpdate`。
- 证据：事件顺序、前后持久 Blob hash/revision、IDBFS 负向结果、网络请求和故障注入结果。

### ACC-PLAY-001：有效游玩时长

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-PLAY-001`。
- 流程：使用 fake clock 先驱动一次 config 后/start 前超过 2 分钟的加载和 pre-start finish，再用新 Launch 驱动 start、两次 heartbeat、页面隐藏、暂停、失联和重复 finish；另提交越界 `clientObservedAtMs`。
- 通过标准：加载阶段没有 PlaySession/idle 误过期，pre-start finish 撤销且不创建游玩记录；真实 start 后才启用 2 分钟 idle。三个事件端点都位于 `/runtime/launches/{launchId}/` 且校验 launch cookie，只有公开 launchId 没有 cookie 时为 401。只累计实际运行区间；隐藏/暂停/超出失联上限不累计；heartbeat/finish 幂等、跳号冲突，client time 只审计且越界拒绝；数据库全为整数毫秒，首页/详情汇总一致。
- 证据：事件时间线、期望/实际 duration 和 API 汇总。

## 12. 八个核心的真实运行画面

### 12.1 每个核心的统一执行流程

每个 `ACC-CORE-*` 都是独立 Case，不允许把八核合成一个可能超时的长 Case。执行前先运行：

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

任一核心失败只重跑该 Case 进行诊断；共享 loader/runtime 变化时仍逐个运行八个 Case，不使用一个无界全量 Case 代替。

## 13. UI、4K 与无障碍

### ACC-UI-001：用户导航与无登录访问

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-UI-001`。
- 流程：从全新浏览器 context 访问首页、游戏库、我的存档和管理后台；从游戏卡片进入详情。
- 通过标准：无需登录；用户侧仅三个主菜单，管理后台固定底部；游戏详情不出现在侧栏且保持游戏库上下文；无移动端验收分支。
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
- 流程：检查首页时长/最近游戏；在游戏库搜索并按平台/目录筛选；从卡片进入详情；查看封面、元信息、时长、存档、核心和 DOS 程序；从存档次要入口进入详情。
- 通过标准：筛选进入 URL 且刷新可恢复；卡片只显示已发布游戏；详情信息完整，默认核心状态准确；存档主操作直接启动、标题/次要操作才进详情。
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
- 流程：在 `1280×800`、`2560×1440` CSS viewport，以及 3840×2160、100% scale viewport 分别打开首页、游戏库、详情、存档和 Player Shell。
- 通过标准：无页面级横向溢出、遮挡、过小控件或跨屏长文本；三个 viewport 的游戏库分别为 4/6/8 列，内容分别不超过文档最大宽度。Player stage 为无边距的 100vw×100dvh；运行后 56px toolbar 自动移出画面且鼠标移动/键盘聚焦可恢复。canvas rect 完全在 viewport 内，CSS/drawing-buffer 宽高比误差 ≤0.01，宽或高至少一边与 viewport 对应边误差 ≤2px，另一边按 contain 公式居中，未被裁切或拉伸。
- 证据：三个 viewport 的布局测量、overflow 断言和页面截图。

### ACC-UI-006：管理侧 4K

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-UI-006`。
- 流程：在 `1280×800`、`2560×1440` 与 `3840×2160` 三个 viewport 打开入库总览、新建导入、任务、待审核、历史、游戏管理列表与详情、平台目录、`/admin/bios` 和 `/admin/bios/dats`。
- 通过标准：表格/卡片密度可读，筛选和主操作可达；子菜单缩进清晰；2560/3840 下历史 diff、任务阶段、BIOS hash 和 DAT 版本不被截断或横向藏在视口外。1280 下没有页面级横向溢出；确需横向滚动的宽表只在带可见提示的局部容器中滚动，行首标识与行末主操作 sticky、键盘可达。游戏管理详情的发布信息/媒体/运行版本/管理操作四区在三个 viewport 均可达。
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
- 流程：创建两个 ImportJob，其中一个含 60 个 REVIEW_PENDING Item、另一个含 3 个；从任务页进入前者的待审核，加载第二页后选择第 57 项，再切换到第 3 项并返回。保存第 3 项草稿，选择第 58 项后用浏览器前进/后退；最后 Approve 第 3 项并 Discard 第 58 项。分别在 1280×800 和 3840×2160 执行，并用键盘完成一次筛选和非顺序选中。
- 通过标准：任务入口带精确 `importJobId`，队列只显示该批 60 项且可清除筛选查看 63 项；每行可辨认来源、草稿标题、批次、目录、Validation/Blocker、候选数量和更新时间，cursor 分页无重复/漏项。3840 下队列与详情同时可见，1280 下列表/详情路由明确分离；选择任意项都会更新 `/admin/reviews/:itemId` 并保留筛选、已加载页和滚动位置，前进/后退可恢复。未保存草稿切换会阻止并提示，已保存草稿不丢失；决策后只移除对应行并聚焦相邻项。页面没有批量 Approve/Discard，所有最终决策各自使用当前 ETag/Idempotency-Key/ReviewEvent，两个批次不会串项。
- 证据：API query/cursor、route 序列、键盘 trace、决策前后队列 DOM 及两个 viewport 的当前截图。

## 14. 缺陷处理与重验

任一 Case 出现非预期行为即登记 defect，不能在原结果上直接改成 PASS：

1. 保存首次失败的 `result.json`、日志、trace 和截图；
2. 在最近可靠层新增回归测试，并证明旧实现失败；
3. 实施修复后运行聚焦回归测试；
4. 重跑原 Case；
5. 重跑受影响类别，最后重跑 `ACC-QA-001`；
6. 在 `defects.json` 记录 root cause、测试路径/名称、修复 commit 和两次 Case result。

若错误只能在真实 EmulatorJS/Chrome 中出现，仍必须在最近确定性边界加自动化测试，并收紧对应 `ACC-CORE-*` 或 UI runner 断言。不得用“只能人工复现”免除固化。

## 15. 最终通过标准

一期项目只有同时满足以下条件才可标记 `PASS`：

- 第 5–13 节所有 Required Case 为 PASS；
- 条件 Case 要么 PASS，要么有可核实的 `NOT_APPLICABLE` 原因；
- 没有 `FAIL`、`BLOCKED`、超时、缺失 Case 或未经解释的重跑；
- 本次生成的八核机器结果与画面复核全部通过；
- 本次发现的每个 bug 均有回归测试和 red/green 证据；
- `make ci` 和两个镜像 build target 通过，且镜像构建没有启动服务；
- 最终报告记录 commit/dirty 状态、环境、Case 结果、缺陷、未执行项和残余风险；
- 报告不包含 ROM/BIOS、游戏截图内容以外的专有二进制、TLS 私钥、launch capability/cookie 或完整宿主路径；非秘密 launchId 只能用于关联 Case。

AI Agent 的最终交付摘要必须列出：总结果、失败/阻塞 Case ID、实际执行命令、证据目录、本次新增回归测试，以及任何 `NOT_APPLICABLE` 原因。不得仅回复“验收通过”。

## 16. 专题覆盖映射

| 专题 | 统一 Case |
| --- | --- |
| 工程质量与回归 | `ACC-QA-001`–`003` |
| 镜像、本地开发、NG/TLS | `ACC-PKG-001`–`003`、`ACC-DEV-001`、`ACC-NET-001`–`002`（`002` 为部署条件 Case） |
| SQLite、CAS、备份、安全、API、运维 | `ACC-DB-001`–`002`、`ACC-CAS-001`–`002`、`ACC-BKP-001`、`ACC-SEC-001`–`004`、`ACC-API-001`、`ACC-OPS-001` |
| 平台目录 | `ACC-PLAT-001`–`005` |
| 游戏管理 | `ACC-GAME-001`–`003` |
| 导入、Hasheous、审核、任务恢复 | `ACC-IMP-001`–`008` |
| BIOS 与 Arcade DAT | `ACC-DAT-001`–`006`、`ACC-BIOS-001`–`002` |
| 启动、存档与游玩时长 | `ACC-RUN-001`–`005`、`ACC-SAVE-001`–`003`、`ACC-PLAY-001` |
| EmulatorJS 八核心 | `ACC-CORE-001`–`008` |
| UI、4K、待审队列与无障碍 | `ACC-UI-001`–`008` |

本文列出的范围不包含 soak、压力或性能基准；未来若需要性能专项，应另建不阻塞一期功能验收的测试计划，不能把长时间运行 Case 混入本文。
