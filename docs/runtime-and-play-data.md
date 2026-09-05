# Runtime Provider 与游玩数据

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已实施 / 权威基线 |
| 版本 | 3.0 |
| 日期 | 2026-09-05 |
| 机器事实源 | `api/runtime-provider/v1/`、已激活 Provider Bundle、`data/runtime-target-bindings/v1/catalog.json` |

## 1. 唯一职责边界

具体引擎的入口、静态资产、Target 能力、输入资源、Target options 和 checkpoint 格式只由 Provider Bundle 的 manifest 声明。Retrom Host 负责验证并激活 Bundle、把产品 Core 绑定到稳定的 `(providerId,targetId)`、准备授权资源并签发 Launch；Host 不保存或推导 Provider 私有 adapter、core、入口文件和资产映射。Web Player 不维护按引擎分支的 registry，只通过共享 dispatcher 加载 Provider Module V1。

当前可部署 Provider 是 `emulatorjs` 与 `retrom-runtime`。`runtime-target-bindings` 只描述产品 Core、平台、内容类型与稳定 Target 的关系，不复制 Target declaration。Provider 安装采用只向前升级：版本必须递增，同版本换字节和降级都被拒绝；没有旧 Bundle fallback 或运行时回滚路径。

## 2. 当前态与冻结态

业务数据采用 current-state 模型：`games` 直接保存当前 metadata 与内容来源，`game_files` 保存当前文件，`game_variants` 保存每个 `(game,core)` 的当前 Provider Target、DAT、依赖快照和兼容状态。编辑、替换和重新验证在原稳定 ID 上推进 `version`；历史变化写入 audit/event/evidence，不再建立 metadata、content 或 variant 的平行业务修订树。

`providerId` 与 `targetId` 是跨升级稳定的语义身份。Provider 当前版本和 manifest 投影可以前移，但已创建的 `launch_sessions` 会冻结当次 `bundleSha256`、内容文件、外部依赖文件、Target、options 和恢复输入。Bundle 升级不会让现有审核结果或已发布 Variant 自动 stale；只有来源内容、Core/Target、DAT、依赖闭包、项目证据或其他真实验证输入改变时才需要重新检查。

内容替换是破坏性的 current-state 切换：新内容必须先完整准备并验证，事务提交时撤销旧 Launch/Netplay、结束游玩、删除旧存档和旧派生文件，再原子写入当前文件、profile 与 Variant；失败时旧当前态保持不变。BIOS 替换只撤销使用旧 BIOS 的运行并阻断相应 Variant 等待重验，game-scoped 存档继续保留。

## 3. Launch Envelope V1

`GET /runtime/launches/{launchId}/config` 只返回 `LaunchEnvelopeV1`：

- `session` 保存用途、模式、展示上下文与返回位置；
- `runtime` 保存 Provider 版本、冻结 Bundle、稳定 Target、模块 URL/SHA-256、能力与 checkpoint declaration；
- `resources[]` 保存带 role、kind、大小、摘要和访问 URL 的授权输入；
- `targetOptions` 是由当前 Target 的闭合 `targetOptionsSchema` 校验后的 Provider 私有配置；
- `restore`、`validation`、`netplay` 分别是可空的标准恢复、验证和联机输入。

Go 在签发前验证 envelope 和 Target options；dispatcher 验证 JSON 边界、模块 URL、模块摘要、Provider 身份与 API 版本；Provider Module 再按自身 manifest 校验 Target options 后执行 `mount(envelope)`。任一身份、摘要、schema、资源或能力不一致都 fail closed。

## 4. Provider dispatcher 与渲染隔离

Player Host 只消费 `PlayerRuntimeV1` 的标准能力和事件，不按 Provider、Target 或游戏类型分支。暂停、音量、输入过滤、视频模式、换盘、截图、帧计数、checkpoint、联机端口和退出由 Provider 实现。退出、异常与 React 卸载共用 exactly-once cleanup；Host 先等待 Provider `exit()`，再撤销 frame、MessagePort、observer 和请求 signal。

除独立 origin 的 Web 项目外，会挂载 DOM/canvas 的运行时都在 Provider 创建的同源空白 frame 内执行。Provider 负责满尺寸 surface、原始宽高比最大内接、居中和 resize observer；Host 不给单个核心补 CSS。该边界同时防止核心全局变量、异常和样式污染 Next.js document，并保证普通与沉浸 Player 一致。

浏览器开发工具注入的 Web Vitals 脚本不属于游戏运行时。应用 document 与运行 frame 都安装窄范围防护，只吞掉该已知脚本在延迟回调中读取缺失 `startTime` 的异常；其他同步错误、Promise rejection 和 Provider 错误仍正常上报。该防护不改变核心逻辑，也不依赖错误出现时序。

## 5. 资源与项目运行时

Provider 静态文件只从 `/runtime/providers/{providerId}/{bundleSha256}/{runtimePath}` 提供，并同时受 closed allowlist、大小和 SHA-256 约束。游戏、BIOS、parent、多盘、项目文件、运行包和 cart 不属于 Provider Bundle，通过 envelope resources 授权；Provider 不得根据扩展名、标题或 Core 名称猜测输入。

