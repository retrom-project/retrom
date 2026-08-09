# EmulatorJS 运行时、快速启动与游玩数据

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期实施基线 |
| 版本 | 1.0 |
| 日期 | 2026-08-06 |
| EmulatorJS 基线 | v4.2.3 |

## 1. 用户可观察契约

从游戏详情“开始游戏”、详情存档“继续”、“我的存档”或首页“继续游戏”发起时，一次点击完成“请求全屏 → 启动预检 → 加载运行时 → 自动开始”。正常路径没有 Retrom 的第二个 Start，也不显示 EmulatorJS `Play Now`。

游戏库卡片仍进入详情；只有语义明确的开始/继续按钮直接启动。存档入口使用存档锁定的 Core、CoreArtifact、GameVariantRevision、DOS entry 和文件，不受平台目录当前默认核心覆盖。

## 2. 一次点击与 Player Shell

~~~mermaid
sequenceDiagram
    actor U as 用户
    participant UI as Next.js Player Shell
    participant FS as Fullscreen API
    participant API as Go Launch API
    participant EJS as 同源 EmulatorJS iframe
    U->>UI: 点击开始 / 继续
    UI->>FS: 同步 requestFullscreen()
    UI->>UI: 在当前文档显示常驻全屏加载壳
    UI->>API: POST /api/v1/launches
    alt 已 READY，预检通过
      API-->>UI: launchId + HttpOnly capability cookie
      UI->>UI: replace 到 /play/{launchId}
      UI->>EJS: 加载 /runtime/launches/{launchId}/config
      EJS->>EJS: startOnLoaded=true
      EJS-->>U: 游戏开始
    else 所选 Core 需要物化验证
      API-->>UI: 202 VALIDATION_PENDING + jobId
      UI->>UI: 在同一全屏加载壳等待 Job
      UI->>API: Job 成功后用新幂等键自动重调 launch
      API-->>UI: launchId + HttpOnly capability cookie
      UI->>EJS: 自动加载并开始
    else 存在 Blocker
      API-->>UI: 422 LAUNCH_BLOCKED
      UI->>FS: exitFullscreen()
      UI-->>U: 回来源上下文并显示修复入口
    end
~~~

实现约束：

- `document.documentElement.requestFullscreen({navigationUI:"hide"})` 必须处于原始 click handler 的同步调用链，不能先 await API。选择始终存在的 document element，避免对尚为 `display:none`、即将被 React 重建的 Player 节点请求全屏。
- Player controller/provider 放在 Next.js 根 layout 的常驻 Client Component。原始 click 同步请求全屏后，在来源 URL 上立即显示该 controller 的全视口 loading overlay；launch 成功才用 App Router `replace` 到 `/play/{launchId}`，且根 layout/controller 不得 remount。失败则移除 overlay、退出全屏并留在来源路由。不得引入 `/play/pending`、整页 navigation 或在 API 返回前伪造 launchId。加载壳只有阶段/错误/退出，没有第二个开始按钮；直接刷新 `/play/{launchId}` 则由同一 controller 从 cookie bootstrap。
- 全屏被浏览器策略拒绝时，游戏仍在无普通导航的 Player Shell 自动运行，并显示非阻塞“进入全屏”。
- 刷新 `/play/:launchId` 时，有本浏览器 launch cookie 才可恢复资源；无 cookie 显示“启动会话不可用”。因刷新没有 user activation，可以显示一次“进入全屏并继续”，但游戏 Start 仍不二次出现；Chrome 若同时阻止 AudioContext，画面继续自动运行并显示“点击恢复全屏/声音”，该点击只 resume audio/request fullscreen，不重新创建 Launch 或调用第二次 game start。
- `Escape` 只退出浏览器全屏；左上返回与更多菜单中的“退出游戏”都先打开 Player 内的影响确认窗，确认后才刷新持久存档、finish/revoke launch 并返回 allowlist 的 `returnTo`。取消确认不改变运行状态。

## 3. Launch API 与凭据

