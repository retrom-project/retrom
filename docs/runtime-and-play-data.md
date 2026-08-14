# EmulatorJS 运行时、快速启动与游玩数据

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期实施基线 |
| 版本 | 1.2 |
| 日期 | 2026-08-14 |
| EmulatorJS 基线 | v4.2.3 |

## 1. 用户可观察契约

从游戏详情“开始游戏”、详情存档“继续”、“我的存档”或首页“继续游戏”发起时，一次点击完成“请求全屏 → 启动预检 → 加载运行时 → 自动开始”。正常路径没有 Retrom 的第二个 Start，也不显示 EmulatorJS `Play Now`。

游戏库卡片仍进入详情；只有语义明确的开始/继续按钮直接启动。存档入口使用存档锁定的 Core、CoreArtifact、GameVariantRevision、DOS entry 和文件，不受游戏目录当前默认核心覆盖。

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
- `Escape` 只退出浏览器全屏；左上返回与更多菜单中的“退出游戏”都先打开 Player 内的影响确认窗。确认窗最左侧提供“创建存档”，调用与工具栏相同的手动状态和截图原子创建事务；保存期间锁定弹窗内的离开动作，成功后保持确认窗并标记“已创建存档”，失败不创建不完整记录且允许原位重试。用户确认退出后才刷新持久存档、finish/revoke launch 并返回 allowlist 的 `returnTo`。进入 `/play/:launchId` 与退出到 `returnTo` 都替换当前浏览器历史项，Player Shell 不留在后退栈中；退出后点击浏览器后退不能重新进入已经结束的游戏画面。取消确认不改变运行状态。

### 2.1 移动 Player 的方向门禁

移动或 `pointer: coarse` 环境只能横屏运行游戏。方向门禁是 Player controller 的显式状态机，不是 CSS 提示：

1. 原始启动点击先请求全屏并 best-effort 调用 `screen.orientation.lock("landscape")`；API 不存在、权限拒绝或设备不支持时继续进入同一方向门禁。
2. `/runtime/launches/:launchId/config` 可先读取并完成结构、版本、mode 和 profile 校验，以确定普通/P1/P2 语义。若当前为竖屏，则停在 `BLOCKED_PORTRAIT`；此时不得创建 EmulatorJS iframe，不得请求 core、game、persistent-save、state 或 disc bytes，也不得创建 `PlaySession`。
3. `matchMedia("(orientation: portrait)")`、viewport resize 与 `screen.orientation` 变化都只更新候选方向。候选需连续稳定 `250ms` 才可提交，避免地址栏、软键盘和旋转中间尺寸导致反复装载/暂停。
4. 首次横屏稳定后状态转为 `PREFLIGHT`，才开始 HEAD/content 预检、持久数据读取、iframe/core/game 装载与 `PlaySession` start；一旦运行，普通横竖切换不得重建 Launch、iframe、Core 或 PlaySession。
5. 运行中转竖屏立即显示覆盖整个 Player 的模态阻断层并释放本地输入。普通单机调用 adapter pause，并记录该暂停是否由方向门禁拥有；回到横屏时仅恢复门禁自己创建的暂停，不能覆盖用户、文档隐藏或其他原因的暂停。
6. 联机 P1 在 canonical 边界请求全局暂停，并在横屏稳定、页面可见且该 pause 仍由方向门禁拥有时请求恢复。P2 只释放本地输入并显示“等待 P1 恢复”，不得发送全局 pause/resume。`document.hidden` 优先级高于方向恢复，隐藏期间绝不继续运行。

阻断层是有标题、说明、状态和重试按钮的焦点陷阱；重试只再次请求全屏/方向锁，不创建第二个 Launch。正常退出先调用 `screen.orientation.unlock()`，再退出全屏和完成现有 finish/revoke 流程。桌面 fine-pointer 路径不启用方向门禁。

### 2.2 移动横屏 HUD 与操作收纳

移动横屏 HUD 高 `48px`，使用横屏左右 `safe-area-inset`，自动隐藏后仍保留至少 `44px` 高的可聚焦揭示柄。HUD 保留返回、标题/Core、P1/P2 与同步状态，以及按“联机状态 > 换盘 > 创建存档”的最高优先级上下文操作；其余状态、存档、光盘、模拟器设置、全屏、调试和退出放入“更多”操作 Sheet。Sheet、退出确认、换盘和设置面板都必须捕获焦点、支持 Escape/遮罩/显式关闭，并在关闭后归还触发器。

## 3. Launch API 与凭据

唯一创建入口是 `POST /api/v1/launches`，body 包含 `gameId`、可空字符串 `coreId`、`saveStateId`、`dosEntry`、站内 `returnTo` 和当前 Chrome 的 `clientCapabilities`。精确 request/response、Idempotency-Key、cookie、TTL 与内容路径见 [HTTP API 契约](./http-api-contract.md)。

关键规则：

