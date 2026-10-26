# 运行、存档与最近游玩

运行声明与规则归retrom-runtime，Go使用不可变完整runtime工具包。工具根含scripts/runtime-cli.mjs、dist/runtime/index.js及事实JSON/必要production依赖，不要求.git或相邻源码。Host用4个常驻Node worker按JSONL --serve调用同一公共execute模块，每worker一次请求；64MiB请求/响应有界，deadline包含排队，取消杀死并重建worker。没有持久队列、额外网络服务或授权状态。

目录保存管理员核心集与默认核心；runtime catalog提供平台友好名和唯一公共core ID，不自动建目录。Run以当前User登录态创建，默认核心不能静默替换不可用核心；saveId恢复由服务端先读本人save并选择原core，不能依赖浏览器重新指定默认值。

准备得到coreId/providerId/targetId/coreFingerprint/romHash、Target options、checkpoint能力和通用资源计划。ROMHash覆盖整份活动Game运行文件集合，各核心一致；单普通文件是原始SHA，多文件/项目由runtime规范TREE算法计算。Target指纹只覆盖Provider/Target身份及显式声明的核心文件内容摘要，不包含runtime版本、adapter/公共codec源码、输入/UI/加载辅助代码或部署路径。已正式生效的存档编码或恢复语义发生不兼容变化时，由runtime显式修改checkpointFormat；尚在开发中的功能遵循[开发期契约规则](engineering-quality-and-testing.md#开发期契约)，直接修改当前定义，不因PFB中间数据迭代格式版本。Bundle/Module摘要继续用于产物完整性校验。游戏自带引擎由完整ROMHash覆盖。Go验证安装bytes/proof/integrity并装配部署URL。

Redis只保存两小时临时run：窄header和被允许的file/index hash字段，读取资源不反序列化整份项目。PG登录会话、Game和save不因Redis丢失消失。上下文丢失返回CONTEXT_EXPIRED；浏览器以冻结身份重建同样run后可重新提交草稿，身份变化则明确拒绝自动续传。

按需读取（Range）默认使用 2 MiB 网络窗口，内部缓存块仍为 256 KiB；5 MiB 及以下的小文件整取。实际读取越过当前窗口一半时，可在预算允许时预取相邻窗口，每个 Session 最多一个纯预取。单次读取以连续 15 秒没有有效数据进度判超时，HTTP、调度、Reader、异步和同步桥按该请求的实际数据进度续期；总下载超过 15 秒仍可完成。隔离网页的 Service Worker 以 `READ_STARTED` 区分排队和活动读取，以递增的 `READ_PROGRESS` 计数续期；重复通知、元数据和其他活动读取不延长某个活动请求。

按需读取的游戏内容遇到请求级超时或网络失败时，runtime 不结束运行：读取保持挂起并按 1/2/4/8 秒退避重试，浏览器恢复联网时立即重试，Player 通过公共事件 `CONTENT_NETWORK`（WAITING/RESTORED）显示重连提示；内容变化、授权失败、校验失败等仍立即失败。游戏正在等待的内容首次请求失败或 5 秒无响应时即报告 WAITING，不等单次读取的 15 秒空闲期限；仅后台预取失败不提示。mkxp、Butterscotch、PPSSPP、KiriKiri 等核心桥不设自身读取期限，统一由 runtime 等待网络。

运行期间ROM/Core变更不会拒绝仍存续的原run保存；存档extinfo只能复制原run冻结coreId/providerId/targetId/coreFingerprint/romHash/checkpointFormat/content/runtimeOptions。普通启动使用当前配置。恢复先读取当前User的Save，再将原extinfo交给runtime：校验当前允许核心、完整ROM身份和可读格式后，按原content与runtimeOptions准备资源，不用后来编辑的运行选项替换存档上下文。Go在准备与restore descriptor中复用同一次Save投影，避免并发覆盖时混合原选项与新payload；实际下载遇到不匹配的强ETag仍可明确失败，不增加持久快照关系。核心指纹变化只返回core_changed警告，不拦截恢复；ROM变化和不可读格式优先于警告。旧存档显示真实不可恢复原因。saveId、kind、slot、name和version仍是存档领域字段，不从内容hash反推。

保存multipart metadata+payload+可选PNG/JPEG截图。commitId为浏览器持久草稿ID：创建save ID取commitId；覆盖保存last_commit_id。相同commit/payload hash/extinfo重试返回同一版本，其他提交按CAS竞争。IO完成后短事务锁Game确认published，再保存CAS；游戏删除后不能提交新存档。列表按distinct game/core一次batch-identity和一次batch-restorable，不逐存档调用Prepare/Node。筛选分页与count采用同条件。

退出立即撤销PG会话、清cookie和释放运行资源，不整站Clear-Site-Data storage。未同步IndexedDB/OPFS草稿按User隔离，只有服务端明确成功或用户丢弃才删除。网络失败、Redis失效或响应丢失不能假成功、删除草稿或提前ack。

保存草稿只有在 IndexedDB 事务完成后才算持久化，不能以单次 put 请求成功代替。实际 `QuotaExceededError` 可触发一次已校验 Provider Module 的 `reclaimContentCache`，然后重试同一份草稿；存储估算不能代替真实写入结果。Provider 只回收未被活动运行租约占用、可重新下载的 Content I/O 缓存，不访问存档、草稿、挂载来源或其他缓存。该入口不依赖仍存活的 PlayerRuntime，退出后的最终存档也适用。回收和重试失败保留原错误原因、同一 commitId 与可导出的内存副本；其他写入错误不触发回收，用户重试仍先确认本地持久化。本地草稿清理失败也保留内存副本，服务端提交重试继续使用原 commitId。

resources由宿主允许ID/Range访问；runtime统一解释ZIP编码、虚拟paths、FILE_TREE/EXTERNAL_FILE_SET及bridgeAssetPath。隔离web bridgeUrl来自实际Target唯一桥资产，宿主不猜引擎。checkpoint HTTP与LOCAL restore均通过原V1公开Host接口。只有purpose=play收到running事件后更新最近表；创建run不提前记最近，review不进入个人最近。

Host 在首次导航前为游戏 iframe 设置 allow-modals，允许游戏的 alert/confirm/prompt；隔离 Web adapter 沿用同一权限。缺少该标志时浏览器会忽略原生确认框并按取消处理。运行 origin、内容桥、弹出窗口与顶层导航限制保持原有边界，游戏确认框不经过 Host 专用接口。

Flycast 的街机内容由所选核心同次构建生成的 ROM 名录判定。GD-ROM 入口保留原始 ZIP，并以名录声明的相对路径关联 CHD；分拆 ROM 的 Parent ZIP 按核心保存在现有运行配置中。ZIP、Parent 和 CHD 使用独立的按需读取句柄，BIOS 仍按声明单独交付。缺少必需文件阻止准备，CRC 差异只提供审核警告。更换入口后重新计算依赖并通过现有引用检查退役旧 Parent，无数据库结构变化。

浏览器 Flycast 的 GD-ROM 在启动 SH4 JIT 前按扇区填充核心已有的 DIMM 内存，避免未插桩的动态代码跨越异步读取边界。压缩 CHD 仍通过有界分段读取交付，不在 JavaScript 或原生文件系统额外保留整份副本；这会增加首次启动的准备时间。

普通runtime/Provider升级在核心文件、存档格式与ROM未变时保持存档可恢复；列表与实际恢复共用runtime判定。沿用现有extinfo字段，无数据库结构变更或历史存档回填。历史指纹不做映射或迁移，也不单独识别旧算法。

Save 的 restorable / restoreReason / restoreWarning 是当前态投影，不持久化。runtime 的统一 restorable 与 batch-restorable 返回独立 warning，只有核心文件指纹不同且其他条件可恢复时是 CORE_CHANGED。恢复 Run 的 extinfo 使用当前实际运行核心指纹，Run.save 保留原存档身份与警告。跨版本恢复的来源存档不作为自动或手动覆盖目标，首次保存创建新档，之后更新新档；服务端也拒绝该Run覆盖来源ID。失败草稿保留同一commitId，重试不会额外复制。无需数据库迁移、指纹映射或旧核心回滚。
