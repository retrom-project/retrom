# Retrom 一期项目验收规范

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期唯一验收基线 |
| 版本 | 2.1 |
| 日期 | 2026-08-25 |
| 执行者 | AI Agent，必要时由人工复核当前运行生成的画面证据 |
| 范围 | 工程质量、镜像、本地开发、账户认证与隔离、游戏目录、普通/Pegasus/EmulationStation/RPG Maker 导入审核、BIOS/DAT/RPG 资源包、存储、安全、EmulatorJS/RetromRpgRuntime、联机、35 个 EmulatorJS 核与 7 个 RPG Maker 版本核、PSP ISO/CSO、320px 起的响应式 UI 和 4K UI |

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
- 由仓库锁定 Playwright 物化的官方 Chrome for Testing；只验收 Chrome，不承诺其他浏览器，手机/平板使用 Chrome 的固定 CSS viewport 和 coarse-pointer 仿真，并在可用时补充真实移动 Chrome 复核；
- 构建镜像 Case 需要 Docker daemon，但不授权启动容器；
- 可选的已部署代理 Case 需要一个已由 NG 暴露的 HTTPS 地址，通过 `RETROM_ACCEPTANCE_BASE_URL` 提供；没有部署环境时只有明确标注的条件 Case 可为 `NOT_APPLICABLE`；
- `ACC-RPG-008` 需要维护者依法持有、自包含且不含付费第三方素材/插件的 RPG Maker MZ Web Browser deployment，通过 `RPG_MZ_SMOKE_ROOT` 指定；它是声明 MZ 完成的 Required 外部前置，缺失时该 Case 为 `BLOCKED` 且整体不得声明全世代支持，不得由 Agent 下载或提交商业内容。
- `ACC-ONS-001` 需要操作者依法持有的 ONS 项目归档，通过 `RETROM_ONS_SMOKE_ARCHIVE` 指定；归档只进入当次隔离数据根，不提交、镜像或写入结构化证据，缺失时该 Case 为 `BLOCKED`。
- `ACC-KIRIKIRI-001` 需要操作者依法持有且使用 KAG 书签 API 的 KiriKiri2 项目归档，通过 `RETROM_KIRIKIRI_SMOKE_ARCHIVE` 指定；归档只进入当次隔离数据根，不提交、镜像或写入结构化证据，缺失时该 Case 为 `BLOCKED`。普通自定义 TJS 项目不因能启动而自动获得存档兼容声明。
- 自动化验收不得读取或下载操作者私有 ROM/BIOS。`testdata/public-roms/gba-smoke/`、`testdata/public-roms/nes-smoke/`、`testdata/public-roms/snes-smoke/`、`testdata/public-roms/arcade-smoke/` 与 `testdata/public-roms/rpgmaker-smoke/` 中的可提交测试程序均由 Retrom 自有源码确定性生成或使用清单锁定的 MIT MV CoreScript、随仓库保留许可且不包含第三方游戏、BIOS、RTP 或商业 runtime bytes。MZ 官方样例始终位于 ignored 操作者目录，只通过转换 provenance 进入 `ACC-RPG-008`。

首次准备依赖可以执行：

```bash
make install-deps
```

日常项目初始化直接执行 `make install-deps`；单独的 `make prepare-deps` 只物化应用 runtime/DAT/许可。初始化会把固定 Chrome for Testing 写入被忽略的 `.cache/tools/`，通过 `make public-fixtures-check` 校验仓库自有公开 GBA、NES、SNES 与 Arcade fixture 和生成源逐字节一致，并在正式计时前完成实际启动校验。依赖下载是验收前准备，不计入单 Case 时长；正式计时期间只运行离线 `make deps-check`。仓库不提供第三方 ROM/BIOS 下载器；没有合法公开 fixture 的核心登记为未覆盖，也不在 manifest 中保存主机名、远端绝对路径或凭据。

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
| 公开 mGBA ROM | `testdata/public-roms/gba-smoke/gba-smoke.gba`、`pegasus-smoke.gba` 与 `emulationstation-smoke.gba`；项目自有源码生成、MIT 许可且具有三个不同内容身份，生成器与普通上传/Pegasus/EmulationStation 实际消费者分别锁定 size/SHA-256/bytes；普通上传与 EmulationStation 使用两组内容身份不同的项目自有封面/视频，后者配套最小严格 `gamelist.xml` |
| 公开 FCEUmm NES ROM | `testdata/public-roms/nes-smoke/nes-smoke.nes`；项目自有 MIT iNES 1.0 NROM 程序，生成器与真实上传/导入/审核/发布/双浏览器消费者锁定 size/SHA-256/bytes，程序读取 P1/P2 控制器并更新可见画面；不需要 BIOS |
| 公开 Nestopia NES ROM | `testdata/public-roms/nes-smoke/nestopia-smoke.nes`；与 FCEUmm fixture 由同一项目自有生成器产出，执行与图形 bytes 相同，只用未执行 marker 形成独立内容身份，使两个游戏分别通过产品去重、导入与审核链路；消费者独立锁定 size/SHA-256/bytes；不需要 BIOS |
| 公开 SNES9x ROM | `testdata/public-roms/snes-smoke/snes-smoke.sfc`；项目自有 MIT 32 KiB LoROM，显式初始化 PPU/WRAM/自动 joypad，生成器与真实上传/审核/Launch/单浏览器/双浏览器消费者锁定 header/checksum/size/SHA-256/bytes；不需要 BIOS、SRAM 或私有 SDK |
| 公开 MAME 2003 split set | `testdata/public-roms/arcade-smoke/`；项目自有 Z80 程序、生成资源、小型 DAT 与测试 BIOS，MIT 许可；生成器与消费者锁定 Child/Parent/BIOS archive、entry、size、CRC32、SHA-1 和 SHA-256；DAT 只由 acceptance-only 装置登记为 test-only BUILTIN，不经 HTTP 上传 |
| 公开 MAME 2003 Plus split set | `testdata/public-roms/arcade-smoke/mame2003_plus/`；复用相同 driver-visible 项目自有成员，固定 ZIP comment 形成独立内容身份；独立小型 DAT 与消费者锁定 Child/Parent/BIOS 及完整 bytes |
| 公开 FBNeo split set | `testdata/public-roms/arcade-smoke/fbneo/`；项目自有 Z80 程序、生成图形/PROM、Logiqx DAT 与测试 BIOS，MIT 许可；driver CRC32 由生成器对其控制的 4 bytes 做确定性校正，完整 bytes 由 SHA-1/SHA-256 锁定；DAT 只由 acceptance-only 装置登记为 test-only BUILTIN，不经 HTTP 上传 |
| 公开 FBA2012 CPS1 set | `testdata/public-roms/arcade-smoke/fbalpha2012_cps1/1941.zip`；按锁定 `1941` driver layout 生成的项目自有 68000/Z80/图形/静音 payload，小型 DAT 锁定完整 entry/CRC32/SHA-1/SHA-256；无 Parent/BIOS |
| 公开 FBA2012 CPS2 set | `testdata/public-roms/arcade-smoke/fbalpha2012_cps2/`；项目自有 Phoenix `spf2xjd.zip` 与 marker-only `spf2t.zip` 父归档，小型 DAT 保留锁定 loader 实际要求的 clone/romof；Parent marker 不含第三方 ROM 且不被 driver 执行 |
| 公开 RPG Maker smoke | `testdata/public-roms/rpgmaker-smoke/`；2000/2003 由项目自有 LCF writer 从固定 JSON 生成，XP/VX/VX Ace 由项目自有 Ruby source 经确定性 Marshal/zlib 生成，MV 只使用锁定 `rpgtkoolmv/corescript` commit `182e31449707ba7e406db0485c44c2a9d11e2dcd` 的 MIT 输入与项目自有最小地图/资源；每个世代都有独立可见 marker、可移动状态和保存恢复变量。同目录的 MV/MZ malicious shape harness 只验证隔离边界，不充当 MZ 真实引擎证据。所有输入必须有许可、唯一生成源、固定 bytes 和真实 Retrom 产品消费者，不得包含厂商 RTP、商业引擎、官方原生 executable 或来源不明素材 |
| 联机 | `test` 与 `alice` 分别作为 P1/P2；八个精确 profile 的 EmulatorJS/core artifact/adapter 边界来自 `data/netplay/v2/manifest.json`；FCEUmm 使用 prediction/rollback，其余七个使用严格 lockstep，经真实导入/Launch/内容端点与两个 Chrome Player 验证，不建立逐 ROM 产品 allowlist |
| 游戏替换 revision | 基于测试内生成的确定性 ZIP 重打包：entry 字节不变，ZIP 时间固定且 comment 为 `retrom-acceptance-revision-2`；原始 Blob SHA-256 必须变化，提取内容 hash 必须不变 |
| 媒体 | Hasheous stub 提供一张固定字节的小型合法 PNG，SHA-256 写入 seed manifest |
| Production 内置 Arcade DAT | `make prepare-deps` 按 manifest 物化并逐字节校验真实 MAME2003、MAME2003 Plus、FBNeo 与 FBA2012 CPS1/CPS2 DAT，供 `ACC-DAT-001/002/004`；产品 ROM smoke 使用上列项目自有 test-only BUILTIN，不上传替代 DAT，也不把两类证据混为一谈 |
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

`result.json` 至少记录 `caseId`、`status`、`startedAtMs`、`finishedAtMs`、`durationMs`、实际命令、Git commit/dirty 状态、断言和证据相对路径。Git dirty 状态必须同时保存仓库相对路径的完整 `status/path` 摘要、条目数及该规范 JSON 的 SHA-256；不得只保存一个无法审计差异面的布尔值。报告不得记录 ROM/BIOS bytes 或宿主绝对路径。所有时刻使用 Unix 毫秒整数。

状态只有：

- `PASS`：本 Case 全部步骤与标准满足；
- `FAIL`：行为、命令、视觉或证据任一不满足；
- `BLOCKED`：缺少明确外部前置条件，例如条件 Case 所需的 NG 验收地址；
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
| 单个 RPG Maker 世代产品链 | 300 秒 |
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
5. `ACC-PLAT-*`、`ACC-GAME-*`、`ACC-IMP-*`、`ACC-DAT-*`、`ACC-BIOS-*`、`ACC-PEG-*`、`ACC-ES-*`、`ACC-MEDIA-*`：管理与入库；
6. `ACC-RUN-*`、`ACC-SAVE-*`、`ACC-PLAY-*`：产品主路径；
7. `ACC-MDISC-*`：多盘导入、协议、adapter、回归与隔离；
8. `ACC-NP-010`–`016`：联机协议、安全、单机回归、双浏览器核心与生命周期；
9. `ACC-UI-*`：信息架构、桌面/4K 和无障碍；
10. `ACC-MOB-*`：移动响应式、管理流程、方向门禁和横屏 Player；
11. `ACC-RPG-001`–`012`：七世代核心、项目导入、绑定、资源包、独立运行源、checkpoint、跨 Launch 精确恢复和恢复后输入；
12. 缺陷回归审计与最终报告。

除明确写明直接命令的 Case 外，执行命令统一为：

```bash
make acceptance-case CASE=<case-id>
```

## 5. 工程质量、镜像、本地开发与网络边界

### ACC-QA-001：完整代码质量门禁

- 上限：900 秒。
- 执行：`make ci`。
- 流程：从依赖锁文件安装工具；先运行全仓源码结构门禁，再验证 OpenAPI 3.0.3 能由固定 `oapi-codegen v2.8.0` 与 TypeScript 生成器处理；确认 Go 生成物由后端编译链按需生成、被 ignore 且不在 Git index，TypeScript 生成物无漂移；再依次执行 Go format/build/test/lint、Web lint/typecheck/test/production build、integration test 和提交数据基线校验。
- 通过标准：命令退出码为 0；全部手写新旧生产/测试文件与函数满足当前规模和复杂度阈值，结构性 suppression 与前端 inline disable/ignore 为零，非结构性 Go suppression 与中央清单双向一致；Go/ESLint 为零 warning；OpenAPI 两端生成结果有效、TS generated diff 为空且 Go generated 未被跟踪；没有跳过 required suite；报告列出实际固定工具版本。
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
- 执行：`make build-backend-image`，随后 `docker image inspect retrom:latest`；用 `docker image save` 到验收临时目录检查最终 image layer 文件清单，再以 UID/GID `1000:1000`、只读 rootfs、无网络的一次性容器读取并哈希全部内置依赖，容器不挂载数据卷、不启动 Retrom 服务。
- 流程：先记录 `make release-input-digest`，只构建根 Dockerfile；检查镜像名、镜像没有创建或声明固定运行用户、HTTP 入口、最终镜像配置/发布输入 label、`THIRD_PARTY_NOTICES`、两个 EmulatorJS manifest 的 38 个许可 component、单一 `retrom-runtime` tag 的 runtime allowlist/许可/notice/上游源码定位、五份 DAT 以及密码 blocklist/许可；从适用 manifest 在验收临时目录重建 notice 并逐字节比较。最后确认所有依赖目录对任意非 root UID 可读、可遍历且不可写，所有依赖文件可读且不可写。
- 通过标准：默认 target 为 `retrom:latest`，image config 的 `User` 为空且 `/etc/passwd` 不含 Retrom 专用账号，运行身份完全由部署编排决定；`io.retrom.release-input-sha256` 等于包含密码与 RPG manifest digest 的本次 helper 值；所有 runtime/DAT/license/blocklist artifact 命中本地 observed 或固定 manifest 校验。最终文件包含 36 个 EmulatorJS 跨版本 selected core/report 条目（合并为 35 个当前新绑定 artifact）、七条 RPG route/artifact 的当前 tag allowlist payload、aggregate notice/许可、PPSSPP assets、五份 DAT、10,000 行密码 blocklist及 MIT 许可，但不包含 RPG 历史版本目录、RTP、用户 MV/MZ runtime/项目、下载 archive、上游源码树、非 allowlist core、用户数据、缓存、TLS 私钥或开发启动命令；被忽略 payload 未被 Git 跟踪且构建不 push。以部署基线 UID/GID `1000:1000` 运行只读依赖校验时不得出现 `payload unavailable`。
- 证据：build log、image ID、RepoTags、User、Entrypoint/Cmd、最终 layer 文件/size/permission 清单、Git tracked-file size 检查、artifact 校验摘要和 UID `1000` 只读校验结果；一次性校验容器销毁后不留下容器、网络或 volume。

### ACC-PKG-002：前端镜像构建

- 上限：900 秒。
- 执行：`make acceptance-case CASE=ACC-PKG-002`。
- 流程：runner 记录 `make release-input-digest`，调用 `make build-web-image` 并 inspect `retrom-web:latest`；确认 target 在编译生产代码前执行 `data-check`，检查 manifest 的 `player_adapter` 与 `web/features/player/adapters/registry.json` 双向一致且每个登记项有实现，再检查 standalone production 产物、镜像没有创建或声明固定运行用户以及内部 HTTP 入口。最后在临时工作树副本把 manifest adapter ID 改为未知值，运行同一 `data-check` 并要求预期失败；不在主工作树留修改，也不对负向样本再构建镜像。
- 通过标准：默认目标 tag 为 `retrom-web:latest`，image config 的 `User` 为空且 `/etc/passwd` 不含 Retrom 专用账号，运行身份完全由部署编排决定；`io.retrom.release-input-sha256` 等于本次 helper 值；普通基线 `ejs-4.2.3-v3` 精确映射版本 `4.2.3`，runtime base/loader 路径命中 manifest allowlist，未知或无实现登记项使临时副本的校验失败；镜像没有开发依赖/缓存、内置后端地址、TLS 私钥或用户数据，Cmd 不是 `next dev`。
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
- 通过标准：test 空库只创建一个 `test` ADMIN/Profile，登录页有测试警告，认证后的 rewrite 同源成功；联机 upgrade 到达 Go 并返回 `401 AUTHENTICATION_REQUIRED`，HMR 仍为 101；标准开发配置文件、数据根与启动状态分别固定到被忽略的 `.dev-data/dev.mk`、`.dev-data/data` 和 `.dev-data/dev-state`，隔离 Case 通过命令行覆盖为临时目录；仓库 `.dev-data/bios` 与 `.dev-data/roms` 已幂等创建，API 分别只投影 `local-bios`/“本地 BIOS”和 `local-roms`/“本地 ROM”两个状态为 `AVAILABLE` 的 root，且不暴露绝对路径；其余进程接管、监听、Docker 哨兵、身份保护和退出约束全部满足。默认 release 不创建测试账号，由 `ACC-AUTH-002` 独立证明。
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

### ACC-DB-002：干净迁移链与 lineage 保护

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-DB-002`。
- 流程：使用全新数据根执行当前 001→010，再次启动验证幂等；分别从当前迁移链的合法前缀恢复执行，并构造名称或 checksum 不匹配、版本缺口、未知/未来版本的 lineage。对单个 migration 注入确定性失败，确认该步 schema 与 migration 记录同事务回滚；另验证当前备份往返，以及旧 manifest schema 或旧 lineage 恢复拒绝。
- 通过标准：全新库到 010 后 `foreign_key_check` 与 `integrity_check` 通过，重复启动不重复变更；Platform/Core 参考行完整、PlatformInstance 为零。已应用记录必须是当前链的精确有序前缀，任一名称/checksum/缺口/未知/未来差异都在业务写入前以 `DATABASE_REBUILD_REQUIRED` 拒绝且不改库；备份只允许与当前完整 lineage 精确一致的数据库恢复。
- 证据：当前 migration 名称/checksum、各实际起始/最终 schema 摘要、行数/hash、原子失败前后 schema、二次启动结果、lineage 负向矩阵与备份恢复结果。

### ACC-CAS-001：SHA-256 去重与原子写入

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-CAS-001`。
- 流程：对同一小型合法 fixture 进行两次顺序和两次并发上传，并故障注入一次中途写失败。
- 通过标准：物理 Blob 只有一个，逻辑引用数量正确；路径由 SHA-256 推导；失败写入不留下可见半文件或数据库孤儿引用。
- 证据：Blob/引用查询、CAS 路径和临时目录清单。

### ACC-CAS-002：引用保护与单轮 GC

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-CAS-002`。
- 流程：按生产所有权 registry 构造 durable、workflow、runtime、共享和无引用 Blob，并让本地上传 Import、Pegasus Import、EmulationStation Import 与游戏永久删除分别进入终态；另建两份小型 archive 并物化各自一个 entry，一份仍有业务根，一份已释放。运行 PayloadRelease、在宽限期内运行一轮 GC，重启 worker 后以 fake clock 推进超过配置宽限期再运行，并在删除前故障注入一个并发新引用。输入不超过 16 个 Blob。
- 通过标准：registry 的每条生产保护边都有测试，Content/Variant revision、非终态工作流、未到期软删除 SaveState 和有业务根的 archive 闭包均受保护；ReviewEvent v2 与已终态 Import/Pegasus/EmulationStation 不构成保护根。PayloadRelease 幂等移除 workflow 边并登记候选；GC 每批不超过 200，宽限期必须处于 24 小时至 30 天，`blob_gc_candidates` 自身不阻止回收；无根 archive 及 entry 成组清理，共享 Blob 直到最后一个所有者释放后才可候选。重启可恢复 RUNNING 的释放/GC Job，删除前新增引用会重新保护目标且不会误删。
- 证据：三轮前后按 registry 边分类的引用闭包、PayloadRelease/GC Job 与事件、ArchiveEntry/文件清单、fake clock 推进值和 GC 决策日志。

### ACC-STOR-001：已登记 CAS 容量分析

- 上限：240 秒。
- 执行：`make acceptance-case CASE=ACC-STOR-001`。
- 流程：在隔离空库通过标准导入流写入项目自有 fixture，再加入 durable、非终态 workflow、终态待释放 workflow、runtime、跨长期用途共享、受保护 archive/member、无业务根 archive/member、软删除存档和 GC 候选的确定性小型组合；执行 PayloadRelease 前后分别调用容量 API，再从 ADMIN 打开 `/admin/storage`，确认一次立即清理并等待 worker 收口。以同一幂等 key 重放，再以缺 key、USER/匿名和未知 query 重试，并覆盖既有 viewport、刷新、失败刷新和确认框矩阵。
- 通过标准：API 只使用 `REGISTERED_CAS_PAYLOAD_V1`，带 `private, no-store`，byte 为无符号十进制字符串；九类按固定顺序含零值，分类 byte/count 之和等于顶层，`protectedBytes + unreferencedBytes = registeredBytes`，同大小不同 Blob 分别计数。保护集合与 GC 使用同一 registry；终态释放前 payload 仍计 workflow，释放后只在没有其他边时进入未引用，独占/共享字节和游戏删除影响摘要逐 Blob 去重且完全一致。封面替换/视频移除后旧 Asset URL 立即 404；ROM/多盘或同 Requirement BIOS 的成功替换同时清理旧运行/存档与旧 durable 边；各自失去最后引用的 Blob 从原分类转入 UNREFERENCED/候选，正常情况下 registered 总量只在宽限期后下降；ADMIN 确认立即清理后，POST 只跳过保留期并返回已调度量，worker 仍逐 Blob 复核保护集合，真正无引用数据收口后 registered/unreferenced/candidate 同步下降，恢复引用的数据不删除。相同 key 只产生一条 `STORAGE_CLEANUP_REQUESTED` 审计并重放原响应，缺 key/CSRF、USER/匿名均失败。完全相同 ROM、多盘或失败替换不得释放 current；不同 Requirement/CoreArtifact 的 BIOS 继续受保护。受保护 archive 的用途单向传播到 member，无业务根 archive 不反向保护；一个长期用途压过 workflow/runtime，两个长期用途归共享。存档状态/截图和清理候选是去重引用视图，不与分类相加；溢出、registry 新增/删除保护边未同步容量语义、读库失败都 fail closed。其余鉴权、脱敏、交互、响应式和无障碍标准不变。
- 证据：API JSON 与直接 `SUM(blobs.size_bytes)`/行数对比、registry/分类单元与 SQLite 组合测试输出、鉴权/脱敏矩阵、viewport DOM/axe 断言和当前截图。

### ACC-BKP-001：备份与空目录恢复

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-BKP-001`。
- 流程：在验收库写入三个 User/Profile、游戏、Blob、当前 release DatVersion、私有存档、一条未完成 UploadPart、ACTIVE AuthSession/AccountLink/LaunchSession，以及各一条 RUNNING 的 BIOS、Pegasus 与 EmulationStation server-import Job/aggregate；另建 GC 宽限期 Blob、crash orphan 和受保护 archive。保持服务运行调用一次 `retrom backup` 验证拒绝，再正常停止服务，备份到不存在的临时输出路径。完成既有 manifest/依赖/lineage 负向矩阵后恢复到第二个不存在的数据根，启动恢复服务，分别用旧认证 cookie、账号链接与 launch capability 访问，再用原密码重新登录并核对三个 Profile 的私有数据。
- 通过标准：既有 bundle 结构、mode/hash、CAS/registry、依赖和负向恢复约束全部满足；外部 source bytes/root/XML/metadata 不进入 bundle。User/Profile/credential 与私有数据的非围栏行数和摘要一致。restore 在开放 HTTP 前用单事务撤销全部非终态 AuthSession、未使用 AccountLink 和非终态 LaunchSession，并把三类外部 source Job/aggregate 置为不可重试 `FAILED/SERVER_IMPORT_SOURCE_NOT_RESTORED`，写一条不含 ID/secret 的 `RESTORE_SECURITY_FENCE` 审计；旧 cookie/link/capability 全部失败，启用用户可用原密码重新登录并只能看到自己的原数据。清单/日志不含密码 hash、session/link/capability/key 明文、BIOS 内容或完整宿主路径。
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
- 流程：先用固定生成器/validator 加载 OpenAPI 3.0.3；覆盖正常读取、未知/重复 JSON 字段、无效 UTF-8、多个顶层 JSON/depth 65、未知/重复 query、未授权、不可见资源、不存在、缺少/错误 If-Match、上传过大、可修复业务校验、限流、Idempotency-Key 的同语义 JSON 不同 key 顺序、异 body/path/If-Match、两个并发相同请求及当前 streaming 摘要，并单独验证 UploadPart 的 path/range/digest 永久幂等，以及 cursor 正常翻页、篡改、过期、超长和错 route/filter 复用；对待审队列另以两个 ImportJob 的交错 Item 验证 `importJobId` 精确筛选、cursor 绑定、封闭摘要字段和宿主路径脱敏。并发发送普通 JSON 与三种当前流式请求，确认 validator chain 选择互不污染；再让代表性成功/错误 response 通过 schema contract test。对 Import SSE，先在事务内注入两个 scope 交错的事件，无 `Last-Event-ID` 连接后记录 snapshot ID，再注入新事件并以该 ID 重连；另对一个通用 Job 注入与其他 Job 交错的事件，重复无 cursor snapshot、合法跨 Job 水位、重连和非法/超前水位矩阵。覆盖 Launch 的四个合法 `returnTo` 和 origin/query/fragment/percent-encoding/不同 game ID 负向值，以及 NEEDS_VALIDATION 的 202/no-cookie、旧 key 稳定重放 202 与 Job 完成后新 key 返回 201/cookie。最后让诊断摘要与其他代表性响应通过 OpenAPI schema 校验。
- 通过标准：固定 JSON object 全部禁止未知 property，lexical guard 在生成 binder 前拒绝重复 key/无效 UTF-8/尾随值，query guard 拒绝未声明名和标量多值；OpenAPI 恰有 `putAdminUploadPart`/`postRuntimeSaveState`/`postRuntimeReviewScreenshot` 三个 `x-retrom-streaming-body=true`，它们生成 reader 而非 `[]byte`/`ParseMultipartForm`；启动时构建普通/流式两条不可变 validator chain，前置 router 按 extension 分派，只有流式链设置 `Options.Options.ExcludeRequestBody=true`，且不跳过 path/query/header。普通 JSON 与流式请求并发时不能使对方误跳过或误读取 body，不得动态修改共享 options、维护 URL skip 清单或使用全局 `Skipper`。错误 envelope 固定为 `error.code/message/details/requestId`；状态码按契约覆盖 400/401/403/404/409/413/416/422/428/429/503；ID 是 UUIDv7 字符串或稳定 seed code、时刻是 int64。语义相同请求返回原 status/body/白名单 header，并发只产生一个领域结果；body/path/precondition/stream digest 任一语义变化均冲突，记录与事务同成败且无敏感 header。Launch validation pending 符合 202 schema、不设 capability cookie/不建 LaunchSession，并发请求复用 Job；完成后只有新 key 的新请求产生 201/cookie。cursor 严格按契约签名/限长/24 小时过期，分页无重复漏项且不能跨 route/filter 复用，payload 不含 secret/宿主路径；Import 与通用 Job SSE 的首帧 snapshot ID 都等于同一快照事务看到的全局最大 JobEvent ID，后续/重连只发送更大且属于目标 aggregate/job 的持久事件，不丢失、不混 scope、不因断开取消 Job；其他 scope/job 的合法 ID 可作为全局水位，非法/超前值返回 `400 INVALID_EVENT_CURSOR`，15 秒 heartbeat 是无 ID comment。`returnTo` 只接受精确白名单。诊断与抽样 response、两条 health response 符合相同 OpenAPI schema。
- 证据：生成/validator 版本、负向请求矩阵和请求/响应 contract snapshot；动态 request ID 需规范化后比较。

