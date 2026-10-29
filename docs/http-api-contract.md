# HTTP 契约

/api/v1为唯一业务路径，字段、状态枚举、请求体和分页细节以api/openapi.yaml及其domains/components为准。生成Go代码是构建输入，TypeScript类型由同份bundle生成。服务对已授权请求执行OpenAPI验证，不能以自定义解码绕过schema；编码斜杠作为BIOS requirementKey的一个路径段解析一次。UUID必须规范，JSON拒绝重复字段、无效UTF8、未知字段和尾随内容。

错误统一{code,message}。分页items,total,offset,limit有界（limit≤100、offset≤100000），总数不由当前页面推算。存档q匹配游戏或存档名，kind筛类型，sort选择recent/title；gameCount为同一过滤条件COUNT DISTINCT game_id。GET /api/v1/saves 的 groupByGame=true 在筛选后、分页前按游戏取最近更新的一份存档，同时间按 Save ID 升序稳定选择；此时 total/offset/limit 以游戏为单位，gameCount 与 total 相同。普通存档查询仍以每份存档分页，单游戏的存档由 gameId 查询完整分页结果。收藏folderId与unclassified互斥。

创建或改名标签与现有活动标签的规范化名称重复时返回409 TAG_NAME_CONFLICT；编辑版本不符仍返回409 VERSION_CONFLICT，二者不得混用。标签软删除后允许重新使用同名，新标签不恢复原有游戏关联。

库、收藏与管理列表的platformId和platformInstanceId按AND过滤；sort=recent使用当前用户最后游玩时间降序，未游玩排后，名称/ID提供稳定次序。最近页只含当前用户已游玩游戏，sort=recent按最后时间、title按名称；q、active标签和afterMs/beforeMs闭区间与总数使用相同过滤条件。

GET `/api/v1/games` 另支持 `sort=random`：在相同可见性与筛选条件内每次独立随机排序，先选取分页候选再组装资料；单次响应中游戏不重复，不同请求可以重合，不提供跨请求的稳定分页。首页发现游戏使用 offset=0，手机 limit=6、桌面 limit=12。普通游戏库的默认标题排序保持不变。

按名称排序的列表（游戏库、目录、收藏、最近与存档的 title 排序）统一按标题字母序：先字母分组 A–Z、最后 #，同一字母内中文按拼音、拉丁按字母，再以名称和 ID 稳定。Game 返回 titleLetter（所属分组）与 sortName（管理员排序名，空串表示按标题）；管理员保存 Game 时可提交 sortName（≤300 字符），非空时代替标题参与分组和排序。GET `/api/v1/games/letters` 与 GET `/api/v1/favorites/letters` 接受与对应列表相同的筛选参数（不含分页和 sort），返回 TitleLetters `{items: [{letter, offset, count}]}`：只含实际存在的分组，按列表顺序排列，offset 是该分组第一项在同一筛选列表中的位置，可直接用作列表 offset。

业务读写使用当前登录会话。管理员只扩展共享管理权限；存档、收藏、最近仍限定本人。写入要求一个精确匹配的Origin，已登录写入还要求当前CSRF。代理链默认只在RemoteAddr属于内网或回环地址时解析，公网直接伪造X-Forwarded-For不会改变限流主体。内网代理必须覆盖转发头，客户端IP解析及边界见[账号与部署](backend-api-and-operations.md)。无凭据的隔离origin仅提供静态桥壳，不提供业务API。

GET资源的HEAD按同份GET读契约验证，保留参数与授权门禁且不返回body。当前Go1.26.5中，匹配If-Match的HEAD带有效单段Range返回206、选中段Content-Length与Content-Range；未带Range返回200和完整长度。GET与HEAD的If-Match不匹配均先返回412空body，保留实际强ETag，不能将它声明为业务错误JSON。存档payload同一次持久查询投影immutable路径和hash，响应强ETag；准备恢复到实际下载之间若存档并发覆盖，旧If-Match明确412，不把新bytes标为旧身份。既有Run的ROM/resources仍按启动冻结。

资源路径绑定本人run上下文及服务端允许资源ID；支持强ETag、If-Match与流式Range。强ETag为双引号sha256-加实际摘要，不能放宽runtime身份校验。受管文件响应带有 `Cache-Control: private, no-store, no-transform`；Provider 资产按下述正式／开发策略分别处理，均保留 `no-transform`，防止中间代理改变已固定的字节表示。Provider静态路径限定已验证安装manifest/integrity资产。文件和索引逻辑路径保留扩展名并规范编码Unicode/#，不暴露宿主路径或任意URL。

GameDetail.files 表示受管物理文件，项目的单条记录 role 为 project_package，大小和 SHA 描述整个 STORE ZIP。管理员通过 GET `/api/v1/admin/games/{gameId}/content?after=…&prefix=…&limit=100` 读取逻辑目录；待审或已发布游戏均需管理员授权。返回 `{items:[{logicalKey,sizeBytes,sha256,mediaType}],nextCursor}`，nextCursor 为下一页使用的路径或 null；按大小写敏感路径顺序分页，limit 为 1..100，prefix 是字面路径前缀。响应不暴露 storage key、成员偏移或成员 ID，详情不嵌入整个成员索引。

