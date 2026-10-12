# FBNeo / Butterscotch 上游同步验收

本轮在 `fbneo-upstream-sync` PFB 同步两个已接入核心。下文保留候选阶段证据，并单独记录正式发行复验。Butterscotch 冷浏览器测试曾出现一次显示／输入异常和一次启动超时，后续复验正常，原因尚未确定；发布不将这些观察改记为已修复，也不声明全库兼容。

## 同步范围

| 仓库 | 原基准 | 同步后的上游 / 当前核心提交 | PFB 分支 |
| --- | --- | --- | --- |
| Retrom | `cf2cbf66e9dfac52270aaa12856a7c7202bf89d0` | 维护分支目录及本记录；无应用逻辑、DB migration 或 API 改动 | `codex/fbneo-upstream-sync` |
| retrom-runtime | `6e69dc029a8ecb912b4786992079c3d1fe4987b7` | 正式核心 pins、配套 DAT / report / 内容配对及公共声明快照 | `codex/fbneo-upstream-sync` |
| FBNeo | `f0f6f846537d7e72310834f5933df515df79e300` | libretro `6e7b7ea6c61a594fbaf2a2071dff326290fedcd1`；核心 `35ce6528f243ec537e5dc3a187d45c7ad073c768` | `feat/fbneo-upstream-sync` |
| Butterscotch | `24b281e86661897687c04fb88009a73c838cdda9` | upstream `0a36f376270087a229bf268591582af4760607fe`；核心 `f1d57b0b98fd7acc6ca6401f4d6a761b3e05f11e` | `sync/upstream-g0a36f3762700` |

FBNeo 合并 libretro 上游，保留 EmulatorJS 的 Emscripten 构建差异、固定 RetroArch 前端、内容加载回执及从实际 Wasm 导出的 required-only DAT。现有维护分支仍为 `retrom/gd5f072e147cf`，新发行前缀使用实际上游 `retrom-core-g6e7b7ea6c61a-rN`。

Butterscotch 将原有 14 个下游补丁重放到新上游（上游新增 60 个提交），新上游之后没有 merge commit。维护分支及 GitHub 默认分支已提升为 `retrom/g0a36f3762700`，Retrom 本 PFB 的依赖清单随之更新；旧维护分支保留，上游镜像不接收下游补丁。

实际发现并修复：

- Web 主循环遗漏了上游新增的 `textureFormat` / `surfaceFormat` 初始化，导致有逻辑和音频、画面全黑。按 WebGL 要求初始化为 `GL_RGBA`；独立浏览器回归使用仓库 Loritta fixture，修复前非黑像素为 0，修复后两帧均为 270,230、14 种颜色。回归已纳入核心 CI。
- 上游将实例角度改为 `GMLReal`，旧恢复代码仍截断为 `float`。保留完整精度，实际精度断言先红后绿。
- 新增物理资源及关闭绘制的状态尚不在 checkpoint v2 的序列化范围，明确阻止此时创建快照；空的房间物理槽数组仍允许。没有扩展存档协议或历史兼容分支。

## 候选阶段构建与产品证据

两核心均从干净提交显式构建。候选 Provider 为 `0.60.4-dev.2`，运行时 source tree 摘要为 `6f001aa6fb03365b74d34fc06f71d1d102219aadc84ab1b9c91066f6b2264aca`。浏览器通过实际 PFB URL 下载的核心字节与候选一致：

| 载荷 | SHA-256 |
| --- | --- |
| FBNeo `fbneo-wasm.data` | `4f0ea82f529eefbbbd4d97f347dcbd55655ba9dd7ccc9d20e4c4806e0885fafa` |
| FBNeo `fbneo-arcade.dat` | `b7192ff1aa025b8281979d96e12c8cf82e4df43a3afb81af6b668a4a7bbb90f9` |
| Butterscotch `butterscotch.wasm` | `5cdde2cf75140dfc8e47ca6ed89825f477c5af062e7ff8352c6b210c47ce235e` |
| Butterscotch `butterscotch-meta.wasm` | `5ed8b1a21b43e42fa6570126f960df72584f0b856262eec42594bde5d268c75c` |

| 检查 | 本轮结果 |
| --- | --- |
| FBNeo | 实际 Wasm、加载回执、DAT 配对、候选校验及 4 项打包测试通过 |
| Butterscotch | 源码约束、11 项 Web host 测试、原生 checkpoint / gamepad / lazy-read、Wasm 构建及真实 WebGL 回归通过 |
| runtime | lint、类型、构建、包和完整 Provider 候选校验通过；308 文件 / 1,775 测试通过 |
| Retrom | `api-check`、`backend-check`、`web-check`、真实独立 PostgreSQL `integration-test` 通过；Web 84 文件 / 375 测试及生产构建通过 |
| 标准浏览器门禁 | `ACC-RF-BROWSER` 四视口 24/24 通过，运行 ID `375740598acf4381`；此门禁使用 NES fixture 和第一版候选，不冒充七款游戏的最终核心测试 |
| PFB | 标准基座导入、build / up、validate 和最终 verify 通过；升级保留数据库、上传和 PFB ID |

