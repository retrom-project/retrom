# Retrom 文档

从部署和使用、开发维护或验证标准进入。平台与核心的声明及内容规则由配套 retrom-runtime 维护；Retrom 文档说明游戏库、账号、导入审核、运行与受管数据的产品流程。

## 部署与使用

| 文档 | 内容 |
| --- | --- |
| [Docker 部署](docker-deployment.md) | 发行镜像、域名与 HTTPS、持久存储、启动检查及升级 |
| [PWA](pwa.md) | 安装、联网游玩、断网提示与更新 |
| [游戏目录](platform-instance.md) | 平台、允许核心、默认核心与推荐目录 |
| [来源文件示例](import-source-examples.md) | Pegasus／EmulationStation 最小目录、元数据和扫描步骤 |
| [导入与审核](import-and-review.md) | 服务器来源、共享审核、内容替换及退回审核 |
| [BIOS 与街机依赖](bios-and-arcade.md) | 依赖安装、扫描、Parent ZIP 与 CHD |
| [收藏与收藏夹](favorites-and-collections.md) | 私人分类、未分类与删除行为 |
| [共享标签](game-tags.md) | 标签维护、关联、筛选与软删除 |
| [运行与存档](runtime-and-play-data.md) | runtime 工具、冻结身份、资源与恢复 |

完整平台／核心对应关系、Provider Target 和存档类型见 runtime 的[支持清单](https://github.com/retrom-project/retrom-runtime/blob/master/docs/supported-platforms-and-cores.md)；核心内容格式、BIOS 和加载前提见 [Target 指南](https://github.com/retrom-project/retrom-runtime/blob/master/docs/targets/README.md)。这些链接跟随 runtime 主干，已部署环境应核对其配套版本。

## 开发与维护

| 文档 | 内容 |
| --- | --- |
| [PFB 开发](pfb-development.md) | 源码准备、初始化、Provider 导入、构建、启动及日常迭代 |
| [产品与模块](retrom-product-architecture.md) | 八模块、职责与依赖方向 |
| [数据模型](data-model.md) | 20 张业务表、状态、索引与应用完整性 |
| [HTTP 契约](http-api-contract.md) / [OpenAPI](../api/openapi.yaml) | 权限、请求、错误和字段定义 |
| [界面与产品行为](ui-specification.md) | 普通／移动／沉浸界面与共享控件规则 |
| [存储与数据库](storage-and-database.md) | 受管目录、文件发布、清理与持久化 |
| [账号与配置](backend-api-and-operations.md) | 登录保护、配置、代理与服务边界 |
| [配套运行输入](dependency-management.md) | 版本 pin、归档认证、开发候选与发行 |
| [平台与核心接入](platform-cores-integration.md) | runtime 与 Host 的接入职责及交付入口 |
| [街机事实源](arcade-dat-baseline.md) | DAT、核心构建与公共投影的关系 |
| [BBK RPG 接入边界](bbkrpg-integration.md) | 既有接入的 Host 边界与运行规则入口 |
| [质量与测试](engineering-quality-and-testing.md) | 工程检查、真实数据库及浏览器回归 |
| [实施与交付](implementation-plan.md) | 变更、评审和交付流程 |

API 字段以当前 OpenAPI 及 Provider V1 schema 为准；运行规则、核心声明和指纹由 runtime 拥有。文档与实现冲突时核对事实源并修正文档。

## 验证标准

完整检查标准见[项目验收](project-acceptance.md)，逐核心产品验证要求见[运行产品验证边界](core-runtime-validation.md)，执行入口见[质量与测试](engineering-quality-and-testing.md)。

文档维护当前行为、操作方法和检查标准。各次执行结果在对应 CI、PR、Release 或本地报告中核对；私有素材及机器证据保存在忽略的验收 workspace，不在 docs 累积阶段回执和历史通过清单。