唯一创建入口是 `POST /api/v1/launches`，body 包含 `gameId`、可空字符串 `coreId`、`saveStateId`、`dosEntry`、站内 `returnTo` 和当前 Chrome 的 `clientCapabilities`。精确 request/response、Idempotency-Key、cookie、TTL 与内容路径见 [HTTP API 契约](./http-api-contract.md)。

关键规则：

- 普通启动省略 `coreId` 时使用 PlatformInstance 默认核心；显式核心只能来自基础平台启用集合。
- 从存档启动时服务端解析精确 VariantRevision；客户端即使提交 core，也只能与存档相同。
- URL 只含非秘密 UUIDv7 `launchId`。32-byte capability 只在路径限定的 HttpOnly cookie 中，数据库只保存其 SHA-256；不把 token 放入 URL、JSON、日志、Referer 或诊断。
- 已 READY 的预检成功返回 `201`；需新验证时返回 `202 VALIDATION_PENDING` 且不签发 credential，Player 在同一加载 overlay 等待 Worker，成功后以新幂等键自动重调。Blocker 返回 `422 LAUNCH_BLOCKED`。整个过程没有第二个 Start/确认页；Warning 不增加确认步骤。
- `VARIANT_REVALIDATE` 按 gameVariant/input digest 跨请求去重且不可由单个 Player 取消；退出加载壳只终止本页订阅并退出全屏，后台任务继续，避免一个朋友中断另一个朋友正在等待的同一验证。
- 默认核心不可运行时不静默尝试其他核心。

## 4. 启动预检

服务端按固定顺序，在创建 credential 前完成：

1. Game 状态为 `PUBLISHED`，PlatformInstance 启用且关系有效；
2. 解析存档锁定核心、显式核心或目录默认核心；
3. PlatformCore 与 Core 启用；
4. 存档路径解析锁定 revision；普通路径对所选 core 调用平台目录专题的幂等 `EnsureVariant`，取得直接引用 `Game.current_content_revision_id` 的 READY GameVariantRevision；
5. 当前或存档锁定的 GameVariantRevision 可用，CoreArtifact 文件命中 manifest；
6. 内容 Blob 可读且 size/hash 未漂移；
7. Arcade Variant 锁定的 DatVersion 可用；
8. parent、BIOS/base archive 和必需 entry 完整；
9. SaveState 与 VariantRevision/CoreArtifact 完全匹配；
10. DOS entry 属于该 revision，派生 bundle 可生成；
11. 需要线程的 core 已在客户端能力摘要中满足安全上下文、跨源隔离和 SharedArrayBuffer。

服务端错误码统一使用 `LAUNCH_` 前缀。缺少依赖、损坏内容、存档不兼容、DOS entry 消失和线程环境不满足是 Blocker；BIOS 文件存在但期望 hash 不同、可选 BIOS 缺失和 Fullscreen 拒绝是 Warning。客户端按 `level/code/details` 渲染，不能从中文 message 猜测。

## 5. 固定 EmulatorJS 配置

每次启动创建新的同源 iframe，由 config 的 `playerAdapterId` 选择显式 adapter，先写完该 adapter 定义的全部 `window.EJS_*`，再加载同一 config 锁定版本的 `loader.js`；退出时移除 iframe、listener、object URL 和 timer。当前基线只有 v4.2.3 adapter。CSP `connect-src 'self'` 与完整依赖门禁共同禁止 loader 的 CDN fallback。

最低配置：

```javascript
window.EJS_player = "#game";
window.EJS_pathtodata = config.runtimeBaseUrl;
window.EJS_core = config.runtimeCore;
window.EJS_gameName = config.gameName;
window.EJS_gameID = config.emulatorGameId;
window.EJS_gameUrl = config.gameUrl;
window.EJS_threads = config.requiresThreads;
window.EJS_startOnLoaded = true;
window.EJS_fullscreenOnLoaded = false;
window.EJS_language = "zh-CN";
window.EJS_disableAutoLang = false;
window.EJS_disableDatabases = true;
window.EJS_disableLocalStorage = true;
window.EJS_CacheLimit = 0;
window.EJS_Buttons = { exitEmulation: false };
window.EJS_paths = config.runtimePathOverrides;
window.EJS_defaultOptions = config.defaultCoreOptions;
window.EJS_externalFiles = config.externalFiles;

if (config.biosUrl !== null) window.EJS_biosUrl = config.biosUrl;
if (config.parentUrl !== null) window.EJS_gameParentUrl = config.parentUrl;
if (config.stateUrl !== null) window.EJS_loadStateURL = config.stateUrl;
```