### ACC-OPS-001：健康检查、快速失败与诊断脱敏

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-OPS-001`。
- 流程：用明确临时数据根和完整依赖启动服务并检查 live/ready、进程数及前端写入；另在独立空库中用可控 DatParser gate 阻塞 bootstrap parse，检查 health 与业务路由，再释放 gate；在第三个空库注入确定性 parse failure。该 test double 只验证状态编排，真实 bytes/解析统计由 `ACC-DAT-001` 验证。随后分别使用不可写/越界数据目录、未来 schema、缺失 manifest 和 hash 不匹配 payload 启动；再分别注入拼错的 `RETROM_DATA_DI`、已移除的 `RETROM_EXAMPLE_ROOT`，以及具有固定假值的 `RETROM_ACCEPTANCE_BASE_URL/RETROM_EJS_DEP_ZIP_V1`；以 fake reader/clock 触发同步 `RETROM_STARTUP_CHECK_TIMEOUT`；触发一次带 capability 的启动失败，再调用 `GET /api/v1/admin/diagnostics` 导出诊断摘要并按封闭 schema、header 和敏感模式扫描。
- 通过标准：后端只有一个 Go 进程并只写配置的数据根，Next.js 不保存业务状态。阻塞时 live=200、ready=`503 DEPENDENCY_INDEXING`，任一非 health 路由（包括 diagnostics）在读取 body/写状态前返回标准 envelope `503 SERVICE_NOT_READY`；释放后 DatVersion 先 READY/active 再 ready=200。确定性失败保持 live=200、ready=`503 DEPENDENCY_DAT_PARSE_FAILED`，重启不清空失败证据或误激活。多个动态故障按 `DATABASE_UNAVAILABLE→CAS_UNAVAILABLE→DEPENDENCY_INVALID→DEPENDENCY_DAT_PARSE_FAILED→DEPENDENCY_INDEXING` 选择首个 reason。可静态发现的坏配置在 10 秒内非零退出且从未开放 HTTP，并给出稳定可操作错误；未知或已移除的非工具变量以 `CONFIG_UNKNOWN_VARIABLE` 失败；三类已声明工具前缀可继承但不改变服务配置且值不进日志。慢同步校验在配置的 60 秒 fake deadline 退出，后台 DAT_PARSE 不受这 60 秒误杀；启动不联网下载或 fallback。ready 后诊断响应为 schemaVersion 1 的严格 JSON、字段/计数/版本排序与数据库快照一致，带 `private, no-store`、固定 attachment filename 和 `nosniff`，且不创建 Blob/归档。结构化日志按契约关联 `requestId`、非秘密 `launchId` 和必要的类型化资源 ID，但没有内容 hash、capability/cookie/key、ROM/BIOS bytes、完整宿主路径、工具变量值或上游敏感响应；诊断摘要在此基础上还不得包含任何资源 ID。
- 证据：健康响应、退出码、耗时和脱敏扫描结果。

## 7. 账户认证、用户管理与私有数据隔离

### ACC-AUTH-001：release 空实例安全初始化

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-AUTH-001`。
- 流程：以 release 和全新数据根启动到 PENDING，读取匿名 auth context；运行主机只读 `retrom setup-code`，分别提交错误 code、两个并发正确 initialize 和初始化后的重复请求；扫描命令/API/日志与数据库。
- 通过标准：启动时为 `PENDING` 且无 User/Profile；错误 code 零写入；并发最多一个 `201`，同事务只创建一名 `ADMIN/ENABLED` User、Profile、Argon2id credential、AuthSession 与初始化审计，另一个和重复请求为稳定冲突。setup code 不进数据库/日志，初始化响应只在安全 cookie/封闭 DTO 中返回会话材料。
- 证据：context/HTTP 记录、User/Profile/credential 行数、审计摘要和敏感模式扫描。

### ACC-AUTH-002：release/test 模式隔离与 lineage 拒绝

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-AUTH-002`。
- 流程：用三个独立数据根分别默认 release、显式 `--mode=test` 和 release 含弱默认账号启动；并发启动两次 test 空库，再尝试 `test/test` 登录；另以名称/checksum 不匹配的 migration lineage 启动。
- 通过标准：默认 release 不创建或接受 `test/test`；test 空库无论并发只创建一个 `test` ADMIN/Profile，密码仅存 Argon2id hash且页面/context 标记 test；release 遇测试默认凭据 fail-fast。lineage 不匹配以 `DATABASE_REBUILD_REQUIRED` 零写入拒绝。
- 证据：三个数据根的启动/登录结果、行数、文件 hash 和模式 UI 截图。

### ACC-AUTH-003：登录、会话、密码与请求防护

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-AUTH-003`。
- 流程：覆盖正确/错误/停用登录、logout、idle 8h、absolute 24h、并发改密、当前/其他 session rotation、Argon2 参数、10,000 项 blocklist、Origin/Fetch Metadata/CSRF 和 username+IP 双维限流；用 fake clock 和可信/不可信代理矩阵，不真实等待。
- 通过标准：登录错误通用且等时路径不泄露账号状态；所有包含密码、初始化码或一次性账号 capability 的 HTML form 都声明原生 `method=post`，即使用户在 React hydration 完成前提交也不得把凭据编码进 URL、查询参数、浏览器历史或代理 access log；hydration 完成后的请求仍使用契约规定的 JSON API。session cookie/CSRF/缓存属性符合契约，过期和撤销立即生效；改密要求当前密码并只保留轮换后的当前会话。release 密码最少 6 个 Unicode 字符，5 个字符稳定拒绝，长度边界与物化 blocklist 均 fail-closed；限流只信任 allowlist 代理并返回稳定 `429/Retry-After`。
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
- 流程：`test` 与 `alice` 对同一已发布游戏分别创建 PlaySession 和显式 SaveState，再读取 home、library/detail、recent、saves、launch config；在同一 Chrome profile 依次登录两个账号。
- 通过标准：两人看到相同公共游戏目录/元信息，只看到各自 Profile 的首页聚合、最近游玩、时长和显式存档；账户切换清理前一用户的查询缓存、平台图钉、DOS 偏好和内存状态，普通 Launch 不自动绑定或恢复任何存档。
- 证据：双账号固定 fixture、API 响应、浏览器存储 namespace 与切换前后 DOM。

### ACC-ISO-002：跨账号 ID、cursor 与幂等探测

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-ISO-002`。
- 流程：由另一账号直接使用 SaveState、截图、私有 Asset、Launch、cursor 和 Idempotency-Key；对相同 key/body 在两主体下并发提交，并尝试从别人的存档启动。
- 通过标准：跨账号资源统一按契约 404/401，不泄露存在、字段或 bytes；cursor 绑定 route/filter/principal，幂等记录按主体分区，同 key 不串响应；客户端提交 owner/Profile ID 无法扩大授权。
- 证据：每类交叉 ID 的状态/body/timing 摘要、数据库 owner 与幂等记录。

### ACC-ISO-003：停用、删除与 Player 残留隔离

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-ISO-003`。
- 流程：两个并发 Chrome context 让目标用户保持页面与 Player 活跃，管理员分别停用、重新启用和软删除；在每个边界继续请求 context、heartbeat 和显式状态保存。使用同一 Chrome profile 令 A 留下 EJS IDBFS bytes，再以 B 普通启动同一游戏。
- 通过标准：停用/删除立即阻止新认证请求并撤销未结束 Launch，Player 写入不新增数据；重新启用仅恢复原 Profile 私有数据并需重新登录，删除不可恢复。B 在 start 前清空整个 `/data/saves`，失败则阻断，绝不运行 A 的 bytes。
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

### ACC-PLAT-006：推荐目录代码 catalog 与幂等补齐

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-PLAT-006`。
- 流程：使用全新数据根建到 schema 040，确认零目录；ADMIN 读取推荐状态并第一次补齐。随后修改一个模板目录名称/核心、停用一个、软删除一个，再手动创建一个缺失 pair 的等价目录；以新 key 再次补齐，并并发提交两次相同缺失集合。另故障注入一次 AuditEvent 写入失败。
- 通过标准：catalog 固定返回 29 项且扩展名来自平台 profile；RPG Maker 恰有一个 `rpgmaker/rpgmaker` 虚拟核心推荐目录，不存在 FDS/MAME 2003 独立模板，NES 包含 `.fds`，Arcade `.zip` 不重复。第一次补齐在一个事务创建 29 项并逐项审计；模板、自定义、等价、停用/删除分别投影为 `ACTIVE/CUSTOMIZED/COVERED_BY_EQUIVALENT/SUPPRESSED`。后续补齐不覆盖、不恢复、不重排、不重复创建，新的缺失项只追加到末尾；同 key 精确重放，并发只有一组创建结果；故障使目录、审计和幂等记录全部回滚。手动目录 key 为 NULL，推荐目录 key 唯一。
- 证据：GET/POST 响应、Idempotency replay header、并发结果、目录/审计/幂等行和 catalog/contentprofile 对照。

## 9. 游戏管理

### ACC-GAME-001：元信息、媒体与重新刮削

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-GAME-001`。
- 流程：在游戏管理按关键字、基础平台和游戏目录找到固定游戏，并检查桌面筛选栏顺序与对齐；检查发布信息/媒体/内容与运行版本/管理操作四区；在未修改时检查保存按钮，记录当前 version，编辑标题、简介、年份与类型并保存，再次检查按钮和版本；替换固定 PNG；针对 current ContentRevision 触发 Hasheous stub 重新刮削，先不采用候选，再选择部分字段和 READY media 应用；构造一个旧 ContentRevision run 做负向 apply，最后用旧 ETag 提交一次并发编辑。
- 通过标准：搜索/筛选结果正确，桌面端“排序”紧跟“运行状态”且位于同一行；没有字段变化和保存成功后“保存新版本”都禁用，不会创建空修订；每次确认修改创建可追溯 MetadataRevision/current Asset，ADMIN_EDIT revision 的 source ref 为 NULL 且同事务 AuditEvent 指向新 revision，RESCRAPE_APPLY ref 精确指向被采用 Candidate；切换 current 后全部旧 GameAsset 叶子删除、旧 URL 404，未改媒体通过新 Asset 指向同一 Blob，失去最后引用的替换/移除媒体进入 GC 候选。游戏库、详情和管理页读取同一当前值。运行区分开显示当前/历史 ContentRevision、各 Core VariantRevision、CoreArtifact/DAT，而不暴露宿主路径/Blob 编辑；显式重新刮削绕过旧 cache，创建独立 MetadataScrapeRun/QueryAttempt/Candidate/Asset 且不自动覆盖，旧 content run 不能 apply；采用范围与字段 diff 一致；旧 ETag 写入以 409 拒绝；Game ID、current content 和游戏目录不变。
- 证据：查询参数、修改前后 API、revision/diff、Blob SHA-256、冲突响应及三处当前 UI 截图。

### ACC-GAME-002：重新上传与不可变文件 revision

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-GAME-002`。
- 流程：为当前 `fceumm` revision 创建一份带截图存档和活动 Launch；创建 COMPLETE UploadSession 后调用内容 revision endpoint。先分别提交 byte-identical ROM 和损坏输入，再用不同内容执行成功替换，并在另一次运行中于验证与提交之间改变目录默认 core/version，确认 retryable conflict。比较 Upload consumption、GameContentRevision/ContentFiles、各 Core VariantRevision、两个 current pointer、存档、Launch/Netplay 与 GC 候选。多盘分支先提交相同盘序/Disc hash，再用一盘不变一盘变化、最后多盘同时变化的完整目录重复验证。RPG Maker 分支先发布固定 RPG2000 项目，再依次提交同世代但不同 filesDigest 的完整目录、固定 RPG2003 目录和依赖声明变化的 RPG2000 目录。
- 通过标准：创建 Job 与 whole-session consumption 原子且同一 Upload 不能再被 Import/Asset 使用；相同单 ROM 或完全相同多盘以不可重试 `GAME_CONTENT_UNCHANGED` 结束并释放 consumption，不改变 current、不创建 revision、不删除存档。损坏输入和快照竞态同样不改变 current 或存档；仍可重试的失败保留输入引用。只有不同的新内容 READY 且最新快照一致时才原子创建新的 GameContentRevision 和默认 core VariantRevision、切换 Game content 与该 Variant current；旧 Content/Variant revision 行继续可审计，但旧 ContentFile/VariantFile、Launch payload 和绑定存档已删除，活动 Launch/Play 被撤销且 Netplay 以 `GAME_CONTENT_REPLACED` 结束，独占旧 Blob 进入 GC 候选，共享未变光盘仍由新 current 保护。RPG Maker 同世代替换保留内部 core/route/逻辑游戏兼容线和运行依赖，使用该线当前 selected artifact/adapter ABI 重新生成派生运行文件，且新 `runtime_validation_id` 为空；旧 artifact 已退役也不能阻断替换。RPG2000→RPG2003 以不可重试 `RPG_REPLACEMENT_GENERATION_MISMATCH` 拒绝，依赖声明变化以不可重试 `RPG_REPLACEMENT_DEPENDENCIES_CHANGED` 拒绝，两者均不得创建 revision、切 current 或删除存档。其他 core 对新内容显示 `NEEDS_VALIDATION`，新普通启动只使用新 revision。
- 证据：单 ROM与多盘上传/Job 结果、原始/派生 hash、Content/Variant revision 链、Upload/Blob/GC 引用、存档与运行终止状态、快照冲突事件和兼容诊断。

### ACC-GAME-003：永久删除、版本保护与墓碑关系

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-GAME-003`。
- 流程：分别用浏览器上传导入和 Pegasus 导入创建带 GameVariant/VariantRevision、媒体、存档、活动 Launch/Netplay 与审核历史的游戏，读取 `deleteImpact`；先使用旧 version/digest，再用当前 version 但错误标题，最后用当前 version、精确标题、精确 digest 和新 Idempotency-Key 永久删除。以同 key 重放，再用不同 key 重删；执行 PayloadRelease、宽限期内外各一轮 GC，并查询可执行列表与最近游玩、收藏、Netplay/Play/Launch、审核和审计关系投影。
- 通过标准：影响摘要精确覆盖内容、媒体、存档、运行时、Import/Pegasus 来源和独占/共享 Blob；影响变化导致 digest/version 409，错误标题返回 `422 GAME_DELETE_CONFIRMATION_MISMATCH` 且均无副作用。成功请求原子设置 `status=DELETED`、`payloadState=RELEASING`、递增 version、撤销活动运行/Netplay、写审计并调度 GAME PayloadRelease，返回 202；同 key稳定重放原响应，不同 key 在墓碑已存在时返回 200。释放完成后 `payloadState=RELEASED`，内容、媒体、存档和运行 payload 的保护边被移除，Game 及文字 Metadata/Content/Variant/Review/Audit/Play/Launch/Netplay/Favorite/Tag 行保留。可执行游戏库、搜索、推荐和启动过滤该游戏；最近游玩、收藏及历史投影返回 `{gameId,title,status:'DELETED',coverUrl:null,availability:'DELETED'}`，收藏只允许移除。共享 Blob 保留，独占 Blob 进入候选并仅在宽限期后由 GC 删除；删除和释放重试均幂等。
- 证据：两种来源的影响摘要/digest、四类 DELETE 响应与幂等记录、PayloadRelease/GC Job 和审计、运行撤销、各可执行/关系入口 DTO、容量前后对比及当前 UI 截图。

## 10. 导入、刮削与审核

### ACC-IMP-001：单文件导入与发布前隔离

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-001`。
- 流程：通过 upload manifest 选择 `acc-nes-fceumm` 和固定的确定性单文件测试夹具；按服务端 fileId 上传 8 MiB parts（该小文件为单尾块），带正确 Content-Range/Content-Digest，重放同 part，再使用当前 ETag/Idempotency-Key complete；断言 `202/FINALIZING` 后等待 UPLOAD_FINALIZE Job 到 SUCCEEDED/session COMPLETE，再创建 ImportJob 并观察至 `REVIEW_PENDING`。另以错误 digest、`../` relativePath 和缺失 part 做负向请求；对缺失 part 只重传服务端列出的 part，以新 key 再 complete。再创建两个小文件的 COMPLETE session，仅把一个文件消费为媒体，用 fake clock 推进 24 小时并运行一次 upload cleanup；另将一个无消费 COMPLETE session 推进 7 天。
- 通过标准：必须选择游戏目录且只接受浏览器相对路径；同 part/同 digest 幂等，异 digest/非法路径拒绝；每次接受 complete 都在短事务递增 `finalizationNo`、创建该编号唯一 Job 并转 FINALIZING，不在请求内组装大文件。Worker 从 bytes 重算 hash/CAS、按已完成文件可恢复并删除其临时 part，全部成功才 COMPLETE；同一轮 I/O retry 复用当前 Job，缺失/损坏 part 修复后的 complete 创建递增编号的新 Job，旧失败 Job/事件保持不变且已 COMPLETE 文件不重组装。只有 COMPLETE session 可创建 ImportJob。上传终结、HASHING/IDENTIFYING/SCRAPING 阶段可见；生成一个 ImportItem、规范 source manifest 和匹配目录默认核心的 READY ImportItemCoreValidation，但审核前不创建 Game/ContentRevision/VariantRevision，游戏库不可见。缺少 `Content-Length` 的合法流式 part 仍成功，越过声明 range/8 MiB 上限的 chunked body 在超限处拒绝且不留下 part/Blob 引用。file-level 消费只保留被消费文件，24 小时后同 session 未消费文件引用被裁剪；无消费 COMPLETE session 在 7 天后 EXPIRED；whole-session Import 证据不被裁剪。
- 证据：UploadSession/File/Part 状态、UPLOAD_FINALIZE/Import 任务事件、Blob hash、part/UploadFile 清理前后清单、fake clock、Item 和游戏库查询。

### ACC-IMP-002：目录分组与 GBA 确定性派生

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-002`。
- 流程：先用“选择目录”打开应用内 Dialog，覆盖目录句柄浏览、递归相对路径、摘要确认、系统选择取消、Dialog 取消/Escape、焦点恢复，以及目录句柄 API 不可用时的 `webkitdirectory` 回退；再将 `gba-smoke.zip` 上传到 Arcade 目录，令它完成安全扫描但以 `ARCADE_MACHINE_NOT_FOUND` 拒绝；以新 UploadSession 把相同 bytes 导入 GBA 目录，验证复用同一 Archive Blob/Entry 并将 Unicode `.gba` member 一次性物化。另导入固定 DOS 目录到审核；检查两份 READY ImportItemCoreValidation，再分别 approve 并读取发布实体。
- 通过标准：本地目录的产品确认只在 Retrom Dialog 内完成，Chrome / Edge 的目录句柄路径不触发“上传 N 个文件到此网站”的浏览器二次确认；Brave 未开放该 API 时自动回退 `webkitdirectory` 并允许其原生安全确认。两条路径中未确认文件都不进入配置步骤，Dialog 都保留以根目录开头的相对路径并满足焦点圈定与返回焦点；第一批次不物化无需的 member；第二批次不重复 ArchiveEntry，`materialized_blob_id` 只从 NULL 提升一次，物化 Blob 的 size/四种 hash 等于 entry/fixtures manifest，尝试改回 NULL、替换 Blob 或修改 entry hash 均被数据库拒绝。审核前 DOS 目录形成可追溯 source manifest/程序候选和确定性 ValidationFile，GBA 原 ZIP Blob/ArchiveEntry 保留，且没有提前创建 GameContentRevision。Approve 后 ContentRevision 的 DOS_SOURCE/CONTENT 与来源 pair 正确，VariantRevision 直接引用 ContentRevision并复制已验证派生文件；浏览器启动不临时猜 ZIP 入口，审批事务不读 archive/重新打包。
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

### ACC-IMP-007：Approve、重复内容决策、Discard 与纯文字历史

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-007`。
- 流程：先用同一 GBA bytes、不同文件名创建两个 ImportJob，使二者在任一发布前都进入审核；发布第一项，普通 approve 第二项，再用响应给出的当前已有 Game 集合二次确认并发布。随后第三次以另一文件名上传相同 bytes。另对两个 Item 分别编辑字段，选择 READY Validation、文本 Candidate 和来自同 Item 两个已完成 run 的 READY media 后 approve/discard；对 READY 与缺 Parent/BIOS 的 blocker 分别打开审核预览，在 production CSP（不含 `unsafe-eval`）下用 4.2.3 的真实 7z core Worker 与 ZIP 内容 Worker 启动，在核心报告 start 前后检查 5 秒 timer、截图上传与草稿投影，并用 blocker 的当前截图直接人工放行和启动；再注入 iframe 异步错误与无 start 超时，生成新 Validation 证明旧截图失效。故障注入证明审批事务没有 archive/ZIP/网络调用；之后尝试修改旧 ReviewEvent，并从历史页回放。再使用固定 `a.zip -> b.zip -> c.zip` Arcade 向量：child-only 进入审核，以不同本地名补 b、错误 c、正确 c，metadata PATCH 与 Attachment Job 并行；正确 c 完成后先用补传前 ETag 发起一次发布以制造版本交错，再验证客户端刷新和有界重试；另在刷新前修改一个发布字段，证明不会自动重试覆盖并发编辑。最后 approve，读取 Content/Variant 文件和 Parent ReviewEvent，并触发一次同 ContentRevision/DatVersion 的首次启动重校验后读取 Player config 与 Parent bundle。另制造补传后的 effective content identity 命中，执行一次拒绝确认和一次精确确认。
- 通过标准：匹配当前目录/config 和 effective source snapshot 的 READY Validation，或同一当前阻断 Validation 的第 5 秒截图，可 Approve；两类审核预览都锁定 source/Validation/CoreArtifact，生产 CSP 保持不含 `unsafe-eval`，4.2.3 的 7z 与 ZIP Worker 在各自 version-bound 转换后均不执行 `eval` 且真实触发 `EJS_onGameStart`，任一源形状漂移则 fail closed。真实 start 前不计时，第 4,999ms 没有截图、第 5,000ms 才开始优先读取核心最后一帧，静态 ROM/BIOS 错误画面可辨识且不能退化为黑帧，核心截图有界失败才回退 canvas，最终保存非空 PNG 并在活动 Review GET 投影；iframe 同步错误、未处理 rejection 或 30 秒未 start 都显示可操作失败而非永久 loading。Blocker 预览只交付主 ROM 与实际存在的依赖、不创建正式 Game/LaunchSession/PlaySession/SaveState；截图写入后启用 Approve，发布 Variant 保留 `REVIEW_SCREENSHOT_OVERRIDE` 结论，ReviewEvent v2 只保留 override reason 而不保存 screenshot ID，正式单机启动继续最佳努力交付，Netplay 仍严格阻断。重新检查、换目录或 CoreArtifact 漂移后旧截图不能投影或放行。第二项首次 approve 返回 `409 DUPLICATE_GAME_CONFIRMATION_REQUIRED` 且不产生 Game/ReviewEvent，错误/过期/重复 acknowledged ID 不能越过；精确 `ALLOW_NEW` 确认后才发布，并在最终事件保留当前有效内容的结构化摘要、policy 和已有 Game IDs。第三次识别直接将 Item 置 `DISCARDED`、任务 `COMPLETED`，计数/文件投影为已导入跳过，保留指向前两个 Game/current ContentRevision 的文字匹配，不创建 ReviewDraft、Validation、刮削或第三个 Game；改文件名/UploadSession 不影响身份，不同基础平台和 `DELETED` Game 不误阻断。Arcade 接受 b 只追加 revision 2 并仍 BLOCKED，错误 c REJECTED 且快照/digest 不变，正确 c 追加 revision 3/READY；metadata PATCH 不被覆盖。补传导致的首次 stale 发布必须重新 GET Review，并在发布草稿逐字段等价、当前可发布且无 active Attachment 时使用新 ETag 和新 Idempotency-Key 自动成功一次；另一编辑者改字段、当前仍阻断或第二次 stale 时不得重试或发布。Approve 的 GameContentFiles 从 revision 3 包含 CONTENT a 与 COMPANION b/c，VariantFiles 含 PARENT b/c；ReviewEvent 保存前后文字/结构化快照、Validation 结论和来源类型，不含媒体 ID/URL、Blob/hash、路径、MIME、尺寸、ROM bytes 或宿主路径。同内容、同 DAT 的后继启动重校验仍保留这两项 PARENT 与依赖索引，config `parentUrl` 非空且 bundle 根级包含 `b.zip/c.zip`。补传后的重复检查使用 revision 3 digest，不沿用 child-only digest。审批只复制 effective source/ValidationFile refs 并原子发布到唯一游戏目录，不做耗时计算。Discard 会取消 active Attachment 且不发布；Approve/Discard 在决策事务写 `ReviewEvent.schemaVersion=2` 并调度 ImportItem/ImportJob PayloadRelease。活动审核期的来源、候选媒体、预览截图和补传仍可用，进入终态后释放 Job 清空其 payload FK/行并将 UploadFile 推进到可 PURGED 状态；审核历史 API/DOM 仍可回放决定、字段差异、Validation 和来源种类，但没有封面、视频、媒体占位或当前 Game 媒体回填。共享到已发布 Game 的 Blob 由 durable 边继续保护，纯 workflow 独占 Blob 进入 GC 候选；释放失败只把 payload 状态置 FAILED 并保留已完成的领域决定，诊断码可重试且不得回滚成待审核。旧 ReviewEvent 不可更新。
- 证据：三次普通任务与 Arcade 分步任务的 Item/文件/快照/Validation/consumption 计数、两次发布响应、409 details、确认和 schema v2 ReviewEvent、Content/Variant 文件、PayloadRelease/GC 状态、游戏库结果、纯文字历史 API/DOM 和旧事件更新拒绝。

- 聚合补充流程：对同一批两个 Item 分别 approve/discard，并在每次决策前后读取 ImportJob 聚合与入库总览。
- 聚合补充通过标准：最后一个 Item 决策后，Item 状态与 ImportJob 的 pending/published/discarded 计数、state/version/completed time 在同一事务收口；总览 `reviewPending` 只统计实际仍为 `REVIEW_PENDING` 的 Item，不能继续显示已发布/丢弃条目或按任务数计数。

### ACC-IMP-008：有界失败、取消、重试和重启恢复

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-008`。
- 7z 子流程：覆盖 magic/SFX、加密、nested、路径与 casefold 冲突、CRC/size、entry/总量/压缩比、worker crash/signal/timeout 与 IPC 上限；只接受未加密单卷非 SFX 的唯一 ROM wrapper，子进程无可用资源隔离时 fail-closed，所有故障不留下半成品 Blob。
- 流程：创建含 3 个 Item 的任务，对其中一个故障注入；让一个 Item 已发布、另一个进入 RUNNING、第三个进入 REVIEW_PENDING 后取消整个 Import，观察 ImportJob/运行 Job 的 CANCEL_REQUESTED，再由注入式 reader 的下一个检查点确认 CANCELLED；另建独立失败任务，分别在 IDENTIFYING 和 SCRAPING 注入 retryable 本地故障后调用 Item retry，记录 Job/Run/配置快照；并在另一个阶段终止 worker，使用 fake clock 令 lease 到期后重启。再让 CANCEL_REQUESTED worker lease 过期，验证恢复器只清理/确认取消；对确定性坏输入验证直接进入 FAILED_FINAL 而非虚假的 FAILED_RETRYABLE。另创建“GBA 中一个合法 ZIP、一个误选平台的 raw PSP ISO、一个 sidecar”的任务，先发布合法 Item，再把尚未解决的 ISO 以 source 当前 ETag 重新配置到 PSP 目录。
- 通过标准：已发布 Item 不回滚，REVIEW_PENDING Item 在取消事务转 CANCELLED；RUNNING cancel 返回 202，ImportJob 在停止前保持 CANCEL_REQUESTED，最后一个 Worker 确认后才为 CANCELLED，且绝不因已有发布/取消混合计数聚合成 COMPLETED/PARTIAL_FAILURE。取消检查不超过规定 reader/token 边界并且不会发布；旧 worker 在取消/lease 转移后提交被 state+lease token 拒绝；取消中 lease 恢复不继续领域计算。IDENTIFYING retry 复用 pipeline Job并增加 execution，SCRAPING retry 新建 Run/Job且旧证据不变；两者都由 persisted failedStage 分派、保留原 Import 配置，不重复创建 Blob/候选/ReviewEvent。重新配置不上传或复制 bytes，新 UploadFile 与旧文件引用相同 SHA-256 Blob，replacement 生成 raw ISO Item并回指 source；source 原 REJECTED reason 保留、resolution 指向 replacement、未解决计数归零并收口，陈旧 ETag/重复接管整体拒绝。JobEvent 仍按每次真实转换追加；普通过期任务被重新领取并完成；确定性错误直接 FAILED_FINAL，attempt 用尽才从 FAILED_RETRYABLE 进入 FAILED_FINAL；没有长事务或真实等待，任务/审核时刻均为 INTEGER。
- 证据：完整状态转换、引用计数、lease/attempt 和事务时长摘要。