真实游戏来自用户授权的本地素材，来源只读。全部走正常扫描、审核／试玩、发布、运行与存档页面。浏览器为 Chrome for Testing 149.0.7827.55，Node 24.18.0 / Playwright 1.61.1；标准手柄输入由浏览器测试注入，键盘由浏览器事件输入，不宣称测试了物理 USB 手柄或扬声器听感。

| 样例 | 实际验证范围 |
| --- | --- |
| 1941、恐龙快打 | CPS1 关卡内方向／攻击，公开存档，退出后不同 Run 恢复及继续操作 |
| 超级街头霸王 II | CPS2 Ryu / Guile 对战，保存、不同 Run 恢复及继续操作 |
| 拳皇 98 | Neo Geo BIOS、投币／选队／实际对战，战斗存档恢复及继续输入 |
| 三国战纪、怒首领蜂 II | PGM BIOS、投币／选人或选机、实际关卡，存档恢复及继续输入 |
| Undertale | 完整项目资源、开场／菜单／命名／房间移动、暂停；702,864 字节公开快照，只有登录 Cookie 的新浏览器恢复后继续手柄与键盘操作；另一个全新浏览器重复恢复及移动通过 |

Undertale 的公开 payload 经只读解包核对：姓名 `A`、房间 `room_area1`，保存及新浏览器恢复均为坐标 `(249,141)`，继续输入后为 `(186,108)`。没有注入或修改游戏内存／存档。状态证据摘要 `01e9dfe5e0485909c45c7452a10af879ad07eeddacc5f6a6ee2876d9580894cf`。开场的音频线程有持续非零 PCM；不保证瞬时存档能恢复原音频播放游标。

FBNeo 游戏最初使用 `dev.1`，最终 `dev.2` 的 FBNeo 核心字节相同；切换 Provider 后，之前的怒首领蜂 II 存档仍可恢复并继续操作。实际核心变化会改变对应核心指纹，此结论不扩展为旧核心版本的存档兼容承诺。

## 观察、影响与交付边界

DAT 从 8,243 个集合变为 8,424 个：新增 189、移除 8、修改 69。对本地可用的 12 个变更集合、18 份 ZIP 做中央目录 CRC / 大小匹配，发现 `bloodstm`、`revx`、`sailormn`、`valkyrie` 各有一份包在旧要求下完整、新要求下缺少必需项（分别 1、4、1、1 项）。这些包需要按新 ROM set 补齐；它不是全库解压校验或 8,424 款游戏实测。

Butterscotch 首次新浏览器恢复曾出现角色未绘制、输入未观察到的现象，但只读快照中的房间和坐标保持。另一次冷启动在 ready 前报告 `BUTTERSCOTCH_RUNTIME_TIMEOUT`。同一源码、同一载荷重试及后续全新浏览器恢复均正常；原生实际游戏恢复／移动，以及新旧 Wasm 的独立冷启动、OPFS 挂载和 Loritta 渲染对照均通过。尚未定位这两次观察的根因，不归咎于上游，也不记为已修复；后续仍需关注冷启动稳定性。

测试脚本中的过早投币、保存选择弹层等待及路径错误保留原始失败，不算产品通过；初始仅 data.win 的不完整项目、街机标题／选人／演示画面也不计入上表。最终证据均来自完整项目或实际关卡。首次候选与工具构建并行清理了 `release/`，之后按顺序重建通过；全量集成测试最初缺少显式 runtime / Provider 环境，补齐环境后的全量结果另存，未放宽门禁。

原始日志、截图、公开快照及证据索引保存在 PFB 忽略目录 `.pfb/evidence/fbneo-upstream-sync/`，索引 SHA-256 为 `18ab023a4d11bcd76df5026b234ea2569aae2a859e0f9e7a69f68e2145399f3a`；标准浏览器产物位于 `.artifacts/acceptance/375740598acf4381/`。素材、真实来源路径、凭据和二进制证据不进入 Git。

候选阶段使用显式开发输入，没有以旧正式 tag 冒充新字节。用户随后授权发布，核心正式资产已替换开发覆盖；正式发行记录见下。

