# HTTP 契约

/api/v1为唯一业务路径，字段、状态枚举、请求体和分页细节以api/openapi.yaml及其domains/components为准。生成Go代码是构建输入，TypeScript类型由同份bundle生成。服务对已授权请求执行OpenAPI验证，不能以自定义解码绕过schema；编码斜杠作为BIOS requirementKey的一个路径段解析一次。UUID必须规范，JSON拒绝重复字段、无效UTF8、未知字段和尾随内容。

错误统一{code,message}。分页items,total,offset,limit有界（limit≤100、offset≤100000），总数不由当前页面推算。存档q匹配游戏或存档名，kind筛类型，sort选择recent/title；gameCount为同一过滤条件COUNT DISTINCT game_id。收藏folderId与unclassified互斥。

创建或改名标签与现有活动标签的规范化名称重复时返回409 TAG_NAME_CONFLICT；编辑版本不符仍返回409 VERSION_CONFLICT，二者不得混用。标签软删除后允许重新使用同名，新标签不恢复原有游戏关联。

库、收藏与管理列表的platformId和platformInstanceId按AND过滤；sort=recent使用当前用户最后游玩时间降序，未游玩排后，名称/ID提供稳定次序。最近页只含当前用户已游玩游戏，sort=recent按最后时间、title按名称；q、active标签和afterMs/beforeMs闭区间与总数使用相同过滤条件。

业务读写使用当前登录会话。管理员只扩展共享管理权限；存档、收藏、最近仍限定本人。写入要求一个精确匹配的Origin，已登录写入还要求当前CSRF。代理链只在RemoteAddr属于配置的可信CIDR时解析，直接伪造X-Forwarded-For不会改变限流主体。无凭据的隔离origin仅提供静态桥壳，不提供业务API。

GET资源的HEAD按同份GET读契约验证，保留参数与授权门禁且不返回body。当前Go1.26.5中，匹配If-Match的HEAD带有效单段Range返回206、选中段Content-Length与Content-Range；未带Range返回200和完整长度。GET与HEAD的If-Match不匹配均先返回412空body，保留实际强ETag，不能将它声明为业务错误JSON。存档payload同一次持久查询投影immutable路径和hash，响应强ETag；准备恢复到实际下载之间若存档并发覆盖，旧If-Match明确412，不把新bytes标为旧身份。既有Run的ROM/resources仍按启动冻结。

资源路径绑定本人run上下文及服务端允许资源ID；支持强ETag、If-Match与流式Range。强ETag为双引号sha256-加实际摘要，不能放宽runtime身份校验。Provider静态路径限定已验证安装manifest/integrity资产。文件和索引逻辑路径保留扩展名并规范编码Unicode/#，不暴露宿主路径或任意URL。

存档checkpoint payload保持不透明。可选截图接受实际PNG/JPEG字节，检查大小及图像尺寸后原样保存；不以上传文件名推断格式，不把JPEG伪报为PNG。截图读取按实际字节返回MediaType；设为游戏封面沿用公共媒体上传边界。multipart metadata仍按权威SaveCommitMetadata校验，包括必需但可为null的slot。

管理员邀请URL为/register#token=...，重置为/reset-password#token=...；token只由匿名页面读取并提交inspect/consume。管理员链接列表只返回脱敏摘要，筛选active/consumed/revoked/expired与total采用同条件；撤销需版本，重置链接显式expiresInHours。创建目录/Tag/Folder请求没有version，编辑请求要求version≥1。

独立隔离桥唯一传输路径为/__retrom/runtime-isolation/{runId}/{asset}。壳注册/run/{runId}/范围的SW，宿主通过精确绑定的MessagePort提供受限内容读取；业务cookie不会进入隔离origin。服务返回COEP/CORP、限定frame-ancestors与Service-Worker-Allowed。共享PFB网关允许的/__retrom/只是传输前缀，不恢复旧运行凭据契约。

管理员来源目录统一使用服务进程可见的绝对path。GET /api/v1/admin/source-directories从path指定目录列出{name,path}，默认界面从/开始；游戏集合检查、游戏扫描、BIOS扫描与内容替换均使用path，不再使用source-roots、rootId或relativePath。仅管理来源浏览返回绝对路径；Run资源仍使用受限资源ID，复制接收后的受管文件所有权不变。

Game.lastPlayedAtMs为必需但可为null的当前用户投影，由既有recent_game_tab在当前分页游戏范围内读取；没有新增Game持久字段、次数或时长。未成功游玩的当前用户值为null，管理身份不读取其他用户的最近记录。
