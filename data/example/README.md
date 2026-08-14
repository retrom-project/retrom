# EmulatorJS 核心验证夹具

本目录提供 Retrom 35 个正式 EmulatorJS 核心的可重复启动示例和 Chrome 冒烟测试；33 个核心使用 `4.2.3`，`dosbox_pure`、`genesis_plus_gx_wide` 和 `azahar` 由 `4.3.0-pre` 定向覆盖。ROM、BIOS、EmulatorJS 发布包和截图留在本机并由 `.gitignore` 排除，清单、示例和脚本可提交。

## 已记录基线

历史基线曾在 Chrome `149.0.7827.114` 验证原 8 核，在 Chrome `149.0.7827.55` 验证扩展后的 28 核；2026-08-11 又将 7 个完成同等真实画面验证的核心提升为正式清单，当前总数为 35。当前机器结果见 [`results/latest.json`](./results/latest.json)，逐图复核记录见 [`results/manual-review.json`](./results/manual-review.json)；历史结果只说明夹具已知状态，不能作为未来版本的当次验收证据。

正式验收必须分别执行 [`project-acceptance.md`](../../docs/project-acceptance.md) 的 `ACC-CORE-001`–`ACC-CORE-035`，使用当次新生成的机器结果和截图，并遵守其中的画面阈值、人工复核与单 Case 超时。PPSSPP 会生成 `ppsspp-cso.png` 与 `ppsspp-iso.png`；其他截图为 `data/example/results/<core>.png`，均不进入 Git。

## 夹具矩阵

| 核心 | 本地内容 | 画面证据 | BIOS | Arcade DAT |
| --- | --- | --- | --- | --- |
| `fceumm` | `data/game/fceumm/fds-smoke.zip` | Famicom/FDS 版权启动画面 | `disksys.rom` | — |
| `snes9x` | `data/game/snes9x/snes-smoke.zip` | Dr. Mario 标题菜单 | — | — |
| `gambatte` | `data/game/gambatte/gb-smoke.gb` | Tetris 版权启动画面 | — | — |
| `mgba` | `data/game/mgba/gba-smoke.gba` | 数独 Advance 标题菜单 | `gba_bios.bin` | — |
| `fbneo` | `data/game/arcade/ldrun.zip` | Lode Runner 标题/投币画面 | 此 machine 无额外依赖 | `fbneo-arcade.dat`，20/20 entries |
| `mame2003` | `data/game/arcade/ldrun.zip` | Lode Runner 标题/演示画面 | 此 machine 无额外依赖 | `mame2003.xml`，20/20 entries |
| `mame2003_plus` | `data/game/arcade/ldrun.zip` | Lode Runner 标题/投币画面 | 此 machine 无额外依赖 | `mame2003-plus.xml`，20/20 entries |
| `dosbox_pure` | `data/game/dosbox_pure/doom2.zip` | DOOM II 标题画面 | — | — |

## 4.2.3 扩展核心

以下 20 个扩展核心均使用 `supportStatus: "supported"`，并已进入 4.2.3 依赖 manifest、产品目录和正式验收：

