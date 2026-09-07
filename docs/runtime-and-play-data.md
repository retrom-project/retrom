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

`retrom-runtime` 的 Target 覆盖 EasyRPG、mkxp、MV/MZ、ONS、KiriKiri、Butterscotch、TyranoScript、Java ME 与 WASM-4。项目可使用 file tree、seekable blob、native web 或 isolated web 资源。MV/MZ bridge 保留 Canvas2D 对非法 `textAlign` 赋值“忽略并保持原值”的浏览器语义；Butterscotch 保留真实 `640×480` backing buffer，但显示尺寸始终按容器等比放大；KiriKiri 在 core `postRun` 后进入可玩状态，checkpoint availability 独立等待书签 API 就绪，其精确的脚本退出 Wasm trap 会转换为一次 `EXIT_REQUESTED`；非匹配 trap 不会被吞掉。所有 Provider 都必须在游戏自身退出时发出标准退出事件，使整个 Player 页面同步关闭。

独立 origin 的项目按 Launch 使用不同 Host。一次性 bootstrap ticket 和 HttpOnly capability 只授权当前 Launch 的封闭资源；项目脚本不能取得应用 Cookie、普通 API 或其他 Launch 内容。cleanup 撤销 capability、过期 Cookie 并清理对应存储。

## 6. Checkpoint 与存档

Checkpoint 对 Host 是不透明字节。Target declaration 的 `writeFormat`、`readFormats[]` 和 `maxBytes` 是唯一格式规则。创建存档时，来源 Launch 必须属于同一 Profile/Game 且允许存档，格式必须位于 `readFormats`、大小和 SHA-256 必须闭合；Host 不解析 Provider payload。

checkpoint 可选 `semantics` 声明恢复方式。省略或 `INSTANT` 表示直接恢复执行状态；`GAME_SAVE` 表示游戏原生存档；运行时可显式创建新原生存档，也可要求用户在游戏中完成保存，导入后可能还需通过游戏菜单读档。Player 根据该公共声明展示提示，不按 Core、Target 或格式名称分支。GAME_SAVE 使用公共 availability revision 检测原生数据变化，按当前游玩会话暂存到浏览器，并在退出确认后提交。两种语义共用 Save API、完整性校验、授权与跨 Launch 恢复机制；Provider 必须在启动游戏前导入原生存档并支持读档后的继续输入。RMS 备份不构成即时快照能力，既有即时恢复回归仍保持原断言。

GAME_SAVE 的 `availability.save` 可声明两个独立能力轴：`capture=RUNTIME/IN_GAME` 表示由运行时触发保存或由用户在游戏内保存，`restore=AUTOMATIC/IN_GAME` 表示支持指定槽位启动恢复或需要游戏内读档。`captureAvailable` 是当前能否创建新存档，与 `available`（是否存在尚未同步的原生数据）独立；尚无存档或内容已同步时仍可允许创建。`checkpoint({intent:"CAPTURE"})` 明确请求新原生保存；`checkpoint({intent:"EXPORT"})` 只导出已有文件，也是 GAME_SAVE 省略参数时的默认行为。后台同步必须使用 EXPORT。支持自动恢复的运行时也不能为无法确定槽位的文件包猜测槽位，此类包保留游戏内恢复路径。

核心自行退出时，公共 `EXIT_REQUESTED` 可携带 `finalSnapshot={checkpoint,screenshot}`；核心必须先关闭实时 checkpoint，再等原生写流及引擎清理阶段完成，冻结最终文件后交付。截图允许为 `null`。Provider 校验相同 checkpoint 格式和大小上限并复制 payload，立即结束核心；Host 直接保留并持久化最终数据，不再调用已退出实例的 checkpoint 或截图。最终包不需要向已关闭实例确认；活动会话仍仅在 Host 持久化成功后确认精确 payload。原生数据内容相同的重复写入不得改变 revision。

`save_states` 只绑定 Profile、Game、checkpoint format、payload、可选截图/DOS 路径/disc index 和来源 Launch，不冻结 Provider 版本或 Variant。恢复时使用游戏当前默认或显式 Core 的 READY Variant；只要当前 Target 的 `readFormats` 包含该格式即可恢复。Provider 升级应继续声明仍受支持的旧格式；删除已被存档引用的可读格式会被安装门禁拒绝。不存在为了恢复而加载旧 Provider 的路径。