### ACC-IMP-009：严格 READY 快速审批、逐项原子性与恢复

- 上限：240 秒。
- 执行：`make acceptance-case CASE=ACC-IMP-009`。
- 流程：创建跨两个 ImportJob 的 READY、阻断截图 override、重复内容、active Parent/多盘 Attachment、过期 Validation 和非法标题 Item；另创建一个使用 Arcade dependency snapshot schema v2、当前 DAT closure 与冻结 Parent/BIOS ValidationFile 完整的 READY Item。以 `q/tagId/importJobId/pegasusImportId/platformInstanceId/blockerCode` 的固定组合预览完整范围。预览后分别修改一个草稿、发布一个重复来源并并发创建两个 batch，验证 stale/active；重新预览后启动。处理期间在发布事务和批次结果之间故障注入、请求取消并模拟进程退出/重启；另在 worker 基础设施失败后领域 retry，最后对一份含非终态批次的 backup 执行 restore。
- 通过标准：预览计数互斥覆盖 matched，candidate 只含严格 generation 4 READY、当前来源/目录/CoreArtifact/DAT/BIOS/DOS/dependency、合法标题、无重复和 active Attachment 的 Item；截图 override 永远排除。Arcade v2 READY 必须按当前 active DAT 重投影 closure、逐 machine 核对 required entries，并确认外部依赖各有唯一冻结 ValidationFile 后进入 candidate 与成功发布；不能因 BIOS v1 parser 不识别 v2 字段而计入 not-ready/stale。范围枚举不受列表 limit/cursor/已加载 DOM 影响，scope/candidate digest 漂移返回 `REVIEW_BULK_PREVIEW_STALE`，零项/10,001/第二个 active batch 使用稳定错误且不创建半个 Job。每个 PUBLISHED 的 Game/Revision/ReviewEvent、普通与对应服务器来源聚合和 batch item/counter 同事务提交，故障时全部回滚；事件含 `QUICK_STRICT_READY/bulkApprovalId`。处理前 duplicate/changed/not-ready 分别 skip，意外项 final failure 不阻断后续项；取消只收口未提交项，已发布不回滚。重启只恢复未提交项且不重复 Game/Revision/Event，通用 Job retry 被拒绝、worker-only 领域 retry 增加 execution；restore 把遗留 Item 取消、aggregate/Job 置 `FAILED/RESTORE_INTERRUPTED` 并保留已发布项。fresh schema 的 foreign key/integrity 检查无结果。
- 证据：preview/create HTTP 摘要、当前 schema/store 约束、故障注入事务行、JobEvent/ReviewEvent、取消/重启/retry/restore 状态序列及最终 Game 数。

## 11. BIOS 与 Arcade DAT

### ACC-DAT-001：真实 DAT 基线完整性

- 上限：300 秒。
- 前置：计时前已执行一次 `make prepare-deps`，本 Case 期间断网。
- 执行：`make acceptance-case CASE=ACC-DAT-001`。
- 流程：runner 先执行 `make data-check` 与 `make deps-check`，验证运行时 manifest/adapter/allowlist、36 个 EmulatorJS 跨版本 selected core/report 条目、单一 `retrom-runtime` tag 的七条 RPG route/artifact、PPSSPP assets、mame2003 override、38 个 EmulatorJS 许可 component、RPG aggregate notice/许可/上游源码定位、五份 DAT，以及密码 blocklist manifest、10,000 行 payload 和 MIT 许可；离线重建适用 notice。再用全新临时 SQLite 和真实五份 DAT 断网启动服务，等待 ready 并重启复用；最后运行 seed/约束负向与 Git payload 边界检查。
- 通过标准：离线命令成功，所有值与机器基线一致；两份普通 manifest 的 adapter 精确为 `ejs-4.2.3-v3 → 4.2.3`、`ejs-4.3.0-pre-v2 → 4.3.0-pre`，联机 manifest 继续精确引用 legacy `ejs-4.2.3-v2`，base/loader 命中 allowlist，缺失、未知、版本错配或无实现 adapter 均使 `data-check` 失败；35 个 enabled EmulatorJS Core 各恰有一条当前 `selected_for_new_bindings=1,available_for_launch=1` CoreArtifact，七个 RPG Core 各恰有一条同状态 route/artifact，全部逐项等于当前 tag manifest；线程 basename 与实际 artifact 一致，未知版本/route 不回退默认 adapter。冷库先 live/`DEPENDENCY_INDEXING`，五个不可取消 bootstrap Job 在事务外解析，最终五个 Arcade core 各有独立 READY active DAT；重启不重跑 parser。两个 FBA2012 DAT 必须从锁定源码分别完成双生成且 bytes 相同。许可输入逐项命中 size/hash，notice 可重复生成；DAT、EJS/RPG runtime/license/notice payload 均未被 Git 跟踪，RPG 本机物化目录不存在历史版本。整个 Case 断网且启动/解析不尝试 CDN；部署前由 `ACC-PKG-001`–`003` 比较两镜像 release-input digest。
- 证据：逐文件校验/统计、DatVersion/Job 状态序列与 parser 调用计数、事务批次摘要、Git 跟踪边界和断网 network log。

### ACC-DAT-002：Core 隔离与依赖闭包

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-DAT-002`。
- 流程：用确定性 Arcade DAT/ZIP 向量验证 core-scoped parent/BIOS 依赖闭包、补充内容和组装隔离，并交换两个核心的 DAT/依赖上下文。
- 通过标准：Requirement、Parent 与 BIOS 不跨 CoreArtifact/DatVersion 串用；依赖组装只包含当前核心允许的 entry，缺项、错误 hash 与跨核心 DAT 必须拒绝。本 Case 不包含真实游戏导入、Launch 或浏览器帧执行。
- 证据：确定性依赖闭包集成测试输出。

### ACC-BIOS-001：正确与错误 Hash 上传

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-BIOS-001`。
- 流程：用确定性 catalog/hash 测试向量上传匹配的 `disksys.rom`，再上传临时生成的错误内容 `gba_bios.bin`，最后用匹配测试向量替换当前安装。对一个固定 Arcade Requirement 再分别上传“必需 entry 名齐全但一项 bytes/hash 不同”和“完全缺少一个必需 entry”的两个小型 ZIP，再为 ZIP 添加一个 DAT 未要求的文件。每次安装后点击 Arcade BIOS 文件名打开条目对比。
- 通过标准：正确文件显示 installed/matched；错误 hash 文件允许保存并明确显示期望/实际 hash Warning，不伪装成 matched，也不因 hash 不同强制拒绝上传；正确替换后活动安装变为 matched，旧 Installation 保留文字/hash/来源审计但 Blob 引用已清空并进入候选，依赖旧安装的存档和运行快照已清理。Arcade entry 名齐全但 size/hash 不同的 installation 为 active/HASH_WARNING，可装入 Launch bundle且不阻断；完全缺必需 entry 的 installation 可保留为 active/MISSING_ENTRY 供修复但 Launch 阻断；损坏/不安全 ZIP 为 INVALID 且不能 active。弹窗仅使用左右两栏面板，两侧各自为文件列表；列表顶部横向表头精确为 `name`、`size`、`crc`，每个文件在下方占一个仅略高于字体行高的紧凑行并包含同序三个值，字段名不在文件行左侧重复。行内没有状态徽标或状态文案，内容别名、不匹配、缺失和额外文件由不同背景色表达，鼠标悬停 tooltip 和辅助技术提供完整状态说明。各值和安装时校验一致，不把非默认 BIOS set 误列为必需项。
- 证据：三次上传响应、实际/期望 hash、安装 revision、BIOS 状态与 UI 截图。

### ACC-BIOS-002：必需、可选与 Full Non-Merged

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-BIOS-002`。
- 流程：移除 FDS 必需 BIOS 做预检；分别以 `.gb/.gbc/.gba` 小型真实 fixture 检查 Gambatte/mGBA 可选 BIOS 不存在、仅安装另一内容类型 BIOS，以及安装匹配内容类型的正确/`HASH_WARNING` BIOS；读取 Launch config/bundle。为 MelonDS 安装 `bios7.bin/bios9.bin/firmware.bin` 后创建 Launch 和存档，切换其中一个 active installation，再检查旧运行终止/载荷释放并创建使用新依赖的 Launch。最后以 entry 名齐全但 hash 不同的 Arcade BIOS/base archive 启动，并检查包含自身依赖的 Full Non-Merged Arcade fixture。
- 通过标准：适用必需文件/entry 完全缺失阻断；不适用 requirement 不进入 digest/bundle，可选文件缺失只提示且不增加 activation option。匹配内容类型的 active `MATCHED/HASH_WARNING` BIOS 以 Requirement 逻辑名装入，Gambatte config 精确增加 `gambatte_gb_bootloader=enabled`、mGBA 增加 `mgba_use_bios=ON`；MelonDS 的三个 BIOS 不进入根 bundle，而是精确映射到三个固定虚拟路径。同一 Requirement 替换 BIOS 会撤销旧 Launch、删除其存档/运行 payload，并使新 Launch 等待新依赖 revision；旧 capability 不可再访问。Arcade entry 名齐全但 size/hash 不同也形成 `HASH_WARNING` 依赖、进入 bundle 并允许启动。另一内容类型 BIOS 不误启用，冲突 option seed 被校验拒绝，浏览器不按 core 名补写。Full Non-Merged 已内含依赖时不要求重复上传；页面按平台/core 聚合而不按游戏目录复制，`gamegenie.nes/sgb_bios.bin` 按一期条件明确标“未使用”而非缺失。
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
- 通过标准：关闭时不替换，开启也只接受严格更优；同 bytes/同版本不创建 revision，版本变化重新验证可创建 revision，任何降级都保留旧 active。真正替换时旧 Installation payload、依赖存档和运行快照按统一规则释放；未替换分支无副作用。最终 source 或 catalog 变化以条目错误收口；Installation、Item 终态、聚合计数和 PROGRESS 事件同事务，崩溃恢复不重复 revision。
- 证据：前后 active ID/status/version、revision 数、竞态结果、JobEvent 与恢复查询。

### ACC-BIOS-006：异步恢复、取消、详情与多尺寸访问

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-BIOS-006`。
- 流程：用确定性门禁 fixture 验证大目录发现、2 个 hash worker、全局 1 个 archive scanner、进度、lease/heartbeat/deadline、瞬时 root 退避、cancel、崩溃恢复和 restore fence；再由 Chrome 在 1280×800、2560×1440，以及物理 3840×2160、150% scale（CSS 2560×1440、DPR 1.5）创建空候选任务并查看终态详情、筛选和候选入口。
- 通过标准：完整发现前零安装，cancel 保留已完成 Item 并终止其余项；零终态 Item 的瞬时错误按固定有界退避，恢复不重复结果，restore 不自动继续外部 source。Drawer 的 radio/目录/checkbox、Escape/focus trap/焦点返回可用；详情无页面横向溢出、状态不只靠颜色，axe 无 serious/critical 结果。
- 证据：Worker 计数/事件/事务、恢复前后摘要、三尺寸截图、键盘与 axe 结果。

### ACC-BIOS-007：FULL_CATALOG 286 条 cursor 分页

- 上限：240 秒。
- 执行：`make acceptance-case CASE=ACC-BIOS-007`。
- 流程：以固定 286 条 catalog 依次请求首页与 cursor 后续页；Chrome 首屏只加载 100 条，故障注入第一次续页 500，保留旧行并以同 cursor 重试，再通过“加载更多”到终点。
- 通过标准：API 页长精确为 100/100/86，ID 无重复遗漏且末页 cursor 为 null；每页 `summary/filteredCount` 恒为 286。UI 依次显示 100/286、200/286、全部 286；失败不清空旧页、重试 cursor 不变、终点不再请求，纯键盘可完成且页面无横向溢出。
- 证据：三页 request/response 摘要、唯一 ID 数、失败/重试 URL、最终 DOM 行数和截图。

### ACC-DAT-003：当前 release DAT schema 与写入边界

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-DAT-003`。
- 流程：在全新当前库枚举 DatVersion 列、DAT 相关表、Job 类型、Blob consumer、OpenAPI 路由与 catalog job 输入；再由 production dependency manifest 执行 `BootstrapCatalogs` 并尝试绕过 manifest 写入不受支持的来源。
- 通过标准：`dat_versions` 只保存当前 release catalog 所需字段，不存在 source/blob/compatibility 差异字段，不存在 DAT import/diff 表、USER DAT 路由、`baseDatVersionId`、`DAT_VERSION` Blob consumer 或生产 USER 分支；bootstrap 只接受受校验的 release manifest，数据库通过 foreign-key/integrity 检查。
- 证据：当前 schema/枚举/API 摘要、production manifest bootstrap 测试、非法来源拒绝与数据库检查。

### ACC-DAT-004：release manifest 选版与 active 修复

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-DAT-004`。
- 流程：物化固定 dependency manifest；先让同一 CoreArtifact 的另一 BUILTIN DatVersion 处于 active，再执行 `BootstrapCatalogs`，并重复执行验证幂等性。
- 通过标准：引导只选择 manifest 中路径/SHA-256 精确匹配的 BUILTIN DatVersion；旧 active 被停用、CoreArtifact version 递增，目标完成解析后成为唯一 READY active，Requirement 全部指向目标；重复引导不增加 DatVersion/Job，也不再次增加 artifact version。
- 证据：聚焦 dependency integration 测试输出、前后 active ID/version、Requirement sourceVersion 与行数。

### ACC-DAT-005：恶意/错误 DAT 拒绝

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-DAT-005`。
- 流程：直接向受限 production DAT parser 提交带安全 DOCTYPE、XXE/参数实体、畸形或超限 XML 和关系循环夹具；夹具全部小于 1 MiB，不经过已移除的上传 HTTP 路由。
- 通过标准：安全 DOCTYPE 可解析但不解析实体；XXE/参数实体、超限与畸形输入在安全解析阶段拒绝且无外连；循环输出稳定诊断，不造成递归崩溃。任何失败都不能发布 READY/active DatVersion。
- 证据：聚焦 parser 测试、错误码、无外连和无状态发布证明。

### ACC-DAT-006：版本升级证据审计（条件 Case）

- 上限：900 秒。
- 条件：EmulatorJS、任一 core artifact 或预置 DAT 相比上一已接受版本发生变化；否则为 `NOT_APPLICABLE`。
- 执行：`make acceptance-case CASE=ACC-DAT-006`。
- 流程：检查新版本目录、发布物 digest、core source 证据、DAT 同提交/生成证据、parser stats、关系完整性、manifest Player adapter 描述/前端 registry/实现一一对应，以及受影响产品集成、`make web-e2e` 和存档兼容结果；先放入未登记 adapter 的小型 manifest 夹具验证 `data-check` 和 Player config guard 失败，再登记并把新版本追加到配置列表但不切 active，创建一份锁定旧 artifact 的存档，再切 active 并分别普通启动、从旧存档启动，最后切回旧 active。
- 通过标准：不覆盖旧版本；相同 `(core_id,route_key,artifact_set_sha256)` 只复用逐字段完全相同的不可变行，任一 identity 碰撞或 payload/运行语义漂移都 fail closed；静态路由只暴露每份 manifest allowlist。未登记/版本不符 adapter 使 `data-check` 失败，浏览器 guard 以 `PLAYER_ADAPTER_UNSUPPORTED` 在 loader 前拒绝且不套用 v4.2.3 默认；全部证据和适用 Case 已通过后才能选择为新绑定。config 中 `emulatorjsVersion/playerAdapterId/runtimeBaseUrl/loaderUrl/path override` 始终来自锁定 artifact 的 `runtime_version/adapter_id/entry_path/compatibility_json` 与精确 manifest；切换后普通启动使用新 `selected_for_new_bindings` artifact 与其 manifest 固定 DAT，旧存档仍从旧版本 URL 和对应 adapter 加载锁定且 `available_for_launch` 的 artifact，release 回退只切回旧选择及其 manifest DAT，不改历史 revision。仍有保护引用的旧版本不可从配置列表、adapter registry 或镜像移除，也不可将 `available_for_launch` 置零；缺任一证据即失败。
- 证据：升级 manifest、受影响产品测试与未覆盖核心清单、切换/回滚记录。

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
- 流程：扫描含两个 Collection 的固定目录，不设置默认映射并按 ETag 提交显式映射；准备普通单文件、M3U+CHD 和 Arcade ZIP+同目标 companion，并在 Arcade Collection 中放入超过 64 个无关 ZIP；在审核前查询 Game，再对 READY 条目逐项 Approve、对另一条目 Discard，并让一个 Pegasus Arcade blocker 先补传 Parent ZIP、等待后继 Validation READY 后以当前 Review ETag Approve；再次导入相同来源与相同内容的另一来源；在审核交接中点模拟进程退出并恢复。
- 通过标准：未映射时不能开始；计划冻结游戏平台目录/核心版本；三种内容均复用既有验证与普通审核管线，M3U 顺序与 Arcade primary source 正确；Arcade 只装配冻结 DAT parent/romof 闭包中的显式 ZIP，无关 ZIP 不进入单 Item 来源且不会触发 64 文件上限。导入前已安装且匹配冻结 CoreArtifact 的 DAT BIOS 在初始 Validation 中即为 `SATISFIED_EXTERNAL` 并进入 `BIOS_BUNDLE`，不得先误报缺失；同时仍缺 Parent 或主内容不匹配的条目继续按真实原因阻断。Worker 完成后 READY 与 blocker 都为普通 `REVIEW_PENDING` 且 Game 数仍为零；只有后续逐项或严格 READY 快速审批才创建 Game。Parent 接受后有效 source manifest、content identity 与 Review version 同步推进，Approve 必须使用后继快照成功创建带 `SERVER_PEGASUS_IMPORT` 来源的 Game/Revision/媒体并同步两组计数，不能再按 Pegasus 初始 manifest 拒绝；Discard 保留 ReviewEvent 并同步为 `REVIEW_DISCARDED`。历史详情在最终事件选择封面 ID 为空时仍按其 ImportItem 一一关联的 Pegasus Item 返回保留的 COPIED COVER，不依赖后续可变的 Game 当前媒体。交接中点恢复复用同一个内部 ImportItem，不重复草稿事件，未交接条目不可见且不可发布。library validation 未通过时原样保留精确 status、compatibility code、Core 与封闭依赖证据；library import 内部错误收口为可重试失败，并持久化 stage/operation/cause/受限技术详情/数量上限和可用关联 ID；重复结果列出全部既有匹配，不生成审核事项或重复创建 Game/Revision/Blob，条目仍有稳定结果和链接。
- 证据：Migration/服务集成测试、发布与重复摘要。

### ACC-PEG-004：取消、重试、恢复、GC 与 restore fence

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-PEG-004`。
- 流程：在扫描和导入阶段分别取消；注入 retryable 失败、过期 lease、deadline/attempt 耗尽和进程重启；覆盖 discard、重复内容跳过、不可重试失败、计划取消、已交接待审与仍可重试状态；备份恢复含 Pegasus 历史的数据库，并在恢复后运行 PayloadRelease 与单轮 Blob GC。
- 通过标准：取消/失败不删除已生成审核事项或回滚已提交游戏；retry/recovery 不重复内部 ImportItem 或 revision；耗尽任务收敛到稳定 FAILED；BIOS/Pegasus 总内容读取并发不超过 2。仅 discard、重复跳过、不可重试失败和取消等终态调度释放；等待审核、可重试失败和运行中状态继续保留来源 payload。Pegasus Item 释放以 `payloadState/payloadReleaseJobId` 可诊断，复制到发布 Game 的内容/媒体由 durable 边保护，纯 Pegasus 独占引用进入候选；释放 Job 重启恢复、重复执行均不重复删引用。restore 终止外部 source 工作且历史、已交接审核事项可读，不可恢复执行；ReviewEvent 历史为纯文字，终态计划不留悬空保护边。
- 证据：worker、maintenance、Pegasus payload 状态、PayloadRelease 事件、blob registry/GC 与 restart 聚焦测试输出。

### ACC-PEG-005：三步 UI、详情恢复与桌面布局

- 上限：240 秒。
- 执行：`make acceptance-case CASE=ACC-PEG-005`。
- 流程：在 1280×800、2560×1440，以及物理 4K、150% scale 场景打开服务器导入页，只用键盘完成 root/目录选择与扫描；扫描后关闭 Drawer，直接进入该计划详情并从“继续映射”恢复，选择一个既有标签并批量追加到全部未跳过 Collection，再完成全部 Collection 显式映射、确认审核计划和启动；另覆盖已完整保存映射后关闭并恢复第三步。任务准备完成后从批次行动区进入限定审核队列，打开 READY 与 blocker 各一项并返回；检查来源 COVER/VIDEO，确认快速审批只位于统一审核页。在详情注入 BIOS 缺失、parent 缺失、内容 entry 缺失、merged set 不支持和结构化 library import 内部失败，展开诊断、触发原计划重检，再使用 URL 筛选、分页、取消/retry 并模拟 SSE 断线。
- 通过标准：三张能力卡等权且共用 root 说明，Pegasus 卡明确扫描不会自动发布并显示待审核总数；实例没有任何游戏目录时，普通导入与 Pegasus/EmulationStation `AWAITING_MAPPING` 都显示“还没有游戏目录”、说明需执行“一键创建推荐目录”并提供“前往游戏目录”，不展示空映射控件或允许确认。760px Drawer 三步可达、无默认映射，批量标签以 union 语义进入所有未跳过 Collection 且可逐项调整，第三步显示覆盖数量并明确“全部进入待审核”；`AWAITING_MAPPING` 详情能恢复指定计划且不重新选目录/扫描，未保存映射重新选择、已完整保存映射直接进入第三步。Drawer 打开时背景不可滚动，扫描转换与同计划摘要轮询不得造成布局跳动、焦点转移或本地映射丢失。详情以扫描范围/待审核/已发布·丢弃·已有/阻断·失败分组，显示 media READY/MISSING/WARNING、逐项审核入口与已有/新游戏链接；批次入口保留 `pegasusImportId`，清除其他筛选不丢批次，Pegasus metadata 不计作“未找到信息”；审核媒体中 VIDEO 等比居中且不自动播放，Pegasus 详情本身不复制快速审批按钮。阻断行展示当前精确原因，展开后可见稳定 code、Core/machine、缺失条目、依赖和处理建议；内部失败展开后可见 stage、operation、cause code、Pegasus Item ID、相对路径、观察数量/上限、可用内部关联 ID 与受限技术详情，不得只显示 `PEGASUS_LIBRARY_IMPORT_FAILED`；原任务重检后仍保持当前精确状态与原因；断线不清空内容；三个 viewport 无页面级横向溢出，焦点、Escape、reduced-motion 和状态文本符合 UI 契约。
- 证据：Playwright DOM/网络/布局断言和三尺寸当前截图。

### ACC-PEG-006：自有 GBA 目录全链发布与核心运行

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-PEG-006`。
- 流程：将 `testdata/public-roms/gba-smoke/pegasus-smoke.gba` 与最小 `metadata.pegasus.txt` 复制到隔离的只读服务器 root；Chrome 从服务器导入页选择该目录，等待真实 scanner 进入 `AWAITING_MAPPING`，把唯一 Collection 显式映射到 GBA 游戏目录并启动 Worker。任务完成后进入 `pegasusImportId` 限定审核队列，打开 READY 条目并“通过并发布”，再从新建 Game 详情一次点击创建 Launch，读取 config、装载锁定 mGBA artifact 并等待核心帧推进。
- 通过标准：公开 fixture 的 size/SHA-256 与生成器一致且在本 Case 前不存在同标题 Game；扫描、映射、复制、验证和审核交接均消费真实后端，不得 route mock 或直接写库。Worker 结束时条目为待审核且 Game 仍不存在；审核详情保留 `Pegasus · GBA Smoke` 来源，发布后恰有一个新 Game。Launch config 锁定 `mgba`、EmulatorJS `4.2.3` 与普通 `ejs-4.2.3-v3`，`gameUrl` 只指向本 Launch 的 `pegasus-smoke.gba` 内容端点；真实 Player canvas 可见且 `getFrameNum()` 至少继续推进 30 帧。
- 证据：Playwright DOM/API/config/帧数断言、运行中 Player 截图和 fixture identity 输出。

### ACC-MEDIA-001：VIDEO 上传、服务与详情播放策略

- 上限：240 秒。
- 执行：`make acceptance-case CASE=ACC-MEDIA-001`。
- 流程：上传合法 MP4/WebM 与伪装/超限媒体；读取 GET/HEAD/single Range；检查管理员媒体区只显示封面和视频、视频槽布局与等比适配；编辑元信息、替换并删除 VIDEO；在详情模拟累计可见、切换后台、播放拒绝、5 秒无 `playing`、用户暂停和 reduced-motion，并监控游戏列表请求。
- 通过标准：VIDEO 尺寸允许 NULL，magic/MIME/大小严格校验；Range/HEAD 和私有缓存契约正确；管理员媒体区不展示背景图/游戏截图，VIDEO 槽占满封面外的剩余宽高并以 `contain` 等比完整适配、以 `50% 50%` 在槽内水平和垂直居中，1920px 及以上双栏高度由左侧发布信息决定且媒体面板不得反向撑高；元信息编辑保留视频，替换/删除产生不可变 revision 且历史引用保留。详情只有前台可见累计 2 秒才 muted/inline/loop 自动播放，`playing` 后 200ms 淡入；失败保持封面和手动播放，用户暂停不再自动恢复，reduced-motion 完全手动；列表无 VIDEO 请求或 autoplay。
- 证据：媒体/HTTP/React/Playwright 聚焦结果和请求断言。

## 12.1 EmulationStation 服务器目录导入

### ACC-ES-001：严格 XML parser、路径安全与确定性

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-ES-001`。
- 流程：以固定小型夹具覆盖 UTF-8 BOM、game/folder、全部允许字段、title fallback、players `N/N-M`、两种日期、hidden/adult/kidgame、Windows 分隔符和 cover 优先级；再覆盖 DTD/实体/PI/namespace/非 UTF-8、错根、重复 path、控制字符、`..`/absolute/tilde/drive/UNC/URI、深度/attribute/token/字段/游戏/warning 上限。对同一合法树以不同临时 root/UUID 扫描两次，并检索数据库、API、JobEvent 和日志中的 command/emulator/core/provider 原值。
- 通过标准：严格输入生成按 gamelist/path/ordinal 稳定排序的相同 Collection、Item、source key、manifest digest 与 warning；非法输入以封闭 `EMULATIONSTATION_*` code fail closed，不读取 root 外文件、不外连、不 panic 或无界分配。`command/emulator/core` 仅产生结构化 ignored warning，`provider` 只保留布尔存在性，四类原值在持久化/API/日志中均为零；扫描期零业务 Blob、ImportJob、ReviewDraft 与 Game。
- 证据：parser/scanner 单元与 fuzz seed 输出、两次 canonical 摘要、非法向量/code 矩阵、无外连及敏感值检索结果。

### ACC-ES-002：两种目录形态、root/HTTP、显式映射与漂移

