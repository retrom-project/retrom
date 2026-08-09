# 后端、API 与运行维护

| 属性 | 内容 |
| --- | --- |
| 状态 | 已审定 / 一期实施基线 |
| 版本 | 1.1 |
| 日期 | 2026-08-06 |
| 适用范围 | Retrom 一期 |
| 技术栈 | Go、SQLite、Next.js、EmulatorJS、本地内容寻址存储、OCI/Docker 镜像 |

本文定义 Retrom 的部署边界、Go 模块划分、API 约定、后台任务、安全与运维要求。领域细节由对应专题文档负责，本文不重复其状态机和数据字典。

## 1. 架构结论

一期后端采用 Go 模块化单体，单个 `retrom` 进程提供 JSON API、后台 Worker、EmulatorJS 运行时和受控内容端点；前端由独立的 `retrom-web` Next.js 进程提供 UI 与 Player Shell。生产环境在二者之前放置已有的 NG（Nginx/网关/反向代理），由 NG 暴露单一站点 origin 并终结 TLS。SQLite 保存业务数据与任务状态，用户文件写入本地 SHA-256 内容寻址存储（CAS）。前后端分镜像是构建与部署边界，不把后端领域拆成微服务，也不引入 Redis、消息队列或 S3。

~~~mermaid
flowchart LR
    Chrome["Chrome 桌面端"] -->|HTTPS| NG["前置 NG / TLS 终结"]
    NG -->|HTTP：页面与 _next| Web["retrom-web / Next.js"]
    NG -->|HTTP：API / content / runtime| Go["retrom / Go 模块化单体"]
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
internal/metadata/        Hasheous 适配器与缓存
internal/arcadedat/       DAT 安装、解析、依赖图与诊断
internal/bios/            BIOS 要求、安装和状态聚合
internal/runtime/         启动预检、LaunchSession/capability、EJS 配置
internal/saves/           状态存档、截图与兼容性
internal/playtime/        PlaySession 和有效时长
internal/blobstore/       CAS 写入、读取、引用与垃圾回收
internal/jobs/            SQLite 队列、租约、重试和 Worker
internal/store/           SQLite 连接、迁移和事务辅助
internal/observability/   结构化日志、健康检查和诊断导出
internal/httpapi/generated/ OpenAPI 生成的 strict server types；禁止手改
migrations/               Go package：embed.go 与有序 SQL migration，编译进后端
api/openapi.yaml          OpenAPI 3.0.3 协议事实源
api/oapi-codegen.yaml     固定 Go 生成器配置
web/                      Next.js + React + Tailwind CSS
data/dat/                 小型依赖 manifest/SHA；大 payload 由 prepare-deps 物化
```

依赖方向遵循 `httpapi/jobs -> application modules -> store/blobstore`。HTTP handler 不直接拼 SQL，DAT 解析器不写游戏元信息，Hasheous 适配器不判断 Arcade 可运行性。

前端按能力分区：

```text
web/app/                  Next.js 路由与页面壳
web/proxy.ts              HTML 每请求 nonce/CSP 与跨源隔离响应头
web/features/library/     游戏库和游戏详情
web/features/player/      持久 Player Shell、预检和 EmulatorJS
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

状态码、受信内网写请求、幂等、分页、上传及全部 route 的唯一协议见 [HTTP API、上传与启动凭据契约](./http-api-contract.md)。后端以固定 `oapi-codegen` 的 strict `net/http` 接口实现 `api/openapi.yaml`，前端由同一文件生成 TypeScript schema 并用类型化 fetch client；`make api-check` 拒绝生成物漂移。不能维护另一组手写路径、DTO 或状态码。

