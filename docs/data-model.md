# 数据模型

目标 schema 由 migrations 按编号顺序叠加得到：001_schema.sql 为初始建表，002_query_indexes.sql 补查询索引，003_content_packages.sql 增加项目包成员表和活动包唯一索引，004_file_ownership_indexes.sql 将文件归属索引覆盖到 active／deleted，005_title_order.sql 为 game_tab 增加标题字母序列并重建列表索引。新库和既有库执行同一序列，新库不跳过后续 migration。已发布的 migration 文件固化不再修改，schema 变化只能追加新编号文件。迁移不读取或重打包游戏字节。应用数据库包含以下20张业务表和schema_migrations_tab技术账本，没有FK、CHECK、用户TRIGGER、VIEW、materialized VIEW、DOMAIN或业务函数。

| 表 | 当前事实 |
| --- | --- |
| instance_state_tab | 空实例一次初始化及管理员标识 |
| user_tab | 唯一身份、角色、状态、会话代数和CAS版本 |
| user_credential_tab | 密码hash、方案、变化时间 |
| auth_session_tab | hash令牌、闲置/绝对有效期和撤销 |
| account_link_tab | 邀请/密码重置的hash令牌、一次消费、撤销和版本 |
| platform_instance_tab | 人工创建的目录 |
| platform_instance_core_tab | 目录允许的核心及排序 |
| game_tab | 资料、排序名与标题字母序、runtime配置、contentHash、来源、生命周期 |
| game_file_tab | Game 独占的物理内容文件及退休状态；项目包只有一条活动记录 |
| game_file_member_tab | 不可变 STORE ZIP 的逻辑路径、偏移、大小、SHA 和 MIME；归属物理包 |
| game_media_tab | 封面、截图和其他媒体 |
| save_tab | User/Game私人存档、extinfo、最后commitId及CAS |
| recent_game_tab | User/Game最后成功运行时间 |
| bios_file_tab | requirementKey当前安装与退休文件 |
| scan_progress_tab | 临时扫描类型、状态、总数与计数 |
| tag_tab | 共享标签、软删除和版本 |
| game_tag_tab | Game/Tag关系 |
| favorite_tab | User/Game收藏 |
| favorite_folder_tab | User私人收藏夹 |
| favorite_folder_game_tab | User/Folder/Game成员关系 |

ID使用规范小写UUID字符串并以TEXT存储，HTTP UUID参数和领域写入统一校验；关系保留明确user_id。时间为Unix毫秒BIGINT。TEXT索引比原生UUID占用更大；规模验收测实际索引和计划，不以类型决定容量。目录与核心关系不由migration/启动seed生成。

Game状态pending_review→published→deleted→purged；拒绝待审直接deleted。文件状态active→deleted→purged。Tag软删除保留关系并即时退出展示/筛选/计数，不批量增加Game版本；同名重建获得新ID。User状态active/disabled/deleted，停用/删除/密码变化撤销有效会话。

应用用例承担完整性。IO在事务外完成，Game/文件/媒体/Tag及成功扫描计数在一个短事务提交；BIOS安装与成功计数同事务；保存锁Game并确认published，再执行存档版本CAS。目录删除按普通SELECT判断有无待审/已发布Game，不建立全局目录锁协议。导入提交前普通复查目录；不承诺并行强内容去重。

列表先分页候选行再组装标签/媒体。按标题排序的列表以 title_sort_key,title,id 排序；全局Game索引匹配status,title_sort_key,title,id，目录索引匹配platform_instance_id,status,title_sort_key,title,id。

标题字母序：title_initial 是字母分组 A–Z 或 #（拉丁字母取基础字母，忽略大小写、重音和全角；汉字取 CLDR 中文排序的拼音首字母；数字、符号、假名等归 #）。title_sort_key（COLLATE "C"）为“分组序号 + 中文排序键”，A–Z 依次在前、# 最后，同一字母内拉丁与汉字标题相邻。title_sort_name 是管理员可选的排序名，非空时代替标题参与分组和排序，用于修正多音字。title_order_version 记录计算规则版本：新增和修改 Game 在同一写入中算好三列；后端启动、对外服务前只把版本不一致的行分批重算（升级、回滚后再升级、从旧备份恢复或直接改库时才会命中），不递增 Game 版本。active／deleted storage_key和save screenshot_key索引服务活动引用与退休归属复核，清理不逐文件全表扫描。所有关系查询在SQL中限定当前user_id及可见状态。

项目包的 game_file_tab.role 为 project_package，size_bytes/sha256 描述整个物理 ZIP。成员表以 (game_file_id, logical_path COLLATE "C") 为复合主键，保存 data_offset、size_bytes、sha256、media_type；没有成员 UUID、role、status、JSON 清单或独立生命周期。原生 ROM/ZIP 仍是普通物理文件，不按扩展名展开成员。一个活动项目包不能与其他活动内容文件混合。

成员按 1000 行一批在包发布事务内插入；点查及路径游标分页使用复合主键。普通游戏列表与详情不加载整个成员表，配置解释和启动通过逻辑内容目录读取，进程目录缓存最多 16 包、估算 32 MiB。包的物理摘要及打包排列不进入项目 ROM 身份。退休包的物理删除成功后，成员索引每轮最多删除 1000 行；父记录保留以支持中断后继续清理。
