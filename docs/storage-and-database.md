# 受管文件与数据库

PostgreSQL保存事实，Redis保存TTL临时run与原子作用域限流，本地POSIX文件系统保存payload。管理员选择的来源路径只读；批准、启动、恢复只消费受管文件，源文件移走不影响已导入内容。私有ROM/BIOS和路径不提交Git。

所有权目录为managed/games/<game-id>、saves/<save-id>、bios/<bios-file-id>和temporary/<uuid>。准备先写temporary独占文件，计算SHA/大小、fsync，再原子rename并fsync文件/父目录。通用存储允许0字节项目文件；ROM/save/BIOS领域在适用边界要求非空。DB事务内不进行复制、解包或Node运行准备。

文件替换写新路径，再短事务活动切换；退休文件24小时宽限。单维护worker每分钟处理≤50个deleted Game、每类≤100退休文件、≤50个仍有私人资源的deleted User，以及≤1000个文件系统遍历步骤。扫描器保留目录偏移，空ReadDir批次和并发消失目录安全处理，不全量walk/sort。终态scan进度超过24小时分批删除≤200。

删除Game先收回Game及Save owner字节，再短事务清关系和从属行、标记purged。移除失败保留deleted和从属路径，下轮重试；退休单文件先复查当前active引用，引用存在保留字节。孤立文件只在宽限后且当前引用复核为否时移除；active文件不因旧row或历史source而被误删。清理失败记录错误，不建立持久工作流事件或恢复计划。

数据库迁移没有业务seed；002只就地调整分页和引用索引，不清空数据。破坏性开发reset必须停止exact PFB并归档data与postgres，保留Provider/缓存/ID/URL；不能以旧schema读取层代替重建。
