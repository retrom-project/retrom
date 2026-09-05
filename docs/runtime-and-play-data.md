# Runtime Provider 与游玩数据

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 当前态简化实施中 / 已确认目标契约 |
| 版本 | 4.0 |
| 日期 | 2026-09-05 |
| 机器事实源 | `api/runtime-provider/v1/`、已激活 Provider Bundle、`data/runtime-target-bindings/v1/catalog.json` |

## 1. 唯一职责边界

具体引擎的入口、静态资产、Target 能力、输入资源、Target options 和 checkpoint 格式只由 Provider Bundle 的 manifest 声明。Retrom Host 负责验证并激活 Bundle、把产品 Core 绑定到稳定的 `(providerId,targetId)`、准备授权资源并签发 Launch；Host 不保存或推导 Provider 私有 adapter、core、入口文件和资产映射。Web Player 不维护按引擎分支的 registry，只通过共享 dispatcher 加载 Provider Module V1。

当前可部署 Provider 是 `emulatorjs` 与 `retrom-runtime`。Host 目录描述产品身份和接入策略，不复制 Target declaration。Provider 安装采用只向前升级：版本必须递增，同版本换字节和降级都被拒绝；没有旧 manifest reader、Bundle fallback 或运行时回滚路径。

### 1.1 产品目录与数据库解耦

复用 `data/runtime-target-bindings/v1/catalog.json` 与 `internal/runtimecatalog`，将平台、核心、平台/核心关系、可接收内容分类、内置资源包定义及产品 binding 汇入同一 Host 声明目录。Provider manifest 仍独占 Target 能力、私有 options schema、当前 checkpoint 格式与实现资产；推荐目录模板只负责用户目录的创建建议，不另立核心接入注册中心。

目录只保留当前 `schemaVersion` 和内容摘要，不设独立 `catalogVersion`、revision 或算法代际。新增现有平台的核心/Target、采用已注册存储/检测/交付策略的接入、采用现有布局策略的资源包，只修改声明及对应 Provider 产物，不修改 SQL 或清库。新增真正的持久化业务结构才需要 migration。

启动必须先完成全部声明、Provider 字节、引用闭包以及 detector/delivery/review/pack-layout 策略注册验证；未知策略直接拒绝。之后在同一事务内按依赖顺序同步产品定义 → Provider/Target → binding/资源包关联 → 当前目录摘要和审计，事务提交后才提供 HTTP。不得在失败后留下部分目录，也不得用宽泛异常捕获尝试旧 manifest。

声明式同步只更新系统拥有的定义，用户目录名称、排序、默认核心、启用选择和已安装资源包不被 seed 覆盖。稳定 ID 不随实现发布而变化；移除被引用定义必须明确拒绝，不能级联删除用户游戏或从旧证据恢复历史运行选项。完全未被引用的移除由同一事务完成。

### 1.2 最终模型与接入策略

`platforms`、`cores`、`platform_cores`、`content_kinds`、`runtime_asset_pack_definitions` 是当前声明的关系投影，不在 migrations 写入具体引擎/RTP seed。SQL 只维护外键、owner、唯一性、生命周期、路径、大小与结构边界；引擎名单、布局映射和识别规则由受限策略处理。

上传用途统一描述普通导入、项目导入或资源包安装，不用每个引擎名称扩展 DDL。文件、目录与压缩包事实保持明确；普通 ZIP 和目录归一化后进入同一检测与导入链路。策略是显式注册的普通代码，不建立动态插件执行或万能 JSON/EAV 数据库。

Binding 只选择接入策略、产品允许的内容子集和独立的启用策略；固定 delivery、review 和 options 行为从 `runtimecatalog` 的同一策略派生，不能在 JSON binding 中重复声明后再比较是否相等。现有数据库列是派生投影，不是第二份声明权威。

项目分类、项目内容类型及上传扩展名从现有 `contentprofile` 推导；项目归档格式变更不需要维护第二份平台名单。导入、审核、启动和内容替换共用 `contentcapability.Policy`。查询通过同一个标量投影读取所选 binding 的关系化内容类型，并在原 SQL 语句/事务内构造能力；不另开查询、不在 SQL 中组装策略 JSON。多盘限制与交付规则只在 Go 构造函数中定义，只有任务快照、API 或摘要边界序列化策略。支持类型按集合规范化，与单个内容相关的校验摘要只包含所选类型及其规则，不因顺序或无关能力扩展失效。

Launch options 按声明绑定的明确接入策略一次组装，再接受 Provider 的闭合 schema 校验；不得在多个无关入口逐一猜测未知属性，更不能把不支持的配置伪装成认证错误。依赖快照中的静态 BIOS/多盘与 Arcade 是不同业务类型，使用明确 discriminator，不以 v1/v2 伪装历史兼容链。

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
- `restore`、`netplay` 分别是可空的标准恢复和联机输入；生产 Envelope 不携带研发验证脚本或位置证明。