- 普通启动省略 `coreId` 时使用 PlatformInstance 默认核心；显式核心只能来自基础平台启用集合。
- 从存档启动时服务端解析精确 VariantRevision；客户端即使提交 core，也只能与存档相同。
- URL 只含非秘密 UUIDv7 `launchId`。32-byte capability 只在路径限定的 HttpOnly cookie 中，数据库只保存其 SHA-256；不把 token 放入 URL、JSON、日志、Referer 或诊断。
- 创建 Launch 的认证用户及其私有 Profile 在事务中锁定到 LaunchSession。launch capability 只授权该会话的受限内容路径，既不是普通用户登录凭据，也不能跨用户读取存档；禁用/软删除用户或执行 restore 安全栅栏时，未结束 Launch 立即失效。角色变化和用户自行改密会撤销认证会话，但不中断已经签发的 Launch。
- 已 READY 的预检成功返回 `201`；需新验证时返回 `202 VALIDATION_PENDING` 且不签发 credential，Player 在同一加载 overlay 等待 Worker，成功后以新幂等键自动重调。Blocker 返回 `422 LAUNCH_BLOCKED`。整个过程没有第二个 Start/确认页；Warning 不增加确认步骤。
- `VARIANT_REVALIDATE` 按 gameVariant/input digest 跨请求去重且不可由单个 Player 取消；退出加载壳只终止本页订阅并退出全屏，后台任务继续，避免一个朋友中断另一个朋友正在等待的同一验证。
- DOS 只有 `DOS_SOURCE`、没有主机平台的 `CONTENT` 行；重校验必须以 ContentRevision 本身作为内容输入，并在内容 revision 未变化时把既有 `DOS_LAUNCH_BUNDLE` 与审核默认入口复制到新 VariantRevision。Worker 任一步骤失败都必须把 Job 收口为可重试 FAILED，进程重启时重新领取 lease 已过期且尚有 attempt 的 RUNNING Job，不能让 Player 永久等待在 `VALIDATION_PENDING`。
- 默认核心不可运行时不静默尝试其他核心。

### 3.1 管理审核预览

审核页的“运行游戏”不是普通用户 Launch。`POST /api/v1/admin/reviews/{itemId}/previews` 为当前 `REVIEW_PENDING` Item 创建短时、capability-scoped 的审核快照并打开 `/admin/review-previews/{previewId}` 子窗体；它锁定当前有效 source snapshot、目标目录默认 CoreArtifact、最新 Validation 和实际存在的依赖。主 ROM 必须存在，已有 Parent、BIOS bundle、external file 与完整多盘内容按普通 Player 协议交付，缺失依赖被省略。DAT 驱动的 Arcade Validation 以其不可变 `ValidationFiles` 作为 Parent/BIOS 交付事实源，不得把 V2 DAT 依赖快照误按普通 BIOS 快照解析；没有 DAT 的普通平台才从 V1 BIOS 快照装配 external file。预览不创建 Game、LaunchSession、PlaySession，不调用 start/heartbeat/finish，不加载或写入 SaveState/PersistentSave，也绝不把“看起来可运行”升级成 READY。

子窗体复用版本锁定的 Player adapter 与 canvas contain 规则。创建成功的 READY 或阻断预览都返回 `reviewPreview.captureAllowed=true`；在真实 `EJS_onGameStart` 回调发生后启动一次 5,000ms timer，通过 adapter 优先读取核心保存的最后一帧 PNG，使停止持续刷新的 ROM/BIOS 缺失错误页仍可进入审核证据；核心截图在 2 秒内不可用时回退到 EmulatorJS canvas 截图。截图上传到同一 preview capability 下的 `review-screenshot`。写入时仍须匹配当前来源快照、目标平台、CoreArtifact 和 prepublish generation；任一漂移都会拒绝旧截图。由于 EmulatorJS/WASM、用户激活和自动播放策略属于浏览器边界，后端不能脱离浏览器伪造这一画面；子窗体必须监听同源 iframe 的同步错误与未处理 Promise rejection，并在 30 秒内没有真实 `EJS_onGameStart` 时转为可见失败，不能永久停留在“正在加载”。弹窗、播放或截图上传被阻止时，审核页明确提示重试。

阻断 Validation 的当前截图允许管理员人工放行。发布后的 Variant 使用 `REVIEW_SCREENSHOT_OVERRIDE` compatibility code，并只交付截图预览时可获得的依赖；普通单机 Launch 跳过同一缺失 BIOS 的强制重解析，仍保留截图放行 warning，Netplay 继续使用严格依赖门禁。截图不是永久绕过所有一致性检查：来源、目标平台、核心或 Validation 更新后必须重新运行并取得新截图。

## 4. 启动预检

服务端按固定顺序，在创建 credential 前完成：

1. Game 状态为 `PUBLISHED`，PlatformInstance 启用且关系有效；
2. 解析存档锁定核心、显式核心或目录默认核心；
3. PlatformCore 与 Core 启用；
4. 存档路径解析锁定 revision；普通路径对所选 core 调用游戏目录专题的幂等 `EnsureVariant`，取得直接引用 `Game.current_content_revision_id` 的 READY GameVariantRevision；
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

`core` 是产品/数据库 core ID，只用于展示与审计；`runtimeCore` 是锁定 artifact compatibility V3 的 EmulatorJS core ID，只有它可以写入 `EJS_core`。`persistentSaveMode`、`inputMode`、`startupActions`、`externalFiles` 与按内容种类派生的可空 `discSet` 同样由该配置返回，Player 只做封闭 schema 校验，不按 `ppsspp`、`melonds` 或显示名推导行为。

