# HTTP API、上传与启动凭据契约

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已审定 / 一期实施基线 |
| 版本 | 1.7 |
| 日期 | 2026-08-25 |
| 适用范围 | `/api/v1`、`/content`、`/runtime`、`/__retrom`、SSE、应用同源与每 Launch 独立 runtime origin 安全 |

## 1. 协议基线

- 普通页面、媒体和 `/api/v1` 只访问 NG 暴露的应用 origin；前端使用相对 URL，API 不启用跨域 CORS。唯一例外是 MV/MZ 每 Launch 独立 origin 上的 `/__retrom/*` 封闭 allowlist，它不暴露普通 API，也不开启通用 CORS。
- JSON API 前缀固定为 `/api/v1`，`Content-Type` 为 `application/json; charset=utf-8`；二进制上传、SSE 与内容端点除外。
- 业务实体 ID 使用规范小写 UUIDv7 字符串。`coreId`、`platformId` 等代码种子使用稳定小写 code（如 `fbneo`、`arcade`），不得混成自增数字。
- 时刻为 camelCase `*AtMs` 的 JSON int64，数据库对应 Unix 毫秒 `INTEGER`；时长为 `*DurationMs`。
- 未知 JSON 字段、重复字段、错误类型、尾随多个 JSON 值一律 `400 INVALID_REQUEST`；UTF-8 无效文本拒绝。所有固定 JSON request/response object schema 显式 `additionalProperties: false`；真正的 map/错误 `details` 才逐项显式允许 additional properties。
- OpenAPI 3.0.3 协议事实源是以 `api/openapi.yaml` 为唯一入口、由 `api/domains/*.yaml` 与 `api/components/*.yaml` 组成的本地文件集。领域文件共同维护 route 与所属 DTO，跨领域组件只进入 common 文件；入口以标准本地 `$ref` 固定完整 path/component 闭集。生成前必须拒绝远程、绝对路径和越出 `api/` 的引用，并确定性生成被忽略的 `.cache/generated/openapi.bundle.yaml`。固定 `oapi-codegen` strict stdlib server types、`openapi-typescript` schema、`openapi-fetch` client 与 contract test 只能消费该统一 bundle。Go 的 `models.gen.go`、`server.gen.go`、`spec.gen.go` 在标准后端编译链路中按需生成、被 Git 忽略且不得提交；TypeScript `web/lib/api/generated/schema.d.ts` 继续保持单一全局 `paths` schema、必须提交。`make api-check` 在临时目录重建 bundle 和两端结果、逐字节检查 TypeScript 漂移并拒绝任一 Go 生成物被跟踪。锁定的 `oapi-codegen v2.8.0` 已支持 OpenAPI 3.1，但一期仍将 3.0.3 作为经审定的项目协议基线，以避免 nullable/schema 方言和两端生成结果在实施中漂移；升级规范版本必须作为独立契约迁移，同时验证全部生成器、validator 与 contract test，不能只修改 `openapi` 版本号。可空字段使用 OAS 3.0 `nullable: true`，不得写 3.1 的 union type；本文锁定一期语义，不能由生成器反向改变。

成功列表统一为：

```json
{
  "items": [],
  "nextCursor": null
}
```

cursor 是服务端签名/校验的不透明字符串，绑定路由、排序和筛选；不能接受另一查询的 cursor。稳定排序必须包含唯一 ID 作为最后 tie-breaker。其字节契约固定为
`base64url(payload) + "." + base64url(HMAC-SHA-256(cursorKey, "retrom-cursor-v1\x00" || payload))`（均无 padding），其中
`cursorKey = HMAC-SHA-256(launchKey, "retrom-cursor-key-v1")`。`payload` 是 RFC 8785 canonical JSON，只含
`version=1`、OpenAPI `operationId`、筛选 canonical object 的 SHA-256、sort code、最后一行的有序 sort values/ID 和 `expiresAtMs`；有效期固定 24 小时，解码后 payload 上限 4 KiB，请求中整个 cursor 上限 8 KiB。签名常量时间比较，版本/过期/签名/operation/筛选不匹配统一返回 `400 INVALID_CURSOR`。唯一例外是下文默认核心影响预览：签名与 operation 有效，但它绑定的完整影响输入已变时返回 `409 IMPACT_PREVIEW_STALE`。签名不是加密，payload 不得放宿主路径、secret 或用户文件名；客户端也不得解析其内部字段。

列表 query 名固定为 camelCase，除下述例外外 `limit` 默认 50、范围 `1..100`；`q` trim 后最多 200 Unicode code points。`/admin/imports` 与 `/admin/reviews` 为滚动工作队列，`limit` 默认 20、范围 `1..20`，单次不得返回更多条目。未列 query/枚举返回 `400 INVALID_QUERY`，不能静默忽略。前端把筛选和 sort 写入页面 URL；API cursor 只用于翻页，不写作可分享筛选状态。

| 端点 | 一期 query |
| --- | --- |
| `/games` | `q`、`tagId`、`platformId`、`platformInstanceId`、`sort=RECENT_DESC|ADDED_DESC|TITLE_ASC`（默认 `RECENT_DESC`）、`cursor/limit`；`limit` 默认 50。 |
| `/saves` | `q`、精确 `gameId`、`platformId`、`platformInstanceId`、`coreId`、`availability=AVAILABLE|BLOCKED|ALL`、`sort=CREATED_DESC|GAME_TITLE_ASC`、`cursor/limit` |
| `/admin/imports` | `q`、`state`、`platformInstanceId`、`sort=UPDATED_DESC|CREATED_DESC`、`cursor/limit` |
| `/admin/reviews` | `q`、`importJobId`、`pegasusImportId`、`emulationStationImportId`、`platformInstanceId`、`blockerCode`、`sort=UPDATED_ASC|UPDATED_DESC`、`cursor/limit`；三个来源批次筛选至多一个非空。 |
| `/admin/review-history` | `q`、`decision=APPROVED|DISCARDED`、`platformInstanceId`、可空 `fromAtMs/toAtMs`、`sort=DECIDED_DESC|DECIDED_ASC`、`cursor/limit` |
| `/admin/games` | `q`、`platformId`、`platformInstanceId`、`status=PUBLISHED|DELETED|ALL`、`sort=TITLE_ASC|UPDATED_DESC`、`cursor/limit`；列表项同时返回 `releaseYear`、`metadataComplete` 与目录默认核心当前 `runtimeStatus`，供管理列表健康摘要与筛选使用。 |
| `/admin/platform-instances` | `platformId`、`enabled`、`sort=SORT_ORDER_ASC|NAME_ASC`、`cursor/limit` |
| `/admin/users` | `q`（1..80 code point）、`role=ADMIN|USER`、`status=ENABLED|DISABLED|DELETED|ALL`、`sort=CREATED_DESC|USERNAME_ASC|LAST_LOGIN_DESC`、`cursor/limit` |
| `/admin/invitations`、`/admin/users/{userId}/password-reset-links` | `state=ACTIVE|CONSUMED|REVOKED|EXPIRED|ALL`、`cursor/limit`；默认 ACTIVE |
| `/admin/bios` | `platformId`、`coreId`、`coreArtifactId`、`scope=REQUIRED_BY_LIBRARY|FULL_CATALOG`、`status`、`cursor/limit` |
| `/admin/bios/{requirementId}/entries` | 无 query；只读当前 active `DAT_MACHINE` installation 的持久化归档条目对比 |
| `/admin/storage-analysis` | 无 query；只读已登记 CAS payload 容量快照；写操作使用独立 `/admin/storage-cleanups` 资源 |

`platformInstanceId` 与 `platformId` 同时出现时必须验证目录属于该平台；`fromAtMs <= toAtMs`。`q` 使用数据模型定义的 `strings.ToLower + unicode.IsSpace` 折叠算法并以 SQLite `instr(search_text, :q)` 匹配；不使用仅 ASCII 的 `NOCASE`，也不把用户输入当 LIKE pattern。排序和 cursor 均以数据库值加 ID 完成，不能先分页再在 Go 内存二次筛选。

`GET /api/v1/admin/imports` 的 cursor 绑定筛选及 `UPDATED_DESC|CREATED_DESC` 排序，每页最多 20 条。每个列表项返回冻结配置中的 `contentMode`，缺省历史配置投影为 `STANDARD`；同时返回 `failedItemCount`、`rejectedFileCount`、`unresolvedRejectedFileCount`、`alreadyImportedItemCount` 与 `alreadyImportedFileCount`。前三项分别表示 Item 失败、分组前未被接受的 UploadFile 总证据和其中尚未通过重新配置任务接管的数量，后两项表示识别阶段因已有未删除游戏使用完全相同内容而跳过的 Item/不同 UploadFile。任务页当前异常总数必须为 `failedItemCount + unresolvedRejectedFileCount`，已导入跳过不计异常并单独解释。Import 详情把参与跳过的 `fileOutcomes[].disposition/reasonCode` 投影为 `ALREADY_IMPORTED`，同时返回 `alreadyImportedMatches[{importItemId,contentIdentityDigest,existingGame{id,title,platformInstanceId,platformInstanceName}}]`；对 MULTI 任务还返回 `itemSummaries[{itemId,state,contentKind,playlist,discCount,presentDiscCount,missingDiscCount,ignoredFileCount,ignoredFiles}]`。其中 `ignoredFiles` 只含同目录未引用文件按相对路径排序后的前 20 个 basename，计数不截断；这些详情不得暴露 Blob ID 或宿主路径。数据库原始 ImportJobFile 仍保留 `SOURCE`。零 Item 且存在未解决拒绝文件的 ImportJob 必须直接聚合为 `PARTIAL_FAILURE`，不得停留在 `RUNNING`；零 Item 且全部文件均为可忽略系统边车，或全部拒绝文件已成功转入 replacement ImportJob 时直接为 `COMPLETED`。所有识别出的 Item 都因已导入而跳过且没有拒绝文件时也直接 `COMPLETED`。

`GET /api/v1/admin/reviews` 只返回 state=`REVIEW_PENDING` 且已满足来源交接门禁的 ImportItem，每页最多 20 条；`importJobId` 精确绑定普通导入，`pegasusImportId` 与 `emulationStationImportId` 分别绑定已完成审核交接的服务器批次，三者进入 cursor filter canonical object且互斥，不存在的批次返回空列表而不回退到全局队列。EmulationStation 内部 Item 从原子创建起携带不可变交接预留；在来源 Item attach 且进入 `REVIEW_PENDING` 前，即使普通 Item 已经待审核，也必须从全局/筛选列表、详情、批量预览/创建、Approve、Discard 与待审核统计中隐藏或拒绝。与 Pegasus/EmulationStation Item 已关联但仍处于复制或校验阶段的内部 Item 同样必须隐藏，不能提前审核。每个 `items[]` 固定包含 `itemId/reviewVersion/importJobId/sourceDisplayName/draftTitle/platformInstance{id,name}/validationStatus/validationJobId/blockerCodes/candidateCount/sourceTotalSizeBytes/sourceMd5/coverUrl/sourceKind/sourceLabel/pegasusImportId/emulationStationImportId/updatedAtMs`。`sourceKind=STANDARD|PEGASUS|EMULATIONSTATION`；服务器来源 `sourceLabel` 是 Collection 展示名，其他来源为 null，两个来源 import ID 不能同时非空。`sourceTotalSizeBytes` 是 Item 全部 source file Blob size 的非负总和；`sourceMd5` 优先取 CONTENT、再取 DOS_SOURCE/COMPANION 的首个文件，无法取得时为 null；`coverUrl` 优先取草稿已选人工封面、再取已选 READY 候选封面、再取已复制的来源 COVER，值为 `/api/v1/admin/review-assets/{assetId}` 或 `/api/v1/admin/review-assets/{sourceRefId}?kind=COVER`，没有时为 null。`validationStatus` 是队列投影枚举 `READY|BLOCKED|INCOMPATIBLE|NEEDS_VALIDATION`；`candidateCount` 只统计本 Item 已完成 Run 的候选，服务器 source metadata 独立于该计数。列表不内嵌完整候选、媒体或 source manifest。

`GET /api/v1/admin/reviews/{importItemId}` 的 `scrapeRuns` 按 `createdAtMs,id` 倒序返回最近 10 个独立批次；每项固定含 `scrapeRunId/jobId/provider/state/jobState/createdAtMs/completedAtMs/errorCode/evidenceCount/attemptCount/candidateCount/outcomes`，其中 `outcomes={hit,miss,rateLimited,timeout,invalidResponse,networkError}` 按该 run 的 QueryAttempt 计数。`candidates` 仍只返回 COMPLETED run 的候选及媒体；`uploadedAssets` 返回该 Item 的不可变人工审核媒体。服务器来源另返回可空 `sourceMedia={sourceKind:"PEGASUS"|"EMULATIONSTATION",sourceRefId,pegasusImportId,emulationStationImportId,sourceLabel,coverUrl,coverWidthPx,coverHeightPx,videoUrl,sourceFlags?}`；两个 import ID 互斥，EmulationStation 的 `sourceFlags={hidden,adult,kidGame}`，URL 使用受保护审核媒体路由，缺失单项为 null。详情另返回可空 `runtimeScreenshot={screenshotId,validationId,coreArtifactId,widthPx,heightPx,capturedAfterMs:5000,capturedAtMs,url}`；只有 Validation 仍匹配当前来源快照、目标平台、CoreArtifact 与 prepublish 输入时才投影，Validation 可以是 READY 或阻断状态。当前 artifact 还没有 Validation、但同一来源快照和目标目录存在历史 Validation 时，详情必须投影最近一条历史记录并返回 `validationStale=true`，不能误报为从未检查；如果历史与当前 artifact 的 `runtime_version` 不同，同时返回 `runtimeVersionChange={previous,current}`，否则该字段为 null。READY Validation 或当前 `runtimeScreenshot` 任一存在时 `canApprove=true`；截图随来源、目标或核心漂移而失效，不能继续解锁发布。`sourceFiles` 按 UploadFile 投影 name/size/SHA-256/MD5/CRC32；若来源是已支持归档则 `archive=true` 并返回有界导入时已解析的 `archiveEntries[{name,sizeBytes,crc32}]`，不会在 GET 时重新解压。识别同时覆盖“从归档中物化单成员”的来源和直接作为运行内容的完整 Arcade/DOS ZIP；后者依据 UploadFile 最终 Blob 已存在的 `archive_entries` 返回成员列表，不能因 `source_archive_blob_id` 为空而漏报。详情还必须返回当前 `contentIdentityDigest` 与 `duplicateGames[{gameId,title,platformInstanceId,platformInstanceName}]`；后者只含同基础平台、当前 `PUBLISHED` 且 current ContentRevision 文件集合完全相同的 Game，空集合返回 `[]`。

错误统一为：

```json
{
  "error": {
    "code": "LAUNCH_BIOS_MISSING",
    "message": "缺少启动所需 BIOS",
    "details": { "filenames": ["neogeo.zip"] },
    "requestId": "0198..."
  }
}
```

状态码固定为：语法/字段错误 `400`，缺少或错误 launch credential `401`，已识别但无权访问 `403`，不存在 `404`，资源版本/状态冲突 `409`，过大 `413`，非法 Range `416`，可修复业务阻断 `422`，缺少并发前置条件 `428`，应用限流 `429`，未就绪 `503`，未知错误 `500`。除第 9 节定义的健康响应外，客户端不得解析 `message` 决策。

全局 readiness 为 false 时，前置 middleware 必须在 validator、body 读取、静态 runtime 和领域 handler 之前阻断除 `/health/live`、`/health/ready` 外的所有后端路由，返回 `503` 标准错误 envelope：`code=SERVICE_NOT_READY`，`details={"reasonCode":"<与 ready 相同的稳定枚举>"}`。健康端点仍使用第 9 节的专用非 envelope 外形；前端只能根据 code/reasonCode 展示稍后重试，不得绕过门禁调用部分管理能力。

`sourceFiles[]` 对归档来源额外返回 `archiveFormat: ZIP|SEVEN_Z`；非归档为 null。审核 UI 必须明确区分“来源 ZIP/7z”与“物化后的运行 CONTENT”，不能根据文件扩展名猜测，也不能在详情 GET 时重新解析 archive。

## 2. 认证、授权与同源写入

`GET /api/v1/auth/context` 是页面 bootstrap 的唯一事实源且永不返回 401。它只返回 `instanceState=INITIALIZATION_REQUIRED|READY`、`mode=release|test`、`authenticationState=NOT_APPLICABLE|UNAUTHENTICATED|AUTHENTICATED`、可空 User、可空 CSRF/idle/absolute expiry 和 `testDefaultAccountActive`。合法组合只有：INITIALIZATION_REQUIRED+NOT_APPLICABLE+全空、READY+UNAUTHENTICATED+全空、READY+AUTHENTICATED+全非空。

无需有效 AuthSession 的路径仅有 health、auth context、release initialize、login/logout、account-link inspect、Invitation accept、PasswordReset complete、固定 runtime allowlist 和已经由 launch cookie 限定的 `/runtime/launches/**`。其余 `/api/v1/home|recent-games|games*|saves*|launches` 及 `/content/**` 要求已登录；`/api/v1/admin/**` 另要求 `role=ADMIN`。普通 USER 访问管理 API统一 `403 ADMIN_REQUIRED`；任意账号访问他人的 SaveState、截图、Launch 或私有 cursor 统一 404/不可用，管理员没有 owner bypass。

AuthSession cookie 在 HTTP 开发环境名为 `retrom_session`，HTTPS 为 `__Host-retrom_session`，固定 `Path=/; HttpOnly; SameSite=Strict`，HTTPS 另有 `Secure`；idle 8h、absolute 24h。数据库只存 token SHA-256。登录失败和 DISABLED/DELETED 账号统一 `401 AUTHENTICATION_FAILED` 与文案“用户名或密码不正确”。logout 是幂等例外：无效/缺失 cookie 不要求 CSRF也返回 204；有效 session必须通过 CSRF，成功撤销并发送过期 cookie及 `Clear-Site-Data: "cache", "cookies", "storage"`。

所有非安全方法先校验 `Origin` 精确等于 `RETROM_PUBLIC_ORIGIN`，拒绝缺失、`null`、多值和 Referer fallback；`Sec-Fetch-Site` 出现时只接受 `same-origin`，`cross-site/same-site/none` 均拒绝。除初始化/登录/链接消费等公开 capability 写入及 launch runtime 外，已登录写请求还必须携带当前 session context 给出的 `X-Retrom-CSRF`。CSRF 只保存在内存，不进入 cookie、URL、Web Storage 或日志。Go API 与 Next.js 动态 HTML 都必须发送 `Referrer-Policy: no-referrer`；API 不返回宽松 CORS header，CORS 也不替代这些校验。

登录按规范用户名和规范客户端 IP分别限 5/30 次，初始化 IP 5 次，链接检查/消费 IP 20 次；窗口和 block均 15 分钟，超限返回 429 与 `Retry-After`。subject 用实例 HMAC 后入库。直接 peer 只有命中 `RETROM_TRUSTED_PROXIES` 规范 CIDR时才读取单个 X-Forwarded-For，从右向左跳过受信代理并取首个不受信地址；超过 16 项、缺失或任一项非法回退 peer IP并只记录稳定诊断码，不读取 X-Real-IP/Forwarded，也不记录原链。

release 密码分别做 NFC 但不 trim，最少 6 个字符且不超过 128 个 Unicode code point/512 bytes，拒绝控制字符，并拒绝固定 10,000 行常见密码列表以及与用户名、显示名称或 `retrom` 相同的 Unicode case-fold 值。存储使用严格 `ARGON2ID_V1` PHC：`$argon2id$v=19$m=19456,t=2,p=1$<16-byte-salt>$<32-byte-hash>`；最多并行执行 4 个 Argon2 计算。`--mode=test` 自动创建的 `test/test` 是唯一豁免，用户修改密码时必须立即满足 release 规则。

### 2.1 初始化、邀请与密码重置

- `POST /api/v1/auth/initialize` 只接受 release+PENDING、同源 Origin、43 字符 setup code、合法用户名/显示名称和确认后的 release 密码。成功原子创建唯一 ADMIN/Profile/Credential、完成 InstanceState、写审计并签发 session；错误证明零写入，重复/并发初始化冲突。
- Invitation/PasswordReset capability 固定为 64 字符，由公开 link ID 与实例 key 的 domain-separated MAC组成，只通过 `/register#invite=...` 或 `/reset-password#reset=...` URL fragment传递。前端首个 effect读取后立即 `history.replaceState` 清 fragment，token只在组件内存存在。
- `POST /api/v1/auth/account-links/inspect` 只向有效持有者返回 kind、邀请 role或重置目标 username、到期时刻；无效、过期、消费、撤销、kind错和 MAC错统一 404。
- ADMIN 创建 Invitation必须有 `Idempotency-Key`，role为 USER时 `confirmAdminRole=false`，ADMIN时必须 true。创建/同 key replay可返回同一完整 URL；列表仅返回非秘密元数据。Invitation 与 PasswordReset创建后 1h、消费或撤销后失效，并发消费最多一次成功。
- PasswordReset创建使用目标 User最新 ETag并递增其 version，同时撤销其他 ACTIVE reset。完成时撤销旧 session并更新密码；目标 ENABLED则签发新 session并返回 AuthContext，目标 DISABLED只返回 `{"status":"PASSWORD_CHANGED_ACCOUNT_DISABLED"}`，绝不重新启用。

### 2.2 用户管理

`GET /api/v1/admin/users` 支持 `q`、`role=ADMIN|USER`、`status=ENABLED|DISABLED|DELETED|ALL`、`sort=CREATED_DESC|USERNAME_ASC|LAST_LOGIN_DESC` 和 `cursor/limit`。User DTO只含 `userId/username/displayName/role/status/version/createdAtMs/lastLoginAtMs/activeSessionCount`；DELETED item 的 `displayName` 固定为“已删除用户”。不得返回 Profile ID、私有游戏/时长/存档、IP、hash、session ID或 Credential。detail 与 PATCH返回最新 ETag。

PATCH 至少修改 role/status之一，升为 ADMIN需 `confirmAdminRole=true`；DELETE是要求输入完整 username的不可逆软删除。两者都需要当前 ETag、Idempotency-Key、Origin和 CSRF。当前登录管理员不能修改自身 role/status或删除自己；任一动作提交后必须保留一名启用 ADMIN。停用/删除在同一事务撤销 AuthSession、ACTIVE account link和待用/活动 Launch，但保留 Profile与私有数据且不向管理员开放。删除后 username不可复用。

邀请列表与目标 User的 reset列表按 `(createdAtMs DESC,id DESC)` cursor分页，state只允许 `ACTIVE|CONSUMED|REVOKED|EXPIRED|ALL`，item不含 URL/token或前后缀。按 link ID撤销只接受当前 ACTIVE与最新 ETag；同 principal/operation/key replay仍成功，其他重复撤销统一 `409 ACCOUNT_LINK_NOT_ACTIVE`。

HTML 响应使用逐响应随机 nonce，Next.js framework/bootstrap script 与 Retrom 自有 inline script（如有）必须携带该 nonce；不得退化为全局 `script-src 'unsafe-inline'`。实现必须使用 Next.js 16 根级 `web/proxy.ts`：为每个 HTML navigation 生成至少 128-bit CSPRNG nonce，把含相同 nonce 的 CSP 同时写入转发给 App Router 的 request header 和最终 response header，使 Next.js 能给 framework script 自动附 nonce。使用 nonce 的页面强制动态渲染，不使用 static export、ISR、PPR 或共享 HTML cache；静态 asset/API/runtime 不进入该 proxy matcher。NG 只能原样保留，不能生成第二个不一致 CSP。

