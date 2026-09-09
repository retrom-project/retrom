# Provider Target 产品链路验证基线

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已实施 / 一期验收基线 |
| 版本 | 2.1 |
| 日期 | 2026-09-08 |

## 1. 证据边界

只有经过 Retrom Upload/Import/Review/Publish、创建 Launch、读取 Launch Envelope、由共享 dispatcher mount Provider、加载受限内容并在真实 Chrome 中产生帧和输入结果，才算产品链路证据。独立引擎页面、解析器、HTTP 200、静止 canvas、结构测试或 Provider 自测都不能替代产品证据。

每次执行证据必须记录精确 `providerId/bundleSha256/targetId`、当前内容 manifest、依赖 snapshot 与测试 fixture digest。Provider Target 之外不得再记录或选择内部路由、宿主适配器或旧式运行构件身份。

## 2. 已覆盖产品 Target

| Target/能力 | 入口 | 确定性输入 | 已证明边界 |
| --- | --- | --- | --- |
| `mgba` | `make web-e2e`、`ACC-RUN-002`、`ACC-PEG-006`、`ACC-IMM-004/005/010` | `testdata/public-roms/gba-smoke/` | 普通与服务器导入、审核发布、真实帧、手柄、显式 checkpoint 与跨 Launch 恢复 |
| `fceumm`、`nestopia` | `ACC-RUN-009`、`ACC-NP-014/016/018` | `testdata/public-roms/nes-smoke/` | 单机恢复、双浏览器输入、state transfer、rollback/lockstep、断线重连 |
| `snes9x` | `ACC-RUN-008`、`ACC-NP-017` | `testdata/public-roms/snes-smoke/` | 单机恢复、checkpoint 收敛、冻结和重连 |
| `mame2003`、`mame2003_plus`、`fbneo`、`fbalpha2012_cps1/cps2` | `ACC-RUN-006/007/010/011/012`、`ACC-NP-015/019/020/021/022`、`ACC-IMM-006` | `testdata/public-roms/arcade-smoke/` | DAT parent/BIOS 闭包、真实画面/输入、checkpoint、双浏览器与沉浸多手柄 |
| Saturn 多盘 | `ACC-MDISC-001..008` | 确定性临时 fixture | 导入、盘序、换盘事件和 checkpoint；不代表真实商业 ROM 兼容 |
| RPG Maker 2000/2003/XP/VX/VX Ace/MV | `ACC-RPG-002..007` | `testdata/public-roms/rpgmaker-smoke/` | 单一虚拟 Core 选择 retrom-runtime Target，真实地图/输入/音频、A→B→C、跨 Launch 恢复 B |
| RPG Maker MZ | `ACC-RPG-008` | 操作者合法输入 | 与 MV 相同的 unique-origin、场景、帧、输入和恢复；缺输入时 BLOCKED |
| ONS、KiriKiri、Butterscotch、TyranoScript | 各自 `ACC-*-001` | 操作者合法输入 | Review Preview、Product、按需内容、checkpoint 和跨 Launch 恢复；结论只覆盖当次样本 |
| PX68K / X68000 | `ACC-PX68K-001/002` | 操作者显式提供的游戏与 BIOS | 单磁盘导入/审核预览/Launch、标准手柄、独立键盘、音频、即时存档和跨 Launch 恢复；限当次样本 |
| TIC-80 | `ACC-TIC-001` | 项目自有 pmem 卡带 + 公开 `.tic` | 导入/预览/Launch、原生数据、新实例恢复与输入 |
| PICO-8 / FAKE-08 | `ACC-PICO-001` | 项目自有卡带 + 公开 `.p8/.p8.png` | 导入/预览/Launch、即时状态、新实例恢复与输入 |
| WASM-4 | PFB loose开发层产品Case | 锁定上游合法 cart | cart 校验、画面、输入、checkpoint、跨 cart 拒绝和清理 |

未列 Target 只有 schema、构建、依赖或相邻逻辑测试，不得声明浏览器产品兼容。没有合法可再分发输入时必须明确 `BLOCKED`，不能下载不明第三方内容补齐。

## 3. 共享验证规则