`EJS_fullscreenOnLoaded` 必须为 `false`：全屏由 Retrom host 在用户手势中唯一管理，避免 loader 稍后重复请求。`EJS_Buttons.exitEmulation=false` 从运行时配置移除 EmulatorJS 自带退出按钮，退出只能经过 Retrom 的确认、持久存档刷新和 PlaySession 结束流程。语言固定 `zh-CN`。v4.2.3 `loader.js` 对 `EJS_disableAutoLang` 的判断是 `!== false`，因此这里必须显式设为 `false` 才会禁用 system locale 分支；不能凭变量名改成 `true`。这样只请求 manifest 中的 `zh-CN.json`。`EJS_disableDatabases=true` 在 v4.2.3 只把 ROM/BIOS/core asset cache 换成 dummy storage，`EJS_disableLocalStorage=true` 关闭设置持久化，`EJS_CacheLimit=0` 防止 ROM cache；它们并不会关闭 `/data/saves` 的 IDBFS，也不会阻止 `saveDatabaseLoaded`。Retrom 必须按第 6 节显式覆盖/清理该 IDBFS 路径，才能让服务端 PersistentSave 成为事实源；不得把开关名称误解为“所有 IndexedDB 均已禁用”。`EJS_gameID` 来自精确 GameVariantRevision 的稳定数字 surrogate，而不是 Game ID。

官方 EmulatorJS 4.2.3 的 `GameManager.loadExternalFiles()` 以 `arraybuffer` 下载外部文件后，把裸 `ArrayBuffer` 直接交给 MEMFS；该组合会建立零长度文件，而主内容因先转换为 `Uint8Array` 不受影响。`ejs-4.2.3-v2` 只在本次 config 含 `externalFiles` 时、且必须在追加 loader script 之前安装版本绑定兼容层：捕获 `EJS_GameManager` 的定义，把其 `writeFile(path, data)` 收到的裸 `ArrayBuffer` 转为 `Uint8Array`，其他字符串和 typed-array 输入逐对象保留。无法安装时以 `PLAYER_EXTERNAL_FILES_COMPATIBILITY_UNAVAILABLE` 阻断，不能继续启动零长度 BIOS/光盘；不得修改或重新哈希已物化的第三方 runtime payload。升级 EmulatorJS 时必须先以真实 external-file smoke 复测，再决定新 adapter 是否仍需该兼容层，不能把 4.2.3 行为无条件继承到未知版本。

官方 EmulatorJS 4.2.3 的 `extract7z.js` 来自旧版 Emscripten，会在 Blob Worker 中通过两个 `eval` 分支解析导出函数和生成 `cwrap` wrapper；生产 CSP 只允许 `wasm-unsafe-eval`，不能为此开放 JavaScript `unsafe-eval`。`ejs-4.2.3-v2` 必须在追加 loader 前捕获 `EJS_COMPRESSION` 的定义，并只对 `7z` Worker Blob 执行版本绑定的精确兼容转换：导出函数只从既有 `Module` 属性读取，动态 `cwrap` 改用同一 worker 已提供的 `ccall`；两处源片段都必须恰好命中一次，转换后仍出现 `eval(` 或任一片段漂移都以 `PLAYER_ARCHIVE_COMPATIBILITY_UNAVAILABLE` 阻断。非 7z worker 逐对象保留，已物化的官方 runtime bytes、manifest hash 和许可证据不得改写；未知 EmulatorJS 版本不能继承该转换。

`runtimeBaseUrl` 与 `loaderUrl` 必须锁定 Launch 所选 CoreArtifact 的精确 `emulatorjs_version`，不能固定取当前 active 版本。对基线 v4.2.3，它们分别是 `/runtime/emulatorjs/4.2.3/data/` 与 `/runtime/emulatorjs/4.2.3/data/loader.js`；通用派生规则是给该版本 manifest 的 `emulatorjs.player_adapter.runtime_base_path_in_release/loader_path_in_release` 加 `/runtime/emulatorjs/<exact-version>/` 前缀，并要求 loader 属于 runtime base 且两者都命中 allowlist。它们只由 config 返回，前端不得拼版本、猜目录或回退 active 版本。`gameName` 固定为 `retrom-<emulatorGameId>`，只使用 ASCII 字母、数字与连字符，使 EJS 的 save key 在元信息重命名后仍稳定。

`runtimePathOverrides` 对每个已接受版本精确包含一个键：该版本 loader 对所选 artifact 实际请求的 basename；值是该 CoreArtifact 的固定同源 URL。这两个值只由 CoreArtifact 的已校验 `compatibility_config_json.requestedArtifactBasename`、`emulatorjs_version` 和 `relative_path` 派生。v4.2.3 的普通 artifact 例如 `{"mgba-wasm.data":"/runtime/emulatorjs/4.2.3/data/cores/mgba-wasm.data"}`；`mame2003` 必须是 `{"mame2003-wasm.data":"/runtime/emulatorjs/4.2.3/overrides/mame2003-4.2.1-wasm.data"}`；DOSBox Pure 使用 `4.3.0-pre` 的 `dosbox_pure-thread-wasm.data`。其余 loader、CSS、语言、archive helper 和 core report 都从本次 config 的 runtime base 读取，不增加浮动 URL。`defaultCoreOptions` 先放固定 `webgl2Enabled: "enabled"`，再合并适用 BIOS Requirement；DOS 不再依赖旁置 config 或 core option。任何重复 key 异值在验证阶段失败，不能靠合并顺序覆盖。

