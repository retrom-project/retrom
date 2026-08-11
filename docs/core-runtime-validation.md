# 核心运行时验证基线

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已验证 / 一期实现基线 |
| 验证时间 | 以 `data/example/results/latest.json` 的当次整数毫秒为准 |
| 浏览器 | 以当次 Chrome 机器结果为准 |
| 前端运行时 | EmulatorJS `4.2.3` 基线 + `4.3.0-pre` 定向覆盖 |
| 结果 | 35 个正式核心均有固定 fixture 与独立验收 Case；历史结果不能替代当次验收 |

## 1. 文档职责

本文定义核心进入正式实现清单所需的运行证据、画面判定依据和已知兼容覆盖。ROM/BIOS 的精确来源、哈希、实际 core artifact 路径和测试参数由 [`data/example/fixtures.json`](../data/example/fixtures.json) 维护；该清单中的 35 个正式条目与本章矩阵、独立验收 Case 一一对应。探索结果在提升前不得混入正式清单。工具说明与故障细节见 [`data/example/README.md`](../data/example/README.md)。本文不重复保存一份可能漂移的二进制清单或验收 Case。

## 2. 统一验收入口

三十五个核心必须分别执行 [项目验收规范](./project-acceptance.md) 的 `ACC-CORE-001`–`ACC-CORE-035`。固定命令、帧/画面阈值、当次截图复核、超时与证据格式只在统一文档维护；本专题的矩阵记录已经确认过的兼容事实，不能代替当前版本的验收结果。`ACC-CORE-028` 内含 PPSSPP ISO/CSO 两个独立 run；`ACC-CORE-029`–`035` 分别覆盖 Beetle VB、WonderSwan、SMS Plus GX、两个 FBA2012 家族、Genesis Plus GX Wide 与 Azahar。

## 3. 已验证矩阵