| 核心 | 固定样本 | 额外文件/处理 | 预期可辨识画面 |
| --- | --- | --- | --- |
| `nestopia` | FDS Smash Ping Pong | `disksys.rom` | Famicom/FDS 启动画面 |
| `melonds` | Zoo Keeper | `bios7.bin`、`bios9.bin`、`firmware.bin` | 动物管理员标题菜单 |
| `desmume2015` | Zoo Keeper | — | 动物管理员标题菜单 |
| `desmume` | Zoo Keeper | — | 动物管理员标题菜单 |
| `a5200` | Super Breakout | `5200.rom` | Super Breakout 游戏画面 |
| `pcsx_rearmed` | Nekketsu Oyako | `scph5500.bin` | PlayStation/游戏启动画面 |
| `mednafen_psx_hw` | Nekketsu Oyako | `scph5500.bin`、thread artifact、software renderer | PlayStation/游戏启动画面 |
| `handy` | Lode Runner | `lynxboot.img` | Lode Runner 标题/游戏画面 |
| `yabause` | Saturn fixture 095 | `saturn_bios.bin` | Sega Saturn 游戏画面 |
| `genesis_plus_gx` | Felix the Cat | — | Felix the Cat 标题画面 |
| `mupen64plus_next` | Dr. Mario 64 | — | Dr. Mario 64 标题画面 |
| `parallel_n64` | Dr. Mario 64 | 产品/runtime/artifact ID 均为 `parallel_n64` | Dr. Mario 64 标题画面 |
| `opera` | Total Eclipse | `panafz10.bin` | Total Eclipse 游戏画面 |
| `prosystem` | Asteroids | `7800 BIOS (U).rom` | Asteroids 标题画面 |
| `stella2014` | Freeway | — | Freeway 游戏画面 |
| `picodrive` | Felix the Cat | — | Felix the Cat 标题画面 |
| `mednafen_pce` | Adventure Island | — | Adventure Island 游戏画面 |
| `mednafen_pcfx` | PC Engine Fan Special CD-ROM Vol. 3 | `pcfx.rom` | 光盘内游戏菜单 |
| `mednafen_ngp` | Pac-Man | 从来源 ZIP 固定提取 `.ngp` | Pac-Man 游戏画面 |
| `ppsspp` | Sheep Defense | raw CSO+ISO 两个 run、thread artifact、PPSSPP assets、自动确认启动提示 | 两种格式均到 Sheep Defense 标题画面 |

`melonds` 的 BIOS 必须放到 4.2.3 RetroArch system 目录 `/retroarch/userdata/system/`；写到虚拟文件系统根目录会回退到 FreeBIOS 并停在空白双屏。`mednafen_psx_hw` 在本机无 GPU 的 Chrome/SwiftShader 硬件渲染路径会于启动阶段失败，因此候选示例显式选择同一 core 的 software renderer；本记录不声称硬件渲染路径已验证。

## 新增 7 个正式核心

以下夹具已进入产品 manifest、平台目录和 `ACC-CORE-029`–`035`；不再使用候选语义。

| 核心 | Runtime | 固定验证内容 | BIOS/父集 |
| --- | --- | --- | --- |
| `beetle_vb` | `4.2.3` | Virtual Boy / Panic Bomber | — |
| `mednafen_wswan` | `4.2.3` | WonderSwan / Mingle Magnet | — |
| `smsplus` | `4.2.3` | Master System / Bank Panic | — |
| `fbalpha2012_cps1` | `4.2.3` | CPS-1 / 1941 | 此 machine 无额外 BIOS |
| `fbalpha2012_cps2` | `4.2.3` | CPS-2 / Pocket Fighter (`sgemf`) | 独立父集，此 machine 无额外 BIOS |
| `genesis_plus_gx_wide` | `4.3.0-pre` | Mega Drive / Fix-It Felix Jr. | — |
| `azahar` | `4.3.0-pre` | Nintendo 3DS / Cave Story 2D | thread artifact，无 BIOS |

另有 16 个未接入核心未生成运行示例，因为授权来源中缺少对应游戏内容；仅有 BIOS 或支持文件不算可运行验证内容：

| 核心 | 来源盘点结果 |
| --- | --- |
| `81`、`fuse` | 有部分 Spectrum/Fuse 系统文件，但无 Spectrum 游戏 |
| `cap32`、`crocods` | 无 Amstrad CPC 游戏 |
| `gearcoleco` | 有 ColecoVision BIOS，但无 ColecoVision 游戏 |
| `puae` | 有 Amiga Kickstart/BIOS，但无 Amiga 游戏磁盘 |
| `same_cdi` | 有 CD-i BIOS 包，但无 CD-i 游戏媒体 |
| `freeintv` | 有 Intellivision BIOS，但无 Intellivision 游戏 |
| `vice_x64`、`vice_x64sc`、`vice_x128`、`vice_xpet`、`vice_xplus4`、`vice_xvic` | VICE 系统文件不完整，且无对应 Commodore 游戏 |
| `virtualjaguar` | 无 Jaguar 游戏 |
| `prboom` | 只有 `prboom.wad` 支持文件，无可启动的 Doom IWAD/PWAD |

