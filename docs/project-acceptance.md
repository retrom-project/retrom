# 当前项目验收

本文件定义当前完整验收标准。以下T01–T47均为必须逐项追踪的Case，不是通过声明；取消旧能力不取消保留行为的回归。测试不能靠旧站、旧SQL种子、伪造payload、标题图标或声明数量冒充新链路。

证据记录Case、源码/工具/Provider SHA、环境与负载、步骤、硬超时、实际响应/SQL/文件/浏览器结果、PASS/FAIL/不适用及原因。私有素材需明确授权且源只读，素材/真实路径/会话/password/token不进入正式文档或Git。受管副本和机器记录放忽略的验收workspace。历史UI参照只用于视觉比较，不是当前功能通过。

| Case | 场景 | 必须验证 |
| --- | --- | --- |
| T01 | 新空库应用目标 migration | 建立目标表与索引；目录和目录核心关系为空，没有平台／核心业务 seed 或具体核心 ID 的字段默认值；不读取历史表或转换旧格式，账号初始化由用户模块处理 |
| T02 | schema introspection | 应用 schema 无表／列／DOMAIN CHECK、无 FK、无用户 trigger、无普通／物化 view；所有应用表有 `_tab` 后缀 |
| T03 | 字段有效性 | 非法枚举、长度、hash、时间顺序、跨字段条件均由代码拒绝；HTTP 和后台写入不绕过同一规则 |
| T04 | 账号结构重写 | 保持账号行为；私有数据直接归 user_id，去除 Profile／CHECK 后输入规则和权限边界仍成立 |
| T05 | 关系与索引 | 关系使用主键／唯一索引；列表按用户、目录、标签和时间分页，无逐行 N+1 |
| T06 | 目录删除 | 普通有游戏场景拒绝、空目录可删；不要求串行化消除用户已接受的竞态 |
| T07 | Pegasus 与 EmulationStation 分别导入 | 都直接创建 Game／文件／媒体及标签关系；来源资料保留，无外部刮削请求 |
| T08 | 复制后删除源文件 | 已发布游戏正常读取受管内容 |
| T09 | 扫描、复制、提交中断 | 单条文件准备完成才创建待审 Game，与成功计数原子提交；中断不留下半条游戏，已提交游戏保留，未提交残留可回收。重启遗留任务标为 interrupted，重新扫描不依赖历史任务或恢复游标；提交结果无法确认时明确中断，不盲目继续或把已提交内容计为失败 |
| T10 | 批准／拒绝／扫描取消 | 批准仅改同一 Game 状态；拒绝进入 deleted；二者不改扫描进度，扫描完成不等待审核；取消阻止未提交内容继续入库，保留已入库游戏 |
| T11 | 清理已终结的扫描进度记录 | 对 game / bios 类型均直接删除进度，不查询／更新 Game 或 BIOS 文件及其从属行；待审审核、正式游戏、已安装 BIOS、媒体、标签、启动及存档继续正常，不保留导入历史 |
| T12 | ROM／BIOS 替换 | 更新成功不触发审核、验证 Job 或全库状态重算 |
| T13 | Game 软删除与清理失败 | 立即不可新启动；失败保持 deleted 并保留路径，重启／Redis 丢失后继续；完成才转 purged，无 cleanup_error 或清理表 |
| T14 | 收藏／收藏夹／标签 | 私有／共享边界正确；成员增删不改变目录或运行配置。删除标签只改 Tag 自身状态／版本，保留关系、不推进 Game 版本；待审与发布的标签展示／筛选／计数均排除它，数量上限不计失效关系；旧标签 ID 不能用于新增关联或被静默忽略筛选条件；同名重建为新 ID，旧关联不复活 |
| T15 | 最近游玩 | 成功运行后更新最后时间；没有次数、时长、PlaySession 或计时心跳 |
| T16 | 单 ROM、街机 Parent、项目游戏 | 统一公共运行接口启动，覆盖 NES／DOS、MV／MZ 文件树、ScummVM 实际游戏选择及 OpenBOR PAK 等保留类型；不需要业务模块解释引擎配置。文档 JSON 仅示例，实际验收使用正式契约和真实可运行内容，不导入 td 文件作为测试 fixture |
| T17 | 核心选择与目录默认变更 | 普通启动遵循选择／默认；存档仍使用原核心，无静默 fallback |
| T18 | 即时／原生存档 | 真实非空 payload 可在新实例恢复，原生读档语义明确 |
| T19 | ROM 或原核心指纹变化 | 旧存档可见但不可恢复；后端同样阻止；无关核心升级不影响 |
| T20 | 两个标签页覆盖同一存档 | 版本冲突明确，不覆盖较新进度；失败不确认 checkpoint 已保存 |
| T21 | 启动后替换内容，再保存 | 使用启动时上下文，不冒充新 ROM/core；旧进度按新当前态显示不可恢复 |
| T22 | 登录与隔离项目加载 | 不创建独立运行凭据；隔离项目仍可正常加载脚本、Worker、音视频和存档 |
| T23 | 越权／撤销 | 上下文 ID 不能跨用户获取存档；退出／停用后后续请求不获权 |
| T24 | Redis key 过期／实例重启 | 已确认存档、账号、两类扫描进度、BIOS 安装与路径、Game 审核／删除状态不丢；缓存可重算，限流窗口可重置；扫描执行中断按 T09／T45 处理，不承诺自动续跑 |
| T25 | Redis 丢失时未同步原生草稿 | 不误报成功、不用当前 hash 伪造旧上下文；草稿保留和恢复按验证后的协议处理 |
| T26 | 清空 Redis 后恢复已确认存档 | 不需要原运行 key；按当前核心和持久 extinfo 创建新运行 |
| T27 | UI视觉验收 | 桌面、390px移动与4K/150%人工视觉检查，关键页面、弹层、运行和审核保持已保留能力；自动无溢出不能代替人工判断。 |
| T28 | 有界规模与查询 | 20,000 Game/约50,000文件的查询计划、扫描峰值内存及200用户混合负载分别实测；说明机器、同机负载与测试边界。 |
| T29 | Redis 认证限流 | 作用域、并发计数、TTL 和限流反馈有效；无数据库限流表；Redis 不可用时受保护入口明确暂不可用，丢失计数后窗口可重置 |
| T30 | 待审与发布共表的访问边界 | 猜测 Game ID、媒体 ID 或绕过 UI 均不能让普通用户读取／运行待审内容；筛选和计数不泄漏待审游戏；试玩不写最近／正式存档 |
| T31 | 替换、存档覆盖与孤立文件清理 | active 引用不误删；旧文件删除失败后再次可发现；写入中断／引用更新冲突的残留可回收，宽限期不与正常提交竞争 |
| T32 | PFB 与 Git 连续性 | 记录各仓库基准及实际提交；实现来自现有仓库的 PFB，Git 历史可追溯，无新建替代工程。清空范围限定在选定工作树，无关改动和原 checkout 保留 |
| T33 | 脱离旧代码独立构建和启动 | 在不挂载旧 checkout／backup 的验收环境，用目标提交、显式声明的依赖和新数据完成生成、构建与启动。构建脚本、别名、软链接、镜像和部署输入均不读取旧参考目录或 td；正常声明的核心／Provider 依赖仍保留 |
| T34 | 复用组件的依赖边界 | 从实际编译依赖、前端模块引用和调用关系核对引入组件，直接／间接依赖均符合目标模块职责；没有因复制组件带回旧 store、DTO、审核编排或运行凭据。变更说明可定位复用来源和新环境验证结果 |
| T35 | 目标 schema 与持久化路径 | 空库实际应用表逐项符合 03 的目录，当前为 19 张，migration bookkeeping 单列说明；结合 SQL 访问核对字段与职责，不只比较数量。不存在旧表读写、重命名恢复的旧实体、JSON 隐藏工作流或双写；无 FK/CHECK/TRIGGER/VIEW |
| T36 | 目标 API 与运行调用链 | 普通、管理、审核、存档、移动及沉浸入口实际调用新契约；统一运行模块解释配置，登录态直接授权。路由、后台任务、生成代码和消费者均无旧流程旁路、兼容别名、二次凭据或新旧切换开关 |
| T37 | 保留功能与视觉完整性 | 需求逐项对应有效验收 Case、目标提交和实际结果；八模块、全平台／核心矩阵、即时及原生存档、UI／移动／沉浸与规模场景均有证据。只允许隐藏已明确取消的数据，不以删掉保留能力或假数据换取结构通过 |
| T38 | 契约、工程配置与交付一致性 | 正式文档、适用开发指令、API/schema 生成源及产物、依赖锁定、构建部署和测试均描述并使用目标实现；旧业务约定已退出有效路径。交付产物与验证提交一致，必要检查无失败、跳过或以历史结果替代本次结果 |
| T39 | Game 与导入过程解耦 | Game 仅保存来源类型，当前服务端扫描创建 server_import；编辑／替换不改变来源，Game 及其文件／JSON／DTO 无扫描或上传批次标识。扫描表仅类型、状态和数量等进度信息，不持有 Game／BIOS 文件 ID、游标或来源映射；审核、运行及游戏删除不读取扫描记录 |
| T40 | 游戏数量进度直达统一审核 | 用两个服务端来源扫描产生待审游戏；游戏任务只显示状态和总数／已扫描等计数，点击任一游戏任务进入同一个汇总列表。列表和详情均无来源／任务分组筛选，无导入历史页面或接口；当前仍没有游戏上传入口 |
| T41 | Game CAS 与运行身份分离 | 两个管理页面基于相同 version 修改同一 Game，后提交的旧版本请求不能覆盖新值；文件关系切换同事务回滚。只改资料不改变 ROM hash 或存档可恢复性，核心／BIOS／目录配置变化不批量推进 Game version |
| T42 | BIOS 在启动时解析 | 同一要求可供多个 Game 使用，切换核心按对应声明重新解析；必需文件缺失报错。替换／移除 BIOS 只改自身记录，后续启动使用当前文件或得到缺失错误，不改 Game／CAS／审核状态，不据 BIOS hash 直接判定存档失效；删除 Game 不删除共享 BIOS |
| T43 | BIOS 扫描只补齐缺失项 | 管理员选择目录与平台／核心范围，明确匹配的缺失要求复制到受管目录并成为 active；删除源文件后仍可读取。已安装项保持原文件，未匹配／歧义项跳过并提示手动处理；无候选排名、自动覆盖、归档拼装、安装历史或审核／运行验证状态；普通用户不能发起扫描 |
| T44 | 两类扫描共用进度 | 19 表中仅 scan_progress_tab 保存类型和通用数量；game 按候选游戏计，bios 按去重要求计，多个核心共用一个要求只计一次，无关源文件不增加计数。计数等式由代码保证，成功安装与进度原子提交；卡片单位正确，游戏跳统一待审核、BIOS 跳管理页，均无任务结果过滤；BIOS 不调用审核服务 |
| T45 | BIOS 扫描中断／重扫／并发 | 取消、进程重启或执行所需 Redis 上下文丢失后，已提交安装和进度保留，未提交处理停止，残留文件可回收；仅展示明细过期不影响安装。重扫跳过已有项；并发安装唯一约束冲突转为跳过，不覆盖另一操作；不增加恢复游标、可靠队列或逐项恢复表 |
| T46 | 核心声明与目录初始化解耦 | 新空库应用 migration、启动并加载运行声明后仍无预置目录；管理员从声明选择核心创建目录。接入新核心时不追加登记数据的 migration 或自动改动目录核心集；目录改名／改默认核心／删除后，重启和组件更新均不覆盖或补回；不可用核心不静默替换。推荐模板如保留只在管理员显式操作时应用，测试 fixture 不进入生产初始化 |
| T47 | lint 从重建起点持续有效 | P0 保留并接通 Go／前端／runtime 的现有 lint、格式化、类型与结构门禁；新目录和新源码均进入检查范围，本地和 CI 失败能阻断。核对有效配置与代表性违规样例的非零结果，正常源码通过；不存在空跑、降低阈值、扩大忽略、批量抑制或延后启用，规则不强迫恢复已取消业务模型 |