- 标准手柄最低能力是方向移动和确认；取消可选，缺少取消不能作为拒绝核心接入的理由。已有可靠的取消仍按对应 Case 验证。
- 同一映射配置中，一个手柄按钮只对应一个具体目标输入；禁止多个键或原生按钮与键盘同时发送，不能为补足确认/取消叠加 A+Enter、B+Escape。真实键盘保持独立；同一目标的按下/释放属于一个输入生命周期。宿主菜单 B 返回不受游戏内可选取消影响。
- Go 和 TypeScript 必须对同一 Launch Envelope fixtures 得出相同接受/拒绝结果。
- Provider Module 的 URL、SHA-256、Provider 身份、API 版本与 Bundle 必须一致。
- `runtime.capabilities` 必须与返回的 `PlayerRuntimeV1` 行为闭合；声明支持却缺方法、未声明却暴露行为均失败。
- checkpoint 只按 format/size/hash 处理，Host 不解析字节；恢复必须由 Target `readFormats` 明确允许。
- content、BIOS、parent、多盘、pack、unique-origin 与 netplay 资源必须全部来自 envelope grant。
- 所有测试必须证明退出/失败/卸载会清理 Provider、撤销 Launch 并停止输入与帧回调。

## 4. EmulatorJS 特殊边界

EmulatorJS Provider declaration 是 44 个 Target 的唯一行为 registry。`mame2003` 的 4.2.1 core 覆盖、DOSBox Pure 的 state 修复、线程 core、shader、启动动作、多盘和八个 netplay profile 都封装在该 Provider 中。Retrom 只看 Target declaration 与标准能力，不按 core 名在 Go 或前端复制规则。

