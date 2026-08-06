# EmulatorJS 核心验证夹具

本目录提供 Retrom 一期 8 个核心的可重复启动示例和 Chrome 冒烟测试。运行时锁定 EmulatorJS `4.2.3`；ROM、BIOS、EmulatorJS 发布包和截图留在本机并由 `.gitignore` 排除，清单、示例、脚本及 JSON 结果可提交。

## 已记录基线

既有全量验证在 Chrome `149.0.7827.114` 下为 **8/8 通过**。机器结果见 [`results/latest.json`](./results/latest.json)，逐图复核记录见 [`results/manual-review.json`](./results/manual-review.json)；这些记录用于说明夹具已知状态，不能作为未来版本的当次验收证据。

正式验收必须分别执行 [`project-acceptance.md`](../../docs/project-acceptance.md) 的 `ACC-CORE-001`–`ACC-CORE-008`，使用当次新生成的机器结果和截图，并遵守其中的画面阈值、人工复核与单 Case 超时。截图保存在本机 `data/example/results/<core>.png`，不进入 Git。

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

来源相对路径、文件大小、SHA-256、BIOS MD5、core artifact 和超时值以 [`fixtures.json`](./fixtures.json) 为唯一事实源。远端 host/root 由操作者分别通过 `RETROM_FIXTURE_HOST`、`RETROM_FIXTURE_ROOT` 提供，不写入仓库。GBA 原始 ZIP 被保留作来源证据；示例使用其中逐字节解出的 `.gba`，因为 4.2.3 直接传入该 ZIP 会进入 RetroArch `Load Content`，不满足自动启动要求。

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
~~~

脚本会启动带 COOP/COEP/CORP 头的本地服务，通过 Chrome DevTools Protocol 驱动真实 Chrome，保存 canvas 截图，并覆盖 `results/latest.json`。可用环境变量：

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
