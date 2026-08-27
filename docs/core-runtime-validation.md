# 核心运行时验证基线

## 1. 文档职责

本文定义 EmulatorJS 与 RPG Maker 版本核心的产品链路验证边界。核心版本、artifact、SHA-256、DAT 与 adapter 映射以 [`data/dat/`](../data/dat/) 的版本化 manifest 和前端 adapter registry 为机器事实源。公开产品 E2E 使用 [`testdata/public-roms/`](../testdata/public-roms/) 中由本项目源码确定性生成并许可分发的 GBA、NES、SNES、Arcade 与 RPG Maker 测试程序。自动化测试不读取操作者私有 ROM/BIOS/商业游戏；`make dev` 的 `.dev-data/` 服务器导入语料也不属于测试 fixture。

独立 HTML 页面直接装载 EmulatorJS 会绕过 Retrom 的导入、审核、发布、Launch capability、内容端点和 Player，因此不能作为产品集成或验收证据。仓库不再维护这种 example runner，也不再用逐核心独立页面的成功结果宣称 Retrom 已覆盖对应核心。

## 2. 当前产品链路覆盖

| 核心/能力 | 实际入口 | 本机资源 | 覆盖边界 |
| --- | --- | --- | --- |
| `mgba` | `make web-e2e`、`ACC-RUN-002`、`ACC-PEG-006`、`ACC-IMM-004/005/010` | `testdata/public-roms/gba-smoke/gba-smoke.gba` 与 `pegasus-smoke.gba`；项目自有 MIT 夹具，使用不同 GBA header/内容身份，size/SHA-256 与生成一致性由各自消费者锁定 | 普通上传与 Pegasus 服务器目录两种真实 Retrom 导入入口，审核发布、Launch、受限内容端点、Player、Chrome canvas 与核心帧推进；沉浸路径额外覆盖手柄入口/游戏、SaveState 启动、双 Select+Start 暂停菜单、显式创建存档、输入隔离和退出返回 |
| `fceumm` | `make web-e2e`、`ACC-NP-014`、`ACC-NP-016` | `testdata/public-roms/nes-smoke/nes-smoke.nes`；项目自有 MIT iNES 1.0 NROM 程序，确定性读取 P1/P2 输入并更新画面 | 真实上传、导入、审核、发布、联机房间、双 Launch/cookie、两路受限内容、两个 Chrome Player、native state transfer/load、输入延迟触发 rollback、120-frame checkpoint digest 收敛、后台冻结恢复与 3 秒 transport drop 后同 session/new epoch 重连；无需 BIOS |
| `nestopia` | `make web-e2e`、`ACC-RUN-009`、`ACC-NP-018` | 与 FCEUmm 由同一项目自有生成器产出功能等价但内容身份不同的 `nestopia-smoke.nes`，建立独立 Game/Variant | 普通上传、审核发布、单机 state capture/load 与两次独立 SaveState 恢复；严格 lockstep 双浏览器验证 119/239/719 及断线后 checkpoint、输入缓冲升降、冻结与断线重连。锁定 core 不恢复紧随 `NST\x1a` 根块的 8-byte libretro input trailer；传输、接收与 checkpoint 只对该精确动态位置作零值投影，其余 MEM 全量逐字节校验；无需 BIOS |
| `snes9x` | `make web-e2e`、`ACC-RUN-008`、`ACC-NP-017` | `testdata/public-roms/snes-smoke/snes-smoke.sfc`；项目自有 MIT 32 KiB LoROM，显式初始化 WRAM/PPU/输入 | 真实上传、审核发布、单机可见 P1/P2/帧变化、native state 与两次 SaveState 恢复；严格 lockstep 双浏览器 checkpoint、缓冲升降、冻结、重连与最终 canvas 一致；整局任一合法检查点若出现一次边界瞬时差异，只接受完整 state 在 load 前已自然一致的唯一 no-op hash recovery，并要求新 epoch 连续两个 checkpoint 一致；取证块不作允许列表，SNES state 不作 byte mask；无需 BIOS/SRAM |
| `mame2003` | `make web-e2e`、`ACC-RUN-006`、`ACC-NP-019`、`ACC-IMM-006` | `testdata/public-roms/arcade-smoke/`；项目自有 MIT 的 Z80 程序、图形/声音资源、小型 MAME XML 和测试 BIOS 角色归档 | `ACC-DAT-004` 独立证明 release 真实 DAT；产品 E2E 以 test-only `BUILTIN` DAT 覆盖 Split Child/Parent/BIOS、审核发布 schema v2、三路内容、4.2.1 override core、单机动画/遥测与严格 lockstep 双浏览器恢复；沉浸路径覆盖双 standard 手柄、Arcade 投币/Start、菜单活动手柄所有权和 teardown；测试 BIOS 不被驱动执行 |
| `mame2003_plus` | `make web-e2e`、`ACC-RUN-010`、`ACC-NP-020` | `arcade-smoke/mame2003_plus/`；driver-visible member 与 MAME2003 相同，ZIP 使用项目自有固定 comment 取得独立内容身份 | 独立 test-only DAT、Split Child/Parent/BIOS、审核发布、三路内容、锁定 4.2.3 core、单机 state/恢复和严格 lockstep 双浏览器 checkpoint/冻结/重连；测试 BIOS 不被驱动执行 |
| `fbneo` | `make web-e2e`、`ACC-RUN-007`、`ACC-NP-015`、`ACC-NP-016` | `testdata/public-roms/arcade-smoke/fbneo/`；项目自有 MIT 的双输入 Z80 程序、生成图形/PROM、Logiqx DAT 和测试 BIOS；生成器将其控制的 4 bytes 校正到锁定驱动所需 CRC32，完整 bytes 另由 SHA-1/SHA-256 固定 | `ACC-DAT-004` 独立证明 release 真实 DAT；单机链路覆盖 test-only DAT、Split Child/Parent/BIOS、审核发布、三路内容、Player 动画与遥测。双浏览器链路覆盖两个 Chrome Player、严格零 prediction/rollback、原始消息到达 RTT 驱动的 1–8 帧输入缓冲升降、720-frame checkpoint digest 收敛、后台冻结恢复与 3 秒 transport drop 后同 session/new epoch 重连；测试 BIOS 仅作为装配内容，不被 Pac-Man 驱动执行 |
| `fbalpha2012_cps1` | `make web-e2e`、`ACC-RUN-011`、`ACC-NP-021` | `arcade-smoke/fbalpha2012_cps1/1941.zip`；按锁定 `1941` driver layout 生成的完整项目自有 68000/Z80/图形/静音 set | test-only DAT 的无 parent/BIOS 根集合、真实审核发布与内容端点、锁定 core 启动、程序 state marker/palette、输入/画面、两次 SaveState 恢复和严格 lockstep 双浏览器 checkpoint/冻结/重连；重连后执行 180 帧视觉稳定窗口，双端 PNG 必须一致且非纯黑 |
| `fbalpha2012_cps2` | `make web-e2e`、`ACC-RUN-012`、`ACC-NP-022` | `arcade-smoke/fbalpha2012_cps2/spf2xjd.zip` 与项目自有 marker-only `spf2t.zip`；Phoenix 明文程序，不含第三方 bytes | 锁定 core loader 实际要求的 child/parent 装配、审核 schema v2 `PARENT SATISFIED_EXTERNAL`、`parentUrl` 非空而 `biosUrl` 为空、程序 state marker/palette、单机两次恢复和严格 lockstep 双浏览器 checkpoint/冻结/重连；marker 父归档不被 driver 执行；重连后执行 180 帧视觉稳定窗口，双端 PNG 必须一致且非纯黑 |
| Saturn 多盘 | `ACC-MDISC-001`–`008` | 普通测试使用确定性临时夹具 | 产品 parser、导入、发布、Launch 内容协议、Player adapter 换盘与存档恢复；当前不包含真实 ROM 的浏览器运行 |