- 上限：240 秒。
- 执行：`make acceptance-case CASE=ACC-ES-002`。
- 流程：在同一隔离 root 准备两个独立来源。Case A 的所选父目录含至少三个子目录，每个子目录各有一份 `gamelist.xml` 与多份游戏文件，其中一份清单无效；Case B 是无子目录目录，只有一份 `gamelist.xml` 和多份游戏文件。分别通过真实 HTTP create/list/detail/gamelists/collections 扫描；以 ETag 分批提交每 Collection 的 IMPORT/SKIP、目标游戏目录与 Tag，start 前后制造 root、清单、源文件、PlatformInstance/CoreArtifact/DAT/Tag 漂移。另覆盖匿名/USER、CSRF/Origin、strict body/query、unknown root、traversal/symlink/special、cursor、active/20 waiting/expiry/delete 边界。
- 通过标准：Case A 中每份有效清单恰为一个 Collection，无效清单独立显示且不吞掉合法 sibling；Case B 恰为一个 Collection且清单中的多游戏分别形成 Item，父子/同目录文件不会误合并。客户端只看到 root label/相对路径；无默认或猜测映射，全部行明确决定且至少一个非空 IMPORT 后才能 start。目标漂移要求重新映射，source/root 漂移要求新计划；任何失败都不创建 Game 或越权 Blob。HTTP 权限、ETag/idempotency/cursor/delete 与稳定错误完全符合 OpenAPI，响应/日志不泄露绝对路径、XML、facts/hash 或底层错误。
- 证据：A/B 目录的脱敏相对 tree、Gamelist/Collection/Item counts 与 mapping snapshot、HTTP/权限矩阵、漂移前后 ETag/状态和数据库/CAS 零副作用摘要。

### ACC-ES-003：普通审核交接、重复、多盘、Arcade 与媒体

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-ES-003`。
- 流程：在一个计划中准备单 ROM、hidden/adult 单 ROM、同目录 2–8 CHD 的 M3U、Arcade primary ZIP 与同 execution/target/冻结 DAT 的 companion，以及缺失/损坏 cover/video。完成显式映射并执行；检查审核前 Game，再按 `emulationStationImportId` 读取队列和详情，预览快速审批分类，逐项 Approve/Discard；对 Arcade blocker 补传 Parent 后以有效后继快照发布。再次从相同与另一清单导入相同内容，并在 review handoff 中点模拟进程退出。
- 通过标准：Worker 复用普通格式、内容身份、CoreValidation/DAT/BIOS/Review，完成后所有新候选只到普通 `REVIEW_PENDING` 且自动创建 Game 数为零。M3U 盘序准确，Arcade 只装配显式闭包，媒体缺失/坏格式只写 warning；来源 cover/video、清单/Collection 与 flags 通过封闭 source media 投影。hidden/adult 只计入 `sourceFlagged` 并排除快速审批，仍可逐项批准。Approve/Discard 与 Parent 后继快照分别原子创建/不创建 `SERVER_EMULATIONSTATION_IMPORT` Game/Revision/ReviewEvent 并推进两组计数；重复内容列出全部 match，不创建第二审核项/Game/Blob。崩溃恢复复用同一个内部 ImportItem且不重复草稿事件，未完成交接项不可见。
- 证据：store/service/HTTP 集成输出、审核前后 Game/Item/Revision/ReviewEvent/aggregate 行数、source media/flag、M3U/Arcade snapshot、重复与崩溃恢复摘要。

### ACC-ES-004：取消、重试、恢复、删除释放与共享 GC

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-ES-004`。
- 流程：在扫描与执行阶段分别取消；注入 retryable failure、过期 lease、deadline/attempt 耗尽与进程重启；覆盖发布、丢弃、已存在、确定性阻断、取消、不可重试失败、待审和可重试状态的 PayloadRelease。发布一款 EmulationStation 游戏并建立一份共享 Blob 引用，完成一次实际 Launch/Player 链路后从管理员详情永久删除该 Game；以 fake clock 推进 GC 宽限，在最终删除前分别移除/新增共享引用。最后对含来源历史、待审 CAS 与 active 外部 source Job 的数据根执行离线 backup/restore。
- 通过标准：cancel/retry/recovery 不删除已交接审核事项、不回滚已发布 Game、不重复 ImportItem/Revision；共享 reader 不超过 2，任务按 lease/heartbeat/attempt/deadline 稳定收口。只有规定终态释放来源 payload，`REVIEW_PENDING`/retryable failure 保留；release 重启/重放幂等。Game 删除转墓碑、立即不可 Launch/读内容，异步释放 Game 内容/媒体/存档/运行与已终态 EmulationStation 来源链；共享 Blob 在最后 durable owner 消失前始终受保护，新引用撤销 GC candidate，最后无引用且宽限到期才删除 bytes/Blob 行。backup 不含外部 root/XML，restore 保留待审/已发布 CAS 与历史，并在 HTTP 前以 `SERVER_IMPORT_SOURCE_NOT_RESTORED` 收口所有外部 source Job。
- 证据：worker/release/GC JobEvent、payload/ownership registry 分类、删除 impact/墓碑、删除前后 CAS/Blob/共享引用、fake clock、backup manifest 与 restore 前后 canonical 摘要。

### ACC-ES-005：三卡、三步 Drawer、详情审核与多尺寸无障碍

- 上限：240 秒。
- 执行：`make acceptance-case CASE=ACC-ES-005`。
- 流程：在 390×844、1280×800、2560×1440 与物理 4K 150% scale 打开服务器导入页，只用键盘从 EmulationStation 卡选择 Case A 父目录、扫描、关闭 Drawer、从计划详情恢复第二步，为每份有效清单逐项 IMPORT/SKIP 与映射并批量/逐项编辑 Tag，再确认启动。完成后从详情进入 `emulationStationImportId` 限定审核队列，检查 READY、blocker 与 hidden/adult 项、来源媒体和快速审批预览；注入无 root/无 PlatformInstance、invalid Gamelist、library failure、SSE 断线、cancel/retry/delete、loading/empty/error/payload released 状态。
- 通过标准：BIOS/Pegasus/EmulationStation 三卡等宽等高等权，文案明确只读 `gamelist.xml`、不执行命令/不自动发布。760px Drawer 三步、背景锁定、焦点/滚动/未保存选择行为正确；每份有效 Gamelist 一行，显示目录/清单、game/extension/issue/folder/hidden/adult 且无默认 mapping，第三步显示来源 flag 警告和全量审核边界。详情计数分组、过滤/分页、继续映射、逐行审核/已有 Game/诊断/释放状态可操作；固定审核筛选不可被“清除全部”移除，sourceFlagged 排除解释清楚。四个尺寸 document 零横向溢出，target 至少 44px，键盘顺序、Escape、焦点返回、aria-live、reduced-motion 正确，axe 无 serious/critical。
- 证据：四尺寸当次截图、Playwright DOM/布局/URL/network、键盘/focus/axe trace、状态与诊断文本断言。

### ACC-ES-006：自有 GBA 清单全链发布、游玩与删除回归

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-ES-006`。
- 流程：运行 `make public-fixtures-check`，把 `testdata/public-roms/gba-smoke/emulationstation-smoke.gba` 与最小严格 `gamelist.xml` 复制到隔离只读 server root。Chrome 从 EmulationStation 卡选择目录，等待真实 scanner 到 `AWAITING_MAPPING`，把唯一 Collection 显式映射到 GBA/mGBA 并启动。任务完成后进入限定审核队列逐项 Approve，从新 Game 详情一次点击创建 Launch，读取 config/内容并等待核心帧；退出后通过管理员 Game 详情取得删除 impact 并永久删除，等待 release 到可验证终态。
- 通过标准：ROM、gamelist、项目自有封面和项目自有视频的 size/SHA-256/完整 bytes 与唯一确定性生成源一致，且 ROM 与普通/Pegasus 两个 GBA 身份不同；gamelist 只能引用同目录这三个项目自有 payload。扫描、映射、复制、验证、审核、发布、Launch、内容端点与 Player 全部经过真实产品代码，无 route mock/直接写库。Worker 结束时 Game 为零，审核页的 EmulationStation 封面和视频可见且受保护端点返回锁定 bytes；Approve 后恰有一个新 Game，用户详情投影并可读取同一封面与视频。Launch config 锁定 `mgba`、EmulatorJS `4.2.3`、普通 `ejs-4.2.3-v3` 与仅本 Launch 可读的 `emulationstation-smoke.gba`，真实 canvas 可见且 `getFrameNum()` 至少继续推进 30 帧。永久删除后 public detail、config、ROM、封面、视频和再次 Launch 均不可用，Game 为不可恢复墓碑；流程与 Game payload 最终释放，ROM/COVER/VIDEO 三个独占 Blob 均从受保护分类转入 `UNREFERENCED` 与 GC candidate，不能泄漏。共享引用保护继续由 `ACC-ES-004` 证明。
- 证据：fixture identity、计划/审核/Game/Launch ID、DOM/API/config/content/帧数、运行中 Player 截图、删除 impact/payload 状态与最终独占引用摘要。

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
- 流程：先用 `testdata/public-roms/gba-smoke/gba-smoke.gba` 经过真实上传、导入、审核和发布建立 mGBA 游戏；在详情点击一次“开始游戏”，记录原始点击、Fullscreen 调用、launch/config 请求、iframe 配置、EmulatorJS network 和 start 事件；运行后读取实际 controls，按 `P` 暂停并再次按 `P` 继续；打开右侧“调试信息”面板并等待两次采样；打开模拟器设置，依次切换画面模式以及 Core/显示面板；再用 `mame2003` override 执行一次短流程。`make web-e2e` 另在物理 4K 150% 项目重复真实 mGBA Player 链路并校验截图像素尺寸。
- 通过标准：对始终存在的 `document.documentElement` 的 Fullscreen 请求仍在用户激活链且发生于第一个 await 前；同一 Player Shell 显示加载并自动开始；没有 Retrom 第二个 Start 或 EmulatorJS `Play Now`；进入有效帧画面。实际 controls 只含运行时专题规定的键盘绑定，所有未列键盘 control 为未绑定；共享投币键 `5` 只命中 P1 control 2，P2 control 2 未绑定，确保一次物理按键只注入一路 coin；P1 的全部 gamepad `value2` 与上游默认逐项相同且 P2/P3/P4 gamepad 默认不变；`P` 不成为游戏 control，能停止并恢复核心帧推进，同时正确投影 Player/heartbeat 的暂停状态。默认“锐利像素”关闭 shader 且 canvas 计算样式为 `image-rendering: pixelated`；“清晰增强”启用 `retrom-sharp-bilinear`，增强锐化、原始画面与返回默认模式即时更新当前 EJS shader/CSS，原始画面关闭 shader 并恢复浏览器默认缩放。顶部栏保留唯一常驻“创建存档”，更多菜单不重复该动作；Core 设置切到显示设置后 Graphics Settings 与 shader 入口可见。`make web-e2e` 的物理 4K 150% Player 截图必须为 3840×2160。点击“调试信息”不暂停 main loop，右侧面板显示从核心帧计数按相邻单调时钟采样计算的一位小数 FPS、累计帧数、真实 canvas 分辨率、Core/EmulatorJS/adapter、输入模式、隔离能力、viewport/DPR 和非秘密 artifact ID，关闭后不残留可聚焦控件。进入游玩页与退出返回均替换当前浏览器历史项，退出后浏览器后退不得重新进入 Player Shell。config 严格符合 HTTP 契约且不含 secret/Blob/宿主路径；`emulatorGameId` 为 `1..9007199254740991` 的 JSON number、`gameName` 为其稳定十进制派生，Arcade `gameUrl` basename 精确为 DAT machine 的 `<machine>.zip`。iframe 先设置 `player/pathtodata/gameName/gameID/paths/defaultControls` 再加载固定 loader，`typeof EJS_gameID === "number"`。EJS 配置固定 `language=zh-CN`、`disableAutoLang=false`（按 v4.2.3 的反向 sentinel 语义），网络只请求 manifest 中的 `zh-CN.json`，不得按系统 locale 或 CDN fallback；普通 core artifact 来自 config 的 basename 映射，`mame2003-wasm.data` 精确请求固定 4.2.1 override，未请求 4.2.3 同名 artifact 或外部 CDN。
- 证据：Playwright trace、两份 config/network 摘要、事件顺序、Player/调试信息截图和按钮断言。

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
- 流程：先用旧 DOS artifact 发布一个只有 `DOS_SOURCE` 的游戏，再启用 4.3 artifact 触发首次启动重校验；导入一个数据/图片排在程序之前且同时含安装器、实际入口、需缩短的长路径和两个会产生同一初始 8.3 名称的程序的 DOS ZIP；另导入一个 `PLAY.BAT` 先运行已知交互配置器、末行才调用实际 EXE 的目录与 ZIP 向量，以及只有 SET/末端 EXE、没有交互辅助程序的控制向量。确认完整候选排序，选择非默认程序并启动；检查锁定内容与 game.zip 的 HEAD/Range/完整 GET；再选择“显示 DOSBox Pure 程序菜单”，重新进入详情并验证记忆选择，最后构造选中程序已不存在的 revision。
- 通过标准：重校验不要求不存在的 CONTENT 行，保留相同 ContentRevision 的 bundle/default entry 并在有界时间进入终态；安装/配置工具只降权不消失；有界 BAT 分析把“交互配置器 → 实际 EXE”的末端程序提升为目录与 ZIP 的默认入口，使第 5 秒截图来自游戏启动序列而不是配置器/模拟器菜单，同时没有已知交互辅助程序的 BAT 维持原排序，所有候选和原 bytes 均保留。直接启动锁定原 bundle Blob、Blob 数不增加，响应 ZIP 的首项是受控 `AUTOBOOT.DBP`，程序菜单首项是受控 `DOSBOX.BAT`，其余原成员压缩 bytes/顺序不变，源包同名保留文件都无法覆盖或劫持选择；标准 UTF-8 与旧式 GB18030 中文目录都按导入时的规范路径命中精确成员。后者在虚拟 ZIP 中把所选入口的高位 byte 路径组件确定性映射为同目录无碰撞的 ASCII 名称，所有共享该目录前缀的 local/central name 与后续 local offset 一致更新，`AUTOBOOT.DBP` 直接运行映射后的 ASCII 8.3 路径；原 Blob、成员内容和数据库记录均不改写。config 的 `externalFiles/defaultCoreOptions` 不含 DOS 启动补丁，4.3 adapter 在 start 前对锁定的 7z/ZIP Worker 执行与 4.2.3 同样精确且 fail-closed 的无 `eval` 转换，在不含 `unsafe-eval` 的生产 CSP 下把完整 ZIP 交给 core，安全路径进入所选程序画面。程序菜单通过 `Z:\PUREMENU` 进入 core 菜单。只有成功创建 Launch 后才按游戏记住入口或菜单，失败不改偏好，存档恢复不改偏好；缺失/不安全 entry 仍分别稳定阻断且不猜替代程序。
- 证据：完整程序列表与排序、launch/config payload、原 Blob/引用计数、三种 game 响应、虚拟 ZIP central directory/引导 bytes、运行画面、浏览器偏好和错误响应；另以 `RETROM_DOS_CORPUS=<合法本地目录> go test ./internal/libraryimport -run TestLocalDOSCorpusCompatibility -count=1 -v` 验证多游戏结构矩阵。

### ACC-RUN-006：MAME 2003 test-only 内置 DAT、Split、Parent 与 BIOS 产品链路

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-RUN-006`。
- 流程：先确认 `ACC-DAT-004` 已独立验证 production manifest 的 MAME 2003 DAT；本 Case 在临时验收数据库中由 acceptance-only Go 装置把项目自有 `mame2003-smoke.xml` 登记为 test-only `BUILTIN/READY`，不调用任何 DAT HTTP/UI 路由。分别通过真实 BIOS installation 与普通 Import 上传 `retrombios.zip`、`pacman.zip` Child、`puckman.zip` Parent，核对审核 schema v2 依赖快照、审核发布、读取详情页并触发首次启动重验证。再从同次实际导入形成的 Content、DatVersion、Parent 与 BIOS 不可变证据复现 screenshot-approved 的 schema v2 current revision，不经首次重验证从详情页直接 Launch。读取两条 Launch config 与受限内容，随后由 Chrome 通过 Retrom Player 启动 MAME 2003。
- 通过标准：test-only active DatVersion 为 `BUILTIN/READY` 且与小型 DAT SHA-256 一致，但不得被解释为 production manifest 基线；其 `pacman`、`cloneof=puckman`、`romof=retrombios` 与三份 archive entry 的 name、size、CRC32、SHA-1 和 fixture bytes 一致。审核及发布 current revision 的依赖证据锁定同一 DatVersion 的 Arcade schema v2，包含 `PARENT puckman` 与 `BIOS_OR_BASE retrombios` 两个 `SATISFIED_EXTERNAL`，不能出现普通 BIOS schema v1 的 `bios` 字段；详情页在首次启动重验证前后都投影同一 DatVersion/READY/schema v2。正常发布、首次重验证与直接 Launch 都保留同一 ContentRevision、DatVersion、Parent 和 BIOS；config `parentUrl/biosUrl` 均非空，并保留 `REVIEW_SCREENSHOT_OVERRIDE` 诊断。config 精确选择 `mame2003`、普通 `ejs-4.2.3-v3` 与 4.2.1 data override；游戏、Parent 和 BIOS 端点 bytes 与 fixture 精确相同。Player 默认启用推荐清晰 shader 与像素合成缩放，无必需 runtime/content 请求失败或页面异常，canvas 两次采样不同，调试遥测为“运行中”且 FPS 大于 0。测试 BIOS 不被目标驱动执行。
- 证据：fixture/production manifest 分层校验、test-only DatVersion/import/review/launch ID、三路内容比对、Playwright trace、动画帧/遥测断言与运行截图。

### ACC-RUN-007：FBNeo test-only 内置 DAT、Split、Parent 与 BIOS 单机产品链路

- 上限：300 秒。
- 执行：`make acceptance-case CASE=ACC-RUN-007`。
- 流程：先确认 `ACC-DAT-004` 已独立验证 production manifest 的 FBNeo DAT；本 Case 在临时验收数据库中由 acceptance-only Go 装置把项目自有 `fbneo-smoke.dat` 登记为 test-only `BUILTIN/READY`，不调用任何 DAT HTTP/UI 路由。分别通过真实 BIOS installation 与普通 Import 上传 `retrombios.zip`、`pacman.zip` Child、`puckman.zip` Parent，核对审核 schema v2 依赖快照、审核发布、读取详情页并触发首次启动重验证。再从同次实际导入形成的 Content、DatVersion、Parent 与 BIOS 不可变证据复现 screenshot-approved 的 schema v2 current revision，不经首次重验证从详情页直接 Launch。读取两条 Launch config 与受限内容，随后由单个 Chrome 页面通过 Retrom Player 启动 FinalBurn Neo。
- 通过标准：test-only active DatVersion 为 `BUILTIN/READY` 且与小型 DAT SHA-256 一致，但不得被解释为 production manifest 基线；其 `pacman`、`cloneof=puckman`、`romof=retrombios` 与三份 archive entry 的 name、size、FBNeo 锁定驱动 CRC32 和 fixture bytes 一致。审核及发布 current revision 的依赖证据锁定同一 DatVersion 的 Arcade schema v2，包含 `PARENT puckman` 与 `BIOS_OR_BASE retrombios` 两个 `SATISFIED_EXTERNAL`，不能出现普通 BIOS schema v1 的 `bios` 字段；详情页在首次启动重验证前后都投影同一 DatVersion/READY/schema v2。正常发布、首次重验证与直接 Launch 都保留同一 ContentRevision、DatVersion、Parent 和 BIOS；config `parentUrl/biosUrl` 均非空，并保留 `REVIEW_SCREENSHOT_OVERRIDE` 诊断。config 精确选择 `fbneo`、普通 `ejs-4.2.3-v3` 与锁定 4.2.3 core artifact；游戏、Parent 和 BIOS 端点 bytes 与 fixture 精确相同。Player 默认启用推荐清晰 shader 与像素合成缩放，无必需 runtime/content 请求失败或页面异常，canvas 两次采样不同，调试遥测为“运行中”且 FPS 大于 0；测试 BIOS 不被目标驱动执行，本 Case 不创建联机房间、不验证双浏览器 confirmed frame、lockstep 或 digest 收敛。
- 证据：fixture/production manifest 分层校验、test-only DatVersion/import/review/launch ID、三路内容比对、Playwright trace、动画帧/遥测断言与运行截图。

### ACC-RUN-008：SNES9x 自有 LoROM 单机执行与状态恢复

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-RUN-008`。
- 流程：将 `snes-smoke.sfc` 经过真实 upload、Import、Review、Approve 建立 READY 游戏，以 `snes9x` 启动 Player，比对 config、ROM 和 core artifact；注入方向/动作输入后捕获 native state 与画面，创建显式存档，再从该存档独立启动两次。
- 通过标准：config 精确为 EmulatorJS 4.2.3、普通 `ejs-4.2.3-v3`、`snes9x-wasm.data`，不带 Parent/BIOS；ROM bytes 与项目自有 32 KiB LoROM 一致。输入后 core digest 和可见画面都发生变化，RASTATE `MEM` 非空且不超过 1 MiB；两次恢复均由原生 load 完成且得到同一完整 core digest，无页面异常或必需 runtime/content 请求失败。
- 证据：产品导入/审核/发布结果、config/content/artifact 摘要、输入前后 state/画面、存档与两次恢复 digest、运行截图。

### ACC-RUN-009：Nestopia 自有 NES 单机执行与状态恢复

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-RUN-009`。
- 流程：把项目自有 `nestopia-smoke.nes` 作为独立游戏经过真实导入、审核与发布，使用 `nestopia` 重复 `ACC-RUN-008` 的输入、native state、显式存档与两次独立恢复链路。
- 通过标准：config 精确为 `nestopia-wasm.data` 且不带 Parent/BIOS；ROM SHA-256 与 fixture 一致。实际核心帧、输入可见反应、1 MiB state 上限与两次恢复均通过，无静默回到开头。本 Case 验证普通存档恢复；Nestopia 联机 authority state 的精确 trailer 归一见 `ACC-NP-018`。
- 证据：同 `ACC-RUN-008`，并包含 Nestopia core/artifact 与 ROM 哈希。

### ACC-RUN-010：MAME 2003 Plus Split/Parent/BIOS 单机链路

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-RUN-010`。
- 流程：使用 acceptance-only 装置登记项目自有 MAME 2003 Plus DAT，将 Child、Parent 与测试 BIOS 经真实产品链路导入、审核、发布并启动，再重复输入反应、存档与两次独立恢复。
- 通过标准：test-only DAT 不冒充 production baseline；config 精确为 `mame2003_plus-wasm.data`，`parentUrl/biosUrl` 均存在，三路 bytes 与 fixture 一致。项目自有驱动程序产生可见输入反应，native state 不超过 1 MiB，两次恢复 digest 一致；测试 BIOS 不被目标驱动执行。
- 证据：DAT 分层、产品导入/审核/发布结果、三路 content 摘要、config、state/恢复 digest 与截图。

### ACC-RUN-011：FBA2012 CPS1 自有 68000 程序单机链路

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-RUN-011`。
- 流程：登记项目自有 CPS1 test-only DAT，将根 machine 归档经真实导入、审核、发布和 Player 启动，执行输入、state、存档与两次恢复。
- 通过标准：config 精确为 `fbalpha2012_cps1-wasm.data`且无 Parent/BIOS；实际 68000 程序已写入锁定 fixture marker 和 CPS palette RAM，输入后 digest/画面都变化，state 不超过 1 MiB，两次独立恢复完整 digest 一致。
- 证据：DAT/ROM 哈希、config/content/artifact、marker/palette/state 断言、恢复 digest 与截图。

### ACC-RUN-012：FBA2012 CPS2 Child/Parent 自有 68000 程序单机链路

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-RUN-012`。
- 流程：登记项目自有 CPS2 test-only DAT，将 `spf2xjd` Child 和锁定核心按 zip-name 链强制打开的 `spf2t` Parent 经真实导入、审核、发布与 Player 启动，重复 CPS1 的输入、state 和恢复链路。
- 通过标准：config 精确为 `fbalpha2012_cps2-wasm.data`，`parentUrl` 存在而 `biosUrl` 不存在；Child 运行完整自有 68000/图形程序，Parent 只含项目自有 marker、无第三方 ROM 且不被驱动执行。marker/palette、输入反应、1 MiB state 上限和两次独立恢复均通过。
- 证据：DAT Child/Parent 闭包、两路 content 哈希、config、marker/palette/state 断言、恢复 digest 与截图。

### ACC-SAVE-001：手动状态存档与截图

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-SAVE-001`。
- 流程：启动游戏后打开退出确认，从操作区最左侧点击“创建存档”；等待成功提示，确认弹窗仍保持打开后取消退出；读取存档记录与“我的存档”卡片。另注入一次创建失败并从同一弹窗重试。
- 通过标准：退出确认内“创建存档、取消、退出游戏”的视觉与 DOM 顺序一致；创建时暂停并锁定弹窗内的离开动作，成功后弹窗不自动退出、显示不可重复点击的“已创建存档”，失败明确说明未创建不完整记录并显示“重试创建存档”。非空状态 Blob 必须与 SaveState 在同一事务引用；上传按实际字节显示 0–100% 进度，直到 HTTP 成功/失败或网络错误才结束，失败同时提醒用户。正常截图路径在暂停前从仍运行的帧取得，工具栏最迟 750ms 暂停而截图可在独立 5 秒期限内继续完成，不能因暂停先完成就丢弃迟到截图；优先使用 core framebuffer，核心原始帧方向与显示 aspect 互换时或能力不可用时回退 canvas，结果方向与 Player 一致、可解码且具有非零亮度分布，已暂停时复用进入暂停瞬间缓存的最后一帧，不能生成全黑 canvas 截图。另注入全部截图路径失败：请求省略 screenshot、存档仍成功且 API 的 `screenshotUrl=null`、UI 显示“无预览图”；空 state 仍必须拒绝且不建记录。两种成功记录都包含 Profile、Game、ContentRevision、CoreArtifact、GameVariantRevision、名称、整数时间和累计时长。物理 4K 150% Player 必须完成带截图创建、服务端截图解码和继续游戏流程。
- 证据：退出确认三个状态、存档 API/数据库、CAS hash 和当前截图。

### ACC-SAVE-002：三个入口快速恢复与不兼容拒绝

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-SAVE-002`。
- 流程：先在本 Case 的 seed 中按 `ACC-SAVE-001` 规则创建一份带截图的有效存档；分别从详情存档、我的存档和首页继续入口恢复；首页继续前先在 `390×844` 核对带存档卡片，再调整为物理 `3129×1380`、150% 缩放所对应的 `2086×920` CSS 尺寸并记录主视觉截图；再用不同 Core/revision 尝试加载。
- 通过标准：三个入口均一次点击直达 Player Shell，不经过详情或二次 Start，且使用存档锁定环境；`390×844` 下封面与游戏信息同处首行，继续按钮完整留在信息列，16:9 存档预览独占第二行，四者均在媒体区内且按钮与预览至少间隔 8px；`2086×920` 下首页不产生 document 纵向滚动，最近玩的游戏媒体区高度至少 160px，5:7 封面、游戏信息和主操作均完整位于媒体区内；不匹配时明确拒绝，不静默迁移或改用目录默认核心。
- 证据：三条 route/launch trace、`390×844` 与 `2086×920` 首页截图和负向错误。

### ACC-SAVE-003：仅显式 SaveState、全核心恢复与本地残留隔离

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-SAVE-003`。
- 流程：分别在已有真实产品覆盖的 NES、FBNeo 与其余选定核心上从普通 Launch 开始，记录 config 与网络请求；持续运行后直接退出，再重新普通启动。随后在同一 Chrome profile 的 `/data/saves` 预置陈旧本地文件并再次普通启动。对每个受测核心只通过“创建存档”生成有效 state 和可选截图，等待上传进度完成，再从该 SaveState 启动并比较保存前后的可辨识位置；MAME 竖屏游戏的带截图分支额外比较 Player 与存档截图方向。对上传失败、空 state、畸形 state 及跨 artifact/revision 做负向验证，并证明无截图的合法存档仍可恢复。
- 通过标准：全部当前 artifact 的 config 不包含自动/持久目录存档字段，Launch 不绑定隐式存档；Player 不监听/上传目录存档，定时运行、直接退出与 `pagehide` 都不产生 SaveState。`saveDatabaseLoaded` 在 start 前清空整个 `/data/saves`，普通开始不从服务端或同浏览器 IDBFS 复活上次位置。只有点击“创建存档”产生 multipart 上传，0–100% 进度保持到 HTTP 成功/失败或网络错误，失败明确提醒且不创建不完整记录。指定存档在 4.2.3 至少等待一帧和 serialization readiness，再以原生 task 成功为 start 门禁；恢复画面/位置与保存点一致，失败必须阻断而不能伪装回到开头。竖屏存档截图与实际显示同向；不同 CoreArtifact/VariantRevision 不串用。数据库、API 和运行时只存在显式 SaveState 能力。
- 证据：各核心 config/网络请求、普通启动前后画面对比、显式上传进度及成功/失败 UI、state-load 原生日志、恢复位置对比、竖屏截图尺寸/方向、IDBFS 清理与数据库行数。