Player adapter 使用 manifest 声明的 `playerAdapterId → adapter` 显式 registry，不允许默认分支把未知 ID/版本当成 v4.2.3。机器可读 registry 固定为 `web/features/player/adapters/registry.json`，当前登记 `ejs-4.2.3-v2 → 4.2.3` 与 `ejs-4.3.0-pre-v1 → 4.3.0-pre`；后者同时服务 DOSBox Pure、Genesis Plus GX Wide 与 Azahar。两份 TypeScript 实现必须与 JSON 双向一一对应。浏览器收到未知或版本不匹配的 ID 时必须在加载 loader 前终止，不得回退 active 版本或任意旧 adapter。

Player Shell 创建同源 `about:blank` iframe，由父页面在 iframe document 中建立唯一 `#game` 容器、设置上述 globals、注册 callback，最后追加 `src=config.loaderUrl` 的 script；不使用 `srcdoc` inline script、跨源 frame 或 `document.write`。iframe 继承父页面 origin/CSP，所有内容请求会按 `/runtime/launches/<launchId>/` 路径自动携带 capability cookie。只有 config 校验与可选 PersistentSave 预读完成后才加载 loader。

Player canvas contain 必须优先使用锁定运行时 `gameManager.getVideoDimensions("aspect")` 的正数结果，只有 game-start 前尚不可用时才回退 drawing-buffer `canvas.width/canvas.height`。这能处理 drawing buffer 仍为横向但核心实际输出为 3:4 等竖屏画面的情况：竖屏画面 CSS 高度贴满 `100dvh`，左右保留必要黑边，不能误在上下留下黑边。viewport、canvas 属性或核心比例变化时必须重新计算；canvas 在父 grid 的水平和垂直方向都显式居中，不能把 contain 后的余量全部留在右侧或底部，也不能拉伸或裁切。

Player config 额外提供人类可读的 `gameTitle/coreName/platformName`，只用于 58px 顶部工具栏显示本次游戏、运行核心和基础平台；EJS 的稳定保存键仍只使用 `gameName=retrom-<emulatorGameId>`，前端不得把展示名称用于选择 artifact、URL 或 option。

Retrom 顶部工具栏是运行中的暂停边界：除光盘菜单与只读“调试信息”面板外，点击工具栏区域或其中操作都先调用 `gameManager.toggleMainLoop(false)` 并同步设置实例 `paused=true`；工具栏内不提供恢复动作，只有随后点击实际游戏画面才调用 `toggleMainLoop(true)` 并恢复 `paused=false`。光盘菜单是独立的原子运行时操作：打开菜单不暂停，真正换盘时只在 `setCurrentDisk` 与回读边界内短暂停止 main loop，并在成功或失败后恢复进入换盘前的暂停状态。运行后工具栏自动隐藏；只有指针进入 viewport 顶部 32 CSS px、键盘操作或焦点进入工具栏才重新显示，普通画面区域的 pointermove 不得唤出，避免干扰 DOS/DS 等鼠标控制游戏。同源 iframe 内只把非 EmulatorJS 工具栏、弹窗、按钮或输入控件的画面点击映射为恢复，避免用户调整原生设置时误启动游戏。该状态进入后续 heartbeat/finish 的 `previousInterval.paused`，暂停区间不累计有效游玩时长；暂停期间工具栏和画面中央“点击游戏画面继续”浮层保持可见。EmulatorJS 底部工具栏默认由 iframe 级显示门禁锁定，启动时的 `menu.open()`、靠近底边和画面点击都不能自行显示；只有 Retrom 更多菜单中的“模拟器设置”解除门禁并调用本次 v4.2.3 实例的真实 `menu.open()`。运行时把工具栏重新收起时门禁自动恢复。这样继续使用 EJS 已装载的控制、显示、Core 和音量设置，同时不复制一套会与运行时状态分叉的设置面板。

顶部“调试信息”按钮打开右侧只读诊断面板且保持游戏继续运行；面板打开期间固定显示顶部工具栏。面板每秒读取一次当前 adapter 暴露的 `gameManager.getFrameNum()`，以相邻采样的核心帧数差和单调时钟计算一位小数的“核心帧率”，不能用 CSS 动画或浏览器 `requestAnimationFrame` 次数伪造 FPS。面板同时展示累计核心帧数、运行/暂停/错误状态、canvas drawing-buffer 分辨率、viewport 与 DPR，以及 config 已锁定的 Core、CoreArtifact、EmulatorJS 版本、Player adapter、输入模式、单机/联机模式和当前 COOP/COEP/SharedArrayBuffer 能力。诊断只存在于当前浏览器 Player，会话结束即丢弃，不写数据库、不发遥测，也不得展示 capability、cookie、Blob hash 或宿主路径。

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

