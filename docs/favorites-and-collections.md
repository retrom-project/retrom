# 收藏与收藏夹

本文是 Retrom 收藏能力的跨模块专题入口。数据字段以 [`data-model.md`](./data-model.md) 为唯一事实源，HTTP 细节以 [`http-api-contract.md`](./http-api-contract.md) 为唯一事实源，页面行为以 [`ui-specification.md`](./ui-specification.md) 为唯一事实源，验收流程只在 [`project-acceptance.md`](./project-acceptance.md) 维护。

## 1. 领域边界

- Favorite 是认证 Profile 与共享 Game 的私有关系；两个账号收藏同一 Game 时互不影响。
- FavoriteFolder 是 Profile 私有的命名容器，一款已收藏游戏可进入零到多个收藏夹；它与 Game 唯一归属的 PlatformInstance（用户文案“游戏集合”）无关。
- “全部收藏”和“未分类”是派生视图。加入收藏夹会自动收藏；从最后一个收藏夹移除仍保留收藏；取消收藏原子移除全部成员关系；删除收藏夹保留收藏。
- 管理员没有 owner bypass。Game 软删除或 PlatformInstance 停用只隐藏投影并保留关系，恢复可见后原关系自然恢复。
- 能力不引入后台 Job、Blob、外网、Core/DAT、Player 或新的第三方服务。

## 2. 实现组成

- Migration 025 新增 `favorite_games`、`favorite_folders`、`favorite_folder_games`，以复合外键阻止跨 Profile 关系。
- `internal/favorites` 在短 `BEGIN IMMEDIATE` 事务中实现收藏、精确分类、批量整理、取消/恢复及 Folder 生命周期。
- `/api/v1/favorites*` 与 `/api/v1/favorite-folders*` 使用认证 Principal、同源 CSRF、签名 cursor、ETag 和 principal-scoped 幂等。
- `/favorites` 提供全部、未分类和收藏夹视图；游戏库与详情提供收藏和管理收藏夹入口。
- 发布前依次通过 `ACC-FAV-001`–`004`；详细命令与证据格式见统一验收文档。

## 3. 设计来源

实施视觉基线保存在 [`../td/004-favorites-and-collections/collection-design.html`](../td/004-favorites-and-collections/collection-design.html)。其中图片、数量和标题仅是评审数据，不进入 seed、fixture 或生产源码。历史设计决策见同目录 `README.md`；本专题及其链接的正式文档是实施后的唯一契约。