`strict-server` 主要约束 handler/response type，并不自动完成全部请求验证。正式 handler 外层固定使用 `github.com/oapi-codegen/nethttp-middleware v1.2.0` 与其锁定的 `github.com/getkin/kin-openapi v0.142.0` 加载同一 OpenAPI 3.0.3，验证 path/query/header/body schema；所有固定 object schema 必须 `additionalProperties:false`。在它之前的 JSON lexical middleware 对 `application/json` body 施加 route 上限（全局最高 16 MiB），先 `utf8.Valid`，再用 token stack 拒绝重复 object key、depth >64、多个顶层值和尾随非空白，最后恢复 body 给 validator/generated binder。query middleware 根据匹配 operation 的参数集合拒绝未知名、标量重复值与非法 percent encoding。

三个大 body operation `putAdminUploadPart`、`postRuntimeSaveState`、`putRuntimePersistentSave` 在 OpenAPI 标记 `x-retrom-streaming-body: true`。启动时基于同一已加载 spec 构建两条不可变 validator chain：普通链保持 `Options.Options.ExcludeRequestBody=false`，流式链设置 `Options.Options.ExcludeRequestBody=true`；前置 kin-openapi router 匹配 operation extension 后分派，不能在并发请求之间修改共享 options。流式链只跳过 schema body 读取，仍验证 method/path/query/必需 headers/content type；随后由 generated strict request 的 `io.Reader`/`multipart.Reader` 交给领域 handler 按 HTTP 专题流式限额、part、digest 和临时文件规则处理。其他 operation 不得设置该 extension。不能按 URL 字符串另维护一份 skip 清单，也不能用全局 `Skipper` 跳过整条验证。SSE/GET 无 request body，不经过 JSON scanner。出站响应由 `httptest.ResponseRecorder` contract test 逐 operation 用同一 schema 验证；生产不为此重复 buffer 大二进制响应。

## 4. API 能力地图