Provider 可在存档边界无损压缩完整原生 checkpoint，格式仍由 Target declaration 明确声明；解码后也必须满足大小上限。Host 存储、上传进度与界面显示的存档大小均使用实际 payload 字节数，不推算核心解压后的内存大小。

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

实现变更必须覆盖：Provider manifest/完整性/升级门禁、当前 catalog 的 Target binding 闭包、Go 与 TypeScript envelope fixtures、dispatcher 装载与 cleanup、current-state 数据不变量、存档跨 Bundle 读取、内容与 BIOS 替换、普通/沉浸 Player、RPG validation、多盘、Pegasus 与 EmulationStation/gamelist 导入。

标准门禁是 `make api-check`、`make backend-check`、`make web-check`、`make integration-test`、`make data-check` 和 `make pfb-verify`。PFB 使用隔离 worktree、持久 workspace 与稳定 URL；开发期 loose module 只叠加到已验证基座 Bundle，不进入 production lock 或正式镜像。真实样本验收必须走产品上传、审核、发布、启动、存档与退出链路，不能绕过 API 直接写结果。

## Java ME 产品接入

Java ME 目录使用基础平台 `j2me`、稳定核心 `j2me` 和 `retrom-runtime/j2me` Target。
Host 的 `J2ME_JAR` 检测/交付策略仅接受原始 `.jar`，按 `SINGLE_FILE` 保留完整字节，以 `ROM_BLOB`
交给 Provider；JAR 自身的 ZIP 容器不会被当作上传包装层解开。审核预览和普通 Launch 经过同一资源与生命周期契约。

该 Target 声明 `checkpoint.semantics=GAME_SAVE`。Player 普通控制栏、退出对话框与沉浸菜单提示先在游戏内
保存；任意 RMS 数据变化后在浏览器本地暂存，恢复启动后需要从游戏菜单读档。Host 的 Save API 继续接收有格式、上限和 SHA-256
身份的完整 opaque payload，不解析 RMS、选择 Java 类或保存 VM 内存。原有 Target 缺省即时恢复语义不变。

开发验证需要包含修复的 J2ME core candidate。历史 v0.3.3 资产不包含此次 RMS 与严格静态资源加载修复，
不得据此声称修复已经发布；发布固定版本与合并是独立交付步骤。

### 原生存档本地草稿与退出确认

GAME_SAVE Provider 通过 availability revision 跟踪所有原生数据变化，不判断哪些 store 是进度。连续写入合并到稳定完整快照。
Player 在当前账号、Launch 范围内将数据包、截图和固定幂等请求保存到 IndexedDB；后台同步不会上传或修改正式存档。运行时支持主动保存且当前场景允许时，普通和沉浸菜单的“创建存档”显式请求 CAPTURE，先持久化完整包，再经同一 Save API 提交；成功后确认精确 payload 并移除草稿。未保存、已同步与暂不可保存是独立状态。
不在本地暂存时调用 acknowledgeCheckpoint，保留启动数据作为比较基准；最终数据回到启动值时清理草稿，退出时仍提示确认。当前具备主动保存能力时仍可创建新的原生存档；没有主动保存能力且数据未变化时不提供保存操作。

无 saveStateId 的 Launch 必须从空原生数据启动。服务端历史存档、此前本地草稿以及其他运行实例均不能作为隐式恢复输入。
只有显式选择存档时导入冻结恢复包。本地草稿数据库不向 runtime 提供启动数据，不自动合并、恢复或清除其他 Launch 的草稿。

