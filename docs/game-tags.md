# 游戏标签

本文是 Retrom 实例级游戏标签的领域事实源。表字段与数据库保护以 [`data-model.md`](./data-model.md) 为准，HTTP 字段与错误以 [`http-api-contract.md`](./http-api-contract.md) 和 `api/openapi.yaml` 为准，页面布局以 [`ui-specification.md`](./ui-specification.md) 与统一设计源为准。

## 1. 边界与权限

- Tag 是整个 Retrom 实例共享的管理员分类；它不属于 Profile，不是 FavoriteFolder，也不改变 Game 对一个 PlatformInstance 的唯一归属。
- 只有 `ADMIN` 可以创建、重命名、删除和分配标签。任意已登录用户只能读取其可见游戏已经关联的活动 `TagReference`。
- 标签必须先在 `/admin/tags` 创建。导入、Pegasus Collection 映射、审核和游戏维护只引用既有活动 Tag ID，不允许自由文本新建。
- 一期 Tag 只有名称、状态、版本、actor 和时刻；不包含颜色、图标、说明、层级、别名、规则或手工顺序。外部 `tags:`、Hasheous Tags、genre、文件名和目录名都不会自动创建或匹配 Tag。
- Tag 不进入 MetadataRevision、GameContentRevision、Variant、Launch、Player、存档、Core/DAT/BIOS 或内容 identity。

`TagReference` 固定为 `{tagId,name}`，只指向活动 Tag。Game、ReviewDraft 与 Pegasus Collection 均可关联 0–20 个活动 Tag；关系无顺序，读取按 `name_key,tag_id` 稳定排序。

## 2. 名称、容量与生命周期

服务端对创建与重命名名称执行一套共享算法：

1. 转为 Unicode NFC；
2. 拒绝任意 Unicode control code point；
3. 删除首尾 Unicode 空白，并把内部连续空白折叠为一个 ASCII 空格；
4. 规范显示名必须为 1–40 个 Unicode code point 且不超过 160 UTF-8 byte；
5. `name_key` 使用 Unicode case-fold，活动标签按它全局唯一；
6. `search_text` 使用与游戏关键字查询一致的空白折叠小写文本。

每实例最多 1,000 个活动 Tag，DELETED tombstone 不计入上限。达到上限返回 `TAG_LIMIT_REACHED`；owner 超过 20 个返回 `TAG_ASSIGNMENT_LIMIT_EXCEEDED`，重复、非法或不存在/已删除 ID 整体拒绝，不能截断或静默去重。

删除是不可恢复的软删除：管理员必须提供当前 Tag ETag 和逐 code point 等于当前显示名的 `confirmName`。删除事务保留 Tag 与全部关系作为历史证据，将 Tag 标为 `DELETED`，推进所有受影响 Game、待审核 ReviewDraft 和未完成 Pegasus aggregate 的版本并写审计。它立即退出选择器、当前投影、关键字和精确筛选；旧编辑器提交时必须因 owner ETag 过期而冲突。之后可用同一规范名称创建新 ID，但新 Tag 不继承旧关系。

标签 create/rename/delete 和每个关系集合变化都会推进相关 Tag version。重命名只改变 Tag 本身；由于搜索和投影动态连接活动 Tag，名称会立即更新而不会重写 `games.search_text` 或创建 MetadataRevision。

## 3. 关系写入与事务

所有集合替换遵守同一过程：先验证数组长度、规范 UUIDv7、重复和全部 Tag 的活动状态；读取当前活动集合；完全相同则 no-op；否则删除不再选择的活动关系、插入新增关系，保留指向 DELETED tombstone 的历史行，推进 touched Tag 和 owner aggregate 的版本并记录领域事件或审计。数据库 trigger 再次保护活动状态、owner 状态和 20 个上限。

- `PUT /admin/games/{gameId}/tags` 以 Game `If-Match` 原子替换 PUBLISHED 或 DELETED Game 的当前标签，推进 Game version 并写 `GAME_TAGS_REPLACED` 审计；它不创建 MetadataRevision。
- Review 标签属于 ReviewDraft version。PATCH 自动保存的 `tagIds` 与标题、媒体、Validation 等草稿选择共同提交；Approve 在原发布事务内重新验证活动 Tag，并将当前 ReviewDraftTag 原子复制到 GameTag。任何失败都回滚整个发布。Discard 保留关系与最终 ReviewEvent 的名称快照。
- Pegasus Collection 标签属于 mapping version。每个 `IMPORT` Collection 的映射保存关系及稳定 `{tagId,name}` snapshot；`SKIP` 必须是空数组。start 后映射冻结，retry 复用该映射；handoff 只把仍活动的选择复制到所创建的 ReviewDraft，且崩溃恢复不得重复写入。

Tag 删除与关系变化都在短数据库写事务内完成，不执行文件扫描、hash、归档读取或网络访问。Tag 删除使用 Tag ETag；Game/Review/Pegasus 写使用各自 owner ETag，因此删除和并发分配只有一种提交顺序能成功。

## 4. 普通导入与 Pegasus