`core` 是产品/数据库 core ID，只用于展示与审计；`runtimeCore` 是锁定 artifact compatibility V2 的 EmulatorJS core ID，只有它可以写入 `EJS_core`。`persistentSaveMode`、`inputMode`、`startupActions` 和 `externalFiles` 同样由该配置返回，Player 只做封闭 schema 校验，不按 `ppsspp`、`melonds` 或显示名推导行为。

`EJS_fullscreenOnLoaded` 必须为 `false`：全屏由 Retrom host 在用户手势中唯一管理，避免 loader 稍后重复请求。`EJS_Buttons.exitEmulation=false` 从运行时配置移除 EmulatorJS 自带退出按钮，退出只能经过 Retrom 的确认、持久存档刷新和 PlaySession 结束流程。语言固定 `zh-CN`。v4.2.3 `loader.js` 对 `EJS_disableAutoLang` 的判断是 `!== false`，因此这里必须显式设为 `false` 才会禁用 system locale 分支；不能凭变量名改成 `true`。这样只请求 manifest 中的 `zh-CN.json`。`EJS_disableDatabases=true` 在 v4.2.3 只把 ROM/BIOS/core asset cache 换成 dummy storage，`EJS_disableLocalStorage=true` 关闭设置持久化，`EJS_CacheLimit=0` 防止 ROM cache；它们并不会关闭 `/data/saves` 的 IDBFS，也不会阻止 `saveDatabaseLoaded`。Retrom 必须按第 6 节显式覆盖/清理该 IDBFS 路径，才能让服务端 PersistentSave 成为事实源；不得把开关名称误解为“所有 IndexedDB 均已禁用”。`EJS_gameID` 来自精确 GameVariantRevision 的稳定数字 surrogate，而不是 Game ID。

`runtimeBaseUrl` 与 `loaderUrl` 必须锁定 Launch 所选 CoreArtifact 的精确 `emulatorjs_version`，不能固定取当前 active 版本。对基线 v4.2.3，它们分别是 `/runtime/emulatorjs/4.2.3/data/` 与 `/runtime/emulatorjs/4.2.3/data/loader.js`；通用派生规则是给该版本 manifest 的 `emulatorjs.player_adapter.runtime_base_path_in_release/loader_path_in_release` 加 `/runtime/emulatorjs/<exact-version>/` 前缀，并要求 loader 属于 runtime base 且两者都命中 allowlist。它们只由 config 返回，前端不得拼版本、猜目录或回退 active 版本。`gameName` 固定为 `retrom-<emulatorGameId>`，只使用 ASCII 字母、数字与连字符，使 EJS 的 save key 在元信息重命名后仍稳定。

`runtimePathOverrides` 对每个已接受版本精确包含一个键：该版本 loader 对所选 artifact 实际请求的 basename；值是该 CoreArtifact 的固定同源 URL。这两个值只由 CoreArtifact 的已校验 `compatibility_config_json.requestedArtifactBasename`、`emulatorjs_version` 和 `relative_path` 派生。v4.2.3 的普通 artifact 例如 `{"mgba-wasm.data":"/runtime/emulatorjs/4.2.3/data/cores/mgba-wasm.data"}`；`mame2003` 必须是 `{"mame2003-wasm.data":"/runtime/emulatorjs/4.2.3/overrides/mame2003-4.2.1-wasm.data"}`。key 不是 CoreArtifact ID，也不是 override 文件自身 basename。v4.2.3 的 `emulator.min.js` 会以 `requestedPath.split("/").pop()` 查 `EJS_paths`；这一映射是选择 4.2.1 override 而不误取 4.2.3 文件的必要条件。其余 loader、CSS、语言、archive helper 和 core report 都从本次 config 的 runtime base 读取，不增加浮动 URL。`defaultCoreOptions` 先放固定 `webgl2Enabled: "enabled"`，再按 Requirement ID 合并本次 VariantRevision 依赖快照中适用、已装入 BIOS bundle 的 `activation_options_json`；DOS 直接启动最后加入 `dosbox_pure_conf: "outside"`，并仅为该 Launch 返回 `externalFiles={"/game.conf":"/runtime/launches/<launchId>/dos-config/game.conf"}`。任何重复 key 异值在验证阶段失败，不能靠合并顺序覆盖。这样 Gambatte/mGBA 上传的启动 BIOS 会实际启用，而缺失可选 BIOS 不会被误升为 Blocker。这些 key/value 必须来自对应版本 loader、静态 BIOS catalog和锁定 artifact 的集成测试，不能由前端按显示名称猜测。`canvasResizePolicy` 也只从该配置读取；`ON_GAME_START_TO_CSS_PIXELS` 在 game-start callback 把 canvas backing `width/height` 设为正整数 `clientWidth/clientHeight`，v4.2.3 仅锁定的 `mame2003` override 使用。

