# 后端、API 与运行维护

| 属性 | 内容 |
| --- | --- |
| 状态 | 已审定 / 一期实施基线 |
| 版本 | 1.7 |
| 日期 | 2026-08-25 |
| 适用范围 | Retrom 一期 |
| 技术栈 | Go、SQLite、Next.js、EmulatorJS、RetromRpgRuntime、本地内容寻址存储、OCI/Docker 镜像 |

本文定义 Retrom 的部署边界、Go 模块划分、API 约定、后台任务、安全与运维要求。领域细节由对应专题文档负责，本文不重复其状态机和数据字典。

## 1. 架构结论

一期后端采用 Go 模块化单体，单个 `retrom` 进程提供 JSON API、后台 Worker、EmulatorJS/RetromRpgRuntime 运行资源和受控内容端点；前端由独立的 `retrom-web` Next.js 进程提供 UI 与 Player Shell。生产环境在二者之前放置已有的 NG（Nginx/网关/反向代理），由 NG 暴露应用 HTTPS origin，并为每个 MV/MZ Launch 暴露一个从固定模板派生、永不复用的 unique HTTPS runtime origin；所有 TLS 仍只在 NG 终结。SQLite 保存业务数据与任务状态，用户文件写入本地 SHA-256 CAS。前后端分镜像是构建与部署边界，不把后端领域拆成微服务，也不引入 Redis、消息队列或 S3。

~~~mermaid
flowchart LR
    Chrome["Chrome 桌面端"] -->|HTTPS| NG["前置 NG / TLS 终结"]
    NG -->|HTTP：页面与 _next| Web["retrom-web / Next.js"]
    NG -->|HTTP：API / content / runtime| Go["retrom / Go 模块化单体"]
    Chrome -->|同源 WSS| NG
    Go --> API["HTTP API / runtime content"]
    Go --> Worker["进程内 Worker"]
    Go --> DB["SQLite"]
    Go --> CAS["本地 CAS"]
    Worker --> DB
    Worker --> CAS
    Worker -->|哈希元信息查询| Hasheous["Hasheous API"]
~~~

选择该形态是为了让浏览器始终看到同一 HTTPS origin，同时保持后端自托管、备份和故障恢复简单。Go 与 Next.js 应用只处理 HTTP；证书生命周期和 TLS 策略完全留在 NG。

## 2. 进程与模块边界

Go module 路径一期固定为 `retrom`，HTTP server 使用标准库 `net/http`；不得另引入 Web framework 或 ORM。目录布局固定为：

```text
cmd/retrom/               进程入口、配置和优雅关闭
internal/httpapi/         路由、中间件、DTO、错误映射
internal/catalog/         Platform、PlatformInstance、Game、GameVariant
internal/importing/       导入任务、分组、刮削与审核编排
internal/emulationstationmeta/ 严格 EmulationStation XML 解析与规范化；不读环境/数据库/CAS
internal/emulationstationimport/ EmulationStation 扫描、映射快照、执行与普通审核交接
internal/metadata/        Hasheous 适配器与缓存
internal/arcadedat/       DAT 安装、解析、依赖图与诊断
internal/bios/            BIOS 要求、安装和状态聚合
internal/runtime/         启动预检、LaunchSession/capability、EJS 配置
internal/rpgmaker/        RPG 项目识别、路由/构件绑定、派生 fileset、pack 匹配、运行验证、隔离与 checkpoint 领域逻辑
internal/rpgmaker/runtimevalidation/ RPG 运行验证 gate、状态投影与恢复协议
internal/rpgmaker/isolation/ unique-origin capability 与 Host 隔离服务
internal/netplay/         Room/Session 控制面、严格实时协议与有界内存 Hub
internal/saves/           状态存档、截图与兼容性
internal/playtime/        PlaySession 和有效时长
internal/blobstore/       CAS 写入、读取、引用与垃圾回收
internal/jobs/            SQLite 队列、租约、重试和 Worker
internal/store/           SQLite 连接、迁移和事务辅助
internal/observability/   结构化日志、健康检查和诊断导出
internal/httpapi/generated/ OpenAPI 编译期生成的 strict server types；禁止手改且不提交 Git
migrations/               Go package：embed.go 与有序 SQL migration，编译进后端
api/openapi.yaml          OpenAPI 3.0.3 协议事实源入口
api/domains/              领域 route、request/response 与所属 schema
api/components/           跨领域公共 component
api/codegen/              固定 Go models/server/spec 生成配置
web/                      Next.js + React + Tailwind CSS
data/dat/                 小型依赖 manifest/SHA；大 payload 由 prepare-deps 物化
data/netplay/v2/          联机 core profile schema 与精确 artifact allowlist manifest
```

依赖方向遵循 `httpapi/jobs -> application modules -> store/blobstore`。HTTP handler 不直接拼 SQL，DAT 解析器不写游戏元信息，Hasheous 适配器不判断 Arcade 可运行性。

前端按能力分区：

```text
web/app/                  Next.js 路由与页面壳
web/proxy.ts              HTML 每请求 nonce/CSP 与跨源隔离响应头
web/features/library/     游戏库和游戏详情
web/features/player/      持久 Player Shell、预检、runtime factory、EmulatorJS 与 RetromRpgRuntime adapter
web/features/netplay/     房间、游戏选择、座位与实时状态 UI
web/features/saves/       存档列表及快速启动
web/features/admin/       导入、审核、游戏、游戏目录、BIOS/DAT
web/lib/api/              类型化 API client 与错误映射
web/components/           无业务状态的通用组件
```

## 3. HTTP 与数据约定

- API 前缀统一为 `/api/v1`，响应使用 JSON；二进制上传、下载和运行时文件端点除外。
- 业务实体 ID 在 JSON 中使用字符串，避免前端数值精度问题；唯一例外是 EmulatorJS 要求 number 类型的 `emulatorGameId` surrogate，范围被限制在 JavaScript 安全整数内，不能把普通实体 ID 数值化。
- 数据库的时刻字段统一为 Unix 毫秒 `INTEGER`；API 使用 camelCase 的 `*AtMs` 并在 OpenAPI 标为 `int64`。完整规则见 [存储与数据库](./storage-and-database.md)。
- 所有写请求接受并返回明确的资源版本；存在并发编辑风险的管理资源使用 `version` 或 `If-Match`，冲突返回 `409`。
- 列表统一使用游标分页，响应包含 `items` 与可空 `nextCursor`。筛选条件进入 query string，刷新后可恢复。
- 错误响应使用稳定机器码，而不是让前端解析中文文案。

错误外形：

```json
{
  "error": {
    "code": "LAUNCH_BIOS_MISSING",
    "message": "缺少启动所需 BIOS",
    "details": { "filenames": ["neogeo.zip"] },
    "requestId": "req_..."
  }
}
```

状态码、认证/CSRF、幂等、分页、上传及全部 route 的唯一协议见 [HTTP API、上传与启动凭据契约](./http-api-contract.md)。后端以固定 `oapi-codegen` 的 strict `net/http` 接口实现以 `api/openapi.yaml` 为入口的领域文件集；构建先生成只含内部引用的统一 bundle，再按 models/server/spec 三个职责生成同一个 Go package。Go 文件由标准后端 build/test/lint/integration/dev 和镜像构建在编译前按需生成、被 Git 忽略且不得提交；前端由同一 bundle 生成须提交的单一 TypeScript schema 并用类型化 fetch client。`make api-check` 在临时目录验证 bundle 与两端生成结果，并拒绝 TypeScript 漂移或任一 Go 生成物被跟踪。不能维护另一组手写路径、DTO 或状态码。