## 当前实现与输入

实现采用八个领域模块、19张业务表和一张技术账本 `schema_migrations_tab`。新空库初始化及真实schema introspection核对字段、索引和职责；没有FK、CHECK、用户trigger、view或业务seed。Game运行JSON只有runtime所有权的content/cores；扫描只有短期通用进度。HTTP通过service访问persistence，生产SQL集中在persistence；Redis只保存认证限流和临时Run上下文。

当前 R11 的唯一配套输入为 `data/runtime-inputs.json`，描述 SHA256 为 `8d6c00323baf93798fe789120dcc9c388713c5c0b3852b33087cdedf219554e9`；来自 runtime 干净提交 `96e04b24c113de248ffea379856d6ede90502855`、源码摘要 `8a1277572acb8b44340246c4c4f0e44cca1f1ec359cb54ee6ec3c8929a763e0a`，明确 `release: null`。同一 V1 新增公共游戏窗口快捷键策略与事件，两 Provider 及全部 110 个 Target 指纹均改变，核心源与核心资产字节保持原样。正式 EJS/native 模块分别为 `d130aa18…` / `326acb2c…`；日常 PFB 通过标准 down/import/up 导入完整配对基座，正常 native watcher 的实际 loose 模块为 `6dc9a953…`。实证分别见 `runtime/ui-menu-bridge-r11/{handoff,paired-input-proof}.json`、`pfb-base-{down,import,up}.log` 和 `.pfb/evidence/20261007T074706Z/`。这些是 PFB 与本地交付输入，没有切换旧参考主站。