RPG Maker 每个用户可见版本核心使用一个独立完整游戏项目，不以 marker-only 文件、格式 parser 或独立引擎页面冒充产品验证：

| 版本核心 | 产品验收入口 | 合法游戏输入 | 必须证明的边界 |
| --- | --- | --- | --- |
| `rpgmaker_2000` | `ACC-RPG-002`、`ACC-RPG-012` | `testdata/public-roms/rpgmaker-smoke/rpg2000/`；012 的第二次导入使用同一生成源的 `rpg2000-compat/`；均为 Retrom 自有 MIT 游戏，由固定 JSON 和有界 LCF writer 确定性生成，不含 RTP | 选择 2000 核心后经上传、审核、EasyRPG Launch 和 Player 进入地图；bridge 回读 RPG2k profile，并执行 A→B 保存→C→不同 Launch 恢复 B→恢复后输入；012 用两个不同 files digest 证明新旧 artifact 绑定，不得重复上传同一内容冒充第二项目 |
| `rpgmaker_2003` | `ACC-RPG-003` | `testdata/public-roms/rpgmaker-smoke/rpg2003/`；同一 MIT 生成体系的独立 LCF 游戏，`ldb_id=2003`，bytes 与 marker 独立 | 证明 2003 route/engine profile，不因与 2000 共用文件形态或 runtime bytes 而 fallback；其余精确恢复门禁与 2000 相同 |
| `rpgmaker_xp` | `ACC-RPG-004` | `testdata/public-roms/rpgmaker-smoke/rpgxp/`；Retrom 自有 MIT Ruby 程序经确定性 Marshal 4.8/zlib 生成，不含厂商默认 RGSS script/RTP | RGSS1 threaded mkxp artifact、可见可移动色块、变量、最多 256 MiB runtime state、不同 Launch 精确恢复，以及禁线程环境在下载前 fail closed |
| `rpgmaker_vx` | `ACC-RPG-005` | `testdata/public-roms/rpgmaker-smoke/rpgvx/`；同一 MIT 源生成的独立 RGSS2 游戏 | RGSS2 route、可见画面、输入/音频和 A/B/C/restore 全字段门禁；不得以 XP/Ace profile 启动 |
| `rpgmaker_vx_ace` | `ACC-RPG-006` | `testdata/public-roms/rpgmaker-smoke/rpgvxace/`；同一 MIT 源生成的独立 RGSS3 游戏 | RGSS3 route、可见画面、输入/音频和 A/B/C/restore 全字段门禁；不得以 XP/VX profile 启动 |
| `rpgmaker_mv` | `ACC-RPG-007` | `testdata/public-roms/rpgmaker-smoke/rpgmv/`；Retrom 自有 MIT 游戏数据/素材，加锁定 MIT `rpgtkoolmv/corescript` 输入 | 只在每 Launch unique runtime origin 执行项目 JavaScript，进入 `Scene_Map`、连续 300 帧、标准 DataManager checkpoint、不同 Launch 恢复和恢复后输入；app origin 不执行游戏脚本 |
| `rpgmaker_mz` | `ACC-RPG-008` | 操作者依法持有、自包含且可下载/部署的 MZ Web Browser 游戏目录或单 ZIP/7z，由 `RPG_MZ_SMOKE_ROOT` 指定；不得提交、镜像或记录其内容/绝对路径 | 与 MV 相同的 unique-origin、场景/帧/输入/音频/精确恢复门禁，并证明 MZ Promise save profile；缺少合法输入时 Case 必须 BLOCKED，不能用 shape harness 代替 |