Player adapter 使用 manifest 声明的 `playerAdapterId → adapter` 显式 registry，不允许默认分支把未知 ID/版本当成 v4.2.3。机器可读 registry 固定为 `web/features/player/adapters/registry.json`，当前只登记 `ejs-4.2.3-v1 → 4.2.3`；同目录 TypeScript 实现必须与 JSON 双向一一对应，`make data-check` 校验每份依赖 manifest 的 ID/版本均命中它。v4.2.3 adapter 的 globals、event 顺序、IDBFS 规则和 callback 以本文为准。未来版本若行为完全兼容也必须新增精确 ID 并跑版本升级门禁；若行为变化则新增独立 adapter。浏览器收到未知或版本不匹配的 ID 时必须在加载 loader 前终止为 `PLAYER_ADAPTER_UNSUPPORTED`，不得回退 active 版本或任意旧 adapter。后端和前端镜像必须来自通过同一次 `data-check` 的同一版本化项目发布；一期不声称两个独立进程能在启动时互相检查镜像内容。

Player Shell 创建同源 `about:blank` iframe，由父页面在 iframe document 中建立唯一 `#game` 容器、设置上述 globals、注册 callback，最后追加 `src=config.loaderUrl` 的 script；不使用 `srcdoc` inline script、跨源 frame 或 `document.write`。iframe 继承父页面 origin/CSP，所有内容请求会按 `/runtime/launches/<launchId>/` 路径自动携带 capability cookie。只有 config 校验与可选 PersistentSave 预读完成后才加载 loader。

Player canvas contain 必须优先使用锁定运行时 `gameManager.getVideoDimensions("aspect")` 的正数结果，只有 game-start 前尚不可用时才回退 drawing-buffer `canvas.width/canvas.height`。这能处理 drawing buffer 仍为横向但核心实际输出为 3:4 等竖屏画面的情况：竖屏画面 CSS 高度贴满 `100dvh`，左右保留必要黑边，不能误在上下留下黑边。viewport、canvas 属性或核心比例变化时必须重新计算，不能拉伸或裁切。

Player config 额外提供人类可读的 `gameTitle/coreName/platformName`，只用于 58px 顶部工具栏显示本次游戏、运行核心和基础平台；EJS 的稳定保存键仍只使用 `gameName=retrom-<emulatorGameId>`，前端不得把展示名称用于选择 artifact、URL 或 option。

Retrom 顶部工具栏是运行中的暂停边界：点击工具栏任意区域或其中任一操作，都先调用 `gameManager.toggleMainLoop(false)` 并同步设置实例 `paused=true`；工具栏内不提供恢复动作，只有随后点击实际游戏画面才调用 `toggleMainLoop(true)` 并恢复 `paused=false`。同源 iframe 内只把非 EmulatorJS 工具栏、弹窗、按钮或输入控件的画面点击映射为恢复，避免用户调整原生设置时误启动游戏。该状态进入后续 heartbeat/finish 的 `previousInterval.paused`，暂停区间不累计有效游玩时长；暂停期间工具栏和画面中央“点击游戏画面继续”浮层保持可见。EmulatorJS 底部工具栏默认由 iframe 级显示门禁锁定，启动时的 `menu.open()`、靠近底边和画面点击都不能自行显示；只有 Retrom 更多菜单中的“模拟器设置”解除门禁并调用本次 v4.2.3 实例的真实 `menu.open()`。运行时把工具栏重新收起时门禁自动恢复。这样继续使用 EJS 已装载的控制、显示、Core 和音量设置，同时不复制一套会与运行时状态分叉的设置面板。