R11 的正式未过滤 `ACC-RF-BROWSER` 为 `7986e6a008824e45`，24/24 通过、耗时 439.670 秒，输入摘要 `a54b044d72a6985aaecad0e1e21853b08c5b512cc6947a713d9844b5b7734ba9`；结果在 `.artifacts/acceptance/7986e6a008824e45/cases/acc-rf-browser/`。下列 R3–R10 历史证明保留原输入和范围，不因核心字节未变就把旧指纹语义验证升级为当前通过。R11 实际界面与游戏证据见后文及 [前端验收](design/clean-refactor-acceptance.md)。

历史 R10 日常PFB通过正式 `pfb-init`、Provider import、`pfb-build` 和 `pfb-up` 启动源码Go、Next dev及正常Provider watcher。输入记录为 `backend/ons-thomson-hud-final/development-summary.json`：Go源码f109b1dc，runtime/tool同源a8c47231，工具archive4ccd8d98、安装revision5ee09e59；EJS实际module06d8bb65，native实际loose module8a836d55。固定Web候选manifest c5442a31属于独立production构建输入，不冒称日常运行standalone。

历史 R10 交付准备和镜像当时的配套描述来自runtime干净本地提交638bb648、实际源码摘要d3457eee，描述SHA005e6bbf。三个认证归档为tool3e16ea95、EJS86da972a、native13e92e7a；两个client、110Target指纹及声明执行资产与上述日常现场精确相同。该描述明确 `release:null`，不是历史正式v0.59.1。运输目录可搬移，默认准备不读取PFB路径、不回退旧发行，未来正式发行使用同一描述形状；见[配套依赖输入](dependency-management.md)。本次没有更换主站运行输入。