`strict-server` 主要约束 handler/response type，并不自动完成全部请求验证。正式 handler 外层固定使用 `github.com/oapi-codegen/nethttp-middleware v1.2.0` 与其锁定的 `github.com/getkin/kin-openapi v0.142.0` 加载同一 OpenAPI 3.0.3，验证 path/query/header/body schema；所有固定 object schema 必须 `additionalProperties:false`。在它之前的 JSON lexical middleware 对 `application/json` body 施加 route 上限（全局最高 16 MiB），先 `utf8.Valid`，再用 token stack 拒绝重复 object key、depth >64、多个顶层值和尾随非空白，最后恢复 body 给 validator/generated binder。query middleware 根据匹配 operation 的参数集合拒绝未知名、标量重复值与非法 percent encoding。

两个大 body operation `putAdminUploadPart`、`postRuntimeSaveState` 在 OpenAPI 标记 `x-retrom-streaming-body: true`。启动时基于同一已加载 spec 构建两条不可变 validator chain：普通链保持 `Options.Options.ExcludeRequestBody=false`，流式链设置 `Options.Options.ExcludeRequestBody=true`；前置 kin-openapi router 匹配 operation extension 后分派，不能在并发请求之间修改共享 options。流式链只跳过 schema body 读取，仍验证 method/path/query/必需 headers/content type；随后由 generated strict request 的 `io.Reader`/`multipart.Reader` 交给领域 handler 按 HTTP 专题流式限额、part、digest 和临时文件规则处理。其他 operation 不得设置该 extension。不能按 URL 字符串另维护一份 skip 清单，也不能用全局 `Skipper` 跳过整条验证。SSE/GET 无 request body，不经过 JSON scanner。出站响应由 `httptest.ResponseRecorder` contract test 逐 operation 用同一 schema 验证；生产不为此重复 buffer 大二进制响应。

## 4. API 能力地图