映射：

| Retrom | EmulatorJS v4.2.3 |
| --- | --- |
| 内容 | `EJS_gameUrl` |
| 一个确定性 BIOS bundle | `EJS_biosUrl` |
| 一个确定性 parent bundle | `EJS_gameParentUrl` |
| 手动状态存档 | `EJS_loadStateURL` |
| 线程 core | `EJS_threads` |
| 自动开始 | `EJS_startOnLoaded = true` |
| Host 管理全屏 | `EJS_fullscreenOnLoaded = false` |

线程核心的 override key 必须使用 loader 实测 basename：`dosbox_pure-thread-wasm.data`、`mednafen_psx_hw-thread-wasm.data`、`ppsspp-thread-wasm.data`。MelonDS 的 `externalFiles` 精确包含 `/retroarch/userdata/system/bios7.bin`、`bios9.bin`、`firmware.bin` 三个虚拟路径，URL 只能指向本 Launch 的 `/external-files/<logicalName>`；这些 Blob 在创建 Launch 时锁定，不能在 config GET 时重新选择 active BIOS。DOS `/game.conf` 与 BIOS external mapping 合并时，虚拟路径或 logical name 冲突必须阻断。

NDS 三核心的 `inputMode=POINTER`：Player 不向 iframe 合成额外的 `pointerdown/click`，真实浏览器事件直接到 EmulatorJS canvas。其他核心为 STANDARD。PPSSPP 的两条 `startupActions` 只在一次 `onGameStart` 后分别延迟 2,000/5,000ms 调用 `simulateInput(0,0,1)`，120ms 后释放；Strict Mode 重入不得重复调度，unmount/失败/退出必须取消 timer 并释放已按下控制，最后一次释放后不再自动输入。这是版本绑定的有限启动动作，不是通用宏功能。

## 6. v4.2.3 事件适配器

不得使用不存在的 `EJS_onExit` 或 `EJS_onSaveUpdate`。v4.2.3 loader 只直接映射 `EJS_ready`、`EJS_onGameStart`、`EJS_onLoadState`、`EJS_onSaveState`、`EJS_onLoadSave` 和 `EJS_onSaveSave`。

在 `EJS_ready` 中向 `window.EJS_emulator` 追加监听：

- `exit`：触发最后一次持久保存、finish 和资源销毁；
- `saveDatabaseLoaded`：取得 FS，注入服务端当前 PersistentSave；
- `saveSaveFiles`：接收 core 定时/退出时产生的持久 bytes；
- 浏览器 `pagehide`：使用 `fetch(..., {keepalive:true})` 尝试最后 heartbeat/finish；服务端仍以最后已确认心跳截断。

`EJS_onSaveState` 的真实 payload 是 `{ screenshot: Blob, format: string, state: Uint8Array }`。Retrom 必须同时上传非空 state 与截图，任一失败都不创建 SaveState。`EJS_onSaveSave` 是用户手动导出持久保存时的 `{ screenshot, format, save }`，自动同步则监听 `saveSaveFiles`，两条路径去重到同一 PersistentSave service。