### ACC-PLAY-001：有效游玩时长

- 上限：120 秒。
- 执行：`make acceptance-case CASE=ACC-PLAY-001`。
- 流程：使用 fake clock 先驱动一次 config 后/start 前超过 2 分钟的加载和 pre-start finish，再用新 Launch 驱动 start、两次 heartbeat、页面隐藏、暂停、失联和重复 finish；另提交越界 `clientObservedAtMs`。
- 通过标准：加载阶段没有 PlaySession/idle 误过期，pre-start finish 撤销且不创建游玩记录；真实 start 后才启用 2 分钟 idle。三个事件端点都位于 `/runtime/launches/{launchId}/` 且校验 launch cookie，只有公开 launchId 没有 cookie 时为 401。只累计实际运行区间；隐藏/暂停/超出失联上限不累计；heartbeat/finish 幂等、跳号冲突，client time 只审计且越界拒绝；数据库全为整数毫秒，首页/详情汇总一致。
- 证据：事件时间线、期望/实际 duration 和 API 汇总。

## 14. 核心产品链路覆盖

核心运行兼容必须由实际 Retrom 产品链路证明，不能用直接加载 EmulatorJS 的独立页面形成验收 Case。当前覆盖范围、对应命令和未覆盖核心由 [`core-runtime-validation.md`](./core-runtime-validation.md) 维护；本验收目录只登记能够覆盖完整产品契约的 Case。

`ACC-RUN-002`、`ACC-RUN-006`–`012` 与 `make web-e2e` 覆盖 mGBA、MAME2003、FBNeo、SNES9x、Nestopia、MAME2003 Plus 与 FBA2012 CPS1/CPS2 的真实浏览器单机产品链路；`ACC-NP-014`–`022` 覆盖八个锁定 profile 的双浏览器联机链路。其余未登记 enabled core 尚无真实 ROM 产品链路 E2E，不得从 manifest、协议、单元测试或相邻核心结果外推为已通过；双浏览器结果也不能外推到其他 artifact 或任意游戏内容。

## 15. UI、4K 与无障碍

### ACC-UI-001：认证入口、用户导航与权限入口

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-UI-001`。
- 流程：从全新浏览器 context 分别访问 PENDING 实例、READY 实例的首页、带 query 的游戏库和管理后台；依次以 USER、ADMIN 登录并从游戏卡片进入详情，再访问认证页和退出。
- 通过标准：PENDING 只进入 `/setup`；READY 匿名重定向 `/login?returnTo=...` 且登录后恢复站内 path/query；已登录访问认证页回首页。桌面用户侧显示首页、游戏库、我的存档、我的收藏、最近游玩以及按 feature flag 出现的联机游玩；手机底栏显示首页、游戏库、存档、收藏、更多，其余入口位于 More Sheet。只有 ADMIN 显示管理入口，USER 直达后台显示 403。游戏详情不作为一级入口且保持游戏库上下文；退出清除会话并回登录。移动细节由 `ACC-MOB-001`–`007` 覆盖。
- 证据：导航可访问名称、route 序列和截图。

### ACC-UI-002：游戏入库父子导航与顶层批次口径

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-UI-002`。
- 流程：依次访问 `/admin/imports`、`/admin/imports/new`、`/admin/imports/server`、`/admin/imports/tasks`、`/admin/reviews` 和 `/admin/reviews/history`，使用浏览器前进/后退；建立一个含至少三个游戏的 Pegasus 批次和一个普通上传批次后重新读取总览与普通任务页。
- 通过标准：“游戏入库”可点击进入独立总览；导入、本地扫描、任务、待审核、历史是同级缩进子菜单；父级上下文与当前子项同时高亮；页面不是通过页内 Tab 伪装路由，浏览器历史正确。一次 Pegasus 操作在最近任务中只出现一行并只贡献一个进行中或完成批次，普通任务页不出现其逐游戏内部 ImportJob；处理中/异常/待审核条目分别来自真实全集，不能等于最近三行的偶然求和。
- 证据：每条 URL、导航状态、六张当前截图，以及顶层批次/条目 API 响应与可访问 DOM 断言。

### ACC-UI-003：首页、游戏库、详情与存档流程

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-UI-003`。
- 流程：检查首页时长、最近游玩和按添加时间倒序的最新 10 款游戏；在游戏库搜索并按平台/目录筛选；从卡片进入详情；查看封面、元信息、时长、最近 3 份存档、全量存档 Drawer、截图预览、核心和 DOS 程序；从存档次要入口进入详情。
- 通过标准：首页五层顺序为最近玩的游戏/快速开始、最近游玩、最新添加、平台、资料库摘要；最新添加只含启用目录中的已发布游戏，最多 10 款且以创建时间和 Game ID 稳定倒序，入口进入游戏详情，“查看游戏库”恢复最近加入排序。游戏库首屏和每个续页只请求 50 条，滚动到末尾才按 cursor 读取下一页；同一次哨兵停留不并发或连续重复请求，跨页无重复/漏项，首分页 facet 仍提供全部平台、目录、活动标签和真实计数，搜索/筛选/排序改动取消旧请求并从首分页重载。筛选进入 URL 且刷新可恢复；卡片只显示已发布游戏；详情信息完整，默认核心状态准确；存在简介时全文可见、不行数截断，在 2560px CSS viewport 中简介占满 Hero 中栏可用宽度而不留固定空白；详情只内联最近 3 份存档，每张卡按保存时间/名称与状态、保存位置/锁定 Core/保存时累计时长、整行恢复操作三层展示，Drawer 包含当前游戏全部存档，其桌面行高约为原紧凑行的 1.5 倍，主色白字“▶ 继续”位于右下角且关键文字使用明确粗体层级；取消运行方式对话框不修改偏好，应用后才生效；存档主操作直接启动、标题/次要操作才进详情。
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
- 流程：在 `1280×800`、`2560×1440` CSS viewport，以及物理 3840×2160、150% scale（`2560×1440` CSS viewport、DPR 1.5）的 `chrome-4k-150` 项目分别打开首页、游戏库、详情、存档、收藏、最近游玩、联机大厅、账户和 Player Shell；首页另在浏览器工具栏占用后的代表性 `1920×950` CSS viewport 复测，并以 `3840×2160` CSS viewport 做不生成标准 4K 截图的补充 ultra-wide 检查。
- 通过标准：无页面级横向溢出、遮挡、过小控件或跨屏长文本；所有被本 Case 打开的用户页面中，可见按钮和链接的操作文案均不追加字面量箭头字符 `→`。同一 CSS viewport 下首页、游戏库、详情、存档、收藏、最近游玩、联机大厅与账户页的主容器相对应用内容区的左、右间距分别一致，测量误差均不超过 1px。首页“最近玩的游戏”、最近游玩和最新添加的全部封面容器在桌面与手机 viewport 均为 5:7，宽高比误差不超过 0.01；此次比例调整保持各布局原有高度，只收窄宽度。三类真实封面图片均保持原比例、居中裁切并填满容器，不显示黑边。首页最近游玩和最新添加在稀疏数据下从左侧自然排列，常规桌面单卡宽度不超过 480px，不得拉伸填满轨道。物理 4K 150% 与 `1920×950` 首页五层均完整落在首屏且 `documentElement.scrollHeight <= clientHeight`，不出现纵向滚动条，紧凑态仍保持正文和卡片信息清晰可读；物理 4K 页面截图的 CSS viewport/DPR 必须为 `2560×1440/1.5`，PNG 必须为 `3840×2160` 像素。标准基线的游戏库在 1280/2560/物理 4K 150% 下分别为 4/6/6 列，补充的 3840 CSS ultra-wide 为 8 列，共享页面有效内容宽度不超过 2320px。详情页在 `2560×1440`（同时代表物理 4K 150% 的 CSS 布局）下 Hero、信息条和最近 3 份存档均完整落在首屏；存档为三列纵向大截图卡片，截图保持比例，Drawer/对话框不推动页面布局；`1280×800` 下关键启动操作和存档区仍在首屏可达。Player stage 为无边距的 100vw×100dvh；进入运行态后 58px toolbar 立即自动移出画面，只有指针进入顶部 32px、`Tab` 导航或工具栏获焦才恢复，画面中央 pointermove 和方向键/WASD/动作键/投币/开始等普通游戏输入均不改变可见性；显式按 `P` 暂停后按暂停态保持工具栏可见。标题/Core/平台和同步状态不挤压主操作。点击顶部 toolbar 的标题空白或任一操作都先暂停且保持暂停，只有点击游戏画面恢复；点击模拟器设置控件不能误恢复。EmulatorJS 原生底部工具栏启动后及靠近底边时始终隐藏；Retrom 的“模拟器设置”首次点击直接显示包含控制、显示、Core 设置、音量、静音和收起的自绘工具栏，桥接出来的原生设置面板与自绘栏均不存在 EmulatorJS 退出按钮。canvas rect 完全在 viewport 内，CSS/drawing-buffer 宽高比误差 ≤0.01，宽或高至少一边与 viewport 对应边误差 ≤2px，另一边按 contain 公式在水平和垂直方向居中，未被裁切或拉伸。
- 证据：三个 viewport 的布局测量、overflow 断言和页面截图。

### ACC-UI-006：管理侧 4K

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-UI-006`。
- 流程：在 `1280×800`、`2560×1440` 与物理 4K 150% 三个场景打开入库总览、新建导入、任务、待审核、历史、游戏管理列表与详情、游戏目录、用户管理、`/admin/bios` 和 `/admin/storage`；另断言已移除的 `/admin/bios/dats` 返回 404。
- 通过标准：表格/卡片密度可读，筛选和主操作可达；所有列出的管理页面中，可见按钮和链接的操作文案均不追加字面量箭头字符 `→`，且在同一 CSS viewport 下相对应用内容区的左、右间距分别一致，测量误差均不超过 1px。2560 CSS/物理 4K 150% 下历史 diff、任务阶段、BIOS hash 和容量九类不被截断或横向藏在视口外，Arcade BIOS 条目对比左右栏可读。运行依赖导航不存在 DAT 子项，BIOS 页说明 Arcade DAT 随 release 自动准备；容量分析紧跟运行依赖，统计范围说明保持只读，立即清理只作用于未引用类别并经危险确认。游戏目录页零数据时不自动打开 Drawer，页首/空状态的“一键创建推荐目录”与手动新建入口可达；请求中按钮禁用，成功/失败 toast 不推动布局。目录表按“游戏目录—游戏平台—联机—扩展名—游戏数—推荐运行方式”排列，扩展名与平台级已验证 payload 规则一致，名称列收窄后仍可读；联机列不显示状态文字，对命中当前精确 manifest 的 NES/FCEUmm、Arcade/MAME2003 Plus 显示带“支持联机”可访问名称的青绿色手柄，对 GBA/mGBA 显示带“不支持联机”可访问名称的灰色斜线手柄，且切换推荐核心后就地更新。1280 下没有页面级横向溢出；确需横向滚动的宽表只在带可见提示的局部容器中滚动，行首标识与行末主操作 sticky、键盘可达。游戏管理详情的发布信息/媒体/运行版本/管理操作四区在三个场景均可达；封面容器保持 3:4 并等比延伸到媒体内容底边，发布信息与媒体面板同高且媒体不能撑出左侧空白。
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
- 流程：创建两个 ImportJob，其中一个含 60 个 REVIEW_PENDING Item、另一个含 3 个；从任务页进入前者的待审核，以长短不同的 Validation/Blocker 文案检查目录和信息来源列对齐，加载第二页后选择第 57 项，点击一次“重新运行检查”并记录 popup 与 Preview 请求，再修改标题并等待实时保存，切换到第 3 项并返回。修改第 3 项草稿并等待实时保存，选择第 58 项后用浏览器前进/后退；Approve 第 3 项后再次直达它的旧详情 URL，再 Discard 第 58 项。随后打开纯文字审核历史，并从游戏管理对带共享/独占 payload 的游戏执行永久删除 Dialog，观察 `正在清理数据/清理完成/清理失败`；再在最近游玩、收藏与 Netplay 查看删除墓碑。分别在 1280×800 和补充的 3840×2160 CSS ultra-wide viewport 执行，并用键盘完成一次筛选和非顺序选中；该 ultra-wide 截图不得标记成物理 4K 150% 证据。
- 通过标准：既有队列范围、列对齐、cursor/预加载、3840 详情布局、四按钮、Preview、路由恢复、实时保存、决策与快速审批标准全部保持。审核历史响应与 DOM 不含图片、视频、媒体 URL或封面占位，只显示 ReviewEvent v2 的文字/结构化决定。永久删除 Dialog 展示精确影响计数、独占/共享容量、来源类型，要求完整标题且影响摘要变化时刷新确认；202 后详情不可再编辑/启动并显示清理进度，失败态显示固定错误码和重试，完成态没有恢复入口。最近游玩、收藏和 Netplay 的已删除游戏统一显示无封面墓碑，收藏仅保留移除操作，键盘焦点与屏幕阅读器可辨认状态且不依赖颜色。
- 证据：API query/cursor、route 序列、键盘 trace、决策前后队列 DOM、纯文字历史 DOM、永久删除 Dialog/进度/失败重试与关系墓碑 DOM，以及两个 viewport 的当前截图。

### ACC-UI-009：账户与用户管理全流程

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-UI-009`。
- 流程：在 `1280×800`、`2560×1440` 和物理 4K 150% 场景完成 setup、test login、邀请复制/注册、logout/login、管理员创建密码重置链接、重置、账户改密及管理员用户筛选/Drawer/角色/状态/删除；确认登录页不提供自助找回密码，账户资料只读且管理员不能代改 displayName/密码。覆盖空、loading、通用错误、429、ETag 冲突、本人和最后管理员状态；只用键盘重复邀请与 Drawer 流程并运行 axe。
- 通过标准：路由和表单符合 `ACC-AUTH-*`；secret 只在一次性对话框出现并从 fragment/状态及时清除；表格无页面级横向溢出，身份/操作列 sticky，Drawer/对话框焦点受控且关闭后返回触发器。危险确认包含用户名和影响，自身/最后管理员控件禁用并解释原因，错误/空/loading 不泄露旧数据或改变布局；测试模式有文本警告，密码/secret 不被辅助技术意外回读。
- 证据：三 viewport 当前截图、route/network/storage trace、axe/键盘结果与后端生命周期摘要。

### ACC-UI-010：快速审批预览、进度恢复与结果

- 上限：180 秒。
- 执行：`make acceptance-case CASE=ACC-UI-010`。
- 流程：在 `390×844`、`1280×800` 与物理 4K 150% 场景打开含 READY、截图 override、重复、active Attachment 和 stale Item 的审核队列，设置 `q/tagId/importJobId/pegasusImportId/platformInstanceId/blockerCode` 后仅加载第一页。只用键盘打开快速审批预览，先制造 preview stale 再确认；刷新带 `bulkApprovalId` 的运行页，取消一次并另建批次运行到含 skip/failure 的终态；注入 worker-only failure 后重试。覆盖零 candidate、已有 active batch 和网络错误。
- 通过标准：页首按钮与历史入口可达；预览来自服务端完整筛选而非当前 DOM，突出自动发布数并以非颜色文本逐类解释排除，截图 override 明确要求人工处理。stale 保留 Dialog、刷新数字并要求再次确认；零项禁用主操作，active batch 恢复其状态。运行卡在刷新/返回后按 URL 恢复，显示状态、processed/candidate、发布/跳过/失败/取消计数和稳定进度，不导致筛选/列表跳动；取消与 retry 只在合法状态出现并带确认/ETag。终态清除当前用户全部 `reviews:` sessionStorage 队列快照、刷新列表，最多首屏 50 条结果都有 Review/Game 链接和可读结果。三个 viewport 无页面级横向溢出；Dialog/status/results 的焦点、Escape、触发器返回、44px target、aria-live、reduced-motion 和 axe serious/critical 全部符合通用契约。
- 证据：三个 viewport 当前截图、URL/sessionStorage/network 序列、键盘/focus/axe trace 和最终结果 DOM。

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

### ACC-MDISC-005：双盘发布与 Player adapter 换盘契约

- 上限：600 秒。
- 执行：`make acceptance-case CASE=ACC-MDISC-005`。
- 流程：用确定性临时内容走 Retrom 多盘导入、发布与 Launch 投影，并对实际普通 `ejs-4.2.3-v3` Player adapter 执行 `0 → 1 → 0`、no-op、错误盘数和暂停保持测试。
- 通过标准：发布内容形成连续双盘 canonical playlist 与 `discSet`；adapter 读取 `diskCount=2`，每次切盘回读正确，no-op 不重复切换，失败不误改当前盘，busy/live 状态按契约收口。
- 证据：产品集成测试与 adapter 单元测试输出。本 Case 不宣称真实 Saturn ROM 已在浏览器中运行。

### ACC-MDISC-006：三盘与跨盘存档恢复契约

- 上限：600 秒。
- 执行：`make acceptance-case CASE=ACC-MDISC-006`。
- 流程：用确定性三盘 config 与实际 Player adapter 往返全部 index；以 `discIndex=1` 的显式 SaveState 驱动恢复状态机，并确认没有任何自动存档请求。
- 通过标准：adapter 只接受连续三盘集合；恢复严格先切到光盘 2 并回读，再显式 load state，之后恢复 main loop/start；失败保持暂停且单盘行为无回归。
- 证据：存档服务集成测试、adapter 与 restore 状态机测试输出。本 Case 不宣称真实 Saturn ROM 已在浏览器中运行。

### ACC-MDISC-007：能力、替换与共享 adapter 回归

- 上限：600 秒。
- 执行：`make acceptance-case CASE=ACC-MDISC-007`。
- 流程：检查 Saturn/yabause 与 PSX/3DO/PC-FX capability；验证省略 `contentMode`、默认核心影响、完整目录替换成功/失败、V2→V3→V3 bootstrap、关闭/重开 flag 对新建与既有任务/内容的影响，并运行共享 adapter 的多盘聚焦测试。
- 通过标准：只有能力交集暴露 MULTI；缺省始终 STANDARD；替换创建新不可变 revision且失败保留旧 current；artifact ID 不变、version 只递增一次、重放不改 updated time，既有 SaveState 绑定不变；flag 关闭只阻止新建/替换，不偷换冻结任务且已发布内容仍可运行。共享 adapter 的盘数、换盘和恢复测试全部通过。
- 证据：capability/flag 矩阵、替换前后 revision、bootstrap 行、在途/已发布行为和 adapter 测试输出。

### ACC-MDISC-008：授权、审计与私有数据隔离

- 上限：600 秒。
- 执行：`make acceptance-case CASE=ACC-MDISC-008`。
- 流程：匿名、普通 USER 和两个 ADMIN 探测全部新增管理 route；两个管理员复用同一幂等键提交不同主体请求。再让两个普通账号运行同一个多盘 Game，分别创建 SaveState 并尝试交叉 ID、cursor、幂等和 Launch 访问，最后停用其中一个账号。
- 通过标准：匿名为 401、USER 为 `ADMIN_REQUIRED`，ADMIN 写入保存真实 User actor，同 key 不跨 principal 串响应；两个 Profile 的盘号存档互不可见/不可写，跨账号探测不泄露存在性；停用只撤销目标账号 Launch，不影响另一账号。结果同时满足本次 `ACC-AUTH-006` 与 `ACC-ISO-001`–`003` route/owner inventory。
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
- 流程：1280×800、2560×1440、物理 4K 150% 覆盖 loading/全空/Folder 空/筛选空/错误/冲突、50 项批量和 100 Folder；键盘创建/管理/取消，reduced motion 与 axe。
- 通过：无横向溢出/遮挡，卡宽 270–320px，Rail 头部和新建入口固定且只有中间列表自滚动，批量栏不盖末行；游戏库与收藏页的收藏按钮在切换前后保持同一 38×38 容器和 18×18 居中心形，已收藏为红色实心、未收藏为空心；4K 字号/控件达标，dialog 焦点与 Escape 正确，axe 无 serious/critical。
- 证据：三 viewport 测量/截图、键盘 trace、焦点/ARIA/axe/reduced-motion 结果。

## 18. 游戏标签

### ACC-TAG-001：Migration、名称、生命周期与备份

- 上限：120 秒。执行：`make acceptance-case CASE=ACC-TAG-001`。
- 流程：从 033 fixture 升级和创建新库；覆盖 NFC、Unicode 空白/case-fold/control、40/41 code point、160/161 byte、活动同名、20/21 owner 与 1,000/1,001 实例上限；关联后软删除，再用同名创建新 ID，并完成带 Tag/关系/tombstone/审计的离线 backup/restore。
- 通过：034 表/列/partial unique/index/trigger、INTEGER 时刻、FK 与完整性正确；DELETED 不可恢复/改名/硬删，立即退出当前投影但历史关系和审计保留；同名新 ID 不继承旧关系；恢复前后标签快照逐项一致且 restore 安全围栏不退化。
- 证据：起止 migration/schema 摘要、名称/容量负向矩阵、删除前后 owner/version/关系、恢复前后 canonical hash 与完整性结果。

### ACC-TAG-002：API、权限、并发与游戏维护

- 上限：120 秒。执行：`make acceptance-case CASE=ACC-TAG-002`。
- 流程：ADMIN/USER/匿名覆盖 Tag CRUD/list/usage、常用标签补齐与 GameTag replace，验证 strict JSON、Origin/CSRF、If-Match、Idempotency-Key、cursor/filter；制造 rename、assignment、delete 版本漂移与同 key 重放；用 `q/tagId` 组合查询用户/管理游戏并检查审计。
- 通过：权限和稳定错误符合 OpenAPI；常用模板精确补齐 10 个领域标签，同名活动项复用、重复执行零新建，容量不足时整批零写入且每个新建项仍写 `TAG_CREATED`；同名、无效引用、删除确认与过期 ETag 都零部分写入；同集合 no-op 不推进版本；关系变化推进 owner/Tag version，删除令旧 owner 写冲突；rename/search 立即生效且不创建 MetadataRevision。
- 证据：请求/响应 contract matrix、ETag/idempotency/cursor 摘要、Game/Tag version 与 AuditEvent before/after/diff。

### ACC-TAG-003：普通导入、审核与原子发布

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-TAG-003`。
- 流程：普通 Import 选择默认标签并生成多个 Item，检查 config snapshot 与每个 ReviewDraft；逐项修改标签，删除一个打开中的 Tag，使用旧 ETag PATCH/Approve 后刷新；分别 Approve/Discard，并回归 duplicate/Validation/content identity 主链。
- 通过：不存在/已删除 Tag 时零 Import/Item/Consumption；每 Draft 正确继承且 reconfigure 只恢复仍活动项；旧 ETag 稳定冲突；Approve 的 Game/GameTag/ReviewEvent 原子，Discard 只保留关系和名称 snapshot；内容 identity、Variant 与运行依赖不受标签影响。
- 证据：Import config、Draft/Game 标签集合、删除前后 ETag、发布/丢弃事务行、ReviewEvent snapshot、content digest 与 Variant 对照。

### ACC-TAG-004：Pegasus Collection 默认标签

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-TAG-004`。
- 流程：多个 Collection 使用不同目录/标签，先把一个标签批量追加到全部未跳过 Collection，再逐项增删，覆盖 SKIP 空集合、20 个上限、mapping 保存/恢复/start 漂移、一次 retry 与审核 handoff；来源 metadata 同时含外部 tags/genre，并对已存在内容走 `SKIPPED_EXISTING`。
- 通过：批量操作只做去重 union、不覆盖已有选择，尚未选择处理方式的 Collection 后续选择 `IMPORT` 仍保留批次标签，`SKIP` 始终清空；每 Collection 关系与 `{tagId,name}` snapshot 稳定，Tag 删除推进 plan/mapping version且旧 start 冲突；handoff 只写一次 DraftTag，retry/crash recovery 不重复；外部字段不创建/猜测 Retrom Tag，SKIPPED_EXISTING 不改已有 GameTag；全部新候选仍先进入统一审核，之后只允许严格 READY 子集按 `ACC-IMP-009` 快速审批。
- 证据：mapping/plan ETag、Collection snapshot、Item/Draft/Game 关系计数、retry execution、外部 metadata 负向和 existing Game 对照。

### ACC-TAG-005：搜索、展示、响应式与无障碍

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-TAG-005`。
- 流程：管理员以键盘补齐常用标签并重复执行，再创建/重命名/删除标签，在普通导入、Pegasus mapping、审核和游戏维护入口选择；把 Pegasus Collection TagPicker 滚动到容器边缘后展开并滚动父容器，检查浮动 listbox 的挂载、跟随与上下翻转；在 Library/Admin/Review/Favorite/Recent/Save/Netplay 用名称及精确 Tag URL 搜索，访问详情；标准截图覆盖 390×844、1280×800、2560×1440、物理 4K 150% 和 axe，另保留 3840×2160 CSS ultra-wide 的无溢出检查。
- 通过：“添加常用标签”报告新建/已存在数量，列表展示完整模板且第二次执行报告全部存在；`q/tagId` 与其他条件取交集且刷新/前进后退恢复；删除标签立即隐藏，chip 位置、截断、`+N` 朗读和 FavoriteFolder/Tag 分区正确；TagPicker 键盘/20 上限、Drawer/Dialog 焦点与错误保留正确，listbox 在顶层浮动层内保持视口可见且不被任一滚动容器裁剪；所有页面零 document 横向溢出且 axe 无 serious/critical。
- 证据：route/network/DOM/键盘/focus trace、四 viewport 尺寸和截图、axe report、删除前后搜索/投影摘要。

## 19. 联机游玩

本节由 `ACC-NP-010`–`013` 维护协议、安全、feature flag 与单机回归，由 `ACC-NP-014`–`022` 维护项目自有 fixture 的真实双浏览器核心运行与生命周期基线。机器证据除第 3.3 节通用字段外，记录适用的协议/profile digest、参与者数、拒绝原因、资源计数与终因；敏感 cookie、能力值与宿主绝对路径不得进入证据。

### ACC-NP-010：协议与安全负向

- 上限：120 秒。执行：`make acceptance-case CASE=ACC-NP-010`。
- 流程：覆盖 foreign/null/missing Origin、错 AuthSession/player/generation、future/replayed input、2MiB+1 message、1MiB+1 state、坏 binary header/RASTATE/digest；同时保持另一独立房间运行。
- 通过：每项按封闭错误/终因拒绝，不泄密、不崩溃、不污染另一房间；严格 JSON 拒绝未知、重复、错型与过深字段。
- 证据：负向矩阵、close code/reason、独立房间前后 checkpoint。

