# 工程质量与测试

## 开发期契约

修改API、协议、schema或持久化布局前，先核对该功能是否已正式发布、用于生产，或存在用户明确要求保留的消费者及数据。合入master本身不能证明正式生效；未合入主线的PFB中间提交、分支镜像与验收数据也不构成正式版本。

尚未正式生效的功能直接在当前契约上调整，同步修改调用方、生成产物、测试和文档，保留已经选定的版本标识。不要为了兼容同一开发周期中的旧实现增加v2、双读写、旧版本检测、迁移链或专门拒旧的测试；验证当前目标行为即可。已正式生效的契约或用户明确要求保留的消费者，按实际影响另行评估，不能把开发期规则扩大到它们。

此规则不授权删除来源或存量数据。开发数据确需重建时仍执行当前PFB的归档/reset流程；软件发行版本、不可变tag和资产摘要继续遵守发布规则。

## 工程门禁

所有原Go lint保持启用及原阈值，Go文件1000行/测试1200行、Web文件600行/测试800行及CSS800行结构门禁继续作用于新路径。不能增加nolint/allowlist、降低复杂度阈值、扩大生成代码忽略或只保留空跑配置。

make backend-check执行数据库隔离上限、结构门禁、格式、build、普通tests和全部Go lint。depguard检查HTTP不能越过service读取持久层、基础层不反依赖业务、runtime/存档不依赖扫描，真实可编译的违规样例必须被门禁拒绝。make integration-test需要显式RETROM_TEST_DATABASE_URL，每例建独立DB迁移并销毁，不skip、不污染产品库。make api-check重生成同一authority对比TS；make web-check与runtime自身lint/type/test保留原强度。

`lint-go` 还执行 service SQL 类型边界：按实际 `persistence.Repository` 方法识别 `Count`/`Execute`，覆盖别名、方法值和方法表达式，允许命名领域操作及其他类型的同名方法。Go/Web 配置回归核对现有 lint、复杂度、行数及忽略范围；规则调整与相应回归通过普通 PR 审阅。

PG integration 在完整应用 migration 后检查应用 schema、精确业务表集合、技术账本、禁用对象和目录/核心关系空种子，并逐项注入违规对象验证拒绝。它检查最终数据库结构。`make migration-check` 离线检查已发布 SQL 摘要和连续编号；PR CI contracts 另执行 `make migration-check-released`，从 GitHub 最新已发布 stable release 的 tag 源码计算摘要，避免本地清单落后漏检。migration 的历史比对及其回归测试在 PR 阶段执行，tag 镜像流程生成当前 SQL 摘要作为发布产物记录。详见 [数据模型](data-model.md)。

PR 的 `quality` 聚合 contracts、backend、integration、web、browser-ui 和独立 `pfb_gateway`。网关使用临时 Docker 网络和容器验证分流及隔离，结束后清理自有资源；`data-check` 同时包含 PFB runtime tool、server-data、runtime origin topology 回归。master 的 required check 应设置为 GitHub Actions 的 `quality` 及 branch-image 的 `build`。镜像 tag 发布保留原有构建与部署验证，stable tag 额外要求源码已在 master，并记录 migration 摘要为 Actions artifact。

风险集成用真实PostgreSQL覆盖：Game/文件写入后失败与progress一起回滚、commit响应丢失按分配ID核对；两save同版本竞争仅一胜、同commit重试不增版本、Game删除后保存拒绝；BIOS自动安装遇并发手动安装只skip；真实文件移除失败保留deleted，active引用保护字节。纯回归覆盖可信代理链/伪造XFF、严格JSON、UUID和编码斜杠、目录有界迭代与并发消失、共享常驻runtime并发关联、单请求取消、异常退出后的恢复。

Playwright通过正常HTTP建立合法MIT NES来源、扫描、共享审核、运行、保存和新实例恢复，页面无error/溢出；保留桌面/移动/4K视口。浏览器CI必须提供同版runtime工具/Provider明确输入，不能用旧正式release冒充新契约。私有素材验收只在授权目录读取并复制，证据和素材不提交。

测试按实际风险选择，不为每个低影响CRUD机械复制实现。最终稳定源码仍需全量格式/lint/tests/schema与浏览器门禁；局部通过不能代表整体P7完成。