本节只描述能力归属。实际方法、路径、body、状态码和缓存策略全部以 [HTTP API 契约第 9 节](./http-api-contract.md#9-核心-api-路由表) 与以 `api/openapi.yaml` 为入口的 OpenAPI 文件集为准；两者不一致时实现任务必须先修正文档/OpenAPI，不能兼容两套路径。

- 用户读取：home、game library/detail、save list。
- 用户写入：创建 LaunchSession、heartbeat/finish、手动通用 checkpoint，以及从 checkpoint 创建新 restore Launch。
- 联机用户：列出精确支持的游戏、创建/查看房间、选座/准备/开始/结束；房间状态使用 SSE，运行输入和 state transfer 使用同源 WebSocket。
- 管理写入：upload、import、受信服务器 BIOS/Pegasus/EmulationStation scan、review（含 RPG 版本核心/资源包/绑定 revision）、RPG runtime validation/判定、game revision、platform instance、BIOS installation、Arcade DAT installation 和 RPG pack installation。
- 管理读取：入库总览/任务/SSE、服务器扫描计划与映射、待审核/历史、游戏管理、BIOS/DAT/RPG 运行依赖、RPG validation gates、审计事件和脱敏诊断摘要。

详情页和存档快速启动都调用同一 `POST /api/v1/launches`；区别只在是否携带 `saveStateId`。所有普通 API 必须先完成账户认证，管理 API 还要求 `ADMIN`；所有已认证写请求同时执行 Origin、Fetch Metadata、CSRF、乐观并发与幂等校验。浏览器目录上传只传相对路径；服务器扫描只接受已配置 capability 的 root ID 与规范相对路径，不提供任意宿主路径入口。

## 5. 内容端点与 LaunchSession capability

浏览器不得获得宿主机路径、Blob ID/hash 或能力秘密。`POST /api/v1/launches` 返回可记录的 UUIDv7
`launchId`，同时通过 `retrom_launch_<launchId>` HttpOnly cookie 下发 32-byte capability；数据库只保存其
SHA-256。Player URL 固定为 `/play/:launchId`；config、状态和事件保留在 `/runtime/launches/:launchId/**`，
ROM/BIOS/parent/外部盘片使用不含 launch ID 的 `/runtime/content/**`，并由相同 capability 派生的
`/runtime/content/` 路径限定 HttpOnly grant 授权。capability 不进入 URL、Referer、JSON 或访问日志。

MV/MZ 项目文件绝不从应用 origin 或上述 `/runtime/content/**` 公开。Player 只会将 sandbox iframe 导航到本 Launch 的 unique runtime origin；该 Host 只接受 HTTP 契约登记的 `/__retrom/*` allowlist。`GET /__retrom/bootstrap` 是唯一无凭据 GET，且仍要校验精确 Host、Launch 存在且未过期；它只返回固定 bootstrap，不返回游戏代码/状态。ticket 只在 request body 中由同源 `POST /__retrom/bootstrap` 一次消费，并换取 host-only、HttpOnly、`Path=/__retrom/` 的 capability cookie；其余 entry/bridge/project/restore/cleanup 端点全部要求该 cookie。项目 entry 注入的 `<base>` 固定为同源 `/__retrom/project/`，CSP 必须包含 `base-uri 'self'`。runtime Host 不接受普通 app session/API/页面 fallback，不设 Domain cookie，不在不同 Launch 间复用 origin。

### 5.1 为什么 MV/MZ 必须使用每 Launch 独立子域名

MV/MZ 的游戏包会携带项目自己的 HTML、JavaScript、插件和资源。即使这些文件来自合法游戏，它们也不是 Retrom 的受信应用代码；导入期静态分析不能证明所有动态插件、反射代码和运行分支都安全。浏览器的安全边界首先是 origin，而不是 URL path、iframe 组件名或服务端目录。因此 Retrom 为每次 Launch 分配形如 `https://{launchId}.rpg-runtime.<site-domain>` 的全新 origin，并且永不把该 origin 分配给另一 Launch。sandbox、CSP、MessageChannel、一次性 ticket 和短期 capability 是该 origin 隔离之上的纵深防御，不能替代它。

如果把游戏放到应用 origin 的 `/runtime/...` 路径，浏览器会把游戏脚本视为与 Retrom 前端同一主体。只要任一项目或插件存在恶意行为、供应链污染或普通 XSS，脚本就可能读取或修改应用 DOM、调用同源 `/api/v1`、借用户身份发起写请求、读取非 `HttpOnly` 的应用数据，并注册覆盖应用 scope 的 Service Worker、Cache Storage 或其他持久状态。仅使用 iframe sandbox 也不足以把这些风险降为同一等级：MV/MZ 需要执行脚本并使用自身 origin 的存储能力，错误增加 sandbox 权限、浏览器差异或后续维护回归都可能重新打开同源权限。若所有游戏再共用一个 runtime origin，一个游戏留下的 localStorage、IndexedDB、Cache、Service Worker 或命名资源还可能污染下一款游戏或下一次 Launch，造成跨游戏数据泄漏、错误恢复和持久化攻击。

独立子域名把最坏影响限制在一条有时限的 Launch capability 内：游戏拿不到 app host-only session cookie，浏览器同源策略阻止其访问父页面和应用 API；每次 Launch 的存储、缓存和网络身份互不复用；退出时可以对该 origin 清理站点数据，异常退出则由唯一 origin 永不复用和 capability 到期兜底。这个机制并不表示上传内容“可信”或允许执行本机 `.exe/.dll/.node`；它只允许经导入规则认可的 Web 项目文件在受限浏览器环境中运行。

部署验证必须同时证明正向和反向边界：合法 Launch 的 exact host 只能访问 `/__retrom/*`；runtime host 的 `/`、`/api/v1/*`、Next 页面和静态 fallback 必须返回 404；不存在或过期 Launch 的 bootstrap 必须失败；应用 host 不得提供 MV/MZ 项目脚本。若 wildcard DNS、证书、Host 保留或这些负向路由任一项未成立，MV/MZ native route 必须视为不可用，不能退回应用 origin 继续运行。

固定 EmulatorJS 与发布媒体可公开 immutable 缓存；媒体替换必须分配新 Asset URL。ROM、parent、BIOS 与
多盘外部文件以带领域分隔的派生内容身份形成新 `/runtime/content/` URL，在有效 Launch content grant 下使用
`private, max-age=31536000, immutable`，替换任一输入都必须改变 URL。状态存档、存档截图和 Launch config
继续 `private, no-store`，不得进入共享或 immutable cache。允许路径、cookie scope/过期、单 Range、ETag、
MIME 和错误隐藏的唯一契约见 [HTTP API 第 7–8 节](./http-api-contract.md#7-launch-创建与凭据)。Go 静态
handler 只能发布依赖 manifest allowlist，不能把物理目录直接挂为文件服务器。

## 6. 后台任务

任务至少覆盖：Upload 终结组装与 Blob 哈希落库、Import 安全扫描/分组与逐 Item pipeline、Pegasus/EmulationStation scan 与 review handoff、Archive 检查、DAT 解析/索引、Arcade 依赖识别、Hasheous 查询与图片获取、严格 READY 快速审批、游戏内容 revision/兼容重校验、业务 payload 引用释放和 Blob 宽限回收。`internal/payloadrelease` 统一执行 ImportItem/ImportJob/PegasusItem/EmulationStationItem/UploadConsumption/Game ownership 释放、provider TTL 和 BLOB_GC；领域终态只创建持久 Job，不自行删 CAS。精确 Job kind/scope 映射以数据模型为准，不另起同义名称。联机不增加 Job kind；Room 到期由 30 秒维护 ticker 执行短事务，frame/input/hash/state/reconnect 只存在于有界 Hub 内存。

SQLite 队列表和 worker 必须实现 [数据模型第 7 节](./data-model.md#7-通用任务事件与审计) 的字段、领取索引、60 秒 lease、15 秒 heartbeat、并发上限和四次 attempt 退避。领取任务必须在短事务内完成，租约到期后可恢复；任务处理必须幂等。网络任务尊重上游 `Retry-After`，但等待上限 15 分钟。

每个 execution 还使用数据模型固定的 kind wall deadline；第一次领取时计算，自动 retry 不重置，人工 retry 才开始新 execution/deadline。Worker 的 reader、网络和解析 context 必须来自该 deadline，超时产生稳定错误而不是无限 RUNNING。测试通过 fake clock/context 触发，不使用长时间 sleep。

运行中取消不是立即宣告完成：API 将 Job 置为 `CANCEL_REQUESTED`，当前 worker 在数据模型规定的有界检查点停止并清理 scratch 后才置 `CANCELLED`；lease 已过期时恢复器只能确认取消。所有成果提交都比较 state、lease token 和 cancel flag，防止旧 worker 在取消/重新领取后写入。UI/SSE 必须区分“正在取消”和“已取消”。

不要把长时间哈希、网络请求、DAT 解析或归档扫描放在持有数据库写锁的事务中。先执行可重入计算，再用短事务提交结果和状态转换。

快速审批在创建时冻结最多 10,000 个严格 READY Item，Worker 顺序领取并逐项调用唯一 Approve 服务；整个批次不得持有一个长写事务。EmulationStation `hidden/adult` 来源项在预览中计入 `sourceFlagged` 并排除候选。每项成功的发布对象、ReviewEvent、普通与对应服务器来源聚合和批次结果共用同一事务及 state/worker fence。取消在 Item 边界检查，重启只把未提交 RUNNING Item 恢复为 PENDING；restore 不继续旧批次。只有 worker 基础设施故障允许快速审批领域 retry，业务 skip/final failure 不通过 retry 复活。

导入任务及审核语义见 [导入、刮削与审核](./import-and-review.md)。

## 7. 构建、镜像、本地开发与 TLS 边界

### 7.1 镜像与 Makefile 契约

仓库只构建两个应用镜像：

| 镜像 | Dockerfile / context | 责任 | 默认内部 HTTP 端口 |
| --- | --- | --- | --- |
| `retrom` | 根 `Dockerfile` / 仓库根目录 | Go API、Worker、迁移、DAT、固定 EmulatorJS/RetromRpgRuntime payload 和受控内容端点 | `8080` |
| `retrom-web` | `web/Dockerfile` / `web/` | Next.js production/standalone UI 与 Player Shell | `3000` |

根 Makefile 必须提供：

| Target | 精确行为 |
| --- | --- |
| `make build-backend-image` | 执行 `data-check`，计算/前后复核发布输入 digest，执行一次 Docker build，默认产出 `retrom:latest` 并带规定 label |
| `make build-web-image` | 先执行离线 `data-check` 保证依赖 manifest 与 Player adapter registry 对齐，计算/前后复核同一 digest，再执行一次 Docker build，默认产出 `retrom-web:latest` 并带规定 label |
| `make build-images` | 固定一份发布输入 digest，依次调用上述两个 target，最后 inspect 并比较两个 label；只有两个都等于预期值才成功 |

变量契约为 `DOCKER ?= docker`、`BACKEND_IMAGE ?= retrom`、`WEB_IMAGE ?= retrom-web`、`IMAGE_TAG ?= latest`。调用者可以显式覆盖镜像仓库前缀或 tag，但默认名称不得变化。

这些 target 的终点是“镜像存在于本地 image store”。它们不得：

- 调用 `docker run`、Docker Compose 或其他服务编排；
- 创建容器、网络或 volume；
- 登录 registry、push 镜像或部署服务；
- 启停本地开发进程；
- 读取或打包用户 ROM、BIOS、SQLite、CAS、测试截图或 TLS 私钥。

两个 Dockerfile 都使用多阶段构建，最终层不保留编译工具、源码缓存或开发依赖。两个镜像都不创建 Retrom 专用账号，也不声明固定 `USER`；运行身份由 Compose/Kubernetes 等部署编排显式决定，生产基线为 UID/GID `1000:1000`。后端持久数据目录必须挂载为该身份可写，镜像不得尝试 chown 未知宿主 UID。后端 dependency builder 必须读取构建参数中 `RETROM_DEPENDENCY_VERSIONS` 对应的小型 manifest 集，按固定来源逐版本物化/校验 EmulatorJS、core、可选 DAT 与许可输入；当前默认 `4.2.3,4.3.0-pre`，后者提供 DOSBox Pure、Genesis Plus GX Wide 与 Azahar 定向覆盖。完整 release 只能作为 builder 输入，随后从 manifest 导出新的只读 allowlist 目录供最终层复制；不能把下载 archive、非 allowlist core、本地依赖缓存、source checkout 或整个 `data/` 目录复制进镜像，也不能用最终层递归 `chmod` 重写整棵依赖树。前端镜像携带经过 `data-check` 的 adapter registry/实现；两个镜像必须携带完全相同的 release-input label。最终镜像中的内置依赖层必须对任意非 root 运行 UID 保持只读且可遍历，不能继承物化阶段仅 root 可访问的 `0700/0600` 权限；部署以 UID/GID `1000:1000` 运行时仍须通过同一依赖校验。

`make build-images` 不自动属于普通 `make ci`，但修改任一 Dockerfile、依赖锁文件、构建脚本、DAT/runtime 打包逻辑或发布资产时必须在合并前同时验证二者。tag 发布流水线不重复 PR quality，只保留 `make build-images` 及后续发布门禁。

### 7.2 GitHub Actions 与 Docker Hub 发布

`.github/workflows/ci.yml` 在所有 pull request 上运行唯一高层质量入口 `make ci`。CI 使用 `go.mod`、`.node-version`、`web/package-lock.json` 与依赖 manifest 的固定版本和缓存；全新 runner 在测试前先执行幂等 `make prepare-deps`，缓存命中也必须重新逐字节校验 runtime/core/DAT/许可 payload。它不另行拼装测试子集，不依赖用户 ROM/BIOS、真实 Hasheous 或开发机浏览器；仓库自有公开 GBA、NES、SNES 与 Arcade 测试程序作为普通 checkout 输入由 `data-check` 验证，但不进入发布镜像。

`.github/workflows/docker-image.yml` 在任意 Git tag push 时触发。tag 发布直接构建并校验双镜像，不重复执行 PR 的 `make ci`，也不等待 GitHub Environment 人工批准，并按下列顺序执行：

1. 要求 Git tag 本身符合 Docker tag 语法，不对包含 `/` 等非法字符的 tag 做可能碰撞的静默改写；
2. 通过 `make build-images BACKEND_IMAGE=xxxsen/retrom WEB_IMAGE=xxxsen/retrom-web IMAGE_TAG=<git-tag>` 构建并复核两个镜像的 release-input label；
3. 仅在两个镜像都成功后，使用 `DOCKER_USER` 与 `DOCKER_PASSWORD` GitHub secret 登录 Docker Hub，其中 `DOCKER_PASSWORD` 必须保存具备目标仓库 push 权限的访问令牌而不是账户明文密码；
4. 推送 `xxxsen/retrom:<git-tag>` 与 `xxxsen/retrom-web:<git-tag>`；不含 `-` 的稳定 tag 同时更新两个 `latest`，预发布 tag 不移动 `latest`。

Action 负责 registry 登录和 push，不改变 Make target 的本地构建边界。GitHub 仓库只需配置 `DOCKER_USER` 与 `DOCKER_PASSWORD` repository secrets；凭据不得写入 workflow、镜像或日志。创建并推送发布 tag 即授权流水线自动发布，维护者必须在创建 tag 前自行确认[依赖管理第 6 节](./dependency-management.md#6-升级与许可门禁)的第三方分发义务已经满足。

### 7.3 `make dev` 只运行本地进程

`make dev` 是宿主机开发入口，不是容器入口，也不得依赖 Docker daemon。它先执行幂等 `make prepare-deps` 与锁文件驱动的 `make web-install`，成功后以前台 supervisor 方式同时启动：

1. `go run ./cmd/retrom --mode=test`，默认监听 `127.0.0.1:8080`；启动器只用 `RETROM_MODE` 选择并转换 CLI 参数，显式以 `RETROM_NETPLAY_ENABLED=true` 打开测试联机入口，随后在执行 Go 前移除工具变量；
2. `cd web && npm run dev`，固定使用 Next 的 `--webpack` 开发 bundler，默认监听所有 IPv4 接口 `0.0.0.0:3000`，可用 `NEXT_DEV_HOST` 显式收窄；
3. Next.js dev rewrite 将应用 origin 的 `/api/`、`/content/`、`/runtime/` 和 `/health/` 转发到本地 Go 端口；标准 `https://dev.sendev.cc` 开发实例的 RPG native iframe 不经此 rewrite，而由前置 NG 将 `https://{launchId}.rpg-runtime.dev.sendev.cc/__retrom/*` 转发到同一 Go listener。仅当操作者把应用 origin 一并改为解析到 loopback 的明文 `http://retrom-app.rpg.localhost:<web-port>` 测试 origin 时，才可显式改用直连 Go 的 `http://{launchId}.rpg.localhost:<backend-port>`；两者必须同属 `rpg.localhost` site，才能让浏览器在 entry 请求携带 `SameSite=Strict` runtime capability。`localhost`、`127.0.0.1` 与 `*.rpg.localhost` 不满足该约束。HTTPS 页面不得加载明文 runtime iframe。开发服务不规范化 Host，也不把远程请求重定向到 localhost。

当前标准 `dev.sendev.cc` 的 NG 运行在容器、Go 运行在宿主机，因此被忽略的 `.dev-data/dev.mk` 显式设置 `RETROM_HTTP_ADDR=0.0.0.0:8080`，同时让 Next 的内部 `NEXT_BACKEND_ORIGIN` 保持 `http://127.0.0.1:8080`；NG runtime vhost 使用宿主名 `local.sendev.cc:8080` 作为 upstream。不得把某个 Docker bridge 地址写成 Go listener 或长期 upstream：bridge 重建不应改变应用配置。该覆盖意味着 8080 可能在宿主网络可达，操作者必须用主机防火墙/受信网络限制它；生产部署仍按第 7.4 节使用编排内服务名，不照搬这个开发 upstream。

脚本必须转发 `SIGINT/SIGTERM`、在任一子进程异常退出时停止另一进程并返回非零状态，退出后不得残留后台进程。每次启动还必须在仓库 `.dev-data/dev-state/dev.pid` 中原子登记 supervisor、Go 与 Next.js 三者的 PID 和 Linux process start ticks；子进程另以独立 process group/session 启动。隔离验收脚本通过 `RETROM_DEV_STATE_DIR` 把同样的登记与接管锁放入本次临时目录，防止测试实例接管日常开发实例。正常接管先用 supervisor 的 PID/start ticks、工作目录和命令行确认身份，再发送 `SIGTERM` 并等待最多 15 秒；若 supervisor 已被 `SIGKILL` 等方式终止，新实例必须分别以登记的子进程 PID/start ticks、process group/session、工作目录和完整启动命令确认遗留 Go/Next.js 身份，只有两者各自通过确认后才向对应精确 process group 发送 `SIGTERM` 并等待数据锁释放。旧版仅登记 supervisor 的两字段文件继续支持正常接管，但不能据此猜测或扫描孤儿子进程。陈旧 PID、PID 复用、伪造登记或其他工作目录的同名进程不得被终止；登记无法证明身份但数据根仍被锁定时，新实例必须在启动子进程前明确失败，不得把错误推迟成后端 `DATA_ROOT_LOCKED`，也不得按端口或进程名批量杀进程。无法在期限内退出时同样失败。启动接管以状态目录中的 `dev-takeover.lock` 串行化，登记文件由 owner 在退出时清理。

`make dev` 不构建镜像、不启动容器、不创建容器网络；本地开发数据库、Blob 和密钥统一写入被 Git 忽略的 `.dev-data/data`，进程登记与接管锁统一写入 `.dev-data/dev-state`。可编辑启动配置集中在同样被忽略的 `.dev-data/dev.mk`，Makefile 在内置默认值前可选加载该文件；其中可覆盖监听、公开 origin、数据/状态目录、依赖版本和功能开关，命令行 Make 变量仍具有最高优先级，隔离验收无需读取日常配置。它使用显式 test 模式，空库创建 `test/test`；不会自动读取或迁移旧 `.cache/retrom` 数据。测试服务器基线的浏览器 origin 为 `https://dev.sendev.cc`，前端固定监听 `0.0.0.0:3000`；后端的仓库内置安全默认仍为回环监听，而标准 `dev.sendev.cc` 实例按上文的被忽略配置显式监听 `0.0.0.0:8080`。调用者可显式覆盖 origin 和监听运行隔离的本地开发实例。仅 test 模式且 insecure flag=true 时允许明文 origin；release 无条件要求 HTTPS。线程核心仍受 Chrome 安全上下文限制。前端的幂等 UUID 与上传/存档 SHA-256 在缺少 `crypto.randomUUID`/`crypto.subtle` 时仍使用受测的 Web Crypto 兼容 fallback；安全随机数始终来自 `crypto.getRandomValues`。

开发拓扑仍只有一个标准 Go 进程和一个标准 `next dev` 进程。`scripts/dev.sh` 只给 Next 子进程预加载仓库内的 upgrade hook；该 hook 仅匹配精确的 `/runtime/netplay/rooms/{roomId}/socket` 路径，把 method、Origin、Cookie、Fetch Metadata、Upgrade 与 `Sec-WebSocket-Protocol` 原样转发到 `NEXT_BACKEND_ORIGIN`，并逐字节桥接升级后的 socket。其他 upgrade（包括 HMR）继续由 Next 自己处理，普通 HTTP 仍走既有 rewrite。验收必须证明未认证的合法联机 upgrade 经前端端口到达 Go 并返回 `401 AUTHENTICATION_REQUIRED`，而不是由 Next 返回自己的 403；生产不加载此开发 hook，仍由上一节 NG 路由负责。

未显式设置 `RETROM_SERVER_IMPORT_ROOTS` 时，`make dev` 在真正启动进程前幂等创建两个被 Git 忽略的仓库目录：`.dev-data/bios` 作为 ID `local-bios`、标签“本地 BIOS”的默认只读扫描 root，`.dev-data/roms` 作为 ID `local-roms`、标签“本地 ROM”的默认只读扫描 root。前者用于放置开发 BIOS，后者用于放置含 `metadata.pegasus.txt` 或 `gamelist.xml`、ROM 与媒体的 Pegasus/EmulationStation 测试目录；其中内容均不得提交。调用者可以显式提供 JSON 数组整体替换这两个默认值，也可以传 `[]` 关闭本地扫描；`scripts/dev.sh --stop` 不创建目录。两个扫描目录与 `.dev-data/data`、`.dev-data/dev-state` 相互隔离，都不属于依赖物化目录或镜像输入。

同一 root 配置同时服务 BIOS、Pegasus 与 EmulationStation 导入。客户端只能提交 `rootId` 与相对路径；后端逐段无跟随打开并拒绝 symlink、special file、路径穿越、根替换和扫描中的来源漂移。三类任务共用全局 2 个内容读取槽，避免各自达到上限后叠加压满磁盘；数据库写事务只提交已完成的有界结果，不覆盖文件读取、XML/哈希、媒体探测或归档扫描。

### 7.4 TLS 只在 NG 终结

生产拓扑中，浏览器只连接 NG 终结的 HTTPS；`retrom-web:3000` 和 `retrom:8080` 只接受来自受信网络的明文 HTTP。Retrom 不提供证书/私钥配置、不监听 HTTPS、不申请证书、不执行 HTTP→HTTPS 跳转，也不管理 HSTS。对应职责全部属于前置 NG，且本项目的镜像构建 target 不构建或启动 NG。

固定同源路由：

| 外部路径 | NG 上游 |
| --- | --- |
| `/_next/*` 及其余页面路由 | `retrom-web:3000` |
| `/api/v1/*` | `retrom:8080` |
| `/content/*`、`/runtime/*` | `retrom:8080` |
| `/health/*` | `retrom:8080`，通常只开放给内部健康检查 |

NG 还必须为 `https://{launchId}.rpg-runtime.<configured-site-domain>` 配置 wildcard DNS/证书与精确 Host 转发，且只把该 Host 的 `/__retrom/*` 送到 `retrom:8080`；不匹配规范 UUID 最左 label、额外 label、Host/Forwarded Host 不一致或其他路径必须在 NG 或 Go 稳定拒绝，不得 fallback 到 Next.js/app API。Player 页面 CSP 的 `frame-src` 只加入本次 Launch 精确 origin，不使用 wildcard 或回显请求 Origin。该子域名不是普通部署别名，而是第 5.1 节定义的浏览器安全边界；缺少它时不得启用 MV/MZ native route。

前端只使用相对 URL，不把内部容器名、端口或环境域名编译进浏览器 bundle。若 Next.js server-side 代码确需访问后端，使用运行时内部 base URL，与浏览器公开 base URL 分离。

TLS 终结外置不等于忽略代理安全：

- NG 必须为页面与运行时资源保留/设置一致的 COOP、COEP、CORP 和 `nosniff` 头，保证 `window.crossOriginIsolated`；这些头不是 TLS 功能。
- NG 的上传大小、buffering 和 timeout 必须允许大 ROM 流式上传；后端仍独立执行大小、归档和路径安全校验。
- `dev.sendev.cc` 的 NG 根 `location /` 和 Next.js 全局 rewrite 代理层将传输天花板固定为 `283115520` bytes（270 MiB），read/send/backend timeout 不低于 300 秒；不得为 `/api/v1/admin/imports` 或 save-state 再建特殊 NG `location`。这只防止大 checkpoint 被代理截断，不改变任何其他 endpoint 的应用层 body 上限、授权或超时契约；Go 只对 `POST /runtime/launches/{launchId}/save-states` 接受该 multipart 总上限并保留 300 秒 route deadline，超限请求必须在读完 body 前失败。
- `/runtime/netplay/rooms/*/socket` 必须保留 `Upgrade`、`Connection` 与 `Sec-WebSocket-Protocol`，关闭代理响应缓冲并允许最长 8 小时连接；应用仍独立执行同源 Origin、Fetch Metadata、AuthSession、room cookie、消息大小和 heartbeat 校验。
- 应用只信任显式配置的代理地址和转发头；客户端不能通过伪造 `X-Forwarded-*` 绕过 origin、日志或限流逻辑。
- 对外公开基址应显式配置为 NG 的 HTTPS origin，应用不根据内部明文连接猜测外部 scheme。
- 本机开发可以使用浏览器认可的 `http://localhost` 安全上下文，但仍要通过 dev rewrite/响应头满足跨源隔离。

## 8. 运行配置与目录

所有运行时可变内容放在一个明确的数据根目录；代码仓库中的 `data/` 只保存小型 manifest、验证代码及被忽略的本地缓存：

```text
RETROM_DATA_DIR/
  retrom.lock
  retrom.db
  blobs/sha256/ab/cd/<full-sha256>
  secrets/launch-capability.key
  secrets/netplay-capability.key
  tmp/uploads/
  tmp/jobs/
  backups/
```

一期环境变量契约固定如下；配置在启动时一次读取并校验，业务代码不直接读环境：

| 变量 | 开发默认 / 生产规则 |
| --- | --- |
| `RETROM_HTTP_ADDR` | 仓库安全默认为 `127.0.0.1:8080`；标准 `dev.sendev.cc` 实例由被忽略的 `.dev-data/dev.mk` 显式覆盖为 `0.0.0.0:8080`，使容器中的 NG 能通过 `local.sendev.cc:8080` 到达；容器化应用部署也显式设为 `0.0.0.0:8080`。所有情形都只监听明文 HTTP，不接受 HTTPS 值。 |
| `RETROM_PUBLIC_ORIGIN` | 当前仓库 `make dev` 测试服务器基线为 `https://dev.sendev.cc`，隔离的本地实例可显式覆盖；它是 Origin 精确比较和 Invitation/PasswordReset URL 的唯一公开基址，不从 Host/X-Forwarded-Host 推导。生产必填且必须是无 userinfo/path/query/fragment/trailing slash 的单个 `https` origin。 |
| `RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN` | 服务默认 `false`；只有 CLI `--mode=test` 且值为 true 时允许明文 origin。release 即使误设 true 也拒绝 HTTP。 |
| `RETROM_DEV_CONFIG` | Makefile 可选加载的本地配置文件，默认为被忽略的 `.dev-data/dev.mk`；文件不存在时使用仓库内置默认值，命令行变量可覆盖文件值。生产入口不读取它。 |
| `RETROM_DEV_STATE_DIR` | 仅供开发启动器使用；`make dev` 默认为仓库 `.dev-data/dev-state`，保存 PID 登记与接管锁。隔离验收必须覆盖为本 Case 的临时目录。 |
| `RETROM_DATA_DIR` | 必须是已解析绝对路径；开发由 Makefile 设为仓库 `.dev-data/data`，生产为全新持久卷。它与只读 `RETROM_DEPENDENCY_ROOT` 及开发扫描目录严格分离；应用创建子目录但拒绝文件系统根、用户 home 和 symlink 数据根。 |
| `RETROM_DB_PATH` | 未设置时派生为数据根下 `retrom.db`；若设置必须是数据根内的绝对普通文件路径。 |
| `RETROM_DEPENDENCY_ROOT` | 必填绝对只读目录；其下按 `dat/emulatorjs/<version>`、`runtime/emulatorjs/<version>` 与 `runtime/rpgmaker/v1/` 布局；RPG manifest 的 artifact `entry_path` 相对该 v1 物化根解析，不以 route/artifact ID 猜目录。开发固定为仓库 `data/` 的绝对路径，镜像内固定为只读依赖层；拒绝 root/home/symlink 逃逸。 |
| `RETROM_DEPENDENCY_VERSIONS` | 必填、无空白/重复且按 SemVer（含 prerelease）升序；当前为 `4.2.3,4.3.0-pre`。每项必须有完整 manifest/runtime/许可 payload，DAT 只在该 manifest 声明时必需。 |
| `RETROM_ACTIVE_EMULATORJS_VERSION` | 必填且必须属于上列；当前为 `4.2.3`。新验证逐 core 使用版本列表中最后一个声明该 core 的 artifact，不覆盖历史 revision 锁定版本。 |
| `RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE` | Go 与 Next 两个进程都必填且值相同，只含一个 `{launchId}`，无 userinfo/path/query/fragment/trailing slash。release 形式固定为 `https://{launchId}.rpg-runtime.<configured-site-domain>`，test 只可在显式允许下使用 `http://{launchId}.rpg.localhost:<backend-port>`。它必须与 `RETROM_PUBLIC_ORIGIN` 不同；`launchId` 是规范小写 UUID 且占完整最左 Host label，静态 suffix/端口不得从请求推导或覆盖。Next 只在精确 `/play/{launchId}` 响应的 `frame-src` 中加入由该模板计算的同一 Launch exact origin，其他页面继续只允许 `'self'`。 |
| `RETROM_MULTI_DISC_IMPORT_ENABLED` | 严格 `true|false`；服务配置缺省为 `false`，仓库 `make dev` 的测试服务器基线显式传入 `true`；控制新建多盘 Import、capability 投影和多盘内容替换。非法值启动失败，生产启用必须显式设为 `true`。 |
| `RETROM_SERVER_IMPORT_ROOTS` | 服务配置缺省为 `[]`；仓库 `make dev` 在变量完全未设置时注入 `.dev-data/bios` 与 `.dev-data/roms` 对应的两项 JSON 数组，显式值（包括 `[]`）优先。生产只能显式配置已挂载的只读目录。 |
| `RETROM_NETPLAY_ENABLED` | 严格 `true|false`，服务默认 `false`；`make dev` 测试基线为 `true`。关闭时不注册联机 API/runtime route且认证上下文令前端隐藏入口，不删除历史表。 |
| `RETROM_NETPLAY_MAX_ACTIVE_ROOMS` | 默认 `16`，封闭范围 `1..128`；只限制 `DRAFT/WAITING/STARTING/RUNNING` 房间，新建超限返回 429，不驱逐既有房间。 |
| `RETROM_NETPLAY_ROOM_IDLE_DRAFT_MS` | 固定 `900000`（15 分钟）；若显式提供其他值则启动失败。 |
| `RETROM_NETPLAY_ROOM_IDLE_WAITING_MS` | 固定 `1800000`（30 分钟）；若显式提供其他值则启动失败。 |
| `RETROM_NETPLAY_RECONNECT_LEASE_MS` | 固定 `10000`（10 秒）；若显式提供其他值则启动失败。 |
| `RETROM_TRUSTED_PROXIES` | 逗号分隔 CIDR；默认空。生产必须精确列出 NG 网段，不能使用 `0.0.0.0/0` 或 `::/0`。 |
| `RETROM_STARTUP_CHECK_TIMEOUT` | 默认 `60s`，范围 `10s..5m`；只约束配置、依赖字节、数据库/migration 与 bootstrap Job 登记等同步预检，不包含后台 `DAT_PARSE` execution。 |
| `RETROM_LOG_LEVEL` | `debug/info/warn/error`，默认 `info`；生产禁止记录内容秘密。 |

`RETROM_DATA_DIR/secrets/launch-capability.key` 与 `netplay-capability.key` 均没有环境变量覆盖，两者使用独立 HMAC domain，不能互换。首次启动分别用系统 CSPRNG 生成 32 bytes，在同目录新建唯一 `0600` 临时文件、完整写入并 fsync；随后以 `os.Link(temp, target)` 发布目标名，利用 hard-link 的 `EEXIST` 语义保证绝不覆盖另一进程已发布的 key，再 unlink 临时文件并 fsync `secrets/` 目录。若发布时目标已存在，丢弃本次候选并重新打开目标；不能使用会覆盖目标的普通 rename。数据根必须位于支持同目录 hard link 与 fsync 的本地文件系统，否则启动失败。`secrets/` 为 `0700`；已存在目标必须经 `Lstat`/无跟随打开确认为非 symlink、owner-only regular file且恰好 32 bytes，否则拒绝启动。密钥不输出、不进数据库/日志/diagnostic，也不 baked 进镜像。删除或轮换任一密钥会撤销相应活动 capability，必须作为显式维护操作；一期不提供 UI 轮换入口。

上传、archive、worker 和网络边界使用 [HTTP API](./http-api-contract.md) 与 [数据模型](./data-model.md) 的安全默认值；允许部署配置调低，调高必须同步威胁评审与验收。Hasheous production base URL 固定为 `https://hasheous.org`，只通过依赖注入在测试替换，不能由不受控运行环境指向任意 host。

多盘 capability 是 `RETROM_MULTI_DISC_IMPORT_ENABLED`、Platform content profile 与当前 `selected_for_new_bindings` CoreArtifact compatibility 的交集，flag 不是校验旁路。关闭时新建 MULTI Import 与 MULTI 内容替换 fail closed，但不删除证据、不取消已冻结的 Import/Attachment/Job，也不阻止既有多盘 Game 的 Launch、换盘、存档和恢复；需要阻止在途审批时必须显式 cancel/discard。既有 rejected-file reconfigure 始终保持 STANDARD。flag 值不进入日志或诊断。

环境变量解析使用封闭规则：上表列出的名称是服务配置；仅供仓库工具使用、可能被父进程继承的 `RETROM_ACCEPTANCE_*`、`RETROM_CHROME_*`、`RETROM_EJS_DEP_*` 由服务配置加载器明确忽略且不记录值；任何其他未知 `RETROM_*`（例如拼错的 `RETROM_DATA_DI` 或已移除的 example 前缀）都以 `CONFIG_UNKNOWN_VARIABLE` 快速失败。维护子命令只校验自身所需的已知服务变量，但使用同一 unknown/工具前缀规则。缺失配置、目录不可写或路径越界同样非零退出并给出变量名和稳定错误码，但不回显变量值、秘密或完整用户路径。应用配置中不存在 TLS 证书、私钥或 ACME 参数。

SQLite 基线：启用外键、WAL 和合理的 `busy_timeout`；仅通过版本化迁移升级；启动时拒绝运行比二进制更新的 schema。数据库连接池需限制写并发，业务上的多表状态转换使用事务。

## 9. 账户模式与安全边界

无参数服务固定为 `release`；唯一可选服务参数为 `--mode=release|test`。release 空实例先启动到 PENDING，主机操作者再运行只读 `retrom setup-code` 取得证明并通过 `/setup` 创建首位管理员；该命令不取写锁、不修改数据库且不打印路径或其他状态。`retrom admin-reset --username <existing-admin>` 必须在服务停止并取得同一 data-root lock 后，从 `/dev/tty` 隐藏读取两次 release 合规密码；它只操作现有非 DELETED ADMIN，重新启用、撤销 session并写 SYSTEM 审计，密码不允许进入参数、环境或日志。

已初始化实例必须登录。ADMIN 可以管理共享游戏内容、服务器配置和账号安全状态，但不能浏览其他用户的私有游戏历史、存档或截图；主机操作者因可读取 data root/backup/进程内存属于更高信任域，部署方必须用文件权限、磁盘加密和备份访问控制保护。上传仍执行大小、归档、路径和文件魔数安全；第三方文本按纯文本展示。日志/诊断不记录密码、session/CSRF、account-link capability、完整 IP/XFF、ROM/BIOS 内容或完整宿主路径，非秘密 `launchId` 可以与 `request_id` 关联。

## 10. 可观测性与故障诊断

- 每个 HTTP 请求和后台任务携带 `request_id` / `job_id`，结构化日志包含稳定错误码。
- `GET /health/live` 只证明进程存活；`GET /health/ready` 每次使用独立只读连接池执行实时探测，仅在数据库可读写、migration checksum、CAS 数据根、全部 manifest 依赖、七条 RPG route 与 ONS route 各恰有一个当前 `selected_for_new_bindings/available_for_launch` 构件、仍受精确绑定保护的 EmulatorJS artifact/pack 可用，以及每个当前 selected Arcade CoreArtifact 的 READY active DatVersion 均通过时返回 `200`。RPG Maker/ONS 的历史构件可以退役；旧存档是否可恢复由当前构件的 `readableSaveAbis` 投影，不把不兼容存档变成全局 readiness 故障。503 的闭集 reason code 按优先级为 `DATABASE_UNAVAILABLE`、`CAS_UNAVAILABLE`、`DEPENDENCY_INVALID`、`DEPENDENCY_DAT_PARSE_FAILED`、`DEPENDENCY_INDEXING`；响应不含路径/hash。冷库 DAT indexing 期间 HTTP/worker 可以存活，但除 health 外全部路由由前置启动门禁返回 `503 SERVICE_NOT_READY`，不得让部分业务读到未激活目录；首次完整就绪后该启动门禁单向打开，普通业务请求不再逐次执行健康 SQL 或因写连接短暂繁忙误报 503，实时运维状态继续由 `/health/ready` 表达。
- 管理后台任务详情展示阶段、进度、最近错误、重试次数和下次重试时间，不展示堆栈。
- 启动失败日志关联 `launchId`、game、VariantRevision、CoreArtifact、DAT 版本和缺失依赖，但不记录 capability。
- RPG 运行日志只允记录非秘密 `launchId`、validation ID、selected core、generation、route key、artifact ID、adapter ABI、pack 状态、gate 名/结果/时长和稳定错误码；不记录 bootstrap ticket/cookie、项目 bytes/JS、文件名/绝对路径、存档 payload、截图 bytes 或 MV/MZ bridge message 内容。Host confusion/replay 只记录低基数 reason，不回显恶意 Host/ticket。
- 联机只在 Room/Session 转移、upgrade 拒绝、resync、终局与 recovery 记录低基数结构化事件；不记录每帧 input/canonical/hash/state bytes、credential、显示名、IP、内容 hash 或路径。可聚合字段限 profile ID、playerNo、状态、终因、耗时、frame lag、rollback/resync 计数。
- 多盘结构化事件覆盖 Import mode/parser 结果、Attachment 状态/重试/执行时长、Validation 结果、Launch 盘数、playlist/DISC 内容响应状态与 bytes，以及 Player 开始/盘数不一致/换盘/存档恢复结果。可聚合标签仅限 platform key、core key、artifact version、盘数 bucket、HTTP 状态与稳定错误码；不得记录标题、basename、路径、内容 hash 或 capability。Import/Attachment/Validation 使用持久 JobEvent，运行端使用固定 schema 的结构化日志；不存在自由形式客户端 telemetry body。
- EmulationStation 事件只记录 import/job ID、phase、封闭计数、执行时长和稳定错误码；不得记录 XML 文本、`command/emulator/core/provider` 值、标题、ROM/媒体 basename、绝对路径、facts digest 或底层 `os.PathError`。管理员失败详情只使用 OpenAPI 封闭字段和截断后的低敏技术 code。
- `GET /api/v1/admin/diagnostics` 提供 HTTP 契约规定的封闭 JSON 诊断摘要，只含版本与状态计数；不打包原始日志、ROM/BIOS，不输出资源 ID、内容 hash、环境变量值或宿主路径。响应必须 `private, no-store`，字段变化先升级 schemaVersion/OpenAPI/验收，不能临时追加自由形式 map。
- `GET /api/v1/admin/storage-analysis` 使用独立只读连接池和一个 snapshot transaction，按存储专题固定口径返回已登记 CAS payload 的用途总量；不得扫描宿主目录或返回资源标识。`POST /api/v1/admin/storage-cleanups` 只允许 ADMIN 在 CSRF/幂等保护下把当前未引用候选推进为立即可执行，仍由既有 PayloadRelease/BLOB_GC worker 逐 Blob 复核并回收；HTTP 不同步删除文件，也不返回 Blob/Job 标识。`OTHER_REFERENCED` 非零时日志只记录 category、count 和 bytes，禁止输出 Blob ID/hash/路径。

## 11. 备份、恢复与 lineage

一致备份使用同一 `retrom` 二进制的离线 `backup`/`restore` 子命令；没有 HTTP Backup API，也不允许在 serve/worker 仍持有数据根 lock 时复制。bundle 包含离线 checkpoint、关闭全部 handle 后复制并二次校验的单文件 SQLite 快照、该快照全部 `blobs` 行对应的 CAS 文件、未完成 UploadPart、`secrets/launch-capability.key`、`secrets/netplay-capability.key`、已配置版本的小型 dependency manifest/SHA256SUMS，以及 `backup.json` 中唯一的 active/有序版本配置与 migration lineage digest；尚在 GC 宽限期的 Blob 仍有数据库行，不能从原样快照的 bundle 中裁掉。精确 v2 目录与封闭 JSON schema见存储专题；不存在第二份运行配置文件。内置大 DAT/runtime/许可 payload 不进入 bundle，由部署方在恢复服务启动前按 manifest 预先物化。该流程只依赖标准 SQL/文件 API，不要求 `modernc.org/sqlite` 暴露私有 Backup API。密钥按 secret 文件处理且不出现在日志或 manifest 明文。

精确命令、原子发布、引用 registry、目标必须不存在和恢复校验见[存储与数据库第 8 节](./storage-and-database.md#8-备份与恢复)。恢复发布前还要在单一事务撤销全部旧 AuthSession、ACTIVE AccountLink和非终态 Launch，把遗留联机 Session/Room 以 `RESTORE` 收口，并写 SYSTEM安全围栏审计；因此恢复后的旧 cookie/capability/WebSocket 全部无效，实时 history 不尝试恢复。命令本身不启动服务、不覆盖旧目录。

当前未发布基线只接受 001–010 clean lineage 的精确有序前缀或完整集合；旧开发 lineage、旧 manifest schema、部分备份和名称/checksum 漂移都在写入前拒绝。部署本次改造时归档或删除标准开发数据库并以空根初始化；回退只能恢复与目标二进制 lineage 精确匹配的完整数据根，不得混合数据库、CAS 或密钥。恢复服务开放 HTTP 前把所有依赖外部 source 的非终态 BIOS/Pegasus/EmulationStation Job 与 aggregate 以 `SERVER_IMPORT_SOURCE_NOT_RESTORED` 失败收口；普通待审和已发布 CAS bytes 保留。首次正式发布后再按当时契约设计只追加升级，不预留未验证的转换分支。

## 12. 统一验收入口

工程门禁与双镜像执行 [一期项目验收规范](./project-acceptance.md) 的 `ACC-QA-*` 和 `ACC-PKG-*`，联机协议、安全、feature flag、单机回归与双浏览器核心生命周期执行 `ACC-NP-010`–`016`，本地进程与 NG/TLS 边界执行 `ACC-DEV-001` 和 `ACC-NET-001`–`002`（后者仅在已部署 NG 时适用），游戏维护执行 `ACC-GAME-*`，API、健康检查及诊断执行 `ACC-API-001` 和 `ACC-OPS-001`。RPG 七世代、运行依赖、unique origin、route/artifact 历史和跨 Launch 精确 checkpoint 恢复执行 `ACC-RPG-001`–`012`；其中 `ACC-RPG-008` 必须显式传 `RPG_MZ_SMOKE_ROOT`。多盘 feature flag、替换和既有内容连续性执行 `ACC-MDISC-007`；Pegasus 外部来源、恢复栅栏、共享读取治理和产品运行链执行 `ACC-PEG-001`–`006`；EmulationStation parser、外部来源、handoff、恢复/释放和产品运行链执行 `ACC-ES-001`–`006`；游戏视频资产执行 `ACC-MEDIA-001`。数据库、内容端点、任务恢复和备份由统一文档中对应 `ACC-DB-*`、`ACC-SEC-*`、`ACC-IMP-008` 与 `ACC-BKP-001` 联合覆盖。

## 13. 服务器导入运维

`RETROM_SERVER_IMPORT_ROOTS` 缺省或 `[]` 时能力为空但服务正常；非空值必须是最多 8 项的封闭 JSON 数组，每项为 `id/label/path`。ID、label 必须唯一且满足长度/字符约束；path 必须是已存在的 clean absolute 普通目录且 root 本身不是 symlink。拒绝 `/`、home、Retrom data root、dependency root、这些目录任一方向的重叠，以及各配置 root 的相同/祖先关系。非法值启动失败，只记录变量名。生产部署仅以只读 volume 映射 source，不改变双镜像或 TLS 契约。

服务从现有 credential root key 目的分离派生 HMAC，任务保存 `rootId + canonical real path` 的不可逆 digest；同 ID 被重定向后 retry 以 `SERVER_IMPORT_ROOT_CHANGED` 失败。共享 reader semaphore 固定为 2，hash worker 固定 2、archive scanner 固定 1；数据库/HTTP 不能按格式再建立一套磁盘并发额度。

EmulationStation 单实例至多一个 active execution、20 个未开始/等待映射计划，等待映射 7 天过期。扫描上限固定为深度 64、目录 250,000、普通文件 2,000,000、精确小写 `gamelist.xml` 1,000、单 XML 8 MiB、XML 总量 64 MiB、XML depth/attributes 16、单 token 1 MiB、总 token 1,000,000、游戏 100,000、单 Item source file 64、warning 64、预计来源 2 TiB与单 execution 8 小时；HTTP 不能放宽。扫描只读取 XML/facts/M3U/媒体与 CHD 头，执行才复制完整内容。

Worker lease 为 60 秒、每 15 秒 heartbeat，并每读取 8 MiB 检查 cancel/deadline。进程恢复复用完整发现结果和终态 Item；root 暂不可用或内部瞬时错误只在零终态 Item 时按 1/5/30/120 秒有界自动重试，最多 4 attempt。日志、JobEvent 和 diagnostics 仅记录 root ID、相对路径的必要脱敏投影和稳定错误码，不记录绝对路径、basename/hash 或底层 `os.PathError`。

## 14. 关联文档

- [产品与架构总览](./retrom-product-architecture.md)
- [一期项目验收规范](./project-acceptance.md)
- [工程质量、Lint 与测试规范](./engineering-quality-and-testing.md)
- [存储与数据库](./storage-and-database.md)
- [游戏目录领域设计](./platform-instance.md)
- [导入、刮削与审核](./import-and-review.md)
- [BIOS 与 Arcade DAT](./bios-and-arcade.md)
- [运行时、启动与游玩数据](./runtime-and-play-data.md)
- [UI 与交互规范](./ui-specification.md)