上述六个公开项目的唯一生成源、逐 byte 锁定和许可证由同目录 `README.md`、`LICENSE`、`fixture-manifest.json` 与 `make public-fixtures-check` 共同约束。MZ 输入只允许通过 Retrom 上传链消费；自动化不得在本机直接打开或运行操作者项目。任何外部可下载游戏只能作为不提交的补充兼容 smoke，不能取代这些确定性产品 Case，也不能因下载页面可访问就推断资源、RTP、默认脚本或插件可再分发。

其余 enabled core 目前只有 manifest/schema、依赖物化、adapter 配置、协议或相邻纯逻辑测试，尚没有走完整 Retrom 产品链路的真实浏览器 E2E。发布或依赖升级不能把这些结构检查解释为“核心已实际启动”。表中联机基线只覆盖精确 artifact/profile 和项目自有 fixture，不证明其他 ROM 或 core 版本；新增覆盖仍应扩展 `make web-e2e` 或对应产品 E2E，并使用项目自有或有明确再分发许可、可确定性生成且能够提交的测试程序。

## 3. 验证原则

- 浏览器运行证据必须从 Retrom 页面进入，并经过后端生成的 Launch config 与受限内容端点；不得直接把 ROM URL 传给独立 EmulatorJS 页面。
- 单元测试使用小型确定性字节、临时目录和临时 SQLite；产品 E2E 只读取仓库自有且许可清晰、生成源可审查的确定性测试 ROM。
- 自动化测试不得依赖操作者私有 ROM/BIOS，也不得在测试期间联网下载第三方游戏内容；没有合法公开 fixture 的核心必须明确登记为未覆盖。
- 资源的大小和 SHA-256 应由最接近的实际消费者校验，不能依赖一个与产品链路分离的全局 example manifest。
- 修改共享 loader、runtime config、内容字节协议、adapter 或存档协议时，运行所有现有受影响产品 E2E；对没有产品 E2E 的核心必须在交付说明中列为未覆盖，不能用独立页面或历史截图补齐。
- 沉浸模式的自动 Gamepad 注入只证明可重复的输入与产品链路；发布结论还必须使用 Chrome 报告 `mapping=standard` 的实体手柄完成当次 smoke。缺少实体设备时标记 `BLOCKED`，不能把自动化结果提升为硬件兼容证据。
- 修改共享画面模式、shader 注入或 canvas 合成策略时，mGBA、MAME 2003 与 FBNeo 的产品 E2E 都必须在默认“锐利像素”模式下确认 shader 关闭、`image-rendering: pixelated`、持续出帧与零页面异常；mGBA 还必须覆盖“清晰增强”自有 shader、增强锐化、原始画面、返回默认模式的即时切换、原生 Core→显示面板切换以及物理 4K 150% 截图。该结果只证明 Player 输出处理，不提高未登记 core 的运行覆盖等级。