`make acceptance-prepare` 验证配套输入，`make acceptance-case CASE=ACC-RF-BROWSER` 与 `make web-e2e` 使用同一入口。需 `RETROM_TEST_DATABASE_URL` 作为可创建临时库的连接、`RETROM_RUNTIME_TOOL_INPUT` 和 `RETROM_PROVIDER_INPUT` 自足目录；其余依赖用 `make prepare-deps`。入口只创建／删除自己分配的 DB，Redis 有独立容器，Go／Next 和受管来源有独立目录，300 秒启动预算；准备阶段用只读 GET 预编译被测 Next 开发路由，页面交互的原有超时与断言不变。入口生成项目 MIT NES 来源，并复制项目自有 RPG Maker fixture 组成单入口／多入口来源，覆盖真实 NES 游玩／存档和 RPG 入口选择；按 `web/e2e/clean-refactor.spec.ts` 的全部用例及 Playwright 配置执行四视口，不在文档固定用例数量，无业务 SQL seed。本地入口默认执行全部视口；CI 用 matrix 为 Playwright 配置中的每个视口项目各起一个独立 job，通过 `RETROM_E2E_PROJECT` 只选一个完整项目，矩阵与配置的项目集合由工作流测试保证一致。证据额外保存逐用例耗时（`browser.log` 列表报告与 `playwright-results.json`）。仅修改 Markdown 或 `docs/` 的 PR 由 `change-scope` 判定后跳过该浏览器 job，其余门禁照常执行；`quality` 只在该判定下接受浏览器 job 为 skipped。PR 只能恢复默认分支保存的缓存，因此 `cache-warm` 工作流在每次推送 master 后按与 CI 完全相同的 key 和路径填充 Go、npm、Node 工具链、原生 PostgreSQL 工具与运行依赖缓存；两者的一致性由工作流测试校验。证据在 `.artifacts/acceptance/<id>/cases/acc-rf-browser`，失败同样保留日志，成功／失败均销毁自有资源。

scripts/verify-backend-image.sh <backend-image> <web-image>验证实际两张部署镜像。临时独立网络内以UID/GID1000创建全新PostgreSQL/Redis与合法只读MIT来源，正常HTTP初始化、扫描、批准、创建Run、读取强If-Match/Range、无cookie隔离shell/SW与实际Provider bridge，再关闭Run。Web standalone使用构建时retrom:8080 rewrite连接同一新库，检查登录HTML、健康代理、已初始化匿名账号上下文和受保护catalog的401。无旧checkout挂载或业务SQL种子；任何失败均清理仅本次自有容器/网络/来源副本。此检查证明镜像闭包与部署连接，不替代浏览器真实运行和原生恢复。

来源浏览回归覆盖未配置的绝对目录、正常目录符号链接和超过1000个子目录，扫描保留管理员认证与受管复制。正式浏览器fixture通过 `RETROM_BROWSER_SOURCE_PATH` 提供服务进程可见的绝对目录；inspect/game/bios/replace不再传来源根ID或相对选择目录。

PFB候选浏览器验收可显式设置 `RETROM_ACCEPTANCE_PFB=<当前名称>`，并将 `RETROM_RUNTIME_TOOL_INPUT`、`RETROM_PROVIDER_INPUT` 指向同一PFB已构建的不可变工具快照与已验证基座。入口校验spec、分支、工具内容摘要和完整Provider基座；仍创建独立PG数据库与Redis、经正常导入和审核运行全部视口，不改变正式镜像的配套输入门禁。

## 内容替换的异步验收

`ACC-RF-BROWSER` 的 clean-refactor 流程通过真实合法NES来源执行内容替换：提交必须为202操作响应，按精确操作ID查询终态，刷新管理详情后仍显示当前结果，确认Game版本仅递增一次、相同内容身份不变且物理文件已切换。每个规定视口保留 `content-replacement-accepted.png`、`content-replacement-completed.png` 与操作JSON；小fixture允许接收截图已处于完成态，不能伪造排队或转换状态。

大项目的补充产品验收须同时创建扫描与不同游戏的内容替换，以真实服务进度观察queued/running、取消中及terminal；记录每项操作ID、Game版本、canCancel、旧新内容身份和实际画面。截图清单使用actualScreenshots字段指向真实页面文件，至少覆盖390×844手机与2560×1440/DPR1.5，并同时完成ACC-RF-BROWSER全部四视口。发布阶段应无取消入口；失败、取消、过期和重启中断应可核对结果，不能仅检查202或标题画面。

服务测试使用独立PostgreSQL验证请求结束后继续准备、版本/CAS冲突、一次发布、COMMIT未知复核、原挂载来源不变及本次受管暂存清理；排队容量、即时取消释放、公平顺序和服务停止由确定性调度测试覆盖。组件测试覆盖接收不报成功、刷新恢复、完成才刷新资料、取消等待清理、publishing禁取消、终态停止轮询、读失败重试与过期提示。最终门禁仍包括backend-check、独立PG integration-test、web-check、api-check、web-ui-check及真实浏览器验收，聚焦测试不能替代最终门禁。