Player/应用文档的生产 CSP 固定为下列能力；EJS 配置放在已打包的同源 adapter 中。启动点击先使当前 document 成为全屏 owner，再用 Next.js App Router `replace` 在同一根 layout 内软导航到 `/play/:launchId` 并直接渲染 Player。不得使用顶层完整 navigation 或再嵌套一份 Next.js Player document：前者会让浏览器退出所有 core 的全屏，后者会建立第二条 HMR/路由生命周期并可能重载顶层页面。为使来源页与 Player 页在软导航前后具有同一 CSP，所有应用 HTML 的 `frame-src` 都从服务端配置的 `RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE` 推导唯一受控 hostname family；模板必须是 `{launchId}` 独占首个 host label、无 path/query/fragment/userinfo，否则 fail closed 为 `'self'`。普通本机开发 family 为 `http://*.rpg.localhost:8080`，PFB family 为 `http://*.rpg.<pfb-id>.localhost:3000`，生产 family 来自部署者配置的 HTTPS runtime domain。这个 wildcard 只允许浏览器建立 frame，runtime 服务仍逐请求校验 exact Launch host、app/runtime capability、cookie、父 origin 与 route，未知、过期、错误 host 均返回 404/410。`style-src 'unsafe-inline'` 是 EmulatorJS v4.2.3 当前 inline style 的受控例外，不能顺带放宽 script：

```text
default-src 'self';
base-uri 'none'; object-src 'none'; form-action 'self'; frame-ancestors 'self';
script-src 'self' 'nonce-<per-response>' blob: 'wasm-unsafe-eval';
style-src 'self' 'unsafe-inline';
connect-src 'self' blob:; worker-src 'self' blob:; frame-src 'self' https://*.rpg-runtime.<app-domain>;
img-src 'self' data: blob:; media-src 'self' blob:; font-src 'self' data:
```