PFB 实际 ID 为 `fbneo-upstr-57c9154ae1b9`，访问地址 `http://fbneo-upstr-57c9154ae1b9.localhost:3000`。基线 checkout 和其他 PFB 保持原状；标准命令更新了共享 PFB registry / gateway 与本 PFB 的 Docker 资源，复用既有浏览器工具及开发镜像。

## 正式发行记录

用户授权后，两核心的评审、维护分支提升和不可变 tag 已完成：

| 组件 | 正式版本 / 源码 | 发布流水线 |
| --- | --- | --- |
| FBNeo | [`retrom-core-g6e7b7ea6c61a-r1`](https://github.com/retrom-project/FBNeo/releases/tag/retrom-core-g6e7b7ea6c61a-r1)，源码 `35ce6528f243ec537e5dc3a187d45c7ad073c768` | PR #3、tag run `38221308471` 通过 |
| Butterscotch | [`retrom-core-g0a36f3762700-r1`](https://github.com/retrom-project/Butterscotch/releases/tag/retrom-core-g0a36f3762700-r1)，源码 `f1d57b0b98fd7acc6ca6401f4d6a761b3e05f11e` | PR #9、tag run `38220722628` 通过 |
| runtime | [`v0.62.0`](https://github.com/retrom-project/retrom-runtime/releases/tag/v0.62.0)，master `fca4138a53e446c9933c660df2909a0e73ac7841` | PR #136、tag run `38227220390` 通过 |

runtime 源码树摘要为 `da91ccdba2a132fdb5c0fa02c0b56f6d54ef9f3871cdb486aa5974dac19e2e08`；Retrom 使用标准 pin 命令逐一验证三份归档及来源闭包后写入 `data/runtime-inputs.json`，保留 master 的 Flycast r6 更新，不引入 DB migration 或 HTTP API 改动。源码门禁为 runtime 308 文件 / 1,787 测试，Retrom Web 84 文件 / 376 测试，另有 API、后端及独立 PostgreSQL 集成测试通过。依赖目录和正式输入检查通过。

FBNeo 正式 `fbneo-wasm.data` 为 9,222,008 字节，SHA `6cf6dcde0dd230415706d49e9b20ffcf3f58666cbd118e66c00d497ab5f5bfb8`；DAT 与候选相同。正式 CI 使用完整 Git checkout，Wasm 包含 `GIT35ce6528f`，归档压缩参数也与本地工作树构建不同，因此不能沿用候选二进制身份。已用正式核心重新游玩六款街机、保存、退出并在不同 Run 恢复，核对实际下载字节与输入。最初停留在标题或选队的记录排除，恐龙快打和拳皇 98 另留实际关卡／对战证据。Butterscotch 正式五项资产与候选相同；三个全新浏览器恢复 `room_area1` 的 `(249,141)`，手柄左移与键盘上移后为 `(186,111)`，并完成重新保存和新 Run 恢复。

正式 EmulatorJS Module SHA 为 `211d00fd3234a1925008938d0ab3c556433885b85ba1d4ff2eff924ec0bd1f9b`，retrom-runtime Module SHA 为 `0bec3e9e509f47def4b3a71e6061478c7f44bf4412915163ff6935f6322a4ce2`。完整发行输入摘要以 `data/runtime-inputs.json` 为准。候选 `0.62.0-dev.1` 与正式 `0.62.0` 的 110 个 Target 指纹及其显式核心输入逐项一致，只有部署身份随发行变化；该指纹对比不是 110 个 Target 的游戏实测。

浏览器门禁首次复跑 `45da7e31927b4851` 为 23 通过 / 1 失败：4K 150% 在退出全屏后过早判断布局，误等手机菜单关闭按钮。测试仅增加视口恢复及工具栏等待，保留四视口和原有全屏、输入、保存、新实例恢复断言；失败 trace 和诊断保存在 `release-defect-ledger.json`。这不是产品 UI 改动。修正后正式输入下完整 `ACC-RF-BROWSER` 运行 `a8de5d1e2ec3420a` 四视口 24/24 通过，输入摘要 `202349d3dd85858502b8dcd934137a29d811a937289fc3ec65f167ccf67d10d9`。

正式 runtime 安装后的七款游戏再次完成公开恢复、继续输入、保存和不同 Run 恢复；浏览器实际下载的两个 Module 与核心摘要均等于正式 Release。Undertale 三次独立冷浏览器恢复的坐标均为 `(249,141)`，继续输入后为 `(186,111)` 或 `(186,108)`。本次新建 FBNeo 存档由 `0.62.0-dev.1` 切换至 `0.62.0` 后仍可恢复，没有因为发行版本变化而失效。原始结果为 `published-runtime-game-results.json`，全部 Target 指纹对比为 `published-fingerprints.json`。两个外层执行会话在用例及清理完成后返回 143：游戏用例已逐项写入 PASS，标准浏览器 runner 已写入 `status=passed`；外层状态原因不明并保留在台账。独立追加一次 Undertale 冷恢复和退出复查返回 0。

## T01–T47 本轮追踪

此表仅追踪本次升级，沿用完整项目 Case 定义，不把全量自动测试等同于每个 Case 的全部产品验收。

| Case | 本轮证据与边界 |
| --- | --- |
| T01 | 新 PFB 数据库及集成测试建库通过；未改 migration |
| T02 | 后端／集成门禁通过；未另行逐表 introspection |
| T03 | 原有全量自动门禁通过；未另行遍历全部非法输入 |
| T04 | 新 PFB 初始化、登录正常；未改账号结构 |
| T05 | 未单独复验全部查询计划／关系索引 |
| T06 | 未单独复验目录删除 |
| T07 | Pegasus 实际导入七款样例；EmulationStation 未单独实测 |
| T08 | 来源始终只读；未删除来源做产品复验 |
| T09 | 全量集成门禁通过；未额外注入扫描中断 |
| T10 | 样例正常审核发布；取消／拒绝未额外实测 |
| T11 | 未单独复验清理扫描记录 |
| T12 | BIOS 手动安装通过；ROM 替换未单独复验 |
| T13 | 未单独注入软删除／清理故障 |
| T14 | 原有自动测试通过；未扩展收藏／标签产品测试 |
| T15 | 样例运行后最近游玩更新可见 |
| T16 | FBNeo 单 ZIP / BIOS、Butterscotch 217 文件项目实际运行；其余保留类型未扩大验收 |
| T17 | 样例按目录选择对应核心；未单独变更默认核心 |
| T18 | 上表七款限定存档链通过；保留冷浏览器异常观察，未证明全核心矩阵 |
| T19 | 正式核心候选到 runtime v0.62.0 后六款街机存档可恢复；110 个 Target 指纹一致，真实核心变更继续区分，无旧档迁移 |
| T20 | 标准浏览器／集成门禁保留；未对七款样例重复双标签覆盖冲突 |
| T21 | 未额外执行运行中替换内容 |
| T22 | 正常登录及 iframe / Worker / Wasm 内容加载，Butterscotch 非零 PCM |
| T23 | 原有权限自动门禁通过；未另行新增越权手测 |
| T24 | PFB down / import / up 后游戏、BIOS、存档保留；未清空 Redis |
| T25 | 未单独注入未同步草稿与 Redis 丢失 |
| T26 | 新浏览器恢复通过；未清空 Redis 验证此完整 Case |
| T27 | 正式输入四视口 24 项通过；七款实际画面在 1280×800 核对，未做全页面人工视觉验收 |
| T28 | 未执行 20,000 游戏／200 用户规模负载 |
| T29 | 原有自动门禁通过；未另行限流压测 |
| T30 | 实际审核试玩与发布正常；原有权限门禁保留 |
| T31 | 原有集成门禁通过；未另行注入文件退休清理故障 |
| T32 | 四个 PFB 仓库源自已记录基准；基线工作文件未修改 |
| T33 | 当前 PFB 显式构建核心、工具、候选 Provider 及应用；不执行其他 PFB 源码 |
| T34 | 保留现有 Host / runtime / 核心边界，无新增业务依赖 |
| T35 | 无 schema / 持久化模型变化；未逐字段重新审计 |
| T36 | 样例正常导入、审核、运行和保存 API；API 生成一致性通过 |
| T37 | 仅七款受影响样例及标准浏览器门禁；全平台矩阵仍为部分验证 |
| T38 | 核心元数据、维护分支、tag CI 与正式 runtime pins 已验证；版本和限定范围见正式发行记录 |
| T39 | 无导入模型变化；未单独扩大审计 |
| T40 | 两类游戏来源导入后进入统一审核页面 |
| T41 | 原有 CAS 自动门禁通过；未额外双管理页面测试 |
| T42 | 缺必需 BIOS 时页面阻止，补齐 Neo Geo / PGM BIOS 后真实启动 |
| T43 | BIOS 扫描无匹配不误装，随后手动安装；未覆盖完整中断／并发场景 |
| T44 | 游戏扫描与 BIOS 扫描均走正常进度流程；完整计数边界由原有自动门禁覆盖 |
| T45 | 全量集成门禁通过；未额外实测 BIOS 中断／重扫／并发 |
| T46 | 样例目录显式创建；没有目录 seed 或核心登记 migration |
| T47 | 本轮 Go / Web / runtime 门禁保持；新增核心 WebGL CI 回归，未降低生产规则 |