普通文件/目录导入在“确认配置”选择本批默认 `tagIds`。服务端在创建事务中先验证全部 Tag，再冻结 `{tagId,name}` 配置快照，并为本批每个新 ReviewDraft 写入相同初始集合；任一引用失效时零 Import/Item/UploadConsumption 写入。reconfigure 使用相同规则，只预填原快照中仍活动的 Tag。

Pegasus 在 Collection 映射步骤逐项选择默认标签，可用批量辅助把既有标签 union 到待导入 Collection。每个 Collection 可有不同集合，恢复页面按当前活动关系显示，start 再检查映射仍有效。Pegasus metadata 自带标签或 genre 只保留为外部元信息，不会关联 Retrom Tag；`SKIPPED_EXISTING` 也不能改变既有 GameTag。

批次和 Collection 选择只提供审核默认值。管理员可在审核详情逐游戏调整，最终以决定前成功冲刷的 ReviewDraftTag 为准。

## 5. 搜索、投影与可见性

游戏关键字 `q` 在 SQL 分页之前匹配既有游戏搜索文本或任一活动 Tag 名；`GET /games`、`GET /admin/games` 和 `GET /admin/reviews` 另接受一个精确 `tagId`。`q`、`tagId`、平台、目录和既有状态条件取交集，cursor digest 绑定 `tagId`，不能跨筛选复用。不存在或已删除 `tagId` 得到合法空页，格式非法仍是 `400 INVALID_REQUEST`。

用户列表只投影 PUBLISHED 且目录可见 Game 的活动标签；管理列表可投影 PUBLISHED/DELETED Game 的活动标签。标签随 Game summary/detail 进入游戏库、首页、最近、收藏、存档、联机选择、管理游戏和审核；数组始终为 `[]` 或按稳定顺序排列的引用，不得为 null。列表通常显示前 2–3 个和可访问的 `+N`，详情显示全部；Player 和运行时响应不携带标签。

标签管理页可读取 ACTIVE/DELETED 列表、summary 与 usage。DELETED 行只读且保留历史 usage；普通 USER 没有列出全实例 taxonomy 的端点，以免暴露不可见游戏使用的分类。

## 6. 管理与用户界面

- App Shell 在“游戏管理”之后提供 `/admin/tags`。页面包含活动/关联游戏/待审核摘要、名称/状态/排序筛选、桌面表与移动卡片、创建/重命名 Drawer 和带 usage、完整名称确认的删除 Dialog。
- 通用 TagPicker 使用 combobox/listbox 语义，支持 ArrowUp/Down、Enter、Escape、带完整名称的移除按钮、20 个上限朗读和空 taxonomy 管理链接；listbox 通过顶层浮动层呈现并随输入位置更新，按视口剩余空间向下或向上展开，不能被 Drawer、列表或其他滚动容器裁剪；它只管理受控选择，业务 feature 负责读取与提交。
- 游戏库搜索提示明确包含标签，并用 `tagId` 单选筛选写入 URL；游戏详情的每个 chip 链接回 `/library?tagId=...`。genre 与 Tag 始终分开展示。
- 普通导入、Pegasus Collection、ReviewDraft 和管理员游戏详情均使用同一活动 TagPicker；成功写入后必须采用响应的新 owner version，冲突时刷新真实聚合，不能在客户端猜测 rename/delete 结果。
- 收藏夹 chip 和实例标签有不同区域与可访问名称，避免把 Profile 私有集合误认为共享 taxonomy。

响应式基线为 390×844、1280×800、2560×1440，以及物理 3840×2160、系统缩放 150%（CSS viewport 2560×1440、DPR 1.5）的 4K 场景。chip 可换行、长名称截断但保留 title，不得制造页面级横向溢出；Drawer/Dialog 遵守焦点圈定、Esc 关闭和关闭后焦点恢复。

## 7. 错误、审计与验证

稳定领域错误为 `TAG_NAME_INVALID`、`TAG_NAME_CONFLICT`、`TAG_LIMIT_REACHED`、`TAG_NOT_FOUND`、`TAG_ALREADY_DELETED`、`TAG_REFERENCE_INVALID`、`TAG_ASSIGNMENT_LIMIT_EXCEEDED`、`TAG_DELETE_CONFIRMATION_MISMATCH` 与 `VERSION_CONFLICT`。状态码、header 和 envelope 由 HTTP 契约规定。

Tag create/rename/delete 写 `TAG_CREATED/TAG_RENAMED/TAG_DELETED` AuditEvent；Game 集合替换写排序后的 before/after 和 added/removed。Review 沿用 `DRAFT_SAVED` 与最终 ReviewEvent，Pegasus 沿用 mapping snapshot。日志和错误只包含非秘密 ID、计数与稳定错误码。

统一验收入口是 [`project-acceptance.md`](./project-acceptance.md) 的 `ACC-TAG-001`–`ACC-TAG-005`。本能力不进入模拟器装载、内容交付、帧执行或存档协议，因此不触发 core smoke 或依赖/fixture 基线重验；若后续改动越过该边界，必须重新沿实际调用链判定。
