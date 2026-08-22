# 核心运行时验证基线

## 1. 文档职责

本文定义 EmulatorJS 核心的产品链路验证边界。核心版本、artifact、SHA-256、DAT 与 adapter 映射以 [`data/dat/`](../data/dat/) 的版本化 manifest 和前端 adapter registry 为机器事实源。公开产品 E2E 使用 [`testdata/public-roms/`](../testdata/public-roms/) 中由本项目源码确定性生成并许可分发的 GBA、NES 与 Arcade 测试程序。自动化测试不读取操作者私有 ROM/BIOS；`make dev` 的 `.dev-data/` 服务器导入语料也不属于测试 fixture。

独立 HTML 页面直接装载 EmulatorJS 会绕过 Retrom 的导入、审核、发布、Launch capability、内容端点和 Player，因此不能作为产品集成或验收证据。仓库不再维护这种 example runner，也不再用逐核心独立页面的成功结果宣称 Retrom 已覆盖对应核心。

## 2. 当前产品链路覆盖

| 核心/能力 | 实际入口 | 本机资源 | 覆盖边界 |
| --- | --- | --- | --- |
| `mgba` | `make web-e2e`、`ACC-RUN-002`、`ACC-PEG-006` | `testdata/public-roms/gba-smoke/gba-smoke.gba` 与 `pegasus-smoke.gba`；项目自有 MIT 夹具，使用不同 GBA header/内容身份，size/SHA-256 与生成一致性由各自消费者锁定 | 普通上传与 Pegasus 服务器目录两种真实 Retrom 导入入口，审核发布、Launch、受限内容端点、Player、Chrome canvas 与核心帧推进 |
| `fceumm` | `make web-e2e`、`ACC-NP-014`、`ACC-NP-016` | `testdata/public-roms/nes-smoke/nes-smoke.nes`；项目自有 MIT iNES 1.0 NROM 程序，确定性读取 P1/P2 输入并更新画面 | 真实上传、导入、审核、发布、联机房间、双 Launch/cookie、两路受限内容、两个 Chrome Player、native state transfer/load、输入延迟触发 rollback、120-frame checkpoint digest 收敛、后台冻结恢复与 3 秒 transport drop 后同 session/new epoch 重连；无需 BIOS |
| `mame2003` | `make web-e2e`、`ACC-RUN-006` | `testdata/public-roms/arcade-smoke/`；项目自有 MIT 的 Z80 程序、图形/声音资源、小型 MAME XML 和测试 BIOS 角色归档 | `ACC-DAT-004` 独立证明 release 真实 DAT 的物化/选版；产品 E2E 将项目自有小型 DAT 通过测试装置登记为 test-only `BUILTIN`（无上传/API），再覆盖 Split Child/Parent/BIOS 识别、审核与发布 schema v2、详情页同一 DatVersion、首次启动重验证仍保持 v2、schema v2 current revision 直接 Launch、三路受限内容、Player、Chrome 动画帧与运行遥测；测试 BIOS 不被 Pac-Man 驱动执行，不证明核心内部 BIOS 执行语义 |
| `fbneo` | `make web-e2e`、`ACC-RUN-007`、`ACC-NP-015`、`ACC-NP-016` | `testdata/public-roms/arcade-smoke/fbneo/`；项目自有 MIT 的双输入 Z80 程序、生成图形/PROM、Logiqx DAT 和测试 BIOS；生成器将其控制的 4 bytes 校正到锁定驱动所需 CRC32，完整 bytes 另由 SHA-1/SHA-256 固定 | `ACC-DAT-004` 独立证明 release 真实 DAT；单机链路覆盖 test-only DAT、Split Child/Parent/BIOS、审核发布、三路内容、Player 动画与遥测。双浏览器链路覆盖两个 Chrome Player、严格零 prediction/rollback、原始消息到达 RTT 驱动的 1–8 帧输入缓冲升降、720-frame checkpoint digest 收敛、后台冻结恢复与 3 秒 transport drop 后同 session/new epoch 重连；测试 BIOS 仅作为装配内容，不被 Pac-Man 驱动执行 |
| Saturn 多盘 | `ACC-MDISC-001`–`008` | 普通测试使用确定性临时夹具 | 产品 parser、导入、发布、Launch 内容协议、Player adapter 换盘与存档恢复；当前不包含真实 ROM 的浏览器运行 |

其余 enabled core（包括 FBA2012）目前只有 manifest/schema、依赖物化、adapter 配置、协议或相邻纯逻辑测试，尚没有走完整 Retrom 产品链路的真实浏览器 E2E。发布或依赖升级不能把这些结构检查解释为“核心已实际启动”。FCEUmm/FBNeo 的联机基线只覆盖表中精确 artifact/profile 和项目自有 fixture，不证明其他 ROM 或 core 版本；新增覆盖仍应扩展 `make web-e2e` 或对应产品 E2E，并使用项目自有或有明确再分发许可、可确定性生成且能够提交的测试程序。

## 3. 验证原则

- 浏览器运行证据必须从 Retrom 页面进入，并经过后端生成的 Launch config 与受限内容端点；不得直接把 ROM URL 传给独立 EmulatorJS 页面。
- 单元测试使用小型确定性字节、临时目录和临时 SQLite；产品 E2E 只读取仓库自有且许可清晰、生成源可审查的确定性测试 ROM。
- 自动化测试不得依赖操作者私有 ROM/BIOS，也不得在测试期间联网下载第三方游戏内容；没有合法公开 fixture 的核心必须明确登记为未覆盖。
- 资源的大小和 SHA-256 应由最接近的实际消费者校验，不能依赖一个与产品链路分离的全局 example manifest。
- 修改共享 loader、runtime config、内容字节协议、adapter 或存档协议时，运行所有现有受影响产品 E2E；对没有产品 E2E 的核心必须在交付说明中列为未覆盖，不能用独立页面或历史截图补齐。
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

存档能力验证统一按显式 SaveState 分支：用户点击“创建存档”后必须看到实际上传进度直至 HTTP 成功或失败，成功记录同时包含非空 state 与方向正确的截图；从该记录启动必须等待对应兼容分支完成 native state load 并回到相同可辨识画面/位置。没有选择存档的“开始游戏/重新开始游戏”必须返回初始状态，且启动前清空 EmulatorJS `/data/saves` 的浏览器 IDBFS 残留；运行、定时器、退出与 `pagehide` 都不能创建 SaveState 或上传游戏进度。竖屏核心的截图方向必须与 Player 显示方向一致，不能保存 core 未应用 rotation 的原始帧。人工 smoke 使用的私有 ROM/BIOS 只能存在于被忽略的操作者目录或临时服务器数据，不得进入 Git、测试 fixture、日志或正式证据中的宿主绝对路径；没有可再分发 fixture 的核心仍保持第 2 节所述的自动化覆盖缺口。

## 6. 依赖升级

升级 EmulatorJS、任一 core、adapter registry 或 Arcade DAT 时执行 [`ACC-DAT-006`](./project-acceptance.md)，并运行 `make data-check`、`make deps-check`、受影响的产品集成测试和 `make web-e2e`。如果升级影响的核心尚无实际产品 E2E，验收结果必须明确记录该缺口；补足覆盖后才能据此声称浏览器运行兼容。