### ACC-NP-011：重启、feature flag 与容量

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-NP-011`。
- 流程：运行中终止并重启后端；再关闭 flag；最后建立 17 个 active room。
- 通过：旧局以 `SERVER_RESTARTED` 收口且 Room/Launch 无假恢复，关联 ACTIVE PlaySession 变为 ABANDONED；正常 `USER_EXIT` 的关联 PlaySession 变为 FINISHED，两者均写入结束时间且不遗留 ACTIVE。flag off 隐藏导航并拒绝新 API；第 17 房稳定 `429 NETPLAY_CAPACITY_REACHED`，前 16 房不被驱逐。
- 证据：重启前后 DB、route/navigation、容量响应和前 16 房抽样。

### ACC-NP-012：2–4 人协议边界

- 上限：120 秒。执行：`make acceptance-case CASE=ACC-NP-012`。
- 流程：fake core/transport 分别锁定 2/3/4 occupied mask，注入乱序贡献并计算 4 人 hash；再打开真实首发房间，并用 205 个目录项检查 DRAFT 首页与向下滚动续页。
- 通过：空座始终 neutral，canonical 只在全部占用座贡献齐全时原子产生；4 人 digest 一致；真实 2P profile 的 P3/P4 显示不支持且不发 claim 请求。DRAFT 首次只取 100 项，未滚动不发续页，触底后每次只取一页且三页无重复遗漏；非 DRAFT 不读取游戏目录。
- 证据：三个 mask 的 frame/input/digest、network 零 claim 与房间截图。

### ACC-NP-013：普通单机回归与生产产物

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-NP-013`。
- 流程：普通受支持游戏从显式 SaveState 恢复、再次主动创建 SaveState、使用 Player controls、直接退出且不保存并结算时长；以一次联机历史渲染首页“再玩一次”并检查 Launch body；再让当前 Arcade schema v2 修订在 BIOS installation 变化后通过普通 Launch 自动重校验；最后检查 production web bundle。
- 通过：普通能力与联机改动前契约一致；联机历史的“再玩一次”只创建普通 single-player Launch，不携带 room/session 字段。当前 Arcade 修订按其锁定 DAT 与最新可用依赖重新生成 schema v2 后成功启动，不能报 `LAUNCH_CORE_VALIDATION_UNAVAILABLE`；联机模式专属禁用不泄漏到 single mode；production 产物不存在测试故障注入或可写 telemetry hook。
- 证据：普通 launch/save/play 断言、Player network/DOM 与产物扫描。

### ACC-NP-014：FCEUmm 双浏览器 rollback 产品链路

- 上限：240 秒。执行：`make acceptance-case CASE=ACC-NP-014`。
- 流程：从 `nes-smoke.nes` 的真实 upload、complete、Import、Review、Approve 建立 READY 游戏；`test/alice` 以 P1/P2 创建房间、选择 `fceumm-423-v1`、ready/start，各自取得 Launch 并打开两个真实 Chrome Player。记录两路 config/内容请求和 native state capture/load；对 P2 INPUT 注入 80ms 传输延迟并按下/释放一个测试程序实际读取的控制，再等待 rollback 与至少 frame 239 的同帧 checkpoint；最后由 P1 在 canonical 边界暂停并抓取两端画面。
- 通过：两路 config 锁定相同 revision/profile/artifact 且都没有 BIOS；ROM bytes 与 fixture SHA-256 一致。P1 authority state 经原生 fixed-point capture，P2 原生 load 完成且 core exact；延迟输入产生至少一次真实 rollback，回放后两端同 frame 的 core digest 一致，暂停后的 canvas SHA-256 一致，timeline retained count/bytes 始终不超过 profile 上限；无页面异常、意外 SESSION_ENDED 或存档请求。canvas 采样只临时隐藏覆盖其上的 Retrom 暂停/帮助 overlay 与开发服务器 Next 调试入口，不修改 iframe、core 状态或像素。
- 证据：导入/审核/房间/双 Launch ID，两个 context trace，config/内容摘要，state/rollback/checkpoint/retained 诊断 JSON、终态截图与 console/error 汇总。

### ACC-NP-015：FBNeo 双浏览器严格 lockstep 与局域网延迟基线

- 上限：240 秒。执行：`make acceptance-case CASE=ACC-NP-015`。
- 流程：用项目自有 FBNeo split fixture 的 test-only DAT，经真实 Child/Parent/BIOS 导入、审核、发布后选择 `fbneo-423-v1` 并打开 P1/P2 两个 Chrome Player。先运行至 frame 239，再对 P2 INPUT 注入 100ms 延迟并按下/释放程序实际读取的控制，观察输入缓冲升高；移除延迟，继续到至少 frame 719 后暂停并比较画面。
- 通过：两路 config 锁定相同 revision/profile/artifact，`parentUrl/biosUrl` 均存在且三路 bytes 与 fixture 一致；整个过程 predictionFrames 恒为 0、rollback 数为 0。缓冲从低延迟基线按 RTT 增大，恢复后仅在连续 120 个低目标样本后逐帧下降，最终低于峰值；至少 frame 719 的同帧 checkpoint core digest 一致，暂停后无遮挡 canvas SHA-256 一致，无页面异常或运行终局。测试 BIOS 不被目标驱动执行。
- 证据：test-only DAT 与三路内容摘要、双 context trace、两路 config、lockstep RTT/buffer/canonical/checkpoint 诊断 JSON、终态截图与 console/error 汇总。

### ACC-NP-016：后台节流与断线重连身份保持

- 上限：240 秒。执行：`make acceptance-case CASE=ACC-NP-016`。
- 流程：对 `fceumm-423-v1` 与 `fbneo-423-v1` 各执行一次真实双浏览器房间；运行后通过 Chrome DevTools Protocol 把 P2 页面冻结 3 秒再恢复，随后关闭 P2 当前 WebSocket transport 3 秒并允许客户端按租约重连。
- 通过：冻结期间不因 native frame/load 墙钟预算、缺少输入或 state transfer timer 异常结束；恢复后继续产生 canonical frame。transport drop 只暂停/重同步该 Session，不释放座位、不新建 Session/Launch；P2 在 lease 内以同一 room/session/participant 身份连接，收到严格更大的 epoch 后继续推进，结束回调计数为 0。两种 core 均无重复 controller、旧 generation 消息污染、页面异常或残留连接。
- 证据：两种 profile 的双 context trace、freeze/恢复时刻、connect/epoch/canonical/ended 诊断 JSON、drop 前后 Room/Session/Participant/Launch identity 与资源计数。

`ACC-NP-017`–`022` 共用严格 lockstep 产品矩阵：每个 Case 都从项目自有 fixture 的真实 upload、导入、审核、发布开始，先确认 manifest 把 profile 显式关联到预期 NES/SNES/Arcade 基础平台、房间 DRAFT 目录能按该平台列出游戏及 profile；再由 `test/alice` 两个真实账号与两个独立 Chrome context 创建房间和 Launch。验证 authority native state 捕获、target native load、1 MiB state 上限、每个 canonical input 恰推进一个原生帧，frame 119/239/719 的完整 core digest 默认必须一致；100ms RTT 使输入缓冲升高且恢复后下降、prediction/rollback 恒为 0。原生帧增长不是 1 必须阻断，不能容忍或用 checkpoint 投影掩盖。

唯一窄例外是 `ACC-NP-017` 的 SNES9x 在整个 Session 的任一合法 120-frame 检查点最多出现一次边界瞬时摘要差异；它只能在本局尚未消费该例外时进入产品既有的 hash-resync，双方还必须各自提供相同的 12 个 SNES 取证块且至少一个块确实不同。块名只用于定位原生 serializer 相位，不能成为排除或允许差异的白名单。还必须同时证明：两端各只有一个 `STATE_MISMATCH` PAUSE；epoch 恰加一且 `nextFrame=atFrame+1`；P1 authority capture 与 P2 load 的 state/core digest 相等；P1 原生归一化没有差异；P2 在真实 native load 前后 `changed=false`，并且 `byteExact/coreExact/nativeCompletion=true`、`firstCoreMismatch=-1`；新 epoch 的前两个合法 120-frame 检查点连续完整一致，期间没有第二次 mismatch、resync、ended 或页面错误。`changed=true`、一局第二次瞬时差异、任一持续差异、跨 epoch 搜索任意较晚摘要或排除任何 SNES bytes 都必须失败；不能按 epoch 重置例外额度。其他五个新 profile 不适用此例外。

随后冻结 P2 页面 3 秒，主动中断 transport 3 秒并在 lease 内重连；必须保持同一 room/session/participant/Launch、获得严格更大 epoch、不产生 ended，并在该 epoch 的首个合法检查点再次完整一致；SNES9x 只有在本局尚未消费上述唯一例外时，才能在此处消费一次并以新 epoch 连续两个完整一致的检查点闭环。最终暂停后的两端无遮挡 canvas PNG 必须逐字节一致，并分别包含超过 16 个 RGB 非零像素，禁止让“两端相同的纯黑画面”通过验收；全程不得有页面异常。每个 Case 的证据包含产品 upload/import/review/launch ID、房间平台/游戏枚举、两路 config/content 摘要、双 context trace、带事件序号/epoch 的 state、PAUSE、lockstep、checkpoint 诊断 JSON、终态截图与 console/error 汇总。

### ACC-NP-017：SNES9x 严格 lockstep 产品链路

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-NP-017`。
- 流程：使用 `snes-smoke.sfc` 和 `snes9x-423-v1` 执行共用严格 lockstep 矩阵。
- 通过：两路 config 精确锁定 `snes9x-wasm.data` 及同一 revision/profile/artifact，不带 Parent/BIOS；满足上述 state、checkpoint、缓冲、后台恢复、重连身份与画面收敛标准，包括全局检查点一致或整局唯一获准的 no-op hash recovery 完整证据；不得对 SNES core state 作 byte mask。

### ACC-NP-018：Nestopia 严格 lockstep 与 authority state 精确归一

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-NP-018`。
- 流程：使用 `nestopia-smoke.nes` 和 `nestopia-423-v1` 执行共用严格 lockstep 矩阵。
- 通过：两路 config 不带 Parent/BIOS。锁定 Nestopia 的 8-byte libretro 输入 trailer 以 `NST\x1a` 根块长度动态定位；authority、传输 bytes、target 重捕获与 checkpoint 只将该区间投影为零，根块、其他 padding、容器和长度仍全部逐字节一致。初始与断线重连的 native state load 均在该规则下通过，重连后的双方 checkpoint 再次一致；任何其他位置、形状、长度或 profile 命中都 fail closed，其余矩阵标准全部通过。

### ACC-NP-019：MAME 2003 override 严格 lockstep 产品链路

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-NP-019`。
- 流程：使用 `mame2003-423-override-v1` 和项目自有 Split Child/Parent/BIOS fixture 执行共用矩阵。
- 通过：两路 config 都带 `parentUrl/biosUrl`，精确锁定 4.2.1 `mame2003-wasm.data` override，三路 bytes 与 fixture 一致；测试 BIOS 不被驱动执行，其余矩阵标准全部通过。

### ACC-NP-020：MAME 2003 Plus 严格 lockstep 产品链路

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-NP-020`。
- 流程：使用 `mame2003-plus-423-v1` 和独立的 MAME 2003 Plus fixture/DAT 执行共用矩阵。
- 通过：两路 config 都带 `parentUrl/biosUrl`、精确锁定 `mame2003_plus-wasm.data`，三路 bytes 与 fixture 一致；测试 BIOS 不被驱动执行，其余矩阵标准全部通过。

### ACC-NP-021：FBA2012 CPS1 严格 lockstep 产品链路

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-NP-021`。
- 流程：使用 `fbalpha2012-cps1-423-v1` 和项目自有 CPS1 68000 程序/DAT 执行共用矩阵。
- 通过：两路 config 均无 Parent/BIOS、精确锁定 `fbalpha2012_cps1-wasm.data`；项目自有程序产生可见输入反应。重连的新 epoch 从 `nextFrame` 再执行至少 180 个 canonical frame 后才暂停取证，避免把真实启动/恢复流程中的短暂黑场误作终态；其余矩阵标准全部通过。

### ACC-NP-022：FBA2012 CPS2 Child/Parent 严格 lockstep 产品链路

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-NP-022`。
- 流程：使用 `fbalpha2012-cps2-423-v1`、项目自有 `spf2xjd` Child 和 `spf2t` marker Parent 执行共用矩阵。
- 通过：两路 config 均有 `parentUrl`、均无 `biosUrl`并精确锁定 `fbalpha2012_cps2-wasm.data`；Parent 无第三方 ROM bytes 且不被驱动执行，Child 程序可见反应于输入。重连的新 epoch 从 `nextFrame` 再执行至少 180 个 canonical frame 后才暂停取证，并执行共用矩阵的非黑像素断言；其余矩阵标准全部通过。

## 20. 移动端与横屏 Player

### ACC-MOB-001：App Shell 与导航

- 上限：120 秒。执行：`make acceptance-case CASE=ACC-MOB-001`。
- 流程：分别以匿名、USER、ADMIN 在 `320×568`、`390×844`、`768×1024` 和 `1024×768` 访问认证页、用户页、详情和管理页；打开/关闭 More、用户 Drawer 与管理导航，使用浏览器前进/后退。
- 通过标准：普通页面无 document 横向溢出和未授权内容闪现；手机五项底栏、平板 Drawer、管理全高导航及 active/context 正确；Sheet/Drawer 锁背景滚动、捕获焦点，Escape/遮罩/关闭按钮均可关闭并恢复触发器与原滚动位置。
- 证据：route/角色矩阵、DOM 宽度与 target 尺寸、焦点 trace、axe 结果和四 viewport 截图。

### ACC-MOB-002：用户核心路径

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-MOB-002`。
- 流程：在四个手机 viewport 完成登录、首页继续入口、游戏库搜索/条件 Sheet、详情、收藏和存档继续；分别注入 loading、empty 与可重试 error。
- 通过标准：筛选草稿取消不改 URL，应用后 query 可刷新恢复；`320–767px` 游戏卡两列且标题/主操作可触摸，详情 Launch Dock 不遮挡内容；收藏、继续启动、权限和 API 数据语义与桌面相同，三种状态均有明确恢复路径。
- 证据：URL/network trace、卡片列数/44px target/overflow 断言、关键截图和状态 DOM。

### ACC-MOB-003：收藏、存档与联机

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-MOB-003`。
- 流程：在 `390×844` 用键盘与触摸语义完成收藏 Folder Sheet、批量栏、两秒撤销、存档预览/删除确认，以及联机房间选座/准备/取消准备。
- 通过标准：Sheet 草稿、确认、undo、seat 和 room action 不被底栏或安全区遮挡；关闭后焦点返回触发器；收藏/存档私有隔离和联机禁用存档契约不变，全部关键动作可只用键盘完成。
- 证据：DOM/focus trace、API 请求序列、undo/确认状态和当前截图。

### ACC-MOB-004：管理完整流程

- 上限：240 秒。执行：`make acceptance-case CASE=ACC-MOB-004`。
- 流程：ADMIN 在 `390×844` 完成推荐目录补齐、导入配置、任务恢复、待审核筛选与详情四步锚点、一次逐项决定、用户全屏 Drawer、BIOS 条目对比和容量分析；USER 直达同一路由一次，并确认已移除的 DAT 管理路由返回 404。
- 通过标准：推荐补齐、手动新建与状态提示的触控目标均不小于 44px，零目录不会自动弹 Drawer，请求中防重复提交且 toast 可读；每个桌面表格行在手机有同字段/状态/主操作的卡片投影。来源、运行检查、发布内容、审核决定顺序与 Tab 顺序一致；autosave、ETag、阻断截图放行和逐项决策没有弱化；Drawer/确认可关闭并恢复焦点，USER 仍为应用级 403。
- 证据：Chrome trace、API/ETag 记录、四步/卡片语义断言、axe 和页面截图。

### ACC-MOB-005：横屏 Player

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-MOB-005`。
- 流程：在 `390×844` portrait 从真实用户点击启动并模拟方向锁拒绝，核对 config 后保持阻断；旋转到 `844×390`，运行后依次回竖屏/横屏，覆盖原本已暂停、页面隐藏与退出。
- 通过标准：portrait 可读/校验 config，但不存在 iframe、core/game/persistent/disc 大字节请求和 PlaySession start；方向稳定 250ms 后只装载一次。运行中竖屏释放输入并暂停，回横屏仅恢复方向门禁拥有的暂停，hidden 优先；退出先 unlock 再按 `returnTo` finish/revoke，Launch/Core/PlaySession 不重建。
- 证据：network/state/clock trace、pause ownership 断言、portrait 阻断和 landscape HUD 截图。

### ACC-MOB-006：Player 多盘与联机状态

- 上限：240 秒。执行：`make acceptance-case CASE=ACC-MOB-006`。
- 流程：在 `568×320`、`667×375`、`844×390`、`932×430` 检查 HUD、More 与光盘 Sheet并用确定性多盘夹具完成一次换盘；以聚焦 Player reducer/adapter 与房间组件测试分别驱动 P1/P2 的方向门禁、暂停和本地输入状态。
- 通过标准：HUD 高 48px、隐藏后揭示柄命中不小于 44px，安全区内无裁切，操作优先级为联机状态、光盘、存档且 overflow 不丢动作；More/光盘 Sheet 占满可用高度、覆盖 iframe 且不把 pointer/input 泄漏给游戏，触屏原生菜单入口不可见，左右虚拟控制区底边距为 70px。P1 承担全局 pause/resume，P2 只清本地输入并等待，状态转换与联机协议契约一致。本 Case 不证明真实双端核心执行、canonical frame 或 digest 收敛。
- 证据：四 viewport 尺寸/命中断言、DOM/network trace 与 P1/P2 pause/input 状态测试输出。

### ACC-MOB-007：可访问性与视觉回归

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-MOB-007`。
- 流程：在 `320×568`、`360×800`、`390×844`、`412×915`、`768×1024`、`1024×768` 运行用户/管理代表页，启用 200% 文本、`prefers-reduced-motion` 和测试 safe-area；再运行既有 `1280×800`、`2560×1440`、物理 4K 150% 基线。
- 通过标准：所有普通页面 document 零横向溢出、关键 target 至少 44px、标题/主操作可见，焦点与朗读顺序和视觉顺序一致；Dialog/Sheet 不被软键盘或 safe-area 裁切；axe 无 serious/critical violation，桌面侧栏、共享画布、卡片列数和 Player 比例没有回退。
- 证据：全部 viewport 的 DOM 尺寸与 axe 报告、键盘 trace，以及本次运行在证据目录生成的桌面截图与视觉核对结果；不得依赖仓库内设计图片或历史 screenshot diff。

## 21. 沉浸模式

本节 Case 使用 standard mapping 的可控 Gamepad 注入完成确定性自动化；注入必须作用于浏览器 `Gamepad` 消费边界并经过真实 DOM、HTTP、Launch、内容端点、Player 与 Core，不得直接调用组件 handler、伪造 Launch 成功或建立独立 EmulatorJS 示例页。自动化证据不等同于实体手柄兼容结论。

### ACC-IMM-001：入口与独立交互隔离

- 上限：90 秒。执行：`make acceptance-case CASE=ACC-IMM-001`。
- 流程：首页先用键盘/鼠标激活显式“进入沉浸模式”，取消后再注入任意 standard 手柄按键，使用左右、A、B 两次操作确认；再访问普通游戏库、收藏与管理页注入同样输入。
- 通过标准：首页始终有可聚焦显式入口，两种入口打开同一个覆盖整个 viewport 的电视确认层且不呈现 PC Dialog；首次弹窗默认“取消”，B 或取消后仍在首页；再次触发可用左右切换并以 A 确认进入 `/immersive`，方向+A 同帧仍执行切换后的选择。Dialog 存在时鼠标/键盘仍可用，关闭后 listener 完整释放；普通其他页面不响应自动沉浸入口，普通 PC/移动 App Shell 不被替换。原生全屏请求失败不阻断路由，恢复按钮只在非全屏且顶部 pointer 揭示期间出现。
- 证据：首页全视口确认层与沉浸首页截图、URL/焦点/Gamepad 事件 trace、全屏请求/恢复控件和普通页面隔离断言。

### ACC-IMM-002：入口与平台选择

- 上限：120 秒。执行：`make acceptance-case CASE=ACC-IMM-002`。
- 流程：以至少两个平台、收藏、存档和 PlaySession 的真实已发布游戏调用 destinations/platform API；其中通过正式导入与游戏资产接口在同一 Arcade 平台准备 3 款带项目自有封面的公开测试游戏。在 1280×720 与 1920×1080 使用手柄左右环绕、A 进入、B 触发退出确认。
- 通过标准：前四卡严格为全部游戏、最近游玩、收藏游戏、我的存档，零计数资料库卡仍保留，之后才出现有可见可运行游戏的平台；各自计数、最近时间与当前 Profile 数据一致，不泄漏另一 Profile 的收藏/存档/顺序。每卡最多三项 `featuredGames` 只使用 current MetadataRevision 封面。左右循环且 `320ms` 三卡位过渡方向正确；静态主次卡在缩放、透视、亮度和饱和度上有明确层级，位置数字和分段轨道同步。当前卡固定 `5:7` 封面堆最多三层，缺封面使用标题占位，每 `3s` 循环，换卡重置，隐藏及 reduced-motion 停止；`960×540` 到 4K 不溢出。底部方向/A/B/Select 底座与矢量图标符合设计，Select 圆形内以三个矢量点几何居中且不溢出；沉浸壳无普通侧栏、底栏、搜索、联机或管理入口；B 不静默丢失页面状态。
- 证据：API 响应、聚合断言、两 viewport 截图与完整手柄导航 trace。

### ACC-IMM-003：游戏媒体列表

- 上限：150 秒。执行：`make acceptance-case CASE=ACC-IMM-003`。
- 流程：在同一平台准备有 COVER+VIDEO+长 description、只有封面及无媒体的游戏，使用上下逐项浏览、左右每次快速翻 8 项并跨过 50 项分页边界，再以 B 返回平台页后重新进入。
- 通过标准：左侧标题选择与右侧媒体/描述同步；首项及其焦点描边不被顶部遮罩或滚动容器裁切，没有目录副标题的序号、标题和收藏心形在条目内垂直居中；COVER+VIDEO 并存时两容器高度测量误差不超过 1px；VIDEO 只为当前稳定选择在 700ms 后请求和播放，切走立即停止且页面最多一个 video；长 description 的 `scrollHeight` 大于容器且 `overflow-y:hidden`，顶部阅读停留后自动滚动到后续正文，到底停留并回到顶部，切换游戏重置顶部，隐藏时不推进，reduced-motion 下保持顶部静止且不截断辅助技术文本；缺失或错误媒体有稳定回退，分页无重复/漏项。返回后平台焦点恢复，重新进入游戏列表时行为确定。
- 证据：媒体端点 network trace、DOM/分页断言、有/无媒体截图与 video 数量断言。

### ACC-IMM-004：真实 GBA 单机游玩闭环

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-IMM-004`。
- 流程：项目自有 GBA fixture 经过真实导入/发布数据，从首页手柄进入平台和游戏，A 创建普通 Launch 并进入带 `experience=immersive` 的 Player，等待 mGBA Core 帧推进，菜单退出后回原平台游戏列表。
- 通过标准：config、ROM 和媒体只走受授权产品端点；Core canvas 非黑且帧推进；活动手柄索引从浏览页传给 Player；退出完成/撤销 Launch 与 PlaySession，回到原 Game 焦点；没有选择“创建存档”时不创建 SaveState。
- 证据：Launch/config/content/network trace、Core canvas/帧断言、Player 菜单和返回页截图、服务端完成状态。

### ACC-IMM-005：暂停菜单与输入隔离

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-IMM-005`。
- 流程：在真实 GBA Player 中依次发送单独 Select、Start、一次完整 chord、满足 `100/60/650ms` 的双 chord；菜单中用左右/A/B 取消，再次打开并创建存档，第三次打开后退出。
- 通过标准：单键按判定窗后到 Core；第一次 chord 作为前缀被抑制且不打开菜单；第二次 chord 先将全部本地输入归零、在 Core 暂停前发起运行帧截图并确认 Core 暂停，菜单严格为“取消、创建存档、退出游戏”且默认取消。取消只恢复本菜单拥有的暂停；创建存档复用暂停前截图并捕获暂停后的 state 一并上传，防止重复提交和暂停后黑帧，成功后可由当前 Profile 读取并恢复；不支持捕获时按钮禁用并说明。退出执行 finish/revoke，无粘键、无自动存档、无输入穿透。
- 证据：fake-clock 状态机输出、Player adapter/Gamepad 快照、pause owner trace、菜单截图与网络时序。

### ACC-IMM-006：Arcade 与多输入回归

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-IMM-006`。
- 流程：项目自有 Arcade fixture 经真实产品链进入 Core，注入两个 standard 手柄，验证投币/Start 与 P2 游戏输入，再仅用浏览时认领的活动手柄打开/操作菜单、退出，并在返回的游戏列表继续按上下键切换游戏。
- 通过标准：普通 Arcade 控制在菜单外到达正确玩家；非活动手柄不能驱动沉浸 UI，但其游戏输入不得被永久屏蔽。菜单打开期间两只手柄对 Core 都为全零，关闭后等待中立再恢复；退出后不显示虚假断连层，浏览器焦点归还当前游戏 option，已有原生全屏保持不变，原活动手柄无需再次认领，返回页出现后的首个上下输入立即移动到相邻游戏，A/B 仍经过 `120ms` 中立门且不能误启动；teardown 后没有粘键且原始 `getGamepads` 恢复。
- 证据：双手柄输入 trace、Core 非黑/运行截图、adapter 快照与 teardown 断言。

### ACC-IMM-007：状态、无障碍与视觉

- 上限：150 秒。执行：`make acceptance-case CASE=ACC-IMM-007`。
- 流程：覆盖手柄断开、页面隐藏、API 错误/重试、游戏删除和媒体漂移、`prefers-reduced-motion`，并在 1280×720、1920×1080、2560×1440 与物理 4K 150% scale 检查入口、平台、列表和菜单。
- 通过标准：断开/隐藏立即清零输入和待定 chord；隐藏/轮询暂停及 Player 返回不清除仍连接的活动手柄，不显示瞬时“等待手柄”，真实连续断连超过 `250ms` 后才进入重新认领。失败页可用手柄重试或返回，已删除游戏不能 Launch，媒体漂移只降级展示。服务端和客户端首屏时钟 HTML 一致且控制台无 hydration mismatch。所有视口零横向溢出、焦点清晰、文本可读，reduced-motion 无非必要动画，axe 无 serious/critical；沉浸独立壳不泄漏普通导航。
- 证据：四 viewport 截图、DOM 尺寸、axe 报告、错误/恢复和 visibility/disconnect trace。

### ACC-IMM-008：adapter、依赖与普通 UI 回归

- 上限：240 秒。执行：`make acceptance-case CASE=ACC-IMM-008`。
- 流程：校验普通 manifest/OpenAPI/registry 使用 `ejs-4.2.3-v3` 与 `ejs-4.3.0-pre-v2`，联机仍精确使用 legacy `ejs-4.2.3-v2`；运行 registry/manifest 双向校验、35 core 配置回归、普通桌面/移动 Player 与已登记联机产品用例。
- 通过标准：未知或版本不匹配 adapter fail closed；新普通 adapter 在非沉浸分支与前代行为等价且不安装过滤；联机 config/adapter 不接受沉浸参数。既有普通启动、存档、多盘、移动 HUD 和八个联机 profile 不回退。
- 证据：`data-check/deps-check`、adapter 单测、OpenAPI/schema 检查和既有产品 E2E 结果。

### ACC-IMM-009：资料库、标题首字符与默认收藏

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-IMM-009`。
- 流程：为两个 Profile 准备以数字、大小写 ASCII、汉字、符号和 emoji 开头的游戏；建立未分类收藏及自定义
  收藏夹，在全部/最近/收藏/收藏夹/平台范围跨过 50 项分页边界，并用 Y 收藏、取消、失败重试。
