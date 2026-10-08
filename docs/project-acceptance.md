# 当前项目验收

2026-10-08 首个 RC 发布决定：用户要求先发布 RC，停止逐 ROM 扩大验收。当前54项限定范围通过、58项当前指纹语义待核实、10项素材不足继续保留原状态；正常构建、CI、镜像及配套依赖检查仍须通过。这不代表完整兼容矩阵或稳定版验收通过。已知异常与后续核实见 runtime [#106](https://github.com/retrom-project/retrom-runtime/issues/106)、[#107](https://github.com/retrom-project/retrom-runtime/issues/107)、[#108](https://github.com/retrom-project/retrom-runtime/issues/108)、[#109](https://github.com/retrom-project/retrom-runtime/issues/109)、[#110](https://github.com/retrom-project/retrom-runtime/issues/110)。

当前稳定版工作继续在同一 PFB 中完成：测试发现的问题先记录 issue，确认的故障修复后复验，再合入 master、打不可变 tag 并发布。此前 RC 通过不等于稳定版已通过，未完成的兼容矩阵也不能改写为通过。

用户随后要求停止新增核心样例验收，收尾范围固定为已记录问题的修复、受影响回归和正式发布门禁。R20 停止时的 47 个核心／平台项（46 个核心）中，14 项有范围明确的通过证据、3 项部分完成、3 项确认故障、1 项观察未定、26 项按用户指令暂停；这不是全核心兼容性结论。后续修复回归另行记录，不改写停止时的结果。未验证项不因发布变成通过，也不在缺少故障证据时登记为缺陷。

R18 正式核心复验已通过 N64 探针／Tennis、MAME2003／Plus 的 Renegade 和 CPS2 的 19XX。这些用例使用实际已发布核心，经过新导入、审核试玩、发布、新公开存档、仅登录 Cookie 的新浏览器恢复和继续输入。KAG 正式核心 r2 已发布，R21 使用实际正式资产完成整条产品链复验；Flash 同轮恢复和退出也通过。旧版对比确认部分问题已存在于旧运行时；样例预期行为、无效报告和未验证素材单独记录，不统称重构回归。

R21 已完成 Butterscotch 姓名／菜单／位置的公开存档冷恢复、DeSmuME 的公开存档恢复及实际拒绝屏幕常亮权限时继续游玩；DeSmuME2015／melonDS 仅验证本次修复的触控映射，不提升为全核心验收通过。Butterscotch 正式核心 r2 的五项载荷与实测候选逐字节一致。原 #120 偶发内容身份错误仍未确定根因，另已修复缓存物理块丢失被错误归类的缺陷，10 项旧行为失败用例转绿，保留真实源内容校验。R22 受控产品复验自动回源并重建完整 ROM，原生触控输入正常；该证据仅覆盖实际 EJS worker，Host 工具另行通过标准流程刷新。R22-r2 的 Renegade、ColecoVision 和 SG-1000 已完成审核、发布、新公开存档、仅 Cookie 新浏览器恢复、七秒未暂停观察、继续输入和正常清理。MAME 正式核心 r3 的 94 个文件与通过验收的候选逐字节一致。这些结论及原始证据摘要统一记录在 runtime `docs/acceptance/release-readiness.json` 的 `r21IssueRegressions` 中。

Retrom `3a009a8` 的后端门禁、真实独立 PostgreSQL 集成测试 106 项（零跳过）、Web 69 个文件／281 项测试、API 生成一致性与生产构建均通过。后端实际创建并销毁 23 个临时测试库；受测源码在门禁前后保持相同。证据为 `root/stable-r16/retrom-backend-final-r18/sealed-proof.json`（SHA `e7cf7c1d0cb40631dae3ad78fd87c07448fcafe4af37589d53cf37fa8965e9c1`）及 `root/stable-r16/retrom-web-final-r19/sealed-proof.json`（SHA `76b7eb73d62792081c71f39b2e7489174f623260525cf2b93b89b09d21edeafa`）。正式 runtime pin 后的浏览器与镜像配套检查仍待执行，这些源码门禁不代替最终发行验收。

用户明确收窄 PS2 本轮标准为能加载并进入游戏；贴图等核心问题不阻塞发布，也不要求本轮证明完整比赛或存档语义。实际 Ridge Racer V 已进入原生队伍设置，方向切换颜色有效，正常操作出现 SAVE COMPLETE 并可继续 Grand Prix／引擎／变速箱菜单。旧版正式 runtime 同样出现破损贴图；本次没有修改或下调 PS2 声明的能力。证明 `root/release-r15/stable-ps2/sealed-proof.json` SHA `9ae16b8f60cb49a754f572ea7333feb9e49f5ff6d449e02b64fafff9e24df83d`，按此限定范围接受，不宣称比赛或公开存档恢复通过。

登录页恢复旧版布局后，Retrom `36c2d0a` 还修复了无效凭据的上下文提示。Web 70 个文件／286 项测试及 lint、类型、构建通过；桌面、移动与 4K 150% 三种视口以真实 HTTP 验证错误登录提示、随后成功登录及退出。该证据位于 `root/stable-r20/login-205/sealed-proof.json`；它不替代正式 runtime 固定后的完整浏览器门禁。

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

R23 最终 runtime 源码为 `c2a22dbe035b160ae60b04ab5545628d95123a63`，与 master 合并提交 `706e5cd6da7748544672a6bd6d11a0546ec44015` 的 Git tree 相同。295 个文件／1,609 项测试以及 lint、类型、构建、包、Provider 输入／构建／校验和发行聚合全部通过，两次归档逐字节一致。初次检查暴露的旧依赖断言已记录为 #123 并修正，未放宽校验。源码／聚合封存证明 `root/stable-r23/runtime-final-gates/sealed-proof.json` SHA `ba4759984ff4275af2144df13339e3426444fe1a1fd0edf764582153404f61a2`；同一提交 GitHub quality `37833154517` 通过。

本仓库正式 pin 后仍须使用真实发行输入执行四视口、全部 24 项 `ACC-RF-BROWSER`，复验受影响的既有产品案例，并校验两张实际发布镜像。最终发行回执与问题结单使用对应 Release／CI 的真实身份，独立于源码快照记录，避免源码摘要自引用。不扩展已暂停的样例集合。

## 当前实现与输入

实现采用八个领域模块、19张业务表和一张技术账本 `schema_migrations_tab`。新空库初始化及真实schema introspection核对字段、索引和职责；没有FK、CHECK、用户trigger、view或业务seed。Game运行JSON只有runtime所有权的content/cores；扫描只有短期通用进度。HTTP通过service访问persistence，生产SQL集中在persistence；Redis只保存认证限流和临时Run上下文。

当前 R13 沿用 R12 的唯一配套输入 `data/runtime-inputs.json`，描述 SHA256 为 `e5b6cead6e48c3576a15393864d4d3775727517176d35085388268c38e15fb13`；来自 runtime 干净提交 `94f4d8627ae8f267682c145245ce47935e955bff`、源码摘要 `2a8080b11683f2e4e1b708815e71e944319e66252a87af79aad60794b0f71903`，明确 `release: null`。R12 在既有 V1 内补 J2ME 数字手柄映射、BIOS-only 审核投影和 DOS 包内启动候选读取；没有新增游戏窗口契约，也没有重建核心。只有 J2ME Target 指纹变为 `4d597d22…`，其余 109 个 Target、671 个声明执行资产及核心输入保持不变。正式 EJS/native 模块分别为 `7cb7d1c0…` / `e06d16d9…`；PFB 通过标准 down/build/import/up 加载新配套输入，实际 native loose 模块 `cb5ac823…` 由各 Run envelope 单独绑定。输入和比较证据为 `runtime/j2me-input-bios-dos-r12/{handoff,paired-input-proof,tool-smoke}.json`，生命周期记录在 `root/ui-fidelity-r12-final/pfb-{down,build,import,up}.log`。工具烟雾与准备成功不替代真实游戏验收；旧参考主站未切换。

当前 R13 最终未过滤 `ACC-RF-BROWSER` 为 `f191621c92444860`：24/24通过，耗时395.962秒，输入摘要 `66ee82a0dfad8b0c6125c0d55c628833f7f6fb694bfb409daa8d821614e2fe60`。正式结果与日志位于 `.artifacts/acceptance/f191621c92444860/cases/acc-rf-browser/`，包含真实NES保存/不同Run恢复/冲突、退出前后历史导航、当前账号最近时间单行与不裁切，以及沉浸持键退出和B/方向/A返回交互。完整质量、实际页面和历史失败边界见下文R13段落。

历史 R12 最终未过滤 `ACC-RF-BROWSER` 为 `f886665edf7041ee`：24/24通过，耗时392.116秒，输入摘要 `3c312e3154250e0821804b30b29eccc354702133b6c5dd0551de878da3271060`。正式 `browser.log` / `result.json` 位于 `.artifacts/acceptance/f886665edf7041ee/cases/acc-rf-browser/`，包括真实NES存档/新实例恢复/409、画面点击关闭菜单、沉浸持键退出及返回后B/方向/A重进；手机沉浸仍验证明确尺寸门禁。PFB最终验证为 `.pfb/evidence/20261007T101856Z/`，正式验收环境与日常PFB分别保留各自输入。R13 没有改动 runtime、Provider 或核心输入；下列历史轮次保留原来的验收范围。

历史 R11 的配套输入为 `data/runtime-inputs.json`，描述 SHA256 为 `8d6c00323baf93798fe789120dcc9c388713c5c0b3852b33087cdedf219554e9`；来自 runtime 干净提交 `96e04b24c113de248ffea379856d6ede90502855`、源码摘要 `8a1277572acb8b44340246c4c4f0e44cca1f1ec359cb54ee6ec3c8929a763e0a`，明确 `release: null`。同一 V1 新增公共游戏窗口快捷键策略与事件，两 Provider 及全部 110 个 Target 指纹均改变，核心源与核心资产字节保持原样。正式 EJS/native 模块分别为 `d130aa18…` / `326acb2c…`；日常 PFB 通过标准 down/import/up 导入完整配对基座，正常 native watcher 的实际 loose 模块为 `6dc9a953…`。实证分别见 `runtime/ui-menu-bridge-r11/{handoff,paired-input-proof}.json`、`pfb-base-{down,import,up}.log` 和 `.pfb/evidence/20261007T074706Z/`。这些是 PFB 与本地交付输入，没有切换旧参考主站。

历史 R11 的正式未过滤 `ACC-RF-BROWSER` 为 `7986e6a008824e45`，24/24 通过、耗时 439.670 秒，输入摘要 `a54b044d72a6985aaecad0e1e21853b08c5b512cc6947a713d9844b5b7734ba9`；结果在 `.artifacts/acceptance/7986e6a008824e45/cases/acc-rf-browser/`。下列 R3–R10 历史证明保留原输入和范围，不因核心字节未变就把旧指纹语义验证升级为当前通过。R11 实际界面与游戏证据见后文及 [前端验收](design/clean-refactor-acceptance.md)。

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

R11 的独立界面验收 `root/ui-fidelity-r11-final/{report,source-manifest}.json` 覆盖17路由×3视口共51张图，全部200、无页面异常或文档横向溢出。927个文件最初与采样一致；本次文档补记前，后续7个代码文件变化仅涉及3个E2E、2个Player测试和2个Player实现，17个非Player路由布局未变。该轮最终Player行为另由 R11 的24项及真实游戏审计绑定。旧版手机后台只有管理限制入口，新版手机验证卡片化、抽屉、Toast和可操作性，不宣称不存在的旧手机管理页像素一致。

J2ME《魔塔》本轮明确为 GAME_SAVE：真实输入取得红钥匙1、位置4,8，游戏内写入JD/RMS；上传失败保留原commitId草稿，重试持久化成功后才ack。cookies-only全新Chrome和不同Run使用游戏第3项“读取进度”恢复红钥匙1/位置4,8，再移动至4,9并保存。自动上传期间可立即打开退出菜单，三动作锁定，Escape不关闭或触发后台菜单；普通和沉浸菜单、M/双组合键、B松开后恢复、完整query和加载后原选中返回均通过。证据为 `root/ui-fidelity-r11-audit/j2me-semantic-proof.json`、`j2me-immersive/report.json` 及 `j2me-immersive-return/report.json`。所测魔塔的真实原生进度缺口已闭合；历史Counter-Strike素材仍只证实初始349字节设置RMS，未完成任务/五槽进度写入，不因此升级为通过。KiriKiri、OpenBOR、Play-PS2与T18/T37其余缺口保持原范围。

R11 隔离 MV/MZ/Tyrano 的普通与沉浸快捷键通过公共V1桥接；`root/ui-fidelity-r11-audit/isolated-fresh-{mv,mz,tyrano}/report.json` 还分别完成实际INSTANT存档、不同Run恢复与真实继续输入：MV map2/3,4/队伍恢复后移动至3,6；MZ map7/14,20/HP838恢复后移动至14,22；Tyrano所测scenario/index/变量恢复后真实选项令riko_f从10到15。各自清理自己的存档，不改既有用户存档。Tyrano最终单次Escape在焦点稳定后3ms内产生公共MENU；焦点重置期间抑制状态false→true→false，早期2ms首按miss保留为中间证据，不能声称已消除保护窗口或任意瞬时按键等效。旧video checkpoint问题未因此关闭；MZ首路由Fast Refresh中断同样保留原记录。

即时和原生存档语义以逐核心矩阵和独立proof为准：必须由正常输入产生可辨识的非初始世界/位置/目标/变量，真实非空payload在不同Run恢复，观察稳定，再正常输入继续。原生GAME_SAVE还需真实游戏内写入/读取。标题、HTTP200、按钮高亮、静态blocks或声明数量都不能替代。历史证明中的自然死亡/重生、首帧不完整渲染、对白位置及其它场景限制继续保留，不扩大为通用恢复结论。

## R13 界面、来源与运行导航验收

本轮沿用上述 R12 配套输入，只修改 Retrom。`frontend/ui-fidelity-r13-shell/index.json` 按采样时源码、后续明确差异及截图范围归档，不把旧截图改标为最终源。首页“一直喜欢的”无封面固定居中 R；桌面/4K 有图和无图卡片保持同高，390px 仍使用原有独立手机首页，不宣称该页展示了桌面收藏轨。库卡片移除标签，实际四标签与无标签卡在三尺寸保持同高；收藏、最近和首页原本独立的卡片只核对不渲染标签，详情、筛选与编辑标签保留。标签搜索隐藏可见 label 但保留可访问名称；多选项移开鼠标后仍有持续选中背景，hover/focus 与选中有区别，取消选择恢复。真实可见滚动条下，44px 已选标签栏容纳完整 28px chip，文字与 × 的中心差为零。

审核详情移除“游戏内容”块，顶部合为封面、标题/目录与操作组成的紧凑审核栏。缺少必需 BIOS 的快审结果可展开到具体文件，详情按已保存配置重查；审核侧只显示名称/核心，不再放大小/hash。运行依赖每条 BIOS 的“文件校验要求”按核心展示已声明的大小/SHA-256/MD5，未知值不猜测，已安装事实单列。`frontend/review-bios-details-r13/final-proof.json` 记录真实 BBC 缺少 BASIC.ROM、DFS-1.2.rom、os.rom，用户游戏前后完整一致、人工批准仍可用，以及独立三项快审夹具分别发布、缺少三文件和检查失败。当前 95 个平台图均实际返回并解码；九个缺失资源恢复既有家族映射，未知/加载失败使用同尺寸手柄占位。审核旧版大小/hash 图仅为中间记录，最终布局以该 proof 为准。

来源选择直接使用服务进程可见的管理员绝对路径，从 `/` 开始浏览，旧来源根配置与下拉已移除；输入草稿不发目录请求，回车/“进入目录”才应用。来源仍只读复制，逻辑内容路径限制另行保留。`frontend/source-scans-r13/report.json` 实测三尺寸子目录/上级/草稿/错误重试；“游戏扫描”和“BIOS扫描”并列且共用按类型命名的进度列表。运行依赖页面不再有扫描入口、进度或轮询。独立空目录 BIOS 扫描为 4 项全部跳过、零安装、零失败，安装数据前后相等；它证明入口与进度，不冒充实际 BIOS 复制，后者由真实 PostgreSQL 集成测试覆盖。该终结进度作为正常记录保留，没有绕过 API 清库。

`frontend/detail-hero-r13/proof.json` 保留详情资料区原有纵向溢出及修复后五尺寸比较（1280、1440、1600、390、2560/DPR1.5）。真实长标题、四标签和有存档样本的资料区不再纵向滚动，标题最多两行并保留完整可访问文字，心形随标题，标签单行可横滚；左右封面、预览和固定顶部尺寸与修复前相同。现存零存档/零预览样本保留同尺寸空槽。物理滚轮后点击最后标签能进入相应筛选，运行核心与手机启动选项弹层实际可开关；`after-overlay.json` 另证五尺寸弹层仍以视口定位，没有落入资料区。所有样本只读、没有创建 Run 或存档。

启动与退出使用 replace，返回保持原路径、查询和 hash；审核与普通 NES 实际前进/后退不会重新进入已结束 Player，见审核 proof。运行中 Select 单键不再呼出普通菜单，仍保留核心输入；普通 Escape/鼠标触屏、沉浸游戏 M/双 Select+Start 和沉浸浏览器 S/Select 是不同入口，后两类不删除。Game.lastPlayedAtMs 从当前用户既有最近记录投影，卡片显示 MM/DD HH:mm、title 保留完整本地时间，未玩显示 —，没有增加计时/次数或数据库字段。`frontend/select-recent-r13/final-proof.json` 实测普通/沉浸 Select 均令核心输入计数递增而不呼出 UI，原双组合、松键后 B、iframe M 与退出仍有效；无刷新返回图库会重新请求并显示真实时间。早期 `final-proof-before-compact-correction.json` 的 null/time 对比都已接入 BrowserTime，不能证明与旧卡片几何一致：随后主审发现继承的24ch宽度把“最近游玩”标签挤成两行。`frontend/select-recent-r13/compact-layout-correction/final-proof.json` 随后只读复拍同一1941游戏：compact宽度按内容，桌面/4K卡高497.03125px与原plain—基线一致，标签单行19.1875px、辅助行31.1875px，时间无裁切且保留完整title。完整时间组件仍保留24ch；手机卡高271.328125px及原有隐藏辅助行不变。原516.21875px报告保留为已纠正的中间记录，用户游戏v3前后相等，独立会话正常撤销。共享横滚规则只在横向确有剩余空间时转换纵向滚轮，边界交回页面，不接管嵌套纵滚或修饰键缩放。`frontend/horizontal-wheel-r13/final-report.json` 在同一最终源的两个独立会话完成三尺寸57项采样：11项真实滚轮、43项保留原生滚动、3项当前无内容；3个小delta场景共18步通过，不能把57项全称为滚动场景。手机图库/依赖统计/收藏外轨去吸附后小幅正反滚轮有效；详细标签另用上述hero证据，已选标签另用三尺寸tag证据，运行中编辑器两处及修饰键/嵌套纵滚保护由聚焦测试覆盖，本轮没有为横滚创建Run。两个会话均零业务写入、零pageerror且logout204/401；原吸附反例及指针取点中间记录保留。`frontend/save-menu-r13/report.json` 另证桌面/手机外部点击保留目标焦点、另一卡关闭旧菜单、Escape回触发器、重命名/删除正常打开并取消，三尺寸无溢出，零存档写入，原总数308及首100条ID/名称/版本相等。

R13 当前完整后端、真实 PostgreSQL、Web/API 门禁已通过，正式未过滤浏览器 `ec609f8d03424002` 已24/24通过、耗时405.865秒，输入摘要 `4b43568788a40532005f9b8bf2c3310ef9f4c43ca05b97b3bf08401a97e66d91`。该结果属于随后手机三轨去吸附修正之前的源码；最终未过滤重验 `f191621c92444860` 在去吸附与紧凑时间修正后完整24/24通过，实际耗时与摘要以上述最终记录为准。最终结果、源码/输入/证据及原基准协议复核由 `root/ui-fidelity-r13-final/closure-proof.json` 绑定；正文不写入自身提交 SHA。各子验收只清理自己创建的夹具，最终测试会话均正常API撤销，用户原有数据不作测试清理。（唯一中间例外是wheel导航中断后丢失cookie的自有会话：协调者只读确认精确id、createdAt和测试账号后定点数据库撤销1行，证据为 `root/ui-fidelity-r13-final/wheel-orphan-session-cleanup.json`，不冒称该会话完成HTTP logout204/401。）

## R12 增量界面与交互验收

`frontend/ui-fidelity-r12-player/final-proof.json` 绑定 91 个 Player、首页、沉浸、模块标签与 E2E 文件及实际报告。普通 Player 左上仅保留返回；打开“…”后，实际同源 NES canvas 与隔离 MV/MZ iframe 的画面点击关闭菜单，切换到其它标签页不误关。Host 只观察自己持有的 iframe 元素焦点，不读取跨源 DOM 或绕过公共运行接口。沉浸返回的原问题有真实红绿：游戏 iframe 退出后文档失焦、B 无效；只在消费 Player 返回标记时聚焦新 Host shell 后，B 返回平台、方向换平台、A 进入列表和重新启动均有效。退出 A 保持400ms不穿透，随后仍需120ms中立；控制器身份与完整返回查询保留，不调用 `window.focus()` 抢系统焦点。

首页通过标准 Gamepad API 的真实按键上升沿弹出确认，触发键必须释放并中立120ms；默认取消，B取消后500ms冷却，左右选择/A进入。没有手柄时首页保持原布局，手机进入后明确显示横屏尺寸限制。长中文、无空格英文及短名称实测当前平台卡在1440视口均为480px、2560视口均为680px，名称单行省略并保留完整 title；相邻卡高度同样不随文字变化。“运行依赖”页面标题、导航、手机栏和通用计数/上传说明统一，具体 BIOS/RTP 类型与内部字段保留。`home-platforms.json`、`phone-entry-final.json`、`runtime-dependencies-final.json` 分别记录行为和1440×1000、390×844、2560×1440/DPR1.5实际界面；6个自建长名称目录已通过正常API删除并复查不存在。

MV/MZ 的游戏修改使用既有公共 `getGameEditor()`，恢复12类别、搜索/分页、队伍/人物与地图事件独立开关布局。普通和沉浸各有实际Run：金币0→1、刷新读回、经同一UI恢复0；地图选择的Escape只关子窗口，返回游戏保留暂停/沉浸菜单，最后明确退出。桌面/手机与额外4K/DPR1.5共8个Run全部通过，没有新建持久存档；这不是新的MV/MZ存档冷恢复声明。重复点击当前金币分类曾使列表清空且待响应失效，原实测及确定性红测试保留，窄修后原动作通过。每个最终Run的主文档timeOrigin稳定；页面异常为空，仍保留Next编译日志和MZ素材的Game.exe-only插件提示。早期首路由/iframe未就绪取样及缺诊断的首轮画面重置不被当作最终通过，也不臆断其原因。真正DPR1.5的图片以 `4k-final/game-editor.json` 为准，早期命名含4k150但实际DPR1的记录保留说明。

`frontend/ui-fidelity-r12-admin/final-proof.json` 验证快速审批当前筛选的31条跨两页快照：首次30条发布、1条正常API并发变更造成CAS失败而留待审，重试成功，筛选外条目未改。实际缺必需BIOS与检查错误分别保持 false/null、均未审批；仅缺可选BIOS的通过分支有聚焦/runtime测试，当前PFB该文件已安装，不冒称实站负例。推荐目录补8条、保留当时136条已有配置；推荐标签补10条、保留已有2条；第二次点击均零POST。去重包含同slug、同平台/默认核心或同平台/推荐名称，用户改过核心或禁用配置不覆盖。

DOS 启动下拉实测43个完整 `.exe/.com/.bat` 候选，三尺寸列表高258px且内部滚动；按完整嵌套路径保存并刷新仍在。相同ZIP名称换内容hash后，旧显式路径保持、警示跨刷新存在，运行请求拒绝；改选可用路径后恢复有效。显式核心菜单在换成仅一个程序的包后仍不被自动替换。有效配置对照创建Run返回200；合成包与BIOS夹具仅证明投影、配置和接口，不是实际DOS/PSX/街机游玩通过。管理文件清单移除、操作按钮右对齐也有三尺寸实拍。管理员通过正常API清理自己的38个游戏、10标签、14目录和1Run，原130目录/2标签复查未改。

新 J2ME 指纹的专项实际结果在 `root/ui-fidelity-r12-final/j2me-r12-handoff.json` 及 `j2me-input-{normal,immersive}/verification.json`。标准手柄仅按Y真实跳过游戏“1键跳过”片头；不是用键盘Digit1代替验证。魔塔仍为GAME_SAVE：游戏内红钥匙0→1后写JD/RMS，网络失败保留原commit，重试PUT成功才ACK并清草稿；仅cookies的全新Chrome/新Run通过游戏第三项“读取进度”恢复红钥匙1，再Dpad继续移动。普通退出保留原生保存提示。沉浸18项检查覆盖双组合、持B后中立恢复、实际JD保存/上传中退出互斥、完整entry返回、A保持400ms不重启、Host焦点与顶层gamepad getter保持，以及B回平台/左右切换/A进列表；该专项没有再用A启动第二Run，重进实证由NES链单独承担。首轮主文档reload原因未定，保留在 `j2me-input-immersive-reload-intermediate/`，不擅自归为HMR；末尾审计脚本重复退出的超时也与已完成真实退出区分。只关闭所测魔塔新指纹语义，不升级CS历史样本或其他核心未完成项。

## 构建与质量

历史 R10 唯一pin的默认链在867个regular文件的独立新源码副本中实际完成准备、无运输来源的离线重复、catalog/Provider核验和两张Docker镜像构建。镜像构建没有prepared-root覆盖；全新PG/Redis完成release初始化、MIT正常来源scan/approve/run、强If-Match/full/Range、匿名唯一隔离shell/SW、登录桥精确字节及standalone `retrom:8080`同库读取。公共blocklist由正常准备产生，Docker内UID1000读取的权限红绿后通过。临时四个容器和网络已清理，没有旧checkout/runtime源码挂载、业务SQLseed或主站重启。证明见 `backend/runtime-input-chain-generated-final/default-construction-proof.json`；只证明构建启动和列出的接口，不替代浏览器游戏语义或远端CI运行。

镜像源码摘要与实际Docker输入一致：仅排除带有锁定生成器标记的三个Go生成文件，Docker也明确排除同三文件并由权威契约重新生成。当前含生成文件和独立新副本不含生成文件的实测摘要一致，证明见 `backend/runtime-input-chain-generated-final/generated-boundary-digest-proof.json`；无标记的手写文件及权威契约改动仍进入检查。

归档同大小篡改、配套source错配，以及保留metadata但替换为仍能运行catalog的工具文件均被拒绝，未发布准备集合；Provider核心资产与本地integrity一起替换同样不能越过认证归档。真实负例、stable/RC跨仓样例、完整data-check及独立入口审阅见 `backend/runtime-input-chain/negative-final-proof.json`、`frontend/runtime-input-entry-audit/entry-audit-final.json`。历史显式输入镜像证明留在原目录，不冒充本次默认链。

Go、前端、runtime均保留原lint、格式、类型及结构门禁强度。历史 R10 Go完整backend-check、全包实际PG integration race及原基准d15ab6e6的工作树/未跟踪协议预检通过；savedContext/单Save投影另有竞态红绿和实际工具互通。补上integration-tag测试的lint覆盖后，发现并修正四个测试的复杂度与一处包装错误比较，原规则/阈值/断言保留，正式lint入口包含该tag。四个测试文件改变使已检查Go树摘要为aa79884f，启动来源仍f109b1dc；生产Go/API/Web与已验证Docker字节0delta，主站不为测试修正重启。原始失败、绿日志和输入证明见 `backend/ons-thomson-hud-final/final-quality-proof.json`。

R13 最终含最近游玩投影与 Select 修复的完整门禁为 `root/ui-fidelity-r13-final/{backend-check-with-recent,integration-test-with-recent,web-api-with-recent}.log`：Go lint 零问题、全量真实 PostgreSQL 集成、62 个 Web 测试文件/253 项测试、七项 UI 规则、严格 lint/type、生产构建及 API 生成一致性均通过。三条手机横轨去吸附后完整 Web 检查再次62/253通过，见 `web-check-after-snap.log`；随后 compact 时间修正的最终完整 Web 检查为62个文件/254项测试，见 `web-check-final.log`。后端/API/runtime未再改动，仍由上述对应门禁覆盖。临时 `.next-build` 生成引用已恢复日常 `.next`；with-recent 检查时701个生产输入前后摘要一致；后续去吸附和紧凑时间差异由各自新检查覆盖，最终检查/浏览器窗口的701项生产摘要再次一致，提交与证据绑定以 closure 记录为准。标准 PFB down/build/up/verify 记录在同目录 `pfb-*-with-recent.*`，没有重建镜像或变更 Provider；最终 PFB 验证为 `.pfb/evidence/20261007T130321Z/`，实际 native 模块仍为cb5ac823…。首轮浏览器 `d525622cc6f747e7` 的诊断证明新历史断言先匹配了库页 H2，详情未到达便检查启动控件；保留失败 trace 后只修 exact URL+H1 等待，不放宽操作或恢复断言，见 `first-browser-diagnostic-interruption.json` 和 `frontend/review-bios-details-r13/navigation-e2e-race-proof.json`。该重跑 ec609f8d03424002 的24项均通过，但后续实际滚轮发现手机吸附会抵消小步移动，去吸附及随后紧凑时间修正均由最终62/254 Web门禁和 f191621c92444860 的24/24浏览器检查重新覆盖。随后的 `a3872039f7794e33` 在主审发现紧凑时间仍继承24ch宽度、造成辅助行换行后精确中止；保留 `browser-compact-review-interruption.json`，不记为通过，也不把中止推作游戏核心失败。

R12 含 DOS/模块命名的完整 backend-check、真实 PostgreSQL integration-test、Web/API 检查通过；日志为 `root/ui-fidelity-r12-final/{backend-check-final,integration-test-final,web-api-check-final}.log`。随后同类别保护与手机提示选择器窄修的完整 `web-check NEXT_DIST_DIR=.next-build` 再次通过，52个测试文件/220项测试，包含严格lint/type/style与生产构建，见 `web-check-editor-fix.log`。临时构建的 `next-env.d.ts` 导入已恢复日常 `.next` 路径。runtime全量289个文件/1562项测试、构建/配套准备均通过，见 runtime R12 handoff；未放宽门禁。E2E后续只修实际canvas中心点击和菜单打开120ms中立前置，保留原POST/PUT、409、恢复、退出/持键/方向/A/B断言，另经lint/type检查。最终正式浏览器使用前述 f886665edf7041ee，24/24通过；早期失败不升级为通过。失败台账 `frontend/ui-fidelity-r12-player/defect-ledger.json` 引用再测前诊断：`a441f80144e845ca` 因新增canvas的10,10点击被可见HUD拦截而中止；`17751c1a77bc45a1` 完整21通过/3失败，trace证实第一次Right在菜单打开约30ms发出，未满足既定120ms中立，因此两次Right只移动一次。修复仅作用于测试交互前置，未改变产品门禁、按钮顺序或期望。

历史 R11 完整 `web-ui-check`、`web-check NEXT_DIST_DIR=.next-build`、`api-check` 通过；Web 为43个文件/173个测试，保留严格lint/type/style/结构限制与正式构建。最终E2E入口修正另经lint/type检查，完整24项重新通过，没有放宽POST/PUT、409、真实payload/恢复计数或草稿断言。runtime全量287个文件/1534个测试通过。 R11 Tag名称冲突与版本冲突分离的完整backend-check在 `frontend/ui-fidelity-r11-pages/backend-check.log`；真实PG的两项聚焦race测试在 `root/ui-fidelity-r11-final/tag-integration-final.log`，覆盖规范化重名、改名回滚、CAS、软删名称复用及并发唯一成功者，独立测试数据库自动清理。原工具路径错误的exit127只保留在 `tag-integration-tool-path-intermediate.log`，没有执行产品测试。日志见 `frontend/ui-fidelity-r11-shell/*-r11-final-v4.log`，正式浏览器结果使用前述7986e6a008824e45，不能以单项phone预检冒充完整门禁。

历史 R10 runtime与ONS已各自本地提交；当时Retrom配套pin、源码及旧实现删除归于对应交付提交，暂存前后按原始基准d15ab6e6执行协议与私密信息门禁。该历史轮次的本仓提交SHA和门禁结果写入忽略验收记录 `backend/runtime-input-chain/commit-proof.json`，不将提交自身SHA写入其文件形成循环。各仓库独立管理Git，未修改基线工程或其它PFB，未推送或发布。 历史 R11 本地提交、协议复核、基线状态及实际证据绑定由 `root/ui-fidelity-r11-final/closure-proof.json` 单独记录；不借用旧提交证明。

历史 R12 本地提交、原始基准协议复核、源码/输入/门禁/浏览器证据和基线状态由 `root/ui-fidelity-r12-final/closure-proof.json` 统一绑定。该记录在提交后保存实际提交SHA，正文不写入自身提交身份形成循环。最终共享测试会话已正常撤销，原会话请求管理页返回401，私有状态文件已删除；这只影响本轮测试会话。

## 有界规模

12逻辑CPU/125GiB RAM主机，同机运行其它PFB。独立数据库、受管目录、第二Go进程测试20,000 Game、约50,000不可变夹具硬链接路径与200用户正常会话。它是loopback/API混合负载，不是200个模拟器，不能推断正式生产容量。

历史固定production R7输入及同身份真实FCE checkpoint完成2600/2600请求，3.018842351秒，Go及4个worker峰值RSS483,430,400字节、CPU4.12秒；200个save PUT全部200、200个Run关闭全部204。p95为PUT489.76ms、run427.5ms、detail481.37ms、library294.25ms。输入和结果封存于 `root/scale/load-r7-proof.json`，不是 R10、R11 或 R12 新指纹负载结果。

SQL先分页24条Game再投影媒体/Tag，并添加active storage引用索引、复用常驻runtime模块。同数据初测39.668秒，优化后3.292秒；EXPLAIN首屏1.176ms、offset10000页4.908ms、目录页3.872ms、active引用0.086ms，见 `root/scale/query-plans-after.txt`。

扫描另测20,000合法Pegasus候选inspect61.6ms；552,740,897字节真实MV ZIP正常1/1导入待审，30.509秒，生成1925文件/849,240,379字节。100ms采样Go及4个Node，RSS基线345,300,992、峰值583,745,536字节，CPU9.97秒，见 `root/scale/scan-memory-product.json`。候选数不冒充20,000成功入库。

## R14 扫描导航、连续审核与父包补齐

当前 `data/runtime-inputs.json` SHA256为 `c7c46412258df6da23d7fcd503424803e76870c0cdfc32a7e595337792e49719`，runtime本地提交 `8797ffb6fc95b9b1c7f45ab54166c85c4bbe6b2d`、source tree `8ec839c052e63b4b9f433157239aee7a45d1e943099b78adbb0dc7dd85816690`；prepared pair为 `8c680b66dc6213ff6d075531b5cc775f917322fa0013c45d4a1a94bca9b6899a`。新增父包投影与合入属于host-tool；110项执行指纹保持，Provider归一化执行代码一致，仅配套资产索引元数据变化（`frontend/arcade-parent-r15/fingerprint-proof-final.json`）。实际PFB试玩仍绑定报告中的7cb7d1…模块，不冒称换成正式配套7a0a…模块。

`frontend/game-scan-navigation-r14/final-proof.json` 记录三视口实际导航：扫描POST成功进入带scanId的审核页，24份项目MIT NES素材完成后列表自动刷新；筛选与刷新保留进度但不筛选Game。连续发布只操作自建游戏，同版本下一条的实际标题输入、运行入口、BIOS请求ID切换，未保存旧资料不残留；失败/空队列/下一条查询失败由回归测试覆盖。桌面与4K管理及待审左右标题栏均按左侧原高度56px对齐基线和分隔线；手机保留原纵排媒体标题/按钮90.59375px。真实首图只抓到读取进度、随后完成24/24，运行中与未知总数等状态由确定性UI测试覆盖，不冒充实拍。

父包实际证据在 `frontend/arcade-parent-demo-r14/{upload-actual-report,play-actual-report,cleanup-report}.json`：临时双核心游戏先给第二核心上传，版本刷新后仍选第二核心并显示对应父包；随后另一核心同名替换，配置、active文件和旧文件退休事实符合契约。补齐后FBNeo真实投币、开始、画面推进并正常退出204，且document timeOrigin稳定；只证明所测1941j场景，不证明街机全库或新的存档恢复。供用户查看的原样例保持v1、pending、单child文件与缺失1941.zip；临时game/目录正常清理，最终会话logout204后受保护读取401。

最终全Web检查通过69个测试文件/281项测试、严格lint/type、七项UI规则和生产构建，见 `root/scan-navigation-r14/web-check-selected-core-final.log`；API、backend-check及真实独立PG全量/三项风险用例通过，见同目录 `api-check.log` 与 `frontend/arcade-parent-r15/backend-quality-proof.json`。runtime全量290文件/1568测试、lint/type/build/package通过。未过滤正式 `ACC-RF-BROWSER` 为 `2cf40dcf0b024cf9`，24/24通过，405.791秒，inputDigest `fb31003512632f16dda07e3002d08f869c60d7bd281809d9e8561e4cf6cc9dc0`；原结果在 `.artifacts/acceptance/2cf40dcf0b024cf9/cases/acc-rf-browser/`。标准PFB最终verify为 `.pfb/evidence/20261007T174357Z/`，原始d15ab6e6基准协议记录在 `root/scan-navigation-r14/retrom-protocol-final.json`。这些通过不升级下方尚未闭合的全项目Case。

三视口导航验收的24个自建游戏、目录及四个最终会话均已正常清理。首轮错误地要求手机纵排header也56px、对pending使用DELETE而非discard的脚本诊断保留；生产未因此改动，首轮素材随后通过正常API清理。该轮唯一丢失cookie的自有cleanup会话由协调者按准确id/userId/createdAt定点DB撤销一行，证明为 `root/scan-navigation-r14/ui-cleanup-orphan-revocation.json`，不声称它执行了HTTP logout。

## R15 正式发布准备与验收复核

README 已补全新版本部署、首次初始化、扫描入库、备份与开发说明；Compose 示例要求显式提供同一个 `RETROM_VERSION`，使用服务端和前端两张对应镜像。契约门禁与示例配置解析已通过。最新发布决定为先发布 RC，不继续逐 ROM 验证；标准构建与配套依赖门禁保留，稳定版另行评估。

按实现指纹重新审计后，不能沿用历史 R10 的通过数量作为当前发布依据。runtime 的 `docs/acceptance/release-readiness.json` 记录当前逐行状态：NES（FCEUmm、Nestopia）、SNES（bsnes、Snes9x）、MV、MZ、TyranoScript、J2ME、ONS、VecX、OpenBOR、ScummVM、WASM-4、PICO-8、TIC-80、Lutro、RPG Maker 2000／2003／XP／VX／VX Ace、mGBA／GBA、DOS、FBNeo／街机、FBA2012 CPS1／街机、Flash、Gambatte／GBC、PrBoom／Doom、Gearboy／SGB、mGBA／GBC、MAME2003／MAME2003 Plus 街机、Ardens／Arduboy、Genesis Plus GX／GX Wide／PicoDrive Mega Drive、Genesis Plus GX SG-1000／Game Gear、Mednafen PCE、Handy／Lynx、ProSystem／Atari7800 及 SuperGrafx／NGPC／WonderSwan／GAM4980／Potator Supervision ／FreeChaF Channel F ／SameDuck Mega Duck／Virtual Boy／Stella2014 Atari2600／Uzem Uzebox／O2EM Odyssey2／FreeIntv Intellivision／Gearcoleco ColecoVision 共五十四项限定场景已核对；58 项当前指纹语义与十项素材缺口仍未闭合，因此 T18／T37 仍为部分完成。

ONS 与 VecX 核心修复已分别经 PR、CI 合入维护分支并发布不可移动核心 tag。实际使用正式核心字节重新完成审核预览、运行、保存、新浏览器恢复和继续输入：ONS 恢复第三个对白等待点及背景；VecX 原始 PNG 比对证明 7,509 个迷宫像素及玩家位置均保留，此前根据预览图推断的暂停缺线并未成立。核心发布与这些限定场景不代表 Retrom 整体通过。

OpenBOR 用独立自建的两关游戏完成真实原生进度验收：引擎写入 `game.sav`，宿主同步 733 字节 gzip 包；仅保留登录态的新浏览器通过游戏自己的 Load Game 菜单读回关卡，继续方向输入可移动。此处是 GAME_SAVE，不声明即时位置或角色自动还原；未修改引擎状态或注入原生存档。《8MAN》实战尚未到存档点的记录继续保留为部分完成。实际退出暴露的 Asyncify `ExitStatus(0)` 已修复，只处理已确认成功的主动退出，异常退出仍报告；回归先红后绿，runtime 全量 290 文件／1,572 项测试及 lint/typecheck 通过，实际三次退出无浏览器异常。该修复只改变 OpenBOR 指纹。

bsnes、Snes9x、Nestopia 使用原创 MIT 测试卡带完成审核预览和发布游玩。标准手柄方向键、A 键改变真实 P1 计数；页面存档后，仅保留登录态的新浏览器运行七秒仍分别保持 20、21、18，继续手柄输入变为 29、30、26，P2 始终为零。观测只读核心原生存档块，实际保存与恢复走公开页面，未写入内存或注入核心状态；此结论限于这些卡带。首轮 RAM 读取接口不支持的脚本失败保留，不算核心故障。

ScummVM 在《Beneath a Steel Sky》中写入原生槽位并同步 13,101 字节 gzip 包，新浏览器通过游戏内 Load 恢复保存位置，七秒后仍保持，随后左摇杆／A 可继续移动。WASM-4、PICO-8 的原创卡带通过 INSTANT 恢复位置／颜色，TIC-80、Lutro 通过真实 GAME_SAVE 恢复原生进度；均覆盖审核、发布、公开保存、新浏览器恢复和继续手柄输入，保留原始 payload、截图与自建数据清理结果。Lutro 首轮脚本按错键，改用已声明的标准 B 映射后通过，没有修改产品映射或降低断言。

RGSS 复测发现工作线程注册手柄监听晚于初始连接通知，已在核心首帧后登记当前手柄。延迟监听回归先红后绿；原 25 项 adapter 测试断言保持，共享测试夹具供新启动测试使用，lint／类型及全量 291 文件／1,573 项测试通过。只改变 XP／VX／VX Ace 指纹。三者均在无需补发测试连接事件的情况下，以已连接的标准手柄完成审核、发布游玩，保存位置 11,8／变量 1；新浏览器七秒后仍保持，继续输入到 12,8。公开存档、截图、模拟手柄范围与自建数据清理结果记录在 `root/release-r15/rgss-connected-fixed/`。

runtime 草稿 PR 的干净 CI 暴露测试依赖尚未生成的 `dist`；`npm test` 现先构建宿主工具输入。清空生成物后 290 文件／1,572 项测试通过，未跳过断言。发布仍等待剩余验收与配套产物验证。

发布、构建、浏览器与定点清理记录保存在 `root/release-r15/`；新通过证据分别位于 `ons-published`、`vecx-published`、`openbor-exit-fixed`、`counter-native-current`。上述已完成场景的临时游戏、存档、目录与登录会话均已清理；私有素材、路径和会话不提交。

mGBA／GBA 使用实际《SD 高达 力量》完成审核试玩、发布游玩和标准手柄方向／确认／攻击。公开保存 29,043 字节，只有登录 cookie 的新 Chromium 七秒后仍恢复 10,000 分、HP3 和角色／敌人／场景位置；继续游玩后场景前进、分数达 33,000。原生 PAUSE 闪烁文字不作为像素恒等断言。原始 payload、截图、清理及模拟手柄范围见 `root/release-r15/mgba-sd-current/`。

DOS 在《PC 原人 2》实际第一关完成审核、方向相反的移动及 A 跳跃；原生启动需要键盘 1，标准 Start 确认难度。公开 908,596 字节存档在仅登录态的新 Chromium 恢复角色、场景、生命 2 与能量 3。七秒位置核对使用页面暂停，不宣称七秒未暂停仍位置恒等；恢复游玩后继续方向、跳跃及七秒场景均保留。早先演示模式的尝试明确排除。

RPG Maker 2000／2003 自有素材修正上层空白图块的透明色索引，避免地图和人物被遮挡；回归先红后绿，并通过确定性生成检查。两者真实审核、发布、公开保存 829／863 字节、新浏览器恢复及继续输入通过：位置 12,8、变量 1 七秒后保持，方向输入继续到 12,10；原始 PNG 解码后的完整 RGBA 帧与保存前完全相同，排除了工具预览疑似黑屏／标题缺失的误判。证据与自建对象清理见 `root/release-r15/{dos-current,easyrpg-palette-fixed,easyrpg2003-palette-fixed}/`。

FBNeo 使用授权的 1941 ZIP 完成真实审核试玩与发布游玩。最终公开快照保存非初始海岸关卡、11001分与 M-GUN 59发弹药；仅登录态的新 Chromium／不同 Run 恢复相同关卡和武器状态。七秒静态核对使用页面暂停；恢复输入后飞机向右约90px再向左返回，A射击使弹药降至57，随后七秒继续游戏。开场警告快照及 CONTINUE 中间快照明确排除。证据 `root/release-r15/fbneo-1941-current/semantic-proof.json` SHA `11b2e9b591daf3369c9241c68573fd387f9bde9e67e12c9ef7447125bc9eaea1`。

Flash 的 A 对应样例原生 Space 确认／SharedObject.flush；仅方向移动尚不写档，确认后正常公共同步保存原生位置80。全新 Chromium 只携带登录cookie，游戏读取导入的 SOL 并在七秒未暂停后保持同一可见位置；继续 Left/A 后原生位置60由公共接口保存为版本2。证据 `root/release-r15/ruffle-followup/semantic-proof.json` SHA `738a628ff01471d27ea421452af7befd2c48595a7bf09704a3863be70b254dce`。早先脚本误等已被自动同步完成的POST而退出，该中间链路不计通过；其丢失cookie的自有会话经精确ID及时间核对后定点撤销一行，不冒称HTTP退出。上述两项各自的存档、游戏、目录均正常API清理，最终测试会话logout204；用户原有待审样例未触及。

FBA2012 CPS1 独立 Target 使用同一授权1941文件完成审核、发布及恢复。最终6703字节公开快照保存2300分、三格生命和正在交战的海面位置；仅登录态的新 Chromium 在不同Run恢复，暂停七秒后继续方向／射击及七秒游玩。首次任务简报中的方向尝试不作为输入证明，另建同内容待审条目实测飞机右移约100px、左移返回及A射击；额外待审条目、目录与全部自建存档均正常API清理。证据 `root/release-r15/cps1-1941-current/semantic-proof.json` SHA `316a82eaacd90825f0baf141e20bb0f0dcf0308197280fc23f0eef7cd5120194`。

Gambatte／GBC 使用 Infinity 预览版完成实际审核输入及发布游玩，最终 10,817 字节快照保存已结束开场对话、站在桌旁的角色。仅登录态的新 Chromium 恢复同一房间位置并保持七秒，随后左右移动和原生角色菜单确认正常。早先白屏转场快照不计通过，也不据此宣称历史床上对白重绘问题均已修复。证据 `root/release-r15/gambatte-gb-current/semantic-proof.json` SHA `f8a30484c895384b190ce70e197d447a02d4a8ec109d389ae7fe4b3ea67149bd`。

PrBoom／Doom 在原生菜单明确选择新游戏、章节和难度后游玩，最终公开 7,091 字节快照保存移动后的蓝色地毯视角、100%生命及48发弹药。新浏览器冷恢复七秒后保持，继续方向输入可移动／转向，X射击使弹药降至46。早先焦点／演示模式存在歧义及过早暂停的截图均排除。证据 `root/release-r15/prboom-current/semantic-proof.json` SHA `4d26ca8a13356ddc366e5cfd27f4d4d6ba8a93c0b9fa97534c22a53700e66e8a`。两项均已正常清理自建存档、游戏、目录并退出测试会话；手柄证据使用标准 Gamepad API 模拟，不冒充实体手柄测试。

## 当前限制与未完成项

- 全平台/核心剩余语义和素材/core兼容性按runtime正式矩阵逐项追踪。历史 R11 全部110个Target指纹改变；R12仅J2ME再次改变，其余109个保持。旧存档按实际冻结core身份判断，旧语义证明保留原指纹和范围，不直接升级为当前PASS。新的NES、魔塔和隔离快捷键证明只覆盖各自实际列出的场景。
- 历史 R10 ONS wait6核心在正常public存档、全新浏览器不同Run中恢复第三等待点与完整背景，再真实输入到第四点；633字节payload的强ETag/SHA精确，证明见 `runtime/interactive/ons-current-wait-cold-r10/semantic-proof.json`。Thomson已完成TO8D存档在当前TO7配置下的冷恢复及继续游戏。上述通过只限所测场景；ONS字体边差异与辅助脚本错误、历史失败仍保留，DOS冻结entry仅有真实工具互通，不扩大为全部游戏语义通过。
- GBC床scene/caption重绘、Tyrano旧video快照、V Rally3、SD高达暂停caption及Flash退出flush日志保留各自范围；其它稳定场景通过不代表这些问题已修复。单曲解码不代表所有音轨听感。
- 标准Next dev首次动态路由编译曾触发全局Refresh。当前预热窗口稳定不保证任意未编译路由；受影响轮次保留partial。固定production证据和日常开发证据不得混用。
- Go当前全量与风险门禁已通过，配套交付与暂存提交门禁的实际结果见相邻ignored记录。整体验收仍受上述逐核心语义缺口限制；历史失败、红绿和各轮切换留在原机器proof，不作为另一套完成标准。

Gearboy／SGB 与 mGBA／GBC 使用 Tobu Tobu Girl 完成实际审核方向／确认、发布游玩、公开即时快照及仅登录态新 Chromium 恢复。Gearboy 的 27,035 字节存档恢复右侧空中角色与蝙蝠场景，公开暂停七秒后左右／加速仍有效；mGBA 的 4,070 字节最终存档恢复游戏内 RESUME／DASH COUNT ON 菜单，运行未暂停等待七秒后，A 回到原关卡并继续双向移动。初始死亡／转场快照不计通过。这是 GB 卡带通过相应产品入口的范围验收，不宣称 SGB 增强边框或 GBC 专属彩色能力已覆盖。两个 `root/release-r15/{gearboy-sgb-current,mgba-gbc-current}/semantic-proof.json` SHA 分别为 `2b5e1448b62d007450414ed362c81890bc2ea23e910e8646afaf49786260de58`、`38b790c60af664591a8db947088c6b93dfe170d7b3761e9f8784c49385d7449e`。

MAME2003 使用 1941 完成审核、发布和恢复，9,315 字节公开存档保存海岸交战场景、1000 分及三格生命。冷启动恢复后公开暂停七秒，继续左右／射击和七秒游玩均正常；不据此宣称另行复现的 Renegade 问题已修复。证据 `root/release-r15/mame2003-1941-current/semantic-proof.json` SHA `953808724ca5efbd52b05926c27dd93262b971af36f2168529078007ad97bc61`。上述三个测试均正常API删除自建存档／游戏／目录并退出账号，未触及用户待审样例。

MAME2003 Plus 的独立指纹也完成 1941 全链路：9,250 字节快照在新浏览器恢复海岸场景与两格生命，短暂覆盖画面的核心启动提示经普通方向输入关闭后，双向移动／射击和七秒继续游玩正常。首轮脚本按钮名称错误不计产品失败，Renegade 的历史异常仍单列保留。证据 `root/release-r15/mame2003plus-1941-current/semantic-proof.json` SHA `ec1b8e557b911ab5d092d1febfc3467c146eef563caac99ff168d854b2915276`；自建数据与会话均正常清理。

Ardens／Arduboy 使用作者 MicroCity1.3 完成实际审核和发布建造；公开 22,317 字节即时快照在仅登录态的新 Chromium 恢复两段道路、9980 资金，未暂停七秒后仍保持。继续 Right/B 建造第三段道路，后续公开存档与原生截图确认资金9970，日历与光标动画正常推进。证据 `root/release-r15/ardens-microcity-current/semantic-proof.json` SHA `855768ff1cfea0c3c23c8fb652fda8ea7ed7f8a2ad19ae12585ae0ce3159a16d`，自建数据及会话正常清理。

Genesis Plus GX 与 GX Wide 均用 Metal Sonic Hyperdrive 完成审核方向／确认／跳跃及发布游玩，公开 37,269／35,998 字节即时快照在新 Chromium 恢复灌木左侧地面位置、金环布局和三条生命，未暂停七秒后仍保持；继续左右移动和跳跃正常，计时与待机动画自然推进。初始菜单快照及抢先读取未出现canvas的脚本错误不计通过。`root/release-r15/{genesis-megadrive-current,genesis-wide-current}/semantic-proof.json` SHA 为 `96eda39e4a74a31c0aaf7caef46557ecdc4953f5becff0f2e3b1dccca81500b3`、`0fe0f0133362b0c0c61ab5ae3d66d8bafebf436b02475d418759a3b79be4b611`，自建数据及会话均正常清理。

PicoDrive／Mega Drive 同样完成 Metal Sonic Hyperdrive 实际审核与发布移动／跳跃；公开 34,684 字节即时快照在新 Chromium 保留角色、关卡布局和三条生命，未暂停七秒后继续左右移动和跳跃正常。`root/release-r15/picodrive-megadrive-current/semantic-proof.json` SHA `0a591ea5ea33f1c22fbba8f43e9456430eed11a434bb28a1c765e3d05f57bffb`。

Genesis Plus GX 的 SG-1000／Game Gear 绑定分别完成 Bomb Jack 和 Columns 实际游玩。Bomb Jack 恢复350分、角色位置及已收集炸弹布局，继续方向输入后达到370／390分；Columns 恢复两侧已堆积宝石，继续方向、旋转、落块后增加第三组。两者均经公开即时存档（8,600／9,209字节）、仅登录态的新 Chromium、公开暂停七秒观察后继续操作；不宣称 Bomb Jack 特定跳跃映射或动画整帧相同。`root/release-r15/{sg1000-congo-current,gamegear-columns-current}/semantic-proof.json` SHA 分别为 `5941b4fb40a622684ab4b95d78e2519d1de4a26a1a91c643f381c56eb5ae0da8`、`18e36162d77950b4add0dc697a0bbb468cb4736819d2ef14d5ffa434db33a4a8`。三项自建数据及会话均正常清理。

Mednafen／PCE 与 Handy／Lynx 分别用 PC Bonk、Basketbrawl 完成实际审核和发布方向／动作，公开 21,180／38,110 字节即时存档在仅登录态的新 Chromium 恢复滚动场景／角色／三颗心，或 Level1-1 球场／角色／计时；公开暂停七秒后继续方向和动作正常。不宣称通关或投篮得分。`root/release-r15/{pce-bonk-current,handy-basketbrawl-current}/semantic-proof.json` SHA 分别为 `c79c585a8f91be5b33726d15dc27d4901d069e690c91930a763cf601551b21bd`、`bba70af60fe8c30554ab3f837f6675f78bd3dfd7d6800496972ec795248976c7`。

ProSystem／Atari7800 的作者游戏 Dungeon Stalker 通过方向选择原生 Start Game、标准手柄 B 确认，实际审核和发布游玩均可离开牢笼。公开 1,586 字节即时快照在新 Chromium 恢复上方通道角色和五条生命，公开暂停七秒后继续左右移动正常；此前演示／未确认菜单不计通过。`root/release-r15/prosystem-dungeon-current/semantic-proof-reviewed.json` SHA `c58245d9e5b9064b1d01a99b6b58422dcf00c49a80cafeb34da0408fa2fc50ac` 显式绑定成功的第二个审核 Run，保留初始证明供追溯。三项自建数据及会话均正常清理。

Mednafen／SuperGrafx《Aldynes》完成真实审核试玩与发布飞行。公开 38,535 字节即时快照在仅登录态的新 Chromium 恢复飞机和场景，公开暂停七秒后继续上下移动有效；不声称击中敌人或得分。证据 `root/release-r15/supergrafx-aldynes-current/semantic-proof.json` SHA `43e121a96a4356e8e4545ad35183a8e389362fe80b0c491d38f7d93aa3cb1a27`。

Mednafen／NGPC《Gears of Fate》通过原生菜单方向／B 确认和第一关实际棋盘旋转。公开 3,985 字节快照在新 Chromium 运行七秒后保留旋转后的红绿位置及蓝色路径，继续 B／A 可再次旋转；装饰齿轮正常动画。不将本次证据描述为历史第二关通关验收。证据 `root/release-r15/ngpc-gears-current/semantic-proof.json` SHA `6cba9ec7da0397d5744e1a28e082f05828e6875ea45d7d49bf0709a3b8f45edd`。两项自建数据和会话均正常清理。

WonderSwan 使用作者公开的 WonderCell v0.2 验证了方向／A 拾取与放牌。公开 4,977 字节快照在仅登录态的新 Chromium 运行七秒后恢复红桃3所在的第一空位及随机牌局，随后可将红桃3移至第二空位。原 Swan Driving 素材画面静止但原生帧数增长的现象保留为未确认问题，不据此宣称修复输入缺陷或全部 ROM 兼容。证据 `root/release-r15/wswan-wondercell-current/semantic-proof.json` SHA `bd15ee7e8f9ca2b6b52f3d91413611aa4bf829a02edfca50521a2bccd83297c2`；自建数据及会话均清理。

GAM4980 本轮用五子棋完成方向与 A 确认、真实棋局保存及仅 cookies 的全新浏览器恢复：12,786 字节 INSTANT 存档保留第一手黑白两子和步数0001，运行七秒不重置；继续方向/A 后变成两黑两白、步数0002，电脑正常应手。Eros 另有实际堆叠恢复与继续移动/下落证据。五子棋证明 `root/release-r15/gam4980-gomoku-current/semantic-proof.json` SHA `8367c709b6e316addee4318d459fbe40809734cba8dfe61d48d7840e93ef0053`；两例均正常清理自有数据和会话。

Potator／Supervision 使用作者 XSnake 实际手柄 A 启动、方向转弯，保存吃到苹果后的20分／剩余11苹果／右侧蛇身。880字节公共存档在全新仅cookies浏览器恢复，公开暂停观察七秒后，Up／Left 继续沿顶部移动。初次637字节关卡标题存档不计通过。证明 `root/release-r15/potator-xsnake-current/semantic-proof.json` SHA `24c0c1f703c44d9f3756b9b2e7e91682cdefaeacd9ec0a35f7409d54f8bb3131`；自有数据与会话已清理。

FreeChaF／Channel F 的作者 QUEST 通过方向移动与 A 战斗确认；1,972 字节公共存档在仅cookies新浏览器恢复非初始位置、随机敌人场景和血量，公开暂停七秒后可继续碰撞战斗、攻击并返回地图。初期错误控制器选择及不存在的只读截图诊断方法不计通过；实际发布和恢复实例只用标准手柄输入。证明 `root/release-r15/freechaf-quest-current/semantic-proof.json` SHA `9436f17dcdf2ec555d31b72d2cf779c4299a852b9b07aafc68c33dc9140c68f0`；自有数据和会话均清理。

SameDuck／Mega Duck 的 Max Pirate 通过 Start／双向移动，正式游玩进入新的尖刺敌人房间。10,350字节公共存档在仅cookies新浏览器恢复房间、右侧角色及8血，公开暂停七秒后继续左右移动；敌人碰撞导致正常掉血。第一轮不暂停恢复的掉血记录保留，不据此断言血量恢复异常；没有返回上一房间或 A 攻击结论。证明 `root/release-r15/sameduck-maxpirate-current/semantic-proof.json` SHA `567a968418ff163d2e85d128e00ce23d2522ce29f4ca320b7373a74a8a271540`；自有数据与会话已清理。

Virtual Boy 的固定时间模拟 A／Start 会在正常比赛及恢复后擅自打开暂停菜单，现已移除该核心启动按键。新开局／恢复两项回归先红后绿；runtime lint、类型、291文件／1,574测试与包检查通过。VB Racing 经实际审核和发布方向／Start确认／A加速；公开46,154字节即时快照在仅登录态新浏览器恢复原生暂停、00:07:48、剩余158、速度104及路旁布局，运行未暂停观察30秒保持不变，随后手动Start和左右／A继续驾驶。早先标题转场快照不计通过；自建记录和会话均清理。证据 `root/release-r15/virtualboy-racing-fixed/semantic-proof.json` SHA `f9edd62c6ef60d2c2183b132a9c9086460fe54412ce9035f149cd27163c5fa78`；仅Virtual Boy指纹变更，不改核心二进制或协议代际。T18/T37及正式发布仍未完成。

Stella2014／Atari2600 用作者 Sheep It Up 验证B确认及方向跳跃。323字节公开即时存档在仅登录态新Chromium恢复低处云边的空中角色；公开暂停七秒后继续下落，再次手柄操作可跳起。初始地面存档不计通过，不宣称已得分爬升或像素完全一致。证据 `root/release-r15/stella-sheep-current/semantic-proof.json` SHA `5cebe734e6b80523528c8c69fef8bab108330c757bc2b2ca802dbc80bf096296`；自有数据和会话清理完成。

Virtual Boy 修复提交 `c5fe4b4065e68f3fe950fb80b612dabfc1504a7b` 已通过远端 quality `37708642739`。同一干净提交的完整配套构建／Provider检查通过，描述SHA `1df353efa487d47e351427549948556c83070a7777363f2157ff54667a8504ac`，明确 `release:null`。EJS模块 `bdce5ed8…` 与已测试修正字节相同，native模块 `4733851a…` 保持不变；全部110目标／122产品行匹配当前指纹。标准down、导入、build、up、verify保留原PFB数据／ID／URL，native开发覆盖恢复；证据 `.pfb/evidence/20261008T003944Z/` 及 `root/release-r15/virtualboy-{artifact-identity,paired-loaded-identities}.json`。此为本地验收基座，未替换正式production pin或创建稳定tag。

Uzem／Uzebox Arkanoid 已完成审核／发布方向挡板与A发球；35,689字节公开快照在仅登录态新浏览器恢复右下绿砖缺口、非居中挡板、球及掉落道具，公开暂停七秒后继续操控。正常丢球／下一条生命保留旧砖块缺口，继续左右与A可打掉另一块砖。过场快照排除，不宣称通关或球位置像素恒等；证据 `root/release-r15/uzem-arkanoid-current/semantic-proof.json` SHA `b8f82f525e3d7184199f3b1c91473864478812fd3b9fa4a0b84eb48e2961786c`，自有数据／会话清理通过。

O2EM／Odyssey2 作者 Bird Hunt 在正常模拟器设置中交换手柄端口后，审核与发布试玩均可方向移动准星、A开火。311字节公开即时存档在仅登录态新Chromium恢复已移动准星、四发子弹和飞鸟场景，公开暂停七秒后再选择相同手柄端口，继续左右移动与开火使弹药降为三发。不宣称核心原生偏好自动存入extinfo或击中得分；直接键盘输入保留默认启用。早期未交换端口的标题尝试及被拒绝的配置请求不计通过。证据 `root/release-r15/o2em-birdhunt-current/semantic-proof.json` SHA `dfe4aaf3ed4595948f20e9a3b255d19d77551261ab0ce2bf9457488885041961` 绑定成功的第二次审核Run，自建数据及会话已清理。

FreeIntv／Intellivision 4-TRIS 完成审核A启动／旋转与左右移动，发布游戏形成440分的彩色落块堆叠。23,963字节公开即时存档在仅登录态新Chromium恢复棋盘／分数／绿色下落块，公开暂停七秒后继续左右操作并自然落块。核心前端自带PAUSED覆盖层不随机器快照保留，未将它计作恢复要求或通过证据；最终使用公开暂停观察。不宣称消行或最终受碰撞限制的旋转成功。证据 `root/release-r15/freeintv-4tris-current/semantic-proof.json` SHA `4df66a6d6a83f217c29d9eb43a1525080c0688b978362fa3cb52e62eb1961bc6`，自有数据及会话已清理。

Gearcoleco／ColecoVision 使用现有240p Test Suite测试ROM验证方向／A进入原生菜单和Grid校准功能。35,449字节公开即时存档在仅登录态新Chromium恢复红色边框、白色网格和点阵，运行七秒保持；B返回后继续上下选择，A打开另一Monoscope图案。限定为测试ROM的活动状态恢复，不宣称商业游戏关卡通过；早期黑屏校准快照及转场不计证据。证明 `root/release-r15/gearcoleco-suite-current/semantic-proof.json` SHA `f1e56ded9fc3177e7dec9dc619e43f8ffa335c23157c0495293e418cb0dbb278`；自有数据及会话已清理。

## 首个 RC 的配套发行输入

runtime `v0.60.0-rc.1` 已通过 quality `37712099498` 与 release `37712529719`（第二次运行；首次仅上传 HTTP408 超时），标签提交为 `88ec0ad5bed4a56bfbc1620ea36b9a622d865a84`。Retrom 经标准 pin-release / prepare 下载、校验并固定完整的已发布工具包和两个 Provider；源码树摘要为 `036368516f5346a575b1c34e49b497ae370f8fef494d0fb640b5ce96b9b95466`。实际发行包的110个Target指纹与122行验收台账全部一致；没有将待核实行提升为通过。已知问题见 runtime #106–#110。Retrom PR 继续对该实际发行输入执行完整 CI、浏览器产品链和镜像检查。