该历史 R10 轮次中75个EJS及ONS指纹改变，其余34个native保持；这不描述 R11 的全部110个指纹变化。旧存档按实际原core身份判断，不因bundle变化一概失效；已确认旧EJS存档CORE_CHANGED且恢复409，未变PV1000存档仍可恢复。历史语义证明只在对应Target指纹相同、原范围适用时沿用。

该 R10 标准切换保留两个数据库全部业务行及迁移账本、PG/Redis容器与挂载、Redis键名以及18,505个数据文件的路径、大小、mtime和inode；文件元数据不冒充全量字节SHA。原登录会话继续有效。ONS工作树仅登记用于后续显式core build，本次使用已验证基座中的核心字节，没有部署时重新构建核心。48次页面请求全部200；首轮编译的刷新保留，第二轮及30秒静止观察timeOrigin不变、无HMR或主文档导航。console诊断及预期已关闭Run的409保留，不宣称console零错误。完整证明见 `backend/ons-thomson-hud-final/ready-proof.json`。

## 当前证据与范围

全部47项的实际文件、SHA、覆盖状态和限制统一记录在忽略的验收目录 `backend/target-case-evidence.json`，源码职责审阅见 `backend/target-source-audit.md`。索引的coverage表示证据范围，不能把结构、API或其它素材通过解释为全平台语义通过。