- 通过标准：每次 MetadataRevision 写入的 `title_initial` 严格为 `#|0-9|A-Z`；数字原样、字母大写、汉字
  使用锁定拼音首字母、其他为 `#`，改名的新 revision 在同一事务更新键。除最近范围外均按
  `titleInitial/title COLLATE NOCASE/gameId` 无重复漏项；最近范围只按本 Profile 最近时间倒序。收藏视图可
  切换本人 Folder，另一 Profile 的 Folder 统一不可见；Y 新增 Favorite 但不自动加入 Folder，取消同时移除
  memberships，服务端失败不乐观伪造成功且旧 cursor 被丢弃。收藏后红色实心爱心固定在当前游戏项最右侧，不落入标题下方。
- 证据：fresh schema/约束与 writer 集成断言、API 分页 tuple、双 Profile HTTP trace、收藏视图截图及 Y 输入
  trace。

### ACC-IMM-010：存档浏览、从存档启动与创建

- 上限：210 秒。执行：`make acceptance-case CASE=ACC-IMM-010`。
- 流程：为同一游戏创建两份不同时间的可用手动存档和截图，另一个 Profile 建立同 Game 存档；从“我的存档”
  浏览截图并选择旧存档启动，验证 Core 恢复，再从 Player 菜单创建一份新存档并返回列表。
- 通过标准：入口计数与列表只包含当前 Profile 未删除存档，游戏和存档均按契约排序；沉浸模式存档轨道中的每张“我的存档”卡片在截图托盘右下角显示不挤压布局的 payload 大小标签，使用 1024 进位并保留最多两位小数；截图走 Profile 私有
  no-store 端点，跨 Profile 读取失败且不泄露存在性。Launch config 的 stateUrl 精确指向所选存档，真实 Core
  恢复到对应画面；右侧不重复游戏名，上半区展示与其他游戏列表一致的封面/视频媒体舞台，并与平台游戏页保持相同约 62% 高度占比，下半区只展示从左到右排列的全部存档；存档轨道按下半区外层可用高度自适应，当前存档框宽明显大于其他存档框，切换后保持可见且框下时间完整落在容器内。截图加载成功且实际像素不为全黑/近全黑；不显示自动“手动存档 XXXX”名称，创建时间在每个框下方右对齐。Player 创建的新 state 与最大 `640×360`、质量 `0.75` 的 JPEG 预览原子可见且只属于当前 Profile。取消、退出、失败上传和不支持
  adapter 均不产生半份或自动存档。
- 证据：双 Profile API/network trace、存档截图/选择 UI、Launch config、Core 恢复帧、新 SaveState 数据与
  失败注入断言。

### ACC-IMM-011：BGM、两组音量与 Select 系统菜单

- 上限：150 秒。执行：`make acceptance-case CASE=ACC-IMM-011`。
- 流程：以可控 HTMLMediaElement、visibility、localStorage 和 Fullscreen API，从入口浏览到列表、Player、
  返回；模拟一次 autoplay 拒绝，再用 Select 打开菜单并逐项调整、静音，以 A 完成一次全屏成功、一次拒绝
  与退出；实体手柄 smoke 还必须在未替换 Fullscreen API 时完成同一路径。
- 通过标准：只请求站内 `/audio/immersive/insert-coin.ogg`，浏览时 loop/preload，自动播放拒绝显示可由 A/
  点击恢复的提示且不阻断导航；入口、任意平台和四类资料库之间的客户端导航保留同一音频节点、currentTime 与 playing 状态，不增加 pause/play 调用；隐藏、BGM 静音/零音量、进入 Player、退出或卸载立即暂停，返回可见浏览时
  尽力恢复，BGM 与 Core 音频不重叠。系统菜单顺序严格为背景音乐音量/静音、游戏音量/静音、进入全屏/
  退出；上下、左右 10%、A/B 与 live status 可访问。key `retrom:immersive:audio-preferences:v1` 只存四字段
  严格 payload，默认 40%/100% 且均不静音；非法或 storage failure 回退但本次内存设置可用。游戏组值应用到
  Core 音频；Chrome 标准手柄的 A 在同一输入处理链请求全屏，全屏成功和拒绝均如实反馈，拒绝不关闭菜单。
- 证据：media 调用序列、localStorage payload、菜单截图/axe、Gamepad trace、Player 音量和 fullscreen
  success/failure 断言。

### ACC-IMM-012：内容身份 URL 与私有存档缓存

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-IMM-012`。
- 流程：分别读取 current COVER/VIDEO、单 ROM、多盘其中一盘、BIOS 与 parent bundle；以完全相同 bytes
  重建一次，再以不同 bytes 依次替换并创建新 Launch；最后读取状态存档和截图，并尝试用旧 grant/另一
  Profile 读取。
- 通过标准：同一不可变输入得到同一 identity/强 ETag；媒体替换创建新 Asset ID/URL，ROM、多盘外部文件、
  BIOS 或 parent 任一有效 bytes/输出选项变化都产生新 `/runtime/content/` URL，任何已发 URL 的 bytes 不得
  原地变化。媒体为 public immutable，运行内容为受 grant 保护的 private immutable；旧/错误 identity 与
  无 grant 请求不泄露内容；同一内容的第二个 Launch 复用 URL，条件请求返回 304。Finish/撤销/硬删除后
  强制网络请求失败。SaveState state/screenshot 始终使用逻辑 ID、`private, no-store` 和 Profile/Launch
  限定授权，不因内容寻址进入共享缓存。
- 证据：替换前后 config/URL/ETag/cache header、相同与不同 bytes 对照、旧授权负向请求、双 Profile 存档
  trace。

实体 smoke 是发布必需的独立证据：至少使用一只 Chrome 报告 `mapping=standard` 的真实手柄完成“首页
Dialog→四类入口/平台→收藏或存档→选游戏→真实 Core→双 chord 菜单→创建存档→再次打开并退出”，并用
Select 操作一次系统菜单。报告只记录浏览器版本、OS、standard mapping、按钮/轴数量和结果，不保存设备
ID。没有实体设备时自动化 Case 可以 PASS，但沉浸模式发布验收必须标记 `BLOCKED`，不得把注入 E2E 写成
实体兼容通过。

## 22. RPG Maker 全世代产品链

本节 Case 必须通过“实际导入→审核选定版本核心→运行验证→发布→产品 Launch→Player”链路，不得使用独立 demo、旧 review preview 或 screenshot override 绕过产品代码。每次执行使用独立临时 SQLite/CAS、固定 Chrome 与当次 JSON/trace/截图。

自动验收入口统一为 `make acceptance-case CASE=ACC-RPG-NNN`。`ACC-RPG-001` 要求
`RETROM_ACCEPTANCE_BASE_URL`、`RETROM_ACCEPTANCE_USERNAME` 与
`RETROM_ACCEPTANCE_PASSWORD`；URL 必须为 HTTPS，只有 `localhost`/`127.0.0.1` 的隔离实例可使用 HTTP。
`ACC-RPG-002` 至 `008` 还分别要求
`RETROM_ACC_RPG_NNN_IMPORT_ITEM_ID`、`RETROM_ACC_RPG_NNN_VALIDATION_ID` 与
`RETROM_ACC_RPG_NNN_GAME_ID`。这些 ID 只用于定位当次实际产品记录；发布后 active Review 详情按产品契约返回
404，因此 runner 必须重新读取不可变的 APPROVED review-history event、Validation 与已发布 Game，
不得接受操作者提供 expected route、generation、gate 或位置结果。002 至 007 固定以
`testdata/public-roms/rpgmaker-smoke/` 对应公开项目逐文件计算 `RETROM_FILESET_V1` 并与服务端
`projectFingerprint` 比较；008 额外要求绝对路径 `RPG_MZ_SMOKE_ROOT`，只把 digest/文件数/总字节写入日志，
不得记录路径或复制项目。runner 随后必须新建一次与冻结 validation route/artifact 一致的 PRODUCT Launch，
用 Chrome 打开其真实 Player 并保存截图。缺少任一 live 输入、MZ 根或场景所需合法资源时结果为 `BLOCKED`，
不能记为 `NOT_APPLICABLE` 或退化成静态 fixture/API 检查。结构化产品证据写入 Case 目录的
`rpgmaker-product.json`，并内嵌到统一 `result.json.productEvidence`；Cookie、CSRF、Launch capability 和宿主
绝对路径不得进入这两个文件。

002 至 008 的统一 runner 还必须在同一浏览器 context 中创建第二个不带存档的 PRODUCT Launch，并在两次首个
可创建存档状态出现时冻结项目内容响应和固定 runtime asset Resource Timing 摘要。EasyRPG 两个 Launch 必须使用
同一稳定 project content identity，只取索引声明中实际使用的部分文件且首屏 project bytes 小于项目总字节；mkxp
必须只以 `206 Range` 读取不小于 4 MiB 的远程 `game.mkxpz`，不得出现项目 archive 的整包 `200`，读取 byte 小于
archive 总大小；两类恢复 Launch 均至少命中一个固定 runtime asset 浏览器缓存。MV/MZ 保持 unique runtime origin，
两次首屏的 native project 响应数都必须大于零且严格小于导入文件总数，不得枚举或下载整个项目。结构化证据只记录
Launch ID、计数和 byte，不记录 content identity、项目路径或资源名。

002 至 008 的 fresh 产品记录统一由
`node scripts/acceptance/rpgmaker_generation_provision.mjs ACC-RPG-NNN` 创建。该命令固定读取相应公开 fixture，
执行 Upload/Import/Review、14 gate、不同 restore Launch、PASS decision 与发布，并只在 stdout 返回三个 ID；
不得使用 ignored 临时 runner 或复用失败的 validation。004 仅在执行 270 MiB 与禁线程扩展边界验证时传入新的绝对
`RETROM_ACC_RPG_004_TRACE`；七核心最小闭环不要求该变量。008 还必须传入同次转换得到的 `RPG_MZ_SMOKE_ROOT`。provision 成功后，再把返回的
三个 ID 交给相同 Case 的统一 runner 生成正式 `result.json`。

`ACC-RPG-010` 与 `011` 不接受外部 fixture path 或预制 Import/Validation ID；runner 固定读取
`testdata/public-roms/rpgmaker-smoke/fixture-manifest.json` 锁定的 `negative-matrix/`、
`malicious-rpgmv/` 和 `malicious-rpgmz/`，在当次隔离实例中重新创建 `RPG_MAKER_PROJECT` Upload、Import、
Review 与所需 validation Launch。`negative-matrix/matrix.json` 必须精确声明 42 个非原版本组合、13 个
安全/歧义输入及 `7 generations × 5 archive formats × extension|magic = 70` 个内层 archive overlay；runner
先执行固定 Go detector 用例逐项证明 42 个内部 core mismatch；它们不是用户可选的产品操作，不得向虚拟
`rpgmaker` 目录伪造底层 core 选择。13 个安全/歧义输入与 70 个 overlay 再按声明逐项提交，任一缺项、额外项、
稳定错误码漂移或未进入真实产品链均失败，不得回退成静态 parser 结果。

`ACC-RPG-001` 默认只做只读 preflight 并返回 `BLOCKED`；只有对全新隔离数据根执行时才设置
`RETROM_ACC_RPG_001_MODE=APPLY`，允许 runner 实际调用“一键补齐推荐目录”并判定 PASS。不得在共享、非空或
正在进行其他验收的实例上设置该值。

世代运行 Case 的存档必须执行同一严格流程：原 Launch 以 `INITIAL_POSITION_RECORDED` 持久化可见状态 A，移动并改变 fixture 变量得到 B，在 B 创建服务端 checkpoint，继续移动/改值得到与 B 不同的 C，结束原 Launch，再由 checkpoint 创建 `launchId` 不同的 restore Launch。恢复后 bridge 回读的 `mapId/playerX/playerY/fixtureState` 必须逐字段等于 B，至少一字段不同于 A 和 C，恢复截图 marker 与 B 一致；随后必须继续真实输入并以 `RESTORE_INPUT` 持久化与恢复位置不同的四字段证据。刷新原 Launch 或 restore Launch 时必须从服务端最后序号和 gate 投影继续，网络中断重放只允许复用同一 eventId，页面刷新不得重复任何已完成 BEGIN/PASS。只有 RESTORE_INPUT 后进入 `AWAITING_DECISION`；只有 Blob/hash 一致、save/load API 成功、同 Launch 内回读或只看截图都不得 PASS。

世代 Case 的 `result.json` 必须包含 selected core ID、expected generation、nullable evidence generation、evidence confidence、content files digest、route key、runtime family、artifact ID/digest、adapter ABI、pack dependency digest、两个不同 Launch ID（不含凭据）、ready/frame/checkpoint/restore gate 及 A/B/C/restore 字段级断言。用户截图只显示 RPG Maker 版本核心名，不显示 EasyRPG、mkxp-z、native host、adapter 或 bridge。

### ACC-RPG-001：单一虚拟核心与七条内部路线

- 上限：180 秒。执行：`make acceptance-case CASE=ACC-RPG-001`。
- 流程：读取 catalog，在空库一键补齐推荐目录，查看用户目录/core 选择器与管理运行诊断。
- 通过标准：只有一个 `rpgmaker` 平台、一个用户可见 `rpgmaker` 虚拟核心和一个推荐目录；分别导入七个精确 fixture 时服务端选择七个不同内部 core/route，用户 UI 不出现内部 core 或底层实现，管理诊断仍可审计七条 route/artifact。
- 证据：catalog/API 响应、单一目录截图、七条检测映射与管理诊断截图。

### ACC-RPG-002：RPG Maker 2000

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-RPG-002`。
- 流程：通过唯一 `rpgmaker` 目录导入 2000 公开 fixture，由服务端选择 2000 内部路线并完成产品链和 A→B 保存→C→不同 Launch 恢复。
- 通过标准：expected/evidence generation=`RPG2000`、confidence=`MATCHED`、route=`RPG2000_EASYRPG`，engine gate 回读 RPG2k，Player 显示 `RETROM RPG2000`；位置/变量/截图精确回到 B 且不是 A/C。
- 证据：通用世代证据、engine profile 和 A/B/C/restore 断言。

### ACC-RPG-003：RPG Maker 2003

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-RPG-003`。
- 流程：通过唯一 `rpgmaker` 目录导入 2003 fixture，由服务端选择 2003 内部路线并完成产品链和 A→B 保存→C→不同 Launch 恢复。
- 通过标准：expected/evidence generation=`RPG2003`、confidence=`MATCHED`、route=`RPG2003_EASYRPG`，engine gate 回读 RPG2k3，Player 显示 `RETROM RPG2003`；精确回到 B，不得依靠共同文件名、自动 fallback 或 load success 判定。
- 证据：通用世代证据、engine profile 和 A/B/C/restore 断言。

### ACC-RPG-004：RPG Maker XP

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-RPG-004`。
- fresh 前置：执行 `node scripts/acceptance/rpgmaker_generation_provision.mjs ACC-RPG-004`。该 provision 只从仓库锁定的 XP
  fixture 走正式 Upload/Import/Review/Player 产品链，stdout 返回本次 `importItemId/validationId/gameId`；trace
  为可选扩展证据：设置新的绝对 `RETROM_ACC_RPG_004_TRACE` 时，以 `0600 + create-exclusive` 写入实际压缩
  checkpoint 的 payload/multipart 字节数、270 MiB+1 的 413 和 validation/restore 两次禁线程拒绝，不记录 cookie、凭据或输入路径。
  随后把三个 ID（以及执行扩展边界时的同一 trace）传给正式 Case。
- 流程：通过唯一 `rpgmaker` 目录导入 fixture，由服务端选择 RGSS1 线程 artifact，执行 A→B 保存→C→不同 Launch 恢复。可选扩展边界另验证 270 MiB 请求天花板和禁线程启动拒绝。
- 通过标准：route=`RPGXP_MKXP`、RGSS1/profile/marker 正确；Launch config 仍固定 256 MiB core serialize buffer，但实际 checkpoint 必须为大于紧凑信封且小于 256 MiB 的 `mkxp-state-compact` payload，服务端返回的 bytes/digest 与上传一致；地图/色坐标/变量逐项回到 B，恢复后可继续输入。提供扩展 trace 时，还必须证明 270 MiB+1 返回 413，并且 validation/restore 任一次报告非 secure context、非 `crossOriginIsolated` 或 SharedArrayBuffer 不可用时，都在签发 Launch credential/下载项目 payload 前稳定失败。
- 证据：通用世代证据和压缩 checkpoint payload 大小/哈希；提供扩展 trace 时再强制校验实际 payload/multipart 字节数、270 MiB 与禁线程负向结果。

### ACC-RPG-005：RPG Maker VX

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-RPG-005`。
- 流程：通过唯一 `rpgmaker` 目录导入 fixture，由服务端选择 `RPGVX_MKXP`/RGSS2 并执行 A→B 保存→C→不同 Launch 恢复。
- 通过标准：route/profile/marker 精确对应 VX；checkpoint 为小于 256 MiB 的 `mkxp-state-compact` payload；B 与 C 不同，恢复后地图/坐标/变量逐字段等于 B 且可继续输入。
- 证据：通用世代证据与 A/B/C/restore 断言。

### ACC-RPG-006：RPG Maker VX Ace

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-RPG-006`。
- 流程：通过唯一 `rpgmaker` 目录导入 fixture，由服务端选择 `RPGVXACE_MKXP`/RGSS3 并执行 A→B 保存→C→不同 Launch 恢复。
- 通过标准：route/profile/marker 精确对应 VX Ace；checkpoint 为小于 256 MiB 的 `mkxp-state-compact` payload；B 与 C 不同，恢复后地图/坐标/变量逐字段等于 B 且可继续输入。
- 证据：通用世代证据与 A/B/C/restore 断言。

### ACC-RPG-007：RPG Maker MV

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-RPG-007`。
- 流程：通过唯一 `rpgmaker` 目录导入固定 MIT fixture，由服务端选择 `RPGMV_NATIVE`，在 unique runtime origin 运行 300 连续帧，将 fixture 变量从 0 改为 B=1、创建 checkpoint、再改为 C=2，结束并从不同 Launch 恢复。
- 通过标准：MV route/profile/marker、帧、输入和音频 gate 通过；map/xy/变量/截图回到 B=1；普通 app origin 的 document/script/resource/cache 不存在游戏 JS 或项目资源。
- 证据：通用世代证据、两个 origin 的 network/DOM/cache 清单和 A/B/C/restore 断言。

### ACC-RPG-008：RPG Maker MZ

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-RPG-008 RPG_MZ_SMOKE_ROOT=<licensed-web-deployment-directory>`。
- 合法输入：官方 `nannnimo.zip` 只能从文档登记的 Gotcha Gotcha Games 下载地址取得并保留在 ignored 操作者目录，仓库不得提交或镜像。先用新输出目录执行：

  ```bash
  python3 scripts/acceptance/rpgmaker_mz_prepare.py \
    --source-zip <absolute-nannnimo.zip> \
    --output-root <absolute-new-retrom-mz-root> \
    --provenance-output <absolute-new-provenance.json>
  ```

  工具固定验证原 ZIP 的 size/SHA，安全展开唯一 `nannnimo/` wrapper，只删除根说明 PDF 和九个随包 save，保留
  `game.rmmzproject`，再注入 Retrom 自有 marker/变量插件；启动时直接进入官方 Map001 的 `(19,24)`，保留地图、
  NPC、角色与场景渲染，确认输入会同时推进角色横坐标与 fixture 变量，确保 B/C/恢复后输入都不是仅变量变化、
  静止画面或黑色背景上的 marker 覆盖层。它拒绝 traversal、symlink、路径碰撞、意外 exclusion、
  非空输出与原始 `plugins.js` 漂移；provenance 绑定十个删除项、两个注入文件及最终 fileset digest/count/bytes。
- fresh 前置：把上述新输出目录设为 `RPG_MZ_SMOKE_ROOT` 后执行
  `node scripts/acceptance/rpgmaker_generation_provision.mjs ACC-RPG-008`；stdout 返回本次实际产品链的
  `importItemId/validationId/gameId`。正式 Case 必须继续使用同一目录与同一 provenance，不能用旧 import、旧
  validation 或手工拼接的转换说明替代。
- 流程：将工具输出目录和 provenance 分别作为 `RPG_MZ_SMOKE_ROOT`、`RPG_MZ_SMOKE_PROVENANCE`，经 `rpgmaker_mz` 导入，在当前 `RPGMZ_NATIVE` unique origin 执行 marker、300 帧、输入/音频与 A→B=1 保存→C=2→不同 Launch 恢复。
- 通过标准：MZ generation/shape/route/artifact/bridge 精确绑定，转换 provenance 与实际项目逐字段一致，map/xy/变量/截图回到 B；恢复 PNG 必须含 marker 的精确 RGB，同时在排除固定 marker 矩形后仍达到独立的非黑像素和颜色桶下限，证明实际游戏地图可见；shape harness 或黑屏上的 marker 不能替代真实产品结果。
- 证据：只记录来源 URL/版本/SHA、转换清单与 files digest、engine version、route/artifact/bridge、Chrome 版本、gate 时长和结果，不复制项目，不记录内容/绝对路径/凭据。

### ACC-RPG-009：RTP/资源包

- 当前阶段暂停：在迁移后的 `ACC-RPG-002`–`008` 七核心最小产品链全部取得当次统一 PASS 前，009、010、011 均必须以 `RPG_SEVEN_CORE_MINIMAL_CLOSURE_REQUIRED` 返回 `BLOCKED`，不得消耗 live DB、浏览器或 pack 输入。以下流程在七核心闭环后恢复。
- 上限：300 秒。执行：`make acceptance-case CASE=ACC-RPG-009`。
- 合法输入：仓库不提交任何厂商 RTP。输入由 Retrom contributors 以
  `testdata/public-roms/rpgmaker-smoke/LICENSE` 中的 MIT 条款授权；先在仓库外的新绝对目录生成 Retrom 自有的确定性 1×1 PNG、目录、
  ZIP、7z 与 RPG Maker smoke 派生项目；生成器拒绝相对路径和已存在目录，`inputs.json` 同时记录每个输入的
  实际文件数、字节数与 SHA-256：

  ```bash
  python3 scripts/acceptance/rpgmaker_pack_inputs.py --output <absolute-new-acc-rpg-009-input-dir>
  ```

  输出的 `inputs` 具名覆盖 `rpg2000Rtp`、`rpg2003Rtp`、`rgss1StandardV1/V2`、`rgss1Custom`、
  `rgss2StandardV1/V2`、`rgss2Custom`、`rgss3StandardV1/V2`、`rgss3Custom`、`zeroReference`；
  `reviewProjects` 具名覆盖 2000/2003 的 `SelfContained/Missing`，以及 XP/VX/VX Ace 的
  `NoRtp/StandardAmbiguous/Custom`；`protectedPackInputs` 与 `protectedProjects` 只用于建立下述两个受保护前置。
- 专用实例前置：从 fresh DB/CAS 启动 Retrom；服务 ready 后使用同一管理员账号、固定 Chrome 和生成器的完整
  `inputs.json` 执行 dedicated provisioner。`--inputs` 必须是绝对常规文件，`--plan` 与 `--evidence` 必须是位于
  真实目录中的两个不同、尚不存在绝对路径；脚本先经只读 HTTP 要求 runtime-pack catalog、Game、Save、Review 和
  Import summary 全为空，任一非空即 fail closed，且失败过程不会生成 plan/evidence：

  ```bash
  RETROM_CHROME_EXECUTABLE="$(pwd)/.cache/tools/retrom-chrome-for-testing" \
  RETROM_ACCEPTANCE_BASE_URL=<fresh-instance-origin> \
  RETROM_ACCEPTANCE_USERNAME=<admin-username> \
  RETROM_ACCEPTANCE_PASSWORD=<admin-password> \
  .cache/tools/node-v24.18.0-linux-x64/bin/node \
    scripts/acceptance/rpgmaker_pack_provision.mjs \
    --inputs <absolute-acc-rpg-009-input-dir>/inputs.json \
    --plan <absolute-new-acc-rpg-009-plan.json> \
    --evidence <absolute-new-acc-rpg-009-provision-evidence.json>
  ```

  provisioner 只经正常产品 HTTP/UI/Player：先执行 `RUNTIME_ASSET_PACK` Upload/complete/install，只安装
  `protectedPackInputs.publishedVariant`（definition 必须为 `rgss1_standard`）与
  `protectedPackInputs.restorableCheckpoint`（definition 必须为 `rgss2_rpgvx`），并要求 catalog 恰有这两个
  installation；再经 RPG Maker Upload/Import/Review、完整 14 gate 与 approve 发布两个 protected project。VX
  protected project 还必须经 PRODUCT Launch、Player 创建存档，并用该 `saveStateId` 创建第二个 PRODUCT Launch、
  打开 Player，证明 checkpoint 可恢复。随后脚本导入 13 个 `reviewProjects`：五个 SelfContained/NoRtp item 运行完整
  原 Launch→checkpoint→不同恢复 Launch→reviewer PASS，但保持 `REVIEW_PENDING` 且不 approve；其余八个保持没有
  validation、没有 pack selection。结束前它重新核对恰有两个 PUBLISHED Game、一个可用 Save、13 个 pending Review、
  两个 READY installation 及其真实 Variant/checkpoint 引用，才以 `0600`、create-exclusive 方式写
  `schemaVersion=2` plan 与 provision evidence。该脚本没有 database 参数，不读取或写入 SQLite，也不得向 DB 注入 review、variant、save、
  installation 或随机零引用 UUID；若中途失败，丢弃该专用 DB/CAS 后从 fresh 实例重来，不能复用部分结果。
- provision evidence 为 `0600`、create-exclusive JSON，只保存去除 `sourcePath` 后的 generator input identity、同样去除
  路径后的 plan identity、13 个 Review 与两个受保护引用的真实 ID、计数，以及 provision 时的 Git commit/dirty
  文件相对路径摘要。它不得包含输入 bytes、绝对路径、口令、CSRF、cookie 或 capability；Case 通过
  `RETROM_ACC_RPG_009_PROVISION_EVIDENCE=<absolute-evidence-json>` 重新核对其与 plan、最终 DB 关系一致，并把文档
  SHA-256 与完整去敏 payload 写入最终 `databaseEvidence.provisioningEvidence`。缺少、篡改或与 plan 不同均失败。
- 参数：`RETROM_ACC_RPG_009_DATABASE` 是上述专用实例 SQLite 文件的绝对常规文件路径；
  `RETROM_ACC_RPG_009_PLAN` 与 `RETROM_ACC_RPG_009_PROVISION_EVIDENCE` 是绝对常规 JSON 文件路径。plan 必须
  `schemaVersion=2` 且只包含：
  `uploads`、`reviewIds`、`protectedReferences`。`uploads` 必须恰为上列 12 个具名 role，每项只能包含
  `sourcePath/sourceType/kind/generation/declaredName/sourceNote/sourceFileCount/sourceSizeBytes/sourceSha256`，值从
  生成结果的同名 `inputs` 逐字段复制；runner 会重新计算目录或文件 identity 并要求与生成清单逐字段相等；
  `reviewIds` 必须恰为上列 13 个具名 `reviewProjects` role 与其真实 import item UUID；
  `protectedReferences.publishedVariant` 只能包含 `installationId/gameId`，
  `protectedReferences.restorableCheckpoint` 只能包含 `installationId/gameId/saveStateId`。所有 ID 必须是互不混淆的
  canonical UUID；两个受保护 installation 必须不同。plan 不得携带预期 HTTP 响应、零引用 installation ID、
  路径摘要或凭据。最终证据只复制 plan 的去路径 identity，不复制原 plan 或 `sourcePath`。