线程核心的 override key 必须使用 loader 实测 basename：`dosbox_pure-thread-wasm.data`、`mednafen_psx_hw-thread-wasm.data`、`ppsspp-thread-wasm.data`。MelonDS 的 `externalFiles` 精确包含 `/retroarch/userdata/system/bios7.bin`、`bios9.bin`、`firmware.bin` 三个虚拟路径，URL 只能指向本 Launch 的 `/external-files/<logicalName>`；这些 Blob 在创建 Launch 时锁定，不能在 config GET 时重新选择 active BIOS。DOS 的 `externalFiles` 为空。

NDS 三核心与 Azahar 的 `inputMode=POINTER`：Player 不向 iframe 合成额外的 `pointerdown/click`，真实浏览器事件直接到 EmulatorJS canvas。其他核心为 STANDARD。`startupActions` 最多 4 条，`delayMs` 上限 30,000、`durationMs` 上限 1,000，只在一次 `onGameStart` 后有界调用并释放 `simulateInput`。PPSSPP 使用 2,000/5,000ms 两条 120ms 确认动作；Beetle VB 使用 2,000/4,000/15,000/25,000ms 四条 120ms 动作。Strict Mode 重入不得重复调度，unmount/失败/退出必须取消 timer 并释放已按下控制，最后一次释放后不再自动输入。这是版本绑定的有限启动动作，不是通用宏功能。

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

导入扫描全部 `.exe`、`.com`、`.bat`，不按数量截断，也不凭文件名删除候选。排序固定为：`game/go/launch/play/run/start` 等明确入口优先，普通程序居中，setup/install/uninstall/config/readme、驱动和解包辅助程序降权；同层再按 EXE/COM/BAT、目录深度和规范路径排序。规则只影响推荐默认值，审核页仍展示全部候选。DOS ZIP 内的嵌套 archive 只作为不透明游戏数据保留、不递归展开；小于等于 16 MiB 的高压缩比空白存档允许通过，较大成员仍受压缩比、单成员、总展开量和成员数上限保护。

详情下拉默认选择审核确认的 `default_dos_entry`。一次普通启动成功签发 Launch 后，浏览器才按 Game 记住这次入口或显式“程序菜单”选择；失败启动不覆盖偏好，候选失效时回退审核默认。存档恢复始终采用存档锁定入口且不改写该偏好。

EmulatorJS 4.2.3 会先展开 ZIP，再把归档中的第一个普通成员误交给 DOSBox Pure；旁置 `.conf` 又在 core 初次启动之后才生效。因此 DOSBox Pure 的新启动固定使用已校验的 `4.3.0-pre` artifact，并采用下列通用装配：

1. LaunchContent 锁定 VariantRevision 的既有 `DOS_LAUNCH_BUNDLE` Blob，`format_version=RETROM_DOS_DIRECT_ZIP_V1`；选择多少入口都不复制游戏内容、不创建派生 Blob；
2. `GET .../game/game.zip` 以 seekable 虚拟视图流式返回合法 ZIP：直接启动在原 local records 之前增加 DOSBox Pure 原生的根级 Store `AUTOBOOT.DBP`，程序菜单增加根级 Store `DOSBOX.BAT` 并运行 `Z:\PUREMENU`；两种视图都修正 central-directory offset/entry count，并保留其他成员压缩 bytes、顺序、名称和 archive comment；原包已有大小写任意的两个保留名时，其 central record 都被移除，不能覆盖受控引导或令显式菜单被自动启动劫持；
3. 端点按 ZIP central-directory 顺序复现 DOSBox Pure 的 8.3 名称缩短和同目录冲突递增规则；`AUTOBOOT.DBP` 只含经校验 entry 映射后的 `C:\...` DOS 路径，由 core 自身切换目录并执行 EXE/COM/BAT。这样长文件名、空格和碰撞不依赖 DOS shell 对引号或 BAT `CALL` 的不一致支持；HEAD、完整 GET 和单 Range 使用同一强 ETag；
4. Player adapter 设置 `EJS_startOnLoaded=false`、`EJS_disableBatchBootup=true`，在 `EJS_ready` 时把 `dosbox_pure` 加入该运行时实例的 `downloadType.rom.dontExtractIfCore`，等待 start button 实际创建后自动点击。这样 core 收到完整 ZIP 而非第一个 entry；未知运行时结构必须阻断，不能退回“猜第一个文件”。

直接启动只允许 `dos_entries` 中每个路径段为 1–255 个 ASCII byte、匹配 `^[A-Za-z0-9][A-Za-z0-9 ._-]{0,254}$`、末 byte 另须匹配 `[A-Za-z0-9_-]`、不为 `.`/`..`，且最后一段后缀为 `.EXE/.COM/.BAT`（ASCII case-insensitive）的精确成员；这会排除尾随空格/点及 shell 元字符。其他合法候选仍显示，但只能进入 core 程序菜单。程序消失返回 `LAUNCH_DOS_ENTRY_MISSING`，路径不满足直接启动规则返回 `LAUNCH_DOS_ENTRY_UNSAFE`，均不猜替代项。

## 9. 多盘启动、换盘与存档恢复