| 边界 | 已确认的实际行为与证据 |
| --- | --- |
| 字段与写入 | 145项非法枚举、长度、hash、时间、跨字段、JSON/multipart检查；共享规则拒绝NUL/非法UTF8及players超过64字节，slot为required-nullable。`backend/public-images-cue-r6/http-product-proof.json`。 |
| 账号与权限 | 邀请、注册、停用、启用、删除及会话撤销；管理员跨用户存档404，旧cookie失效、私有资源保留与删除清理。`root/account-lifecycle-product.json`、`root/account-cleanup-product.json`。 |
| 审核、目录与核心 | 待审资料/媒体/统计/运行不泄漏，试玩不写最近或正式存档；批准同一Game且不改进度；目录CAS和改默认后仍以原core恢复。`root/review-access-product.json`、`root/default-core-restore-product.json`。 |
| 导入与受管内容 | Pegasus ZIP、EmulationStation 7z、中文成员；源移除后受管ROM精确可读。runtime公共裸CUE依赖发现及Host受控复制，根外symlink/父引用拒绝。`root/source-removed-product.json`、`backend/cue-discovery-r6/private/product-proof.json`。 |
| 扫描故障 | 实际取消、SIGKILL、重启interrupted、已提交游戏与成功进度一致；无法证明提交结果时明确interrupted。BIOS并发只补缺失、不覆盖手动安装。`backend/fault-windows/fault-product-proof.json`及真实PG集成测试。 |
| 收藏、标签与最近 | 删除夹保留未分类收藏；标签软删只改自身且同名新ID不复活旧关系；实际running事件才写最后游玩；平台/目录AND与最后游玩排序为SQL投影。`root/domain-product.json`、`backend/list-public-api-product.json`。 |
| BIOS | 6文件/4去重要求首次import4、重扫skip4；手动未知hash替换不做识别门禁。两Game共享必需JP文件，移除后均BIOS_MISSING，改选核心使用US；原bytes重装后恢复。缺BIOS不改Save身份/可恢复性投影，实际prepare仍拒绝，复装后正常；Game资料/version/hash均不变。`root/bios-shared-required-r10/proof.json`、`root/bios-scan-product.json`、`root/bios-replacement-product.json`。 |
| 存档并发与冻结 | 同版本覆盖200/409、同commit重试不重复；运行中换ROM仍保存原extinfo、原资源可读、下一次恢复拒绝。恢复只读一次Save投影，将原content/options及payload身份交runtime；实际Thomson/DOS文件Prepare与受控PG竞态红绿通过。`root/save-cas-rom-product.json`、`backend/ons-thomson-hud-final/final-real-prepare-proof.json`。 |
| 冻结配置与当前游戏语义 | Thomson正常TO8D游戏得分后存档，当前Game改TO7，全新浏览器恢复仍为原TO8D世界，暂停7秒图像一致，再正常输入得分继续。GAM4980实际堆积棋盘在冷浏览器不同Run恢复并继续落块。`root/interactive/theodore-played-cold-context-r10/semantic-proof.json`、`root/interactive/gam4980-cold-played-r10/semantic-proof.json`。 |
| 冷恢复HTTP | 同查询读取payload路径/hash，GET/HEAD/Range与强ETag、条件412保持正确。Handy、FreeChaF及ScummVM原存档在全新Chrome/context空IDB/cache中实际恢复并继续输入；真实UI请求If-Match为空，不能由独立条件HTTP测试推定。`root/interactive/{handy,freechaf,scummvm}-cold-etag-r9/semantic-proof.json`。 |
| 图片 | 真实Tyrano opaque payload/JPEG原字节旧400→新200，GET/Range正确MIME和SHA，设封面不改ROM hash；PNG同样支持。`backend/public-images-cue-r6/jpeg-green-proof.json`。 |
| Redis与草稿 | 独立Redis故障时持久读取有效、受保护入口503；32并发限流和TTL；GAME_SAVE丢自身Run后草稿保留、严格同身份重建与POST/PUT同步。`backend/fault-windows/fault-product-proof.json`、`frontend/lutro-context-r6-final/proof.json`。 |
| 清理 | unlink失败保留deleted和路径，恢复权限后purged；active BIOS/封面不误删；终态进度超过24小时仅删除自身记录。`root/cleanup-window.json`、`root/scan-progress-expiry-product.json`。 |
| UI与隔离项目 | 移动搜索/筛选/Recent裁切红绿、人工390/4K150图像及当前HUD普通点击/键盘焦点；隔离origin无cookie、CSP拒Host auth、未列路径404、Worker策略及视频Range；MZ所测单曲有真实decodedframes/source节点，MV/MZ实际不同Run存档恢复。`frontend/frontend-closure-index-r10.json`、`frontend/isolation-resources-r5/proof.json`、`frontend/native/mz-save-final-mobile-newgame-keyboard-r6/proof.json`。 |