运行项目索引仍为 schemaVersion 1 的 path/url/sizeBytes/sha256/mediaType。每个逻辑资源在 Run 中分配不透明 UUID，并冻结所属包、key、偏移、长度和摘要；Range、Content-Range 总长及 ETag 都对应该成员。替换当前包不改变已创建 Run 的定位，也不改变浏览器读取协议。

正式 Provider 资源 `/runtime/providers/{providerId}/{bundleSha256}/{asset}` 始终返回该已安装 Bundle 的原始内容。不超过 8 MiB 且扩展名为 `.js`、`.mjs`、`.json`、`.css`、`.html`、`.svg`、`.png`、`.jpg`、`.woff`、`.woff2`、`.ttf`、`.otf` 或 `.ico` 的资源采用 `private, max-age=31536000, immutable, no-transform`；Wasm、核心归档及其他资源采用 `private, no-store, no-transform`，可由 Runtime 的摘要缓存复用。授权及资产允许列表仍在服务端校验。

正式及开发 Provider 资源由后端统一返回 `Cross-Origin-Embedder-Policy: require-corp` 与 `Cross-Origin-Resource-Policy: same-origin`，使 Worker 入口保持宿主的跨源隔离策略；GET、HEAD、Range 和 304 响应均保留这些头。部署网关无需额外补齐该策略。

PFB 开发资源采用 `/runtime/providers/{providerId}/{bundleSha256}/dev/{moduleSha256}/{asset}`，精确匹配当前开发模块摘要，返回 `private, no-store, no-transform`。覆盖层未修改的资源读取同一已安装 Bundle；旧开发摘要返回 404。Envelope 的 runtime base、入口模块和 bridge 必须属于同一正式或开发路径，正式 URL 不读取开发覆盖层。

存档checkpoint payload保持不透明。可选截图接受实际PNG/JPEG字节，检查大小及图像尺寸后原样保存；不以上传文件名推断格式，不把JPEG伪报为PNG。截图读取按实际字节返回MediaType；设为游戏封面沿用公共媒体上传边界。multipart metadata仍按权威SaveCommitMetadata校验，包括必需但可为null的slot。

管理员邀请URL为/register#token=...，重置为/reset-password#token=...；token只由匿名页面读取并提交inspect/consume。管理员链接列表只返回脱敏摘要，筛选active/consumed/revoked/expired与total采用同条件；撤销需版本，重置链接显式expiresInHours。创建目录/Tag/Folder请求没有version，编辑请求要求version≥1。

独立隔离桥唯一传输路径为/__retrom/runtime-isolation/{runId}/{asset}。壳注册/run/{runId}/范围的SW，宿主通过精确绑定的MessagePort提供受限内容读取；业务cookie不会进入隔离origin。服务返回COEP/CORP、限定frame-ancestors与Service-Worker-Allowed。共享PFB网关允许的/__retrom/只是传输前缀，不恢复旧运行凭据契约。

管理员来源目录统一使用服务进程可见的绝对path。GET /api/v1/admin/source-directories从path指定目录列出{name,path}，默认界面从/开始；游戏集合检查、游戏扫描、BIOS扫描与内容替换均使用path，不再使用source-roots、rootId或relativePath。仅管理来源浏览返回绝对路径；Run资源仍使用受限资源ID，复制接收后的受管文件所有权不变。

浏览器上传是游戏来源的另一种形式，只有创建它的管理员可读写。POST /api/v1/admin/uploads 声明format（pegasus、emulationstation或files）及全部{path,sizeBytes}（安全相对路径、≤50,000个文件、单文件≤4GiB、合计≤20GiB，声明请求体上限16MiB），同时活跃会话≤2（409 UPLOAD_LIMIT），可用空间须≥2×合计+10GiB（507 INSUFFICIENT_STORAGE），返回Upload{id,format,chunkSizeBytes,expiresAtMs,completed,files[].receivedBytes}；GET /api/v1/admin/uploads 按最近写入倒序列出当前管理员尚未开始扫描的上传，供刷新或换浏览器后续传、映射。PUT /api/v1/admin/uploads/{uploadId}/files/{index}?offset= 以application/octet-stream追加≤chunkSizeBytes的块，offset必须等于该文件已接收字节，否则409 UPLOAD_OFFSET_CONFLICT，客户端以GET /api/v1/admin/uploads/{uploadId}读取进度后续传；会话在最后一次写入后24小时过期。DELETE同一地址取消并删除暂存。SourceInspectRequest与GameScanRequest的path和uploadId二选一，format为pegasus、emulationstation或files；未完成的上传不能检查或扫描，以某种format检查上传会记住该format，扫描开始后会话即被消费，扫描任何终态后删除暂存。

Game.lastPlayedAtMs为必需但可为null的当前用户投影，由既有recent_game_tab在当前分页游戏范围内读取；没有新增Game持久字段、次数或时长。未成功游玩的当前用户值为null，管理身份不读取其他用户的最近记录。

