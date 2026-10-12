# FBNeo / Butterscotch 上游同步验收

本轮在 `fbneo-upstream-sync` PFB 同步两个已接入核心。源码、构建和以下限定游戏链已完成验证；Butterscotch 冷浏览器测试曾出现一次显示／输入异常和一次启动超时，后续复验正常，原因尚未确定。保留这些观察，不声明全库兼容或正式发行验收通过。

## 同步范围

| 仓库 | 原基准 | 同步后的上游 / 当前核心提交 | PFB 分支 |
| --- | --- | --- | --- |
| Retrom | `cf2cbf66e9dfac52270aaa12856a7c7202bf89d0` | 维护分支目录及本记录；无应用逻辑、DB migration 或 API 改动 | `codex/fbneo-upstream-sync` |
| retrom-runtime | `6e69dc029a8ecb912b4786992079c3d1fe4987b7` | FBNeo 开发候选、配套 DAT / report / 内容配对及公共声明快照 | `codex/fbneo-upstream-sync` |
| FBNeo | `f0f6f846537d7e72310834f5933df515df79e300` | libretro `6e7b7ea6c61a594fbaf2a2071dff326290fedcd1`；核心 `35ce6528f243ec537e5dc3a187d45c7ad073c768` | `feat/fbneo-upstream-sync` |
| Butterscotch | `24b281e86661897687c04fb88009a73c838cdda9` | upstream `0a36f376270087a229bf268591582af4760607fe`；核心 `f1d57b0b98fd7acc6ca6401f4d6a761b3e05f11e` | `sync/upstream-g0a36f3762700` |

FBNeo 合并 libretro 上游，保留 EmulatorJS 的 Emscripten 构建差异、固定 RetroArch 前端、内容加载回执及从实际 Wasm 导出的 required-only DAT。现有维护分支仍为 `retrom/gd5f072e147cf`，新发行前缀使用实际上游 `retrom-core-g6e7b7ea6c61a-rN`。

Butterscotch 将原有 14 个下游补丁重放到新上游（上游新增 60 个提交），新上游之后没有 merge commit。拟用维护分支为 `retrom/g0a36f3762700`，Retrom 本 PFB 的依赖清单随之更新。本轮没有提升远端维护分支或发布核心 tag。

实际发现并修复：

- Web 主循环遗漏了上游新增的 `textureFormat` / `surfaceFormat` 初始化，导致有逻辑和音频、画面全黑。按 WebGL 要求初始化为 `GL_RGBA`；独立浏览器回归使用仓库 Loritta fixture，修复前非黑像素为 0，修复后两帧均为 270,230、14 种颜色。回归已纳入核心 CI。
- 上游将实例角度改为 `GMLReal`，旧恢复代码仍截断为 `float`。保留完整精度，实际精度断言先红后绿。
- 新增物理资源及关闭绘制的状态尚不在 checkpoint v2 的序列化范围，明确阻止此时创建快照；空的房间物理槽数组仍允许。没有扩展存档协议或历史兼容分支。

## 构建与产品证据

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

Butterscotch 首次新浏览器恢复曾出现角色未绘制、输入未观察到的现象，但只读快照中的房间和坐标保持。另一次冷启动在 ready 前报告 `BUTTERSCOTCH_RUNTIME_TIMEOUT`。同一源码、同一载荷重试及后续全新浏览器恢复均正常；原生实际游戏恢复／移动，以及新旧 Wasm 的独立冷启动、OPFS 挂载和 Loritta 渲染对照均通过。尚未定位这两次观察的根因，不归咎于上游，也不记为已修复；正式发布前仍需关注冷启动稳定性。

测试脚本中的过早投币、保存选择弹层等待及路径错误保留原始失败，不算产品通过；初始仅 data.win 的不完整项目、街机标题／选人／演示画面也不计入上表。最终证据均来自完整项目或实际关卡。首次候选与工具构建并行清理了 `release/`，之后按顺序重建通过；全量集成测试最初缺少显式 runtime / Provider 环境，补齐环境后的全量结果另存，未放宽门禁。

原始日志、截图、公开快照及证据索引保存在 PFB 忽略目录 `.pfb/evidence/fbneo-upstream-sync/`，索引 SHA-256 为 `18ab023a4d11bcd76df5026b234ea2569aae2a859e0f9e7a69f68e2145399f3a`；标准浏览器产物位于 `.artifacts/acceptance/375740598acf4381/`。素材、真实来源路径、凭据和二进制证据不进入 Git。

当前 runtime 使用显式开发候选，不以旧正式 tag 冒充新字节。后续正式集成需要先完成核心维护分支评审／发行，再使用真实 release pins 替换开发候选、构建正式 runtime 和 Retrom 配套；本轮不自动创建 PR、合入 master、打 tag 或 release。

PFB 实际 ID 为 `fbneo-upstr-57c9154ae1b9`，访问地址 `http://fbneo-upstr-57c9154ae1b9.localhost:3000`。基线 checkout 和其他 PFB 保持原状；标准命令更新了共享 PFB registry / gateway 与本 PFB 的 Docker 资源，复用既有浏览器工具及开发镜像。

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
| T19 | dev.1 到 dev.2 后 FBNeo 存档可恢复；真实核心变更继续由指纹区分，无旧档迁移 |
| T20 | 标准浏览器／集成门禁保留；未对七款样例重复双标签覆盖冲突 |
| T21 | 未额外执行运行中替换内容 |
| T22 | 正常登录及 iframe / Worker / Wasm 内容加载，Butterscotch 非零 PCM |
| T23 | 原有权限自动门禁通过；未另行新增越权手测 |
| T24 | PFB down / import / up 后游戏、BIOS、存档保留；未清空 Redis |
| T25 | 未单独注入未同步草稿与 Redis 丢失 |
| T26 | 新浏览器恢复通过；未清空 Redis 验证此完整 Case |
| T27 | 标准四视口 24 项通过；七款实际画面在 1280×800 核对，未做全页面人工视觉验收 |
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
| T38 | 核心元数据、CI、候选及清单一致；正式 release pins 与发行尚未执行 |
| T39 | 无导入模型变化；未单独扩大审计 |
| T40 | 两类游戏来源导入后进入统一审核页面 |
| T41 | 原有 CAS 自动门禁通过；未额外双管理页面测试 |
| T42 | 缺必需 BIOS 时页面阻止，补齐 Neo Geo / PGM BIOS 后真实启动 |
| T43 | BIOS 扫描无匹配不误装，随后手动安装；未覆盖完整中断／并发场景 |
| T44 | 游戏扫描与 BIOS 扫描均走正常进度流程；完整计数边界由原有自动门禁覆盖 |
| T45 | 全量集成门禁通过；未额外实测 BIOS 中断／重扫／并发 |
| T46 | 样例目录显式创建；没有目录 seed 或核心登记 migration |
| T47 | 本轮 Go / Web / runtime 门禁保持；新增核心 WebGL CI 回归，未降低生产规则 |