来源相对路径、文件大小、SHA-256、BIOS MD5、core artifact 和超时值以 [`fixtures.json`](./fixtures.json) 为唯一事实源。远端 host/root 由操作者分别通过 `RETROM_FIXTURE_HOST`、`RETROM_FIXTURE_ROOT` 提供，不写入仓库。GBA 与 NGP 原始 ZIP 被保留作来源证据；示例使用其中逐字节解出的 ROM，因为 4.2.3 直接传入这些 ZIP 会进入 RetroArch `Load Content`，不满足自动启动要求。

schema v3 的 `multiDiscFixtures` 另锁定 Saturn 双盘、三盘和 PSX capability 负向证据。Saturn CHD 必须由有权使用内容的操作者放到清单给出的 `data/game/yabause/multidisc-saturn-{2,3}/disc-NNN.chd`；清单不保存来源主机路径，通用 `fetch-fixtures.sh` 也不猜测本地目录结构。仓库只提交 canonical M3U，小文件与每张盘都由 `verify-fixtures.py` 按 size/SHA-256 校验。PSX 负向只检查 manifest capability，不要求放置或运行专有 bytes。

PPSSPP 的 `game` 是固定 CSO，`formatVariants` 中的 ISO 由 `materialize-cso.py` 以 CISO v1 块流确定性派生。脚本只接受清单固定源 hash，校验 index/offset/块长度、目标 size/hash 与 ISO sector 16 的 `CD001`，写同目录临时文件后原子替换；生产导入和 Player 从不调用它。`smoke-test.mjs ppsspp` 自动展开两个独立 run。

三个 Atari fixture 保持 7z 来源，`expectedMaterializedMember` 固定运行时 raw member 的 name/size/CRC32/SHA-256。standalone 页可保留原有核心启动证据；正式产品导入必须证明 Launch 返回的是物化 raw bytes，而不是来源 7z。

## MAME2003 兼容覆盖

官方 EmulatorJS 4.2.3 的 `mame2003` bundle `2.0.2`（normal、legacy 和 thread 分支）在 Chrome 中能够识别 DAT-valid 的 `ldrun`/`galaxian` driver，但随后在 `retro_load_game` 阶段触发 WASM `unreachable`。因此本验证使用：

- 前端运行时：EmulatorJS `4.2.3`；
- `mame2003` core：官方 EmulatorJS `4.2.1` bundle `2.0.1`；
- core SHA-256：`1d8283ce042f71607b9b55656cd4068f703c52faa7a3d0940855c9dd21d542df`；
- DAT：现有 MAME 0.78 `mame2003.xml`。它在覆盖核心源码提交与 4.2.3 核心源码提交中逐字节相同；
- 启动兼容处理：`start` 后把旧 core 留下的 300×150 canvas backing size 同步到实际 CSS 尺寸。

这不是把 `mame2003_plus` 冒充为 `mame2003`。具体证据已写入 `data/dat/emulatorjs/4.2.3/manifest.json`；未来升级 EmulatorJS 时必须先移除覆盖再复测，不能永久继承该例外。

## 运行

若需从用户授权服务器重新获取夹具并还原固定运行时/Arcade DAT：

~~~bash
export RETROM_FIXTURE_HOST='<operator-provided-host>'
export RETROM_FIXTURE_ROOT='<operator-provided-absolute-root>'
data/example/fetch-fixtures.sh
make prepare-deps
~~~

夹具脚本没有默认主机/根目录，使用现有 SSH agent 或用户 SSH config；所有文件在写入 `data/game` 前都会校验 SHA-256。脚本只读取清单中的相对路径，不扫描或复制源目录中的其他游戏。`make prepare-deps` 只读取公开的小型版本 manifest，完整规则见[依赖管理](../../docs/dependency-management.md)。