`blob:` script/worker 与 `'wasm-unsafe-eval'` 分别用于 v4.2.3 动态 core glue/worker 和 WebAssembly 编译；生产不允许任意 scheme/CDN、`unsafe-eval` 或受控 runtime family 以外的外部 frame。官方 v4.2.3 `extract7z.js` 与 `extractzip.js` 的旧 Emscripten `eval` 不能成为放宽 CSP 的理由；固定 Player adapter 在创建 7z/ZIP Worker Blob 前执行运行时专题规定的精确、fail-closed 兼容转换，官方物化 bytes 保持不变。Next.js 开发模式因 React 调试代码确实需要 eval，`web/proxy.ts` 只在 `NODE_ENV=development` 向 `script-src` 追加 `'unsafe-eval'`；production build/E2E 必须断言它不存在。非 HTML 静态/runtime/content 响应无需重复 nonce CSP，但必须带相应 CORP/COEP/`nosniff` 头，且不能覆盖顶层文档的隔离策略。实现依据固定为 [Next.js 官方 nonce CSP 指南](https://nextjs.org/docs/app/guides/content-security-policy)；不得套用旧版 `middleware.ts` 示例。

## 3. 乐观并发与幂等

- 可编辑资源响应包含 `version: integer` 和 `ETag: "v<version>"`。`PATCH`、状态转换和删除必须携带 `If-Match`；缺少为 `428 PRECONDITION_REQUIRED`，一般资源不匹配为 `409 VERSION_CONFLICT`。User 与 AccountLink 的管理写入是显式例外，不匹配返回 `412 RESOURCE_VERSION_CONFLICT`。
- 创建上传、上传终结、ImportJob、Launch、账号管理、可能投递兼容性任务的游戏移动预览、游戏永久删除、审核通过/Discard 等可能被重试的写操作必须携带规范小写 UUIDv4/UUIDv7 `Idempotency-Key`。服务端按 `principal + operationId + key` 保存语义请求摘要和结果 24 小时；同一账号同 key/同语义请求返回原 status/body 及白名单响应头，不同请求返回 `409 IDEMPOTENCY_KEY_REUSED`，跨账号使用同 key 是独立命名空间。白名单只含 `Content-Type/Location/ETag/Retry-After`，绝不持久化 `Set-Cookie`、认证 header、密码或任意 capability；Launch 与一次性链接 replay 按服务端 key/公开 ID重新派生同一 secret 响应。
- 状态转换在单个短事务中同时写资源、不可变事件和 outbox/job 记录；重复请求不得重复发布、重复引用 Blob 或重复 ReviewEvent。

语义请求摘要固定为 lowercase hex SHA-256(RFC 8785 canonical JSON)：object 包含 `operationId`、按 OpenAPI 名排序的规范 path/query 参数、可空 `If-Match`、规范 media type，以及 body 表示；不包含 cookie、`Idempotency-Key`、request ID 等非业务 header。普通 JSON body 在严格解析后以 canonical JSON 嵌入，空 body 为 `null`。runtime SaveState streaming operation 嵌入 canonical metadata、两个 part 的 media type/length/SHA-256。Upload part 不写 idempotency record，它按路径中的 upload/file/part、Content-Range 和声明/实际 digest 使用自身永久唯一规则。服务端必须先完成有界流式接收与摘要，再在一个 `BEGIN IMMEDIATE` 短事务中检查记录，并把领域变更、不可变事件和 COMPLETED idempotency record 一起提交；事务前产生但未引用的 CAS Blob 交给 GC。这样并发相同请求只有一份领域结果，不需要持有事务读取大 body，也不存在“已保存响应但领域事务回滚”的窗口。24 小时后相同 key 可视为新请求；永久唯一性仍由领域约束保证，不能依赖幂等记录充当数据库约束。

## 4. 浏览器文件与目录上传

一期只接受浏览器选择的文件/目录，不提供“输入服务器绝对路径导入”。Chrome 目录选择使用 `<input type="file" webkitdirectory multiple>`；客户端只发送 `File.webkitRelativePath`。拖放目录可作为增强能力，失败时必须回退到目录选择器。

### 4.1 创建上传会话

```http
POST /api/v1/admin/uploads
Idempotency-Key: <uuid>

{
  "sourceType": "DIRECTORY",
  "files": [
    {"clientFileId":"f1","relativePath":"GAME/DOOM2.WAD","sizeBytes":14604584}
  ]
}
```

路径使用 `/`，必须是非空相对路径；拒绝 `.`/`..` 段、绝对/drive/UNC 路径、NUL、控制字符和规范化后重复路径。服务端生成不透明 `fileId`，不能信任 `clientFileId` 作为物理路径。

默认硬限制（可通过配置调低，调高需同步安全验收）：单文件 8 GiB、会话总量 32 GiB、10,000 文件、路径 UTF-8 1,024 bytes、8 MiB 分块（最后一块除外）、未完成会话 24 小时过期。COMPLETE 且未消费的 session 保留 7 天；file-level 消费后未被消费的同会话文件在 24 小时后裁剪，whole-session 消费则保留全部文件证据；精确行/Blob 规则以数据字典为准。归档另限 20,000 entries、单 entry 8 GiB、总展开 32 GiB、压缩比 200:1；先触及任一限制即拒绝。

### 4.2 分块、恢复与完成

| 方法与路径 | 语义 |
| --- | --- |
| `GET /api/v1/admin/uploads/{uploadId}` | 返回会话、文件、已接收 part bitmap 和过期时间，用于断点恢复。 |
| `PUT /api/v1/admin/uploads/{uploadId}/files/{fileId}/parts/{partNo}` | body 是单个分块；必须携带精确 `Content-Range` 与 RFC 9530 `Content-Digest: sha-256=:base64:`。允许乱序；同 part/同 digest 幂等，内容不同冲突。正常只接受 CREATED/UPLOADING；若终结以 `UPLOAD_PART_MISSING|UPLOAD_PART_CORRUPT` 失败，FAILED session 只允许重传错误明细列出的 part，并回到 UPLOADING。 |
| `POST /api/v1/admin/uploads/{uploadId}/complete` | 必须携带当前 `If-Match` 和 `Idempotency-Key`。短事务把 `finalizationNo` 加 1、冻结本次 part 输入，创建该编号唯一的 kind=`UPLOAD_FINALIZE`/scope=`UPLOAD_SESSION` Job，更新 session 当前 job 并置 session/未完成 file 为 `FINALIZING`，返回 `202 {"uploadId":"...","jobId":"...","finalizationNo":1,"state":"FINALIZING"}`。Worker 在事务外校验每个 byte 恰好出现一次，流式组装并从实际 bytes 计算最终 hash/CAS；全部成功才置 `COMPLETE`。同一次的可重试 I/O 故障使用该 Job retry；缺失/损坏 part 修复后必须以新 key 再次 complete，创建递增编号的新 Job 并保留旧 Job/事件。同一个已完成文件不重组装。 |
| `DELETE /api/v1/admin/uploads/{uploadId}` | 必须携带当前 `If-Match`。没有任何 consumption 且未运行终结时同步置 `CANCELLED` 并返回 204；FINALIZING 时只把 Job 转 `CANCEL_REQUESTED`、返回 `202 {"status":"CANCELLATION_REQUESTED","jobId":"..."}`，session 在 Worker 有界停止/清理前保持 FINALIZING 并带 `cancelRequested=true`。已被 Import/Game/BIOS/DAT/Asset 使用时返回冲突。 |

Upload 状态仅为 `CREATED | UPLOADING | FINALIZING | COMPLETE | FAILED | CANCELLED | EXPIRED`。客户端声明的 size/digest 只用于传输校验，CAS hash 必须由服务端从实际 bytes 计算。只有 `COMPLETE` upload 才能创建 ImportJob 或被其他领域消费；前端必须通过 Job SSE/快照等待，不得在 202 后立即创建 Import。

上传 part 有 `Content-Length` 时先校验其与 `Content-Range`/8 MiB 上限一致；HTTP/2 未提供该 header 时仍允许流式接收，读到 range 声明长度或上限后的第一个多余 byte 立即以 `400 UPLOAD_RANGE_MISMATCH` / `413 UPLOAD_PART_TOO_LARGE` 终止并删除临时 part。不能因为 NG buffering 或客户端 header 缺失而无界读入内存。

## 5. Import、审核与 SSE

创建导入：

```http
POST /api/v1/admin/imports

{
  "uploadId": "0198...",
  "targetPlatformInstanceId": "0198...",
  "metadataProvider": "HASHEOUS"
}
```

`metadataProvider` 仅允许 `HASHEOUS | NONE`；Arcade DAT 不是 provider。`contentMode=RPG_MAKER_PROJECT_V1` 没有单 ROM 哈希语义，用户只选择 `rpgmaker` 虚拟核心且禁用在线刮削：客户端固定显示并提交 `NONE`，服务端也把旧客户端提交的 `HASHEOUS` 规范化为 `NONE`。

创建端点只执行有界准入：校验 UploadSession 已 `COMPLETE`、用途/来源类型、目标目录与当前候选 artifact 能力，随后在一个短事务创建 `ImportJob(state=QUEUED)`、`IMPORT_GROUP Job`、每个 UploadFile 的 `PENDING` disposition、whole-session consumption 以及不可变 `import_group_requests` 输入快照，并立即返回 `202 {importJobId,jobId,state:"QUEUED",itemCount:0}`。浏览器收到 202 后直接进入 `/admin/imports/tasks`；不得继续把上传进度停在 92% 等待项目识别。

归档安全扫描、解压、实际 member hash、CAS 物化、RPG Maker/ONS/KiriKiri/Butterscotch 项目检测、分组和运行依赖检查全部由并发度 1 的 `IMPORT_GROUP` archive worker 在 HTTP 响应之后执行。ZIP 在完整验证 central directory 后逐 member 单次解压，并把选中的 member 从临时候选原子提交到 CAS；不得为“扫描 hash”和“物化 CAS”再次解压同一 member，也不得把被规范器排除的候选发布进 CAS。7z 继续使用隔离 worker 的完整扫描与受限批量提取。任务以 `QUEUED/STARTED/PROGRESS/SUCCEEDED|FAILED` 事件投影 `WAITING_FOR_WORKER/INSPECTING/PERSISTING` 阶段；重启恢复遗留 RUNNING，取消在安全检查点收口并释放上传消费。

请求 JSON、Upload 状态/用途、目标目录或 capability 在短准入阶段无效时仍在创建前返回 4xx。必须读取项目内容才能确定的错误（归档加密/越限/路径冲突、项目根缺失或歧义、RPG 世代不兼容、多盘播放列表缺失等）在 202 后把 ImportJob/Job 置 `FAILED`，并通过列表/详情的 `lastErrorCode/errorCode` 和事件返回稳定错误码；不得让原 POST 长时间占用连接。RPG Maker worker 从项目 bytes 唯一检测世代后，才把冻结的虚拟 core 绑定解析为内部 core/artifact；`bindingState=PENDING` 时详情的 `coreArtifactId` 必须为 null，不能暴露为了满足数据库外键而暂存的内部候选。

重新配置导入：

```http
POST /api/v1/admin/imports/{importJobId}/reconfigure
If-Match: "v3"
Idempotency-Key: 0198...

{
  "targetPlatformInstanceId": "0198...",
  "metadataProvider": "HASHEOUS"
}
```

source ImportJob 必须为当前 `PARTIAL_FAILURE`，且至少有一个尚无 resolution 的 REJECTED ImportJobFile。服务端只克隆这些待处理文件的逻辑 UploadFile 行并复用相同 final Blob，创建新的 COMPLETE UploadSession 后按请求配置执行普通 Import 创建；不能复制 CAS bytes、消费原 UploadSession 或把旧 disposition 改成 SOURCE。replacement ImportJob 返回 `202` 与 `Location`，并以 `reconfiguredFromImportJobId` 回指 source；source 详情的对应 `fileOutcomes[].resolution` 返回 `action=RECONFIGURED/replacementImportJobId/resolvedAtMs`。replacement 创建、全部 resolution、source `resolvedRejectedFileCount/version/state` 与 lineage 必须在同一短事务提交；任何版本漂移或文件已被接管均返回 `409 IMPORT_RECONFIGURE_CONFLICT`，不得留下部分 resolution。重新配置仍执行完整 archive/platform 校验，不提供安全检查 override。

进度端点为 `GET /api/v1/admin/imports/{id}/events`，`Content-Type: text/event-stream`。它按全局 JobEvent ID 合并该 ImportJob 的 IMPORT_GROUP 事件，以及经 `ImportItem.import_job_id` 归属的全部 IMPORT_ITEM_PIPELINE/METADATA_SCRAPE/MEDIA_FETCH 事件；不把其他 Import 的事件混入。每个持久事件使用递增数据库 event ID、`event:` 类型和 JSON `data`；客户端重连携带 `Last-Event-ID`。首次连接没有该 header 时，服务端先发一个 `event: snapshot`，其 `id` 是事务内读取任务快照时看到的全局最大 JobEvent ID（空库为 `0`），`data` 是与 `GET ImportJob` 相同的当前摘要，随后只推送更大 ID 的归属事件，避免 snapshot/subscribe 竞态。`Last-Event-ID` 只接受 `0..当前全局最大 ID` 的十进制整数，其他值返回 `400 INVALID_EVENT_CURSOR`；它可以对应其他 scope，过滤仍按 `id > cursor` 执行。服务端每 15 秒发送无 `id` 的 comment heartbeat。snapshot、事件 batch 与 heartbeat 每次写入/flush 前都通过 `http.ResponseController` 把 write deadline 刷新为当前服务端时刻 + 30 秒；只有精确的 `errors.ErrUnsupported` 可按不支持 deadline 处理，其他 SetWriteDeadline、Write 或 Flush 错误都立即结束本连接且不取消底层 Job。

一期 JobEvent 与 JobInputSnapshot 一样永久保留，不实现后台裁剪，因此不存在一边声明 append-only、一边删除事件的隐藏特权路径，也不返回 `EVENT_CURSOR_EXPIRED`。将来若数据库增长需要保留窗口，必须先增加显式 prune watermark/migration、API 过期语义和恢复测试，不能直接 `DELETE` 后让全局 ID/cursor 失真。SSE 断开不取消任务。

通用 Job 的 `GET /api/v1/admin/jobs/{jobId}/events` 使用同一套全局 JobEvent cursor 规则，但只过滤 `job_id` 精确等于路径资源的事件。无 `Last-Event-ID` 时，服务端在一个只读事务中取得与 `GET /api/v1/admin/jobs/{jobId}` 相同的 Job 快照和当时全局最大 JobEvent ID，先发送 `event: snapshot`，其 `id` 为该全局水位、`data` 为 Job 快照；随后只发送 ID 更大且属于该 Job 的持久事件。重连时可使用属于其他 scope/job 的合法全局 ID 作为水位，仍只按 `id > cursor AND job_id = :jobId` 过滤；负数、非十进制整数或超过当前全局最大值统一为 `400 INVALID_EVENT_CURSOR`。事件 JSON、无 ID 的 15 秒 comment heartbeat、永久保留和“断开不取消”语义与 Import SSE 完全相同。Launch、游戏移动和其他等待共享 `VARIANT_REVALIDATE` 的前端必须使用这条协议，不能轮询一套含不同终态或取消语义的本地状态机。

审核草稿 `PATCH /api/v1/admin/reviews/{id}` 使用 `If-Match`；通过与 Discard 分别为 `/approve`、`/discard`，必须有 Idempotency-Key。Approve 普通 body 可为 `{}`；Review ETag 或当前 Validation/来源证据漂移返回 `409 REVIEW_VALIDATION_STALE`。审核客户端收到该冲突后必须重新 GET Review；仅当目标目录、发布字段、素材选择、DOS entry 与标签集合和当前页面完全一致，最新 Review 又允许发布且没有 active Attachment 时，才可用新 ETag、新 Idempotency-Key 自动重试一次。字段发生并发变化、补传尚未完成、最新 Validation 不可发布或第二次仍冲突时必须停止并要求人工核对，不能用重试绕过乐观并发。若当前有完全相同内容的未删除游戏则返回 `409 DUPLICATE_GAME_CONFIRMATION_REQUIRED`，`details={contentIdentityDigest,games}`。继续发布必须重交 `{"duplicatePolicy":"ALLOW_NEW","acknowledgedGameIds":["..."]}`，ID 集合与事务内重查的当前 games 完全一致才成功；新增、减少、重复或未知 ID 均不接受，确认写入 ReviewEvent。历史端点只读，不提供修改或删除事件 API。

### 5.1 快速审批

`GET /api/v1/admin/review-bulk-approval-preview` 接受审核列表同名的 `q/tagId/importJobId/pegasusImportId/emulationStationImportId/platformInstanceId/blockerCode`，三个来源批次筛选互斥，未知或重复 query 拒绝；不接受 sort/cursor/limit。它在一个只读快照中枚举完整筛选范围，返回规范 `scope`、`scopeDigest`、`candidateManifestDigest`、`generatedAtMs`、可空 `activeBulkApproval` 和 `counts={matched,strictReady,screenshotOnly,duplicate,attachmentActive,sourceFlagged,notReadyOrStale}`。`sourceFlagged` 只统计本可成为候选但来源 `hidden|adult=true` 的 EmulationStation Item，且不与其他排除桶重复；`strictReady` 不含这些来源项、阻断截图人工放行、重复、active Attachment 或任何过期输入。

`POST /api/v1/admin/review-bulk-approvals` 要求 ADMIN、同源/CSRF 与 Idempotency-Key，body 固定为 `{"scope":{...},"scopeDigest":"lowercase-sha256","candidateManifestDigest":"lowercase-sha256"}`。服务端在同一写事务重做预览；digest 不一致返回 `409 REVIEW_BULK_PREVIEW_STALE`，已有 active batch 返回 `409 REVIEW_BULK_APPROVAL_ACTIVE`，零 candidate 返回 `409 REVIEW_BULK_SCOPE_EMPTY`，超过 10,000 返回 `422 REVIEW_BULK_SCOPE_TOO_LARGE`。成功为 `202`，返回 aggregate summary、`ETag: "v1"`，并冻结每项 Review version/Validation/source snapshot 后启动 `REVIEW_BULK_APPROVE` Job。

`GET /api/v1/admin/review-bulk-approvals/{bulkApprovalId}` 返回 aggregate、初始分类和当前结果计数并携带 ETag。`GET .../{bulkApprovalId}/items` 以 opaque cursor、`limit<=50` 和可空 `outcome=PUBLISHED|SKIPPED_DUPLICATE|SKIPPED_CHANGED|SKIPPED_NOT_READY|FAILED_FINAL|CANCELLED` 分页，返回冻结标题/目录、结果码及可空 Game/ReviewEvent 链接。`POST .../{bulkApprovalId}/cancel` 使用 `If-Match`、Idempotency-Key 和 `{"reason":"..."}`，只停止未提交 Item；`POST .../{bulkApprovalId}/retry` 使用当前 ETag、Idempotency-Key 和 `{}`，只接受 `FAILED/REVIEW_BULK_WORKER_UNAVAILABLE`。这两个领域 action 不能替换为通用 Job cancel/retry。所有状态变化均保持已提交 Game 与 ReviewEvent，不提供批次回滚或批量 Discard。

### 5.2 Arcade Parent Attachment

Arcade Review GET 额外返回 `effectiveSourceSnapshotId` 和可空 `arcadeDependencies`。非 Arcade 为 null；Arcade object 固定含 `machine/status/compatibilityCode/nodes/activeAttachment`。每个 node 含 `kind/machine/requiredBy/depth/expectedLogicalName/state/requiredEntryCount/requiredEntries/canAttach/attachment`；BIOS/Base 另有站内 `managementUrl`。`canAttach=true` 仅限当前有效 Validation 的 Parent `MISSING/MISMATCH`，且 Item 可编辑、无 active Attachment、非 Merged/CHD/cycle/config stale。服务端只投影当前 Arcade V2 依赖，客户端不得自行推断 Parent 层级。

bytes 先使用第 4 节通用协议创建单文件 `sourceType=FILES` session、上传 parts、complete 并等待 UploadFile COMPLETE。随后创建业务 Attachment：

```http
POST /api/v1/admin/reviews/{importItemId}/arcade-parent-attachments
If-Match: "v7"
Idempotency-Key: 0198...
Content-Type: application/json

{
  "validationId": "0198...",
  "baseSourceSnapshotId": "0198...",
  "dependencyMachine": "b",
  "uploadFileId": "0198..."
}
```

服务端验证 Review/Item 状态、版本、Arcade 平台、Validation 对 Item/有效快照/目录版本/CoreArtifact/活动 DAT 的绑定、machine 是当前可修复 Parent、UploadSession/File 都 COMPLETE、文件为单个 `.zip`、不存在 whole-session consumption 且 CAS Blob 可读。成功返回 `202`、`Location: /api/v1/admin/jobs/{jobId}`、更新后的 Review ETag 和 `{attachmentId,state:"QUEUED",jobId}`。相同幂等键与完全相同请求返回同一结果；同键异 body 使用通用幂等冲突。每 Item 只有一个 active Attachment，服务端约束是最终防线。

进度只订阅 `GET /api/v1/admin/jobs/{jobId}/events`，事件序列可含 `QUEUED/STARTED/ARCHIVE_SCANNED/PARENT_MATCHED|PARENT_REJECTED/SOURCE_SNAPSHOT_CREATED/CORE_VALIDATION_COMPLETED/SUCCEEDED|FAILED|CANCELLED`。SSE 断线自动按通用协议重连，不取消 Job；终态必须重新 GET Review，并以返回的 Review version、有效来源快照和 Validation 作为后续 Approve 输入。若页面遗漏这次终态刷新，Approve 的通用 stale 恢复仍按上一段的严格等价检查有界处理。`FAILED_RETRYABLE` 使用通用 Job retry 并复用同一 UploadFile。Discard 使用通用 Job cancel 语义收口 Attachment；离开审核页不触发 cancel。

稳定错误如下，前端按 code/state 分支，中文 message 只用于显示：

| HTTP | code | 语义 |
| --- | --- | --- |
| 400 | `REVIEW_PARENT_UPLOAD_INVALID` | 请求、Upload/File 状态或 ZIP 类型无效 |
| 404 | `REVIEW_NOT_FOUND` | 审核项不存在 |
| 409 | `REVIEW_VERSION_CONFLICT` | Review ETag 已过期 |
| 409 | `REVIEW_PARENT_ATTACHMENT_IN_PROGRESS` | 同 Item 已有 active Attachment |
| 409 | `REVIEW_PARENT_INPUT_STALE` | Validation、来源快照、CoreArtifact 或 DAT 已漂移 |
| 409 | `REVIEW_ALREADY_FINALIZED` | Item 已发布、丢弃或取消 |
| 422 | `REVIEW_PARENT_NOT_REQUIRED` | machine 不是当前缺失/不匹配 Parent |
| 422 | `REVIEW_PARENT_ARCHIVE_UNSAFE` | ZIP 安全扫描失败；details 可含底层稳定 archive code |
| 422 | `REVIEW_PARENT_CONTENT_MISMATCH` | ZIP 安全但 entry/size/hash 不匹配 |
| 422 | `REVIEW_PARENT_STRUCTURE_UNSUPPORTED` | 没有可校验根级 ROM entry、CHD 或其他不可补传结构；安全 clone 子目录 extra 本身不触发此错误 |
| 503 | `REVIEW_PARENT_VALIDATION_UNAVAILABLE` | 可重试的存储或 worker 故障 |

### 5.3 多盘 Import 与 Review Attachment

`POST /api/v1/admin/imports` 与 `POST /api/v1/admin/games/{gameId}/content-revisions` 的可选 `contentMode` 只允许 `STANDARD|MULTI_DISC_M3U_V1|RPG_MAKER_PROJECT_V1|ONS_PROJECT_V1|KIRIKIRI_PROJECT_V1|BUTTERSCOTCH_PROJECT_V1|TYRANOSCRIPT_PROJECT_V1`，缺省严格为 STANDARD。MULTI 必须引用完整 DIRECTORY Upload，且 capability 由 feature flag、平台 profile 与当前 `selected_for_new_bindings` artifact 共同决定。首次独立 runtime 项目导入接受一个完整 DIRECTORY，或 FILES 中恰好一个 ZIP/7z；已发布 RPG 游戏的内容替换只接受完整 DIRECTORY，并由虚拟核心重新检测 generation，不接受客户端指定内部世代。新旧 generation 不同的 Job 以不可重试 `RPG_REPLACEMENT_GENERATION_MISMATCH` 失败；运行依赖声明变化以不可重试 `RPG_REPLACEMENT_DEPENDENCIES_CHANGED` 失败；两种失败均保留当前内容和存档。Review detail 的可空 `multiDisc` 只返回 playlist 摘要、ordered entries、PRESENT/MISSING、大小/hash、缺失引用、冻结的 `maxDiscs/maxTotalBytes` 与 attachment 状态，不返回 Blob ID、宿主路径或 capability。Attachment 状态包含 Job/Attachment version、可空 diagnostics 与仅在通用 Job 可人工重试时为 true 的 `canRetry`。

`POST /api/v1/admin/reviews/{importItemId}/multi-disc-attachments` 要求 ADMIN、同源/CSRF、`If-Match`、User-scoped `Idempotency-Key` 与 `{uploadId}`，只接受包含当前全部缺盘的 COMPLETE FILES upload。成功为 202，返回 Job/Attachment、`Location`、新 Review ETag；版本、active/retry、能力漂移、集合不符与内容无效使用 OpenAPI 中稳定错误码。关闭新 Import flag 不取消已冻结的 Attachment/Job，也不影响已发布读取。

## 6. Hasheous 边界

一期固定调用公开 `POST https://hasheous.org/api/v1/Lookup/ByHash`，body 字段沿用上游的 `mD5`、`shA1`、`shA256`、`crc`，至少一项非空；该 lookup 不需要用户或 App Key。只发送 hash，不发送 ROM bytes、本地路径、文件名或平台私有信息。

Retrom 不调用需要 App Key 的 MetadataProxy，也不调用需要 User Key 的 Submission。lookup 的 `200` body 按一个 object 解析，单请求不是 candidate array；`404 text/plain` 仍是正常 MISS。provider 字段先解析到内部 DTO；未知字段保留在 raw response Blob，但不直接暴露为 Retrom API 契约。每次 ImportItem/Game 查询属于独立 MetadataScrapeRun；response、按 provider game ID 聚合的 candidate、全部 hash hit 及 candidate asset 都持久化。`Logo` 作为 cover、`Screenshot1..4` 作为 screenshot；`AIDescription` 只在标准 description 为空时按导入专题规则作为显式 fallback，Tags/未知属性不自动成为正式字段。精确 mapping 以导入专题 `BY_HASH_V1` 为唯一规则。符合该规则的 `/api/v1/images/<opaque-id>` 分配 Retrom 自有 candidateAssetId；图片取不到只把该 asset 标为 FAILED，不阻止人工审核。

每个 ContentHashEvidence 的实际网络 attempt 或 cache reuse 都通过 MetadataScrapeQueryAttempt 关联到 ProviderResponse，MISS/错误也保留。规范 request digest、HIT/MISS TTL 与显式重刮削 bypass cache 的唯一规则见导入专题；HTTP handler 不得自行拼另一套 cache key。每个 run 都有持久 Job，provider=NONE 以同一事务完成 no-op Job/Run，不制造一次假网络请求。

后端 lookup 使用 15 秒总 deadline、4 MiB body 上限和导入专题的 run/candidate 数量门禁。媒体获取必须：只接受 HTTPS 和配置的 `hasheous.org` host/固定相对 image ID；每次 DNS 解析及 redirect 后拒绝 loopback、private、link-local、multicast、metadata 地址与非允许端口；最多 3 次 redirect；限制单项 10 MiB、30 秒、PNG/JPEG/WebP，校验魔数和解码像素不超过 40 MP，并执行每 run 100 MiB 实际响应总预算；拒绝 SVG/HTML。响应 `Content-Type` 必须声明为 PNG/JPEG/WebP 之一，实际类型以魔数与解码结果为准；仅受支持图片子类型之间的错标不拒绝，并按真实格式写入 CAS。文本始终按纯文本展示。常规测试使用本地 fake，不访问真实服务。

## 7. Launch 创建与凭据

唯一创建端点是：

```http
POST /api/v1/launches
Idempotency-Key: <uuid>

{
  "gameId": "0198...",
  "coreId": "mgba",
  "saveStateId": null,
  "dosEntry": null,
  "returnTo": "/games/0198...",
  "clientCapabilities": {
    "secureContext": true,
    "crossOriginIsolated": true,
    "sharedArrayBuffer": true
  }
}
```

`coreId` 可省略以使用游戏目录默认核心；有 `saveStateId` 时服务端忽略默认核心并要求显式值（若给出）与存档一致。`returnTo` 通常只接受无 query、无 fragment、无 percent-encoding 的精确路径 `/`、`/library`、`/saves` 或 `/games/{gameId}`；沉浸普通启动另外接受 `/immersive/platforms/{platformId}?gameId={gameId}`、`/immersive/library/{all|recent}?gameId={gameId}`，以及可选唯一 `folderId` 的 `/immersive/library/favorites?gameId={gameId}`。其中 `platformId` 必须是 1..64 位小写 slug，`gameId` 必须与 body 一致，`folderId` 必须是规范 UUID。沉浸存档恢复只接受 `/immersive/library/saves?gameId={gameId}&saveStateId={saveStateId}`，两个 ID 都必须与 body 一致；存档恢复不得返回普通沉浸平台或资料库。所有 query 均拒绝未知、重复或空字段。不接受 origin、反斜杠、协议相对或任意 URL。`clientCapabilities` 三个布尔字段必需，由点击时的 Chrome 环境读取；它们不是授权证据，但用于在签发 credential 前给 thread core 产生可理解 Blocker。Player 在加载 loader 前必须再次读取真实环境，任一值变差则终止为 `LAUNCH_THREADS_UNAVAILABLE`，不能只信请求快照。

若所选 core 对 Game current ContentRevision 没有同 `validationInputDigest` 的完成结果，本端点在短事务创建/复用 `VARIANT_REVALIDATE` Job 并返回 `202`；此时不创建 LaunchSession、不设 cookie：

```json
{
  "status": "VALIDATION_PENDING",
  "jobId": "0198...",
  "retryAfterMs": 1000
}
```

并发相同 GameVariant/input digest 必须返回同一 Job。Player 使用通用 Job 快照/SSE 等待；Job SUCCEEDED 后以原 body 和新 Idempotency-Key 自动重调本端点。旧 key 永远幂等重放当时的 202，不能在后台偷换为 201；新请求看到 READY 后才签发下述 credential。Job FAILED/CANCELLED 的错误以稳定 `LAUNCH_` code 展示，不自动反复新建 Job。

成功返回 `201`：

```json
{
  "runtimeFamily": "EMULATORJS",
  "mode": "single",
  "launchId": "0198...",
  "playUrl": "/play/0198...",
  "warnings": [],
  "bootstrapExpiresAtMs": 1786000300000,
  "hardExpiresAtMs": 1786086400000
}
```

响应 capability 是 32-byte HMAC-SHA-256 输出：`HMAC(launchKey, "retrom-launch-v1\x00" || UUIDv7 的 16 raw bytes)`。`launchKey` 是后端首次启动在数据根原子生成的独立 32-byte CSPRNG 本机密钥，精确存储规则见运维专题；不能使用兼容 session token、数据库 hash、公开配置或 UUID 文本本身作 key。输出以无 padding base64url 编码，数据库只保存 capability 原始 32 bytes 的 SHA-256，并设置动态 cookie `retrom_launch_<launchId>`：`Path=/runtime/launches/<launchId>/; HttpOnly; SameSite=Strict; Max-Age=86400`，不设置 `Domain`，生产 HTTPS 增加 `Secure`。

相同 Idempotency-Key/body 取得原 launchId 后重新计算同一 capability 和 `Set-Cookie`，因此无需把 secret 放进 idempotency record，也不会让并发重放互相作废。不同 launchId 经过域隔离得到不同输出；服务端仍常量时间比较 request cookie 的 SHA-256 与 session 行。`finish`/撤销响应用相同 name/path 和 `Max-Age=0` 尽力清理 cookie；session 撤销状态始终是最终防线。`launchId` 是可记录、可出现在 URL 的非秘密 UUIDv7；cookie 才是凭据。日志、JSON、路由、query、Referer、诊断和数据库不得出现 capability 明文。

已有验证结果的预检阻断返回 `422 LAUNCH_BLOCKED`，`details.blockers` 和 `details.warnings` 使用稳定 code/level/message/details，不创建 credential。常用 code 统一加 `LAUNCH_` 前缀，例如 `LAUNCH_BIOS_MISSING`、`LAUNCH_PARENT_MISSING`、`LAUNCH_SAVE_INCOMPATIBLE`、`LAUNCH_DOS_ENTRY_MISSING`、`LAUNCH_DOS_ENTRY_UNSAFE`、`LAUNCH_CORE_VALIDATION_UNAVAILABLE`、`LAUNCH_CORE_VALIDATION_TIMEOUT`、`LAUNCH_THREADS_UNAVAILABLE`；全屏拒绝是浏览器侧 Warning，不是假装后端错误。

凭据创建后 5 分钟内没有请求 bootstrap 即过期；首次正确 config 请求转为 `ACTIVE`。在 ROM/core 下载和 `EJS_onGameStart` 之间没有 PlaySession，因此只受创建后 24 小时 hard expiry，不能用 2 分钟 idle 把较大内容加载到一半的合法启动误杀；真实 start 成功后设置 idle expiry 为服务端接收时刻 + 2 分钟，此后每个连续 heartbeat/finish 更新或终结它。hard/idle 任一到期即撤销；无 PlaySession 的显式退出或 `pagehide` 也必须调用下面定义的 pre-start finish 尽力撤销。管理员删除游戏同样撤销。复制 `/play/<launchId>` 到没有 cookie 的浏览器只能显示“启动会话不可用”，不能取得内容。

PlaySession 事件 API 位于 launch cookie 的限定 Path 内，同时要求正确 launch cookie 和连续序号；不能只凭公开的 launchId 更新游玩记录，也不能把 cookie Path 放宽到 `/`：

- `POST /runtime/launches/{launchId}/start` body 为 `{ "clientSequence": 0, "clientObservedAtMs": int64 }`，只在真实 `EJS_onGameStart` 后调用；重复相同 body 返回原 PlaySession。
- `POST /runtime/launches/{launchId}/heartbeat` body 为 `{ "clientSequence": n, "clientObservedAtMs": int64, "previousInterval": { "running": bool, "visible": bool, "paused": bool } }`，`n` 从 1 连续递增。
- `POST /runtime/launches/{launchId}/finish`：已经 start 时使用下一个连续 sequence 和同样的 interval body，提交最后区间并撤销 launch；尚未 start 时只接受 `{ "clientSequence": 0, "clientObservedAtMs": int64, "previousInterval": null }`，不创建 PlaySession、直接撤销。两种重复请求都幂等。

多盘 Player 另使用 `POST /runtime/launches/{launchId}/player-events` 上报封闭的低基数运行结果。它同样要求正确的 launch cookie 和 Origin，只接受 `eventType=START/DISK_COUNT_MISMATCH/SWITCH_SUCCESS/SWITCH_FAILURE/SAVE_RESTORE_SUCCESS/SAVE_RESTORE_FAILURE`、稳定 `resultCode`、锁定的 `discCount` 与可空 `observedDiscCount`；服务端必须重新读取 Launch 锁定的 platform/core/artifact/disc count 并拒绝盘数不一致的 body。成功返回 `204`，失败不改变 Launch、PlaySession、存档或换盘结果。body、日志与指标都不得包含标题、basename、路径、hash 或 capability；该 best-effort 观测请求失败不能阻断 Player 主链路。

服务端以接收时刻计算 interval，单次最多 45 秒；client time 只审计且必须是 `0..253402300799999` 的 JSON integer，绝不能参与授权、顺序或计时。重复序号返回原 accepted delta，跳号为 `409 PLAY_SEQUENCE_GAP`。未 start、`running=false`、`visible=false`、`paused=true` 或超出上限的部分计 0。

## 8. 内容端点与缓存

| 路径 | 授权与缓存 |
| --- | --- |
| `/runtime/emulatorjs/{configuredVersion}/...` | 固定公开运行时；当前配置 `4.2.3,4.3.0-pre`，后者包含 DOSBox Pure、Genesis Plus GX Wide 与 Azahar 定向覆盖。版本参数接受规范 SemVer prerelease，且必须在后端启动时已验证的依赖列表内；路径必须命中该版本 manifest allowlist。允许 EmulatorJS 自身附加且仅附加一次的 `v` cache-buster 查询参数，该参数不参与文件选择；其他查询键仍拒绝。`public, max-age=31536000, immutable`，强 ETag。CSP 禁止 CDN fallback。 |
| `/content/assets/{assetId}` | 只用于已发布封面/截图等站内可见媒体；服务端解析逻辑 asset ID。每个 Asset ID 在存续期内 bytes 不变，替换 COVER/VIDEO 等媒体必须创建新 Asset ID 与新 URL，current 切换后旧 URL 立即失效；`public, max-age=31536000, immutable`。浏览器必须携带当前 session 直接请求该逻辑 URL；前端不得把受保护媒体交给不会转发 session cookie 的 Next.js 图片优化器。 |
| `/content/save-states/{saveStateId}/screenshot` | 只用于确有截图、未删除且所属游戏仍已发布的手动存档；服务端解析逻辑 SaveState ID，不向浏览器暴露 Blob ID。没有截图、存档删除或游戏下架均返回 404；成功响应固定为 `private, no-store`。 |
| `/api/v1/admin/review-assets/{assetId}` | 用于仍待审核 Item、候选媒体、人工上传审核媒体、Pegasus/EmulationStation 来源媒体或审核运行截图；服务器来源 `assetId` 为格式专属 Item ID 并带 `kind=COVER|VIDEO`（默认 COVER），封闭 UNION 必须恰好命中一个来源。响应为 `private, no-store`，不得把上游 URL 或 Blob ID 暴露给浏览器；ReviewEvent 本身不长期保留媒体。 |
| `/runtime/launches/{launchId}/config` | 需要 launch cookie；返回以 `runtimeFamily=EMULATORJS|RPGMAKER|ONS|KIRIKIRI|BUTTERSCOTCH|TYRANOSCRIPT|WASM4` 判别的非秘密配置 union，`private, no-store`、`Vary: Cookie`。普通审核 preview 继续由其专用配置分支投影；RPG validation 使用正式 LaunchConfig。 |
| `/runtime/content/game/{contentIdentity}/{logicalName}` | 只允许任一当前有效正式 Launch 或审核预览 grant 已锁定、且服务器重新计算身份等于 path 的运行内容；content identity 由领域版本、格式、Core、实际 ROM digest 与影响输出的选项派生，不直接暴露 Blob hash。需要仅作用于 `/runtime/content/` 的 HttpOnly grant cookie；`private, max-age=31536000, immutable`。替换 ROM 或影响输出的配置必须产生新 identity/URL，旧授权不能读取新内容。 |
| `/runtime/content/bios/{contentIdentity}/bundle.zip` | 支持 GET/HEAD；identity 由带领域版本、规范按逻辑名排序的 BIOS bundle 成员名与每个文件 digest 派生，不直接暴露成员 hash。任一成员替换都会产生新 URL；需要有效 content grant，`private, max-age=31536000, immutable`。HEAD 执行与 GET 相同的授权、Launch 状态和 bundle 清单校验。 |
| `/runtime/content/parent/{contentIdentity}/bundle.zip` | 与 BIOS bundle 相同，但只服务确定性 parent bundle；任一成员变化产生新 identity/URL，`private, max-age=31536000, immutable`。 |
| `/runtime/content/external/{contentIdentity}/{logicalName}` | 只允许有效 content grant 锁定的多盘或外部文件；identity 由带领域分隔的实际文件 digest 派生，不直接暴露 Blob hash，文件替换产生新 URL。未锁定名、错误 identity、跨 Launch、错误/过期 grant 与 Blob 缺失不得泄露存在性；`private, max-age=31536000, immutable`。 |
| `/runtime/launches/{launchId}/state` | 只允许选中状态存档；需要 cookie；`private, no-store`、`Vary: Cookie`。 |
| `/runtime/launches/{launchId}/review-screenshot` | 接受 `image/png` 或 `image/jpeg`，先鉴权再有界流式读取 ≤10 MiB。非 RPG 审核 preview 分支只接受 preview capability，按魔数与解码结果校验真实媒体类型，仍匹配当前来源、目标平台和 CoreArtifact 的 READY 或阻断 preview，固定记录 `capturedAfterMs=5000`。RPG 分支仍只接受 PNG 和当前 validation 的 `restore_launch_id` capability，要求原 Launch 已结束、位置恢复 gate 已 PASS 且 binding 未漂移，直接把新 Blob 关联到该 validation 的 `evidence_screenshot_blob_id`，随后才允许 `RESTORE_SCREENSHOT` gate PASS。PRODUCT Launch、原 validation Launch、过期 cookie、重复截图或来源/配置漂移均拒绝。 |

`GET /runtime/launches/{launchId}/config` 是首次 bootstrap 请求；credential、5 分钟 bootstrap TTL 和全部预检快照有效后，服务端原子把 LaunchSession 从 `CREATED` 转为 `ACTIVE` 并返回：

```json
{
  "launchId": "0198...",
  "emulatorjsVersion": "4.2.3",
  "playerAdapterId": "ejs-4.2.3-v3",
  "core": "mame2003",
  "runtimeCore": "mame2003",
  "coreName": "MAME 2003",
  "coreArtifactId": "0198...",
  "emulatorGameId": 42,
  "gameName": "retrom-42",
  "gameTitle": "1943: The Battle of Midway",
  "platformName": "Arcade",
  "runtimeBaseUrl": "/runtime/emulatorjs/4.2.3/data/",
  "loaderUrl": "/runtime/emulatorjs/4.2.3/data/loader.js",
  "gameUrl": "/runtime/content/game/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/ldrun.zip",
  "biosUrl": "/runtime/content/bios/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/bundle.zip",
  "parentUrl": "/runtime/content/parent/cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc/bundle.zip",
  "stateUrl": null,
  "inputMode": "STANDARD",
  "startupActions": [],
  "requiresThreads": false,
  "runtimePathOverrides": {
    "mame2003-wasm.data": "/runtime/emulatorjs/4.2.3/overrides/mame2003-4.2.1-wasm.data"
  },
  "defaultCoreOptions": {"webgl2Enabled":"enabled"},
  "externalFiles": {},
  "dosEntry": null,
  "warnings": [],
  "returnTo": "/games/0198..."
}
```

`emulatorGameId` 是 `1..9007199254740991` 的 JSON integer；`gameName` 必须精确为 ASCII `retrom-` 加它的十进制表示。`gameTitle/coreName/platformName` 只用于 Player 工具栏的人类可读上下文，不参与 EJS 配置、运行选择或授权判断。`biosUrl`、`parentUrl`、`stateUrl` 与 `dosEntry` 可为 `null`；其他字段必需。`defaultCoreOptions` 是最多 32 项的 ASCII string→string map，禁止危险 key；DOS 的该 map 和 `externalFiles` 均不包含启动入口，启动入口只由锁定内容的虚拟 ZIP 视图表达。其他核心的 `externalFiles` 只能指向由同一 Launch grant 授权的 `/runtime/content/external/{contentIdentity}/{logicalName}`。普通 `emulatorjsVersion/playerAdapterId` 必须等于锁定 CoreArtifact 所属 manifest 的版本/adapter ID；`dosbox_pure`、`genesis_plus_gx_wide`、`azahar` 的新 Launch 为 `4.3.0-pre/ejs-4.3.0-pre-v2`，其余 4.2.3 核心使用 `ejs-4.2.3-v3`。联机 config 由联机 manifest 精确锁定 legacy `ejs-4.2.3-v2`，不得用普通 manifest 当前 ID 覆盖。所有 URL 必须是同源站内路径，响应不得含 capability、Blob ID/hash、宿主路径或客户端可改写 URL。

`startupActions` 最多 4 项，只接受 `event=GAME_START`、`kind=PRESS_CONTROL`、`delayMs=0..30000`、`durationMs=1..1000` 以及有界 player/control 整数；OpenAPI、后端 manifest/Launch 校验与 Player adapter 必须采用同一边界。动作是锁定 CoreArtifact 的只读兼容配置，不能由请求者编辑，也不能按 core ID 在任一端补默认值。

`gameUrl` 的 `logicalName` 保留实际运行文件后缀；host console ZIP 已在入库时物化为唯一可运行 member，Arcade 与 DOS 才向 EJS 提供规范 ZIP。多盘 `gameUrl` 固定为 `/runtime/content/game/{contentIdentity}/playlist.m3u`，`discSet` 包含 2–8 个连续 index、中文 label、规范 `/disc-NNN.chd` virtualPath 和锁定的 initial index；同一组 DISC URL 必须逐项出现在 `externalFiles`。BIOS/parent 外层 bundle 的结构见运行时专题。config response 先按严格 JSON schema 校验，再由 Player adapter 设置 globals；页面不得从 core 名称自行推导文件名、线程开关、option 或 URL。

二进制端点支持 `GET`、`HEAD` 和单 Range；多 Range 返回 `416`。所有响应设置正确 MIME、`X-Content-Type-Options: nosniff`、`Accept-Ranges: bytes` 和强 ETag。DOS 的 `game.zip` 是从锁定基础 Blob与 entry 确定性派生的 seekable 虚拟 ZIP，HEAD/Range/完整 GET 必须同 size/ETag 且不落盘。受限 URL 不包含 Blob ID/hash，不设置 `public`，错误响应也不得泄露资源是否属于其他游戏。

运行内容的 `private immutable` 响应只允许同一浏览器缓存复用，不进入共享缓存；不同 Launch 锁定相同输入时
生成相同 URL，因此可以直接命中已有私有缓存。任何真正到达服务器的请求仍逐次验证 content grant、Launch/
审核预览状态与 path identity。Finish、撤销、硬删除或到期后新的/强制网络请求必须失败；HTTP 无法追溯擦除
浏览器先前已合法取得的 immutable 副本，这也是状态存档与存档截图继续 `private, no-store` 的原因。

运行中写入要求正确 launch cookie：

- `POST /runtime/launches/{launchId}/save-states` 使用 `multipart/form-data`，携带 UUID `Idempotency-Key`，且只允许 `metadata`、`payload` 与可选 `screenshot`。metadata 是严格 `{payloadKind,name?,discIndex?}`：PRODUCT 的 name trim 后必须为 1–120 Unicode code point，validation 必须省略 name；discIndex 只对多盘 PRODUCT 必填，其他场景省略或为 null。metadata 明确声明由 Launch artifact 决定的 `payloadKind=RUNTIME_STATE|NATIVE_SAVE_BUNDLE_V1|ONS_SAVE_BUNDLE_V1`；native profile/resume slot 从冻结构件与 payload 校验得出，不由客户端提交，客户端也不能覆盖 artifact/owner/content/ABI/dependency。既有 EmulatorJS/EasyRPG/MV/MZ/ONS payload 上限 64 MiB，mkxp-z 的最坏情况上限为 256 MiB，但 `mkxp-state-compact` 正常返回可变且显著更小的 payload，截图上限 10 MiB。PRODUCT 写正式 SaveState；RPG_RUNTIME_VALIDATION 只写同 validation 临时 checkpoint，response union 明确 resource kind，且 validation 分支必须返回服务端已持久化 payload 的小写 64 位 `sha256` 和 `sizeBytes`，供随后 `CHECKPOINT_CREATED` gate 提交相同证据；该临时资源不得出现在 `/saves`。同一幂等键重放必须返回与首次创建完全相同的 digest 与字节数。
- 本机 PFB 网关、部署 NG 与 Next.js 全局 rewrite 代理层使用 `283115520` bytes（270 MiB）传输天花板和 300 秒 read/send/backend timeout，不对 `/api/v1/admin/imports` 或 save-state 增加独立 NG location。Go 层仅 `POST /runtime/launches/{launchId}/save-states` 的 multipart 总上限同样为 `283115520` bytes 且 route deadline 为 300 秒；其他 endpoint 仍执行自己更小的应用层 body 上限和 deadline。超过存档 route 上限必须在读取完整 body 前返回通用 413，不能由代理层截断为 500。

运行时写端点在验证 launch cookie 后才读取 body；有 `Content-Length` 时先校验上限，没有时允许 HTTP/2/chunked 并在流式读取超过上限的第一个 byte 立即终止为 `413`。NG 必须关闭请求 buffering 或使用足够的临时空间，且自身限制不得低于应用上限；应用仍独立流式计数、校验 digest 并清理临时文件。`pagehide` 不发送 save body；显式退出只等待用户已经发起的 `/save-states` 上传完成，不生成退出存档。

OpenAPI 中 `putAdminUploadPart`、`postRuntimeSaveState` 与 `postRuntimeReviewScreenshot` 三个 operation 必须且只能标记 `x-retrom-streaming-body: true`；生成物应分别暴露 `io.Reader`/`multipart.Reader`，不能生成 `[]byte` 或先 `ParseMultipartForm`。启动时基于同一份已加载 spec 构建两条不可变的 `nethttp-middleware` validator chain：普通链保持 `Options.Options.ExcludeRequestBody=false`，流式链设置 `Options.Options.ExcludeRequestBody=true`。前置 kin-openapi router 先匹配 operation 并读取该 extension，再把请求分派给对应链；请求处理中不得修改共享 options。流式链仍验证 method/path/query/header/content-type，领域 handler 的流式检查才是 body 的权威门禁；不得另维护 URL skip 清单，也不得用全局 `Skipper` 跳过完整验证。所有 `operationId` 使用唯一 lowerCamelCase，格式为 HTTP 动词加稳定领域动作；已经发布后改名视为生成代码破坏性变更。

`core` 表示产品目录 ID，`runtimeCore` 表示 EmulatorJS runtime ID；Player 只能把后者写入 `EJS_core`。`inputMode` 只允许 `STANDARD|POINTER`。`externalFiles` 当前只用于 Variant dependency snapshot 锁定的 MelonDS 三个绝对虚拟路径，其 URL 必须属于 `/runtime/content/external/`、命中 config 的派生 content identity，并由同一 Launch grant 授权；最多 16 项，重复路径/逻辑名或跨 Launch URL 均阻断。主机/掌机 ZIP/7z 已在入库时物化为唯一可运行 member；PSP `.iso/.cso` 作为 raw CONTENT 返回，不做运行时转换。

## 9. 核心 API 路由表

所有 `{id}` 使用对应资源 ID，不接受混用；列表均遵循第 1 节 cursor envelope。第 3 节列出的创建/管理 action POST 需要 Idempotency-Key，PATCH/DELETE/管理状态转换同时需要当前 If-Match；PlaySession 事件用连续 sequence，上传 part 与运行时二进制写入按各自幂等规则。

### 9.1 管理写入的最小请求契约

以下字段名与语义是 OpenAPI 实施基线。未列字段一律未知字段拒绝；PATCH 中“未出现”表示保持不变，显式 `null` 只允许清空表中标为可空的字段。字符串先验证 UTF-8，再按字段执行 trim：游戏 title/游戏目录 name/`confirmTitle` 为 1–200 Unicode code points，SaveState name 为 1–120，developer/publisher/genre 为 0–200，description 为 0–10,000，reason 为 1–500。游戏目录 slug 不接受客户端输入，由服务端生成 1–80 ASCII chars 的领域合法值。年份为 `1950..当前 UTC 年+1`，players 为 `1..64`。所有引用 ID 必须存在、类型正确且处于该操作允许状态。

| 操作 | JSON body / 必需 header | 成功语义 |
| --- | --- | --- |
| SaveState 重命名 | `PATCH /saves/{id}`：`{"name":"..."}` + `If-Match` | 更新可变 name/version，不新建状态 Blob。 |
| Import/通用 Job 取消 | `{"reason":"..."}` + `If-Match` + `Idempotency-Key` | 只影响声明 `cancellable=true` 且尚未 final 的范围。Import 领域 route 在同一事务写 aggregate cancel 字段，把未运行/待审核/可重试 Item 转 CANCELLED，并向运行中子 Job 请求取消；无运行项时 ImportJob 同步 CANCELLED 并返回 200，否则为 CANCEL_REQUESTED 并返回 202，最后一个 Worker 确认后才变为 CANCELLED。普通 Job 同样是 QUEUED 同步 CANCELLED、RUNNING 转 CANCEL_REQUESTED。已经发布/提升或确定性失败的领域结果不回滚，显式取消后的 ImportJob 不得聚合成 COMPLETED。 |
| ImportItem / 通用 Job 重试 | `{}` + `If-Match` + `Idempotency-Key` | Item 仅接受 `FAILED_RETRYABLE`：`failedStage=HASHING|IDENTIFYING` 时增加既有 IMPORT_ITEM_PIPELINE execution且保留 ImportJob 冻结配置，`SCRAPING` 时以同一冻结 provider/config 创建新 MetadataScrapeRun/Job；旧 Run/Job/Response 不改。通用 Job 仅接受 `FAILED` 且 `errorRetryable=true`，清除旧 lease/finished/error、attempt 重置为 0、version 递增并追加事件。每次都新建 InputSnapshot，但只有 GAME_FILE_REVISION 领域 retry 刷新 Game/目录/依赖快照；其他 kind 不得借 retry 偷换语义输入。`METADATA_SCRAPE` 的通用 retry 返回 `409 RETRY_VIA_DOMAIN_ACTION`；审核/游戏页重试也必须创建新 Run/Job，不污染旧批次证据。 |
| Review 草稿 | `PATCH /admin/reviews/{itemId}` + `If-Match`；body 可含 `targetPlatformInstanceId`、`metadata`（title/description/developer/publisher/genre/players/releaseYear）、`selectedValidationId`、`selectedCandidateId`、`selectedAssets`（`coverCandidateAssetId` 与 `coverUploadedAssetId` 互斥且可空，`backgroundCandidateAssetId` 可空，`screenshotCandidateAssetIds` 最多 32 个且不重复）、`defaultDosEntry` | 同事务把 metadata partial 合并为完整 draft object；验证 Validation READY 且匹配目录/config，candidate 属于本 Item COMPLETED run，候选 asset 属于本 Item 任意 COMPLETED run 且 READY，人工封面属于本 Item，DOS entry 属于 Item；规范化截图顺序并追加 ReviewEvent。页面以 450ms 防抖串行 PATCH，决定前必须冲刷最新状态。把 selectedCandidateId 设为 null 只改变来源，不暗中回滚 metadata。只能改到同一基础平台的另一目录，跨平台返回 `422 REIMPORT_REQUIRED_FOR_PLATFORM_CHANGE`。 |
| Review 人工封面 | `POST /admin/reviews/{itemId}/assets`：`{"uploadFileId":"...","kind":"COVER"}` + `If-Match` + `Idempotency-Key` | UploadFile 必须 COMPLETE 且为 ≤10 MiB、≤40 MP 的 PNG/JPEG/WebP；创建不可变 `review_uploaded_assets` 和 `REVIEW_ASSET` consumption，不改变草稿版本。响应返回审核资源逻辑 URL；采用仍通过 Review 草稿 PATCH 完成，从而对比弹窗上传不会在“应用”前覆盖当前封面。 |
| Review 运行预览 | `POST /admin/reviews/{itemId}/previews`：`{"clientCapabilities":{...}}` + `Idempotency-Key` | ADMIN 为当前有效来源、目标默认 CoreArtifact 和最新 Validation 创建短时 capability cookie；主 ROM 必需，现有 Parent/BIOS/external files 锁定，缺失依赖省略。返回 `previewId/playUrl/captureAllowed/captureAfterMs=5000`，不改变发布资格。 |
| Review 显式重刮削 | `POST /admin/reviews/{itemId}/scrape-candidates`：`{"metadataProvider":"HASHEOUS|NONE"}` + `If-Match` + `Idempotency-Key` | Item 必须 REVIEW_PENDING；HASHEOUS bypass cache 创建新 Run/Job 并返回 `202`，NONE 同事务创建 COMPLETED Run/SUCCEEDED Job 并返回 `201`；两者追加 SCRAPE_REQUESTED，不自动改 draft selection。 |
| Review 通过 / Discard | discard 与无重复的 approve body 可为 `{}`；服务端为旧客户端继续接受可空 `reason`，新 UI 不采集发布说明或丢弃原因。重复内容确认的 approve body 为 `{"duplicatePolicy":"ALLOW_NEW","acknowledgedGameIds":[uuid...]}`；`If-Match` + `Idempotency-Key` | approve 只接受当前匹配的 READY selected Validation，且 title trim 后为 1–200 Unicode code points、无控制字符；在同一写事务 claim 内容身份并重查同平台 current published contents。命中且未精确确认时返回 `409 DUPLICATE_GAME_CONFIRMATION_REQUIRED` 和当前 games；确认后原子创建发布实体、复制 ValidationFiles 与候选/人工上传封面并把确认写入事件，不得在事务内重扫/打包。discard 不删除证据。 |
| Review 快速审批 | preview：`GET /admin/review-bulk-approval-preview` + 当前审核筛选；create：`POST /admin/review-bulk-approvals` + preview digests + Idempotency-Key；cancel/retry：aggregate `If-Match` + Idempotency-Key | 只冻结严格 READY、无重复/active Attachment 的当前候选。每项复用普通 approve 事务并原子写 batch result；dependency evidence 按静态 BIOS/多盘 schema v1 或 Arcade schema v2 的对应分支核对，合法 Arcade v2 不能被当作 parser stale；截图 override 继续逐项。create stale/empty/active/too-large 使用上文稳定错误码，cancel 不回滚已发布项，worker infrastructure failure 才可领域 retry。 |
| 游戏元信息 | `PATCH /admin/games/{gameId}` + `If-Match`；body 直接包含 title/description/developer/publisher/genre/players/releaseYear 中至少一个字段，例如 `{"title":"..."}`，不再包一层 metadata | 复制未变文本和当前完整 asset 清单，按最终 title 计算受约束 `title_initial`，创建 MetadataRevision/Asset refs、更新 `games.search_text` 后切换 current；改名与排序键必须同事务原子前移。同一事务移除旧 revision 的 Asset 叶子引用，文字 revision 继续保留。 |
| 游戏媒体 | `POST /admin/games/{gameId}/assets`：`{"uploadFileId":"...","kind":"COVER|BACKGROUND|SCREENSHOT","ordinal":0}` + `If-Match` + `Idempotency-Key` | UploadFile 必须 COMPLETE 且为受支持图片；复制文本/未变媒体，创建完整新 MetadataRevision 清单并切换 current。COVER/BACKGROUND 的 ordinal 只能 0，SCREENSHOT 为 `0..31`。切换后旧 Asset URL 立即失效；失去最后引用的 Blob 转入等待回收，不承诺已登记 CAS 总量立即下降。 |
| 游戏内容 revision | `POST /admin/games/{gameId}/content-revisions`：`{"uploadId":"..."}` + Game `If-Match` + `Idempotency-Key` | UploadSession 必须 COMPLETE、未消费，且按游戏基础平台恰好组成一个内容项。事务快照 Game current content、目录/version、默认 core/artifact/DAT，创建 `GAME_FILE_REVISION` Job 和 whole-session consumption，返回 `202`。Worker 在事务外安全扫描/物化/验证；单 ROM role/hash 序列相同，或多盘的盘序与全部 Disc hash 相同时，以不可重试 `GAME_CONTENT_UNCHANGED` 结束、释放 consumption 且不创建 revision。只有不同的新内容 READY 且快照仍一致时才原子创建 GameContentRevision/ContentFiles/VariantRevision、切换 Game content 与目标 Variant current，并删除旧 Content/Variant/Launch payload 及绑定旧 revision 的存档、结束旧运行；旧 revision 行作为结构化审计保留。其他失败不创建 revision、不改 current、不删除存档；配置/内容竞态及可修复依赖错误标为 retryable。 |
| 重新刮削 | `POST /admin/games/{gameId}/scrape-candidates`：`{"metadataProvider":"HASHEOUS"}` + `If-Match` + `Idempotency-Key` | 对当时 Game current ContentRevision 建 MetadataScrapeRun 和有界后台任务，显式 bypass cache，不改 current metadata；若任务执行前 content 已变化则以 retryable conflict 结束，不能对旧内容冒充最新 run。 |
| 应用重新刮削候选 | `POST /admin/games/{gameId}/scrape-candidates/{candidateId}/apply`：`{"fields":["title",...],"selectedAssets":{"coverCandidateAssetId":null|string,"backgroundCandidateAssetId":null|string,"screenshotCandidateAssetIds":[]}}` + `If-Match` + `Idempotency-Key` | candidate/asset 必须属于直接引用 Game current ContentRevision、并按 `(created_at_ms,id)` 确定的最新 COMPLETED HASHEOUS run，且每个选中 asset 为 READY；fields 只能是 metadata 白名单且不重复。复制未选字段/媒体，创建 RESCRAPE_APPLY MetadataRevision 和完整 Asset 清单。 |

`GET /admin/games/{gameId}/scrape-candidates` 的每个候选除 metadata/evidence/hitCount 外还返回 `assets[]`，字段与审核候选媒体投影一致，预览 URL 仍使用受保护的 `/api/v1/admin/review-assets/{candidateAssetId}`。管理页不得只凭“证据命中”提供一键文字覆盖；应用候选时未选择替换的当前封面、背景和截图必须复制到新 MetadataRevision，不能因只换文字或封面而丢失其他媒体。
| 游戏移动预览 / 提交 | preview：`{"targetPlatformInstanceId":"..."}` + Game `If-Match` + `Idempotency-Key`；commit：`{"targetPlatformInstanceId":"...","impactDigest":"...","confirmBlocked":false}` + 同一 Game 当前 `If-Match` + 新 `Idempotency-Key` | 只允许同基础平台且目标不能等于当前目录。目标默认 CoreArtifact 对 Game current ContentRevision 缺少当前输入结果时，preview 创建/复用共享 `VARIANT_REVALIDATE` Job，返回 `202 {"status":"VALIDATION_PENDING","jobId":"...","retryAfterMs":1000}`，不返回 digest、不移动；Job 终态后客户端用新 key 重新 preview。已有当前 READY/BLOCKED/INCOMPATIBLE 结果时返回 `200` 影响与 digest，不写业务状态。commit 重算 digest/version/结果；有 blocker 仅在 `confirmBlocked=true` 时移动，否则 `422 MOVE_TARGET_CORE_BLOCKED`。移动只更新 Game 归属/version并写审计，不删除或改写 Variant/revision/save。 |
| 游戏永久删除 | `DELETE /admin/games/{gameId}`：`{"confirmTitle":"当前完整标题","impactDigest":"64位小写SHA-256"}` + Game `If-Match` + `Idempotency-Key` | 管理详情先返回 ROM/媒体/存档/活动运行/联机/审核计数、已登记/独占/共享十进制 byte 字符串、来源类型和 digest。提交重算标题/version/digest；同一事务转 `DELETED+RELEASING`、撤销运行、创建唯一 PayloadRelease Job 和审计。首次返回 `202` 墓碑 DTO；同 key 重放同一响应；不同 key 再删返回现有墓碑与原 Job 的 `200`，不创建第二个 Job。 |
| 游戏目录创建 | `{"platformId":"arcade","defaultCoreId":"fbneo","name":"...","description":"","sortOrder":0}` + `Idempotency-Key` | 创建 enabled/version=1 目录；服务端从 name 生成 slug，冲突时追加最小可用数字后缀，且软删除后永不复用。 |
| 游戏目录普通修改 | `PATCH /admin/platform-instances/{id}` + `If-Match`；仅可含 name/description/sortOrder/enabled | 不允许 platformId/slug/defaultCoreId；`enabled=false` 允许非空目录，用于从用户侧首页、游戏库、详情、存档与启动入口隐藏该目录游戏，管理端记录仍保留。删除仍要求目录为空。 |
| 游戏目录排序 | `PUT /admin/platform-instances/order`：`{"items":[{"id":"uuid","version":1},...]}` | `items` 必须恰好包含全部未删除目录且 ID 不重复；在同一短事务按数组顺序写 `sortOrder=100,200,...`、逐项校验 version 并递增 version/写审计。目录集合变化返回 `409 PLATFORM_INSTANCE_ORDER_STALE`，任一版本变化返回 `409 VERSION_CONFLICT`，不得部分排序。 |
| 默认核心预览 / 提交 | preview：`{"coreId":"...","cursor":null|string,"limit":100}` + `If-Match`；commit：`{"coreId":"...","impactDigest":"...","confirmBlocked":false}` + 当前 `If-Match` + `Idempotency-Key` | preview 不写状态且免 Idempotency-Key；commit 原子修改目录 version 并写 AuditEvent，不重写存档/revision。 |
| BIOS 安装 | `POST /admin/bios/{requirementId}/installations`：`{"uploadFileId":"..."}` + `If-Match` + `Idempotency-Key` | 从 COMPLETE UploadFile 流式验证；新 installation 可为 MATCHED/HASH_WARNING/MISSING_ENTRY，原 active 同事务取消并清空 Blob，依赖旧 Installation 的存档/运行 payload 同时删除，活动 Launch/Play/Netplay 终止。静态文件 hash 不同，或 Arcade 必需 entry 名齐全但 size/hash 不同，均为可装入且不阻断的 HASH_WARNING；完全缺 entry 才是保存并 active 但阻断的 MISSING_ENTRY。损坏/不安全 archive 为 INVALID、只留审计且不可 active。 |
| BIOS 归档条目对比 | `GET /admin/bios/{requirementId}/entries` | 只接受已安装且 active 的 `DAT_MACHINE` Requirement；响应为 `requirementId/logicalName/installationId/installationStatus/entries[]`，每项固定含 `status=MATCHED|ALIASED|MISMATCHED|MISSING|EXTRA`以及可空 `expected/actual={name,sizeBytes,crc32}`。期望条目必须来自 Requirement `sourceVersion` 指定的精确 DAT 版本，实际条目必须读安装阶段落库的 ArchiveEntry，GET 不重扫 Blob、不返回宿主路径；非 DAT/未安装返回 `404 BIOS_ARCHIVE_FACTS_NOT_FOUND`。 |

Arcade DAT 没有管理 HTTP API。服务只接受 dependency manifest 固定的内置 DAT，在依赖引导阶段校验、解析并选择与 CoreArtifact 精确对应的版本；管理端不能上传、比较、启用、回滚或删除 DatVersion。

`impactDigest` 固定为无 padding base64url(SHA-256(RFC 8785 canonical JSON))。被摘要的 preview 至少包含 action、当前 actor User ID、目标资源/version、目标 core/目录/DAT、受影响实体 ID + current revision/version、blocker code 和生成时配置版本；数组按 ID UTF-8 byte 升序。提交时重新计算并常量时间比较，任一变化返回 `409 IMPACT_PREVIEW_STALE`。普通 preview 不落业务状态，但游戏移动 preview 在缺少当前兼容性结果时允许且只允许按上一表投递共享验证 Job；它仍不移动 Game，也不提前生成 impact digest。其余 preview 只可写不含秘密的访问日志。

默认核心 preview 是一个有界 POST 分页特例：`limit` 默认 100、范围 `1..100`，首页 `cursor=null`，items 按 Game ID UTF-8 bytes 升序；响应固定为 `{"coreId":"...","platformInstanceVersion":1,"counts":{"ready":0,"needsValidation":0,"blocked":0},"items":[],"nextCursor":null,"impactDigest":"..."}`，counts 始终是全量计数而不是当页计数。cursor 的 filter canonical object 固定包含 `coreId/platformInstanceId/platformInstanceVersion/impactDigest`，sort code 为 `GAME_ID_ASC`；服务端每页先重算全部影响输入，再验证 cursor filter hash。目录版本、Game/current revision、artifact/DAT/BIOS 输入或诊断发生任一变化都返回 `409 IMPACT_PREVIEW_STALE`，客户端必须从首页重新收集；不得返回同 digest 的部分新快照。commit 不上传 cursor/items，以完整 digest 重算为唯一权威。

游戏移动 preview 的幂等记录遵循固定响应重放：首次得到 `202` 的旧 key 在 Job 完成后仍重放原 `202`，不得把已保存结果暗改为 `200`；客户端观察 Job 终态后必须使用新 key 重新请求。并发 preview 以同一 GameVariant/`validation_input_digest` 唯一约束复用一个不可取消的验证 Job。Job 失败时新 preview 返回 `422 MOVE_VALIDATION_FAILED` 和同一 `jobId`；只有通用 Job retry 成功或依赖输入改变后才可再次取得影响预览。

Upload manifest/part/complete、Import 创建、Launch、PlaySession 与 runtime 二进制 body 已在第 4、5、7、8 节完整定义，不在本表复制。单资源读取/创建直接返回该资源 JSON；异步创建另含 `jobId`，`202`；同步创建为 `201`。PATCH/同步状态转换为 `200` 并返回新 version/ETag；同步完成的 DELETE 为 `204`，运行中任务的取消请求为 `202`；幂等重放返回已保存的原 status/body/header。

| 方法与路径 | 用途 |
| --- | --- |
| `GET /health/live`、`GET /health/ready` | 不需要 cookie。live 成功固定返回 `200 {"status":"ok"}`；ready 成功返回 `200 {"status":"ready"}`，失败返回 `503 {"status":"not_ready","reasonCode":"DATABASE_UNAVAILABLE|CAS_UNAVAILABLE|DEPENDENCY_INVALID|DEPENDENCY_DAT_PARSE_FAILED|DEPENDENCY_INDEXING"}`，多原因时按此枚举顺序选第一项。响应禁止暴露宿主路径、hash 或秘密，两条路径都进入 OpenAPI。 |
| `GET /api/v1/auth/context`、`POST /api/v1/auth/initialize`、`POST /api/v1/auth/login|logout|change-password` | 实例/会话 bootstrap、安全初始化、登录退出和密码轮换；精确 schema、错误码与安全 header 以 OpenAPI 和第 2 节为准。旧 `/api/v1/session` 不存在。 |
| `POST /api/v1/auth/account-links/inspect`、`POST /api/v1/auth/invitations/accept`、`POST /api/v1/auth/password-resets/complete` | fragment capability检查、邀请注册与密码重置。 |
| `GET /api/v1/home` | 首页聚合：启用目录中的统计、按 PlaySession `started_at_ms` 选择的最近 10 款游戏、按 Game `created_at_ms DESC, id DESC` 选择的最新添加 10 款已发布游戏、最后启动的一次游玩及仅由该次 Launch 产生的最新手动存档、全部支持平台，以及按 PlaySession 次数降序的前 4 个快捷平台。相同启动时刻按 PlaySession ID 确定唯一会话，平台热度相同时按名称和 ID 确定性排序；旧会话较晚结束或补写 heartbeat 不得反向夺取“最后游玩”，历史存档只影响“查看存档”，不得冒充最后一次游玩的恢复点。`latestGames[]` 固定提供 `gameId/title/platform/platformInstance/createdAtMs/coverUrl`，目录停用后对应游戏不进入该投影。 |
| `GET /api/v1/recent-games` | 返回启用目录中全部有游玩记录的已发布游戏，不截断为固定 50 款；按最新 PlaySession 的 `started_at_ms` 降序聚合 `lastPlayedAtMs/activeDurationMs/sessionCount` 与可空封面 URL。每款游戏只占一行，接口不接受 `limit`；响应级 `generatedAtMs` 是页面分组与 7/30 天滚动窗口的统一时钟。 |
| `GET /api/v1/games`、`GET /api/v1/games/{gameId}` | 已发布游戏列表/详情；两者的可空 `coverUrl` 只投影当前 MetadataRevision 中按 ordinal/ID 排序的首个 `COVER`，值为 `/content/assets/{assetId}` 逻辑 URL，不暴露 Blob ID。列表项同时包含基础平台、游戏目录、推荐 Core、`createdAtMs` 与可空 `lastPlayedAtMs`；列表按 `RECENT_DESC/ADDED_DESC/TITLE_ASC` 的服务端稳定 cursor 分页，每页默认 50。无 cursor 的首分页额外返回 `filteredCount` 与 `facets={totalCount,platforms,platformInstances,tags}`；facet 覆盖完整可见游戏库并带真实 count，续页不重复返回。响应级 `generatedAtMs` 作为相对时间的统一时钟。 |
| `GET /api/v1/saves`、`PATCH /api/v1/saves/{saveStateId}`、`DELETE /api/v1/saves/{saveStateId}` | 手动存档列表、重命名和软删除。`gameId` 为精确游戏筛选并进入 cursor filter digest；`availability=AVAILABLE` 只返回当前可恢复存档，`ALL` 还保留 RPG Maker/ONS 的 `SAVE_RUNTIME_INCOMPATIBLE` 与 EmulatorJS 的 `SAVE_CORE_UNAVAILABLE` 阻断项。列表项包含基础平台、游戏目录、锁定 Core、payload `sizeBytes`、可空 `screenshotUrl`（存在时为 `/content/save-states/{saveStateId}/screenshot`）与累计有效游玩 `activeDurationMs`，不暴露 payload/screenshot Blob ID。响应级 `generatedAtMs` 为分组页面的“今天/昨天”和分页聚合提供统一时钟。 |
| `POST /api/v1/launches` | READY 时预检并创建 LaunchSession/cookie；缺少当前 Variant 结果时返回 202 的可观察验证 Job，不先签发 credential。 |
| `POST /runtime/launches/{launchId}/start`、`POST /runtime/launches/{launchId}/heartbeat`、`POST /runtime/launches/{launchId}/finish` | 第 7 节 PlaySession 连续事件、时长和撤销；使用限定 Path 的 launch cookie。 |
| `GET /runtime/launches/{launchId}/config` 及第 8 节内容路径 | 受 capability 保护的配置、内容与显式状态。 |
| `POST /runtime/launches/{launchId}/save-states` | 用户显式触发的运行中状态保存；payload 必需，截图可选。 |
| `POST /runtime/launches/{launchId}/review-screenshot` | 非 RPG 审核 preview 保存核心启动后第 5 秒 PNG/JPEG；RPG validation 的不同 restore Launch 仍保存位置恢复证据 PNG；PRODUCT Launch 禁止。 |
| `POST /api/v1/admin/uploads` | 创建文件/目录 upload manifest。 |
| `GET /api/v1/admin/uploads/{uploadId}`、`PUT /api/v1/admin/uploads/{uploadId}/files/{fileId}/parts/{partNo}` | 恢复状态与上传 part。 |
| `POST /api/v1/admin/uploads/{uploadId}/complete`、`DELETE /api/v1/admin/uploads/{uploadId}` | 投递异步 UPLOAD_FINALIZE 或取消 upload；两者都使用当前 ETag，complete 另需 Idempotency-Key。 |
| `GET /api/v1/admin/imports/summary` | 入库总览按用户可见顶层批次聚合：浏览器上传/重新配置产生的 ImportJob 各计一次，PegasusImport/EmulationStationImport 各计一次，两类服务器来源为审核交接逐游戏创建的内部 ImportJob 不再重复计数。`running/completed/failed` 是批次数，其中 `failed` 只含 `PARTIAL_FAILURE/FAILED`、不把主动取消当异常，并以 `ordinaryFailed/pegasusFailed/emulationStationFailed` 提供正确处置入口；`processingItems/issueItems` 分别是当前处理中条目数和阻断/失败/未解决拒绝文件数；`reviewPending` 固定为全局实际可进入审核的 ImportItem 数量，不含尚未完成来源 attach 的 EmulationStation 预留 Item，`publishedItems` 为实际 `PUBLISHED` 的 ImportItem 数量。 |
| `GET /api/v1/admin/users`、`GET|PATCH|DELETE /api/v1/admin/users/{userId}` | 只含账号与安全状态的用户列表/详情、角色状态变更和软删除。 |
| `GET|POST /api/v1/admin/invitations`、`GET|POST /api/v1/admin/users/{userId}/password-reset-links`、`DELETE /api/v1/admin/account-links/{accountLinkId}` | 一次性链接的非秘密列表、创建和撤销；完整 URL只在 create/replay响应出现。 |
| `GET /api/v1/admin/imports`、`POST /api/v1/admin/imports`、`GET /api/v1/admin/imports/{importJobId}` | 列表只投影用户发起的浏览器上传/重新配置 ImportJob，排除通过 Pegasus 或 EmulationStation source Item 关联的逐游戏审核交接任务；两类顶层历史分别由格式专属 route 返回。创建与详情契约不变，详情包含原文件处置和可空 resolution；已知内部 ID 的详情仍可用于管理员诊断。 |
| `GET /api/v1/admin/imports/{importJobId}/events`、`POST /api/v1/admin/imports/{importJobId}/cancel` | SSE 进度与取消。 |
| `POST /api/v1/admin/imports/{importJobId}/reconfigure` | 携带 `If-Match`/`Idempotency-Key`，复用未解决 REJECTED 文件的 CAS Blob，以新游戏目录配置创建 replacement ImportJob。 |
| `POST /api/v1/admin/import-items/{importItemId}/retry` | 仅重试 retryable item。 |
| `GET /api/v1/admin/jobs/{jobId}`、`GET /api/v1/admin/jobs/{jobId}/events`、`POST /api/v1/admin/jobs/{jobId}/cancel`、`POST /api/v1/admin/jobs/{jobId}/retry` | Upload 终结、DAT/重校验/游戏内容 revision 等非 Import 长任务的快照、SSE、有界取消与显式 retryable 重试；Import 仍使用领域 route，`METADATA_SCRAPE` 人工重试使用 review/game 领域 route 新建批次。 |
| `GET /api/v1/admin/reviews`、`GET /api/v1/admin/reviews/{importItemId}`、`PATCH /api/v1/admin/reviews/{importItemId}` | 待审核队列、详情（含 Validation、scrape run/candidate/asset）和草稿。 |
| `GET /api/v1/admin/review-bulk-approval-preview`、`POST /api/v1/admin/review-bulk-approvals`、`GET /api/v1/admin/review-bulk-approvals/{bulkApprovalId}`、`GET .../{bulkApprovalId}/items`、`POST .../{bulkApprovalId}/cancel|retry` | 当前筛选的严格 READY 快速审批预览、冻结批次、进度/结果分页、有界取消及领域 retry。 |
| `POST /api/v1/admin/reviews/{importItemId}/previews` | 仅为非 RPG 内容创建审核专用 best-effort 子窗体快照；RPG core 固定返回 `RPG_RUNTIME_VALIDATION_REQUIRED`。 |
| `POST /api/v1/admin/reviews/{importItemId}/scrape-candidates` | 审核中切换/重新执行 HASHEOUS 或 NONE 元信息源；显式请求不使用旧 cache。 |
| `POST /api/v1/admin/reviews/{importItemId}/approve`、`POST /api/v1/admin/reviews/{importItemId}/discard` | 最终审核决策。 |
| `GET /api/v1/admin/review-history`、`GET /api/v1/admin/review-history/{reviewEventId}` | 只读最终决策列表与 ReviewEvent v2 的纯文字/结构化回放。actor 三元组不变；详情不返回 `coverUrl`、selected asset 字段或任何历史媒体端点。 |
| `GET /api/v1/admin/games`、`GET /api/v1/admin/games/{gameId}`、`PATCH /api/v1/admin/games/{gameId}`、`DELETE /api/v1/admin/games/{gameId}` | 游戏管理、MetadataRevision 与永久删除；墓碑详情只返回基本文字、payload state/release Job/稳定错误，内容、媒体和存档清单为空。 |
| `POST /api/v1/admin/games/{gameId}/assets` | 从已完成 UploadFile 创建新 Asset。 |
| `POST /api/v1/admin/games/{gameId}/content-revisions` | 从已完成 UploadSession 创建游戏内容验证 Job；成功才创建 ContentRevision/VariantRevision 并切换两个 current。 |
| `GET /api/v1/admin/games/{gameId}/scrape-candidates`、`POST /api/v1/admin/games/{gameId}/scrape-candidates`、`POST /api/v1/admin/games/{gameId}/scrape-candidates/{candidateId}/apply` | 重刮削候选列表、创建批次与选择字段/媒体应用。 |
| `POST /api/v1/admin/games/{gameId}/move-preview`、`POST /api/v1/admin/games/{gameId}/move` | 同基础游戏目录移动影响预览与提交。 |
| `GET /api/v1/admin/platforms`、`GET /api/v1/admin/core-artifacts` | 平台/启用核心关系与 artifact/version 只读字典，供目录和运行依赖管理使用。artifact 项固定返回 `id/coreId/coreName/routeKey/runtimeFamily/runtimeAdapterKind/runtimeVersion/adapterId/selectedForNewBindings/availableForLaunch/version/sizeBytes`，不返回宿主路径；RPG 管理诊断据此审计七个版本核心的 route/artifact。平台字典的每个 `cores[]` 固定返回 `netplaySupported`；它只有在该平台、Core、当前 `selectedForNewBindings` CoreArtifact 的 EmulatorJS 版本与 SHA-256 精确命中联机 manifest 时才为 `true`，表示运行核心能力而非某款游戏已通过联机资格检查。 |
| `GET /api/v1/admin/platform-instances`、`POST /api/v1/admin/platform-instances`、`GET /api/v1/admin/platform-instances/{platformInstanceId}`、`PATCH /api/v1/admin/platform-instances/{platformInstanceId}` | 游戏目录 CRUD；创建、列表和详情投影 `gameCount` 与基础平台的 `supportedExtensions[]`，PATCH 不允许改 platform/slug/default core。扩展名是带前导点、ASCII 小写、稳定有序且无重复的已验证游戏 payload 格式；不从目录默认核心反推。NES 为 `.nes/.unf/.unif/.fds`，Arcade 合并目录后仍只返回一次 `.zip`。普通 ROM 的 ZIP/7z 只作上传 wrapper，不进入该字段；DOS `.exe/.com/.bat` 是 payload，必须进入。 |
| `GET /api/v1/admin/platform-instances/recommendations` | 返回代码 catalog 的 `catalogVersion/items/summary`。每项包含 template key、顺序、名称/说明、Platform/Core 展示引用、从基础平台 profile 读取的扩展名、`ACTIVE/CUSTOMIZED/COVERED_BY_EQUIVALENT/SUPPRESSED/MISSING` 状态和可空目录 ID；无 query，不修改数据库。 |
| `POST /api/v1/admin/platform-instances/recommendations/apply` | ADMIN-only 一键补齐；body 必须是严格空对象 `{}` 并携带 UUID `Idempotency-Key`。一个 `BEGIN IMMEDIATE` 事务只为当前 `MISSING` 项创建目录，把它们按 catalog 顺序追加到现有最大顺序之后，同时提交逐项 AuditEvent 与幂等响应。成功为 200，返回 `created[]/createdTemplateKeys[]/items/summary`；重复、并发、已有等价目录、自定义/停用/软删除模板均不覆盖、不恢复、不重排。相同 key 重放原 status/body 并带 `X-Retrom-Idempotent-Replay: true`；任何创建或审计失败整体回滚。 |
| `PUT /api/v1/admin/platform-instances/order` | 以全部目录的 ID/version 原子替换显示顺序，供拖拽和键盘排序。 |
| `POST /api/v1/admin/platform-instances/{platformInstanceId}/default-core-preview`、`POST /api/v1/admin/platform-instances/{platformInstanceId}/default-core` | 默认核心影响 digest 与提交。 |
| `DELETE /api/v1/admin/platform-instances/{platformInstanceId}` | 只允许空目录软删除。 |
| `GET /api/v1/admin/bios`、`GET /api/v1/admin/bios/{requirementId}/entries`、`POST /api/v1/admin/bios/{requirementId}/installations` | BIOS 状态、Arcade ZIP 条目对比与从已完成 UploadFile 新建 installation revision。同 Requirement 的替换是破坏性边界，旧 Installation 只保留结构化审计并释放 payload；一期没有独立删除 Installation API。 |
| `GET /api/v1/admin/diagnostics` | 下载不含内容标识与路径的封闭 JSON 诊断摘要；只读、无需 Idempotency-Key，但仍受全局 readiness 门禁。 |
| `GET /api/v1/admin/storage-analysis` | ADMIN-only、`private, no-store` 的已登记 CAS payload 容量快照；无 query，不返回 Blob ID、hash、文件名、路径或 capability。 |
| `POST /api/v1/admin/storage-cleanups` | ADMIN-only 立即回收请求；无 query/body，要求 CSRF 与 UUID `Idempotency-Key`，202 返回被推进到立即执行的 Blob 数、byte 与接受时刻；不返回 Blob/Job 标识。 |

`GET /api/v1/games/{gameId}` 顶层返回非空 `currentContentRevisionId` 和全部未删除存档总数 `saveStateCount`，并在 `saveStates[]` 保留该游戏最近 8 份未删除手动存档的 `saveStateId/name/createdAtMs/core{id,name}` 轻量投影。详情页的一屏最近 3 份与全量 Drawer 统一通过 `GET /api/v1/saves?gameId=<id>&availability=ALL` 分页取全，避免把 8 份投影误当成全量。其 `coreOptions` 必须覆盖该基础平台全部 enabled Core，稳定按平台配置顺序返回：`coreId`、`name`、`isDefault`、`status=READY|NEEDS_VALIDATION|DEPENDENCY_MISSING|INCOMPATIBLE`、`revalidationStatus=NOT_REQUIRED|PENDING|FAILED`、可空 `currentVariantRevisionId/coreArtifactId/datVersionId/revalidationJobId`、`requiresThreads`、结构化 `reasons[]`。DEPENDENCY_MISSING 覆盖 BIOS/parent/base 的可修复缺失，具体标签由 reason code 决定，不得把 parent 缺失误称为 BIOS。主 status 以是否存在直接引用当前 ContentRevision 的 READY 结果及其锁定快照是否仍可部署计算；旧内容或相同 bytes 的另一 ContentRevision 曾验证成功不能冒充当前 READY，但活动 DAT 更新也不能让当前内容的旧锁定快照突然不可运行。`POST /api/v1/launches` 收到显式/默认 core 时按游戏目录第 5.1 节执行同一 `EnsureVariant`，必要时先返回同一 `VARIANT_REVALIDATE` Job；前端预热不是正确性前提，也没有第二个启动 endpoint。

诊断摘要成功响应固定为下列封闭 JSON，所有 count 为非负 int64，版本数组按 SemVer 升序；它从一个有界只读快照生成，不创建 Blob 或临时归档：

```json
{
  "schemaVersion": 1,
  "generatedAtMs": 1786000000000,
  "databaseSchemaVersion": 1,
  "dependencies": {
    "configuredEmulatorjsVersions": ["4.2.3", "4.3.0-pre"],
    "activeEmulatorjsVersion": "4.2.3"
  },
  "counts": {
    "games": {"published": 0, "deleted": 0},
    "saveStates": {"active": 0, "deleted": 0},
    "blobs": 0,
    "jobs": {"queued": 0, "running": 0, "cancelRequested": 0, "succeeded": 0, "failed": 0, "cancelled": 0},
    "datVersions": {"pending": 0, "parsing": 0, "ready": 0, "failed": 0, "cancelled": 0}
  }
}
```

响应固定为 `application/json; charset=utf-8`、`Cache-Control: private, no-store`、`Content-Disposition: attachment; filename="retrom-diagnostics.json"` 与 `X-Content-Type-Options: nosniff`。不得增加自由文本日志、资源 ID、游戏/文件名、Blob/DAT/core hash、上传/provider 原文、环境变量值、cookie/capability/key、内部地址或宿主路径；需要新增诊断字段时先升级 schemaVersion、OpenAPI 和 `ACC-OPS-001`，不能把任意 map 当作后门。

容量分析成功响应是同一只读事务生成的封闭对象：

```json
{
  "scope": "REGISTERED_CAS_PAYLOAD_V1",
  "generatedAtMs": 1787448000000,
  "totals": {"registeredBytes": "0", "protectedBytes": "0", "unreferencedBytes": "0", "blobCount": 0},
  "categories": [
    {"code": "GAME_CONTENT", "bytes": "0", "blobCount": 0},
    {"code": "BIOS", "bytes": "0", "blobCount": 0},
    {"code": "SAVES", "bytes": "0", "blobCount": 0},
    {"code": "MEDIA", "bytes": "0", "blobCount": 0},
    {"code": "WORKFLOW", "bytes": "0", "blobCount": 0},
    {"code": "RUNTIME_SNAPSHOT", "bytes": "0", "blobCount": 0},
    {"code": "SHARED_DURABLE", "bytes": "0", "blobCount": 0},
    {"code": "OTHER_REFERENCED", "bytes": "0", "blobCount": 0},
    {"code": "UNREFERENCED", "bytes": "0", "blobCount": 0}
  ],
  "details": {
    "saveStates": {"activeCount": 0, "deletedCount": 0, "stateReferenceBytes": "0", "screenshotReferenceBytes": "0"},
    "cleanupCandidates": {"blobCount": 0, "bytes": "0"}
  },
  "excluded": ["DATABASE_FILES", "UPLOAD_PARTS", "JOB_SCRATCH", "DEPENDENCY_ROOT", "FILESYSTEM_OVERHEAD", "UNREGISTERED_ORPHANS", "VOLUME_FREE_SPACE"]
}
```

Byte 数只能是无符号十进制字符串；count 和 `generatedAtMs` 为非负 int64。`categories` 恰含上述九类和顺序，零值不省略；顶层与分类恒等式、归类优先级和排除边界由[存储专题第 7.1 节](./storage-and-database.md#71-已登记-cas-容量分析)定义。GET 不设置下载 header，不提供 query、cursor 或任何资源标识；失败返回通用错误 envelope，不返回半个快照。

立即清理成功响应固定为 `{"scheduledBlobCount":0,"scheduledBytes":"0","acceptedAtMs":1787448000000}`。POST 先按同一 registry 补齐未引用候选，再把当前仍未引用候选推进为立即可执行；失败的 BLOB_GC Job 以新 execution 重排队。返回值是已接受调度量而非已物理删除量，worker 删除前仍复核引用，恢复引用的 Blob 会被保留。相同 principal/operation/key 重放原 202 响应；缺少/复用错误 key、USER/匿名、CSRF 失败和数据库失败使用通用稳定错误。统一验证为 `ACC-STOR-001`。

## 10. 收藏与收藏夹 API

### 10.1 通用协议

所有路由要求有效 AuthSession，ADMIN 与 USER 都只能使用 Principal 的 `profile_id`；请求 path/query/body 和响应 DTO 均不接受或返回 owner。写请求执行全局精确 Origin、Fetch Metadata 与 `X-Retrom-Csrf` 检查；所有响应固定 `Cache-Control: private, no-store`。JSON schema 封闭，拒绝重复字段、未知字段、重复 ID 和非规范 UUID。

集合式 PUT 自然幂等，不要求 `Idempotency-Key`。所有批量 POST 与 Folder create/PATCH/DELETE 要求规范 UUID `Idempotency-Key`；同 principal、operation、key 重放原响应并返回 `X-Retrom-Idempotent-Replay: true`，同 key 异语义为 `409 IDEMPOTENCY_KEY_REUSED`。Folder PATCH/DELETE 还要求 `If-Match: "v<version>"`。幂等 namespace 和 cursor digest 都绑定 Principal User ID，不能跨账号复用结果。

### 10.2 Route 与写入上限

| 方法与路径 | 请求 | 成功响应与语义 |
| --- | --- | --- |
| `GET /api/v1/favorites` | 第 10.3 节 query | `200 FavoriteListResponse`；同一只读事务返回 scope 结果、精确计数、Folder 与平台摘要。 |
| `PUT /api/v1/favorites/{gameId}` | `{}` | `200 FavoriteState`；Game 必须当前可见，不存在则创建，已存在原样返回且不刷新 `favoritedAtMs`。 |
| `PUT /api/v1/favorites/{gameId}/folders` | `folderIds` 必填、唯一、0–100 | `200 FavoriteState`；精确替换完整 Folder 集合并在需要时自动收藏。 |
| `POST /api/v1/favorites/organize` | 1–50 `gameIds`；`addFolderIds/removeFolderIds` 各 0–20，互斥且总边数不超过 1000 | `200 FavoriteBatchResult`；整批原子执行，add 自动收藏，空动作或重复 ID 拒绝。 |
| `POST /api/v1/favorites/unfavorite` | 1–100 `gameIds` | `200 UnfavoriteResult`；按稳定顺序返回删除前的 `gameId/folderIds` 快照，不返回 Folder 名称；不存在的 Favorite 不泄漏并产生空项结果。 |
| `POST /api/v1/favorites/restore` | 1–100 个 `{gameId,folderIds}`，总 Folder 引用不超过 1000 | `200 FavoriteRestoreResult`；只恢复仍可见 Game 和仍属于 Principal 的 Folder，返回排序且去重的 restored/skipped IDs。 |
| `POST /api/v1/favorite-folders` | `name` 与必填 `initialGameIds` 0–100 | `201 FavoriteFolder` + `Location` + `ETag`；原子创建 Folder，并收藏、分类显式给出的 Game；`[]` 表示创建空 Folder。 |
| `PATCH /api/v1/favorite-folders/{folderId}` | `name` + `If-Match` + `Idempotency-Key` | `200 FavoriteFolder` + 新 `ETag`；只重命名并把版本精确加一。 |
| `DELETE /api/v1/favorite-folders/{folderId}` | `{}` + `If-Match` + `Idempotency-Key` | `204`；删除 Membership 和 Folder，保留 Favorite。 |

所有 ID 数组必须唯一；服务端不静默去重。任一资源、owner、可见性、版本或上限校验失败都使整个请求零写入。

### 10.3 列表 query、排序与计数

`GET /api/v1/favorites` 只接受以下 query：

| Query | 规则 |
| --- | --- |
| `scope` | `ALL|UNCATEGORIZED|FOLDER`，默认 `ALL`。 |
| `folderId` | 仅 `scope=FOLDER` 时必填；其他 scope 携带该字段非法，且 ID 必须属于当前 Profile。 |
| `q` | 复用 Game `search_text` 规范化，最多 200 code point。 |
| `platformId` | 稳定平台 code；不存在时返回空结果，不报错。 |
| `sort` | 默认 `FAVORITED_DESC`；另有 `RECENTLY_PLAYED_DESC|TITLE_ASC|RELEASE_YEAR_DESC`。 |
| `cursor/limit` | `limit` 默认 50、范围 1–100；签名 cursor 24 小时到期，绑定 User、scope、folderId、q、platformId 和 sort。 |

稳定排序 tuple 为：

- `FAVORITED_DESC`：`favorite_games.created_at_ms DESC, game_id DESC`；
- `RECENTLY_PLAYED_DESC`：有 PlaySession 的项优先，`last_played_at_ms DESC, title ASC, game_id ASC`，未游玩项随后按标题和 ID；
- `TITLE_ASC`：`title ASC, game_id ASC`；
- `RELEASE_YEAR_DESC`：非空年份优先，`release_year DESC, title ASC, game_id ASC`，空年份随后按标题和 ID。

`summary.favoriteCount` 是所有可见 Favorite 数，`uncategorizedCount` 是其中无 Membership 的可见数，`folderCount` 包含空 Folder。`folders[].visibleGameCount` 独立于当前 q/platform/scope；`platforms[]` 在当前 scope 内、应用 q/platform 前生成；`totalCount` 是应用 scope+q+platform 后的完整结果数，不是当前页长度。上述数据和 `items` 来自同一 SQLite 只读事务。

Cursor 只保证稳定 tuple 与筛选绑定，不提供跨请求快照隔离。任一收藏写入成功后，客户端必须丢弃旧 cursor 并从首页刷新，不能拼接写入前后的页。

### 10.4 DTO 投影

`FavoriteState` 固定为 `gameId/favoritedAtMs/folderIds`。`FavoriteFolder` 固定为 `folderId/name/version/visibleGameCount/createdAtMs/updatedAtMs`。`FavoriteListResponse` 固定包含 `generatedAtMs/summary/folders/platforms/totalCount/items/nextCursor`；每个 item 提供 Game、platform、PlatformInstance、defaultCore、封面、发行年份、最近游玩时间和非空 Favorite 投影。Folder ID 按 Folder `created_at_ms,id` 排序。

`UnfavoriteResult.items[]` 只返回删除前的 `gameId/folderIds`，按请求 Game ID 的 UTF-8 bytes 排序。`FavoriteRestoreResult` 返回 `restoredGameIds/skippedGameIds/skippedFolderIds`，每个数组排序且不重复。

用户侧 `/api/v1/games` 列表和详情增加可空字段：

```json
"favorite": {
  "favoritedAtMs": 1786000000000,
  "folderIds": ["01980000-0000-7000-8000-000000000101"]
}
```

未收藏时为 `null`。管理侧 `/api/v1/admin/games*` 不返回 Favorite 字段、逐用户计数或 Folder 信息。

### 10.5 稳定错误

| HTTP | code | 条件 |
| ---: | --- | --- |
| 400 | `INVALID_QUERY` | scope/folderId 组合、sort、limit 或未知 query 非法。 |
| 400 | `INVALID_REQUEST` | 重复 ID、add/remove 相交、空 organize、未知字段或 Folder 名称非法。 |
| 400 | `INVALID_CURSOR` | cursor 签名、主体、筛选、排序、版本或到期不匹配。 |
| 400 | `INVALID_IDEMPOTENCY_KEY` | 要求幂等的写入缺失或携带非规范 UUID key。 |
| 401 | `AUTHENTICATION_REQUIRED` | 没有有效 AuthSession。 |
| 404 | `GAME_NOT_FOUND` | 写入要求的 Game 不存在、已删除或所属目录停用。 |
| 404 | `FAVORITE_FOLDER_NOT_FOUND` | Folder 不存在或不属于当前 Profile，两种情况不区分。 |
| 409 | `FAVORITE_FOLDER_NAME_CONFLICT` | 当前 Profile 已有相同 `name_key`。 |
| 409 | `IDEMPOTENCY_KEY_REUSED` | 同 operation/key 已绑定不同语义请求。 |
| 412 | `RESOURCE_VERSION_CONFLICT` | Folder `If-Match` 已过期。 |
| 413 | `FAVORITE_BATCH_TOO_LARGE` | 超过 Game、Folder 或总边数任一上限。 |
| 422 | `FAVORITE_FOLDER_LIMIT_REACHED` | 当前 Profile 已有 100 个 Folder。 |
| 428 | `PRECONDITION_REQUIRED` | Folder PATCH/DELETE 缺少或携带非法 `If-Match`。 |

精确机器 schema 以 [`../api/openapi.yaml`](../api/openapi.yaml) 为入口的领域文件集为准；人类可读契约与 schema 发生漂移时必须在同一变更修正，验收见 `ACC-FAV-002`。

## 11. 服务器 BIOS 导入 API

所有下列 route 都要求 ADMIN；匿名返回 401，USER 返回 403。写请求继续执行 Origin/Fetch Metadata/CSRF、Idempotency-Key 与 `If-Match` 规则。DTO 永不包含宿主绝对路径、root digest、CAS path 或 source inode。

| Route | 契约 |
| --- | --- |
| `GET /api/v1/admin/server-import-roots` | 返回配置 root 的 `id/label/status`。 |
| `GET /api/v1/admin/server-import-roots/{rootId}/directories` | `path` 为规范相对目录，`limit<=100`；cursor 绑定 root/path/operation，仅列直接子目录。 |
| `POST /api/v1/admin/server-imports` | 封闭 body：`kind=BIOS_DIRECTORY`、`rootId`、`sourceRelativePath`、`replaceIfBetter`；成功 201/Location/ETag，重放同 body 返回同资源。 |
| `GET /api/v1/admin/server-imports` | 按 `createdAtMs DESC,id DESC` cursor 分页，`limit<=20`。 |
| `GET /api/v1/admin/server-imports/{id}` | 结果按稳定 key cursor 分页，`limit<=50`，支持 `q/outcome/matchMethod`。 |
| `GET .../{id}/bios-items/{requirementId}/candidates` | 按 rank/id cursor 分页，`limit<=50`，返回证据与未选原因。 |
| `POST .../{id}/cancel` / `retry` | 使用 ETag；cancel 保留已提交 Item，retry 只允许明确 retryable 的领域失败且重验 root/catalog digest。 |

稳定错误至少包括配置/root/path/cursor/active-conflict/source-or-catalog-change/scan-limit/retry-not-allowed 等 OpenAPI 枚举；详细字段和 response 是以 [`../api/openapi.yaml`](../api/openapi.yaml) 为入口的 OpenAPI 文件集所定义的唯一机器契约。

`GET /api/v1/admin/bios` 的 FULL_CATALOG 以及所有服务端筛选固定 `limit<=100`、cursor 绑定 scope 与完整 query。每页 items 不影响 `scopeCounts/summary/filteredCount`，这些值始终基于服务端全集；客户端不得把首批 100 条当成完整目录。

## 12. Pegasus 导入与详情 VIDEO API

Pegasus route 全部要求 ADMIN，写请求执行同一 Origin/Fetch Metadata/CSRF、UUID Idempotency-Key 与 `If-Match`。DTO 只返回 root ID/label、规范相对路径、稳定 code 和审计投影，不返回宿主路径、source facts/inode、Blob ID/hash 或原始 metadata/command。

| Route | 契约 |
| --- | --- |
| `POST/GET /api/v1/admin/pegasus-imports` | POST `{rootId,sourceRelativePath}` 返回 202 scan plan；GET 按 `createdAtMs DESC,id DESC`、`limit<=20` 分页并可筛 state。 |
| `GET/DELETE /api/v1/admin/pegasus-imports/{id}` | GET 返回 aggregate、两个 Job ID、phase/counts、mapping/version/expiry 与 ETag；DELETE 只删除无 execution 结果的 `AWAITING_MAPPING|EXPIRED` 投影。 |
| `GET .../{id}/collections` | `limit<=100`，cursor 绑定 import；返回 metadata 相对路径、segment、name/shortname、game/issue 数与当前映射。 |
| `PUT .../{id}/collection-mappings` | 最多 100 个精确 replacement；每项只能为 `IMPORT+platformInstanceId` 或 `SKIP`，没有 suggestion/default。 |
| `POST .../{id}/start` | body `version` 必须等于 `If-Match`；映射完整、至少选择一个 Collection、计划未过期且 metadata snapshot 未漂移才返回 202。 |
| `GET .../{id}/items` | `limit<=50`，cursor 绑定 `q/outcome/warning/collectionId`；返回映射、内容类型、`payloadState/payloadReleaseJobId`、COVER/VIDEO 状态、warning、全部 existing matches、可空 `reviewItemId` 与发布/已有 Game 链接 ID。RELEASED 后媒体数组不再提供可加载内容，只显示来源结构化结论。execution state 包含 `REVIEW_PENDING/REVIEW_DISCARDED`；前者的 `reviewItemId` 是逐项审核入口。存在 library runtime validation 时同时返回 `runtimeCheck`：稳定 `status/code`、Core、machine、缺失/不匹配条目、parent/BIOS 逻辑依赖及其必需 entry、多盘缺失引用。内部失败时返回可空 `failureDetails`：stage、operation、causeCode、受限 technicalDetail、来源相对路径、观察文件数/上限，以及已创建时的内部 ImportJob/ImportItem ID；不得返回 Blob/hash、宿主绝对路径、凭据或未截断上游 payload。 |
| `POST .../{id}/cancel|retry` | cancel 不回滚已发布 Game，也不删除已经交接的审核事项；retry 仅在 aggregate `retryable=true` 时创建新 execution。确定性 validation blocker 进入审核处理，不作为 Pegasus retry 项。 |

Aggregate `counts` 除扫描/映射/阻断/失败等既有字段外固定包含 `reviewPending/published/reviewDiscarded`。任务 `COMPLETED` 只表示审核事项准备结束，不表示全部游戏已发布；后续逐项审核或严格 READY 快速审批的每个成功 Item 都原子推进三个计数和 aggregate version。快速审批使用独立 aggregate route，不在 Pegasus route 内建立第二套发布动作。

稳定错误包括 `PEGASUS_METADATA_NOT_FOUND`、`PEGASUS_SCAN_LIMIT_EXCEEDED`、`PEGASUS_MAPPING_INCOMPLETE`、`PEGASUS_NO_COLLECTION_SELECTED`、`PEGASUS_SOURCE_CHANGED`、`PEGASUS_PLAN_EXPIRED`、`PEGASUS_IMPORT_ACTIVE`、`PEGASUS_LIBRARY_IMPORT_FAILED`、`SERVER_IMPORT_ROOT_CHANGED` 与 `SERVER_IMPORT_SOURCE_NOT_RESTORED`；Item/warning 使用 OpenAPI 的封闭状态与稳定 code。`failureDetails.causeCode` 至少区分 `SOURCE_FILE_LIMIT_EXCEEDED`、`LIBRARY_IMPORT_INPUT_INVALID`、`MULTI_DISC_MODE_UNAVAILABLE`、`DATABASE_BUSY`、`DATABASE_CONSTRAINT_FAILED`、`OPERATION_TIMEOUT`、`OPERATION_CANCELLED`、`METADATA_JSON_INVALID` 与 `INTERNAL_OPERATION_FAILED`。library validation 的 `LAUNCH_BIOS_MISSING`、`LAUNCH_PARENT_MISSING`、`ARCADE_CONTENT_MISSING_ENTRY`、`ARCADE_DEPENDENCY_MISMATCH`、`UNSUPPORTED_MERGED_ROMSET`、`UNSUPPORTED_CHD`、`ARCADE_DAT_UNAVAILABLE`、`ARCADE_DEPENDENCY_CYCLE` 与 `MULTI_DISC_FILE_MISSING` 等 compatibility code 必须原样保留，客户端不得根据 message 反推原因。

`GET /api/v1/games/{gameId}` 增加可空 `videoUrl=/content/assets/{assetId}`，只指向 current MetadataRevision 的 ordinal 0 VIDEO；普通列表/Home/Recent/Favorites/Saves DTO 均无该字段，唯一额外用户投影是第 12.2 节的沉浸游戏平台列表。管理 `POST /api/v1/admin/games/{gameId}/assets` 接受 VIDEO，`DELETE .../assets/VIDEO` 以新 MetadataRevision 移除当前视频。current 切换后旧 Asset 行被删除，旧逻辑 URL 返回 404；同一 Asset ID 在存续期间仍沿用强 ETag、immutable cache、`nosniff`、完整 GET 与单 Range。非法/多 Range、不可见 Game 与未知/已退役 Asset 继续使用统一拒绝语义。

## 12.1 EmulationStation 服务器目录导入 API

全部 route 要求 ADMIN。写入沿用 strict JSON、唯一标量 query、Origin/Fetch Metadata、CSRF 与 UUID Idempotency-Key；除 create 外的状态写要求 `If-Match`。DTO 只返回 root ID/label、规范相对路径、稳定 code 与审计投影，不返回绝对路径、root/source facts digest、Blob ID/hash、XML 原文、`command/emulator/core/provider` 值或底层错误。

| Route | 契约 |
| --- | --- |
| `POST /api/v1/admin/emulationstation-imports` | body `{rootId,sourceRelativePath}`；创建 aggregate 与 `SERVER_EMULATIONSTATION_SCAN` Job，返回 202、summary、Location 与 ETag。 |
| `GET /api/v1/admin/emulationstation-imports` | `state/cursor/limit<=20`；按 `createdAtMs DESC,id DESC`。 |
| `GET /api/v1/admin/emulationstation-imports/{id}` | aggregate、scan/import Job、phase、全部 counts、version/expiry/retryable 与 ETag。 |
| `DELETE /api/v1/admin/emulationstation-imports/{id}` | 仅无 execution 结果的 `AWAITING_MAPPING|EXPIRED` 计划；成功 204，其他状态冲突。 |
| `GET .../{id}/gamelists` | `parseState/cursor/limit<=100`；返回有效/无效文件与稳定错误，不返回 XML。 |
| `GET .../{id}/collections` | `cursor/limit<=100`；返回清单路径、相对目录、展示名、扩展摘要、folder/hidden/adult/issue 计数和当前 mapping。 |
| `PUT .../{id}/collection-mappings` | body `{mappings:[{collectionId,action,platformInstanceId?,tagIds}]}`，1–100 项精确 replacement；只接受 `IMPORT+target` 或 `SKIP`，没有 suggestion/default。 |
| `POST .../{id}/start` | body `{version}` 必须与 ETag 一致；同步重验不超过 64 MiB 的全部清单、完整 mapping、至少一个非空 IMPORT、计划/root/source/target snapshot 后返回 202。 |
| `GET .../{id}/items` | cursor 绑定 `q/outcome/warning/collectionId`，`limit<=50`；返回执行/释放/运行检查/失败详情、媒体、来源 flag、review 与 Game 链接。 |
| `POST .../{id}/cancel` | body `{reason}`；同步收口为 200，否则 202；不删除已交接审核事项或回滚已发布 Game。 |
| `POST .../{id}/retry` | 仅 aggregate `retryable=true`；创建同一 import Job 的新 execution/input snapshot并先重验 root/清单，返回 202。 |

`EmulationStationImportSummary.counts` 固定包含 `gamelists/invalidGamelists/collections/foldersIgnored/games/estimatedSourceBytes/mappedCollections/skippedCollections/skippedMapping/processable/blocked/reviewPending/published/reviewDiscarded/existing/failed/cancelled/mediaWarnings/covers/videos`。任务 `COMPLETED` 只表示审核事项准备结束，不代表全部发布；后续逐项或严格 READY 快速审批的成功决定原子推进来源 aggregate 与普通 ImportJob。

Gamelist item 固定为 `relativePath/parseState/errorCode/gameCount/folderCount/providerPresent/ignoredFieldNames/ignoredFieldOtherCount/createdAtMs`。`ignoredFieldNames` 去重后按 ASCII 元素名排序且最多 64 个；无效清单也只返回相对路径和稳定 code。Collection 固定为 `id/gamelistRelativePath/relativeDirectory/displayName/gameCount/issueCount/folderEntryCount/hiddenGameCount/adultGameCount/extensionSummary[{extension,count}]/extensionOtherCount/mappingAction/targetPlatformInstanceId/name/targetDefaultCoreId/name/tags`。Item 与 Pegasus Item 同构并增加 `gamelistRelativePath/sourceFlags{hidden,adult,kidGame}`；其余固定投影 `executionState/payloadState/contentKind/media/warnings/discoveryCode/errorCode/failureDetails/runtimeCheck/retryable/reviewItemId/publishedGameId/existingGameId/existingMatches/tags/updatedAtMs`。

计划级稳定错误为：

```text
EMULATIONSTATION_GAMELIST_NOT_FOUND
EMULATIONSTATION_NO_VALID_GAMELIST
EMULATIONSTATION_SCAN_LIMIT_EXCEEDED
EMULATIONSTATION_MAPPING_INCOMPLETE
EMULATIONSTATION_NO_COLLECTION_SELECTED
EMULATIONSTATION_MAPPING_TARGET_CHANGED
EMULATIONSTATION_SOURCE_CHANGED
EMULATIONSTATION_PLAN_EXPIRED
EMULATIONSTATION_IMPORT_ACTIVE
EMULATIONSTATION_IMPORT_NOT_FOUND
EMULATIONSTATION_IMPORT_NOT_CANCELLABLE
EMULATIONSTATION_IMPORT_NOT_RETRYABLE
EMULATIONSTATION_LIBRARY_IMPORT_FAILED
SERVER_IMPORT_ROOT_CHANGED
SERVER_IMPORT_SOURCE_NOT_RESTORED
```

解析/条目级稳定错误为：

```text
EMULATIONSTATION_GAMELIST_TOO_LARGE
EMULATIONSTATION_GAMELIST_INVALID_UTF8
EMULATIONSTATION_XML_INVALID
EMULATIONSTATION_XML_ROOT_INVALID
EMULATIONSTATION_XML_LIMIT_EXCEEDED
EMULATIONSTATION_GAME_PATH_MISSING
EMULATIONSTATION_GAME_PATH_AMBIGUOUS
EMULATIONSTATION_PATH_INVALID
EMULATIONSTATION_SOURCE_NOT_FOUND
EMULATIONSTATION_SOURCE_NOT_REGULAR
EMULATIONSTATION_CONTENT_FORMAT_UNSUPPORTED
EMULATIONSTATION_MEDIA_MISSING
EMULATIONSTATION_MEDIA_READ_FAILED
EMULATIONSTATION_IMAGE_INVALID
EMULATIONSTATION_VIDEO_UNSUPPORTED
EMULATIONSTATION_VIDEO_TOO_LARGE
EMULATIONSTATION_EXECUTION_FIELD_IGNORED
```

M3U 与 library validation 保留现有 `MULTI_DISC_*`、`LAUNCH_*`、`ARCADE_*` code，不包装为模糊来源错误。`failureDetails.causeCode` 复用 Pegasus 的 `SOURCE_FILE_LIMIT_EXCEEDED/LIBRARY_IMPORT_INPUT_INVALID/MULTI_DISC_MODE_UNAVAILABLE/DATABASE_BUSY/DATABASE_CONSTRAINT_FAILED/OPERATION_TIMEOUT/OPERATION_CANCELLED/METADATA_JSON_INVALID/INTERNAL_OPERATION_FAILED`。HTTP 状态继续遵循统一 envelope；扫描期 XML/路径/容量问题进入 Job/aggregate，不把 create 伪装成同步解析。

## 12.2 沉浸模式只读 API

沉浸模式使用独立、只读的电视交互投影，不复用普通游戏库的搜索、筛选或管理 DTO。四个端点均要求有效
Profile 会话，只返回当前用户可见且已发布、可运行的游戏；Favorite/Folder、SaveState 与 PlaySession
投影必须使用 Principal 的 `profile_id`。查询不得暴露内部 Blob ID、宿主路径、内容逻辑名或 Launch
capability；运行时内容身份只由 Launch config 的受权 URL 返回。

`GET /api/v1/immersive/destinations` 在一个只读事务中返回 `generatedAtMs/items`。`items` 固定先按
`all,recent,favorites,saves` 返回“全部游戏、最近游玩、收藏游戏、我的存档”四张卡，再追加有可见游戏的
平台卡；四张资料库卡即使计数为零也保留，平台卡仍按规范化平台名、`platformId` 排序。每项包含
`destinationId/kind/name/gameCount/lastPlayedAtMs/featuredGames`，预览最多三款，只使用 current COVER；
Favorite、SaveState 和最近时间不得跨 Profile 聚合。

`GET /api/v1/immersive/platforms` 返回：

- 顶层 `generatedAtMs` 与 `items`；
- 每项包含稳定 `platformId`、展示名 `platformName`、`gameCount`、可空 `lastPlayedAtMs`，以及最多三项
  `featuredGames=[{gameId,title,coverUrl,lastPlayedAtMs}]`；
- 只保留至少有一款可见游戏的已启用平台，按规范化平台名、`platformId` 稳定排序；
- `lastPlayedAtMs` 为当前 Profile 在该平台所有可见游戏上的 `PlaySession.started_at_ms` 最大值，未游玩为 `null`。
- `featuredGames` 先按当前 Profile 的 `lastPlayedAtMs DESC` 选择最近游玩游戏，再按游戏
  `created_at_ms DESC,game_id DESC` 用尚未游玩的最近加入游戏补足三项；封面只投影当前 MetadataRevision 的
  ordinal 0 `COVER`，缺失为 `null`。平台统计与预览必须在同一个只读事务中生成，不能跨请求拼接或泄漏其他
  Profile 的最近游玩顺序。

`GET /api/v1/immersive/platforms/{platformId}/games?cursor=&limit=` 返回：

- `limit` 默认且最大为 50；除此之外不接受搜索、排序、标签或状态查询参数；
- 平台不存在、已禁用或对当前 Profile 没有可见游戏时统一返回 404 `IMMERSIVE_PLATFORM_NOT_FOUND`，不能泄露隐藏平台；
- 按 current MetadataRevision 的 `title_initial ASC,title COLLATE NOCASE ASC,gameId ASC` 稳定分页；签名 cursor 绑定 Profile、route、`platformId` 与 `limit`，过期、篡改或跨范围复用返回 400 `INVALID_CURSOR`；
- 响应顶层 `platform` 固定为本页所属平台；每项包含 `gameId/title/titleInitial/platformInstance/defaultCore`、当前元数据中的 `description` 与可空 `releaseYear/developer/genre`、当前 Profile 的可空 `lastPlayedAtMs`、`favorited/saveStates`，以及 current 媒体 ordinal 0 的可空 `coverUrl/videoUrl`；
- 媒体 URL 必须继续进入既有受 AuthSession 保护的内容端点，不建立沉浸模式专用 Blob 旁路；
- 响应包含 `generatedAtMs`、`items` 与可空 `nextCursor`。空的后续页返回 200 空数组，只有平台整体不可见时返回 404。

`GET /api/v1/immersive/libraries/{libraryKind}/games?folderId=&cursor=&limit=` 的 `libraryKind` 只接受
`all|recent|favorites|saves`，`limit` 默认且最大 50。`folderId` 只可用于 favorites，必须属于当前 Profile；
favorites 响应同时返回该 Profile 的 `folders` 与当前可空 `folder`，其他范围两者为空。all/favorites/saves
使用上述 `titleInitial/title/gameId` 顺序；recent 例外地按本 Profile `lastPlayedAtMs DESC,gameId DESC`，没有
游玩记录的游戏不进入 recent。saves 只列至少有一份当前可用存档的游戏，每项 `saveStates` 按
`createdAtMs DESC,saveStateId DESC` 返回，并投影真实 payload `sizeBytes`；截图 URL 继续是私有 no-store
端点。签名 cursor 绑定 Profile、
libraryKind、folderId、limit 与排序版本；未知 query、非法组合、篡改或跨范围 cursor 均稳定拒绝。

`ImmersiveGameItem` 的 `titleInitial` 只接受 `#|0-9|A-Z`；ASCII 数字原样、ASCII 字母转大写、首个汉字取
拼音首字母大写、其他首字符为 `#`。该值来自不可变 MetadataRevision，不由 HTTP handler 临时推断。
每个 item 的 `defaultCore` 必须来自当前 PlatformInstance 默认 Core；`favorited` 表示当前 Profile 是否收藏，
Y 默认收藏写入仍使用既有 `PUT /api/v1/favorites/{gameId}`，不会自动加入任意 Folder。

全部响应都是当次读取快照的展示投影，不建立持久化沉浸会话。客户端返回后台再进入时重新获取数据，不依赖
旧页面中的可见性或容量结论；创建 Launch、切换收藏、创建存档仍调用各自既有写 API，不在只读投影中复制
第二套状态机。

## 13. 受限联机 REST、SSE、WebSocket 与凭据

全部 `/api/v1/netplay/**` 要求有效 AuthSession；写入继续要求同源 Origin/Fetch Metadata、CSRF 和表中列出的幂等/ETag。`RETROM_NETPLAY_ENABLED=false` 时路由根本不注册并返回 404，auth context 投影 `netplayEnabled=false`。Room GET 对任意已登录房间链接访问者开放最小投影，成员身份、权限和写入始终从当前 Profile 推导，不能相信 body 中的 actor/seat/profile。

| Route | 并发/幂等 | 成功语义 |
| --- | --- | --- |
| `GET /api/v1/netplay/games?availability=SUPPORTED\|ALL&cursor=&limit=` | 签名 cursor，默认且最大 100 | 按 lowercase title+gameId 稳定分页；cursor/limit 先约束候选行，再只对生成当前响应所需的候选执行 eligibility 与标签装配，不能先展开整个游戏库再切页。SUPPORTED 必须同时命中 profile 的显式 `platformIds`、core artifact、内容类型与当前依赖快照；item 的 `platformId/platformName` 是房间平台标记与筛选的唯一来源。item 只给展示字段、`SUPPORTED/UNSUPPORTED`、可选 profile summary 和封闭 blocker，不返回内容/core hash。 |
| `GET /api/v1/netplay/rooms?view=active\|recent&cursor=&limit=` | 签名 cursor，默认 24 | active 只列本人主持/占座的非终态；recent 只列本人参与且 24 小时内的终态，按 updatedAt+roomId 倒序。 |
| `POST /api/v1/netplay/rooms` | I | 201 + Location/ETag；DRAFT 且房主原子占 P1。 |
| `GET /api/v1/netplay/rooms/:roomId` | — | 200 + ETag；最小 Room/game/member/session/permissions 投影。 |
| `PUT/DELETE /api/v1/netplay/rooms/:roomId/game` | I/E | 房主在 DRAFT/WAITING 选择 exact game/core-profile 或清回 DRAFT；保留座位、清 ready。 |
| `PUT .../members/me/seat`、`PUT .../members/me/ready` | I/E | WAITING 中占/换 P2–P4 与切换 ready；越界、占用和 profile stale 原子拒绝。 |
| `DELETE .../members/me`、`DELETE .../members/:memberId` | I/E | 访客离开；房主仅可在 WAITING 移出访客。运行中访客离开等价全局 `USER_EXIT`。 |
| `POST .../start` | I/E | 202；仅房主且至少两位、所有 active member ready；原子锁定 Session/Participant snapshot。 |
| `POST .../sessions/:sessionId/launch` | I | 本人 Participant 首次 201、重放 200；设置本人 launch cookie 与 room cookie，只返回本人 playUrl。 |
| `POST .../pause`、`POST .../resume` | I | 202；仅 P1，pause 在 canonical 边界停，resume 经 `RESYNCHRONIZING` state transfer 开新 epoch。 |
| `POST .../end`、`DELETE /api/v1/netplay/rooms/:roomId` | I（DELETE 另 E） | 任一 Participant 以 `USER_EXIT` 结束当前 Session并撤销全体 Launch/cookie；访客同时释放座位、房间回 WAITING，房主结束本局时保留成员。DELETE 仅房主可用，以 `HOST_CLOSED` 关闭整个房间。 |
| `GET .../events` | Last-Event-ID | `text/event-stream`；先发 `room.snapshot`，之后只发 `room.updated/member.updated/session.updated/room.ended`。响应固定 `Cache-Control: private, no-store, no-transform`、`Content-Encoding: identity` 与 `X-Accel-Buffering: no`，任何反向代理都不得压缩或缓冲事件；每次 snapshot/event/heartbeat 写与 flush 前刷新 30 秒 write deadline，错误处理与通用 Job SSE 相同。 |

Room DTO 固定含 `roomId/state/version/game/members/currentSession/permissions/selfMemberId/expiresAtMs/serverNowMs/endedAtMs/endReason`；成员最多四个，不返回 profileId、credential 或输入。Game eligibility 每项最多返回八个 exact profile 摘要；blocker 只允许 `CONTENT_NOT_ALLOWLISTED/CORE_NOT_ALLOWLISTED/DEPENDENCY_STALE/GAME_UNAVAILABLE`；其中 `CONTENT_NOT_ALLOWLISTED` 只表示 content kind 不在协议允许集合，不表示单个 ROM 未登记，`CORE_NOT_ALLOWLISTED` 同时覆盖平台/core 组合未登记。稳定领域错误包括 `NETPLAY_ROOM_NOT_FOUND/SESSION_NOT_FOUND/FORBIDDEN/INVALID_SEAT/INVALID_PROFILE/SEAT_TAKEN/ROOM_NOT_READY/ROOM_STATE_CONFLICT/PROFILE_STALE/CAPACITY_REACHED/PRECONDITION_FAILED`；SSE 超限使用 `429 NETPLAY_RATE_LIMITED`。每房最多 16 条 SSE、每 Profile/房最多 2 条，连接 30 分钟关闭，客户端断线后退化为有界轮询。客户端必须按 `version` 单调应用 SSE、GET 与 mutation 快照，旧响应不得覆盖新状态；ready 写入若因刚收到的成员更新返回一次 `PRECONDITION_FAILED`，客户端刷新后仅在仍为 WAITING、本人仍可 ready 且目标 ready 状态尚未满足时，以新 ETag 重试一次。

WebSocket 固定 `GET /runtime/netplay/rooms/{roomId}/socket`、子协议 `retrom.netplay.v1`。Upgrade 必须同时满足唯一且逐字匹配的同源 `Origin`、有效 AuthSession、精确一项子协议、active 本人联机 Launch 和 room cookie；query/fragment/subprotocol 不得携带凭据。Chromium 的 WebSocket handshake 不发送 Fetch Metadata，因此该 header 缺失时仍只依靠强制 Origin；若出现则必须是唯一的 `Sec-Fetch-Site: same-origin`，多值或 cross-site 均拒绝。认证查询的“未找到/已撤销”映射为普通拒绝；数据库或存储失败必须保留为 service error，不能伪装成凭据失效并清 cookie。连接后的周期认证采用 `VALID/REVOKED/UNAVAILABLE` 三态：成功即清零连续不可用计数，前两次 UNAVAILABLE 只记录低基数 warning，连续第三次只丢弃该 transport；REVOKED 立即以 `AUTH_REVOKED` 结束全局 Session。

JSON 最大 64 KiB、INPUT 最大 4 KiB且每连接采用 120 条/秒、burst 240 的 token bucket，连接 read limit 2 MiB；HELLO 必须在 10 秒内到达。重复 JSON key、未知字段、深度超过 8、非连续 client seq、错误 session/epoch/player、限流或越界 frame/control 都以 policy violation 结束全局 Session。服务端发送 WELCOME/REQUEST_STATE/STATE_META/START_EPOCH/CANONICAL/HISTORY/PAUSE/SESSION_ENDED；客户端发送 HELLO/RUNTIME_READY/INPUT/HASH/PAUSED/STATE_META/STATE_READY/STATE_APPLIED/HISTORY_APPLIED/END_REQUEST。END_REQUEST.reason 只接受 `USER_EXIT/ROLLBACK_WINDOW_EXCEEDED/STATE_RING_CAPACITY_EXCEEDED/STATE_INVALID/NETPLAY_UNSTABLE/INTERNAL_ERROR/PROTOCOL_VIOLATION`，服务端持久化相应终因并发出权威 SESSION_ENDED；客户端不得用 WebSocket close code 代替这次终局握手。窗口失焦或隐藏只在浏览器本地清空输入，不发送协议消息。

同一参与者的新连接替换旧连接时，旧连接的关闭不能解释为用户退出；若替换发生在初始 state transfer 中，服务端停止旧 transfer、生成新 transferId 并从 P1 重新发送完整状态。state transfer 没有独立的 15 秒 wall-clock 终局 timer，存活性只由 HELLO/socket/ping、认证、协议和 Session 恢复租约决定。PAUSE 后每个 occupied seat 必须先回滚到指定 canonical 边界并发送 PAUSED；恢复端还须以 HISTORY_APPLIED 确认服务端 history 的 `toFrame`，全部确认前不得 state transfer、resume 或开启新 epoch。二进制 state frame 为 `RNS1 + session UUID raw16 + transfer UUID raw16 + epoch uint32-be + nextFrame uint64-be + length uint32-be + RASTATE`，总 payload 上限 1 MiB，header 与 JSON meta/digest 必须完全一致。`STATE_APPLIED.recaptureMatched=true` 表示原生装载完成后重抓的 `MEM ` core bytes 与 `STATE_META.coreSha256` 一致；Nestopia profile 先把由 `NST\x1a` 根块长度定位的 8-byte libretro input trailer 投影为零，其他 profile 不作投影。RASTATE 外层元数据可重建，因此 full-state byte exact 只记入诊断而不改变该字段。

服务端只以 socket/ping、认证与协议状态判断连接有效性，不以浏览器短时未贡献输入推断断线；后台节流期间 canonical lockstep 可以等待，页面恢复执行后沿原连接继续。ping 超时、单连接 write 失败、queue bytes/frames 超限或连续认证 UNAVAILABLE 都只能通过一次性 peer transport drop 关闭目标 socket、释放其待 flush 记账并让 Session 进入租约恢复，不能调用全局 fail；重复 close/error/remove 必须幂等。只有 protocol violation、明确 END_REQUEST、AUTH_REVOKED、租约超时或其他领域终因结束全局 Session。`MarkDisconnected` 或终局持久化失败只记录不含原始 error/cookie/IP 的低基数日志，不能因一次数据库写失败扩大为全局断线风暴。服务端协议/权限关闭仍可使用 RFC 6455 `1008`；业务终因只认权威 SESSION_ENDED/持久 Session 状态。

room cookie 名为 `retrom_netplay_{roomId去连字符}`。原始 32 bytes 固定为 `HMAC-SHA-256(netplayKey, "retrom-netplay-v2\\x00" || session UUID raw16 || profile UUID raw16 || credentialGeneration uint32-be)`，响应只写无 padding base64url；属性为 `HttpOnly; SameSite=Strict; Path=/runtime/netplay/rooms/{roomId}/; Max-Age=28800`，HTTPS public origin 时加 Secure，永不设 Domain。数据库只保存 raw credential 的 SHA-256，比较 constant-time；幂等重放重新派生并重发两类 cookie，不把明文或 Set-Cookie 存入 idempotency record。独立 key 位于 `RETROM_DATA_DIR/secrets/netplay-capability.key`，与 launch key 同样为 32 bytes、`0600`、no-follow、原子 hard-link 发布和目录 fsync，并进入离线 backup 的 `NETPLAY_KEY` 槽。

联机 Launch config 使用 `mode=netplay` 和 exact canonical profile；canonical `maxPredictionFrames` 必须来自锁定的 core profile 且不超过协议上限 8。当前 FCEUmm profile 为 8；FBNeo、SNES9x、Nestopia、MAME2003/Plus 与 FBA2012 CPS1/CPS2 七个 profile 均为 0（严格 lockstep），前端不得覆盖该值。所有手动 save-state 和 state 内容 route 在鉴权后统一返回 `409 NETPLAY_SAVE_UNSUPPORTED`。普通 Launch 为 `mode=single,netplay=null`，不得因开启联机改变原有 cookie 或 DTO 行为。

## 14. 游戏标签 API

`GET /api/v1/admin/tags`、`POST /api/v1/admin/tags`、`POST /api/v1/admin/tags/defaults` 与 `GET|PATCH|DELETE /api/v1/admin/tags/{tagId}` 只允许 ADMIN。列表接受 `q/status=ACTIVE|DELETED|ALL/sort=NAME_ASC|UPDATED_DESC/cursor/limit`，返回 `generatedAtMs/summary/items/nextCursor`；cursor 绑定 principal 与全部筛选。item 固定含 `tagId/name/status/version/usage/createdAtMs/updatedAtMs/deletedAtMs`，写响应返回 ETag。create 使用 Idempotency-Key；defaults 接受严格空对象，以同一事务补齐当前 common-tag 模板，响应把新建与已存在活动项分别放入 `createdItems/existingItems`，容量不足零部分写入；rename/delete 同时要求 Tag `If-Match`，delete body 为精确 `confirmName`，成功为 204 并回送新 tombstone ETag。

`PUT /api/v1/admin/games/{gameId}/tags` 以 Game `If-Match`、Idempotency-Key 和严格 `{"tagIds":[]}` 原子替换当前集合，响应为 `gameId/version/tags` 与新 Game ETag。普通 import/reconfigure body 必须含 `tagIds`，服务端兼容旧客户端省略时按 `[]`；Review PATCH、Pegasus 与 EmulationStation Collection mapping 的 `tagIds` 必须显式存在，`SKIP` Collection 只接受空数组。Import detail、两类服务器 Collection/item 和 Review/Game DTO 按 OpenAPI 投影标签或名称 snapshot。

`GET /api/v1/games`、`GET /api/v1/admin/games`、`GET /api/v1/admin/reviews` 接受单个 `tagId`；它与 `q` 及其他条件取交集并进入 cursor digest。合法但不存在或 DELETED 的 ID 返回空页；格式非法返回 `400 INVALID_REQUEST`。这些 route 的 `q` 在 SQL 分页前动态匹配活动标签名。Favorite、Home、Recent、Save 与 Netplay 的既有 game summary 同样投影活动 `tags`，但普通用户没有全 taxonomy 管理列表。

稳定错误映射为：名称规则 `422 TAG_NAME_INVALID`，活动同名 `409 TAG_NAME_CONFLICT`，实例上限 `409 TAG_LIMIT_REACHED`，不存在 `404 TAG_NOT_FOUND`，已删除 `409 TAG_ALREADY_DELETED`，引用不存在/已删除 `422 TAG_REFERENCE_INVALID`，owner 上限 `422 TAG_ASSIGNMENT_LIMIT_EXCEEDED`，删除确认不符 `422 TAG_DELETE_CONFIRMATION_MISMATCH`，过期 ETag `409 VERSION_CONFLICT`。所有管理写沿用 strict JSON、Origin/Fetch Metadata、CSRF、Idempotency-Key 和响应重放白名单；未知字段、重复字段或多值标量 query 在 handler 前拒绝。完整领域语义见 [`game-tags.md`](./game-tags.md)。

## 15. Payload 释放、墓碑与内容不可用

`GET /api/v1/admin/games/{gameId}` 的 `deleteImpact` 和 DELETE response 以 OpenAPI 为准。删除前 impact digest 绑定全部计数、去重 Blob 数、registered/exclusive/shared bytes 与排序后的 sourceKinds；任一新存档、媒体、内容引用或活动运行使旧 digest 返回 `409 GAME_DELETE_IMPACT_STALE`。标题不匹配为 `422 GAME_DELETE_CONFIRMATION_MISMATCH`，Game ETag 不匹配为 `409 VERSION_CONFLICT`，非管理员为 403，未知或对非管理员不可见统一为 404。

ImportJob/ImportItem、Pegasus/EmulationStation Item 和管理员 Game 详情均投影 `payloadState/payloadReleaseJobId`，FAILED 另给稳定 `payloadLastErrorCode`；重试使用已有 `POST /api/v1/admin/jobs/{jobId}/retry`，不会重新执行第二次删除。RELEASED 后仍返回来源类型、文件名、总大小、manifest/hash 摘要、状态、warning/error 和审核文字，但不返回 review asset、preview、runtime screenshot、来源媒体或内容下载 URL。前端显示“源文件已清理”。

Game 一旦 DELETED，公共 `GET /api/v1/games/{gameId}`、Launch 创建、Game media/content、Save 下载及已签发但尚未使用的 capability 都使用 404，不泄露墓碑存在性。管理员墓碑读取不复用公共内容端点。写入新 Metadata/ContentRevision、Asset、Save、Launch 或联机选择均由数据库/领域双重阻断。

查询分为两类：availability projection（游戏库、搜索、首页继续/最新添加、平台浏览、联机游戏选择）只返回启用目录的 PUBLISHED Game；relationship/history projection（最近游玩、收藏、Play/Launch/Netplay 历史、审核和审计）保留删除项，并固定携带 `{gameId,title,status:"DELETED",coverUrl:null,availability:"DELETED"}`。删除项没有详情/启动/编辑/存档入口；收藏只允许移出。Netplay 房间墓碑同时保留 `endReason=GAME_DELETED`，Room game 投影包含 `status/availability`。

## 16. RPG Maker 项目、运行验证与 unique-origin API

### 16.1 管理与审核 API

上传 transport 仍只用 `sourceType=FILES|DIRECTORY`；`upload_sessions.purpose` 新增 `RPG_MAKER_PROJECT|RUNTIME_ASSET_PACK`，Import create 的 `contentMode` 新增 `RPG_MAKER_PROJECT_V1`。项目 mode 只接受一个 DIRECTORY，或 FILES 中恰好一个 `.zip/.7z`；不得新增含混的传输枚举。Pegasus、EmulationStation 和通用 server import 向 `rpgmaker` 目标创建项目时固定返回 `422 RPG_SERVER_IMPORT_UNSUPPORTED`。

RPG 条目的 Review detail 额外返回可空 `rpgMaker`：固定包含 `selectedCoreId/generation/evidenceGeneration/evidenceConfidence/selfContained/selfContainedOverride/runtimeBindingRevision/runtimePackRequirements/runtimePackSelections/runtimeValidation/runtimeValidationCurrent`。`runtimePackRequirements` 按 slot 返回 `{slot,declaredName,normalizedDeclaredName}`，其中规范名是服务端 Unicode NFKC full case-fold 结果；管理端必须用它与 pack definition 的同名字段精确匹配，不得用 JavaScript locale lowercase 猜测。`runtimePackSelections` 是按 slot 排序的 `{slot,declaredName,installationId}`；`runtimeValidation` 为当前条目最新一次验证的完整只读投影或 null，`runtimeValidationCurrent` 精确表示其 binding revision 是否仍等于当前草稿。RPG 的 `canApprove` 在该验证仍 current 且已经分配原始 `launchId` 时为 true；管理员主动点击“运行游戏”并取得 Launch 即可确认发布，后续机器 gate、checkpoint 和跨 Launch 恢复是可选的高级验证。`runtimeScreenshot` 固定为 null；存在恢复证据时只从 `rpgMaker.runtimeValidation.checkpointRoundTrip.screenshotUrl` 读取，绝不触发普通截图 override。审核页不后台轮询验证投影；它只在创建 Launch、用户主动执行高级验证动作、重新载入页面，以及本地观察到本次游戏子窗体关闭后读取一次。validation Launch 在 `ORIGINAL_LAUNCH_ENDED` 通过前关闭，或 restore Launch 在 `RESTORE_INPUT` 通过前关闭时，服务端以 `RPG_RUNTIME_VALIDATION_WINDOW_CLOSED` 收口为 `FAILED`；已完成相应终态 gate 的正常关闭不改变验证状态。读取验证或创建下一次验证时，服务端还必须对账已经终结的 Launch 与仍未终结的 validation，修复旧进程或进程外过期留下的孤儿状态。终态 validation 不阻塞同一 binding 新建验证，因此管理员可多次运行游戏。

| 方法与路径 | 固定契约 |
| --- | --- |
| `GET /api/v1/admin/runtime-asset-packs` | 返回 definition、installation、引用计数与验证状态；definition 同时返回服务端生成的 `normalizedDeclaredName` 作为 exact matching key，不返回宿主路径。 |
| `POST /api/v1/admin/runtime-asset-packs/installations` | 携带 `Idempotency-Key`；严格 body `{uploadId,kind,generation?,declaredName?,sourceNote?}`；只消费 COMPLETE 且未使用的单目录/单归档 `RUNTIME_ASSET_PACK` upload，返回 202 Job 与 installation `ETag`；重复 upload/相同 definition + files digest 返回 409，超 10,000 文件或 512 MiB 返回 413；隔离 archive worker 因进程/资源边界暂不可用时返回 `503 RPG_RUNTIME_PACK_UNAVAILABLE`，不得误报成内容无效，客户端只可对该稳定码使用新幂等键做有界重试。 |
| `DELETE /api/v1/admin/runtime-asset-packs/installations/{installationId}` | 携带 installation `If-Match` 与 `Idempotency-Key`；只删除 READY/FAILED 且未引用 installation；有 Variant/Save 引用时 `409 RPG_RUNTIME_PACK_IN_USE`，版本漂移返回 412；成功 204 并进入既有 payload release 保留期。 |
| `PATCH /api/v1/admin/reviews/{importItemId}` | RPG 字段完整替换：`runtimePackSelections` 必须是最多三项的 `{slot,installationId}` 唯一数组，`rpgSelfContainedOverride` 必须显式布尔；同样要求 `If-Match`。运行输入变化递增 `runtimeBindingRevision`，metadata-only 编辑不使既有验证失效。 |
| `POST /api/v1/admin/reviews/{importItemId}/runtime-validations` | `If-Match` 与 `Idempotency-Key`；严格 body `{"clientCapabilities":{"secureContext":boolean,"crossOriginIsolated":boolean,"sharedArrayBuffer":boolean}}`；从当前 source/core/route/artifact/packs 冻结一个 `RPG_RUNTIME_VALIDATION` Launch，201 返回 validationId、launchId、playerUrl 和 expiry。服务端用摘要在签发 credential 前校验 artifact 的线程要求；客户端不得提交冻结字段。 |
| `GET /api/v1/admin/reviews/{importItemId}/runtime-validations/{validationId}` | 返回状态、机器 gates、route evidence、checkpoint round-trip 与审核决定；不返回凭据。 |
| `POST /api/v1/admin/reviews/{importItemId}/runtime-validations/{validationId}/restore-launch` | `If-Match`、`Idempotency-Key` 与同样严格的 `clientCapabilities` body；只在 CHECKPOINTED、临时 payload 完整、原 Launch 已结束且当前浏览器满足冻结 artifact 能力时创建不同 restore Launch；同事务写 `restoreLaunchId`，幂等重放返回同一 Launch。 |
| `POST /api/v1/admin/reviews/{importItemId}/runtime-validations/{validationId}/decision` | `If-Match` 与 `Idempotency-Key`；严格 `{decision:"PASS"|"FAIL",note:string}`；只有全部机器 gate 通过、restore Launch 不同且绑定未漂移才接受 PASS。 |
| `GET /runtime/launches/{launchId}/checkpoint-status` | 返回当前 payload kind 和最近受序 availability `{available,reason}`；即时 UI 仍以 adapter event 为准。 |
| `POST /runtime/launches/{launchId}/rpgmaker-gates/events` | 只接受 validation Launch + launch capability；严格 `{sequence,eventId,gate,phase,observedAtMs,evidence}`，sequence 从 1 递增、eventId UUID 幂等，同 gate 至多一个终态。 |

validation GET 的 `launchId` 字段始终存在；正常创建后是 UUID，只有在 Launch 行写入前即失败并原子收口为 `FAILED` 时为 `null`。该失败记录仍须完整返回 route/failure/machine-gate 证据并允许同 binding 新建验证，不能让审核详情因没有 Launch 而返回 500。其他非 `FAILED` 状态的空 launchId 是数据不变量错误。

gate 闭集和顺序为 `RUNTIME_READY/ENGINE_PROFILE/FRAMES_300/INPUT/AUDIO/INITIAL_POSITION_RECORDED/SAVE_POINT_RECORDED/CHECKPOINT_CREATED/POST_SAVE_STATE_DIVERGED/ORIGINAL_LAUNCH_ENDED/RESTORE_STARTED/RESTORE_POSITION_VERIFIED/RESTORE_SCREENSHOT/RESTORE_INPUT`，phase 只允许 `BEGIN|PASS|FAIL`，同一 gate 必须先 BEGIN 再唯一终态且下一个 gate 只能在前一个 PASS 后开始。原 Launch 只能上报到 ORIGINAL_LAUNCH_ENDED，restore Launch 只能上报后四项；服务端以当前 launchId 区分，不信任 body 声明。五个位置 gate 的 evidence 必须是 int32 范围的 `mapId/playerX/playerY/fixtureState`，其中 mapId 为正、坐标非负；服务端证明 B≠A、C≠B、restore=B 且 restore≠A/C、RESTORE_INPUT≠restore。截图只由服务端关联当前受授权上传，客户端不得提交 Blob ID；截图和 RESTORE_INPUT 均 PASS 后才进入 `AWAITING_DECISION`。任何 FAIL 立即收口 validation。

Launch config 顶层是 `runtimeFamily` discriminated union。`EMULATORJS` 保留既有字段；`RPGMAKER` 必须返回 `protocolVersion:1/purpose/coreId/coreName/generation/routeKey/artifactId/checkpoint/checkpointAvailability/runtimeValidation/adapter`。PRODUCT 的 `runtimeValidation` 为 null；验证 Launch 必须返回 `{validationId,state,originalLaunchId,restoreLaunchId,lastGateSequence,machineGates,checkpointEvidence,restoreScreenshotUploaded}`，其中 `machineGates` 恰为上述 14 项有序服务端状态/evidence，足以让同一受授权 Launch 的页面刷新从首个未完成动作继续。adapter 只允许：

- `EASYRPG_WEB`：`adapterId=easyrpg-web`、强制 `engineMode=rpg2k|rpg2k3`、runtime/project/index URL、可空唯一 RTP `FILE_TREE_V1 {kind,indexUrl}`、slot 100。RTP index 恰为 `{schemaVersion:1,files:[{path,url,sizeBytes}]}`；adapter 只下载该小型 index，并把逐文件 URL 交给 Player，项目缺少且游戏实际请求到匹配 lookup key 时才下载该 RTP 文件，禁止在 mount 前完整下载或解压 RTP bundle；
- `MKXP_LIBRETRO_WEB`：只允许 `adapterId=mkxp-libretro-web`；config 分别返回 core JS 与 Wasm 的 `url/sizeBytes/sha256`、`artifactSetSha256`、固定 bridge，以及项目/pack 的 `SEEKABLE_BLOB_V1 { kind,rangeRequired=true,url,sizeBytes,sha256 }` 内容源，强制 `rgssVersion=1|2|3`、`stateBufferBytes=268435456`。adapter 只完整读取并校验固定 core/bridge；项目与 RTP 不得在 mount 前转成 Blob 或执行完整 GET，而应把 URL 登记给核心的 Range 文件系统。core 的 size/hash 是下载固定 tag Release asset 后记录的 observed cache coordinates，用于内容响应、浏览器缓存完整性与本机损坏检测；它们不是远端 Release 准入身份，同 tag 同名资产由固定 tag commit 与 adapter ABI 定义兼容性；
- `NATIVE_WEB`：只允许 `adapterId=native-web`，并按 generation 精确冻结 `bridgeProfile=RPGMV|RPGMZ`、`uniqueOrigin/bootstrapUrl/bootstrapTicket`。ENGINE_PROFILE evidence 与 route projection 必须与 Launch 的 generation、adapter 和 profile 逐字段一致，不接受版本别名或 fallback。

`runtimeFamily=ONS` 与 `runtimeFamily=KIRIKIRI` 同样是严格 discriminated config。KiriKiri只允许 `KIRIKIRI2_WEB/kirikiri2-web`，返回 `runtimeBaseUrl/projectIndexUrl`、可空唯一 `startupXp3Path`、slot 1999 与 `gameCompatibilityLine/saveAbi/readableSaveAbis`；项目索引只列 Launch或 Review Preview冻结的 `PROJECT_FILE`。该 config只承诺 KAG书签恢复，不能把形状匹配解释为任意 TJS项目的 checkpoint能力。

`runtimeFamily=BUTTERSCOTCH` 只允许 `BUTTERSCOTCH_WEB/butterscotch-web` 与 `coreId=butterscotch`。严格 config 固定 `protocolVersion=1`、`mode=single`、`runtimeBaseUrl/projectIndexUrl`、不可变 content digest 和可空 `RUNTIME_STATE` checkpoint URL；产品 checkpoint 上限 16 MiB。Review Preview 使用同一 runtime/config 形状但额外携带专用 preview 元数据，不能被产品 Player 的严格 decoder 接受。

`runtimeFamily=TYRANOSCRIPT` 只允许 `TYRANOSCRIPT_WEB/tyranoscript-web` 与 `coreId=tyranoscript`。严格 config 固定 `protocolVersion=1`、`mode=single`、不可变 content digest、unique-origin bootstrap/entry/cleanup URL 和可空 `RUNTIME_STATE` checkpoint URL；产品 checkpoint 上限 16 MiB。项目自带引擎只从当次冻结的 `PROJECT_FILE` 投影读取，不能取得宿主 cookie 或退回应用 origin。

`runtimeFamily=WASM4` 只允许 `WASM4_WEB/wasm4-web` 与 `coreId=wasm4`。严格 config 固定 `protocolVersion=1`、`mode=single`、`cart={kind:WASM4_CART_V1,url,sizeBytes,sha256}`、`adapterAbi/saveAbi/readableSaveAbis=wasm4-state-v1`、精确 `runtimeBaseUrl/jsPath=wasm4-retrom.mjs`、compatibility line 和可空 `RUNTIME_STATE` checkpoint URL；cart 只允许 1–65,536 bytes，checkpoint 上限 132,144 bytes。服务端与浏览器都必须在执行前校验 cart size/SHA-256，checkpoint 必须绑定同一 cart digest，不能 fallback 到 EmulatorJS、通用 WebAssembly loader 或其他 runtime artifact。

EasyRPG 与 mkxp 的同源内容端点属于严格 OpenAPI 契约，不能只在 Go router 中注册：

ONS/KiriKiri/Butterscotch 的 `index.json` 逐项必须包含准确 `path/sizeBytes/url`。ONS 非视频文件以含稳定 content identity 的完整 URL 作为持久缓存身份，流式读取时必须与 `sizeBytes` 精确一致；adapter 优先把文件有界串行写入 origin-private file system，OPFS 不可用时才回退 Cache Storage，因此数百 MiB 归档不依赖 Chromium 普通 HTTP disk-cache 或 Cache Storage 的单项大小限制。ONS 视频直接把该 URL 交给浏览器媒体元素并消费同一内容端点的 Range，不得为了缓存而先完整下载到 Wasm 文件系统。KiriKiri 的大型 XP3 继续由 VLFS 按 256 KiB 有界 Range 注册，不能在 adapter 中预取整包。

Butterscotch core 需要本地文件路径，因此 adapter 必须先把冻结索引的每个项目文件流式写入按 content digest 分区的 OPFS，再启动 worker；下载过程按总字节上报进度。缓存文件长度与索引一致时跨 Launch 复用，长度漂移则删除并重新下载；恢复 Launch 仍重新读取 `index.json` 和 grant，但不重复获取已完整缓存的 `data.win`。

- `GET|HEAD /runtime/retrom-runtime/{runtimeVersion}/{runtimePath}` 只允许命中固定 retrom-runtime Release manifest 的逐文件 allowlist，本地逐字节复核 observed size/hash 后返回不可变公共响应；未知版本、路径或 MIME 返回 404；
- `GET|HEAD /runtime/content/project/{contentIdentity}/{projectPath}` 使用通用 `/runtime/content/` HttpOnly Launch grant。`contentIdentity` 由冻结项目的规范 logical path、format 与逐文件 digest，以及 EasyRPG/mkxp 派生 bundle 和所选 runtime pack 的锁定内容共同确定；相同内容和运行投影在不同 Launch 中得到相同 URL，替换任一文件或 pack 必须产生新 identity。服务端逐个验证当前有效 grant 实际锁定的身份，不能仅凭 path 查询可变 Game/Review。`index.json` 是 EasyRPG/ONS/KiriKiri/Butterscotch adapter 使用的保留虚拟索引；EasyRPG RTP 另投影为 `__retrom__/packs/{slot}/index.json` 和 `__retrom__/packs/{slot}/files/{path}`。所有响应为 `private, max-age=31536000, immutable`、强 ETag、准确 MIME/长度并支持单 Range；未知、未授权或跨身份文件不能回退到上传源、最新 content revision 或可变 ReviewDraft。

两条路径都必须以 `x-retrom-router-template` 保留含 `/` 的尾部路径。OpenAPI 中间件必须先识别它们再进入内容 handler；否则即使文件已物化也会被错误地提前映射为 404。

成功的 `GET /runtime/launches/{launchId}/state` 继续只返回二进制 payload；MIME、Content-Length、ETag 和最大尺寸由 Launch 的当前构件与存档 payload kind 决定，不在 body 混入 JSON。EmulatorJS 恢复不匹配 content revision/精确 artifact/adapter ABI/dependency snapshot 时在读取 payload 前返回 `RPG_CHECKPOINT_INCOMPATIBLE`；其他独立 runtime 要求逻辑游戏兼容线和 dependency snapshot不变、当前构件显式声明可读存档 save ABI，否则在创建 Launch时返回 `LAUNCH_SAVE_INCOMPATIBLE`，不会读取 payload或回退旧 runtime。

### 16.2 MV/MZ runtime origin

`/__retrom/*` 不属于 cookie-authenticated `/api/v1` client。请求 Host 必须精确匹配配置模板中以 launchId 为完整最左 label 的 unique origin；应用 cookie 必须 host-only。allowlist 恰为：

```text
GET  /__retrom/bootstrap
POST /__retrom/bootstrap
POST /__retrom/cleanup
GET  /__retrom/entry
GET  /__retrom/bridge.js
GET  /__retrom/project/{safeLogicalPath}
GET  /__retrom/restore-payload
```

`GET /__retrom/bootstrap` 是唯一允许无凭据到达的 GET：首次启动仍校验 Host/Launch/expiry，只返回固定 nonce bootstrap，不读取项目内容。父页面以 exact-origin `postMessage` 发送 LaunchConfig 中的 60 秒一次性 ticket；bootstrap 同源 POST 后，服务端在一个事务消费 ticket 并设置 runtime host-only cookie，生产属性固定为 `HttpOnly; Secure; SameSite=Strict; Path=/__retrom/`，永不设置 Domain。同一 Launch 的父页面刷新时，config 只有在未消费 ticket 或同 Launch/exact origin 的未撤销、未过期 isolated capability 存在时才返回；runtime host 的 bootstrap GET 必须实际验证对应 HttpOnly cookie，成功后以 `303 Location: /__retrom/entry` 恢复，不能重新消费或更新 ticket/capability。无 cookie、伪造 cookie、错误 Host/origin、撤销或过期一律 `410 RPG_RUNTIME_BOOTSTRAP_EXPIRED`。其余 route 都要求该 capability；cleanup 撤销 credential、过期 cookie 并清空存储。ticket/capability 不得进入 URL、日志、错误或数据库明文。

entry CSP 固定为：`default-src 'self' data: blob:`；`script-src 'self' 'nonce-<per-response>' 'unsafe-eval' blob:`；`style-src 'self' 'unsafe-inline'`；`img-src/media-src/font-src 'self' data: blob:`；`connect-src 'self'`；`worker-src 'self' blob:`；`frame-src/object-src 'none'`；`base-uri 'self'`；`form-action 'none'`；`frame-ancestors <exact app origin>`。worker 只允许项目同源脚本或项目脚本生成的 blob worker，以支持 MV/MZ 官方音频解码器；外网 worker 与外网 connect 仍被禁止，`Sec-Fetch-Dest: serviceworker` 的 project 请求固定 404，不能借此注册持久 service worker。不得直接允许项目 inline script；合法项目的 inline script 必须按文档顺序提取为同源、不可变派生 Blob，并把转换摘要计入 profile，不能放宽 CSP。bootstrap 使用 `default-src 'none'; script-src 'nonce-<per-response>'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors <exact app origin>`。bootstrap/entry 是应用 COEP 页面必须跨源嵌入的两个文档，固定 `Cross-Origin-Resource-Policy: cross-origin`，但仍由上述 exact `frame-ancestors` 限制嵌入者；bridge、project、restore、cleanup 与其他 runtime 响应继续固定 `Cross-Origin-Resource-Policy: same-origin`。两类文档响应同时固定 `Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=(), usb=(), serial=(), bluetooth=(), midi=(), clipboard-read=(), clipboard-write=()`、`X-Content-Type-Options: nosniff`、`Referrer-Policy: no-referrer`。iframe sandbox 恰为 `allow-scripts allow-same-origin allow-pointer-lock`。project/restore 响应 `private,no-store`、强 ETag、准确长度、固定 MIME、nosniff，并只支持单 Range；错误/多 Range/redirect fail closed。唯一 origin 的普通 `/api`、页面、认证和媒体管理 route 一律 404。

`GET /__retrom/project/{safeLogicalPath}` 只查询从完整 source snapshot 冻结到本 Launch 的 Native Web 运行投影，并先按逻辑路径逐 byte 精确查找；该投影只包含 `index.html` 与固定 Web 资源 MIME allowlist，根 `package.json` 及 `.exe/.dll/.so/.dylib/.node/.bat/.cmd/.ps1` 等 desktop/native payload 即使保留在 source snapshot/filesDigest 中也绝不进入投影。仅当精确查找不存在时，允许在同一 Launch 的投影内做一次 SQLite ASCII `NOCASE` 查找，以兼容 Windows 上生成、却在脚本中使用不同 ASCII 大小写的 MV/MZ 资源引用。候选必须恰好一个，否则返回 404；导入期的 NFC/NFKC case-fold 碰撞门禁仍是前置不变量。该回退不做 Unicode 归一化或模糊路径猜测，不适用于 `entry`、restore、普通 Launch content、external file 或任何 `/api/v1` 内容端点。

`GET /__retrom/bridge.js` 从 Launch 当前 `coreArtifactId` 读取 `runtimeVersion/entryPath`，再命中 RPG runtime manifest allowlist 并执行本机 observed 完整性校验。manifest 只保留当前 `retrom-runtime` tag 的一份 `native/bridge.js`；不得硬编码版本、绕过 artifact binding、保留旧 bundle 或在文件缺失时 fallback 到其他内容。升级是否可恢复旧存档只由当前构件声明的 `readableSaveAbis` 决定。

bootstrap POST 成功必须设置 `Clear-Site-Data: "storage"` 后用 `location.replace('/__retrom/entry')`，cleanup 再清空 storage、撤销 capability 并过期 cookie。只有 entry 可返回 `text/html`；项目内其他 HTML 不服务。`.js/.mjs/.css/.json/.wasm` 和登记媒体/font 使用固定 MIME，profile 登记的加密/未知非执行资源才可用 `application/octet-stream`，用户 MIME/archive metadata 不参与决定。入口 HTML 直接引用 native executable 后缀属于确凿运行依赖并以 `RPG_NATIVE_DEPENDENCY_UNSUPPORTED` 拒绝；仅携带但未引用的同名文件仍保留在 source snapshot，却不会进入 Launch 投影或由该端点返回。

RPG 错误沿用全局 error envelope，`code` 与 HTTP 状态固定分组如下；handler、后台 Job 与 Launch 预检必须使用同一码，客户端不得解析 message：

- `400`：`RPG_PROJECT_NOT_FOUND`；
- `408`：`RPG_RUNTIME_TIMEOUT`；
- `410`：`RPG_RUNTIME_BOOTSTRAP_EXPIRED`；
- `413`：`RPG_RGSS_CONTENT_TOO_LARGE`（multipart 超过 route 总上限继续使用通用 body-too-large code）；
- `404`：`RPG_RUNTIME_VALIDATION_NOT_FOUND`；
- `503`：`RPG_RUNTIME_PACK_UNAVAILABLE`（仅表示隔离 archive worker 的瞬时进程/资源不足，客户端只可对该稳定码使用新幂等键做有界重试）；
- `409`：`RPG_PROJECT_ROOT_AMBIGUOUS/RPG_GENERATION_AMBIGUOUS/RPG_RUNTIME_PACK_MISSING/RPG_RUNTIME_PACK_AMBIGUOUS/RPG_RUNTIME_PACK_IN_USE/RPG_RUNTIME_ROUTE_UNAVAILABLE/RPG_RUNTIME_THREADS_UNAVAILABLE/RPG_RUNTIME_OPFS_UNAVAILABLE/RPG_RUNTIME_VALIDATION_REQUIRED/RPG_RUNTIME_VALIDATION_VERSION_CONFLICT/RPG_RUNTIME_INVALID_STATE/RPG_RUNTIME_PROTOCOL_VIOLATION/RPG_RUNTIME_CONTENT_MISMATCH/RPG_CHECKPOINT_UNAVAILABLE/RPG_CHECKPOINT_INCOMPATIBLE`；
- `422`：`RPG_CORE_UNSUPPORTED/RPG_GENERATION_UNSUPPORTED/RPG_SELECTED_CORE_MISMATCH/RPG_SERVER_IMPORT_UNSUPPORTED/RPG_LCF_INVALID/RPG_LCF_GENERATION_UNKNOWN/RPG_LMT_INVALID/RPG_INI_INVALID/RPG_INI_ENCODING_UNSUPPORTED/RPG_RGSS_GENERATION_CONFLICT/RPG_WEB_FORMAT_INVALID/RPG_PATH_COLLISION/RPG_NATIVE_DEPENDENCY_UNSUPPORTED/RPG_NATIVE_BRIDGE_UNSUPPORTED/RPG_RUNTIME_VALIDATION_DECISION_INVALID/RPG_RUNTIME_SCREENSHOT_INVALID/RPG_CHECKPOINT_INVALID/RPG_CHECKPOINT_RESTORE_FAILED`。

`RPG_CORE_UNSUPPORTED` 表示请求的用户 core 不是 `rpgmaker`，或内部 route core 不在七世代闭集；`RPG_WEB_FORMAT_INVALID` 表示 MV/MZ 的 `System.json`、入口 HTML 或固定 core JavaScript shape 无法安全解析。两者是 detector 对外稳定映射，不得折叠成通用 500 或按错误字符串分支。`RPG_GENERATION_AMBIGUOUS` 表示同一项目存在多个完整世代证据或无法唯一裁决，必须拒绝而不是要求用户猜测内部 core。

## 17. 统一验收入口

通用协议由 `ACC-API-001` 覆盖；认证/账户隔离由 `ACC-AUTH-*` 与 `ACC-ISO-*` 覆盖；同源 CSRF、launch cookie、受限缓存和媒体 SSRF 由 `ACC-SEC-002`–`ACC-SEC-004` 覆盖；上传协议由 `ACC-IMP-001`、`ACC-IMP-002` 和 `ACC-IMP-008` 覆盖；Pegasus/VIDEO 由 `ACC-PEG-001`–`006` 与 `ACC-MEDIA-001` 覆盖；EmulationStation 协议、扫描/映射、审核扩展与释放由 `ACC-ES-001`–`006` 覆盖；标签由 `ACC-TAG-002`–`005` 覆盖；多盘协议由 `ACC-MDISC-001`–`004`、`007`–`008` 覆盖；一次点击启动由 `ACC-RUN-*` 覆盖；联机协议、transport/auth/SSE 错误边界和双浏览器生命周期由 `ACC-NP-010`–`016` 覆盖；RPG 项目、pack、runtime validation、unique-origin bootstrap、gate 与 checkpoint API 由 `ACC-RPG-001`–`012` 覆盖。
