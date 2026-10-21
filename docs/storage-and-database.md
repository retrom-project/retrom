# 受管文件与数据库

PostgreSQL保存事实，Redis保存TTL临时run与原子作用域限流，本地POSIX文件系统保存payload。管理员选择的来源路径只读；批准、启动、恢复只消费受管文件，源文件移走不影响已导入内容。私有ROM/BIOS和路径不提交Git。

受管存储的共同根目录为 `RETROM_DATA_DIR/managed/`，数据库的 `storage_key` 相对此根记录：

| 内容 | 相对受管根的路径 |
| --- | --- |
| 游戏内容及媒体 | `games/<game-id>/<file-uuid>` |
| 存档 payload 及截图 | `saves/<save-id>/<file-uuid>` |
| BIOS／运行依赖 | `bios/<bios-file-id>/<file-uuid>` |
| 尚未发布的临时文件 | `temporary/<uuid>` |

例如存档实际位于 `RETROM_DATA_DIR/managed/saves/`，不在 `RETROM_DATA_DIR/saves/`。账号链接密钥另存于 `RETROM_DATA_DIR/account-link.key`。浏览器上传暂存于 `RETROM_DATA_DIR/uploads/<upload-id>/`（`files/` 为完成的来源树，`parts/` 为接收中的文件），它是临时来源而非受管内容，扫描结束或会话失效后删除，可不纳入备份。备份应包含整个数据根和对应的 PostgreSQL 备份，步骤及停写要求见[Docker 部署指南](docker-deployment.md#数据备份与版本升级)。

准备先写 `temporary/` 独占文件，计算 SHA/大小、fsync，再原子 rename 并 fsync 文件/父目录。通用存储允许0字节项目文件；ROM/save/BIOS领域在适用边界要求非空。DB事务内不进行复制、解包或Node运行准备。

普通受管文件及每个逻辑成员上限为 8 GiB；项目最多 50,000 成员，正文总量最多 64 GiB。受管项目 ZIP 支持 ZIP64，物理上限另加每个成员两份最长路径、头部和 ZIP64 元数据的预算，不能套用 8 GiB 或恰好 64 GiB 的限制。目录/归档读取同时限制实际成员数量和字节数；拒绝危险、重复路径、符号链接和加密归档成员。

runtime 声明 ZIP_STORE 布局的项目在导入/替换的内容准备阶段写为一个 STORE ZIP；当前包含 RPG Maker 七种版本、Siglus、TyranoScript、洞窟物语、GameMaker、KRKR、ONS 和 ScummVM。原生 ZIP、单 ROM 及范围外文件集合使用 FILES 布局。输入仍接受目录、ZIP、7z；ASAR/EXE 等项目转换由 runtime 解释，不新增 gzip/RAR 导入能力。浏览器上传只是可选来源，不是打包依赖。

准备过程只在 temporary 下暂存逻辑内容，规范化后构建并逐成员回读校验、计算整包摘要，fsync/rename 后短事务发布一条物理包记录及成员索引。完成、失败及取消清理本次暂存；事务结果不明先按分配 ID 核实。STORE 不压缩正文，活动受管内容长期只保留一份包；只读来源原包不由系统删除。空间检查以当前可用空间、实际展开量和新包预算计算，并保留 1 GiB 余量；不按压缩输入大小乘二估算。

运行资源冻结具体 key、包记录、成员偏移、长度和 SHA。浏览器继续使用普通单文件 URL/Range，服务端返回逻辑成员的长度、强 ETag 和 MIME，不暴露物理偏移。Linux 保留可用的 sendfile 路径；每次请求使用独立描述符，有界 Read/Seek 及 ServeContent 的受限复制防止跨成员读取。Tyrano/ScummVM 等宿主工具也通过同一有界 locator 读取。

mkxp 所需 ZIP 的成员集合和路径完全一致时复用物理包；子集或路径不同则生成 Run 临时 ZIP。关闭时释放，活跃读取续期，过期或进程中断残留按两小时空闲宽限有界回收；不会每次启动都新增永久游戏 ZIP。

文件替换写新路径，再短事务活动切换；退休文件24小时宽限。单维护worker每分钟处理≤50个deleted Game、每类≤100退休文件、≤50个仍有私人资源的deleted User，以及≤1000个文件系统遍历步骤。扫描器保留目录偏移，空ReadDir批次和并发消失目录安全处理，不全量walk/sort。终态scan进度超过24小时分批删除≤200。

删除Game先收回Game及Save owner字节，再短事务清关系和从属行、标记purged。移除失败保留deleted和从属路径，下轮重试；退休单文件先复查当前active引用，引用存在保留字节。孤立文件只在宽限后且当前引用复核为否时移除；active文件不因旧row或历史source而被误删。清理失败记录错误，不建立持久工作流事件或恢复计划。

数据库迁移没有业务 seed；002/003 就地增加索引/成员表，不清空数据。新版本通过统一内容目录读取 FILES 和 ZIP_STORE 两种合法布局，既有散文件无需在启动时重打包。破坏性开发 reset 必须停止 exact PFB 并归档 data 与 postgres，保留 Provider/缓存/ID/URL。

升级后，管理员可在相同数据库、受管根及配套 runtime/Provider 环境执行：

```sh
retrom content pack-existing --dry-run --all
retrom content pack-existing --apply --game-id <UUID>
retrom content pack-existing --apply --all --limit 20
```

默认预览；--all 和 --game-id 二选一，每次默认最多处理 20 个项目，已完成或范围外的游戏不占限额。工具串行读取当前受管散文件并核验大小/SHA，不需要上传来源，不重新识别入口或生成配置。新包准备完成后锁定 Game 并复查版本/文件快照，原子切换文件布局、递增版本，保留 Game ID、状态、来源、配置、contentHash 及私人关系。失败保留原活动内容；已完成项目重跑跳过，提交结果不明先核实，空间不足停止后续转换。

旧文件继续按 24 小时宽限退休，批量迁移需累计预留所有尚未回收旧文件与新包同时存在的空间。正式转换前备份同一时间点的数据库和受管内容；仅回滚应用镜像不会撤销新的存储布局。此命令不在启动或数据库 migration 中自动执行。