`MULTI_DISC_M3U_V1` Launch 把 canonical playlist、全部 2–8 个 DISC Blob、连续 index、规范 `disc-NNN.chd` virtual path、CoreArtifact 和初始盘号一次锁定。`gameUrl` 只指向服务端生成的 `playlist.m3u`，`externalFiles` 包含每张盘的本 Launch 受限 URL；来源 M3U 名、原始 CHD 名和跨 Launch URL 都不可用于读取内容。Player 在加载 loader 前严格校验 `discSet`，对全部盘执行 HEAD 并显示盘数/总大小；任一盘缺失、长度无效或响应不在 capability 范围内都以 `PLAYER_DISC_SET_INVALID` 阻断，不能降级成单盘运行。

EmulatorJS 4.2.3 使用 `ejs-4.2.3-v2` adapter。它在 `EJS_ready` 初始化运行时光盘设置，并要求 `getDiskCount/getCurrentDisk/setCurrentDisk` 全部存在。`EJS_onGameStart` 在同一暂停边界内先完成 PersistentSave 注入，再核对真实盘数，切到 Launch 锁定的 `initialDiscIndex` 并回读；从 SaveState 启动时，Player 预先以 64 MiB 硬上限读取 state bytes，盘号确认后才显式 `loadState`，多盘不设置 `EJS_loadStateURL`，从而避免运行时异步加载与换盘竞态。任一步失败都不恢复 main loop，也不提交 PlaySession start。

多盘真实 smoke 在 `EJS_onGameStart` 后还必须从 MEMFS 读取全部 canonical external file 的长度，数量须等于盘数且逐项等于 fixture 锁定的 size；standalone 页先拒绝零长度，runner 再完成精确对照。这条断言专门防止外部文件下载成功、HTTP 200 但落盘为空或截断的回归。换盘动作只在当前 run 已抓到可辨识的游戏画面后开始；结果同时保存该游戏画面和换盘后的当前画面。内容可能在换盘后处于自身黑场/过场，因此换盘成功依赖每一步 `target/observed` 一致且 `frameAfter > frameBefore`，不能用最终单帧复杂度代替运行时回读，也不能用持续帧掩盖换盘前从未进入游戏的情况。

Player 在开始、盘数不一致、换盘成功/失败和跨盘状态恢复成功/失败时 best-effort 上报封闭运行事件；请求只含固定事件类型、稳定结果码以及期望/实际盘数。后端以 Launch 锁定的 platform/core/artifact version 和盘数 bucket 记录结构化事件，不接受游戏标题、文件名、路径、hash 或 capability。观测失败不改变上述暂停、换盘、存档恢复和 PlaySession 状态机。

盘数匹配后工具栏在“创建存档”之前显示 `光盘 N / M`。菜单按 canonical 顺序列出全部盘，当前项带选中状态；选择其他盘时在短暂停止 main loop 后调用 `setCurrentDisk`，回读一致才更新界面并宣告成功，随后恢复换盘前的运行/暂停状态，不能留下无提示的暂停画面；失败则保持原盘号并同样恢复原状态。当前项只关闭菜单；Escape 关闭并把焦点还给触发器。手动存档从 runtime 回读当前盘号并写入 `disc_index`；恢复要求存档锁定的 VariantRevision、CoreArtifact、盘数和盘号全部兼容，绝不改用其他盘尝试加载。

## 10. 状态存档与持久存档

SaveState 同时引用 Profile、Game、GameVariantRevision、CoreArtifact、可空 DatVersion、DOS entry、状态 Blob、截图 Blob、名称、累计有效时长和创建时刻。默认禁止跨 CoreArtifact 或 VariantRevision 恢复；未来若有显式迁移器，必须另建兼容结果，不能自动尝试。

Profile 必须等于当前认证用户唯一绑定的私有 Profile。存档列表、详情、创建、恢复、软删除、最近游玩、累计时长和 PersistentSave current/revision 查询都先按该 Profile 限定；客户端提交另一个 Profile ID、SaveState ID 或 Launch ID 不能扩大授权。用于写操作重放的 Idempotency-Key 同样按认证用户主体分区。

PersistentSave 用于 SRAM/NVRAM/DOS overlay 等，按 `Profile + VariantRevision + kind` 隔离。每次成功上传先创建带 `LaunchSession + 连续 client sequence + AUTO_INTERVAL/MANUAL_EXPORT/EXIT` 的不可变 revision，校验后以 current compare-and-swap 提升；同 sequence 只能重放相同 event/bytes，失败不覆盖最后有效版本。首项必须仍以 Launch 锁定的 base 为服务器 current，后续项必须以上一 sequence revision 为 current；若另一会话已推进则返回 `PERSISTENT_SAVE_CONFLICT`，当前页保留 bytes 并提供本地下载/退出重启，不把旧进度最后写入覆盖新进度。Player 仅在当前上传成功后递增 sequence，回调并发时把最新 bytes 排到下一 sequence，不能让重试 body 漂移。替换游戏文件后旧保存继续绑定旧 VariantRevision。