正常退出先暂停并等候稳定数据，结合实例当前主动保存能力与数据变化显示确认弹窗。没有提供主动保存能力的游戏保留游戏内保存流程；即时快照退出流程不变。
有变化时提示数据已变更，并提醒用户确保本次在游戏中主动执行过“保存游戏”，避免异常数据变更覆盖此前存档；
按顺序提供“返回游戏 / 直接退出 / 存档并退出”。没有主动保存能力且无变化时提示本次似乎未进行存档操作，提醒退出前在游戏中主动保存，
仅提供“返回游戏 / 继续退出”，不创建或更新存档。两种状态均默认聚焦“返回游戏”，键盘与手柄按可见按钮顺序操作。
普通与沉浸 Player 选择返回时关闭确认、回到游戏；Escape 或手柄 B 等同返回，不上传或丢弃草稿。
具备主动保存能力时，“存档并退出”先请求引擎创建原生存档；否则只提交游戏已写入的数据。整个流程不序列化模拟器内存。存档成功后才确认 checkpoint、清理草稿并退出；
直接退出只丢弃本次草稿。上传失败保留草稿和幂等键、保持弹窗并允许重试；保存期间禁用全部操作，防止并发退出。
核心自行结束时，Player 接收最终存档后仍允许用户保存、直接退出并丢弃草稿，或保留草稿后退出；不显示“返回游戏”，也不调用已结束核心的暂停、截图或 checkpoint。保留草稿必须先确认浏览器持久化成功；本地存储失败时仍保留当前页内存数据，可直接上传或重试。最终存档没有截图时保存有效 payload，并省略截图表单项，不生成占位图片。
从已有存档启动时提交更新原存档；无存档启动时提交创建独立存档。payload 与截图原子更新，保留原 ID、名称和创建时间。

异常关闭保留本地草稿，下一次非 Player 页面提示用户处理；当前账号可通过认证的 local-save API 显式提交已结束/到期会话的草稿。
服务端从原 Launch 取得目标和预期数据版本，其他会话已更新或目标已删除时拒绝覆盖。活跃页面通过本地租约避免被草稿提示并发处理。
草稿只属于当前浏览器，不承诺跨设备或清理站点数据后的恢复；不会自动载入新的游戏。未确认时不建立服务端草稿或正式存档。
Review Preview 保持预览范围，不创建 Product 草稿记录或正式存档；即时快照行为不变。

Player 调试面板的“画面呈现率”由公共 getFrameCount 的增量计算，不代表屏幕刷新率或游戏逻辑速度。按需重绘核心可在游戏画面静止时停止提交帧；Host 不插入重复帧补足 60 FPS，输入与暂停控制继续正常工作。

### ScummVM 项目与原生恢复

`retrom-runtime/scummvm` 消费一份 `game: FILE_TREE`。Launch 冻结审核选定的
`engineId`、`gameId`、相对 `root`、`language`、`platform`、`extra`、`guiOptions` 与可选
`filename`；用户标题与会话 ID 不参与 ScummVM 游戏 target 命名。完整来源树保持不变，
相对 root 只决定本次运行的游戏目录。索引和逐文件内容沿用 Launch capability 与来源摘要授权。

Provider 使用 `scummvm-save-bundle-v1`（`GAME_SAVE`，上限 64 MiB），原生文件集合为不透明 payload。
运行中的手动保存使用 `CAPTURE`；后台草稿只用 `EXPORT`。正常退出时，如当前游戏仍允许原生保存，
即使尚未写过存档也提供“存档并退出”，明确调用原生保存后提交。当前场景不允许主动保存时只收集已完成的文件。
核心自行结束时禁止再调用活跃保存接口，沿用最终快照和本地草稿退出流程。

准确的恢复槽位由 Provider 保存于 payload。自动恢复同时要求引擎支持指定存档启动和核心能确认实际读档结果；当前构建已接入 Sky、SCUMM、SCI、Queen、Drascula 的结果通知，其他引擎保留游戏内读档。新 Launch 在运行前导入文件，等待准确槽位的成功通知后才完成运行时装载；失败、槽位不符或 60 秒内未完成均报错，不能静默新开游戏。
仅收集游戏菜单写入、无法确定准确槽位或游戏不支持自动启动恢复时，完整导入后由用户在游戏菜单读档。
未选择存档的新 Launch 使用空保存目录。当前不声明 ScummVM 即时内存快照、联机或回滚能力。

代表性产品验收见 [ACC-SCUMMVM-001](./project-acceptance.md#acc-scummvm-001scummvm-原生存档与手柄产品闭环) 与 [ACC-SCUMMVM-002](./project-acceptance.md#acc-scummvm-002scummvm-延迟保存原生退出与手动读档)。构建成功或能力标志不等于全部游戏经过实测。