Go 在签发前验证 envelope 和 Target options；dispatcher 验证 JSON 边界、模块 URL、模块摘要、Provider 身份与 API 版本，然后只调用 `createRuntime(envelope, host)`。Provider 创建入口按自身声明验证外部 Envelope 与 Host，直接构造核心私有的最小类型参数，不再提供单独预检，也不在内部重复验证相同 Envelope 或转换后的通用 config。下载文件、解码 checkpoint、跨 origin 消息仍在各自信任边界校验；任一身份、摘要、schema、资源或能力不一致都 fail closed。

Provider 是核心生命周期的唯一所有者，不包装第二个 controller。公开状态为 `CREATED/MOUNTING/RUNNING/PAUSED/CHECKPOINTING/EXITING/EXITED/FAILED`；暂停、恢复、checkpoint 和控制操作共用一个队列，退出可抢占排队及进行中的操作。启动在 restore、frame 和 core 等异步边界后检查取消，晚到的核心只清理、不重新进入 RUNNING。核心主动退出只发出一次公共退出事件；失败保持 FAILED 终态，退出清理幂等。Host 继续独立负责页面导航、iframe 与授权会话，不承担核心内部状态转换。

## 4. Provider dispatcher 与渲染隔离

Player Host 只消费 `PlayerRuntimeV1` 的标准能力和事件，不按 Provider、Target 或游戏类型分支。暂停、音量、输入过滤、视频模式、换盘、截图、帧计数、checkpoint、联机端口和退出由 Provider 实现。退出、异常与 React 卸载共用 exactly-once cleanup；Host 先等待 Provider `exit()`，再撤销 frame、MessagePort、observer 和请求 signal。

除独立 origin 的 Web 项目外，会挂载 DOM/canvas 的运行时都在 Provider 创建的同源空白 frame 内执行。Provider 负责满尺寸 surface、原始宽高比最大内接、居中和 resize observer；Host 不给单个核心补 CSS。该边界同时防止核心全局变量、异常和样式污染 Next.js document，并保证普通与沉浸 Player 一致。

从 Host 控制栏或暂停遮罩恢复运行后，Provider 在核心确认恢复且会话仍有效时，把键盘焦点交还游戏 canvas；不暴露 canvas 的隔离项目聚焦其运行窗口。暂停、恢复失败或被退出抢占时不得抢回 Host 焦点。这个行为由两个 Provider 的公共入口实现，不由单个核心或验收脚本补焦点。

浏览器开发工具注入的 Web Vitals 脚本不属于游戏运行时。应用 document、Provider 与运行 frame 不拦截或吞掉该脚本的异常，也不修改浏览器性能 API、DevTools 设置或其独立执行上下文。匿名脚本错误必须先按执行上下文、脚本字节与实际堆栈定位，不能因含有 `startTime` 就归因于 Player；诊断与回归边界见工程质量专题第 8.2 节。

## 5. 资源与项目运行时

Provider 静态文件只从 `/runtime/providers/{providerId}/{bundleSha256}/{runtimePath}` 提供，并同时受 closed allowlist、大小和 SHA-256 约束。游戏、BIOS、parent、多盘、项目文件、运行包和 cart 不属于 Provider Bundle，通过 envelope resources 授权；Provider 不得根据扩展名、标题或 Core 名称猜测输入。

`retrom-runtime` 的 Target 覆盖 EasyRPG、mkxp、MV/MZ、ONS、KiriKiri、Butterscotch、TyranoScript 与 WASM-4。项目可使用 file tree、seekable blob、native web 或 isolated web 资源。MV/MZ bridge 保留 Canvas2D 对非法 `textAlign` 赋值“忽略并保持原值”的浏览器语义；Butterscotch 保留真实 `640×480` backing buffer，但显示尺寸始终按容器等比放大；KiriKiri 在 core `postRun` 后进入可玩状态，checkpoint availability 独立等待书签 API 就绪，其精确的脚本退出 Wasm trap 会转换为一次 `EXIT_REQUESTED`；非匹配 trap 不会被吞掉。所有 Provider 都必须在游戏自身退出时发出标准退出事件，使整个 Player 页面同步关闭。

独立 origin 的项目按 Launch 使用不同 Host。一次性 bootstrap ticket 和 HttpOnly capability 只授权当前 Launch 的封闭资源；项目脚本不能取得应用 Cookie、普通 API 或其他 Launch 内容。cleanup 撤销 capability、过期 Cookie 并清理对应存储。

## 6. Checkpoint 与存档