新增 `fuse`、`gearcoleco`、`prboom`、`puae`、`vice-x128`、`vice-x64sc`、`vice-xvic` 与 `virtualjaguar` 只声明 `SINGLE_FILE`，`discSwitch=false` 且不开放 netplay。逐核产品验收按 [ACC-RUN-013](./project-acceptance.md#acc-run-013八个-emulatorjs-单文件候选的逐核产品验证) 执行；首次验收可从产品白名单任选一种扩展名，一个 Target 的结果不能替代另一个 Target。

Mega Drive 的 Genesis Plus GX、GX Wide 与 PicoDrive 由 Provider 在输入表建立前明确选择 Mega Drive 手柄布局，保留 Start、方向与 A/B/C/X/Y/Z；不能采用多平台核心自动推断出的 Master System 布局。键盘与标准手柄使用同一控制表，原始 `.md`/`.smd` 与归档内成员行为一致。固定 EmulatorJS 4.2.3 的六键布局使用等价的 `segaCD` 输入别名，4.3.0-pre 使用 `segaMD`；这只选择输入布局，不切换运行核心或内容类型。

EmulatorJS 构造期间已检测到的手柄，在控制表就绪时补齐空闲玩家分配，保留已有分配且不重复绑定；后续插拔继续使用 EmulatorJS 原有事件。不能要求启动前已连接的手柄重新插拔才能操作。

PSP 优化的定向验证应使用操作者提供的合法样本，通过真实 Review Preview、Product Launch、新建 checkpoint、不同 Launch 恢复和恢复后输入检查。4K、150% 缩放使用 `2560×1440` CSS 视口与 `deviceScaleFactor=1.5`；必须同时检查窗口变化、全屏、设置面板、完整画面与边缘内容。帧率对照应记录相同游戏场景、核心配置与 GPU renderer，软件渲染结果不能推断为实体显卡性能。压缩收益以该次存档压缩前后完整字节数计算，不以不同场景的两份存档相除。

Provider 私有的 PSP 存档读取必须等待原生异步序列化结束，期间不得恢复主循环造成 Asyncify 重入；只释放原生数据 allocation，不释放借用的描述符。压缩格式、画布上限和固定核心版本由 Provider 自己声明，Host 不增加 PSP 分支。

指定存档不能在首帧盲目自动加载；Provider 必须等待目标核心可序列化，再执行原生 load 并以明确失败 fail closed。普通开始必须清理浏览器遗留的隐式目录存档，只有用户点击“创建存档”才上传显式 checkpoint。

Dreamcast 通过 `emulatorjs/flycast` Target 接入 nasomers/flycast-wasm 的 WASM JIT，由
`retrom-project/flycast-wasm` 固定源码构建。首期只接受单文件 `.chd`，使用 WebGL2、
640×480、无 pthreads；Windows CE/MMU、NAOMI、Atomiswave、多盘与联机不在支持范围。
BIOS 使用安装快照中的 `/dc/dc_boot.bin` 与 `/dc/dc_flash.bin`，关闭 HLE BIOS。
标准手柄的 A/B/X/Y 按 Dreamcast 物理位置绑定，方向、摇杆及 L/R 扳机由标准输入表传递。

Provider 在 OPFS 按完整 SHA-256 缓存 CHD，每次命中重新流式校验长度和摘要；不支持 OPFS
或写入配额不足时回退到经过同样校验的内存 Blob。缓存只保存游戏字节，不保存 Launch URL
或授权信息。新 Launch 仍须取得当前 envelope grant；清除站点存储会重新下载。
即时存档写入独立的 `flycast-state-gzip-v1` 格式，完整状态无损压缩后上传；恢复时按声明格式解压，
压缩前后均遵守 Provider 的大小上限，并继续读取旧 `flycast-state-v1` 存档。恢复等待核心启动完成。
Flycast 的 iframe 在创建 WebGL 上下文时保留绘图缓冲区，避免浏览器呈现后清空缓冲区，
使暂停后的 Canvas 截图仍可读取最后画面；退出时恢复该 iframe 的上下文创建方法。
操作者语料的验收规则见 `ACC-FLYCAST-001`；单个样本结果不能外推为 Dreamcast 全库兼容。

## 5. retrom-runtime 特殊边界

WebMSX 使用独立 `msx-webmsx` Target，固定 MSX2+ 日本机器，接收单媒体 Blob。
`webmsx-state-v1` 是绑定游戏摘要的有界即时快照，须通过 `ACC-MSX-001` 的新 Launch 恢复、
位置/形状保持与恢复后输入断言。截图、审核预览、发布和运行复用公共路径。
本次先验证所选 MSX1/MSX2 卡带；单个样本不能证明全部磁盘、磁带、turbo R 或多盘软件兼容。

Flash 使用独立 `flash-ruffle` Target。只接收原始单 SWF，SharedObject 是游戏原生存档（`GAME_SAVE`），
不承诺即时执行快照。`dataKind: STORAGE` 表明其为原生数据容器，可能只有设置或计数，不保证可恢复进度。
按 `ACC-FLASH-001` 验证自有确定性程序的原生保存、新 Launch 恢复、继续输入与空启动，
并通过真实公开游戏的导入/预览/发布/启动检查兼容性。GPU 画布截图必须走核心重绘捕获接口，页面截图不能替代
存档截图能力。未验证的 Stage3D、外部资源、联网和其他 Flash API 不纳入兼容性声明。

RPG 世代检测只选择 `retrom-runtime` Provider 内的 Target；用户仍只看到一个 RPG Maker Core。EasyRPG、mkxp、Native Web、ONS、KiriKiri、Butterscotch、TyranoScript 和 WASM-4 的文件策略、bridge、OPFS/Range、输入和 checkpoint codec 都属于 Provider 私有实现。

Native Web 必须使用每 Launch unique origin，拒绝应用 cookie、普通 API、跨 Launch 项目和 ticket 重放。审核试运行复用普通 Preview/Player 与同一 Provider Module，不包含专用证明协议或发布前置。严格的帧、输入、音频、A/B/C、checkpoint、跨会话恢复与截图断言只由研发验收驱动普通产品操作并从自有 fixture/普通存档读取，规则见项目验收专题。

## 6. 升级验证

Provider 升级必须在同一数据库上顺序启动旧版与更高版本，证明：

- active Bundle 只向前移动；
- 同版本不同 bytes、降级和 Target 删除均拒绝 readiness；
- 稳定 `providerId/targetId` 保持一致；
- 兼容升级的 `readFormats` 包含旧 checkpoint format，旧 Save 可由新 Bundle 恢复；
- 若新 Target 不再读取未删除的持久用户存档格式，升级拒绝 readiness，原 Save 记录与字节不变；审核临时 checkpoint 不参与此门槛；
- 新普通 Launch 和新 Save 都绑定新 Bundle/Target declaration；
- 不再从旧 Bundle 静态端点或旧模块 fallback。

PFB只能证明当前worktree、基座Provider与当前开发模块组合的产品行为；正式Release授权后必须使用production lock重跑相同Case，才可成为发布证据。

## 7. 必跑门禁

共享运行层改变时至少运行 `ACC-PROVIDER-001..008`、`make web-e2e`、全部已有受影响产品 Case、Provider 仓库全量 lint/typecheck/test/build/package 检查，以及 Retrom 的 API、Go、Web、集成、数据和镜像/PFB 验证。真实硬件兼容结论仍需 Chrome `mapping=standard` 的实体手柄 smoke；自动注入不能替代硬件验收。

EmulatorJS 4.2.3 的恢复就绪以 native serializer 成功返回非空状态为准，不对所有核心统一要求诊断 frame counter 大于零。MAME 2003 Plus 的原生 unserialize 拒绝第零帧，因此该 Target 还必须完成首帧后才能读档；其他核心不继承这个条件。写入恢复状态后仍必须等待 native 读档完成信号，不能把超时视为成功。