PersistentSave 预载必须避免事件竞态：Launch 创建时锁定当时可空 current revision，Player 在加载 `loader.js` 前从本次受限 URL 把该精确 revision 完整读入一个 `Uint8Array`；另一会话稍后的保存不能改变本次 GET。服务端和客户端共同硬限 64 MiB，超限在 loader 启动前以 `LAUNCH_PERSISTENT_SAVE_TOO_LARGE` 阻断，不能在主线程复制 512 MiB 级对象。`EJS_ready` 立即注册 `saveDatabaseLoaded/saveSaveFiles/exit` listener。`saveDatabaseLoaded` 在 v4.2.3 中无条件发生于 IDBFS `mount + syncfs(true)` 后、ROM 下载与 `startGame()` 前，即使 `disableDatabases=true` 也会触发。真实 mGBA v4.2.3 验证表明该事件和 `startGame()` 入口处的 `gameManager.getSaveFilePath()` 均仍可为空，路径直到 `EJS_onGameStart` 才稳定；因此 handler 在 `saveDatabaseLoaded` 保存经验证的 FS 引用，并在 `EJS_onGameStart` 的第一个同步动作中暂停 main loop、取得路径、完成注入、调用 `loadSaveFiles()` 后再恢复 main loop和提交 Retrom `start` 事件。这样不会把未恢复的区间计入 PlaySession，也不会让后端开始 idle 计时。有服务端 bytes 时创建父目录并覆盖目标；没有服务端保存时若 IDBFS 中存在同路径旧文件则先删除，避免复活浏览器残留。在任何可能调用 `saveState()` 前必须至少注册一个 `saveState` listener；v4.2.3 的 `callEvent` 忽略 callback 返回值并返回 listener 数，只有该数量为 0 时才 fallback 写入独立 `EmulatorJS-states` store，所以不得实现一个依赖 callback 布尔返回值的虚假协议。路径为空、listener/FS 未及时安装或读写失败都终止本次运行并显示 `LAUNCH_PERSISTENT_SAVE_LOAD_FAILED`；不得在 Retrom `start` 事件后再注入。每个 CoreArtifact 的此顺序必须有真实 smoke，尤其是 DOS overlay。

## 7. BIOS 与 parent bundle

EmulatorJS 每类只有一个 URL，后端按本次 VariantRevision 生成两个可缓存的确定性外层 ZIP：

- BIOS bundle：普通固件只取本次 dependency snapshot 中适用且状态允许的 Requirement，以逻辑文件名为 entry；Arcade BIOS ROMset 以 `neogeo.zip`、`pgm.zip` 等内层 archive 作为 entry。
- Parent bundle：Split ROMset 的 parent archive 按 DAT 逻辑 archive name 作为 entry；沿完整 parent 链排序。

v4.2.3 的实际 loader 对 Arcade `EJS_gameUrl` 保留整个 ROMset ZIP，并以 URL basename 写入虚拟文件系统；之后依次把 `EJS_biosUrl`、`EJS_gameParentUrl` 的外层 ZIP 解一层，并把每个 entry 的 basename 写到同一根目录。因此契约固定为：Arcade game URL 的逻辑 basename 必须精确为 `<machine>.zip`；两个外层 bundle 只能有根级 file entry，不能含 `/`、目录或同 basename 的大小写变体；main ROM、BIOS 和 parent 全部逻辑名做一次 ASCII case-insensitive 全局冲突检查，任何覆盖可能都在签发 Launch 前阻断。内层 `neogeo.zip`/parent ZIP bytes 保持不变，不能再解开或套额外目录。

依赖外层格式固定为 `RETROM_EJS_DEP_ZIP_V1`：entry 按逻辑名 UTF-8 byte 升序，method 一律 ZIP Store，时间为 `1980-01-01T00:00:00Z`，Unix mode `0644`，空 extra/file comment/archive comment，不写宿主 uid/gid；使用锁定 Go toolchain 的 `archive/zip`，相同测试向量必须逐字节同 hash。cache key 是 VariantRevision、依赖逻辑名、Blob SHA-256 与 bundle format version 的 canonical digest，生成 Blob/hash/format version 写入 VariantFiles/依赖快照。Full Non-Merged 已由 CONTENT 满足 entry 时不重复装配；格式、Go 版本或 header 参数改变必须提升 format version 并重跑 Arcade core smoke。

## 8. DOS 程序选择

导入扫描 `.exe`、`.com`、`.bat`，排除控制字符/路径逃逸，对 setup/install/uninstall/配置工具降权，但不凭文件名删除候选。详情下拉默认选择审核确认的 `default_dos_entry`；用户可选择另一个 entry，或显式选择“显示 DOSBox Pure 程序菜单”。存档记录当次选择。