- 流程：Case 先只读核验两个前置确为 `PUBLISHED/READY/availableForLaunch` 的真实当前 variant 与源自 PRODUCT
  Launch 的未删除 checkpoint。随后通过真实管理 HTTP 以目录和单 ZIP/7z 上传 12 个输入，等待每个
  `UPLOAD_FINALIZE` 终态及其 SUCCEEDED event，并等待 `RUNTIME_ASSET_PACK_VALIDATE` 的
  QUEUED→STARTED→SUCCEEDED events 与 READY catalog 投影；
  `zeroReference` installation 必须由本次 Upload/Install 新建。安装前对 2000/2003 missing 与三个 custom review
  执行空 selection PATCH，要求 `422 REVIEW_DRAFT_INVALID`，并要求 approve 返回
  `409 REVIEW_VALIDATION_STALE`；安装两份标准版本后，三个 Standard review 的空 selection 同样必须 422，随后
  对全部五个 missing/custom 与三个 ambiguous review 执行携带具体 slot/installation 的真实 PATCH，重新 GET
  必须看到 selection 改变且 validation 已 stale，approve 必须 409。五个已经 PASSED 的 SelfContained/NoRtp
  review 则必须由 Case 真实 approve 为五个 PUBLISHED game，不能以 GET 或同值 PATCH代替发布。
- 引用与删除：XP/VX 的两个 V1/V2 新 installation 必须与对应受保护 definition 相同、彼此及旧 files digest 不同；
  Case 对受保护 installation 删除必须得到 `409 RPG_RUNTIME_PACK_IN_USE`，且前后 files digest、状态与引用计数完全
  不变。Case 对自己新建的 `zeroReference` 先用错误 ETag 得到 412，再用当前 ETag 得到 204；最终状态必须为
  DELETED、pack file 行为零、bundle 引用清空。只读检查器随后等待该 installation 的唯一 UploadConsumption
  `PAYLOAD_RELEASE` 成功，核验 input snapshot digest、QUEUED/STARTED/SUCCEEDED 事件、
  `released_at_ms/UPLOAD_CONSUMED`、全部 upload file PURGED、完成 audit，以及原 bundle Blob 已进入尚未执行删除的
  retention GC candidate。任一步失败时浏览器只留下 `status=OBSERVED`，只有只读 DB 关系检查全部通过后 runner 才
  写 `status=PASS`。
- 通过标准：上传只消费 COMPLETE 且未消费的 `RUNTIME_ASSET_PACK` session，目录/归档经同一 10,000 文件、512 MiB、安全路径与确定性 files digest 门禁；Job 只从 VALIDATING 到 READY/FAILED，列表展示文件数、字节、来源说明、诊断与 Variant/checkpoint 两类引用数。只有 generation/name/version/files digest 精确唯一匹配才可 PASS；缺失/多义阻断发布；审核完整替换 slot selection 或仅对 2000/2003启用 self-contained override。被引用 installation 删除返回 `409 RPG_RUNTIME_PACK_IN_USE` 且行、文件与 Blob 引用不变；新版本只供新 binding 选择，不改写旧 variant/checkpoint 的 pack digest。零引用删除使用当前 ETag 返回 204、旧 ETag 返回 412，installation 保留 DELETED 审计形态，upload consumption 由既有 PayloadRelease 释放并进入保留期 GC。
- 证据：`rpgmaker-product.json` 只记录具名 role 的 upload/session/consumption ID、Job input digest/events/终态、
  definition/installation/files digest、真实 PATCH/approve/rejection、管理 UI 与审核 slot 截图、published variant 与
  restorable checkpoint 关系、409/412/204、PayloadRelease/audit/GC candidate、新旧 binding，以及上述去敏
  generator/plan/provision identity；不记录原 plan、资源绝对路径、资源 bytes、口令、CSRF、cookie 或 capability。

### ACC-RPG-010：版本选择与内容安全

- 上限：600 秒。执行：`make acceptance-case CASE=ACC-RPG-010`。
- 流程：Case 必须在没有并发管理写入的全新独立 SQLite/CAS 中执行。明文 localhost 拓扑必须让
  `http://{launchId}.rpg.localhost:<backend-port>` 直接到达 Go，不得把 runtime origin 指向 Next 端口；
  后者会把 `/__retrom/bootstrap` 当应用页面重定向到登录页，driver 必须以
  `BLOCKED/RPG_ACCEPTANCE_SECURITY_RUNTIME_ORIGIN_MISROUTED` 快速结束。应用 origin 必须使用
  `http://retrom-app.rpg.localhost:<web-port>`（或同属 `rpg.localhost` site 且解析到 loopback 的等价固定 Host），不能使用 `localhost` 或
  `127.0.0.1`；否则 `*.rpg.localhost` 与应用跨 site，浏览器不会在 entry 请求
  携带 `SameSite=Strict` capability。本地 driver 通过仅接受 `*.rpg.localhost`、只连接 loopback 的进程内代理提供
  确定性解析，不依赖操作者修改 `/etc/hosts`；driver 以 `BLOCKED/RPG_ACCEPTANCE_SECURITY_RUNTIME_SITE_MISMATCH`
  快速结束。若任一本次应接受的固定 fixture
  已被该实例导入，产品会返回 `alreadyImportedItemCount>0` 且不会创建新 Review；driver 必须在尝试
  Review/validation 前以 `BLOCKED` 和 `RPG_ACCEPTANCE_SECURITY_FRESH_DATABASE_REQUIRED` 结束，不能把零
  Review 降级为产品失败或复用旧 Review。七个合法 fixture 都只提交到 `rpgmaker` 虚拟核心，并逐项断言服务端检测出的 generation、内部 core 与 route；42 个跨世代内部 core mismatch 由 detector 的确定性矩阵覆盖，不再暴露成用户可选择的产品流程。随后提交 `.`/`www` 双根、多世代、RGSS 冲突、LCF 截断、case/NFKC/gencache 冲突、穿越、symlink、archive bomb、外链、确凿 Node/native 运行依赖，以及只携带但不被 Web 路径引用的 `.exe/.dll/.node/.bat`。
- 内层归档验证：矩阵在 2000/2003、XP/VX/VX Ace、MV/MZ 项目根分别加入有扩展与仅 magic 可识别的 ZIP/7z/RAR/TAR/gzip，共 70 项。每项必须经 Upload、Import、Review 和真实 runtime validation Launch；为避免 70 次重型 Player 启动，driver 使用 validation 创建响应设置的正式 capability cookie读取 Launch config，随后只读实际内容端点，并以 `clientSequence=0/previousInterval=null` 的正式 `finish` 事件安全结束 ACTIVE validation Launch。EasyRPG 必须同时从派生 `index.json` 找到逻辑成员并从 project endpoint 取回与 source SHA/size 精确相同的 bytes；RGSS 必须从 config 冻结的 `projectArchive` 内容端点下载派生 MKXPZ，先核验 archive SHA/size，再证明其中唯一 stored member 的名称与 bytes 精确匹配；Native Web 必须一次消费 bootstrap、从 unique-origin project endpoint 得到 404、调用 cleanup 撤销 capability，再结束 Launch。每项结束后重新 GET Review，要求 source `filesDigest` 不变；不得从本机 fixture 路径直接读取运行投影、绕过 content endpoint 或把创建 validation 当成运行证据。
- 通过标准：七个合法项目均由虚拟核心选择唯一正确的内部 generation/core/route；多世代或未知项目在创建 ImportJob 前拒绝。每个项目根内层归档只物化自身精确 bytes，`nestedEntryCount=0`，从未递归打开其中的 marker、路径或压缩内容，也不让这些内容参与世代/项目根判断。EasyRPG 原始项目端点或 RGSS 派生 bundle 携带该精确成员；Native Web 遵守最小 Web MIME 投影，非 Web archive 后缀和无扩展 magic sidecar 在 `/__retrom/project/*` 返回 404。未被 Web 路径引用的 native executable 文件仍逐 byte保留为 PROJECT_FILE 并进入 filesDigest，但 runtime endpoint 固定 404；只有确凿运行依赖以 `RPG_NATIVE_DEPENDENCY_UNSUPPORTED` 阻止导入。
- 证据：七条 virtual-core detection/binding 结果、42 个内部 mismatch 单元矩阵；70 项记录 generation/format/detection、source SHA/size/filesDigest、Import/Review/Validation/Launch/artifact/route ID、adapter kind、真实 content projection、派生容器 SHA、Launch FINISHED 及复查后的 filesDigest；另记录 opaque native 文件 SHA/filesDigest/404 矩阵和 13 项外层安全结果。机器证据不得包含本机 fixture 路径、bootstrap ticket、cookie/capability 或资源 bytes，只记录清单相对名、内容摘要和产品 ID。

### ACC-RPG-011：原生 Web 独立运行源隔离

- 上限：300 秒。执行：`make acceptance-case CASE=ACC-RPG-011`。
- 流程：分别运行项目自有 MV/MZ malicious harness，尝试 parent DOM/app cookie、top navigation、popup、form、external fetch、service worker、非 allowlist API、ticket replay/过期和 Host confusion；同时执行合法 render、MessageChannel、截图和 checkpoint。
- 通过标准：越权操作均被 browser/NG/Go 组合边界阻止，runtime Host 不接受 app session/API/HTML fallback，bootstrap GET 是唯一无凭据 GET 且 ticket 一次消费，CSP 含 `base-uri 'self'`；合法 bridge/render/save 仍成功。
- 证据：必须恰有一份 MV 与一份 MZ harness；两个 origin 的 CSP/cookie/Host/network/navigation/service-worker 记录、同 ticket 重放 410、已认证 capability reload 303→entry、MV/MZ 各自冻结的 route/artifact/adapter、14 gate、不同 Launch checkpoint 与独立 original/restore 截图；四个 original/restore Launch ID 必须全局不同，两个世代的截图文件名不得复用或互相覆盖。不得记录 bootstrap ticket 或 runtime capability。缺少 Chrome 输入时 runner 必须在启动浏览器前以 `BLOCKED` 结束。

### ACC-RPG-012：前向运行时升级与存档兼容

- 当前状态：`BLOCKED`。只有在出现第二个已完成七核心产品验证的稳定 `retrom-runtime` tag 后才启用；当前执行必须在读取数据库或启动浏览器前返回 `RPG_SECOND_RUNTIME_RELEASE_REQUIRED`。不得预建 previous/next route、向数据库注入历史构件、篡改存档绑定或保留旧 runtime bundle。
- 上限：300 秒。启用后使用专用 DB/CAS，按真实部署顺序完成两阶段产品链：先固定旧 tag 启动 Retrom，经 Upload/Import/Review/Player 创建至少一个 RPG Maker 或 ONS 游戏和真实 SaveState；停止服务后把同一 Retrom checkout 的唯一 manifest pin 升级为新 tag，运行正常 migration/bootstrap，再重启同一 DB。两阶段只能使用各自 tag 的真实 Release 资产和普通产品 API，不使用 acceptance-only artifact seeder。
- 兼容升级：新 tag 保持相同 `coreId/routeKey/gameCompatibilityLine`，并在 `readableSaveAbis` 包含旧 save ABI。升级后旧 Game 的普通 Launch 必须使用新 artifact；旧 SaveState 必须仍可由新 artifact 创建不同 Launch、恢复到保存位置并继续输入；新 SaveState 必须记录新 tag 的 adapter ABI 和 save ABI。旧 artifact 必须已退役且运行内容端点不可再访问。
- 破坏性存档升级：新 tag 仍保持相同 `gameCompatibilityLine`，但 `readableSaveAbis` 不包含旧 save ABI。升级后同一个 Game 的普通 Launch 仍必须使用新 artifact 并正常运行；旧 SaveState 保留在 `availability=ALL`，状态为 `BLOCKED`、reason=`SAVE_RUNTIME_INCOMPATIBLE`，不得出现在默认可恢复列表、首页恢复点、游戏详情可用计数或沉浸模式存档入口。用该 save ID 创建 Launch 必须返回 `422 LAUNCH_SAVE_INCOMPATIBLE` 且不创建 Launch；从新 runtime 创建的新 SaveState 必须可恢复。
- 游戏兼容线变化：若同一 core/route 的新 manifest 改变 `gameCompatibilityLine`，而数据库已有 READY 游戏 revision，bootstrap/readiness 必须 fail closed；发布流程必须把这种变化作为新逻辑 route/core 设计，而不能静默让既有游戏进入未知兼容性。
- EmulatorJS 不参与本规则，继续精确绑定历史 artifact；本 Case 不改变其回滚或历史构件保留策略。Retrom 不提供 runtime 回滚 UI/API，也不在升级后加载旧 RPG Maker/ONS bundle。
- 证据：两次真实 tag/commit/metadata 身份、升级前后 artifact 与逻辑兼容字段、旧 Game 普通 Launch 的当前 artifact、兼容旧存档跨 Launch 恢复及恢复后输入、破坏性旧存档的保留/过滤/422 无副作用、新存档恢复，以及旧 runtime 内容端点不可用。证据不得包含本机路径、Cookie、CSRF、Launch capability 或游戏 bytes。

### ACC-ONS-001：ONS 最小产品闭环

- 上限：300 秒。执行：`RETROM_ONS_SMOKE_ARCHIVE=<absolute-licensed-archive> make acceptance-case CASE=ACC-ONS-001`；同时需要公共的 `RETROM_ACCEPTANCE_BASE_URL/USERNAME/PASSWORD` 与 `RETROM_CHROME_EXECUTABLE`，基础地址必须为 HTTPS origin。
- 流程：经正式 Upload 创建 `ONS_PROJECT_V1` Import，等待唯一 Review；打开 ONS Review Preview，聚焦真实 canvas 并发送基本键盘输入，等待第 5 秒自动截图；审核通过并发布后创建 PRODUCT Launch，再次发送输入并从 Player 创建 `ONS_SAVE_BUNDLE_V1` 存档；关闭原页面，以该存档创建 ID 不同的第二个 PRODUCT Launch，等服务端 state 响应和 runtime restore ready，再发送恢复后输入。两次 Product Launch 都在首个有效 canvas 出现时冻结项目索引、内容响应和 runtime asset Resource Timing 摘要。
- 通过标准：五次截图前都必须证明核心已把 canvas backing buffer 从浏览器默认 `300×150` 设置为实际游戏分辨率、backing/display 宽高比误差不超过 `0.01`、canvas 相对 `data-ons-runtime-surface` 的横纵中心偏差各不超过 1 px，且 canvas 已持有键盘焦点。预览、Product 输入前后、恢复和恢复后输入共五张实际 canvas PNG 均为非黑有效画面；Product 输入前后及恢复前后输入的 RGBA digest 必须变化；自动截图后审核按钮才可通过；checkpoint payload kind、大小和服务器回执有效；原/恢复 Launch 不同；浏览器没有 page error、console error 或意外 dialog。项目必须包含至少一个不小于 4 MiB 的未访问文件，首个有效画面前只请求索引声明的真正在用文件，请求文件数和响应 byte 都严格小于完整项目且未请求全部大文件；两个 Launch 必须解析到同一稳定 project content identity，恢复 Launch 至少命中一个固定 runtime asset 的浏览器缓存。结构化证据只记录计数/byte，不记录文件名或 logical path。ScriptProcessor/WebGL 性能 warning 不作为失败。
- 证据：当次 `result.json`、`ons-product.json` 与五张 PNG。结构化证据只含非秘密产品 ID、payload kind/size、canvas backing/display 尺寸、居中偏差、焦点、非黑像素、RGBA digest、按需加载计数/byte和错误计数，不含归档路径、文件名、游戏 bytes、账号、CSRF、cookie 或 Launch capability。该 Case 只证明锁定 `retrom-runtime` tag 或显式本地候选与本次样本的最小兼容性；本地候选 PASS 只允许进入 runtime Release 流程，不能冒充固定 tag 的发布验收，也不扩大为全部 ONS 游戏兼容声明。

### ACC-KIRIKIRI-001：KiriKiri2 KAG 最小产品闭环

- 上限：300 秒。执行：`RETROM_KIRIKIRI_SMOKE_ARCHIVE=<absolute-licensed-kag-archive> make acceptance-case CASE=ACC-KIRIKIRI-001`；同时需要公共的 `RETROM_ACCEPTANCE_BASE_URL/USERNAME/PASSWORD` 与 `RETROM_CHROME_EXECUTABLE`，基础地址必须为 HTTPS origin或 loopback 验收 origin。
- 流程：经正式 Upload 创建 `KIRIKIRI_PROJECT_V1` Import，等待唯一 Review；打开 Review Preview，注入浏览器标准手柄，先证明 B 键产生取消语义，再用左摇杆驱动运行时可见虚拟指针到归一化坐标 `(0.5,0.34)`，以 A 键触发 smoke 固定的第一个 KAG 选项，等待离开并重新进入可保存标签及第 5 秒截图；审核发布后先创建独立沉浸模式 PRODUCT Launch，以两次 Select+Start 组合键打开 Retrom 退出菜单并核对三个动作，再创建普通 PRODUCT Launch，以同一手柄输入在第一个 KAG 可保存标签上从 A 到 B并创建 `KIRIKIRI_SAVE_BUNDLE_V1` checkpoint，再输入到 C；关闭原页面，以该存档创建 ID 不同的 PRODUCT Launch，等待服务端 state、KAG 标签就绪和书签恢复完成，确认回到 B后继续用手柄输入。两个普通 Product Launch 都在首个有效 canvas 出现时冻结项目索引、XP3 响应和 runtime asset Resource Timing 摘要。
- 通过标准：预览、沉浸模式 Launch 和两次普通 PRODUCT Launch 都运行锁定 KiriKiri2 core；标准手柄左摇杆/方向、A 确认和 B 取消通过 adapter 的可见虚拟指针生效，不能以 Playwright 直接鼠标点击或键盘输入替代；沉浸模式同一活动手柄的两次 Select+Start 必须打开包含“取消、创建存档、退出游戏”的 Retrom 菜单。canvas backing buffer不是浏览器默认 `300×150`，backing/display 宽高比误差不超过 `0.01`，相对 `data-kirikiri-runtime-surface` 横纵居中偏差各不超过 1 px且持有焦点。预览、A、B、C、恢复 B和恢复后输入截图均为非黑有效画面；A/B/C 与恢复后画面按输入发生变化，三个 PRODUCT Launch ID 互不相同。恢复判定只使用 B 与 C 之间发生变化的降采样像素，要求恢复帧到 B 的平均 RGB 距离严格小于到 C 距离的一半且至少有 100 个判别像素；不得因不属于存档状态的瞬时 UI 动画或重绘时序要求全画面 SHA 逐字相同。checkpoint 大小为 `1..64 MiB`，payload kind固定为 `KIRIKIRI_SAVE_BUNDLE_V1`。XP3 必须不小于 4 MiB且只产生 `206 Range`，首屏收到的 project bytes 严格小于索引声明的总大小，不能出现整份项目 archive 的 `200`，两个 Launch 必须使用同一稳定 project content identity，恢复 Launch 至少命中一个固定 runtime asset 的浏览器缓存；结构化证据只保留计数/byte，不保留项目路径。浏览器无 page error、console error或意外 dialog。
- 能力边界：即时存档是 KAG `saveBookMark/loadBookMark` 的语义存档，保存 `/save` 与 `/savedata` 的确定性文件集合，不是任意 KiriKiri/TJS 游戏的 Wasm 内存快照。无法找到 KAG API、首个可保存标签或唯一启动 XP3 时必须 fail closed，不能生成看似成功但不可恢复的存档。加密来源归档无法在不接收密码的情况下进入安全扫描，服务端以 `ARCHIVE_ENCRYPTED_UNSUPPORTED` 拒绝，验收将该操作者输入记为 `BLOCKED`，不能误记为 core 运行失败；操作者可在仓库外解密后提供新的合法归档。
- 证据：当次 `result.json`、`kirikiri-product.json`、六张 canvas PNG 与一张沉浸退出菜单 PNG。结构化证据只含非秘密产品 ID、菜单动作、payload kind/size、canvas 尺寸/居中/焦点、非黑像素、RGBA digest、按需加载计数/byte与错误计数，不含归档路径、文件名、游戏 bytes、账号、CSRF、cookie或 Launch capability。本地 `retrom-runtime` 候选 PASS 只允许进入 runtime Release 流程；Release 完成后 Retrom 必须解除本地链接、固定 tag/commit/assets并重跑本 Case。

## 23. 缺陷处理与重验

任一 Case 出现非预期行为即登记 defect，不能在原结果上直接改成 PASS：

1. 保存首次失败的 `result.json`、日志、trace 和截图；
2. 在最近可靠层新增回归测试，并证明旧实现失败；
3. 实施修复后运行聚焦回归测试；
4. 重跑原 Case；
5. 重跑受影响类别，最后重跑 `ACC-QA-001`；
6. 在 `defects.json` 记录 root cause、测试路径/名称、修复 commit 和两次 Case result。

runner 每次归档旧 `FAIL` 时会按 Case/attempt 将其确定性登记为 `OPEN` defect；已有 `attempts/` 中遗漏的失败也会在
下次执行该 Case 时补登记。存在 `OPEN` defect 时，runner 在调用产品 driver 前 fail closed，禁止静默重跑。操作者必须
提供绝对常规文件 `RETROM_ACCEPTANCE_DEFECT_RESOLUTION`，其 `schemaVersion=1`、`caseId`、
`rerunExplanation` 与 `defects[]` 必须完整覆盖该 Case 全部 open defect；每项精确记录 `defectId/rootCause/`
`regressionTest/redEvidence/greenCommand/fixCommit`，其中 regression test 必须指向仓库内真实文件，red evidence 必须
精确等于已归档失败 result，fix commit 为 40 位 commit。只有随后原 Case 真正 PASS，runner 才把 defect 更新为
`FIXED`、写入新的 `successfulResult` 并将去敏 resolution 保存到 Case evidence。产品重跑前 runner 会以 300 秒硬
超时实际执行每条去重后的 `greenCommand`，将 stdout/stderr、退出码与超时状态写入 `greenEvidence`；任一命令失败
则不调用产品 driver，重跑再次失败也不会关闭缺陷。`acceptance-report` 与 `ACC-QA-003` 对任何 open defect 或缺少
red/green/root cause/fix/result/rerun 映射、green command 非零或超时的记录均失败。
归档 result 的 `caseId` 必须与所在 `cases/acc-…` 目录精确对应，否则 defect 同步立即失败；操作输入缺失产生的
`BLOCKED` 不是产品 defect，不得登记或要求伪造修复 commit。

若错误只能在真实 EmulatorJS/Chrome 中出现，仍必须在最近确定性边界加自动化测试，并收紧实际 Retrom 产品 E2E 或 UI runner 断言。不得用“只能人工复现”免除固化，也不得新增绕过产品链路的独立 example 页面代替回归。

## 24. 最终通过标准

一期项目只有同时满足以下条件才可标记 `PASS`：

- 第 5–22 节所有 Required Case 为 PASS；
- 条件 Case 要么 PASS，要么有可核实的 `NOT_APPLICABLE` 原因；
- 没有 `FAIL`、`BLOCKED`、超时、缺失 Case 或未经解释的重跑；
- 当前明确登记的产品 E2E 与产品集成测试全部通过；[`core-runtime-validation.md`](./core-runtime-validation.md) 中未覆盖核心和 Saturn 真实浏览器运行缺口已如实列入最终报告，不得表述为已验证；
- `ACC-NP-010`–`022` 全部通过；八个 profile 的双浏览器结果只表述为锁定 profile/artifact 与项目自有 fixture 的产品链路基线，不得扩大为任意 ROM 或未登记 core 兼容性；
- `ACC-ES-001`–`006` 全部通过；两种目录结构、删除释放和真实 mGBA 游玩证据缺一不可，私有 Batocera source 不能替代公开确定性 fixture；
- `ACC-IMM-001`–`012` 全部通过，且当次实体 standard 手柄 smoke 通过；缺少实体手柄时必须报告 `BLOCKED`，不能删除临时方案或宣称沉浸模式发布完成；
- `ACC-RPG-001`–`012` 全部为当次 PASS；MZ 必须使用 `RPG_MZ_SMOKE_ROOT` 指定的合法真实 deployment，七个世代必须各至少一次在不同 Launch 精确恢复到 B 点并通过恢复后 `RESTORE_INPUT`；任一只有 API/hash/load success 证据的结果都不得通过；
- `ACC-ONS-001` 为当次 PASS，且必须包含真实导入、可见画面、基本输入、存档、不同 Launch 恢复与恢复后输入；
- 本次发现的每个 bug 均有回归测试和 red/green 证据；
- `make ci` 和两个镜像 build target 通过，且镜像构建没有启动服务；
- 最终报告记录 commit/dirty 状态、环境、Case 结果、缺陷、未执行项和残余风险；
- 报告不包含 ROM/BIOS、游戏截图内容以外的专有二进制、TLS 私钥、launch capability/cookie 或完整宿主路径；非秘密 launchId 只能用于关联 Case。

AI Agent 的最终交付摘要必须列出：总结果、失败/阻塞 Case ID、实际执行命令、证据目录、本次新增回归测试，以及任何 `NOT_APPLICABLE` 原因。不得仅回复“验收通过”。

## 25. 专题覆盖映射

| 专题 | 统一 Case |
| --- | --- |
| 工程质量与回归 | `ACC-QA-001`–`003` |
| 镜像、本地开发、NG/TLS | `ACC-PKG-001`–`003`、`ACC-DEV-001`、`ACC-NET-001`–`002`（`002` 为部署条件 Case） |
| SQLite、CAS、容量、备份、安全、API、运维 | `ACC-DB-001`–`002`、`ACC-CAS-001`–`002`、`ACC-STOR-001`、`ACC-BKP-001`、`ACC-SEC-001`–`004`、`ACC-API-001`、`ACC-OPS-001` |
| 游戏目录 | `ACC-PLAT-001`–`005` |
| 游戏管理 | `ACC-GAME-001`–`003` |
| 导入、Hasheous、审核、任务恢复 | `ACC-IMP-001`–`009` |
| 多盘导入、运行、回归与隔离 | `ACC-MDISC-001`–`008` |
| BIOS、服务器导入与 Arcade DAT | `ACC-DAT-001`–`006`、`ACC-BIOS-001`–`007` |
| Pegasus 目录导入与游戏视频 | `ACC-PEG-001`–`006`、`ACC-MEDIA-001` |
| EmulationStation 服务器目录导入 | `ACC-ES-001`–`006` |
| 启动、存档与游玩时长 | `ACC-RUN-001`–`012`、`ACC-SAVE-001`–`003`、`ACC-PLAY-001` |
| EmulatorJS 产品运行链路 | `ACC-RUN-002`、`ACC-RUN-006`–`012`、`ACC-PEG-006`、`ACC-ES-006`、`make web-e2e` 与 [`core-runtime-validation.md`](./core-runtime-validation.md) 登记的产品集成测试 |
| 账户认证、用户管理与隔离 | `ACC-AUTH-001`–`006`、`ACC-ISO-001`–`003` |
| UI、4K、待审队列与无障碍 | `ACC-UI-001`–`009`、`ACC-ES-005` |
| 移动 App Shell、响应式页面与横屏 Player | `ACC-MOB-001`–`007` |
| 收藏与收藏夹 | `ACC-FAV-001`–`004` |
| 游戏标签 | `ACC-TAG-001`–`005` |
| 联机协议、安全、真实双浏览器核心与生命周期 | `ACC-NP-010`–`022` |
| 沉浸模式独立 UI、资料库/收藏/存档导航、声音与系统菜单、真实单机 Player、内容身份缓存和输入隔离 | `ACC-IMM-001`–`012`，以及实体 standard 手柄 smoke |
| RPG Maker 七世代核心、项目导入、route/artifact、资源包、运行验证、原生 Web 隔离、跨 Launch checkpoint 精确恢复与恢复后输入 | `ACC-RPG-001`–`012` |
| ONS 项目导入、审核试玩、发布、基本控制、存档与不同 Launch 恢复 | `ACC-ONS-001` |
| KiriKiri2 KAG 项目导入、审核试玩、发布、基本控制、书签存档与不同 Launch 恢复 | `ACC-KIRIKIRI-001` |

本文列出的范围不包含 soak、压力或性能基准；未来若需要性能专项，应另建不阻塞一期功能验收的测试计划，不能把长时间运行 Case 混入本文。