管理员读取 GET /api/v1/admin/scans 可使用既有 limit/offset 分页，响应 ScanList.items；待审核页的scanId仅为UI定位某项进度，不传给Game列表筛选，也不持久化为游戏批次关系。

街机父包维护与待审、已发布Game共用管理员边界。GET /api/v1/admin/games/{gameId}/runtime-options/arcade?coreId=… 返回runtime按已保存配置、DAT与实际成员投影的{coreId,parentFiles,missingParents}；缺失名称为runtime给出的完整ZIP名，Go/Web不猜测。POST /api/v1/admin/games/{gameId}/parents 接受multipart的version、coreId、file并返回GameDetail，按Game版本CAS补充或替换同logicalKey父包，不能覆盖游戏入口。成功由runtime将所选核心中同DAT父名引用换为上传文件的logicalKey，保留其他核心/选项/其他文件及出版状态，Game.version递增一次并重算全部active文件的contentHash。上传验证普通ZIP和引用结构，不以完整成员或Prepare作为保存门禁；不齐时投影仍报告缺失，实际启动返回400 RUNTIME_PARENT_MISSING并说明名称。

父包投影或启动中的已知runtime内容失败保留具体code和安全message，例如400 RUNTIME_ARCADE_MACHINE_UNKNOWN，不降为INVALID_INPUT。审核readiness的逐条error采用相同错误映射，运行条件无法检查时返回REVIEW_READINESS_UNAVAILABLE；响应结构与持久化模型保持不变。

街机配套投影同时返回mediaFiles、missingMedia；MAME Current和Flycast可通过同一parents上传接口补充CHD。MAME按CHD头部SHA-1匹配当前DAT要求，错误文件不提交；上传保留文件名作为logicalKey，运行时映射到DAT磁盘路径。CHD不写入Parent配置，审核与运行仍分别检查其必需性。文件在上传时成为同一Game的受管内容，批准仅改变发布状态，不移动或重新打包ROM、Parent与CHD。

POST `/api/v1/admin/games/{gameId}/withdraw` 接收VersionRequest，仅管理员可用并经过同源Origin／CSRF校验；以版本CAS把published退回pending_review并返回GameDetail，重复／过时版本409，保留原Game与文件及私有关系。ReviewReadiness新增必填missingRomMembers数组（coreId、romSet、ROM／PARENT／BIOS角色、member、sizeBytes、MISSING／SIZE_MISMATCH原因），失败时为空，CRC仍独立提示。ApproveReviewRequest保留必填version并可选coreId；省略按目录默认核心检查。批准事务外检查当前原始归档与配套DAT，再锁定同版本提交；必需成员缺失或大小不符返回REVIEW_REQUIRED_FILES_MISSING，CRC差异不阻止人工批准，试玩非必需。没有新增数据库表／列或正常启动门禁。

Save.restoreWarning 为必需的当前态字符串：空值或 core_changed。核心版本差异对应 restorable=true、restoreReason=""、restoreWarning="core_changed"，表示可尝试但不保证恢复；ROM不一致等阻断结果的restoreWarning为空，core_changed不再是restoreReason。Run.save返回同一权威投影，Run.extinfo为实际运行身份。带核心变化警告的Run不能覆盖其来源存档ID（409），创建新存档与随后覆盖新档仍按原CAS/commitId契约。

GET /api/v1/admin/games/{gameId}/runtime-options/rpgmaker 仅允许管理员读取待审或已发布游戏的入口候选，返回 {entries: string[]}，可传未保存的 engine 选择。Host 仅传活动文件事实和配置，不复制入口规则；旧入口无效不阻止读取替代候选。runtime 对保存、审核和启动统一拒绝不匹配的 RPG 入口，返回 RUNTIME_RPG_ENTRY_INVALID。

内容替换为管理员暂态操作：POST `/api/v1/admin/games/{gameId}/content-replacement` 接收绝对path与当前version，校验published游戏、版本和启用目录后返回202 ContentReplacement；不复制内容或等待runtime。独立接收量为1执行＋2 FIFO等待，队满503，同一游戏正在替换或版本过时409。GET同一路径读取当前操作，GET `/{operationId}` 精确读取当前ID，POST `/{operationId}/cancel` 请求取消；写请求保持Origin/CSRF门禁。响应包含id、gameId、status、phase、canCancel、可空result（version、contentHash）、可空error（安全code、message）与createdAtMs/updatedAtMs/expiresAtMs，不返回来源路径或受管文件key。status为queued/running/canceling/completed/failed/cancelled/interrupted，phase为queued/preparing/normalizing/configuring/packaging/publishing/completed。发布前版本重查及最终CAS都保留；publishing时canCancel=false，重复取消和终态取消均返回当前进度。completed才表示提交确认，取消须等待准备清理确认，转换失败保留CONTENT_NORMALIZATION_FAILED，runtime容量不足保留RUNTIME_BUSY。仅Redis保存每个游戏最新操作，最后一次状态写入后24小时过期，读取不延期；旧ID被新操作覆盖或进度失效返回404，重启遗留进度明确中断或按分配文件ID核实已提交，无法核实时保持可重查结果，不自动重新执行。
