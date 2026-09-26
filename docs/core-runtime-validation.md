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
| `fceumm`、`nestopia` | `ACC-RUN-009` | `testdata/public-roms/nes-smoke/` | 普通启动、输入、存档与恢复 |
| `snes9x` | `ACC-RUN-008` | `testdata/public-roms/snes-smoke/` | 普通启动、输入、存档与跨 Launch 恢复 |
| `mame2003`、`mame2003_plus`、`fbneo`、`fbalpha2012_cps1/cps2` | `ACC-RUN-006/007/010/011/012`、`ACC-IMM-006` | `testdata/public-roms/arcade-smoke/` | DAT parent/BIOS 闭包、真实画面/输入、checkpoint 与沉浸多手柄 |
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
- 所有通过 RuntimeHost 挂载的核心 iframe 共用 `1920×1080` CSS 运行视口上限；大屏幕由宿主等比放大到 Player 区域，窗口尺寸变化后重新计算，小于上限时保持原尺寸。Provider 可在此基础上声明更低的画布上限；4K 帧率证据必须同时记录 iframe 视口、核心画布尺寸和 GPU renderer。
- checkpoint 只按 format/size/hash 处理，Host 不解析字节；恢复必须由 Target `readFormats` 明确允许。
- content、BIOS、parent、多盘、pack、unique-origin 资源必须全部来自 envelope grant。
- 所有测试必须证明退出/失败/卸载会清理 Provider、撤销 Launch 并停止输入与帧回调。

## 4. EmulatorJS 特殊边界

EmulatorJS Provider declaration 是 56 个 Target 的唯一行为 registry。`mame2003` 的 4.2.1 core 覆盖、DOSBox Pure 的 state 修复、线程 core、shader、启动动作与多盘都封装在该 Provider 中。Retrom 只看 Target declaration 与标准能力，不按 core 名在 Go 或前端复制规则。

原始画面与锐利像素使用显式颜色直通、无滤波的 `retrom-passthrough` shader，避开 4.2.3 关闭 shader 后在原生分辨率切换时出现纯色/裁切的 GL fallback。浏览器画面必须与核心截图保持完整内容，启动和跨 Launch 恢复均需覆盖；不能用切换画面模式的人工操作替代默认模式验收。