R11 的独立界面验收 `root/ui-fidelity-r11-final/{report,source-manifest}.json` 覆盖17路由×3视口共51张图，全部200、无页面异常或文档横向溢出。927个文件最初与采样一致；本次文档补记前，后续7个代码文件变化仅涉及3个E2E、2个Player测试和2个Player实现，17个非Player路由布局未变。最终Player行为另由本轮24项及真实游戏审计绑定。旧版手机后台只有管理限制入口，新版手机验证卡片化、抽屉、Toast和可操作性，不宣称不存在的旧手机管理页像素一致。

J2ME《魔塔》本轮明确为 GAME_SAVE：真实输入取得红钥匙1、位置4,8，游戏内写入JD/RMS；上传失败保留原commitId草稿，重试持久化成功后才ack。cookies-only全新Chrome和不同Run使用游戏第3项“读取进度”恢复红钥匙1/位置4,8，再移动至4,9并保存。自动上传期间可立即打开退出菜单，三动作锁定，Escape不关闭或触发后台菜单；普通和沉浸菜单、M/双组合键、B松开后恢复、完整query和加载后原选中返回均通过。证据为 `root/ui-fidelity-r11-audit/j2me-semantic-proof.json`、`j2me-immersive/report.json` 及 `j2me-immersive-return/report.json`。所测魔塔的真实原生进度缺口已闭合；历史Counter-Strike素材仍只证实初始349字节设置RMS，未完成任务/五槽进度写入，不因此升级为通过。KiriKiri、OpenBOR、Play-PS2与T18/T37其余缺口保持原范围。

本轮隔离 MV/MZ/Tyrano 的普通与沉浸快捷键通过公共V1桥接；`root/ui-fidelity-r11-audit/isolated-fresh-{mv,mz,tyrano}/report.json` 还分别完成实际INSTANT存档、不同Run恢复与真实继续输入：MV map2/3,4/队伍恢复后移动至3,6；MZ map7/14,20/HP838恢复后移动至14,22；Tyrano所测scenario/index/变量恢复后真实选项令riko_f从10到15。各自清理自己的存档，不改既有用户存档。Tyrano最终单次Escape在焦点稳定后3ms内产生公共MENU；焦点重置期间抑制状态false→true→false，早期2ms首按miss保留为中间证据，不能声称已消除保护窗口或任意瞬时按键等效。旧video checkpoint问题未因此关闭；MZ首路由Fast Refresh中断同样保留原记录。