DOSBox Pure 没有 EmulatorJS 的“启动某个可执行文件”参数，但支持读取与内容同 basename、位于内容旁的 `.conf`。正常直接启动使用 EmulatorJS 的虚拟文件注入与该 core 能力：

1. LaunchContent 始终锁定 VariantRevision 已有的规范 `DOS_LAUNCH_BUNDLE`，以原 `game.zip` 作为 `EJS_gameUrl`；选择多少个入口都不得复制 ROM bundle 或创建 Blob；
2. LaunchSession 只保存审核过的 `dos_entry_path`；`GET /runtime/launches/{launchId}/dos-config/game.conf` 在通过该 Launch 的 HttpOnly cookie、状态与 hard expiry 校验后，按请求即时返回极小的确定性配置，不落 CAS/数据库/临时文件；
3. config 设置 `EJS_externalFiles={"/game.conf":"/runtime/launches/<launchId>/dos-config/game.conf"}`，由 v4.2.3 loader 在下载 ROM 前写入同一虚拟文件系统；
4. 通过 `EJS_defaultOptions` 设置 `dosbox_pure_conf: "outside"`，使 core 把 `game.conf` 作为 `game.zip` 的同名旁置配置读取；
5. 配置中的 `[autoexec]` 只执行规范化后的所选相对程序，不生成 `DOSBOX.BAT`，也不改变原 ZIP。

autoexec 不接受用户参数。直接启动只允许 `dos_entries` 中每个路径段为 1–255 个 ASCII byte、匹配 `^[A-Za-z0-9][A-Za-z0-9 ._-]{0,254}$`、末 byte 另须匹配 `[A-Za-z0-9_-]`、不为 `.`/`..`，且最后一段后缀为 `.EXE/.COM/.BAT`（ASCII case-insensitive）的精确成员；这会排除尾随空格/点及所有 shell 元字符。其他合法 DOS entry 仍可在 core 程序菜单中启动，但详情页把直接启动选项标为不可用。把路径分隔符 `/` 替换为单个 `0x5C` 反斜杠后，`dosbox.conf` 必须是 UTF-8 无 BOM、使用 CRLF。例如选中成员 `GAMES/DOOM2.EXE` 时，唯一模板实例为：

```ini
[autoexec]
@ECHO OFF
C:
CD "\GAMES"
"DOOM2.EXE"
```