## 4. MAME2003 版本覆盖

EmulatorJS 4.2.3 发布包中的官方 `mame2003` bundle `2.0.2` 曾在 Chrome 环境中于 `retro_load_game` 阶段稳定触发 WASM `unreachable`。当前依赖 manifest 因此为 `mame2003` 精确锁定 EmulatorJS 4.2.1 bundle `2.0.1`，同时保留 4.2.3 loader/UI。覆盖 URL、大小、SHA-256、build report、core source commit 和理由由 [`data/dat/emulatorjs/4.2.3/manifest.json`](../data/dat/emulatorjs/4.2.3/manifest.json) 维护。

实现必须满足：

- Core 保存实际 artifact 路径、SHA-256 和兼容配置，不能只保存 EmulatorJS 版本；
- resolver 对 `mame2003` 读取版本化覆盖，不能用 `mame2003_plus` 静默替代；
- 后续升级先通过实际 Retrom 产品 E2E，再删除覆盖；
- 覆盖变化视为 CoreArtifact 变化，旧存档不得默认跨版本加载。

## 5. 内容与运行时约束

GBA 单成员 ZIP 在导入时应逐字节物化实际 `.gba` entry 到 CAS；原始上传 Blob、archive entry 与派生内容保持可追溯。浏览器启动时不得临时猜测 ZIP 入口。

`dosbox_pure`、`genesis_plus_gx_wide` 与 `azahar` 使用 EmulatorJS `4.3.0-pre` 定向覆盖，其他核心使用各自 manifest 锁定版本。线程核心要求同源响应具备 COOP、COEP 与 CORP，生产 CSP 不得为运行时放开无界 `unsafe-eval`。