本节只描述能力归属。实际方法、路径、body、状态码和缓存策略全部以 [HTTP API 契约第 9 节](./http-api-contract.md#9-核心-api-路由表) 与实现后的 `api/openapi.yaml` 为准；两者不一致时实现任务必须先修正文档/OpenAPI，不能兼容两套路径。

- 用户读取：home、game library/detail、save list。
- 用户写入：创建 LaunchSession、heartbeat/finish、手动状态存档。
- 管理写入：upload、import、review、game revision、platform instance、BIOS installation、Arcade DAT installation。
- 管理读取：入库总览/任务/SSE、待审核/历史、游戏管理、BIOS/DAT 状态、审计事件和脱敏诊断摘要。

详情页和存档快速启动都调用同一 `POST /api/v1/launches`；区别只在是否携带 `saveStateId`。管理 API 一期不做账户鉴权或 CSRF 校验，但所有写请求仍执行乐观并发与幂等校验。浏览器目录上传只传相对路径，服务端不提供任意宿主目录扫描端点。

## 5. 内容端点与 LaunchSession capability

浏览器不得获得宿主机路径、Blob ID 或能力秘密。`POST /api/v1/launches` 返回可记录的 UUIDv7 `launchId`，同时通过 `retrom_launch_<launchId>` HttpOnly cookie 下发 32-byte capability；数据库只保存其 SHA-256。Player URL 固定为 `/play/:launchId`，受控内容固定在 `/runtime/launches/:launchId/**`，因此 capability 不会进入 URL、Referer、JSON 或访问日志。

固定 EmulatorJS 和发布媒体可以公开 immutable 缓存；ROM、parent、BIOS、持久保存和状态存档必须是 `private, no-store` 且 `Vary: Cookie`。允许路径、cookie scope/过期、单 Range、ETag、MIME 和错误隐藏的唯一契约见 [HTTP API 第 7–8 节](./http-api-contract.md#7-launch-创建与凭据)。Go 静态 handler 只能发布依赖 manifest allowlist，不能把物理目录直接挂为文件服务器。

## 6. 后台任务

任务至少覆盖：Upload 终结组装与 Blob 哈希落库、Import 安全扫描/分组与逐 Item pipeline、Archive 检查、DAT 解析/索引、Arcade 依赖识别、Hasheous 查询与图片获取、游戏内容 revision/兼容重校验、孤儿 Blob 垃圾回收。精确 Job kind/scope 映射以数据模型为准，不另起一组同义名称。

SQLite 队列表和 worker 必须实现 [数据模型第 7 节](./data-model.md#7-通用任务事件与审计) 的字段、领取索引、60 秒 lease、15 秒 heartbeat、并发上限和四次 attempt 退避。领取任务必须在短事务内完成，租约到期后可恢复；任务处理必须幂等。网络任务尊重上游 `Retry-After`，但等待上限 15 分钟。

每个 execution 还使用数据模型固定的 kind wall deadline；第一次领取时计算，自动 retry 不重置，人工 retry 才开始新 execution/deadline。Worker 的 reader、网络和解析 context 必须来自该 deadline，超时产生稳定错误而不是无限 RUNNING。测试通过 fake clock/context 触发，不使用长时间 sleep。

运行中取消不是立即宣告完成：API 将 Job 置为 `CANCEL_REQUESTED`，当前 worker 在数据模型规定的有界检查点停止并清理 scratch 后才置 `CANCELLED`；lease 已过期时恢复器只能确认取消。所有成果提交都比较 state、lease token 和 cancel flag，防止旧 worker 在取消/重新领取后写入。UI/SSE 必须区分“正在取消”和“已取消”。

不要把长时间哈希、网络请求、DAT 解析或归档扫描放在持有数据库写锁的事务中。先执行可重入计算，再用短事务提交结果和状态转换。

导入任务及审核语义见 [导入、刮削与审核](./import-and-review.md)。

## 7. 构建、镜像、本地开发与 TLS 边界

### 7.1 镜像与 Makefile 契约

仓库只构建两个应用镜像：

| 镜像 | Dockerfile / context | 责任 | 默认内部 HTTP 端口 |
| --- | --- | --- | --- |
| `retrom` | 根 `Dockerfile` / 仓库根目录 | Go API、Worker、迁移、DAT、固定 EmulatorJS runtime 和受控内容端点 | `8080` |
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

两个 Dockerfile 使用多阶段构建和非 root 运行用户，最终层不保留编译工具、源码缓存或开发依赖。后端 dependency builder 必须读取构建参数中 `RETROM_DEPENDENCY_VERSIONS` 对应的小型 manifest 集，按固定来源逐版本物化/校验 EmulatorJS、core、可选 DAT 与许可输入；当前默认 `4.2.3,4.3.0-pre`，后者只提供 DOSBox Pure 定向覆盖。前端镜像携带经过 `data-check` 的 adapter registry/实现；两个镜像必须携带完全相同的 release-input label。不能把下载 archive、本地依赖缓存、source checkout 或整个 `data/` 目录复制进镜像。

`make build-images` 不自动属于普通 `make ci`，但修改任一 Dockerfile、依赖锁文件、构建脚本、DAT/runtime 打包逻辑或发布资产时必须同时执行二者；发布流水线也必须把二者作为独立门禁。

### 7.2 `make dev` 只运行本地进程

`make dev` 是宿主机开发入口，不是容器入口，也不得依赖 Docker daemon。它先执行幂等 `make prepare-deps`，成功后以前台 supervisor 方式同时启动：

1. `go run ./cmd/retrom`，默认监听 `127.0.0.1:8080`；
2. `cd web && npm run dev`，默认监听所有 IPv4 接口 `0.0.0.0:3000`，可用 `NEXT_DEV_HOST` 显式收窄；
3. Next.js dev rewrite 将 `/api/`、`/content/`、`/runtime/` 和 `/health/` 转发到本地 Go 端口，使浏览器始终通过访问页面时使用的同一 origin 请求前后端资源。开发服务不规范化 Host，也不把远程请求重定向到 localhost。

脚本必须转发 `SIGINT/SIGTERM`、在任一子进程异常退出时停止另一进程并返回非零状态，退出后不得残留后台进程。每次启动还必须在仓库 `.cache/retrom/dev.pid` 中原子登记 supervisor、Go 与 Next.js 三者的 PID 和 Linux process start ticks；子进程另以独立 process group/session 启动。正常接管先用 supervisor 的 PID/start ticks、工作目录和命令行确认身份，再发送 `SIGTERM` 并等待最多 15 秒；若 supervisor 已被 `SIGKILL` 等方式终止，新实例必须分别以登记的子进程 PID/start ticks、process group/session、工作目录和完整启动命令确认遗留 Go/Next.js 身份，只有两者各自通过确认后才向对应精确 process group 发送 `SIGTERM` 并等待数据锁释放。旧版仅登记 supervisor 的两字段文件继续支持正常接管，但不能据此猜测或扫描孤儿子进程。陈旧 PID、PID 复用、伪造登记或其他工作目录的同名进程不得被终止；登记无法证明身份但数据根仍被锁定时，新实例必须在启动子进程前明确失败，不得把错误推迟成后端 `DATA_ROOT_LOCKED`，也不得按端口或进程名批量杀进程。无法在期限内退出时同样失败。启动接管以 `.cache/retrom/dev-takeover.lock` 串行化，登记文件由 owner 在退出时清理。

`make dev` 不构建镜像、不启动容器、不创建容器网络；本地开发数据写入明确且被 Git 忽略的 `RETROM_DATA_DIR`。前端固定监听 `0.0.0.0:3000`，后端仍保持回环监听；仓库默认浏览器 origin 为 `http://local.sendev.cc:3000`，便于从独立开发机访问测试服务器。Next.js 开发服务器不得把 `/_next` 静态资源或 HMR 绑定到该 origin，所有常规外部 DNS 域名、IPv4 地址和 opaque origin 均可访问开发资源；Retrom 不执行 Host 重定向。非 localhost 的明文域名不是浏览器安全上下文，PPSSPP、DOSBox Pure 和 Mednafen PSX HW 等线程核心仍受 Chrome 的 `SharedArrayBuffer` 能力限制；页面只报告能力不足，不跳转到客户端 localhost。仅 `make dev` 注入 `RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN=true` 以支持受信测试网中的明文 origin；普通服务进程默认拒绝非 localhost 的 HTTP origin。前端的幂等 UUID 与上传/存档 SHA-256 在缺少 `crypto.randomUUID`/`crypto.subtle` 时仍使用受测的 Web Crypto 兼容 fallback；安全随机数始终来自 `crypto.getRandomValues`。

### 7.3 TLS 只在 NG 终结

生产拓扑中，浏览器只连接 NG 的 HTTPS 地址；`retrom-web:3000` 和 `retrom:8080` 只接受来自受信网络的明文 HTTP。Retrom 不提供证书/私钥配置、不监听 HTTPS、不申请证书、不执行 HTTP→HTTPS 跳转，也不管理 HSTS。对应职责全部属于前置 NG，且本项目的镜像构建 target 不构建或启动 NG。

固定同源路由：

| 外部路径 | NG 上游 |
| --- | --- |
| `/_next/*` 及其余页面路由 | `retrom-web:3000` |
| `/api/v1/*` | `retrom:8080` |
| `/content/*`、`/runtime/*` | `retrom:8080` |
| `/health/*` | `retrom:8080`，通常只开放给内部健康检查 |

前端只使用相对 URL，不把内部容器名、端口或环境域名编译进浏览器 bundle。若 Next.js server-side 代码确需访问后端，使用运行时内部 base URL，与浏览器公开 base URL 分离。

TLS 终结外置不等于忽略代理安全：

- NG 必须为页面与运行时资源保留/设置一致的 COOP、COEP、CORP 和 `nosniff` 头，保证 `window.crossOriginIsolated`；这些头不是 TLS 功能。
- NG 的上传大小、buffering 和 timeout 必须允许大 ROM 流式上传；后端仍独立执行大小、归档和路径安全校验。
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
  tmp/uploads/
  tmp/jobs/
  backups/
```

一期环境变量契约固定如下；配置在启动时一次读取并校验，业务代码不直接读环境：

| 变量 | 开发默认 / 生产规则 |
| --- | --- |
| `RETROM_HTTP_ADDR` | `make dev` 注入 `127.0.0.1:8080`；容器部署显式设为 `0.0.0.0:8080`，没有 HTTPS 值。 |
| `RETROM_PUBLIC_ORIGIN` | 当前仓库开发默认 `http://local.sendev.cc:3000`，可显式覆盖为实际受信开发 origin；它用于后端 cookie Secure 策略和公开 origin 配置，不限制 Next.js 开发静态资源或 HMR 的请求来源，不触发 Host 重定向，也不作为写请求授权。非 localhost 主机名必须使用 HTTPS 才能运行线程核心。生产必填且必须是无 path/query/fragment 的单个 `https` origin。 |
| `RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN` | 服务默认 `false`/未设置，仅接受 `https` 或 `http://localhost`；`make dev` 固定注入 `true`，允许显式 `RETROM_PUBLIC_ORIGIN=http://<开发域名或局域网地址>:3000`。生产必须保持未设置或 `false`。 |
| `RETROM_DATA_DIR` | 必须是已解析绝对路径；开发由 Makefile 设为仓库 `.cache/retrom/data`，生产为持久卷。它与只读 `RETROM_DEPENDENCY_ROOT` 严格分离；应用创建子目录但拒绝文件系统根、用户 home 和 symlink 数据根。 |
| `RETROM_DB_PATH` | 未设置时派生为数据根下 `retrom.db`；若设置必须是数据根内的绝对普通文件路径。 |
| `RETROM_DEPENDENCY_ROOT` | 必填绝对只读目录；其下按 `dat/emulatorjs/<version>` 与 `runtime/emulatorjs/<version>` 布局。开发固定为仓库 `data/` 的绝对路径，镜像内固定为只读依赖层；拒绝 root/home/symlink 逃逸。 |
| `RETROM_DEPENDENCY_VERSIONS` | 必填、无空白/重复且按 SemVer（含 prerelease）升序；当前为 `4.2.3,4.3.0-pre`。每项必须有完整 manifest/runtime/许可 payload，DAT 只在该 manifest 声明时必需。 |
| `RETROM_ACTIVE_EMULATORJS_VERSION` | 必填且必须属于上列；当前为 `4.2.3`。新验证逐 core 使用版本列表中最后一个声明该 core 的 artifact，不覆盖历史 revision 锁定版本。 |
| `RETROM_TRUSTED_PROXIES` | 逗号分隔 CIDR；默认空。生产必须精确列出 NG 网段，不能使用 `0.0.0.0/0` 或 `::/0`。 |
| `RETROM_STARTUP_CHECK_TIMEOUT` | 默认 `60s`，范围 `10s..5m`；只约束配置、依赖字节、数据库/migration 与 bootstrap Job 登记等同步预检，不包含后台 `DAT_PARSE` execution。 |
| `RETROM_LOG_LEVEL` | `debug/info/warn/error`，默认 `info`；生产禁止记录内容秘密。 |

`RETROM_DATA_DIR/secrets/launch-capability.key` 没有环境变量覆盖。首次启动用系统 CSPRNG 生成 32 bytes，在同目录新建唯一 `0600` 临时文件、完整写入并 fsync；随后以 `os.Link(temp, target)` 发布目标名，利用 hard-link 的 `EEXIST` 语义保证绝不覆盖另一进程已发布的 key，再 unlink 临时文件并 fsync `secrets/` 目录。若发布时目标已存在，丢弃本次候选并重新打开目标；不能使用会覆盖目标的普通 rename。数据根必须位于支持同目录 hard link 与 fsync 的本地文件系统，否则启动失败。`secrets/` 为 `0700`；已存在目标必须经 `Lstat`/无跟随打开确认为非 symlink、owner-only regular file 且恰好 32 bytes，否则拒绝启动。两个并发首次启动因此收敛到同一 key；一期本就只允许单个后端写进程。密钥只用于 HTTP 契约定义的 HMAC domain，不输出、不进数据库/日志/diagnostic，也不 baked 进镜像。删除或轮换会使最长 24 小时内的活动 launch cookie 失效，必须作为显式维护操作；一期不提供 UI 轮换入口。

上传、archive、worker 和网络边界使用 [HTTP API](./http-api-contract.md) 与 [数据模型](./data-model.md) 的安全默认值；允许部署配置调低，调高必须同步威胁评审与验收。Hasheous production base URL 固定为 `https://hasheous.org`，只通过依赖注入在测试替换，不能由不受控运行环境指向任意 host。

环境变量解析使用封闭规则：上表列出的名称是服务配置；仅供仓库工具使用、可能被父进程继承的 `RETROM_ACCEPTANCE_*`、`RETROM_CHROME_*`、`RETROM_EJS_DEP_*`、`RETROM_EXAMPLE_*`、`RETROM_FIXTURE_*`、`RETROM_SMOKE_*` 由服务配置加载器明确忽略且不记录值；任何其他未知 `RETROM_*`（例如拼错的 `RETROM_DATA_DI`）都以 `CONFIG_UNKNOWN_VARIABLE` 快速失败。维护子命令只校验自身所需的已知服务变量，但使用同一 unknown/工具前缀规则。缺失配置、目录不可写或路径越界同样非零退出并给出变量名和稳定错误码，但不回显变量值、秘密或完整用户路径。应用配置中不存在 TLS 证书、私钥或 ACME 参数。

SQLite 基线：启用外键、WAL 和合理的 `busy_timeout`；仅通过版本化迁移升级；启动时拒绝运行比二进制更新的 schema。数据库连接池需限制写并发，业务上的多表状态转换使用事务。

## 9. 无账户模式的安全边界

一期访问模型是“可信局域网内共享管理员”，不是公网匿名服务：

- `make dev` 的 Go 后端默认只监听回环地址，Next.js 前端默认监听 `0.0.0.0:3000` 以供受信开发局域网访问；宿主机防火墙必须限制该端口，且不得把无账户管理界面直接暴露到公网。容器端口只能留在受信内部网络，由 NG 作为唯一生产入口。
- 对外 HTTPS 必须由前置 NG 提供；Retrom 两个应用进程只监听内部 HTTP。线程模式和 Fullscreen API 的完整能力仍依赖 NG 暴露的安全上下文。
- 所有改变状态的请求校验同源 `Origin`/`Sec-Fetch-Site`，Cookie 若存在必须为 `SameSite=Strict`。
- 上传只接受白名单后缀只是提示层，服务端仍需检查大小、归档结构和文件魔数。
- 展示第三方刮削文本时按纯文本处理；不执行外部 HTML/SVG/脚本。
- 日志不记录 ROM/BIOS 内容、launch capability、cookie/header、完整宿主路径或上游敏感响应；非秘密 `launchId` 可以记录并应与 `request_id` 关联。

未来加入账户时，应在现有 `/admin` 路由组和 Profile 外键上增加认证授权，不改变 Game、Variant、SaveState 的核心归属。

## 10. 可观测性与故障诊断

- 每个 HTTP 请求和后台任务携带 `request_id` / `job_id`，结构化日志包含稳定错误码。
- `GET /health/live` 只证明进程存活；`GET /health/ready` 仅在数据库可读写、migration checksum、CAS 数据根、全部 manifest 依赖，以及每个当前 enabled Arcade CoreArtifact 的 READY active DatVersion 均通过时返回 `200`。503 的闭集 reason code 按优先级为 `DATABASE_UNAVAILABLE`、`CAS_UNAVAILABLE`、`DEPENDENCY_INVALID`、`DEPENDENCY_DAT_PARSE_FAILED`、`DEPENDENCY_INDEXING`；响应不含路径/hash。冷库 DAT indexing 期间 HTTP/worker 可以存活，但除 health 外全部路由由前置 readiness middleware 返回 `503 SERVICE_NOT_READY`，不得让部分业务读到未激活目录。
- 管理后台任务详情展示阶段、进度、最近错误、重试次数和下次重试时间，不展示堆栈。
- 启动失败日志关联 `launchId`、game、VariantRevision、CoreArtifact、DAT 版本和缺失依赖，但不记录 capability。
- `GET /api/v1/admin/diagnostics` 提供 HTTP 契约规定的封闭 JSON 诊断摘要，只含版本与状态计数；不打包原始日志、ROM/BIOS，不输出资源 ID、内容 hash、环境变量值或宿主路径。响应必须 `private, no-store`，字段变化先升级 schemaVersion/OpenAPI/验收，不能临时追加自由形式 map。

## 11. 备份、恢复与升级

一致备份使用同一 `retrom` 二进制的离线 `backup`/`restore` 子命令；没有 HTTP Backup API，也不允许在 serve/worker 仍持有数据根 lock 时复制。bundle 包含离线 checkpoint、关闭全部 handle 后复制并二次校验的单文件 SQLite 快照、该快照全部 `blobs` 行对应的 CAS 文件、未完成 UploadPart、`secrets/launch-capability.key`、已配置版本的小型 dependency manifest/SHA256SUMS，以及 `backup.json` 中唯一的 active/有序版本配置；尚在 GC 宽限期的 Blob 仍有数据库行，不能从原样快照的 bundle 中裁掉。精确 v1 目录与封闭 JSON schema见存储专题；不存在第二份运行配置文件。内置大 DAT/runtime/许可 payload 由部署方在恢复服务启动前按 manifest 预先物化，用户 DAT 已在 CAS。该流程只依赖标准 SQL/文件 API，不要求 `modernc.org/sqlite` 暴露私有 Backup API。密钥按 secret 文件处理且不出现在日志或 manifest 明文。

精确命令、原子发布、引用 registry、目标必须不存在和恢复校验见[存储与数据库第 8 节](./storage-and-database.md#8-备份与恢复)。恢复后通过全部完整性/依赖检查，再由操作者显式以新数据根启动服务；命令本身不启动服务、不覆盖旧目录。

升级顺序：备份 → 在依赖版本列表追加并物化新版本 → 校验目标 EmulatorJS/DAT 兼容矩阵和旧存档 → 部署仍含受保护旧版本的二进制/前端镜像并切换 active 版本 → 执行向前迁移 → 重建派生索引 → 抽样普通启动与旧存档启动。升级不得静默改写已有 GameVariant 或存档绑定；回滚切回旧 active 版本而不覆盖目录或历史 revision。

## 12. 统一验收入口

工程门禁与双镜像执行 [一期项目验收规范](./project-acceptance.md) 的 `ACC-QA-*` 和 `ACC-PKG-*`，本地进程与 NG/TLS 边界执行 `ACC-DEV-001` 和 `ACC-NET-001`–`002`（后者仅在已部署 NG 时适用），游戏维护执行 `ACC-GAME-*`，API、健康检查及诊断执行 `ACC-API-001` 和 `ACC-OPS-001`。数据库、内容端点、任务恢复和备份由统一文档中对应 `ACC-DB-*`、`ACC-SEC-*`、`ACC-IMP-008` 与 `ACC-BKP-001` 联合覆盖。

## 13. 关联文档

- [产品与架构总览](./retrom-product-architecture.md)
- [一期项目验收规范](./project-acceptance.md)
- [工程质量、Lint 与测试规范](./engineering-quality-and-testing.md)
- [存储与数据库](./storage-and-database.md)
- [游戏目录领域设计](./platform-instance.md)
- [导入、刮削与审核](./import-and-review.md)
- [BIOS 与 Arcade DAT](./bios-and-arcade.md)
- [运行时、启动与游玩数据](./runtime-and-play-data.md)
- [UI 与交互规范](./ui-specification.md)