Checkpoint 对 Host 是不透明字节。Target declaration 的 `writeFormat`、`readFormats[]` 和 `maxBytes` 是唯一格式规则。创建存档时，来源 Launch 必须属于同一 Profile/Game 且允许存档，格式必须位于 `readFormats`、大小和 SHA-256 必须闭合；Host 不解析 Provider payload。

`save_states` 只绑定 Profile、Game、checkpoint format、payload、可选截图/DOS 路径/disc index 和来源 Launch，不冻结 Provider 版本或 Variant。恢复时使用游戏当前默认或显式 Core 的 READY Variant；只要当前 Target 的 `readFormats` 包含该格式即可恢复。Provider 升级应继续声明仍受支持的旧格式；删除已被存档引用的可读格式会被安装门禁拒绝。不存在为了恢复而加载旧 Provider 的路径。

普通与沉浸模式使用相同的受保护存档 HTTP 端点和 capability cookie；iframe/frame 内的请求通过明确的 credential 策略发送，不能依赖应用页 Cookie 偶然透传。

## 7. 审核试运行

审核只运行当前算法，不保存 `prepublish_generation` 或历史算法选择。有效性取决于当前来源、稳定运行选择、DAT/依赖闭包和与该内容有关的校验规则；目录展示字段、无关核心及单独的 Provider 发布变化不使正常审核失效。需要重算的算法修复通过明确限定范围的当前态重新校验完成，不增加 schema 代际。

所有内容类型均使用普通审核 preview 和同一 Player，流程为“运行游戏 → 试玩 → 返回审核 → 管理员通过/拒绝”。RPG Maker 的 generation、项目 fingerprint、来源、Provider/Target 和依赖摘要仍用于真实检测及资源装配，但不创建另一套运行验证或人工证明状态机。试运行不创建 Game 或 Variant，不计入已发布游戏的游玩记录。

试运行可使用标准截图与 checkpoint。临时 checkpoint 只归属创建它的审核会话和操作者；恢复创建普通的新 preview，要求当前审核来源、目标与 checkpoint 可读格式匹配，不要求特定原会话/恢复会话事件顺序。临时数据在过期或审核 payload 释放时清理，不进入用户存档列表，也不参与 Provider 升级预检。已存在的持久用户存档继续受到 `readFormats` 升级门槛保护。

退出、关闭、失败和加载取消都走相同 Player/Provider 清理并撤销试运行授权；可重复试运行，不维护 gate、序列、机器证明或独立 PASS/FAIL 决定。精确帧、输入、画面及跨会话位置恢复断言仅存在于研发验收，不能为测试保留生产探针 API、fixtureState 或 A/B/C 证明协议。

草稿 PATCH、来源替换和依赖处理按当前真实输入更新或创建 validation，并原子切换 ReviewDraft 的当前选择；审核页没有 `validationStale` 或人工“重新运行检查”状态。Provider Bundle 前移不会改变稳定 Provider/Target，也不会要求用户在上传后无故重检；来源、Target、DAT、依赖或项目证据改变时，对应写事务直接生成新的当前校验。当前 validation 即使为 BLOCKED，仍允许尽最大可能启动诊断 Player。

## 8. 联机

联机资格由稳定 Provider/Target、Target 的标准能力和 Retrom 的受控 profile 共同决定。Netplay profile 与 session 冻结 Bundle、Provider/Target、内容和依赖摘要；不再维护平行的稳定 Target字段。参与者必须取得完全一致的冻结输入。Provider 只通过 `PlayerRuntimeV1.netplayPort` 交换标准消息；单机 Launch 不取得联机凭据，联机 Launch 禁止普通存档。

## 9. PlaySession 生命周期

Provider 报告真实 ready/start 后，Host 才创建 PlaySession。heartbeat 以连续序号报告上一时段的 running/visible/paused，服务端按接收时间计费；页面隐藏、暂停、失联、重放或跳号不能伪造时长。用户菜单退出、游戏自身退出和异常退出最终都幂等 finish Launch；卸载失败由 hard expiry 收口。

## 10. 验证与发布门禁

实现变更必须覆盖：Provider manifest/完整性/升级门禁、47 个 Target 的 binding 闭包、Go 与 TypeScript envelope fixtures、dispatcher 装载与 cleanup、current-state 数据不变量、存档跨 Bundle 读取、内容与 BIOS 替换、普通/沉浸 Player、RPG validation、多盘、Pegasus 与 EmulationStation/gamelist 导入。

标准门禁是 `make api-check`、`make backend-check`、`make web-check`、`make integration-test`、`make data-check` 和 `make pfb-verify`。PFB 使用隔离 worktree、持久 workspace 与稳定 URL；开发期 loose module 只叠加到已验证基座 Bundle，不进入 production lock 或正式镜像。真实样本验收必须走产品上传、审核、发布、启动、存档与退出链路，不能绕过 API 直接写结果。