PSP profile 接受 raw `.cso` 和 `.iso`，两者分别以 `RAW_FILE_V1` 进入 CONTENT、Variant 与 Launch；产品导入、Launch 与 Player 不做格式互转。PPSSPP 与 `mednafen_psx_hw` 的线程 artifact basename、assets 和 renderer 配置以依赖 manifest 与 adapter 实现为准，不从旧的示例物化脚本推导。所有 4.2.3 核心的指定 SaveState 恢复都不能使用 EmulatorJS 的首帧自动加载：Player 必须至少等一个核心帧且序列化布局就绪，再执行原生 state task，并以原生失败日志 fail closed；smoke 至少比较保存截图与恢复后的可辨识游戏位置，不能只证明大文件上传/下载成功。使用操作者测试服务器私有 PSP 游戏的人工 smoke 可以验证部署实例，但不提升第 2 节的可提交公开产品 E2E 覆盖等级。

锁定的 `4.3.0-pre` DOSBox Pure thread artifact 还需要独立的状态兼容层：其 4 MiB WASM stack 会在 core 序列化时破坏 `save_state_info` 描述，而该版本 EmulatorJS helper 又会错误释放该 C 函数返回的 stack pointer。Player 只能在已校验 artifact 的唯一 marker 与两个等长 stack-high 值同时精确匹配时，把运行中 WASM 的两个 high watermark 扩为 64 MiB；随后同步复制 state allocation，只释放 data pointer。artifact 形状、marker 数量或导出布局有任何漂移都以 `PLAYER_DOS_STATE_COMPATIBILITY_UNAVAILABLE` 阻断，不能猜测补丁位置。指定存档不交给 EmulatorJS 首帧自动加载，而是等待 Launch 锁定的具体 DOS 程序已可序列化后显式执行 native load：先暂停并给 worker 一个有界的 50 ms 消费窗口，再把已严格校验含 `MEM ` block 的 RASTATE 写入固定 MEMFS 路径，排队该锁定 RetroArch build 的 blocking load task。manifest 固定的 source loader 同时捕获原生 `[State] Loading ... game.state` 及其同一回调内可能紧随的失败日志；运行时工厂的 `postMainLoop` hook 证明该轮 `task_queue_check` 已结束，随后立即再次暂停并确认核心仍可产出结构合法的 RASTATE。DOSBox Pure 在 unserialize 时会主动归一化计时器和宿主相关字段，因此恢复后的序列化 bytes 不能与输入逐字节比较；原生成败日志、任务后状态结构与真实产品 smoke 中保存/恢复的可辨识游戏位置共同构成证据。格式/长度/越界/缺块、明确失败日志、回调超时或空状态都 fail closed。未锁定具体程序的 DOS 程序菜单 Launch 无法确定恢复前应先运行哪个程序，因此 UI 禁止创建不可恢复的存档，并提示用户退出后选择具体程序启动。

存档能力验证统一按显式 SaveState 分支：用户点击“创建存档”后必须看到实际上传进度直至 HTTP 成功或失败，成功记录同时包含非空 state 与方向正确的截图；从该记录启动必须等待对应兼容分支完成 native state load 并回到相同可辨识画面/位置。没有选择存档的“开始游戏/重新开始游戏”必须返回初始状态，且启动前清空 EmulatorJS `/data/saves` 的浏览器 IDBFS 残留；运行、定时器、退出与 `pagehide` 都不能创建 SaveState 或上传游戏进度。竖屏核心的截图方向必须与 Player 显示方向一致，不能保存 core 未应用 rotation 的原始帧；core framebuffer 解码采样为近黑帧时必须回退显示 canvas；沉浸菜单必须在暂停 Core 前请求截图并在用户确认创建时复用，不能暂停后重新捕获。人工 smoke 使用的私有 ROM/BIOS 只能存在于被忽略的操作者目录或临时服务器数据，不得进入 Git、测试 fixture、日志或正式证据中的宿主绝对路径；没有可再分发 fixture 的核心仍保持第 2 节所述的自动化覆盖缺口。

## 6. 依赖升级

升级 EmulatorJS、任一 core、adapter registry 或 Arcade DAT 时执行 [`ACC-DAT-006`](./project-acceptance.md)，并运行 `make data-check`、`make deps-check`、受影响的产品集成测试和 `make web-e2e`。如果升级影响的核心尚无实际产品 E2E，验收结果必须明确记录该缺口；补足覆盖后才能据此声称浏览器运行兼容。