| 核心 | 内容 | 依赖 | 人工确认画面 | 结果 |
| --- | --- | --- | --- | --- |
| `fceumm` | FDS Smash Ping Pong | `disksys.rom` | Family Computer/FDS 启动画面 | 通过 |
| `snes9x` | Dr. Mario | 无 | 标题与玩家选择菜单 | 通过 |
| `gambatte` | Tetris | 无 | 版权启动画面 | 通过 |
| `mgba` | 数独 Advance | `gba_bios.bin` | 标题与菜单 | 通过 |
| `fbneo` | Lode Runner | 当前 machine 无 parent/BIOS | 标题/投币画面 | 通过 |
| `mame2003` | Lode Runner | 当前 machine 无 parent/BIOS | 标题/attract 画面 | 通过，使用第 5 节覆盖 |
| `mame2003_plus` | Lode Runner | 当前 machine 无 parent/BIOS | 标题/投币画面 | 通过 |
| `dosbox_pure` | DOOM II | 线程与跨源隔离 | DOOM II 标题画面 | 通过 |
| `nestopia` | FDS Smash Ping Pong | 条件 `disksys.rom` | Family Computer/FDS 启动画面 | 正式支持 |
| `melonds` | Zoo Keeper | 三个 external BIOS、pointer | Zoo Keeper 标题菜单 | 正式支持 |
| `desmume2015` | Zoo Keeper | pointer | Zoo Keeper 标题菜单 | 正式支持 |
| `desmume` | Zoo Keeper | pointer | Zoo Keeper 标题菜单 | 正式支持 |
| `a5200` | Super Breakout | 7z→`.a52`、`5200.rom` | 游戏画面 | 正式支持 |
| `pcsx_rearmed` | Nekketsu Oyako | 单文件 CHD、`scph5500.bin` | PlayStation/游戏启动画面 | 正式支持 |
| `mednafen_psx_hw` | Nekketsu Oyako | thread、software renderer、`scph5500.bin` | PlayStation/游戏启动画面 | 正式支持 |
| `handy` | Lode Runner | `lynxboot.img`、PersistentSave NONE | 标题/游戏画面 | 正式支持 |
| `yabause` | Saturn fixture 095 | 单文件 CHD、`saturn_bios.bin` | 游戏画面 | 正式支持 |
| `genesis_plus_gx` | Felix the Cat | ZIP→`.md` | 标题画面 | 正式支持 |
| `mupen64plus_next` | Dr. Mario 64 | raw `.z64` | 标题画面 | 正式支持 |
| `parallel_n64` | Dr. Mario 64 | 产品/runtime ID 均为 `parallel_n64` | 标题画面 | 正式支持 |
| `opera` | Total Eclipse | 单文件 CHD、`panafz10.bin` | 游戏画面 | 正式支持 |
| `prosystem` | Asteroids | 7z→`.a78`、BIOS、PersistentSave NONE | 标题画面 | 正式支持 |
| `stella2014` | Freeway | 7z→`.a26`、PersistentSave NONE | 游戏画面 | 正式支持 |
| `picodrive` | Felix the Cat | ZIP→`.md` | 标题画面 | 正式支持 |
| `mednafen_pce` | Adventure Island | raw `.pce` | 游戏画面 | 正式支持 |
| `mednafen_pcfx` | PC Engine Fan Special CD-ROM Vol. 3 | 单文件 CHD、`pcfx.rom` | 光盘内菜单 | 正式支持 |
| `mednafen_ngp` | Pac-Man | raw `.ngp` | 游戏画面 | 正式支持 |
| `ppsspp` | Sheep Defense | raw CSO+ISO、thread、assets、启动动作、PersistentSave NONE | 两种格式标题画面 | 正式支持 |
| `beetle_vb` | Panic Bomber | 4 条有界启动动作，最后一条延迟 25 秒 | 动画开场 | 正式支持 |
| `mednafen_wswan` | Mingle Magnet | 无 BIOS | 标题画面 | 正式支持 |
| `smsplus` | Bank Panic | 无 BIOS | 标题画面 | 正式支持 |
| `fbalpha2012_cps1` | 1941 | 专属 227-machine DAT；目标 machine 无额外 BIOS | attract/游戏画面 | 正式支持 |
| `fbalpha2012_cps2` | Pocket Fighter | 专属 284-machine DAT；目标 machine 无额外 BIOS | 动画开场 | 正式支持 |
| `genesis_plus_gx_wide` | Fix-It Felix Jr. | 4.3.0-pre 定向 artifact | high-score/attract 画面 | 正式支持 |
| `azahar` | Cave Story 2D | 4.3.0-pre thread、pointer、WebGL2 | 中文标题和菜单 | 正式支持 |

Arcade 的 `ldrun.zip` 分别使用三个既有核心自己的 DAT 验证；三者均匹配 20/20 个必需 ROM entry。ZIP 内另有 3 个不属于 `ldrun` 父集要求的成员，不构成缺失或 hash mismatch。`1941.zip` 与 `sgemf.zip` 分别只用 `fbalpha2012_cps1`、`fbalpha2012_cps2` 的绑定 DAT 验证，不能复用 FBNeo 或彼此的 DAT；产品集成测试还必须证明跨家族 DAT 启动被拒绝。DAT 只用于兼容与依赖判断，不参与元信息刮削。

### 3.1 Saturn 多盘基线与负向能力

机器清单另登记受控的 `multidisc-saturn-2`、`multidisc-saturn-3` 与 `multidisc-psx-negative-2` fixture；每个 playlist/disc 的 size、SHA-256、运行 artifact 与 adapter ID 只在 [`fixtures.json`](../data/example/fixtures.json) 维护。Saturn 双盘与三盘基线使用 `yabause`、EmulatorJS 4.2.3 和 `ejs-4.2.3-v2`：必须进入游戏画面、证明 MEMFS 中每张 CHD 均为非零完整下载、回报精确盘数，全部 index 往返后帧继续推进；三盘还要证明在光盘 2 创建的状态存档按“先切盘、后 load state”恢复且 PersistentSave 无回归。机器结果分别保存换盘前的可辨识游戏画面和换盘后当前画面；最终帧恰处内容黑场时仍须保留该截图，并由逐次盘号回读与帧增量判定换盘，不得把黑场当作游戏启动证据。这些历史兼容事实不能替代当前 commit 的 `ACC-MDISC-005`–`006` 证据。