`retrom-runtime` 的 Target 覆盖 EasyRPG、mkxp、MV/MZ、ONS、KiriKiri、Butterscotch、TyranoScript 与 WASM-4。项目可使用 file tree、seekable blob、native web 或 isolated web 资源。MV/MZ bridge 保留 Canvas2D 对非法 `textAlign` 赋值“忽略并保持原值”的浏览器语义；Butterscotch 保留真实 `640×480` backing buffer，但显示尺寸始终按容器等比放大；KiriKiri 在 core `postRun` 后进入可玩状态，checkpoint availability 独立等待书签 API 就绪，其精确的脚本退出 Wasm trap 会转换为一次 `EXIT_REQUESTED`；非匹配 trap 不会被吞掉。所有 Provider 都必须在游戏自身退出时发出标准退出事件，使整个 Player 页面同步关闭。

独立 origin 的项目按 Launch 使用不同 Host。一次性 bootstrap ticket 和 HttpOnly capability 只授权当前 Launch 的封闭资源；项目脚本不能取得应用 Cookie、普通 API 或其他 Launch 内容。cleanup 撤销 capability、过期 Cookie 并清理对应存储。

## 6. Checkpoint 与存档

Checkpoint 对 Host 是不透明字节。Target declaration 的 `writeFormat`、`readFormats[]` 和 `maxBytes` 是唯一格式规则。创建存档时，来源 Launch 必须属于同一 Profile/Game 且允许存档，格式必须位于 `readFormats`、大小和 SHA-256 必须闭合；Host 不解析 Provider payload。

`save_states` 只绑定 Profile、Game、checkpoint format、payload、可选截图/DOS 路径/disc index 和来源 Launch，不冻结 Provider 版本或 Variant。恢复时使用游戏当前默认或显式 Core 的 READY Variant；只要当前 Target 的 `readFormats` 包含该格式即可恢复。Provider 升级应继续声明仍受支持的旧格式；删除已被存档引用的可读格式会被安装门禁拒绝。不存在为了恢复而加载旧 Provider 的路径。

普通与沉浸模式使用相同的受保护存档 HTTP 端点和 capability cookie；iframe/frame 内的请求通过明确的 credential 策略发送，不能依赖应用页 Cookie 偶然透传。

## 7. 审核与 RPG Runtime Validation

非 RPG 审核 preview 和正式 Launch 复用相同 Provider Module 与 envelope。RPG Maker 审核保存 generation、项目 fingerprint、来源快照、Provider/Target 与依赖摘要；创建正式 `RPG_RUNTIME_VALIDATION` Launch 后可发布。A→B checkpoint→C→独立 restore Launch 的 14 个 gate 是可选高级验证及自动化验收基线。

草稿 PATCH、来源替换和依赖处理按当前真实输入更新或创建 validation，并原子切换 ReviewDraft 的当前选择；审核页没有 `validationStale` 或人工“重新运行检查”状态。Provider Bundle 前移不会改变稳定 Provider/Target，也不会要求用户在上传后无故重检；来源、Target、DAT、依赖或项目证据改变时，对应写事务直接生成新的当前校验。当前 validation 即使为 BLOCKED，仍允许尽最大可能启动诊断 Player。

## 8. 联机

联机资格由稳定 Provider/Target、Target 的标准能力和 Retrom 的受控 profile 共同决定。Netplay profile 与 session 冻结 Bundle、Provider/Target、内容和依赖摘要；不再维护平行的稳定 Target字段。参与者必须取得完全一致的冻结输入。Provider 只通过 `PlayerRuntimeV1.netplayPort` 交换标准消息；单机 Launch 不取得联机凭据，联机 Launch 禁止普通存档。

## 9. PlaySession 生命周期

Provider 报告真实 ready/start 后，Host 才创建 PlaySession。heartbeat 以连续序号报告上一时段的 running/visible/paused，服务端按接收时间计费；页面隐藏、暂停、失联、重放或跳号不能伪造时长。用户菜单退出、游戏自身退出和异常退出最终都幂等 finish Launch；卸载失败由 hard expiry 收口。

## 10. 验证与发布门禁

实现变更必须覆盖：Provider manifest/完整性/升级门禁、47 个 Target 的 binding 闭包、Go 与 TypeScript envelope fixtures、dispatcher 装载与 cleanup、current-state 数据不变量、存档跨 Bundle 读取、内容与 BIOS 替换、普通/沉浸 Player、RPG validation、多盘、Pegasus 与 EmulationStation/gamelist 导入。

标准门禁是 `make api-check`、`make backend-check`、`make web-check`、`make integration-test`、`make data-check` 和 `make pfb-verify`。PFB 使用隔离 worktree、持久 workspace 与稳定 URL；开发期 loose module 只叠加到已验证基座 Bundle，不进入 production lock 或正式镜像。真实样本验收必须走产品上传、审核、发布、启动、存档与退出链路，不能绕过 API 直接写结果。
