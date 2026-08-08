# 核心运行时验证基线

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已验证 / 一期实现基线 |
| 验证时间 | `1785954879887`（UTC Unix 毫秒） |
| 浏览器 | Chrome `149.0.7827.114` |
| 前端运行时 | EmulatorJS `4.2.3` |
| 结果 | 8/8 核心进入游戏画面 |

## 1. 文档职责

本文定义一期核心能否进入实现清单的既有运行证据、画面判定依据和已知兼容覆盖。ROM/BIOS 的精确来源、哈希、实际 core artifact 路径和测试参数由 [`data/example/fixtures.json`](../data/example/fixtures.json) 维护；该清单还可保存 `supportStatus: "candidate"` 的探索性示例，但候选记录不会自动进入本章矩阵、产品核心清单或正式验收 Case。工具说明与故障细节见 [`data/example/README.md`](../data/example/README.md)。本文不重复保存一份可能漂移的二进制清单或验收 Case。

## 2. 统一验收入口

八个核心必须分别执行 [一期项目验收规范](./project-acceptance.md) 的 `ACC-CORE-001`–`ACC-CORE-008`。固定命令、帧/画面阈值、当次截图复核、超时与证据格式只在统一文档维护；本专题的矩阵记录已经确认过的兼容事实，不能代替当前版本的验收结果。

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

Arcade 的 `ldrun.zip` 分别使用三个核心自己的 DAT 验证；三者均匹配 20/20 个必需 ROM entry。ZIP 内另有 3 个不属于 `ldrun` 父集要求的成员，不构成缺失或 hash mismatch。DAT 只用于兼容与依赖判断，不参与元信息刮削。

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
~~~

机器结果使用整数毫秒字段并写入 [`latest.json`](../data/example/results/latest.json)；人工复核写入 [`manual-review.json`](../data/example/results/manual-review.json)。这两个结果文件可以包含候选示例；只有本章 8 个正式核心对应 `ACC-CORE-001`–`ACC-CORE-008`。截图包含游戏内容，默认只保存在本机，不提交仓库。

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

`dosbox_pure` 使用 `dosbox_pure-thread-wasm.data`。验证服务器返回：

~~~http
Cross-Origin-Opener-Policy: same-origin
Cross-Origin-Embedder-Policy: require-corp
Cross-Origin-Resource-Policy: same-origin
~~~

测试同时断言 `window.crossOriginIsolated === true`。该 DOOM II bundle 保持原 ZIP，smoke 通过 `EJS_externalFiles` 成功请求旁置 `game.conf`，以 `dosbox_pure_conf=outside` 自动启动并进入标题画面。运行后出现一个非阻断 `ErrnoError`，但 frame counter 持续增长且标题序列继续播放；结果中保留该诊断，不将其隐藏。

## 8. 统一升级验收入口

升级 EmulatorJS、任一 core 或 Arcade DAT 时执行 [一期项目验收规范](./project-acceptance.md) 的条件 Case `ACC-DAT-006`，并按其引用分别重跑 `ACC-CORE-*`、Arcade 依赖和存档兼容 Case。旧版本保留、hash/来源证据、逐核心画面复核和回滚均属于该 Case 的硬性通过标准。

当前样本只证明这 8 个具体内容能进入游戏画面，不代表相应核心支持的全部 ROMset 都已验证。导入审核仍必须按 GameVariantRevision 做兼容性诊断。