PSX fixture 只作为能力负向对照，不要求执行专有内容：`pcsx_rearmed` 与 `mednafen_psx_hw` 不得投影多盘；3DO `opera` 与 PC-FX `mednafen_pcfx` 因没有受控真实兼容证据同样禁用。确定性 parser/安全测试使用生成的小型 CHD header，不把它宣称为 EmulatorJS 运行证明。

## 4. 可执行验证链路

~~~mermaid
flowchart LR
    A["fixtures.json 固定相对来源与哈希"] --> B["verify-fixtures.py"]
    B --> C["ROM / BIOS / core / DAT 一致"]
    C --> D["serve.py + 隔离响应头"]
    D --> E["真实 Chrome + EmulatorJS"]
    E --> F["start + frame delta + canvas"]
    F --> G["PNG 人工复核"]
    G --> H["latest.json + manual-review.json"]
~~~

执行命令：

~~~bash
python3 data/example/verify-fixtures.py
node data/example/smoke-test.mjs
node data/example/smoke-test.mjs multidisc-saturn-2 multidisc-saturn-3
~~~

机器结果使用整数毫秒字段并写入 [`latest.json`](../data/example/results/latest.json)；人工复核写入 [`manual-review.json`](../data/example/results/manual-review.json)。35 个正式核心对应 `ACC-CORE-001`–`ACC-CORE-035`；PPSSPP 展开为 `ppsspp-cso`、`ppsspp-iso` 两个 run，多盘 Saturn 对应 `ACC-MDISC-005`–`006`。截图包含游戏内容，默认只保存在本机，不提交仓库。

## 5. MAME2003 版本覆盖

EmulatorJS 4.2.3 发布包中的官方 `mame2003` bundle `2.0.2` 在本次 Chrome 环境中存在可重复回归：

- `ldrun` 与 `galaxian` 均通过对应 MAME 0.78 DAT 完整校验；
- 核心日志能够找到正确 game driver；
- normal/legacy/thread 产物、headless/headful Chrome 均在 `retro_load_game` 阶段触发 WASM `unreachable`；
- 因而不能把问题归因为 ROM 缺失、parent/BIOS 缺失或无头浏览器。

一期验证基线保留 EmulatorJS 4.2.3 loader/UI，但为 `mame2003` 精确映射官方 4.2.1 bundle `2.0.1`。旧 bundle 启动后未同步 canvas backing size，示例在 `start` 后将其从默认 300×150 调整到 CSS 像素尺寸，最终画面与其他 Arcade 核心一致。

该覆盖的 URL、大小、SHA-256、build report、core source commit 和理由已记录在 [`data/dat` manifest](../data/dat/emulatorjs/4.2.3/manifest.json)。覆盖源码提交与 4.2.3 core 源码提交中的 `metadata/mame2003.xml` SHA-256 均为 `dacf9d5739ddf386705bc703f7f70239ac61b8ab44c438f76f6658c7156da147`，所以现有 DAT 可继续作为精确输入。

实现要求：

- Core 表不能只保存 `emulatorjs_version`；必须保存实际 artifact URL/path、SHA-256 和兼容配置；
- artifact resolver 对 `mame2003` 应读取版本化覆盖，不能把 `mame2003_plus` 静默替代为 `mame2003`；
- 后续升级先测试新官方 bundle；通过后删除覆盖与 canvas 兼容代码；
- 任何覆盖变化都视为 Core version 变化，旧存档不得默认跨版本加载。

## 6. GBA 内容规范