即时和原生存档语义以逐核心矩阵和独立proof为准：必须由正常输入产生可辨识的非初始世界/位置/目标/变量，真实非空payload在不同Run恢复，观察稳定，再正常输入继续。原生GAME_SAVE还需真实游戏内写入/读取。标题、HTTP200、按钮高亮、静态blocks或声明数量都不能替代。历史证明中的自然死亡/重生、首帧不完整渲染、对白位置及其它场景限制继续保留，不扩大为通用恢复结论。

## 构建与质量

历史 R10 唯一pin的默认链在867个regular文件的独立新源码副本中实际完成准备、无运输来源的离线重复、catalog/Provider核验和两张Docker镜像构建。镜像构建没有prepared-root覆盖；全新PG/Redis完成release初始化、MIT正常来源scan/approve/run、强If-Match/full/Range、匿名唯一隔离shell/SW、登录桥精确字节及standalone `retrom:8080`同库读取。公共blocklist由正常准备产生，Docker内UID1000读取的权限红绿后通过。临时四个容器和网络已清理，没有旧checkout/runtime源码挂载、业务SQLseed或主站重启。证明见 `backend/runtime-input-chain-generated-final/default-construction-proof.json`；只证明构建启动和列出的接口，不替代浏览器游戏语义或远端CI运行。

镜像源码摘要与实际Docker输入一致：仅排除带有锁定生成器标记的三个Go生成文件，Docker也明确排除同三文件并由权威契约重新生成。当前含生成文件和独立新副本不含生成文件的实测摘要一致，证明见 `backend/runtime-input-chain-generated-final/generated-boundary-digest-proof.json`；无标记的手写文件及权威契约改动仍进入检查。

归档同大小篡改、配套source错配，以及保留metadata但替换为仍能运行catalog的工具文件均被拒绝，未发布准备集合；Provider核心资产与本地integrity一起替换同样不能越过认证归档。真实负例、stable/RC跨仓样例、完整data-check及独立入口审阅见 `backend/runtime-input-chain/negative-final-proof.json`、`frontend/runtime-input-entry-audit/entry-audit-final.json`。历史显式输入镜像证明留在原目录，不冒充本次默认链。

Go、前端、runtime均保留原lint、格式、类型及结构门禁强度。历史 R10 Go完整backend-check、全包实际PG integration race及原基准d15ab6e6的工作树/未跟踪协议预检通过；savedContext/单Save投影另有竞态红绿和实际工具互通。补上integration-tag测试的lint覆盖后，发现并修正四个测试的复杂度与一处包装错误比较，原规则/阈值/断言保留，正式lint入口包含该tag。四个测试文件改变使已检查Go树摘要为aa79884f，启动来源仍f109b1dc；生产Go/API/Web与已验证Docker字节0delta，主站不为测试修正重启。原始失败、绿日志和输入证明见 `backend/ons-thomson-hud-final/final-quality-proof.json`。

R11 完整 `web-ui-check`、`web-check NEXT_DIST_DIR=.next-build`、`api-check` 通过；Web 为43个文件/173个测试，保留严格lint/type/style/结构限制与正式构建。最终E2E入口修正另经lint/type检查，完整24项重新通过，没有放宽POST/PUT、409、真实payload/恢复计数或草稿断言。runtime全量287个文件/1534个测试通过。 R11 Tag名称冲突与版本冲突分离的完整backend-check在 `frontend/ui-fidelity-r11-pages/backend-check.log`；真实PG的两项聚焦race测试在 `root/ui-fidelity-r11-final/tag-integration-final.log`，覆盖规范化重名、改名回滚、CAS、软删名称复用及并发唯一成功者，独立测试数据库自动清理。原工具路径错误的exit127只保留在 `tag-integration-tool-path-intermediate.log`，没有执行产品测试。日志见 `frontend/ui-fidelity-r11-shell/*-r11-final-v4.log`，正式浏览器结果使用前述7986e6a008824e45，不能以单项phone预检冒充完整门禁。