PersistentSave 能力来自 artifact compatibility：`SINGLE_FILE` 沿用上述单文件流程，`DOS_OVERLAY` 使用 DOS overlay，`NONE` 则不请求 persistent URL、不要求 `getSaveFilePath()`、不监听或上传 `saveSaveFiles`，game-start 直接继续。`handy`、`prosystem`、`stella2014`、`ppsspp` 当前为 NONE；状态存档仍必须可创建/恢复，UI 明示“此核心不支持自动持久存档，可使用状态存档”。服务端对 NONE 的 persistent GET/PUT 返回 `409 PERSISTENT_SAVE_UNSUPPORTED`，Launch 的 persistent base 必须为空。

浏览器中的 `/data/saves` IDBFS 不是跨账号事实源。每次 `EJS_onGameStart` 在恢复 main loop 前，都必须用本次 Launch 锁定的服务器 revision 覆盖目标路径；服务器没有保存时必须删除同路径残留，再调用 `loadSaveFiles()`。覆盖、删除或 reload 任一步失败都以 `LAUNCH_PERSISTENT_SAVE_LOAD_FAILED` 阻断，不能继续使用旧浏览器 bytes。账户切换 E2E 必须在同一 Chrome profile 中证明 B 用户既看不到 A 的 API 数据，也不会从 EJS IDBFS 复活 A 的保存。

## 11. PlaySession 与有效时长

`EJS_onGameStart`（必然发生在 `saveDatabaseLoaded` handler 成功之后）立即提交 sequence 0 的 start。前端每 30 秒发送 heartbeat，包含连续 sequence 以及上一区间的 `running/visible/paused`。`paused` 读取固定适配器中的 `EJS_emulator.paused`；可见性来自 `document.visibilityState`。联机 Player 的 `blur/visibilitychange` 只清空本地控制输入，不发送 `SUSPEND_REQUEST`、不关闭 WebSocket、不结束 Launch，也不导航回房间；Chrome 后台节流 EmulatorJS 时共享 lockstep 可以等待，恢复前台后沿原连接继续。React effect 清理、Fast Refresh 或同一 Launch 的新控制器接管只能释放旧控制器，不能发送 `END_REQUEST` 或 finish Launch；初始同步期间的瞬时 socket 关闭同样在 Player 内重连，服务端废弃旧 transfer 并从 P1 重新传输状态。页面刷新、关闭、主动退出、真实 socket/ping 故障超过恢复窗口或服务端终局才进入对应结束路径，服务端不得用短时缺少输入贡献代替网络存活判断。

config bootstrap 后到 `EJS_onGameStart` 前尚无 PlaySession，只受 LaunchSession hard expiry；显式退出或 `pagehide` 发送 sequence 0、`previousInterval:null` 的 pre-start finish。真实 start 后服务端才建立 2 分钟 idle expiry，heartbeat 每 30 秒刷新；因此 ROM/core 下载耗时不会被误判成游玩失联。

服务端：

- 重复 sequence 幂等，跳号冲突；
- 单次最多计 45 秒；未开始、隐藏、暂停或失联段计 0；
- `exit`/显式退出 finish，`pagehide` 只尽力上报；异常关闭按最后已确认 heartbeat 截断；
- active duration 由服务端整数毫秒累计，客户端只报告状态，不直接提交总时长。

## 12. Chrome、线程与代理边界

`dosbox_pure` 使用 thread artifact。生产访问必须是 NG 提供的 HTTPS，同源页面/iframe/runtime 内容均设置：

```http
Cross-Origin-Opener-Policy: same-origin
Cross-Origin-Embedder-Policy: require-corp
Cross-Origin-Resource-Policy: same-origin
X-Content-Type-Options: nosniff
```

启动前检查 `window.isSecureContext`、`window.crossOriginIsolated` 和 `SharedArrayBuffer`。`make dev` 的测试服务器基线公开 origin 为 `https://dev.sendev.cc`，且不得把任何远程请求重定向到 localhost；隔离的本地实例可显式覆盖该值。若线程核心详情页从明文 HTTP 主机名打开，前端不发送一个注定被拒绝的 Launch，只明确报告浏览器线程能力不足；测试者可自行使用受信 HTTPS origin 或浏览器测试参数，本项目不替换请求 Host。Go/Next.js 只监听明文 HTTP，不终结 TLS。

## 13. 联机 Launch、帧 adapter 与 rollback

`GET /runtime/launches/:launchId/config` 使用 `mode` 区分普通与联机。普通为 `mode=single,netplay=null`；联机为 `mode=netplay`，并额外返回 `roomId/sessionId/playerNo/netplayProfile/runtimeSocketUrl`，同时强制 `persistentSaveMode=NONE`、`persistentSaveUrl/stateUrl/discSet=null`。前端必须在请求 game/core/runtime 之前验证这一组合、profile canonical 字段、EmulatorJS 4.2.3、`ejs-4.2.3-v2` 与 `ejs-netplay-4.2.3-v1`；矛盾或未知值立即阻断。联机 config、内容与普通 PlaySession 仍由本人的 Launch cookie 授权，room WebSocket 另用独立 room cookie。

联机 Player 不显示创建存档、普通暂停、换盘、控制重映射或 Core 设置；明确展示 P1/P2、网络/同步状态和“不读取或写入个人存档”。P1 可发起下一 canonical 边界的全局暂停/继续；隐藏或失焦只清空本地控制，不请求暂停、不断开 Socket 也不结束 Launch。退出发送全局 `USER_EXIT`，结束 PlaySession 后使用 `location.replace('/netplay/rooms/:roomId')`，浏览器后退不能复活旧 Player。所有 persistent/save-state runtime route 对联机 Launch 返回 `409 NETPLAY_SAVE_UNSUPPORTED`，前端也不得探测这些 URL。