新增 `fuse`、`gearcoleco`、`prboom`、`puae`、`vice-x128`、`vice-x64sc`、`vice-xvic` 与 `virtualjaguar` 只声明 `SINGLE_FILE`，`discSwitch=false`。逐核产品验收按 [ACC-RUN-013](./project-acceptance.md#acc-run-013八个-emulatorjs-单文件候选的逐核产品验证) 执行；首次验收可从产品白名单任选一种扩展名，一个 Target 的结果不能替代另一个 Target。

SNES 的 `bsnes` 通过独立 `emulatorjs/bsnes` Target 使用锁定的 EmulatorJS 4.3.0-pre
前端与 `retrom-project/bsnes-libretro` 的单线程 WASM candidate，接收平台允许的单文件 ROM。它作为备用核心供显式选择，不改变 Snes9x 默认推荐目录，
不新增推荐目录、不开放多盘。标准 SNES 手柄和即时存档沿用 Provider 公共边界；bsnes 声明
独立的 `bsnes-state-v1-storage-v1` 格式，不读取通用 EmulatorJS 格式。宿主按 `readFormats`
检查兼容性，因此 bsnes 与 Snes9x 即时存档不能混用，不增加存档身份字段或按核心分支。该 PFB candidate 修复上游缺失的
Asyncify 和协程重建能力、异步保存回调及原生内存所有权，来源基线为 `4b344745e3878e7c0675a60c624582935524b8f7`，
仅允许 candidate 构建；正式发布前需先发布 fork 资产，再固定 Provider 来源。逐样本产品验证见
[ACC-RUN-016](./project-acceptance.md#acc-run-016bsnes-备用核心产品验证)。

Mega Drive 的 Genesis Plus GX、GX Wide 与 PicoDrive 由 Provider 在输入表建立前明确选择 Mega Drive 手柄布局，保留 Start、方向与 A/B/C/X/Y/Z；不能采用多平台核心自动推断出的 Master System 布局。键盘与标准手柄使用同一控制表，原始 `.md`/`.smd` 与归档内成员行为一致。固定 EmulatorJS 4.2.3 的六键布局使用等价的 `segaCD` 输入别名，4.3.0-pre 使用 `segaMD`；这只选择输入布局，不切换运行核心或内容类型。

Sega CD、Amiga CD32 与 Satellaview / BS-X 作为独立平台分别绑定 `genesis_plus_gx` 核心的 `genesis-plus-gx-cd` Target、`puae` Target、`snes9x` Target。首期 Sega CD 只接收单文件 CHD；CD32 接收单文件 CHD/ISO；Satellaview 接收单文件 BS/SFC/SMC。PUAE 的普通 Amiga 与 CD32 内容均由同一个 `puae` Target 通过 runtime Content I/O 持久化完整文件，再从 EmulatorJS 公共虚拟 FS 交给线程核心；文件扩展名取自内容 URL，保留 ADF/LHA/CHD/ISO 等核心识别路径。三者均不声明多盘、CUE/BIN 目录、换盘或 Satellaview 多卡带。Sega CD 的欧、美、日三份 BIOS 和 CD32 Kickstart/extended ROM 在相应 CD 内容验证时才必需，不得让普通 Mega Drive 卡带或 Amiga 软盘承担这些依赖。CD32 的 PUAE 资产必须是声明线程能力的 `puae-thread-wasm.data`：无线程资产在创建 CD 音频解包线程失败后会卡在等待循环，原始线程资产则因 pthread 入口函数签名不符而在浏览器执行时崩溃；核心 fork 须正确适配线程入口并在建线程失败时返回错误。Snes9x 的 `BS-X.bin` 维持现有可选固件槽，操作者运行需要固件的 BS-X 内容前应安装它；不能从 `.sfc/.smc` 后缀推断是否需要 BS-X BIOS。真实游戏的准入按 [ACC-RUN-018](./project-acceptance.md#acc-run-018sega-cdamiga-cd32-与-satellaviewbs-x-产品验证) 逐平台验证；单个样本不能外推全部游戏兼容。

普通 Amiga 的 A500/A1200 Kickstart 作为可选 BIOS，仅匹配电脑内容，不影响 CD32 固件条件；需要 Kickstart 的真实 ADF/LHA 样本必须安装对应文件并完成画面验证。

Game Gear 与 SG-1000/Multivision 使用 Genesis Plus GX 的内容扩展选择 `segaGG` / `segaMS` 控制布局，不能套用 Mega Drive 六键布局。URL 的查询参数不参与扩展识别。GX4000 的 `.cpr` 在 Cap32 启动前选择 `6128+ (experimental)` 与所需的 `cap32_gfx_colors=24bit`；普通 CPC 磁盘保留原默认值。七个新平台与现有 Arcade/FBNeo 的 Neo Geo 样本按 `ACC-RUN-017` 验证；Pico 的方向/确认结果不能证明笔、翻页等未测操作，也不能外推为全库兼容。

EmulatorJS 构造期间已检测到的手柄，在控制表就绪时补齐空闲玩家分配，保留已有分配且不重复绑定；后续插拔继续使用 EmulatorJS 原有事件。不能要求启动前已连接的手柄重新插拔才能操作。

PSP 优化的定向验证应使用操作者提供的合法样本，通过真实 Review Preview、Product Launch、新建 checkpoint、不同 Launch 恢复和恢复后输入检查。4K、150% 缩放使用 `2560×1440` CSS 视口与 `deviceScaleFactor=1.5`；必须同时检查窗口变化、全屏、设置面板、完整画面与边缘内容。帧率对照应记录相同游戏场景、核心配置与 GPU renderer，软件渲染结果不能推断为实体显卡性能。压缩收益以该次存档压缩前后完整字节数计算，不以不同场景的两份存档相除。

Provider 私有的 PSP 存档读取必须等待原生异步序列化结束，期间不得恢复主循环造成 Asyncify 重入；只释放原生数据 allocation，不释放借用的描述符。压缩格式、画布上限和固定核心版本由 Provider 自己声明，Host 不增加 PSP 分支。

指定存档不能在首帧盲目自动加载；Provider 必须等待目标核心可序列化，再执行原生 load 并以明确失败 fail closed。普通开始必须清理浏览器遗留的隐式目录存档，只有用户点击“创建存档”才上传显式 checkpoint。

新增七个核心和 PC Engine CD 的逐项验收使用 [ACC-RUN-015](./project-acceptance.md#acc-run-015剩余-emulatorjs-核心与-pc-engine-cd-产品验证)。候选声明不代替真实浏览器兼容性证据。

Dreamcast 通过 `emulatorjs/flycast` Target 接入 nasomers/flycast-wasm 的 WASM JIT，由
`retrom-project/flycast-wasm` 固定源码构建。首期只接受单文件 `.chd`，使用 WebGL2、
640×480、无 pthreads；Windows CE/MMU 与多盘不在该 Target 的支持范围。
BIOS 使用安装快照中的 `/dc/dc_boot.bin` 与 `/dc/dc_flash.bin`，关闭 HLE BIOS。
标准手柄的 A/B/X/Y 按 Dreamcast 物理位置绑定，方向、摇杆及 L/R 扳机由标准输入表传递。

Flycast 的 CHD 通过 `SEEKABLE_BLOB` 和 Content I/O Range reader 向原生 CHD hunk 读取提供最多 256 KiB 的块；
启动前不物化整个镜像。每个 Launch 仍须取得当前 envelope grant，Range reader 负责块缓存、身份校验和取消。
即时存档由公共 Provider 边界统一压缩一次，写入 `flycast-state-v1-storage-v1`；恢复时按声明格式有界解压，
压缩前后均遵守 Provider 的大小上限，并继续读取旧 `flycast-state-v1` 和 `flycast-state-gzip-v1` 存档。恢复等待核心启动完成。
Flycast 的 iframe 在创建 WebGL 上下文时保留绘图缓冲区，避免浏览器呈现后清空缓冲区，
使暂停后的 Canvas 截图仍可读取最后画面；退出时恢复该 iframe 的上下文创建方法。
操作者语料的验收规则见 `ACC-FLYCAST-001`；单个样本结果不能外推为 Dreamcast 全库兼容。

NAOMI、NAOMI 2 与 Atomiswave 通过各自的 Platform/Core 绑定使用同一 Flycast 核心字节，
分别对应 `emulatorjs/flycast-naomi`、`flycast-naomi2`、`flycast-atomiswave` Target。
首期内容为单个街机卡带 ROM ZIP；保留机器短名作为 Flycast 的游戏识别名，浏览器不解压。
卡带 ZIP 也使用同一 Flycast Range 桥接；原生 ZIP 解析按 seek 请求最多 256 KiB 的块，不预先下载整个归档。
卡带初始化仍可能读取多数或全部 ROM entry，实际总传输量由核心的读取行为决定。
三者分别要求安装 `/dc/naomi.zip`、`/dc/naomi2.zip`、`/dc/awbios.zip`。
GD-ROM 游戏所需的 ZIP + CHD 配对，以及 clone/parent ROM 集合，尚未进入该内容契约。

Intellivision 使用 EmulatorJS 4.3.0-pre 的 `freeintv`，仅声明单卡带、标准手柄与即时存档，
不开放多盘。ECS 扩展不在支持范围。通过 `ACC-INTV-001` 对操作者提供的样本验证；
Provider declaration 与构建成功不能代替该次产品链路结果。

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

### PC-98 / NP2kai

`np2kai-pc98` 使用 ROM_BLOB 单文件 HDI/D88，最多 512 MiB；首批游戏验证使用作者公开下载的
《囚人へのペル・エム・フル》。不包含 BIOS，不声明多软盘切换或其他磁盘格式兼容性。
固定 fork 负责 Emscripten/SDL 核心与字体、完整组件许可；Provider 负责磁盘校验、OPFS 缓存、
进度、标准手柄和 `np2kai-state-v1`。状态包含原生 CPU/内存/设备状态及相对只读基盘的磁盘改动，
总量有界，恢复必须匹配基盘摘要。原生队列接口须同步完成后才能返回，不能提早删除尚未加载的状态文件。
软件帧缓冲须在暂停时保持可读截图。产品门禁为 `ACC-PC98-001`。

### PC-88 / QUASI88

EmulatorJS Provider 的 `quasi88` Target 接收单文件 D88/U88，默认 N88 V2。
目录推荐 `pc88`，独立 BIOS 目录负责七个 NEC ROM 的摘要、大小和安装状态；
通过 EXTERNAL_FILE 装入 `/retroarch/userdata/system/quasi88/`，不随游戏或 Provider 分发。
D-pad 对应数字小键盘 8/2/4/6，主确认键通过原生 Start 发送 Return；真实键盘独立。
《The Librarian》的六边形地图使用 7/9/4/6/1/3，四个斜向和事件字母键需要键盘。
首版不声明多盘切换或未验证的媒体格式。共享 gzip 即时状态使用
`emulatorjs-state-v1-storage-v1`，须验证不同 Launch 回到保存时的位置并继续输入。
产品验收为 `ACC-PC88-001`，外部语料为作者公开发布的《The Librarian》v0.91。

### Cave Story / NXEngine

`cavestory` 平台使用 `retrom-runtime/nxengine` 的 `FILE_TREE` Target。输入为原版免费
《洞窟物语》的 `Doukutsu.exe` 与完整 `data/` 目录；不执行 Windows 程序。采用
libretro/nxengine-libretro 的固定源码，由独立 fork 构建 Emscripten 核心。
不宣称 Cave Story+ 或任意修改版兼容性。标准手柄 A 确认/跳跃、B 射击、方向移动，
键盘 Z/X/方向独立保留。取消能力按核心原生菜单处理，不使用叠加映射。

存档语义为 `GAME_SAVE`：游戏内保存后，通过公共 gzip 边界提交原生 profile 文件；
不同 Launch 在核心启动前导入明确指定的存档，用户在游戏菜单 Load 后继续。
未选择存档的新 Launch 保持空保存目录。产品准入用例为 `ACC-NXENGINE-001`。
正式 runtime tag 固定已发布的核心和聚合包；发布后复跑同一产品用例。

### Lutro

`lutro` 推荐目录只接收原始 `.lutro` 卡带。它是由核心自己读取的 ZIP 容器，
EmulatorJS 4.2.3 不得在传给核心之前解开为 `main.lua`。Provider Target 的存档语义为
`GAME_SAVE`，格式为 `lutro-native-v1-storage-v1`，只导出游戏通过
`lutro.filesystem.write` 写下的原生文件。选择存档启动时，Provider 在核心启动前导入
文件；不选择存档时清空该游戏的原生保存目录。Provider 按文件内容摘要报告变更，
Host 上传成功后才确认该版本已经持久化。最多 256 个文件、16 MiB，公共存储层只压缩一次。

声明的 `dataKind` 是 `STORAGE`：Lutro 游戏可能只写设置或分数，也可能写进度；
不把任意游戏的当前运行位置说成即时快照。`ACC-LUTRO-001` 用项目自有、确实写入关卡位置的
卡带验证新 Launch 恢复。其他 `.lutro` 游戏的兼容性须单独验证，当前结论只覆盖固定样本。
未写原生文件的卡带不能创建可恢复存档，该样本不得算通过本 Target 的存档准入。

### Apple II / Apple2JS

`apple2-apple2js` 的标准手柄方向映射到 Apple II 摇杆轴，A/B 映射到摇杆按钮 0/1。
标准手柄的 Start（Button 9）单独发送键盘 `1`；已验证的《Donkey Kong》从标题动画按一次 Start 进入人数选择，
再按一次 Start 选择单人并进入关卡。Select/Back（Button 8）单独发送 Escape。
真实键盘仍可独立操作，其他游戏的菜单按键需要逐样本验证。输入诊断观察 Apple2JS
实际读取手柄的内层 iframe，显示按钮按下与松开；观测记录不代表核心已执行该动作。

### Daphne 候选接入

Daphne fork 的 `retro_serialize_size()` 返回零，保存与恢复接口均返回失败；已有的
NVRAM/分数文件不能重建正在游玩的场景。用户已明确允许 Daphne 使用 `NO_SAVE`，
因此仅 Daphne 的 Target 声明 `checkpoint: null` 和 `capabilities.checkpoint: false`，
宿主不提供创建或恢复存档。其他平台的存档要求保持不变。

候选 Target `emulatorjs/daphne` 使用 `DAPHNE_PROJECT` 和 `FILE_TREE`。项目检测当前接受
根目录中 3–16 个文件：一个 ZIP ROM、同名 TXT framefile、一个被 framefile 引用的 M2V，
以及可选的 DAT/OGG 等小文件。ZIP、TXT、DAT、OGG 经公共 Content I/O 完整读取；
M2V 通过公共 Range reader 按需读取。核心的 MPEG 解码运行在 pthread；线程的同步
`fd_read` 必须等待主线程的 Content I/O Promise 完成，不能让 Asyncify 提前返回零字节。

在命名 PFB 中使用 `interstellar.daphne` 验证了目录上传、Review Preview、批准和
Product Launch。该街机游戏须先按手柄 Select 投币，再按 Start 开始；仅按 Start 无法从
Game Over 开局。修复后的核心将 Select、Start、方向键和两个动作键交给游戏驱动；
产品验收从 Game Over 画面按 Select、Start 后确认 Game Over 消失、生命图标出现，
再按方向键确认玩家飞船移动。M2V 请求为有界 206 Range，未整包下载。该验证只覆盖
单视频 framefile 的这个样本；多视频项目、无 ZIP 项目和其他 Daphne 游戏尚未获得准入。
候选 Provider/核心仍需按依赖顺序发布和正式包复验。

## 6. 升级验证

Provider 升级必须在同一数据库上顺序启动旧版与更高版本，证明：

- active Bundle 只向前移动；
- 同版本不同 bytes、降级和 Target 删除均拒绝 readiness；
- 稳定 `providerId/targetId` 保持一致；
- 兼容升级的 `readFormats` 包含旧 checkpoint format，旧 Save 可由新 Bundle 恢复；
- 若新 Target 不再读取未删除的持久用户存档格式，升级拒绝 readiness，原 Save 记录与字节不变；审核临时 checkpoint 不参与此门槛；
- 新普通 Launch 和新 Save 都绑定新 Bundle/Target declaration；
- 不再从旧 Bundle 静态端点或旧模块 fallback。

PFB只能证明当前worktree、基座Provider与当前开发模块组合的产品行为；正式Release授权后必须使用production release tag重跑相同Case，才可成为发布证据。

## 7. 必跑门禁

共享运行层改变时至少运行 `ACC-PROVIDER-001..008`、`make web-e2e`、全部已有受影响产品 Case、Provider 仓库全量 lint/typecheck/test/build/package 检查，以及 Retrom 的 API、Go、Web、集成、数据和镜像/PFB 验证。真实硬件兼容结论仍需 Chrome `mapping=standard` 的实体手柄 smoke；自动注入不能替代硬件验收。

EmulatorJS 4.2.3 的恢复就绪以 native serializer 成功返回非空状态为准，不对所有核心统一要求诊断 frame counter 大于零。MAME 2003 Plus 的原生 unserialize 拒绝第零帧，因此该 Target 还必须完成首帧后才能读档；其他核心不继承这个条件。写入恢复状态后仍必须等待 native 读档完成信号，不能把超时视为成功。

### Vectrex / VecX

`emulatorjs/vecx` 使用 libretro-vecx 的固定 fork，以软件向量渲染运行 `.vec/.bin` 单卡带；
不要求管理员另行上传 BIOS，不声明多盘、Light Pen 或 3D Imager 支持。
标准方向键和四个面键各自只映射一个原生输入。原始键盘输入保持独立。
fork 的完整即时状态包括 CPU、RAM、VIA、PSG、卡带银行、模拟电路与向量画面；
不能读取上游不完整的 VecX 状态。Provider 公共层按 `emulatorjs-state-v1-storage-v1`
压缩一次，恢复到不同 Launch 后必须继续接受输入。
开发候选的准入按 `ACC-VECTREX-001` 执行；候选声明不等于正式发行支持。

## Neo Geo CD

`neogeocd/neocd` 通过 `emulatorjs/neocd` 接入，首期只接受单文件 CHD；
CUE/BIN、M3U 与换盘不在本次产品契约中。管理员安装 512 KiB CDZ `neocd.bin`，
服务端按 BIOS catalog 校验并以 external file 交付到 `/neocd/neocd.bin`。
不依赖上游实验性 HLE BIOS。推荐目录为“Neo Geo CD 游戏”。

Provider 将 CHD 声明为 `SEEKABLE_BLOB`，通过 256 KiB Range 块按需读取，内存 LRU 上限 16 MiB。
启动前不全量下载或扫描镜像。每个响应核对 206、Content-Range、长度与冻结 SHA-256 ETag；
持久块缓存按内容摘要/大小/偏移隔离，命中时校验该块长度与本地摘要。缓存不可用时继续
有界网络读取；服务器忽略 Range 则明确失败，不退回整文件下载。按需读取不显示全游戏
下载进度。核心通过 Asyncify 等待缺失块，暂停、存档及退出协调在途读取。标准手柄使用 arcade 映射，
一个按钮只对应一个原生输入。即时存档采用公共 `emulatorjs-state-v1-storage-v1`，
按声明大小有界解压，并在新的 Launch 恢复后继续接收输入。

产品证据见统一验收 `ACC-NEOCD-001`。正式 runtime tag 固定发布资产，开发候选
只能用于显式 PFB 验证。通过样本不能推断整个游戏库或实体手柄兼容性。

### Pokémon Mini / GBE+

`gbe-pokemini` 必须通过 `ACC-POKEMINI-001` 的真实产品流程，验证标准手柄
方向与确认、音频、暂停、截图、完整即时存档、不同 Launch 恢复后输入和跨实例内容缓存。
用户提供的 Mini 游戏与 BIOS 不进入 fixture。当前验证单机。

### Uzebox / Uzem

`emulatorjs/uzem` 使用固定 libretro-uzem fork 的 `retrom-core-gd991ee94547c-r1` 发行，接受 ATmega644 单卡带 `.uze`，
通过 SNES 控制表传递方向、面键和 Start。只支持玩家一；不声明鼠标、SD 外部文件、
ATmega1284、网络或多盘。核心 v1 即时状态包含 AVR 寄存器、SRAM、EEPROM、Flash、
内部计时器/待处理 I/O、随机数状态、手柄锁存与软件画面，使用显式小端字段及版本、
内容标识和完整性校验；宿主使用公共 gzip checkpoint，不解析核心字节。
接入必须通过 [ACC-UZEBOX-001](./project-acceptance.md#acc-uzebox-001uzebox-单卡带产品验证)，
且不同 Launch 恢复后继续接受输入。已验证游戏不代表全库兼容。

### Odyssey² / O2EM

`emulatorjs/o2em` 使用固定 `retrom-project/libretro-o2em` fork，首期接受单文件 `.bin` 卡带及 ZIP/7z 单主文件导入。
管理员必须安装 1024 bytes 的 `o2rom.bin`；启动时按 BIOS catalog 校验，以 `BIOS_BUNDLE` 交付到 libretro system 目录。
默认采用 Odyssey² NTSC BIOS；其他地区机型、The Voice、专用键盘游戏及特殊外设需各自验证，当前浏览器构建不支持 The Voice。
标准手柄对两位玩家分别映射方向与单一动作按钮；原生键盘输入保留。
即时存档使用公共 `emulatorjs-state-v1-storage-v1`，必须在不同 Launch 恢复后继续接受输入。
正式版本按 `ACC-O2EM-001` 执行，单个样本通过不代表全库兼容。


## 独立 PSP / PPSSPP

`coreId=ppsspp` 通过 `retrom-runtime/ppsspp`、`OPTICAL_DISC` 和 `SINGLE_FILE` 接入。
Retrom 使用公共 SEEKABLE_BLOB、输入、截图、暂停和存档协议；核心源码、
WebAssembly 构建与浏览器前端由 `retrom-project/ppsspp` 维护，宿主不加载 EmulatorJS PSP 前端。
游戏通过独立 I/O Worker 按 256 KiB Range 读取，内存缓存有界，持久分块缓存按文件摘要、
长度和块位置跨实例复用。不得启动前整包下载，不把未请求的全游戏字节作为 LOAD_PROGRESS。
响应必须为 206，区间、长度及 SHA-256 身份 ETag 与冻结内容一致；本地块摘要检测缓存损坏，
源内容身份依赖服务端不可变 CAS，不能声称客户端提前验证了全盘摘要。核心代码资源仍完整
校验摘要。要求 Chrome 的 WebGL2、OffscreenCanvas、SharedArrayBuffer 与 cross-origin isolation。

新存档为 `ppsspp-state-v1-storage-v1`，公共 gzip 内含完整执行状态和记忆棒文件，解压上限
256 MiB。未验证 EmulatorJS PSP 存档与新核心兼容，因此不声明旧格式可读；旧存档不可恢复时
按公共兼容性策略禁用恢复。已有游戏应经当前 binding 重新验证后启动。EmulatorJS Provider
中的历史 Target 身份仍保留，产品 PSP binding 仅选择新的独立核心。

PFB 开发使用同一 PFB 中的核心候选、已声明的来源覆盖与完整候选 Provider，不能将候选
路径、摘要或未发布版本写入 production release tag。发布顺序为 core fork → retrom-runtime → Retrom
正式 runtime tag，每一步在授权后进行。产品门禁见 `ACC-PSP-001` 与 `ACC-PSP-002`。