历史 R10 runtime与ONS已各自本地提交；当时Retrom配套pin、源码及旧实现删除归于对应交付提交，暂存前后按原始基准d15ab6e6执行协议与私密信息门禁。该历史轮次的本仓提交SHA和门禁结果写入忽略验收记录 `backend/runtime-input-chain/commit-proof.json`，不将提交自身SHA写入其文件形成循环。各仓库独立管理Git，未修改基线工程或其它PFB，未推送或发布。 本轮 R11 本地提交、协议复核、基线状态及实际证据绑定由 `root/ui-fidelity-r11-final/closure-proof.json` 单独记录；不借用旧提交证明。

## 有界规模

12逻辑CPU/125GiB RAM主机，同机运行其它PFB。独立数据库、受管目录、第二Go进程测试20,000 Game、约50,000不可变夹具硬链接路径与200用户正常会话。它是loopback/API混合负载，不是200个模拟器，不能推断正式生产容量。

历史固定production R7输入及同身份真实FCE checkpoint完成2600/2600请求，3.018842351秒，Go及4个worker峰值RSS483,430,400字节、CPU4.12秒；200个save PUT全部200、200个Run关闭全部204。p95为PUT489.76ms、run427.5ms、detail481.37ms、library294.25ms。输入和结果封存于 `root/scale/load-r7-proof.json`，不是 R10 或 R11 新指纹负载结果。

SQL先分页24条Game再投影媒体/Tag，并添加active storage引用索引、复用常驻runtime模块。同数据初测39.668秒，优化后3.292秒；EXPLAIN首屏1.176ms、offset10000页4.908ms、目录页3.872ms、active引用0.086ms，见 `root/scale/query-plans-after.txt`。

扫描另测20,000合法Pegasus候选inspect61.6ms；552,740,897字节真实MV ZIP正常1/1导入待审，30.509秒，生成1925文件/849,240,379字节。100ms采样Go及4个Node，RSS基线345,300,992、峰值583,745,536字节，CPU9.97秒，见 `root/scale/scan-memory-product.json`。候选数不冒充20,000成功入库。

## 当前限制与未完成项

- 全平台/核心剩余语义和素材/core兼容性按runtime正式矩阵逐项追踪。R11 全部110个Target指纹改变；旧存档按实际冻结core身份判断，旧语义证明保留历史范围，不直接升级为当前PASS。新的NES、魔塔和隔离快捷键证明只覆盖各自实际列出的场景。
- 历史 R10 ONS wait6核心在正常public存档、全新浏览器不同Run中恢复第三等待点与完整背景，再真实输入到第四点；633字节payload的强ETag/SHA精确，证明见 `runtime/interactive/ons-current-wait-cold-r10/semantic-proof.json`。Thomson已完成TO8D存档在当前TO7配置下的冷恢复及继续游戏。上述通过只限所测场景；ONS字体边差异与辅助脚本错误、历史失败仍保留，DOS冻结entry仅有真实工具互通，不扩大为全部游戏语义通过。
- GBC床scene/caption重绘、Tyrano旧video快照、V Rally3、SD高达暂停caption及Flash退出flush日志保留各自范围；其它稳定场景通过不代表这些问题已修复。单曲解码不代表所有音轨听感。
- 标准Next dev首次动态路由编译曾触发全局Refresh。当前预热窗口稳定不保证任意未编译路由；受影响轮次保留partial。固定production证据和日常开发证据不得混用。
- Go当前全量与风险门禁已通过，配套交付与暂存提交门禁的实际结果见相邻ignored记录。整体验收仍受上述逐核心语义缺口限制；历史失败、红绿和各轮切换留在原机器proof，不作为另一套完成标准。