`EJSNetplayFrameBridge` 只适配 4.2.3：拦截本地 P1 physical controls、通过 `postMainLoop`/`toggleMainLoop` 精确推进一帧、用原生 `getState/loadState` 处理 RASTATE，并在 rollback replay 时抑制画面/音频。RASTATE 必须含 version 1 与 `MEM ` chunk；full state 和 core chunk 分别 SHA-256，state 最大 1 MiB。`loadState` 即使目标 bytes 与当前状态相同也必须真实调用；联机模式使用 manifest 锁定的 4.2.3 source loader，让 adapter 接收 RetroArch 的 `[State] ... game.state` 原生完成日志，同时显式关闭 EmulatorJS 实验性 netplay transport。每次加载由 adapter 先替换内存 FS 的 `/game.state`、调用固定 `functions.loadState("game.state", 0)`，在日志 callback 返回后的 microtask 暂停主循环并重抓 RASTATE。重抓结果必须与传入状态的 `MEM ` core bytes 逐字节一致；full-state 是否逐字节一致只作诊断证据，因为 FBNeo 可在合法装载后重建 RASTATE 外层元数据。P1 authority 初始状态也必须经原生加载/重抓归一化到 core bytes 的 fixed point 后才传输。验证完成后立即删除该文件；不能使用公共 `GameManager.loadState` 的五秒延迟清理、JS 函数返回、文件 open/close 或 digest 相同替代 native completion。

每 epoch 从 P1 state transfer 开始；prediction 上限由 canonical core profile 固定。FCEUmm 为 8 帧，保存 120 帧/128 MiB state ring，canonical 差异回滚到最早帧并确定性 replay。EmulatorJS 4.2.3 的 FBNeo save state 无法完整恢复一次推测执行改变的全部内部状态，因此 FBNeo 为 0 帧严格 lockstep：两端根据实测 input-to-canonical 往返时间在 1–8 帧间自适应输入缓冲，低延迟连接从 1 帧开始，只在网络时延需要时扩大；只有收到对应帧的同一 canonical input 后才各执行一次，不推测执行也不进入 rollback replay。输入缓冲不是 prediction，失焦清空控制后至多仍消费已提交的有界缓冲。FBNeo 联机统一关闭 core hiscore 持久化，避免未进入本协议的本地高分状态影响确定性。每 120 帧上报 core digest；FCEUmm 与非 Neo Geo FBNeo 驱动使用完整 `MEM ` bytes。FBNeo Neo Geo 驱动从 checkpoint 投影中排除两段绑定 4.2.3 FBNeo 布局的音频状态：libretro `nCurrentFrame` 后、NeoScan 游戏状态前由 `YM2610_save_state` 注册的 1588 bytes，以及紧邻 uPD4990A RTC 前的 96-byte AY8910 状态和两个 32-bit YM2610/AY8910 浏览器输出游标；CPU、RAM、RTC、libretro frame counter 和其余 core state 仍严格参与校验。这些区域受浏览器音频采样调度影响，不是游戏逻辑状态；投影只影响 checkpoint hash，不放宽 state transfer 或 state load 的逐字节 core 校验。服务端 history 保留 600 帧；PAUSE 后所有 occupied seat 先确认相同边界，恢复端再确认 history `toFrame`，断线 10 秒内客户端才经 authority state 开新 epoch。访客超时结束本局、释放其座位并回 WAITING，房主超时以 `HOST_LOST` 关闭房间。60 秒内前三次 checkpoint mismatch 都执行真实 authority state resync，第四次以 `NETPLAY_UNSTABLE` 结束本局。任何 ring 溢出、窗口越界、state 格式/协议突变都 fail closed，不以继续运行掩盖分歧。

普通 Player adapter、普通 Launch、状态/持久存档和多盘路径保持原契约；联机 core profile 是否可选只由服务端 allowlist、当前 READY revision 的精确 CoreArtifact 与当前依赖快照共同判断，不由前端 core 名称推断，也不按单个 ROM hash 建白名单。房间锁定的 GameVariantRevision 继续保证所有参与者得到同一内容字节。

## 14. 统一验收入口

启动与全屏执行 `ACC-RUN-001`–`ACC-RUN-005`；移动方向门禁、横屏 HUD、P1/P2 暂停职责和请求时序执行 `ACC-MOB-005`–`ACC-MOB-007`；状态/持久存档执行 `ACC-SAVE-001`–`ACC-SAVE-003`；多盘锁定、换盘与跨盘恢复执行 `ACC-MDISC-004`–`ACC-MDISC-006`；账户与 Player 数据隔离执行 `ACC-ISO-001`–`ACC-ISO-003` 与 `ACC-MDISC-008`；有效时长执行 `ACC-PLAY-001`；事件映射、三十五 core 画面与跨源隔离分别由 `ACC-CORE-*`、`ACC-NET-001` 和运行时回归测试覆盖。