先校验所有本地二进制、BIOS 和 DAT：

~~~bash
python3 data/example/verify-fixtures.py
~~~

运行全部核心，或只运行指定核心：

~~~bash
node data/example/smoke-test.mjs
node data/example/smoke-test.mjs mgba mame2003
node data/example/smoke-test.mjs multidisc-saturn-2 multidisc-saturn-3
~~~

新增 7 核的专有内容只从操作者明确指定的本地根目录物化到被忽略目录；脚本只读取 `fixtures.json` 锁定的相对路径，并在原子写入前校验 size/SHA-256：

~~~bash
python3 data/example/materialize-fixtures.py --source-root '<operator-provided-absolute-root>' beetle_vb mednafen_wswan smsplus
python3 data/example/verify-fixtures.py beetle_vb mednafen_wswan smsplus
RETROM_EXAMPLE_RESULTS_DIR=.artifacts/core-smoke \
  node data/example/smoke-test.mjs beetle_vb mednafen_wswan smsplus
~~~

省略 materializer selector 会物化全部 7 个本地夹具。二进制写入 `data/example/local-fixtures/`，当次机器结果和截图写入 `RETROM_EXAMPLE_RESULTS_DIR`，两者均不进入 Git。机器 `PASS` 仍必须人工确认截图确为指定游戏标题或游戏画面，而不是系统 Logo、启动提示、菜单或错误页。

需要验证 FBA2012 的真实 DAT 导入、Variant 与 Launch 隔离时，在本地夹具校验通过后显式启用 `localfixtures` Go build tag；该测试不属于默认 `make integration-test` 或 `make ci`：

~~~bash
go test -tags='integration localfixtures' ./internal/libraryimport -run '^TestFBA2012' -count=1
~~~

验收 runner 可用同名 selector 只校验当前 Case 的受控 bytes，例如 `python3 data/example/verify-fixtures.py mgba` 或 `python3 data/example/verify-fixtures.py multidisc-saturn-2`。不传 selector 仍校验完整清单；某个尚未物化的专有多盘 fixture 不得阻断无关核心的独立验收，也不能被无关核心的通过结果掩盖。

脚本会启动带 COOP/COEP/CORP 头的本地服务，通过 Chrome DevTools Protocol 驱动真实 Chrome，保存 canvas 截图，并覆盖 `results/latest.json`。4.2.3 standalone 页与产品 adapter 使用同一版本边界的 external-file 兼容策略：在 loader 定义 `EJS_GameManager` 时把裸 `ArrayBuffer` 写入归一化为 `Uint8Array`；多盘 smoke 随后把每张盘的 MEMFS 长度逐项对照 fixture 锁定的 size。多盘 run 先保存 `<fixture>-initial.png` 作为换盘前游戏画面，再保存 `<fixture>.png` 作为换盘后当前画面；`latest.json` 同时记录两者的画面统计、盘号回读、帧增量与 external file sizes。可用环境变量：

- `RETROM_CHROME_BIN`：Chrome 路径，默认 `/usr/bin/google-chrome`；
- `RETROM_EXAMPLE_PORT`：服务端口，默认 `4173`；
- `RETROM_CHROME_HEADFUL=1`：诊断时使用有界面 Chrome。

手动打开示例：

~~~bash
python3 data/example/serve.py
~~~

随后访问 `http://127.0.0.1:4173/data/example/<core>/`。示例为核心验证页，故不请求浏览器全屏；产品 Player Shell 仍按运行时专题的“一次点击、默认全屏、自动开始”契约实现。

## 二进制放置

- 游戏与 BIOS：`data/game/`；
- EmulatorJS 4.2.3 发布包及解压运行时：`data/runtime/emulatorjs/4.2.3/`；
- MAME2003 兼容 core：`data/runtime/emulatorjs/4.2.3/overrides/`；
- 每个核心的可打开示例：`data/example/<core>/index.html`。

这些游戏和 BIOS 只用于用户有权使用的本地验证，不随源码分发。
