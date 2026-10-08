# 工程质量与测试

所有原Go lint保持启用及原阈值，Go文件1000行/测试1200行、Web文件600行/测试800行及CSS800行结构门禁继续作用于新路径。不能增加nolint/allowlist、降低复杂度阈值、扩大生成代码忽略或只保留空跑配置。

make backend-check执行数据库隔离上限、结构门禁、格式、build、普通tests和全部Go lint。depguard检查HTTP不能越过service读取持久层、基础层不反依赖业务、runtime/存档不依赖扫描，真实可编译的违规样例必须被门禁拒绝。make integration-test需要显式RETROM_TEST_DATABASE_URL，每例建独立DB迁移并销毁，不skip、不污染产品库。make api-check重生成同一authority对比TS；make web-check与runtime自身lint/type/test保留原强度。

风险集成用真实PostgreSQL覆盖：Game/文件写入后失败与progress一起回滚、commit响应丢失按分配ID核对；两save同版本竞争仅一胜、同commit重试不增版本、Game删除后保存拒绝；BIOS自动安装遇并发手动安装只skip；真实文件移除失败保留deleted，active引用保护字节。纯回归覆盖可信代理链/伪造XFF、严格JSON、UUID和编码斜杠、目录有界迭代与并发消失、常驻runtime复用和取消重建。

Playwright通过正常HTTP建立合法MIT NES来源、扫描、共享审核、运行、保存和新实例恢复，页面无error/溢出；保留桌面/移动/4K视口。浏览器CI必须提供同版runtime工具/Provider明确输入，不能用旧正式release冒充新契约。私有素材验收只在授权目录读取并复制，证据和素材不提交。

测试按实际风险选择，不为每个低影响CRUD机械复制实现。最终稳定源码仍需全量格式/lint/tests/schema与浏览器门禁；局部通过不能代表整体P7完成。

make acceptance-prepare验证配套输入，make acceptance-case CASE=ACC-RF-BROWSER与make web-e2e使用同一新入口。需RETROM_TEST_DATABASE_URL作为可创建临时库的连接、RETROM_RUNTIME_TOOL_INPUT和RETROM_PROVIDER_INPUT自足目录；其余依赖用make prepare-deps。入口只创建/删除自己分配的DB，Redis有独立容器，Go/Next和受管来源有独立目录，300秒启动预算。使用项目MIT builder生成唯一NES来源，经正常UI扫描审核后执行clean-refactor.spec.ts全部四视口；无业务SQL seed。证据在.artifacts/acceptance/<id>/cases/acc-rf-browser，失败同样保留日志，成功/失败均销毁自有资源。

scripts/verify-backend-image.sh <backend-image> <web-image>验证实际两张部署镜像。临时独立网络内以UID/GID1000创建全新PostgreSQL/Redis与合法只读MIT来源，正常HTTP初始化、扫描、批准、创建Run、读取强If-Match/Range、无cookie隔离shell/SW与实际Provider bridge，再关闭Run。Web standalone使用构建时retrom:8080 rewrite连接同一新库，检查登录HTML、健康代理、已初始化匿名账号上下文和受保护catalog的401。无旧checkout挂载或业务SQL种子；任何失败均清理仅本次自有容器/网络/来源副本。此检查证明镜像闭包与部署连接，不替代浏览器真实运行和原生恢复。

来源浏览回归覆盖未配置的绝对目录、正常目录符号链接和超过1000个子目录，扫描保留管理员认证与受管复制。正式浏览器fixture通过 `RETROM_BROWSER_SOURCE_PATH` 提供服务进程可见的绝对目录；inspect/game/bios/replace不再传来源根ID或相对选择目录。