远端 GBA 样本原始形态是只含一个 Unicode 文件名成员的 ZIP。把 ZIP 直接作为 4.2.3 `EJS_gameUrl` 时，mGBA 只进入 RetroArch `Load Content`，不符合自动启动要求；逐字节提取 `.gba` 后无需其他运行时补丁即可进入游戏标题。

因此导入层应：

- 原样保存用户上传 ZIP Blob 和 SHA-256；
- 在识别到单一受支持 ROM 成员时逐字节物化该 entry 到 CAS，而不是重编码 ROM；
- GameContentRevision 的唯一 `CONTENT` GameContentFile 指向实际传给 EmulatorJS 的 `.gba` Blob，GameVariantRevision 直接引用该 ContentRevision；
- GameContentFile 同时保存来源 archive/entry ordinal，ArchiveEntry 保存原 entry 名与物化 Blob/hash，保证审核与重建可追溯；提取过程不产生另一种 ROM bytes，后端实现版本由审核 source manifest/任务证据记录。

不能在浏览器启动时临时猜测 ZIP 内的入口，也不能因派生成功而丢弃原始上传 Blob。

## 7. DOS 与跨源隔离

`dosbox_pure`、`genesis_plus_gx_wide` 与 `azahar` 使用 EmulatorJS `4.3.0-pre` 定向运行时覆盖，artifact 分别为 `dosbox_pure-thread-wasm.data`、`genesis_plus_gx_wide-wasm.data` 与 `azahar-thread-wasm.data`；其他核心继续使用 4.2.3 或其已记录覆盖。验证服务器返回：

~~~http
Cross-Origin-Opener-Policy: same-origin
Cross-Origin-Embedder-Policy: require-corp
Cross-Origin-Resource-Policy: same-origin
~~~

测试同时断言 `window.crossOriginIsolated === true`。smoke 从未跟踪的原 DOOM II ZIP 确定性生成带根级 `AUTOBOOT.DBP` 的临时派生包；adapter 在 `EJS_ready` 安装 whole-archive 模式后自动点击运行时 start button，帧持续推进且画面通过非空/非均匀检查。生产链路不物化该派生包，而由受限内容端点以 seekable 虚拟 ZIP 等价返回。用户本地 DOS corpus 另执行结构矩阵，覆盖安装器优先、数据首项、嵌套资源和高压缩比空白存档。

## 8. PSP ISO/CSO 与线程产物

PSP profile 正式接受 raw `.cso` 和 `.iso`，均以 `RAW_FILE_V1` 成为独立 CONTENT、Variant 与 Launch，生产导入/Launch/Player 不做格式互转。fixture 的 ISO 只由固定 CSO 通过 `data/example/materialize-cso.py` 确定性派生：ISO 为 11,024,384 bytes，SHA-256 `081e200248ac13c279821023c8cd7bdb1fdd59205129d0d9b46f2814ea583dbf`，sector 16 标识 `CD001`。该脚本只服务测试供应链，物化 ISO 被 Git 忽略。

`smoke-test.mjs ppsspp` 必须生成 `ppsspp-cso` 与 `ppsspp-iso` 两条结果并分别校验请求路径/hash、帧推进与截图。PPSSPP 使用 `ppsspp-thread-wasm.data` 和固定 `ppsspp-assets.zip`；`mednafen_psx_hw` 使用 `mednafen_psx_hw-thread-wasm.data`，固定 software renderer。线程 basename 来自 loader 实测，不得映射成非 thread 名称。

## 9. 统一升级验收入口

升级 EmulatorJS、任一 core 或 Arcade DAT 时执行 [一期项目验收规范](./project-acceptance.md) 的条件 Case `ACC-DAT-006`，并按其引用分别重跑 `ACC-CORE-*`、Arcade 依赖和存档兼容 Case。旧版本保留、hash/来源证据、逐核心画面复核和回滚均属于该 Case 的硬性通过标准。

当前样本只证明 35 个正式 fixture 中锁定的具体内容能进入游戏画面，不代表相应核心支持的全部 ROMset 都已验证。导入审核仍必须按 GameVariantRevision 做兼容性诊断。