根目录程序的 `CD` 行固定为一个 `C`、`D`、空格和单个反斜杠（hex `43 44 20 5c`，即 `CD \`）；非根目录则取选中成员最后一段之前的目录段，用反斜杠连接并在引号内以单个反斜杠开头；程序行只写成员的最后一段。双引号内不会出现引号、百分号、shell 分隔符或转义字符，因为上述 allowlist 已拒绝它们；文件名保留 archive 中的原始大小写，不能添加参数。若选择“程序菜单”，使用原始/规范 bundle 且不注入 conf；这是用户主动选择的 core UI，不是 Retrom 的等待页。程序消失返回 `LAUNCH_DOS_ENTRY_MISSING`，路径不满足直接启动规则返回 `LAUNCH_DOS_ENTRY_UNSAFE`，均不猜替代项。

依据：[DOSBox Pure 官方 README](https://github.com/schellingb/dosbox-pure#loading-a-dosboxconf-file)说明同名旁置 `.conf`；[EmulatorJS options](https://emulatorjs.org/docs/options/)说明 `EJS_externalFiles` 的虚拟路径到 URL 映射；`dosbox_pure_conf=outside` 同时由固定 core artifact 内的 option strings 与 smoke 锁定。

## 9. 状态存档与持久存档

SaveState 同时引用 Profile、Game、GameVariantRevision、CoreArtifact、可空 DatVersion、DOS entry、状态 Blob、截图 Blob、名称、累计有效时长和创建时刻。默认禁止跨 CoreArtifact 或 VariantRevision 恢复；未来若有显式迁移器，必须另建兼容结果，不能自动尝试。

PersistentSave 用于 SRAM/NVRAM/DOS overlay 等，按 `Profile + VariantRevision + kind` 隔离。每次成功上传先创建带 `LaunchSession + 连续 client sequence + AUTO_INTERVAL/MANUAL_EXPORT/EXIT` 的不可变 revision，校验后以 current compare-and-swap 提升；同 sequence 只能重放相同 event/bytes，失败不覆盖最后有效版本。首项必须仍以 Launch 锁定的 base 为服务器 current，后续项必须以上一 sequence revision 为 current；若另一会话已推进则返回 `PERSISTENT_SAVE_CONFLICT`，当前页保留 bytes 并提供本地下载/退出重启，不把旧进度最后写入覆盖新进度。Player 仅在当前上传成功后递增 sequence，回调并发时把最新 bytes 排到下一 sequence，不能让重试 body 漂移。替换游戏文件后旧保存继续绑定旧 VariantRevision。

PersistentSave 能力来自 artifact compatibility：`SINGLE_FILE` 沿用上述单文件流程，`DOS_OVERLAY` 使用 DOS overlay，`NONE` 则不请求 persistent URL、不要求 `getSaveFilePath()`、不监听或上传 `saveSaveFiles`，game-start 直接继续。`handy`、`prosystem`、`stella2014`、`ppsspp` 当前为 NONE；状态存档仍必须可创建/恢复，UI 明示“此核心不支持自动持久存档，可使用状态存档”。服务端对 NONE 的 persistent GET/PUT 返回 `409 PERSISTENT_SAVE_UNSUPPORTED`，Launch 的 persistent base 必须为空。

## 10. PlaySession 与有效时长

`EJS_onGameStart`（必然发生在 `saveDatabaseLoaded` handler 成功之后）立即提交 sequence 0 的 start。前端每 30 秒发送 heartbeat，包含连续 sequence 以及上一区间的 `running/visible/paused`。`paused` 读取固定适配器中的 `EJS_emulator.paused`；可见性来自 `document.visibilityState`。

config bootstrap 后到 `EJS_onGameStart` 前尚无 PlaySession，只受 LaunchSession hard expiry；显式退出或 `pagehide` 发送 sequence 0、`previousInterval:null` 的 pre-start finish。真实 start 后服务端才建立 2 分钟 idle expiry，heartbeat 每 30 秒刷新；因此 ROM/core 下载耗时不会被误判成游玩失联。

服务端：

- 重复 sequence 幂等，跳号冲突；
- 单次最多计 45 秒；未开始、隐藏、暂停或失联段计 0；
- `exit`/显式退出 finish，`pagehide` 只尽力上报；异常关闭按最后已确认 heartbeat 截断；
- active duration 由服务端整数毫秒累计，客户端只报告状态，不直接提交总时长。

## 11. Chrome、线程与代理边界

`dosbox_pure` 使用 thread artifact。生产访问必须是 NG 提供的 HTTPS，同源页面/iframe/runtime 内容均设置：

```http
Cross-Origin-Opener-Policy: same-origin
Cross-Origin-Embedder-Policy: require-corp
Cross-Origin-Resource-Policy: same-origin
X-Content-Type-Options: nosniff
```

启动前检查 `window.isSecureContext`、`window.crossOriginIsolated` 和 `SharedArrayBuffer`。`make dev` 默认公开 origin 为可从独立开发机访问的 `http://local.sendev.cc:3000`，且不得把任何远程请求重定向到 localhost。若线程核心详情页从明文 HTTP 主机名打开，前端不发送一个注定被拒绝的 Launch，只明确报告浏览器线程能力不足；测试者可自行使用受信 HTTPS origin 或浏览器测试参数，本项目不替换请求 Host。Go/Next.js 只监听明文 HTTP，不终结 TLS。

## 12. 统一验收入口

启动与全屏执行 `ACC-RUN-001`–`ACC-RUN-005`；状态/持久存档执行 `ACC-SAVE-001`–`ACC-SAVE-003`；有效时长执行 `ACC-PLAY-001`；事件映射、八 core 画面与跨源隔离分别由 `ACC-CORE-*`、`ACC-NET-001` 和运行时回归测试覆盖。
